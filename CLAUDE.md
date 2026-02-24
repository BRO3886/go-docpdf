# go-docpdf

## What This Is
Lightweight Go HTTP service: POST a `.docx`, get back a PDF. Shells out to LibreOffice. Designed to replace Gotenberg for a single use-case.

## Status
**Complete (session 001, 2026-02-24)**

- POST /convert — multipart upload → PDF response
- GET /health — {"status":"ok"}
- 14 tests, all passing

## Package Layout
```
cmd/server/main.go               — entry point, mux, PORT env
internal/converter/converter.go  — Converter interface + LibreOffice impl
internal/converter/converter_test.go  — 4 tests
internal/handler/handler.go      — Convert + Health handlers
internal/handler/handler_test.go — 10 tests
Dockerfile                       — golang:1.24-alpine builder + alpine:3.21 runtime
```

## Build & Run
```sh
go build ./cmd/server
go test ./...

# Local (Mac)
LIBREOFFICE_PATH=/Applications/LibreOffice.app/Contents/MacOS/soffice go run ./cmd/server

# Docker
docker build -t go-docpdf .
docker run -p 8080:8080 go-docpdf
```

## Architecture Non-Negotiables
- `converter.Converter` interface — never call `LibreOffice` directly from handler tests; always inject mock
- `sync.Mutex` on `handler.Convert` struct — serializes all LibreOffice calls
- Errors: always `{"error": "<safe message>"}` JSON, never expose paths or system details
- Sentinel errors in `converter` package: `ErrTimeout`, `ErrNoOutput`, `ErrConversionFailed`
- No external dependencies — stdlib only
