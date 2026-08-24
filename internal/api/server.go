package api

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/backup"
	"github.com/wtj-0527/lazycat-maoyan/internal/buildinfo"
	"github.com/wtj-0527/lazycat-maoyan/internal/pki"
	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
	"github.com/wtj-0527/lazycat-maoyan/internal/runtimeapps"
	"github.com/wtj-0527/lazycat-maoyan/internal/stability"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

type Server struct {
	store         *store.Store
	ca            *pki.Authority
	webDir        string
	pairingTTL    time.Duration
	mux           *http.ServeMux
	backup        *backup.Manager
	stability     *stability.Monitor
	restart       func()
	runtimeApps   *runtimeapps.Source
	localDeviceID string
}

func New(st *store.Store, ca *pki.Authority, webDir string, pairingTTL time.Duration) *Server {
	s := &Server{store: st, ca: ca, webDir: webDir, pairingTTL: pairingTTL, mux: http.NewServeMux()}
	s.routes()
	return s
}
func (s *Server) ConfigureOperations(manager *backup.Manager, monitor *stability.Monitor, restart func()) {
	s.backup, s.stability, s.restart = manager, monitor, restart
}
func (s *Server) ConfigureRuntimeApps(source *runtimeapps.Source, localDeviceID string) {
	s.runtimeApps, s.localDeviceID = source, localDeviceID
}
func (s *Server) Handler() http.Handler { return securityHeaders(s.mux) }
func (s *Server) CollectorHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", s.health)
	mux.HandleFunc("POST /api/v1/metrics/batch", s.ingestMetricsMTLS)
	mux.HandleFunc("POST /api/v1/certificate/rotate", s.rotateCertificateMTLS)
	return securityHeaders(mux)
}
func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.health)
	s.mux.HandleFunc("POST /api/v1/pairing-codes", s.createPairingCode)
	s.mux.HandleFunc("POST /api/v1/collectors/pair", s.pairCollector)
	s.mux.HandleFunc("GET /api/v1/overview", s.overview)
	s.mux.HandleFunc("GET /api/v1/devices", s.listDevices)
	s.mux.HandleFunc("GET /api/v1/devices/{id}", s.deviceDetail)
	s.mux.HandleFunc("PUT /api/v1/devices/{id}/metadata", s.updateDeviceMetadata)
	s.mux.HandleFunc("GET /api/v1/devices/{id}/metrics", s.metricHistory)
	s.mux.HandleFunc("GET /api/v1/devices/{id}/events", s.deviceEvents)
	s.mux.HandleFunc("GET /api/v1/applications", s.applications)
	s.mux.HandleFunc("GET /api/v1/applications/{id}/metrics", s.applicationMetrics)
	s.mux.HandleFunc("GET /api/v1/storage", s.storageView)
	s.mux.HandleFunc("GET /api/v1/alerts", s.alertsView)
	s.mux.HandleFunc("POST /api/v1/alerts/{fingerprint}/acknowledge", s.alertAction)
	s.mux.HandleFunc("POST /api/v1/alerts/{fingerprint}/silence", s.alertAction)
	s.mux.HandleFunc("POST /api/v1/alerts/{fingerprint}/resolve", s.alertAction)
	s.mux.HandleFunc("POST /api/v1/alerts/bulk-acknowledge", s.bulkAcknowledgeAlerts)
	s.mux.HandleFunc("GET /api/v1/inspections", s.listInspections)
	s.mux.HandleFunc("POST /api/v1/inspections", s.startInspection)
	s.mux.HandleFunc("GET /api/v1/inspections/{id}", s.inspectionDetail)
	s.mux.HandleFunc("GET /api/v1/inspections/{id}/export", s.exportInspection)
	s.mux.HandleFunc("GET /api/v1/inspections/live", s.inspectionView)
	s.mux.HandleFunc("GET /api/v1/settings", s.settingsView)
	s.mux.HandleFunc("PUT /api/v1/settings", s.settingsView)
	s.mux.HandleFunc("GET /api/v1/operations", s.operationsView)
	s.mux.HandleFunc("GET /api/v1/version", s.versionView)
	s.mux.HandleFunc("GET /api/v1/backups", s.listBackups)
	s.mux.HandleFunc("POST /api/v1/backups", s.createBackup)
	s.mux.HandleFunc("POST /api/v1/backups/{name}/restore", s.restoreBackup)
	s.mux.HandleFunc("GET /api/v1/database/status", s.databaseStatus)
	s.mux.HandleFunc("GET /api/v1/stability", s.stabilityStatus)
	s.mux.HandleFunc("POST /api/v1/stability/reset", s.resetStability)
	s.mux.HandleFunc("POST /api/v1/devices/{id}/revoke", s.revokeDevice)
	s.mux.HandleFunc("GET /api/v1/saved-views", s.savedViews)
	s.mux.HandleFunc("POST /api/v1/saved-views", s.savedViews)
	s.mux.HandleFunc("DELETE /api/v1/saved-views/{id}", s.deleteSavedView)
	s.mux.HandleFunc("GET /api/v1/alert-rules", s.alertRules)
	s.mux.HandleFunc("PUT /api/v1/alert-rules", s.alertRules)
	s.mux.HandleFunc("GET /api/v1/maintenance-windows", s.maintenanceWindows)
	s.mux.HandleFunc("POST /api/v1/maintenance-windows", s.maintenanceWindows)
	s.mux.HandleFunc("DELETE /api/v1/maintenance-windows/{id}", s.deleteMaintenanceWindow)
	s.mux.HandleFunc("GET /api/v1/audit", s.auditView)
	s.mux.HandleFunc("POST /api/v1/notifications/test", s.testNotification)
	s.mux.HandleFunc("/", s.static)
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "appVersion": buildinfo.Version, "protocolVersion": protocol.Version, "time": time.Now().UTC()})
}
func (s *Server) createPairingCode(w http.ResponseWriter, r *http.Request) {
	code, expires, err := s.store.CreatePairingCode(r.Context(), s.pairingTTL)
	if err != nil {
		problem(w, 500, "internal_error", "无法生成配对码")
		return
	}
	writeJSON(w, 201, protocol.CreatePairingCodeResponse{Code: code, ExpiresAt: expires})
}
func (s *Server) pairCollector(w http.ResponseWriter, r *http.Request) {
	var req protocol.PairCollectorRequest
	if err := decodeJSON(r, &req); err != nil || req.Code == "" || req.Hostname == "" || req.CollectorVer == "" {
		problem(w, 400, "invalid_request", "code、hostname 和 collectorVersion 必填")
		return
	}
	res, err := s.store.PairCollector(r.Context(), req)
	if errors.Is(err, store.ErrInvalidPairingCode) {
		problem(w, 401, "invalid_pairing_code", "配对码无效、已过期或已使用")
		return
	}
	if err != nil {
		problem(w, 409, "pairing_failed", err.Error())
		return
	}
	identity, err := s.ca.IssueClient(res.DeviceID, req.Hostname, 365*24*time.Hour)
	if err != nil {
		problem(w, 500, "certificate_issue_failed", "无法签发 Collector 证书")
		return
	}
	if err := s.store.SetCertificate(r.Context(), res.DeviceID, identity.Serial, identity.ExpiresAt); err != nil {
		problem(w, 500, "certificate_store_failed", "无法保存 Collector 证书状态")
		return
	}
	res.CertificatePEM = identity.CertificatePEM
	res.PrivateKeyPEM = identity.PrivateKeyPEM
	res.CACertificatePEM = identity.CACertificatePEM
	res.CertificateSerial = identity.Serial
	res.CertificateExpiresAt = identity.ExpiresAt
	writeJSON(w, 201, res)
}
func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := s.store.ListDevices(r.Context())
	if err != nil {
		problem(w, 500, "internal_error", "无法读取设备")
		return
	}
	metadata, _ := s.store.DeviceMetadataMap(r.Context())
	attachMetadata(devices, metadata)
	writeJSON(w, 200, map[string]any{"items": devices, "count": len(devices)})
}
func (s *Server) revokeDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("id")
	if deviceID == "" {
		problem(w, 400, "invalid_device", "设备 ID 必填")
		return
	}
	if err := s.store.RevokeDevice(r.Context(), deviceID); err != nil {
		problem(w, 404, "device_not_found", "设备不存在或已吊销")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "deviceId": deviceID})
}
func (s *Server) ingestMetrics(w http.ResponseWriter, r *http.Request) {
	var batch protocol.MetricBatch
	if err := decodeJSON(r, &batch); err != nil || !validBatch(batch) {
		problem(w, 400, "invalid_batch", "deviceId 和 1–1000 个指标点必填")
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" || token == r.Header.Get("Authorization") {
		problem(w, 401, "unauthorized", "缺少设备凭据")
		return
	}
	if err := s.store.AuthenticateDevice(r.Context(), batch.DeviceID, token); err != nil {
		problem(w, 401, "unauthorized", "设备凭据无效")
		return
	}
	if err := s.store.IngestMetrics(r.Context(), batch); err != nil {
		problem(w, 500, "ingest_failed", "指标写入失败")
		return
	}
	_ = s.SyncAlerts(r.Context())
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": len(batch.Points)})
}

