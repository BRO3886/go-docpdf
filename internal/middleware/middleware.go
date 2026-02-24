// Package middleware provides HTTP middleware for request tracing,
// structured JSON logging, and Prometheus metrics collection.
package middleware

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/BRO3886/go-docpdf/internal/metrics"
)

// contextKey is an unexported type for context keys in this package.
type contextKey struct{}

// requestState holds per-request observability state set on the context.
type requestState struct {
	id       string
	logError string
	outcome  string
}

// RequestIDFromContext returns the request ID stored by RequestID middleware,
// or "" if none is present.
func RequestIDFromContext(ctx context.Context) string {
	if s, ok := ctx.Value(contextKey{}).(*requestState); ok && s != nil {
		return s.id
	}
	return ""
}

// SetOutcome records the conversion outcome ("success", "timeout", "failed")
// on the context. It is a no-op when no state is present (e.g., in tests that
// do not use the middleware).
func SetOutcome(ctx context.Context, outcome string) {
	if s, ok := ctx.Value(contextKey{}).(*requestState); ok && s != nil {
		s.outcome = outcome
	}
}

// SetLogError records a human-readable error reason that the Logging middleware
// will include as an "error" field in the structured log line. It is a no-op
// when no state is present.
func SetLogError(ctx context.Context, reason string) {
	if s, ok := ctx.Value(contextKey{}).(*requestState); ok && s != nil {
		s.logError = reason
	}
}

// RequestID is middleware that ensures every request carries an X-Request-ID
// header. If the incoming request already has one it is reused; otherwise a
// new UUIDv4 is generated.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newUUID()
		}

		state := &requestState{id: id}
		ctx := context.WithValue(r.Context(), contextKey{}, state)
		w.Header().Set("X-Request-ID", id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// responseRecorder wraps http.ResponseWriter to capture the status code.
type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.status = code
	rr.ResponseWriter.WriteHeader(code)
}

// Logging is middleware that emits one structured JSON log line to stderr
// after each request completes, including request ID, method, path, status,
// duration, and any error set via SetLogError.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		durationMs := time.Since(start).Milliseconds()
		fields := map[string]any{
			"time":        time.Now().UTC().Format(time.RFC3339),
			"request_id":  RequestIDFromContext(r.Context()),
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      rec.status,
			"duration_ms": durationMs,
		}
		if s, ok := r.Context().Value(contextKey{}).(*requestState); ok && s != nil && s.logError != "" {
			fields["error"] = s.logError
		}

		line, _ := json.Marshal(fields)
		fmt.Fprintf(os.Stderr, "%s\n", line)
	})
}

// Metrics is middleware that records conversion metrics (in-flight gauge,
// outcome counters, and duration histogram) for each request.
// It should only wrap /convert, not /health or /metrics.
func Metrics(reg *metrics.Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reg.IncInFlight()
		start := time.Now()

		next.ServeHTTP(w, r)

		durationMs := time.Since(start).Milliseconds()
		reg.DecInFlight()
		reg.ObserveDuration(durationMs)

		outcome := "failed"
		if s, ok := r.Context().Value(contextKey{}).(*requestState); ok && s != nil && s.outcome != "" {
			outcome = s.outcome
		}
		switch outcome {
		case "success":
			reg.IncSuccess()
		case "timeout":
			reg.IncTimeout()
		default:
			reg.IncFailed()
		}
	})
}

// newUUID generates a random UUIDv4 string.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
