package scheduler

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/api"
	"github.com/wtj-0527/lazycat-maoyan/internal/pki"
	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

func TestScheduledInspectionIsDeduplicatedPerPeriod(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	code, _, err := st.CreatePairingCode(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	device, err := st.PairCollector(ctx, protocol.PairCollectorRequest{Code: code, Name: "node", Hostname: "node", CollectorVer: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.IngestMetrics(ctx, protocol.MetricBatch{DeviceID: device.DeviceID, Points: []protocol.MetricPoint{{Name: "system.load.1m", Value: 1, CollectedAt: time.Now().UTC()}}}); err != nil {
		t.Fatal(err)
	}
	ca, err := pki.LoadOrCreate(filepath.Join(t.TempDir(), "pki"))
	if err != nil {
		t.Fatal(err)
	}
	server := api.New(st, ca, "../../web", time.Minute)
	scheduler := NewInspectionScheduler(server, st, slog.Default())
	scheduler.runPeriod(ctx, "daily", "2026-08-23", "scheduled-daily")
	scheduler.runPeriod(ctx, "daily", "2026-08-23", "scheduled-daily")
	items, err := st.ListInspections(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TriggerType != "scheduled-daily" {
		t.Fatalf("items=%+v", items)
	}
}

func TestFormatWeek(t *testing.T) {
	if got := formatWeek(2026, 7); got != "2026-W07" {
		t.Fatalf("got %q", got)
	}
}
