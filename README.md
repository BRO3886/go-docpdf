# go-docpdf

Lightweight HTTP service that converts `.docx` files to PDF by shelling out to LibreOffice. Zero external Go dependencies — stdlib only.

**Why this exists:** Many applications accept `.docx` uploads, but LLMs like Gemini work best (or exclusively) with PDFs. go-docpdf sits in the middle — POST a `.docx`, get back a PDF ready to pass to your AI pipeline.

## API

### `POST /convert`

Accepts a `multipart/form-data` request with a `file` field containing a `.docx` file. Returns `application/pdf` on success.

```sh
curl -X POST http://localhost:8080/convert \
  -F "file=@document.docx" \
  -o output.pdf
```

**Limits & validation**

| Condition | Status |
|-----------|--------|
| File > 10 MB | `413 Request Entity Too Large` |
| File doesn't start with PK magic bytes | `415 Unsupported Media Type` |
| Missing `file` field | `400 Bad Request` |
| LibreOffice times out (60s) | `504 Gateway Timeout` |
| Conversion produces no output | `500 Internal Server Error` |

All errors return JSON: `{"error": "<message>"}`. Internal paths are never exposed.

**Request tracing:** pass an `X-Request-ID` header and it will be echoed on the response and included in every log line. If omitted, one is generated automatically.

### `GET /health`

```sh
curl http://localhost:8080/health
# {"status":"ok"}
```

### `GET /metrics`

Prometheus text format exposition. Exposes conversion counters, in-flight gauge, and a duration histogram.

```sh
curl http://localhost:8080/metrics
```

Metrics exposed:

| Metric | Type | Description |
|--------|------|-------------|
| `docpdf_conversions_total{outcome="success\|timeout\|failed"}` | counter | Conversion outcomes |
| `docpdf_conversions_in_flight` | gauge | Concurrent conversions in progress |
| `docpdf_conversion_duration_ms` | histogram | Duration in ms (buckets: 100–30000) |

## Running

### Docker (recommended)

```sh
docker run -p 8080:8080 ghcr.io/bro3886/go-docpdf:latest
```

### Local (requires LibreOffice)

```sh
# macOS
LIBREOFFICE_PATH=/Applications/LibreOffice.app/Contents/MacOS/soffice go run ./cmd/server

# Linux (libreoffice on PATH)
go run ./cmd/server
```

## Configuration

| Env var | Default | Description |
|---------|---------|-------------|
| `LIBREOFFICE_PATH` | `libreoffice` | Path to the LibreOffice binary |
| `PORT` | `8080` | Port to listen on |

## Design notes

- Each conversion runs in an isolated LibreOffice user profile (`HOME` set to a per-request temp directory). This prevents lock-file conflicts and state bleed between concurrent requests — the same approach used by Gotenberg.
- Temp directories are always cleaned up via `defer`, even on panic.
- Magic-byte validation (`PK\x03\x04`) rejects non-OOXML files regardless of extension.
- No global state except the per-request temp dirs.

## Project structure

```
cmd/server/          — entry point
internal/converter/  — Converter interface + LibreOffice implementation
internal/handler/    — HTTP handlers
internal/metrics/    — Prometheus-compatible registry (atomic counters + histogram)
internal/middleware/ — RequestID, Logging, and Metrics middleware
```

## Tests

```sh
go test ./... -race
```

## License

MIT
