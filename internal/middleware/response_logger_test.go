package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResponseLoggerMiddlewareSkipsConfiguredPaths(t *testing.T) {
	skips := []string{
		"/",
		"/login",
		"/imagine",
		"/voice",
		"/admin",
		"/admin/login",
		"/admin/config",
		"/admin/cache",
		"/admin/token",
		"/static/public/pages/login.html",
	}

	for _, path := range skips {
		path := path
		t.Run(path, func(t *testing.T) {
			recorder := &CaptureLogger{}
			mwWithLogger := NewResponseLoggerMiddleware(recorder, TraceIDHeader())

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := GetTraceID(r.Context()); ok {
					t.Fatalf("trace id should not be injected for skipped path: %s", path)
				}
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, path, nil)
			rr := httptest.NewRecorder()

			mwWithLogger.Wrap(next).ServeHTTP(rr, req)

			if len(recorder.Entries) != 0 {
				t.Fatalf("logs=%d, want 0 for skipped path %s", len(recorder.Entries), path)
			}
		})
	}
}

func TestResponseLoggerMiddlewareInjectsTraceAndLogsSuccess(t *testing.T) {
	recorder := &CaptureLogger{}
	mw := NewResponseLoggerMiddleware(recorder, StaticTraceIDProvider("trace-fixed"))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID, ok := GetTraceID(r.Context())
		if !ok {
			t.Fatalf("trace id missing in context")
		}
		if traceID != "trace-fixed" {
			t.Fatalf("trace id=%q, want trace-fixed", traceID)
		}
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	rr := httptest.NewRecorder()

	mw.Wrap(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status=%d, want %d", rr.Code, http.StatusCreated)
	}

	if len(recorder.Entries) != 2 {
		t.Fatalf("logs=%d, want 2(request+response)", len(recorder.Entries))
	}

	requestLog := recorder.Entries[0]
	if requestLog.Level != "info" {
		t.Fatalf("request level=%q, want info", requestLog.Level)
	}
	assertLogField(t, requestLog.Fields, "traceID", "trace-fixed")
	assertLogField(t, requestLog.Fields, "method", http.MethodPost)
	assertLogField(t, requestLog.Fields, "path", "/v1/models")

	responseLog := recorder.Entries[1]
	if responseLog.Level != "info" {
		t.Fatalf("response level=%q, want info", responseLog.Level)
	}
	assertLogField(t, responseLog.Fields, "traceID", "trace-fixed")
	assertLogField(t, responseLog.Fields, "method", http.MethodPost)
	assertLogField(t, responseLog.Fields, "path", "/v1/models")
	assertLogField(t, responseLog.Fields, "status", http.StatusCreated)

	durObj, ok := responseLog.Fields["duration_ms"]
	if !ok {
		t.Fatalf("response log missing duration_ms")
	}
	if _, ok := durObj.(float64); !ok {
		t.Fatalf("duration_ms type=%T, want float64", durObj)
	}
}

func TestResponseLoggerMiddlewareLogsErrorAndRethrows(t *testing.T) {
	recorder := &CaptureLogger{}
	mw := NewResponseLoggerMiddleware(recorder, StaticTraceIDProvider("trace-err"))

	boom := errors.New("boom")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(boom)
	})

	wrapped := mw.Wrap(next)
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	rr := httptest.NewRecorder()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic to be rethrown")
		}

		if len(recorder.Entries) != 2 {
			t.Fatalf("logs=%d, want 2(request+error)", len(recorder.Entries))
		}

		errorLog := recorder.Entries[1]
		if errorLog.Level != "error" {
			t.Fatalf("error level=%q, want error", errorLog.Level)
		}
		assertLogField(t, errorLog.Fields, "traceID", "trace-err")
		assertLogField(t, errorLog.Fields, "method", http.MethodGet)
		assertLogField(t, errorLog.Fields, "path", "/v1/chat/completions")

		errObj, ok := errorLog.Fields["error"]
		if !ok {
			t.Fatalf("error log missing error field")
		}
		if errObj != "boom" {
			t.Fatalf("error=%v, want boom", errObj)
		}
	}()

	wrapped.ServeHTTP(rr, req)
}

func assertLogField(t *testing.T, fields map[string]any, key string, want any) {
	t.Helper()
	got, ok := fields[key]
	if !ok {
		t.Fatalf("missing field %s", key)
	}
	if got != want {
		t.Fatalf("field %s=%v, want %v", key, got, want)
	}
}
