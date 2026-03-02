package reverse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNormalizeChatProxy(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "socks5 to socks5h", in: "socks5://127.0.0.1:1080", want: "socks5h://127.0.0.1:1080"},
		{name: "socks4 to socks4a", in: "socks4://127.0.0.1:9050", want: "socks4a://127.0.0.1:9050"},
		{name: "http unchanged", in: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "empty", in: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeChatProxy(tc.in)
			if got != tc.want {
				t.Fatalf("NormalizeChatProxy() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildAppChatPayloadIncludesKeyFields(t *testing.T) {
	cfg := DefaultAppChatConfig()
	req := AppChatRequest{
		Message: "hello",
		Model:   "grok-4",
		Mode:    "chat",
		FileAttachments: []string{
			"https://example.com/a.png",
		},
		ToolOverrides: map[string]any{"search": true},
	}

	payload := BuildAppChatPayload(req, cfg)

	if payload["message"] != "hello" {
		t.Fatalf("payload.message = %v, want hello", payload["message"])
	}
	if payload["modelName"] != "grok-4" {
		t.Fatalf("payload.modelName = %v, want grok-4", payload["modelName"])
	}
	if payload["modelMode"] != "chat" {
		t.Fatalf("payload.modelMode = %v, want chat", payload["modelMode"])
	}
	if payload["disableMemory"] != cfg.DisableMemory {
		t.Fatalf("payload.disableMemory = %v, want %v", payload["disableMemory"], cfg.DisableMemory)
	}
	if payload["temporary"] != cfg.Temporary {
		t.Fatalf("payload.temporary = %v, want %v", payload["temporary"], cfg.Temporary)
	}

	attachments, ok := payload["fileAttachments"].([]string)
	if !ok || len(attachments) != 1 {
		t.Fatalf("payload.fileAttachments = %#v, want 1 element", payload["fileAttachments"])
	}

	responseMetadata, ok := payload["responseMetadata"].(map[string]any)
	if !ok {
		t.Fatalf("payload.responseMetadata type = %T, want map[string]any", payload["responseMetadata"])
	}
	requestModelDetails, ok := responseMetadata["requestModelDetails"].(map[string]any)
	if !ok {
		t.Fatalf("payload.responseMetadata.requestModelDetails type = %T, want map[string]any", responseMetadata["requestModelDetails"])
	}
	if requestModelDetails["modelId"] != "grok-4" {
		t.Fatalf("payload.responseMetadata.requestModelDetails.modelId = %v, want grok-4", requestModelDetails["modelId"])
	}
}

func TestAppChatStreamHappyPath(t *testing.T) {
	body, err := os.ReadFile("testdata/app_chat_stream.txt")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}

		var payload map[string]any
		if decodeErr := json.NewDecoder(r.Body).Decode(&payload); decodeErr != nil {
			t.Fatalf("Decode(payload) error = %v", decodeErr)
		}
		if payload["modelName"] != "grok-4" {
			t.Fatalf("payload.modelName = %v, want grok-4", payload["modelName"])
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer server.Close()

	adapter := NewAppChatAdapter(server.URL, server.Client(), DefaultAppChatConfig())

	var got []string
	streamErr := adapter.Stream(
		context.Background(),
		"sso=test-token",
		AppChatRequest{Message: "hello", Model: "grok-4"},
		func(line string) error {
			got = append(got, line)
			return nil
		},
	)
	if streamErr != nil {
		t.Fatalf("Stream() error = %v", streamErr)
	}

	want := strings.Split(strings.TrimSpace(string(body)), "\n")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stream lines = %#v, want %#v", got, want)
	}
}

func TestMapHTTPErrorEntryPoints(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		headers  http.Header
		wantCode string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantCode: ErrorCodeInvalidAPIKey},
		{name: "rate limit", status: http.StatusTooManyRequests, headers: http.Header{"Retry-After": []string{"2"}}, wantCode: ErrorCodeRateLimitExceeded},
		{name: "server error", status: http.StatusBadGateway, wantCode: ErrorCodeUpstreamServerError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := MapHTTPError(tc.status, "upstream-body", tc.headers)
			if err == nil {
				t.Fatal("MapHTTPError() returned nil")
			}
			if err.Code != tc.wantCode {
				t.Fatalf("MapHTTPError().Code = %q, want %q", err.Code, tc.wantCode)
			}
			if tc.status == http.StatusTooManyRequests && err.RetryAfter != 2*time.Second {
				t.Fatalf("MapHTTPError().RetryAfter = %s, want 2s", err.RetryAfter)
			}
		})
	}
}
