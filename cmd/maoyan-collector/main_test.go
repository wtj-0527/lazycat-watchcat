package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
)

func TestFrontendSetupPairsAndDoesNotPersistCode(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/collectors/pair" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		writeSetupJSON(w, http.StatusCreated, protocol.PairCollectorResponse{
			DeviceID: "remote-1", Token: "token", CertificatePEM: "cert", PrivateKeyPEM: "key",
			CACertificatePEM: "ca", CertificateExpiresAt: time.Now().Add(365 * 24 * time.Hour),
		})
	}))
	defer hub.Close()

	dir := t.TempDir()
	var status atomic.Value
	status.Store("unpaired")
	ready := make(chan pairedRuntime, 1)
	server := &setupServer{
		status: &status, configPath: filepath.Join(dir, "setup.json"), credsPath: filepath.Join(dir, "credentials.json"),
		ready: ready, logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
	body := `{"hubUrl":"` + hub.URL + `","pairingCode":"one-time-secret"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/setup", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	server.configure(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	select {
	case paired := <-ready:
		if paired.creds.DeviceID != "remote-1" || paired.config.DeviceName == "" {
			t.Fatalf("paired=%+v", paired)
		}
	default:
		t.Fatal("paired runtime was not delivered")
	}
	stored, err := os.ReadFile(server.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "one-time-secret") {
		t.Fatal("pairing code was persisted")
	}
	var config runtimeConfig
	if err := json.Unmarshal(stored, &config); err != nil || config.HubURL != hub.URL {
		t.Fatalf("config=%+v err=%v", config, err)
	}
	if info, err := os.Stat(server.credsPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials mode=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestRuntimeConfigValidationRequiresMTLSHTTPS(t *testing.T) {
	err := validateRuntimeConfig(runtimeConfig{HubURL: "http://hub:18080", CollectorURL: "http://hub:18443", DeviceName: "canway"}, "code")
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("err=%v", err)
	}
}

func TestDeriveCollectorURLUsesSameHostAndStandardForwardPort(t *testing.T) {
	got, err := deriveCollectorURL("http://192.168.124.27:18080")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://192.168.124.27:18443" {
		t.Fatalf("collector URL=%q", got)
	}
}
