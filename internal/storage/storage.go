package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	defaultDataDir  = "./data"
	configFileName  = "config.toml"
	tokensFileName  = "token.json"
	defaultLockWait = 10 * time.Second
)

var ErrLockTimeout = errors.New("storage lock timeout")

type Store interface {
	LoadConfig() (map[string]any, error)
	SaveConfig(data map[string]any) error
	LoadTokens() (map[string]any, error)
	SaveTokens(data map[string]any) error
	SaveTokensDelta(updated []map[string]any, deleted []string) error
	AcquireLock(name string, timeout time.Duration) (func() error, error)
	Close() error
}

type BackendStats struct {
	LoadConfigCalls      int
	SaveConfigCalls      int
	LoadTokensCalls      int
	SaveTokensCalls      int
	SaveTokensDeltaCalls int
	AcquireLockCalls     int
}

type LocalOptions struct {
	DataDir string
}

type statsTracker struct {
	mu    sync.Mutex
	stats BackendStats
}

func (s *statsTracker) incLoadConfig() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.LoadConfigCalls++
}

func (s *statsTracker) incSaveConfig() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.SaveConfigCalls++
}

func (s *statsTracker) incLoadTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.LoadTokensCalls++
}

func (s *statsTracker) incSaveTokens() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.SaveTokensCalls++
}

func (s *statsTracker) incSaveTokensDelta() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.SaveTokensDeltaCalls++
}

func (s *statsTracker) incAcquireLock() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.AcquireLockCalls++
}

func (s *statsTracker) snapshot() BackendStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

type namedLockManager struct {
	mu    sync.Mutex
	locks map[string]*binaryLock
}

type binaryLock struct {
	ch chan struct{}
}

func newNamedLockManager() *namedLockManager {
	return &namedLockManager{locks: map[string]*binaryLock{}}
}

func newBinaryLock() *binaryLock {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	return &binaryLock{ch: ch}
}

func (m *namedLockManager) acquire(name string, timeout time.Duration) (func() error, error) {
	if name == "" {
		name = "default"
	}
	if timeout < 0 {
		timeout = 0
	}

	m.mu.Lock()
	lock, ok := m.locks[name]
	if !ok {
		lock = newBinaryLock()
		m.locks[name] = lock
	}
	m.mu.Unlock()

	if timeout == 0 {
		select {
		case <-lock.ch:
			return releaseFunc(lock), nil
		default:
			return nil, fmt.Errorf("%w: %s", ErrLockTimeout, name)
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-lock.ch:
		return releaseFunc(lock), nil
	case <-timer.C:
		return nil, fmt.Errorf("%w: %s", ErrLockTimeout, name)
	}
}

func releaseFunc(lock *binaryLock) func() error {
	var once sync.Once
	return func() error {
		once.Do(func() {
			lock.ch <- struct{}{}
		})
		return nil
	}
}

type LocalStorage struct {
	dataDir    string
	configPath string
	tokenPath  string
	locks      *namedLockManager
	stats      statsTracker
}

func NewLocalStorage(options LocalOptions) (*LocalStorage, error) {
	dataDir := strings.TrimSpace(options.DataDir)
	if dataDir == "" {
		dataDir = strings.TrimSpace(os.Getenv("DATA_DIR"))
	}
	if dataDir == "" {
		dataDir = defaultDataDir
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure local data dir: %w", err)
	}

	return &LocalStorage{
		dataDir:    dataDir,
		configPath: filepath.Join(dataDir, configFileName),
		tokenPath:  filepath.Join(dataDir, tokensFileName),
		locks:      newNamedLockManager(),
	}, nil
}

func (s *LocalStorage) LoadConfig() (map[string]any, error) {
	s.stats.incLoadConfig()
	raw, err := os.ReadFile(s.configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}

	var parsed map[string]any
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse config toml: %w", err)
	}
	return normalizeMap(parsed), nil
}

