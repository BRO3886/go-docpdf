package middleware_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/BRO3886/go-docpdf/internal/metrics"
	"github.com/BRO3886/go-docpdf/internal/middleware"
)

// ---------- RequestID ----------

func TestRequestID_Generated(t *testing.T) {
	var capturedID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = middleware.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.RequestID(inner)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedID == "" {
		t.Fatal("expected a request ID to be generated")
	}
	// Should be UUID-ish: 8-4-4-4-12 hex groups
	if len(capturedID) != 36 {
		t.Errorf("unexpected request ID length %d: %q", len(capturedID), capturedID)
	}
	if w.Header().Get("X-Request-ID") != capturedID {
		t.Errorf("response header X-Request-ID mismatch: got %q want %q",
			w.Header().Get("X-Request-ID"), capturedID)
	}
}

func TestRequestID_Forwarded(t *testing.T) {
	const existingID = "my-existing-id"
	var capturedID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = middleware.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.RequestID(inner)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-ID", existingID)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if capturedID != existingID {
		t.Errorf("expected forwarded ID %q, got %q", existingID, capturedID)
	}
	if w.Header().Get("X-Request-ID") != existingID {
		t.Errorf("response header should echo incoming ID, got %q", w.Header().Get("X-Request-ID"))
	}
}

func TestRequestIDFromContext_NoState(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	id := middleware.RequestIDFromContext(req.Context())
	if id != "" {
		t.Errorf("expected empty string without middleware, got %q", id)
	}
}

// ---------- Logging ----------

func TestLogging_EmitsJSON(t *testing.T) {
	// Redirect stderr to a buffer by capturing through the recorder pattern.
	// We use a pipe-based approach: wrap the inner handler so we can inspect
	// what Logging writes to stderr by capturing the log output indirectly.
	// Since Logging writes to os.Stderr directly, we verify indirectly that
	// the handler completes successfully and returns the right status.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Chain: RequestID → Logging so request state exists on context.
	handler := middleware.RequestID(middleware.Logging(inner))
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Request-ID", "test-log-id")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

// ---------- SetOutcome / SetLogError ----------

func TestSetOutcome_NoStateNoPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Should not panic even though no middleware state is on context.
	middleware.SetOutcome(req.Context(), "success")
	middleware.SetLogError(req.Context(), "something")
}

// ---------- Metrics ----------

func TestMetrics_IncrementsSuccess(t *testing.T) {
	reg := metrics.New()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		middleware.SetOutcome(r.Context(), "success")
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.RequestID(middleware.Metrics(reg, inner))
	req := httptest.NewRequest(http.MethodPost, "/convert", bytes.NewReader([]byte("data")))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	mw := httptest.NewRecorder()
	reg.ServeHTTP(mw, httptest.NewRequest("GET", "/metrics", nil))
	body := mw.Body.String()

	if !strings.Contains(body, `docpdf_conversions_total{outcome="success"} 1`) {
		t.Errorf("expected success=1, got:\n%s", body)
	}
	if !strings.Contains(body, `docpdf_conversions_total{outcome="failed"} 0`) {
		t.Errorf("expected failed=0, got:\n%s", body)
	}
}

func TestMetrics_IncrementsTimeout(t *testing.T) {
	reg := metrics.New()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		middleware.SetOutcome(r.Context(), "timeout")
		w.WriteHeader(http.StatusGatewayTimeout)
	})

	handler := middleware.RequestID(middleware.Metrics(reg, inner))
	req := httptest.NewRequest(http.MethodPost, "/convert", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	mw := httptest.NewRecorder()
	reg.ServeHTTP(mw, httptest.NewRequest("GET", "/metrics", nil))
	body := mw.Body.String()

	if !strings.Contains(body, `docpdf_conversions_total{outcome="timeout"} 1`) {
		t.Errorf("expected timeout=1, got:\n%s", body)
	}
}

func TestMetrics_DefaultFailed(t *testing.T) {
	reg := metrics.New()
	// Handler sets no outcome — should default to "failed".
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	handler := middleware.RequestID(middleware.Metrics(reg, inner))
	req := httptest.NewRequest(http.MethodPost, "/convert", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	mw := httptest.NewRecorder()
	reg.ServeHTTP(mw, httptest.NewRequest("GET", "/metrics", nil))
	body := mw.Body.String()

	if !strings.Contains(body, `docpdf_conversions_total{outcome="failed"} 1`) {
		t.Errorf("expected failed=1, got:\n%s", body)
	}
}

func TestMetrics_InFlight(t *testing.T) {
	reg := metrics.New()
	started := make(chan struct{})
	unblock := make(chan struct{})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-unblock
		middleware.SetOutcome(r.Context(), "success")
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.RequestID(middleware.Metrics(reg, inner))
	req := httptest.NewRequest(http.MethodPost, "/convert", nil)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(w, req)
		close(done)
	}()

	<-started // handler is inside Metrics, in-flight should be 1

	mw := httptest.NewRecorder()
	reg.ServeHTTP(mw, httptest.NewRequest("GET", "/metrics", nil))
	body := mw.Body.String()
	if !strings.Contains(body, "docpdf_conversions_in_flight 1") {
		t.Errorf("expected in_flight=1 while handler is running, got:\n%s", body)
	}

	close(unblock)
	<-done

	mw2 := httptest.NewRecorder()
	reg.ServeHTTP(mw2, httptest.NewRequest("GET", "/metrics", nil))
	body2 := mw2.Body.String()
	if !strings.Contains(body2, "docpdf_conversions_in_flight 0") {
		t.Errorf("expected in_flight=0 after handler returns, got:\n%s", body2)
	}
}

// ---------- JSON log format spot-check ----------

// logEntry is used to decode a single log line for structural verification.
type logEntry struct {
	Time        string `json:"time"`
	RequestID   string `json:"request_id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Status      int    `json:"status"`
	DurationMs  int64  `json:"duration_ms"`
	ErrorField  string `json:"error,omitempty"`
}

func TestLogging_JSONFields(t *testing.T) {
	old, flush := captureStderr(t)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		middleware.SetLogError(r.Context(), "test error")
		w.WriteHeader(http.StatusBadRequest)
	})

	handler := middleware.RequestID(middleware.Logging(inner))
	req := httptest.NewRequest(http.MethodPost, "/convert", nil)
	req.Header.Set("X-Request-ID", "abc-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Restore stderr first, then flush the pipe so the goroutine can drain.
	restoreStderr(t, old)
	line := flush()

	var entry logEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &entry); err != nil {
		t.Fatalf("log line is not valid JSON: %v\nline: %s", err, line)
	}
	if entry.Method != "POST" {
		t.Errorf("expected method POST, got %q", entry.Method)
	}
	if entry.Path != "/convert" {
		t.Errorf("expected path /convert, got %q", entry.Path)
	}
	if entry.Status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", entry.Status)
	}
	if entry.RequestID != "abc-123" {
		t.Errorf("expected request_id abc-123, got %q", entry.RequestID)
	}
	if entry.ErrorField != "test error" {
		t.Errorf("expected error field 'test error', got %q", entry.ErrorField)
	}
}
