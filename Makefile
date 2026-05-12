.PHONY: all build test test-race lint fmt tidy audit clean help

GO ?= go
BIN_DIR := bin
PKG_LIST := ./...

# CGO is required for sqlite-vec (vec0 virtual table) via mattn/go-sqlite3.
# The asg017/sqlite-vec-go-bindings/cgo package links against a vendored
# sqlite-vec amalgamation, so no external shared library is needed.
export CGO_ENABLED ?= 1

all: build ## Default: build the ckv binary

build: ## Build bin/ckv
	$(GO) build -o $(BIN_DIR)/ckv ./cmd/ckv

# The combined CKG+CKV binary (cks-mcp) lives in the CKS repository, not
# here. CKV stays as a pure Vector-layer library; CKS imports it (and CKG)
# and produces the multiplexed MCP binary. See plan-S1-ckv.md §7.

test: ## Run unit tests
	$(GO) test $(PKG_LIST)

test-race: ## Run tests with race detector + coverage
	$(GO) test -race -coverprofile=coverage.out $(PKG_LIST)

lint: ## go vet (golangci-lint optional)
	$(GO) vet $(PKG_LIST)
	@if command -v golangci-lint >/dev/null 2>&1; then \
	    golangci-lint run; \
	else \
	    echo "golangci-lint not installed (skipping). install: brew install golangci-lint"; \
	fi

fmt: ## Format Go sources
	$(GO) fmt $(PKG_LIST)
	@if command -v goimports >/dev/null 2>&1; then \
	    goimports -w .; \
	fi

tidy: ## go mod tidy
	$(GO) mod tidy

audit: ## govulncheck (call-graph reachable vulns)
	@if command -v govulncheck >/dev/null 2>&1; then \
	    govulncheck $(PKG_LIST); \
	else \
	    echo "govulncheck not installed."; \
	    echo "  install: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
	    exit 1; \
	fi

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)/ coverage.out /tmp/ckv-*

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
