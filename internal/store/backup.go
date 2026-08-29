package store

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) CreateSQLiteBackup(ctx context.Context, destination string) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		return fmt.Errorf("checkpoint database: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM main INTO ?`, destination); err != nil {
		return fmt.Errorf("vacuum database into backup: %w", err)
	}
	return nil
}

func (s *Store) IntegrityCheck(ctx context.Context) error {
	var result string
	if err := s.db.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("sqlite quick_check: %s", result)
	}
	return nil
}

// DatabaseProbe verifies that SQLite can read the schema and execute a query
// without scanning the multi-gigabyte metrics database. Full quick_check is
// intentionally reserved for explicit maintenance and backup verification:
// running it periodically can keep a mechanical data disk at 100% Busy for
// tens of minutes.
func (s *Store) DatabaseProbe(ctx context.Context) error {
	var version int
	err := s.reader().QueryRowContext(ctx, `SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("database probe: %w", err)
	}
	return nil
}
