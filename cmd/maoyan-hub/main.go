package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/wtj-0527/lazycat-maoyan/internal/api"
	"github.com/wtj-0527/lazycat-maoyan/internal/config"
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
	srv := &http.Server{Addr: cfg.Addr, Handler: api.New(st, cfg.WebDir, cfg.PairingTTL).Handler(), ReadHeaderTimeout: 5e9, IdleTimeout: 60e9, MaxHeaderBytes: 1 << 20}
	logger.Info("maoyan hub started", "addr", cfg.Addr, "database", cfg.DatabasePath(), "protocol", "v1")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
