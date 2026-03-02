package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type backendFactory struct {
	name string
	new  func(t *testing.T) Store
}

func TestBackendParitySuite(t *testing.T) {
	backends := []backendFactory{
		{
			name: "local",
			new: func(t *testing.T) Store {
				t.Helper()
				st, err := NewLocalStorage(LocalOptions{DataDir: t.TempDir()})
				if err != nil {
					t.Fatalf("NewLocalStorage() error = %v", err)
				}
				return st
			},
		},
		{
			name: "redis",
			new: func(t *testing.T) Store {
				t.Helper()
				st, err := NewRedisStorage("redis://parity-suite")
				if err != nil {
					t.Fatalf("NewRedisStorage() error = %v", err)
				}
				return st
			},
		},
		{
			name: "mysql",
			new: func(t *testing.T) Store {
				t.Helper()
				st, err := NewMySQLStorage("mysql://parity-suite")
				if err != nil {
					t.Fatalf("NewMySQLStorage() error = %v", err)
				}
				return st
			},
		},
		{
			name: "pgsql",
			new: func(t *testing.T) Store {
				t.Helper()
				st, err := NewPostgreSQLStorage("postgres://parity-suite")
				if err != nil {
					t.Fatalf("NewPostgreSQLStorage() error = %v", err)
				}
				return st
			},
		},
	}

	seedConfig := map[string]any{
		"app": map[string]any{
			"public_enabled": true,
			"app_key":        "grok2api",
		},
		"retry": map[string]any{
			"max_retry": 3,
		},
	}

	seedTokens := map[string]any{
		"pool-main": []any{
			map[string]any{"token": "tok-1", "status": "active", "quota": 10},
			"tok-legacy",
		},
		"pool-fallback": []any{
			map[string]any{"token": "tok-2", "status": "active"},
		},
	}

	deltaUpdated := []map[string]any{
		{"pool_name": "pool-main", "token": "tok-1", "status": "cooling", "quota": 9, "_update_kind": "usage"},
		{"pool_name": "pool-fallback", "token": "tok-3", "status": "active"},
	}

	expectedAfterDelta := map[string]any{
		"pool-main": []any{
			map[string]any{"token": "tok-1", "status": "cooling", "quota": 9},
		},
		"pool-fallback": []any{
			map[string]any{"token": "tok-3", "status": "active"},
		},
	}

	deleted := []string{"tok-2", "tok-legacy"}

	for _, backend := range backends {
		backend := backend
		t.Run(backend.name, func(t *testing.T) {
			store := backend.new(t)
			t.Cleanup(func() { _ = store.Close() })

			if err := store.SaveConfig(seedConfig); err != nil {
				t.Fatalf("SaveConfig() error = %v", err)
			}

			cfg, err := store.LoadConfig()
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			assertCanonicalEqual(t, cfg, seedConfig)

			if err := store.SaveTokens(seedTokens); err != nil {
				t.Fatalf("SaveTokens() error = %v", err)
			}

			statsProvider, ok := store.(interface{ Stats() BackendStats })
			if !ok {
				t.Fatalf("store does not provide Stats()")
			}
			beforeDelta := statsProvider.Stats()

			if err := store.SaveTokensDelta(deltaUpdated, deleted); err != nil {
				t.Fatalf("SaveTokensDelta() error = %v", err)
			}

			afterDelta := statsProvider.Stats()
			if afterDelta.SaveTokensCalls != beforeDelta.SaveTokensCalls {
				t.Fatalf("SaveTokensDelta() should not trigger full SaveTokens: before=%d after=%d", beforeDelta.SaveTokensCalls, afterDelta.SaveTokensCalls)
			}
			if afterDelta.SaveTokensDeltaCalls != beforeDelta.SaveTokensDeltaCalls+1 {
				t.Fatalf("SaveTokensDeltaCalls mismatch: before=%d after=%d", beforeDelta.SaveTokensDeltaCalls, afterDelta.SaveTokensDeltaCalls)
			}

			tokens, err := store.LoadTokens()
			if err != nil {
				t.Fatalf("LoadTokens() error = %v", err)
			}
			assertCanonicalEqual(t, tokens, expectedAfterDelta)
		})
	}
}

