package collector

import (
	"bufio"
	"context"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

var blockDeviceName = regexp.MustCompile(`^(?:sd[a-z]+|vd[a-z]+|xvd[a-z]+|nvme\d+n\d+|md\d+|dm-\d+)$`)

type diskIOSample struct {
	at      time.Time
	counter disk.IOCountersStat
}

type diskIOSampler struct {
	mu       sync.Mutex
	previous map[string]diskIOSample
}

var hostDiskIOSampler = diskIOSampler{previous: map[string]diskIOSample{}}

func collectHostMetrics(ctx context.Context, now time.Time) []protocol.MetricPoint {
	points := collectSystemHostMetrics(ctx, now)
	points = append(points, collectDiskIOMetrics(ctx, now)...)
	points = append(points, collectTemperatureMetrics(ctx, now)...)
	return points
}

func collectSystemHostMetrics(ctx context.Context, now time.Time) []protocol.MetricPoint {
	var points []protocol.MetricPoint
	add := func(name string, value float64, unit string, labels map[string]string) {
		points = append(points, protocol.MetricPoint{Name: name, Value: value, Unit: unit, Labels: labels, CollectedAt: now})
	}

	if avg, err := load.AvgWithContext(ctx); err == nil {
		add("system.load.5m", avg.Load5, "", nil)
		add("system.load.15m", avg.Load15, "", nil)
	}
	if values, err := cpu.PercentWithContext(ctx, 200*time.Millisecond, false); err == nil && len(values) > 0 {
		add("system.cpu.usage", values[0], "%", nil)
	}
	if swap, err := mem.SwapMemoryWithContext(ctx); err == nil {
		add("system.swap.usage", swap.UsedPercent, "%", nil)
		add("system.swap.used", float64(swap.Used), "bytes", nil)
		add("system.swap.total", float64(swap.Total), "bytes", nil)
	}
	if pressure, err := readIOPressure("/proc/pressure/io"); err == nil {
		add("system.io.pressure.some", pressure["some"], "%", nil)
		add("system.io.pressure.full", pressure["full"], "%", nil)
	}
	if counters, err := gnet.IOCountersWithContext(ctx, true); err == nil {
		sort.Slice(counters, func(i, j int) bool { return counters[i].Name < counters[j].Name })
		for _, item := range counters {
			if !includeNetworkInterface(item.Name) {
				continue
			}
			labels := map[string]string{"interface": item.Name}
			add("network.interface.receive.bytes_total", float64(item.BytesRecv), "bytes", labels)
			add("network.interface.transmit.bytes_total", float64(item.BytesSent), "bytes", labels)
			add("network.interface.receive.errors_total", float64(item.Errin), "count", labels)
			add("network.interface.transmit.errors_total", float64(item.Errout), "count", labels)
			add("network.interface.receive.dropped_total", float64(item.Dropin), "count", labels)
			add("network.interface.transmit.dropped_total", float64(item.Dropout), "count", labels)
		}
	}
	return points
}

func collectDiskIOMetrics(ctx context.Context, now time.Time) []protocol.MetricPoint {
	if counters, err := disk.IOCountersWithContext(ctx); err == nil {
		return hostDiskIOSampler.points(counters, now)
	}
	return nil
}

func (s *diskIOSampler) points(counters map[string]disk.IOCountersStat, now time.Time) []protocol.MetricPoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.previous == nil {
		s.previous = map[string]diskIOSample{}
	}
	names := make([]string, 0, len(counters))
	for name := range counters {
		if blockDeviceName.MatchString(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	points := make([]protocol.MetricPoint, 0, len(names)*11)
	for _, name := range names {
		item := counters[name]
		labels := map[string]string{"device": name}
		points = append(points,
			protocol.MetricPoint{Name: "disk.io.read.bytes_total", Value: float64(item.ReadBytes), Unit: "bytes", Labels: labels, CollectedAt: now},
			protocol.MetricPoint{Name: "disk.io.write.bytes_total", Value: float64(item.WriteBytes), Unit: "bytes", Labels: labels, CollectedAt: now},
			protocol.MetricPoint{Name: "disk.io.read.operations_total", Value: float64(item.ReadCount), Unit: "count", Labels: labels, CollectedAt: now},
			protocol.MetricPoint{Name: "disk.io.write.operations_total", Value: float64(item.WriteCount), Unit: "count", Labels: labels, CollectedAt: now},
			protocol.MetricPoint{Name: "disk.io.queue_depth", Value: float64(item.IopsInProgress), Unit: "count", Labels: labels, CollectedAt: now},
		)
		if previous, ok := s.previous[name]; ok {
			elapsed := now.Sub(previous.at).Seconds()
			if elapsed > 0 && elapsed <= 10*time.Minute.Seconds() {
				readBytes := counterDelta(item.ReadBytes, previous.counter.ReadBytes)
				writeBytes := counterDelta(item.WriteBytes, previous.counter.WriteBytes)
				readOps := counterDelta(item.ReadCount, previous.counter.ReadCount)
				writeOps := counterDelta(item.WriteCount, previous.counter.WriteCount)
				ioTime := counterDelta(item.IoTime, previous.counter.IoTime)
				weightedIO := counterDelta(item.WeightedIO, previous.counter.WeightedIO)
				totalOps := readOps + writeOps
				busy := clamp(float64(ioTime)/(elapsed*1000)*100, 0, 100)
				await := 0.0
				if totalOps > 0 {
					serviceTime := counterDelta(item.ReadTime, previous.counter.ReadTime) + counterDelta(item.WriteTime, previous.counter.WriteTime)
					await = float64(serviceTime) / float64(totalOps)
				}
				points = append(points,
					protocol.MetricPoint{Name: "disk.io.busy_percent", Value: busy, Unit: "%", Labels: labels, CollectedAt: now},
					protocol.MetricPoint{Name: "disk.io.read.bytes_per_second", Value: float64(readBytes) / elapsed, Unit: "bytes/s", Labels: labels, CollectedAt: now},
					protocol.MetricPoint{Name: "disk.io.write.bytes_per_second", Value: float64(writeBytes) / elapsed, Unit: "bytes/s", Labels: labels, CollectedAt: now},
					protocol.MetricPoint{Name: "disk.io.read.iops", Value: float64(readOps) / elapsed, Unit: "iops", Labels: labels, CollectedAt: now},
					protocol.MetricPoint{Name: "disk.io.write.iops", Value: float64(writeOps) / elapsed, Unit: "iops", Labels: labels, CollectedAt: now},
					protocol.MetricPoint{Name: "disk.io.await", Value: await, Unit: "ms", Labels: labels, CollectedAt: now},
					protocol.MetricPoint{Name: "disk.io.average_queue_depth", Value: float64(weightedIO) / (elapsed * 1000), Unit: "count", Labels: labels, CollectedAt: now},
				)
			}
		}
		s.previous[name] = diskIOSample{at: now, counter: item}
	}
	for name := range s.previous {
		if _, ok := counters[name]; !ok {
			delete(s.previous, name)
		}
	}
	return points
}

func counterDelta(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func readIOPressure(path string) (map[string]float64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := map[string]float64{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		for _, field := range fields[1:] {
			key, raw, ok := strings.Cut(field, "=")
			if !ok || key != "avg10" {
				continue
			}
			value, parseErr := strconv.ParseFloat(raw, 64)
			if parseErr == nil {
				result[fields[0]] = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if _, ok := result["some"]; !ok {
		return nil, os.ErrInvalid
	}
	if _, ok := result["full"]; !ok {
		result["full"] = 0
	}
	return result, nil
}

func collectTemperatureMetrics(ctx context.Context, now time.Time) []protocol.MetricPoint {
	var points []protocol.MetricPoint
	if temperatures, err := sensors.TemperaturesWithContext(ctx); err == nil {
		sort.Slice(temperatures, func(i, j int) bool { return temperatures[i].SensorKey < temperatures[j].SensorKey })
		for _, item := range temperatures {
			if item.Temperature <= -20 || item.Temperature >= 150 || strings.TrimSpace(item.SensorKey) == "" {
				continue
			}
			points = append(points, protocol.MetricPoint{
				Name: "system.temperature", Value: item.Temperature, Unit: "celsius",
				Labels: map[string]string{"sensor": item.SensorKey}, CollectedAt: now,
			})
		}
	}
	return points
}

func includeNetworkInterface(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == "lo" {
		return false
	}
	for _, prefix := range []string{"veth", "docker", "cni", "flannel", "virbr"} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}
