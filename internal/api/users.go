package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

func (s *Server) usersView(w http.ResponseWriter, r *http.Request) {
	actor := strings.TrimSpace(r.Header.Get("X-Hc-User-Id"))
	if s.runtimeUsers != nil && s.localDeviceID != "" && actor != "" {
		if users, err := s.runtimeUsers.Query(r.Context(), actor); err == nil {
			_ = s.store.ObserveRuntimeUsers(r.Context(), s.localDeviceID, users)
		}
	}
	users, err := s.store.ListRuntimeUsers(r.Context())
	if err != nil {
		problem(w, 500, "internal_error", "无法读取用户状态")
		return
	}
	sessions, err := s.store.ListUserLoginSessions(r.Context(), time.Now().UTC().Add(-365*24*time.Hour))
	if err != nil {
		problem(w, 500, "internal_error", "无法读取登录历史")
		return
	}
	devices, _ := s.store.ListDevices(r.Context())
	names := map[string]string{}
	for _, d := range devices {
		names[d.ID] = d.Name
	}
	apps, _ := s.store.ListRuntimeApplications(r.Context())
	type item struct {
		DeviceID             string    `json:"deviceId"`
		DeviceName           string    `json:"deviceName"`
		Local                bool      `json:"local"`
		UserID               string    `json:"userId"`
		Nickname             string    `json:"nickname"`
		Role                 string    `json:"role"`
		AppInstallPermission bool      `json:"appInstallPermission"`
		Online               bool      `json:"online"`
		ActiveDevices        int       `json:"activeDevices"`
		TotalDevices         int       `json:"totalDevices"`
		ApplicationCount     int       `json:"applicationCount"`
		InstanceCount        int       `json:"instanceCount"`
		FirstObservedAt      time.Time `json:"firstObservedAt"`
		UpdatedAt            time.Time `json:"updatedAt"`
		LastLoginAt          time.Time `json:"lastLoginAt,omitempty"`
		LastLogoutAt         time.Time `json:"lastLogoutAt,omitempty"`
		OnlineSeconds24h     int64     `json:"onlineSeconds24h"`
		OnlineSeconds7d      int64     `json:"onlineSeconds7d"`
		OnlineSeconds30d     int64     `json:"onlineSeconds30d"`
		LoginCount           int       `json:"loginCount"`
		Devices              any       `json:"devices"`
		Sessions             any       `json:"sessions"`
	}
	out := make([]item, 0, len(users))
	now := time.Now().UTC()
	for _, u := range users {
		x := item{DeviceID: u.DeviceID, DeviceName: names[u.DeviceID], Local: u.DeviceID == s.localDeviceID, UserID: u.UserID, Nickname: u.Nickname, Role: u.Role, AppInstallPermission: u.AppInstallPermission, Online: u.Online, ActiveDevices: u.ActiveDevices, TotalDevices: u.TotalDevices, FirstObservedAt: u.FirstObservedAt, UpdatedAt: u.UpdatedAt, Devices: u.Devices}
		var own []userSessionView
		for _, session := range sessions {
			if session.DeviceID != u.DeviceID || session.UserID != u.UserID {
				continue
			}
			end := now
			if session.LogoutAt != nil {
				end = *session.LogoutAt
				if end.After(x.LastLogoutAt) {
					x.LastLogoutAt = end
				}
			}
			if session.LoginAt.After(x.LastLoginAt) {
				x.LastLoginAt = session.LoginAt
			}
			own = append(own, userSessionView{EndDeviceID: session.EndDeviceID, LoginAt: session.LoginAt, LogoutAt: session.LogoutAt, DurationSeconds: int64(end.Sub(session.LoginAt).Seconds())})
		}
		x.Sessions = own
		x.LoginCount = len(own)
		x.OnlineSeconds24h = unionOnlineSeconds(own, now.Add(-24*time.Hour), now)
		x.OnlineSeconds7d = unionOnlineSeconds(own, now.Add(-7*24*time.Hour), now)
		x.OnlineSeconds30d = unionOnlineSeconds(own, now.Add(-30*24*time.Hour), now)
		seenApps := map[string]bool{}
		for _, a := range apps {
			if a.DeviceID == u.DeviceID && a.UserID == u.UserID {
				x.InstanceCount++
				seenApps[a.AppID] = true
			}
		}
		x.ApplicationCount = len(seenApps)
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		if out[i].DeviceName != out[j].DeviceName {
			return out[i].DeviceName < out[j].DeviceName
		}
		return out[i].Nickname < out[j].Nickname
	})
	writeJSON(w, 200, map[string]any{"items": out, "count": len(out), "recordingSince": oldestUserObservation(users), "updatedAt": now})
}

type userSessionView struct {
	EndDeviceID     string     `json:"endDeviceId"`
	LoginAt         time.Time  `json:"loginAt"`
	LogoutAt        *time.Time `json:"logoutAt,omitempty"`
	DurationSeconds int64      `json:"durationSeconds"`
}

