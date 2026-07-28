# Build from micro-service/ so the sibling go-packages module is available
# (notification-service go.mod uses: replace => ../go-packages).
#
#   docker build -f notification-service/Dockerfile --target api -t notification-api ..
#
# Compose sets context to the parent directory.

# ---------- Base ----------
FROM golang:1.24.2-alpine AS base
RUN apk add --no-cache git build-base ca-certificates
WORKDIR /src

# ---------- Deps ----------
FROM base AS deps
COPY go-packages/go.mod go-packages/go.sum* ./go-packages/
COPY notification-service/go.mod notification-service/go.sum* ./notification-service/
WORKDIR /src/notification-service
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# ---------- Builder ----------
FROM base AS builder
COPY --from=deps /go/pkg/mod /go/pkg/mod
COPY go-packages /src/go-packages
COPY notification-service /src/notification-service
WORKDIR /src/notification-service
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-w -s" -o /out/notification-api ./cmd/api && \
    go build -ldflags="-w -s" -o /out/notification-worker ./cmd/worker && \
    go build -ldflags="-w -s" -o /out/notification-outbox ./cmd/outbox

# ---------- API ----------
FROM alpine:3.19 AS api
RUN apk add --no-cache ca-certificates tzdata wget
WORKDIR /app
COPY --from=builder /out/notification-api /app/notification-api
COPY --from=builder /src/notification-service/config.yml /app/config.yml
COPY --from=builder /src/notification-service/migrations /app/migrations
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health-check || exit 1
ENTRYPOINT ["/app/notification-api"]

# ---------- Worker ----------
FROM alpine:3.19 AS worker
RUN apk add --no-cache ca-certificates tzdata wget
WORKDIR /app
COPY --from=builder /out/notification-worker /app/notification-worker
COPY --from=builder /src/notification-service/config.yml /app/config.yml
EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8081/health-check || exit 1
ENTRYPOINT ["/app/notification-worker"]

# ---------- Outbox publisher ----------
FROM alpine:3.19 AS outbox
RUN apk add --no-cache ca-certificates tzdata wget
WORKDIR /app
COPY --from=builder /out/notification-outbox /app/notification-outbox
COPY --from=builder /src/notification-service/config.yml /app/config.yml
EXPOSE 8082
HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8082/health-check || exit 1
ENTRYPOINT ["/app/notification-outbox"]
