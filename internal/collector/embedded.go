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
	dataPath   string
	syncAlerts func(context.Context) error
}

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
	return &Embedded{store: st, logger: logger, deviceID: deviceID, advanced: AdvancedConfigFromEnv(), dataPath: envValue("MAOYAN_HOST_DATA_PATH", "/lzcapp/var"), syncAlerts: syncAlerts}, nil
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
	if includeAdvanced {
		callCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		points, warnings := CollectAdvanced(callCtx, e.advanced, now)
		cancel()
		batch.Points = append(batch.Points, points...)
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
	has := func(prefix string) bool {
		for _, point := range points {
			if strings.HasPrefix(point.Name, prefix) {
				return true
			}
		}
		return false
	}
	items := []store.CapabilityStatus{
		{Capability: "system.metrics", Status: "available", Detail: "读取宿主机共享 /proc 指标", CheckedAt: now},
		{Capability: "filesystem.lazycat_data", Status: "available", Detail: "校准路径 " + e.dataPath + "，对应 LazyCat 数据存储池", CheckedAt: now},
		{Capability: "network.metrics", Status: statusOf(has("network.")), Detail: "读取网络命名空间累计流量", CheckedAt: now},
	}
	items = append(items,
		capabilityFromConfig("smart", len(e.advanced.SmartDevices) > 0, has("disk."), warnings, "宿主机块设备未映射给应用；当前不生成 SMART 健康结论", now),
		capabilityFromConfig("btrfs", len(e.advanced.BtrfsMounts) > 0, has("btrfs."), warnings, "宿主机 Btrfs 挂载未映射给应用；当前不生成 Btrfs 健康结论", now),
		capabilityFromConfig("lpk.runtime", e.advanced.LPKStatusFile != "", has("lpk."), warnings, "LazyCat Runtime 未提供只读状态源；当前不生成应用健康结论", now),
	)
	for i := range items {
		items[i].DeviceID = e.deviceID
	}
	if err := e.store.SetCapabilityStatuses(ctx, e.deviceID, items); err != nil {
		e.logger.Warn("persist collector capabilities", "error", err)
	}
}

func statusOf(ok bool) string {
	if ok {
		return "available"
	}
	return "unavailable"
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
