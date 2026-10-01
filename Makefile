.PHONY: fmt test test-race vet lint integration provider ci build

fmt:
	@test -z "$$(gofmt -l .)" || (echo "needs gofmt:"; gofmt -l .; exit 1)

build:
	go build -ldflags "-X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/docmcp ./cmd/docmcp

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run
	go vet ./...

integration:
	go test -tags=integration ./...

provider:
	DOCMCP_TEST_LOCAL=1 go test ./internal/embedding/ ./internal/app/

ci: fmt vet lint test test-race integration