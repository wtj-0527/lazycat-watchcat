package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/collector"
)

const version = "1.0.0"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	var status atomic.Value
	status.Store("starting")
	healthAddr := env("MAOYAN_HEALTH_ADDR", ":8090")
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/v1/health", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"` + status.Load().(string) + `","version":"` + version + `"}`))
		})
		if err := http.ListenAndServe(healthAddr, mux); err != nil {
			logger.Error("health server stopped", "error", err)
		}
	}()
	dataDir := env("MAOYAN_COLLECTOR_DATA_DIR", "/lzcapp/var/data")
	hub := os.Getenv("MAOYAN_HUB_URL")
	if hub == "" {
		status.Store("unconfigured")
		logger.Warn("collector is waiting for MAOYAN_HUB_URL configuration")
		select {}
	}
	hostname, _ := os.Hostname()
	name := env("MAOYAN_DEVICE_NAME", hostname)
	credsPath := filepath.Join(dataDir, "credentials.json")
	creds, err := collector.LoadCredentials(credsPath)
	if err != nil {
		code := os.Getenv("MAOYAN_PAIRING_CODE")
		if code == "" {
			status.Store("unpaired")
			logger.Warn("collector is waiting for MAOYAN_PAIRING_CODE")
			select {}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		creds, err = collector.Pair(ctx, http.DefaultClient, hub, code, name, hostname, version)
		cancel()
		if err != nil {
			logger.Error("pair collector", "error", err)
			os.Exit(1)
		}
		if err := collector.SaveCredentials(credsPath, creds); err != nil {
			logger.Error("save credentials", "error", err)
			os.Exit(1)
		}
		logger.Info("collector paired", "device_id", creds.DeviceID, "certificate_expires_at", creds.CertificateExpiresAt)
	}
	collectorURL := env("MAOYAN_COLLECTOR_URL", hub)
	metricClient, err := collector.NewMTLSClient(creds)
	if err != nil {
		logger.Error("create mTLS client", "error", err)
		os.Exit(1)
	}
	status.Store("online")
	queue := collector.NewQueue(filepath.Join(dataDir, "metrics.queue.json"), 2048)
	interval := 30 * time.Second
	if raw := os.Getenv("MAOYAN_COLLECT_INTERVAL"); raw != "" {
		if parsed, e := time.ParseDuration(raw); e == nil && parsed >= 10*time.Second {
			interval = parsed
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		batch, err := collector.Collect(creds.DeviceID, time.Now().UTC())
		if err == nil {
			_ = queue.Append(batch)
		}
		flush(logger, queue, metricClient, collectorURL, creds)
		<-ticker.C
	}
}
func flush(logger *slog.Logger, q *collector.Queue, client *http.Client, hub string, creds collector.Credentials) {
	for i := 0; i < 20; i++ {
		batch, ok, err := q.Peek()
		if err != nil || !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = collector.Send(ctx, client, hub, creds, batch)
		cancel()
		if err != nil {
			logger.Warn("metric delivery deferred", "error", err)
			return
		}
		if err := q.Pop(); err != nil {
			return
		}
	}
}
func env(k, v string) string {
	if x := os.Getenv(k); x != "" {
		return x
	}
	return v
}
