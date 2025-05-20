# Stage 1: Build the Go application
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Install git for private modules if any, and other build tools if necessary.
# Alpine images are minimal, so ca-certificates might be needed for HTTPS calls during build.
RUN apk add --no-cache git ca-certificates

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
FROM alpine:latest

# Install ca-certificates for HTTPS communication by the application
# and any other runtime dependencies. For Rod to download its browser,
# it might need additional dependencies. Common ones are:
# fontconfig, freetype, ttf-freefont, dumb-init
# Add them if Rod fails to download/run browser.
# dumb-init is a simple process supervisor.
RUN apk add --no-cache ca-certificates dumb-init

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
