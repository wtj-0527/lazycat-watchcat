package stability

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

func TestObservationPersistsAcrossMonitorRestart(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	first := New(st, logger, time.Hour)
	status, err := first.Reset(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.SampleCount < 1 || !status.DatabaseIntegrityOK || status.DatabaseIntegrityAt == nil {
		t.Fatalf("unexpected first sample: %+v", status)
	}
	firstIntegrityAt := *status.DatabaseIntegrityAt
	first.sample(context.Background())
	resampled := first.Current()
	if resampled.SampleCount != status.SampleCount+1 || resampled.DatabaseIntegrityAt == nil || !resampled.DatabaseIntegrityAt.Equal(firstIntegrityAt) {
		t.Fatalf("integrity check should be reused inside cadence: first=%+v resampled=%+v", status, resampled)
	}
	second := New(st, logger, time.Hour)
	second.loadOrReset(context.Background())
	loaded := second.Current()
	if !loaded.StartedAt.Equal(resampled.StartedAt) || loaded.SampleCount != resampled.SampleCount || loaded.DatabaseIntegrityAt == nil {
		t.Fatalf("observation was not persisted: first=%+v loaded=%+v", resampled, loaded)
	}
}
