# Alexandria — Makefile (see CLAUDE.md and CHAD.md)
#
# Pipeline: resolve (Pkl deps) -> gen (Go types, committed) -> build.

GO          ?= go
BUN         ?= bun
PKL         ?= pkl
PKL_GO_PKG  := package://pkg.pkl-lang.org/pkl-go/pkl.golang@0.13.2
APP         := alexandria
MODULE      := github.com/bmurray/alexandria
CONFIG_FILE ?= ./configs/dev.pkl
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
IMAGE       ?= ghcr.io/bmurray/$(APP)

# Explicit package list: web/node_modules may contain stray Go files.
GO_PKGS     := ./cmd/... ./internal/... ./config/... ./web
# Schema modules that get Go types. The deploy schema is rendered by the CLI only.
PKL_SCHEMAS := $(shell find config -name '*.pkl' -not -path 'config/deploy/*' | sort)
GEN_STAMP   := internal/gen/.stamp

.PHONY: all dev web-dev build web-build dist gen gen-force resolve pkl-eval pkl-bin \
        test test-go test-web e2e e2e-install lint typecheck fmt clean \
        image manifests bootstrap oci-push

all: build

## ---- development ----

# Go server on :24380 with the Vite proxy (configs/dev.pkl sets server.devProxy).
dev: $(GEN_STAMP)
	CONFIG_FILE=$(CONFIG_FILE) $(GO) run -tags dev ./cmd/$(APP)

# Vite dev server on :24381. Open http://localhost:24380 (Go proxies to it).
web-dev: web/node_modules
	cd web && $(BUN) run dev

web/node_modules: web/package.json web/bun.lock
	cd web && $(BUN) install --frozen-lockfile
	@touch web/node_modules

## ---- Pkl ----

resolve: config/PklProject.deps.json configs/PklProject.deps.json devops/k3s/PklProject.deps.json
config/PklProject.deps.json: config/PklProject
	cd config && $(PKL) project resolve
configs/PklProject.deps.json: configs/PklProject config/PklProject
	cd configs && $(PKL) project resolve
devops/k3s/PklProject.deps.json: devops/k3s/PklProject config/PklProject
	cd devops/k3s && $(PKL) project resolve

# Pkl -> Go codegen into internal/gen (one package per module). Committed.
gen: $(GEN_STAMP)

$(GEN_STAMP): $(PKL_SCHEMAS) config/PklProject config/PklProject.deps.json
	$(PKL) run --project-dir config '$(PKL_GO_PKG)#/gen.pkl' -p projectDir=config -p moduleDir=config --output-path . -- $(PKL_SCHEMAS)
	gofmt -w internal/gen
	@mkdir -p internal/gen && touch $(GEN_STAMP)

gen-force:
	rm -rf internal/gen
	$(MAKE) gen

# Evaluate a config against the schema (same resolution as pkl-loader).
pkl-eval: configs/PklProject.deps.json
	$(PKL) eval --project-dir configs $(CONFIG_FILE)

# Freeze a config into a self-contained .pklbin (no pkl binary needed at runtime).
pkl-bin: configs/PklProject.deps.json
	$(PKL) eval --project-dir configs -f pkl-binary -o $(basename $(CONFIG_FILE)).pklbin $(CONFIG_FILE)

## ---- build ----

web-build: web/node_modules
	cd web && $(BUN) run build

build: web-build $(GEN_STAMP)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(APP) ./cmd/$(APP)

# Cross-compile for every platform the image ships (linux amd64 and arm64).
# Pure Go (modernc sqlite), so no toolchain beyond `go` and no emulation.
dist: dist/$(APP)-linux-amd64 dist/$(APP)-linux-arm64
dist/$(APP)-linux-%: web-build $(GEN_STAMP)
	CGO_ENABLED=0 GOOS=linux GOARCH=$* $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $@ ./cmd/$(APP)

## ---- quality ----

lint: $(GEN_STAMP) web/node_modules
	$(GO) vet -tags dev $(GO_PKGS)
	cd web && $(BUN) run lint

typecheck: $(GEN_STAMP) web/node_modules
	$(GO) build -tags dev $(GO_PKGS)
	cd web && $(BUN) run typecheck

fmt:
	gofmt -w ./cmd ./internal ./config

test: test-go test-web

test-go: $(GEN_STAMP)
	$(GO) test -race -tags dev $(GO_PKGS)

test-web: web/node_modules
	cd web && $(BUN) run test

# Playwright against a running server (BASE_URL). `make e2e-install` once.
e2e: e2e/node_modules
	cd e2e && BASE_URL=$${BASE_URL:-http://localhost:24380} $(BUN) run test

e2e/node_modules: e2e/package.json e2e/bun.lock
	cd e2e && $(BUN) install --frozen-lockfile
	@touch e2e/node_modules

e2e-install: e2e/node_modules
	cd e2e && $(BUN) x playwright install --with-deps chromium

## ---- containers and deploy ----

# Multi-arch image from the dist/ binaries. Loads into the local docker daemon
# for the host arch only unless PUSH=1.
TAG ?= dev
image: dist
	docker buildx build --platform linux/amd64,linux/arm64 -t $(IMAGE):$(TAG) $(if $(PUSH),--push,--load --platform linux/$(shell $(GO) env GOARCH)) .

# Render the k3s manifests. IMAGE_TAG (env) selects the image tag; release.yml
# sets it to the bare semver so the artifact and the image share a version.
MANIFEST_DIR := deploy/out/k3s/manifests
manifests: devops/k3s/PklProject.deps.json
	mkdir -p $(MANIFEST_DIR)
	$(PKL) eval --project-dir devops/k3s -o $(MANIFEST_DIR)/$(APP).yaml devops/k3s/$(APP).pkl
	@echo "wrote $(MANIFEST_DIR)/$(APP).yaml (image tag: $${IMAGE_TAG:-main})"

# Render what Flux never owns: namespace, ServiceAccount, Secret. A human applies.
bootstrap:
	bash deploy/secrets.sh

# Push the rendered manifests as a Flux OCI artifact (what release.yml does).
OCI_REPO ?= oci://ghcr.io/bmurray/$(APP)-manifests
OCI_TAG  ?= $(patsubst v%,%,$(VERSION))
oci-push: manifests
	flux push artifact $(OCI_REPO):$(OCI_TAG) --path $(MANIFEST_DIR) \
		--source="$$(git config --get remote.origin.url)" \
		--revision="$$(git branch --show-current)@sha1:$$(git rev-parse HEAD)"

clean:
	rm -rf bin dist web/dist deploy/out internal/gen/.stamp
