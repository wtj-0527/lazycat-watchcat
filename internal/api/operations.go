package api

import (
	"net/http"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/buildinfo"
)

func (s *Server) versionView(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": buildinfo.Version})
}

func (s *Server) listBackups(w http.ResponseWriter, _ *http.Request) {
	if s.backup == nil {
		problem(w, http.StatusServiceUnavailable, "backup_unavailable", "备份服务未配置")
		return
	}
	items, err := s.backup.List()
	if err != nil {
		problem(w, http.StatusInternalServerError, "backup_list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (s *Server) createBackup(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		problem(w, http.StatusServiceUnavailable, "backup_unavailable", "备份服务未配置")
		return
	}
	item, err := s.backup.Create(r.Context(), "manual")
	if err != nil {
		problem(w, http.StatusInternalServerError, "backup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil || s.restart == nil {
		problem(w, http.StatusServiceUnavailable, "restore_unavailable", "恢复服务未配置")
		return
	}
	name := r.PathValue("name")
	if err := s.backup.StageRestore(name); err != nil {
		problem(w, http.StatusBadRequest, "restore_rejected", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "restart-scheduled",
		"backup":  name,
		"message": "恢复请求已校验，应用即将重启并原子替换数据库",
	})
	go func() {
		time.Sleep(750 * time.Millisecond)
		s.restart()
	}()
}

func (s *Server) databaseStatus(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		problem(w, http.StatusServiceUnavailable, "backup_unavailable", "备份服务未配置")
		return
	}
	writeJSON(w, http.StatusOK, s.backup.Status(r.Context()))
}

func (s *Server) stabilityStatus(w http.ResponseWriter, _ *http.Request) {
	if s.stability == nil {
		problem(w, http.StatusServiceUnavailable, "stability_unavailable", "稳定性观测未配置")
		return
	}
	writeJSON(w, http.StatusOK, s.stability.Current())
}

func (s *Server) resetStability(w http.ResponseWriter, r *http.Request) {
	if s.stability == nil {
		problem(w, http.StatusServiceUnavailable, "stability_unavailable", "稳定性观测未配置")
		return
	}
	status, err := s.stability.Reset(r.Context())
	if err != nil {
		problem(w, http.StatusInternalServerError, "stability_reset_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}
