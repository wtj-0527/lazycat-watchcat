package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
)

const defaultDockerSocket = "/lzcapp/run/lzc-docker/docker.sock"
const defaultDockerStatsBatchSize = 8
const defaultDockerStatsConcurrency = 2
const smartHelperLabel = "community.lazycat.app.maoyan.smart-helper"

var smartBlockDevice = regexp.MustCompile(`^/dev/(?:sd[a-z]+|nvme[0-9]+n[0-9]+)$`)

type DockerCollector struct {
	socket           string
	client           *http.Client
	statsBatch       int
	statsConcurrency int
	cursorMu         sync.Mutex
	statsCursor      int
}

type dockerContainer struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
}

type dockerContainerInspect struct {
	Image string `json:"Image"`
}

type dockerCreateResponse struct {
	ID       string   `json:"Id"`
	Warnings []string `json:"Warnings"`
}

type dockerWaitResponse struct {
	StatusCode int `json:"StatusCode"`
	Error      *struct {
		Message string `json:"Message"`
	} `json:"Error"`
}

type dockerDeviceMapping struct {
	PathOnHost        string `json:"PathOnHost"`
	PathInContainer   string `json:"PathInContainer"`
	CgroupPermissions string `json:"CgroupPermissions"`
}

type dockerHelperConfig struct {
	Image        string            `json:"Image"`
	Entrypoint   []string          `json:"Entrypoint"`
	Cmd          []string          `json:"Cmd"`
	AttachStdout bool              `json:"AttachStdout"`
	AttachStderr bool              `json:"AttachStderr"`
	Tty          bool              `json:"Tty"`
	Labels       map[string]string `json:"Labels"`
	HostConfig   struct {
		NetworkMode    string                `json:"NetworkMode"`
		ReadonlyRootfs bool                  `json:"ReadonlyRootfs"`
		CapDrop        []string              `json:"CapDrop"`
		CapAdd         []string              `json:"CapAdd,omitempty"`
		SecurityOpt    []string              `json:"SecurityOpt"`
		Binds          []string              `json:"Binds,omitempty"`
		Devices        []dockerDeviceMapping `json:"Devices,omitempty"`
		PidsLimit      int64                 `json:"PidsLimit"`
		Memory         int64                 `json:"Memory"`
	} `json:"HostConfig"`
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
	statsConcurrency := defaultDockerStatsConcurrency
	if configured, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MAOYAN_DOCKER_STATS_CONCURRENCY"))); err == nil && configured > 0 && configured <= 8 {
		statsConcurrency = configured
	}
	return &DockerCollector{
		socket: socket, client: &http.Client{Transport: transport, Timeout: 30 * time.Second},
		statsBatch: statsBatch, statsConcurrency: statsConcurrency,
	}
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
	sem := make(chan struct{}, d.statsConcurrency)
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

func (d *DockerCollector) doJSON(ctx context.Context, method, path string, body any, accepted ...int) ([]byte, error) {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://docker"+path, payload)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := d.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	for _, status := range accepted {
		if response.StatusCode == status {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("docker %s %s: %s: %s", method, path, response.Status, strings.TrimSpace(string(raw)))
}

func (d *DockerCollector) helperImage(ctx context.Context) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	var inspected dockerContainerInspect
	if err := d.getJSON(ctx, "/containers/"+url.PathEscape(hostname)+"/json", &inspected); err == nil && inspected.Image != "" {
		return inspected.Image, nil
	}
	var containers []dockerContainer
	if err := d.getJSON(ctx, "/containers/json?all=1", &containers); err != nil {
		return "", fmt.Errorf("list containers to identify collector image: %w", err)
	}
	appID := strings.TrimSpace(os.Getenv("LAZYCAT_APP_ID"))
	service := strings.TrimSpace(os.Getenv("LAZYCAT_APP_SERVICE_NAME"))
	for _, item := range containers {
		if appID != "" && dockerAppID(item) != appID {
			continue
		}
		if service != "" && item.Labels["com.docker.compose.service"] != service {
			continue
		}
		if item.State != "running" {
			continue
		}
		if item.ImageID != "" {
			return item.ImageID, nil
		}
		if item.Image != "" {
			return item.Image, nil
		}
	}
	return "", fmt.Errorf("identify collector image for app=%q service=%q", appID, service)
}

func newDockerHelperConfig(image string, entrypoint, cmd []string) dockerHelperConfig {
	cfg := dockerHelperConfig{
		Image: image, Entrypoint: entrypoint, Cmd: cmd, AttachStdout: true, AttachStderr: true, Tty: true,
		Labels: map[string]string{smartHelperLabel: "true"},
	}
	cfg.HostConfig.NetworkMode = "none"
	cfg.HostConfig.ReadonlyRootfs = true
	cfg.HostConfig.CapDrop = []string{"ALL"}
	cfg.HostConfig.SecurityOpt = []string{"no-new-privileges"}
	cfg.HostConfig.PidsLimit = 32
	cfg.HostConfig.Memory = 64 << 20
	return cfg
}

