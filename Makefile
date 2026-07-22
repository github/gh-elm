# gh-elm — build and developer tasks.
# Binaries are named `gh-elm` so `gh extension install .` can find them.

BINARY  := gh-elm
PKG     := github.com/github/gh-elm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build
build: ## Build the ./gh-elm binary
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

.PHONY: install
install: build ## Build and (re)install as a local gh extension (gh elm ...)
	-gh extension remove elm
	gh extension install .

.PHONY: test
test: ## Run unit tests
	go test -race ./...

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: fmt
fmt: ## Format the code
	gofmt -w .

.PHONY: tidy
tidy: ## Tidy go.mod/go.sum
	go mod tidy

.PHONY: audit
audit: fmt vet lint test ## Run the checks CI runs

.PHONY: lint
lint: ## Run golangci-lint
	script/lint-go-code

.PHONY: release-snapshot
release-snapshot: ## Build a local GoReleaser snapshot into dist/ (no publish)
	goreleaser release --snapshot --clean --skip=publish

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf dist

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
