package store

import (
	"context"
	"database/sql"
	"time"
)

type RuntimeUserDevice struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Model       string    `json:"model"`
	RemarkName  string    `json:"remarkName"`
	Online      bool      `json:"online"`
	BindingTime time.Time `json:"bindingTime,omitempty"`
	LoginTime   time.Time `json:"loginTime,omitempty"`
}

type RuntimeUser struct {
	DeviceID             string              `json:"deviceId"`
	UserID               string              `json:"userId"`
	Nickname             string              `json:"nickname"`
	Role                 string              `json:"role"`
	AppInstallPermission bool                `json:"appInstallPermission"`
	Online               bool                `json:"online"`
	ActiveDevices        int                 `json:"activeDevices"`
	TotalDevices         int                 `json:"totalDevices"`
	FirstObservedAt      time.Time           `json:"firstObservedAt"`
	UpdatedAt            time.Time           `json:"updatedAt"`
	Devices              []RuntimeUserDevice `json:"devices"`
}

type UserLoginSession struct {
	DeviceID    string     `json:"deviceId"`
	UserID      string     `json:"userId"`
	EndDeviceID string     `json:"endDeviceId"`
	LoginAt     time.Time  `json:"loginAt"`
	LogoutAt    *time.Time `json:"logoutAt,omitempty"`
}

func (s *Store) ObserveRuntimeUsers(ctx context.Context, deviceID string, users []RuntimeUser) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	nowRaw := now.Format(time.RFC3339Nano)
	seenUsers := map[string]bool{}
	seenDevices := map[string]bool{}
	for _, user := range users {
		if user.UserID == "" {
			continue
		}
		seenUsers[user.UserID] = true
		var first string
		err := tx.QueryRowContext(ctx, `SELECT first_observed_at FROM user_runtime_state WHERE device_id=? AND user_id=?`, deviceID, user.UserID).Scan(&first)
		if err == sql.ErrNoRows {
			first = nowRaw
		} else if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO user_runtime_state(device_id,user_id,nickname,role,app_install_permission,online,active_devices,total_devices,first_observed_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(device_id,user_id) DO UPDATE SET nickname=excluded.nickname,role=excluded.role,
			app_install_permission=excluded.app_install_permission,online=excluded.online,active_devices=excluded.active_devices,total_devices=excluded.total_devices,updated_at=excluded.updated_at`,
			deviceID, user.UserID, user.Nickname, user.Role, user.AppInstallPermission, user.Online, user.ActiveDevices, user.TotalDevices, first, nowRaw)
		if err != nil {
			return err
		}
		for _, endpoint := range user.Devices {
			if endpoint.ID == "" {
				continue
			}
			key := user.UserID + "\x00" + endpoint.ID
			seenDevices[key] = true
			var wasOnline int
			stateErr := tx.QueryRowContext(ctx, `SELECT online FROM user_device_state WHERE device_id=? AND user_id=? AND end_device_id=?`, deviceID, user.UserID, endpoint.ID).Scan(&wasOnline)
			if stateErr != nil && stateErr != sql.ErrNoRows {
				return stateErr
			}
			loginRaw, bindingRaw := "", ""
			if !endpoint.LoginTime.IsZero() {
				loginRaw = endpoint.LoginTime.UTC().Format(time.RFC3339Nano)
			}
			if !endpoint.BindingTime.IsZero() {
				bindingRaw = endpoint.BindingTime.UTC().Format(time.RFC3339Nano)
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO user_device_state(device_id,user_id,end_device_id,name,model,remark_name,online,binding_time,login_time,updated_at)
				VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(device_id,user_id,end_device_id) DO UPDATE SET name=excluded.name,model=excluded.model,
				remark_name=excluded.remark_name,online=excluded.online,binding_time=excluded.binding_time,login_time=excluded.login_time,updated_at=excluded.updated_at`,
				deviceID, user.UserID, endpoint.ID, endpoint.Name, endpoint.Model, endpoint.RemarkName, endpoint.Online, bindingRaw, loginRaw, nowRaw)
			if err != nil {
				return err
			}
			if endpoint.Online && (stateErr == sql.ErrNoRows || wasOnline == 0) {
				start := endpoint.LoginTime.UTC()
				if start.IsZero() || start.After(now) {
					start = now
				}
				_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO user_login_sessions(device_id,user_id,end_device_id,login_at,created_at) VALUES(?,?,?,?,?)`,
					deviceID, user.UserID, endpoint.ID, start.Format(time.RFC3339Nano), nowRaw)
			} else if !endpoint.Online && stateErr == nil && wasOnline != 0 {
				_, err = tx.ExecContext(ctx, `UPDATE user_login_sessions SET logout_at=? WHERE device_id=? AND user_id=? AND end_device_id=? AND logout_at IS NULL`, nowRaw, deviceID, user.UserID, endpoint.ID)
			}
			if err != nil {
				return err
			}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT user_id,end_device_id,online FROM user_device_state WHERE device_id=?`, deviceID)
	if err != nil {
		return err
	}
	type missing struct {
		user, endpoint string
		online         int
	}
	var absent []missing
	for rows.Next() {
		var m missing
		if err = rows.Scan(&m.user, &m.endpoint, &m.online); err != nil {
			rows.Close()
			return err
		}
		if !seenDevices[m.user+"\x00"+m.endpoint] {
			absent = append(absent, m)
		}
	}
	rows.Close()
	for _, m := range absent {
		if m.online != 0 {
			_, _ = tx.ExecContext(ctx, `UPDATE user_login_sessions SET logout_at=? WHERE device_id=? AND user_id=? AND end_device_id=? AND logout_at IS NULL`, nowRaw, deviceID, m.user, m.endpoint)
		}
		_, _ = tx.ExecContext(ctx, `UPDATE user_device_state SET online=0,updated_at=? WHERE device_id=? AND user_id=? AND end_device_id=?`, nowRaw, deviceID, m.user, m.endpoint)
	}
	for userID := range seenUsers {
		_, _ = tx.ExecContext(ctx, `UPDATE user_runtime_state SET online=EXISTS(SELECT 1 FROM user_device_state d WHERE d.device_id=? AND d.user_id=? AND d.online=1), active_devices=(SELECT count(*) FROM user_device_state d WHERE d.device_id=? AND d.user_id=? AND d.online=1), total_devices=(SELECT count(*) FROM user_device_state d WHERE d.device_id=? AND d.user_id=?) WHERE device_id=? AND user_id=?`, deviceID, userID, deviceID, userID, deviceID, userID, deviceID, userID)
	}
	return tx.Commit()
}

