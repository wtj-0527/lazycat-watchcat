package collector

import (
	"testing"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

func TestMarkProcessHistoryKeepsHighIOSources(t *testing.T) {
	items := make([]protocol.ProcessSample, 12)
	for index := range items {
		items[index] = protocol.ProcessSample{
			PID: index + 1, CPUPercent: float64(100 - index),
			MemoryRSSBytes: uint64(1000 - index),
		}
	}
	items[11].CPUPercent = 0.1
	items[11].MemoryRSSBytes = 1
	items[11].WriteRate = 1024 * 1024

	markProcessHistory(items, 6)

	if !items[11].RecordHistory {
		t.Fatal("highest I/O process was omitted from history")
	}
	count := 0
	for _, item := range items {
		if item.RecordHistory {
			count++
		}
	}
	if count != 6 {
		t.Fatalf("history count=%d", count)
	}
}
