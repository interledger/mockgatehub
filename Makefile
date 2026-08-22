.PHONY: help test unit-tests e2e-tests helm-test helm-deps coverage build lint clean

help:
	@echo "MockGatehub Test Commands"
	@echo ""
	@echo "test              Run lint + unit tests + feature e2e tests"
	@echo "unit-tests        Run unit tests only"
	@echo "e2e-tests         Run feature (godog) e2e tests"
	@echo "helm-test         Lint, unit test and schema-validate the Helm chart"
	@echo "coverage          Run unit tests with coverage report"
	@echo "build             Build the mockgatehub binary"
	@echo "lint              Run linter (gofmt, go vet)"
	@echo "clean             Clean up build artifacts and test binaries"
	@echo ""

# Run all tests: unit tests + feature tests
test: lint unit-tests e2e-tests
	@echo ""
	@echo "✅ All tests completed"

# Run unit tests
unit-tests:
	@echo "Running all unit tests..."
	@go test -v ./...
	@echo "Checking coverage for internal"
	@go test -v -coverprofile=coverage.out ./internal/...
	

e2e-tests:
	@echo "Running feature e2e tests (godog)..."
	@go test -tags e2e -v -count=1 ./testenv/ -run TestFeatures

# Ensure the chart's dependencies are present. The subchart is vendored under
# helm/mockgatehub/charts, so this is normally a no-op and needs no network;
# it only fetches when that directory has been cleaned.
helm-deps:
	@if ls helm/mockgatehub/charts/*.tgz >/dev/null 2>&1; then \
		echo "Chart dependencies already vendored"; \
	else \
		echo "Fetching chart dependencies..."; \
		helm repo add groundhog2k https://groundhog2k.github.io/helm-charts/ --force-update >/dev/null; \
		helm dependency build helm/mockgatehub; \
	fi

# Validate the Helm chart: lint, unit tests, and schema validation of the
# rendered manifests with the optional templates both off and on.
helm-test: helm-deps
	@echo "Linting chart..."
	@helm lint helm/mockgatehub
	@echo "Running chart unit tests..."
	@helm unittest helm/mockgatehub
	@echo "Validating rendered manifests (defaults)..."
	@helm template mockgatehub helm/mockgatehub | kubeconform -strict -summary -ignore-missing-schemas -
	@echo "Validating rendered manifests (optional templates enabled)..."
	@helm template mockgatehub helm/mockgatehub -f helm/mockgatehub/tests/ci-values.yaml \
		| kubeconform -strict -summary -ignore-missing-schemas -
	@echo "✅ Helm chart validated"

# Run tests with coverage report
coverage:
	@echo "Running unit tests with coverage..."
	@go test -v ./... -coverprofile=coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Build the mockgatehub binary
build:
	@echo "Building mockgatehub..."
	@go build -v -o mockgatehub ./cmd/mockgatehub

# Run linter
lint:
	@echo "Running linters..."
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "gofmt found unformatted files"; exit 1)
	@go vet -tags e2e ./...
	@echo "Running golangci-lint..."
	@golangci-lint run ./...

# Clean up artifacts
clean:
	@echo "Cleaning up..."
	@rm -f mockgatehub coverage.out coverage.html
	@cd testenv && rm -f test_e2e
	@go clean -testcache
	@echo "Clean complete"
