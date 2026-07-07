# Rexion-plugin-sheet

An MCP stdio server that gives [Rexion](../../README.md) spreadsheet (xlsx/csv) capabilities: read, write, query (SQL-like), and chart.

Part of the [Personal Agent roadmap](../../docs/personal-agent-roadmap.md) — Phase 1. Zero侵入: it is a standalone binary wired in via `Rexion.toml`; the Rexion core never imports it.

## Tools

All four tools surface as `mcp__sheet__<name>` once the plugin is configured.

| Tool | Mode | Description |
|------|------|-------------|
| `read_sheet` | read-only | Read rows from xlsx/csv; returns Markdown/JSON/CSV; capped by `max_rows` (default 1000) to bound tokens |
| `write_sheet` | read-write | Write rows to xlsx/csv; supports `overwrite` and `append` modes; accepts 2D array or Markdown table |
| `query_sheet` | read-only | Run a SQL-like query (WHERE / GROUP BY / ORDER BY / LIMIT + SUM/COUNT/AVG/MIN/MAX) without loading full table into context |
| `chart_sheet` | read-only | Render a bar/line/pie/scatter chart and return a base64 PNG |

Read-only tools declare `readOnlyHint`, so the agent batches them in parallel and the permission layer auto-allows them.

## Configuration

Add to `Rexion.toml`:

```toml
[[plugins]]
name    = "sheet"
command = "Rexion-plugin-sheet"
# Optional: restrict which tools are visible
# tools = ["read_sheet", "query_sheet"]   # read-only profile
```

Rexion then surfaces the four tools under the `mcp__sheet__` namespace.

## Usage examples (from `Rexion chat`)

```
> read /abs/path/sales.xlsx and tell me the top 3 regions by revenue
  → agent calls read_sheet or query_sheet(GROUP BY region, ORDER BY total DESC)

> draw a bar chart of monthly sales from data.xlsx
  → agent calls chart_sheet(type=bar, x=month, y=sales)

> append a row [华南, 9999] to regions.xlsx
  → agent calls write_sheet(mode=append)
```

### query_sheet WHERE syntax

```
region='华东' AND amount>1000
region IN ('华东','华北')
(region='华北' OR amount>5000) AND status='active'
```

Operators: `=` `!=` `>` `>=` `<` `<=` and `IN (...)`. Logic: `AND` `OR` and parentheses.

### Aggregates

```jsonc
{
  "select": ["region", "SUM(amount) as total", "COUNT(*) as cnt"],
  "group_by": ["region"],
  "order_by": "total DESC",
  "limit": 50
}
```

Supported: `SUM` `COUNT` `AVG` `MIN` `MAX`.

## Build

```sh
go build ./cmd/Rexion-plugin-sheet/     # → Rexion-plugin-sheet(.exe)
```

Pure Go, `CGO_ENABLED=0`, single binary — same distribution story as Rexion itself.

## Dependencies

| Dependency | Purpose | Notes |
|------------|---------|-------|
| `github.com/xuri/excelize/v2` | xlsx read/write | Pure Go |
| `github.com/wcharczuk/go-chart/v2` | chart rendering (PNG) | Pure Go |
| stdlib `encoding/csv` | CSV read/write | |

No external system dependencies (no pandoc/libreoffice needed).

## Design notes

- **Token safety**: `read_sheet` defaults to `max_rows=1000`; `query_sheet` aggregates server-side and returns only the result set. Large sheets never flood the model context.
- **Round-trip**: `write_sheet` accepts both 2D arrays (JSON) and Markdown tables, so the agent can write back what it read.
- **Image delivery**: `chart_sheet` returns a base64 PNG as an MCP image content block; the desktop frontend renders it via the existing `mediaTokenStore` path.
- **Error model**: handler errors become `isError: true` content results (in-band, model-visible for recovery); protocol errors (bad params, unknown tool) become JSON-RPC errors.

## Testing

```sh
go test ./cmd/Rexion-plugin-sheet/ -v
```

Covers: cell-name parsing, range filtering, Markdown/CSV round-trip, WHERE parser (AND/OR/IN/parentheses), aggregates, and a full MCP protocol end-to-end test driving `serve()` over a pipe.

## Status

Phase 1 of the [Personal Agent roadmap](../../docs/personal-agent-roadmap.md). Phase 2 wraps these tools in a `sheet-analysis` Skill for end-to-end workflows.
