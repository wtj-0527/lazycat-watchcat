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

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

var ErrInvalidPairingCode = errors.New("invalid or expired pairing code")

type Store struct {
	db     *sql.DB
	readDB *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepathDir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(60000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=cache_size(-16384)&_pragma=mmap_size(268435456)")
	if err != nil {
		return nil, err
	}
	// Keep one connection available for a nested/read-side query while
	// strongly limiting competing SQLite writers. A single connection
	// deadlocks methods that intentionally query while rows are still open;
	// two connections plus a long busy timeout gives WAL one reader and one
	// serialized writer without the four-writer SQLITE_BUSY storm.
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	readDB, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=query_only(1)&_pragma=foreign_keys(1)&_pragma=cache_size(-32768)&_pragma=mmap_size(268435456)")
	if err != nil {
		db.Close()
		return nil, err
	}
	// Keep API reads independent from collector writes. A slow process or
	// metrics transaction must never consume every connection available to
	// overview, device, storage, and alert endpoints.
	readDB.SetMaxOpenConns(4)
	readDB.SetMaxIdleConns(4)
	s.readDB = readDB
	return s, nil
}

func filepathDir(path string) string {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "."
	}
	return path[:i]
}

func (s *Store) reader() *sql.DB {
	if s.readDB != nil {
		return s.readDB
	}
	return s.db
}

