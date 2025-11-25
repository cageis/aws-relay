.PHONY: build run clean docker-build docker-up docker-down test fmt lint

BINARY_NAME=aws-relay
DOCKER_IMAGE=aws-relay

# Build the binary
build:
	go build -o $(BINARY_NAME) .

# Run locally
run: build
	AWS_UPSTREAM_URL=http://localhost:4566 ./$(BINARY_NAME)

# Run with custom upstream
run-upstream: build
	./$(BINARY_NAME)

# Clean build artifacts
clean:
	rm -f $(BINARY_NAME)
	go clean

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	golangci-lint run

# Run tests
test:
	go test -v ./...

# Build Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE) .

# Start with docker compose
docker-up:
	docker compose up -d

# Stop docker compose
docker-down:
	docker compose down

# Rebuild and restart
docker-restart: docker-down docker-build docker-up

# View logs
docker-logs:
	docker compose logs -f

# Quick dev cycle - format, build, run
dev: fmt build run

