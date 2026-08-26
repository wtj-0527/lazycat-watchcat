package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

type Manager struct {
	dbPath  string
	dir     string
	version string
	store   *store.Store
}

type Manifest struct {
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	AppVersion string    `json:"appVersion"`
	CreatedAt  time.Time `json:"createdAt"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256"`
	Verified   bool      `json:"verified"`
}

type RestoreRequest struct {
	Name        string    `json:"name"`
	RequestedAt time.Time `json:"requestedAt"`
}

type Status struct {
	DatabasePath   string    `json:"databasePath"`
	DatabaseSize   int64     `json:"databaseSize"`
	IntegrityOK    bool      `json:"integrityOk"`
	IntegrityError string    `json:"integrityError,omitempty"`
	BackupCount    int       `json:"backupCount"`
	LatestBackup   *Manifest `json:"latestBackup,omitempty"`
	PendingRestore string    `json:"pendingRestore,omitempty"`
	CurrentVersion string    `json:"currentVersion"`
	CheckedAt      time.Time `json:"checkedAt"`
}

func New(dbPath, dir, version string) *Manager {
	return &Manager{dbPath: dbPath, dir: dir, version: version}
}

func (m *Manager) Attach(st *store.Store) { m.store = st }

func (m *Manager) Prepare(ctx context.Context) error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	if err := m.applyStagedRestore(ctx); err != nil {
		return err
	}
	var previous string
	raw, err := os.ReadFile(m.versionMarker())
	if err == nil {
		previous = strings.TrimSpace(string(raw))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if fileExists(m.dbPath) && previous != m.version {
		sourceVersion := safeName(previous)
		if sourceVersion == "" {
			sourceVersion = "unversioned"
		}
		if _, err := m.createOffline(ctx, "pre-upgrade", "from-"+sourceVersion); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) MarkVersion() error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return err
	}
	tmp := m.versionMarker() + ".tmp"
	if err := os.WriteFile(tmp, []byte(m.version+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.versionMarker())
}

func (m *Manager) Status(ctx context.Context) Status {
	status := Status{DatabasePath: m.dbPath, CurrentVersion: m.version, CheckedAt: time.Now().UTC()}
	if info, err := os.Stat(m.dbPath); err == nil {
		status.DatabaseSize = info.Size()
	}
	if items, err := m.List(); err == nil {
		status.BackupCount = len(items)
		if len(items) > 0 {
			latest := items[0]
			status.LatestBackup = &latest
		}
	}
	if raw, err := os.ReadFile(m.restoreRequestPath()); err == nil {
		var request RestoreRequest
		if json.Unmarshal(raw, &request) == nil {
			status.PendingRestore = request.Name
		}
	}
	return status
}

func (m *Manager) Create(ctx context.Context, kind string) (Manifest, error) {
	if m.store == nil {
		return Manifest{}, errors.New("backup manager is not attached")
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return Manifest{}, err
	}
	name := backupName(kind, m.version, "")
	path := filepath.Join(m.dir, name+".db")
	if err := m.store.CreateSQLiteBackup(ctx, path); err != nil {
		return Manifest{}, err
	}
	item, err := m.writeManifest(path, name, kind)
	if err != nil {
		_ = os.Remove(path)
		return Manifest{}, err
	}
	keep := store.DefaultOperationalSettings().BackupRetentionCount
	if m.store != nil {
		keep = m.store.OperationalSettings(ctx).BackupRetentionCount
	}
	if err := m.Prune(keep); err != nil {
		return item, err
	}
	return item, nil
}

func (m *Manager) List() ([]Manifest, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Manifest{}, nil
		}
		return nil, err
	}
	var out []Manifest
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || entry.Name() == "restore-request.json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			continue
		}
		var item Manifest
		if json.Unmarshal(raw, &item) != nil || item.Name == "" {
			continue
		}
		// Full SHA-256 and SQLite verification is intentionally performed when
		// a backup is created and again before restore. Listing must remain
		// metadata-only; re-reading every multi-gigabyte backup blocked the
		// settings page for minutes.
		info, statErr := os.Stat(filepath.Join(m.dir, item.Name+".db"))
		item.Verified = item.Verified && statErr == nil && info.Size() == item.Size
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *Manager) StageRestore(name string) error {
	normalized := safeName(name)
	if normalized == "" || normalized != name {
		return errors.New("invalid backup name")
	}
	name = normalized
	item, err := m.loadManifest(name)
	if err != nil {
		return err
	}
	if err := m.verifyManifest(item); err != nil {
		return err
	}
	request := RestoreRequest{Name: name, RequestedAt: time.Now().UTC()}
	raw, _ := json.MarshalIndent(request, "", "  ")
	tmp := m.restoreRequestPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.restoreRequestPath())
}

