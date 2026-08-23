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
	if status.SampleCount < 1 || !status.DatabaseIntegrityOK {
		t.Fatalf("unexpected first sample: %+v", status)
	}
	second := New(st, logger, time.Hour)
	second.loadOrReset(context.Background())
	loaded := second.Current()
	if !loaded.StartedAt.Equal(status.StartedAt) || loaded.SampleCount != status.SampleCount {
		t.Fatalf("observation was not persisted: first=%+v loaded=%+v", status, loaded)
	}
}
