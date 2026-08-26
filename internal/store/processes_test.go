package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

func TestProcessSnapshotAndHistory(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "watchcat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	deviceID, err := st.EnsureLocalDevice(ctx, "test", "test-host", "linux/amd64", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	batch := protocol.MetricBatch{
		DeviceID:           deviceID,
		Points:             []protocol.MetricPoint{{Name: "system.load.1m", Value: 1, CollectedAt: now}},
		ProcessesCollected: true,
		Processes: []protocol.ProcessSample{
			{PID: 10, StartTime: "100", Name: "worker", User: "root", CPUPercent: 12, MemoryRSSBytes: 1024, RecordHistory: true, CollectedAt: now},
			{PID: 11, StartTime: "101", Name: "idle", User: "nobody", CPUPercent: 1, MemoryRSSBytes: 2048, CollectedAt: now},
		},
	}
	if err := st.IngestMetrics(ctx, batch); err != nil {
		t.Fatal(err)
	}
	page, err := st.LatestProcesses(ctx, deviceID, ProcessListOptions{Sort: "cpu", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].PID != 10 {
		t.Fatalf("page=%+v", page)
	}
	history, err := st.ProcessHistory(ctx, deviceID, 10, "100", now.Add(-time.Hour), now.Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Name != "worker" {
		t.Fatalf("history=%+v", history)
	}
	noHistory, err := st.ProcessHistory(ctx, deviceID, 11, "101", now.Add(-time.Hour), now.Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(noHistory) != 0 {
		t.Fatalf("non-ranked process unexpectedly persisted: %+v", noHistory)
	}
}
