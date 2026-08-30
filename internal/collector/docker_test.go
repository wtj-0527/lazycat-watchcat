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

func TestStaleHelperContainerIDsOnlyReturnsOldStoppedWatchCatHelpers(t *testing.T) {
	now := time.Unix(10_000, 0)
	containers := []dockerContainer{
		{ID: "old-created", State: "created", Created: now.Add(-20 * time.Minute).Unix(), Labels: map[string]string{smartHelperLabel: "true"}},
		{ID: "old-exited", State: "exited", Created: now.Add(-30 * time.Minute).Unix(), Labels: map[string]string{smartHelperLabel: "true"}},
		{ID: "fresh-created", State: "created", Created: now.Add(-time.Minute).Unix(), Labels: map[string]string{smartHelperLabel: "true"}},
		{ID: "running", State: "running", Created: now.Add(-time.Hour).Unix(), Labels: map[string]string{smartHelperLabel: "true"}},
		{ID: "foreign", State: "created", Created: now.Add(-time.Hour).Unix(), Labels: map[string]string{"app": "other"}},
	}
	got := staleHelperContainerIDs(containers, now, 10*time.Minute)
	if !slices.Equal(got, []string{"old-created", "old-exited"}) {
		t.Fatalf("cleanup candidates=%v", got)
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

func TestBtrfsDeviceWhitelist(t *testing.T) {
	for _, device := range []string{"/dev/sdb1", "/dev/nvme0n1p1", "/dev/mapper/nvme0n1p1"} {
		if !safeBtrfsDevice.MatchString(device) {
			t.Fatalf("valid Btrfs device rejected: %s", device)
		}
	}
	for _, device := range []string{"/dev/sdb", "/dev/nvme0n1", "/dev/mapper/data/child", "/dev/mapper/data;reboot", "../../dev/sdb1"} {
		if safeBtrfsDevice.MatchString(device) {
			t.Fatalf("unsafe Btrfs device accepted: %s", device)
		}
	}
}

func TestParseBtrfsHelperDoesNotReportPresentDeviceAsMissing(t *testing.T) {
	now := time.Now()
	raw := `Overall:
    Device size: 2048390070272
    Device allocated: 1852229812224
    Device unallocated: 196160258048
    Device missing: 0
    Used: 1628847173632
    Free (estimated): 387067351040
[/dev/mapper/nvme0n1p1].write_io_errs 0
[/dev/mapper/nvme0n1p1].read_io_errs 0
Scrub status for fb3c3a32
    Error summary: no errors found
`
	points := parseBtrfsHelper(raw, "/lzcsys/data", "/dev/mapper/nvme0n1p1", now)
	for _, point := range points {
		if point.Labels["backing_device"] != "/dev/mapper/nvme0n1p1" {
			t.Fatalf("backing device label=%v", point.Labels)
		}
		if point.Name == "btrfs.device_missing" {
			if point.Value != 0 || point.Unit != "bytes" {
				t.Fatalf("missing device metric=%+v", point)
			}
			return
		}
	}
	t.Fatal("missing btrfs.device_missing metric")
}

func TestParseFilesystemDF(t *testing.T) {
	now := time.Now().UTC()
	target := mountedFilesystem{path: "/lzcsys/run/media/sda1", device: "/dev/sda1", filesystem: "ntfs"}
	points := parseFilesystemDF("/dev/sda1 644247191552 540878835712 103368351744 84% /volume\n", target, now)
	if len(points) != 3 {
		t.Fatalf("points=%+v", points)
	}
	if points[0].Name != "filesystem.volume.size" || points[0].Value != 644247191552 {
		t.Fatalf("size=%+v", points[0])
	}
	if points[1].Name != "filesystem.volume.available" || points[1].Value != 103368351744 {
		t.Fatalf("available=%+v", points[1])
	}
	if points[2].Name != "filesystem.volume.usage" || points[2].Value != 84 {
		t.Fatalf("usage=%+v", points[2])
	}
	for _, point := range points {
		if point.Labels["backing_device"] != "/dev/sda1" || point.Labels["filesystem"] != "ntfs" {
			t.Fatalf("labels=%v", point.Labels)
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

func TestParseUpgradeProgressName(t *testing.T) {
	progress, ok := parseUpgradeProgressName(
		"/hermes-studio-rootfs-progress-acde1234-copy-usr-p023-c389-t1500-u1788060000",
	)
	if !ok {
		t.Fatal("expected progress marker to parse")
	}
	if progress.phase != "copy-usr" || progress.percent != 23 {
		t.Fatalf("progress=%+v", progress)
	}
	if progress.completedBytes != 389*1024*1024 || progress.totalBytes != 1500*1024*1024 {
		t.Fatalf("bytes=%d/%d", progress.completedBytes, progress.totalBytes)
	}
	if !progress.updatedAt.Equal(time.Unix(1788060000, 0).UTC()) {
		t.Fatalf("updatedAt=%s", progress.updatedAt)
	}
}
