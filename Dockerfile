# Stage 1: Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git gcc musl-dev

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
# CGO_ENABLED=0 untuk static binary, GOOS=linux untuk Linux target
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o main *.go

# Stage 2: Runtime stage
FROM alpine:latest

# Install CA certificates untuk HTTPS connections
RUN apk --no-cache add ca-certificates curl

# Create non-root user untuk security
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

# Set working directory
WORKDIR /app

# Copy binary dari builder stage
COPY --from=builder /build/main .

# Copy web files
COPY --from=builder /build/web ./web

# Change ownership
RUN chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Run the application
CMD ["./main"]
