# ──────────────────────────────────────────
# Stage 1: Build
# ──────────────────────────────────────────
FROM golang:1.22-alpine AS builder

# pg_query_go использует CGO — нужен gcc
RUN apk add --no-cache gcc musl-dev git

WORKDIR /app

# Сначала зависимости (кэшируем слой)
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux \
    go build \
    -ldflags="-w -s" \
    -o /queryguard \
    ./cmd/queryguard/...

# ──────────────────────────────────────────
# Stage 2: Runtime (минимальный образ)
# ──────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S queryguard && \
    adduser -S queryguard -G queryguard

COPY --from=builder /queryguard /usr/local/bin/queryguard

# Configs are mounted at runtime via volumes/ConfigMaps — never baked into the image.
# This prevents credentials from being committed to the image layers.
RUN mkdir -p /etc/queryguard

USER queryguard

EXPOSE 5433 8080 9090

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s \
    CMD wget -qO- http://localhost:9090/health || exit 1

ENTRYPOINT ["queryguard"]
CMD ["-config", "/etc/queryguard/config.yaml"]
