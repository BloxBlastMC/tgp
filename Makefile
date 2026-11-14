# tgp build tasks. `make` builds the web UI and all three binaries into bin/.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := git.taile67276.ts.net/BloxBlast/tgp/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)
GOFLAGS ?= -trimpath
export CGO_ENABLED ?= 0

BINARIES := tgp-server tgp tgp-runner

.PHONY: all build web web-deps generate check-generate test test-go test-web e2e lint fmt run clean

all: web build

build: $(addprefix bin/,$(BINARIES))

bin/%: FORCE
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/$*

FORCE:

web-deps:
	cd web && pnpm install --frozen-lockfile

web: web-deps
	cd web && pnpm build

# Regenerate Go and TypeScript code from api/openapi.yaml.
generate:
	go generate ./...
	cd web && pnpm run generate

# Fails if generated code is out of date with the spec.
check-generate: generate
	git diff --exit-code -- internal/api/apigen web/src/api/schema.gen.ts

test: test-go test-web

test-go:
	CGO_ENABLED=1 go test -race ./...

test-web:
	cd web && pnpm test

# Browser end-to-end tests against a freshly built server. Set CHROMIUM_PATH
# to use a system Chromium instead of Playwright's download.
e2e: web build
	cd web && pnpm e2e

lint:
	go vet ./...
	test -z "$$(gofmt -l $$(git ls-files -co --exclude-standard '*.go'))" || (gofmt -l $$(git ls-files -co --exclude-standard '*.go'); exit 1)
	cd web && pnpm typecheck

fmt:
	gofmt -w $$(git ls-files -co --exclude-standard '*.go')

# Run the server against ./data with the currently built UI.
run: build
	./bin/tgp-server

clean:
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete
