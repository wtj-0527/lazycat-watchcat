package collector

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/protocol"
)

type Credentials struct {
	DeviceID             string    `json:"deviceId"`
	Token                string    `json:"token"`
	CertificatePEM       string    `json:"certificatePem"`
	PrivateKeyPEM        string    `json:"privateKeyPem"`
	CACertificatePEM     string    `json:"caCertificatePem"`
	CertificateExpiresAt time.Time `json:"certificateExpiresAt"`
}

type Queue struct {
	path       string
	maxBatches int
}

func NewQueue(path string, maxBatches int) *Queue { return &Queue{path: path, maxBatches: maxBatches} }
func (q *Queue) Append(batch protocol.MetricBatch) error {
	items, err := q.load()
	if err != nil {
		return err
	}
	items = append(items, batch)
	if len(items) > q.maxBatches {
		items = items[len(items)-q.maxBatches:]
	}
	return q.save(items)
}
func (q *Queue) Peek() (protocol.MetricBatch, bool, error) {
	items, err := q.load()
	if err != nil || len(items) == 0 {
		return protocol.MetricBatch{}, false, err
	}
	return items[0], true, nil
}
func (q *Queue) Pop() error {
	items, err := q.load()
	if err != nil {
		return err
	}
	if len(items) > 0 {
		items = items[1:]
	}
	return q.save(items)
}
func (q *Queue) load() ([]protocol.MetricBatch, error) {
	data, err := os.ReadFile(q.path)
	if errors.Is(err, os.ErrNotExist) {
		return []protocol.MetricBatch{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []protocol.MetricBatch
	if len(data) > 0 {
		err = json.Unmarshal(data, &items)
	}
	return items, err
}
func (q *Queue) save(items []protocol.MetricBatch) error {
	if err := os.MkdirAll(filepath.Dir(q.path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(items)
	if err != nil {
		return err
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}

func Collect(deviceID string, now time.Time) (protocol.MetricBatch, error) {
	return CollectWithFilesystem(deviceID, now, "/", map[string]string{"mount": "/"})
}

func CollectWithFilesystem(deviceID string, now time.Time, filesystemPath string, labels map[string]string) (protocol.MetricBatch, error) {
	points := []protocol.MetricPoint{{Name: "system.cpu.cores", Value: float64(runtime.NumCPU()), Unit: "count", CollectedAt: now}}
	if load, err := readLoad(); err == nil {
		points = append(points, protocol.MetricPoint{Name: "system.load.1m", Value: load, CollectedAt: now})
	}
	if total, avail, err := readMemory(); err == nil {
		used := float64(total-avail) / float64(total) * 100
		points = append(points, protocol.MetricPoint{Name: "system.memory.usage", Value: used, Unit: "%", CollectedAt: now}, protocol.MetricPoint{Name: "system.memory.available", Value: float64(avail), Unit: "bytes", CollectedAt: now})
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(filesystemPath, &fs); err == nil && fs.Blocks > 0 {
		used := float64(fs.Blocks-fs.Bavail) / float64(fs.Blocks) * 100
		points = append(points, protocol.MetricPoint{Name: "filesystem.root.usage", Value: used, Unit: "%", Labels: labels, CollectedAt: now}, protocol.MetricPoint{Name: "filesystem.root.available", Value: float64(fs.Bavail) * float64(fs.Bsize), Unit: "bytes", Labels: labels, CollectedAt: now})
	}
	if up, err := readUptime(); err == nil {
		points = append(points, protocol.MetricPoint{Name: "system.uptime", Value: up, Unit: "seconds", CollectedAt: now})
	}
	return protocol.MetricBatch{DeviceID: deviceID, Points: points}, nil
}
func readLoad() (float64, error) {
	f, e := os.Open("/proc/loadavg")
	if e != nil {
		return 0, e
	}
	defer f.Close()
	var value float64
	_, e = fmt.Fscan(f, &value)
	return value, e
}
func readMemory() (uint64, uint64, error) {
	f, e := os.Open("/proc/meminfo")
	if e != nil {
		return 0, 0, e
	}
	defer f.Close()
	var total, avail uint64
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = v * 1024
		case "MemAvailable:":
			avail = v * 1024
		}
	}
	if total == 0 {
		return 0, 0, errors.New("MemTotal unavailable")
	}
	return total, avail, s.Err()
}
func readUptime() (float64, error) {
	b, e := os.ReadFile("/proc/uptime")
	if e != nil {
		return 0, e
	}
	return strconv.ParseFloat(strings.Fields(string(b))[0], 64)
}

func Pair(ctx context.Context, client *http.Client, hubURL, code, name, hostname, version string) (Credentials, error) {
	reqBody, _ := json.Marshal(protocol.PairCollectorRequest{Code: code, Name: name, Hostname: hostname, CollectorVer: version, Capabilities: []string{"host.metrics", "filesystem.metrics", "offline.queue", "mtls.v1"}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hubURL, "/")+"/api/v1/collectors/pair", bytes.NewReader(reqBody))
	if err != nil {
		return Credentials{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Credentials{}, fmt.Errorf("pair failed: %s: %s", resp.Status, string(body))
	}
	var out protocol.PairCollectorResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Credentials{}, err
	}
	return Credentials{DeviceID: out.DeviceID, Token: out.Token, CertificatePEM: out.CertificatePEM, PrivateKeyPEM: out.PrivateKeyPEM, CACertificatePEM: out.CACertificatePEM, CertificateExpiresAt: out.CertificateExpiresAt}, nil
}
func Send(ctx context.Context, client *http.Client, hubURL string, creds Credentials, batch protocol.MetricBatch) error {
	body, _ := json.Marshal(batch)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(hubURL, "/")+"/api/v1/metrics/batch", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+creds.Token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("send failed: %s: %s", resp.Status, string(b))
	}
	return nil
}
func SaveCredentials(path string, creds Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func LoadCredentials(path string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, err
	}
	var creds Credentials
	err = json.Unmarshal(data, &creds)
	return creds, err
}

func NewMTLSClient(creds Credentials) (*http.Client, error) {
	certificate, err := tls.X509KeyPair([]byte(creds.CertificatePEM), []byte(creds.PrivateKeyPEM))
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(creds.CACertificatePEM)) {
		return nil, errors.New("invalid CA certificate")
	}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			Certificates: []tls.Certificate{certificate},
			RootCAs:      roots,
		},
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, nil
}

func RotateCertificate(ctx context.Context, client *http.Client, collectorURL string, current Credentials) (Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(collectorURL, "/")+"/api/v1/certificate/rotate", nil)
	if err != nil {
		return Credentials{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Credentials{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Credentials{}, fmt.Errorf("certificate rotation failed: %s: %s", resp.Status, string(body))
	}
	var rotated protocol.PairCollectorResponse
	if err := json.NewDecoder(resp.Body).Decode(&rotated); err != nil {
		return Credentials{}, err
	}
	current.CertificatePEM = rotated.CertificatePEM
	current.PrivateKeyPEM = rotated.PrivateKeyPEM
	current.CACertificatePEM = rotated.CACertificatePEM
	current.CertificateExpiresAt = rotated.CertificateExpiresAt
	return current, nil
}
