.DEFAULT_GOAL := help

BINARY_NAME := modrot
COVERAGE_DIR := coverage
GOSEC_EXCLUDE ?= G204,G304

.PHONY: help
help: ## Display this help message
	@awk 'BEGIN {FS = ":.*##"; printf "Usage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[33m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build

.PHONY: build
build: ## Build the binary
	go build -o $(BINARY_NAME) .

.PHONY: install
install: ## Install to GOPATH/bin
	go install .

##@ Testing

.PHONY: test
test: ## Run all tests
	go test -race ./...

.PHONY: coverage
coverage: ## Generate test coverage report
	@mkdir -p $(COVERAGE_DIR)
	go test -race -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	go tool cover -func=$(COVERAGE_DIR)/coverage.out

.PHONY: coverage-html
coverage-html: coverage ## Generate and open HTML coverage report
	go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	open $(COVERAGE_DIR)/coverage.html

##@ Code Quality

.PHONY: fmt
fmt: ## Format all Go source files
	gofmt -w .

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found. Install:"; \
		echo "  macOS:  brew install golangci-lint"; \
		echo "  Linux:  https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	golangci-lint run ./...

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not found. Install:"; \
		echo "  macOS:  brew install golangci-lint"; \
		echo "  Linux:  https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	golangci-lint run --fix ./...

.PHONY: check
check: fmt vet lint ## Run all code quality checks (fmt, vet, lint)

##@ Dependencies

.PHONY: tidy
tidy: ## Tidy and verify go modules
	go mod tidy
	go mod verify

##@ Security

.PHONY: govulncheck
govulncheck: ## Run vulnerability check on dependencies
	@command -v govulncheck >/dev/null 2>&1 || { \
		echo "Installing govulncheck..."; \
		go install golang.org/x/vuln/cmd/govulncheck@latest; \
	}
	govulncheck ./...

.PHONY: trivy
trivy: ## Run Trivy filesystem vulnerability scanner
	@command -v trivy >/dev/null 2>&1 || { \
		echo "trivy not found. Install:"; \
		echo "  macOS:  brew install trivy"; \
		echo "  Linux:  https://aquasecurity.github.io/trivy/latest/getting-started/installation/"; \
		exit 1; \
	}
	trivy fs --scanners vuln,secret --severity HIGH,CRITICAL .

.PHONY: gosec
gosec: ## Run gosec security scanner (override GOSEC_EXCLUDE= to show all)
	@command -v gosec >/dev/null 2>&1 || { \
		echo "Installing gosec..."; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
	}
	gosec -quiet $(if $(GOSEC_EXCLUDE),-exclude=$(GOSEC_EXCLUDE)) ./...

.PHONY: gitleaks
gitleaks: ## Run gitleaks secret scanner
	@command -v gitleaks >/dev/null 2>&1 || { \
		echo "gitleaks not found. Install:"; \
		echo "  macOS:  brew install gitleaks"; \
		echo "  Linux:  https://github.com/gitleaks/gitleaks#installing"; \
		exit 1; \
	}
	gitleaks detect --source . --no-banner --redact

.PHONY: security
security: govulncheck trivy gosec gitleaks ## Run all security scans

##@ Verify

.PHONY: verify
verify: tidy check test security ## Run all checks before commit

##@ Cleanup

.PHONY: clean
clean: ## Clean build artifacts
	rm -rf $(BINARY_NAME) $(COVERAGE_DIR) coverage.out
