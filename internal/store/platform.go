package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

type DeviceMetadata struct {
	DeviceID  string            `json:"deviceId"`
	Group     string            `json:"group"`
	Location  string            `json:"location"`
	Labels    map[string]string `json:"labels"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

func (s *Store) DeviceMetadataMap(ctx context.Context) (map[string]DeviceMetadata, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT device_id,group_name,location,labels_json,updated_at FROM device_metadata`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DeviceMetadata{}
	for rows.Next() {
		var item DeviceMetadata
		var labels, updated string
		if err := rows.Scan(&item.DeviceID, &item.Group, &item.Location, &labels, &updated); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labels), &item.Labels)
		if item.Labels == nil {
			item.Labels = map[string]string{}
		}
		item.UpdatedAt = parseTime(updated)
		out[item.DeviceID] = item
	}
	return out, rows.Err()
}

func (s *Store) SetDeviceMetadata(ctx context.Context, item DeviceMetadata) error {
	if strings.TrimSpace(item.DeviceID) == "" {
		return errors.New("device id required")
	}
	if item.Labels == nil {
		item.Labels = map[string]string{}
	}
	raw, err := json.Marshal(item.Labels)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices WHERE id=?`, item.DeviceID).Scan(&exists); err != nil || exists != 1 {
		return sql.ErrNoRows
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO device_metadata(device_id,group_name,location,labels_json,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(device_id) DO UPDATE SET group_name=excluded.group_name,location=excluded.location,labels_json=excluded.labels_json,updated_at=excluded.updated_at`,
		item.DeviceID, strings.TrimSpace(item.Group), strings.TrimSpace(item.Location), string(raw), now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	meta, _ := json.Marshal(map[string]any{"group": item.Group, "location": item.Location, "labels": item.Labels})
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES('device.metadata.updated','device',?,?,?)`,
		item.DeviceID, string(meta), now.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

type SavedView struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Query     json.RawMessage `json:"query"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

func (s *Store) ListSavedViews(ctx context.Context) ([]SavedView, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT id,name,query_json,created_at,updated_at FROM saved_views ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SavedView
	for rows.Next() {
		var item SavedView
		var raw, created, updated string
		if err := rows.Scan(&item.ID, &item.Name, &raw, &created, &updated); err != nil {
			return nil, err
		}
		item.Query, item.CreatedAt, item.UpdatedAt = json.RawMessage(raw), parseTime(created), parseTime(updated)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SaveView(ctx context.Context, id, name string, query any) (SavedView, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SavedView{}, errors.New("view name required")
	}
	if id == "" {
		id = uuid.NewString()
	}
	raw, err := json.Marshal(query)
	if err != nil {
		return SavedView{}, err
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO saved_views(id,name,query_json,created_at,updated_at) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,query_json=excluded.query_json,updated_at=excluded.updated_at`,
		id, name, string(raw), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return SavedView{}, err
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES('saved_view.updated','saved_view',?,?,?)`,
		id, string(raw), now.Format(time.RFC3339Nano))
	return SavedView{ID: id, Name: name, Query: raw, CreatedAt: now, UpdatedAt: now}, nil
}

func (s *Store) DeleteSavedView(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM saved_views WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,created_at) VALUES('saved_view.deleted','saved_view',?,?)`,
		id, time.Now().UTC().Format(time.RFC3339Nano))
	return nil
}

type MaintenanceWindow struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s *Store) ListMaintenanceWindows(ctx context.Context) ([]MaintenanceWindow, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT id,name,starts_at,ends_at,enabled,created_at,updated_at FROM maintenance_windows ORDER BY starts_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MaintenanceWindow
	for rows.Next() {
		var item MaintenanceWindow
		var starts, ends, created, updated string
		var enabled int
		if err := rows.Scan(&item.ID, &item.Name, &starts, &ends, &enabled, &created, &updated); err != nil {
			return nil, err
		}
		item.StartsAt, item.EndsAt, item.Enabled = parseTime(starts), parseTime(ends), enabled != 0
		item.CreatedAt, item.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SaveMaintenanceWindow(ctx context.Context, item MaintenanceWindow) (MaintenanceWindow, error) {
	if strings.TrimSpace(item.Name) == "" || item.StartsAt.IsZero() || !item.EndsAt.After(item.StartsAt) {
		return MaintenanceWindow{}, errors.New("invalid maintenance window")
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	item.CreatedAt, item.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `INSERT INTO maintenance_windows(id,name,starts_at,ends_at,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name,starts_at=excluded.starts_at,ends_at=excluded.ends_at,enabled=excluded.enabled,updated_at=excluded.updated_at`,
		item.ID, strings.TrimSpace(item.Name), item.StartsAt.UTC().Format(time.RFC3339Nano), item.EndsAt.UTC().Format(time.RFC3339Nano), item.Enabled,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,created_at) VALUES('maintenance_window.updated','maintenance_window',?,?)`,
			item.ID, now.Format(time.RFC3339Nano))
	}
	return item, err
}

func (s *Store) DeleteMaintenanceWindow(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM maintenance_windows WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) InMaintenance(ctx context.Context, now time.Time) (bool, error) {
	var count int
	err := s.reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM maintenance_windows WHERE enabled=1 AND starts_at<=? AND ends_at>?`,
		now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano)).Scan(&count)
	return count > 0, err
}

type AuditEntry struct {
	ID          int64           `json:"id"`
	Action      string          `json:"action"`
	SubjectType string          `json:"subjectType"`
	SubjectID   string          `json:"subjectId"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   time.Time       `json:"createdAt"`
}

func (s *Store) RecordAudit(ctx context.Context, action, subjectType, subjectID string, metadata any) error {
	raw := []byte("{}")
	if metadata != nil {
		var err error
		raw, err = json.Marshal(metadata)
		if err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES(?,?,?,?,?)`,
		action, subjectType, subjectID, string(raw), time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

type DeviceEvent struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"createdAt"`
}

func (s *Store) ListDeviceEvents(ctx context.Context, deviceID string, limit int) ([]DeviceEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.reader().QueryContext(ctx, `
		SELECT id,type,title,detail,created_at FROM (
			SELECT 'audit-'||id AS id,'audit' AS type,action AS title,metadata_json AS detail,created_at
			FROM audit_log WHERE subject_type='device' AND subject_id=?
			UNION ALL
			SELECT 'alert-'||t.id AS id,'alert' AS type,
				('告警状态：'||t.from_status||' → '||t.to_status) AS title,
				json_object('fingerprint',t.fingerprint,'severity',t.severity,'reason',t.reason) AS detail,
				t.created_at
			FROM alert_transitions t JOIN alert_instances a ON a.fingerprint=t.fingerprint WHERE a.device_id=?
		) ORDER BY created_at DESC LIMIT ?`, deviceID, deviceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceEvent
	for rows.Next() {
		var item DeviceEvent
		var raw, created string
		if err := rows.Scan(&item.ID, &item.Type, &item.Title, &raw, &created); err != nil {
			return nil, err
		}
		item.Detail, item.CreatedAt = json.RawMessage(raw), parseTime(created)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.reader().QueryContext(ctx, `SELECT id,action,subject_type,subject_id,metadata_json,created_at FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var item AuditEntry
		var raw, created string
		if err := rows.Scan(&item.ID, &item.Action, &item.SubjectType, &item.SubjectID, &raw, &created); err != nil {
			return nil, err
		}
		item.Metadata, item.CreatedAt = json.RawMessage(raw), parseTime(created)
		out = append(out, item)
	}
	return out, rows.Err()
}
