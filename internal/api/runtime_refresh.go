package api

import (
	"context"
	"strings"
	"time"
)

// Page GET handlers only schedule a missing initial runtime synchronization.
// LazyCat gRPC calls can take tens of seconds under host I/O pressure, so they
// must never be part of the response path used by frontend polling.
func (s *Server) scheduleRuntimeUsersRefresh(actor string) {
	actor = strings.TrimSpace(actor)
	if s.runtimeUsers == nil || s.localDeviceID == "" || actor == "" || s.runtimeUsers.LastUID() != "" {
		return
	}
	s.runtimeRefreshMu.Lock()
	if s.runtimeUsersSyncing {
		s.runtimeRefreshMu.Unlock()
		return
	}
	s.runtimeUsersSyncing = true
	s.runtimeRefreshMu.Unlock()
	go func() {
		defer func() {
			s.runtimeRefreshMu.Lock()
			s.runtimeUsersSyncing = false
			s.runtimeRefreshMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		users, err := s.runtimeUsers.Query(ctx, actor)
		if err == nil {
			_ = s.store.ObserveRuntimeUsers(ctx, s.localDeviceID, users)
		}
	}()
}

func (s *Server) scheduleRuntimeApplicationsRefresh(actor string) {
	actor = strings.TrimSpace(actor)
	if s.runtimeApps == nil || s.localDeviceID == "" || actor == "" || s.runtimeApps.LastUID() != "" {
		return
	}
	s.runtimeRefreshMu.Lock()
	if s.runtimeAppsSyncing {
		s.runtimeRefreshMu.Unlock()
		return
	}
	s.runtimeAppsSyncing = true
	s.runtimeRefreshMu.Unlock()
	go func() {
		defer func() {
			s.runtimeRefreshMu.Lock()
			s.runtimeAppsSyncing = false
			s.runtimeRefreshMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = s.SyncRuntimeApplications(ctx, actor)
	}()
}
