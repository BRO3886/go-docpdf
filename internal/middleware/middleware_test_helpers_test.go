package middleware_test

import (
	"bytes"
	"os"
	"testing"
)

// captureStderr replaces os.Stderr with a pipe. It returns:
//   - old: the original os.Stderr (pass to restoreStderr when done)
//   - flush: call this AFTER restoreStderr to block until all data has been
//     drained from the pipe into buf and return buf's contents.
func captureStderr(t *testing.T) (old *os.File, flush func() string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureStderr: os.Pipe: %v", err)
	}
	old = os.Stderr
	os.Stderr = w

	buf := &bytes.Buffer{}
	done := make(chan struct{})
	go func() {
		buf.ReadFrom(r)
		close(done)
	}()

	flush = func() string {
		// Caller must have already restored os.Stderr (closed w externally).
		// Close the write-end here to signal EOF to the reader goroutine.
		w.Close()
		<-done
		r.Close()
		return buf.String()
	}
	return old, flush
}

// restoreStderr sets os.Stderr back to the saved *os.File.
func restoreStderr(t *testing.T, old *os.File) {
	t.Helper()
	os.Stderr = old
}
