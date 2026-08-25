package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/buildinfo"
	"github.com/wtj-0527/lazycat-maoyan/internal/collector"
)

type runtimeConfig struct {
	HubURL       string `json:"hubUrl"`
	CollectorURL string `json:"collectorUrl"`
	DeviceName   string `json:"deviceName"`
}

type pairedRuntime struct {
	config runtimeConfig
	creds  collector.Credentials
}

type setupServer struct {
	mu         sync.Mutex
	status     *atomic.Value
	configPath string
	credsPath  string
	current    runtimeConfig
	paired     bool
	pairing    bool
	ready      chan<- pairedRuntime
	logger     *slog.Logger
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	var status atomic.Value
	status.Store("starting")
	dataDir := env("MAOYAN_COLLECTOR_DATA_DIR", "/lzcapp/var/data")
	configPath := filepath.Join(dataDir, "setup.json")
	credsPath := filepath.Join(dataDir, "credentials.json")
	config := loadRuntimeConfig(configPath)
	if config.HubURL == "" {
		config.HubURL = strings.TrimSpace(os.Getenv("MAOYAN_HUB_URL"))
	}
	if config.CollectorURL == "" {
		config.CollectorURL = strings.TrimSpace(os.Getenv("MAOYAN_COLLECTOR_URL"))
	}
	hostname, _ := os.Hostname()
	if config.DeviceName == "" {
		config.DeviceName = env("MAOYAN_DEVICE_NAME", hostname)
	}
	ready := make(chan pairedRuntime, 1)
	setup := &setupServer{status: &status, configPath: configPath, credsPath: credsPath, current: config, ready: ready, logger: logger}
	startSetupServer(env("MAOYAN_HEALTH_ADDR", ":8090"), setup, logger)

	creds, err := collector.LoadCredentials(credsPath)
	if err != nil {
		code := strings.TrimSpace(os.Getenv("MAOYAN_PAIRING_CODE"))
		if code != "" && config.HubURL != "" {
			paired, pairErr := pairAndSave(config, code, hostname, configPath, credsPath)
			if pairErr != nil {
				logger.Error("pair collector", "error", pairErr)
				status.Store("unpaired")
			} else {
				creds = paired.creds
				config = paired.config
				setup.markPaired(config)
				logger.Info("collector paired", "device_id", creds.DeviceID, "certificate_expires_at", creds.CertificateExpiresAt)
			}
		}
		if creds.DeviceID == "" {
			status.Store("unpaired")
			logger.Warn("collector is waiting for frontend setup", "setup_addr", env("MAOYAN_HEALTH_ADDR", ":8090"))
			paired := <-ready
			config, creds = paired.config, paired.creds
			logger.Info("collector paired from frontend", "device_id", creds.DeviceID, "certificate_expires_at", creds.CertificateExpiresAt)
		}
	} else {
		setup.markPaired(config)
	}

	collectorURL := config.CollectorURL
	if collectorURL == "" {
		collectorURL = config.HubURL
	}
	metricClient, err := collector.NewMTLSClient(creds)
	if err != nil {
		logger.Error("create mTLS client", "error", err)
		os.Exit(1)
	}
	if time.Until(creds.CertificateExpiresAt) < 30*24*time.Hour {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		rotated, rotateErr := collector.RotateCertificate(ctx, metricClient, collectorURL, creds)
		cancel()
		if rotateErr != nil {
			logger.Warn("certificate rotation deferred", "error", rotateErr)
		} else if saveErr := collector.SaveCredentials(credsPath, rotated); saveErr != nil {
			logger.Error("save rotated certificate", "error", saveErr)
			os.Exit(1)
		} else {
			creds = rotated
			metricClient, err = collector.NewMTLSClient(creds)
			if err != nil {
				logger.Error("reload rotated certificate", "error", err)
				os.Exit(1)
			}
			logger.Info("collector certificate rotated", "expires_at", creds.CertificateExpiresAt)
		}
	}
	status.Store("online")
	queue := collector.NewQueue(filepath.Join(dataDir, "metrics.queue.json"), 2048)
	advancedConfig := collector.AdvancedConfigFromEnv()
	var lastAdvanced time.Time
	interval := 30 * time.Second
	if raw := os.Getenv("MAOYAN_COLLECT_INTERVAL"); raw != "" {
		if parsed, e := time.ParseDuration(raw); e == nil && parsed >= 10*time.Second {
			interval = parsed
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		now := time.Now().UTC()
		batch, collectErr := collector.Collect(creds.DeviceID, now)
		if collectErr == nil && (lastAdvanced.IsZero() || now.Sub(lastAdvanced) >= 5*time.Minute) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			points, warnings := collector.CollectAdvanced(ctx, advancedConfig, now)
			cancel()
			batch.Points = append(batch.Points, points...)
			lastAdvanced = now
			if len(warnings) > 0 {
				logger.Warn("advanced collection partially degraded", "warnings", warnings)
			}
		}
		if collectErr == nil {
			_ = queue.Append(batch)
		}
		flush(logger, queue, metricClient, collectorURL, creds)
		<-ticker.C
	}
}

