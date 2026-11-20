# loans — local development entrypoint. Keep compatible with GNU Make 3.81 (macOS).
SHELL := /bin/bash
MAKEFLAGS += --no-print-directory
.DEFAULT_GOAL := help

GO            ?= go
GOFMT         ?= gofmt
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK   ?= govulncheck
BINARY_NAME   ?= loans
DIST_DIR      ?= dist
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COVERPROFILE  ?= cover.out

MAIN_PACKAGE := ./cmd/loans
BINARY       := $(abspath $(DIST_DIR))/$(BINARY_NAME)
LDFLAGS       = -X main.version=$(VERSION)

# --- meta -------------------------------------------------------------------

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## "; print "Usage: make <target>\n" } \
		/^[a-zA-Z0-9_.-]+:.*## / { printf "  %-15s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# --- build ------------------------------------------------------------------

run: ## Run from source, e.g. make run ARGS="--help"
	$(GO) run -ldflags "$(LDFLAGS)" $(MAIN_PACKAGE) $(ARGS)

build: ## Build the standalone binary into ./dist
	CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o "$(BINARY)" $(MAIN_PACKAGE)

install: ## Install loans with go install
	CGO_ENABLED=0 $(GO) install -ldflags "$(LDFLAGS)" $(MAIN_PACKAGE)

tidy: ## Tidy module dependencies
	$(GO) mod tidy

fmt: ## Format Go sources
	$(GOFMT) -s -w .

vet: ## Run go vet on all packages
	$(GO) vet ./...

# --- test / lint ------------------------------------------------------------

test: ## Run all tests and enforce 90% whole-module statement coverage
test-race: ## Run the same tests and coverage check with the race detector
test-race: RACE_FLAG := -race
test test-race:
	$(GO) test $(RACE_FLAG) $(GOTEST_FLAGS) -coverpkg=./... -coverprofile="$(COVERPROFILE)" ./...
	$(GO) tool cover -func="$(COVERPROFILE)"
	@awk 'NR > 1 { size[$$1] = $$2; if ($$3) hit[$$1] = 1 } \
		END { for (b in size) { total += size[b]; covered += size[b] * hit[b] } \
		if (!total || 100 * covered < 90 * total) { print "Statement coverage below 90%"; exit 1 } }' "$(COVERPROFILE)"

lint: ## Run golangci-lint on all packages
	$(GOLANGCI_LINT) run $(GOLANGCI_LINT_FLAGS) ./...

lint-fix: ## Apply fixes offered by golangci-lint
	$(GOLANGCI_LINT) run --fix $(GOLANGCI_LINT_FLAGS) ./...

govulncheck: ## Scan dependencies for known vulnerabilities
	$(GOVULNCHECK) ./...

ci: vet lint test-race ## Run local pre-push checks

ci-full: ci govulncheck ## Also scan for known vulnerabilities

clean: ## Remove build artifacts and the coverage profile
	rm -rf -- "$(DIST_DIR)"
	rm -f -- "$(COVERPROFILE)"

.PHONY: help run build install tidy fmt vet test test-race lint lint-fix \
        govulncheck ci ci-full clean
