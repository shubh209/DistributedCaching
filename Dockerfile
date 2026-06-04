# ── Build stage ──────────────────────────────────────────────────────────────
# Compile all 4 binaries from the single Go module.
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Copy dependency files first — Docker caches this layer if go.mod/go.sum
# haven't changed, so we don't re-download deps on every code change.
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build all binaries
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/cachenode ./cmd/cachenode
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/api       ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/proxy     ./cmd/proxy
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/benchmark ./cmd/benchmark

# ── Final stage ───────────────────────────────────────────────────────────────
# Minimal image containing only the compiled binaries + wget for health checks.
FROM alpine:3.19

RUN apk add --no-cache wget

COPY --from=builder /app/cachenode  /app/cachenode
COPY --from=builder /app/api        /app/api
COPY --from=builder /app/proxy      /app/proxy
COPY --from=builder /app/benchmark  /app/benchmark

# Default command — overridden by docker-compose.yml per service
CMD ["/app/api"]
