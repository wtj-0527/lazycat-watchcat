package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

type alertRule struct {
	Metric   string  `json:"metric"`
	Label    string  `json:"label"`
	Warning  float64 `json:"warning"`
	Critical float64 `json:"critical"`
	Enabled  bool    `json:"enabled"`
}

func defaultAlertRules() []alertRule {
	return []alertRule{
		{Metric: "system.cpu.usage", Label: "CPU 使用率", Warning: 85, Critical: 95, Enabled: true},
		{Metric: "system.memory.usage", Label: "内存使用率", Warning: 90, Critical: 95, Enabled: true},
		{Metric: "filesystem.root.usage", Label: "文件系统使用率", Warning: 85, Critical: 95, Enabled: true},
		{Metric: "filesystem.volume.usage", Label: "存储分区使用率", Warning: 85, Critical: 95, Enabled: true},
		{Metric: "btrfs.usage", Label: "Btrfs 使用率", Warning: 85, Critical: 95, Enabled: true},
		{Metric: "disk.temperature", Label: "磁盘温度", Warning: 70, Critical: 80, Enabled: true},
		{Metric: "disk.io.busy_percent", Label: "磁盘繁忙度", Warning: 80, Critical: 95, Enabled: true},
		{Metric: "container.memory.usage_percent", Label: "容器内存使用率", Warning: 90, Critical: 95, Enabled: true},
	}
}

func (s *Server) currentAlertRules(r *http.Request) []alertRule {
	return s.loadAlertRules(r.Context())
}

func (s *Server) loadAlertRules(ctx context.Context) []alertRule {
	defaults := defaultAlertRules()
	rules := append([]alertRule(nil), defaults...)
	if ok, _ := s.store.GetSystemState(ctx, "alert.rules", &rules); !ok {
		_ = s.store.SetSystemState(ctx, "alert.rules", defaults)
		return defaults
	}
	existing := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		existing[rule.Metric] = struct{}{}
	}
	changed := false
	for _, rule := range defaults {
		if _, ok := existing[rule.Metric]; !ok {
			rules = append(rules, rule)
			changed = true
		}
	}
	if changed {
		_ = s.store.SetSystemState(ctx, "alert.rules", rules)
	}
	return rules
}

