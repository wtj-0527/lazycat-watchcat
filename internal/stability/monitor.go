package stability

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

const stateKey = "stability_observation"

type Status struct {
	StartedAt              time.Time  `json:"startedAt"`
	TargetEndAt            time.Time  `json:"targetEndAt"`
	LastSampleAt           time.Time  `json:"lastSampleAt"`
	SampleCount            int64      `json:"sampleCount"`
	FailureCount           int64      `json:"failureCount"`
	ConsecutiveFailures    int64      `json:"consecutiveFailures"`
	LastError              string     `json:"lastError,omitempty"`
	DatabaseIntegrityOK    bool       `json:"databaseIntegrityOk"`
	DatabaseLatencyMS      int64      `json:"databaseLatencyMs"`
	LatestMetricAt         *time.Time `json:"latestMetricAt,omitempty"`
	MetricFreshnessSeconds *int64     `json:"metricFreshnessSeconds,omitempty"`
	PendingNotifications   int        `json:"pendingNotifications"`
	LastRetentionAt        *time.Time `json:"lastRetentionAt,omitempty"`
	Qualified              bool       `json:"qualified"`
	RemainingSeconds       int64      `json:"remainingSeconds"`
}

type Monitor struct {
	store    *store.Store
	logger   *slog.Logger
	interval time.Duration
	mu       sync.RWMutex
	status   Status
}

func New(st *store.Store, logger *slog.Logger, interval time.Duration) *Monitor {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Monitor{store: st, logger: logger, interval: interval}
}

func (m *Monitor) Run(ctx context.Context) {
	m.loadOrReset(ctx)
	m.sample(ctx)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sample(ctx)
		}
	}
}

func (m *Monitor) Current() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := m.status
	now := time.Now().UTC()
	status.Qualified = !status.StartedAt.IsZero() && !now.Before(status.TargetEndAt) && status.FailureCount == 0
	if now.Before(status.TargetEndAt) {
		status.RemainingSeconds = int64(time.Until(status.TargetEndAt).Seconds())
	} else {
		status.RemainingSeconds = 0
	}
	return status
}

func (m *Monitor) Reset(ctx context.Context) (Status, error) {
	now := time.Now().UTC()
	status := Status{StartedAt: now, TargetEndAt: now.Add(7 * 24 * time.Hour)}
	if err := m.store.SetSystemState(ctx, stateKey, status); err != nil {
		return Status{}, err
	}
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
	m.sample(ctx)
	return m.Current(), nil
}

func (m *Monitor) loadOrReset(ctx context.Context) {
	var status Status
	ok, err := m.store.GetSystemState(ctx, stateKey, &status)
	if err != nil || !ok || status.StartedAt.IsZero() {
		if _, resetErr := m.Reset(ctx); resetErr != nil {
			m.logger.Warn("initialize stability observation", "error", resetErr)
		}
		return
	}
	m.mu.Lock()
	m.status = status
	m.mu.Unlock()
}

func (m *Monitor) sample(ctx context.Context) {
	started := time.Now()
	integrityErr := m.store.IntegrityCheck(ctx)
	inputs, inputErr := m.store.StabilityInputs(ctx)
	now := time.Now().UTC()

	m.mu.Lock()
	status := m.status
	if status.StartedAt.IsZero() {
		status.StartedAt = now
		status.TargetEndAt = now.Add(7 * 24 * time.Hour)
	}
	status.LastSampleAt = now
	status.SampleCount++
	status.DatabaseLatencyMS = time.Since(started).Milliseconds()
	status.DatabaseIntegrityOK = integrityErr == nil
	status.LatestMetricAt = inputs.LatestMetricAt
	status.PendingNotifications = inputs.PendingNotifications
	status.LastRetentionAt = inputs.LastRetentionAt
	if inputs.LatestMetricAt != nil {
		freshness := int64(now.Sub(*inputs.LatestMetricAt).Seconds())
		if freshness < 0 {
			freshness = 0
		}
		status.MetricFreshnessSeconds = &freshness
	}
	if integrityErr != nil || inputErr != nil {
		status.FailureCount++
		status.ConsecutiveFailures++
		if integrityErr != nil {
			status.LastError = integrityErr.Error()
		} else {
			status.LastError = inputErr.Error()
		}
	} else {
		status.ConsecutiveFailures = 0
		status.LastError = ""
	}
	m.status = status
	m.mu.Unlock()

	if err := m.store.SetSystemState(ctx, stateKey, status); err != nil {
		m.logger.Warn("persist stability observation", "error", err)
	}
}
