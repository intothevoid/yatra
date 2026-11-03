# Build the browser app before embedding it in the Go binary.
FROM node:24-bookworm-slim AS frontend

WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Build stage
FROM golang:1.26-bookworm AS builder

WORKDIR /app

# Install dependencies
RUN apt-get update && apt-get install -y \
    exiftool \
    libvips-tools \
    && rm -rf /var/lib/apt/lists/*

# Copy go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .
COPY --from=frontend /app/internal/server/static ./internal/server/static

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -o /travel-diary ./cmd/travel-diary

# Runtime stage
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && apt-get install -y \
    exiftool \
    libvips-tools \
    ca-certificates \
    wget \
    && rm -rf /var/lib/apt/lists/*

# Create app user
RUN useradd -m -s /bin/sh appuser

WORKDIR /app

# Copy binary
COPY --from=builder /travel-diary .

# Create data directory
RUN mkdir -p /data /imports && chown -R appuser:appuser /data /imports

USER appuser

VOLUME ["/data", "/imports"]

EXPOSE 8080

ENV DATA_DIR=/data
ENV IMPORT_ROOTS=/imports
ENV PORT=8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 -O /dev/null http://localhost:8080/health/live || exit 1

CMD ["./travel-diary"]
