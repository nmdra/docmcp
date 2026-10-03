.PHONY: fmt test test-race vet lint integration provider ci build

fmt:
	@git ls-files -co --exclude-standard -- '*.go' | xargs gofmt -l | \
		awk 'NF { if (!bad++) print "needs gofmt:"; print } END { exit bad }'

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
	go test -p 1 -tags=integration ./...

provider:
	DOCMCP_TEST_LOCAL=1 go test -p 1 -tags=provider ./...

ci: fmt vet lint test test-race integration provider
