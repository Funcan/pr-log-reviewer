# Discover every command package under cmd/ and map it to a binary in bin/.
BIN_DIR := bin
CMDS    := $(notdir $(wildcard cmd/*))
BINS    := $(addprefix $(BIN_DIR)/,$(CMDS))
GOFLAGS ?=

.DEFAULT_GOAL := default

.PHONY: default
default: format test build

.PHONY: build
build: $(BINS) ## Build all binaries under cmd/ into bin/

# Build a single binary; relink if any Go source changes.
$(BIN_DIR)/%: $(shell find . -name '*.go') go.mod
	@mkdir -p $(BIN_DIR)
	go build $(GOFLAGS) -o $@ ./cmd/$*

.PHONY: format
format: ## Format all Go code
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any Go code is not formatted
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "Unformatted files:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: check
check: fmt-check vet test ## Run format check, vet, and tests

.PHONY: eval
eval: ## Run the eval harness over the corpus using recorded fixtures (no network)
	go run ./cmd/eval -mode replay

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)
	go clean

.PHONY: help
help: ## List available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'
