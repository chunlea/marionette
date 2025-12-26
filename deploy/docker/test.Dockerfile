# Dockerfile for running Go tests on Linux
# This enables testing Linux-specific code like namespace detection.

# Use golang:latest to stay current with go.mod version
# go.mod specifies go 1.25.5
FROM golang:latest

# Install dependencies needed for tests
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    make \
    util-linux \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Set environment variable for tests
ENV MARIONETTE_RUNNER_TOKEN=test_token
ENV CGO_ENABLED=1

# Default: run all agent tests with coverage
CMD ["go", "test", "-race", "-cover", "./pkg/agent/..."]
