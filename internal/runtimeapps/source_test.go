package runtimeapps

import (
	"context"
	"testing"
	"time"

	"gitee.com/linakesi/lzc-sdk/lang/go/sys"
	"google.golang.org/grpc"
)

type fakePackageManager struct {
	calls int
}

func (f *fakePackageManager) QueryApplication(_ context.Context, _ *sys.QueryApplicationRequest, _ ...grpc.CallOption) (*sys.QueryApplicationResponse, error) {
	f.calls++
	version, title, domain := "1.5.0", "猫眼", "maoyan.box.example"
	status := sys.InstanceStatus_Status_Running
	return &sys.QueryApplicationResponse{InfoList: []*sys.AppInfo{{
		Appid: "community.lazycat.app.maoyan", DeployId: "community.lazycat.app.maoyan6",
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
