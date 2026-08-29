package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

func testStoreDevice(t *testing.T) (*Store, protocol.PairCollectorResponse) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := st.CreatePairingCode(context.Background(), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	paired, err := st.PairCollector(context.Background(), protocol.PairCollectorRequest{
		Code: code, Name: "节点一", Hostname: "node-1", CollectorVer: "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	return st, paired
}

func TestDatabaseProbeUsesLightweightSchemaRead(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if err := st.DatabaseProbe(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentAlertStateMachine(t *testing.T) {
	ctx := context.Background()
	st, paired := testStoreDevice(t)
	defer st.Close()
	signal := AlertSignal{Fingerprint: "safe-fingerprint", DeviceID: paired.DeviceID, DeviceName: "节点一", Severity: "warning", Resource: "/", Message: "存储使用率 90%", Value: 90, Unit: "%", ObservedAt: time.Now().UTC()}
	if err := st.ReconcileAlerts(ctx, []AlertSignal{signal}); err != nil {
		t.Fatal(err)
	}
	alerts, err := st.ListAlerts(ctx, false)
	if err != nil || len(alerts) != 1 || alerts[0].Status != "firing" {
		t.Fatalf("initial alerts=%+v err=%v", alerts, err)
	}
	if err := st.SetAlertState(ctx, signal.Fingerprint, "resolve", 0); err == nil {
		t.Fatal("manual resolve must be rejected by the alert state machine")
	}
	alerts, err = st.ListAlerts(ctx, false)
	if err != nil || len(alerts) != 1 || alerts[0].Status != "firing" {
		t.Fatalf("manual resolve changed alert: alerts=%+v err=%v", alerts, err)
	}
	if err := st.SetAlertState(ctx, signal.Fingerprint, "acknowledge", 0); err != nil {
		t.Fatal(err)
	}
	signal.ObservedAt = signal.ObservedAt.Add(time.Minute)
	if err := st.ReconcileAlerts(ctx, []AlertSignal{signal}); err != nil {
		t.Fatal(err)
	}
	alerts, _ = st.ListAlerts(ctx, false)
	if alerts[0].Status != "acknowledged" {
		t.Fatalf("acknowledgement was not preserved: %+v", alerts[0])
	}
	if err := st.ReconcileAlerts(ctx, nil); err != nil {
		t.Fatal(err)
	}
	all, _ := st.ListAlerts(ctx, true)
	if all[0].Status != "resolved" || all[0].ResolvedAt == nil {
		t.Fatalf("alert was not resolved: %+v", all[0])
	}
	if err := st.ReconcileAlerts(ctx, []AlertSignal{signal}); err != nil {
		t.Fatal(err)
	}
	alerts, _ = st.ListAlerts(ctx, false)
	if alerts[0].Status != "firing" {
		t.Fatalf("resolved alert did not refire: %+v", alerts[0])
	}
	var transitionCount, outboxCount int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM alert_transitions WHERE fingerprint=?`, signal.Fingerprint).Scan(&transitionCount); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM notification_outbox WHERE alert_fingerprint=?`, signal.Fingerprint).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if transitionCount != 4 || outboxCount != 2 {
		t.Fatalf("transitions=%d outbox=%d", transitionCount, outboxCount)
	}
}

func TestInspectionEvidenceAndRetentionRollupAreIdempotent(t *testing.T) {
	ctx := context.Background()
	st, paired := testStoreDevice(t)
	defer st.Close()
	now := time.Now().UTC().Truncate(time.Hour)
	points := []protocol.MetricPoint{
		{Name: "system.load.1m", Value: 1, CollectedAt: now.Add(-2*time.Hour + time.Minute)},
		{Name: "system.load.1m", Value: 2, CollectedAt: now.Add(-2*time.Hour + 2*time.Minute)},
		{Name: "system.load.1m", Value: 9, CollectedAt: now.Add(-2*time.Hour + 3*time.Minute)},
	}
	if err := st.IngestMetrics(ctx, protocol.MetricBatch{DeviceID: paired.DeviceID, Points: points}); err != nil {
		t.Fatal(err)
	}
	runAt := time.Now().UTC().Add(time.Millisecond)
	first, err := st.RunRetention(ctx, runAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.RunRetention(ctx, runAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var count, samples int
	var min, max, avg, p95 float64
	if err := st.db.QueryRow(`SELECT COUNT(*),sample_count,min_value,max_value,avg_value,p95_value FROM metric_rollups_hourly`).Scan(&count, &samples, &min, &max, &avg, &p95); err != nil {
		t.Fatal(err)
	}
	if first.RollupBuckets != 1 || second.RollupBuckets != 0 || count != 1 || samples != 3 || min != 1 || max != 9 || avg != 4 || p95 != 9 {
		t.Fatalf("first=%+v second=%+v count=%d samples=%d values=%v/%v/%v/%v", first, second, count, samples, min, max, avg, p95)
	}
	report := map[string]any{"devices": 1, "checks": []string{"online", "storage"}}
	inspection, err := st.SaveInspection(ctx, "manual", report, map[string]any{"baseline": false}, 1, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.EvidenceSHA256) != 64 {
		t.Fatalf("evidence hash=%q", inspection.EvidenceSHA256)
	}
	loaded, err := st.InspectionByID(ctx, inspection.ID)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(loaded.Report, &decoded); err != nil || decoded["devices"].(float64) != 1 {
		t.Fatalf("report=%s err=%v", loaded.Report, err)
	}
}

func TestReadPoolRemainsAvailableWhenWriterPoolIsOccupied(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "read-pool.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	deviceID, err := st.EnsureLocalDevice(ctx, "node", "node", "linux/amd64", "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.ReplaceRuntimeApplications(ctx, deviceID, []RuntimeApplication{{
		DeviceID: deviceID, DeployID: "app-1", AppID: "app.one", Title: "App One",
	}}); err != nil {
		t.Fatal(err)
	}
	if err = st.ObserveRuntimeUsers(ctx, deviceID, []RuntimeUser{{
		UserID: "user-1", Nickname: "User One",
	}}); err != nil {
		t.Fatal(err)
	}
	first, err := st.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := st.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	devices, err := st.ListDevices(readCtx)
	if err != nil {
		t.Fatalf("read blocked by occupied writer pool: %v", err)
	}
	if len(devices) != 1 || devices[0].Name != "node" {
		t.Fatalf("devices=%+v", devices)
	}
	applications, err := st.ListRuntimeApplications(readCtx)
	if err != nil || len(applications) != 1 {
		t.Fatalf("runtime applications blocked by occupied writer pool: items=%+v err=%v", applications, err)
	}
	users, err := st.ListRuntimeUsers(readCtx)
	if err != nil || len(users) != 1 {
		t.Fatalf("runtime users blocked by occupied writer pool: items=%+v err=%v", users, err)
	}
	sessions, err := st.ListUserLoginSessions(readCtx, time.Now().Add(-24*time.Hour))
	if err != nil || len(sessions) != 0 {
		t.Fatalf("user sessions blocked by occupied writer pool: items=%+v err=%v", sessions, err)
	}
}

func TestLatestMetricsProjectionAvoidsHistoricalScanAndRejectsOlderSamples(t *testing.T) {
	ctx := context.Background()
	st, paired := testStoreDevice(t)
	defer st.Close()

	now := time.Now().UTC()
	newer := protocol.MetricPoint{
		Name: "system.cpu.usage", Value: 42, Unit: "%",
		Labels: map[string]string{"scope": "host"}, CollectedAt: now,
	}
	older := newer
	older.Value = 99
	older.CollectedAt = now.Add(-time.Minute)
	if err := st.IngestMetrics(ctx, protocol.MetricBatch{DeviceID: paired.DeviceID, Points: []protocol.MetricPoint{newer}}); err != nil {
		t.Fatal(err)
	}
	if err := st.IngestMetrics(ctx, protocol.MetricBatch{DeviceID: paired.DeviceID, Points: []protocol.MetricPoint{older}}); err != nil {
		t.Fatal(err)
	}

	items, err := st.ListLatestMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Value != 42 || !items[0].CollectedAt.Equal(now) {
		t.Fatalf("latest metrics=%+v", items)
	}
	var historyRows, latestRows int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM metrics`).Scan(&historyRows); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM latest_metrics`).Scan(&latestRows); err != nil {
		t.Fatal(err)
	}
	if historyRows != 2 || latestRows != 1 {
		t.Fatalf("historyRows=%d latestRows=%d", historyRows, latestRows)
	}
}
