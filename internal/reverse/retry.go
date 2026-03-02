package reverse

import (
	"context"
	"errors"
	"math"
	"time"
)

type RetryPolicy struct {
	MaxRetry      int
	StatusCodes   []int
	RetryBudget   time.Duration
	BackoffBase   time.Duration
	BackoffFactor float64
	BackoffMax    time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetry:      3,
		StatusCodes:   []int{401, 429, 403},
		RetryBudget:   60 * time.Second,
		BackoffBase:   500 * time.Millisecond,
		BackoffFactor: 2.0,
		BackoffMax:    20 * time.Second,
	}
}

type SleepFunc func(delay time.Duration) error

func RetryOnError(ctx context.Context, policy RetryPolicy, sleepFn SleepFunc, operation func() error) error {
	if operation == nil {
		return errors.New("retry operation is nil")
	}
	if policy.BackoffBase <= 0 {
		policy.BackoffBase = 500 * time.Millisecond
	}
	if policy.BackoffFactor <= 0 {
		policy.BackoffFactor = 2.0
	}
	if policy.BackoffMax <= 0 {
		policy.BackoffMax = 20 * time.Second
	}
	if policy.MaxRetry < 0 {
		policy.MaxRetry = 0
	}
	if sleepFn == nil {
		sleepFn = func(delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}

	totalDelay := time.Duration(0)
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := operation()
		if err == nil {
			return nil
		}

		revErr := asReverseError(err)
		status := 0
		if revErr != nil {
			status = revErr.Status
		}

		if attempt >= policy.MaxRetry || !shouldRetryStatus(status, policy.StatusCodes) {
			return err
		}

		delay := calculateDelay(policy, attempt, revErr)
		if policy.RetryBudget > 0 && totalDelay+delay > policy.RetryBudget {
			return err
		}
		if delay > 0 {
			if sleepErr := sleepFn(delay); sleepErr != nil {
				return sleepErr
			}
			totalDelay += delay
		}
	}
}

func shouldRetryStatus(status int, retryStatusCodes []int) bool {
	if status == 0 {
		return false
	}
	for _, code := range retryStatusCodes {
		if status == code {
			return true
		}
	}
	return false
}

func calculateDelay(policy RetryPolicy, attempt int, err *ReverseError) time.Duration {
	if err != nil && err.RetryAfter > 0 {
		if err.RetryAfter > policy.BackoffMax && policy.BackoffMax > 0 {
			return policy.BackoffMax
		}
		return err.RetryAfter
	}

	multiplier := math.Pow(policy.BackoffFactor, float64(attempt))
	delay := time.Duration(float64(policy.BackoffBase) * multiplier)
	if delay > policy.BackoffMax && policy.BackoffMax > 0 {
		return policy.BackoffMax
	}
	return delay
}

func asReverseError(err error) *ReverseError {
	if err == nil {
		return nil
	}
	revErr, ok := err.(*ReverseError)
	if !ok {
		return nil
	}
	return revErr
}
