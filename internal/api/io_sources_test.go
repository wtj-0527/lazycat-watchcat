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
		Points: []protocol.MetricPoint{
			{
				Name: "container.running", Value: 1, Unit: "bool", CollectedAt: now,
				Labels: map[string]string{
					"app": "community.lazycat.app.hermes-studio", "deployId": "community.lazycat.app.hermes-studio2",
					"userId": "u1", "container": "abcdef123456", "name": "hermes-studio-web",
				},
			},
			{
				Name: "container.running", Value: 1, Unit: "bool", CollectedAt: now,
				Labels: map[string]string{
					"app": "community.lazycat.app.hermes-studio", "container": "999999999999", "name": "legacy-unscoped",
				},
			},
		},
		ProcessesCollected: true,
		Processes: []protocol.ProcessSample{
			{PID: 42, StartTime: "100", Name: "node", User: "root", Cgroup: "0::/docker/abcdef1234567890", ReadRate: 2048, WriteRate: 4096, CollectedAt: now},
			{PID: 43, StartTime: "101", Name: "legacy", User: "root", Cgroup: "0::/docker/999999999999abcd", ReadRate: 512, CollectedAt: now},
			{PID: 7, StartTime: "50", Name: "dockerd", User: "root", Cgroup: "0::/system.slice/docker.service", WriteRate: 8192, CollectedAt: now},
			{PID: 8, StartTime: "51", Name: "idle", User: "root", CollectedAt: now},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceRuntimeApplications(ctx, deviceID, []store.RuntimeApplication{
		{
			DeviceID: deviceID, DeployID: "community.lazycat.app.hermes-studio2",
			AppID: "community.lazycat.app.hermes-studio", Title: "Hermes Studio",
			UserID: "u1", UserName: "Mandy", InstanceStatus: "running",
		},
		{
			DeviceID: deviceID, DeployID: "community.lazycat.app.hermes-studio3",
			AppID: "community.lazycat.app.hermes-studio", Title: "Hermes Studio",
			UserID: "u3", UserName: "Bob", InstanceStatus: "paused",
		},
		{
			DeviceID: deviceID, DeployID: "cloud.lazycat.app.photo2",
			AppID: "cloud.lazycat.app.photo", Title: "Photos",
			UserID: "u2", UserName: "Alice", InstanceStatus: "paused",
		},
	}); err != nil {
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
	if len(response.Processes) != 3 || response.Processes[0].Name != "dockerd" {
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
	if len(response.Applications) != 3 || response.Applications[0].AppTitle != "Hermes Studio" ||
		response.Applications[0].ReadRate != 2048 || response.Applications[0].WriteRate != 4096 ||
		response.Applications[0].ProcessCount != 1 || response.Applications[0].ActiveProcessCount != 1 ||
		len(response.Applications[0].Processes) != 1 {
		t.Fatalf("applications=%+v", response.Applications)
	}
	var pausedHermes, pausedPhotos *ioApplicationSource
	for index := range response.Applications {
		item := &response.Applications[index]
		if item.DeployID == "community.lazycat.app.hermes-studio3" {
			pausedHermes = item
		}
		if item.AppID == "cloud.lazycat.app.photo" {
			pausedPhotos = item
		}
	}
	if pausedHermes == nil || pausedPhotos == nil || pausedPhotos.AppTitle != "懒猫相册" ||
		pausedPhotos.InstanceStatus != "paused" || pausedPhotos.ReadRate != 0 || pausedPhotos.WriteRate != 0 {
		t.Fatalf("idle applications missing=%+v", response.Applications)
	}
}
