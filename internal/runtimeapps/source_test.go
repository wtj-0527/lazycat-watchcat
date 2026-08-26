package runtimeapps

import (
	"context"
	"testing"
	"time"

	"gitee.com/linakesi/lzc-sdk/lang/go/common"
	"gitee.com/linakesi/lzc-sdk/lang/go/sys"
	"google.golang.org/grpc"
)

type fakePackageManager struct {
	calls int
}

func (f *fakePackageManager) QueryApplication(_ context.Context, _ *sys.QueryApplicationRequest, _ ...grpc.CallOption) (*sys.QueryApplicationResponse, error) {
	f.calls++
	version, title, domain := "1.5.0", "WatchCat", "watchcat.box.example"
	status := sys.InstanceStatus_Status_Running
	return &sys.QueryApplicationResponse{InfoList: []*sys.AppInfo{{
		Appid: "community.lazycat.app.watchcat", DeployId: "community.lazycat.app.watchcat6",
		Version: &version, Title: &title, Domain: &domain, Status: sys.AppStatus_Installed, InstanceStatus: &status,
	}}}, nil
}

func TestQueryNormalizesAndCachesApplications(t *testing.T) {
	client := &fakePackageManager{}
	source := NewWithClient(client, time.Minute)
	first, err := source.Query(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := source.Query(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || len(first) != 1 || len(second) != 1 {
		t.Fatalf("calls=%d first=%+v second=%+v", client.calls, first, second)
	}
	if first[0].InstanceStatus != "running" || first[0].InstallStatus != "installed" || first[0].Version != "1.5.0" {
		t.Fatalf("normalized app=%+v", first[0])
	}
}

func TestQueryRequiresUserIdentity(t *testing.T) {
	source := NewWithClient(&fakePackageManager{}, time.Minute)
	if _, err := source.Query(context.Background(), ""); err == nil {
		t.Fatal("empty uid was accepted")
	}
}

type fakeMultiUserManager struct {
	queryCalls int
}

func (f *fakeMultiUserManager) ListUIDs(_ context.Context, _ *common.ListUIDsRequest, _ ...grpc.CallOption) (*common.ListUIDsReply, error) {
	return &common.ListUIDsReply{Uids: []string{"owner-1", "owner-2", "owner-2"}}, nil
}

func (f *fakeMultiUserManager) QueryUserInfo(_ context.Context, request *common.UserID, _ ...grpc.CallOption) (*common.UserInfo, error) {
	f.queryCalls++
	return &common.UserInfo{Uid: request.GetUid(), Nickname: "name-" + request.GetUid()}, nil
}

type fakeMultiUserPackageManager struct {
	targets []string
}

func (f *fakeMultiUserPackageManager) QueryApplication(_ context.Context, request *sys.QueryApplicationRequest, _ ...grpc.CallOption) (*sys.QueryApplicationResponse, error) {
	target := request.GetOtherUid()
	f.targets = append(f.targets, target)
	running, paused := sys.InstanceStatus_Status_Running, sys.InstanceStatus_Status_Paused
	version, hermes := "2.0.0", "Hermes Studio"
	globalTitle := "Global"
	builtin := true
	global := &sys.AppInfo{Appid: "app.global", DeployId: "app.global", Title: &globalTitle, Version: &version, Status: sys.AppStatus_Installed, InstanceStatus: &running, Builtin: &builtin}
	if target == "owner-1" {
		owner := "owner-1"
		return &sys.QueryApplicationResponse{InfoList: []*sys.AppInfo{
			global,
			{Appid: "app.hermes", DeployId: "app.hermes1", Owner: owner, Title: &hermes, Version: &version, Status: sys.AppStatus_Installed, InstanceStatus: &running},
		}}, nil
	}
	owner := "owner-2"
	return &sys.QueryApplicationResponse{InfoList: []*sys.AppInfo{
		global,
		{Appid: "app.hermes", DeployId: "app.hermes2", Owner: owner, Title: &hermes, Version: &version, Status: sys.AppStatus_Installed, InstanceStatus: &paused},
	}}, nil
}

func TestQueryCollectsEveryUserAndDeduplicatesGlobalDeployments(t *testing.T) {
	client := &fakeMultiUserPackageManager{}
	users := &fakeMultiUserManager{}
	source := NewWithClients(client, users, time.Minute)
	items, err := source.Query(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(client.targets) != 2 || client.targets[0] != "owner-1" || client.targets[1] != "owner-2" {
		t.Fatalf("targets=%v", client.targets)
	}
	if len(items) != 3 {
		t.Fatalf("items=%+v", items)
	}
	var hermes int
	for _, item := range items {
		if item.AppID == "app.hermes" {
			hermes++
			if item.UserName != "name-"+item.UserID {
				t.Fatalf("user identity not resolved: %+v", item)
			}
		}
	}
	if hermes != 2 || users.queryCalls != 2 {
		t.Fatalf("hermes=%d userQueries=%d items=%+v", hermes, users.queryCalls, items)
	}
}
