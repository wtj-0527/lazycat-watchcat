package collector

import (
	"slices"
	"testing"
	"time"
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

func TestSummarizeUnusedImagesExcludesAllContainerReferences(t *testing.T) {
	images := []dockerImage{
		{ID: "sha256:used-running", RepoTags: []string{"running:1"}, Size: 10},
		{ID: "sha256:used-stopped", RepoTags: []string{"stopped:1"}, Size: 20},
		{ID: "sha256:unused-small", Size: 30, Created: 1},
		{ID: "sha256:unused-large", RepoDigest: []string{"unused@sha256:large"}, Size: 90},
	}
	containers := []dockerContainer{
		{ImageID: "sha256:used-running", State: "running"},
		{ImageID: "sha256:used-stopped", State: "exited"},
	}

	result := summarizeUnusedImages(images, containers)
	if !result.Available || result.Count != 2 || result.TotalSize != 120 {
		t.Fatalf("summary=%+v", result)
	}
	if result.DanglingCount != 1 || result.DanglingSize != 30 || result.CachedCount != 1 || result.CachedSize != 90 {
		t.Fatalf("categories=%+v", result)
	}
	if result.Items[0].ID != "sha256:unused-large" || result.Items[1].ID != "sha256:unused-small" {
		t.Fatalf("items not sorted by size: %+v", result.Items)
	}
	if !result.Items[1].CreatedAt.Equal(time.Unix(1, 0).UTC()) {
		t.Fatalf("createdAt=%s", result.Items[1].CreatedAt)
	}
}

func TestSummarizeImagePruneCountsUniqueDeletedImages(t *testing.T) {
	result := summarizeImagePrune(dockerImagePruneResponse{
		ImagesDeleted: []dockerImageDelete{
			{Untagged: "old:tag"},
			{Deleted: "sha256:a"},
			{Deleted: "sha256:a"},
			{Deleted: "sha256:b"},
		},
		SpaceReclaimed: 4096,
	})
	if result.ImagesDeleted != 2 || result.ReferencesUntagged != 1 || result.SpaceReclaimed != 4096 {
		t.Fatalf("result=%+v", result)
	}
}
