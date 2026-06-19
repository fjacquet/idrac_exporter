# idrac_exporter — Makefile
# CI reproduces locally: everything CI runs is a target here.

BINARY     := idrac_exporter
REPOSITORY := github.com/fjacquet/idrac_exporter
PKG        := ./cmd/idrac_exporter

# Version is the vX.Y.Z tag on HEAD (stripped of the leading v), else "dev".
# Portable across BSD/GNU (the old `grep -oP` failed on macOS).
VERSION  := $(or $(shell git tag --points-at HEAD 2>/dev/null | sed -n 's/^v\([0-9.]*\)$$/\1/p'),dev)
REVISION := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)

LDFLAGS := -s -w
LDFLAGS += -X $(REPOSITORY)/internal/version.Version=$(VERSION)
LDFLAGS += -X $(REPOSITORY)/internal/version.Revision=$(REVISION)
GOFLAGS := -trimpath -ldflags "$(LDFLAGS)"

RUNFLAGS ?= --config config.yml --verbose

# Canonical tool versions — fjacquet/ci standard interface.
DIST  ?= dist
COVER ?= coverage.out
GOLANGCI_VERSION   ?= v2.12.2
GORELEASER_VERSION ?= v2.16.0

CYCLONEDX_GOMOD_VERSION := latest
GOVULNCHECK_VERSION     := latest

.PHONY: all clean install tools lint format test build vuln sbom security docs coverage-upload release ci \
        fmt fmt-check vet test-race test-coverage sure run run-cli docker release-snapshot help

# Bare `make` runs the local verify+build pipeline, not the first target.
.DEFAULT_GOAL := all

all: clean lint test build

## install: download Go module dependencies
install:
	go mod download

## tools: install pinned dev tooling
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	go install github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)

## lint: golangci-lint
lint:
	golangci-lint run --timeout=5m

## format: format all Go sources via golangci-lint
format:
	golangci-lint fmt

## fmt: format all Go sources (gofmt)
fmt:
	gofmt -w .

## fmt-check: fail if any file needs formatting
fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## vet: go vet
vet:
	go vet ./...

## test: unit tests with coverage
test:
	go test -race -coverprofile=$(COVER) -covermode=atomic ./...

## test-race: unit tests with the race detector
test-race:
	go test -race ./...

## test-coverage: write coverage.out + coverage.html
test-coverage:
	go test -coverprofile=$(COVER) ./...
	go tool cover -html=$(COVER) -o coverage.html

## build: compile all packages
build:
	go build -v ./...

## vuln: govulncheck
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

## sbom: CycloneDX module SBOM
sbom:
	mkdir -p $(DIST)
	go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest mod -json -output $(DIST)/sbom.cdx.json

## security: semgrep scan
security:
	uvx semgrep scan --config auto --error --skip-unknown-extensions

## docs: build documentation site
docs:
	uvx --with mkdocs-material --with pymdown-extensions mkdocs build --strict --site-dir site

## coverage-upload: upload coverage to Codecov
coverage-upload:
	uvx --from codecov-cli codecov upload-process --file $(COVER) || true

## ci: the gate — lint, test, build, vuln
ci: lint test build vuln

## release: GoReleaser release (publishes)
release:
	goreleaser release --clean

## release-snapshot: GoReleaser local dry-run (no publish)
release-snapshot:
	goreleaser release --snapshot --clean

## sure: local convenience — fmt, vet, test, cli, lint
sure: fmt vet test cli lint

## cli: build bin/$(BINARY) with version ldflags
cli:
	CGO_ENABLED=0 go build $(GOFLAGS) -o bin/$(BINARY) $(PKG)

## run: go run with RUNFLAGS
run:
	go run $(PKG) $(RUNFLAGS)

## run-cli: build then run bin/$(BINARY)
run-cli: cli
	./bin/$(BINARY) $(RUNFLAGS)

## docker: build the local (non-release) image
docker:
	docker build -t $(BINARY):$(VERSION) .

## clean: remove build artifacts
clean:
	rm -rf bin $(DIST) site $(COVER) coverage.html sbom.json *.sarif

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
