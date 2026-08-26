package api

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

func (s *Server) upstreamStatus(w http.ResponseWriter, _ *http.Request) {
	if s.upstream == nil {
		problem(w, http.StatusServiceUnavailable, "upstream_unavailable", "上游连接组件未启用")
		return
	}
	writeJSON(w, http.StatusOK, s.upstream.Status())
}

func (s *Server) joinUpstream(w http.ResponseWriter, r *http.Request) {
	if s.upstream == nil {
		problem(w, http.StatusServiceUnavailable, "upstream_unavailable", "上游连接组件未启用")
		return
	}
	var request struct {
		Invitation string `json:"invitation"`
	}
	if err := decodeJSON(r, &request); err != nil || strings.TrimSpace(request.Invitation) == "" {
		problem(w, http.StatusBadRequest, "invalid_invitation", "请粘贴完整的设备邀请")
		return
	}
	hostname := strings.TrimSpace(os.Getenv("LAZYCAT_BOX_NAME"))
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	name := strings.TrimSpace(os.Getenv("WATCHCAT_LOCAL_DEVICE_NAME"))
	if name == "" {
		name = hostname
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	status, err := s.upstream.Join(ctx, request.Invitation, name, hostname)
	if err != nil {
		problem(w, http.StatusBadGateway, "upstream_pairing_failed", "加入失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, status)
}

func (s *Server) disconnectUpstream(w http.ResponseWriter, r *http.Request) {
	if s.upstream == nil {
		problem(w, http.StatusServiceUnavailable, "upstream_unavailable", "上游连接组件未启用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := s.upstream.RemoveBoth(ctx); err != nil {
		problem(w, http.StatusBadGateway, "upstream_remove_failed", "无法在主 WatchCat 删除设备，请检查连接后重试："+err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
