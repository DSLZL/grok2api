package token

import (
	"strings"
	"time"
)

const (
	BasicDefaultQuota = 80
	SuperDefaultQuota = 140
	FailThreshold     = 5

	BasicPoolName = "ssoBasic"
	SuperPoolName = "ssoSuper"

	SuperWindowThresholdSeconds = 14400
)

type TokenStatus string

const (
	TokenStatusActive   TokenStatus = "active"
	TokenStatusDisabled TokenStatus = "disabled"
	TokenStatusExpired  TokenStatus = "expired"
	TokenStatusCooling  TokenStatus = "cooling"
)

type EffortType string

const (
	EffortLow  EffortType = "low"
	EffortHigh EffortType = "high"
)

var effortCost = map[EffortType]int{
	EffortLow:  1,
	EffortHigh: 4,
}

type TokenInfo struct {
	Token          string
	Status         TokenStatus
	Quota          int
	CreatedAt      int64
	LastUsedAt     *int64
	UseCount       int
	FailCount      int
	LastFailAt     *int64
	LastFailReason string
	LastSyncAt     *int64
	Tags           []string
	Note           string
}

func NewTokenInfo(token string, quota int) *TokenInfo {
	now := time.Now().UnixMilli()
	if quota < 0 {
		quota = 0
	}
	return &TokenInfo{
		Token:     normalizeToken(token),
		Status:    TokenStatusActive,
		Quota:     quota,
		CreatedAt: now,
		Tags:      []string{},
	}
}

func (t *TokenInfo) Consume(effort EffortType) int {
	if t == nil {
		return 0
	}
	cost, ok := effortCost[effort]
	if !ok {
		cost = effortCost[EffortLow]
	}
	if cost < 0 {
		cost = 0
	}
	actual := cost
	if actual > t.Quota {
		actual = t.Quota
	}
	now := time.Now().UnixMilli()
	t.LastUsedAt = ptrInt64(now)
	t.UseCount += actual
	t.Quota -= actual
	if t.Quota < 0 {
		t.Quota = 0
	}

	if t.Quota == 0 {
		t.Status = TokenStatusCooling
	} else if t.Status == TokenStatusCooling {
		t.Status = TokenStatusActive
	}

	return actual
}

func (t *TokenInfo) UpdateQuota(newQuota int) {
	if t == nil {
		return
	}
	if newQuota < 0 {
		newQuota = 0
	}
	t.Quota = newQuota
	if t.Quota == 0 {
		t.Status = TokenStatusCooling
	} else if t.Status == TokenStatusCooling || t.Status == TokenStatusExpired {
		t.Status = TokenStatusActive
	}
}

func (t *TokenInfo) RecordFail(statusCode int, reason string, threshold int) {
	if t == nil || statusCode != 401 {
		return
	}
	if threshold < 1 {
		threshold = 1
	}
	now := time.Now().UnixMilli()
	t.FailCount++
	t.LastFailAt = ptrInt64(now)
	t.LastFailReason = reason
	if t.FailCount >= threshold {
		t.Status = TokenStatusExpired
	}
}

func (t *TokenInfo) NeedRefresh(interval time.Duration) bool {
	if t == nil || t.Status != TokenStatusCooling {
		return false
	}
	if t.LastSyncAt == nil {
		return true
	}
	if interval <= 0 {
		return true
	}
	next := time.UnixMilli(*t.LastSyncAt).Add(interval)
	return !time.Now().Before(next)
}

func (t *TokenInfo) MarkSynced() {
	if t == nil {
		return
	}
	now := time.Now().UnixMilli()
	t.LastSyncAt = ptrInt64(now)
}

func (t *TokenInfo) ToMap() map[string]any {
	out := map[string]any{
		"token":      t.Token,
		"status":     string(t.Status),
		"quota":      t.Quota,
		"created_at": t.CreatedAt,
		"use_count":  t.UseCount,
		"fail_count": t.FailCount,
		"tags":       append([]string(nil), t.Tags...),
		"note":       t.Note,
	}
	if t.LastUsedAt != nil {
		out["last_used_at"] = *t.LastUsedAt
	}
	if t.LastFailAt != nil {
		out["last_fail_at"] = *t.LastFailAt
	}
	if strings.TrimSpace(t.LastFailReason) != "" {
		out["last_fail_reason"] = t.LastFailReason
	}
	if t.LastSyncAt != nil {
		out["last_sync_at"] = *t.LastSyncAt
	}
	return out
}

func defaultQuotaForPool(poolName string) int {
	if poolName == SuperPoolName {
		return SuperDefaultQuota
	}
	return BasicDefaultQuota
}

func normalizeToken(token string) string {
	return strings.TrimPrefix(strings.TrimSpace(token), "sso=")
}

func ptrInt64(v int64) *int64 {
	return &v
}
