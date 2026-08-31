GO      ?= go
PKG     := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build fmt fmt-check lint test test-integration test-e2e test-e2e-full test-e2e-full-evidence test-e2e-matrix api-docs \
        ci-local ci-local-unit ci-local-integration coverage coverage-gate generate golden clean build-images build-images-multiarch

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

test:
	$(GO) test -race -shuffle=on $(PKG)

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

ci-local:
	$(MAKE) ci-local-unit
	$(MAKE) ci-local-integration

ci-local-unit:
	$(MAKE) fmt-check
	$(MAKE) lint
	$(MAKE) build
	$(MAKE) test
	$(MAKE) coverage-gate

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

generate:
	@echo "not implemented until phase 3"; exit 0

api-docs:
	$(GO) run ./internal/tools/apidocs -input api/openapi.yaml -output docs/api.md

golden:
	$(GO) test -tags=integration -run Golden ./internal/wire -update

clean:
	rm -rf bin dist coverage.out coverage.html

build-images:
	docker build -f Dockerfile.agent -t ghcr.io/manprint/pglens-agent:dev --build-arg VERSION=$(VERSION) .
	docker build -f Dockerfile.server -t ghcr.io/manprint/pglens-server:dev --build-arg VERSION=$(VERSION) .

build-images-multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.agent -t ghcr.io/manprint/pglens-agent:dev --build-arg VERSION=$(VERSION) .
	docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.server -t ghcr.io/manprint/pglens-server:dev --build-arg VERSION=$(VERSION) .
