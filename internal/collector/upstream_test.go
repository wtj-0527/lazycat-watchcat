package collector

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

func TestParseInvitation(t *testing.T) {
	hub, code, err := ParseInvitation("http://192.168.1.20:18080/#pairing-code=ABCD-1234")
	if err != nil {
		t.Fatal(err)
	}
	if hub != "http://192.168.1.20:18080" || code != "ABCD-1234" {
		t.Fatalf("hub=%q code=%q", hub, code)
	}
	if _, _, err := ParseInvitation("http://192.168.1.20:18080/"); err == nil {
		t.Fatal("expected missing pairing code error")
	}
}

func TestRejectedCredentialsClearPersistedUpstream(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/metrics/batch" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		http.Error(w, "removed", http.StatusUnauthorized)
	}))
	defer remote.Close()

	dataDir := t.TempDir()
	upstream := NewUpstream(dataDir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	upstream.config = upstreamConfig{HubURL: remote.URL}
	upstream.credentials = Credentials{DeviceID: "removed-device", Token: "expired-token"}
	if err := saveUpstreamConfig(upstream.configPath, upstream.config); err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(upstream.credentialsPath, upstream.credentials); err != nil {
		t.Fatal(err)
	}

	upstream.Send(context.Background(), protocol.MetricBatch{Points: []protocol.MetricPoint{{
		Name: "system.cpu.usage", Value: 1, Unit: "%", CollectedAt: time.Now().UTC(),
	}}})

	if status := upstream.Status(); status.Paired || status.HubURL != "" || status.DeviceID != "" {
		t.Fatalf("credentials were not cleared: %+v", status)
	}
	for _, path := range []string{
		filepath.Join(dataDir, "upstream", "config.json"),
		filepath.Join(dataDir, "upstream", "credentials.json"),
		filepath.Join(dataDir, "upstream", "metrics.queue.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("persisted upstream file remains: %s err=%v", path, err)
		}
	}
}
