.PHONY: proto-gen build test bench up down

# Generate Go bindings from all .proto files
proto-gen:
	mkdir -p internal/proto/cache/v1 internal/proto/raft/v1 internal/proto/api/v1
	protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/cache/v1/cache.proto \
		proto/raft/v1/raft.proto \
		proto/api/v1/api.proto

# Build all service binaries
build:
	go build -o bin/cachenode ./cmd/cachenode
	go build -o bin/api       ./cmd/api
	go build -o bin/proxy     ./cmd/proxy
	go build -o bin/benchmark ./cmd/benchmark

# Run all tests
test:
	go test ./...

# Run tests with race detector
race:
	go test -race ./...

# Run benchmarks
bench:
	go run ./cmd/benchmark

# Start all services via Docker Compose
up:
	docker compose up --build

# Stop all services
down:
	docker compose down
