package errors

import (
	"encoding/json"
	stdErrors "errors"
	"strings"
	"testing"
)

func TestErrorResponseEnvelopeSchemaStrict(t *testing.T) {
	env := ErrorResponse("boom", ErrorTypeServer, nil, nil)

	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(payload) != 1 {
		t.Fatalf("top-level keys = %d, want 1", len(payload))
	}

	errorObj, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("payload.error type = %T, want object", payload["error"])
	}

	if len(errorObj) != 4 {
		t.Fatalf("error keys = %d, want 4(message/type/param/code)", len(errorObj))
	}

	for _, key := range []string{"message", "type", "param", "code"} {
		if _, exists := errorObj[key]; !exists {
			t.Fatalf("missing error.%s", key)
		}
	}

	if errorObj["param"] != nil {
		t.Fatalf("error.param = %v, want nil", errorObj["param"])
	}
	if errorObj["code"] != nil {
		t.Fatalf("error.code = %v, want nil", errorObj["code"])
	}
}

func TestHttpStatusToErrorTypeMapping(t *testing.T) {
	tests := []struct {
		status   int
		wantType string
		wantCode any
	}{
		{status: 401, wantType: ErrorTypeAuthentication, wantCode: "invalid_api_key"},
		{status: 403, wantType: ErrorTypePermission, wantCode: "insufficient_quota"},
		{status: 404, wantType: ErrorTypeNotFound, wantCode: "model_not_found"},
		{status: 429, wantType: ErrorTypeRateLimit, wantCode: "rate_limit_exceeded"},
		{status: 500, wantType: ErrorTypeServer, wantCode: nil},
	}

	for _, tt := range tests {
		gotType, gotCode := HTTPStatusToErrorTypeMapping(tt.status)
		if gotType != tt.wantType {
			t.Fatalf("status=%d type=%q, want %q", tt.status, gotType, tt.wantType)
		}
		if gotCode != tt.wantCode {
			t.Fatalf("status=%d code=%v, want %v", tt.status, gotCode, tt.wantCode)
		}
	}
}

func TestValidationErrorResponseJSONSpecialRule(t *testing.T) {
	env := ValidationErrorResponse("JSON decode failed", "json_invalid", []any{"body", "payload"})

	if env.Error.Message != InvalidJSONMessage {
		t.Fatalf("message=%q, want %q", env.Error.Message, InvalidJSONMessage)
	}
	if env.Error.Param != "body" {
		t.Fatalf("param=%v, want body", env.Error.Param)
	}
	if env.Error.Code != "json_invalid" {
		t.Fatalf("code=%v, want json_invalid", env.Error.Code)
	}

	env2 := ValidationErrorResponse("Invalid JSON body", "invalid_value", []any{"body", "x"})
	if env2.Error.Message != InvalidJSONMessage {
		t.Fatalf("message=%q, want %q", env2.Error.Message, InvalidJSONMessage)
	}
	if env2.Error.Param != "body" {
		t.Fatalf("param=%v, want body", env2.Error.Param)
	}
}

func TestValidationErrorResponseParamExtraction(t *testing.T) {
	env := ValidationErrorResponse(
		"invalid content",
		"invalid_value",
		[]any{"body", "messages", 0, "content", "1"},
	)

	if env.Error.Param != "body.messages.content" {
		t.Fatalf("param=%v, want body.messages.content", env.Error.Param)
	}
	if env.Error.Message != "invalid content" {
		t.Fatalf("message=%q, want invalid content", env.Error.Message)
	}
	if env.Error.Code != "invalid_value" {
		t.Fatalf("code=%v, want invalid_value", env.Error.Code)
	}
}

func TestSSEErrorFrameIncludesEventErrorAndDone(t *testing.T) {
	frame := SSEErrorFrame(NewAppError(
		"too many requests",
		ErrorTypeRateLimit,
		"rate_limit_exceeded",
		nil,
		429,
	))

	if !strings.HasPrefix(frame, "event: error\ndata: ") {
		t.Fatalf("frame prefix invalid: %q", frame)
	}
	if !strings.Contains(frame, "\n\ndata: [DONE]\n\n") {
		t.Fatalf("frame missing done marker: %q", frame)
	}
}

func TestSSEErrorFrameFallbackForUnknownError(t *testing.T) {
	frame := SSEErrorFrame(stdErrors.New(""))

	if !strings.Contains(frame, `"type":"server_error"`) {
		t.Fatalf("frame missing server_error: %q", frame)
	}
	if !strings.Contains(frame, `"code":"stream_error"`) {
		t.Fatalf("frame missing stream_error code: %q", frame)
	}
	if !strings.Contains(frame, `"message":"stream_error"`) {
		t.Fatalf("frame missing stream_error message: %q", frame)
	}
}
