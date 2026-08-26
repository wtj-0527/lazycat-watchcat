package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Addr           string
	CollectorAddr  string
	CollectorHosts []string
	DataDir        string
	WebDir         string
	PairingTTL     time.Duration
}

func Load() Config {
	dataDir := getenv("WATCHCAT_DATA_DIR", "/lzcapp/var/data")
	if _, err := os.Stat("web"); err == nil && os.Getenv("WATCHCAT_DATA_DIR") == "" {
		dataDir = "data"
	}
	return Config{
		Addr:           getenv("WATCHCAT_ADDR", ":8080"),
		CollectorAddr:  getenv("WATCHCAT_COLLECTOR_ADDR", ":8443"),
		CollectorHosts: splitHosts(getenv("WATCHCAT_COLLECTOR_HOSTS", "watchcat-hub,localhost,127.0.0.1")),
		DataDir:        dataDir,
		WebDir:         getenv("WATCHCAT_WEB_DIR", defaultWebDir()),
		PairingTTL:     10 * time.Minute,
	}
}

func splitHosts(value string) []string {
	var hosts []string
	for _, host := range strings.Split(value, ",") {
		if host = strings.TrimSpace(host); host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, "watchcat.db") }

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
