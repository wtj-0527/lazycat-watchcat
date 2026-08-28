package upgradecoord

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerSerializesAndPersistsUpgradeLeases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade-coordinator.json")
	now := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	manager, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = func() time.Time { return now }
	first, err := manager.Acquire(Request{RequestID: "one", AppID: "app", InstanceID: "studio-a"})
	if err != nil || first.Status != "granted" || first.Lease == nil {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := manager.Acquire(Request{RequestID: "two", AppID: "app", InstanceID: "studio-b"})
	if err != nil || second.Status != "waiting" || second.Position != 1 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	reloaded.now = func() time.Time { return now.Add(10 * time.Second) }
	snapshot := reloaded.Snapshot()
	if snapshot.Active == nil || snapshot.Active.RequestID != "one" || len(snapshot.Queue) != 1 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if _, err := reloaded.Release(first.Lease.Token); err != nil {
		t.Fatal(err)
	}
	granted, err := reloaded.Acquire(Request{RequestID: "two", AppID: "app", InstanceID: "studio-b"})
	if err != nil || granted.Status != "granted" || granted.Lease == nil {
		t.Fatalf("granted=%+v err=%v", granted, err)
	}
}

func TestManagerExpiresAbandonedLease(t *testing.T) {
	manager, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	manager.now = func() time.Time { return now }
	first, _ := manager.Acquire(Request{RequestID: "one", AppID: "app", InstanceID: "a"})
	manager.now = func() time.Time { return now.Add(defaultLeaseTTL + time.Second) }
	second, err := manager.Acquire(Request{RequestID: "two", AppID: "app", InstanceID: "b"})
	if err != nil || second.Status != "granted" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if _, err := manager.Renew(first.Lease.Token); !errors.Is(err, ErrLeaseNotFound) {
		t.Fatalf("renew err=%v", err)
	}
}
