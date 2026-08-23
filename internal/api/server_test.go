package api

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/pki"
	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

func TestPairingCodeIsSingleUse(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ca, err := pki.LoadOrCreate(filepath.Join(t.TempDir(), "pki"))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(st, ca, "../../web", 10*time.Minute).Handler())
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
	if paired.CertificatePEM == "" || paired.PrivateKeyPEM == "" || paired.CACertificatePEM == "" {
		t.Fatal("pairing response did not include mTLS identity")
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
	block, _ := pem.Decode([]byte(paired.CertificatePEM))
	if block == nil {
		t.Fatal("invalid client certificate PEM")
	}
	clientCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	mtlsRequest := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/batch", bytes.NewReader(metricBody))
	mtlsRequest.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{clientCert}}
	mtlsRecorder := httptest.NewRecorder()
	New(st, ca, "../../web", 10*time.Minute).CollectorHandler().ServeHTTP(mtlsRecorder, mtlsRequest)
	if mtlsRecorder.Code != http.StatusAccepted {
		t.Fatalf("mTLS metric status=%d body=%s", mtlsRecorder.Code, mtlsRecorder.Body.String())
	}
	overviewResponse, err := http.Get(ts.URL + "/api/v1/overview")
	if err != nil {
		t.Fatal(err)
	}
	var overview struct {
		Stats   map[string]int `json:"stats"`
		Devices []deviceView   `json:"devices"`
	}
	if err := json.NewDecoder(overviewResponse.Body).Decode(&overview); err != nil {
		t.Fatal(err)
	}
	overviewResponse.Body.Close()
	if overview.Stats["devices"] != 1 || len(overview.Devices) != 1 {
		t.Fatalf("overview=%+v", overview)
	}

	rotateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/certificate/rotate", nil)
	rotateRequest.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{clientCert}}
	rotateRecorder := httptest.NewRecorder()
	New(st, ca, "../../web", 10*time.Minute).CollectorHandler().ServeHTTP(rotateRecorder, rotateRequest)
	if rotateRecorder.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", rotateRecorder.Code, rotateRecorder.Body.String())
	}
	var rotated protocol.PairCollectorResponse
	if err := json.NewDecoder(rotateRecorder.Body).Decode(&rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.CertificateSerial == paired.CertificateSerial {
		t.Fatal("certificate serial was not rotated")
	}

	revokeRequest := httptest.NewRequest(http.MethodPost, "/api/v1/devices/"+paired.DeviceID+"/revoke", nil)
	revokeRecorder := httptest.NewRecorder()
	New(st, ca, "../../web", 10*time.Minute).Handler().ServeHTTP(revokeRecorder, revokeRequest)
	if revokeRecorder.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", revokeRecorder.Code, revokeRecorder.Body.String())
	}
	deniedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/metrics/batch", bytes.NewReader(metricBody))
	deniedRequest.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{clientCert}}
	deniedRecorder := httptest.NewRecorder()
	New(st, ca, "../../web", 10*time.Minute).CollectorHandler().ServeHTTP(deniedRecorder, deniedRequest)
	if deniedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("revoked certificate status=%d", deniedRecorder.Code)
	}
}
