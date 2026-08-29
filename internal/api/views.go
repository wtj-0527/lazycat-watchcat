package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/buildinfo"
	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

type deviceView struct {
	protocol.Device
	Online bool                            `json:"online"`
	Stale  bool                            `json:"stale"`
	Health string                          `json:"health"`
	Local  bool                            `json:"local,omitempty"`
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

type applicationResourceView struct {
	Containers  int       `json:"containers"`
	CPUPercent  float64   `json:"cpuPercent"`
	MemoryUsage float64   `json:"memoryUsage"`
	MemoryLimit float64   `json:"memoryLimit"`
	NetworkRX   float64   `json:"networkReceive"`
	NetworkTX   float64   `json:"networkTransmit"`
	BlockRead   float64   `json:"blockRead"`
	BlockWrite  float64   `json:"blockWrite"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`
}
type applicationHistoryPoint struct {
	Value       float64   `json:"value"`
	CollectedAt time.Time `json:"collectedAt"`
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	devices, metrics, err := s.snapshot(r)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取实时状态")
		return
	}
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
	savedViews, _ := s.store.ListSavedViews(r.Context())
	writeJSON(w, 200, map[string]any{"stats": stats, "devices": devices, "alerts": alerts, "savedViews": savedViews, "updatedAt": latestTimestamp(metrics)})
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
	meta, _ := s.store.DeviceMetadataMap(r.Context())
	deviceList := []protocol.Device{device}
	attachMetadata(deviceList, meta)
	view := buildDeviceViewsWithRules(deviceList, metrics, s.loadAlertRules(r.Context()))[0]
	view.Local = id == s.localDeviceID
	writeJSON(w, 200, view)
}
func (s *Server) metricHistory(w http.ResponseWriter, r *http.Request) {
	id, name := r.PathValue("id"), r.URL.Query().Get("name")
	if name == "" {
		problem(w, 400, "metric_required", "指标名称必填")
		return
	}
	from, to, code, message := deviceMetricTimeRange(r)
	if code != "" {
		problem(w, http.StatusBadRequest, code, message)
		return
	}
	points, _ := strconv.Atoi(r.URL.Query().Get("points"))
	var samples []store.MetricSample
	var err error
	if points > 0 {
		samples, err = s.store.SampledMetricHistory(r.Context(), id, name, from, to, points)
	} else {
		samples, err = s.store.MetricHistoryRange(r.Context(), id, name, from, to, 2000)
	}
	if err != nil {
		problem(w, 500, "internal_error", "无法读取指标历史")
		return
	}
	writeJSON(w, 200, map[string]any{"deviceId": id, "name": name, "items": samples})
}

func (s *Server) deviceProcesses(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	result, err := s.store.LatestProcesses(r.Context(), r.PathValue("id"), store.ProcessListOptions{
		Query: r.URL.Query().Get("q"), Sort: r.URL.Query().Get("sort"), Order: r.URL.Query().Get("order"),
		Limit: limit, Offset: (page - 1) * limit,
	})
	if err != nil {
		problem(w, 500, "internal_error", "无法读取宿主机进程")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": result.Items, "total": result.Total, "page": page, "pageSize": limit, "collectedAt": result.CollectedAt,
	})
}

func (s *Server) processMetrics(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(r.PathValue("pid"))
	startTime := strings.TrimSpace(r.URL.Query().Get("startTime"))
	if err != nil || pid <= 0 || startTime == "" {
		problem(w, http.StatusBadRequest, "process_identity_required", "pid 和 startTime 必填")
		return
	}
	from, to, code, message := deviceMetricTimeRange(r)
	if code != "" {
		problem(w, http.StatusBadRequest, code, message)
		return
	}
	items, err := s.store.ProcessHistory(r.Context(), r.PathValue("id"), pid, startTime, from, to, 2000)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取进程历史")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deviceId": r.PathValue("id"), "pid": pid, "startTime": startTime, "items": items,
	})
}

