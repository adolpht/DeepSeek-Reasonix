# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Rexion (formerly DeepSeek-Reasonix) is a config- and plugin-driven coding agent. It has a Go CLI (terminal TUI), a Wails desktop app (React + Go webview), an HTTP/SSE server frontend, and an ACP (Agent Communication Protocol) server for headless integration. The core architecture is transport-agnostic: every frontend drives the same `control.Controller` — the terminal TUI, the desktop webview, and the HTTP/SSE server all share identical session lifecycle, cancellation, and approval logic.

## Build & Development Commands

### CLI (root module)

```bash
make build              # Build CLI + all plugins → bin/rexion
make plugins            # Build all plugin binaries → bin/plugins/rexion-plugin-*
make test               # go test ./...
make vet                # go vet ./...
make fmt                # gofmt -w .
make hooks              # Install git hooks (pre-push runs go vet)
make cross              # Cross-compile CLI for 6 OS/arch combos → dist/
make clean              # Remove bin/ and dist/

# Build CLI only (no plugins):
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=dev" -o bin/rexion ./cmd/rexion

# Run a specific test:
go test ./internal/agent -run TestSession -v -count=1

# Race detector:
go test -race ./...

# Lint:
golangci-lint run --timeout=5m
```

### Desktop app (nested module under `desktop/`)

```bash
./dev                   # Launch desktop dev mode (Wails + Vite hot reload)
cd desktop && wails dev # Manual Wails dev
cd desktop && wails build  # Production build → build/bin/

# Frontend only (no Go side):
cd desktop/frontend && pnpm install && pnpm dev

# Desktop Go tests:
cd desktop && go test ./...
```

### Frontend (React/TS, Vite, pnpm)

```bash
cd desktop/frontend
pnpm install            # Install deps
pnpm build              # Build (includes CSS check, z-index check, typecheck, vite build)
pnpm typecheck          # tsc --noEmit
pnpm check:css          # CSS syntax + z-index token validation
pnpm test               # Math-golden test via tsx
```

### Plugins

- **Main-module plugins** (5: office, sheet, mail, im, dws) — built from root `go.mod`, output to `bin/plugins/rexion-plugin-*`
- **Standalone-module plugins** (6: calendar, slides, search, browser, design, computer) — each has its own `go.mod` under `cmd/rexion-plugin-*/`, built from that directory

### Cache hit guard (CI/release gating)

```bash
REXION_RELEASE_CACHE_GUARD=1 go test ./internal/agent -run '^TestReleaseCacheHitGuard$' -v -count=1
./scripts/cache-guard.sh
```

## Architecture

### Core assembly pipeline

`internal/boot.Build` is the composition root — the single place that turns config into a live `Controller`. Wiring flow: config → provider(s) → system prompt → tool registry (built-ins + MCP plugins + CodeGraph + LSP) → permission gate → hooks → plugin host → agent pool → `agent.Agent` (optionally wrapped in two-model `Coordinator`) → `control.Controller`. No DI framework; all wiring is explicit via constructor arguments. Frontends pass only a sink and run knobs; everything else comes from config.

### Key packages and their roles

| Package | Role |
|---------|------|
| `internal/cli` | Subcommand routing, flag parsing, entry point (`cli.Run`) |
| `internal/boot` | Composition root: config → Controller (one place, shared by all frontends) |
| `internal/control` | Transport-agnostic session driver. Owns the run loop, takes commands (Send/Cancel/Approve/SetPlanMode), emits typed events to `event.Sink`. ~2800 lines |
| `internal/agent` | LLM loop executor. Streaming, tool dispatch, context compaction, retry, session persistence (SQLite/JSONL), multi-agent spawn/pool |
| `internal/agent.Coordinator` | Two-model collaboration: planner (read-only) produces a plan → executor (full writer access) carries it out. Both satisfy `agent.Runner`, so Controller is agnostic |
| `internal/agent.Pool` | Multi-agent orchestration. `spawn_agent`, `wait_agent`, `send_input`, `close_agent` tools. Child agents run isolated loops (max depth 1) |
| `internal/provider` | Model-backend abstraction + factory registry. Subpackages (`anthropic`, `openai`) self-register via `init()` |
| `internal/plugin` | MCP client. Connects external MCP servers (stdio/HTTP/SSE), adapts their tools to `tool.Tool`. Startup tiers: eager/lazy/background |
| `internal/tool` | `Tool` interface + `Registry`. Built-ins self-register via `init()`; plugin tools namespaced `mcp__<server>__<tool>` |
| `internal/skill` | Playbook loader from Markdown. Scopes: project > custom > global > builtin. Convention dirs: `.rexion`, `.agents`, `.agent`, `.claude` |
| `internal/workflow` | DAG-based workflow engine. Nodes: skill/prompt/tool/condition/parallel. Triggers: manual/cron/event. Topological sort execution |
| `internal/config` | TOML config loader. Resolution: flag > `./Rexion.toml` > `~/.config/Rexion/config.toml` > defaults. Secrets via `api_key_env` |
| `internal/sandbox` | OS-level confinement for bash (Seatbelt macOS, bwrap+seccomp Linux, job object Windows, unwrapped fallback) |
| `internal/acp` | ACP server (NDJSON JSON-RPC 2.0 over stdin/stdout) for IDE/headless integration |
| `internal/mcpserver` | Rexion-as-MCP-server: exposes `Rexion_code`/`Rexion_ask` tools |
| `internal/serve` | HTTP frontend: SSE event stream + JSON POST command endpoints. Broadcaster pub/sub for multiple tabs |
| `internal/event` | ~30 typed event kinds (TurnStarted, Text, Reasoning, ToolDispatch, ToolResult, ApprovalRequest, etc.) |
| `internal/memory` | Hierarchical doc memory + auto-memory store + PKM |
| `internal/permission` | Policy (allow/ask/deny rules) + Gate (runtime enforcement + optional interactive approver) |
| `internal/evidence` | Tool receipt ledger for readiness checks (Basic/Verified/MergeReady) |
| `internal/i18n` | Internationalization (en + zh, auto-detect from `$LANG`/`$REXION_LANG`) |
| `internal/hook` | Shell-command hooks around the agent loop (PreToolUse, PostToolUse, PostLLMCall, etc.) |

