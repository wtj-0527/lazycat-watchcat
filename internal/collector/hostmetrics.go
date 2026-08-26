package collector

import (
	"context"
	"regexp"
	"sort"
	"strings"
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

func collectHostMetrics(ctx context.Context, now time.Time) []protocol.MetricPoint {
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
	if counters, err := disk.IOCountersWithContext(ctx); err == nil {
		names := make([]string, 0, len(counters))
		for name := range counters {
			if blockDeviceName.MatchString(name) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			item := counters[name]
			labels := map[string]string{"device": name}
			add("disk.io.read.bytes_total", float64(item.ReadBytes), "bytes", labels)
			add("disk.io.write.bytes_total", float64(item.WriteBytes), "bytes", labels)
			add("disk.io.read.operations_total", float64(item.ReadCount), "count", labels)
			add("disk.io.write.operations_total", float64(item.WriteCount), "count", labels)
		}
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
	if temperatures, err := sensors.TemperaturesWithContext(ctx); err == nil {
		sort.Slice(temperatures, func(i, j int) bool { return temperatures[i].SensorKey < temperatures[j].SensorKey })
		for _, item := range temperatures {
			if item.Temperature <= -20 || item.Temperature >= 150 || strings.TrimSpace(item.SensorKey) == "" {
				continue
			}
			add("system.temperature", item.Temperature, "celsius", map[string]string{"sensor": item.SensorKey})
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
