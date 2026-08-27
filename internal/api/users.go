package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

const removeUserEndDeviceAction = "remove_user_end_device"

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
		DeviceID             string                    `json:"deviceId"`
		DeviceName           string                    `json:"deviceName"`
		Local                bool                      `json:"local"`
		UserID               string                    `json:"userId"`
		Nickname             string                    `json:"nickname"`
		Role                 string                    `json:"role"`
		AppInstallPermission bool                      `json:"appInstallPermission"`
		AppAccessNoLimit     bool                      `json:"appAccessNoLimit"`
		AllowedAppIDs        []string                  `json:"allowedAppIds"`
		Online               bool                      `json:"online"`
		ActiveDevices        int                       `json:"activeDevices"`
		TotalDevices         int                       `json:"totalDevices"`
		ApplicationCount     int                       `json:"applicationCount"`
		InstanceCount        int                       `json:"instanceCount"`
		FirstObservedAt      time.Time                 `json:"firstObservedAt"`
		UpdatedAt            time.Time                 `json:"updatedAt"`
		LastLoginAt          time.Time                 `json:"lastLoginAt,omitempty"`
		LastLogoutAt         time.Time                 `json:"lastLogoutAt,omitempty"`
		OnlineSeconds24h     int64                     `json:"onlineSeconds24h"`
		OnlineSeconds7d      int64                     `json:"onlineSeconds7d"`
		OnlineSeconds30d     int64                     `json:"onlineSeconds30d"`
		LoginCount           int                       `json:"loginCount"`
		Devices              []store.RuntimeUserDevice `json:"devices"`
		Sessions             []userSessionView         `json:"sessions"`
	}
	out := make([]item, 0, len(users))
	now := time.Now().UTC()
	for _, u := range users {
		x := item{DeviceID: u.DeviceID, DeviceName: names[u.DeviceID], Local: u.DeviceID == s.localDeviceID, UserID: u.UserID, Nickname: u.Nickname, Role: u.Role, AppInstallPermission: u.AppInstallPermission, AppAccessNoLimit: u.AppAccessNoLimit, AllowedAppIDs: append([]string{}, u.AllowedAppIDs...), Online: u.Online, ActiveDevices: u.ActiveDevices, TotalDevices: u.TotalDevices, FirstObservedAt: u.FirstObservedAt, UpdatedAt: u.UpdatedAt, Devices: append([]store.RuntimeUserDevice{}, u.Devices...)}
		own := make([]userSessionView, 0)
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

func (s *Server) updateUserAppAccess(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("id"))
	actor := strings.TrimSpace(r.Header.Get("X-Hc-User-Id"))
	if s.runtimeUsers == nil || uid == "" {
		problem(w, http.StatusServiceUnavailable, "user_manager_unavailable", "用户管理服务不可用")
		return
	}
	var req struct {
		NoLimit       bool     `json:"noLimit"`
		AllowedAppIDs []string `json:"allowedAppIds"`
	}
	if decodeJSON(r, &req) != nil {
		problem(w, 400, "invalid_app_access", "应用可见范围格式无效")
		return
	}
	seen := map[string]bool{}
	allowed := make([]string, 0, len(req.AllowedAppIDs))
	for _, raw := range req.AllowedAppIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		if len(id) > 256 {
			problem(w, 400, "invalid_app_id", "应用 ID 长度不能超过 256 个字符")
			return
		}
		seen[id] = true
		allowed = append(allowed, id)
	}
	if len(allowed) > 1000 {
		problem(w, 400, "too_many_apps", "单个用户最多允许配置 1000 个应用")
		return
	}
	sort.Strings(allowed)
	if req.NoLimit {
		allowed = nil
	}
	if err := s.runtimeUsers.SetAppAccess(r.Context(), actor, uid, req.NoLimit, allowed); err != nil {
		problem(w, 502, "app_access_update_failed", "修改应用可见范围失败："+err.Error())
		return
	}
	users, err := s.runtimeUsers.Query(r.Context(), actor)
	if err != nil {
		problem(w, 502, "app_access_verify_failed", "权限已提交，但服务端回读失败："+err.Error())
		return
	}
	if err = s.store.ObserveRuntimeUsers(r.Context(), s.localDeviceID, users); err != nil {
		problem(w, 500, "app_access_persist_failed", "权限已提交，但保存回读结果失败")
		return
	}
	var updated *store.RuntimeUser
	for i := range users {
		if users[i].UserID == uid {
			updated = &users[i]
			break
		}
	}
	if updated == nil {
		problem(w, 404, "user_not_found", "服务端回读时未找到该用户")
		return
	}
	_ = s.store.RecordAudit(r.Context(), "user.app_access.updated", "lazycat_user", uid, map[string]any{"noLimit": updated.AppAccessNoLimit, "allowedAppIds": updated.AllowedAppIDs})
	writeJSON(w, 200, map[string]any{"updated": true, "noLimit": updated.AppAccessNoLimit, "allowedAppIds": updated.AllowedAppIDs})
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