func (s *Store) ListRuntimeUsers(ctx context.Context) ([]RuntimeUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,user_id,nickname,role,app_install_permission,online,active_devices,total_devices,first_observed_at,updated_at FROM user_runtime_state ORDER BY device_id,nickname,user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuntimeUser
	for rows.Next() {
		var u RuntimeUser
		var install, online int
		var first, updated string
		if err = rows.Scan(&u.DeviceID, &u.UserID, &u.Nickname, &u.Role, &install, &online, &u.ActiveDevices, &u.TotalDevices, &first, &updated); err != nil {
			return nil, err
		}
		u.AppInstallPermission = install != 0
		u.Online = online != 0
		u.FirstObservedAt = parseTime(first)
		u.UpdatedAt = parseTime(updated)
		drows, qerr := s.db.QueryContext(ctx, `SELECT end_device_id,name,model,remark_name,online,binding_time,login_time FROM user_device_state WHERE device_id=? AND user_id=? ORDER BY online DESC,name`, u.DeviceID, u.UserID)
		if qerr != nil {
			return nil, qerr
		}
		for drows.Next() {
			var d RuntimeUserDevice
			var on int
			var binding, login string
			if qerr = drows.Scan(&d.ID, &d.Name, &d.Model, &d.RemarkName, &on, &binding, &login); qerr != nil {
				drows.Close()
				return nil, qerr
			}
			d.Online = on != 0
			d.BindingTime = parseTime(binding)
			d.LoginTime = parseTime(login)
			u.Devices = append(u.Devices, d)
		}
		drows.Close()
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) ListUserLoginSessions(ctx context.Context, since time.Time) ([]UserLoginSession, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,user_id,end_device_id,login_at,logout_at FROM user_login_sessions WHERE login_at>=? OR logout_at IS NULL ORDER BY login_at DESC`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserLoginSession
	for rows.Next() {
		var x UserLoginSession
		var login string
		var logout sql.NullString
		if err = rows.Scan(&x.DeviceID, &x.UserID, &x.EndDeviceID, &login, &logout); err != nil {
			return nil, err
		}
		x.LoginAt = parseTime(login)
		if logout.Valid {
			v := parseTime(logout.String)
			x.LogoutAt = &v
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
