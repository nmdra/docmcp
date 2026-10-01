.PHONY: test test-race vet lint integration provider ci build

build:
	go build -ldflags "-X main.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)" -o bin/docmcp ./cmd/docmcp

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint: vet
	golangci-lint run

integration:
	go test -tags=integration ./...

provider:
	DOCMCP_TEST_OLLAMA=1 go test -tags=provider ./...

ci: test test-race lint integration