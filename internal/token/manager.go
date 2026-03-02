package token

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultRefreshBatchSize   = 10
	defaultRefreshConcurrency = 5
)

type ManagerOptions struct {
	SaveDelay               time.Duration
	UsageFlushInterval      time.Duration
	ReloadInterval          time.Duration
	FailThreshold           int
	RefreshIntervalBasic    time.Duration
	RefreshIntervalSuper    time.Duration
	RefreshBatchSize        int
	RefreshConcurrency      int
	RefreshRetryDelay       time.Duration
	RefreshBatchDelay       time.Duration
	SchedulerLockTimeout    time.Duration
	SchedulerAcquireTimeout time.Duration
	SchedulerLockName       string
	SaveLockName            string
	SaveLockTimeout         time.Duration
	SchedulerTick           time.Duration
}

func DefaultManagerOptions() ManagerOptions {
	return ManagerOptions{
		SaveDelay:               500 * time.Millisecond,
		UsageFlushInterval:      5 * time.Second,
		ReloadInterval:          30 * time.Second,
		FailThreshold:           FailThreshold,
		RefreshIntervalBasic:    8 * time.Hour,
		RefreshIntervalSuper:    2 * time.Hour,
		RefreshBatchSize:        defaultRefreshBatchSize,
		RefreshConcurrency:      defaultRefreshConcurrency,
		RefreshRetryDelay:       500 * time.Millisecond,
		RefreshBatchDelay:       1 * time.Second,
		SchedulerLockTimeout:    9 * time.Hour,
		SchedulerAcquireTimeout: 1 * time.Second,
		SchedulerLockName:       "token_refresh",
		SaveLockName:            "tokens_save",
		SaveLockTimeout:         10 * time.Second,
		SchedulerTick:           8 * time.Hour,
	}
}

type TokenPool struct {
	Name   string
	Tokens map[string]*TokenInfo
}

func newTokenPool(name string) *TokenPool {
	return &TokenPool{Name: name, Tokens: map[string]*TokenInfo{}}
}

func (p *TokenPool) Get(token string) (*TokenInfo, bool) {
	if p == nil {
		return nil, false
	}
	info, ok := p.Tokens[normalizeToken(token)]
	return info, ok
}

func (p *TokenPool) Add(info *TokenInfo) {
	if p == nil || info == nil {
		return
	}
	p.Tokens[normalizeToken(info.Token)] = info
}

func (p *TokenPool) Remove(token string) {
	if p == nil {
		return
	}
	delete(p.Tokens, normalizeToken(token))
}

type dirtyMeta struct {
	poolName   string
	changeKind string
}

type Manager struct {
	store     TokenStore
	refresher UsageRefresher
	options   ManagerOptions

	mu           sync.Mutex
	initialized  bool
	pools        map[string]*TokenPool
	lastReloadAt time.Time

	dirtyTokens  map[string]dirtyMeta
	dirtyDeletes map[string]struct{}
	dirty        bool

	hasStateChanges bool
	hasUsageChanges bool
	stateChangeSeq  int
	usageChangeSeq  int

	lastUsageFlushAt time.Time
	saveTaskRunning  bool
}

func NewManager(store TokenStore, refresher UsageRefresher, options ManagerOptions) *Manager {
	if options.FailThreshold < 1 {
		options.FailThreshold = 1
	}
	if options.RefreshBatchSize <= 0 {
		options.RefreshBatchSize = defaultRefreshBatchSize
	}
	if options.RefreshConcurrency <= 0 {
		options.RefreshConcurrency = defaultRefreshConcurrency
	}
	if strings.TrimSpace(options.SaveLockName) == "" {
		options.SaveLockName = "tokens_save"
	}
	if strings.TrimSpace(options.SchedulerLockName) == "" {
		options.SchedulerLockName = "token_refresh"
	}

	return &Manager{
		store:            store,
		refresher:        refresher,
		options:          options,
		pools:            map[string]*TokenPool{},
		dirtyTokens:      map[string]dirtyMeta{},
		dirtyDeletes:     map[string]struct{}{},
		lastReloadAt:     time.Now(),
		lastUsageFlushAt: time.Time{},
	}
}

