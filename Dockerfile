FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 GOOS=linux go build -o docpdf ./cmd/server

FROM alpine:3.21

# Install LibreOffice and required fonts
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

USER nobody
CMD ["./docpdf"]