### Frontend architecture (desktop)

The desktop app is a **nested Go module** (`desktop/go.mod`, `replace rexion => ../`) that imports the same `rexion/internal/*` kernel. This separation keeps the CGO/WebKit build away from the CLI's `CGO_ENABLED=0` static-binary guarantee. The parent module's `go build/test ./...` skips `desktop/`.

```
webview (React + TS, Vite)
  bridge.ts ──calls──▶ App.{Submit,Cancel,…}
  bridge.ts ◀─events── runtime.EventsOn("agent:event")
       │                        │
desktop/app.go  App (bound)  +  eventSink
       │                        │
internal/boot.Build → internal/control.Controller (kernel)
```

**Editor seam**: `CodeViewer.tsx` and `DiffView.tsx` use lazy-loaded editor implementations. Swap the lazy import to upgrade from `PlainCode`/`PlainDiff` to Monaco or CodeMirror.

**Wire contract**: `desktop/wire.go` mirrors `internal/serve/wire.go`; `frontend/src/lib/types.ts` mirrors both. Keep all three in sync when changing event shapes.

**Desktop multi-tab**: Each tab builds its own Controller via `boot.Build` with `WorkspaceRoot` set to the project dir. The IM plugin watcher runs at the app level, not per-tab.

### Key design patterns

- **Interface-driven seams**: `Provider`, `Tool`, `Sink`, `Gate`, `Asker`, `Runner`, `transport` — packages stay independent, agent never imports CLI
- **Factory self-registration**: Providers and tools register via `init()` — core oblivious to concrete implementations, no import cycles
- **Event stream (observer)**: Agent emits typed `Event`s; frontends subscribe via `Sink` and render. Decouples "what happened" from "how to show it"
- **Coordinator/Strategy**: When `planner_model` configured, `Coordinator` wraps executor — planner + executor both satisfy `Runner`
- **Gate/Policy**: `Policy` is pure rules; `Gate` adds runtime enforcement + optional interactive approver (headless runs skip approval)
- **Tiered plugin startup**: eager (blocks boot) / lazy (first-use spawn + schema cache) / background (async goroutine)

### Config convention directories

Skills and instructions are discovered from multiple convention directories: `.rexion`, `.agents`, `.agent`, `.claude` — under both the project root and home dir. This allows skills authored for other agent tools to migrate in unchanged.

## Configuration

- Config format: TOML
- Config resolution: flag > `./Rexion.toml` > `~/.config/Rexion/config.toml` > defaults
- Secrets: environment variables via `api_key_env` (never stored in config files)
- `.env.example` has `DEEPSEEK_API_KEY` and `MIMO_API_KEY`

## CI

CI runs on `main-v2` branch push/PR (`.github/workflows/ci.yml`):
- **test**: 3-platform matrix (Ubuntu/macOS/Windows) — gofmt, vet, build, `go test ./...`
- **race**: Ubuntu-only — `go test -race ./...`
- **desktop**: Ubuntu-22.04 — Wails build, vet, test for desktop module
- **lint**: golangci-lint v2.12.2 (errcheck, govet, ineffassign, staticcheck, unused)
- **govulncheck**: informational, non-blocking
- **coverage**: coverage report upload

Release pipelines: `v*` tags → CLI release (GoReleaser + npm); `desktop-v*` tags → desktop release (native platform builds).

Pre-push hook: `go vet ./...` (install with `make hooks`).

## Key Conventions

- Module name in Go code: `rexion` (import paths are `rexion/internal/...`)
- Go 1.25.0 / toolchain go1.26.4
- CLI must stay `CGO_ENABLED=0` — no CGO in the root module
- Desktop module uses CGO (WebKit/Wails) and is intentionally isolated
- Built-in tools and providers self-register via `init()` — blank imports in `main.go` wire them
- All frontends share `internal/boot.Build` and `control.Controller` — never duplicate session logic
- Event stream is typed (`event.Event` types) — frontends render events, never re-derive state
- Plugins use MCP protocol (JSON-RPC 2.0, stdio/HTTP/SSE transports)
- Windows-specific code uses `_windows.go` suffix; other platforms use `_other.go` or `_darwin.go`/`_unix.go`
- Desktop frontend uses pnpm (not npm/yarn)
