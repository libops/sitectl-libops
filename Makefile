.PHONY: build check check-core-version deps lint test docker integration-test docs plugins install-plugins install

BINARY_NAME=sitectl-libops
GO ?= go
GOFMT ?= gofmt

deps:
	$(GO) mod tidy

build:
	$(GO) build -o $(BINARY_NAME) .

install: build
	mv $(BINARY_NAME) /usr/local/bin

lint:
	test -z "$$(find . -name '*.go' -not -path './vendor/*' -exec $(GOFMT) -l {} +)"
	golangci-lint run

	@if command -v json5 > /dev/null 2>&1; then \
		echo "Running json5 validation on renovate.json5"; \
		json5 --validate renovate.json5 > /dev/null; \
	else \
		echo "json5 not found, skipping renovate validation"; \
	fi

check-core-version:
	./scripts/check-sitectl-core-version.sh v1.4.0

test: check-core-version build
	$(GO) test -v -race ./...

check: lint test
