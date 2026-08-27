package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
	"github.com/wtj-0527/lazycat-watchcat/internal/store"
)

func (s *Server) applicationInstanceAction(w http.ResponseWriter, r *http.Request) {
	appID := strings.TrimSpace(r.PathValue("id"))
	deployID := strings.TrimSpace(r.PathValue("deployId"))
	var request struct {
		DeviceID  string `json:"deviceId"`
		Action    string `json:"action"`
		Autostart *bool  `json:"autostart"`
	}
	if decodeJSON(r, &request) != nil || appID == "" || deployID == "" {
		problem(w, http.StatusBadRequest, "invalid_application_action", "应用、实例和操作参数必填")
		return
	}
	request.DeviceID, request.Action = strings.TrimSpace(request.DeviceID), strings.TrimSpace(request.Action)
	if request.Action != "start" && request.Action != "stop" && request.Action != "set_autostart" {
		problem(w, http.StatusBadRequest, "unsupported_application_action", "仅支持启动、停止和设置开机自启动")
		return
	}
	if request.Action == "set_autostart" && request.Autostart == nil {
		problem(w, http.StatusBadRequest, "autostart_required", "必须提供开机自启动状态")
		return
	}
	var instance store.RuntimeApplication
	found := false
	items, err := s.store.ListRuntimeApplications(r.Context())
	if err != nil {
		problem(w, http.StatusInternalServerError, "application_state_failed", "无法读取应用实例状态")
		return
	}
	for _, item := range items {
		if item.DeviceID == request.DeviceID && item.DeployID == deployID && item.AppID == appID {
			instance, found = item, true
			break
		}
	}
	if !found {
		problem(w, http.StatusNotFound, "application_instance_not_found", "应用实例不存在或设备不匹配")
		return
	}
	if applicationControlRestricted(instance) {
		problem(w, http.StatusConflict, "protected_application", "该系统或监控实例不允许在 WatchCat 中修改运行状态")
		return
	}
	command := protocol.ApplicationCommand{
		DeviceID: request.DeviceID, DeployID: deployID, AppID: appID, UserID: instance.UserID,
		Action: request.Action, Autostart: request.Autostart,
	}
	if request.DeviceID == s.localDeviceID {
		if s.runtimeApps == nil {
			problem(w, http.StatusServiceUnavailable, "runtime_control_unavailable", "LazyCat Package Manager 控制接口不可用")
			return
		}
		uid := strings.TrimSpace(r.Header.Get("X-Hc-User-Id"))
		if uid == "" {
			uid = s.runtimeApps.LastUID()
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		result, err := s.runtimeApps.Control(ctx, uid, deployID, request.Action, request.Autostart)
		if err != nil {
			_ = s.store.RecordAudit(r.Context(), "application.instance.control_failed", "application_instance", deployID,
				map[string]any{"deviceId": request.DeviceID, "appId": appID, "action": request.Action, "error": err.Error()})
			problem(w, http.StatusBadGateway, "runtime_control_failed", err.Error())
			return
		}
		if result.Autostart != nil {
			_ = s.store.SetApplicationAutostart(r.Context(), request.DeviceID, deployID, *result.Autostart)
		}
		_, _ = s.SyncRuntimeApplications(context.Background(), uid)
		_ = s.store.RecordAudit(r.Context(), "application.instance."+request.Action, "application_instance", deployID,
			map[string]any{"deviceId": request.DeviceID, "appId": appID, "autostart": result.Autostart})
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "succeeded", "deviceId": request.DeviceID, "deployId": deployID,
			"instanceStatus": result.InstanceStatus, "autostart": result.Autostart,
		})
		return
	}
	created, err := s.store.CreateApplicationCommand(r.Context(), command)
	if err != nil {
		problem(w, http.StatusInternalServerError, "application_command_failed", "无法创建远端应用操作")
		return
	}
	_ = s.store.RecordAudit(r.Context(), "application.instance.command_queued", "application_instance", deployID,
		map[string]any{"commandId": created.ID, "deviceId": request.DeviceID, "appId": appID, "action": request.Action, "autostart": request.Autostart})
	writeJSON(w, http.StatusAccepted, created)
}