func startSetupServer(addr string, setup *setupServer, logger *slog.Logger) {
	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/v1/health", setup.health)
		mux.HandleFunc("GET /api/v1/setup", setup.setupStatus)
		mux.HandleFunc("POST /api/v1/setup", setup.configure)
		mux.HandleFunc("GET /", setup.page)
		server := &http.Server{Addr: addr, Handler: setupHeaders(mux), ReadHeaderTimeout: 5 * time.Second}
		if err := server.ListenAndServe(); err != nil {
			logger.Error("setup server stopped", "error", err)
		}
	}()
}

func (s *setupServer) health(w http.ResponseWriter, _ *http.Request) {
	writeSetupJSON(w, http.StatusOK, map[string]any{"status": s.status.Load(), "version": buildinfo.Version})
}

func (s *setupServer) setupStatus(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeSetupJSON(w, http.StatusOK, map[string]any{
		"paired": s.paired, "pairing": s.pairing, "setupRequired": !s.paired,
		"hubUrl": s.current.HubURL, "collectorUrl": s.current.CollectorURL, "deviceName": s.current.DeviceName,
	})
}

func (s *setupServer) configure(w http.ResponseWriter, r *http.Request) {
	var request struct {
		HubURL       string `json:"hubUrl"`
		CollectorURL string `json:"collectorUrl"`
		DeviceName   string `json:"deviceName"`
		PairingCode  string `json:"pairingCode"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeSetupProblem(w, http.StatusBadRequest, "配置格式无效")
		return
	}
	config := runtimeConfig{HubURL: strings.TrimRight(strings.TrimSpace(request.HubURL), "/"), CollectorURL: strings.TrimRight(strings.TrimSpace(request.CollectorURL), "/"), DeviceName: strings.TrimSpace(request.DeviceName)}
	if err := validateRuntimeConfig(config, request.PairingCode); err != nil {
		writeSetupProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	if s.paired {
		s.mu.Unlock()
		writeSetupProblem(w, http.StatusConflict, "Collector 已配对；如需重新配对，请先在本机删除凭据")
		return
	}
	if s.pairing {
		s.mu.Unlock()
		writeSetupProblem(w, http.StatusConflict, "正在配对，请稍候")
		return
	}
	s.pairing = true
	s.status.Store("pairing")
	s.mu.Unlock()

	hostname, _ := os.Hostname()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	creds, err := collector.Pair(ctx, http.DefaultClient, config.HubURL, strings.TrimSpace(request.PairingCode), config.DeviceName, hostname, buildinfo.Version)
	cancel()
	if err == nil {
		err = saveRuntimeConfig(s.configPath, config)
	}
	if err == nil {
		err = collector.SaveCredentials(s.credsPath, creds)
	}
	s.mu.Lock()
	s.pairing = false
	if err != nil {
		s.status.Store("unpaired")
		s.mu.Unlock()
		s.logger.Warn("frontend pairing failed", "error", err)
		writeSetupProblem(w, http.StatusBadGateway, "配对失败："+err.Error())
		return
	}
	s.paired = true
	s.current = config
	s.status.Store("paired")
	s.mu.Unlock()
	s.ready <- pairedRuntime{config: config, creds: creds}
	writeSetupJSON(w, http.StatusCreated, map[string]any{"status": "paired", "deviceId": creds.DeviceID})
}

func (s *setupServer) markPaired(config runtimeConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paired = true
	s.current = config
}

func pairAndSave(config runtimeConfig, code, hostname, configPath, credsPath string) (pairedRuntime, error) {
	if err := validateRuntimeConfig(config, code); err != nil {
		return pairedRuntime{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	creds, err := collector.Pair(ctx, http.DefaultClient, config.HubURL, code, config.DeviceName, hostname, buildinfo.Version)
	if err != nil {
		return pairedRuntime{}, err
	}
	if err := saveRuntimeConfig(configPath, config); err != nil {
		return pairedRuntime{}, err
	}
	if err := collector.SaveCredentials(credsPath, creds); err != nil {
		return pairedRuntime{}, err
	}
	return pairedRuntime{config: config, creds: creds}, nil
}

func validateRuntimeConfig(config runtimeConfig, pairingCode string) error {
	if config.HubURL == "" || config.CollectorURL == "" || config.DeviceName == "" || strings.TrimSpace(pairingCode) == "" {
		return errors.New("配对入口、指标入口、设备名称和配对码均为必填项")
	}
	hub, err := url.Parse(config.HubURL)
	if err != nil || hub.Host == "" || (hub.Scheme != "http" && hub.Scheme != "https") {
		return errors.New("配对入口必须是有效的 HTTP 或 HTTPS 地址")
	}
	collectorURL, err := url.Parse(config.CollectorURL)
	if err != nil || collectorURL.Host == "" || collectorURL.Scheme != "https" {
		return errors.New("指标入口必须是有效的 HTTPS 地址")
	}
	return nil
}

func loadRuntimeConfig(path string) runtimeConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return runtimeConfig{}
	}
	var config runtimeConfig
	_ = json.Unmarshal(data, &config)
	return config
}

func saveRuntimeConfig(path string, config runtimeConfig) error {
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

func writeSetupJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSetupProblem(w http.ResponseWriter, status int, message string) {
	writeSetupJSON(w, status, map[string]any{"error": map[string]string{"message": message}})
}

func setupHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *setupServer) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, setupPage)
}

const setupPage = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>猫眼 Collector 配置</title><style>
:root{font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif;color:#162033;background:#f5f7fb}*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px}.shell{width:min(680px,100%)}.brand{display:flex;align-items:center;gap:12px;margin-bottom:18px}.logo{width:44px;height:44px;border-radius:14px;background:#111827;color:#fff;display:grid;place-items:center;font-size:24px}.brand h1{font-size:22px;margin:0}.brand p{margin:3px 0 0;color:#667085}.card{background:#fff;border:1px solid #e5e9f2;border-radius:18px;padding:26px;box-shadow:0 18px 45px rgba(34,51,84,.08)}.status{padding:12px 14px;border-radius:10px;background:#f3f6fb;margin-bottom:20px}.status.ok{background:#ecfdf3;color:#087443}.status.bad{background:#fff1f2;color:#b42318}label{display:block;margin:15px 0 0;font-weight:650}input{display:block;width:100%;margin-top:7px;border:1px solid #ccd4e0;border-radius:9px;padding:11px 12px;font:inherit}small{display:block;color:#667085;margin-top:5px;line-height:1.55}button{width:100%;margin-top:22px;border:0;border-radius:10px;padding:12px 16px;background:#2563eb;color:#fff;font:inherit;font-weight:700;cursor:pointer}button:disabled{opacity:.55;cursor:wait}.note{margin-top:18px;padding-top:16px;border-top:1px solid #edf0f5;color:#667085;line-height:1.65}code{background:#f3f4f6;padding:2px 5px;border-radius:5px}</style></head><body><main class="shell"><div class="brand"><div class="logo">◉</div><div><h1>猫眼 Collector 配置</h1><p>一次完成远端设备配对与安全上报配置</p></div></div><section class="card"><div id="status" class="status">正在读取状态…</div><form id="form"><label>设备名称<input id="deviceName" required placeholder="例如：canway"></label><label>配对入口<input id="hubUrl" required placeholder="http://192.168.124.27:18080"></label><small>用于提交一次性配对码，指向 nasw 猫眼的普通 HTTP API。</small><label>指标入口<input id="collectorUrl" required placeholder="https://maoyan-hub:18443"></label><small>用于日常 mTLS 上报，主机名必须与猫眼证书匹配。</small><label>一次性配对码<input id="pairingCode" required autocomplete="one-time-code" placeholder="在 nasw 猫眼“接入”页面生成"></label><button id="submit" type="submit">验证并完成配对</button></form><div class="note">配对成功后，证书和非敏感连接配置保存在 Collector 数据目录；配对码不会保存。若需重新配对，必须先删除本机 <code>credentials.json</code>。</div></section></main><script>
const statusEl=document.querySelector('#status'),form=document.querySelector('#form'),button=document.querySelector('#submit');
async function load(){try{const r=await fetch('/api/v1/setup'),d=await r.json();document.querySelector('#deviceName').value=d.deviceName||'';document.querySelector('#hubUrl').value=d.hubUrl||'';document.querySelector('#collectorUrl').value=d.collectorUrl||'';if(d.paired){statusEl.className='status ok';statusEl.textContent='已完成配对，Collector 正在使用证书上报数据。';form.hidden=true}else{statusEl.textContent=d.pairing?'正在配对…':'尚未配对，请填写以下配置。'}}catch(e){statusEl.className='status bad';statusEl.textContent='无法读取 Collector 状态：'+e.message}}
form.addEventListener('submit',async e=>{e.preventDefault();button.disabled=true;button.textContent='正在验证并配对…';statusEl.className='status';statusEl.textContent='正在连接猫眼服务并签发证书…';try{const r=await fetch('/api/v1/setup',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({deviceName:deviceName.value,hubUrl:hubUrl.value,collectorUrl:collectorUrl.value,pairingCode:pairingCode.value})}),d=await r.json();if(!r.ok)throw new Error(d.error?.message||('HTTP '+r.status));statusEl.className='status ok';statusEl.textContent='配对成功，设备 ID：'+d.deviceId;form.hidden=true}catch(e){statusEl.className='status bad';statusEl.textContent=e.message;button.disabled=false;button.textContent='验证并完成配对'}});load();
</script></body></html>`

func flush(logger *slog.Logger, q *collector.Queue, client *http.Client, hub string, creds collector.Credentials) {
	for i := 0; i < 20; i++ {
		batch, ok, err := q.Peek()
		if err != nil || !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = collector.Send(ctx, client, hub, creds, batch)
		cancel()
		if err != nil {
			logger.Warn("metric delivery deferred", "error", err)
			return
		}
		if err := q.Pop(); err != nil {
			return
		}
	}
}
func env(k, v string) string {
	if x := os.Getenv(k); x != "" {
		return x
	}
	return v
}
