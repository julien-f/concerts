GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOVULNCHECK_VERSION := v1.9.0

ADDR ?= :8080
DEMO_DB ?= demo.db

.PHONY: run test lint vuln check build demo verify-ui measure

run:
	CONCERTS_BASE_URL=http://localhost:8080 go run ./cmd/concerts serve

test:
	@command -v gcc >/dev/null || { echo "gcc is required by -race"; exit 1; }
	go test -race ./...

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

# Needs network: fetches the Go vulnerability database.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

check: lint vuln test

# Fresh demo database (group, members, past and future concerts), then serves it.
demo: build
	rm -f $(DEMO_DB) $(DEMO_DB)-wal $(DEMO_DB)-shm
	CONCERTS_DB=$(DEMO_DB) CONCERTS_BASE_URL=http://localhost:8080 bin/concerts demo
	CONCERTS_DB=$(DEMO_DB) CONCERTS_BASE_URL=http://localhost:8080 CONCERTS_ADDR=$(ADDR) bin/concerts serve

# axe-core, vnu and page weight on every screen of `make demo`; downloads its tools once.
verify-ui:
	scripts/verify-ui.sh

# Go lines, binary size and `make check` duration, logged per slice.
measure:
	scripts/measure.sh

build:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/concerts ./cmd/concerts

# Pinned and project-local, so another golangci-lint on PATH (e.g. v1) is never used.
$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@
