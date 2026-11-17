GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOVULNCHECK_VERSION := v1.9.0

ADDR ?= :8080
DEMO_DB ?= demo.db

.PHONY: run test lint fmt vuln check build demo verify-ui measure install uninstall deploy rollback

run:
	CONCERTS_BASE_URL=http://localhost:8080 go run ./cmd/concerts serve

# GORACE skips the race detector's 1 s pause at each test binary's exit (see CLAUDE.md Pitfalls).
test:
	@command -v gcc >/dev/null || { echo "gcc is required by -race"; exit 1; }
	GORACE="$${GORACE:+$$GORACE }atexit_sleep_ms=0" go test -race ./...

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

fmt:
	gofmt -w cmd internal

# Needs network: fetches the Go vulnerability database. Run by make deploy, and by the pre-commit hook when go.mod or go.sum is staged.
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

# Deployment: README.md "Deploy". No default host, so nothing goes to a machine by accident.
# install/uninstall run deploy/*.sh as root (ADMIN_HOST: an account with sudo);
# deploy/rollback log in as the service user (DEPLOY_HOST=concerts@<server>) and need no root.
ifneq ($(filter deploy rollback,$(MAKECMDGOALS)),)
ifeq ($(DEPLOY_HOST),)
$(error DEPLOY_HOST is required, e.g. make deploy DEPLOY_HOST=concerts@concerts.example.com)
endif
endif
ifneq ($(filter install uninstall,$(MAKECMDGOALS)),)
ifeq ($(ADMIN_HOST),)
$(error ADMIN_HOST is required, e.g. make install ADMIN_HOST=admin@concerts.example.com BASE_URL=https://concerts.example.com DEPLOY_KEY=~/.ssh/id_ed25519.pub)
endif
endif

# Copies deploy/ and $(1) to a fresh mktemp dir (root must not run files from a path another
# local user could create first), then runs $(2) there under sudo. ssh -n: the first ssh would
# otherwise swallow stdin, e.g. the answer piped to uninstall's confirmation.
define as_root
d=$$(ssh -n $(ADMIN_HOST) mktemp -d) && \
scp -q deploy/* $(1) $(ADMIN_HOST):"$$d/" && \
ssh -t $(ADMIN_HOST) "sudo sh $$d/$(2); s=\$$?; rm -r $$d; exit \$$s"
endef

install:
	@test -n "$(BASE_URL)" -a -f $(DEPLOY_KEY) || { echo "BASE_URL and DEPLOY_KEY (a public key file) are required" >&2; exit 1; }
	$(call as_root,$(DEPLOY_KEY),install.sh '$(BASE_URL)' $$d/$(notdir $(DEPLOY_KEY)))

uninstall:
	$(call as_root,,uninstall.sh)

# Uploaded into bin/ itself: same filesystem for an atomic rename, and bin/'s SELinux label.
deploy: vuln build
	scp -q bin/concerts $(DEPLOY_HOST):bin/concerts.new
	scp -q deploy/admin $(DEPLOY_HOST):bin/admin.new
	ssh $(DEPLOY_HOST) 'chmod 700 bin/admin.new bin/concerts.new && mv bin/admin.new bin/admin && bin/admin deploy'

rollback:
	ssh $(DEPLOY_HOST) bin/admin rollback
