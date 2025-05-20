# Stage 1: Build the Go application
FROM golang:1.24-bookworm AS builder

WORKDIR /app

# Install git for private modules if any, and other build tools if necessary.
# ca-certificates are needed for HTTPS calls during build.
# build-essential provides C compilers if any cgo dependency needs them (even with CGO_ENABLED=0, some edge cases exist)
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Copy go.mod and go.sum first to leverage Docker cache for dependencies
COPY go.mod go.sum ./
RUN go mod download
RUN go mod verify

# Copy the rest of the application source code
COPY . .

# Build the application
# Using CGO_ENABLED=0 to build a statically linked binary, which is good for minimal images.
# -ldflags="-w -s" reduces the binary size by omitting debug information.
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o /server cmd/server/main.go

# Stage 2: Create the final lightweight image
FROM debian:bookworm-slim

# Install ca-certificates for HTTPS communication by the application,
# dumb-init as a simple process supervisor,
# and necessary dependencies for headless Chromium (downloaded by Rod).
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    dumb-init \
    # Dependencies for headless Chrome / Rod:
    libasound2 \
    libatk1.0-0 \
    libatk-bridge2.0-0 \
    libcups2 \
    libdbus-1-3 \
    libdrm2 \
    libgbm1 \
    libgtk-3-0 \
    libnspr4 \
    libnss3 \
    libpango-1.0-0 \
    libx11-6 \
    libx11-xcb1 \
    libxcb1 \
    libxcomposite1 \
    libxdamage1 \
    libxext6 \
    libxfixes3 \
    libxrandr2 \
    libxrender1 \
    libxtst6 \
    fonts-liberation \
    xdg-utils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /server /app/server

# Copy .env.example if you have one, to show available env vars
# COPY .env.example .env.example

# Set environment variables for production
ENV GIN_MODE=release
# SERVER_PORT is typically set at runtime, but you can default it here if desired
# ENV SERVER_PORT=8080

# Expose the port the app runs on (default 8080, or as set by SERVER_PORT)
EXPOSE 8080

# Command to run the application using dumb-init
# dumb-init helps manage signals and zombie processes correctly
ENTRYPOINT ["/usr/bin/dumb-init", "--"]
CMD ["/app/server"]
