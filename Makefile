GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOVULNCHECK_VERSION := v1.9.0

# Per-machine values (DEPLOY_HOST, ADMIN_HOST…), git-ignored; the command line still overrides them.
# The main checkout's, so git worktrees (which lack ignored files) use it too.
-include $(dir $(shell git rev-parse --path-format=absolute --git-common-dir 2>/dev/null))local.mk

ADDR ?= :8080
DEMO_DB ?= demo.db

.PHONY: run test lint fmt vuln check build demo verify-ui measure-scroll measure install uninstall deploy rollback print-deploy-host

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

# axe-core and vnu on every screen of `make demo`, plus the group page's weight; downloads its tools once.
verify-ui:
	scripts/verify-ui.sh

# Scroll smoothness of style.css at REF vs the working tree's, on a phone on the same network; PAGE=list or affiche.
REF ?= main
PAGE ?= list
measure-scroll:
	scripts/measure-scroll.sh $(REF) $(PAGE)

# Go lines, binary size and `make check` duration, for a merge commit's body.
measure:
	scripts/measure.sh

build:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/concerts ./cmd/concerts

# Pinned and project-local, so another golangci-lint on PATH (e.g. v1) is never used.
$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	mv bin/golangci-lint $@

# Deployment: docs/operations.md. No default host, so nothing goes to a machine by accident.
# install/uninstall run deploy/*.sh as root (ADMIN_HOST: an account with sudo);
# deploy/rollback log in as the service user (DEPLOY_HOST=<name>@<server>) and need no root.
NAME ?= concerts
ifneq ($(filter deploy rollback,$(MAKECMDGOALS)),)
ifeq ($(DEPLOY_HOST),)
$(error DEPLOY_HOST is required: set it in local.mk, see docs/operations.md)
endif
endif
ifneq ($(filter install uninstall,$(MAKECMDGOALS)),)
ifeq ($(ADMIN_HOST),)
$(error ADMIN_HOST is required: set it in local.mk, see docs/operations.md)
endif
endif

# Checks the arguments locally (deploy/lib.sh) before they reach the remote shell. Then copies
# deploy/ and $(1) to a fresh mktemp dir (root must not run files from a path another local user
# could create first) and runs $(2) there under sudo. ssh -n: the first ssh would otherwise
# swallow stdin, e.g. the answer piped to uninstall's confirmation.
define as_root
sh -c '. deploy/lib.sh; check_name NAME "$$1"; [ -z "$$2" ] || check_name PROXY_USER "$$2"; [ -z "$$3" ] || check_url "$$3"' \
	make '$(NAME)' '$(PROXY_USER)' '$(BASE_URL)' && \
d=$$(ssh -n $(ADMIN_HOST) mktemp -d) && \
scp -q deploy/* $(1) $(ADMIN_HOST):"$$d/" && \
ssh -t $(ADMIN_HOST) "sudo sh $$d/$(2); s=\$$?; rm -r $$d; exit \$$s"
endef

install:
	@{ test -n "$(PROXY_USER)" && test -n "$(BASE_URL)" && test -n "$(DEPLOY_KEY)" && test -f $(DEPLOY_KEY); } || { echo "PROXY_USER, BASE_URL and DEPLOY_KEY (a public key file) are required: set them in local.mk, see docs/operations.md" >&2; exit 1; }
	$(call as_root,$(DEPLOY_KEY),install.sh $(NAME) $(PROXY_USER) $(BASE_URL) $$d/$(notdir $(DEPLOY_KEY)))

uninstall:
	$(call as_root,,uninstall.sh $(NAME))

# Uploaded into bin/ itself: same filesystem for an atomic rename, and bin/'s SELinux label.
# ssh -T: see docs/operations.md, Admin commands.
deploy: vuln build
	scp -q bin/concerts $(DEPLOY_HOST):bin/concerts.new
	scp -q deploy/admin $(DEPLOY_HOST):bin/admin.new
	ssh -T $(DEPLOY_HOST) 'chmod 700 bin/admin.new bin/concerts.new && mv bin/admin.new bin/admin && bin/admin deploy'

rollback:
	ssh -T $(DEPLOY_HOST) bin/admin rollback

# For scripts/admin.
print-deploy-host:
	@printf '%s\n' '$(strip $(DEPLOY_HOST))'