func applicationControlRestricted(instance store.RuntimeApplication) bool {
	return instance.Builtin || strings.HasPrefix(instance.AppID, "cloud.lazycat.shell.") ||
		instance.AppID == "community.lazycat.app.watchcat"
}

func (s *Server) applicationOperation(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.ApplicationCommandByID(r.Context(), strings.TrimSpace(r.PathValue("id")))
	if errors.Is(err, context.Canceled) {
		return
	}
	if store.IsNotFound(err) {
		problem(w, http.StatusNotFound, "application_operation_not_found", "应用操作不存在")
		return
	}
	if err != nil {
		problem(w, http.StatusInternalServerError, "application_operation_failed", "无法读取应用操作")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) nextCollectorCommand(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.authenticateCollectorBearer(w, r)
	if !ok {
		return
	}
	command, err := s.store.ClaimApplicationCommand(r.Context(), deviceID)
	if err != nil {
		problem(w, http.StatusInternalServerError, "application_command_claim_failed", "无法领取应用操作")
		return
	}
	if command == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, command)
}

func (s *Server) completeCollectorCommand(w http.ResponseWriter, r *http.Request) {
	deviceID, ok := s.authenticateCollectorBearer(w, r)
	if !ok {
		return
	}
	var result protocol.ApplicationCommandResult
	if decodeJSON(r, &result) != nil || strings.TrimSpace(result.ID) == "" || result.ID != r.PathValue("id") {
		problem(w, http.StatusBadRequest, "invalid_application_command_result", "应用操作结果无效")
		return
	}
	command, commandErr := s.store.ApplicationCommandByID(r.Context(), result.ID)
	if commandErr != nil || command.DeviceID != deviceID {
		problem(w, http.StatusNotFound, "application_command_not_found", "设备操作不存在")
		return
	}
	if err := s.store.CompleteApplicationCommand(r.Context(), deviceID, result); err != nil {
		if store.IsNotFound(err) {
			problem(w, http.StatusNotFound, "application_command_not_found", "应用操作不存在")
			return
		}
		problem(w, http.StatusInternalServerError, "application_command_complete_failed", "无法保存应用操作结果")
		return
	}
	if result.Success && command.Action == removeUserEndDeviceAction {
		if err := s.store.DeleteRuntimeUserDevice(r.Context(), deviceID, command.UserID, command.DeployID); err != nil && !store.IsNotFound(err) {
			problem(w, http.StatusInternalServerError, "end_device_persist_failed", "终端已删除，但中心状态清理失败")
			return
		}
	}
	action := "application.instance.command_failed"
	if result.Success {
		action = "application.instance.command_succeeded"
	}
	if command.Action == removeUserEndDeviceAction {
		action = "user.end_device.remove_failed"
		if result.Success {
			action = "user.end_device.removed"
		}
	}
	_ = s.store.RecordAudit(r.Context(), action, "application_command", result.ID,
		map[string]any{"deviceId": deviceID, "instanceStatus": result.InstanceStatus, "autostart": result.Autostart, "error": result.Error})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authenticateCollectorBearer(w http.ResponseWriter, r *http.Request) (string, bool) {
	deviceID := strings.TrimSpace(r.Header.Get("X-WatchCat-Device-ID"))
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if deviceID == "" || token == "" || token == r.Header.Get("Authorization") {
		problem(w, http.StatusUnauthorized, "unauthorized", "缺少设备凭据")
		return "", false
	}
	if err := s.store.AuthenticateDevice(r.Context(), deviceID, token); err != nil {
		problem(w, http.StatusUnauthorized, "unauthorized", "设备凭据无效")
		return "", false
	}
	return deviceID, true
}
