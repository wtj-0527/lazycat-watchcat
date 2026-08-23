package collector

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeRunner map[string]string

func (f fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	return []byte(f[name+" "+strings.Join(args, " ")]), nil
}

func TestParseSmartNVMe(t *testing.T) {
	raw := []byte(`{"temperature":{"current":72},"power_on_time":{"hours":18420},"nvme_smart_health_information_log":{"critical_warning":4,"available_spare":100,"percentage_used":38,"media_errors":2}}`)
	points, err := parseSmart(raw, "/dev/nvme0n1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{"disk.temperature": false, "disk.nvme.media_errors": false, "disk.nvme.percentage_used": false}
	for _, p := range points {
		if _, ok := wanted[p.Name]; ok {
			wanted[p.Name] = true
		}
		if p.Labels["device"] != "nvme0n1" {
			t.Fatalf("labels=%v", p.Labels)
		}
	}
	for name, ok := range wanted {
		if !ok {
			t.Fatalf("missing %s", name)
		}
	}
}
func TestParseSmartATAOmitsNVMeMetrics(t *testing.T) {
	raw := []byte(`{"temperature":{"current":41},"power_on_time":{"hours":3200},"ata_smart_attributes":{"table":[{"name":"Reallocated_Sector_Ct","raw":{"value":0}}]}}`)
	points, err := parseSmart(raw, "/dev/sda", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	seenATA := false
	for _, point := range points {
		if strings.HasPrefix(point.Name, "disk.nvme.") {
			t.Fatalf("ATA payload produced NVMe metric %q", point.Name)
		}
		if point.Name == "disk.ata.reallocated_sectors" {
			seenATA = true
		}
	}
	if !seenATA {
		t.Fatal("missing ATA reallocated sector metric")
	}
}

func TestParseSmartNVMeRetainsZeroHealthValues(t *testing.T) {
	raw := []byte(`{"nvme_smart_health_information_log":{"critical_warning":0,"available_spare":0,"percentage_used":0,"media_errors":0}}`)
	points, err := parseSmart(raw, "/dev/nvme0n1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, point := range points {
		seen[point.Name] = true
	}
	for _, name := range []string{"disk.nvme.available_spare", "disk.nvme.percentage_used", "disk.nvme.media_errors", "disk.nvme.critical_warning"} {
		if !seen[name] {
			t.Fatalf("missing zero-valued NVMe metric %s", name)
		}
	}
}

func TestParseSmartNVMeOmitsMissingHealthFields(t *testing.T) {
	raw := []byte(`{"nvme_smart_health_information_log":{"percentage_used":12,"media_errors":0}}`)
	points, err := parseSmart(raw, "/dev/nvme0n1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, point := range points {
		seen[point.Name] = true
	}
	if seen["disk.nvme.available_spare"] || seen["disk.nvme.critical_warning"] {
		t.Fatalf("missing fields produced metrics: %v", seen)
	}
	if !seen["disk.nvme.percentage_used"] || !seen["disk.nvme.media_errors"] {
		t.Fatalf("present fields missing from metrics: %v", seen)
	}
}

func TestParseBtrfs(t *testing.T) {
	raw := "Device size: 7300000000000\nUsed: 6500000000000\n"
	points := parseBtrfs(raw, "/data", time.Now())
	if len(points) != 2 {
		t.Fatalf("points=%+v", points)
	}
	if points[1].Value < 89 || points[1].Value > 90 {
		t.Fatalf("usage=%f", points[1].Value)
	}
}
func TestDeviceWhitelist(t *testing.T) {
	values := validatedDevices([]string{"/dev/sda", "/dev/nvme0n1", "/tmp/not-device", "/dev/sda;reboot"})
	if len(values) != 2 {
		t.Fatalf("values=%v", values)
	}
}
