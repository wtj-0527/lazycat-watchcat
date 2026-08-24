package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gitee.com/linakesi/lzc-sdk/lang/go/sys"
	"github.com/wtj-0527/lazycat-maoyan/internal/pki"
	"github.com/wtj-0527/lazycat-maoyan/internal/runtimeapps"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestAggregateApplicationResourcesIgnoresStaleContainers(t *testing.T) {
	now := time.Now().UTC()
	metrics := []store.LatestMetric{
		{Name: "container.running", Value: 1, Labels: map[string]string{"app": "app.one", "container": "a"}, CollectedAt: now},
		{Name: "container.running", Value: 1, Labels: map[string]string{"app": "app.one", "container": "b"}, CollectedAt: now},
		{Name: "container.cpu.usage", Value: 12.5, Labels: map[string]string{"app": "app.one", "container": "a"}, CollectedAt: now},
		{Name: "container.cpu.usage", Value: 7.5, Labels: map[string]string{"app": "app.one", "container": "b"}, CollectedAt: now},
		{Name: "container.memory.usage", Value: 1024, Labels: map[string]string{"app": "app.one", "container": "a"}, CollectedAt: now},
		{Name: "container.memory.usage", Value: 9999, Labels: map[string]string{"app": "app.one", "container": "old"}, CollectedAt: now.Add(-7 * time.Minute)},
	}
	item := aggregateApplicationResources(metrics, now)["app.one"]
	if item.Containers != 2 || item.CPUPercent != 20 || item.MemoryUsage != 1024 {
		t.Fatalf("item=%+v", item)
	}
}

func TestTemperatureAlertsUseStableSensorClasses(t *testing.T) {
	if severity, _ := metricAlert("system.temperature", 99, "celsius", map[string]string{"sensor": "coretemp_core_0"}); severity != "" {
		t.Fatalf("per-core spike should remain telemetry only, severity=%q", severity)
	}
	if severity, _ := metricAlert("system.temperature", 99, "celsius", map[string]string{"sensor": "coretemp_package_id_0"}); severity != "" {
		t.Fatalf("CPU package below its hardware limit should not page, severity=%q", severity)
	}
	if severity, _ := metricAlert("system.temperature", 100, "celsius", map[string]string{"sensor": "coretemp_package_id_0"}); severity != "critical" {
		t.Fatalf("CPU package at its hardware limit should be critical, severity=%q", severity)
	}
	if severity, _ := metricAlert("system.temperature", 86, "celsius", map[string]string{"sensor": "nvme_composite"}); severity != "warning" {
		t.Fatalf("NVMe composite threshold mismatch, severity=%q", severity)
	}
}

type fakeRuntimePackageManager struct {
	uid string
}

func (f *fakeRuntimePackageManager) QueryApplication(ctx context.Context, _ *sys.QueryApplicationRequest, _ ...grpc.CallOption) (*sys.QueryApplicationResponse, error) {
	if outgoing, ok := metadata.FromOutgoingContext(ctx); ok {
		if values := outgoing.Get("x-hc-user-id"); len(values) > 0 {
			f.uid = values[0]
		}
	}
	running, paused := sys.InstanceStatus_Status_Running, sys.InstanceStatus_Status_Paused
	v1, v2, title1, title2 := "1.6.0", "2.0.0", "猫眼", "文件服务"
	return &sys.QueryApplicationResponse{InfoList: []*sys.AppInfo{
		{Appid: "community.lazycat.app.maoyan", DeployId: "maoyan6", Version: &v1, Title: &title1, Status: sys.AppStatus_Installed, InstanceStatus: &running},
		{Appid: "cloud.lazycat.app.files", DeployId: "files6", Version: &v2, Title: &title2, Status: sys.AppStatus_Installed, InstanceStatus: &paused},
	}}, nil
}

func TestApplicationsUsePackageManagerAndPersistSnapshot(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	deviceID, err := st.EnsureLocalDevice(context.Background(), "node", "node", "linux/amd64", "1.6.0", []string{"collector.embedded"})
	if err != nil {
		t.Fatal(err)
	}
	ca, err := pki.LoadOrCreate(filepath.Join(root, "pki"))
	if err != nil {
		t.Fatal(err)
	}
	client := &fakeRuntimePackageManager{}
	source := runtimeapps.NewWithClient(client, time.Minute)
	server := New(st, ca, "../../web", time.Minute)
	server.ConfigureRuntimeApps(source, deviceID)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/applications", nil)
	request.Header.Set("X-Hc-User-Id", "user-1")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Count int `json:"count"`
		Items []struct {
			ID           string         `json:"id"`
			Healthy      int            `json:"healthy"`
			Paused       int            `json:"paused"`
			StatusCounts map[string]int `json:"statusCounts"`
		} `json:"items"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if client.uid != "user-1" || response.Count != 2 {
		t.Fatalf("uid=%q response=%+v", client.uid, response)
	}
	states, err := st.ListRuntimeApplications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("persisted states=%+v", states)
	}
	capabilities, err := st.ListCapabilityStatuses(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, capability := range capabilities {
		found = found || capability.Capability == "lpk.runtime" && capability.Status == "available"
	}
	if !found {
		t.Fatalf("runtime capability not available: %+v", capabilities)
	}
}
