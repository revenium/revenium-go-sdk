.PHONY: help test-all lint-all fmt-all vet-all deps build-all clean

MODULES := $(shell find . -name 'go.mod' -not -path './go.mod' -not -path './examples/go.mod' -exec dirname {} \;)

define for_each_module
	@for mod in $(MODULES); do \
		echo "=== $(1) $$mod ==="; \
		(cd $$mod && $(2)) || exit 1; \
	done
	@echo "=== $(1) ./examples ==="
	@(cd examples && GOWORK=off $(2)) || exit 1
endef

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

test-all: ## Run tests for all modules
	$(call for_each_module,Testing,go test -v -race ./...)

lint-all: ## Run go vet for all modules
	$(call for_each_module,Vetting,go vet ./...)

fmt-all: ## Format all modules
	$(call for_each_module,Formatting,gofmt -w .)

vet-all: lint-all ## Alias for lint-all

deps: ## Download dependencies for all modules
	$(call for_each_module,Downloading deps for,go mod download)

build-all: ## Build all modules
	$(call for_each_module,Building,go build ./...)

clean: ## Clean build artifacts
	@find . -name '*.test' -delete
	@find . -name '*.out' -delete
	@find . -name 'coverage.txt' -delete