func (d *DockerCollector) runHelper(ctx context.Context, cfg dockerHelperConfig) ([]byte, int, error) {
	raw, err := d.doJSON(ctx, http.MethodPost, "/containers/create", cfg, http.StatusCreated)
	if err != nil {
		return nil, -1, err
	}
	var created dockerCreateResponse
	if err := json.Unmarshal(raw, &created); err != nil {
		return nil, -1, fmt.Errorf("decode Docker helper create response: %w", err)
	}
	if created.ID == "" {
		return nil, -1, errors.New("decode Docker helper create response: empty container ID")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = d.doJSON(cleanupCtx, http.MethodDelete, "/containers/"+url.PathEscape(created.ID)+"?force=1", nil, http.StatusNoContent, http.StatusNotFound)
	}()
	if _, err := d.doJSON(ctx, http.MethodPost, "/containers/"+url.PathEscape(created.ID)+"/start", nil, http.StatusNoContent); err != nil {
		return nil, -1, err
	}
	raw, err = d.doJSON(ctx, http.MethodPost, "/containers/"+url.PathEscape(created.ID)+"/wait?condition=not-running", nil, http.StatusOK)
	if err != nil {
		return nil, -1, err
	}
	var waited dockerWaitResponse
	if err := json.Unmarshal(raw, &waited); err != nil {
		return nil, -1, err
	}
	logs, logsErr := d.doJSON(ctx, http.MethodGet, "/containers/"+url.PathEscape(created.ID)+"/logs?stdout=1&stderr=1", nil, http.StatusOK)
	if logsErr != nil {
		return nil, waited.StatusCode, logsErr
	}
	if waited.Error != nil && waited.Error.Message != "" {
		return logs, waited.StatusCode, errors.New(waited.Error.Message)
	}
	return logs, waited.StatusCode, nil
}

func (d *DockerCollector) discoverSmartDevices(ctx context.Context, image string) ([]string, error) {
	const script = `for p in /host-sys/class/block/*; do n=${p##*/}; case "$n" in sd[a-z]|nvme[0-9]n[0-9]) echo /dev/$n;; esac; done`
	cfg := newDockerHelperConfig(image, []string{"/bin/sh"}, []string{"-c", script})
	cfg.HostConfig.Binds = []string{"/sys:/host-sys:ro"}
	raw, exitCode, err := d.runHelper(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("device discovery helper exited %d: %s", exitCode, strings.TrimSpace(string(raw)))
	}
	seen := map[string]bool{}
	var devices []string
	for _, line := range strings.Split(string(raw), "\n") {
		device := strings.TrimSpace(line)
		if !smartBlockDevice.MatchString(device) || seen[device] {
			continue
		}
		seen[device] = true
		devices = append(devices, device)
	}
	sort.Strings(devices)
	return devices, nil
}

func (d *DockerCollector) readSmartDevice(ctx context.Context, image, device, smartType string) ([]byte, int, error) {
	if !smartBlockDevice.MatchString(device) {
		return nil, -1, fmt.Errorf("unsafe SMART device %q", device)
	}
	args := []string{"-j", "-a"}
	if smartType != "" {
		args = append(args, "-d", smartType)
	}
	args = append(args, device)
	cfg := newDockerHelperConfig(image, []string{"/usr/sbin/smartctl"}, args)
	cfg.HostConfig.CapAdd = []string{"SYS_RAWIO"}
	cfg.HostConfig.Devices = []dockerDeviceMapping{{
		PathOnHost: device, PathInContainer: device, CgroupPermissions: "rwm",
	}}
	if strings.HasPrefix(device, "/dev/nvme") {
		controller := strings.TrimSuffix(device, "n1")
		if controller != device {
			cfg.HostConfig.CapAdd = append(cfg.HostConfig.CapAdd, "SYS_ADMIN")
			cfg.HostConfig.Devices = append(cfg.HostConfig.Devices, dockerDeviceMapping{
				PathOnHost: controller, PathInContainer: controller, CgroupPermissions: "rwm",
			})
		}
	}
	return d.runHelper(ctx, cfg)
}

func smartNeedsSAT(raw []byte) bool {
	text := strings.ToLower(string(raw))
	return strings.Contains(text, "unknown usb bridge") ||
		strings.Contains(text, "please specify device type with the -d option")
}

func (d *DockerCollector) CollectSMART(ctx context.Context, now time.Time) ([]protocol.MetricPoint, []string) {
	if !d.Available() {
		return nil, []string{"docker smart: Docker socket unavailable"}
	}
	image, err := d.helperImage(ctx)
	if err != nil {
		return nil, []string{"docker smart: " + err.Error()}
	}
	devices, err := d.discoverSmartDevices(ctx, image)
	if err != nil {
		return nil, []string{"docker smart: " + err.Error()}
	}
	var points []protocol.MetricPoint
	var warnings []string
	for _, device := range devices {
		raw, exitCode, runErr := d.readSmartDevice(ctx, image, device, "")
		smartType := "auto"
		if smartNeedsSAT(raw) && strings.HasPrefix(device, "/dev/sd") {
			raw, exitCode, runErr = d.readSmartDevice(ctx, image, device, "sat")
			smartType = "sat"
		}
		if runErr != nil {
			warnings = append(warnings, "docker smart "+device+": "+runErr.Error())
			continue
		}
		parsed, err := parseSmart(raw, device, now)
		if err != nil {
			if exitCode != 0 {
				warnings = append(warnings, fmt.Sprintf("docker smart %s: smartctl exited %d: %v", device, exitCode, err))
			} else {
				warnings = append(warnings, "docker smart "+device+": "+err.Error())
			}
			continue
		}
		if exitCode != 0 {
			warnings = append(warnings, fmt.Sprintf("docker smart %s: smartctl health/status bitmask %d", device, exitCode))
		}
		for i := range parsed {
			parsed[i].Labels["source"] = "lazycat-docker-helper"
			parsed[i].Labels["smart_type"] = smartType
		}
		points = append(points, parsed...)
	}
	return points, warnings
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
