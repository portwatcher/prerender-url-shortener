.PHONY: test test-coverage test-short bench build clean lint fmt vet install-deps help

# Default target
help:
	@echo "Available targets:"
	@echo "  test          - Run all tests"
	@echo "  test-coverage - Run tests with coverage report"
	@echo "  test-short    - Run tests with short flag"
	@echo "  bench         - Run benchmarks"
	@echo "  build         - Build the application"
	@echo "  clean         - Clean build artifacts"
	@echo "  lint          - Run golangci-lint"
	@echo "  fmt           - Format code"
	@echo "  vet           - Run go vet"
	@echo "  install-deps  - Install development dependencies"

# Test targets
test:
	go test -v ./...

test-coverage:
	go test -v -cover -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-short:
	go test -short ./...

bench:
	go test -bench=. -benchmem ./...

# Build targets
build:
	go build -o bin/server cmd/server/main.go

clean:
	rm -f bin/server coverage.out coverage.html
	go clean

# Code quality targets
lint:
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Run: make install-deps" && exit 1)
	golangci-lint run

fmt:
	go fmt ./...

vet:
	go vet ./...

# Development dependencies
install-deps:
	go mod tidy
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run the application
run:
	go run cmd/server/main.go

# Database setup for testing (if needed)
test-db-setup:
	@echo "Setting up test database..."
	# Add database setup commands here if needed

# Docker targets (if using Docker)
docker-build:
	docker build -t prerender-url-shortener .

docker-run:
	docker run -p 8080:8080 prerender-url-shortener 