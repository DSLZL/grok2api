package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const traceIDContextKey = "trace_id"

type Logger interface {
	Info(message string, fields map[string]any)
	Error(message string, fields map[string]any)
}

type TraceIDProvider interface {
	NewTraceID(r *http.Request) string
}

type ResponseLoggerMiddleware struct {
	logger    Logger
	traceID   TraceIDProvider
	now       func() time.Time
	skipPaths map[string]struct{}
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

type CaptureLogger struct {
	Entries []LogEntry
}

type LogEntry struct {
	Level   string
	Message string
	Fields  map[string]any
}

type noopLogger struct{}

type stdLogger struct{}

type traceIDProviderFunc func(*http.Request) string

func (f traceIDProviderFunc) NewTraceID(r *http.Request) string {
	return f(r)
}

func NewResponseLoggerMiddleware(logger Logger, traceIDProvider TraceIDProvider) *ResponseLoggerMiddleware {
	if logger == nil {
		logger = stdLogger{}
	}
	if traceIDProvider == nil {
		traceIDProvider = TraceIDHeader()
	}

	skipPaths := map[string]struct{}{
		"/":             {},
		"/login":        {},
		"/imagine":      {},
		"/voice":        {},
		"/admin":        {},
		"/admin/login":  {},
		"/admin/config": {},
		"/admin/cache":  {},
		"/admin/token":  {},
	}

	return &ResponseLoggerMiddleware{
		logger:    logger,
		traceID:   traceIDProvider,
		now:       time.Now,
		skipPaths: skipPaths,
	}
}

func (m *ResponseLoggerMiddleware) Wrap(next http.Handler) http.Handler {
	if next == nil {
		next = http.NotFoundHandler()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if m.shouldSkipPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		traceID := m.traceID.NewTraceID(r)
		ctx := context.WithValue(r.Context(), traceIDContextKey, traceID)
		r = r.WithContext(ctx)
		w.Header().Set("X-Trace-ID", traceID)

		start := m.now()
		m.logger.Info(
			fmt.Sprintf("Request: %s %s", r.Method, path),
			map[string]any{
				"traceID": traceID,
				"method":  r.Method,
				"path":    path,
			},
		)

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			if recovered := recover(); recovered != nil {
				duration := m.now().Sub(start).Seconds() * 1000
				m.logger.Error(
					fmt.Sprintf("Response Error: %s %s - %s (%.2fms)", r.Method, path, panicString(recovered), duration),
					map[string]any{
						"traceID":     traceID,
						"method":      r.Method,
						"path":        path,
						"duration_ms": roundMs(duration),
						"error":       panicString(recovered),
					},
				)
				panic(recovered)
			}
		}()

		next.ServeHTTP(recorder, r)

		duration := m.now().Sub(start).Seconds() * 1000
		m.logger.Info(
			fmt.Sprintf("Response: %s %s - %d (%.2fms)", r.Method, path, recorder.status, duration),
			map[string]any{
				"traceID":     traceID,
				"method":      r.Method,
				"path":        path,
				"status":      recorder.status,
				"duration_ms": roundMs(duration),
			},
		)
	})
}

func (m *ResponseLoggerMiddleware) shouldSkipPath(path string) bool {
	if strings.HasPrefix(path, "/static/") {
		return true
	}
	_, ok := m.skipPaths[path]
	return ok
}

func (s *statusRecorder) WriteHeader(statusCode int) {
	if s.wroteHeader {
		return
	}
	s.wroteHeader = true
	s.status = statusCode
	s.ResponseWriter.WriteHeader(statusCode)
}

func (s *statusRecorder) Write(data []byte) (int, error) {
	if !s.wroteHeader {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(data)
}

func (c *CaptureLogger) Info(message string, fields map[string]any) {
	c.Entries = append(c.Entries, LogEntry{Level: "info", Message: message, Fields: cloneFields(fields)})
}

func (c *CaptureLogger) Error(message string, fields map[string]any) {
	c.Entries = append(c.Entries, LogEntry{Level: "error", Message: message, Fields: cloneFields(fields)})
}

func (noopLogger) Info(string, map[string]any)  {}
func (noopLogger) Error(string, map[string]any) {}

func (stdLogger) Info(message string, fields map[string]any) {
	log.Printf("INFO %s fields=%v", message, fields)
}

func (stdLogger) Error(message string, fields map[string]any) {
	log.Printf("ERROR %s fields=%v", message, fields)
}

func NopLogger() Logger {
	return noopLogger{}
}

func TraceIDHeader() TraceIDProvider {
	return traceIDProviderFunc(func(*http.Request) string {
		return randomTraceID()
	})
}

func StaticTraceIDProvider(value string) TraceIDProvider {
	return traceIDProviderFunc(func(*http.Request) string {
		return value
	})
}

func GetTraceID(ctx context.Context) (string, bool) {
	value := ctx.Value(traceIDContextKey)
	traceID, ok := value.(string)
	if !ok || strings.TrimSpace(traceID) == "" {
		return "", false
	}
	return traceID, true
}

func randomTraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	hexStr := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexStr[0:8], hexStr[8:12], hexStr[12:16], hexStr[16:20], hexStr[20:32])
}

func cloneFields(fields map[string]any) map[string]any {
	if fields == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		out[key] = value
	}
	return out
}

func panicString(value any) string {
	if value == nil {
		return ""
	}
	if err, ok := value.(error); ok {
		return err.Error()
	}
	return fmt.Sprintf("%v", value)
}

func roundMs(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
