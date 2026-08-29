package store

import (
	"context"
	"path/filepath"
	"sync"
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
	wifi := true
	user := RuntimeUser{UserID: "user-1", Nickname: "User", AppAccessNoLimit: false, AllowedAppIDs: []string{"app.one", "app.two"}, Online: true, ActiveDevices: 1, TotalDevices: 1, Devices: []RuntimeUserDevice{{
		ID: "client-1", Name: "iPad", Model: "iPad Air", DeviceAPIURL: "https://device.d.heiyu.space:443",
		IsMobile: true, Lang: "zh-Hans", TimeZone: "Asia/Shanghai", IsWifi: &wifi, Online: true, LoginTime: login,
	}}}
	if err = st.ObserveRuntimeUsers(ctx, deviceID, []RuntimeUser{user}); err != nil {
		t.Fatal(err)
	}
	users, err := st.ListRuntimeUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].AppAccessNoLimit || len(users[0].AllowedAppIDs) != 2 || users[0].AllowedAppIDs[1] != "app.two" {
		t.Fatalf("users=%+v", users)
	}
	endpoint := users[0].Devices[0]
	if endpoint.DeviceAPIURL != "https://device.d.heiyu.space:443" || !endpoint.IsMobile || endpoint.TimeZone != "Asia/Shanghai" || endpoint.IsWifi == nil || !*endpoint.IsWifi {
		t.Fatalf("endpoint=%+v", endpoint)
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

func TestListRuntimeUsersSupportsConcurrentReaders(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "users-concurrent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	deviceID, err := st.EnsureLocalDevice(ctx, "node", "node", "linux/amd64", "1.1.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	users := make([]RuntimeUser, 20)
	for index := range users {
		users[index] = RuntimeUser{
			UserID: "user-" + string(rune('a'+index)), Nickname: "User",
			Devices: []RuntimeUserDevice{{ID: "client", Name: "Client", Online: true}},
		}
	}
	if err = st.ObserveRuntimeUsers(ctx, deviceID, users); err != nil {
		t.Fatal(err)
	}

	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for index := 0; index < cap(errors); index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, readErr := st.ListRuntimeUsers(readCtx)
			if readErr == nil && len(items) != len(users) {
				readErr = context.Canceled
			}
			errors <- readErr
		}()
	}
	wg.Wait()
	close(errors)
	for readErr := range errors {
		if readErr != nil {
			t.Fatalf("concurrent ListRuntimeUsers failed: %v", readErr)
		}
	}
}
