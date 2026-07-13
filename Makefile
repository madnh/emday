BINARY  := bin/emday
PKG     := ./cmd/emday
MODULE  := github.com/madnh/emday

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
           -X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
           -X $(MODULE)/internal/buildinfo.Date=$(DATE)

.PHONY: build-dev build-release test fmt clean

build-dev: ## Dev build (keeps debug symbols) → ./bin/emday
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

build-release: ## Release build (stripped + -trimpath) — the only shape to distribute
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS) -s -w" -o $(BINARY) $(PKG)

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal

clean:
	rm -rf bin
