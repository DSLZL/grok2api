package reverse

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	ErrorCodeInvalidAPIKey       = "invalid_api_key"
	ErrorCodeRateLimitExceeded   = "rate_limit_exceeded"
	ErrorCodeUpstreamServerError = "upstream_server_error"
	ErrorCodeWsClosed            = "ws_closed"
	ErrorCodeBlocked             = "blocked"
	ErrorCodeConnectionFailed    = "connection_failed"
)

type ReverseError struct {
	Status     int
	Code       string
	Message    string
	Body       string
	RetryAfter time.Duration
	Details    map[string]any
}

func (e *ReverseError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.Status > 0 {
		return fmt.Sprintf("reverse upstream error: status=%d code=%s", e.Status, e.Code)
	}
	return fmt.Sprintf("reverse upstream error: code=%s", e.Code)
}

func MapHTTPError(status int, body string, headers http.Header) *ReverseError {
	err := &ReverseError{
		Status:  status,
		Body:    body,
		Message: fmt.Sprintf("app chat upstream failed: %d", status),
	}

	switch {
	case status == http.StatusUnauthorized:
		err.Code = ErrorCodeInvalidAPIKey
	case status == http.StatusTooManyRequests:
		err.Code = ErrorCodeRateLimitExceeded
		err.RetryAfter = parseRetryAfterHeader(headers)
	case status >= 500:
		err.Code = ErrorCodeUpstreamServerError
	default:
		err.Code = ErrorCodeUpstreamServerError
	}

	return err
}

func MapWSCloseCode(closeCode int, reason string) *ReverseError {
	err := &ReverseError{
		Status:  http.StatusBadGateway,
		Code:    ErrorCodeWsClosed,
		Message: fmt.Sprintf("websocket closed: code=%d reason=%s", closeCode, reason),
		Details: map[string]any{"close_code": closeCode, "reason": reason},
	}

	switch closeCode {
	case 4001:
		err.Status = http.StatusUnauthorized
		err.Code = ErrorCodeInvalidAPIKey
	case 4008:
		err.Status = http.StatusTooManyRequests
		err.Code = ErrorCodeRateLimitExceeded
	case 1008:
		err.Status = http.StatusForbidden
		err.Code = ErrorCodeBlocked
	}

	return err
}

func parseRetryAfterHeader(headers http.Header) time.Duration {
	if headers == nil {
		return 0
	}
	value := strings.TrimSpace(headers.Get("Retry-After"))
	if value == "" {
		value = strings.TrimSpace(headers.Get("retry-after"))
	}
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}

	if at, err := http.ParseTime(value); err == nil {
		delta := time.Until(at)
		if delta > 0 {
			return delta
		}
	}

	return 0
}