func TestLockContentionTimeout(t *testing.T) {
	store, err := NewRedisStorage("redis://lock-timeout")
	if err != nil {
		t.Fatalf("NewRedisStorage() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	unlock, err := store.AcquireLock("token-save", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("first AcquireLock() error = %v", err)
	}
	defer func() { _ = unlock() }()

	started := time.Now()
	_, err = store.AcquireLock("token-save", 120*time.Millisecond)
	elapsed := time.Since(started)

	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("second AcquireLock() error = %v, want ErrLockTimeout", err)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("lock contention should wait until timeout, elapsed=%s", elapsed)
	}
}

func TestStorageFactorySelection(t *testing.T) {
	t.Setenv("SERVER_STORAGE_URL", "")
	t.Setenv("DATA_DIR", t.TempDir())

	t.Run("default local backend", func(t *testing.T) {
		ResetStorageFactoryForTest()
		t.Setenv("SERVER_STORAGE_TYPE", "")

		store, err := GetStorage()
		if err != nil {
			t.Fatalf("GetStorage() error = %v", err)
		}
		if _, ok := store.(*LocalStorage); !ok {
			t.Fatalf("GetStorage() type = %T, want *LocalStorage", store)
		}
	})

	t.Run("redis requires url", func(t *testing.T) {
		ResetStorageFactoryForTest()
		t.Setenv("SERVER_STORAGE_TYPE", "redis")
		t.Setenv("SERVER_STORAGE_URL", "")

		_, err := GetStorage()
		if err == nil {
			t.Fatalf("GetStorage() error = nil, want url required error")
		}
	})

	t.Run("mysql backend", func(t *testing.T) {
		ResetStorageFactoryForTest()
		t.Setenv("SERVER_STORAGE_TYPE", "mysql")
		t.Setenv("SERVER_STORAGE_URL", "mysql://unit-test")

		store, err := GetStorage()
		if err != nil {
			t.Fatalf("GetStorage() error = %v", err)
		}
		sqlStore, ok := store.(*SQLStorage)
		if !ok {
			t.Fatalf("GetStorage() type = %T, want *SQLStorage", store)
		}
		if sqlStore.Dialect() != "mysql" {
			t.Fatalf("SQLStorage dialect = %q, want %q", sqlStore.Dialect(), "mysql")
		}
	})

	t.Run("pgsql backend", func(t *testing.T) {
		ResetStorageFactoryForTest()
		t.Setenv("SERVER_STORAGE_TYPE", "pgsql")
		t.Setenv("SERVER_STORAGE_URL", "postgres://unit-test")

		store, err := GetStorage()
		if err != nil {
			t.Fatalf("GetStorage() error = %v", err)
		}
		sqlStore, ok := store.(*SQLStorage)
		if !ok {
			t.Fatalf("GetStorage() type = %T, want *SQLStorage", store)
		}
		if sqlStore.Dialect() != "pgsql" {
			t.Fatalf("SQLStorage dialect = %q, want %q", sqlStore.Dialect(), "pgsql")
		}
	})
}

func TestLocalLoadErrorPropagation(t *testing.T) {
	dataDir := t.TempDir()
	st, err := NewLocalStorage(LocalOptions{DataDir: dataDir})
	if err != nil {
		t.Fatalf("NewLocalStorage() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(dataDir, "config.toml"), []byte("[app\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config.toml) error = %v", err)
	}

	if _, err := st.LoadConfig(); err == nil {
		t.Fatalf("LoadConfig() error = nil, want parse error")
	}

	if err := os.WriteFile(filepath.Join(dataDir, "token.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile(token.json) error = %v", err)
	}

	if _, err := st.LoadTokens(); err == nil {
		t.Fatalf("LoadTokens() error = nil, want parse error")
	}
}

func assertCanonicalEqual(t *testing.T, got any, want any) {
	t.Helper()

	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal(got) error = %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal(want) error = %v", err)
	}

	var gotAny any
	if err := json.Unmarshal(gotJSON, &gotAny); err != nil {
		t.Fatalf("json.Unmarshal(gotJSON) error = %v", err)
	}
	var wantAny any
	if err := json.Unmarshal(wantJSON, &wantAny); err != nil {
		t.Fatalf("json.Unmarshal(wantJSON) error = %v", err)
	}

	gotCanonical, err := json.Marshal(gotAny)
	if err != nil {
		t.Fatalf("json.Marshal(gotAny) error = %v", err)
	}
	wantCanonical, err := json.Marshal(wantAny)
	if err != nil {
		t.Fatalf("json.Marshal(wantAny) error = %v", err)
	}

	if string(gotCanonical) != string(wantCanonical) {
		t.Fatalf("canonical mismatch\n got=%s\nwant=%s", string(gotCanonical), string(wantCanonical))
	}
}
