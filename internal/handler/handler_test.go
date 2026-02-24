package handler_test

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BRO3886/go-docpdf/internal/converter"
	"github.com/BRO3886/go-docpdf/internal/handler"
)

// mockConverter is a test double for converter.Converter.
type mockConverter struct {
	mu      sync.Mutex
	calls   []string
	callsFn func(ctx context.Context, inputPath, outDir string) (string, error)
}

func (m *mockConverter) Convert(ctx context.Context, inputPath, outDir string) (string, error) {
	m.mu.Lock()
	m.calls = append(m.calls, inputPath)
	m.mu.Unlock()
	return m.callsFn(ctx, inputPath, outDir)
}

// docxMagic mirrors the magic bytes checked by the handler.
var docxMagic = []byte{0x50, 0x4B, 0x03, 0x04}

// validDocxBody returns a byte slice of size bytes starting with the PK magic header.
func validDocxBody(size int) []byte {
	data := make([]byte, size)
	copy(data, docxMagic)
	return data
}

// buildRequest constructs a multipart POST request with the given bytes as the "file" field.
func buildRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "test.docx")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = fw.Write(body)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/convert", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// happyMock returns a mockConverter that writes a small PDF to outDir and returns its path.
func happyMock() *mockConverter {
	return &mockConverter{
		callsFn: func(_ context.Context, _ string, outDir string) (string, error) {
			pdfPath := filepath.Join(outDir, "input.pdf")
			_ = os.WriteFile(pdfPath, []byte("%PDF-1.4 fake"), 0600)
			return pdfPath, nil
		},
	}
}

func TestConvert_HappyPath(t *testing.T) {
	h := handler.NewConvert(happyMock())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, buildRequest(t, validDocxBody(1024)))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("expected application/pdf, got %s", ct)
	}
	if rr.Body.Len() == 0 {
		t.Fatal("expected non-empty PDF body")
	}
}

func TestConvert_FileTooLarge(t *testing.T) {
	h := handler.NewConvert(happyMock())
	rr := httptest.NewRecorder()
	// 11 MB — exceeds the 10 MB limit.
	h.ServeHTTP(rr, buildRequest(t, validDocxBody(11<<20)))

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", rr.Code, rr.Body.String())
	}
	assertJSONError(t, rr.Body.String())
}

func TestConvert_WrongFileType(t *testing.T) {
	h := handler.NewConvert(happyMock())
	rr := httptest.NewRecorder()
	// Plain text — no PK magic header.
	h.ServeHTTP(rr, buildRequest(t, []byte("Hello, plain text")))

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", rr.Code, rr.Body.String())
	}
	assertJSONError(t, rr.Body.String())
}

func TestConvert_TimeoutSimulation(t *testing.T) {
	mc := &mockConverter{
		callsFn: func(_ context.Context, _, _ string) (string, error) {
			return "", converter.ErrTimeout
		},
	}
	h := handler.NewConvert(mc)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, buildRequest(t, validDocxBody(1024)))

	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d: %s", rr.Code, rr.Body.String())
	}
	assertJSONError(t, rr.Body.String())
}

func TestConvert_ConcurrentRequestsAllSucceed(t *testing.T) {
	// With per-request profile isolation, concurrent calls are safe.
	// Verify that N simultaneous requests all complete successfully.
	mc := &mockConverter{
		callsFn: func(_ context.Context, _ string, outDir string) (string, error) {
			time.Sleep(10 * time.Millisecond) // simulate work
			pdfPath := filepath.Join(outDir, "input.pdf")
			_ = os.WriteFile(pdfPath, []byte("%PDF fake"), 0600)
			return pdfPath, nil
		},
	}
	h := handler.NewConvert(mc)

	const n = 5
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, buildRequest(t, validDocxBody(512)))
			codes[idx] = rr.Code
		}(i)
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, code)
		}
	}
}

func TestConvert_TempFilesCleanedUpAfterFailure(t *testing.T) {
	var capturedDir string
	mc := &mockConverter{
		callsFn: func(_ context.Context, _ string, outDir string) (string, error) {
			capturedDir = outDir
			return "", fmt.Errorf("conversion failed")
		},
	}
	h := handler.NewConvert(mc)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, buildRequest(t, validDocxBody(512)))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
	if capturedDir == "" {
		t.Fatal("converter was not called — cannot verify cleanup")
	}
	if _, err := os.Stat(capturedDir); !os.IsNotExist(err) {
		t.Fatalf("temp dir %s still exists after handler returned", capturedDir)
	}
}

func TestConvert_MissingFileField(t *testing.T) {
	h := handler.NewConvert(happyMock())

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("other", "value")
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/convert", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConvert_MethodNotAllowed(t *testing.T) {
	h := handler.NewConvert(happyMock())
	req := httptest.NewRequest(http.MethodGet, "/convert", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	handler.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"ok"`) {
		t.Fatalf("expected status ok, got: %s", rr.Body.String())
	}
}

func TestConvert_ErrorBodyDoesNotLeakPaths(t *testing.T) {
	mc := &mockConverter{
		callsFn: func(_ context.Context, _ string, _ string) (string, error) {
			return "", fmt.Errorf("conversion failed")
		},
	}
	h := handler.NewConvert(mc)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, buildRequest(t, validDocxBody(512)))

	body := rr.Body.String()
	if strings.Contains(body, "/tmp") || strings.Contains(body, "docpdf-") {
		t.Fatalf("error response leaks internal path: %s", body)
	}
	assertJSONError(t, body)
}

// assertJSONError fails the test if body doesn't contain a JSON "error" key.
func assertJSONError(t *testing.T, body string) {
	t.Helper()
	if !strings.Contains(body, `"error"`) {
		t.Fatalf("expected JSON error body, got: %s", body)
	}
}
