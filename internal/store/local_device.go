package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

func (s *Store) EnsureLocalDevice(ctx context.Context, name, hostname, osVersion, collectorVersion string, capabilities []string) (string, error) {
	var id string
	var revoked sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,revoked_at FROM devices WHERE hostname=?`, hostname).Scan(&id, &revoked)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	caps, _ := json.Marshal(capabilities)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err == nil {
		if revoked.Valid {
			return "", errors.New("local device identity was revoked")
		}
		_, err = s.db.ExecContext(ctx, `UPDATE devices SET name=?,os_version=?,collector_version=?,capabilities_json=?,status='online' WHERE id=?`,
			name, osVersion, collectorVersion, string(caps), id)
		return id, err
	}
	id = uuid.NewString()
	_, err = s.db.ExecContext(ctx, `INSERT INTO devices(id,name,hostname,os_version,collector_version,capabilities_json,token_hash,status,created_at,last_seen_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		id, name, hostname, osVersion, collectorVersion, string(caps), hash("embedded:"+id), "online", now, now)
	if err != nil {
		return "", err
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES('collector.embedded_registered','device',?,?,?)`,
		id, `{"mode":"embedded"}`, now)
	return id, nil
}

func (s *Store) IsEmbeddedDevice(ctx context.Context, id string) (bool, error) {
	var caps string
	err := s.db.QueryRowContext(ctx, `SELECT capabilities_json FROM devices WHERE id=?`, id).Scan(&caps)
	if err != nil {
		return false, err
	}
	var values []string
	_ = json.Unmarshal([]byte(caps), &values)
	for _, value := range values {
		if value == "collector.embedded" {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) RemoveLegacyEmbeddedFilesystemSeries(ctx context.Context, deviceID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM metrics WHERE device_id=? AND name IN ('filesystem.root.usage','filesystem.root.available') AND labels_json IN ('null','{}')`, deviceID)
	return err
}
