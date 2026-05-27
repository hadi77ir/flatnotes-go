SHELL := /bin/sh

APP_NAME ?= flatnotes-go
BUILD_DIR ?= bin
GO ?= go
NPM ?= npm
GOVULNCHECK ?= govulncheck
GOCACHE ?= /tmp/go-build-cache
NPM_CACHE ?= /tmp/npm-cache
GOFLAGS ?=
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS ?= -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)
DOCKER_IMAGE ?= flatnotes-go

.PHONY: all deps frontend embed build test vet audit npm-audit govulncheck security docker docker-rootless snapshot clean

all: test build

deps:
	$(NPM) ci --cache $(NPM_CACHE)
	$(GO) mod download

frontend:
	$(NPM) run build

embed: frontend
	rm -rf internal/web/dist
	mkdir -p internal/web/dist
	cp -R client/dist/. internal/web/dist/

build: embed
	mkdir -p $(BUILD_DIR)
	GOCACHE=$(GOCACHE) CGO_ENABLED=0 $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) ./cmd/flatnotes-go

test:
	GOCACHE=$(GOCACHE) $(GO) test ./...

vet:
	GOCACHE=$(GOCACHE) $(GO) vet ./...

npm-audit:
	$(NPM) audit --audit-level=moderate --cache $(NPM_CACHE)

govulncheck:
	GOCACHE=$(GOCACHE) $(GOVULNCHECK) ./...

audit: npm-audit govulncheck

security: test vet audit

docker:
	docker build -f Dockerfile -t $(DOCKER_IMAGE):latest .

docker-rootless:
	docker build -f Dockerfile.rootless -t $(DOCKER_IMAGE):rootless .

snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf $(BUILD_DIR) client/dist internal/web/dist dist
	mkdir -p internal/web/dist
	touch internal/web/dist/.gitkeep
