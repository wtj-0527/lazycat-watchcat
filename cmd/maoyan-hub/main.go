package main

import (
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"

	"github.com/wtj-0527/lazycat-maoyan/internal/api"
	"github.com/wtj-0527/lazycat-maoyan/internal/config"
	"github.com/wtj-0527/lazycat-maoyan/internal/pki"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	st, err := store.Open(cfg.DatabasePath())
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer st.Close()
	ca, err := pki.LoadOrCreate(cfg.DataDir + "/pki")
	if err != nil {
		logger.Error("open PKI", "error", err)
		os.Exit(1)
	}
	handlers := api.New(st, ca, cfg.WebDir, cfg.PairingTTL)
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
	logger.Info("maoyan hub started", "addr", cfg.Addr, "database", cfg.DatabasePath(), "protocol", "v1")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
