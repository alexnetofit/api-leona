# ============================================
# Build stage
# ============================================
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache gcc musl-dev sqlite-dev git

WORKDIR /build

# Copy go.mod and go.sum first for dependency caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code (this layer changes often, deps layer above is cached)
COPY . .

# Build the binary
RUN CGO_ENABLED=1 GOOS=linux go build -o leona-api ./cmd/server

# ============================================
# Runtime stage
# ============================================
FROM alpine:3.20

RUN apk add --no-cache ca-certificates sqlite-libs tzdata wget curl

ENV TZ=America/Sao_Paulo

WORKDIR /app
COPY --from=builder /build/leona-api .
COPY --from=builder /build/migrations ./migrations
COPY --from=builder /build/config.example.json ./config.json
COPY --from=builder /build/web ./web

# Storage para SQLite sessions (montar como volume persistente no EasyPanel)
RUN mkdir -p /app/storage

EXPOSE 8080

# Health check para EasyPanel monitorar
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./leona-api"]
