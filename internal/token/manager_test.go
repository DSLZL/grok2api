package token

import (
	"errors"
	"testing"
	"time"
)

func TestDeltaSaveAndReloadStale(t *testing.T) {
	t.Run("delta save delayed and usage flush throttled", func(t *testing.T) {
		store := newMemoryTokenStore(map[string]any{
			BasicPoolName: []any{
				map[string]any{"token": "tok-u", "status": "active", "quota": 10},
			},
		})

		options := DefaultManagerOptions()
		options.SaveDelay = 12 * time.Millisecond
		options.UsageFlushInterval = 100 * time.Millisecond

		manager := NewManager(store, nil, options)
		if err := manager.Load(); err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		if ok := manager.Consume("tok-u", EffortLow); !ok {
			t.Fatalf("Consume(first) = false, want true")
		}

		time.Sleep(2 * time.Millisecond)
		if got := store.SaveTokensDeltaCalls(); got != 0 {
			t.Fatalf("SaveTokensDeltaCalls() too early = %d, want 0 before save delay", got)
		}

		waitUntil(t, 800*time.Millisecond, func() bool {
			return store.SaveTokensDeltaCalls() == 1
		}, "first usage delta flush")

		if ok := manager.Consume("tok-u", EffortLow); !ok {
			t.Fatalf("Consume(second) = false, want true")
		}

		time.Sleep(35 * time.Millisecond)
		if got := store.SaveTokensDeltaCalls(); got != 1 {
			t.Fatalf("SaveTokensDeltaCalls() during usage flush interval = %d, want 1", got)
		}

		waitUntil(t, 1200*time.Millisecond, func() bool {
			return store.SaveTokensDeltaCalls() >= 2
		}, "second usage delta flush after interval")

		updates := store.LastUpdates()
		if len(updates) == 0 {
			t.Fatalf("LastUpdates() is empty")
		}
		if got := updates[0]["_update_kind"]; got != "usage" {
			t.Fatalf("update kind = %v, want usage", got)
		}
		if got := store.SaveTokensCalls(); got != 0 {
			t.Fatalf("SaveTokensCalls() = %d, want 0 (delta only)", got)
		}
	})

	t.Run("reload if stale", func(t *testing.T) {
		store := newMemoryTokenStore(map[string]any{
			BasicPoolName: []any{
				map[string]any{"token": "tok-a", "status": "active", "quota": 9},
			},
		})

		reloadOptions := DefaultManagerOptions()
		reloadOptions.ReloadInterval = 40 * time.Millisecond

		reloader := NewManager(store, nil, reloadOptions)
		if err := reloader.Load(); err != nil {
			t.Fatalf("reloader.Load() error = %v", err)
		}

		store.ReplaceTokens(map[string]any{
			BasicPoolName: []any{
				map[string]any{"token": "tok-a", "status": "active", "quota": 77},
			},
		})

		if err := reloader.ReloadIfStale(); err != nil {
			t.Fatalf("ReloadIfStale() immediate error = %v", err)
		}
		info, ok := reloader.GetTokenInfo(BasicPoolName, "tok-a")
		if !ok {
			t.Fatalf("GetTokenInfo() missing tok-a")
		}
		if info.Quota != 9 {
			t.Fatalf("quota after non-stale reload = %d, want 9", info.Quota)
		}

		time.Sleep(60 * time.Millisecond)
		if err := reloader.ReloadIfStale(); err != nil {
			t.Fatalf("ReloadIfStale() stale error = %v", err)
		}
		info, ok = reloader.GetTokenInfo(BasicPoolName, "tok-a")
		if !ok {
			t.Fatalf("GetTokenInfo() missing tok-a after stale reload")
		}
		if info.Quota != 77 {
			t.Fatalf("quota after stale reload = %d, want 77", info.Quota)
		}
	})
}

func TestRefreshCoolingTokens(t *testing.T) {
	oldSync := time.Now().Add(-10 * time.Hour).UnixMilli()
	store := newMemoryTokenStore(map[string]any{
		BasicPoolName: []any{
			map[string]any{"token": "recover-token", "status": "cooling", "quota": 0, "last_sync_at": oldSync},
			map[string]any{"token": "expire-token", "status": "cooling", "quota": 0, "last_sync_at": oldSync},
			map[string]any{"token": "active-token", "status": "active", "quota": 11},
		},
	})

	refresher := &fakeUsageRefresher{
		responses: map[string][]fakeUsageResponse{
			"recover-token": {
				{result: map[string]any{"remainingTokens": 5}},
			},
			"expire-token": {
				{err: errors.New("401 Unauthorized")},
			},
		},
		delay: 1 * time.Millisecond,
	}

	options := DefaultManagerOptions()
	options.SaveDelay = 0
	options.RefreshRetryDelay = 1 * time.Millisecond
	options.RefreshBatchDelay = 1 * time.Millisecond

	manager := NewManager(store, refresher, options)
	if err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	stats, err := manager.RefreshCoolingTokens()
	if err != nil {
		t.Fatalf("RefreshCoolingTokens() error = %v", err)
	}

	if stats.Checked != 2 {
		t.Fatalf("Checked = %d, want 2", stats.Checked)
	}
	if stats.Refreshed != 2 {
		t.Fatalf("Refreshed = %d, want 2", stats.Refreshed)
	}
	if stats.Recovered != 1 {
		t.Fatalf("Recovered = %d, want 1", stats.Recovered)
	}
	if stats.Expired != 1 {
		t.Fatalf("Expired = %d, want 1", stats.Expired)
	}

	if got := refresher.CallCount("expire-token"); got != 3 {
		t.Fatalf("expire-token call count = %d, want 3 retries", got)
	}

	recovered, ok := manager.GetTokenInfo(BasicPoolName, "recover-token")
	if !ok {
		t.Fatalf("recover-token missing")
	}
	if recovered.Status != TokenStatusActive || recovered.Quota != 5 {
		t.Fatalf("recover-token state = (%s,%d), want (active,5)", recovered.Status, recovered.Quota)
	}

	expired, ok := manager.GetTokenInfo(BasicPoolName, "expire-token")
	if !ok {
		t.Fatalf("expire-token missing")
	}
	if expired.Status != TokenStatusExpired {
		t.Fatalf("expire-token status = %s, want expired", expired.Status)
	}

	if got := store.SaveTokensDeltaCalls(); got == 0 {
		t.Fatalf("SaveTokensDeltaCalls() = %d, want >=1", got)
	}
}