func deviceMetricTimeRange(r *http.Request) (time.Time, time.Time, string, string) {
	now := time.Now().UTC()
	fromRaw, toRaw := strings.TrimSpace(r.URL.Query().Get("from")), strings.TrimSpace(r.URL.Query().Get("to"))
	if fromRaw == "" && toRaw == "" {
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		if hours <= 0 || hours > 24*30 {
			hours = 24
		}
		return now.Add(-time.Duration(hours) * time.Hour), now, "", ""
	}
	if fromRaw == "" || toRaw == "" {
		return time.Time{}, time.Time{}, "time_range_incomplete", "自定义时间必须同时提供 from 和 to"
	}
	from, err := time.Parse(time.RFC3339, fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, "invalid_from", "from 必须是 RFC3339 时间"
	}
	to, err := time.Parse(time.RFC3339, toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, "invalid_to", "to 必须是 RFC3339 时间"
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, "invalid_time_range", "开始时间必须早于结束时间"
	}
	if to.Sub(from) > 30*24*time.Hour {
		return time.Time{}, time.Time{}, "time_range_too_large", "单次查询范围不能超过 30 天"
	}
	return from, to, "", ""
}
func (s *Server) applications(w http.ResponseWriter, r *http.Request) {
	s.scheduleRuntimeApplicationsRefresh(r.Header.Get("X-Hc-User-Id"))
	states, err := s.store.ListRuntimeApplications(r.Context())
	if err != nil {
		problem(w, 500, "internal_error", "无法读取应用状态")
		return
	}
	devices, err := s.store.ListDevices(r.Context())
	if err != nil {
		problem(w, 500, "internal_error", "无法读取设备状态")
		return
	}
	names := map[string]string{}
	for _, device := range devices {
		names[device.ID] = device.Name
	}
	userPolicies := map[string]store.RuntimeUser{}
	if runtimeUsers, userErr := s.store.ListRuntimeUsers(r.Context()); userErr == nil {
		for _, user := range runtimeUsers {
			userPolicies[user.DeviceID+"\x00"+user.UserID] = user
		}
	}
	type app struct {
		ID           string                  `json:"id"`
		Title        string                  `json:"title"`
		Versions     map[string]int          `json:"versions"`
		StatusCounts map[string]int          `json:"statusCounts"`
		Instances    int                     `json:"instances"`
		Healthy      int                     `json:"healthy"`
		Unhealthy    int                     `json:"unhealthy"`
		Paused       int                     `json:"paused"`
		Devices      []map[string]any        `json:"devices"`
		Resources    applicationResourceView `json:"resources"`
	}
	apps := map[string]*app{}
	instanceResources := map[string]applicationResourceView{}
	mergedResources := map[string]bool{}
	if metrics, metricErr := s.store.ListLatestMetrics(r.Context()); metricErr == nil {
		instanceResources = aggregateApplicationResourcesByDevice(metrics, time.Now().UTC())
	}
	var updatedAt time.Time
	users := map[string]string{}
	autostart, _ := s.store.ApplicationAutostartMap(r.Context())
	for _, state := range states {
		a := apps[state.AppID]
		if a == nil {
			a = &app{ID: state.AppID, Title: localizedAppTitle(state.AppID, state.Title), Versions: map[string]int{}, StatusCounts: map[string]int{}}
			apps[state.AppID] = a
		}
		resourceKey := state.DeviceID + "\x00" + state.AppID
		instanceResource, scoped := instanceResources[resourceKey+"\x00"+state.DeployID]
		if !scoped && state.UserID != "" {
			instanceResource, scoped = instanceResources[resourceKey+"\x00user:"+state.UserID]
		}
		if !scoped {
			instanceResource = instanceResources[resourceKey]
		}
		if state.InstanceStatus == "running" {
			if !mergedResources[resourceKey] {
				a.Resources = mergeApplicationResources(a.Resources, instanceResources[resourceKey])
				mergedResources[resourceKey] = true
			}
		} else {
			instanceResource = applicationResourceView{}
		}
		a.Instances++
		a.Versions[state.Version]++
		a.StatusCounts[state.InstanceStatus]++
		switch state.InstanceStatus {
		case "running":
			a.Healthy++
		case "paused":
			a.Paused++
		case "error":
			a.Unhealthy++
		}
		userID := strings.TrimSpace(state.UserID)
		userName := strings.TrimSpace(state.UserName)
		if userName == "" {
			userName = userID
		}
		if userID != "" {
			users[userID] = userName
		}
		accessPolicyKnown, accessGranted, accessReason := false, false, "unknown"
		if policy, ok := userPolicies[state.DeviceID+"\x00"+userID]; ok {
			accessPolicyKnown = true
			switch {
			case policy.AppAccessNoLimit:
				accessGranted, accessReason = true, "all_apps"
			case containsString(policy.AllowedAppIDs, state.AppID):
				accessGranted, accessReason = true, "allowed_app"
			default:
				accessReason = "not_allowed"
			}
		}
		a.Devices = append(a.Devices, map[string]any{
			"deviceId": state.DeviceID, "deviceName": names[state.DeviceID], "deployId": state.DeployID,
			"healthy": state.InstanceStatus == "running", "status": state.InstanceStatus,
			"installStatus": state.InstallStatus, "version": state.Version, "domain": state.Domain,
			"builtin": state.Builtin, "userId": userID, "userName": userName,
			"accessPolicyKnown": accessPolicyKnown, "accessGranted": accessGranted, "accessReason": accessReason,
			"collectedAt": state.UpdatedAt, "resources": instanceResource,
			"autostart":    autostart[state.DeviceID+"\x00"+state.DeployID],
			"controllable": !applicationControlRestricted(state),
		})
		if state.UpdatedAt.After(updatedAt) {
			updatedAt = state.UpdatedAt
		}
	}
	out := make([]*app, 0, len(apps))
	for _, a := range apps {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	userList := make([]map[string]string, 0, len(users))
	for id, name := range users {
		userList = append(userList, map[string]string{"id": id, "name": name})
	}
	sort.Slice(userList, func(i, j int) bool { return userList[i]["name"] < userList[j]["name"] })
	writeJSON(w, 200, map[string]any{"items": out, "users": userList, "count": len(out), "updatedAt": updatedAt, "source": "lazycat-package-manager", "stale": s.runtimeApps != nil && s.runtimeApps.LastUID() == ""})
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func localizedAppTitle(appID, fallback string) string {
	if title := map[string]string{
		"cloud.lazycat.app.contacts":                    "懒猫通讯录",
		"cloud.lazycat.app.downloader":                  "懒猫下载器",
		"cloud.lazycat.app.movie":                       "懒猫影视",
		"cloud.lazycat.app.photo":                       "懒猫相册",
		"cloud.lazycat.app.todolist":                    "懒猫清单",
		"cloud.lazycat.app.video":                       "懒猫视频",
		"cloud.lazycat.shell.appstore":                  "应用商店",
		"cloud.lazycat.shell.backup":                    "备份与恢复",
		"cloud.lazycat.shell.files":                     "懒猫云盘",
		"cloud.lazycat.shell.home":                      "桌面",
		"cloud.lazycat.shell.settings":                  "系统设置",
		"cloud.lazycat.developer.tools":                 "开发者工具",
		"cloud.lazycat.app.forward":                     "端口转发",
		"cloud.lazycat.app.cloudmount":                  "OpenList 挂载工具",
		"cloud.lazycat.app.testflight":                  "测试飞行",
		"cloud.lazycat.app.lazycat-agent-browser-skill": "懒猫智能体浏览器",
	}[appID]; title != "" {
		return title
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return appID
}

func (s *Server) SyncRuntimeApplications(ctx context.Context, uid string) (int, error) {
	if s.runtimeApps == nil || s.localDeviceID == "" {
		return 0, errors.New("LazyCat Package Manager API is unavailable")
	}
	items, err := s.runtimeApps.Query(ctx, uid)
	if err != nil {
		_ = s.store.SetCapabilityStatuses(ctx, s.localDeviceID, []store.CapabilityStatus{{
			DeviceID: s.localDeviceID, Capability: "lpk.runtime", Status: "error",
			Detail: "LazyCat Package Manager API: " + err.Error(), CheckedAt: time.Now().UTC(),
		}})
		return 0, err
	}
	stored := make([]store.RuntimeApplication, 0, len(items))
	for _, item := range items {
		stored = append(stored, store.RuntimeApplication{
			DeviceID: s.localDeviceID, DeployID: item.DeployID, AppID: item.AppID,
			Title: item.Title, Version: item.Version, InstallStatus: item.InstallStatus,
			InstanceStatus: item.InstanceStatus, Domain: item.Domain, Builtin: item.Builtin,
			UserID: item.UserID, UserName: item.UserName,
		})
	}
	if err := s.store.ReplaceRuntimeApplications(ctx, s.localDeviceID, stored); err != nil {
		return 0, err
	}
	_ = s.store.SetCapabilityStatuses(ctx, s.localDeviceID, []store.CapabilityStatus{{
		DeviceID: s.localDeviceID, Capability: "lpk.runtime", Status: "available",
		Detail: fmt.Sprintf("官方 Package Manager API，已同步 %d 个应用实例", len(stored)), CheckedAt: time.Now().UTC(),
	}})
	return len(stored), nil
}

func (s *Server) applicationMetrics(w http.ResponseWriter, r *http.Request) {
	appID := strings.TrimSpace(r.PathValue("id"))
	if appID == "" {
		problem(w, http.StatusBadRequest, "application_required", "应用 ID 必填")
		return
	}
	from, to, code, message := applicationTimeRange(r)
	if code != "" {
		problem(w, http.StatusBadRequest, code, message)
		return
	}
	ctx, done, ok := s.beginAnalytics(w, r)
	if !ok {
		return
	}
	defer done()
	now := time.Now().UTC()
	summary := map[string]float64{}
	duration := to.Sub(from)
	bucket := applicationHistoryBucket(int(duration.Hours()))
	metrics := []struct {
		name    string
		key     string
		counter bool
	}{
		{"container.cpu.usage", "cpuPercent", false},
		{"container.memory.usage", "memoryUsage", false},
		{"container.network.receive.bytes_total", "networkReceiveRate", true},
		{"container.network.transmit.bytes_total", "networkTransmitRate", true},
		{"container.block.read.bytes_total", "blockReadRate", true},
		{"container.block.write.bytes_total", "blockWriteRate", true},
	}
	series := make(map[string][]applicationHistoryPoint, len(metrics))
	deviceID := strings.TrimSpace(r.URL.Query().Get("deviceId"))
	deployID := strings.TrimSpace(r.URL.Query().Get("deployId"))
	userID := strings.TrimSpace(r.URL.Query().Get("userId"))
	for _, metric := range metrics {
		var samples []store.ApplicationMetricSample
		var err error
		if deviceID != "" {
			samples, err = s.store.ApplicationMetricHistoryForDevice(ctx, deviceID, appID, metric.name, from, to, 100000)
		} else {
			samples, err = s.store.ApplicationMetricHistory(ctx, appID, metric.name, from, to, 100000)
		}
		if err != nil {
			problem(w, http.StatusInternalServerError, "internal_error", "无法读取应用资源历史")
			return
		}
		samples = filterApplicationSamples(samples, deployID, userID)
		if metric.counter {
			points, total := aggregateApplicationCounterWithTotal(samples, bucket)
			series[metric.key] = points
			summary[metric.key+"Bytes"] = total
		} else {
			series[metric.key] = aggregateApplicationGauge(samples, bucket)
		}
	}
	summary["networkTotalBytes"] = summary["networkReceiveRateBytes"] + summary["networkTransmitRateBytes"]
	summary["blockTotalBytes"] = summary["blockReadRateBytes"] + summary["blockWriteRateBytes"]
	writeJSON(w, http.StatusOK, map[string]any{
		"appId": appID, "deviceId": deviceID, "deployId": deployID, "userId": userID,
		"from": from, "to": to, "bucketSeconds": int(bucket.Seconds()),
		"series": series, "summary": summary, "updatedAt": now,
	})
}

func filterApplicationSamples(samples []store.ApplicationMetricSample, deployID, userID string) []store.ApplicationMetricSample {
	if deployID == "" && userID == "" {
		return preferScopedApplicationSamples(samples)
	}
	out := make([]store.ApplicationMetricSample, 0, len(samples))
	for _, sample := range samples {
		if deployID != "" && sample.Labels["deployId"] != deployID {
			continue
		}
		if userID != "" && sample.Labels["userId"] != userID {
			continue
		}
		out = append(out, sample)
	}
	return out
}

func preferScopedApplicationSamples(samples []store.ApplicationMetricSample) []store.ApplicationMetricSample {
	scoped := map[string]bool{}
	for _, sample := range samples {
		if sample.Labels["deployId"] != "" || sample.Labels["userId"] != "" {
			scoped[sample.DeviceID+"\x00"+sample.Labels["app"]] = true
		}
	}
	if len(scoped) == 0 {
		return samples
	}
	out := make([]store.ApplicationMetricSample, 0, len(samples))
	for _, sample := range samples {
		base := sample.DeviceID + "\x00" + sample.Labels["app"]
		if scoped[base] && sample.Labels["deployId"] == "" && sample.Labels["userId"] == "" {
			continue
		}
		out = append(out, sample)
	}
	return out
}

func applicationTimeRange(r *http.Request) (time.Time, time.Time, string, string) {
	now := time.Now().UTC()
	fromRaw, toRaw := strings.TrimSpace(r.URL.Query().Get("from")), strings.TrimSpace(r.URL.Query().Get("to"))
	if fromRaw == "" && toRaw == "" {
		hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
		if hours <= 0 || hours > 24*7 {
			hours = 24
		}
		return now.Add(-time.Duration(hours) * time.Hour), now, "", ""
	}
	if fromRaw == "" || toRaw == "" {
		return time.Time{}, time.Time{}, "time_range_incomplete", "自定义时间必须同时提供 from 和 to"
	}
	from, err := time.Parse(time.RFC3339, fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, "invalid_from", "from 必须是 RFC3339 时间"
	}
	to, err := time.Parse(time.RFC3339, toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, "invalid_to", "to 必须是 RFC3339 时间"
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, "invalid_time_range", "开始时间必须早于结束时间"
	}
	if to.Sub(from) > 30*24*time.Hour {
		return time.Time{}, time.Time{}, "time_range_too_large", "单次查询范围不能超过 30 天"
	}
	return from, to, "", ""
}

func (s *Server) applicationMetricsComparison(w http.ResponseWriter, r *http.Request) {
	metric := strings.TrimSpace(r.URL.Query().Get("metric"))
	if metric == "" {
		metric = "cpu"
	}
	if metric != "cpu" && metric != "memory" && metric != "network" && metric != "disk" {
		problem(w, http.StatusBadRequest, "invalid_metric", "metric 仅支持 cpu、memory、network 或 disk")
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "app"
	}
	if scope != "app" && scope != "instance" {
		problem(w, http.StatusBadRequest, "invalid_scope", "scope 仅支持 app 或 instance")
		return
	}
	from, to, code, message := applicationTimeRange(r)
	if code != "" {
		problem(w, http.StatusBadRequest, code, message)
		return
	}
	ctx, done, ok := s.beginAnalytics(w, r)
	if !ok {
		return
	}
	defer done()
	bucket := applicationHistoryBucket(int(to.Sub(from).Hours()))
	type comparisonItem struct {
		AppID    string                    `json:"appId"`
		DeviceID string                    `json:"deviceId,omitempty"`
		DeployID string                    `json:"deployId,omitempty"`
		UserID   string                    `json:"userId,omitempty"`
		Value    float64                   `json:"value"`
		Unit     string                    `json:"unit"`
		Points   []applicationHistoryPoint `json:"points"`
	}
	items := map[string]*comparisonItem{}
	itemIdentity := func(key string, samples []store.ApplicationMetricSample) (string, string, string, string) {
		if len(samples) == 0 {
			return "", "", "", ""
		}
		appID := samples[0].Labels["app"]
		if scope == "instance" {
			return appID, samples[0].DeviceID, samples[0].Labels["deployId"], samples[0].Labels["userId"]
		}
		return appID, "", "", ""
	}
	deviceFilter := strings.TrimSpace(r.URL.Query().Get("deviceId"))
	userFilter := strings.TrimSpace(r.URL.Query().Get("userId"))
	filterSamples := func(samples []store.ApplicationMetricSample) []store.ApplicationMetricSample {
		samples = preferScopedApplicationSamples(samples)
		out := make([]store.ApplicationMetricSample, 0, len(samples))
		for _, sample := range samples {
			if deviceFilter != "" && sample.DeviceID != deviceFilter {
				continue
			}
			if userFilter != "" && sample.Labels["userId"] != userFilter {
				continue
			}
			out = append(out, sample)
		}
		return out
	}
	addGauge := func(name, unit string) error {
		samples, err := s.store.AllApplicationMetricHistory(ctx, name, from, to, 300000)
		if err != nil {
			return err
		}
		samples = filterSamples(samples)
		for key, groupedSamples := range groupApplicationSamplesByScope(samples, scope) {
			points := aggregateApplicationGauge(groupedSamples, bucket)
			appID, deviceID, deployID, userID := itemIdentity(key, groupedSamples)
			items[key] = &comparisonItem{AppID: appID, DeviceID: deviceID, DeployID: deployID, UserID: userID, Value: averageHistoryPoints(points), Unit: unit, Points: points}
		}
		return nil
	}
	addCounter := func(names []string) error {
		for _, name := range names {
			samples, err := s.store.AllApplicationMetricHistory(ctx, name, from, to, 300000)
			if err != nil {
				return err
			}
			samples = filterSamples(samples)
			for key, groupedSamples := range groupApplicationSamplesByScope(samples, scope) {
				points, total := aggregateApplicationCounterWithTotal(groupedSamples, bucket)
				item := items[key]
				if item == nil {
					appID, deviceID, deployID, userID := itemIdentity(key, groupedSamples)
					item = &comparisonItem{AppID: appID, DeviceID: deviceID, DeployID: deployID, UserID: userID, Unit: "bytes"}
					items[key] = item
				}
				item.Value += total
				item.Points = mergeHistoryPoints(item.Points, points)
			}
		}
		return nil
	}
	var err error
	switch metric {
	case "cpu":
		err = addGauge("container.cpu.usage", "%")
	case "memory":
		err = addGauge("container.memory.usage", "bytes")
	case "network":
		err = addCounter([]string{"container.network.receive.bytes_total", "container.network.transmit.bytes_total"})
	case "disk":
		err = addCounter([]string{"container.block.read.bytes_total", "container.block.write.bytes_total"})
	}
	if err != nil {
		problem(w, http.StatusInternalServerError, "internal_error", "无法读取应用对比数据")
		return
	}
	out := make([]*comparisonItem, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Value > out[j].Value })
	writeJSON(w, http.StatusOK, map[string]any{
		"metric": metric, "scope": scope, "from": from, "to": to, "bucketSeconds": int(bucket.Seconds()),
		"items": out, "updatedAt": time.Now().UTC(),
	})
}

func (s *Server) beginAnalytics(w http.ResponseWriter, r *http.Request) (context.Context, func(), bool) {
	wait := time.NewTimer(2 * time.Second)
	defer wait.Stop()
	select {
	case s.analytics <- struct{}{}:
	case <-r.Context().Done():
		return nil, func() {}, false
	case <-wait.C:
		w.Header().Set("Retry-After", "2")
		problem(w, http.StatusTooManyRequests, "analytics_busy", "历史分析正在计算，请稍后重试")
		return nil, func() {}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	return ctx, func() {
		cancel()
		<-s.analytics
	}, true
}

func groupApplicationSamples(samples []store.ApplicationMetricSample) map[string][]store.ApplicationMetricSample {
	return groupApplicationSamplesByScope(samples, "app")
}

func groupApplicationSamplesByScope(samples []store.ApplicationMetricSample, scope string) map[string][]store.ApplicationMetricSample {
	out := map[string][]store.ApplicationMetricSample{}
	for _, sample := range samples {
		appID := sample.Labels["app"]
		if appID == "" {
			continue
		}
		key := appID
		if scope == "instance" {
			key = sample.DeviceID + "\x00" + appID
			if deployID := sample.Labels["deployId"]; deployID != "" {
				key += "\x00" + deployID
			} else if userID := sample.Labels["userId"]; userID != "" {
				key += "\x00user:" + userID
			}
		}
		out[key] = append(out[key], sample)
	}
	return out
}

func averageHistoryPoints(points []applicationHistoryPoint) float64 {
	if len(points) == 0 {
		return 0
	}
	total := 0.0
	for _, point := range points {
		total += point.Value
	}
	return total / float64(len(points))
}

func mergeHistoryPoints(left, right []applicationHistoryPoint) []applicationHistoryPoint {
	values := map[time.Time]float64{}
	for _, point := range left {
		values[point.CollectedAt] += point.Value
	}
	for _, point := range right {
		values[point.CollectedAt] += point.Value
	}
	times := make([]time.Time, 0, len(values))
	for at := range values {
		times = append(times, at)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	out := make([]applicationHistoryPoint, 0, len(times))
	for _, at := range times {
		out = append(out, applicationHistoryPoint{Value: values[at], CollectedAt: at})
	}
	return out
}

func applicationHistoryBucket(hours int) time.Duration {
	switch {
	case hours <= 1:
		return time.Minute
	case hours <= 6:
		return 2 * time.Minute
	case hours <= 24:
		return 5 * time.Minute
	default:
		return 30 * time.Minute
	}
}

func applicationSeriesKey(sample store.ApplicationMetricSample) string {
	return sample.DeviceID + "\x00" + sample.Labels["container"]
}

func aggregateApplicationGauge(samples []store.ApplicationMetricSample, bucket time.Duration) []applicationHistoryPoint {
	type values struct {
		sum   float64
		count int
	}
	byBucket := map[time.Time]map[string]values{}
	for _, sample := range samples {
		at := sample.CollectedAt.Truncate(bucket)
		if byBucket[at] == nil {
			byBucket[at] = map[string]values{}
		}
		key := applicationSeriesKey(sample)
		item := byBucket[at][key]
		item.sum += sample.Value
		item.count++
		byBucket[at][key] = item
	}
	return applicationHistoryPoints(byBucket, func(items map[string]values) float64 {
		total := 0.0
		for _, item := range items {
			if item.count > 0 {
				total += item.sum / float64(item.count)
			}
		}
		return total
	})
}

func aggregateApplicationCounter(samples []store.ApplicationMetricSample, bucket time.Duration) []applicationHistoryPoint {
	points, _ := aggregateApplicationCounterWithTotal(samples, bucket)
	return points
}

func aggregateApplicationCounterWithTotal(samples []store.ApplicationMetricSample, bucket time.Duration) ([]applicationHistoryPoint, float64) {
	type values struct {
		sum   float64
		count int
	}
	byBucket := map[time.Time]map[string]values{}
	previous := map[string]store.ApplicationMetricSample{}
	total := 0.0
	for _, sample := range samples {
		key := applicationSeriesKey(sample)
		if before, ok := previous[key]; ok {
			elapsed := sample.CollectedAt.Sub(before.CollectedAt).Seconds()
			if elapsed > 0 && elapsed <= 30*60 && sample.Value >= before.Value {
				total += sample.Value - before.Value
				at := sample.CollectedAt.Truncate(bucket)
				if byBucket[at] == nil {
					byBucket[at] = map[string]values{}
				}
				item := byBucket[at][key]
				item.sum += (sample.Value - before.Value) / elapsed
				item.count++
				byBucket[at][key] = item
			}
		}
		previous[key] = sample
	}
	points := applicationHistoryPoints(byBucket, func(items map[string]values) float64 {
		total := 0.0
		for _, item := range items {
			if item.count > 0 {
				total += item.sum / float64(item.count)
			}
		}
		return total
	})
	return points, total
}

func applicationHistoryPoints[T any](buckets map[time.Time]T, value func(T) float64) []applicationHistoryPoint {
	times := make([]time.Time, 0, len(buckets))
	for at := range buckets {
		times = append(times, at)
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	points := make([]applicationHistoryPoint, 0, len(times))
	for _, at := range times {
		points = append(points, applicationHistoryPoint{Value: value(buckets[at]), CollectedAt: at})
	}
	return points
}

func aggregateApplicationResources(metrics []store.LatestMetric, now time.Time) map[string]applicationResourceView {
	byDevice := aggregateApplicationResourcesByDevice(metrics, now)
	out := map[string]applicationResourceView{}
	for key, item := range byDevice {
		firstSeparator := strings.IndexByte(key, 0)
		if firstSeparator < 0 || strings.IndexByte(key[firstSeparator+1:], 0) >= 0 {
			continue
		}
		appID := key[firstSeparator+1:]
		out[appID] = mergeApplicationResources(out[appID], item)
	}
	return out
}

func aggregateApplicationResourcesByDevice(metrics []store.LatestMetric, now time.Time) map[string]applicationResourceView {
	out := map[string]applicationResourceView{}
	containers := map[string]map[string]struct{}{}
	scopedBases := map[string]bool{}
	for _, metric := range metrics {
		if now.Sub(metric.CollectedAt) > 6*time.Minute {
			continue
		}
		appID := metric.Labels["app"]
		if appID != "" && strings.HasPrefix(metric.Name, "container.") &&
			(metric.Labels["deployId"] != "" || metric.Labels["userId"] != "") {
			scopedBases[metric.DeviceID+"\x00"+appID] = true
		}
	}
	for _, metric := range metrics {
		if now.Sub(metric.CollectedAt) > 6*time.Minute {
			continue
		}
		appID := metric.Labels["app"]
		if appID == "" || !strings.HasPrefix(metric.Name, "container.") {
			continue
		}
		baseKey := metric.DeviceID + "\x00" + appID
		deployID, userID := metric.Labels["deployId"], metric.Labels["userId"]
		if scopedBases[baseKey] && deployID == "" && userID == "" {
			continue
		}
		key := baseKey
		if deployID != "" {
			key += "\x00" + deployID
		} else if userID != "" {
			key += "\x00user:" + userID
		}
		item := out[key]
		switch metric.Name {
		case "container.running":
			if metric.Value >= 1 {
				if containers[key] == nil {
					containers[key] = map[string]struct{}{}
				}
				containers[key][metric.Labels["container"]] = struct{}{}
			}
		case "container.cpu.usage":
			item.CPUPercent += metric.Value
		case "container.memory.usage":
			item.MemoryUsage += metric.Value
		case "container.memory.limit":
			item.MemoryLimit += metric.Value
		case "container.network.receive.bytes_total":
			item.NetworkRX += metric.Value
		case "container.network.transmit.bytes_total":
			item.NetworkTX += metric.Value
		case "container.block.read.bytes_total":
			item.BlockRead += metric.Value
		case "container.block.write.bytes_total":
			item.BlockWrite += metric.Value
		}
		if metric.CollectedAt.After(item.UpdatedAt) {
			item.UpdatedAt = metric.CollectedAt
		}
		out[key] = item
	}
	for key, set := range containers {
		item := out[key]
		item.Containers = len(set)
		out[key] = item
	}
	for key, item := range out {
		firstSeparator := strings.IndexByte(key, 0)
		if firstSeparator < 0 {
			continue
		}
		secondRelative := strings.IndexByte(key[firstSeparator+1:], 0)
		if secondRelative < 0 {
			continue
		}
		baseKey := key[:firstSeparator+1+secondRelative]
		out[baseKey] = mergeApplicationResources(out[baseKey], item)
	}
	return out
}

func mergeApplicationResources(left, right applicationResourceView) applicationResourceView {
	left.Containers += right.Containers
	left.CPUPercent += right.CPUPercent
	left.MemoryUsage += right.MemoryUsage
	left.MemoryLimit += right.MemoryLimit
	left.NetworkRX += right.NetworkRX
	left.NetworkTX += right.NetworkTX
	left.BlockRead += right.BlockRead
	left.BlockWrite += right.BlockWrite
	if right.UpdatedAt.After(left.UpdatedAt) {
		left.UpdatedAt = right.UpdatedAt
	}
	return left
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
	totalBytes := float64(0)
	rules := s.loadAlertRules(r.Context())
	availableByVolume := map[string]float64{}
	for _, metric := range metrics {
		if metric.Name == "filesystem.root.available" {
			availableByVolume[metric.DeviceID+"\x00"+metric.Labels["mount"]+"\x00"+metric.Labels["device"]] = metric.Value
		}
	}
	for _, m := range metrics {
		if !(strings.HasPrefix(m.Name, "filesystem.") || strings.HasPrefix(m.Name, "btrfs.") || strings.HasPrefix(m.Name, "disk.")) {
			continue
		}
		severity, _ := metricAlertWithRules(m.Name, m.Value, m.Unit, m.Labels, rules)
		items = append(items, map[string]any{"deviceId": m.DeviceID, "deviceName": names[m.DeviceID], "name": m.Name, "value": m.Value, "unit": m.Unit, "labels": m.Labels, "collectedAt": m.CollectedAt, "risk": severity})
		if m.Name == "filesystem.root.usage" && m.Value >= 0 && m.Value < 100 {
			key := m.DeviceID + "\x00" + m.Labels["mount"] + "\x00" + m.Labels["device"]
			if available := availableByVolume[key]; available > 0 {
				totalBytes += available / (1 - m.Value/100)
			}
		}
	}
	writeJSON(w, 200, map[string]any{
		"items": items, "count": len(items), "updatedAt": latestTimestamp(metrics),
		// Keep the initial storage snapshot independent from the raw history
		// table. On long-running installations that table can be gigabytes,
		// and a synchronous 30-day scan used to block the whole page.
		"summary": map[string]any{"totalBytes": totalBytes, "fillWithin30Days": 0},
	})
}
func (s *Server) alertsView(w http.ResponseWriter, r *http.Request) {
	devices, _, err := s.snapshot(r)
	if err != nil {
		problem(w, 500, "internal_error", "无法读取告警状态")
		return
	}
	if err := s.store.ReconcileAlerts(r.Context(), alertSignals(deriveAlertsWithRules(devices, s.loadAlertRules(r.Context())))); err != nil {
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
	if action == "resolve" {
		problem(w, http.StatusConflict, "alert_resolution_automatic", "告警仅在规则不再成立时自动恢复；请先处理原因并等待下一次规则评估")
		return
	}
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
	checks := inspectionChecks(devices)
	writeJSON(w, 200, map[string]any{"generatedAt": time.Now().UTC(), "source": "live-snapshot", "checks": checks, "devices": devices})
}

func inspectionChecks(devices []deviceView) map[string]int {
	checks := map[string]int{"devices": len(devices), "online": 0, "healthy": 0, "warning": 0, "critical": 0}
	for _, device := range devices {
		if device.Online && !device.Stale {
			checks["online"]++
		}
		switch {
		case device.Health == "critical":
			checks["critical"]++
		case !device.Online || device.Stale || device.Health == "warning":
			checks["warning"]++
		case device.Health == "healthy":
			checks["healthy"]++
		}
	}
	return checks
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
	derived := deriveAlertsWithRules(devices, s.loadAlertRulesContext(ctx))
	if err := s.store.ReconcileAlerts(ctx, alertSignals(derived)); err != nil {
		return store.Inspection{}, fmt.Errorf("无法更新告警状态")
	}
	checks := inspectionChecks(devices)
	applications, _ := s.store.ListRuntimeApplications(ctx)
	appChecks := map[string]int{"instances": len(applications), "running": 0, "paused": 0, "error": 0}
	for _, application := range applications {
		switch application.InstanceStatus {
		case "running":
			appChecks["running"]++
		case "paused":
			appChecks["paused"]++
		default:
			appChecks["error"]++
		}
	}
	notificationChecks, _ := s.store.NotificationSummary(ctx)
	report := map[string]any{
		"schemaVersion":      2,
		"generatedAt":        time.Now().UTC(),
		"source":             "collector-snapshot",
		"checks":             checks,
		"applicationChecks":  appChecks,
		"notificationChecks": notificationChecks,
		"devices":            devices,
		"alerts":             derived,
		"applications":       applications,
		"latestMetricAt":     latestTimestamp(metrics),
	}
	change := inspectionChange(s.store, ctx, derived, checks)
	report["change"] = change
	item, err := s.store.SaveInspection(ctx, trigger, report, change, len(devices), checks["healthy"], checks["warning"], checks["critical"])
	if err != nil {
		return store.Inspection{}, fmt.Errorf("无法保存巡检报告")
	}
	if checks["critical"] > 0 {
		_ = s.store.QueueNotification(ctx, "inspection:"+item.ID, "WatchCat 巡检发现 Critical", fmt.Sprintf("%d 台设备存在 Critical，%d 台 Warning", checks["critical"], checks["warning"]), "lzc://community.lazycat.app.watchcat/inspections")
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
	if r.Method == http.MethodPut {
		settings := s.store.OperationalSettings(r.Context())
		if decodeJSON(r, &settings) != nil {
			problem(w, 400, "invalid_settings", "设置参数无效")
			return
		}
		if err := s.store.SetOperationalSettings(r.Context(), settings); err != nil {
			problem(w, 400, "invalid_settings", err.Error())
			return
		}
		if s.backup != nil {
			_ = s.backup.Prune(settings.BackupRetentionCount)
		}
		writeJSON(w, 200, settings)
		return
	}
	settings := s.store.OperationalSettings(r.Context())
	// Exact COUNT(*) queries over the raw metrics table are intentionally not
	// part of page initialization. Production databases can contain tens of
	// gigabytes of samples, making those counts block the whole settings page.
	writeJSON(w, 200, map[string]any{
		"appVersion": buildinfo.Version, "singleUser": true, "deploymentMode": "single-lpk",
		"embeddedCollector": true, "maxDevices": 100,
		"systemIntervalSeconds":   settings.SystemIntervalSeconds,
		"runtimeIntervalSeconds":  settings.RuntimeIntervalSeconds,
		"storageIntervalSeconds":  settings.StorageIntervalSeconds,
		"advancedIntervalSeconds": settings.AdvancedIntervalSeconds,
		"rawRetentionDays":        settings.RawRetentionDays, "rollupRetentionDays": settings.RollupRetentionDays,
		"auditRetentionDays": settings.AuditRetentionDays, "inspectionRetentionDays": settings.InspectionRetentionDays,
		"backupRetentionCount": settings.BackupRetentionCount,
		"dailyInspectionHour":  settings.DailyInspectionHour, "weeklyInspectionHour": settings.WeeklyInspectionHour,
		"notificationChannel": "lazycat", "notificationDelivery": "outbox-retry",
		"certificateRotationDaysBeforeExpiry": 30,
	})
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
	settings := s.store.OperationalSettings(r.Context())
	writeJSON(w, 200, map[string]any{
		"capabilities": capabilities,
		"states":       states,
		"schedule": map[string]any{
			"daily":    map[string]any{"enabled": true, "hour": settings.DailyInspectionHour},
			"weekly":   map[string]any{"enabled": true, "weekday": "Sunday", "hour": settings.WeeklyInspectionHour},
			"timezone": time.Now().Format("MST (UTCZ07:00)"),
		},
	})
}

func (s *Server) SyncAlerts(ctx context.Context) (result error) {
	s.alertSyncMu.Lock()
	if s.alertSyncing || (!s.alertLastSync.IsZero() && time.Since(s.alertLastSync) < 20*time.Second) {
		s.alertSyncMu.Unlock()
		return nil
	}
	s.alertSyncing = true
	s.alertSyncMu.Unlock()
	defer func() {
		s.alertSyncMu.Lock()
		s.alertSyncing = false
		if result == nil {
			s.alertLastSync = time.Now()
		}
		s.alertSyncMu.Unlock()
	}()

	devices, err := s.store.ListDevices(ctx)
	if err != nil {
		return err
	}
	metrics, err := s.store.ListLatestMetrics(ctx)
	if err != nil {
		return err
	}
	meta, _ := s.store.DeviceMetadataMap(ctx)
	attachMetadata(devices, meta)
	rules := s.loadAlertRulesContext(ctx)
	return s.store.ReconcileAlerts(ctx, alertSignals(deriveAlertsWithRules(buildDeviceViewsWithRules(devices, metrics, rules), rules)))
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
	meta, _ := s.store.DeviceMetadataMap(ctx)
	attachMetadata(devices, meta)
	views := buildDeviceViewsWithRules(devices, metrics, s.loadAlertRulesContext(ctx))
	for index := range views {
		views[index].Local = views[index].ID == s.localDeviceID
	}
	return views, metrics, nil
}

func (s *Server) loadAlertRulesContext(ctx context.Context) []alertRule {
	return s.loadAlertRules(ctx)
}

func attachMetadata(devices []protocol.Device, metadata map[string]store.DeviceMetadata) {
	for index := range devices {
		if item, ok := metadata[devices[index].ID]; ok {
			devices[index].Group, devices[index].Location, devices[index].Labels = item.Group, item.Location, item.Labels
		}
	}
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
	return buildDeviceViewsWithRules(devices, metrics, defaultAlertRules())
}
func buildDeviceViewsWithRules(devices []protocol.Device, metrics []store.LatestMetric, rules []alertRule) []deviceView {
	byDevice := map[string]map[string][]store.LatestMetric{}
	for _, m := range metrics {
		m.Risk, _ = metricAlertWithRules(m.Name, m.Value, m.Unit, m.Labels, rules)
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
		view := deviceView{Device: d, Online: online, Stale: stale, Latest: latest, Health: "unknown"}
		if online && hasFreshHealthEvidence(latest, now) {
			view.Health = "healthy"
			for _, a := range deriveDeviceAlertsWithRules(view, rules) {
				if a.Severity == "critical" {
					view.Health = "critical"
					break
				}
				view.Health = "warning"
			}
		}
		out = append(out, view)
	}
	return out
}

func hasFreshHealthEvidence(latest map[string][]store.LatestMetric, now time.Time) bool {
	for name, metrics := range latest {
		if !isHealthMetric(name) {
			continue
		}
		for _, metric := range metrics {
			if now.Sub(metric.CollectedAt) <= 10*time.Minute {
				return true
			}
		}
	}
	return false
}

func isHealthMetric(name string) bool {
	switch name {
	case "system.cpu.usage", "filesystem.root.usage", "filesystem.volume.usage", "btrfs.usage", "system.memory.usage",
		"system.temperature", "container.memory.usage_percent", "disk.temperature",
		"disk.io.busy_percent",
		"disk.nvme.media_errors", "disk.nvme.critical_warning", "disk.ata.reallocated_sectors",
		"disk.ata.pending_sectors", "disk.ata.offline_uncorrectable", "disk.ata.reported_uncorrectable",
		"lpk.application.healthy":
		return true
	default:
		return false
	}
}

func deriveAlerts(devices []deviceView) []alertView {
	return deriveAlertsWithRules(devices, defaultAlertRules())
}
func deriveAlertsWithRules(devices []deviceView, rules []alertRule) []alertView {
	var out []alertView
	for _, d := range devices {
		out = append(out, deriveDeviceAlertsWithRules(d, rules)...)
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
	return deriveDeviceAlertsWithRules(d, defaultAlertRules())
}
func deriveDeviceAlertsWithRules(d deviceView, rules []alertRule) []alertView {
	var out []alertView
	if !d.Online {
		out = append(out, alertView{Fingerprint: d.ID + ":offline", DeviceID: d.ID, DeviceName: d.Name, Severity: "warning", Resource: "collector", Message: "设备未在 90 秒内上报", CollectedAt: d.LastSeenAt})
	}
	for name, list := range d.Latest {
		for _, m := range list {
			severity, msg := metricAlertWithRules(name, m.Value, m.Unit, m.Labels, rules)
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
				resource = m.Labels["sensor"]
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
	return metricAlertWithRules(name, value, unit, labels, defaultAlertRules())
}
func metricAlertWithRules(name string, value float64, unit string, labels map[string]string, rules []alertRule) (string, string) {
	for _, rule := range rules {
		if rule.Enabled && rule.Metric == name {
			if value >= rule.Critical {
				return "critical", fmt.Sprintf("%s %.1f%s", rule.Label, value, unit)
			}
			if value >= rule.Warning {
				return "warning", fmt.Sprintf("%s %.1f%s", rule.Label, value, unit)
			}
			return "", ""
		}
	}
	switch name {
	case "system.temperature":
		sensor := strings.ToLower(labels["sensor"])
		switch {
		case strings.HasPrefix(sensor, "coretemp_core_"), strings.HasPrefix(sensor, "nvme_sensor_"):
			// Per-core and NVMe sub-sensors are intentionally retained as raw
			// telemetry but are too bursty or vendor-specific for paging.
			return "", ""
		case strings.HasPrefix(sensor, "coretemp_package"):
			if value >= 100 {
				return "critical", fmt.Sprintf("CPU 封装温度 %.0f°C", value)
			}
			return "", ""
		case strings.HasPrefix(sensor, "nvme_composite"):
			if value >= 90 {
				return "critical", fmt.Sprintf("NVMe 综合温度 %.0f°C", value)
			}
			if value >= 85 {
				return "warning", fmt.Sprintf("NVMe 综合温度 %.0f°C", value)
			}
			return "", ""
		case strings.HasPrefix(sensor, "spd5118"):
			if value >= 85 {
				return "critical", fmt.Sprintf("内存温度 %.0f°C", value)
			}
			if value >= 55 {
				return "warning", fmt.Sprintf("内存温度 %.0f°C", value)
			}
			return "", ""
		}
		if value >= 90 {
			return "critical", fmt.Sprintf("系统温度 %.0f°C", value)
		}
		if value >= 80 {
			return "warning", fmt.Sprintf("系统温度 %.0f°C", value)
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
	case "disk.ata.pending_sectors":
		if value > 0 {
			return "critical", fmt.Sprintf("待处理扇区 %.0f", value)
		}
	case "disk.ata.offline_uncorrectable":
		if value > 0 {
			return "critical", fmt.Sprintf("离线不可校正扇区 %.0f", value)
		}
	case "disk.ata.reported_uncorrectable":
		if value > 0 {
			return "critical", fmt.Sprintf("已报告不可校正错误 %.0f", value)
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
