---
name: minimax-pdf
description: "专业 PDF 生成、表单填充和重排版 — MCP 工具驱动，支持 Markdown 转 PDF、封面设计、表单填充"
runAs: subagent
allowed-tools: read_file, ls, glob, grep, bash, write_file, write_pdf, mcp__office__md_to_pdf
license: MIT
metadata:
  version: "2.0"
  category: document-generation
  author: MiniMaxAI
---

# minimax-pdf

Three tasks. One skill.

## Execution Paths (Priority Order)

### Path 1: MCP Tools (ALWAYS available, PREFERRED)

The `write_pdf` / `mcp__office__md_to_pdf` tool handles PDF generation without any external dependencies. Use this as the DEFAULT path.

**Capabilities:**
- Convert Markdown content to professional PDF
- Apply document styles, fonts, colors
- Generate tables, code blocks, images
- Page numbers, headers/footers
- Multiple output formats and paper sizes

**When to use:** All PDF generation from content (reports, proposals, resumes, etc.)

### Path 2: Python Scripts (for advanced PDF operations)

For tasks requiring direct PDF manipulation (form filling, merging, reformatting), use the Python scripts under `SKILL_DIR/scripts/`. These may require additional Python packages (reportlab, pypdf).

**Setup check:** Verify Python 3.9+ and required packages: `bash SKILL_DIR/scripts/make.sh check`. If packages are missing, install with `bash SKILL_DIR/scripts/make.sh fix`.

**When to use:** Form filling, PDF merging, reformatting existing documents, custom cover page rendering.

---

## Route Table

| User intent | Primary Path | Fallback |
|---|---|---|
| Generate a new PDF from scratch | **Path 1**: MCP `write_pdf` / `md_to_pdf` | Path 2: Python pipeline |
| Fill form fields in existing PDF | **Path 2**: `fill_inspect.py` → `fill_write.py` | Manual field-by-field edit |
| Reformat / re-style existing document | **Path 2**: `reformat_parse.py` → pipeline | Path 1: Re-create from content |

**Rule:** when in doubt between CREATE and REFORMAT, ask whether the user has an existing document to start from. If yes → REFORMAT. If no → CREATE.

---

## Route A: CREATE (Primary — MCP Tool)

Generate a new PDF from Markdown content.

**Step 1:** Prepare content as Markdown, choosing a document type for visual style.

**Step 2:** Use MCP tool:
```
write_pdf({
  path: "output.pdf",
  content: "# Report Title\n\n## Section 1\n\nBody text...",
  style: "report",
  accent_color: "#2D5F8A"
})
```

Or:
```
mcp__office__md_to_pdf({
  path: "output.pdf",
  markdown: "# Report Title\n\n## Section 1\n\nBody text..."
})
```

**Doc types / styles:** `report` · `proposal` · `resume` · `portfolio` · `academic` · `general` · `minimal`

**Accent color selection guidance:**

| Context | Suggested accent range |
|---|---|
| Legal / compliance / finance | Deep navy `#1C3A5E`, charcoal `#2E3440` |
| Healthcare / medical | Teal-green `#2A6B5A`, cool green `#3A7D6A` |
| Technology / engineering | Steel blue `#2D5F8A`, indigo `#3D4F8A` |
| Environmental / sustainability | Forest `#2E5E3A`, olive `#4A5E2A` |
| Creative / arts / culture | Burgundy `#6B2A35`, terracotta `#8A3A2A` |
| Academic / research | Deep teal `#2A5A6B`, library blue `#2A4A6B` |
| Corporate / neutral | Slate `#3D4A5A`, graphite `#444C56` |

**Rule:** choose a color that a thoughtful designer would select for this specific document. Muted, desaturated tones work best; avoid vivid primaries.

**Advanced (Path 2, if scripts available):** Read `SKILL_DIR/design/design.md` before any CREATE or REFORMAT work for detailed design token specifications.

```bash
bash SKILL_DIR/scripts/make.sh run \
  --title "Q3 Strategy Review" --type proposal \
  --author "Strategy Team" --date "October 2025" \
  --accent "#2D5F8A" \
  --content content.json --out report.pdf
```

**content.json block types** (for Path 2):

| Block | Usage | Key fields |
|---|---|---|
| `h1` | Section heading + accent rule | `text` |
| `h2` | Subsection heading | `text` |
| `body` | Justified paragraph; supports `<b>` `<i>` | `text` |
| `bullet` | Unordered list item | `text` |
| `numbered` | Ordered list item | `text` |
| `callout` | Highlighted insight box | `text` |
| `table` | Data table | `headers`, `rows` |
| `image` | Embedded image | `path`/`src`, `caption`? |
| `code` | Monospace code block | `text`, `language`? |
| `chart` | Bar/line/pie chart | `chart_type`, `labels`, `datasets` |
| `bibliography` | Numbered reference list | `items` [{id, text}] |
| `divider` | Accent-colored rule | — |
| `pagebreak` | Force new page | — |

---

## Route B: FILL (Path 2 — Python Scripts)

Fill form fields in an existing PDF without altering layout or design.

**Requires:** Python 3.9+ + `pypdf` package.

```bash
# Step 1: inspect fields
python3 SKILL_DIR/scripts/fill_inspect.py --input form.pdf

# Step 2: fill fields
python3 SKILL_DIR/scripts/fill_write.py --input form.pdf --out filled.pdf \
  --values '{"FirstName": "Jane", "Agree": "true", "Country": "US"}'
```

| Field type | Value format |
|---|---|
| `text` | Any string |
| `checkbox` | `"true"` or `"false"` |
| `dropdown` | Must match a choice value from inspect output |
| `radio` | Must match a radio value |

Always run `fill_inspect.py` first to get exact field names.

---

## Route C: REFORMAT (Path 2 — Python Scripts)

Parse an existing document → content.json → CREATE pipeline.

**Requires:** Python 3.9+ + `pypdf` + `reportlab`.

```bash
bash SKILL_DIR/scripts/make.sh reformat \
  --input source.md --title "My Report" --type report --out output.pdf
```

**Supported input formats:** `.md` `.txt` `.pdf` `.json`

---

## Environment Check (Path 2 only)

```bash
bash SKILL_DIR/scripts/make.sh check   # verify all deps
bash SKILL_DIR/scripts/make.sh fix     # auto-install missing deps
bash SKILL_DIR/scripts/make.sh demo    # build a sample PDF
```

| Tool | Used by | Install |
|---|---|---|
| Python 3.9+ | all `.py` scripts | system |
| `reportlab` | `render_body.py` | `pip install reportlab` |
| `pypdf` | fill, merge, reformat | `pip install pypdf` |
| Node.js 18+ | `render_cover.js` | system |
| `playwright` + Chromium | `render_cover.js` | `npm install -g playwright && npx playwright install chromium` |
