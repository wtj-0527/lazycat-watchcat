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
}
type MetricSample struct {
	Value       float64           `json:"value"`
	Unit        string            `json:"unit"`
	Labels      map[string]string `json:"labels"`
	CollectedAt time.Time         `json:"collectedAt"`
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
	rows, err := s.db.QueryContext(ctx, `SELECT m.device_id,m.name,m.value,m.unit,m.labels_json,m.collected_at FROM metrics m JOIN (SELECT device_id,name,labels_json,MAX(collected_at) AS max_time FROM metrics GROUP BY device_id,name,labels_json) latest ON latest.device_id=m.device_id AND latest.name=m.name AND latest.labels_json=m.labels_json AND latest.max_time=m.collected_at ORDER BY m.device_id,m.name`)
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
	rows, err := s.db.QueryContext(ctx, `SELECT m.device_id,m.name,m.value,m.unit,m.labels_json,m.collected_at FROM metrics m JOIN (SELECT name,labels_json,MAX(collected_at) AS max_time FROM metrics WHERE device_id=? GROUP BY name,labels_json) latest ON latest.name=m.name AND latest.labels_json=m.labels_json AND latest.max_time=m.collected_at WHERE m.device_id=? ORDER BY m.name`, deviceID, deviceID)
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
func IsNotFound(err error) bool { return err == sql.ErrNoRows }
