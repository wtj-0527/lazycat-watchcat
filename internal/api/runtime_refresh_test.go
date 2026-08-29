package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/runtimeapps"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

type blockingRuntimeUsers struct {
	started chan struct{}
	release chan struct{}
}

func (f *blockingRuntimeUsers) LastUID() string { return "" }
func (f *blockingRuntimeUsers) Query(ctx context.Context, _ string) ([]store.RuntimeUser, error) {
	select {
	case <-f.started:
	default:
		close(f.started)
	}
	select {
	case <-f.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (*blockingRuntimeUsers) Create(context.Context, string, string, string, string) error {
	return nil
}
func (*blockingRuntimeUsers) ChangeRole(context.Context, string, string, string) error { return nil }
func (*blockingRuntimeUsers) ResetPassword(context.Context, string, string, string) error {
	return nil
}
func (*blockingRuntimeUsers) Delete(context.Context, string, string, bool) error { return nil }
func (*blockingRuntimeUsers) RenameDevice(context.Context, string, string, string, string) error {
	return nil
}
func (*blockingRuntimeUsers) RemoveDevice(context.Context, string, string, string) error { return nil }
func (*blockingRuntimeUsers) SetAppAccess(context.Context, string, string, bool, []string) error {
	return nil
}

type blockingRuntimeApps struct {
	started chan struct{}
	release chan struct{}
}

func (f *blockingRuntimeApps) LastUID() string { return "" }
func (f *blockingRuntimeApps) Query(ctx context.Context, _ string) ([]runtimeapps.Application, error) {
	select {
	case <-f.started:
	default:
		close(f.started)
	}
	select {
	case <-f.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (*blockingRuntimeApps) Control(context.Context, string, string, string, *bool) (runtimeapps.ControlResult, error) {
	return runtimeapps.ControlResult{}, nil
}

func TestRuntimeBackendsNeverBlockPollingViews(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "runtime-refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	deviceID, err := st.EnsureLocalDevice(context.Background(), "node", "node", "linux", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	server := New(st, nil, "", time.Minute)
	users := &blockingRuntimeUsers{started: make(chan struct{}), release: make(chan struct{})}
	apps := &blockingRuntimeApps{started: make(chan struct{}), release: make(chan struct{})}
	server.ConfigureRuntimeUsers(users, deviceID)
	server.ConfigureRuntimeApps(apps, deviceID)

	assertFast := func(path string, handler func(http.ResponseWriter, *http.Request)) {
		t.Helper()
		request := httptest.NewRequest("GET", path, nil)
		request.Header.Set("X-Hc-User-Id", "admin")
		response := httptest.NewRecorder()
		startedAt := time.Now()
		handler(response, request)
		if elapsed := time.Since(startedAt); elapsed > 250*time.Millisecond {
			t.Fatalf("%s blocked for %s", path, elapsed)
		}
		if response.Code != 200 {
			t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}

	assertFast("/api/v1/users", server.usersView)
	assertFast("/api/v1/applications", server.applications)
	select {
	case <-users.started:
	case <-time.After(time.Second):
		t.Fatal("user runtime refresh was not scheduled")
	}
	select {
	case <-apps.started:
	case <-time.After(time.Second):
		t.Fatal("application runtime refresh was not scheduled")
	}
	close(users.release)
	close(apps.release)
	deadline := time.Now().Add(time.Second)
	for {
		server.runtimeRefreshMu.Lock()
		syncing := server.runtimeUsersSyncing || server.runtimeAppsSyncing
		server.runtimeRefreshMu.Unlock()
		if !syncing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runtime refresh workers did not stop")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
