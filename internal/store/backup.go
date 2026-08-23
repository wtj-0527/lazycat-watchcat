package store

import (
	"context"
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
