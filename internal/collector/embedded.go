package collector

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/buildinfo"
	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

type capabilityEvidence struct {
	halReachable     bool
	dockerMapped     bool
	dockerReachable  bool
	dockerRestricted bool
	smartAttempted   bool
	smartRestricted  bool
	processReachable bool
}

type Embedded struct {
	store      *store.Store
	logger     *slog.Logger
	deviceID   string
	advanced   AdvancedConfig
	hal        *HALCollector
	docker     *DockerCollector
	upstream   *Upstream
	dataPath   string
	syncAlerts func(context.Context) error
}

func (e *Embedded) DeviceID() string               { return e.deviceID }
func (e *Embedded) Docker() *DockerCollector       { return e.docker }
func (e *Embedded) SetUpstream(upstream *Upstream) { e.upstream = upstream }

func NewEmbedded(ctx context.Context, st *store.Store, logger *slog.Logger, syncAlerts func(context.Context) error) (*Embedded, error) {
	hostname := envValue("LAZYCAT_BOX_NAME", "")
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	name := envValue("WATCHCAT_LOCAL_DEVICE_NAME", hostname)
	deviceID, err := st.EnsureLocalDevice(ctx, name, hostname, runtime.GOOS+"/"+runtime.GOARCH, buildinfo.Version, []string{
		"collector.embedded", "host.metrics", "filesystem.metrics", "advanced.whitelist",
	})
	if err != nil {
		return nil, err
	}
	if err := st.RemoveLegacyEmbeddedFilesystemSeries(ctx, deviceID); err != nil {
		return nil, err
	}
	halCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	hal, halErr := NewHALCollector(halCtx)
	cancel()
	if halErr != nil {
		logger.Warn("connect LazyCat HAL read-only collector", "error", halErr)
	}
	return &Embedded{
		store: st, logger: logger, deviceID: deviceID, advanced: AdvancedConfigFromEnv(),
		hal: hal, docker: NewDockerCollector(envValue("WATCHCAT_DOCKER_SOCKET", defaultDockerSocket)),
		dataPath: envValue("WATCHCAT_HOST_DATA_PATH", "/lzcapp/var"), syncAlerts: syncAlerts,
	}, nil
}

func (e *Embedded) Close() error {
	if e == nil || e.hal == nil {
		return nil
	}
	return e.hal.Close()
}

func (e *Embedded) Run(ctx context.Context) {
	jobs := []struct {
		name     string
		delay    time.Duration
		interval func(store.OperationalSettings) int
		collect  func(context.Context)
	}{
		{"system", 0, func(s store.OperationalSettings) int { return s.SystemIntervalSeconds }, e.collectSystem},
		{"runtime", 2 * time.Second, func(s store.OperationalSettings) int { return s.RuntimeIntervalSeconds }, e.collectRuntime},
		{"storage", 5 * time.Second, func(s store.OperationalSettings) int { return s.StorageIntervalSeconds }, e.collectStorage},
		{"advanced", 10 * time.Second, func(s store.OperationalSettings) int { return s.AdvancedIntervalSeconds }, e.collectAdvanced},
	}
	for _, job := range jobs {
		go e.runScheduled(ctx, job.name, job.delay, job.interval, job.collect)
	}
	<-ctx.Done()
}

