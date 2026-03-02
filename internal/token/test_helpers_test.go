package token

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeUsageResponse struct {
	result map[string]any
	err    error
}

type fakeUsageRefresher struct {
	mu        sync.Mutex
	responses map[string][]fakeUsageResponse
	calls     map[string]int
	delay     time.Duration
}

func (f *fakeUsageRefresher) Get(token string) (map[string]any, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	key := normalizeToken(token)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[key]++

	list := f.responses[key]
	if len(list) == 0 {
		return nil, fmt.Errorf("no fake response for token %s", key)
	}
	if len(list) > 1 {
		resp := list[0]
		f.responses[key] = list[1:]
		return cloneMap(resp.result), resp.err
	}
	resp := list[0]
	return cloneMap(resp.result), resp.err
}

func (f *fakeUsageRefresher) CallCount(token string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		return 0
	}
	return f.calls[normalizeToken(token)]
}

type memoryTokenStore struct {
	mu sync.Mutex

	tokens map[string]any
	locks  map[string]chan struct{}

	saveTokensDeltaCalls int
	saveTokensCalls      int
	acquireLockCalls     int
	lastUpdates          []map[string]any
}

func newMemoryTokenStore(tokens map[string]any) *memoryTokenStore {
	return &memoryTokenStore{
		tokens: cloneMap(tokens),
		locks:  map[string]chan struct{}{},
	}
}

func (s *memoryTokenStore) LoadTokens() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneMap(s.tokens), nil
}

func (s *memoryTokenStore) SaveTokensDelta(updated []map[string]any, deleted []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveTokensDeltaCalls++
	s.lastUpdates = cloneUpdateList(updated)

	applyDeletes(s.tokens, deleted)
	applyUpdates(s.tokens, updated)
	return nil
}

func (s *memoryTokenStore) AcquireLock(name string, timeout time.Duration) (func() error, error) {
	s.mu.Lock()
	s.acquireLockCalls++
	if strings.TrimSpace(name) == "" {
		name = "default"
	}
	lock, ok := s.locks[name]
	if !ok {
		lock = make(chan struct{}, 1)
		lock <- struct{}{}
		s.locks[name] = lock
	}
	s.mu.Unlock()

	if timeout <= 0 {
		select {
		case <-lock:
			return func() error {
				lock <- struct{}{}
				return nil
			}, nil
		default:
			return nil, fmt.Errorf("acquire lock timeout: %s", name)
		}
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-lock:
		var once sync.Once
		return func() error {
			once.Do(func() {
				lock <- struct{}{}
			})
			return nil
		}, nil
	case <-timer.C:
		return nil, fmt.Errorf("acquire lock timeout: %s", name)
	}
}

func (s *memoryTokenStore) ReplaceTokens(tokens map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = cloneMap(tokens)
}

func (s *memoryTokenStore) SaveTokensDeltaCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveTokensDeltaCalls
}

func (s *memoryTokenStore) SaveTokensCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveTokensCalls
}

func (s *memoryTokenStore) AcquireLockCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acquireLockCalls
}

func (s *memoryTokenStore) LastUpdates() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneUpdateList(s.lastUpdates)
}

func applyDeletes(tokens map[string]any, deleted []string) {
	if len(deleted) == 0 {
		return
	}
	set := map[string]struct{}{}
	for _, token := range deleted {
		raw := normalizeToken(token)
		if raw != "" {
			set[raw] = struct{}{}
		}
	}

	for poolName, listAny := range tokens {
		list, ok := listAny.([]any)
		if !ok {
			continue
		}
		filtered := make([]any, 0, len(list))
		for _, item := range list {
			token := ""
			switch typed := item.(type) {
			case string:
				token = normalizeToken(typed)
			case map[string]any:
				token = normalizeToken(toString(typed["token"]))
			}
			if _, shouldDelete := set[token]; shouldDelete {
				continue
			}
			filtered = append(filtered, cloneAny(item))
		}
		tokens[poolName] = filtered
	}
}

func applyUpdates(tokens map[string]any, updated []map[string]any) {
	for _, item := range updated {
		if item == nil {
			continue
		}
		poolName := toString(item["pool_name"])
		token := normalizeToken(toString(item["token"]))
		if poolName == "" || token == "" {
			continue
		}

		payload := map[string]any{}
		for k, v := range item {
			if k == "pool_name" || k == "_update_kind" {
				continue
			}
			payload[k] = cloneAny(v)
		}

		listAny, ok := tokens[poolName]
		list, okList := listAny.([]any)
		if !ok || !okList {
			list = []any{}
		}
		replaced := false
		for idx := range list {
			existingToken := ""
			switch typed := list[idx].(type) {
			case string:
				existingToken = normalizeToken(typed)
			case map[string]any:
				existingToken = normalizeToken(toString(typed["token"]))
			}
			if existingToken == token {
				list[idx] = payload
				replaced = true
				break
			}
		}
		if !replaced {
			list = append(list, payload)
		}
		tokens[poolName] = list
	}
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool, label string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for condition: %s", label)
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneAny(typed[i])
		}
		return out
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	default:
		return typed
	}
}

func cloneUpdateList(in []map[string]any) []map[string]any {
	out := make([]map[string]any, len(in))
	for i := range in {
		out[i] = cloneMap(in[i])
	}
	return out
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func toInt(v any, fallback int) int {
	if v == nil {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprintf("%v", v)))
	if err != nil {
		return fallback
	}
	return parsed
}
