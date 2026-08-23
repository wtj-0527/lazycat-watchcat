package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
)

var ErrInvalidPairingCode = errors.New("invalid or expired pairing code")

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepathDir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func filepathDir(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "."
	}
	return path[:i]
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS pairing_codes (id TEXT PRIMARY KEY, code_hash TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, consumed_at TEXT, created_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS devices (id TEXT PRIMARY KEY, name TEXT NOT NULL, hostname TEXT NOT NULL UNIQUE, os_version TEXT NOT NULL DEFAULT '', collector_version TEXT NOT NULL, capabilities_json TEXT NOT NULL DEFAULT '[]', token_hash TEXT NOT NULL UNIQUE, status TEXT NOT NULL DEFAULT 'online', created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS audit_log (id INTEGER PRIMARY KEY AUTOINCREMENT, action TEXT NOT NULL, subject_type TEXT NOT NULL, subject_id TEXT NOT NULL, metadata_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS metrics (id INTEGER PRIMARY KEY AUTOINCREMENT, device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE, name TEXT NOT NULL, value REAL NOT NULL, unit TEXT NOT NULL DEFAULT '', labels_json TEXT NOT NULL DEFAULT '{}', collected_at TEXT NOT NULL, received_at TEXT NOT NULL);`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_device_name_time ON metrics(device_id,name,collected_at DESC);`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, datetime('now'));`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(2, datetime('now'));`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	return nil
}

func (s *Store) CreatePairingCode(ctx context.Context, ttl time.Duration) (string, time.Time, error) {
	code, err := randomCode(10)
	if err != nil {
		return "", time.Time{}, err
	}
	now, expires := time.Now().UTC(), time.Now().UTC().Add(ttl)
	_, err = s.db.ExecContext(ctx, `INSERT INTO pairing_codes(id, code_hash, expires_at, created_at) VALUES(?,?,?,?)`, uuid.NewString(), hash(normalizeCode(code)), expires.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err == nil {
		_, err = s.db.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,created_at) VALUES('pairing_code.created','pairing_code',?,?)`, hash(code)[:12], now.Format(time.RFC3339Nano))
	}
	return code, expires, err
}

func (s *Store) PairCollector(ctx context.Context, req protocol.PairCollectorRequest) (protocol.PairCollectorResponse, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return protocol.PairCollectorResponse{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM pairing_codes WHERE code_hash=? AND consumed_at IS NULL AND expires_at>?`, hash(normalizeCode(req.Code)), now.Format(time.RFC3339Nano)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return protocol.PairCollectorResponse{}, ErrInvalidPairingCode
	}
	if err != nil {
		return protocol.PairCollectorResponse{}, err
	}
	token, err := randomToken(32)
	if err != nil {
		return protocol.PairCollectorResponse{}, err
	}
	deviceID := uuid.NewString()
	caps, _ := json.Marshal(req.Capabilities)
	if req.Name == "" {
		req.Name = req.Hostname
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO devices(id,name,hostname,os_version,collector_version,capabilities_json,token_hash,status,created_at,last_seen_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, deviceID, req.Name, req.Hostname, req.OSVersion, req.CollectorVer, string(caps), hash(token), "online", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return protocol.PairCollectorResponse{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE pairing_codes SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, now.Format(time.RFC3339Nano), id)
	if err != nil {
		return protocol.PairCollectorResponse{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return protocol.PairCollectorResponse{}, ErrInvalidPairingCode
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES('collector.paired','device',?,?,?)`, deviceID, `{"hostname":`+quote(req.Hostname)+`}`, now.Format(time.RFC3339Nano))
	if err != nil {
		return protocol.PairCollectorResponse{}, err
	}
	if err := tx.Commit(); err != nil {
		return protocol.PairCollectorResponse{}, err
	}
	return protocol.PairCollectorResponse{DeviceID: deviceID, Token: token}, nil
}

func (s *Store) ListDevices(ctx context.Context) ([]protocol.Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,hostname,os_version,collector_version,capabilities_json,status,created_at,last_seen_at FROM devices ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []protocol.Device{}
	for rows.Next() {
		var d protocol.Device
		var caps, created, seen string
		if err := rows.Scan(&d.ID, &d.Name, &d.Hostname, &d.OSVersion, &d.CollectorVer, &caps, &d.Status, &created, &seen); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(caps), &d.Capabilities)
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		d.LastSeenAt, _ = time.Parse(time.RFC3339Nano, seen)
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *Store) AuthenticateDevice(ctx context.Context, deviceID, token string) error {
	var found string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM devices WHERE id=? AND token_hash=?`, deviceID, hash(token)).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("invalid device credentials")
	}
	return err
}

func (s *Store) IngestMetrics(ctx context.Context, batch protocol.MetricBatch) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO metrics(device_id,name,value,unit,labels_json,collected_at,received_at) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, point := range batch.Points {
		labels, _ := json.Marshal(point.Labels)
		if _, err := stmt.ExecContext(ctx, batch.DeviceID, point.Name, point.Value, point.Unit, string(labels), point.CollectedAt.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET last_seen_at=?,status='online' WHERE id=?`, now.Format(time.RFC3339Nano), batch.DeviceID); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizeCode(s string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), "-", ""))
}
func hash(s string) string  { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }
func quote(s string) string { b, _ := json.Marshal(s); return string(b) }
func randomCode(n int) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)[:12]
	return raw[:4] + "-" + raw[4:8] + "-" + raw[8:], nil
}
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
