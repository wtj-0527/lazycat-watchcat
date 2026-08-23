package config

import (
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	Addr       string
	DataDir    string
	WebDir     string
	PairingTTL time.Duration
}

func Load() Config {
	dataDir := getenv("MAOYAN_DATA_DIR", "/lzcapp/var/data")
	if _, err := os.Stat("web"); err == nil && os.Getenv("MAOYAN_DATA_DIR") == "" {
		dataDir = "data"
	}
	return Config{
		Addr:       getenv("MAOYAN_ADDR", ":8080"),
		DataDir:    dataDir,
		WebDir:     getenv("MAOYAN_WEB_DIR", defaultWebDir()),
		PairingTTL: 10 * time.Minute,
	}
}

func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, "maoyan.db") }

func defaultWebDir() string {
	if _, err := os.Stat("web"); err == nil {
		return "web"
	}
	return "/lzcapp/pkg/content/web"
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
