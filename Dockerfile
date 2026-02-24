FROM golang:1.24.0-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o docpdf ./cmd/server

FROM alpine:3.21

LABEL org.opencontainers.image.source="https://github.com/BRO3886/go-docpdf" \
      org.opencontainers.image.description="Lightweight docx-to-PDF conversion service backed by LibreOffice" \
      org.opencontainers.image.licenses="MIT"

# Install LibreOffice and fonts required for document rendering.
# libreoffice-writer pulls in the core engine; font packages ensure glyphs
# render correctly for common document fonts.
RUN apk add --no-cache \
    libreoffice \
    libreoffice-writer \
    font-dejavu \
    ttf-freefont \
    && rm -rf /var/cache/apk/*

WORKDIR /app
COPY --from=builder /app/docpdf .

ENV LIBREOFFICE_PATH=/usr/bin/libreoffice
EXPOSE 8080

# Run as non-root. Alpine's nobody is UID 65534.
USER 65534:65534

CMD ["./docpdf"]
