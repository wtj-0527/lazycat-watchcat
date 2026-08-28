package upgradecoord

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultLeaseTTL = 5 * time.Minute
	queueEntryTTL   = 30 * time.Minute
)

var ErrLeaseNotFound = errors.New("upgrade lease not found")

type Request struct {
	RequestID  string    `json:"requestId"`
	AppID      string    `json:"appId"`
	InstanceID string    `json:"instanceId"`
	UserID     string    `json:"userId,omitempty"`
	EnqueuedAt time.Time `json:"enqueuedAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type Lease struct {
	Request
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Result struct {
	Status            string    `json:"status"`
	Position          int       `json:"position"`
	RetryAfterSeconds int       `json:"retryAfterSeconds"`
	Lease             *Lease    `json:"lease,omitempty"`
	Active            *Lease    `json:"active,omitempty"`
	Queue             []Request `json:"queue,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type state struct {
	Active *Lease    `json:"active,omitempty"`
	Queue  []Request `json:"queue"`
}

type Manager struct {
	mu       sync.Mutex
	path     string
	leaseTTL time.Duration
	now      func() time.Time
	state    state
}

func New(path string) (*Manager, error) {
	manager := &Manager{path: path, leaseTTL: defaultLeaseTTL, now: time.Now}
	if err := manager.load(); err != nil {
		return nil, err
	}
	manager.mu.Lock()
	changed := manager.cleanupLocked(manager.now().UTC())
	if changed {
		_ = manager.persistLocked()
	}
	manager.mu.Unlock()
	return manager, nil
}

func (m *Manager) Acquire(request Request) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	m.cleanupLocked(now)
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.AppID = strings.TrimSpace(request.AppID)
	request.InstanceID = strings.TrimSpace(request.InstanceID)
	request.UserID = strings.TrimSpace(request.UserID)
	if request.RequestID == "" || request.AppID == "" || request.InstanceID == "" {
		return Result{}, errors.New("requestId、appId 和 instanceId 必填")
	}
	if m.state.Active != nil && m.state.Active.RequestID == request.RequestID {
		m.state.Active.ExpiresAt = now.Add(m.leaseTTL)
		if err := m.persistLocked(); err != nil {
			return Result{}, err
		}
		return m.resultLocked("granted", 0, m.state.Active, now), nil
	}
	position := -1
	for index := range m.state.Queue {
		if m.state.Queue[index].RequestID == request.RequestID {
			m.state.Queue[index].LastSeenAt = now
			position = index
			break
		}
	}
	if position < 0 {
		request.EnqueuedAt = now
		request.LastSeenAt = now
		m.state.Queue = append(m.state.Queue, request)
		position = len(m.state.Queue) - 1
	}
	if m.state.Active == nil && position == 0 {
		token, err := randomToken()
		if err != nil {
			return Result{}, err
		}
		granted := m.state.Queue[0]
		m.state.Queue = append([]Request(nil), m.state.Queue[1:]...)
		m.state.Active = &Lease{Request: granted, Token: token, ExpiresAt: now.Add(m.leaseTTL)}
		if err := m.persistLocked(); err != nil {
			return Result{}, err
		}
		return m.resultLocked("granted", 0, m.state.Active, now), nil
	}
	if err := m.persistLocked(); err != nil {
		return Result{}, err
	}
	return m.resultLocked("waiting", position+1, nil, now), nil
}

func (m *Manager) Renew(token string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	m.cleanupLocked(now)
	if m.state.Active == nil || !secureEqual(m.state.Active.Token, strings.TrimSpace(token)) {
		return Result{}, ErrLeaseNotFound
	}
	m.state.Active.ExpiresAt = now.Add(m.leaseTTL)
	if err := m.persistLocked(); err != nil {
		return Result{}, err
	}
	return m.resultLocked("granted", 0, m.state.Active, now), nil
}

func (m *Manager) Release(token string) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	m.cleanupLocked(now)
	if m.state.Active == nil || !secureEqual(m.state.Active.Token, strings.TrimSpace(token)) {
		return Result{}, ErrLeaseNotFound
	}
	m.state.Active = nil
	if err := m.persistLocked(); err != nil {
		return Result{}, err
	}
	return m.resultLocked("released", 0, nil, now), nil
}

func (m *Manager) Snapshot() Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	if m.cleanupLocked(now) {
		_ = m.persistLocked()
	}
	return m.resultLocked("ok", 0, nil, now)
}

func (m *Manager) resultLocked(status string, position int, lease *Lease, now time.Time) Result {
	result := Result{
		Status: status, Position: position, RetryAfterSeconds: 5, Lease: cloneLease(lease),
		Active: cloneLease(m.state.Active), Queue: append([]Request(nil), m.state.Queue...), UpdatedAt: now,
	}
	return result
}

func (m *Manager) cleanupLocked(now time.Time) bool {
	changed := false
	if m.state.Active != nil && !m.state.Active.ExpiresAt.After(now) {
		m.state.Active = nil
		changed = true
	}
	filtered := m.state.Queue[:0]
	for _, request := range m.state.Queue {
		if now.Sub(request.LastSeenAt) <= queueEntryTTL {
			filtered = append(filtered, request)
		} else {
			changed = true
		}
	}
	m.state.Queue = filtered
	return changed
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		m.state.Queue = []Request{}
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, &m.state)
}

func (m *Manager) persistLocked() error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	temp := m.path + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, m.path)
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) || left == "" {
		return false
	}
	var different byte
	for index := range left {
		different |= left[index] ^ right[index]
	}
	return different == 0
}

func cloneLease(lease *Lease) *Lease {
	if lease == nil {
		return nil
	}
	copy := *lease
	return &copy
}
