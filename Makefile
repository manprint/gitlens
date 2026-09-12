GO      ?= go
PNPM    ?= pnpm
WEB     := web
PKG     := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"
UI_E2E_RUN ?= UI

.PHONY: build fmt fmt-check lint vet-tags vuln tidy-check stress test test-integration test-e2e test-e2e-full test-e2e-full-evidence test-e2e-matrix api-docs \
        ci-local ci-local-unit ci-local-integration ci-local-security coverage coverage-gate generate golden clean build-images build-images-multiarch \
        web-install web-gen-api web-typecheck web-lint web-build web-budget web-test web-coverage web-coverage-gate test-ui-e2e

build:
	CGO_ENABLED=0 $(GO) build $(LDFLAGS) -o bin/pglens-agent  ./cmd/pglens-agent
	CGO_ENABLED=0 $(GO) build $(LDFLAGS) -o bin/pglens-server ./cmd/pglens-server

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	elif test -x "$$(go env GOPATH)/bin/golangci-lint"; then \
		"$$(go env GOPATH)/bin/golangci-lint" run ./...; \
	else \
		echo "golangci-lint is required (install it or put it on PATH)" >&2; exit 1; \
	fi

# Tagged code is not compiled by `go build ./...`, so a refactor can break the
# integration and e2e suites without any fast-lane job noticing — the failure
# then surfaces 40 minutes later in the Docker-backed gate, or not until a
# nightly run. Vetting under each tag costs seconds and catches it immediately.
vet-tags:
	$(GO) vet ./...
	$(GO) vet -tags=integration ./...
	$(GO) vet -tags=e2e ./...

# Standard-library advisories reach this code through the ingest endpoint, the
# agent's HTTP client and the notification channels; go.mod's `toolchain`
# directive is what pins them out, and this is what proves the pin still holds.
vuln:
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	elif test -x "$$(go env GOPATH)/bin/govulncheck"; then \
		"$$(go env GOPATH)/bin/govulncheck" ./...; \
	else \
		echo "govulncheck is required: go install golang.org/x/vuln/cmd/govulncheck@latest" >&2; exit 1; \
	fi

tidy-check:
	@cp go.mod go.mod.tidycheck && cp go.sum go.sum.tidycheck
	@trap 'mv go.mod.tidycheck go.mod; mv go.sum.tidycheck go.sum' EXIT; \
		$(GO) mod tidy && diff -u go.mod.tidycheck go.mod && diff -u go.sum.tidycheck go.sum \
		|| { echo "go.mod/go.sum are not tidy; run: go mod tidy" >&2; exit 1; }
	$(GO) mod verify

test:
	$(GO) test -race -shuffle=on $(PKG)

# The packages whose correctness is a concurrency property rather than a pure
# function: a scheduler that leaks a ticker loop, a buffer whose reader and
# writer share a file offset, an engine whose Stop races its own Start. One
# pass of the race detector finds the reliable ones; repeated shuffled passes
# find the ones that need an unlucky interleaving.
#
# Deliberately N separate invocations rather than `-count=N`: -count reruns
# inside the same process, and several suites here assert on process-wide
# state (the server's package-level metric counters, the advisor's global rule
# registry), so a second in-process pass fails for reasons that have nothing
# to do with concurrency. A fresh process per round also gives each round its
# own -shuffle seed, which is the point of the exercise.
STRESS_PKG ?= ./internal/agent/... ./internal/ash/... ./internal/alert/... ./internal/advisor/... ./internal/server/... ./internal/leaktest/...
STRESS_ROUNDS ?= 5
stress:
	@set -eu; \
	for i in $$(seq 1 $(STRESS_ROUNDS)); do \
		echo "==> stress round $$i/$(STRESS_ROUNDS)"; \
		$(GO) test -race -shuffle=on -count=1 -timeout=20m $(STRESS_PKG); \
	done

test-integration:
	$(GO) test -tags=integration -race -shuffle=on -timeout=15m $(PKG)

test-e2e:
	$(GO) test -tags=e2e -timeout=35m -count=1 ./test/e2e/... -run 'Smoke'

test-e2e-full:
	$(GO) test -tags=e2e -timeout=90m -count=1 ./test/e2e/...

