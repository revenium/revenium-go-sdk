.PHONY: help test-all lint-all fmt-all vet-all deps build-all clean

MODULES := $(shell find . -name 'go.mod' -not -path './go.mod' -exec dirname {} \;)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

test-all: ## Run tests for all modules
	@for mod in $(MODULES); do \
		echo "=== Testing $$mod ==="; \
		(cd $$mod && go test -v -race ./...) || exit 1; \
	done

lint-all: ## Run go vet for all modules
	@for mod in $(MODULES); do \
		echo "=== Vetting $$mod ==="; \
		(cd $$mod && go vet ./...) || exit 1; \
	done

fmt-all: ## Format all modules
	@for mod in $(MODULES); do \
		echo "=== Formatting $$mod ==="; \
		(cd $$mod && gofmt -w .) || exit 1; \
	done

vet-all: lint-all ## Alias for lint-all

deps: ## Download dependencies for all modules
	@for mod in $(MODULES); do \
		echo "=== Downloading deps for $$mod ==="; \
		(cd $$mod && go mod download) || exit 1; \
	done

build-all: ## Build all modules
	@for mod in $(MODULES); do \
		echo "=== Building $$mod ==="; \
		(cd $$mod && go build ./...) || exit 1; \
	done

clean: ## Clean build artifacts
	@find . -name '*.test' -delete
	@find . -name '*.out' -delete
	@find . -name 'coverage.txt' -delete