func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.store == nil {
		return fmt.Errorf("token store is nil")
	}
	raw, err := m.store.LoadTokens()
	if err != nil {
		return err
	}
	m.pools = parsePools(raw)
	m.initialized = true
	m.lastReloadAt = time.Now()
	return nil
}

func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := m.store.LoadTokens()
	if err != nil {
		return err
	}
	m.pools = parsePools(raw)
	m.initialized = true
	m.lastReloadAt = time.Now()
	return nil
}

func (m *Manager) ReloadIfStale() error {
	m.mu.Lock()
	if m.options.ReloadInterval <= 0 || time.Since(m.lastReloadAt) < m.options.ReloadInterval {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	return m.Reload()
}

func (m *Manager) GetTokenInfo(poolName, token string) (*TokenInfo, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool, ok := m.pools[poolName]
	if !ok {
		return nil, false
	}
	info, ok := pool.Get(token)
	if !ok {
		return nil, false
	}
	copy := *info
	return &copy, true
}

func (m *Manager) Consume(token string, effort EffortType) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
		return false
	}
	raw := normalizeToken(token)
	for _, pool := range m.pools {
		info, ok := pool.Get(raw)
		if !ok {
			continue
		}
		oldStatus := info.Status
		_ = info.Consume(effort)
		kind := "usage"
		if oldStatus != info.Status {
			kind = "state"
		}
		m.trackTokenChangeLocked(info, pool.Name, kind)
		m.scheduleSaveLocked()
		return true
	}
	return false
}

func (m *Manager) RecordFail(token string, statusCode int, reason string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw := normalizeToken(token)
	for _, pool := range m.pools {
		info, ok := pool.Get(raw)
		if !ok {
			continue
		}
		if statusCode == 401 {
			threshold := m.options.FailThreshold
			if threshold < 1 {
				threshold = 1
			}
			info.RecordFail(statusCode, reason, threshold)
			m.trackTokenChangeLocked(info, pool.Name, "state")
			m.scheduleSaveLocked()
		}
		return true
	}
	return false
}

func (m *Manager) MarkRateLimited(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw := normalizeToken(token)
	for _, pool := range m.pools {
		info, ok := pool.Get(raw)
		if !ok {
			continue
		}
		info.Quota = 0
		info.Status = TokenStatusCooling
		m.trackTokenChangeLocked(info, pool.Name, "state")
		m.scheduleSaveLocked()
		return true
	}
	return false
}

type RefreshStats struct {
	Checked   int
	Refreshed int
	Recovered int
	Expired   int
}

type tokenRefreshItem struct {
	poolName string
	info     *TokenInfo
}

func (m *Manager) RefreshCoolingTokens() (RefreshStats, error) {
	m.mu.Lock()
	items := make([]tokenRefreshItem, 0)
	for _, pool := range m.pools {
		interval := m.options.RefreshIntervalBasic
		if pool.Name == SuperPoolName {
			interval = m.options.RefreshIntervalSuper
		}
		for _, info := range pool.Tokens {
			if info.NeedRefresh(interval) {
				items = append(items, tokenRefreshItem{poolName: pool.Name, info: info})
			}
		}
	}
	m.mu.Unlock()

	if len(items) == 0 {
		return RefreshStats{}, nil
	}
	if m.refresher == nil {
		return RefreshStats{}, errors.New("usage refresher is nil")
	}

	stats := RefreshStats{Checked: len(items)}
	for i := 0; i < len(items); i += m.options.RefreshBatchSize {
		end := i + m.options.RefreshBatchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]
		results := m.refreshBatch(batch)
		stats.Refreshed += len(batch)
		for _, res := range results {
			if res.recovered {
				stats.Recovered++
			}
			if res.expired {
				stats.Expired++
			}
		}
		if end < len(items) && m.options.RefreshBatchDelay > 0 {
			time.Sleep(m.options.RefreshBatchDelay)
		}
	}

	m.mu.Lock()
	for _, item := range items {
		poolName := m.poolNameForTokenLocked(item.info.Token)
		if poolName == "" {
			poolName = item.poolName
		}
		m.trackTokenChangeLocked(item.info, poolName, "state")
	}
	m.mu.Unlock()

	if err := m.save(true); err != nil {
		return stats, err
	}
	return stats, nil
}

