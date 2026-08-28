package api

import (
	"context"
	"net/http"
	"time"

	"github.com/wtj-0527/lazycat-watchcat/internal/buildinfo"
	"github.com/wtj-0527/lazycat-watchcat/internal/collector"
	"github.com/wtj-0527/lazycat-watchcat/internal/protocol"
)

type dockerMaintenance interface {
	UnusedImages(context.Context) (collector.UnusedImageSummary, error)
	PruneUnusedImages(context.Context) (collector.ImagePruneResult, error)
	DeleteUnusedImage(context.Context, string) (collector.ImageDeleteResult, error)
	UpgradeQueue(context.Context) (*collector.UpgradeQueueEntry, []collector.UpgradeQueueEntry, error)
	CollectStorageInventory(context.Context, time.Time) ([]protocol.MetricPoint, []string)
	CollectSMART(context.Context, time.Time) ([]protocol.MetricPoint, []string)
}

func (s *Server) versionView(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": buildinfo.Version})
}

func (s *Server) storageCheck(w http.ResponseWriter, r *http.Request) {
	if s.docker == nil || s.localDeviceID == "" {
		problem(w, http.StatusServiceUnavailable, "storage_check_unavailable", "存储只读检查服务未配置")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	now := time.Now().UTC()
	points, warnings := s.docker.CollectStorageInventory(ctx, now)
	smartPoints, smartWarnings := s.docker.CollectSMART(ctx, now)
	points = append(points, smartPoints...)
	warnings = append(warnings, smartWarnings...)
	if len(points) == 0 {
		problem(w, http.StatusServiceUnavailable, "storage_check_failed", "未获得物理磁盘或 Btrfs 数据")
		return
	}
	if err := s.store.IngestMetrics(ctx, protocol.MetricBatch{DeviceID: s.localDeviceID, Points: points}); err != nil {
		problem(w, http.StatusInternalServerError, "storage_check_store_failed", "无法保存存储检查结果")
		return
	}
	_ = s.store.RecordAudit(ctx, "storage.check.completed", "device", s.localDeviceID, map[string]any{"points": len(points), "warnings": warnings})
	writeJSON(w, http.StatusOK, map[string]any{"points": len(points), "warnings": warnings, "checkedAt": now})
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

func (s *Server) deleteBackup(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		problem(w, http.StatusServiceUnavailable, "backup_unavailable", "备份服务未配置")
		return
	}
	name := r.PathValue("name")
	if err := s.backup.Delete(name); err != nil {
		problem(w, http.StatusBadRequest, "backup_delete_rejected", err.Error())
		return
	}
	_ = s.store.RecordAudit(r.Context(), "backup.deleted", "backup", name, nil)
	w.WriteHeader(http.StatusNoContent)
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
	status := s.backup.Status(r.Context())
	if s.stability != nil {
		observed := s.stability.Current()
		status.IntegrityOK = observed.DatabaseIntegrityOK
		if observed.DatabaseIntegrityAt != nil {
			status.CheckedAt = *observed.DatabaseIntegrityAt
		} else {
			status.IntegrityError = "数据库完整性检查尚未完成"
		}
	} else {
		status.IntegrityError = "稳定性观测未配置"
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) unusedDockerImages(w http.ResponseWriter, r *http.Request) {
	if s.docker == nil {
		problem(w, http.StatusServiceUnavailable, "docker_maintenance_unavailable", "Docker 镜像维护服务未配置")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := s.docker.UnusedImages(ctx)
	if err != nil {
		problem(w, http.StatusServiceUnavailable, "docker_images_failed", "无法读取未引用镜像")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) pruneDockerImages(w http.ResponseWriter, r *http.Request) {
	if s.docker == nil {
		problem(w, http.StatusServiceUnavailable, "docker_maintenance_unavailable", "Docker 镜像维护服务未配置")
		return
	}
	select {
	case s.dockerPrune <- struct{}{}:
		defer func() { <-s.dockerPrune }()
	default:
		problem(w, http.StatusConflict, "docker_prune_in_progress", "镜像清理正在执行")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	result, err := s.docker.PruneUnusedImages(ctx)
	if err != nil {
		_ = s.store.RecordAudit(r.Context(), "docker.images.prune_failed", "device", s.localDeviceID, map[string]any{"error": err.Error()})
		problem(w, http.StatusInternalServerError, "docker_prune_failed", "未引用镜像清理失败")
		return
	}
	_ = s.store.RecordAudit(r.Context(), "docker.images.pruned", "device", s.localDeviceID, result)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) deleteDockerImage(w http.ResponseWriter, r *http.Request) {
	if s.docker == nil {
		problem(w, http.StatusServiceUnavailable, "docker_maintenance_unavailable", "Docker 镜像维护服务未配置")
		return
	}
	select {
	case s.dockerPrune <- struct{}{}:
		defer func() { <-s.dockerPrune }()
	default:
		problem(w, http.StatusConflict, "docker_prune_in_progress", "镜像清理正在执行")
		return
	}
	imageID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	result, err := s.docker.DeleteUnusedImage(ctx, imageID)
	if err != nil {
		_ = s.store.RecordAudit(r.Context(), "docker.image.delete_failed", "docker_image", imageID, map[string]any{"error": err.Error()})
		problem(w, http.StatusConflict, "docker_image_delete_failed", "镜像仍被引用或删除失败")
		return
	}
	_ = s.store.RecordAudit(r.Context(), "docker.image.deleted", "docker_image", imageID, result)
	writeJSON(w, http.StatusOK, result)
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