func unionOnlineSeconds(items []userSessionView, from, to time.Time) int64 {
	type interval struct{ a, b time.Time }
	var in []interval
	for _, x := range items {
		a, b := x.LoginAt, to
		if x.LogoutAt != nil {
			b = *x.LogoutAt
		}
		if a.Before(from) {
			a = from
		}
		if b.After(to) {
			b = to
		}
		if a.Before(b) {
			in = append(in, interval{a, b})
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].a.Before(in[j].a) })
	var total time.Duration
	var a, b time.Time
	for _, x := range in {
		if a.IsZero() {
			a, b = x.a, x.b
			continue
		}
		if !x.a.After(b) {
			if x.b.After(b) {
				b = x.b
			}
			continue
		}
		total += b.Sub(a)
		a, b = x.a, x.b
	}
	if !a.IsZero() {
		total += b.Sub(a)
	}
	return int64(total.Seconds())
}
func oldestUserObservation(items []store.RuntimeUser) time.Time {
	var out time.Time
	for _, x := range items {
		if !x.FirstObservedAt.IsZero() && (out.IsZero() || x.FirstObservedAt.Before(out)) {
			out = x.FirstObservedAt
		}
	}
	return out
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var req struct{ UserID, Password, Role string }
	if decodeJSON(r, &req) != nil || strings.TrimSpace(req.UserID) == "" || len(req.Password) < 8 {
		problem(w, 400, "invalid_user", "用户 ID 必填，密码至少 8 位")
		return
	}
	actor := strings.TrimSpace(r.Header.Get("X-Hc-User-Id"))
	if s.runtimeUsers == nil {
		problem(w, 503, "user_manager_unavailable", "用户管理服务不可用")
		return
	}
	if err := s.runtimeUsers.Create(r.Context(), actor, strings.TrimSpace(req.UserID), req.Password, req.Role); err != nil {
		problem(w, 502, "user_create_failed", "创建用户失败："+err.Error())
		return
	}
	_ = s.store.RecordAudit(r.Context(), "user.created", "lazycat_user", req.UserID, map[string]any{"role": req.Role})
	writeJSON(w, 201, map[string]any{"created": true})
}
func (s *Server) changeUserRole(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("id"))
	actor := strings.TrimSpace(r.Header.Get("X-Hc-User-Id"))
	if s.runtimeUsers == nil || uid == "" {
		problem(w, http.StatusServiceUnavailable, "user_manager_unavailable", "用户管理服务不可用")
		return
	}
	if uid == actor {
		problem(w, 409, "self_role_change_rejected", "不能修改当前登录用户自己的角色")
		return
	}
	var req struct{ Role string }
	if decodeJSON(r, &req) != nil || (req.Role != "admin" && req.Role != "normal") {
		problem(w, 400, "invalid_role", "角色必须是 admin 或 normal")
		return
	}
	if err := s.runtimeUsers.ChangeRole(r.Context(), actor, uid, req.Role); err != nil {
		problem(w, 502, "role_change_failed", "修改角色失败："+err.Error())
		return
	}
	_ = s.store.RecordAudit(r.Context(), "user.role_changed", "lazycat_user", uid, map[string]any{"role": req.Role})
	writeJSON(w, 200, map[string]any{"updated": true})
}
func (s *Server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("id"))
	actor := strings.TrimSpace(r.Header.Get("X-Hc-User-Id"))
	if s.runtimeUsers == nil || uid == "" {
		problem(w, http.StatusServiceUnavailable, "user_manager_unavailable", "用户管理服务不可用")
		return
	}
	var req struct{ Password string }
	if decodeJSON(r, &req) != nil || len(req.Password) < 8 {
		problem(w, 400, "invalid_password", "新密码至少 8 位")
		return
	}
	if err := s.runtimeUsers.ResetPassword(r.Context(), actor, uid, req.Password); err != nil {
		problem(w, 502, "password_reset_failed", "重置密码失败："+err.Error())
		return
	}
	_ = s.store.RecordAudit(r.Context(), "user.password_reset", "lazycat_user", uid, nil)
	writeJSON(w, 200, map[string]any{"updated": true})
}
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("id"))
	actor := strings.TrimSpace(r.Header.Get("X-Hc-User-Id"))
	if s.runtimeUsers == nil || uid == "" {
		problem(w, http.StatusServiceUnavailable, "user_manager_unavailable", "用户管理服务不可用")
		return
	}
	if uid == actor {
		problem(w, 409, "self_delete_rejected", "不能删除当前登录用户")
		return
	}
	clear := r.URL.Query().Get("clearData") == "true"
	if err := s.runtimeUsers.Delete(r.Context(), actor, uid, clear); err != nil {
		problem(w, 502, "user_delete_failed", "删除用户失败："+err.Error())
		return
	}
	_ = s.store.RecordAudit(r.Context(), "user.deleted", "lazycat_user", uid, map[string]any{"clearData": clear})
	w.WriteHeader(204)
}
