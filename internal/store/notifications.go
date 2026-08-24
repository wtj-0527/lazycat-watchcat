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
	rows, err := s.db.QueryContext(ctx, `SELECT status,COUNT(*) FROM notification_outbox GROUP BY status`)
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
	err := s.db.QueryRowContext(ctx, `SELECT id,alert_fingerprint,transition,title,body,deeplink,attempts FROM notification_outbox WHERE status='pending' AND next_attempt_at<=? ORDER BY id LIMIT 1`, time.Now().UTC().Format(time.RFC3339Nano)).
		Scan(&n.ID, &n.AlertFingerprint, &n.Transition, &n.Title, &n.Body, &n.Deeplink, &n.Attempts)
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
