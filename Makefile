GO      ?= go
PKG     := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build fmt fmt-check lint test test-integration test-e2e test-e2e-full \
        coverage coverage-gate generate golden clean build-images

build:
	CGO_ENABLED=0 $(GO) build $(LDFLAGS) -o bin/pglens-agent  ./cmd/pglens-agent
	CGO_ENABLED=0 $(GO) build $(LDFLAGS) -o bin/pglens-server ./cmd/pglens-server

fmt:
	gofmt -w .

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi

lint:
	golangci-lint run

test:
	$(GO) test -race -shuffle=on $(PKG)

test-integration:
	$(GO) test -tags=integration -race -shuffle=on -timeout=15m $(PKG)

test-e2e:
	$(GO) test -tags=e2e -timeout=25m -count=1 ./test/e2e/... -run 'Smoke'

test-e2e-full:
	$(GO) test -tags=e2e -timeout=45m -count=1 ./test/e2e/...

coverage:
	$(GO) test -race -coverprofile=coverage.out -covermode=atomic $(PKG)
	$(GO) tool cover -html=coverage.out -o coverage.html

coverage-gate:
	$(GO) test -race -coverprofile=coverage.out -covermode=atomic ./internal/...
	./scripts/coverage_gate.sh coverage.out

generate:
	@echo "not implemented until phase 3"; exit 0

golden:
	$(GO) test -tags=integration -run Golden ./internal/wire -update

clean:
	rm -rf bin dist coverage.out coverage.html

build-images:
	docker build -f Dockerfile.agent -t ghcr.io/manprint/pglens-agent:dev --build-arg VERSION=$(VERSION) .
	docker build -f Dockerfile.server -t ghcr.io/manprint/pglens-server:dev --build-arg VERSION=$(VERSION) .

build-images-multiarch:
	docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.agent -t ghcr.io/manprint/pglens-agent:dev .
	docker buildx build --platform linux/amd64,linux/arm64 -f Dockerfile.server -t ghcr.io/manprint/pglens-server:dev .