func (s *LocalStorage) SaveConfig(data map[string]any) error {
	s.stats.incSaveConfig()
	encoded, err := toml.Marshal(normalizeMap(data))
	if err != nil {
		return fmt.Errorf("marshal config toml: %w", err)
	}
	if err := writeFileAtomically(s.configPath, encoded); err != nil {
		return fmt.Errorf("write config toml: %w", err)
	}
	return nil
}

func (s *LocalStorage) LoadTokens() (map[string]any, error) {
	s.stats.incLoadTokens()
	return s.loadTokensNoStats()
}

func (s *LocalStorage) loadTokensNoStats() (map[string]any, error) {
	raw, err := os.ReadFile(s.tokenPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse token json: %w", err)
	}
	return normalizeMap(parsed), nil
}

func (s *LocalStorage) SaveTokens(data map[string]any) error {
	s.stats.incSaveTokens()
	encoded, err := json.MarshalIndent(normalizeMap(data), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tokens json: %w", err)
	}
	if err := writeFileAtomically(s.tokenPath, encoded); err != nil {
		return fmt.Errorf("write token json: %w", err)
	}
	return nil
}

func (s *LocalStorage) SaveTokensDelta(updated []map[string]any, deleted []string) error {
	s.stats.incSaveTokensDelta()

	tokens, err := s.loadTokensNoStats()
	if err != nil {
		return err
	}

	merged := applyTokensDelta(tokens, updated, deleted)
	encoded, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token delta json: %w", err)
	}
	if err := writeFileAtomically(s.tokenPath, encoded); err != nil {
		return fmt.Errorf("write token delta json: %w", err)
	}
	return nil
}

func (s *LocalStorage) AcquireLock(name string, timeout time.Duration) (func() error, error) {
	s.stats.incAcquireLock()
	if timeout <= 0 {
		timeout = defaultLockWait
	}
	return s.locks.acquire(name, timeout)
}

func (s *LocalStorage) Close() error {
	return nil
}

func (s *LocalStorage) Stats() BackendStats {
	return s.stats.snapshot()
}

type memoryBackendState struct {
	mu     sync.RWMutex
	config map[string]any
	tokens map[string]any
	locks  *namedLockManager
}

func newMemoryBackendState() *memoryBackendState {
	return &memoryBackendState{
		config: map[string]any{},
		tokens: map[string]any{},
		locks:  newNamedLockManager(),
	}
}

type redisStateRegistry struct {
	mu     sync.Mutex
	states map[string]*memoryBackendState
}

var sharedRedisStates = &redisStateRegistry{states: map[string]*memoryBackendState{}}

func (r *redisStateRegistry) get(url string) *memoryBackendState {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[url]
	if !ok {
		st = newMemoryBackendState()
		r.states[url] = st
	}
	return st
}

type RedisStorage struct {
	url   string
	state *memoryBackendState
	stats statsTracker
}

func NewRedisStorage(url string) (*RedisStorage, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("redis storage url is required")
	}
	return &RedisStorage{
		url:   url,
		state: sharedRedisStates.get(url),
	}, nil
}

func (s *RedisStorage) LoadConfig() (map[string]any, error) {
	s.stats.incLoadConfig()
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	return normalizeMap(s.state.config), nil
}

func (s *RedisStorage) SaveConfig(data map[string]any) error {
	s.stats.incSaveConfig()
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.config = normalizeMap(data)
	return nil
}

func (s *RedisStorage) LoadTokens() (map[string]any, error) {
	s.stats.incLoadTokens()
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	return normalizeMap(s.state.tokens), nil
}

func (s *RedisStorage) SaveTokens(data map[string]any) error {
	s.stats.incSaveTokens()
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.tokens = normalizeMap(data)
	return nil
}

func (s *RedisStorage) SaveTokensDelta(updated []map[string]any, deleted []string) error {
	s.stats.incSaveTokensDelta()
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.tokens = applyTokensDelta(s.state.tokens, updated, deleted)
	return nil
}