func (e *Embedded) runScheduled(ctx context.Context, name string, initialDelay time.Duration, interval func(store.OperationalSettings) int, collect func(context.Context)) {
	if initialDelay > 0 {
		timer := time.NewTimer(initialDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	for {
		started := time.Now()
		collect(ctx)
		seconds := interval(e.store.OperationalSettings(ctx))
		if seconds < 1 {
			e.logger.Error("collector interval is invalid", "category", name, "seconds", seconds)
			seconds = 30
		}
		wait := time.Duration(seconds)*time.Second - time.Since(started)
		if wait < time.Second {
			wait = time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (e *Embedded) collectSystem(ctx context.Context) {
	now := time.Now().UTC()
	batch, err := CollectSystem(e.deviceID, now)
	if err != nil {
		e.logger.Warn("embedded collector system metrics", "error", err)
		return
	}
	if e.hal != nil {
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		points, halErr := e.hal.Collect(callCtx, now)
		cancel()
		if halErr != nil {
			e.logger.Warn("embedded collector HAL metrics", "error", halErr)
		} else {
			batch.Points = append(batch.Points, points...)
		}
	}
	e.ingest(ctx, batch, false)
}

func (e *Embedded) collectRuntime(ctx context.Context) {
	now := time.Now().UTC()
	batch := protocol.MetricBatch{DeviceID: e.deviceID}
	if e.docker.Available() {
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		points, dockerErr := e.docker.Collect(callCtx, now)
		cancel()
		batch.Points = append(batch.Points, points...)
		if dockerErr != nil {
			e.logger.Warn("embedded collector container metrics", "error", dockerErr)
		}
		processCtx, processCancel := context.WithTimeout(ctx, 20*time.Second)
		processes, processErr := e.docker.CollectProcesses(processCtx, now)
		processCancel()
		if processErr != nil {
			e.logger.Warn("embedded collector host processes", "error", processErr)
		} else {
			batch.Processes = processes
			batch.ProcessesCollected = true
		}
	}
	e.ingest(ctx, batch, true)
}

func (e *Embedded) collectStorage(ctx context.Context) {
	now := time.Now().UTC()
	batch := protocol.MetricBatch{
		DeviceID: e.deviceID,
		Points: CollectFilesystem(now, e.dataPath, map[string]string{
			"mount": "LazyCat data", "scope": "host-data-volume", "path": e.dataPath,
		}),
	}
	if e.docker.Available() {
		callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		points, warnings := e.docker.CollectDiskInventory(callCtx, now)
		cancel()
		batch.Points = append(batch.Points, points...)
		callCtx, cancel = context.WithTimeout(ctx, 60*time.Second)
		filesystemPoints, filesystemWarnings := e.docker.CollectMountedFilesystems(callCtx, now)
		cancel()
		batch.Points = append(batch.Points, filesystemPoints...)
		callCtx, cancel = context.WithTimeout(ctx, 60*time.Second)
		btrfsPoints, btrfsWarnings := e.docker.CollectBtrfsUsage(callCtx, now)
		cancel()
		batch.Points = append(batch.Points, btrfsPoints...)
		warnings = append(warnings, filesystemWarnings...)
		warnings = append(warnings, btrfsWarnings...)
		if len(warnings) > 0 {
			e.logger.Warn("embedded collector storage inventory partially degraded", "warnings", warnings)
		}
	}
	e.ingest(ctx, batch, false)
}

func (e *Embedded) collectAdvanced(ctx context.Context) {
	now := time.Now().UTC()
	batch := protocol.MetricBatch{DeviceID: e.deviceID}
	var warnings []string
	evidence := capabilityEvidence{dockerMapped: e.docker.Available()}
	if e.hal != nil {
		evidence.halReachable = true
	} else {
		warnings = append(warnings, "hal fan: LazyCat HAL connection unavailable")
	}
	if evidence.dockerMapped {
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		runtimePoints, dockerErr := e.docker.Collect(callCtx, now)
		cancel()
		evidence.dockerReachable = dockerErr == nil || len(runtimePoints) > 0
		if dockerErr != nil {
			evidence.dockerRestricted = permissionDenied(dockerErr)
			warnings = append(warnings, "docker runtime: "+dockerErr.Error())
		}
		processCtx, processCancel := context.WithTimeout(ctx, 20*time.Second)
		_, processErr := e.docker.CollectProcesses(processCtx, now)
		processCancel()
		if processErr != nil {
			warnings = append(warnings, "host processes: "+processErr.Error())
		} else {
			evidence.processReachable = true
		}
		evidence.smartAttempted = true
		callCtx, cancel = context.WithTimeout(ctx, 60*time.Second)
		smartPoints, smartWarnings := e.docker.CollectSMART(callCtx, now)
		cancel()
		batch.Points = append(batch.Points, smartPoints...)
		warnings = append(warnings, smartWarnings...)
		for _, warning := range smartWarnings {
			if permissionDenied(errors.New(warning)) {
				evidence.smartRestricted = true
				break
			}
		}
		callCtx, cancel = context.WithTimeout(ctx, 60*time.Second)
		btrfsPoints, btrfsWarnings := e.docker.CollectBtrfsHealth(callCtx, now)
		cancel()
		batch.Points = append(batch.Points, btrfsPoints...)
		warnings = append(warnings, btrfsWarnings...)
	}
	callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	points, advancedWarnings := CollectAdvanced(callCtx, e.advanced, now)
	cancel()
	points = append(points, collectTemperatureMetrics(ctx, now)...)
	batch.Points = append(batch.Points, points...)
	warnings = append(warnings, advancedWarnings...)
	if len(warnings) > 0 {
		e.logger.Warn("embedded collector advanced metrics partially degraded", "warnings", warnings)
	}
	e.recordCapabilities(ctx, now, batch.Points, warnings, evidence)
	e.ingest(ctx, batch, false)
}

func (e *Embedded) ingest(ctx context.Context, batch protocol.MetricBatch, includeRuntimeState bool) {
	if err := e.store.IngestMetrics(ctx, batch); err != nil {
		e.logger.Warn("embedded collector metric ingest", "error", err)
		return
	}
	if e.upstream != nil {
		if includeRuntimeState {
			e.attachRuntimeApplications(ctx, &batch)
			e.attachRuntimeUsers(ctx, &batch)
		}
		sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		e.upstream.Send(sendCtx, batch)
		cancel()
	}
	if e.syncAlerts != nil {
		_ = e.syncAlerts(ctx)
	}
}

func (e *Embedded) attachRuntimeUsers(ctx context.Context, batch *protocol.MetricBatch) {
	items, err := e.store.ListRuntimeUsers(ctx)
	if err != nil {
		e.logger.Warn("read runtime users for upstream", "error", err)
		return
	}
	for _, u := range items {
		if u.DeviceID != e.deviceID {
			continue
		}
		item := protocol.RuntimeUser{UserID: u.UserID, Nickname: u.Nickname, Role: u.Role, AppInstallPermission: u.AppInstallPermission, AppAccessNoLimit: u.AppAccessNoLimit, AllowedAppIDs: u.AllowedAppIDs, Online: u.Online, ActiveDevices: u.ActiveDevices, TotalDevices: u.TotalDevices}
		for _, d := range u.Devices {
			item.Devices = append(item.Devices, protocol.RuntimeUserDevice{
				ID: d.ID, Name: d.Name, Model: d.Model, RemarkName: d.RemarkName, DeviceAPIURL: d.DeviceAPIURL,
				IsMobile: d.IsMobile, IsTV: d.IsTV, Lang: d.Lang, TimeZone: d.TimeZone, IsWifi: d.IsWifi,
				Online: d.Online, BindingTime: d.BindingTime, LoginTime: d.LoginTime,
			})
		}
		batch.Users = append(batch.Users, item)
	}
	batch.UsersCollected = len(batch.Users) > 0
}

func (e *Embedded) attachRuntimeApplications(ctx context.Context, batch *protocol.MetricBatch) {
	items, err := e.store.ListRuntimeApplications(ctx)
	if err != nil {
		e.logger.Warn("read runtime applications for upstream", "error", err)
		return
	}
	for _, item := range items {
		if item.DeviceID != e.deviceID {
			continue
		}
		batch.Applications = append(batch.Applications, protocol.RuntimeApplication{
			DeployID: item.DeployID, AppID: item.AppID, Title: item.Title, Version: item.Version,
			InstallStatus: item.InstallStatus, InstanceStatus: item.InstanceStatus,
			Domain: item.Domain, Builtin: item.Builtin, UserID: item.UserID, UserName: item.UserName,
		})
	}
	batch.ApplicationsCollected = len(batch.Applications) > 0
}

func (e *Embedded) recordCapabilities(ctx context.Context, now time.Time, points []protocol.MetricPoint, warnings []string, evidence capabilityEvidence) {
	has := func(prefixes ...string) bool { return metricPrefixPresent(points, prefixes...) }
	var nvmeDevices []string
	for _, device := range e.advanced.SmartDevices {
		if strings.HasPrefix(device, "/dev/nvme") {
			nvmeDevices = append(nvmeDevices, device)
		}
	}
	smartWarnings := warningsForTargets(warnings, "smart ", e.advanced.SmartDevices)
	for _, warning := range warnings {
		if strings.HasPrefix(warning, "docker smart") {
			smartWarnings = append(smartWarnings, warning)
		}
	}
	nvmeWarnings := warningsForTargets(warnings, "smart ", nvmeDevices)
	for _, warning := range warnings {
		if strings.HasPrefix(warning, "docker smart /dev/nvme") {
			nvmeWarnings = append(nvmeWarnings, warning)
		}
	}
	btrfsWarnings := warningsForTargets(warnings, "btrfs ", e.advanced.BtrfsMounts)
	items := []store.CapabilityStatus{
		{Capability: "system.metrics", Status: "available", Detail: "读取宿主机共享 /proc 指标", CheckedAt: now},
		capabilityFromConfig("system.metrics.gopsutil", true, has("system.cpu.usage") && has("system.load.5m"), warnings, "gopsutil 扩展指标不可用", now),
		{Capability: "filesystem.lazycat_data", Status: "available", Detail: "校准路径 " + e.dataPath + "，对应 LazyCat 数据存储池", CheckedAt: now},
		{Capability: "network.metrics", Status: statusOf(has("network.")), Detail: "读取网络命名空间累计流量", CheckedAt: now},
		optionalCapability("process.metrics", evidence.processReachable, "通过受控 Docker helper 只读采集宿主机进程", "当前无法读取宿主机 PID namespace", "error", now),
		optionalCapability("system.temperature", has("system.temperature"), "读取 /sys 硬件温度传感器", "当前运行环境未暴露硬件温度传感器", "unsupported", now),
		accessCapability("system.fan", evidence.halReachable, true, false, warnings, "hal fan:", "LazyCat HAL GetFanRpm 只读接口", "LazyCat HAL 调用失败", now),
		accessCapability("container.runtime", evidence.dockerReachable, evidence.dockerMapped, evidence.dockerRestricted, warnings, "docker runtime:", "LazyCat Docker socket，仅调用 List/Stats", "只读 LazyCat Docker socket 未授权或不可用", now),
	}
	items = append(items,
		smartCapability("smart", evidence.smartAttempted || len(e.advanced.SmartDevices) > 0, has("disk.temperature", "disk.power_on_hours", "disk.nvme.", "disk.ata."), evidence.smartRestricted, smartWarnings, "通过短生命周期、无网络、只读根文件系统的 Docker helper 读取 SMART；仅附加 SYS_RAWIO 与单设备映射", now),
		smartCapability("nvme", evidence.smartAttempted || len(nvmeDevices) > 0, has("disk.nvme."), evidence.smartRestricted, nvmeWarnings, "通过受控 Docker helper 读取 NVMe SMART", now),
		capabilityFromConfig("btrfs", evidence.dockerMapped || len(e.advanced.BtrfsMounts) > 0, has("btrfs."), btrfsWarnings, "通过受控 Docker helper 只读映射白名单挂载点，采集 usage、device stats 与 scrub 状态", now),
	)
	if e.advanced.LPKStatusFile != "" {
		items = append(items, capabilityFromConfig("lpk.runtime.file", true, has("lpk."), warnings, "状态文件不可用", now))
	}
	for i := range items {
		items[i].DeviceID = e.deviceID
	}
	if err := e.store.SetCapabilityStatuses(ctx, e.deviceID, items); err != nil {
		e.logger.Warn("persist collector capabilities", "error", err)
	}
}

func smartCapability(name string, attempted, available, restricted bool, warnings []string, detail string, now time.Time) store.CapabilityStatus {
	if available {
		if len(warnings) > 0 {
			detail += "；部分设备失败：" + warnings[0]
		}
		return store.CapabilityStatus{Capability: name, Status: "available", Detail: detail, CheckedAt: now}
	}
	if restricted {
		detail = "LazyCat Docker 拒绝受控块设备采集"
		if len(warnings) > 0 {
			detail = warnings[0]
		}
		return store.CapabilityStatus{Capability: name, Status: "restricted", Detail: detail, CheckedAt: now}
	}
	if attempted {
		detail = "未获得受支持的 SMART 数据"
		if len(warnings) > 0 {
			detail = warnings[0]
		}
		return store.CapabilityStatus{Capability: name, Status: "error", Detail: detail, CheckedAt: now}
	}
	return store.CapabilityStatus{Capability: name, Status: "unsupported", Detail: "未启用 SMART 采集", CheckedAt: now}
}

func metricPrefixPresent(points []protocol.MetricPoint, prefixes ...string) bool {
	for _, point := range points {
		for _, prefix := range prefixes {
			if strings.HasPrefix(point.Name, prefix) {
				return true
			}
		}
	}
	return false
}

func statusOf(ok bool) string {
	if ok {
		return "available"
	}
	return "error"
}

func optionalCapability(name string, available bool, detail, fallback, unavailableStatus string, now time.Time) store.CapabilityStatus {
	if available {
		return store.CapabilityStatus{Capability: name, Status: "available", Detail: detail, CheckedAt: now}
	}
	return store.CapabilityStatus{Capability: name, Status: unavailableStatus, Detail: fallback, CheckedAt: now}
}

func accessCapability(name string, reachable, mapped, restricted bool, warnings []string, warningPrefix, detail, fallback string, now time.Time) store.CapabilityStatus {
	item := store.CapabilityStatus{Capability: name, Status: "restricted", Detail: fallback, CheckedAt: now}
	warningDetail := ""
	for _, warning := range warnings {
		if strings.HasPrefix(warning, warningPrefix) {
			warningDetail = warning
			break
		}
	}
	if reachable {
		item.Status, item.Detail = "available", detail
		if warningDetail != "" {
			item.Detail += "；部分采集失败：" + warningDetail
		}
		return item
	}
	if !mapped {
		return item
	}
	if restricted {
		if warningDetail != "" {
			item.Detail = warningDetail
		}
		return item
	}
	item.Status = "error"
	if warningDetail != "" {
		item.Detail = warningDetail
	}
	return item
}

func permissionDenied(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "permission denied") ||
		strings.Contains(message, "operation not permitted") ||
		strings.Contains(message, "unauthorized") ||
		strings.Contains(message, "forbidden")
}

func warningsForTargets(warnings []string, prefix string, targets []string) []string {
	var matched []string
	for _, warning := range warnings {
		for _, target := range targets {
			if strings.HasPrefix(warning, prefix+target+": ") {
				matched = append(matched, warning)
				break
			}
		}
	}
	return matched
}

func capabilityFromConfig(name string, configured, available bool, warnings []string, fallback string, now time.Time) store.CapabilityStatus {
	item := store.CapabilityStatus{Capability: name, Status: "restricted", Detail: fallback, CheckedAt: now}
	if configured {
		warningPrefix := strings.Split(name, ".")[0]
		if name == "nvme" {
			warningPrefix = "smart"
		}
		for _, warning := range warnings {
			if strings.HasPrefix(warning, warningPrefix) {
				item.Status, item.Detail = "error", warning
				return item
			}
		}
	}
	if available {
		item.Status, item.Detail = "available", "只读采集已验证"
		return item
	}
	if configured {
		item.Status = "error"
	}
	return item
}

func envValue(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
