package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

func TestPairingCodeIsSingleUse(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ts := httptest.NewServer(New(st, "../../web", 10*time.Minute).Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/api/v1/pairing-codes", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var code protocol.CreatePairingCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&code); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	body, _ := json.Marshal(protocol.PairCollectorRequest{Code: code.Code, Name: "猫盒-01", Hostname: "lc-01", CollectorVer: "1.0.0", Capabilities: []string{"host.metrics"}})
	resp, err = http.Post(ts.URL+"/api/v1/collectors/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("first pair status=%d", resp.StatusCode)
	}
	var paired protocol.PairCollectorResponse
	if err := json.NewDecoder(resp.Body).Decode(&paired); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	resp, err = http.Post(ts.URL+"/api/v1/collectors/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("second pair status=%d", resp.StatusCode)
	}
	resp.Body.Close()
	resp, err = http.Get(ts.URL + "/api/v1/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 {
		t.Fatalf("count=%d", list.Count)
	}

	metricBody, _ := json.Marshal(protocol.MetricBatch{DeviceID: paired.DeviceID, Points: []protocol.MetricPoint{{
		Name:        "system.cpu.usage",
		Value:       32.5,
		Unit:        "%",
		CollectedAt: time.Now().UTC(),
	}}})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/metrics/batch", bytes.NewReader(metricBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+paired.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("metric status=%d", resp.StatusCode)
	}
	resp.Body.Close()
}
