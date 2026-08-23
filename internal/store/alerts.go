package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type AlertSignal struct {
	Fingerprint string    `json:"fingerprint"`
	DeviceID    string    `json:"deviceId"`
	DeviceName  string    `json:"deviceName"`
	Severity    string    `json:"severity"`
	Resource    string    `json:"resource"`
	Message     string    `json:"message"`
	Value       float64   `json:"value"`
	Unit        string    `json:"unit"`
	ObservedAt  time.Time `json:"observedAt"`
}

type Alert struct {
	AlertSignal
	Status          string     `json:"status"`
	FirstSeenAt     time.Time  `json:"firstSeenAt"`
	LastSeenAt      time.Time  `json:"lastSeenAt"`
	AcknowledgedAt  *time.Time `json:"acknowledgedAt,omitempty"`
	SilencedUntil   *time.Time `json:"silencedUntil,omitempty"`
	ResolvedAt      *time.Time `json:"resolvedAt,omitempty"`
	OccurrenceCount int        `json:"occurrenceCount"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

var ErrAlertNotFound = errors.New("alert not found")

func (s *Store) ReconcileAlerts(ctx context.Context, signals []AlertSignal) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seen := make(map[string]struct{}, len(signals))
	for _, signal := range signals {
		if signal.Fingerprint == "" || signal.DeviceID == "" {
			continue
		}
		seen[signal.Fingerprint] = struct{}{}
		if signal.ObservedAt.IsZero() {
			signal.ObservedAt = now
		}
		var status, severity, previousSeen string
		var silenced sql.NullString
		var occurrences int
		err := tx.QueryRowContext(ctx, `SELECT status,severity,silenced_until,occurrence_count,last_seen_at FROM alert_instances WHERE fingerprint=?`, signal.Fingerprint).Scan(&status, &severity, &silenced, &occurrences, &previousSeen)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.ExecContext(ctx, `INSERT INTO alert_instances(fingerprint,device_id,device_name,severity,status,resource,message,value,unit,first_seen_at,last_seen_at,occurrence_count,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				signal.Fingerprint, signal.DeviceID, signal.DeviceName, signal.Severity, "firing", signal.Resource, signal.Message, signal.Value, signal.Unit, signal.ObservedAt.Format(time.RFC3339Nano), signal.ObservedAt.Format(time.RFC3339Nano), 1, now.Format(time.RFC3339Nano)); err != nil {
				return err
			}
			if err := recordAlertTransition(ctx, tx, signal, "", "firing", "threshold-entered", now, true); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			nextStatus := status
			if status == "resolved" || (status == "silenced" && (!silenced.Valid || parseTime(silenced.String).Before(now))) {
				nextStatus = "firing"
			}
			if signal.ObservedAt.After(parseTime(previousSeen)) {
				occurrences++
			}
			if _, err := tx.ExecContext(ctx, `UPDATE alert_instances SET device_name=?,severity=?,status=?,resource=?,message=?,value=?,unit=?,last_seen_at=?,resolved_at=NULL,occurrence_count=?,updated_at=? WHERE fingerprint=?`,
				signal.DeviceName, signal.Severity, nextStatus, signal.Resource, signal.Message, signal.Value, signal.Unit, signal.ObservedAt.Format(time.RFC3339Nano), occurrences, now.Format(time.RFC3339Nano), signal.Fingerprint); err != nil {
				return err
			}
			if status != nextStatus {
				if err := recordAlertTransition(ctx, tx, signal, status, nextStatus, "threshold-reentered", now, true); err != nil {
					return err
				}
			} else if severity != signal.Severity && signal.Severity == "critical" && nextStatus != "silenced" {
				if err := recordAlertTransition(ctx, tx, signal, status, status, "severity-escalated", now, true); err != nil {
					return err
				}
			}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT fingerprint,device_id,device_name,severity,resource,message,value,unit FROM alert_instances WHERE status IN ('firing','acknowledged','silenced')`)
	if err != nil {
		return err
	}
	var resolving []AlertSignal
	for rows.Next() {
		var a AlertSignal
		if err := rows.Scan(&a.Fingerprint, &a.DeviceID, &a.DeviceName, &a.Severity, &a.Resource, &a.Message, &a.Value, &a.Unit); err != nil {
			rows.Close()
			return err
		}
		if _, ok := seen[a.Fingerprint]; !ok {
			resolving = append(resolving, a)
		}
	}
	rows.Close()
	for _, a := range resolving {
		var old string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM alert_instances WHERE fingerprint=?`, a.Fingerprint).Scan(&old); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE alert_instances SET status='resolved',resolved_at=?,updated_at=? WHERE fingerprint=?`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), a.Fingerprint); err != nil {
			return err
		}
		if err := recordAlertTransition(ctx, tx, a, old, "resolved", "threshold-cleared", now, true); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func recordAlertTransition(ctx context.Context, tx *sql.Tx, a AlertSignal, from, to, reason string, now time.Time, notify bool) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO alert_transitions(fingerprint,from_status,to_status,severity,reason,created_at) VALUES(?,?,?,?,?,?)`, a.Fingerprint, from, to, a.Severity, reason, now.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if !notify {
		return nil
	}
	transition := to
	title := "猫眼告警"
	body := fmt.Sprintf("[%s] %s · %s：%s", a.Severity, a.DeviceName, a.Resource, a.Message)
	if to == "resolved" {
		transition = "recovery"
		title = "猫眼告警已恢复"
		body = fmt.Sprintf("%s · %s：%s", a.DeviceName, a.Resource, a.Message)
	}
	dedupe := fmt.Sprintf("%s:%s:%d", a.Fingerprint, transition, now.UTC().UnixNano())
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO notification_outbox(dedupe_key,alert_fingerprint,transition,title,body,deeplink,next_attempt_at,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		dedupe, a.Fingerprint, transition, title, body, "lzc://community.lazycat.app.maoyan/alerts", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListAlerts(ctx context.Context, includeResolved bool) ([]Alert, error) {
	query := `SELECT fingerprint,device_id,device_name,severity,status,resource,message,value,unit,first_seen_at,last_seen_at,acknowledged_at,silenced_until,resolved_at,occurrence_count,updated_at FROM alert_instances`
	if !includeResolved {
		query += ` WHERE status!='resolved'`
	}
	query += ` ORDER BY CASE severity WHEN 'critical' THEN 0 ELSE 1 END,last_seen_at DESC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var first, last, updated string
		var ack, silence, resolved sql.NullString
		if err := rows.Scan(&a.Fingerprint, &a.DeviceID, &a.DeviceName, &a.Severity, &a.Status, &a.Resource, &a.Message, &a.Value, &a.Unit, &first, &last, &ack, &silence, &resolved, &a.OccurrenceCount, &updated); err != nil {
			return nil, err
		}
		a.ObservedAt = parseTime(last)
		a.FirstSeenAt, a.LastSeenAt, a.UpdatedAt = parseTime(first), parseTime(last), parseTime(updated)
		a.AcknowledgedAt, a.SilencedUntil, a.ResolvedAt = nullableTime(ack), nullableTime(silence), nullableTime(resolved)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) SetAlertState(ctx context.Context, fingerprint, action string, silenceFor time.Duration) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var a AlertSignal
	var old string
	err = tx.QueryRowContext(ctx, `SELECT device_id,device_name,severity,status,resource,message,value,unit FROM alert_instances WHERE fingerprint=?`, fingerprint).
		Scan(&a.DeviceID, &a.DeviceName, &a.Severity, &old, &a.Resource, &a.Message, &a.Value, &a.Unit)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAlertNotFound
	}
	if err != nil {
		return err
	}
	a.Fingerprint = fingerprint
	var result sql.Result
	switch action {
	case "acknowledge":
		result, err = tx.ExecContext(ctx, `UPDATE alert_instances SET status='acknowledged',acknowledged_at=?,updated_at=? WHERE fingerprint=? AND status!='resolved'`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), fingerprint)
	case "silence":
		if silenceFor <= 0 || silenceFor > 30*24*time.Hour {
			silenceFor = 24 * time.Hour
		}
		result, err = tx.ExecContext(ctx, `UPDATE alert_instances SET status='silenced',silenced_until=?,updated_at=? WHERE fingerprint=? AND status!='resolved'`, now.Add(silenceFor).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), fingerprint)
	case "resolve":
		result, err = tx.ExecContext(ctx, `UPDATE alert_instances SET status='resolved',resolved_at=?,updated_at=? WHERE fingerprint=? AND status!='resolved'`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), fingerprint)
	default:
		return errors.New("unsupported alert action")
	}
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrAlertNotFound
	}
	if err := recordAlertTransition(ctx, tx, a, old, map[string]string{"acknowledge": "acknowledged", "silence": "silenced", "resolve": "resolved"}[action], "user-"+action, now, action == "resolve"); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES(?,?,?,?,?)`, "alert."+action, "alert", fingerprint, "{}", now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}
func nullableTime(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t := parseTime(v.String)
	return &t
}
