package runtimeusers

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	gohelper "gitee.com/linakesi/lzc-sdk/lang/go"
	"gitee.com/linakesi/lzc-sdk/lang/go/common"
	"gitee.com/linakesi/lzc-sdk/lang/go/sys"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type userManager interface {
	ListUIDs(context.Context, *common.ListUIDsRequest, ...grpc.CallOption) (*common.ListUIDsReply, error)
	QueryUserInfo(context.Context, *common.UserID, ...grpc.CallOption) (*common.UserInfo, error)
	CreateUser(context.Context, *common.CreateUserRequest, ...grpc.CallOption) (*emptypb.Empty, error)
	ChangeRole(context.Context, *common.ChangeRoleReqeust, ...grpc.CallOption) (*emptypb.Empty, error)
	ForceResetPassword(context.Context, *common.ForceResetPasswordRequest, ...grpc.CallOption) (*emptypb.Empty, error)
	DeleteUser(context.Context, *common.DeleteUserRequest, ...grpc.CallOption) (*emptypb.Empty, error)
}
type deviceManager interface {
	ListEndDevices(context.Context, *common.ListEndDeviceRequest, ...grpc.CallOption) (*common.ListEndDeviceReply, error)
}
type accessController interface {
	QueryAppAccessPolicy(context.Context, *sys.AppAccessPolicyRequest, ...grpc.CallOption) (*sys.AppAccessPolicy, error)
	SetAppAccessPolicy(context.Context, *sys.AppAccessPolicyRequest, ...grpc.CallOption) (*emptypb.Empty, error)
}

type Source struct {
	users   userManager
	devices deviceManager
	access  accessController
	close   func() error
	uidPath string
	mu      sync.Mutex
	lastUID string
}

func NewPersistent(ctx context.Context, uidPath string) (*Source, error) {
	g, err := gohelper.NewAPIGateway(ctx)
	if err != nil {
		return nil, err
	}
	s := &Source{users: g.Users, devices: g.Devices, access: g.AccessControler, close: g.Close, uidPath: uidPath}
	if data, e := os.ReadFile(uidPath); e == nil {
		s.lastUID = strings.TrimSpace(string(data))
	}
	return s, nil
}
func (s *Source) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}
func (s *Source) LastUID() string { s.mu.Lock(); defer s.mu.Unlock(); return s.lastUID }

func (s *Source) Query(ctx context.Context, actor string) ([]store.RuntimeUser, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return nil, errors.New("LazyCat user identity is missing")
	}
	qctx := gohelper.WithRealUID(ctx, actor)
	list, err := s.users.ListUIDs(qctx, &common.ListUIDsRequest{})
	if err != nil {
		return nil, err
	}
	var out []store.RuntimeUser
	for _, uid := range list.GetUids() {
		uid = strings.TrimSpace(uid)
		if uid == "" {
			continue
		}
		info, err := s.users.QueryUserInfo(qctx, &common.UserID{Uid: uid})
		if err != nil {
			return nil, err
		}
		endpoints, err := s.devices.ListEndDevices(qctx, &common.ListEndDeviceRequest{Uid: uid})
		if err != nil {
			return nil, err
		}
		u := store.RuntimeUser{UserID: uid, Nickname: strings.TrimSpace(info.GetNickname()), Role: "normal", AppInstallPermission: info.GetHasAppInstallPermission()}
		policy, err := s.access.QueryAppAccessPolicy(qctx, &sys.AppAccessPolicyRequest{Uid: uid})
		if err != nil {
			return nil, err
		}
		u.AppAccessNoLimit = policy.GetNoLimit()
		u.AllowedAppIDs = append([]string(nil), policy.GetAllowAccessAppids()...)
		if info.GetRole() == common.Role_ROLE_ADMIN {
			u.Role = "admin"
		}
		if u.Nickname == "" {
			u.Nickname = uid
		}
		for _, d := range endpoints.GetDevices() {
			item := store.RuntimeUserDevice{ID: d.GetUniqueDeivceId(), Name: d.GetName(), Model: d.GetModel(), RemarkName: d.GetRemarkName(), Online: d.GetIsOnline()}
			if d.GetBindingTime() != nil {
				item.BindingTime = d.GetBindingTime().AsTime()
			}
			if d.GetLoginTime() != nil {
				item.LoginTime = d.GetLoginTime().AsTime()
			}
			u.Devices = append(u.Devices, item)
			u.TotalDevices++
			if item.Online {
				u.Online = true
				u.ActiveDevices++
			}
		}
		out = append(out, u)
	}
	s.mu.Lock()
	s.lastUID = actor
	s.mu.Unlock()
	return out, nil
}

func (s *Source) actor(ctx context.Context, actor string) (context.Context, error) {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return nil, errors.New("LazyCat user identity is missing")
	}
	return gohelper.WithRealUID(ctx, actor), nil
}
func (s *Source) Create(ctx context.Context, actor, uid, password, role string) error {
	q, e := s.actor(ctx, actor)
	if e != nil {
		return e
	}
	r := common.Role_ROLE_NORMAL
	if role == "admin" {
		r = common.Role_ROLE_ADMIN
	}
	_, e = s.users.CreateUser(q, &common.CreateUserRequest{Uid: uid, Password: password, Role: r})
	return e
}
func (s *Source) ChangeRole(ctx context.Context, actor, uid, role string) error {
	q, e := s.actor(ctx, actor)
	if e != nil {
		return e
	}
	r := common.Role_ROLE_NORMAL
	if role == "admin" {
		r = common.Role_ROLE_ADMIN
	}
	_, e = s.users.ChangeRole(q, &common.ChangeRoleReqeust{Uid: uid, Role: r})
	return e
}
func (s *Source) ResetPassword(ctx context.Context, actor, uid, password string) error {
	q, e := s.actor(ctx, actor)
	if e != nil {
		return e
	}
	_, e = s.users.ForceResetPassword(q, &common.ForceResetPasswordRequest{Uid: uid, NewPassword: password})
	return e
}
func (s *Source) Delete(ctx context.Context, actor, uid string, clear bool) error {
	q, e := s.actor(ctx, actor)
	if e != nil {
		return e
	}
	_, e = s.users.DeleteUser(q, &common.DeleteUserRequest{Uid: uid, ClearUserData: clear})
	return e
}

func (s *Source) SetAppAccess(ctx context.Context, actor, uid string, noLimit bool, allowedAppIDs []string) error {
	q, e := s.actor(ctx, actor)
	if e != nil {
		return e
	}
	_, e = s.access.SetAppAccessPolicy(q, &sys.AppAccessPolicyRequest{
		Uid: uid,
		Policy: &sys.AppAccessPolicy{
			NoLimit:           &noLimit,
			AllowAccessAppids: allowedAppIDs,
		},
	})
	return e
}
