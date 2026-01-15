# Build stage for frontend
FROM node:18-alpine AS frontend-builder

WORKDIR /app/frontend

# Copy frontend package files
COPY app/package*.json ./

# Install dependencies
RUN npm ci

# Copy frontend source
COPY app/ ./

# Build frontend
RUN npm run build

# Build stage for backend
FROM golang:1.20-alpine AS backend-builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Copy built frontend from previous stage
COPY --from=frontend-builder /app/frontend/dist ./app/dist

# Build arguments
ARG BUILD_DATE
ARG VCS_REF
ARG VERSION

# Build backend with version info
RUN CGO_ENABLED=1 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE} -X main.GitCommit=${VCS_REF}" \
    -o coai .

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata sqlite-libs

WORKDIR /app

# Copy binary from builder
COPY --from=backend-builder /app/coai .

# Copy frontend dist
COPY --from=backend-builder /app/app/dist ./app/dist

# Create data directory
RUN mkdir -p /app/data

# Expose port
EXPOSE 8080

# Set environment variables
ENV GIN_MODE=release
ENV TZ=Asia/Shanghai

# Add labels
LABEL org.opencontainers.image.created="${BUILD_DATE}"
LABEL org.opencontainers.image.revision="${VCS_REF}"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.title="CoAI"
LABEL org.opencontainers.image.description="AI Chat Application"
LABEL org.opencontainers.image.source="https://github.com/yourusername/coai"

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8080/ || exit 1

# Run the application
CMD ["./coai"]