type refreshResult struct {
	recovered bool
	expired   bool
}

func (m *Manager) refreshBatch(batch []tokenRefreshItem) []refreshResult {
	results := make([]refreshResult, len(batch))
	type indexedResult struct {
		idx int
		res refreshResult
	}
	ch := make(chan indexedResult, len(batch))
	sem := make(chan struct{}, m.options.RefreshConcurrency)
	var wg sync.WaitGroup

	for idx, item := range batch {
		idx := idx
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ch <- indexedResult{idx: idx, res: m.refreshOne(item)}
		}()
	}

	wg.Wait()
	close(ch)
	for item := range ch {
		results[item.idx] = item.res
	}
	return results
}

func (m *Manager) refreshOne(item tokenRefreshItem) refreshResult {
	token := normalizeToken(item.info.Token)
	for retry := 0; retry < 3; retry++ {
		result, err := m.refresher.Get(token)
		if err != nil {
			if isUnauthorizedErr(err) {
				if retry < 2 {
					if m.options.RefreshRetryDelay > 0 {
						time.Sleep(m.options.RefreshRetryDelay)
					}
					continue
				}
				m.mu.Lock()
				item.info.Status = TokenStatusExpired
				m.mu.Unlock()
				return refreshResult{expired: true}
			}
			return refreshResult{}
		}

		quota, ok := extractQuota(result)
		if !ok {
			return refreshResult{}
		}

		m.mu.Lock()
		oldQuota := item.info.Quota
		item.info.UpdateQuota(quota)
		item.info.MarkSynced()
		window, ok := extractWindowSizeSeconds(result)
		if ok {
			m.maybeMovePoolLocked(item.info, window)
		}
		m.mu.Unlock()

		return refreshResult{recovered: oldQuota == 0 && quota > 0}
	}
	return refreshResult{}
}

func (m *Manager) maybeMovePoolLocked(info *TokenInfo, windowSize int) {
	current := m.poolNameForTokenLocked(info.Token)
	if current == "" {
		return
	}
	if current == SuperPoolName && windowSize >= SuperWindowThresholdSeconds {
		m.moveTokenPoolLocked(info, SuperPoolName, BasicPoolName)
		return
	}
	if current == BasicPoolName && windowSize < SuperWindowThresholdSeconds {
		m.moveTokenPoolLocked(info, BasicPoolName, SuperPoolName)
	}
}

func (m *Manager) poolNameForTokenLocked(token string) string {
	raw := normalizeToken(token)
	for name, pool := range m.pools {
		if _, ok := pool.Get(raw); ok {
			return name
		}
	}
	return ""
}

func (m *Manager) moveTokenPoolLocked(info *TokenInfo, fromPool, toPool string) {
	if fromPool == toPool {
		return
	}
	to, ok := m.pools[toPool]
	if !ok {
		to = newTokenPool(toPool)
		m.pools[toPool] = to
	}
	if from, ok := m.pools[fromPool]; ok {
		from.Remove(info.Token)
	}
	to.Add(info)
}

func (m *Manager) trackTokenChangeLocked(token *TokenInfo, poolName string, changeKind string) {
	if token == nil {
		return
	}
	key := normalizeToken(token.Token)
	if _, ok := m.dirtyDeletes[key]; ok {
		delete(m.dirtyDeletes, key)
	}
	if existing, ok := m.dirtyTokens[key]; ok && existing.changeKind == "state" {
		changeKind = "state"
	}
	m.dirtyTokens[key] = dirtyMeta{poolName: poolName, changeKind: changeKind}
	if changeKind == "state" {
		m.hasStateChanges = true
		m.stateChangeSeq++
		return
	}
	m.hasUsageChanges = true
	m.usageChangeSeq++
}

