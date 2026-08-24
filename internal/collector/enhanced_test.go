package collector

import (
	"context"
	"testing"
	"time"

	"gitee.com/linakesi/lzc-sdk/lang/go/sys"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
)

type fakeHALClient struct {
	rpm int64
	err error
}

func (f fakeHALClient) GetFanRpm(context.Context, *emptypb.Empty, ...grpc.CallOption) (*sys.FanRpm, error) {
	return &sys.FanRpm{Rpm: f.rpm}, f.err
}

func TestHALCollectorOnlyEmitsPositiveReadOnlyFanRPM(t *testing.T) {
	now := time.Now().UTC()
	points, err := newHALCollectorWithClient(fakeHALClient{rpm: 1680}).Collect(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].Name != "system.fan.rpm" || points[0].Value != 1680 {
		t.Fatalf("points=%+v", points)
	}
	if points[0].Labels["source"] != "lazycat-hal" {
		t.Fatalf("labels=%+v", points[0].Labels)
	}
}

func TestDockerLabelsPreferLazyCatRuntimeLabel(t *testing.T) {
	labels := dockerLabels(dockerContainer{
		ID: "0123456789abcdef", Names: []string{"/maoyan"}, Image: "image:v1", State: "running",
		Labels: map[string]string{
			"home-cloud.app-id":          "legacy.app",
			"lzcapp.app-id":              "community.lazycat.app.maoyan",
			"com.docker.compose.service": "main",
		},
	})
	if labels["app"] != "community.lazycat.app.maoyan" || labels["container"] != "0123456789ab" || labels["service"] != "main" {
		t.Fatalf("labels=%+v", labels)
	}
}

func TestDockerStatPointsCalculateResourceMetrics(t *testing.T) {
	var stats dockerStats
	stats.CPUStats.CPUUsage.TotalUsage = 400
	stats.PreCPUStats.CPUUsage.TotalUsage = 200
	stats.CPUStats.SystemCPUUsage = 2000
	stats.PreCPUStats.SystemCPUUsage = 1000
	stats.CPUStats.OnlineCPUs = 4
	stats.MemoryStats.Usage = 1024
	stats.MemoryStats.Limit = 4096
	stats.MemoryStats.Stats = map[string]uint64{"inactive_file": 256}
	stats.Networks = map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	}{"eth0": {RxBytes: 10, TxBytes: 20}}
	stats.BlkioStats.IOServiceBytesRecursive = append(stats.BlkioStats.IOServiceBytesRecursive,
		struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		}{Op: "Read", Value: 30},
		struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		}{Op: "Write", Value: 40},
	)
	points := dockerStatPoints(stats, map[string]string{"app": "app.one"}, time.Now().UTC())
	values := map[string]float64{}
	for _, point := range points {
		values[point.Name] = point.Value
	}
	if values["container.cpu.usage"] != 80 || values["container.memory.usage"] != 768 ||
		values["container.memory.usage_percent"] != 18.75 || values["container.block.write.bytes_total"] != 40 {
		t.Fatalf("values=%+v", values)
	}
}

func TestDockerStatsTargetsRotateAcrossRunningContainers(t *testing.T) {
	collector := NewDockerCollector("/not-used")
	collector.statsBatch = 2
	running := []dockerContainer{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}}

	first := collector.nextStatsTargets(running)
	second := collector.nextStatsTargets(running)
	third := collector.nextStatsTargets(running)
	got := []string{first[0].ID, first[1].ID, second[0].ID, second[1].ID, third[0].ID, third[1].ID}
	want := []string{"a", "b", "c", "d", "e", "a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rotation=%v want=%v", got, want)
		}
	}
}

func TestMetricFiltersAvoidVirtualNoise(t *testing.T) {
	if includeNetworkInterface("lo") || includeNetworkInterface("veth1234") || !includeNetworkInterface("eth0") || !includeNetworkInterface("lzc-br0") {
		t.Fatal("unexpected network interface filter result")
	}
	for _, name := range []string{"sda", "nvme0n1", "dm-0"} {
		if !blockDeviceName.MatchString(name) {
			t.Fatalf("expected %s to match", name)
		}
	}
	if blockDeviceName.MatchString("loop0") {
		t.Fatal("loop devices must be excluded")
	}
}

func TestDiskIOMetricsDoNotClaimSMARTCapability(t *testing.T) {
	points := []protocol.MetricPoint{{Name: "disk.io.read.bytes_total"}}
	if metricPrefixPresent(points, "disk.temperature", "disk.power_on_hours", "disk.nvme.", "disk.ata.") {
		t.Fatal("disk IO must not be treated as SMART evidence")
	}
	points = append(points, protocol.MetricPoint{Name: "disk.nvme.media_errors"})
	if !metricPrefixPresent(points, "disk.temperature", "disk.power_on_hours", "disk.nvme.", "disk.ata.") {
		t.Fatal("expected an explicit SMART metric to be recognized")
	}
}