test-e2e-full-evidence:
	./scripts/e2e_evidence.sh $(MAKE) test-e2e-full

test-e2e-matrix:
	AGENT_MODE=container $(MAKE) test-e2e
	AGENT_MODE=binary $(MAKE) test-e2e

test-ui-e2e: web-build build-images
	@browser="$$(cd $(WEB) && $(PNPM) exec node -e 'process.stdout.write(require("@playwright/test").chromium.executablePath())')"; \
	if test ! -x "$$browser"; then \
		echo "Playwright Chromium is not installed at $$browser" >&2; \
		echo "Install it once with: cd $(WEB) && $(PNPM) exec playwright install --with-deps chromium" >&2; \
		exit 1; \
	fi
	$(GO) test -tags=e2e -timeout=40m -count=1 ./test/e2e/... -run '$(UI_E2E_RUN)'

ci-local:
	$(MAKE) ci-local-unit
	$(MAKE) ci-local-security
	$(MAKE) ci-local-integration

ci-local-security:
	$(MAKE) tidy-check
	$(MAKE) vuln

ci-local-unit:
	$(MAKE) fmt-check
	$(MAKE) lint
	$(MAKE) vet-tags
	$(MAKE) build
	$(MAKE) test
	$(MAKE) coverage-gate
	$(MAKE) web-install
	$(MAKE) web-lint
	$(MAKE) web-typecheck
	$(MAKE) web-test
	$(MAKE) web-coverage-gate
	$(MAKE) web-budget

ci-local-integration:
	@set -eu; \
	for pg in 15 16 17 18; do \
		for profile in vanilla rds-like; do \
			echo "==> integration PG$$pg / $$profile"; \
			PGLENS_PG_VERSIONS=$$pg PGLENS_PG_PROFILE=$$profile $(MAKE) test-integration; \
		done; \
	done

coverage:
	$(GO) test -race -coverprofile=coverage.out -covermode=atomic $(PKG)
	$(GO) tool cover -html=coverage.out -o coverage.html

coverage-gate:
	$(GO) test -race -coverprofile=coverage.out -covermode=atomic ./internal/...
	./scripts/coverage_gate.sh coverage.out

web-install:
	cd $(WEB) && $(PNPM) install --frozen-lockfile

web-typecheck:
	cd $(WEB) && $(PNPM) run typecheck

web-gen-api:
	cd $(WEB) && $(PNPM) run gen:api
	git diff --exit-code -- web/src/api/generated.ts

web-lint: web-gen-api
	cd $(WEB) && $(PNPM) run lint && $(PNPM) run format:check

web-build:
	cd $(WEB) && PGLENS_VERSION=$(VERSION) $(PNPM) run build
	@printf 'built_at=%s\nvite=8.2.2\n' "$$(date -u +%Y-%m-%dT%H:%M:%SZ)" > internal/webui/dist/.built

web-budget: web-build
	cd $(WEB) && $(PNPM) exec tsx scripts/bundle-budget.ts

web-test:
	cd $(WEB) && $(PNPM) run test

web-coverage:
	cd $(WEB) && $(PNPM) run test:coverage

web-coverage-gate: web-coverage
	./scripts/coverage_gate_ui.sh

generate:
	@echo "not implemented until phase 3"; exit 0

api-docs:
	$(GO) run ./internal/tools/apidocs -input api/openapi.yaml -output docs/api.md

golden:
	$(GO) test -tags=integration -run Golden ./internal/wire -update

clean:
	rm -rf bin dist coverage.out coverage.html web/node_modules web/dist
	@if test -d internal/webui/dist; then \
		find internal/webui/dist -mindepth 1 -maxdepth 1 ! -name index.html -exec rm -rf {} +; \
		git checkout -- internal/webui/dist/index.html; \
	fi

build-images:
	docker build -f Dockerfile.agent -t ghcr.io/manprint/pglens-agent:dev --build-arg VERSION=$(VERSION) .
	docker build -f Dockerfile.server -t ghcr.io/manprint/pglens-server:dev --build-arg VERSION=$(VERSION) .

build-images-multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.agent -t ghcr.io/manprint/pglens-agent:dev --build-arg VERSION=$(VERSION) .
	docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.server -t ghcr.io/manprint/pglens-server:dev --build-arg VERSION=$(VERSION) .
