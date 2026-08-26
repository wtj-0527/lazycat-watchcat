package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

func TestCreateValidateAndRestore(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "data", "watchcat.db")
	manager := New(dbPath, filepath.Join(root, "backups"), "1.4.0")
	if err := manager.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	manager.Attach(st)
	if err := manager.MarkVersion(); err != nil {
		t.Fatal(err)
	}
	code, _, _ := st.CreatePairingCode(ctx, time.Minute)
	if _, err := st.PairCollector(ctx, protocol.PairCollectorRequest{Code: code, Hostname: "before-restore", CollectorVer: "1.4.0"}); err != nil {
		t.Fatal(err)
	}
	item, err := manager.Create(ctx, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if !item.Verified || len(item.SHA256) != 64 {
		t.Fatalf("invalid manifest: %+v", item)
	}
	code, _, _ = st.CreatePairingCode(ctx, time.Minute)
	if _, err := st.PairCollector(ctx, protocol.PairCollectorRequest{Code: code, Hostname: "after-backup", CollectorVer: "1.4.0"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.StageRestore(item.Name); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	startup := New(dbPath, filepath.Join(root, "backups"), "1.4.0")
	if err := startup.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	devices, err := restored.ListDevices(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Hostname != "before-restore" {
		t.Fatalf("restore did not return to backup state: %+v", devices)
	}
	items, err := startup.List()
	if err != nil {
		t.Fatal(err)
	}
	foundSafetyBackup := false
	for _, candidate := range items {
		foundSafetyBackup = foundSafetyBackup || candidate.Type == "pre-restore"
	}
	if !foundSafetyBackup {
		t.Fatal("pre-restore safety backup was not created")
	}
}

func TestCorruptBackupIsRejectedAndUpgradeBackupIsCreated(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath, backupDir := filepath.Join(root, "watchcat.db"), filepath.Join(root, "backups")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	manager := New(dbPath, backupDir, "1.3.1")
	if err := manager.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	manager.Attach(st)
	if err := manager.MarkVersion(); err != nil {
		t.Fatal(err)
	}
	item, err := manager.Create(ctx, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, item.Name+".db"), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.StageRestore(item.Name); err == nil {
		t.Fatal("corrupt backup was accepted")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	upgrade := New(dbPath, backupDir, "1.4.0")
	if err := upgrade.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := upgrade.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range items {
		found = found || candidate.Type == "pre-upgrade"
	}
	if !found {
		t.Fatal("version transition did not create a pre-upgrade backup")
	}
}

func TestDeleteBackupRemovesManifestAndDatabase(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath, backupDir := filepath.Join(root, "watchcat.db"), filepath.Join(root, "backups")
	manager := New(dbPath, backupDir, "1.12.5")
	if err := manager.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	manager.Attach(st)
	if err := manager.MarkVersion(); err != nil {
		t.Fatal(err)
	}
	item, err := manager.Create(ctx, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(item.Name); err != nil {
		t.Fatal(err)
	}
	items, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("backup still listed: %+v", items)
	}
	for _, suffix := range []string{".db", ".json"} {
		if _, err := os.Stat(filepath.Join(backupDir, item.Name+suffix)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists or unexpected stat error: %v", suffix, err)
		}
	}
}
