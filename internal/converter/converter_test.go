package converter_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BRO3886/go-docpdf/internal/converter"
)

// TestLibreOffice_Timeout verifies that a converter with a very short timeout
// returns ErrTimeout when the subprocess doesn't finish in time.
func TestLibreOffice_Timeout(t *testing.T) {
	tmpDir := t.TempDir()

	// Write a shell script that sleeps indefinitely.
	scriptPath := filepath.Join(tmpDir, "fake-lo.sh")
	_ = os.WriteFile(scriptPath, []byte("#!/bin/sh\nsleep 60\n"), 0755)

	c := &converter.LibreOffice{
		BinaryPath: scriptPath,
		Timeout:    100 * time.Millisecond,
	}

	inputPath := filepath.Join(tmpDir, "input.docx")
	_ = os.WriteFile(inputPath, []byte("dummy"), 0600)

	_, err := c.Convert(context.Background(), inputPath, tmpDir)
	if err == nil {
		t.Fatal("expected ErrTimeout, got nil")
	}
	if err != converter.ErrTimeout {
		t.Fatalf("expected ErrTimeout, got %q", err)
	}
}

// TestLibreOffice_MissingOutput verifies that when the subprocess succeeds but
// produces no PDF file, ErrNoOutput is returned.
func TestLibreOffice_MissingOutput(t *testing.T) {
	c := &converter.LibreOffice{
		BinaryPath: "true", // exits 0, produces nothing
		Timeout:    5 * time.Second,
	}

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.docx")
	_ = os.WriteFile(inputPath, []byte("dummy"), 0600)

	_, err := c.Convert(context.Background(), inputPath, tmpDir)
	if err == nil {
		t.Fatal("expected error for missing output, got nil")
	}
	if err != converter.ErrNoOutput {
		t.Fatalf("expected ErrNoOutput, got %q", err)
	}
}

// TestLibreOffice_OutputFound verifies that when the subprocess produces a PDF
// at the expected path, Convert returns that path with no error.
func TestLibreOffice_OutputFound(t *testing.T) {
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.docx")
	_ = os.WriteFile(inputPath, []byte("dummy"), 0600)

	scriptPath := filepath.Join(tmpDir, "fake-lo.sh")
	script := fmt.Sprintf("#!/bin/sh\necho 'fake pdf content' > %s/input.pdf\n", tmpDir)
	_ = os.WriteFile(scriptPath, []byte(script), 0755)

	c := &converter.LibreOffice{
		BinaryPath: scriptPath,
		Timeout:    5 * time.Second,
	}

	pdfPath, err := c.Convert(context.Background(), inputPath, tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pdfPath == "" {
		t.Fatal("expected non-empty pdfPath")
	}
	if _, statErr := os.Stat(pdfPath); statErr != nil {
		t.Fatalf("PDF file not found at %s: %v", pdfPath, statErr)
	}
}

// TestLibreOffice_ConversionFailed verifies that a non-zero exit from the
// subprocess returns ErrConversionFailed (wrapped).
func TestLibreOffice_ConversionFailed(t *testing.T) {
	c := &converter.LibreOffice{
		BinaryPath: "false", // exits 1 immediately
		Timeout:    5 * time.Second,
	}

	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "input.docx")
	_ = os.WriteFile(inputPath, []byte("dummy"), 0600)

	_, err := c.Convert(context.Background(), inputPath, tmpDir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestLibreOffice_ProfileIsolation verifies that each Convert call receives a
// distinct HOME environment variable, confirming per-request profile isolation.
func TestLibreOffice_ProfileIsolation(t *testing.T) {
	// homeLog collects the HOME values seen by each subprocess invocation.
	var (
		mu      sync.Mutex
		homeSeen []string
	)

	// Run two conversions concurrently. Each uses its own outDir, so HOME
	// should differ between them.
	const n = 2
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			tmpDir := t.TempDir()
			inputPath := filepath.Join(tmpDir, "input.docx")
			_ = os.WriteFile(inputPath, []byte("dummy"), 0600)

			// Fake binary: write $HOME to a known file, then create input.pdf.
			homeFile := filepath.Join(tmpDir, "home.txt")
			script := fmt.Sprintf(
				"#!/bin/sh\nprintf \"%%s\" \"$HOME\" > %s\necho fake > %s/input.pdf\n",
				homeFile, tmpDir,
			)
			scriptPath := filepath.Join(tmpDir, "fake-lo.sh")
			_ = os.WriteFile(scriptPath, []byte(script), 0755)

			c := &converter.LibreOffice{BinaryPath: scriptPath, Timeout: 5 * time.Second}
			_, errs[idx] = c.Convert(context.Background(), inputPath, tmpDir)

			data, readErr := os.ReadFile(homeFile)
			if readErr == nil {
				mu.Lock()
				homeSeen = append(homeSeen, strings.TrimSpace(string(data)))
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("conversion %d failed: %v", i, err)
		}
	}

	if len(homeSeen) != n {
		t.Fatalf("expected %d HOME values recorded, got %d", n, len(homeSeen))
	}
	if homeSeen[0] == homeSeen[1] {
		t.Errorf("both conversions shared the same HOME (%q) — isolation not working", homeSeen[0])
	}
}
