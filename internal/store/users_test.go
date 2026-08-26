package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestObserveRuntimeUsersCreatesAndClosesLoginSession(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "users.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	deviceID, err := st.EnsureLocalDevice(ctx, "node", "node", "linux/amd64", "1.1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	login := time.Now().UTC().Add(-time.Hour)
	user := RuntimeUser{UserID: "user-1", Nickname: "User", Online: true, ActiveDevices: 1, TotalDevices: 1, Devices: []RuntimeUserDevice{{ID: "client-1", Online: true, LoginTime: login}}}
	if err = st.ObserveRuntimeUsers(ctx, deviceID, []RuntimeUser{user}); err != nil {
		t.Fatal(err)
	}
	sessions, err := st.ListUserLoginSessions(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].LogoutAt != nil {
		t.Fatalf("sessions=%+v", sessions)
	}
	user.Online = false
	user.ActiveDevices = 0
	user.Devices[0].Online = false
	if err = st.ObserveRuntimeUsers(ctx, deviceID, []RuntimeUser{user}); err != nil {
		t.Fatal(err)
	}
	sessions, err = st.ListUserLoginSessions(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].LogoutAt == nil {
		t.Fatalf("sessions=%+v", sessions)
	}
}
