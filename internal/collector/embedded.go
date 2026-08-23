package collector

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/buildinfo"
	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

type Embedded struct {
	store      *store.Store
	logger     *slog.Logger
	deviceID   string
	advanced   AdvancedConfig
	hal        *HALCollector
	docker     *DockerCollector
	dataPath   string
	syncAlerts func(context.Context) error
}

func (e *Embedded) DeviceID() string { return e.deviceID }

func NewEmbedded(ctx context.Context, st *store.Store, logger *slog.Logger, syncAlerts func(context.Context) error) (*Embedded, error) {
	hostname := envValue("LAZYCAT_BOX_NAME", "")
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	name := envValue("MAOYAN_LOCAL_DEVICE_NAME", hostname)
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
		hal: hal, docker: NewDockerCollector(envValue("MAOYAN_DOCKER_SOCKET", defaultDockerSocket)),
		dataPath: envValue("MAOYAN_HOST_DATA_PATH", "/lzcapp/var"), syncAlerts: syncAlerts,
	}, nil
}

func (e *Embedded) Close() error {
	if e == nil || e.hal == nil {
		return nil
	}
	return e.hal.Close()
}

func (e *Embedded) Run(ctx context.Context) {
	basicTicker := time.NewTicker(30 * time.Second)
	advancedTicker := time.NewTicker(5 * time.Minute)
	defer basicTicker.Stop()
	defer advancedTicker.Stop()
	e.collect(ctx, true)
	for {
		select {
		case <-ctx.Done():
			return
		case <-basicTicker.C:
			e.collect(ctx, false)
		case <-advancedTicker.C:
			e.collect(ctx, true)
		}
	}
}

func (e *Embedded) collect(ctx context.Context, includeAdvanced bool) {
	now := time.Now().UTC()
	batch, err := CollectWithFilesystem(e.deviceID, now, e.dataPath, map[string]string{"mount": "LazyCat data", "scope": "host-data-volume", "path": e.dataPath})
	if err != nil {
		e.logger.Warn("embedded collector basic metrics", "error", err)
		return
	}
	var warnings []string
	if e.hal != nil {
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		points, halErr := e.hal.Collect(callCtx, now)
		cancel()
		if halErr != nil {
			warnings = append(warnings, "hal fan: "+halErr.Error())
		} else {
			batch.Points = append(batch.Points, points...)
		}
	} else if includeAdvanced {
		warnings = append(warnings, "hal fan: LazyCat HAL connection unavailable")
	}
	if e.docker.Available() {
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		points, dockerErr := e.docker.Collect(callCtx, now)
		cancel()
		batch.Points = append(batch.Points, points...)
		if dockerErr != nil {
			warnings = append(warnings, "docker runtime: "+dockerErr.Error())
		}
	} else if includeAdvanced {
		warnings = append(warnings, "docker runtime: read-only LazyCat Docker socket unavailable")
	}
	if includeAdvanced {
		callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		points, advancedWarnings := CollectAdvanced(callCtx, e.advanced, now)
		cancel()
		batch.Points = append(batch.Points, points...)
		warnings = append(warnings, advancedWarnings...)
		if len(warnings) > 0 {
			e.logger.Warn("embedded collector partially degraded", "warnings", warnings)
		}
		e.recordCapabilities(ctx, now, batch.Points, warnings)
	}
	if err := e.store.IngestMetrics(ctx, batch); err != nil {
		e.logger.Warn("embedded collector metric ingest", "error", err)
		return
	}
	if e.syncAlerts != nil {
		_ = e.syncAlerts(ctx)
	}
}

func (e *Embedded) recordCapabilities(ctx context.Context, now time.Time, points []protocol.MetricPoint, warnings []string) {
	has := func(prefixes ...string) bool { return metricPrefixPresent(points, prefixes...) }
	items := []store.CapabilityStatus{
		{Capability: "system.metrics", Status: "available", Detail: "读取宿主机共享 /proc 指标", CheckedAt: now},
		capabilityFromConfig("system.metrics.gopsutil", true, has("system.cpu.usage") && has("system.load.5m"), warnings, "gopsutil 扩展指标不可用", now),
		{Capability: "filesystem.lazycat_data", Status: "available", Detail: "校准路径 " + e.dataPath + "，对应 LazyCat 数据存储池", CheckedAt: now},
		{Capability: "network.metrics", Status: statusOf(has("network.")), Detail: "读取网络命名空间累计流量", CheckedAt: now},
		optionalCapability("system.temperature", has("system.temperature"), "读取 /sys 硬件温度传感器", "当前运行环境未暴露硬件温度传感器", now),
		optionalCapability("system.fan", has("system.fan.rpm"), "LazyCat HAL GetFanRpm 只读接口", "LazyCat HAL 未返回风扇转速", now),
		optionalCapability("container.runtime", has("container."), "LazyCat Docker socket，仅调用 List/Stats", "只读 LazyCat Docker socket 未授权或不可用", now),
	}
	items = append(items,
		capabilityFromConfig("smart", len(e.advanced.SmartDevices) > 0, has("disk.temperature", "disk.power_on_hours", "disk.nvme.", "disk.ata."), warnings, "宿主机块设备未映射给应用；当前不生成 SMART 健康结论", now),
		capabilityFromConfig("btrfs", len(e.advanced.BtrfsMounts) > 0, has("btrfs."), warnings, "宿主机 Btrfs 挂载未映射给应用；当前不生成 Btrfs 健康结论", now),
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
	return "unavailable"
}

func optionalCapability(name string, available bool, detail, fallback string, now time.Time) store.CapabilityStatus {
	if available {
		return store.CapabilityStatus{Capability: name, Status: "available", Detail: detail, CheckedAt: now}
	}
	return store.CapabilityStatus{Capability: name, Status: "unavailable", Detail: fallback, CheckedAt: now}
}

func capabilityFromConfig(name string, configured, available bool, warnings []string, fallback string, now time.Time) store.CapabilityStatus {
	item := store.CapabilityStatus{Capability: name, Status: "unavailable", Detail: fallback, CheckedAt: now}
	if available {
		item.Status, item.Detail = "available", "只读采集已验证"
		return item
	}
	if configured {
		item.Status = "degraded"
		for _, warning := range warnings {
			if strings.HasPrefix(warning, strings.Split(name, ".")[0]) {
				item.Detail = warning
				break
			}
		}
	}
	return item
}

func envValue(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
