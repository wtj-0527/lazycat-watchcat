package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

type deviceView struct {
	protocol.Device
	Online bool                            `json:"online"`
	Stale  bool                            `json:"stale"`
	Health string                          `json:"health"`
	Latest map[string][]store.LatestMetric `json:"latest"`
}
type alertView struct {
	Fingerprint string    `json:"fingerprint"`
	DeviceID    string    `json:"deviceId"`
	DeviceName  string    `json:"deviceName"`
	Severity    string    `json:"severity"`
	Resource    string    `json:"resource"`
	Message     string    `json:"message"`
	Value       float64   `json:"value"`
	Unit        string    `json:"unit"`
	CollectedAt time.Time `json:"collectedAt"`
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	devices, metrics, err := s.snapshot(r)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取实时状态")
		return
	}
	_ = s.store.ReconcileAlerts(r.Context(), alertSignals(deriveAlerts(devices)))
	alerts, _ := s.store.ListAlerts(r.Context(), false)
	stats := map[string]int{"devices": len(devices), "online": 0, "offline": 0, "critical": 0, "warning": 0, "healthy": 0}
	for _, d := range devices {
		if d.Online {
			stats["online"]++
		} else {
			stats["offline"]++
		}
		stats[d.Health]++
	}
	writeJSON(w, 200, map[string]any{"stats": stats, "devices": devices, "alerts": alerts, "updatedAt": latestTimestamp(metrics)})
}
func (s *Server) deviceDetail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	device, err := s.store.DeviceByID(r.Context(), id)
	if store.IsNotFound(err) {
		problem(w, 404, "device_not_found", "设备不存在")
		return
	}
	if err != nil {
		problem(w, 500, "internal_error", "无法读取设备")
		return
	}
	metrics, err := s.store.LatestMetricsForDevice(r.Context(), id)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取指标")
		return
	}
	view := buildDeviceViews([]protocol.Device{device}, metrics)[0]
	writeJSON(w, 200, view)
}
func (s *Server) metricHistory(w http.ResponseWriter, r *http.Request) {
	id, name := r.PathValue("id"), r.URL.Query().Get("name")
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 24*30 {
		hours = 24
	}
	if name == "" {
		problem(w, 400, "metric_required", "指标名称必填")
		return
	}
	samples, err := s.store.MetricHistory(r.Context(), id, name, time.Now().UTC().Add(-time.Duration(hours)*time.Hour), 2000)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取指标历史")
		return
	}
	writeJSON(w, 200, map[string]any{"deviceId": id, "name": name, "items": samples})
}
func (s *Server) applications(w http.ResponseWriter, r *http.Request) {
	devices, metrics, err := s.snapshot(r)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取应用状态")
		return
	}
	names := map[string]string{}
	for _, d := range devices {
		names[d.ID] = d.Name
	}
	type app struct {
		ID        string           `json:"id"`
		Versions  map[string]int   `json:"versions"`
		Instances int              `json:"instances"`
		Healthy   int              `json:"healthy"`
		Unhealthy int              `json:"unhealthy"`
		Devices   []map[string]any `json:"devices"`
	}
	apps := map[string]*app{}
	for _, m := range metrics {
		if m.Name != "lpk.application.healthy" {
			continue
		}
		id := m.Labels["app"]
		if id == "" {
			continue
		}
		a := apps[id]
		if a == nil {
			a = &app{ID: id, Versions: map[string]int{}}
			apps[id] = a
		}
		a.Instances++
		if m.Value >= 1 {
			a.Healthy++
		} else {
			a.Unhealthy++
		}
		a.Versions[m.Labels["version"]]++
		a.Devices = append(a.Devices, map[string]any{"deviceId": m.DeviceID, "deviceName": names[m.DeviceID], "healthy": m.Value >= 1, "status": m.Labels["status"], "version": m.Labels["version"], "collectedAt": m.CollectedAt})
	}
	out := make([]*app, 0, len(apps))
	for _, a := range apps {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, 200, map[string]any{"items": out, "count": len(out), "updatedAt": latestTimestamp(metrics)})
}
func (s *Server) storageView(w http.ResponseWriter, r *http.Request) {
	devices, metrics, err := s.snapshot(r)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取存储状态")
		return
	}
	names := map[string]string{}
	for _, d := range devices {
		names[d.ID] = d.Name
	}
	var items []map[string]any
	for _, m := range metrics {
		if !(strings.HasPrefix(m.Name, "filesystem.") || strings.HasPrefix(m.Name, "btrfs.") || strings.HasPrefix(m.Name, "disk.")) {
			continue
		}
		items = append(items, map[string]any{"deviceId": m.DeviceID, "deviceName": names[m.DeviceID], "name": m.Name, "value": m.Value, "unit": m.Unit, "labels": m.Labels, "collectedAt": m.CollectedAt})
	}
	writeJSON(w, 200, map[string]any{"items": items, "count": len(items), "updatedAt": latestTimestamp(metrics)})
}
func (s *Server) alertsView(w http.ResponseWriter, r *http.Request) {
	devices, _, err := s.snapshot(r)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取告警状态")
		return
	}
	if err := s.store.ReconcileAlerts(r.Context(), alertSignals(deriveAlerts(devices))); err != nil {
		problem(w, 500, "internal_error", "无法更新告警状态")
		return
	}
	alerts, err := s.store.ListAlerts(r.Context(), r.URL.Query().Get("includeResolved") == "true")
	if err != nil {
		problem(w, 500, "internal_error", "无法读取持久化告警")
		return
	}
	writeJSON(w, 200, map[string]any{"items": alerts, "count": len(alerts), "updatedAt": time.Now().UTC()})
}
func (s *Server) alertAction(w http.ResponseWriter, r *http.Request) {
	fingerprint := r.PathValue("fingerprint")
	action := strings.TrimPrefix(r.URL.Path[strings.LastIndex(r.URL.Path, "/"):], "/")
	var req struct {
		DurationMinutes int `json:"durationMinutes"`
	}
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			problem(w, 400, "invalid_request", "请求体无效")
			return
		}
	}
	if err := s.store.SetAlertState(r.Context(), fingerprint, action, time.Duration(req.DurationMinutes)*time.Minute); err != nil {
		if err == store.ErrAlertNotFound {
			problem(w, 404, "alert_not_found", "告警不存在或状态不可变更")
		} else {
			problem(w, 400, "alert_action_failed", err.Error())
		}
		return
	}
	writeJSON(w, 200, map[string]any{"fingerprint": fingerprint, "status": action})
}
func (s *Server) inspectionView(w http.ResponseWriter, r *http.Request) {
	devices, _, err := s.snapshot(r)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取巡检状态")
		return
	}
	checks := map[string]int{"devices": len(devices), "online": 0, "healthy": 0, "warning": 0, "critical": 0}
	for _, d := range devices {
		if d.Online {
			checks["online"]++
		}
		checks[d.Health]++
	}
	writeJSON(w, 200, map[string]any{"generatedAt": time.Now().UTC(), "source": "live-snapshot", "checks": checks, "devices": devices})
}
func (s *Server) startInspection(w http.ResponseWriter, r *http.Request) {
	item, err := s.RunInspection(r.Context(), "manual")
	if err != nil {
		problem(w, 500, "inspection_failed", err.Error())
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) RunInspection(ctx context.Context, trigger string) (store.Inspection, error) {
	devices, metrics, err := s.snapshotContext(ctx)
	if err != nil {
		return store.Inspection{}, fmt.Errorf("无法读取巡检数据")
	}
	derived := deriveAlerts(devices)
	if err := s.store.ReconcileAlerts(ctx, alertSignals(derived)); err != nil {
		return store.Inspection{}, fmt.Errorf("无法更新告警状态")
	}
	checks := map[string]int{"devices": len(devices), "online": 0, "healthy": 0, "warning": 0, "critical": 0}
	for _, d := range devices {
		if d.Online {
			checks["online"]++
		}
		checks[d.Health]++
	}
	report := map[string]any{
		"schemaVersion":  1,
		"generatedAt":    time.Now().UTC(),
		"source":         "collector-snapshot",
		"checks":         checks,
		"devices":        devices,
		"alerts":         derived,
		"latestMetricAt": latestTimestamp(metrics),
	}
	change := inspectionChange(s.store, ctx, derived, checks)
	report["change"] = change
	item, err := s.store.SaveInspection(ctx, trigger, report, change, len(devices), checks["healthy"], checks["warning"], checks["critical"])
	if err != nil {
		return store.Inspection{}, fmt.Errorf("无法保存巡检报告")
	}
	if checks["critical"] > 0 {
		_ = s.store.QueueNotification(ctx, "inspection:"+item.ID, "猫眼巡检发现 Critical", fmt.Sprintf("%d 台设备存在 Critical，%d 台 Warning", checks["critical"], checks["warning"]), "lzc://community.lazycat.app.maoyan/inspections")
	}
	return item, nil
}
func (s *Server) listInspections(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListInspections(r.Context(), 50)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取巡检记录")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "count": len(items)})
}
func (s *Server) inspectionDetail(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.InspectionByID(r.Context(), r.PathValue("id"))
	if store.IsInspectionNotFound(err) {
		problem(w, 404, "inspection_not_found", "巡检记录不存在")
		return
	}
	if err != nil {
		problem(w, 500, "internal_error", "无法读取巡检报告")
		return
	}
	writeJSON(w, 200, item)
}
func (s *Server) settingsView(w http.ResponseWriter, r *http.Request) {
	stats, _ := s.store.RetentionStats(r.Context())
	writeJSON(w, 200, map[string]any{"singleUser": true, "deploymentMode": "single-lpk", "embeddedCollector": true, "maxDevices": 100, "collectIntervalSeconds": 30, "advancedIntervalSeconds": 300, "rawRetentionDays": 30, "rollupRetentionDays": 365, "auditRetentionDays": 180, "inspectionRetentionDays": 365, "notificationChannel": "lazycat", "notificationDelivery": "outbox-retry", "certificateRotationDaysBeforeExpiry": 30, "storageStats": stats})
}
func (s *Server) operationsView(w http.ResponseWriter, r *http.Request) {
	capabilities, err := s.store.ListCapabilityStatuses(r.Context())
	if err != nil {
		problem(w, 500, "internal_error", "无法读取采集能力状态")
		return
	}
	states, err := s.store.ListSystemStates(r.Context())
	if err != nil {
		problem(w, 500, "internal_error", "无法读取运维状态")
		return
	}
	writeJSON(w, 200, map[string]any{
		"capabilities": capabilities,
		"states":       states,
		"schedule": map[string]any{
			"daily":    map[string]any{"enabled": true, "hour": 3},
			"weekly":   map[string]any{"enabled": true, "weekday": "Sunday", "hour": 4},
			"timezone": time.Now().Format("MST (UTCZ07:00)"),
		},
	})
}

func (s *Server) SyncAlerts(ctx context.Context) error {
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return err
	}
	metrics, err := s.store.ListLatestMetrics(ctx)
	if err != nil {
		return err
	}
	return s.store.ReconcileAlerts(ctx, alertSignals(deriveAlerts(buildDeviceViews(devices, metrics))))
}

