GO := go
NPM := npm

WEB_SOURCES := $(shell find web/src -type f -print) web/index.html web/package.json web/package-lock.json web/tsconfig.json web/vite.config.ts
GO_SOURCES := $(shell find cmd internal sourceplugin gen -type f -name '*.go' -print) go.mod go.sum
WEB_DEPS := web/node_modules/.package-lock.json
WEB_BUILD := web/dist/generated/.build
GO_BINARY := bin/agentmetry

.PHONY: build agent-e2e

build: $(GO_BINARY)

$(WEB_DEPS): web/package.json web/package-lock.json
	$(NPM) --prefix web ci

$(WEB_BUILD): $(WEB_DEPS) $(WEB_SOURCES)
	$(NPM) --prefix web run build
	touch $@

$(GO_BINARY): $(WEB_BUILD) $(GO_SOURCES)
	mkdir -p $(@D)
	CGO_ENABLED=0 $(GO) build -trimpath -o $@ ./cmd/agentmetry

agent-e2e:
	npm --prefix evals/agentmetry install
	npm --prefix evals/agentmetry run e2e
