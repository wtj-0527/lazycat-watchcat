package collector

import (
	"slices"
	"testing"
)

func TestDockerSmartHelperSecurityDefaults(t *testing.T) {
	cfg := newDockerHelperConfig("sha256:test", []string{"/usr/sbin/smartctl"}, []string{"-j", "-a", "/dev/sdb"})
	if cfg.HostConfig.NetworkMode != "none" {
		t.Fatalf("network mode=%q", cfg.HostConfig.NetworkMode)
	}
	if !cfg.HostConfig.ReadonlyRootfs {
		t.Fatal("SMART helper root filesystem must be read-only")
	}
	if !slices.Equal(cfg.HostConfig.CapDrop, []string{"ALL"}) {
		t.Fatalf("cap drop=%v", cfg.HostConfig.CapDrop)
	}
	if !slices.Contains(cfg.HostConfig.SecurityOpt, "no-new-privileges") {
		t.Fatalf("security options=%v", cfg.HostConfig.SecurityOpt)
	}
	if cfg.HostConfig.PidsLimit <= 0 || cfg.HostConfig.Memory <= 0 {
		t.Fatalf("missing helper resource limits: pids=%d memory=%d", cfg.HostConfig.PidsLimit, cfg.HostConfig.Memory)
	}
}

func TestSmartSATFallbackDetection(t *testing.T) {
	for _, raw := range []string{
		`{"smartctl":{"messages":[{"string":"/dev/sdb: Unknown USB bridge [0x0bda:0x9201]"}]}}`,
		"Please specify device type with the -d option.",
	} {
		if !smartNeedsSAT([]byte(raw)) {
			t.Fatalf("did not detect SAT fallback requirement in %q", raw)
		}
	}
	if smartNeedsSAT([]byte(`{"temperature":{"current":35}}`)) {
		t.Fatal("successful SMART payload incorrectly requested SAT fallback")
	}
}

func TestDockerSmartDeviceWhitelist(t *testing.T) {
	for _, device := range []string{"/dev/sda", "/dev/sdb", "/dev/nvme0n1"} {
		if !smartBlockDevice.MatchString(device) {
			t.Fatalf("valid device rejected: %s", device)
		}
	}
	for _, device := range []string{"/dev/sda1", "/dev/nvme0", "/dev/mapper/data", "/dev/sdb;reboot", "../../dev/sda"} {
		if smartBlockDevice.MatchString(device) {
			t.Fatalf("unsafe device accepted: %s", device)
		}
	}
}
