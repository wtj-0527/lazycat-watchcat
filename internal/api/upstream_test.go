package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/collector"
	"github.com/wtj-0527/lazycat-maoyan/internal/pki"
	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

func TestUpstreamJoinAndBearerMetricForwarding(t *testing.T) {
	remoteStore, err := store.Open(filepath.Join(t.TempDir(), "remote.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer remoteStore.Close()
	remoteCA, err := pki.LoadOrCreate(filepath.Join(t.TempDir(), "remote-pki"))
	if err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(New(remoteStore, remoteCA, "../../web", 10*time.Minute).Handler())
	defer remote.Close()

	codeResponse, err := http.Post(remote.URL+"/api/v1/pairing-codes", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	var pairing protocol.CreatePairingCodeResponse
	if err := json.NewDecoder(codeResponse.Body).Decode(&pairing); err != nil {
		t.Fatal(err)
	}
	codeResponse.Body.Close()
	invitation, _ := url.Parse(remote.URL)
	invitation.Fragment = url.Values{"pairing-code": {pairing.Code}}.Encode()

	localStore, err := store.Open(filepath.Join(t.TempDir(), "local.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	localCA, err := pki.LoadOrCreate(filepath.Join(t.TempDir(), "local-pki"))
	if err != nil {
		t.Fatal(err)
	}
	upstream := collector.NewUpstream(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	localServer := New(localStore, localCA, "../../web", 10*time.Minute)
	localServer.ConfigureUpstream(upstream)
	local := httptest.NewServer(localServer.Handler())
	defer local.Close()

	body, _ := json.Marshal(map[string]string{"invitation": invitation.String()})
	response, err := http.Post(local.URL+"/api/v1/upstream/join", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("join status=%d body=%s", response.StatusCode, readBody(response))
	}
	response.Body.Close()

	upstream.Send(context.Background(), protocol.MetricBatch{Points: []protocol.MetricPoint{{
		Name: "system.cpu.usage", Value: 21.5, Unit: "%", CollectedAt: time.Now().UTC(),
	}}})
	devicesResponse, err := http.Get(remote.URL + "/api/v1/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer devicesResponse.Body.Close()
	var devices struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(devicesResponse.Body).Decode(&devices); err != nil {
		t.Fatal(err)
	}
	if devices.Count != 1 {
		t.Fatalf("remote device count=%d", devices.Count)
	}
	if status := upstream.Status(); !status.Paired || status.LastSuccessAt.IsZero() || status.LastError != "" {
		t.Fatalf("unexpected upstream status: %+v", status)
	}
}

func readBody(response *http.Response) string {
	data, _ := io.ReadAll(response.Body)
	return string(data)
}
