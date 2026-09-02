package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

type PendingNotification struct {
	ID               int64
	AlertFingerprint string
	TargetDeviceID   string
	Transition       string
	Title            string
	Body             string
	Deeplink         string
	Attempts         int
}

type NotificationSummary struct {
	Pending int `json:"pending"`
	Sent    int `json:"sent"`
	Failed  int `json:"failed"`
	Total   int `json:"total"`
}

func (s *Store) NotificationSummary(ctx context.Context) (NotificationSummary, error) {
	var result NotificationSummary
	rows, err := s.reader().QueryContext(ctx, `SELECT status,COUNT(*) FROM notification_outbox GROUP BY status`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return result, err
		}
		result.Total += count
		switch status {
		case "pending":
			result.Pending = count
		case "sent":
			result.Sent = count
		case "failed":
			result.Failed = count
		}
	}
	return result, rows.Err()
}

func (s *Store) NextNotification(ctx context.Context) (PendingNotification, error) {
	var n PendingNotification
	if !s.NotificationSettings(ctx).Enabled {
		return n, sql.ErrNoRows
	}
	err := s.db.QueryRowContext(ctx, `SELECT n.id,n.alert_fingerprint,COALESCE(a.device_id,''),n.transition,n.title,n.body,n.deeplink,n.attempts
		FROM notification_outbox n LEFT JOIN alert_instances a ON a.fingerprint=n.alert_fingerprint
		WHERE n.status='pending' AND n.next_attempt_at<=? ORDER BY n.id LIMIT 1`, time.Now().UTC().Format(time.RFC3339Nano)).
		Scan(&n.ID, &n.AlertFingerprint, &n.TargetDeviceID, &n.Transition, &n.Title, &n.Body, &n.Deeplink, &n.Attempts)
	return n, err
}

func (s *Store) MarkNotificationSent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notification_outbox SET status='sent',sent_at=?,last_error='' WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (s *Store) MarkNotificationFailed(ctx context.Context, id int64, attempts int, message string) error {
	if len(message) > 1000 {
		message = message[:1000]
	}
	delay := time.Duration(math.Min(3600, math.Pow(2, float64(attempts))*30)) * time.Second
	status := "pending"
	if attempts >= 12 {
		status = "failed"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE notification_outbox SET status=?,attempts=?,next_attempt_at=?,last_error=? WHERE id=?`, status, attempts, time.Now().UTC().Add(delay).Format(time.RFC3339Nano), message, id)
	return err
}

func IsNoPendingNotification(err error) bool { return errors.Is(err, sql.ErrNoRows) }
