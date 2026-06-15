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

RUNFLAGS ?= -config config.yml -verbose

# Pinned dev tools (installed by `make tools`).
GOLANGCI_LINT_VERSION   := v2.12.2
CYCLONEDX_GOMOD_VERSION := latest
GOVULNCHECK_VERSION     := latest

.PHONY: tools fmt fmt-check vet lint test test-race test-coverage vuln ci sure \
        cli build run run-cli sbom release release-snapshot docker clean help

## tools: install pinned dev tooling
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@$(CYCLONEDX_GOMOD_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

## fmt: format all Go sources
fmt:
	gofmt -w .

## fmt-check: fail if any file needs formatting
fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## vet: go vet
vet:
	go vet ./...

## lint: golangci-lint
lint:
	golangci-lint run ./...

## test: unit tests
test:
	go test ./...

## test-race: unit tests with the race detector
test-race:
	go test -race ./...

## test-coverage: write coverage.out + coverage.html
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

## vuln: govulncheck
vuln:
	govulncheck ./...

## ci: the gate — fmt-check, vet, lint, race tests, vuln
ci: fmt-check vet lint test-race vuln

## sure: local convenience — fmt, vet, test, build, lint
sure: fmt vet test cli lint

## cli: build bin/$(BINARY) with version ldflags
cli:
	CGO_ENABLED=0 go build $(GOFLAGS) -o bin/$(BINARY) $(PKG)

## build: alias for cli
build: cli

## run: go run with RUNFLAGS
run:
	go run $(PKG) $(RUNFLAGS)

## run-cli: build then run bin/$(BINARY)
run-cli: cli
	./bin/$(BINARY) $(RUNFLAGS)

## sbom: CycloneDX module SBOM
sbom:
	cyclonedx-gomod mod -licenses -json -output sbom.json

## release: GoReleaser release (publishes)
release:
	goreleaser release --clean

## release-snapshot: GoReleaser local dry-run (no publish)
release-snapshot:
	goreleaser release --snapshot --clean

## docker: build the local (non-release) image
docker:
	docker build -t $(BINARY):$(VERSION) .

## clean: remove build artifacts
clean:
	rm -rf bin dist coverage.out coverage.html sbom.json $(BINARY)

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //'
