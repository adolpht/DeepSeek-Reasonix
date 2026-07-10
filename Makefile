VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOEXE := $(shell go env GOEXE)

# CodeGraph release pinned for the bundled MCP server / e2e test. Bump together
# with any change to the integration in internal/codegraph.
CODEGRAPH_VERSION := v0.9.7

# Main-module plugins (built from the root go.mod)
# Source dirs are cmd/rexon-plugin-* but output is renamed to rexion-plugin-*
# for consistent naming across the NSIS installer and plugin discovery.
MAIN_PLUGINS_SRC := rexon-plugin-office rexon-plugin-sheet rexon-plugin-mail rexon-plugin-im rexon-plugin-dws
MAIN_PLUGINS_OUT := rexion-plugin-office rexion-plugin-sheet rexion-plugin-mail rexion-plugin-im rexion-plugin-dws

# Standalone-module plugins (each has its own go.mod under cmd/)
STANDALONE_PLUGINS_SRC := rexon-plugin-calendar rexon-plugin-slides rexon-plugin-search
STANDALONE_PLUGINS_OUT := rexion-plugin-calendar rexion-plugin-slides rexion-plugin-search

ALL_PLUGINS := $(MAIN_PLUGINS_OUT) $(STANDALONE_PLUGINS_OUT)

.PHONY: build plugins vet fmt test hooks cross clean e2e-codegraph

build: plugins
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/rexion$(GOEXE) ./cmd/rexion

plugins: $(foreach p,$(ALL_PLUGINS),bin/plugins/$p$(GOEXE))

# Main-module plugins: build from root module, output to bin/plugins/rexion-plugin-*
$(foreach p,$(MAIN_PLUGINS_OUT),bin/plugins/$p$(GOEXE)):
	@mkdir -p bin/plugins
	$(eval IDX := $(words $(filter $(MAIN_PLUGINS_OUT),$(patsubst bin/plugins/%$(GOEXE),%,$@))))
	$(eval SRC := $(word $(IDX),$(MAIN_PLUGINS_SRC)))
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/$(SRC)

# Standalone-module plugins: build from their own module directory
$(foreach p,$(STANDALONE_PLUGINS_OUT),bin/plugins/$p$(GOEXE)):
	@mkdir -p bin/plugins
	$(eval IDX := $(words $(filter $(STANDALONE_PLUGINS_OUT),$(patsubst bin/plugins/%$(GOEXE),%,$@))))
	$(eval SRC := $(word $(IDX),$(STANDALONE_PLUGINS_SRC)))
	cd cmd/$(SRC) && go build -o ../../$@ .

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
