package runtimeapps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gohelper "gitee.com/linakesi/lzc-sdk/lang/go"
	"gitee.com/linakesi/lzc-sdk/lang/go/common"
	"gitee.com/linakesi/lzc-sdk/lang/go/sys"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type packageManager interface {
	QueryApplication(context.Context, *sys.QueryApplicationRequest, ...grpc.CallOption) (*sys.QueryApplicationResponse, error)
}

type packageController interface {
	Pause(context.Context, *sys.AppInstance, ...grpc.CallOption) (*emptypb.Empty, error)
	Resume(context.Context, *sys.AppInstance, ...grpc.CallOption) (*emptypb.Empty, error)
	ChangeDeployCfg(context.Context, *sys.ChangeDeployCfgRequest, ...grpc.CallOption) (*sys.ChangeDeployCfgResponse, error)
}

type userManager interface {
	ListUIDs(context.Context, *common.ListUIDsRequest, ...grpc.CallOption) (*common.ListUIDsReply, error)
	QueryUserInfo(context.Context, *common.UserID, ...grpc.CallOption) (*common.UserInfo, error)
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
	UserID         string
	UserName       string
}

type cachedResult struct {
	items     []Application
	expiresAt time.Time
}

type Source struct {
	client  packageManager
	users   userManager
	close   func() error
	ttl     time.Duration
	mu      sync.Mutex
	cache   map[string]cachedResult
	uidPath string
	lastUID string
}

func New(ctx context.Context) (*Source, error) {
	return NewPersistent(ctx, "")
}

func NewPersistent(ctx context.Context, uidPath string) (*Source, error) {
	gateway, err := gohelper.NewAPIGateway(ctx)
	if err != nil {
		return nil, err
	}
	source := &Source{
		client:  gateway.PkgManager,
		users:   gateway.Users,
		close:   gateway.Close,
		ttl:     30 * time.Second,
		cache:   map[string]cachedResult{},
		uidPath: uidPath,
	}
	if data, readErr := os.ReadFile(uidPath); readErr == nil {
		source.lastUID = strings.TrimSpace(string(data))
	}
	return source, nil
}

func NewWithClient(client packageManager, ttl time.Duration) *Source {
	return &Source{client: client, ttl: ttl, cache: map[string]cachedResult{}}
}

func NewWithClients(client packageManager, users userManager, ttl time.Duration) *Source {
	return &Source{client: client, users: users, ttl: ttl, cache: map[string]cachedResult{}}
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
	targetUIDs := []string{uid}
	if s.users != nil {
		response, err := s.users.ListUIDs(queryCtx, &common.ListUIDsRequest{})
		if err != nil {
			return nil, err
		}
		targetUIDs = uniqueUIDs(response.GetUids())
		if len(targetUIDs) == 0 {
			return nil, errors.New("LazyCat user manager returned no users")
		}
	}

	items := make([]Application, 0)
	seen := map[string]struct{}{}
	userNames := map[string]string{}
	for _, targetUID := range targetUIDs {
		request := &sys.QueryApplicationRequest{IgnorePendingPkg: &ignorePending}
		if s.users != nil {
			otherUID := targetUID
			request.OtherUid = &otherUID
		}
		response, err := s.client.QueryApplication(queryCtx, request)
		if err != nil {
			return nil, err
		}
		for _, info := range response.GetInfoList() {
			if info.GetAppid() == "" || info.GetResourceOnly() {
				continue
			}
			deployID := info.GetDeployId()
			if deployID == "" {
				deployID = info.GetAppid()
			}
			if _, exists := seen[deployID]; exists {
				continue
			}
			seen[deployID] = struct{}{}
			owner := strings.TrimSpace(info.GetOwner())
			if owner == "" {
				owner = targetUID
			}
			userName, known := userNames[owner]
			if !known {
				userName = owner
				if s.users != nil {
					if user, userErr := s.users.QueryUserInfo(queryCtx, &common.UserID{Uid: owner}); userErr == nil && strings.TrimSpace(user.GetNickname()) != "" {
						userName = strings.TrimSpace(user.GetNickname())
					}
				}
				userNames[owner] = userName
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
				UserID:         owner,
				UserName:       userName,
			})
		}
	}
	s.mu.Lock()
	s.cache[uid] = cachedResult{items: append([]Application(nil), items...), expiresAt: now.Add(s.ttl)}
	s.lastUID = uid
	s.mu.Unlock()
	s.persistUID(uid)
	return items, nil
}

func (s *Source) LastUID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastUID
}

type ControlResult struct {
	DeployID       string
	InstanceStatus string
	Autostart      *bool
}

func (s *Source) Control(ctx context.Context, uid, deployID, action string, autostart *bool) (ControlResult, error) {
	uid, deployID = strings.TrimSpace(uid), strings.TrimSpace(deployID)
	if uid == "" {
		return ControlResult{}, errors.New("LazyCat user identity is missing")
	}
	if deployID == "" {
		return ControlResult{}, errors.New("deploy id is missing")
	}
	callCtx := gohelper.WithRealUID(ctx, uid)
	instance := &sys.AppInstance{DeployId: deployID}
	result := ControlResult{DeployID: deployID}
	controller, ok := s.client.(packageController)
	if !ok {
		return ControlResult{}, errors.New("LazyCat Package Manager control API is unavailable")
	}
	switch action {
	case "start":
		if _, err := controller.Resume(callCtx, instance); err != nil {
			return ControlResult{}, err
		}
		result.InstanceStatus = "running"
	case "stop":
		if _, err := controller.Pause(callCtx, instance); err != nil {
			return ControlResult{}, err
		}
		result.InstanceStatus = "paused"
	case "set_autostart":
		if autostart == nil {
			return ControlResult{}, errors.New("autostart is required")
		}
		response, err := controller.ChangeDeployCfg(callCtx, &sys.ChangeDeployCfgRequest{DeployId: deployID, Autostart: autostart})
		if err != nil {
			return ControlResult{}, err
		}
		if response.GetResult() != sys.ChangeDeployCfgResponse_OK {
			return ControlResult{}, errors.New("LazyCat Package Manager rejected deploy configuration: " + response.GetResult().String())
		}
		value := *autostart
		result.Autostart = &value
	default:
		return ControlResult{}, errors.New("unsupported application action")
	}
	s.mu.Lock()
	s.cache = map[string]cachedResult{}
	s.mu.Unlock()
	return result, nil
}

func (s *Source) persistUID(uid string) {
	if s.uidPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.uidPath), 0o700); err != nil {
		return
	}
	tmp := s.uidPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(uid+"\n"), 0o600); err == nil {
		_ = os.Rename(tmp, s.uidPath)
	}
}

func uniqueUIDs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func normalizeStatus(value string) string {
	return strings.ToLower(value)
}

func normalizeInstanceStatus(value sys.InstanceStatus) string {
	return strings.ToLower(strings.TrimPrefix(value.String(), "Status_"))
}
