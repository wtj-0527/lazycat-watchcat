package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/collector"
	"github.com/wtj-0527/lazycat-watchcat/internal/pki"
	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

type fakeDockerMaintenance struct {
	preview collector.UnusedImageSummary
	result  collector.ImagePruneResult
	deleted collector.ImageDeleteResult
	err     error
}

func (f *fakeDockerMaintenance) UnusedImages(context.Context) (collector.UnusedImageSummary, error) {
	return f.preview, f.err
}

func (f *fakeDockerMaintenance) PruneUnusedImages(context.Context) (collector.ImagePruneResult, error) {
	return f.result, f.err
}

func (f *fakeDockerMaintenance) DeleteUnusedImage(_ context.Context, imageID string) (collector.ImageDeleteResult, error) {
	result := f.deleted
	result.ImageID = imageID
	return result, f.err
}

func (f *fakeDockerMaintenance) CollectStorageInventory(context.Context, time.Time) ([]protocol.MetricPoint, []string) {
	return nil, nil
}
func (f *fakeDockerMaintenance) CollectSMART(context.Context, time.Time) ([]protocol.MetricPoint, []string) {
	return nil, nil
}

func TestDockerImagePreviewAndPruneAreAudited(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ca, err := pki.LoadOrCreate(filepath.Join(root, "pki"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(st, ca, "../../web", time.Minute)
	s.localDeviceID = "local-device"
	s.docker = &fakeDockerMaintenance{
		preview: collector.UnusedImageSummary{
			Available: true, Count: 1, TotalSize: 2048,
			Items: []collector.UnusedImage{{ID: "sha256:unused", Tags: []string{"unused:old"}, Size: 2048}},
		},
		result:  collector.ImagePruneResult{ImagesDeleted: 1, ReferencesUntagged: 1, SpaceReclaimed: 1024},
		deleted: collector.ImageDeleteResult{ReferencesUntagged: 1, DeleteRecords: 2},
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/v1/docker/images/unused")
	if err != nil {
		t.Fatal(err)
	}
	var preview collector.UnusedImageSummary
	if err := json.NewDecoder(response.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || preview.Count != 1 || preview.Items[0].ID != "sha256:unused" {
		t.Fatalf("preview status=%d result=%+v", response.StatusCode, preview)
	}

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/docker/images/prune", nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var result collector.ImagePruneResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || result.ImagesDeleted != 1 || result.SpaceReclaimed != 1024 {
		t.Fatalf("prune status=%d result=%+v", response.StatusCode, result)
	}

	imageID := "sha256:" + strings.Repeat("a", 64)
	request, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/v1/docker/images/"+imageID, nil)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var deleted collector.ImageDeleteResult
	if err := json.NewDecoder(response.Body).Decode(&deleted); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || deleted.ImageID != imageID || deleted.DeleteRecords != 2 {
		t.Fatalf("delete status=%d result=%+v", response.StatusCode, deleted)
	}

	audit, err := st.ListAudit(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range audit {
		if item.Action == "docker.images.pruned" && item.SubjectID == "local-device" &&
			strings.Contains(string(item.Metadata), "spaceReclaimed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing prune audit entry: %+v", audit)
	}
}
