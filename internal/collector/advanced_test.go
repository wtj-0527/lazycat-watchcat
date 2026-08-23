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
