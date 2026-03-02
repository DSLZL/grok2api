package token

import (
	"context"
	"time"
)

type Scheduler struct {
	manager *Manager
	store   TokenStore
	options ManagerOptions
}

func NewScheduler(manager *Manager, store TokenStore, options ManagerOptions) *Scheduler {
	if options.SchedulerTick <= 0 {
		options.SchedulerTick = 8 * time.Hour
	}
	if options.SchedulerAcquireTimeout <= 0 {
		options.SchedulerAcquireTimeout = 1 * time.Second
	}
	if options.SchedulerLockTimeout <= 0 {
		options.SchedulerLockTimeout = options.SchedulerTick + time.Minute
	}
	if options.SchedulerLockName == "" {
		options.SchedulerLockName = "token_refresh"
	}
	return &Scheduler{manager: manager, store: store, options: options}
}

func (s *Scheduler) TickOnce(ctx context.Context) bool {
	if s == nil || s.manager == nil || s.store == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	default:
	}

	unlock, err := s.store.AcquireLock(s.options.SchedulerLockName, s.options.SchedulerAcquireTimeout)
	if err != nil {
		return false
	}
	if unlock == nil {
		unlock = func() error { return nil }
	}

	defer func() {
		_ = unlock()
	}()

	_, err = s.manager.RefreshCoolingTokens()
	return err == nil
}

func (s *Scheduler) Run(ctx context.Context) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(s.options.SchedulerTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.TickOnce(ctx)
		}
	}
}
