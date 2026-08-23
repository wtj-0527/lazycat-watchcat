package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/api"
	"github.com/wtj-0527/lazycat-maoyan/internal/backup"
	"github.com/wtj-0527/lazycat-maoyan/internal/buildinfo"
	"github.com/wtj-0527/lazycat-maoyan/internal/collector"
	"github.com/wtj-0527/lazycat-maoyan/internal/config"
	"github.com/wtj-0527/lazycat-maoyan/internal/notify"
	"github.com/wtj-0527/lazycat-maoyan/internal/pki"
	"github.com/wtj-0527/lazycat-maoyan/internal/runtimeapps"
	"github.com/wtj-0527/lazycat-maoyan/internal/scheduler"
	"github.com/wtj-0527/lazycat-maoyan/internal/stability"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	backupManager := backup.New(cfg.DatabasePath(), filepath.Join(cfg.DataDir, "backups"), buildinfo.Version)
	if err := backupManager.Prepare(context.Background()); err != nil {
		logger.Error("prepare database", "error", err)
		os.Exit(1)
	}
	st, err := store.Open(cfg.DatabasePath())
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()
	backupManager.Attach(st)
	if err := backupManager.MarkVersion(); err != nil {
		logger.Error("mark database version", "error", err)
		os.Exit(1)
	}
	ca, err := pki.LoadOrCreate(cfg.DataDir + "/pki")
	if err != nil {
		logger.Error("open PKI", "error", err)
		os.Exit(1)
	}
	handlers := api.New(st, ca, cfg.WebDir, cfg.PairingTTL)
	stabilityMonitor := stability.New(st, logger, time.Minute)
	// Replace the process image in place so database restore does not depend on
	// an external restart policy. Go and SQLite descriptors are close-on-exec;
	// the new process applies the staged restore before opening the database.
	handlers.ConfigureOperations(backupManager, stabilityMonitor, func() {
		if err := syscall.Exec("/proc/self/exe", os.Args, os.Environ()); err != nil {
			logger.Error("restart after restore", "error", err)
			os.Exit(75)
		}
	})
	go stabilityMonitor.Run(context.Background())
	embedded, err := collector.NewEmbedded(context.Background(), st, logger, handlers.SyncAlerts)
	if err != nil {
		logger.Error("start embedded collector", "error", err)
		os.Exit(1)
	}
	runtimeCtx, runtimeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	runtimeSource, runtimeErr := runtimeapps.New(runtimeCtx)
	runtimeCancel()
	if runtimeErr != nil {
		logger.Warn("connect LazyCat package manager", "error", runtimeErr)
	} else {
		defer runtimeSource.Close()
		handlers.ConfigureRuntimeApps(runtimeSource, embedded.DeviceID())
	}
	go embedded.Run(context.Background())
	notifier := notify.NewLazyCat(st, logger)
	inspectionScheduler := scheduler.NewInspectionScheduler(handlers, st, logger)
	go inspectionScheduler.Run(context.Background())
	go func() {
		alertTicker := time.NewTicker(30 * time.Second)
		retentionTicker := time.NewTicker(time.Hour)
		defer alertTicker.Stop()
		defer retentionTicker.Stop()
		_ = handlers.SyncAlerts(context.Background())
		notifier.ProcessPending(context.Background(), 20)
		for {
			select {
			case <-alertTicker.C:
				if err := handlers.SyncAlerts(context.Background()); err != nil {
					logger.Warn("sync alerts", "error", err)
				}
				notifier.ProcessPending(context.Background(), 20)
			case now := <-retentionTicker.C:
				result, err := st.RunRetention(context.Background(), now)
				if err != nil {
					logger.Warn("retention worker", "error", err)
				} else {
					logger.Info("retention worker completed", "rollups", result.RollupBuckets, "rawDeleted", result.RawDeleted)
				}
			}
		}
	}()
	identity, err := ca.EnsureServerIdentity(cfg.DataDir+"/pki", cfg.CollectorHosts)
	if err != nil {
		logger.Error("create collector server identity", "error", err)
		os.Exit(1)
	}
	collectorServer := &http.Server{
		Addr:              cfg.CollectorAddr,
		Handler:           handlers.CollectorHandler(),
		ReadHeaderTimeout: 5e9,
		IdleTimeout:       60e9,
		MaxHeaderBytes:    1 << 20,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  ca.CertPool(),
		},
	}
	go func() {
		logger.Info("collector mTLS endpoint started", "addr", cfg.CollectorAddr)
		if err := collectorServer.ListenAndServeTLS(identity.CertificateFile, identity.PrivateKeyFile); err != nil && err != http.ErrServerClosed {
			logger.Error("collector endpoint stopped", "error", err)
			os.Exit(1)
		}
	}()
	srv := &http.Server{Addr: cfg.Addr, Handler: handlers.Handler(), ReadHeaderTimeout: 5e9, IdleTimeout: 60e9, MaxHeaderBytes: 1 << 20}
	logger.Info("maoyan hub started", "addr", cfg.Addr, "database", cfg.DatabasePath(), "version", buildinfo.Version, "protocol", "v1")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
