package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wtj-0527/lazycat-maoyan/internal/api"
	"github.com/wtj-0527/lazycat-maoyan/internal/store"
)

type InspectionScheduler struct {
	api    *api.Server
	store  *store.Store
	logger *slog.Logger
	now    func() time.Time
}

func NewInspectionScheduler(server *api.Server, st *store.Store, logger *slog.Logger) *InspectionScheduler {
	return &InspectionScheduler{api: server, store: st, logger: logger, now: time.Now}
}

func (s *InspectionScheduler) Run(ctx context.Context) {
	startedAt := time.Now().UTC()
	for {
		latest, err := s.store.LatestMetricTimestamp(ctx)
		if err == nil && !latest.Before(startedAt) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	s.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *InspectionScheduler) tick(ctx context.Context) {
	now := s.now().In(time.Local)
	settings := s.store.OperationalSettings(ctx)
	if latest, err := s.store.LatestMetricTimestamp(ctx); err != nil || now.Sub(latest.In(time.Local)) > 2*time.Minute {
		s.logger.Warn("scheduled inspection waiting for fresh metrics", "latest", latest, "error", err)
		return
	}
	if now.Weekday() == time.Sunday && now.Hour() >= settings.WeeklyInspectionHour {
		year, week := now.ISOWeek()
		if s.runPeriod(ctx, "weekly", formatWeek(year, week), "scheduled-weekly") {
			_ = s.store.SetSystemState(ctx, "inspection_schedule_daily", now.Format("2006-01-02"))
		}
		return
	}
	if now.Hour() >= settings.DailyInspectionHour {
		s.runPeriod(ctx, "daily", now.Format("2006-01-02"), "scheduled-daily")
	}
}

func (s *InspectionScheduler) runPeriod(ctx context.Context, key, period, trigger string) bool {
	stateKey := "inspection_schedule_" + key
	var completed string
	found, err := s.store.GetSystemState(ctx, stateKey, &completed)
	if err != nil {
		s.logger.Warn("read inspection schedule state", "schedule", key, "error", err)
		return false
	}
	if found && completed == period {
		return true
	}
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		item, err := s.api.RunInspection(ctx, trigger)
		if err == nil {
			_ = s.store.SetSystemState(ctx, stateKey, period)
			_ = s.store.SetSystemState(ctx, "last_scheduled_inspection", map[string]any{
				"id": item.ID, "trigger": trigger, "completedAt": item.CompletedAt, "evidenceSha256": item.EvidenceSHA256,
			})
			s.logger.Info("scheduled inspection completed", "schedule", key, "inspection", item.ID, "attempt", attempt)
			return true
		}
		lastErr = err
		if attempt < 3 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(time.Duration(1<<uint(attempt-1)) * time.Second):
			}
		}
	}
	_ = s.store.SetSystemState(ctx, "last_scheduled_inspection_error", map[string]any{
		"schedule": key, "period": period, "error": lastErr.Error(), "failedAt": time.Now().UTC(),
	})
	s.logger.Error("scheduled inspection failed", "schedule", key, "error", lastErr)
	return false
}

func formatWeek(year, week int) string {
	return fmt.Sprintf("%04d-W%02d", year, week)
}
