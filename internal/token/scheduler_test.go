package token

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestDistributedRefreshLockExclusion(t *testing.T) {
	oldSync := time.Now().Add(-10 * time.Hour).UnixMilli()
	store := newMemoryTokenStore(map[string]any{
		BasicPoolName: []any{
			map[string]any{"token": "cool-1", "status": "cooling", "quota": 0, "last_sync_at": oldSync},
		},
	})

	refresher := &fakeUsageRefresher{
		responses: map[string][]fakeUsageResponse{
			"cool-1": {
				{result: map[string]any{"remainingTokens": 2}},
			},
		},
		delay: 25 * time.Millisecond,
	}

	options := DefaultManagerOptions()
	options.SaveDelay = 0
	options.RefreshRetryDelay = 1 * time.Millisecond
	options.RefreshBatchDelay = 1 * time.Millisecond
	options.SchedulerAcquireTimeout = 5 * time.Millisecond
	options.SchedulerLockName = "token_refresh"

	manager := NewManager(store, refresher, options)
	if err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	s1 := NewScheduler(manager, store, options)
	s2 := NewScheduler(manager, store, options)

	ctx := context.Background()
	resultCh := make(chan bool, 2)
	go func() { resultCh <- s1.TickOnce(ctx) }()
	go func() { resultCh <- s2.TickOnce(ctx) }()

	first := <-resultCh
	second := <-resultCh

	successCount := 0
	if first {
		successCount++
	}
	if second {
		successCount++
	}
	if successCount != 1 {
		t.Fatalf("success count = %d, want 1 (lock exclusion)", successCount)
	}

	if calls := refresher.CallCount("cool-1"); calls != 1 {
		t.Fatalf("refresher call count = %d, want 1", calls)
	}
}

func TestSchedulerRunStopsOnContextCancel(t *testing.T) {
	store := newMemoryTokenStore(map[string]any{})
	manager := NewManager(store, &fakeUsageRefresher{}, DefaultManagerOptions())
	if err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	options := DefaultManagerOptions()
	options.SchedulerTick = 10 * time.Millisecond
	scheduler := NewScheduler(manager, store, options)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		scheduler.Run(ctx)
		close(done)
	}()

	time.Sleep(25 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("scheduler did not stop after context cancel")
	}
}

func TestRecordFailAndMarkRateLimitedSemantics(t *testing.T) {
	store := newMemoryTokenStore(map[string]any{
		BasicPoolName: []any{
			map[string]any{"token": "tok-f", "status": "active", "quota": 10},
		},
	})

	options := DefaultManagerOptions()
	options.FailThreshold = 2
	options.SaveDelay = 0

	manager := NewManager(store, nil, options)
	if err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !manager.RecordFail("tok-f", 500, "ignore") {
		t.Fatalf("RecordFail(non-401) expected true for token hit")
	}
	info, _ := manager.GetTokenInfo(BasicPoolName, "tok-f")
	if info.FailCount != 0 {
		t.Fatalf("FailCount after non-401 = %d, want 0", info.FailCount)
	}

	_ = manager.RecordFail("tok-f", 401, "bad auth")
	info, _ = manager.GetTokenInfo(BasicPoolName, "tok-f")
	if info.FailCount != 1 || info.Status != TokenStatusActive {
		t.Fatalf("after first 401: fail=%d status=%s, want 1/active", info.FailCount, info.Status)
	}

	_ = manager.RecordFail("tok-f", 401, "bad auth again")
	info, _ = manager.GetTokenInfo(BasicPoolName, "tok-f")
	if info.Status != TokenStatusExpired {
		t.Fatalf("after threshold 401 status = %s, want expired", info.Status)
	}

	if !manager.MarkRateLimited("tok-f") {
		t.Fatalf("MarkRateLimited() = false, want true")
	}
	info, _ = manager.GetTokenInfo(BasicPoolName, "tok-f")
	if info.Status != TokenStatusCooling || info.Quota != 0 {
		t.Fatalf("after mark rate limited = (%s,%d), want (cooling,0)", info.Status, info.Quota)
	}
}