func (s *Server) renameUserEndDevice(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("id"))
	endDeviceID := strings.TrimSpace(r.PathValue("deviceId"))
	actor := strings.TrimSpace(r.Header.Get("X-Hc-User-Id"))
	if s.runtimeUsers == nil {
		problem(w, http.StatusServiceUnavailable, "user_manager_unavailable", "用户管理服务不可用")
		return
	}
	if uid == "" || endDeviceID == "" {
		problem(w, http.StatusBadRequest, "invalid_end_device", "用户 ID 和终端 ID 必填")
		return
	}
	if !s.isLocalUserEndDevice(r, uid, endDeviceID) {
		problem(w, http.StatusNotFound, "end_device_not_found", "本机用户中未找到该登录终端")
		return
	}
	var req struct {
		RemarkName string `json:"remarkName"`
	}
	if decodeJSON(r, &req) != nil {
		problem(w, http.StatusBadRequest, "invalid_remark_name", "终端备注格式无效")
		return
	}
	req.RemarkName = strings.TrimSpace(req.RemarkName)
	if len([]rune(req.RemarkName)) > 64 {
		problem(w, http.StatusBadRequest, "invalid_remark_name", "终端备注不能超过 64 个字符")
		return
	}
	if err := s.runtimeUsers.RenameDevice(r.Context(), actor, uid, endDeviceID, req.RemarkName); err != nil {
		problem(w, http.StatusBadGateway, "end_device_rename_failed", "修改终端备注失败："+err.Error())
		return
	}
	users, err := s.runtimeUsers.Query(r.Context(), actor)
	if err != nil {
		problem(w, http.StatusBadGateway, "end_device_verify_failed", "备注已提交，但服务端回读失败："+err.Error())
		return
	}
	if err = s.store.ObserveRuntimeUsers(r.Context(), s.localDeviceID, users); err != nil {
		problem(w, http.StatusInternalServerError, "end_device_persist_failed", "备注已提交，但保存回读结果失败")
		return
	}
	_ = s.store.RecordAudit(r.Context(), "user.end_device.renamed", "lazycat_end_device", endDeviceID, map[string]any{"userId": uid, "remarkName": req.RemarkName})
	writeJSON(w, http.StatusOK, map[string]any{"updated": true, "remarkName": req.RemarkName})
}

func (s *Server) removeUserEndDevice(w http.ResponseWriter, r *http.Request) {
	uid := strings.TrimSpace(r.PathValue("id"))
	endDeviceID := strings.TrimSpace(r.PathValue("deviceId"))
	targetDeviceID := strings.TrimSpace(r.URL.Query().Get("deviceId"))
	actor := strings.TrimSpace(r.Header.Get("X-Hc-User-Id"))
	if uid == "" || endDeviceID == "" {
		problem(w, http.StatusBadRequest, "invalid_end_device", "用户 ID 和终端 ID 必填")
		return
	}
	if targetDeviceID == "" {
		targetDeviceID = s.localDeviceID
	}
	if !s.hasUserEndDevice(r, targetDeviceID, uid, endDeviceID) {
		problem(w, http.StatusNotFound, "end_device_not_found", "指定设备的用户中未找到该登录终端")
		return
	}
	if targetDeviceID != s.localDeviceID {
		created, err := s.store.CreateApplicationCommand(r.Context(), protocol.ApplicationCommand{
			DeviceID: targetDeviceID,
			DeployID: endDeviceID,
			AppID:    "community.lazycat.app.watchcat.user-end-device",
			UserID:   uid,
			Action:   removeUserEndDeviceAction,
		})
		if err != nil {
			problem(w, http.StatusInternalServerError, "end_device_command_failed", "无法创建远端终端删除操作")
			return
		}
		_ = s.store.RecordAudit(r.Context(), "user.end_device.remove_queued", "lazycat_end_device", endDeviceID,
			map[string]any{"commandId": created.ID, "deviceId": targetDeviceID, "userId": uid})
		writeJSON(w, http.StatusAccepted, created)
		return
	}
	if s.runtimeUsers == nil {
		problem(w, http.StatusServiceUnavailable, "user_manager_unavailable", "用户管理服务不可用")
		return
	}
	if err := s.runtimeUsers.RemoveDevice(r.Context(), actor, uid, endDeviceID); err != nil {
		problem(w, http.StatusBadGateway, "end_device_remove_failed", "删除登录终端失败："+err.Error())
		return
	}
	users, err := s.runtimeUsers.Query(r.Context(), actor)
	if err != nil {
		problem(w, http.StatusBadGateway, "end_device_verify_failed", "终端已删除，但服务端回读失败："+err.Error())
		return
	}
	if err = s.store.ObserveRuntimeUsers(r.Context(), s.localDeviceID, users); err != nil {
		problem(w, http.StatusInternalServerError, "end_device_persist_failed", "终端已删除，但保存回读结果失败")
		return
	}
	if err = s.store.DeleteRuntimeUserDevice(r.Context(), s.localDeviceID, uid, endDeviceID); err != nil && !store.IsNotFound(err) {
		problem(w, http.StatusInternalServerError, "end_device_persist_failed", "终端已删除，但清理本地终端状态失败")
		return
	}
	_ = s.store.RecordAudit(r.Context(), "user.end_device.removed", "lazycat_end_device", endDeviceID, map[string]any{"userId": uid})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) hasUserEndDevice(r *http.Request, deviceID, uid, endDeviceID string) bool {
	users, err := s.store.ListRuntimeUsers(r.Context())
	if err != nil {
		return false
	}
	for _, user := range users {
		if user.DeviceID != deviceID || user.UserID != uid {
			continue
		}
		for _, endpoint := range user.Devices {
			if endpoint.ID == endDeviceID {
				return true
			}
		}
	}
	return false
}

func (s *Server) isLocalUserEndDevice(r *http.Request, uid, endDeviceID string) bool {
	return s.hasUserEndDevice(r, s.localDeviceID, uid, endDeviceID)
}
