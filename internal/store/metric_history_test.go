package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

func TestSampledMetricHistoryPreservesLabelSeriesAcrossRange(t *testing.T) {
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
	start := time.Now().UTC().Add(-12 * time.Hour).Truncate(time.Second)
	for index := 0; index < 12; index++ {
		at := start.Add(time.Duration(index) * time.Hour)
		if err := st.IngestMetrics(ctx, protocol.MetricBatch{
			DeviceID: deviceID,
			Points: []protocol.MetricPoint{
				{Name: "disk.io.busy_percent", Value: float64(index), Unit: "%", Labels: map[string]string{"device": "sda"}, CollectedAt: at},
				{Name: "disk.io.busy_percent", Value: float64(index + 100), Unit: "%", Labels: map[string]string{"device": "sdb"}, CollectedAt: at},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	items, err := st.SampledMetricHistory(ctx, deviceID, "disk.io.busy_percent", start.Add(-time.Minute), start.Add(12*time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	series := map[string]int{}
	for _, item := range items {
		series[item.Labels["device"]]++
	}
	if series["sda"] < 2 || series["sdb"] < 2 {
		t.Fatalf("expected both disk series across the range, got counts=%v items=%+v", series, items)
	}
	if !items[0].CollectedAt.Before(items[len(items)-1].CollectedAt) {
		t.Fatalf("samples are not chronological: %+v", items)
	}
}