func (s *Server) ingestMetricsMTLS(w http.ResponseWriter, r *http.Request) {
	var batch protocol.MetricBatch
	if err := decodeJSON(r, &batch); err != nil || !validBatch(batch) {
		problem(w, 400, "invalid_batch", "指标批次无效")
		return
	}
	cert := peerCertificate(r)
	if cert == nil || cert.Subject.CommonName != batch.DeviceID {
		problem(w, 401, "invalid_client_certificate", "Collector 证书与设备不匹配")
		return
	}
	if err := s.store.CertificateAllowed(r.Context(), batch.DeviceID, cert.SerialNumber.Text(16)); err != nil {
		problem(w, 401, "revoked_client_certificate", "Collector 证书未知或已吊销")
		return
	}
	if err := s.store.IngestMetrics(r.Context(), batch); err != nil {
		problem(w, 500, "ingest_failed", "指标写入失败")
		return
	}
	_ = s.SyncAlerts(r.Context())
	writeJSON(w, http.StatusAccepted, map[string]any{"accepted": len(batch.Points)})
}

func (s *Server) rotateCertificateMTLS(w http.ResponseWriter, r *http.Request) {
	cert := peerCertificate(r)
	if cert == nil || cert.Subject.CommonName == "" {
		problem(w, 401, "invalid_client_certificate", "缺少 Collector 证书")
		return
	}
	deviceID, oldSerial := cert.Subject.CommonName, cert.SerialNumber.Text(16)
	if err := s.store.CertificateAllowed(r.Context(), deviceID, oldSerial); err != nil {
		problem(w, 401, "revoked_client_certificate", "Collector 证书未知或已吊销")
		return
	}
	identity, err := s.ca.IssueClient(deviceID, deviceID, 365*24*time.Hour)
	if err != nil {
		problem(w, 500, "certificate_issue_failed", "无法签发新证书")
		return
	}
	if err := s.store.RotateCertificate(r.Context(), deviceID, oldSerial, identity.Serial, identity.ExpiresAt, 24*time.Hour); err != nil {
		problem(w, 500, "certificate_rotate_failed", "无法轮换证书")
		return
	}
	writeJSON(w, http.StatusOK, protocol.PairCollectorResponse{
		DeviceID:             deviceID,
		CertificatePEM:       identity.CertificatePEM,
		PrivateKeyPEM:        identity.PrivateKeyPEM,
		CACertificatePEM:     identity.CACertificatePEM,
		CertificateSerial:    identity.Serial,
		CertificateExpiresAt: identity.ExpiresAt,
	})
}

func validBatch(batch protocol.MetricBatch) bool {
	if batch.DeviceID == "" || len(batch.Points) == 0 || len(batch.Points) > 1000 {
		return false
	}
	now := time.Now().UTC()
	for _, point := range batch.Points {
		if point.Name == "" || len(point.Name) > 128 || math.IsNaN(point.Value) || math.IsInf(point.Value, 0) || point.CollectedAt.IsZero() || point.CollectedAt.Before(now.Add(-31*24*time.Hour)) || point.CollectedAt.After(now.Add(5*time.Minute)) {
			return false
		}
	}
	return true
}

func peerCertificate(r *http.Request) *x509.Certificate {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return nil
	}
	return r.TLS.PeerCertificates[0]
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		problem(w, 404, "not_found", "接口不存在")
		return
	}
	path := filepath.Join(s.webDir, filepath.Clean(r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFile(w, r, path)
		return
	}
	// The SPA shell references content-hashed assets. Never let the gateway or
	// browser pin an old index across an LPK upgrade.
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	http.ServeFile(w, r, filepath.Join(s.webDir, "index.html"))
}
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	d := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
