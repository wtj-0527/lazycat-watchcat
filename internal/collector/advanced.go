package collector

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
)

type AdvancedConfig struct {
	SmartDevices  []string
	BtrfsMounts   []string
	LPKStatusFile string
}
type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

var safeDevice = regexp.MustCompile(`^/dev/(?:sd[a-z]+|nvme[0-9]+n[0-9]+)$`)

func AdvancedConfigFromEnv() AdvancedConfig {
	return AdvancedConfig{SmartDevices: validatedDevices(splitCSV(os.Getenv("MAOYAN_SMART_DEVICES"))), BtrfsMounts: validatedMounts(splitCSV(os.Getenv("MAOYAN_BTRFS_MOUNTS"))), LPKStatusFile: os.Getenv("MAOYAN_LPK_STATUS_FILE")}
}
func CollectAdvanced(ctx context.Context, cfg AdvancedConfig, now time.Time) ([]protocol.MetricPoint, []string) {
	return collectAdvanced(ctx, osCommandRunner{}, cfg, now)
}
func collectAdvanced(ctx context.Context, runner commandRunner, cfg AdvancedConfig, now time.Time) ([]protocol.MetricPoint, []string) {
	var points []protocol.MetricPoint
	var warnings []string
	if network, err := readNetwork(now); err == nil {
		points = append(points, network...)
	} else {
		warnings = append(warnings, "network: "+err.Error())
	}
	for _, device := range cfg.SmartDevices {
		cmdCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		raw, err := runner.Run(cmdCtx, "smartctl", "-j", "-a", device)
		cancel()
		if err != nil {
			warnings = append(warnings, "smart "+device+": "+err.Error())
			continue
		}
		parsed, err := parseSmart(raw, device, now)
		if err != nil {
			warnings = append(warnings, "smart "+device+": "+err.Error())
			continue
		}
		points = append(points, parsed...)
	}
	for _, mount := range cfg.BtrfsMounts {
		cmdCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		raw, err := runner.Run(cmdCtx, "btrfs", "filesystem", "usage", "-b", "--raw", mount)
		cancel()
		if err != nil {
			warnings = append(warnings, "btrfs "+mount+": "+err.Error())
			continue
		}
		points = append(points, parseBtrfs(string(raw), mount, now)...)
	}
	if cfg.LPKStatusFile != "" {
		p, err := readLPKStatus(cfg.LPKStatusFile, now)
		if err != nil {
			warnings = append(warnings, "lpk runtime: "+err.Error())
		} else {
			points = append(points, p...)
		}
	}
	return points, warnings
}

func readNetwork(now time.Time) ([]protocol.MetricPoint, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rx, tx uint64
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.Fields(strings.Replace(line, ":", " ", 1))
		if len(parts) < 17 || parts[0] == "lo" {
			continue
		}
		r, _ := strconv.ParseUint(parts[1], 10, 64)
		t, _ := strconv.ParseUint(parts[9], 10, 64)
		rx += r
		tx += t
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return []protocol.MetricPoint{{Name: "network.receive.bytes_total", Value: float64(rx), Unit: "bytes", CollectedAt: now}, {Name: "network.transmit.bytes_total", Value: float64(tx), Unit: "bytes", CollectedAt: now}}, nil
}

