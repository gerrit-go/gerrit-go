# syntax=docker/dockerfile:1

# ---- Stage 1: build the React/Vite frontend ----
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# ---- Stage 2: build the Go backend (static binary; SQLite or PostgreSQL) ----
FROM golang:1.25-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/gerrit-go ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate-db ./cmd/migrate-db

# ---- Stage 3: runtime ----
# git is required at runtime: the server shells out to `git http-backend` for
# Smart HTTP and to `git` for submit/rebase/cherry-pick strategies. Alpine's git
# package omits the git-http-backend CGI, which makes Smart HTTP return empty
# responses, so the runtime uses Debian (its git package ships git-http-backend).
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
        git ca-certificates tzdata wget \
 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=backend /out/gerrit-go /app/gerrit-go
COPY --from=backend /out/migrate-db /app/migrate-db
COPY --from=web /src/web/dist /app/dist
RUN mkdir -p /data
VOLUME ["/data"]
EXPOSE 8080 29418
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:8080/config" >/dev/null 2>&1 || exit 1
ENTRYPOINT ["/app/gerrit-go"]
CMD ["-addr", ":8080", "-data", "/data", "-static", "/app/dist"]
