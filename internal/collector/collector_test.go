package collector

import (
	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
	"path/filepath"
	"testing"
	"time"
)

func TestQueueIsBoundedAndFIFO(t *testing.T) {
	q := NewQueue(filepath.Join(t.TempDir(), "q.json"), 2)
	for i := 0; i < 3; i++ {
		if err := q.Append(protocol.MetricBatch{DeviceID: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	first, ok, err := q.Peek()
	if err != nil || !ok || first.DeviceID != "b" {
		t.Fatalf("first=%+v ok=%v err=%v", first, ok, err)
	}
	if err := q.Pop(); err != nil {
		t.Fatal(err)
	}
	next, _, _ := q.Peek()
	if next.DeviceID != "c" {
		t.Fatalf("next=%+v", next)
	}
}
func TestCollectReturnsWhitelistedMetrics(t *testing.T) {
	batch, err := Collect("device-1", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Points) < 3 {
		t.Fatalf("points=%d", len(batch.Points))
	}
	for _, p := range batch.Points {
		if p.Name == "" || p.CollectedAt.IsZero() {
			t.Fatalf("invalid point %+v", p)
		}
	}
}
