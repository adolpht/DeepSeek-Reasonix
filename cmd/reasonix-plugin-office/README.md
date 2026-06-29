# reasonix-plugin-office

An MCP stdio server that gives [Reasonix](../../README.md) document capabilities: read `.docx`, write `.docx`, render text templates, and (when pandoc is installed) convert Markdown to PDF.

Part of the [Personal Agent roadmap](../../docs/personal-agent-roadmap.md) — Phase 3. Zero-invasive: a standalone binary wired in via `reasonix.toml`; the Reasonix core never imports it.

## Tools

All tools surface as `mcp__office__<name>` once the plugin is configured.

| Tool | Mode | Description |
|------|------|-------------|
| `read_docx` | read-only | Read a `.docx` and return Markdown (headings, paragraphs, tables). `mode=outline` returns headings only to save tokens. `max_chars` caps output (default 20000). |
| `write_docx` | read-write | Write Markdown to a `.docx`. Headings (`#`/`##`/`###`) and pipe tables become native Word styles; other text becomes plain paragraphs. Overwrites existing files. |
| `render_template` | read-only | Render a Go `text/template` against a variables map. Use `template_path` (file) or `template` (inline source). Returns the rendered text. |
| `md_to_pdf` | read-write | Convert Markdown (or any pandoc-readable file) to PDF via the external `pandoc` binary. **Only registered when pandoc is found on PATH at startup.** |

Read-only tools declare `readOnlyHint`, so the agent batches them in parallel and the permission layer auto-allows them.

## Configuration

Add to `reasonix.toml`:

```toml
[[plugins]]
name    = "office"
command = "reasonix-plugin-office"
# Optional: restrict which tools are visible
# tools = ["read_docx", "render_template"]   # read-only profile
```

Reasonix then surfaces the tools under the `mcp__office__` namespace. If `pandoc` is not installed, `md_to_pdf` is silently omitted from `tools/list` — the agent never sees a tool it can't call.

## Usage examples (from `reasonix chat`)

```
> read the meeting notes in /abs/path/notes.docx and summarize the action items
  → agent calls read_docx(mode=outline) for the agenda, then read_docx(mode=full) for body

> draft a weekly report from this markdown and save it as report.docx
  → agent calls write_docx(path=..., content="# Weekly Report\n\n...")

> fill in the Q1 review template with these numbers
  → agent calls render_template(template_path=templates/q1.tmpl, variables={revenue: ..., growth: ...})

> convert this markdown to PDF with 1-inch margins
  → agent calls md_to_pdf(markdown="...", output=report.pdf, extra_args=["-V","geometry:margin=1in"])
```

### read_docx modes

- **`full`** (default): headings + paragraphs + tables, as Markdown.
- **`outline`**: headings only — best for skimming a long document's structure before deciding which sections to read in full.

### render_template syntax

Standard Go `text/template` (https://pkg.go.dev/text/template):

```
Hello {{.name}}, you have {{len .tasks}} tasks:
{{- range .tasks}}
- {{.}}
{{- end}}
Status: {{if .done}}complete{{else}}pending{{end}}
```

`missingkey=error` is set, so referencing an undefined variable fails loudly instead of silently emitting `<no value>`.

### md_to_pdf engines

`pdf_engine` is passed to `pandoc --pdf-engine`. Common values:

| Engine | Install | Notes |
|--------|---------|------|
| `pdflatex` | TeX Live / MiKTeX | Default; high-quality typesetting |
| `wkhtmltopdf` | wkhtmltopdf.org | HTML/CSS-based; lighter than LaTeX |
| `weasyprint` | `pip install weasyprint` | Modern HTML/CSS |
| `tectonic` | tectonic-typesetting.github.io | Self-contained LaTeX alternative |

If omitted, pandoc picks its default (usually `pdflatex`).

## Build

```sh
go build ./cmd/reasonix-plugin-office/     # → reasonix-plugin-office(.exe)
```

Pure Go, `CGO_ENABLED=0`, single binary — same distribution story as Reasonix itself. No third-party dependencies: docx read/write uses only the standard library (`archive/zip` + `encoding/xml`).

## Dependencies

| Dependency | Purpose | Notes |
|------------|---------|------|
| `archive/zip` (stdlib) | docx is a zip archive | Used for both read and write |
| `encoding/xml` (stdlib) | docx body is OOXML | Token-based parser; no schema binding |
| `text/template` (stdlib) | `render_template` | `missingkey=error` enforced |
| `pandoc` (external, optional) | `md_to_pdf` | Probed via `exec.LookPath` at startup; missing → tool unregistered |

## Implementation notes

### docx read

A `.docx` is a zip whose `word/document.xml` holds the body. We parse it with a token-based XML decoder (the body mixes `<w:p>` paragraphs and `<w:tbl>` tables as siblings) and emit Markdown. Heading depth is read from `<w:pStyle w:val="Heading1"/>` (and the Chinese equivalent `标题1`). Tables become Markdown pipe tables.

### docx write

A `.docx` is a zip of XML parts. We generate the minimal set (`[Content_Types].xml`, `_rels/.rels`, `word/document.xml`, optional `docProps/core.xml` for title metadata) so Word/WPS opens it. Markdown headings map to Heading styles; paragraphs map to `<w:p>`; tables map to `<w:tbl>`. Other Markdown inline syntax is preserved as plain text — this is not a full Markdown renderer.

### md_to_pdf

We shell out to pandoc rather than implement a PDF renderer in Go: pandoc is the de-facto standard, produces high-quality output, and supports LaTeX / wkhtmltopdf / weasyprint back-ends via `--pdf-engine`. The plugin stays a thin wrapper so users pick their own engine.

### render_template

`text/template` ships with the stdlib, so the single-binary story stays intact. The agent can compose meeting agendas, weekly reports, and similar documents without needing a heavyweight Markdown engine or yet another dependency.

## Testing

```sh
go test ./cmd/reasonix-plugin-office/...
```

Covers: docx write→read round-trip (headings/paragraphs/tables), outline mode, extension guards, template rendering (inline + file + missingkey), MCP protocol end-to-end over a pipe (initialize → tools/list → tools/call).
