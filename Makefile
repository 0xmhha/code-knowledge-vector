.PHONY: all build test test-race lint fmt tidy audit clean help model-fetch eval-pr eval-pr-1run eval-ab

GO ?= go
BIN_DIR := bin
PKG_LIST := ./...

# CGO is required for sqlite-vec (vec0 virtual table) via mattn/go-sqlite3.
# The asg017/sqlite-vec-go-bindings/cgo package links against a vendored
# sqlite-vec amalgamation, so no external shared library is needed.
export CGO_ENABLED ?= 1

# macOS only: silence the sqlite3_auto_extension / sqlite3_cancel_auto_extension
# deprecation warnings emitted from sqlite-vec-go-bindings/cgo/lib.go. Apple's
# System SDK marks those symbols deprecated because process-global state
# conflicts with sandboxing, but they still link and run. We do not call them
# directly — only via the upstream binding. A real fix would require the
# upstream library to switch to per-connection extension registration.
ifeq ($(shell uname),Darwin)
export CGO_CFLAGS := -Wno-deprecated-declarations $(CGO_CFLAGS)
endif

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

# ---- bgeonnx targets (require -tags bgeonnx + model files) ----

model-fetch: ## Download bge-large-en-v1.5 ONNX model (~1.34GB)
	$(GO) run ./cmd/ckv model fetch bge-large-en-v1.5

eval-pr: ## Run 12-PR regression eval with bgeonnx (32GB+ RAM, CoreML)
	$(GO) run -tags bgeonnx ./cmd/ckv eval \
		--pr-fixture ./testdata/prs.yaml \
		--embedder=bgeonnx --pr-runs 3 \
		--judge claude --json

eval-pr-1run: ## Single-run PR eval (faster, no noise averaging)
	$(GO) run -tags bgeonnx ./cmd/ckv eval \
		--pr-fixture ./testdata/prs.yaml \
		--embedder=bgeonnx --pr-runs 1 \
		--judge claude --json

eval-ab: ## A/B measurement: bgeonnx BM25 OFF vs ON (testdata/sample)
	@echo "=== BM25 OFF ===" && \
	TMP=$$(mktemp -d) && \
	CKV_MEM_GUARD=off CKV_DISABLE_COREML=1 $(GO) run -tags bgeonnx ./cmd/ckv build --src ./testdata/sample --out "$$TMP" --embedder=bgeonnx && \
	CKV_MEM_GUARD=off CKV_DISABLE_COREML=1 $(GO) run -tags bgeonnx ./cmd/ckv eval --fixture ./testdata/queries.yaml --out "$$TMP" --src ./testdata/sample --embedder=bgeonnx --json && \
	echo "=== BM25 ON ===" && \
	CKV_MEM_GUARD=off CKV_DISABLE_COREML=1 $(GO) run -tags bgeonnx ./cmd/ckv eval --fixture ./testdata/queries.yaml --out "$$TMP" --src ./testdata/sample --embedder=bgeonnx --bm25-rerank --json && \
	rm -rf "$$TMP"

# ---- cleanup ----

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)/ coverage.out /tmp/ckv-*

help: ## Show this help
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
