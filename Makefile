.PHONY: build install clean test lint

BINARY=gitrespect
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS=-ldflags "-X github.com/juangracia/gitrespect/internal/cmd.Version=$(VERSION) \
                  -X github.com/juangracia/gitrespect/internal/cmd.Commit=$(COMMIT) \
                  -X github.com/juangracia/gitrespect/internal/cmd.Date=$(DATE)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/gitrespect

install:
	go install $(LDFLAGS) ./cmd/gitrespect

clean:
	rm -f $(BINARY)
	rm -rf dist/

test:
	go test -v ./...

lint:
	golangci-lint run

# Build for all platforms
release:
	mkdir -p dist
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY)-darwin-amd64 ./cmd/gitrespect
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY)-darwin-arm64 ./cmd/gitrespect
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY)-linux-amd64 ./cmd/gitrespect
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY)-linux-arm64 ./cmd/gitrespect
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY)-windows-amd64.exe ./cmd/gitrespect
