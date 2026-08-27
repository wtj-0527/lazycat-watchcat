package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	commandExecutor func(context.Context, protocol.ApplicationCommand) protocol.ApplicationCommandResult
}

func (u *Upstream) SetCommandExecutor(executor func(context.Context, protocol.ApplicationCommand) protocol.ApplicationCommandResult) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.commandExecutor = executor
}

func (u *Upstream) RunCommands(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		u.runOneCommand(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (u *Upstream) runOneCommand(ctx context.Context) {
	u.mu.Lock()
	hubURL, credentials, executor := u.config.HubURL, u.credentials, u.commandExecutor
	u.mu.Unlock()
	if hubURL == "" || credentials.DeviceID == "" || executor == nil {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	command, err := fetchApplicationCommand(callCtx, upstreamHTTPClient, hubURL, credentials)
	if err != nil {
		if errors.Is(err, ErrCredentialsRejected) {
			_ = u.Disconnect()
		}
		u.mu.Lock()
		u.lastError = err.Error()
		u.mu.Unlock()
		return
	}
	if command == nil {
		return
	}
	result := executor(callCtx, *command)
	result.ID = command.ID
	if err := submitApplicationCommandResult(callCtx, upstreamHTTPClient, hubURL, credentials, result); err != nil {
		u.mu.Lock()
		u.lastError = err.Error()
		u.mu.Unlock()
		return
	}
	u.mu.Lock()
	u.lastSuccessAt, u.lastError = time.Now().UTC(), ""
	u.mu.Unlock()
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
	return u.disconnectLocked()
}

func (u *Upstream) RemoveBoth(ctx context.Context) error {
	u.mu.Lock()
	hubURL, credentials := u.config.HubURL, u.credentials
	u.mu.Unlock()
	if hubURL == "" || credentials.DeviceID == "" {
		return u.Disconnect()
	}
	if err := RemoveRemote(ctx, upstreamHTTPClient, hubURL, credentials); err != nil && !errors.Is(err, ErrCredentialsRejected) {
		u.mu.Lock()
		u.lastError = err.Error()
		u.mu.Unlock()
		return err
	}
	return u.Disconnect()
}

func (u *Upstream) disconnectLocked() error {
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
			if errors.Is(err, ErrCredentialsRejected) {
				u.logger.Info("upstream device was removed; clearing local credentials", "hub", u.config.HubURL)
				if clearErr := u.disconnectLocked(); clearErr != nil {
					u.lastError = clearErr.Error()
				}
				return
			}
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

func fetchApplicationCommand(ctx context.Context, client *http.Client, hubURL string, credentials Credentials) (*protocol.ApplicationCommand, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(hubURL, "/")+"/api/v1/collectors/commands/next", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credentials.Token)
	req.Header.Set("X-WatchCat-Device-ID", credentials.DeviceID)
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return nil, fmt.Errorf("%w: command fetch failed: %s: %s", ErrCredentialsRejected, response.Status, body)
		}
		return nil, fmt.Errorf("command fetch failed: %s: %s", response.Status, body)
	}
	var command protocol.ApplicationCommand
	if err := json.NewDecoder(response.Body).Decode(&command); err != nil {
		return nil, err
	}
	return &command, nil
}

func submitApplicationCommandResult(ctx context.Context, client *http.Client, hubURL string, credentials Credentials, result protocol.ApplicationCommandResult) error {
	body, _ := json.Marshal(result)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(hubURL, "/")+"/api/v1/collectors/commands/"+result.ID+"/result", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+credentials.Token)
	req.Header.Set("X-WatchCat-Device-ID", credentials.DeviceID)
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: command result failed: %s: %s", ErrCredentialsRejected, response.Status, responseBody)
	}
	return fmt.Errorf("command result failed: %s: %s", response.Status, responseBody)
}