func (s *Store) Close() error {
	var result error
	if s.readDB != nil {
		result = s.readDB.Close()
	}
	return errors.Join(result, s.db.Close())
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS pairing_codes (id TEXT PRIMARY KEY, code_hash TEXT NOT NULL UNIQUE, expires_at TEXT NOT NULL, consumed_at TEXT, created_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS devices (id TEXT PRIMARY KEY, name TEXT NOT NULL, hostname TEXT NOT NULL UNIQUE, os_version TEXT NOT NULL DEFAULT '', collector_version TEXT NOT NULL, capabilities_json TEXT NOT NULL DEFAULT '[]', token_hash TEXT NOT NULL UNIQUE, status TEXT NOT NULL DEFAULT 'online', created_at TEXT NOT NULL, last_seen_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS audit_log (id INTEGER PRIMARY KEY AUTOINCREMENT, action TEXT NOT NULL, subject_type TEXT NOT NULL, subject_id TEXT NOT NULL, metadata_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS metrics (id INTEGER PRIMARY KEY AUTOINCREMENT, device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE, name TEXT NOT NULL, value REAL NOT NULL, unit TEXT NOT NULL DEFAULT '', labels_json TEXT NOT NULL DEFAULT '{}', collected_at TEXT NOT NULL, received_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS device_certificates (serial TEXT PRIMARY KEY, device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE, expires_at TEXT NOT NULL, valid_until TEXT, revoked_at TEXT, created_at TEXT NOT NULL);`,
		`CREATE INDEX IF NOT EXISTS idx_device_certificates_device ON device_certificates(device_id);`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_device_name_time ON metrics(device_id,name,collected_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_received_at ON metrics(received_at);`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_application_app_time
			ON metrics(device_id,name,json_extract(labels_json,'$.app'),collected_at DESC)
			WHERE json_extract(labels_json,'$.app')<>'';`,
		`CREATE INDEX IF NOT EXISTS idx_metrics_application_time
			ON metrics(device_id,name,collected_at DESC)
			WHERE json_extract(labels_json,'$.app')<>'';`,
		`CREATE TABLE IF NOT EXISTS latest_metrics (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			value REAL NOT NULL,
			unit TEXT NOT NULL DEFAULT '',
			labels_json TEXT NOT NULL DEFAULT '{}',
			collected_at TEXT NOT NULL,
			received_at TEXT NOT NULL,
			PRIMARY KEY(device_id,name,labels_json)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_latest_metrics_device_name ON latest_metrics(device_id,name);`,
		`CREATE TABLE IF NOT EXISTS process_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			pid INTEGER NOT NULL,
			start_time TEXT NOT NULL,
			name TEXT NOT NULL,
			user_name TEXT NOT NULL DEFAULT '',
			command TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT '',
			cgroup_path TEXT NOT NULL DEFAULT '',
			cpu_percent REAL NOT NULL DEFAULT 0,
			memory_rss_bytes INTEGER NOT NULL DEFAULT 0,
			read_bytes INTEGER NOT NULL DEFAULT 0,
			write_bytes INTEGER NOT NULL DEFAULT 0,
			read_rate REAL NOT NULL DEFAULT 0,
			write_rate REAL NOT NULL DEFAULT 0,
			threads INTEGER NOT NULL DEFAULT 0,
			uptime_seconds REAL NOT NULL DEFAULT 0,
			collected_at TEXT NOT NULL,
			received_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_process_samples_identity_time ON process_samples(device_id,pid,start_time,collected_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_process_samples_collected ON process_samples(collected_at);`,
		`CREATE TABLE IF NOT EXISTS latest_processes (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			pid INTEGER NOT NULL,
			start_time TEXT NOT NULL,
			name TEXT NOT NULL,
			user_name TEXT NOT NULL DEFAULT '',
			command TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT '',
			cgroup_path TEXT NOT NULL DEFAULT '',
			cpu_percent REAL NOT NULL DEFAULT 0,
			memory_rss_bytes INTEGER NOT NULL DEFAULT 0,
			read_bytes INTEGER NOT NULL DEFAULT 0,
			write_bytes INTEGER NOT NULL DEFAULT 0,
			read_rate REAL NOT NULL DEFAULT 0,
			write_rate REAL NOT NULL DEFAULT 0,
			threads INTEGER NOT NULL DEFAULT 0,
			uptime_seconds REAL NOT NULL DEFAULT 0,
			collected_at TEXT NOT NULL,
			received_at TEXT NOT NULL,
			PRIMARY KEY(device_id,pid,start_time)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_latest_processes_device_cpu ON latest_processes(device_id,cpu_percent DESC);`,
		`CREATE TABLE IF NOT EXISTS alert_instances (
			fingerprint TEXT PRIMARY KEY,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			device_name TEXT NOT NULL,
			severity TEXT NOT NULL,
			status TEXT NOT NULL,
			resource TEXT NOT NULL,
			message TEXT NOT NULL,
			value REAL NOT NULL DEFAULT 0,
			unit TEXT NOT NULL DEFAULT '',
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			acknowledged_at TEXT,
			silenced_until TEXT,
			resolved_at TEXT,
			occurrence_count INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_alert_instances_status_severity ON alert_instances(status,severity,last_seen_at DESC);`,
		`CREATE TABLE IF NOT EXISTS alert_transitions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint TEXT NOT NULL,
			from_status TEXT NOT NULL DEFAULT '',
			to_status TEXT NOT NULL,
			severity TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_alert_transitions_fingerprint_time ON alert_transitions(fingerprint,created_at DESC);`,
		`CREATE TABLE IF NOT EXISTS notification_outbox (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dedupe_key TEXT NOT NULL UNIQUE,
			alert_fingerprint TEXT NOT NULL,
			transition TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			deeplink TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TEXT NOT NULL,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			sent_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_notification_outbox_pending ON notification_outbox(status,next_attempt_at);`,
		`CREATE TABLE IF NOT EXISTS inspections (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			trigger_type TEXT NOT NULL,
			started_at TEXT NOT NULL,
			completed_at TEXT,
			device_count INTEGER NOT NULL DEFAULT 0,
			healthy_count INTEGER NOT NULL DEFAULT 0,
			warning_count INTEGER NOT NULL DEFAULT 0,
			critical_count INTEGER NOT NULL DEFAULT 0,
			report_json TEXT NOT NULL DEFAULT '{}',
			evidence_sha256 TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS idx_inspections_started ON inspections(started_at DESC);`,
		`CREATE TABLE IF NOT EXISTS metric_rollups_hourly (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			labels_json TEXT NOT NULL,
			bucket_start TEXT NOT NULL,
			min_value REAL NOT NULL,
			max_value REAL NOT NULL,
			avg_value REAL NOT NULL,
			p95_value REAL NOT NULL,
			sample_count INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(device_id,name,labels_json,bucket_start)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_metric_rollups_lookup ON metric_rollups_hourly(device_id,name,bucket_start DESC);`,
		`CREATE TABLE IF NOT EXISTS retention_state (name TEXT PRIMARY KEY, value TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS system_state (name TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS collector_capabilities (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			capability TEXT NOT NULL,
			status TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			checked_at TEXT NOT NULL,
			PRIMARY KEY(device_id,capability)
		);`,
		`CREATE TABLE IF NOT EXISTS application_runtime_state (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			deploy_id TEXT NOT NULL,
			app_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			version TEXT NOT NULL DEFAULT '',
			install_status TEXT NOT NULL,
			instance_status TEXT NOT NULL,
			domain TEXT NOT NULL DEFAULT '',
			builtin INTEGER NOT NULL DEFAULT 0,
			user_id TEXT NOT NULL DEFAULT '',
			user_name TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(device_id,deploy_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_application_runtime_app ON application_runtime_state(app_id,instance_status);`,
		`CREATE TABLE IF NOT EXISTS application_instance_preferences (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			deploy_id TEXT NOT NULL,
			autostart INTEGER,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(device_id,deploy_id)
		);`,
		`CREATE TABLE IF NOT EXISTS application_commands (
			id TEXT PRIMARY KEY,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			deploy_id TEXT NOT NULL,
			app_id TEXT NOT NULL,
			user_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			autostart INTEGER,
			status TEXT NOT NULL DEFAULT 'pending',
			error TEXT NOT NULL DEFAULT '',
			observed_status TEXT NOT NULL DEFAULT '',
			requested_at TEXT NOT NULL,
			started_at TEXT,
			completed_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_application_commands_device_status ON application_commands(device_id,status,requested_at);`,
		`CREATE TABLE IF NOT EXISTS user_runtime_state (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL,
			nickname TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'normal',
			app_install_permission INTEGER NOT NULL DEFAULT 0,
			app_access_no_limit INTEGER NOT NULL DEFAULT 1,
			allowed_app_ids_json TEXT NOT NULL DEFAULT '[]',
			online INTEGER NOT NULL DEFAULT 0,
			active_devices INTEGER NOT NULL DEFAULT 0,
			total_devices INTEGER NOT NULL DEFAULT 0,
			first_observed_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(device_id,user_id)
		);`,
		`CREATE TABLE IF NOT EXISTS user_device_state (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL,
			end_device_id TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			remark_name TEXT NOT NULL DEFAULT '',
			device_api_url TEXT NOT NULL DEFAULT '',
			is_mobile INTEGER NOT NULL DEFAULT 0,
			is_tv INTEGER NOT NULL DEFAULT 0,
			lang TEXT NOT NULL DEFAULT '',
			time_zone TEXT NOT NULL DEFAULT '',
			is_wifi INTEGER NOT NULL DEFAULT -1,
			online INTEGER NOT NULL DEFAULT 0,
			binding_time TEXT NOT NULL DEFAULT '',
			login_time TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY(device_id,user_id,end_device_id)
		);`,
		`CREATE TABLE IF NOT EXISTS user_login_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL,
			end_device_id TEXT NOT NULL,
			login_at TEXT NOT NULL,
			logout_at TEXT,
			created_at TEXT NOT NULL
		);`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_user_session_open ON user_login_sessions(device_id,user_id,end_device_id) WHERE logout_at IS NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_user_session_history ON user_login_sessions(device_id,user_id,login_at DESC);`,
		`CREATE TABLE IF NOT EXISTS device_metadata (
			device_id TEXT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
			group_name TEXT NOT NULL DEFAULT '',
			location TEXT NOT NULL DEFAULT '',
			labels_json TEXT NOT NULL DEFAULT '{}',
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS saved_views (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			query_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS maintenance_windows (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			starts_at TEXT NOT NULL,
			ends_at TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, datetime('now'));`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(2, datetime('now'));`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE devices ADD COLUMN certificate_serial TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migration certificate_serial: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE devices ADD COLUMN certificate_expires_at TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migration certificate_expires_at: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE devices ADD COLUMN revoked_at TEXT`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migration revoked_at: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(3, datetime('now'))`)
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(4, datetime('now'))`)
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE inspections ADD COLUMN change_summary_json TEXT NOT NULL DEFAULT '{}'`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migration inspection change summary: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(5, datetime('now'))`)
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(6, datetime('now'))`)
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(7, datetime('now'))`)
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(8, datetime('now'))`)
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE application_runtime_state ADD COLUMN user_id TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migration application user_id: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE application_runtime_state ADD COLUMN user_name TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migration application user_name: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(9, datetime('now'))`)
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(10, datetime('now'))`)
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE user_runtime_state ADD COLUMN app_access_no_limit INTEGER NOT NULL DEFAULT 1`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migration user app access no limit: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE user_runtime_state ADD COLUMN allowed_app_ids_json TEXT NOT NULL DEFAULT '[]'`); err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("migration user allowed app ids: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(11, datetime('now'))`)
	userDeviceColumns := []struct {
		name string
		sql  string
	}{
		{"device_api_url", `ALTER TABLE user_device_state ADD COLUMN device_api_url TEXT NOT NULL DEFAULT ''`},
		{"is_mobile", `ALTER TABLE user_device_state ADD COLUMN is_mobile INTEGER NOT NULL DEFAULT 0`},
		{"is_tv", `ALTER TABLE user_device_state ADD COLUMN is_tv INTEGER NOT NULL DEFAULT 0`},
		{"lang", `ALTER TABLE user_device_state ADD COLUMN lang TEXT NOT NULL DEFAULT ''`},
		{"time_zone", `ALTER TABLE user_device_state ADD COLUMN time_zone TEXT NOT NULL DEFAULT ''`},
		{"is_wifi", `ALTER TABLE user_device_state ADD COLUMN is_wifi INTEGER NOT NULL DEFAULT -1`},
	}
	for _, column := range userDeviceColumns {
		if _, err := s.db.ExecContext(ctx, column.sql); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migration user device %s: %w", column.name, err)
		}
	}
	_, _ = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(12, datetime('now'))`)
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
	rows, err := s.reader().QueryContext(ctx, `SELECT id,name,hostname,os_version,collector_version,capabilities_json,status,created_at,last_seen_at FROM devices ORDER BY created_at DESC`)
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

func (s *Store) SetCertificate(ctx context.Context, deviceID, serial string, expiresAt time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE devices SET certificate_serial=?,certificate_expires_at=? WHERE id=? AND revoked_at IS NULL`, serial, expiresAt.UTC().Format(time.RFC3339Nano), deviceID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("device not found or revoked")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_certificates(serial,device_id,expires_at,created_at) VALUES(?,?,?,?)`, serial, deviceID, expiresAt.UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CertificateAllowed(ctx context.Context, deviceID, serial string) error {
	var found string
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err := s.db.QueryRowContext(ctx, `SELECT c.device_id FROM device_certificates c JOIN devices d ON d.id=c.device_id WHERE c.device_id=? AND c.serial=? AND c.revoked_at IS NULL AND d.revoked_at IS NULL AND c.expires_at>? AND (c.valid_until IS NULL OR c.valid_until>?)`, deviceID, serial, now, now).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("certificate revoked or unknown")
	}
	return err
}

func (s *Store) RotateCertificate(ctx context.Context, deviceID, oldSerial, newSerial string, expiresAt time.Time, grace time.Duration) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE device_certificates SET valid_until=? WHERE serial=? AND device_id=? AND revoked_at IS NULL`, now.Add(grace).Format(time.RFC3339Nano), oldSerial, deviceID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("old certificate not found")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_certificates(serial,device_id,expires_at,created_at) VALUES(?,?,?,?)`, newSerial, deviceID, expiresAt.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET certificate_serial=?,certificate_expires_at=? WHERE id=? AND revoked_at IS NULL`, newSerial, expiresAt.UTC().Format(time.RFC3339Nano), deviceID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RevokeDevice(ctx context.Context, deviceID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(ctx, `UPDATE devices SET revoked_at=?,status='revoked' WHERE id=? AND revoked_at IS NULL`, now, deviceID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return errors.New("device not found or already revoked")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE device_certificates SET revoked_at=? WHERE device_id=? AND revoked_at IS NULL`, now, deviceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,created_at) VALUES('device.revoked','device',?,?)`, deviceID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteDevice(ctx context.Context, deviceID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name, hostname string
	if err := tx.QueryRowContext(ctx, `SELECT name,hostname FROM devices WHERE id=?`, deviceID).Scan(&name, &hostname); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE id=?`, deviceID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return sql.ErrNoRows
	}
	metadata, _ := json.Marshal(map[string]string{"name": name, "hostname": hostname})
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_log(action,subject_type,subject_id,metadata_json,created_at) VALUES('device.deleted','device',?,?,?)`,
		deviceID, string(metadata), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
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
	latestStmt, err := tx.PrepareContext(ctx, `INSERT INTO latest_metrics(device_id,name,value,unit,labels_json,collected_at,received_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(device_id,name,labels_json) DO UPDATE SET
			value=excluded.value,
			unit=excluded.unit,
			collected_at=excluded.collected_at,
			received_at=excluded.received_at
		WHERE excluded.collected_at>=latest_metrics.collected_at`)
	if err != nil {
		return err
	}
	defer latestStmt.Close()
	for _, point := range batch.Points {
		labels, _ := json.Marshal(point.Labels)
		collectedAt := point.CollectedAt.UTC().Format(time.RFC3339Nano)
		receivedAt := now.Format(time.RFC3339Nano)
		if _, err := stmt.ExecContext(ctx, batch.DeviceID, point.Name, point.Value, point.Unit, string(labels), collectedAt, receivedAt); err != nil {
			return err
		}
		if _, err := latestStmt.ExecContext(ctx, batch.DeviceID, point.Name, point.Value, point.Unit, string(labels), collectedAt, receivedAt); err != nil {
			return err
		}
	}
	if batch.ProcessesCollected {
		latestProcessStmt, err := tx.PrepareContext(ctx, `INSERT INTO latest_processes(
			device_id,pid,start_time,name,user_name,command,state,cgroup_path,cpu_percent,memory_rss_bytes,
			read_bytes,write_bytes,read_rate,write_rate,threads,uptime_seconds,collected_at,received_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(device_id,pid,start_time) DO UPDATE SET
			name=excluded.name,user_name=excluded.user_name,command=excluded.command,state=excluded.state,
			cgroup_path=excluded.cgroup_path,cpu_percent=excluded.cpu_percent,memory_rss_bytes=excluded.memory_rss_bytes,
			read_bytes=excluded.read_bytes,write_bytes=excluded.write_bytes,read_rate=excluded.read_rate,
			write_rate=excluded.write_rate,threads=excluded.threads,uptime_seconds=excluded.uptime_seconds,
			collected_at=excluded.collected_at,received_at=excluded.received_at`)
		if err != nil {
			return err
		}
		defer latestProcessStmt.Close()
		historyProcessStmt, err := tx.PrepareContext(ctx, `INSERT INTO process_samples(
			device_id,pid,start_time,name,user_name,command,state,cgroup_path,cpu_percent,memory_rss_bytes,
			read_bytes,write_bytes,read_rate,write_rate,threads,uptime_seconds,collected_at,received_at
		) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer historyProcessStmt.Close()
		for _, process := range batch.Processes {
			args := []any{
				batch.DeviceID, process.PID, process.StartTime, process.Name, process.User, process.Command,
				process.State, process.Cgroup, process.CPUPercent, process.MemoryRSSBytes, process.ReadBytes,
				process.WriteBytes, process.ReadRate, process.WriteRate, process.Threads, process.UptimeSeconds,
				process.CollectedAt.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
			}
			if _, err := latestProcessStmt.ExecContext(ctx, args...); err != nil {
				return err
			}
			if process.RecordHistory {
				if _, err := historyProcessStmt.ExecContext(ctx, args...); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM latest_processes WHERE device_id=? AND received_at<?`, batch.DeviceID, now.Format(time.RFC3339Nano)); err != nil {
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
