// Package converter provides the Converter interface and its LibreOffice implementation.
package converter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Sentinel errors returned by Convert.
var (
	// ErrTimeout is returned when LibreOffice exceeds the configured timeout.
	ErrTimeout = errors.New("conversion timed out")

	// ErrNoOutput is returned when LibreOffice exits successfully but produces no PDF.
	ErrNoOutput = errors.New("conversion produced no output")

	// ErrConversionFailed is returned when LibreOffice exits with a non-zero status.
	ErrConversionFailed = errors.New("conversion failed")
)

// Converter converts a .docx file to PDF.
type Converter interface {
	// Convert converts the file at inputPath, writing the PDF to outDir.
	// Returns the absolute path of the generated PDF on success.
	Convert(ctx context.Context, inputPath string, outDir string) (string, error)
}

// LibreOffice implements Converter by shelling out to LibreOffice.
type LibreOffice struct {
	BinaryPath string
	Timeout    time.Duration
}

// New returns a LibreOffice converter configured from the environment.
// LIBREOFFICE_PATH overrides the default binary name "libreoffice".
func New() *LibreOffice {
	bin := os.Getenv("LIBREOFFICE_PATH")
	if bin == "" {
		bin = "libreoffice"
	}
	return &LibreOffice{
		BinaryPath: bin,
		Timeout:    60 * time.Second,
	}
}

// Convert implements Converter.
func (lo *LibreOffice) Convert(ctx context.Context, inputPath string, outDir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, lo.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx,
		lo.BinaryPath,
		"--headless",
		"--convert-to", "pdf",
		"--outdir", outDir,
		inputPath,
	)
	// Give each conversion its own HOME so LibreOffice creates a fresh, isolated
	// user profile inside outDir. This prevents lock-file conflicts and state
	// bleed between concurrent requests. outDir is already cleaned up by the
	// caller, so the profile is removed for free.
	cmd.Env = append(os.Environ(),
		"HOME="+outDir,
		"UserInstallation=file://"+outDir+"/lo-profile",
	)

	if _, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", ErrTimeout
		}
		return "", fmt.Errorf("%w: %w", ErrConversionFailed, err)
	}

	// LibreOffice names the output after the input file with a .pdf extension.
	base := filepath.Base(inputPath)
	pdfName := strings.TrimSuffix(base, filepath.Ext(base)) + ".pdf"
	pdfPath := filepath.Join(outDir, pdfName)

	info, err := os.Stat(pdfPath)
	if err != nil || info.Size() == 0 {
		return "", ErrNoOutput
	}

	return pdfPath, nil
}
