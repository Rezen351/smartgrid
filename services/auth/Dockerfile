# ─── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install dependencies for CGO-free build
RUN apk add --no-cache git

# Copy module files first for better cache reuse
COPY go.mod go.sum ./
RUN for i in 1 2 3 4 5; do go mod download && break || { echo "go mod download failed, retrying..."; sleep 5; }; done

# Copy source
COPY . .

# Ensure module files are complete after source copy
RUN go mod tidy

# Build a statically linked binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o auth-svc .

# ─── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.19

# ca-certificates for TLS (NATS over TLS, external HTTPS calls)
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/auth-svc .

# Run as non-root user
RUN adduser -D -H -s /bin/false appuser
USER appuser

EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["./auth-svc"]
