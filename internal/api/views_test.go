package api

import (
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

func TestBuildDeviceViewsSeparatesConnectivityFromHealth(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name        string
		device      protocol.Device
		metrics     []store.LatestMetric
		wantOnline  bool
		wantStale   bool
		wantHealth  string
		wantOffline bool
	}{
		{
			name:       "online healthy",
			device:     protocol.Device{ID: "healthy", Name: "Healthy", Status: "active", LastSeenAt: now},
			metrics:    []store.LatestMetric{{DeviceID: "healthy", Name: "system.cpu.usage", Value: 20, Unit: "%", CollectedAt: now}},
			wantOnline: true, wantHealth: "healthy",
		},
		{
			name:       "online without health evidence is unknown",
			device:     protocol.Device{ID: "heartbeat-only", Name: "Heartbeat only", Status: "active", LastSeenAt: now},
			wantOnline: true, wantHealth: "unknown",
		},
		{
			name:      "offline has unknown health",
			device:    protocol.Device{ID: "offline", Name: "Offline", Status: "active", LastSeenAt: now.Add(-2 * time.Minute)},
			wantStale: true, wantHealth: "unknown", wantOffline: true,
		},
		{
			name:       "revoked has unknown health",
			device:     protocol.Device{ID: "revoked", Name: "Revoked", Status: "revoked", LastSeenAt: now},
			wantHealth: "unknown", wantOffline: true,
		},
		{
			name:       "fresh warning metric",
			device:     protocol.Device{ID: "warning", Name: "Warning", Status: "active", LastSeenAt: now},
			metrics:    []store.LatestMetric{{DeviceID: "warning", Name: "system.cpu.usage", Value: 90, Unit: "%", CollectedAt: now}},
			wantOnline: true, wantHealth: "warning",
		},
		{
			name:   "critical takes precedence",
			device: protocol.Device{ID: "critical", Name: "Critical", Status: "active", LastSeenAt: now},
			metrics: []store.LatestMetric{
				{DeviceID: "critical", Name: "system.cpu.usage", Value: 90, Unit: "%", CollectedAt: now},
				{DeviceID: "critical", Name: "filesystem.root.usage", Value: 96, Unit: "%", CollectedAt: now},
			},
			wantOnline: true, wantHealth: "critical",
		},
		{
			name:       "stale critical preserves health evidence",
			device:     protocol.Device{ID: "stale-critical", Name: "Stale Critical", Status: "active", LastSeenAt: now.Add(-70 * time.Second)},
			metrics:    []store.LatestMetric{{DeviceID: "stale-critical", Name: "filesystem.root.usage", Value: 96, Unit: "%", CollectedAt: now}},
			wantOnline: true, wantStale: true, wantHealth: "critical",
		},
		{
			name:       "expired metric leaves health unknown",
			device:     protocol.Device{ID: "expired", Name: "Expired", Status: "active", LastSeenAt: now},
			metrics:    []store.LatestMetric{{DeviceID: "expired", Name: "filesystem.root.usage", Value: 99, Unit: "%", CollectedAt: now.Add(-11 * time.Minute)}},
			wantOnline: true, wantHealth: "unknown",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			view := buildDeviceViews([]protocol.Device{test.device}, test.metrics)[0]
			if view.Online != test.wantOnline || view.Stale != test.wantStale || view.Health != test.wantHealth {
				t.Fatalf("view online=%v stale=%v health=%q", view.Online, view.Stale, view.Health)
			}
			offline := false
			for _, alert := range deriveDeviceAlerts(view) {
				if alert.Fingerprint == test.device.ID+":offline" {
					offline = true
					if alert.Resource != "collector" || alert.Severity != "warning" {
						t.Fatalf("offline alert=%+v", alert)
					}
				}
			}
			if offline != test.wantOffline {
				t.Fatalf("offline alert present=%v, want %v", offline, test.wantOffline)
			}
		})
	}
}

func TestInspectionChecksPreservesCriticalOverConnectivity(t *testing.T) {
	checks := inspectionChecks([]deviceView{
		{Device: protocol.Device{ID: "healthy"}, Online: true, Health: "healthy"},
		{Device: protocol.Device{ID: "stale"}, Online: true, Stale: true, Health: "healthy"},
		{Device: protocol.Device{ID: "offline"}, Health: "unknown"},
		{Device: protocol.Device{ID: "critical"}, Online: true, Health: "critical"},
		{Device: protocol.Device{ID: "stale-critical"}, Online: true, Stale: true, Health: "critical"},
	})

	if checks["devices"] != 5 || checks["online"] != 2 || checks["healthy"] != 1 || checks["warning"] != 2 || checks["critical"] != 2 {
		t.Fatalf("checks=%v", checks)
	}
}
