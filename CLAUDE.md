# go-docpdf

## What This Is

Lightweight Go HTTP service: POST a `.docx`, get back a PDF. Shells out to LibreOffice. Designed to replace Gotenberg for a single use-case.

## Status

**Complete (session 004, 2026-02-25)** — 19 tests passing, observability added, Docker image pushed to GHCR, repo public on GitHub

- POST /convert — multipart upload → PDF response
- GET /health — {"status":"ok"}
- GET /metrics — Prometheus text format (counters + histogram)
- 19 tests, all passing (including `-race`)
- Docker image pushed: `ghcr.io/bro3886/go-docpdf:latest` + `ghcr.io/bro3886/go-docpdf:bb80ed7` (917MB)
- GitHub: https://github.com/BRO3886/go-docpdf

## Package Layout

```
cmd/server/main.go                    — entry point, mux, PORT env, middleware chain
internal/converter/converter.go       — Converter interface + LibreOffice impl
internal/converter/converter_test.go  — 5 tests
internal/handler/handler.go           — Convert + Health handlers (SetOutcome/SetLogError at each return)
internal/handler/handler_test.go      — 10 tests
internal/metrics/metrics.go           — Registry: atomic counters, histogram, Prometheus text renderer
internal/metrics/metrics_test.go      — 5 tests
internal/middleware/middleware.go     — RequestID, Logging, Metrics middleware + context helpers
internal/middleware/middleware_test.go — 9 tests
Dockerfile                            — golang:1.24.0-alpine builder + alpine:3.21 runtime
.dockerignore
README.md
LICENSE                               — MIT
```

## Build & Run

```sh
go build ./cmd/server
go test ./... -race

# Local (Mac)
LIBREOFFICE_PATH=/Applications/LibreOffice.app/Contents/MacOS/soffice go run ./cmd/server

# Docker
docker build -t ghcr.io/bro3886/go-docpdf:latest .
docker run -p 8080:8080 ghcr.io/bro3886/go-docpdf:latest
```

## Architecture Non-Negotiables

- `converter.Converter` interface — never call `LibreOffice` directly from handler tests; always inject mock
- **No mutex** — LibreOffice concurrency is handled by per-request profile isolation (`HOME=outDir`), NOT a mutex
- Per-request `HOME` + `UserInstallation` env vars isolate each LO subprocess; profile cleanup is free via `defer os.RemoveAll(tmpDir)`
- Errors: always `{"error": "<safe message>"}` JSON, never expose paths or system details
- Sentinel errors in `converter` package: `ErrTimeout`, `ErrNoOutput`, `ErrConversionFailed`
- No external dependencies — stdlib only
- Docker: `USER 65534:65534` (numeric UID, not `nobody` string — more portable on Alpine)
- Middleware context helpers (`SetOutcome`, `SetLogError`) are nil-safe — no-op when no state on context; preserves all existing tests unchanged
- Histogram buckets stored cumulatively in `atomic.Int64` array; rendered directly (no re-accumulation in ServeHTTP)
- `Metrics` middleware wraps only `/convert` — health and metrics scrapes must not pollute counters
- JSON logs go to `os.Stderr`; startup log also JSON via `json.Marshal` + `fmt.Fprintf`