func (m *Manager) Prune(keep int) error {
	if keep < 1 {
		keep = 1
	}
	items, err := m.List()
	if err != nil {
		return err
	}
	if len(items) <= keep {
		return nil
	}
	for _, item := range items[keep:] {
		_ = os.Remove(filepath.Join(m.dir, item.Name+".db"))
		_ = os.Remove(filepath.Join(m.dir, item.Name+".json"))
	}
	return nil
}

func (m *Manager) Delete(name string) error {
	normalized := safeName(name)
	if normalized == "" || normalized != name {
		return errors.New("invalid backup name")
	}
	item, err := m.loadManifest(name)
	if err != nil {
		return err
	}
	if raw, readErr := os.ReadFile(m.restoreRequestPath()); readErr == nil {
		var request RestoreRequest
		if json.Unmarshal(raw, &request) == nil && request.Name == name {
			return errors.New("backup is pending restore")
		}
	}
	if err := os.Remove(filepath.Join(m.dir, item.Name+".db")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(filepath.Join(m.dir, item.Name+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (m *Manager) applyStagedRestore(ctx context.Context) error {
	raw, err := os.ReadFile(m.restoreRequestPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var request RestoreRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return fmt.Errorf("decode restore request: %w", err)
	}
	item, err := m.loadManifest(safeName(request.Name))
	if err != nil {
		return err
	}
	if err := m.verifyManifest(item); err != nil {
		return err
	}
	if fileExists(m.dbPath) {
		if _, err := m.createOffline(ctx, "pre-restore", ""); err != nil {
			return err
		}
	}
	source := filepath.Join(m.dir, item.Name+".db")
	tmp := m.dbPath + ".restore.tmp"
	if err := os.MkdirAll(filepath.Dir(m.dbPath), 0o700); err != nil {
		return err
	}
	if err := copyFile(source, tmp, 0o600); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(m.dbPath + suffix)
	}
	if err := os.Rename(tmp, m.dbPath); err != nil {
		return err
	}
	return os.Remove(m.restoreRequestPath())
}

func (m *Manager) createOffline(ctx context.Context, kind, suffix string) (Manifest, error) {
	name := backupName(kind, m.version, suffix)
	path := filepath.Join(m.dir, name+".db")
	db, err := sql.Open("sqlite", m.dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return Manifest{}, err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		return Manifest{}, err
	}
	if _, err := db.ExecContext(ctx, `VACUUM main INTO ?`, path); err != nil {
		return Manifest{}, err
	}
	return m.writeManifest(path, name, kind)
}

func (m *Manager) writeManifest(path, name, kind string) (Manifest, error) {
	if err := verifySQLite(path); err != nil {
		return Manifest{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Manifest{}, err
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return Manifest{}, err
	}
	item := Manifest{Name: name, Type: kind, AppVersion: m.version, CreatedAt: time.Now().UTC(), Size: info.Size(), SHA256: sum, Verified: true}
	raw, _ := json.MarshalIndent(item, "", "  ")
	if err := os.WriteFile(filepath.Join(m.dir, name+".json"), raw, 0o600); err != nil {
		return Manifest{}, err
	}
	return item, nil
}

func (m *Manager) loadManifest(name string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(m.dir, name+".json"))
	if err != nil {
		return Manifest{}, err
	}
	var item Manifest
	if err := json.Unmarshal(raw, &item); err != nil {
		return Manifest{}, err
	}
	if item.Name != name {
		return Manifest{}, errors.New("backup manifest name mismatch")
	}
	return item, nil
}

func (m *Manager) verifyManifest(item Manifest) error {
	path := filepath.Join(m.dir, item.Name+".db")
	sum, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if sum != item.SHA256 {
		return errors.New("backup checksum mismatch")
	}
	return verifySQLite(path)
}

func verifySQLite(path string) error {
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("backup quick_check: %s", result)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func backupName(kind, version, suffix string) string {
	parts := []string{safeName(kind), time.Now().UTC().Format("20060102T150405.000000000Z"), "v" + safeName(version)}
	if suffix != "" {
		parts = append(parts, safeName(suffix))
	}
	return strings.Join(parts, "-")
}
func safeName(value string) string {
	var out strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			out.WriteRune(r)
		}
	}
	return strings.Trim(out.String(), ".")
}
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
func (m *Manager) restoreRequestPath() string { return filepath.Join(m.dir, "restore-request.json") }
func (m *Manager) versionMarker() string      { return filepath.Join(m.dir, "current-version") }
