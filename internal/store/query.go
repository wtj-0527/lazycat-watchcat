package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
)

type LatestMetric struct {
	DeviceID    string            `json:"deviceId"`
	Name        string            `json:"name"`
	Value       float64           `json:"value"`
	Unit        string            `json:"unit"`
	Labels      map[string]string `json:"labels"`
	CollectedAt time.Time         `json:"collectedAt"`
	Risk        string            `json:"risk,omitempty"`
}
type MetricSample struct {
	Value       float64           `json:"value"`
	Unit        string            `json:"unit"`
	Labels      map[string]string `json:"labels"`
	CollectedAt time.Time         `json:"collectedAt"`
}
type ApplicationMetricSample struct {
	DeviceID    string
	Value       float64
	Labels      map[string]string
	CollectedAt time.Time
}

func (s *Store) DeviceByID(ctx context.Context, id string) (protocol.Device, error) {
	var d protocol.Device
	var caps, created, seen string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,hostname,os_version,collector_version,capabilities_json,status,created_at,last_seen_at FROM devices WHERE id=?`, id).Scan(&d.ID, &d.Name, &d.Hostname, &d.OSVersion, &d.CollectorVer, &caps, &d.Status, &created, &seen)
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal([]byte(caps), &d.Capabilities)
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	d.LastSeenAt, _ = time.Parse(time.RFC3339Nano, seen)
	return d, nil
}

func (s *Store) ListLatestMetrics(ctx context.Context) ([]LatestMetric, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,name,value,unit,labels_json,collected_at FROM latest_metrics ORDER BY device_id,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LatestMetric
	for rows.Next() {
		var m LatestMetric
		var labels, collected string
		if err := rows.Scan(&m.DeviceID, &m.Name, &m.Value, &m.Unit, &labels, &collected); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labels), &m.Labels)
		m.CollectedAt, _ = time.Parse(time.RFC3339Nano, collected)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) LatestMetricsForDevice(ctx context.Context, deviceID string) ([]LatestMetric, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT device_id,name,value,unit,labels_json,collected_at FROM latest_metrics WHERE device_id=? ORDER BY name`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LatestMetric
	for rows.Next() {
		var m LatestMetric
		var labels, collected string
		if err := rows.Scan(&m.DeviceID, &m.Name, &m.Value, &m.Unit, &labels, &collected); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labels), &m.Labels)
		m.CollectedAt, _ = time.Parse(time.RFC3339Nano, collected)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MetricHistory(ctx context.Context, deviceID, name string, since time.Time, limit int) ([]MetricSample, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT value,unit,labels_json,collected_at FROM metrics WHERE device_id=? AND name=? AND collected_at>=? ORDER BY collected_at ASC LIMIT ?`, deviceID, name, since.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricSample
	for rows.Next() {
		var m MetricSample
		var labels, collected string
		if err := rows.Scan(&m.Value, &m.Unit, &labels, &collected); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labels), &m.Labels)
		m.CollectedAt, _ = time.Parse(time.RFC3339Nano, collected)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) ApplicationMetricHistory(ctx context.Context, appID, name string, since, until time.Time, limit int) ([]ApplicationMetricSample, error) {
	if limit <= 0 || limit > 100000 {
		limit = 50000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT metrics.device_id,metrics.value,metrics.labels_json,metrics.collected_at
		FROM (SELECT DISTINCT device_id FROM application_runtime_state WHERE app_id=?) AS app_devices
		JOIN metrics ON metrics.device_id=app_devices.device_id
		WHERE metrics.name=? AND metrics.collected_at>=? AND metrics.collected_at<=? AND json_extract(metrics.labels_json,'$.app')=?
		ORDER BY metrics.device_id,metrics.labels_json,metrics.collected_at ASC LIMIT ?`,
		appID, name, since.UTC().Format(time.RFC3339Nano), until.UTC().Format(time.RFC3339Nano), appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApplicationMetricSample
	for rows.Next() {
		var item ApplicationMetricSample
		var labels, collected string
		if err := rows.Scan(&item.DeviceID, &item.Value, &labels, &collected); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labels), &item.Labels)
		item.CollectedAt, _ = time.Parse(time.RFC3339Nano, collected)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AllApplicationMetricHistory(ctx context.Context, name string, since, until time.Time, limit int) ([]ApplicationMetricSample, error) {
	if limit <= 0 || limit > 300000 {
		limit = 200000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT metrics.device_id,metrics.value,metrics.labels_json,metrics.collected_at
		FROM (SELECT DISTINCT device_id FROM application_runtime_state) AS app_devices
		JOIN metrics ON metrics.device_id=app_devices.device_id
		WHERE metrics.name=? AND metrics.collected_at>=? AND metrics.collected_at<=?
			AND json_extract(metrics.labels_json,'$.app')<>''
		ORDER BY metrics.device_id,metrics.labels_json,metrics.collected_at ASC LIMIT ?`,
		name, since.UTC().Format(time.RFC3339Nano), until.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ApplicationMetricSample
	for rows.Next() {
		var item ApplicationMetricSample
		var labels, collected string
		if err := rows.Scan(&item.DeviceID, &item.Value, &labels, &collected); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labels), &item.Labels)
		item.CollectedAt, _ = time.Parse(time.RFC3339Nano, collected)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) LatestMetricTimestamp(ctx context.Context) (time.Time, error) {
	var value sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(collected_at) FROM metrics`).Scan(&value); err != nil {
		return time.Time{}, err
	}
	if !value.Valid {
		return time.Time{}, nil
	}
	return parseTime(value.String), nil
}

func IsNotFound(err error) bool { return err == sql.ErrNoRows }
