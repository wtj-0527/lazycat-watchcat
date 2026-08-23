package store

import (
	"context"
	"time"
)

type RuntimeApplication struct {
	DeviceID       string    `json:"deviceId"`
	DeployID       string    `json:"deployId"`
	AppID          string    `json:"appId"`
	Title          string    `json:"title"`
	Version        string    `json:"version"`
	InstallStatus  string    `json:"installStatus"`
	InstanceStatus string    `json:"instanceStatus"`
	Domain         string    `json:"domain"`
	Builtin        bool      `json:"builtin"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func (s *Store) ReplaceRuntimeApplications(ctx context.Context, deviceID string, items []RuntimeApplication) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seen := make([]string, 0, len(items))
	for _, item := range items {
		if item.DeployID == "" || item.AppID == "" {
			continue
		}
		seen = append(seen, item.DeployID)
		_, err := tx.ExecContext(ctx, `INSERT INTO application_runtime_state(
			device_id,deploy_id,app_id,title,version,install_status,instance_status,domain,builtin,updated_at
		) VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(device_id,deploy_id) DO UPDATE SET
			app_id=excluded.app_id,title=excluded.title,version=excluded.version,
			install_status=excluded.install_status,instance_status=excluded.instance_status,
			domain=excluded.domain,builtin=excluded.builtin,updated_at=excluded.updated_at`,
			deviceID, item.DeployID, item.AppID, item.Title, item.Version, item.InstallStatus,
			item.InstanceStatus, item.Domain, item.Builtin, now)
		if err != nil {
			return err
		}
	}
	if len(seen) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM application_runtime_state WHERE device_id=?`, deviceID); err != nil {
			return err
		}
	} else {
		placeholders := "?"
		args := make([]any, 0, len(seen)+1)
		args = append(args, deviceID)
		for index, deployID := range seen {
			if index > 0 {
				placeholders += ",?"
			}
			args = append(args, deployID)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM application_runtime_state WHERE device_id=? AND deploy_id NOT IN (`+placeholders+`)`, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListRuntimeApplications(ctx context.Context) ([]RuntimeApplication, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,deploy_id,app_id,title,version,install_status,instance_status,domain,builtin,updated_at
		FROM application_runtime_state ORDER BY title,app_id,deploy_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RuntimeApplication
	for rows.Next() {
		var item RuntimeApplication
		var builtin int
		var updated string
		if err := rows.Scan(&item.DeviceID, &item.DeployID, &item.AppID, &item.Title, &item.Version,
			&item.InstallStatus, &item.InstanceStatus, &item.Domain, &builtin, &updated); err != nil {
			return nil, err
		}
		item.Builtin = builtin != 0
		item.UpdatedAt = parseTime(updated)
		out = append(out, item)
	}
	return out, rows.Err()
}
