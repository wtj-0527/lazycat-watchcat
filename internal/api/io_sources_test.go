package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

func TestDeviceIOSourcesMapsProcessesToApplicationsAndContainers(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "watchcat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	deviceID, err := st.EnsureLocalDevice(ctx, "canway", "canway", "linux/amd64", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.IngestMetrics(ctx, protocol.MetricBatch{
		DeviceID: deviceID,
		Points: []protocol.MetricPoint{{
			Name: "container.running", Value: 1, Unit: "bool", CollectedAt: now,
			Labels: map[string]string{
				"app": "community.lazycat.app.hermes-studio", "deployId": "community.lazycat.app.hermes-studio2",
				"userId": "u1", "container": "abcdef123456", "name": "hermes-studio-web",
			},
		}},
		ProcessesCollected: true,
		Processes: []protocol.ProcessSample{
			{PID: 42, StartTime: "100", Name: "node", User: "root", Cgroup: "0::/docker/abcdef1234567890", ReadRate: 2048, WriteRate: 4096, CollectedAt: now},
			{PID: 7, StartTime: "50", Name: "dockerd", User: "root", Cgroup: "0::/system.slice/docker.service", WriteRate: 8192, CollectedAt: now},
			{PID: 8, StartTime: "51", Name: "idle", User: "root", CollectedAt: now},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceRuntimeApplications(ctx, deviceID, []store.RuntimeApplication{{
		DeviceID: deviceID, DeployID: "community.lazycat.app.hermes-studio2",
		AppID: "community.lazycat.app.hermes-studio", Title: "Hermes Studio",
		UserID: "u1", UserName: "Mandy",
	}}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+deviceID+"/io-sources", nil)
	recorder := httptest.NewRecorder()
	New(st, nil, "../../web", time.Minute).Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Processes    []ioProcessSource     `json:"processes"`
		Applications []ioApplicationSource `json:"applications"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Processes) != 2 || response.Processes[0].Name != "dockerd" {
		t.Fatalf("processes=%+v", response.Processes)
	}
	var mapped ioProcessSource
	for _, process := range response.Processes {
		if process.PID == 42 {
			mapped = process
		}
	}
	if mapped.AppID != "community.lazycat.app.hermes-studio" || mapped.ContainerName != "hermes-studio-web" {
		t.Fatalf("mapped=%+v", mapped)
	}
	if len(response.Applications) != 1 || response.Applications[0].AppTitle != "Hermes Studio" ||
		response.Applications[0].ReadRate != 2048 || response.Applications[0].WriteRate != 4096 {
		t.Fatalf("applications=%+v", response.Applications)
	}
}
