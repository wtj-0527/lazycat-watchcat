package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
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
	return s.MetricHistoryRange(ctx, deviceID, name, since, time.Time{}, limit)
}

func (s *Store) MetricHistoryRange(ctx context.Context, deviceID, name string, since, until time.Time, limit int) ([]MetricSample, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	query := `SELECT value,unit,labels_json,collected_at FROM metrics WHERE device_id=? AND name=? AND collected_at>=?`
	args := []any{deviceID, name, since.UTC().Format(time.RFC3339Nano)}
	if !until.IsZero() {
		query += ` AND collected_at<=?`
		args = append(args, until.UTC().Format(time.RFC3339Nano))
	}
	query += ` ORDER BY collected_at ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
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
	deviceIDs, err := s.applicationDeviceIDs(ctx, appID)
	if err != nil {
		return nil, err
	}
	return s.applicationMetricHistoryForDevices(ctx, deviceIDs, appID, name, since, until, limit)
}

func (s *Store) AllApplicationMetricHistory(ctx context.Context, name string, since, until time.Time, limit int) ([]ApplicationMetricSample, error) {
	if limit <= 0 || limit > 300000 {
		limit = 200000
	}
	deviceIDs, err := s.applicationDeviceIDs(ctx, "")
	if err != nil {
		return nil, err
	}
	return s.applicationMetricHistoryForDevices(ctx, deviceIDs, "", name, since, until, limit)
}

func (s *Store) ApplicationMetricHistoryForDevice(ctx context.Context, deviceID, appID, name string, since, until time.Time, limit int) ([]ApplicationMetricSample, error) {
	if limit <= 0 || limit > 100000 {
		limit = 100000
	}
	return s.applicationMetricHistoryForDevices(ctx, []string{deviceID}, appID, name, since, until, limit)
}

func (s *Store) applicationDeviceIDs(ctx context.Context, appID string) ([]string, error) {
	query := `SELECT DISTINCT device_id FROM application_runtime_state`
	var args []any
	if appID != "" {
		query += ` WHERE app_id=?`
		args = append(args, appID)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var deviceID string
		if err := rows.Scan(&deviceID); err != nil {
			return nil, err
		}
		out = append(out, deviceID)
	}
	return out, rows.Err()
}

func (s *Store) applicationMetricHistoryForDevices(ctx context.Context, deviceIDs []string, appID, name string, since, until time.Time, limit int) ([]ApplicationMetricSample, error) {
	var out []ApplicationMetricSample
	for _, deviceID := range deviceIDs {
		remaining := limit - len(out)
		if remaining <= 0 {
			break
		}
		index := "idx_metrics_application_time"
		if appID != "" {
			index = "idx_metrics_application_app_time"
		}
		query := `SELECT device_id,value,labels_json,collected_at
			FROM metrics INDEXED BY ` + index + `
			WHERE device_id=? AND name=? AND collected_at>=? AND collected_at<=?
				AND json_extract(labels_json,'$.app')<>''`
		args := []any{deviceID, name, since.UTC().Format(time.RFC3339Nano), until.UTC().Format(time.RFC3339Nano)}
		if appID != "" {
			query += ` AND json_extract(labels_json,'$.app')=?`
			args = append(args, appID)
		}
		query += ` ORDER BY collected_at ASC LIMIT ?`
		args = append(args, remaining)
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var item ApplicationMetricSample
			var labels, collected string
			if err := rows.Scan(&item.DeviceID, &item.Value, &labels, &collected); err != nil {
				rows.Close()
				return nil, err
			}
			_ = json.Unmarshal([]byte(labels), &item.Labels)
			item.CollectedAt, _ = time.Parse(time.RFC3339Nano, collected)
			out = append(out, item)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if left.DeviceID != right.DeviceID {
			return left.DeviceID < right.DeviceID
		}
		leftKey, rightKey := left.Labels["app"]+"\x00"+left.Labels["container"], right.Labels["app"]+"\x00"+right.Labels["container"]
		if leftKey != rightKey {
			return leftKey < rightKey
		}
		return left.CollectedAt.Before(right.CollectedAt)
	})
	return out, nil
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
