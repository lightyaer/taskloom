FROM golang:1.22-alpine AS builder

# Install necessary build tools and dependencies
RUN apk update && apk add --no-cache \
    gcc \
    g++ \
    make \
    musl-dev \
    git

# Set working directory
WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o taskloom ./cmd/server

# Use a smaller image for the final stage
FROM alpine:latest

# Install required runtime dependencies
RUN apk update && apk add --no-cache \
    ca-certificates \
    tzdata \
    sqlite

# Create a non-root user and group
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Create directory for the database
RUN mkdir -p /data && chown -R appuser:appgroup /data

# Set working directory
WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/taskloom .

# Copy any static files if needed
# COPY --from=builder /app/static ./static

# Switch to non-root user
USER appuser

# Set environment variables with defaults that can be overridden
ENV PORT=8080
ENV TURSO_URL=file:/data/taskloom.db
ENV TURSO_TOKEN=""
ENV JWT_SECRET=""
ENV GIN_MODE=release

# Expose the application port
EXPOSE 8080

# Set volume for persistent database
VOLUME ["/data"]

# Command to run the application
CMD ["./taskloom"] 