func alertSignals(alerts []alertView) []store.AlertSignal {
	out := make([]store.AlertSignal, 0, len(alerts))
	for _, a := range alerts {
		out = append(out, store.AlertSignal{
			Fingerprint: a.Fingerprint, DeviceID: a.DeviceID, DeviceName: a.DeviceName,
			Severity: a.Severity, Resource: a.Resource, Message: a.Message,
			Value: a.Value, Unit: a.Unit, ObservedAt: a.CollectedAt,
		})
	}
	return out
}
func (s *Server) snapshot(r *http.Request) ([]deviceView, []store.LatestMetric, error) {
	return s.snapshotContext(r.Context())
}
func (s *Server) snapshotContext(ctx context.Context) ([]deviceView, []store.LatestMetric, error) {
	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return nil, nil, err
	}
	metrics, err := s.store.ListLatestMetrics(ctx)
	if err != nil {
		return nil, nil, err
	}
	return buildDeviceViews(devices, metrics), metrics, nil
}

func inspectionChange(st *store.Store, ctx context.Context, current []alertView, checks map[string]int) map[string]any {
	change := map[string]any{"baseline": false, "newAlerts": []string{}, "resolvedAlerts": []string{}, "warningDelta": 0, "criticalDelta": 0}
	previous, err := st.LatestInspection(ctx)
	if err != nil {
		return change
	}
	change["baseline"] = true
	change["warningDelta"] = checks["warning"] - previous.WarningCount
	change["criticalDelta"] = checks["critical"] - previous.CriticalCount
	var old struct {
		Alerts []struct {
			Fingerprint string `json:"fingerprint"`
		} `json:"alerts"`
	}
	_ = json.Unmarshal(previous.Report, &old)
	oldSet, currentSet := map[string]bool{}, map[string]bool{}
	for _, alert := range old.Alerts {
		oldSet[alert.Fingerprint] = true
	}
	for _, alert := range current {
		currentSet[alert.Fingerprint] = true
	}
	var newAlerts, resolvedAlerts []string
	for fingerprint := range currentSet {
		if !oldSet[fingerprint] {
			newAlerts = append(newAlerts, fingerprint)
		}
	}
	for fingerprint := range oldSet {
		if !currentSet[fingerprint] {
			resolvedAlerts = append(resolvedAlerts, fingerprint)
		}
	}
	sort.Strings(newAlerts)
	sort.Strings(resolvedAlerts)
	change["newAlerts"], change["resolvedAlerts"] = newAlerts, resolvedAlerts
	return change
}

