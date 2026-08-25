package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/pki"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

func TestPlatformConfigurationAndExports(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	deviceID, err := st.EnsureLocalDevice(context.Background(), "nas", "nas", "linux", "test", []string{"collector.embedded"})
	if err != nil {
		t.Fatal(err)
	}
	ca, err := pki.LoadOrCreate(filepath.Join(root, "pki"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(st, ca, "../../web", time.Minute).Handler())
	defer server.Close()

	requestJSON := func(method, path string, body any) (*http.Response, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			reader = bytes.NewReader(raw)
		}
		request, requestErr := http.NewRequest(method, server.URL+path, reader)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		request.Header.Set("Content-Type", "application/json")
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		return response, raw
	}

	response, _ := requestJSON(http.MethodPut, "/api/v1/devices/"+deviceID+"/metadata", map[string]any{
		"group": "生产", "location": "机柜 A", "labels": map[string]string{"role": "storage"},
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("metadata status=%d", response.StatusCode)
	}
	response, raw := requestJSON(http.MethodGet, "/api/v1/overview", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"group":"生产"`) {
		t.Fatalf("overview metadata status=%d body=%s", response.StatusCode, raw)
	}

	response, raw = requestJSON(http.MethodPost, "/api/v1/saved-views", map[string]any{
		"name": "生产设备", "query": map[string]string{"group": "生产"},
	})
	if response.StatusCode != http.StatusCreated || !strings.Contains(string(raw), "生产设备") {
		t.Fatalf("saved view status=%d body=%s", response.StatusCode, raw)
	}

	rules := []map[string]any{{"metric": "system.cpu.usage", "label": "CPU", "warning": 70, "critical": 90, "enabled": true}}
	response, raw = requestJSON(http.MethodPut, "/api/v1/alert-rules", map[string]any{"items": rules})
	if response.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"warning":70`) {
		t.Fatalf("rules status=%d body=%s", response.StatusCode, raw)
	}

	response, raw = requestJSON(http.MethodPut, "/api/v1/settings", map[string]any{
		"rawRetentionDays": 14, "rollupRetentionDays": 180, "auditRetentionDays": 90,
		"inspectionRetentionDays": 180, "dailyInspectionHour": 2, "weeklyInspectionHour": 5,
	})
	if response.StatusCode != http.StatusOK || !strings.Contains(string(raw), `"dailyInspectionHour":2`) {
		t.Fatalf("settings status=%d body=%s", response.StatusCode, raw)
	}
	response, raw = requestJSON(http.MethodGet, "/api/v1/settings", nil)
	if response.StatusCode != http.StatusOK || strings.Contains(string(raw), `"storageStats"`) {
		t.Fatalf("settings GET must not run synchronous raw-metric counts: status=%d body=%s", response.StatusCode, raw)
	}

	now := time.Now().UTC()
	response, raw = requestJSON(http.MethodPost, "/api/v1/maintenance-windows", map[string]any{
		"name": "升级", "startsAt": now, "endsAt": now.Add(time.Hour), "enabled": true,
	})
	if response.StatusCode != http.StatusCreated || !strings.Contains(string(raw), "升级") {
		t.Fatalf("maintenance status=%d body=%s", response.StatusCode, raw)
	}

	response, raw = requestJSON(http.MethodGet, "/api/v1/devices/"+deviceID+"/events", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(raw), "device.metadata.updated") {
		t.Fatalf("events status=%d body=%s", response.StatusCode, raw)
	}
	response, raw = requestJSON(http.MethodGet, "/api/v1/audit?limit=20", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(raw), "settings.updated") {
		t.Fatalf("audit status=%d body=%s", response.StatusCode, raw)
	}

	inspection, err := st.SaveInspection(context.Background(), "manual", map[string]any{"schemaVersion": 2}, map[string]any{}, 1, 1, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	response, raw = requestJSON(http.MethodGet, "/api/v1/inspections/"+inspection.ID+"/export?format=pdf", nil)
	if response.StatusCode != http.StatusOK || !bytes.HasPrefix(raw, []byte("%PDF-1.4")) {
		t.Fatalf("pdf status=%d body=%q", response.StatusCode, raw)
	}
}
