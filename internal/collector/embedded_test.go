package collector

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestOptionalCapabilityStatuses(t *testing.T) {
	now := time.Now().UTC()
	available := optionalCapability("system.temperature", true, "sensor found", "no sensor", "unsupported", now)
	if available.Status != "available" || available.Detail != "sensor found" {
		t.Fatalf("available=%+v", available)
	}
	unsupported := optionalCapability("system.temperature", false, "sensor found", "no sensor", "unsupported", now)
	if unsupported.Status != "unsupported" || unsupported.Detail != "no sensor" {
		t.Fatalf("unsupported=%+v", unsupported)
	}
	restricted := optionalCapability("container.runtime", false, "socket ready", "socket unavailable", "restricted", now)
	if restricted.Status != "restricted" || restricted.Detail != "socket unavailable" {
		t.Fatalf("restricted=%+v", restricted)
	}
}

func TestCapabilityFromConfigStatuses(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		configured bool
		available  bool
		warnings   []string
		status     string
		detail     string
	}{
		{name: "smart", available: true, status: "available", detail: "只读采集已验证"},
		{name: "smart", status: "restricted", detail: "not mapped"},
		{name: "smart", configured: true, warnings: []string{"smart /dev/sda: permission denied"}, status: "error", detail: "smart /dev/sda: permission denied"},
		{name: "btrfs", configured: true, warnings: []string{"network: ignored", "btrfs /data: not mounted"}, status: "error", detail: "btrfs /data: not mounted"},
		{name: "nvme", configured: true, warnings: []string{"smart /dev/nvme0n1: command failed"}, status: "error", detail: "smart /dev/nvme0n1: command failed"},
	}
	for _, test := range tests {
		t.Run(test.name+"/"+test.status, func(t *testing.T) {
			item := capabilityFromConfig(test.name, test.configured, test.available, test.warnings, "not mapped", now)
			if item.Status != test.status || item.Detail != test.detail {
				t.Fatalf("item=%+v", item)
			}
		})
	}
}

func TestCapabilityFromConfigKeepsFallbackWithoutMatchingWarning(t *testing.T) {
	item := capabilityFromConfig("smart", true, false, []string{"network: unavailable"}, "not mapped", time.Now().UTC())
	if item.Status != "error" || item.Detail != "not mapped" {
		t.Fatalf("item=%+v", item)
	}
}

func TestAccessCapabilityStatuses(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name       string
		reachable  bool
		mapped     bool
		restricted bool
		warnings   []string
		status     string
		detail     string
	}{
		{name: "reachable without metrics", reachable: true, mapped: true, status: "available", detail: "read-only API verified"},
		{name: "reachable with partial failure", reachable: true, mapped: true, warnings: []string{"docker runtime: one stats request failed"}, status: "available", detail: "read-only API verified；部分采集失败：docker runtime: one stats request failed"},
		{name: "not mapped", status: "restricted", detail: "unavailable"},
		{name: "mapped permission denied", mapped: true, restricted: true, warnings: []string{"docker runtime: permission denied"}, status: "restricted", detail: "docker runtime: permission denied"},
		{name: "mapped call failed", mapped: true, warnings: []string{"docker runtime: connection reset"}, status: "error", detail: "docker runtime: connection reset"},
		{name: "mapped failure without detail", mapped: true, status: "error", detail: "unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := accessCapability("container.runtime", test.reachable, test.mapped, test.restricted, test.warnings, "docker runtime:", "read-only API verified", "unavailable", now)
			if item.Status != test.status || item.Detail != test.detail {
				t.Fatalf("item=%+v", item)
			}
		})
	}
}

func TestWarningsForTargets(t *testing.T) {
	warnings := []string{
		"smart /dev/sda: permission denied",
		"smart /dev/nvme0n1: command failed",
		"smart /dev/nvme1n1: timeout",
		"smart /dev/nvme1n10: unrelated prefix collision",
		"btrfs /data: not mounted",
	}
	matched := warningsForTargets(warnings, "smart ", []string{"/dev/nvme0n1", "/dev/nvme1n1"})
	if len(matched) != 2 || matched[0] != warnings[1] || matched[1] != warnings[2] {
		t.Fatalf("matched=%v", matched)
	}
}

func TestCapabilityFromConfigWarningTakesPrecedence(t *testing.T) {
	item := capabilityFromConfig("smart", true, true, []string{"smart /dev/sdb: permission denied"}, "not mapped", time.Now().UTC())
	if item.Status != "error" || item.Detail != "smart /dev/sdb: permission denied" {
		t.Fatalf("item=%+v", item)
	}
}

func TestStatusOf(t *testing.T) {
	if statusOf(true) != "available" || statusOf(false) != "error" {
		t.Fatalf("statusOf values: true=%q false=%q", statusOf(true), statusOf(false))
	}
	for _, value := range []string{statusOf(true), statusOf(false)} {
		if strings.Contains(value, "unavailable") || strings.Contains(value, "degraded") {
			t.Fatalf("legacy status %q", value)
		}
	}
}

func TestPermissionDenied(t *testing.T) {
	for _, message := range []string{"permission denied", "403 Forbidden", "operation not permitted"} {
		if !permissionDenied(errors.New(message)) {
			t.Fatalf("expected %q to be restricted", message)
		}
	}
	if permissionDenied(errors.New("connection reset")) {
		t.Fatal("connection reset must remain an error")
	}
}
