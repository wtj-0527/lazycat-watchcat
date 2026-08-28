package collector

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestDiskIOSamplerCalculatesBusyAndRates(t *testing.T) {
	sampler := diskIOSampler{previous: map[string]diskIOSample{}}
	start := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	sampler.points(map[string]disk.IOCountersStat{
		"sda": {ReadBytes: 100, WriteBytes: 200, ReadCount: 10, WriteCount: 20, ReadTime: 30, WriteTime: 40, IoTime: 1000, WeightedIO: 1200},
	}, start)
	points := sampler.points(map[string]disk.IOCountersStat{
		"sda": {ReadBytes: 1100, WriteBytes: 2200, ReadCount: 20, WriteCount: 40, ReadTime: 80, WriteTime: 90, IoTime: 1500, WeightedIO: 2200, IopsInProgress: 2},
	}, start.Add(time.Second))
	values := map[string]float64{}
	for _, point := range points {
		values[point.Name] = point.Value
	}
	if values["disk.io.busy_percent"] != 50 {
		t.Fatalf("busy=%v", values["disk.io.busy_percent"])
	}
	if values["disk.io.read.bytes_per_second"] != 1000 || values["disk.io.write.bytes_per_second"] != 2000 {
		t.Fatalf("throughput=%v/%v", values["disk.io.read.bytes_per_second"], values["disk.io.write.bytes_per_second"])
	}
	if values["disk.io.read.iops"] != 10 || values["disk.io.write.iops"] != 20 {
		t.Fatalf("iops=%v/%v", values["disk.io.read.iops"], values["disk.io.write.iops"])
	}
	if values["disk.io.await"] != float64(100)/30 {
		t.Fatalf("await=%v", values["disk.io.await"])
	}
	if values["disk.io.average_queue_depth"] != 1 {
		t.Fatalf("queue=%v", values["disk.io.average_queue_depth"])
	}
}

func TestDiskIOSamplerIgnoresCounterResetAndVirtualDevices(t *testing.T) {
	sampler := diskIOSampler{previous: map[string]diskIOSample{}}
	start := time.Now().UTC()
	sampler.points(map[string]disk.IOCountersStat{"sda": {IoTime: 5000}}, start)
	points := sampler.points(map[string]disk.IOCountersStat{
		"sda":   {IoTime: 10},
		"loop0": {IoTime: 1000},
	}, start.Add(time.Second))
	for _, point := range points {
		if point.Labels["device"] == "loop0" {
			t.Fatal("loop device must be excluded")
		}
		if point.Name == "disk.io.busy_percent" && point.Value != 0 {
			t.Fatalf("reset busy=%v", point.Value)
		}
	}
}

func TestReadIOPressure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "io")
	if err := os.WriteFile(path, []byte("some avg10=12.34 avg60=1.00 avg300=2.00 total=10\nfull avg10=5.67 avg60=0.00 avg300=0.00 total=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := readIOPressure(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["some"] != 12.34 || values["full"] != 5.67 {
		t.Fatalf("values=%v", values)
	}
}
