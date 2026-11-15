GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

.PHONY: run test lint check build

run:
	go run ./cmd/concerts serve

test:
	@command -v gcc >/dev/null || { echo "gcc is required by -race"; exit 1; }
	go test -race ./...

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

check: lint test

build:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/concerts ./cmd/concerts

# Pinned and project-local, so another golangci-lint on PATH (e.g. v1) is never used.
$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@
