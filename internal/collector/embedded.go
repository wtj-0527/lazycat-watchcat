package collector

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

const EmbeddedVersion = "1.2.0"

type Embedded struct {
	store      *store.Store
	logger     *slog.Logger
	deviceID   string
	advanced   AdvancedConfig
	syncAlerts func(context.Context) error
}

func NewEmbedded(ctx context.Context, st *store.Store, logger *slog.Logger, syncAlerts func(context.Context) error) (*Embedded, error) {
	hostname := envValue("LAZYCAT_BOX_NAME", "")
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	name := envValue("MAOYAN_LOCAL_DEVICE_NAME", hostname)
	deviceID, err := st.EnsureLocalDevice(ctx, name, hostname, runtime.GOOS+"/"+runtime.GOARCH, EmbeddedVersion, []string{
		"collector.embedded", "host.metrics", "filesystem.metrics", "advanced.whitelist",
	})
	if err != nil {
		return nil, err
	}
	return &Embedded{store: st, logger: logger, deviceID: deviceID, advanced: AdvancedConfigFromEnv(), syncAlerts: syncAlerts}, nil
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
	batch, err := Collect(e.deviceID, now)
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
	}
	if err := e.store.IngestMetrics(ctx, batch); err != nil {
		e.logger.Warn("embedded collector metric ingest", "error", err)
		return
	}
	if e.syncAlerts != nil {
		_ = e.syncAlerts(ctx)
	}
}

func envValue(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