func buildDeviceViews(devices []protocol.Device, metrics []store.LatestMetric) []deviceView {
	byDevice := map[string]map[string][]store.LatestMetric{}
	for _, m := range metrics {
		if byDevice[m.DeviceID] == nil {
			byDevice[m.DeviceID] = map[string][]store.LatestMetric{}
		}
		byDevice[m.DeviceID][m.Name] = append(byDevice[m.DeviceID][m.Name], m)
	}
	now := time.Now().UTC()
	out := make([]deviceView, 0, len(devices))
	for _, d := range devices {
		latest := byDevice[d.ID]
		online := d.Status != "revoked" && now.Sub(d.LastSeenAt) <= 90*time.Second
		stale := d.Status != "revoked" && now.Sub(d.LastSeenAt) > 60*time.Second
		view := deviceView{Device: d, Online: online, Stale: stale, Latest: latest, Health: "healthy"}
		if !online {
			view.Health = "warning"
		}
		for _, a := range deriveDeviceAlerts(view) {
			if a.Severity == "critical" {
				view.Health = "critical"
				break
			}
			view.Health = "warning"
		}
		out = append(out, view)
	}
	return out
}
func deriveAlerts(devices []deviceView) []alertView {
	var out []alertView
	for _, d := range devices {
		out = append(out, deriveDeviceAlerts(d)...)
	}
	sort.Slice(out, func(i, j int) bool {
		rank := map[string]int{"critical": 0, "warning": 1}
		if rank[out[i].Severity] != rank[out[j].Severity] {
			return rank[out[i].Severity] < rank[out[j].Severity]
		}
		return out[i].CollectedAt.After(out[j].CollectedAt)
	})
	return out
}
func deriveDeviceAlerts(d deviceView) []alertView {
	var out []alertView
	if !d.Online {
		out = append(out, alertView{Fingerprint: d.ID + ":offline", DeviceID: d.ID, DeviceName: d.Name, Severity: "warning", Resource: "collector", Message: "设备未在 90 秒内上报", CollectedAt: d.LastSeenAt})
	}
	for name, list := range d.Latest {
		for _, m := range list {
			severity, msg := metricAlert(name, m.Value, m.Unit, m.Labels)
			if severity == "" || time.Since(m.CollectedAt) > 10*time.Minute {
				continue
			}
			resource := m.Labels["device"]
			if resource == "" {
				resource = m.Labels["mount"]
			}
			if resource == "" {
				resource = m.Labels["app"]
			}
			if resource == "" {
				resource = name
			}
			labels, _ := json.Marshal(m.Labels)
			sum := sha256.Sum256([]byte(d.ID + "\x00" + name + "\x00" + string(labels)))
			out = append(out, alertView{Fingerprint: hex.EncodeToString(sum[:]), DeviceID: d.ID, DeviceName: d.Name, Severity: severity, Resource: resource, Message: msg, Value: m.Value, Unit: m.Unit, CollectedAt: m.CollectedAt})
		}
	}
	return out
}
func metricAlert(name string, value float64, unit string, labels map[string]string) (string, string) {
	switch name {
	case "filesystem.root.usage", "btrfs.usage":
		if value >= 95 {
			return "critical", fmt.Sprintf("存储使用率 %.1f%%", value)
		}
		if value >= 85 {
			return "warning", fmt.Sprintf("存储使用率 %.1f%%", value)
		}
	case "system.memory.usage":
		if value >= 95 {
			return "critical", fmt.Sprintf("内存使用率 %.1f%%", value)
		}
		if value >= 90 {
			return "warning", fmt.Sprintf("内存使用率 %.1f%%", value)
		}
	case "disk.temperature":
		if value >= 80 {
			return "critical", fmt.Sprintf("磁盘温度 %.0f°C", value)
		}
		if value >= 70 {
			return "warning", fmt.Sprintf("磁盘温度 %.0f°C", value)
		}
	case "disk.nvme.media_errors":
		if value > 0 {
			return "critical", fmt.Sprintf("NVMe Media Errors %.0f", value)
		}
	case "disk.nvme.critical_warning":
		if value > 0 {
			return "critical", fmt.Sprintf("NVMe Critical Warning 0x%X", int(value))
		}
	case "disk.ata.reallocated_sectors":
		if value > 0 {
			return "warning", fmt.Sprintf("重映射扇区 %.0f", value)
		}
	case "lpk.application.healthy":
		if value < 1 {
			return "warning", fmt.Sprintf("应用 %s 状态异常", labels["app"])
		}
	}
	return "", ""
}
func latestTimestamp(metrics []store.LatestMetric) time.Time {
	var latest time.Time
	for _, m := range metrics {
		if m.CollectedAt.After(latest) {
			latest = m.CollectedAt
		}
	}
	return latest
}