type smartJSON struct {
	Temperature struct {
		Current float64 `json:"current"`
	} `json:"temperature"`
	PowerOnTime struct {
		Hours float64 `json:"hours"`
	} `json:"power_on_time"`
	NVMe *struct {
		CriticalWarning *float64 `json:"critical_warning"`
		Temperature     *float64 `json:"temperature"`
		AvailableSpare  *float64 `json:"available_spare"`
		PercentageUsed  *float64 `json:"percentage_used"`
		MediaErrors     *float64 `json:"media_errors"`
	} `json:"nvme_smart_health_information_log"`
	ATASmartAttributes struct {
		Table []struct {
			Name string `json:"name"`
			Raw  struct {
				Value float64 `json:"value"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
}

func parseSmart(raw []byte, device string, now time.Time) ([]protocol.MetricPoint, error) {
	var s smartJSON
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	labels := map[string]string{"device": filepath.Base(device)}
	var p []protocol.MetricPoint
	add := func(name string, value float64, unit string) {
		p = append(p, protocol.MetricPoint{Name: name, Value: value, Unit: unit, Labels: labels, CollectedAt: now})
	}
	temp := s.Temperature.Current
	if temp == 0 && s.NVMe != nil && s.NVMe.Temperature != nil && *s.NVMe.Temperature > 0 {
		temp = *s.NVMe.Temperature
	}
	if temp > 0 {
		add("disk.temperature", temp, "celsius")
	}
	if s.PowerOnTime.Hours > 0 {
		add("disk.power_on_hours", s.PowerOnTime.Hours, "hours")
	}
	if s.NVMe != nil {
		if s.NVMe.AvailableSpare != nil {
			add("disk.nvme.available_spare", *s.NVMe.AvailableSpare, "%")
		}
		if s.NVMe.PercentageUsed != nil {
			add("disk.nvme.percentage_used", *s.NVMe.PercentageUsed, "%")
		}
		if s.NVMe.MediaErrors != nil {
			add("disk.nvme.media_errors", *s.NVMe.MediaErrors, "count")
		}
		if s.NVMe.CriticalWarning != nil {
			add("disk.nvme.critical_warning", *s.NVMe.CriticalWarning, "bitmask")
		}
	}
	for _, a := range s.ATASmartAttributes.Table {
		if a.Name == "Reallocated_Sector_Ct" {
			add("disk.ata.reallocated_sectors", a.Raw.Value, "count")
		}
	}
	if len(p) == 0 {
		return nil, errors.New("no supported SMART fields")
	}
	return p, nil
}

var numberPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`)

func parseBtrfs(raw, mount string, now time.Time) []protocol.MetricPoint {
	labels := map[string]string{"mount": mount}
	var total, used float64
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		matches := numberPattern.FindStringSubmatch(line)
		if len(matches) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(matches[1], 64)
		switch {
		case strings.HasPrefix(line, "Device size:"):
			total = v
		case strings.HasPrefix(line, "Used:"):
			used = v
		}
	}
	p := []protocol.MetricPoint{}
	if total > 0 {
		p = append(p, protocol.MetricPoint{Name: "btrfs.size", Value: total, Unit: "bytes", Labels: labels, CollectedAt: now}, protocol.MetricPoint{Name: "btrfs.usage", Value: used / total * 100, Unit: "%", Labels: labels, CollectedAt: now})
	}
	return p
}

type lpkStatus struct {
	Applications []struct {
		ID       string  `json:"id"`
		Version  string  `json:"version"`
		Status   string  `json:"status"`
		Restarts float64 `json:"restarts"`
	} `json:"applications"`
}

func readLPKStatus(path string, now time.Time) ([]protocol.MetricPoint, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("status file must use an absolute path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var status lpkStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return nil, err
	}
	var p []protocol.MetricPoint
	for _, app := range status.Applications {
		if app.ID == "" {
			continue
		}
		labels := map[string]string{"app": app.ID, "version": app.Version, "status": app.Status}
		healthy := 0.0
		if strings.EqualFold(app.Status, "running") || strings.EqualFold(app.Status, "healthy") {
			healthy = 1
		}
		p = append(p, protocol.MetricPoint{Name: "lpk.application.healthy", Value: healthy, Unit: "bool", Labels: labels, CollectedAt: now}, protocol.MetricPoint{Name: "lpk.application.restarts", Value: app.Restarts, Unit: "count", Labels: labels, CollectedAt: now})
	}
	return p, nil
}
func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var out []string
	for _, v := range strings.Split(value, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func validatedDevices(values []string) []string {
	var out []string
	for _, v := range values {
		if safeDevice.MatchString(v) {
			out = append(out, v)
		}
	}
	return out
}
func validatedMounts(values []string) []string {
	var out []string
	for _, v := range values {
		clean := filepath.Clean(v)
		if filepath.IsAbs(clean) {
			out = append(out, clean)
		}
	}
	return out
}
func formatWarning(warnings []string) string {
	return fmt.Sprintf("%d collector warnings", len(warnings))
}
