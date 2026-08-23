package store

import (
	"context"
	"database/sql"
	"time"
)

type StabilityInputs struct {
	LatestMetricAt       *time.Time `json:"latestMetricAt,omitempty"`
	PendingNotifications int        `json:"pendingNotifications"`
	LastRetentionAt      *time.Time `json:"lastRetentionAt,omitempty"`
}

func (s *Store) StabilityInputs(ctx context.Context) (StabilityInputs, error) {
	var out StabilityInputs
	var latest sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(received_at) FROM metrics`).Scan(&latest); err != nil {
		return out, err
	}
	if latest.Valid {
		value := parseTime(latest.String)
		out.LatestMetricAt = &value
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_outbox WHERE status='pending'`).Scan(&out.PendingNotifications); err != nil {
		return out, err
	}
	// Use the state row timestamp as a stable cross-version signal instead of
	// depending on the JSON payload written by a particular release.
	var updated string
	if err := s.db.QueryRowContext(ctx, `SELECT updated_at FROM system_state WHERE name='last_retention'`).Scan(&updated); err == nil {
		value := parseTime(updated)
		out.LastRetentionAt = &value
	} else if err != sql.ErrNoRows {
		return out, err
	}
	return out, nil
}
