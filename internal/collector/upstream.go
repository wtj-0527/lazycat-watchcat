package collector

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/buildinfo"
	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

type UpstreamStatus struct {
	Paired        bool      `json:"paired"`
	HubURL        string    `json:"hubUrl,omitempty"`
	DeviceID      string    `json:"deviceId,omitempty"`
	LastSuccessAt time.Time `json:"lastSuccessAt,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
}

type upstreamConfig struct {
	HubURL string `json:"hubUrl"`
}

type Upstream struct {
	mu              sync.Mutex
	logger          *slog.Logger
	configPath      string
	credentialsPath string
	queue           *Queue
	config          upstreamConfig
	credentials     Credentials
	lastSuccessAt   time.Time
	lastError       string
}

var upstreamHTTPClient = &http.Client{
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

func NewUpstream(dataDir string, logger *slog.Logger) *Upstream {
	dir := filepath.Join(dataDir, "upstream")
	upstream := &Upstream{
		logger: logger, configPath: filepath.Join(dir, "config.json"),
		credentialsPath: filepath.Join(dir, "credentials.json"),
		queue:           NewQueue(filepath.Join(dir, "metrics.queue.json"), 2048),
	}
	configData, configErr := os.ReadFile(upstream.configPath)
	credentials, credentialsErr := LoadCredentials(upstream.credentialsPath)
	if configErr == nil && credentialsErr == nil {
		var config upstreamConfig
		if json.Unmarshal(configData, &config) == nil && config.HubURL != "" && credentials.DeviceID != "" {
			upstream.config, upstream.credentials = config, credentials
		}
	}
	return upstream
}

func (u *Upstream) Status() UpstreamStatus {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.statusLocked()
}

func (u *Upstream) Join(ctx context.Context, invitation, name, hostname string) (UpstreamStatus, error) {
	hubURL, code, err := ParseInvitation(invitation)
	if err != nil {
		return UpstreamStatus{}, err
	}
	u.mu.Lock()
	alreadyPaired := u.config.HubURL != "" && u.credentials.DeviceID != ""
	u.mu.Unlock()
	if alreadyPaired {
		return UpstreamStatus{}, errors.New("本机已经加入现有 WatchCat，请先断开当前连接")
	}
	credentials, err := Pair(ctx, upstreamHTTPClient, hubURL, code, name, hostname, buildinfo.Version)
	if err != nil {
		return UpstreamStatus{}, err
	}
	config := upstreamConfig{HubURL: hubURL}
	if err := saveUpstreamConfig(u.configPath, config); err != nil {
		return UpstreamStatus{}, err
	}
	if err := SaveCredentials(u.credentialsPath, credentials); err != nil {
		return UpstreamStatus{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.config, u.credentials = config, credentials
	u.lastError = ""
	_ = u.queue.save(nil)
	return u.statusLocked(), nil
}

func (u *Upstream) Disconnect() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	var result error
	for _, path := range []string{u.configPath, u.credentialsPath, u.queue.path} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, err)
		}
	}
	u.config, u.credentials = upstreamConfig{}, Credentials{}
	u.lastSuccessAt, u.lastError = time.Time{}, ""
	return result
}

func (u *Upstream) Send(ctx context.Context, batch protocol.MetricBatch) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.config.HubURL == "" || u.credentials.DeviceID == "" {
		return
	}
	batch.DeviceID = u.credentials.DeviceID
	if err := u.queue.Append(batch); err != nil {
		u.lastError = err.Error()
		return
	}
	for {
		next, ok, err := u.queue.Peek()
		if err != nil {
			u.lastError = err.Error()
			return
		}
		if !ok {
			u.lastError = ""
			return
		}
		if err := Send(ctx, upstreamHTTPClient, u.config.HubURL, u.credentials, next); err != nil {
			u.lastError = err.Error()
			u.logger.Warn("forward embedded metrics", "hub", u.config.HubURL, "error", err)
			return
		}
		if err := u.queue.Pop(); err != nil {
			u.lastError = err.Error()
			return
		}
		u.lastSuccessAt, u.lastError = time.Now().UTC(), ""
	}
}

func (u *Upstream) statusLocked() UpstreamStatus {
	return UpstreamStatus{
		Paired: u.config.HubURL != "" && u.credentials.DeviceID != "",
		HubURL: u.config.HubURL, DeviceID: u.credentials.DeviceID,
		LastSuccessAt: u.lastSuccessAt, LastError: u.lastError,
	}
}

func ParseInvitation(invitation string) (string, string, error) {
	parsed, err := url.Parse(strings.TrimSpace(invitation))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", "", errors.New("设备邀请必须是有效的 HTTP 或 HTTPS 链接")
	}
	values, err := url.ParseQuery(strings.TrimPrefix(parsed.Fragment, "?"))
	if err != nil {
		return "", "", errors.New("设备邀请格式无效")
	}
	code := strings.TrimSpace(values.Get("pairing-code"))
	if code == "" {
		code = strings.TrimSpace(values.Get("code"))
	}
	if code == "" {
		code = strings.TrimSpace(parsed.Query().Get("pairing-code"))
	}
	if code == "" {
		return "", "", errors.New("设备邀请中没有找到配对码")
	}
	parsed.Fragment, parsed.RawQuery = "", ""
	return strings.TrimRight(parsed.String(), "/"), code, nil
}

func saveUpstreamConfig(path string, config upstreamConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
