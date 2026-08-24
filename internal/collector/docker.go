package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
)

const defaultDockerSocket = "/lzcapp/run/lzc-docker/docker.sock"
const defaultDockerStatsBatchSize = 8

type DockerCollector struct {
	socket      string
	client      *http.Client
	statsBatch  int
	cursorMu    sync.Mutex
	statsCursor int
}

type dockerContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IOServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

func NewDockerCollector(socket string) *DockerCollector {
	if strings.TrimSpace(socket) == "" {
		socket = defaultDockerSocket
	}
	transport := &http.Transport{
		DisableCompression: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socket)
		},
	}
	statsBatch := defaultDockerStatsBatchSize
	if configured, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MAOYAN_DOCKER_STATS_BATCH_SIZE"))); err == nil && configured > 0 && configured <= 64 {
		statsBatch = configured
	}
	return &DockerCollector{socket: socket, client: &http.Client{Transport: transport, Timeout: 8 * time.Second}, statsBatch: statsBatch}
}

func (d *DockerCollector) Available() bool {
	info, err := os.Stat(d.socket)
	return err == nil && info.Mode()&os.ModeSocket != 0
}

func (d *DockerCollector) Collect(ctx context.Context, now time.Time) ([]protocol.MetricPoint, error) {
	if !d.Available() {
		return nil, fmt.Errorf("docker socket unavailable: %s", d.socket)
	}
	var containers []dockerContainer
	if err := d.getJSON(ctx, "/containers/json?all=1", &containers); err != nil {
		return nil, err
	}
	monitored := make([]dockerContainer, 0, len(containers))
	for _, item := range containers {
		if dockerAppID(item) != "" {
			monitored = append(monitored, item)
		}
	}
	sort.Slice(monitored, func(i, j int) bool { return monitored[i].ID < monitored[j].ID })
	points := make([]protocol.MetricPoint, 0, len(monitored)*8)
	runningContainers := make([]dockerContainer, 0, len(monitored))
	for _, item := range monitored {
		labels := dockerLabels(item)
		runningValue := 0.0
		if item.State == "running" {
			runningValue = 1
			runningContainers = append(runningContainers, item)
		}
		points = append(points, protocol.MetricPoint{Name: "container.running", Value: runningValue, Unit: "bool", Labels: labels, CollectedAt: now})
	}
	statsTargets := d.nextStatsTargets(runningContainers)
	var pointsMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	var firstErr error
	for _, item := range statsTargets {
		item := item
		labels := dockerLabels(item)
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			var stats dockerStats
			if err := d.getJSON(ctx, "/containers/"+url.PathEscape(item.ID)+"/stats?stream=false", &stats); err != nil {
				pointsMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				pointsMu.Unlock()
				return
			}
			collected := dockerStatPoints(stats, labels, now)
			pointsMu.Lock()
			points = append(points, collected...)
			pointsMu.Unlock()
		}()
	}
	wg.Wait()
	if len(points) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return points, firstErr
}

func (d *DockerCollector) nextStatsTargets(running []dockerContainer) []dockerContainer {
	if len(running) == 0 {
		return nil
	}
	batch := d.statsBatch
	if batch <= 0 || batch >= len(running) {
		return append([]dockerContainer(nil), running...)
	}
	d.cursorMu.Lock()
	defer d.cursorMu.Unlock()
	start := d.statsCursor % len(running)
	out := make([]dockerContainer, 0, batch)
	for offset := 0; offset < batch; offset++ {
		out = append(out, running[(start+offset)%len(running)])
	}
	d.statsCursor = (start + batch) % len(running)
	return out
}

func (d *DockerCollector) getJSON(ctx context.Context, path string, dest any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	response, err := d.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return fmt.Errorf("docker GET %s: %s: %s", path, response.Status, strings.TrimSpace(string(body)))
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(dest); err != nil {
		return fmt.Errorf("decode docker %s: %w", path, err)
	}
	return nil
}

func dockerLabels(item dockerContainer) map[string]string {
	name := strings.TrimPrefix(first(item.Names), "/")
	labels := map[string]string{
		"container": shortContainerID(item.ID), "name": name, "image": item.Image, "state": item.State,
	}
	appID := dockerAppID(item)
	if appID != "" {
		labels["app"] = appID
	}
	if service := item.Labels["com.docker.compose.service"]; service != "" {
		labels["service"] = service
	}
	return labels
}

func dockerAppID(item dockerContainer) string {
	appID := item.Labels["lzcapp.app-id"]
	if appID == "" {
		appID = item.Labels["home-cloud.app-id"]
	}
	return appID
}

func dockerStatPoints(stats dockerStats, labels map[string]string, now time.Time) []protocol.MetricPoint {
	add := func(out *[]protocol.MetricPoint, name string, value float64, unit string) {
		*out = append(*out, protocol.MetricPoint{Name: name, Value: value, Unit: unit, Labels: labels, CollectedAt: now})
	}
	var out []protocol.MetricPoint
	var cpuDelta, systemDelta uint64
	if stats.CPUStats.CPUUsage.TotalUsage >= stats.PreCPUStats.CPUUsage.TotalUsage {
		cpuDelta = stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage
	}
	if stats.CPUStats.SystemCPUUsage >= stats.PreCPUStats.SystemCPUUsage {
		systemDelta = stats.CPUStats.SystemCPUUsage - stats.PreCPUStats.SystemCPUUsage
	}
	cpus := stats.CPUStats.OnlineCPUs
	if cpus == 0 {
		cpus = 1
	}
	if systemDelta > 0 && cpuDelta <= systemDelta*uint64(cpus)*100 {
		add(&out, "container.cpu.usage", float64(cpuDelta)/float64(systemDelta)*float64(cpus)*100, "%")
	}
	memoryUsage := stats.MemoryStats.Usage
	if inactive := stats.MemoryStats.Stats["inactive_file"]; inactive < memoryUsage {
		memoryUsage -= inactive
	}
	add(&out, "container.memory.usage", float64(memoryUsage), "bytes")
	add(&out, "container.memory.limit", float64(stats.MemoryStats.Limit), "bytes")
	if stats.MemoryStats.Limit > 0 {
		add(&out, "container.memory.usage_percent", float64(memoryUsage)/float64(stats.MemoryStats.Limit)*100, "%")
	}
	var rx, tx uint64
	for _, network := range stats.Networks {
		rx += network.RxBytes
		tx += network.TxBytes
	}
	add(&out, "container.network.receive.bytes_total", float64(rx), "bytes")
	add(&out, "container.network.transmit.bytes_total", float64(tx), "bytes")
	var read, write uint64
	for _, item := range stats.BlkioStats.IOServiceBytesRecursive {
		switch strings.ToLower(item.Op) {
		case "read":
			read += item.Value
		case "write":
			write += item.Value
		}
	}
	add(&out, "container.block.read.bytes_total", float64(read), "bytes")
	add(&out, "container.block.write.bytes_total", float64(write), "bytes")
	return out
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func shortContainerID(value string) string {
	value = filepath.Base(value)
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