func (s *Server) updateDeviceMetadata(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Group    string            `json:"group"`
		Location string            `json:"location"`
		Labels   map[string]string `json:"labels"`
	}
	if decodeJSON(r, &req) != nil {
		problem(w, 400, "invalid_request", "设备元数据无效")
		return
	}
	item := store.DeviceMetadata{DeviceID: r.PathValue("id"), Group: req.Group, Location: req.Location, Labels: req.Labels}
	if err := s.store.SetDeviceMetadata(r.Context(), item); err != nil {
		problem(w, 404, "device_not_found", "设备不存在")
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) deviceEvents(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListDeviceEvents(r.Context(), r.PathValue("id"), 100)
	if err != nil {
		problem(w, 500, "device_events_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) savedViews(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		items, err := s.store.ListSavedViews(r.Context())
		if err != nil {
			problem(w, 500, "saved_views_failed", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	var req struct {
		ID    string         `json:"id"`
		Name  string         `json:"name"`
		Query map[string]any `json:"query"`
	}
	if decodeJSON(r, &req) != nil {
		problem(w, 400, "invalid_request", "保存视图参数无效")
		return
	}
	item, err := s.store.SaveView(r.Context(), req.ID, req.Name, req.Query)
	if err != nil {
		problem(w, 400, "save_view_failed", err.Error())
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) deleteSavedView(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteSavedView(r.Context(), r.PathValue("id")); err != nil {
		problem(w, 404, "saved_view_not_found", "保存视图不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) alertRules(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"items": s.currentAlertRules(r)})
		return
	}
	var req struct {
		Items []alertRule `json:"items"`
	}
	if decodeJSON(r, &req) != nil || len(req.Items) == 0 {
		problem(w, 400, "invalid_rules", "告警规则不能为空")
		return
	}
	for _, item := range req.Items {
		if item.Metric == "" || item.Warning < 0 || item.Critical <= item.Warning {
			problem(w, 400, "invalid_rules", "规则阈值必须满足 Critical > Warning")
			return
		}
	}
	if err := s.store.SetSystemState(r.Context(), "alert.rules", req.Items); err != nil {
		problem(w, 500, "rules_update_failed", err.Error())
		return
	}
	_ = s.store.RecordAudit(r.Context(), "alert_rules.updated", "settings", "alert.rules", map[string]any{"count": len(req.Items)})
	_ = s.SyncAlerts(r.Context())
	writeJSON(w, 200, map[string]any{"items": req.Items})
}

func (s *Server) maintenanceWindows(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		items, err := s.store.ListMaintenanceWindows(r.Context())
		if err != nil {
			problem(w, 500, "maintenance_failed", err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"items": items})
		return
	}
	var item store.MaintenanceWindow
	if decodeJSON(r, &item) != nil {
		problem(w, 400, "invalid_request", "维护窗口参数无效")
		return
	}
	created, err := s.store.SaveMaintenanceWindow(r.Context(), item)
	if err != nil {
		problem(w, 400, "maintenance_failed", err.Error())
		return
	}
	writeJSON(w, 201, created)
}

func (s *Server) deleteMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteMaintenanceWindow(r.Context(), id); err != nil {
		problem(w, 404, "maintenance_not_found", "维护窗口不存在")
		return
	}
	_ = s.store.RecordAudit(r.Context(), "maintenance_window.deleted", "maintenance_window", id, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) auditView(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.store.ListAudit(r.Context(), limit)
	if err != nil {
		problem(w, 500, "audit_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	key := fmt.Sprintf("test:%d", time.Now().UTC().UnixNano())
	if err := s.store.QueueNotification(r.Context(), key, "WatchCat 测试通知", "LazyCat 系统通知渠道工作正常", "lzc://community.lazycat.app.watchcat/settings"); err != nil {
		problem(w, 500, "notification_test_failed", err.Error())
		return
	}
	_ = s.store.RecordAudit(r.Context(), "notification.test_queued", "notification", key, nil)
	writeJSON(w, 202, map[string]any{"status": "queued"})
}

func (s *Server) bulkAcknowledgeAlerts(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Fingerprints []string `json:"fingerprints"`
	}
	if decodeJSON(r, &req) != nil || len(req.Fingerprints) == 0 || len(req.Fingerprints) > 200 {
		problem(w, 400, "invalid_request", "请选择 1–200 个告警")
		return
	}
	updated := 0
	for _, fingerprint := range req.Fingerprints {
		if s.store.SetAlertState(r.Context(), fingerprint, "acknowledge", 0) == nil {
			updated++
		}
	}
	writeJSON(w, 200, map[string]any{"updated": updated})
}

func (s *Server) exportInspection(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.InspectionByID(r.Context(), r.PathValue("id"))
	if err != nil {
		problem(w, 404, "inspection_not_found", "巡检记录不存在")
		return
	}
	format := r.URL.Query().Get("format")
	if format == "pdf" {
		body := fmt.Sprintf("WatchCat Inspection %s\\nStatus: %s\\nStarted: %s\\nDevices: %d\\nHealthy: %d Warning: %d Critical: %d\\nEvidence SHA-256: %s",
			item.ID, item.Status, item.StartedAt.Format(time.RFC3339), item.DeviceCount, item.HealthyCount, item.WarningCount, item.CriticalCount, item.EvidenceSHA256)
		pdf := minimalPDF(body)
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="inspection-`+item.ID+`.pdf"`)
		w.Write(pdf)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="inspection-`+item.ID+`.json"`)
	_ = json.NewEncoder(w).Encode(item)
}

func minimalPDF(text string) []byte {
	text = strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)", "\n", ") Tj 0 -14 Td (").Replace(text)
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
		"",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	stream := "BT /F1 10 Tf 50 742 Td (" + text + ") Tj ET"
	objects[3] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream)
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&out, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&out, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}
