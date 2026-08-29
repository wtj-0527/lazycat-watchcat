package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Inspection struct {
	ID             string          `json:"id"`
	Status         string          `json:"status"`
	TriggerType    string          `json:"triggerType"`
	StartedAt      time.Time       `json:"startedAt"`
	CompletedAt    *time.Time      `json:"completedAt,omitempty"`
	DeviceCount    int             `json:"deviceCount"`
	HealthyCount   int             `json:"healthyCount"`
	WarningCount   int             `json:"warningCount"`
	CriticalCount  int             `json:"criticalCount"`
	Report         json.RawMessage `json:"report,omitempty"`
	ChangeSummary  json.RawMessage `json:"changeSummary,omitempty"`
	EvidenceSHA256 string          `json:"evidenceSha256"`
	Error          string          `json:"error,omitempty"`
}

func (s *Store) SaveInspection(ctx context.Context, trigger string, report, changeSummary any, devices, healthy, warning, critical int) (Inspection, error) {
	now := time.Now().UTC()
	raw, err := json.Marshal(report)
	if err != nil {
		return Inspection{}, err
	}
	sum := sha256.Sum256(raw)
	changeRaw, err := json.Marshal(changeSummary)
	if err != nil {
		return Inspection{}, err
	}
	item := Inspection{
		ID: uuid.NewString(), Status: "completed", TriggerType: trigger, StartedAt: now, CompletedAt: &now,
		DeviceCount: devices, HealthyCount: healthy, WarningCount: warning, CriticalCount: critical,
		Report: raw, ChangeSummary: changeRaw, EvidenceSHA256: hex.EncodeToString(sum[:]),
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO inspections(id,status,trigger_type,started_at,completed_at,device_count,healthy_count,warning_count,critical_count,report_json,evidence_sha256,change_summary_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Status, item.TriggerType, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), devices, healthy, warning, critical, string(raw), item.EvidenceSHA256, string(changeRaw))
	if err == nil {
		_, err = s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES('inspection.completed','inspection',?,?,?)`, item.ID, `{"evidenceSha256":"`+item.EvidenceSHA256+`"}`, now.Format(time.RFC3339Nano))
	}
	return item, err
}

func (s *Store) ListInspections(ctx context.Context, limit int) ([]Inspection, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.reader().QueryContext(ctx, `SELECT id,status,trigger_type,started_at,completed_at,device_count,healthy_count,warning_count,critical_count,evidence_sha256,error,change_summary_json FROM inspections ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Inspection
	for rows.Next() {
		var i Inspection
		var started string
		var completed sql.NullString
		var change string
		if err := rows.Scan(&i.ID, &i.Status, &i.TriggerType, &started, &completed, &i.DeviceCount, &i.HealthyCount, &i.WarningCount, &i.CriticalCount, &i.EvidenceSHA256, &i.Error, &change); err != nil {
			return nil, err
		}
		i.StartedAt = parseTime(started)
		i.CompletedAt = nullableTime(completed)
		i.ChangeSummary = json.RawMessage(change)
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) InspectionByID(ctx context.Context, id string) (Inspection, error) {
	var i Inspection
	var started string
	var completed sql.NullString
	var report, change string
	err := s.reader().QueryRowContext(ctx, `SELECT id,status,trigger_type,started_at,completed_at,device_count,healthy_count,warning_count,critical_count,report_json,evidence_sha256,error,change_summary_json FROM inspections WHERE id=?`, id).
		Scan(&i.ID, &i.Status, &i.TriggerType, &started, &completed, &i.DeviceCount, &i.HealthyCount, &i.WarningCount, &i.CriticalCount, &report, &i.EvidenceSHA256, &i.Error, &change)
	if err != nil {
		return i, err
	}
	i.StartedAt, i.CompletedAt, i.Report, i.ChangeSummary = parseTime(started), nullableTime(completed), json.RawMessage(report), json.RawMessage(change)
	return i, nil
}

func (s *Store) LatestInspection(ctx context.Context) (Inspection, error) {
	var id string
	err := s.reader().QueryRowContext(ctx, `SELECT id FROM inspections WHERE status='completed' ORDER BY started_at DESC LIMIT 1`).Scan(&id)
	if err != nil {
		return Inspection{}, err
	}
	return s.InspectionByID(ctx, id)
}

func IsInspectionNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
