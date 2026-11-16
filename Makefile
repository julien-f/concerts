GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOVULNCHECK_VERSION := v1.9.0

ADDR ?= :8080
DEMO_DB ?= demo.db

.PHONY: run test lint fmt vuln check build demo verify-ui measure deploy rollback

run:
	CONCERTS_BASE_URL=http://localhost:8080 go run ./cmd/concerts serve

test:
	@command -v gcc >/dev/null || { echo "gcc is required by -race"; exit 1; }
	go test -race ./...

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

fmt:
	gofmt -w cmd internal

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

# Deployment over SSH to the server set up as in README.md (sudo there). No default host,
# so a deploy never goes to a machine by accident.
ifneq ($(filter deploy rollback,$(MAKECMDGOALS)),)
ifeq ($(DEPLOY_HOST),)
$(error DEPLOY_HOST is required, e.g. make deploy DEPLOY_HOST=admin@concerts.example.com)
endif
endif
REMOTE_BIN := /usr/local/bin/concerts
REMOTE_DB := /var/lib/concerts/concerts.db
REMOTE_BACKUP := /var/lib/concerts/backups/pre-deploy.db
# Written before a deploy's restart, removed once /healthz passes (deploy or rollback).
REMOTE_FAILED := $(REMOTE_BACKUP).deploy-failed
REMOTE_CURL := command -v curl >/dev/null || { echo "curl is required on the server" >&2; exit 1; }
REMOTE_HEALTHZ := for i in $$(seq 20); do curl -fsS http://127.0.0.1:8080/healthz && sudo rm -f $(REMOTE_FAILED) && exit 0; sleep 1; done; echo "healthz failed: journalctl -u concerts" >&2; exit 1

# The installed (old) binary takes the backup: the new one would first migrate the live database.
# After a failed deploy, the installed binary is the broken one: the last good pair is kept,
# unless it now passes /healthz (the failure was elsewhere, e.g. a dropped ssh session).
deploy: build
	scp bin/concerts $(DEPLOY_HOST):concerts.new
	ssh -t $(DEPLOY_HOST) 'set -e; $(REMOTE_CURL); \
		if sudo test -e $(REMOTE_FAILED) && sudo test -f $(REMOTE_BACKUP) && [ -x $(REMOTE_BIN).prev ] \
			&& ! curl -fs --max-time 5 http://127.0.0.1:8080/healthz >/dev/null; then \
			echo "last deploy failed: keeping its pre-deploy snapshot and $(REMOTE_BIN).prev" >&2; \
		elif [ -x $(REMOTE_BIN) ]; then \
			sudo -u concerts mkdir -p $(dir $(REMOTE_BACKUP)); \
			sudo -u concerts rm -f $(REMOTE_BACKUP); \
			sudo -u concerts env CONCERTS_DB=$(REMOTE_DB) $(REMOTE_BIN) backup $(REMOTE_BACKUP); \
			sudo cp -p $(REMOTE_BIN) $(REMOTE_BIN).prev; \
			sudo touch $(REMOTE_FAILED); \
		fi; \
		sudo install -m 755 concerts.new $(REMOTE_BIN).new; \
		rm concerts.new; \
		sudo mv $(REMOTE_BIN).new $(REMOTE_BIN); \
		sudo systemctl restart concerts; \
		$(REMOTE_HEALTHZ)'

# Restores the previous binary and the pre-deploy database; writes made since the deploy
# stay in the moved-aside concerts.db.pre-rollback-* files.
rollback:
	ssh -t $(DEPLOY_HOST) 'set -e; $(REMOTE_CURL); \
		test -x $(REMOTE_BIN).prev; \
		sudo test -f $(REMOTE_BACKUP); \
		sudo systemctl stop concerts; \
		ts=$$(date +%Y%m%dT%H%M%S); \
		for f in $(REMOTE_DB) $(REMOTE_DB)-wal $(REMOTE_DB)-shm; do \
			if sudo test -e $$f; then sudo mv $$f $$f.pre-rollback-$$ts; fi; \
		done; \
		sudo install -o concerts -g concerts -m 600 $(REMOTE_BACKUP) $(REMOTE_DB); \
		sudo cp -p $(REMOTE_BIN).prev $(REMOTE_BIN); \
		sudo systemctl start concerts; \
		$(REMOTE_HEALTHZ)'
