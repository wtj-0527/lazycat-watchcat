package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type NotificationSettings struct {
	Enabled               bool   `json:"enabled"`
	CriticalAlerts        bool   `json:"criticalAlerts"`
	WarningAlerts         bool   `json:"warningAlerts"`
	RecoveryNotifications bool   `json:"recoveryNotifications"`
	InspectionResults     bool   `json:"inspectionResults"`
	CooldownMinutes       int    `json:"cooldownMinutes"`
	QuietHoursEnabled     bool   `json:"quietHoursEnabled"`
	QuietStart            string `json:"quietStart"`
	QuietEnd              string `json:"quietEnd"`
}

func DefaultNotificationSettings() NotificationSettings {
	return NotificationSettings{
		Enabled: true, CriticalAlerts: true, WarningAlerts: true,
		RecoveryNotifications: true, InspectionResults: true,
		CooldownMinutes: 10, QuietStart: "22:00", QuietEnd: "08:00",
	}
}

func (s *Store) NotificationSettings(ctx context.Context) NotificationSettings {
	result := DefaultNotificationSettings()
	_, _ = s.GetSystemState(ctx, "notification.settings", &result)
	return result
}

func (s *Store) SetNotificationSettings(ctx context.Context, value NotificationSettings) error {
	if value.CooldownMinutes < 1 || value.CooldownMinutes > 1440 ||
		!validClock(value.QuietStart) || !validClock(value.QuietEnd) {
		return errors.New("invalid notification settings")
	}
	if err := s.SetSystemState(ctx, "notification.settings", value); err != nil {
		return err
	}
	if !value.Enabled {
		_, _ = s.db.ExecContext(ctx, `UPDATE notification_outbox SET status='cancelled',last_error='notifications disabled' WHERE status='pending'`)
	}
	raw, _ := json.Marshal(value)
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES('notification.settings.updated','settings','notifications',?,?)`,
		string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func notificationSettingsTx(ctx context.Context, tx *sql.Tx) NotificationSettings {
	result := DefaultNotificationSettings()
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM system_state WHERE name='notification.settings'`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &result)
	}
	return result
}

func validClock(value string) bool {
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	_, err := time.Parse("15:04", value)
	return err == nil
}

func notificationNextAttempt(now time.Time, settings NotificationSettings) time.Time {
	if !settings.QuietHoursEnabled {
		return now
	}
	location, _ := time.LoadLocation("Asia/Shanghai")
	local := now.In(location)
	startClock, _ := time.Parse("15:04", settings.QuietStart)
	endClock, _ := time.Parse("15:04", settings.QuietEnd)
	start := time.Date(local.Year(), local.Month(), local.Day(), startClock.Hour(), startClock.Minute(), 0, 0, location)
	end := time.Date(local.Year(), local.Month(), local.Day(), endClock.Hour(), endClock.Minute(), 0, 0, location)
	if !end.After(start) {
		if local.Before(end) {
			start = start.Add(-24 * time.Hour)
		} else {
			end = end.Add(24 * time.Hour)
		}
	}
	if !local.Before(start) && local.Before(end) {
		return end.UTC()
	}
	return now
}

func (s *Store) CancelAcceptedRisk(ctx context.Context, fingerprint string) error {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE alert_instances SET status='firing',accepted_at=NULL,accepted_until=NULL,updated_at=? WHERE fingerprint=? AND status='accepted'`,
		now.Format(time.RFC3339Nano), fingerprint)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrAlertNotFound
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES('alert.accept.cancelled','alert',?,'{}',?)`,
		fingerprint, now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListAcceptedRisks(ctx context.Context) ([]Alert, error) {
	alerts, err := s.ListAlerts(ctx, true)
	if err != nil {
		return nil, err
	}
	result := make([]Alert, 0)
	for _, alert := range alerts {
		if alert.Status == "accepted" {
			result = append(result, alert)
		}
	}
	return result, nil
}

func notificationEventEnabled(settings NotificationSettings, severity, transition, from string) bool {
	if !settings.Enabled {
		return false
	}
	if from == "accepted" {
		return false
	}
	if transition == "recovery" {
		return settings.RecoveryNotifications
	}
	if severity == "critical" {
		return settings.CriticalAlerts
	}
	return settings.WarningAlerts
}
