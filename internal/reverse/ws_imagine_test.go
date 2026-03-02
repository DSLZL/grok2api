package reverse

import (
	"context"
	"io"
	"testing"
)

type fakeWSMessageSource struct {
	messages []WSMessage
	index    int
	err      error
}

func (f *fakeWSMessageSource) Receive(context.Context) (WSMessage, error) {
	if f.err != nil {
		return WSMessage{}, f.err
	}
	if f.index >= len(f.messages) {
		return WSMessage{}, io.EOF
	}
	msg := f.messages[f.index]
	f.index++
	return msg, nil
}

func (f *fakeWSMessageSource) Close() error { return nil }

func TestWsServerCloseCodeMapping(t *testing.T) {
	tests := []struct {
		name       string
		code       int
		wantStatus int
		wantCode   string
	}{
		{name: "auth close", code: 4001, wantStatus: 401, wantCode: ErrorCodeInvalidAPIKey},
		{name: "rate limit close", code: 4008, wantStatus: 429, wantCode: ErrorCodeRateLimitExceeded},
		{name: "blocked close", code: 1008, wantStatus: 403, wantCode: ErrorCodeBlocked},
		{name: "generic close", code: 1011, wantStatus: 502, wantCode: ErrorCodeWsClosed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := MapWSCloseCode(tc.code, "test")
			if err == nil {
				t.Fatal("MapWSCloseCode() returned nil")
			}
			if err.Status != tc.wantStatus {
				t.Fatalf("MapWSCloseCode().Status = %d, want %d", err.Status, tc.wantStatus)
			}
			if err.Code != tc.wantCode {
				t.Fatalf("MapWSCloseCode().Code = %q, want %q", err.Code, tc.wantCode)
			}
		})
	}
}

func TestWsCloseAndRetryMapping(t *testing.T) {
	cfg := DefaultWSImagineConfig()
	cfg.MaxRetries = 2
	cfg.MediumMinBytes = 3
	cfg.FinalMinBytes = 5

	attempts := 0
	adapter := NewWSImagineAdapter(cfg, func(context.Context) (WSMessageSource, error) {
		attempts++
		if attempts == 1 {
			return &fakeWSMessageSource{messages: []WSMessage{{Type: WSMessageTypeClose, CloseCode: 1011, CloseReason: "boom"}}}, nil
		}
		return &fakeWSMessageSource{messages: []WSMessage{{Type: WSMessageTypeImage, URL: "https://grok.com/images/abc-def.jpg", Blob: "123456"}}}, nil
	})

	var items []ImagineItem
	err := adapter.Stream(context.Background(), 1, func(item ImagineItem) error {
		items = append(items, item)
		return nil
	})
	if err != nil {
		t.Fatalf("Stream() error = %v, want nil", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if !items[0].IsFinal {
		t.Fatalf("items[0].IsFinal = %v, want true", items[0].IsFinal)
	}
}

func TestWSBlockedOnMediumWithoutFinal(t *testing.T) {
	cfg := DefaultWSImagineConfig()
	cfg.MaxRetries = 1
	cfg.MediumMinBytes = 3
	cfg.FinalMinBytes = 10

	adapter := NewWSImagineAdapter(cfg, func(context.Context) (WSMessageSource, error) {
		return &fakeWSMessageSource{
			messages: []WSMessage{
				{Type: WSMessageTypeImage, URL: "https://grok.com/images/abc-def.jpg", Blob: "12345"},
				{Type: WSMessageTypeTimeout},
			},
		}, nil
	})

	err := adapter.Stream(context.Background(), 1, func(item ImagineItem) error { return nil })
	if err == nil {
		t.Fatal("Stream() error = nil, want blocked error")
	}
	revErr, ok := err.(*ReverseError)
	if !ok {
		t.Fatalf("Stream() error type = %T, want *ReverseError", err)
	}
	if revErr.Code != ErrorCodeBlocked {
		t.Fatalf("blocked error code = %q, want %q", revErr.Code, ErrorCodeBlocked)
	}
}
