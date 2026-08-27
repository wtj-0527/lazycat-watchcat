package runtimeusers

import (
	"context"
	"testing"

	"gitee.com/linakesi/lzc-sdk/lang/go/common"
	"gitee.com/linakesi/lzc-sdk/lang/go/sys"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fakeUsers struct{}

func (*fakeUsers) ListUIDs(context.Context, *common.ListUIDsRequest, ...grpc.CallOption) (*common.ListUIDsReply, error) {
	return &common.ListUIDsReply{Uids: []string{"user-1"}}, nil
}
func (*fakeUsers) QueryUserInfo(context.Context, *common.UserID, ...grpc.CallOption) (*common.UserInfo, error) {
	return &common.UserInfo{Nickname: "User"}, nil
}
func (*fakeUsers) CreateUser(context.Context, *common.CreateUserRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
func (*fakeUsers) ChangeRole(context.Context, *common.ChangeRoleReqeust, ...grpc.CallOption) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
func (*fakeUsers) ForceResetPassword(context.Context, *common.ForceResetPasswordRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}
func (*fakeUsers) DeleteUser(context.Context, *common.DeleteUserRequest, ...grpc.CallOption) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, nil
}

type fakeDevices struct {
	remove *common.RemoveEndDeviceRequest
	rename *common.SetDeviceRemarkNameRequest
}

func (*fakeDevices) ListEndDevices(context.Context, *common.ListEndDeviceRequest, ...grpc.CallOption) (*common.ListEndDeviceReply, error) {
	return &common.ListEndDeviceReply{}, nil
}
func (f *fakeDevices) RemoveEndDevice(_ context.Context, req *common.RemoveEndDeviceRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	f.remove = req
	return &emptypb.Empty{}, nil
}
func (f *fakeDevices) SetDeviceRemarkName(_ context.Context, req *common.SetDeviceRemarkNameRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	f.rename = req
	return &emptypb.Empty{}, nil
}

type fakeAccess struct {
	set *sys.AppAccessPolicyRequest
}

func TestRenameAndRemoveEndDevice(t *testing.T) {
	devices := &fakeDevices{}
	source := &Source{users: &fakeUsers{}, devices: devices, access: &fakeAccess{}}
	if err := source.RenameDevice(context.Background(), "admin", "user-1", "device-1", "工作电脑"); err != nil {
		t.Fatal(err)
	}
	if devices.rename == nil || devices.rename.GetUid() != "user-1" || devices.rename.GetUniqueDeviceId() != "device-1" || devices.rename.GetRemarkName() != "工作电脑" {
		t.Fatalf("rename request=%+v", devices.rename)
	}
	if err := source.RemoveDevice(context.Background(), "admin", "user-1", "device-1"); err != nil {
		t.Fatal(err)
	}
	if devices.remove == nil || devices.remove.GetUid() != "user-1" || devices.remove.GetUniqueDeivceId() != "device-1" {
		t.Fatalf("remove request=%+v", devices.remove)
	}
}

func (*fakeAccess) QueryAppAccessPolicy(context.Context, *sys.AppAccessPolicyRequest, ...grpc.CallOption) (*sys.AppAccessPolicy, error) {
	noLimit := false
	return &sys.AppAccessPolicy{NoLimit: &noLimit, AllowAccessAppids: []string{"app.one"}}, nil
}
func (f *fakeAccess) SetAppAccessPolicy(_ context.Context, req *sys.AppAccessPolicyRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	f.set = req
	return &emptypb.Empty{}, nil
}

func TestQueryAndSetAppAccessPolicy(t *testing.T) {
	access := &fakeAccess{}
	source := &Source{users: &fakeUsers{}, devices: &fakeDevices{}, access: access}
	items, err := source.Query(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].AppAccessNoLimit || len(items[0].AllowedAppIDs) != 1 || items[0].AllowedAppIDs[0] != "app.one" {
		t.Fatalf("items=%+v", items)
	}
	if err = source.SetAppAccess(context.Background(), "admin", "user-1", false, []string{"app.two"}); err != nil {
		t.Fatal(err)
	}
	if access.set == nil || access.set.GetUid() != "user-1" || access.set.GetPolicy().GetNoLimit() || len(access.set.GetPolicy().GetAllowAccessAppids()) != 1 {
		t.Fatalf("request=%+v", access.set)
	}
}
