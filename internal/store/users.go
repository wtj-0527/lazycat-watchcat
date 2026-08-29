package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type RuntimeUserDevice struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Model        string    `json:"model"`
	RemarkName   string    `json:"remarkName"`
	DeviceAPIURL string    `json:"deviceApiUrl,omitempty"`
	IsMobile     bool      `json:"isMobile"`
	IsTV         bool      `json:"isTv"`
	Lang         string    `json:"lang,omitempty"`
	TimeZone     string    `json:"timeZone,omitempty"`
	IsWifi       *bool     `json:"isWifi,omitempty"`
	Online       bool      `json:"online"`
	BindingTime  time.Time `json:"bindingTime,omitempty"`
	LoginTime    time.Time `json:"loginTime,omitempty"`
}

type RuntimeUser struct {
	DeviceID             string              `json:"deviceId"`
	UserID               string              `json:"userId"`
	Nickname             string              `json:"nickname"`
	Role                 string              `json:"role"`
	AppInstallPermission bool                `json:"appInstallPermission"`
	AppAccessNoLimit     bool                `json:"appAccessNoLimit"`
	AllowedAppIDs        []string            `json:"allowedAppIds"`
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
		allowedJSON, _ := json.Marshal(user.AllowedAppIDs)
		_, err = tx.ExecContext(ctx, `INSERT INTO user_runtime_state(device_id,user_id,nickname,role,app_install_permission,app_access_no_limit,allowed_app_ids_json,online,active_devices,total_devices,first_observed_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(device_id,user_id) DO UPDATE SET nickname=excluded.nickname,role=excluded.role,
			app_install_permission=excluded.app_install_permission,app_access_no_limit=excluded.app_access_no_limit,allowed_app_ids_json=excluded.allowed_app_ids_json,
			online=excluded.online,active_devices=excluded.active_devices,total_devices=excluded.total_devices,updated_at=excluded.updated_at`,
			deviceID, user.UserID, user.Nickname, user.Role, user.AppInstallPermission, user.AppAccessNoLimit, string(allowedJSON), user.Online, user.ActiveDevices, user.TotalDevices, first, nowRaw)
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
			wifi := -1
			if endpoint.IsWifi != nil {
				if *endpoint.IsWifi {
					wifi = 1
				} else {
					wifi = 0
				}
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO user_device_state(device_id,user_id,end_device_id,name,model,remark_name,device_api_url,is_mobile,is_tv,lang,time_zone,is_wifi,online,binding_time,login_time,updated_at)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(device_id,user_id,end_device_id) DO UPDATE SET name=excluded.name,model=excluded.model,
				remark_name=excluded.remark_name,device_api_url=excluded.device_api_url,is_mobile=excluded.is_mobile,is_tv=excluded.is_tv,
				lang=excluded.lang,time_zone=excluded.time_zone,is_wifi=excluded.is_wifi,online=excluded.online,
				binding_time=excluded.binding_time,login_time=excluded.login_time,updated_at=excluded.updated_at`,
				deviceID, user.UserID, endpoint.ID, endpoint.Name, endpoint.Model, endpoint.RemarkName, endpoint.DeviceAPIURL,
				endpoint.IsMobile, endpoint.IsTV, endpoint.Lang, endpoint.TimeZone, wifi, endpoint.Online, bindingRaw, loginRaw, nowRaw)
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
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,user_id,nickname,role,app_install_permission,app_access_no_limit,allowed_app_ids_json,online,active_devices,total_devices,first_observed_at,updated_at FROM user_runtime_state ORDER BY device_id,nickname,user_id`)
	if err != nil {
		return nil, err
	}
	var out []RuntimeUser
	indexes := map[string]int{}
	for rows.Next() {
		var u RuntimeUser
		var install, noLimit, online int
		var allowed, first, updated string
		if err = rows.Scan(&u.DeviceID, &u.UserID, &u.Nickname, &u.Role, &install, &noLimit, &allowed, &online, &u.ActiveDevices, &u.TotalDevices, &first, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		u.AppInstallPermission = install != 0
		u.AppAccessNoLimit = noLimit != 0
		_ = json.Unmarshal([]byte(allowed), &u.AllowedAppIDs)
		u.Online = online != 0
		u.FirstObservedAt = parseTime(first)
		u.UpdatedAt = parseTime(updated)
		indexes[u.DeviceID+"\x00"+u.UserID] = len(out)
		out = append(out, u)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}

	// Do not issue child queries while the parent rows are open. With the
	// intentionally small SQLite pool, two concurrent callers could otherwise
	// each hold one connection and wait forever for the other connection.
	drows, err := s.db.QueryContext(ctx, `SELECT device_id,user_id,end_device_id,name,model,remark_name,device_api_url,is_mobile,is_tv,lang,time_zone,is_wifi,online,binding_time,login_time
		FROM user_device_state ORDER BY device_id,user_id,online DESC,name`)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var deviceID, userID string
		var d RuntimeUserDevice
		var mobile, tv, wifi, on int
		var binding, login string
		if err = drows.Scan(&deviceID, &userID, &d.ID, &d.Name, &d.Model, &d.RemarkName, &d.DeviceAPIURL, &mobile, &tv, &d.Lang, &d.TimeZone, &wifi, &on, &binding, &login); err != nil {
			return nil, err
		}
		index, ok := indexes[deviceID+"\x00"+userID]
		if !ok {
			continue
		}
		d.IsMobile = mobile != 0
		d.IsTV = tv != 0
		if wifi >= 0 {
			value := wifi != 0
			d.IsWifi = &value
		}
		d.Online = on != 0
		d.BindingTime = parseTime(binding)
		d.LoginTime = parseTime(login)
		out[index].Devices = append(out[index].Devices, d)
	}
	return out, drows.Err()
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

func (s *Store) DeleteRuntimeUserDevice(ctx context.Context, deviceID, userID, endDeviceID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `UPDATE user_login_sessions SET logout_at=? WHERE device_id=? AND user_id=? AND end_device_id=? AND logout_at IS NULL`,
		now, deviceID, userID, endDeviceID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM user_device_state WHERE device_id=? AND user_id=? AND end_device_id=?`,
		deviceID, userID, endDeviceID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	if _, err = tx.ExecContext(ctx, `UPDATE user_runtime_state SET
		online=EXISTS(SELECT 1 FROM user_device_state d WHERE d.device_id=? AND d.user_id=? AND d.online=1),
		active_devices=(SELECT count(*) FROM user_device_state d WHERE d.device_id=? AND d.user_id=? AND d.online=1),
		total_devices=(SELECT count(*) FROM user_device_state d WHERE d.device_id=? AND d.user_id=?)
		WHERE device_id=? AND user_id=?`,
		deviceID, userID, deviceID, userID, deviceID, userID, deviceID, userID); err != nil {
		return err
	}
	return tx.Commit()
}
