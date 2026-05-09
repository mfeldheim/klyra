# syntax=docker/dockerfile:1

# ── Stage 1: build React UI ──────────────────────────────────────────────────
FROM node:alpine AS ui-builder
WORKDIR /ui
COPY ui/package*.json ./
RUN npm ci
COPY ui/ .
RUN npm run build

# ── Stage 2: build Go binary ─────────────────────────────────────────────────
FROM golang:alpine AS go-builder
WORKDIR /app
# Copy go module files and download deps first (cache layer)
COPY go.mod go.sum ./
RUN go mod download
# Copy source
COPY . .
# Copy built UI into the server package for go:embed
COPY --from=ui-builder /ui/dist ./internal/server/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o klyra .

# ── Stage 3: minimal runtime image ───────────────────────────────────────────
FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=go-builder /app/klyra .
EXPOSE 8080
ENTRYPOINT ["./klyra"]
