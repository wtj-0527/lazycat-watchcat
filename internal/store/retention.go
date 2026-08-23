package store

import (
	"context"
	"database/sql"
	"math"
	"sort"
	"time"
)

type RetentionResult struct {
	RollupBuckets      int `json:"rollupBuckets"`
	RawDeleted         int `json:"rawDeleted"`
	RollupsDeleted     int `json:"rollupsDeleted"`
	AuditDeleted       int `json:"auditDeleted"`
	InspectionsDeleted int `json:"inspectionsDeleted"`
}

type rollupKey struct {
	deviceID, name, labels, bucket string
}
type rollupValues struct {
	values []float64
}

func (s *Store) RunRetention(ctx context.Context, now time.Time) (RetentionResult, error) {
	now = now.UTC()
	currentHour := now.Truncate(time.Hour)
	var lastRun string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM retention_state WHERE name='last_rollup_received_at'`).Scan(&lastRun)
	if err == sql.ErrNoRows {
		lastRun = now.Add(-31 * 24 * time.Hour).Format(time.RFC3339Nano)
	} else if err != nil {
		return RetentionResult{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT device_id,name,labels_json,substr(collected_at,1,13)||':00:00Z' AS bucket_start
		FROM metrics WHERE received_at>? AND received_at<=? AND collected_at<?`,
		lastRun, now.Format(time.RFC3339Nano), currentHour.Format(time.RFC3339Nano))
	if err != nil {
		return RetentionResult{}, err
	}
	var affected []rollupKey
	for rows.Next() {
		var key rollupKey
		if err := rows.Scan(&key.deviceID, &key.name, &key.labels, &key.bucket); err != nil {
			rows.Close()
			return RetentionResult{}, err
		}
		affected = append(affected, key)
	}
	if err := rows.Close(); err != nil {
		return RetentionResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RetentionResult{}, err
	}
	defer tx.Rollback()
	result := RetentionResult{}
	for _, key := range affected {
		bucketStart := parseTime(key.bucket)
		valueRows, err := tx.QueryContext(ctx, `SELECT value FROM metrics WHERE device_id=? AND name=? AND labels_json=? AND collected_at>=? AND collected_at<? ORDER BY value`,
			key.deviceID, key.name, key.labels, bucketStart.Format(time.RFC3339Nano), bucketStart.Add(time.Hour).Format(time.RFC3339Nano))
		if err != nil {
			return RetentionResult{}, err
		}
		group := &rollupValues{}
		for valueRows.Next() {
			var value float64
			if err := valueRows.Scan(&value); err != nil {
				valueRows.Close()
				return RetentionResult{}, err
			}
			group.values = append(group.values, value)
		}
		valueRows.Close()
		if len(group.values) == 0 {
			continue
		}
		sort.Float64s(group.values)
		min, max, sum := group.values[0], group.values[len(group.values)-1], 0.0
		for _, value := range group.values {
			sum += value
		}
		p95Index := int(math.Ceil(float64(len(group.values))*0.95)) - 1
		if p95Index < 0 {
			p95Index = 0
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO metric_rollups_hourly(device_id,name,labels_json,bucket_start,min_value,max_value,avg_value,p95_value,sample_count,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(device_id,name,labels_json,bucket_start) DO UPDATE SET min_value=excluded.min_value,max_value=excluded.max_value,avg_value=excluded.avg_value,p95_value=excluded.p95_value,sample_count=excluded.sample_count,updated_at=excluded.updated_at`,
			key.deviceID, key.name, key.labels, key.bucket, min, max, sum/float64(len(group.values)), group.values[p95Index], len(group.values), now.Format(time.RFC3339Nano))
		if err != nil {
			return RetentionResult{}, err
		}
		result.RollupBuckets++
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO retention_state(name,value) VALUES('last_rollup_received_at',?) ON CONFLICT(name) DO UPDATE SET value=excluded.value`, now.Format(time.RFC3339Nano)); err != nil {
		return RetentionResult{}, err
	}
	type cleanup struct {
		query  string
		cutoff time.Time
		dest   *int
	}
	for _, item := range []cleanup{
		{`DELETE FROM metrics WHERE collected_at<?`, now.Add(-30 * 24 * time.Hour), &result.RawDeleted},
		{`DELETE FROM metric_rollups_hourly WHERE bucket_start<?`, now.Add(-365 * 24 * time.Hour), &result.RollupsDeleted},
		{`DELETE FROM audit_log WHERE created_at<?`, now.Add(-180 * 24 * time.Hour), &result.AuditDeleted},
		{`DELETE FROM inspections WHERE started_at<?`, now.Add(-365 * 24 * time.Hour), &result.InspectionsDeleted},
	} {
		res, err := tx.ExecContext(ctx, item.query, item.cutoff.Format(time.RFC3339Nano))
		if err != nil {
			return RetentionResult{}, err
		}
		n, _ := res.RowsAffected()
		*item.dest = int(n)
	}
	if err := tx.Commit(); err != nil {
		return RetentionResult{}, err
	}
	_ = s.SetSystemState(ctx, "last_retention", map[string]any{"completedAt": now, "result": result})
	return result, nil
}

type RetentionStats struct {
	RawMetricRows   int    `json:"rawMetricRows"`
	RollupRows      int    `json:"rollupRows"`
	OldestRaw       string `json:"oldestRaw,omitempty"`
	OldestRollup    string `json:"oldestRollup,omitempty"`
	PendingNotifies int    `json:"pendingNotifications"`
}

func (s *Store) RetentionStats(ctx context.Context) (RetentionStats, error) {
	var out RetentionStats
	var rawOldest, rollupOldest sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),MIN(collected_at) FROM metrics`).Scan(&out.RawMetricRows, &rawOldest)
	if err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),MIN(bucket_start) FROM metric_rollups_hourly`).Scan(&out.RollupRows, &rollupOldest); err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_outbox WHERE status='pending'`).Scan(&out.PendingNotifies); err != nil {
		return out, err
	}
	if rawOldest.Valid {
		out.OldestRaw = rawOldest.String
	}
	if rollupOldest.Valid {
		out.OldestRollup = rollupOldest.String
	}
	return out, nil
}