func TestRefreshCoolingTokensUnauthorizedRetries(t *testing.T) {
	oldSync := time.Now().Add(-10 * time.Hour).UnixMilli()
	store := newMemoryTokenStore(map[string]any{
		BasicPoolName: []any{
			map[string]any{"token": "tok-unauth", "status": "cooling", "quota": 0, "last_sync_at": oldSync},
		},
	})

	refresher := &fakeUsageRefresher{
		responses: map[string][]fakeUsageResponse{
			"tok-unauth": {
				{err: errors.New("401")},
			},
		},
	}

	options := DefaultManagerOptions()
	options.SaveDelay = 0
	options.RefreshRetryDelay = 1 * time.Millisecond
	manager := NewManager(store, refresher, options)
	if err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	stats, err := manager.RefreshCoolingTokens()
	if err != nil {
		t.Fatalf("RefreshCoolingTokens() error = %v", err)
	}
	if stats.Expired != 1 {
		t.Fatalf("Expired = %d, want 1", stats.Expired)
	}
	if got := refresher.CallCount("tok-unauth"); got != 3 {
		t.Fatalf("CallCount = %d, want 3", got)
	}
}

func TestSchedulerTickOnceReturnsFalseOnRefreshError(t *testing.T) {
	oldSync := time.Now().Add(-10 * time.Hour).UnixMilli()
	store := newMemoryTokenStore(map[string]any{
		BasicPoolName: []any{
			map[string]any{"token": "tok-missing-refresher", "status": "cooling", "quota": 0, "last_sync_at": oldSync},
		},
	})
	manager := NewManager(store, nil, DefaultManagerOptions())
	if err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	scheduler := NewScheduler(manager, store, DefaultManagerOptions())
	if ok := scheduler.TickOnce(context.Background()); ok {
		t.Fatalf("TickOnce() = true, want false when refresher missing and refresh fails")
	}
}

func TestSchedulerTickOnceHonorsContext(t *testing.T) {
	store := newMemoryTokenStore(map[string]any{})
	manager := NewManager(store, &fakeUsageRefresher{}, DefaultManagerOptions())
	if err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	scheduler := NewScheduler(manager, store, DefaultManagerOptions())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if ok := scheduler.TickOnce(ctx); ok {
		t.Fatalf("TickOnce(cancelled ctx) = true, want false")
	}
}

func TestRefreshBatchConcurrencyCap(t *testing.T) {
	oldSync := time.Now().Add(-10 * time.Hour).UnixMilli()
	store := newMemoryTokenStore(map[string]any{
		BasicPoolName: []any{
			map[string]any{"token": "tok-c1", "status": "cooling", "quota": 0, "last_sync_at": oldSync},
			map[string]any{"token": "tok-c2", "status": "cooling", "quota": 0, "last_sync_at": oldSync},
			map[string]any{"token": "tok-c3", "status": "cooling", "quota": 0, "last_sync_at": oldSync},
		},
	})

	var inflight int32
	var maxInflight int32
	refresher := &fakeUsageRefresher{
		responses: map[string][]fakeUsageResponse{
			"tok-c1": {{result: map[string]any{"remainingTokens": 1}}},
			"tok-c2": {{result: map[string]any{"remainingTokens": 1}}},
			"tok-c3": {{result: map[string]any{"remainingTokens": 1}}},
		},
		delay: 5 * time.Millisecond,
	}

	wrapped := UsageRefresherFunc(func(token string) (map[string]any, error) {
		cur := atomic.AddInt32(&inflight, 1)
		for {
			prev := atomic.LoadInt32(&maxInflight)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxInflight, prev, cur) {
				break
			}
		}
		result, err := refresher.Get(token)
		atomic.AddInt32(&inflight, -1)
		return result, err
	})

	options := DefaultManagerOptions()
	options.RefreshConcurrency = 1
	options.SaveDelay = 0
	manager := NewManager(store, wrapped, options)
	if err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if _, err := manager.RefreshCoolingTokens(); err != nil {
		t.Fatalf("RefreshCoolingTokens() error = %v", err)
	}

	if atomic.LoadInt32(&maxInflight) > 1 {
		t.Fatalf("max inflight = %d, want <=1", atomic.LoadInt32(&maxInflight))
	}
}

type UsageRefresherFunc func(token string) (map[string]any, error)

func (f UsageRefresherFunc) Get(token string) (map[string]any, error) {
	return f(token)
}
