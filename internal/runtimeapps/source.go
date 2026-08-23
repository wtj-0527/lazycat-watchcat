package runtimeapps

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	gohelper "gitee.com/linakesi/lzc-sdk/lang/go"
	"gitee.com/linakesi/lzc-sdk/lang/go/sys"
	"google.golang.org/grpc"
)

type packageManager interface {
	QueryApplication(context.Context, *sys.QueryApplicationRequest, ...grpc.CallOption) (*sys.QueryApplicationResponse, error)
}

type Application struct {
	DeployID       string
	AppID          string
	Title          string
	Version        string
	InstallStatus  string
	InstanceStatus string
	Domain         string
	Builtin        bool
}

type cachedResult struct {
	items     []Application
	expiresAt time.Time
}

type Source struct {
	client packageManager
	close  func() error
	ttl    time.Duration
	mu     sync.Mutex
	cache  map[string]cachedResult
}

func New(ctx context.Context) (*Source, error) {
	gateway, err := gohelper.NewAPIGateway(ctx)
	if err != nil {
		return nil, err
	}
	return &Source{
		client: gateway.PkgManager,
		close:  gateway.Close,
		ttl:    30 * time.Second,
		cache:  map[string]cachedResult{},
	}, nil
}

func NewWithClient(client packageManager, ttl time.Duration) *Source {
	return &Source{client: client, ttl: ttl, cache: map[string]cachedResult{}}
}

func (s *Source) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

func (s *Source) Query(ctx context.Context, uid string) ([]Application, error) {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return nil, errors.New("LazyCat user identity is missing")
	}
	now := time.Now()
	s.mu.Lock()
	if cached, ok := s.cache[uid]; ok && now.Before(cached.expiresAt) {
		items := append([]Application(nil), cached.items...)
		s.mu.Unlock()
		return items, nil
	}
	s.mu.Unlock()

	queryCtx := gohelper.WithRealUID(ctx, uid)
	ignorePending := true
	response, err := s.client.QueryApplication(queryCtx, &sys.QueryApplicationRequest{IgnorePendingPkg: &ignorePending})
	if err != nil {
		return nil, err
	}
	items := make([]Application, 0, len(response.GetInfoList()))
	for _, info := range response.GetInfoList() {
		if info.GetAppid() == "" || info.GetResourceOnly() {
			continue
		}
		deployID := info.GetDeployId()
		if deployID == "" {
			deployID = info.GetAppid()
		}
		items = append(items, Application{
			DeployID:       deployID,
			AppID:          info.GetAppid(),
			Title:          info.GetTitle(),
			Version:        info.GetVersion(),
			InstallStatus:  normalizeStatus(info.GetStatus().String()),
			InstanceStatus: normalizeInstanceStatus(info.GetInstanceStatus()),
			Domain:         info.GetDomain(),
			Builtin:        info.GetBuiltin(),
		})
	}
	s.mu.Lock()
	s.cache[uid] = cachedResult{items: append([]Application(nil), items...), expiresAt: now.Add(s.ttl)}
	s.mu.Unlock()
	return items, nil
}

func normalizeStatus(value string) string {
	return strings.ToLower(value)
}

func normalizeInstanceStatus(value sys.InstanceStatus) string {
	return strings.ToLower(strings.TrimPrefix(value.String(), "Status_"))
}
