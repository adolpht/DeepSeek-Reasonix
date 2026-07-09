VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOEXE := $(shell go env GOEXE)

# CodeGraph release pinned for the bundled MCP server / e2e test. Bump together
# with any change to the integration in internal/codegraph.
CODEGRAPH_VERSION := v0.9.7

# Main-module plugins (built from the root go.mod)
MAIN_PLUGINS := rexon-plugin-office rexon-plugin-sheet rexon-plugin-mail rexon-plugin-im rexon-plugin-dws

# Standalone-module plugins (each has its own go.mod under cmd/)
STANDALONE_PLUGINS := rexon-plugin-calendar rexon-plugin-slides rexon-plugin-search

ALL_PLUGINS := $(MAIN_PLUGINS) $(STANDALONE_PLUGINS)

.PHONY: build plugins vet fmt test hooks cross clean e2e-codegraph

build: plugins
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/rexion$(GOEXE) ./cmd/rexion

plugins: $(foreach p,$(ALL_PLUGINS),bin/$p$(GOEXE))

# Main-module plugins: build from root module
$(foreach p,$(MAIN_PLUGINS),bin/$p$(GOEXE)):
	@mkdir -p bin
	$(eval PLUGIN := $(notdir $(patsubst %$(GOEXE),%,$@)))
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/$(PLUGIN)

# Standalone-module plugins: build from their own module directory
$(foreach p,$(STANDALONE_PLUGINS),bin/$p$(GOEXE)):
	@mkdir -p bin
	$(eval PLUGIN := $(notdir $(patsubst %$(GOEXE),%,$@)))
	cd cmd/$(PLUGIN) && go build -o ../../$@ .

vet:
	go vet ./...

fmt:
	gofmt -w .

test:
	go test ./...

hooks:
	@git config core.hooksPath .githooks
	@echo "installed: core.hooksPath -> .githooks (pre-push runs go vet)"

cross:
	@mkdir -p dist
	@for p in darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64; do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "build $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o dist/rexion-$$os-$$arch$$ext ./cmd/rexion; \
	done

clean:
	rm -rf bin dist

# Fetch the matching CodeGraph bundle into bin/codegraph/ (the distribution
# layout: launcher at bin/codegraph/bin/codegraph beside bin/rexion) and run the
# gated MCP end-to-end test against it. Requires `gh`. Windows: install via the
# upstream install.ps1 and run the test with REXION_CODEGRAPH_BIN set.
e2e-codegraph:
	@os=$$(uname -s | tr 'A-Z' 'a-z'); arch=$$(uname -m); \
	case $$arch in arm64|aarch64) arch=arm64;; x86_64|amd64) arch=x64;; *) echo "unsupported arch $$arch"; exit 1;; esac; \
	asset=codegraph-$$os-$$arch.tar.gz; dest=bin/codegraph; \
	echo "fetching $$asset ($(CODEGRAPH_VERSION)) -> $$dest"; \
	rm -rf $$dest && mkdir -p $$dest; \
	gh release download $(CODEGRAPH_VERSION) -R colbymchenry/codegraph -p $$asset -O /tmp/$$asset; \
	tar -xzf /tmp/$$asset -C $$dest --strip-components=1; \
	REXION_CODEGRAPH_E2E=1 REXION_CODEGRAPH_BIN=$$PWD/$$dest/bin/codegraph \
		go test ./internal/codegraph/ -run E2E -v -count=1