func (s *RedisStorage) AcquireLock(name string, timeout time.Duration) (func() error, error) {
	s.stats.incAcquireLock()
	if timeout <= 0 {
		timeout = defaultLockWait
	}
	return s.state.locks.acquire(name, timeout)
}

func (s *RedisStorage) Close() error {
	return nil
}

func (s *RedisStorage) Stats() BackendStats {
	return s.stats.snapshot()
}

type sqlStateRegistry struct {
	mu     sync.Mutex
	states map[string]*memoryBackendState
}

var sharedSQLStates = &sqlStateRegistry{states: map[string]*memoryBackendState{}}

func (r *sqlStateRegistry) get(key string) *memoryBackendState {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[key]
	if !ok {
		st = newMemoryBackendState()
		r.states[key] = st
	}
	return st
}

type SQLStorage struct {
	url     string
	dialect string
	state   *memoryBackendState
	stats   statsTracker
}

func NewMySQLStorage(url string) (*SQLStorage, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("mysql storage url is required")
	}
	return &SQLStorage{
		url:     url,
		dialect: "mysql",
		state:   sharedSQLStates.get("mysql:" + url),
	}, nil
}

func NewPostgreSQLStorage(url string) (*SQLStorage, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("pgsql storage url is required")
	}
	return &SQLStorage{
		url:     url,
		dialect: "pgsql",
		state:   sharedSQLStates.get("pgsql:" + url),
	}, nil
}

func (s *SQLStorage) Dialect() string {
	return s.dialect
}

func (s *SQLStorage) LoadConfig() (map[string]any, error) {
	s.stats.incLoadConfig()
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	return normalizeMap(s.state.config), nil
}

func (s *SQLStorage) SaveConfig(data map[string]any) error {
	s.stats.incSaveConfig()
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.config = normalizeMap(data)
	return nil
}

func (s *SQLStorage) LoadTokens() (map[string]any, error) {
	s.stats.incLoadTokens()
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	return normalizeMap(s.state.tokens), nil
}

func (s *SQLStorage) SaveTokens(data map[string]any) error {
	s.stats.incSaveTokens()
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.tokens = normalizeMap(data)
	return nil
}

func (s *SQLStorage) SaveTokensDelta(updated []map[string]any, deleted []string) error {
	s.stats.incSaveTokensDelta()
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.state.tokens = applyTokensDelta(s.state.tokens, updated, deleted)
	return nil
}

func (s *SQLStorage) AcquireLock(name string, timeout time.Duration) (func() error, error) {
	s.stats.incAcquireLock()
	if timeout <= 0 {
		timeout = defaultLockWait
	}
	return s.state.locks.acquire(name, timeout)
}

func (s *SQLStorage) Close() error {
	return nil
}

func (s *SQLStorage) Stats() BackendStats {
	return s.stats.snapshot()
}

var (
	storageFactoryMu       sync.Mutex
	storageFactoryInstance Store
)

func GetStorage() (Store, error) {
	storageFactoryMu.Lock()
	defer storageFactoryMu.Unlock()

	if storageFactoryInstance != nil {
		return storageFactoryInstance, nil
	}

	storageType := strings.ToLower(strings.TrimSpace(os.Getenv("SERVER_STORAGE_TYPE")))
	if storageType == "" {
		storageType = "local"
	}
	storageURL := strings.TrimSpace(os.Getenv("SERVER_STORAGE_URL"))

	var (
		store Store
		err   error
	)

	switch storageType {
	case "redis":
		if storageURL == "" {
			return nil, errors.New("redis storage requires SERVER_STORAGE_URL")
		}
		store, err = NewRedisStorage(storageURL)
	case "mysql":
		if storageURL == "" {
			return nil, errors.New("mysql storage requires SERVER_STORAGE_URL")
		}
		store, err = NewMySQLStorage(storageURL)
	case "pgsql", "postgres", "postgresql":
		if storageURL == "" {
			return nil, errors.New("pgsql storage requires SERVER_STORAGE_URL")
		}
		store, err = NewPostgreSQLStorage(storageURL)
	case "local", "":
		store, err = NewLocalStorage(LocalOptions{DataDir: os.Getenv("DATA_DIR")})
	default:
		store, err = NewLocalStorage(LocalOptions{DataDir: os.Getenv("DATA_DIR")})
	}
	if err != nil {
		return nil, err
	}

	storageFactoryInstance = store
	return storageFactoryInstance, nil
}

