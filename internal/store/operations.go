package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type CapabilityStatus struct {
	DeviceID   string    `json:"deviceId"`
	Capability string    `json:"capability"`
	Status     string    `json:"status"`
	Detail     string    `json:"detail"`
	CheckedAt  time.Time `json:"checkedAt"`
}

type OperationalSettings struct {
	SystemIntervalSeconds   int `json:"systemIntervalSeconds"`
	RuntimeIntervalSeconds  int `json:"runtimeIntervalSeconds"`
	StorageIntervalSeconds  int `json:"storageIntervalSeconds"`
	AdvancedIntervalSeconds int `json:"advancedIntervalSeconds"`
	RawRetentionDays        int `json:"rawRetentionDays"`
	RollupRetentionDays     int `json:"rollupRetentionDays"`
	AuditRetentionDays      int `json:"auditRetentionDays"`
	InspectionRetentionDays int `json:"inspectionRetentionDays"`
	BackupRetentionCount    int `json:"backupRetentionCount"`
	DailyInspectionHour     int `json:"dailyInspectionHour"`
	WeeklyInspectionHour    int `json:"weeklyInspectionHour"`
}

func DefaultOperationalSettings() OperationalSettings {
	return OperationalSettings{
		SystemIntervalSeconds: 15, RuntimeIntervalSeconds: 30,
		StorageIntervalSeconds: 120, AdvancedIntervalSeconds: 600,
		RawRetentionDays: 30, RollupRetentionDays: 365, AuditRetentionDays: 180,
		InspectionRetentionDays: 365, BackupRetentionCount: 20,
		DailyInspectionHour: 3, WeeklyInspectionHour: 4,
	}
}

func (s *Store) OperationalSettings(ctx context.Context) OperationalSettings {
	result := DefaultOperationalSettings()
	_, _ = s.GetSystemState(ctx, "operational.settings", &result)
	return result
}

func (s *Store) SetOperationalSettings(ctx context.Context, value OperationalSettings) error {
	if value.SystemIntervalSeconds < 10 || value.SystemIntervalSeconds > 30 ||
		value.RuntimeIntervalSeconds < 15 || value.RuntimeIntervalSeconds > 30 ||
		value.StorageIntervalSeconds < 60 || value.StorageIntervalSeconds > 300 ||
		value.AdvancedIntervalSeconds < 300 || value.AdvancedIntervalSeconds > 1800 ||
		value.RawRetentionDays < 1 || value.RawRetentionDays > 365 ||
		value.RollupRetentionDays < value.RawRetentionDays || value.RollupRetentionDays > 3650 ||
		value.AuditRetentionDays < 1 || value.AuditRetentionDays > 3650 ||
		value.InspectionRetentionDays < 1 || value.InspectionRetentionDays > 3650 ||
		value.BackupRetentionCount < 1 || value.BackupRetentionCount > 100 ||
		value.DailyInspectionHour < 0 || value.DailyInspectionHour > 23 ||
		value.WeeklyInspectionHour < 0 || value.WeeklyInspectionHour > 23 {
		return errors.New("invalid operational settings")
	}
	if err := s.SetSystemState(ctx, "operational.settings", value); err != nil {
		return err
	}
	raw, _ := json.Marshal(value)
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES('settings.updated','settings','operational',?,?)`,
		string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) SetCapabilityStatuses(ctx context.Context, deviceID string, items []CapabilityStatus) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		if item.CheckedAt.IsZero() {
			item.CheckedAt = time.Now().UTC()
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO collector_capabilities(device_id,capability,status,detail,checked_at) VALUES(?,?,?,?,?)
			ON CONFLICT(device_id,capability) DO UPDATE SET status=excluded.status,detail=excluded.detail,checked_at=excluded.checked_at`,
			deviceID, item.Capability, item.Status, item.Detail, item.CheckedAt.Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListCapabilityStatuses(ctx context.Context) ([]CapabilityStatus, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT device_id,capability,status,detail,checked_at FROM collector_capabilities ORDER BY device_id,capability`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CapabilityStatus
	for rows.Next() {
		var item CapabilityStatus
		var checked string
		if err := rows.Scan(&item.DeviceID, &item.Capability, &item.Status, &item.Detail, &checked); err != nil {
			return nil, err
		}
		item.CheckedAt = parseTime(checked)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SetSystemState(ctx context.Context, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO system_state(name,value,updated_at) VALUES(?,?,?)
		ON CONFLICT(name) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`,
		name, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetSystemState(ctx context.Context, name string, dest any) (bool, error) {
	var raw string
	err := s.reader().QueryRowContext(ctx, `SELECT value FROM system_state WHERE name=?`, name).Scan(&raw)
	if IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), dest)
}

type SystemStateItem struct {
	Name      string          `json:"name"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

func (s *Store) ListSystemStates(ctx context.Context) ([]SystemStateItem, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT name,value,updated_at FROM system_state ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SystemStateItem
	for rows.Next() {
		var item SystemStateItem
		var raw, updated string
		if err := rows.Scan(&item.Name, &raw, &updated); err != nil {
			return nil, err
		}
		item.Value, item.UpdatedAt = json.RawMessage(raw), parseTime(updated)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) QueueNotification(ctx context.Context, dedupeKey, title, body, deeplink string) error {
	settings := s.NotificationSettings(ctx)
	if !settings.Enabled || !settings.InspectionResults {
		return nil
	}
	current := time.Now().UTC()
	now := current.Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO notification_outbox(dedupe_key,alert_fingerprint,transition,title,body,deeplink,next_attempt_at,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		dedupeKey, "system", "inspection", title, body, deeplink, notificationNextAttempt(current, settings).Format(time.RFC3339Nano), now)
	return err
}
