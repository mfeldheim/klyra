# syntax=docker/dockerfile:1

# ── Stage 1: build Go binary ─────────────────────────────────────────────────
# internal/server/dist/ must be present before running docker build.
# Use 'make build' for local builds, or let CI place it via build-ui artifact.
FROM golang:alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o klyra .

# ── Stage 2: minimal runtime image ───────────────────────────────────────────
FROM alpine:latest
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=go-builder /app/klyra .
EXPOSE 8080
ENTRYPOINT ["./klyra"]
