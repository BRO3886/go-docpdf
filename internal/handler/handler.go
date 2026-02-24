// Package handler provides the HTTP handlers for the docpdf service.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/BRO3886/go-docpdf/internal/converter"
)

const maxFileSize = 10 << 20 // 10 MB

// docxMagic is the PK ZIP header that all OOXML (.docx) files start with.
var docxMagic = [4]byte{0x50, 0x4B, 0x03, 0x04}

// Convert handles POST /convert requests.
// It validates the uploaded file, shells out to LibreOffice via the Converter,
// and streams back the resulting PDF.
type Convert struct {
	conv converter.Converter
}

// NewConvert returns a Convert handler backed by conv.
func NewConvert(conv converter.Converter) *Convert {
	return &Convert{conv: conv}
}

// ServeHTTP implements http.Handler.
func (h *Convert) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Cap the request body before parsing so oversized uploads fail fast.
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+4096)

	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer f.Close()

	// Read up to maxFileSize+1 bytes to detect oversized uploads.
	lr := &io.LimitedReader{R: f, N: maxFileSize + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read file")
		return
	}
	if int64(len(data)) > maxFileSize {
		writeError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	if !hasDocxMagic(data) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported file type")
		return
	}

	tmpDir, err := os.MkdirTemp("", "docpdf-*")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer os.RemoveAll(tmpDir)

	inputPath := fmt.Sprintf("%s/input.docx", tmpDir)
	if err := os.WriteFile(inputPath, data, 0600); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	pdfPath, convErr := h.conv.Convert(context.Background(), inputPath, tmpDir)

	if convErr != nil {
		switch {
		case errors.Is(convErr, converter.ErrTimeout):
			writeError(w, http.StatusGatewayTimeout, "conversion timed out")
		default:
			writeError(w, http.StatusInternalServerError, "conversion failed")
		}
		return
	}

	pdfData, err := os.ReadFile(pdfPath)
	if err != nil || len(pdfData) == 0 {
		writeError(w, http.StatusInternalServerError, "conversion produced no output")
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(pdfData)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfData)
}

// Health handles GET /health requests.
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// hasDocxMagic returns true when data begins with the PK ZIP magic bytes.
func hasDocxMagic(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	return [4]byte(data[:4]) == docxMagic
}

// writeError writes {"error": msg} as JSON with the given HTTP status.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
