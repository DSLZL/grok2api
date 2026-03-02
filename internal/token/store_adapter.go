package token

import (
	"fmt"
	"strings"
	"time"

	"grok2api/internal/storage"
)

type TokenStore interface {
	LoadTokens() (map[string]any, error)
	SaveTokensDelta(updated []map[string]any, deleted []string) error
	AcquireLock(name string, timeout time.Duration) (func() error, error)
}

type UsageRefresher interface {
	Get(token string) (map[string]any, error)
}

type StorageAdapter struct {
	store storage.Store
}

func NewStorageAdapter(store storage.Store) *StorageAdapter {
	return &StorageAdapter{store: store}
}

func NewDefaultStorageAdapter() (*StorageAdapter, error) {
	store, err := storage.GetStorage()
	if err != nil {
		return nil, err
	}
	return NewStorageAdapter(store), nil
}

func (a *StorageAdapter) LoadTokens() (map[string]any, error) {
	if a == nil || a.store == nil {
		return nil, fmt.Errorf("token storage is nil")
	}
	return a.store.LoadTokens()
}

func (a *StorageAdapter) SaveTokensDelta(updated []map[string]any, deleted []string) error {
	if a == nil || a.store == nil {
		return fmt.Errorf("token storage is nil")
	}
	return a.store.SaveTokensDelta(updated, deleted)
}

func (a *StorageAdapter) AcquireLock(name string, timeout time.Duration) (func() error, error) {
	if a == nil || a.store == nil {
		return nil, fmt.Errorf("token storage is nil")
	}
	if strings.TrimSpace(name) == "" {
		name = "tokens_save"
	}
	return a.store.AcquireLock(name, timeout)
}
