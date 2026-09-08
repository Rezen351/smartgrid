# ─── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

# Copy module files first for better cache reuse
COPY go.mod ./
RUN for i in 1 2 3 4 5; do go mod download && break || { echo "go mod download failed, retrying..."; sleep 5; }; done

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o export-svc .

# ─── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -H -s /bin/false appuser

WORKDIR /app
COPY --from=builder /app/export-svc .

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["./export-svc"]