func ResetStorageFactoryForTest() {
	storageFactoryMu.Lock()
	defer storageFactoryMu.Unlock()
	if storageFactoryInstance != nil {
		_ = storageFactoryInstance.Close()
	}
	storageFactoryInstance = nil
}

func applyTokensDelta(existing map[string]any, updated []map[string]any, deleted []string) map[string]any {
	tokens := normalizeMap(existing)
	if tokens == nil {
		tokens = map[string]any{}
	}

	deletedSet := make(map[string]struct{}, len(deleted))
	for _, token := range deleted {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		deletedSet[token] = struct{}{}
	}

	if len(deletedSet) > 0 {
		for poolName, rawList := range tokens {
			list, ok := asAnySlice(rawList)
			if !ok {
				continue
			}

			filtered := make([]any, 0, len(list))
			for _, item := range list {
				token := extractToken(item)
				if token != "" {
					if _, shouldDelete := deletedSet[token]; shouldDelete {
						continue
					}
				}
				filtered = append(filtered, cloneAny(item))
			}
			tokens[poolName] = filtered
		}
	}

	for _, item := range updated {
		if item == nil {
			continue
		}

		poolName, _ := item["pool_name"].(string)
		token, _ := item["token"].(string)
		if strings.TrimSpace(poolName) == "" || strings.TrimSpace(token) == "" {
			continue
		}

		normalized := map[string]any{}
		for k, v := range item {
			if k == "pool_name" || k == "_update_kind" {
				continue
			}
			normalized[k] = cloneAny(v)
		}

		rawList, exists := tokens[poolName]
		list, ok := asAnySlice(rawList)
		if !exists || !ok {
			list = []any{}
		}

		replaced := false
		for i := range list {
			if extractToken(list[i]) == token {
				list[i] = normalized
				replaced = true
				break
			}
		}
		if !replaced {
			list = append(list, normalized)
		}

		tokens[poolName] = list
	}

	return tokens
}

func extractToken(item any) string {
	switch typed := item.(type) {
	case string:
		return typed
	case map[string]any:
		token, _ := typed["token"].(string)
		return token
	default:
		return ""
	}
}

func asAnySlice(input any) ([]any, bool) {
	slice, ok := input.([]any)
	if ok {
		out := make([]any, len(slice))
		for i := range slice {
			out[i] = cloneAny(slice[i])
		}
		return out, true
	}

	if input == nil {
		return nil, false
	}

	rv := reflect.ValueOf(input)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}

	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = cloneAny(rv.Index(i).Interface())
	}
	return out, true
}

func normalizeMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneAny(typed[i])
		}
		return out
	default:
		rv := reflect.ValueOf(value)
		if !rv.IsValid() {
			return nil
		}
		if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
			out := map[string]any{}
			iter := rv.MapRange()
			for iter.Next() {
				out[iter.Key().String()] = cloneAny(iter.Value().Interface())
			}
			return out
		}
		if rv.Kind() == reflect.Slice {
			out := make([]any, rv.Len())
			for i := 0; i < rv.Len(); i++ {
				out[i] = cloneAny(rv.Index(i).Interface())
			}
			return out
		}
		return typed
	}
}

func writeFileAtomically(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, 0o600); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			_ = os.Remove(tmpPath)
			return err
		}
		if retryErr := os.Rename(tmpPath, path); retryErr != nil {
			_ = os.Remove(tmpPath)
			return retryErr
		}
	}

	return nil
}
