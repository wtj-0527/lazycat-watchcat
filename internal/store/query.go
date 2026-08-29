package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

type ProcessListOptions struct {
	Query  string
	Sort   string
	Order  string
	Limit  int
	Offset int
}

type ProcessPage struct {
	Items       []protocol.ProcessSample `json:"items"`
	Total       int                      `json:"total"`
	CollectedAt time.Time                `json:"collectedAt,omitempty"`
}

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
	err := s.reader().QueryRowContext(ctx, `SELECT id,name,hostname,os_version,collector_version,capabilities_json,status,created_at,last_seen_at FROM devices WHERE id=?`, id).Scan(&d.ID, &d.Name, &d.Hostname, &d.OSVersion, &d.CollectorVer, &caps, &d.Status, &created, &seen)
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal([]byte(caps), &d.Capabilities)
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	d.LastSeenAt, _ = time.Parse(time.RFC3339Nano, seen)
	return d, nil
}

func (s *Store) ListLatestMetrics(ctx context.Context) ([]LatestMetric, error) {
	rows, err := s.reader().QueryContext(ctx, `SELECT device_id,name,value,unit,labels_json,collected_at FROM latest_metrics ORDER BY device_id,name`)
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
	rows, err := s.reader().QueryContext(ctx, `SELECT device_id,name,value,unit,labels_json,collected_at FROM latest_metrics WHERE device_id=? ORDER BY name`, deviceID)
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
	rows, err := s.reader().QueryContext(ctx, query, args...)
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

// SampledMetricHistory returns at most one sample per label set and time bucket.
// It uses the time index for each bucket instead of reading thousands of
// interleaved table rows, which keeps long-range charts responsive on large
// SQLite databases stored on HDDs.
func (s *Store) SampledMetricHistory(ctx context.Context, deviceID, name string, since, until time.Time, points int) ([]MetricSample, error) {
	if points < 10 {
		points = 10
	}
	if points > 500 {
		points = 500
	}
	if until.IsZero() {
		until = time.Now().UTC()
	}
	if !since.Before(until) {
		return nil, nil
	}
	conn, err := s.reader().Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	labelRows, err := conn.QueryContext(ctx, `SELECT labels_json FROM latest_metrics WHERE device_id=? AND name=? ORDER BY labels_json`, deviceID, name)
	if err != nil {
		return nil, err
	}
	var labelSets []string
	for labelRows.Next() {
		var labels string
		if err := labelRows.Scan(&labels); err != nil {
			labelRows.Close()
			return nil, err
		}
		labelSets = append(labelSets, labels)
	}
	if err := labelRows.Close(); err != nil {
		return nil, err
	}
	if len(labelSets) == 0 {
		return s.MetricHistoryRange(ctx, deviceID, name, since, until, points)
	}

	stmt, err := conn.PrepareContext(ctx, `SELECT value,unit,labels_json,collected_at
		FROM metrics INDEXED BY idx_metrics_device_name_time
		WHERE device_id=? AND name=? AND labels_json=? AND collected_at>=? AND collected_at<=?
		ORDER BY collected_at DESC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	firstStmt, err := conn.PrepareContext(ctx, `SELECT collected_at
		FROM metrics INDEXED BY idx_metrics_device_name_time
		WHERE device_id=? AND name=? AND labels_json=? AND collected_at>=? AND collected_at<=?
		ORDER BY collected_at ASC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer firstStmt.Close()

	seen := make(map[string]struct{}, len(labelSets)*points)
	out := make([]MetricSample, 0, len(labelSets)*points)
	for _, labelSet := range labelSets {
		var firstText string
		err := firstStmt.QueryRowContext(ctx, deviceID, name, labelSet, since.UTC().Format(time.RFC3339Nano), until.UTC().Format(time.RFC3339Nano)).Scan(&firstText)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		effectiveFrom, err := time.Parse(time.RFC3339Nano, firstText)
		if err != nil || effectiveFrom.Before(since) {
			effectiveFrom = since
		}
		step := until.Sub(effectiveFrom) / time.Duration(points)
		if step <= 0 {
			step = time.Second
		}
		fromText := effectiveFrom.UTC().Format(time.RFC3339Nano)
		for bucket := 1; bucket <= points; bucket++ {
			bucketEnd := effectiveFrom.Add(time.Duration(bucket) * step)
			if bucket == points || bucketEnd.After(until) {
				bucketEnd = until
			}
			var sample MetricSample
			var labels, collected string
			err := stmt.QueryRowContext(ctx, deviceID, name, labelSet, fromText, bucketEnd.UTC().Format(time.RFC3339Nano)).
				Scan(&sample.Value, &sample.Unit, &labels, &collected)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			key := labels + "\x00" + collected
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			_ = json.Unmarshal([]byte(labels), &sample.Labels)
			sample.CollectedAt, _ = time.Parse(time.RFC3339Nano, collected)
			out = append(out, sample)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CollectedAt.Equal(out[j].CollectedAt) {
			return fmt.Sprint(out[i].Labels) < fmt.Sprint(out[j].Labels)
		}
		return out[i].CollectedAt.Before(out[j].CollectedAt)
	})
	return out, nil
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
	rows, err := s.reader().QueryContext(ctx, query, args...)
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
		rows, err := s.reader().QueryContext(ctx, query, args...)
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
	if err := s.reader().QueryRowContext(ctx, `SELECT MAX(collected_at) FROM metrics`).Scan(&value); err != nil {
		return time.Time{}, err
	}
	if !value.Valid {
		return time.Time{}, nil
	}
	return parseTime(value.String), nil
}

func (s *Store) LatestProcesses(ctx context.Context, deviceID string, options ProcessListOptions) (ProcessPage, error) {
	if options.Limit <= 0 || options.Limit > 200 {
		options.Limit = 20
	}
	if options.Offset < 0 {
		options.Offset = 0
	}
	orderColumns := map[string]string{
		"cpu": "cpu_percent", "memory": "memory_rss_bytes", "read": "read_rate",
		"write": "write_rate", "pid": "pid", "name": "name", "user": "user_name",
		"state": "state", "threads": "threads", "uptime": "uptime_seconds",
	}
	column := orderColumns[options.Sort]
	if column == "" {
		column = "cpu_percent"
	}
	direction := "DESC"
	if strings.EqualFold(options.Order, "asc") {
		direction = "ASC"
	}
	where := `device_id=?`
	args := []any{deviceID}
	if query := strings.TrimSpace(options.Query); query != "" {
		where += ` AND (name LIKE ? OR user_name LIKE ? OR command LIKE ? OR CAST(pid AS TEXT) LIKE ?)`
		like := "%" + query + "%"
		args = append(args, like, like, like, like)
	}
	var page ProcessPage
	if err := s.reader().QueryRowContext(ctx, `SELECT COUNT(*),MAX(collected_at) FROM latest_processes WHERE `+where, args...).Scan(&page.Total, nullableTimeScanner{dest: &page.CollectedAt}); err != nil {
		return page, err
	}
	query := `SELECT pid,start_time,name,user_name,command,state,cgroup_path,cpu_percent,memory_rss_bytes,
		read_bytes,write_bytes,read_rate,write_rate,threads,uptime_seconds,collected_at
		FROM latest_processes WHERE ` + where + ` ORDER BY ` + column + ` ` + direction + `,pid ASC LIMIT ? OFFSET ?`
	args = append(args, options.Limit, options.Offset)
	rows, err := s.reader().QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanProcess(rows)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

func (s *Store) ProcessHistory(ctx context.Context, deviceID string, pid int, startTime string, since, until time.Time, limit int) ([]protocol.ProcessSample, error) {
	if limit <= 0 || limit > 5000 {
		limit = 2000
	}
	rows, err := s.reader().QueryContext(ctx, `SELECT pid,start_time,name,user_name,command,state,cgroup_path,cpu_percent,memory_rss_bytes,
		read_bytes,write_bytes,read_rate,write_rate,threads,uptime_seconds,collected_at
		FROM process_samples WHERE device_id=? AND pid=? AND start_time=? AND collected_at>=? AND collected_at<=?
		ORDER BY collected_at ASC LIMIT ?`,
		deviceID, pid, startTime, since.UTC().Format(time.RFC3339Nano), until.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.ProcessSample
	for rows.Next() {
		item, err := scanProcess(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type processScanner interface{ Scan(...any) error }

func scanProcess(scanner processScanner) (protocol.ProcessSample, error) {
	var item protocol.ProcessSample
	var collected string
	err := scanner.Scan(
		&item.PID, &item.StartTime, &item.Name, &item.User, &item.Command, &item.State, &item.Cgroup,
		&item.CPUPercent, &item.MemoryRSSBytes, &item.ReadBytes, &item.WriteBytes, &item.ReadRate,
		&item.WriteRate, &item.Threads, &item.UptimeSeconds, &collected,
	)
	item.CollectedAt = parseTime(collected)
	return item, err
}

type nullableTimeScanner struct{ dest *time.Time }

func (s nullableTimeScanner) Scan(value any) error {
	if value == nil {
		return nil
	}
	text, ok := value.(string)
	if !ok {
		if bytes, bytesOK := value.([]byte); bytesOK {
			text, ok = string(bytes), true
		}
	}
	if !ok {
		return fmt.Errorf("unsupported time value %T", value)
	}
	*s.dest = parseTime(text)
	return nil
}

func IsNotFound(err error) bool { return err == sql.ErrNoRows }