func (m *Manager) scheduleSaveLocked() {
	m.dirty = true
	if m.options.SaveDelay == 0 {
		if !m.saveTaskRunning {
			m.saveTaskRunning = true
			go m.flushLoop()
		}
		return
	}
	if m.saveTaskRunning {
		return
	}
	m.saveTaskRunning = true
	go m.flushLoop()
}

func (m *Manager) flushLoop() {
	for {
		delay := m.options.SaveDelay
		if delay > 0 {
			time.Sleep(delay)
		}

		m.mu.Lock()
		if !m.dirty {
			m.saveTaskRunning = false
			m.mu.Unlock()
			return
		}
		m.dirty = false
		m.mu.Unlock()

		_ = m.save(false)
	}
}

func (m *Manager) save(force bool) error {
	m.mu.Lock()
	if len(m.dirtyTokens) == 0 && len(m.dirtyDeletes) == 0 {
		m.mu.Unlock()
		return nil
	}
	if !force && !m.hasStateChanges && m.options.UsageFlushInterval > 0 {
		if !m.lastUsageFlushAt.IsZero() && time.Since(m.lastUsageFlushAt) < m.options.UsageFlushInterval {
			m.dirty = true
			m.mu.Unlock()
			return nil
		}
	}

	stateSeq := m.stateChangeSeq
	usageSeq := m.usageChangeSeq
	dirtyTokens := m.dirtyTokens
	dirtyDeletes := m.dirtyDeletes
	m.dirtyTokens = map[string]dirtyMeta{}
	m.dirtyDeletes = map[string]struct{}{}

	updates := make([]map[string]any, 0, len(dirtyTokens))
	deleted := make([]string, 0, len(dirtyDeletes))
	for token := range dirtyDeletes {
		deleted = append(deleted, token)
	}
	for token, meta := range dirtyTokens {
		if _, deletedToken := dirtyDeletes[token]; deletedToken {
			continue
		}
		pool := m.pools[meta.poolName]
		if pool == nil {
			continue
		}
		info, ok := pool.Get(token)
		if !ok {
			continue
		}
		payload := info.ToMap()
		payload["pool_name"] = meta.poolName
		payload["_update_kind"] = meta.changeKind
		updates = append(updates, payload)
	}
	m.mu.Unlock()

	unlock, err := m.store.AcquireLock(m.options.SaveLockName, m.options.SaveLockTimeout)
	if err != nil {
		m.restoreDirty(dirtyTokens, dirtyDeletes)
		return err
	}
	if unlock == nil {
		unlock = func() error { return nil }
	}

	errSave := m.store.SaveTokensDelta(updates, deleted)
	errUnlock := unlock()
	if errSave != nil {
		m.restoreDirty(dirtyTokens, dirtyDeletes)
		if errUnlock != nil {
			return fmt.Errorf("save tokens delta: %w (unlock: %v)", errSave, errUnlock)
		}
		return errSave
	}
	if errUnlock != nil {
		return errUnlock
	}

	m.mu.Lock()
	if stateSeq == m.stateChangeSeq {
		m.hasStateChanges = false
	}
	if usageSeq == m.usageChangeSeq {
		m.hasUsageChanges = false
		m.lastUsageFlushAt = time.Now()
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) restoreDirty(tokens map[string]dirtyMeta, deletes map[string]struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dirty = true
	for token, meta := range tokens {
		existing, ok := m.dirtyTokens[token]
		if ok && existing.changeKind == "state" {
			continue
		}
		if meta.changeKind == "state" && ok {
			m.dirtyTokens[token] = dirtyMeta{poolName: meta.poolName, changeKind: "state"}
			continue
		}
		m.dirtyTokens[token] = meta
	}
	for token := range deletes {
		m.dirtyDeletes[token] = struct{}{}
		delete(m.dirtyTokens, token)
	}
}

func parsePools(raw map[string]any) map[string]*TokenPool {
	pools := map[string]*TokenPool{}
	for poolName, listAny := range raw {
		pool := newTokenPool(poolName)
		items, ok := listAny.([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			info, ok := parseToken(item, poolName)
			if !ok {
				continue
			}
			pool.Add(info)
		}
		pools[poolName] = pool
	}
	if _, ok := pools[BasicPoolName]; !ok {
		pools[BasicPoolName] = newTokenPool(BasicPoolName)
	}
	if _, ok := pools[SuperPoolName]; !ok {
		pools[SuperPoolName] = newTokenPool(SuperPoolName)
	}
	return pools
}

func parseToken(input any, poolName string) (*TokenInfo, bool) {
	switch typed := input.(type) {
	case string:
		return NewTokenInfo(typed, defaultQuotaForPool(poolName)), true
	case map[string]any:
		token := normalizeToken(asString(typed["token"]))
		if token == "" {
			return nil, false
		}
		quota := asInt(typed["quota"], defaultQuotaForPool(poolName))
		createdAt := asInt64(typed["created_at"], time.Now().UnixMilli())
		status := TokenStatus(strings.ToLower(asString(typed["status"])))
		if status == "" {
			status = TokenStatusActive
		}
		info := &TokenInfo{
			Token:          token,
			Status:         status,
			Quota:          quota,
			CreatedAt:      createdAt,
			UseCount:       asInt(typed["use_count"], 0),
			FailCount:      asInt(typed["fail_count"], 0),
			LastFailReason: asString(typed["last_fail_reason"]),
			Note:           asString(typed["note"]),
			Tags:           asStringSlice(typed["tags"]),
		}
		if v, ok := asOptionalInt64(typed["last_used_at"]); ok {
			info.LastUsedAt = ptrInt64(v)
		}
		if v, ok := asOptionalInt64(typed["last_fail_at"]); ok {
			info.LastFailAt = ptrInt64(v)
		}
		if v, ok := asOptionalInt64(typed["last_sync_at"]); ok {
			info.LastSyncAt = ptrInt64(v)
		}
		return info, true
	default:
		return nil, false
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asInt(v any, fallback int) int {
	if v == nil {
		return fallback
	}
	s := fmt.Sprintf("%v", v)
	i, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return i
}

func asInt64(v any, fallback int64) int64 {
	if v == nil {
		return fallback
	}
	s := fmt.Sprintf("%v", v)
	i, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return fallback
	}
	return i
}

func asOptionalInt64(v any) (int64, bool) {
	if v == nil {
		return 0, false
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" {
		return 0, false
	}
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return i, true
}

func asStringSlice(v any) []string {
	slice, ok := v.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(slice))
	for _, item := range slice {
		s := asString(item)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func extractQuota(result map[string]any) (int, bool) {
	if result == nil {
		return 0, false
	}
	if v, ok := result["remainingTokens"]; ok {
		return asInt(v, 0), true
	}
	if v, ok := result["remainingQueries"]; ok {
		return asInt(v, 0), true
	}
	return 0, false
}

func extractWindowSizeSeconds(result map[string]any) (int, bool) {
	if result == nil {
		return 0, false
	}
	for _, key := range []string{"windowSizeSeconds", "window_size_seconds"} {
		if v, ok := result[key]; ok {
			return asInt(v, 0), true
		}
	}
	limits, ok := result["limits"].(map[string]any)
	if !ok {
		limits, ok = result["rateLimits"].(map[string]any)
		if !ok {
			return 0, false
		}
	}
	for _, key := range []string{"windowSizeSeconds", "window_size_seconds"} {
		if v, ok := limits[key]; ok {
			return asInt(v, 0), true
		}
	}
	return 0, false
}

func isUnauthorizedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized")
}
