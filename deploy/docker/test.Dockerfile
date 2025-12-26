# Dockerfile for running Go tests on Linux
# This enables testing Linux-specific code like namespace detection.

FROM golang:1.24-alpine

# Install dependencies needed for tests
RUN apk add --no-cache \
    git \
    make \
    util-linux \
    bash

# Set working directory
WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Set environment variable for tests
ENV MARIONETTE_RUNNER_TOKEN=test_token

# Run tests with coverage
CMD ["go", "test", "-race", "-cover", "./pkg/agent/..."]
