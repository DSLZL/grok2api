package reverse

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRetryUsesRetryAfterPriority(t *testing.T) {
	policy := RetryPolicy{
		MaxRetry:      3,
		StatusCodes:   []int{http.StatusTooManyRequests},
		RetryBudget:   10 * time.Second,
		BackoffBase:   100 * time.Millisecond,
		BackoffFactor: 2.0,
		BackoffMax:    20 * time.Second,
	}

	var sleeps []time.Duration
	attempts := 0
	err := RetryOnError(
		context.Background(),
		policy,
		func(delay time.Duration) error {
			sleeps = append(sleeps, delay)
			return nil
		},
		func() error {
			attempts++
			if attempts == 1 {
				return &ReverseError{Status: http.StatusTooManyRequests, Code: ErrorCodeRateLimitExceeded, RetryAfter: 2 * time.Second}
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("RetryOnError() error = %v, want nil", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(sleeps) != 1 {
		t.Fatalf("sleep count = %d, want 1", len(sleeps))
	}
	if sleeps[0] != 2*time.Second {
		t.Fatalf("sleep delay = %s, want 2s", sleeps[0])
	}
}

func TestRetryBudgetStopsBeforeSleep(t *testing.T) {
	policy := RetryPolicy{
		MaxRetry:      3,
		StatusCodes:   []int{http.StatusTooManyRequests},
		RetryBudget:   1 * time.Second,
		BackoffBase:   100 * time.Millisecond,
		BackoffFactor: 2.0,
		BackoffMax:    20 * time.Second,
	}

	sleepCalled := false
	attempts := 0
	err := RetryOnError(
		context.Background(),
		policy,
		func(delay time.Duration) error {
			sleepCalled = true
			return nil
		},
		func() error {
			attempts++
			return &ReverseError{Status: http.StatusTooManyRequests, Code: ErrorCodeRateLimitExceeded, RetryAfter: 2 * time.Second}
		},
	)
	if err == nil {
		t.Fatal("RetryOnError() error = nil, want budget-exhausted error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	if sleepCalled {
		t.Fatal("sleepCalled = true, want false when budget exceeded")
	}
}
