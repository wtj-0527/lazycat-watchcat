package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

func TestAcceleratedConcurrentWritesReadsBackupAndReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "stress.db")
	st, paired := testStoreDeviceAt(t, dbPath)

	const batches = 120
	const pointsPerBatch = 25
	var wg sync.WaitGroup
	errs := make(chan error, batches+40)
	for i := 0; i < batches; i++ {
		wg.Add(1)
		go func(batch int) {
			defer wg.Done()
			points := make([]protocol.MetricPoint, 0, pointsPerBatch)
			for j := 0; j < pointsPerBatch; j++ {
				points = append(points, protocol.MetricPoint{
					Name:        "stress.metric",
					Value:       float64(batch*pointsPerBatch + j),
					Unit:        "n",
					Labels:      map[string]string{"series": "accelerated"},
					CollectedAt: time.Now().UTC(),
				})
			}
			if err := st.IngestMetrics(ctx, protocol.MetricBatch{DeviceID: paired.DeviceID, Points: points}); err != nil {
				errs <- err
			}
		}(i)
		if i%3 == 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := st.ListLatestMetrics(ctx); err != nil {
					errs <- err
				}
			}()
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	backupPath := filepath.Join(root, "stress-backup.db")
	if err := st.CreateSQLiteBackup(ctx, backupPath); err != nil {
		t.Fatal(err)
	}
	if err := st.IntegrityCheck(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM metrics WHERE name='stress.metric'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != batches*pointsPerBatch {
		t.Fatalf("metric count=%d want=%d", count, batches*pointsPerBatch)
	}
}

func testStoreDeviceAt(t *testing.T, path string) (*Store, protocol.PairCollectorResponse) {
	t.Helper()
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := st.CreatePairingCode(context.Background(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	paired, err := st.PairCollector(context.Background(), protocol.PairCollectorRequest{
		Code: code, Hostname: "stress-node", CollectorVer: "1.4.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	return st, paired
}
