package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/upgradecoord"
)

func (s *Server) upgradeCoordinatorStatus(w http.ResponseWriter, _ *http.Request) {
	if s.upgradeDocker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		active, queue, err := s.upgradeDocker.UpgradeQueue(ctx)
		cancel()
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "ok", "active": active, "queue": queue, "updatedAt": time.Now().UTC(),
			})
			return
		}
	}
	if s.docker != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		active, queue, err := s.docker.UpgradeQueue(ctx)
		cancel()
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "ok", "active": active, "queue": queue, "updatedAt": time.Now().UTC(),
			})
			return
		}
	}
	if s.upgradeCoordinator == nil {
		problem(w, http.StatusServiceUnavailable, "upgrade_coordinator_unavailable", "升级协调器不可用")
		return
	}
	writeJSON(w, http.StatusOK, s.upgradeCoordinator.Snapshot())
}

func (s *Server) acquireUpgradeLease(w http.ResponseWriter, r *http.Request) {
	if s.upgradeCoordinator == nil {
		problem(w, http.StatusServiceUnavailable, "upgrade_coordinator_unavailable", "升级协调器不可用")
		return
	}
	var request upgradecoord.Request
	if decodeJSON(r, &request) != nil {
		problem(w, http.StatusBadRequest, "invalid_upgrade_request", "升级协调请求格式无效")
		return
	}
	result, err := s.upgradeCoordinator.Acquire(request)
	if err != nil {
		problem(w, http.StatusBadRequest, "invalid_upgrade_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) renewUpgradeLease(w http.ResponseWriter, r *http.Request) {
	s.updateUpgradeLease(w, r, false)
}

func (s *Server) releaseUpgradeLease(w http.ResponseWriter, r *http.Request) {
	s.updateUpgradeLease(w, r, true)
}

func (s *Server) updateUpgradeLease(w http.ResponseWriter, r *http.Request, release bool) {
	if s.upgradeCoordinator == nil {
		problem(w, http.StatusServiceUnavailable, "upgrade_coordinator_unavailable", "升级协调器不可用")
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	if decodeJSON(r, &request) != nil || strings.TrimSpace(request.Token) == "" {
		problem(w, http.StatusBadRequest, "invalid_upgrade_lease", "租约 token 必填")
		return
	}
	var (
		result upgradecoord.Result
		err    error
	)
	if release {
		result, err = s.upgradeCoordinator.Release(request.Token)
	} else {
		result, err = s.upgradeCoordinator.Renew(request.Token)
	}
	if errors.Is(err, upgradecoord.ErrLeaseNotFound) {
		problem(w, http.StatusConflict, "upgrade_lease_expired", "升级租约不存在或已过期")
		return
	}
	if err != nil {
		problem(w, http.StatusInternalServerError, "upgrade_lease_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}
