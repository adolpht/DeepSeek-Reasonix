---
name: minimax-docx
description: "专业 DOCX 文档创建、编辑和排版 — MCP 工具 + Python 脚本驱动，支持创建/编辑/套模板三条流水线。触发词：Word, docx, 文档, 报告, 合同, 公文, 排版, 套模板"
runAs: subagent
allowed-tools: read_file, ls, glob, grep, bash, write_file, write_docx, mcp__office__write_docx, mcp__office__read_docx
license: MIT
metadata:
  version: "3.0.0"
  category: document-processing
  author: MiniMaxAI
triggers:
  - Word
  - docx
  - document
  - 文档
  - Word文档
  - 报告
  - 合同
  - 公文
  - 排版
  - 套模板
---

# minimax-docx

Create, edit, and format DOCX documents. Uses MCP tools as the primary path, Python scripts for advanced operations — no .NET SDK or third-party dependencies required.

**Skill directory:** `SKILL_DIR` expands to this skill's absolute path at runtime.

## Execution Paths (Priority Order)

### Path 1: MCP Tool (ALWAYS available, PREFERRED)

The `write_docx` / `mcp__office__write_docx` tool handles most document operations without any external dependencies. Use this as the DEFAULT path.

**Capabilities:**
- Create new documents with paragraphs, tables, headers/footers, images, page numbers
- Apply styles (Heading 1-9, Normal, Title, TOC, custom styles)
- Set fonts, sizes, colors, bold/italic/underline
- Create tables with borders, merged cells, shading
- Add headers/footers with page numbers
- Insert images (inline and floating)
- Set page margins, orientation, page size
- Add section breaks, columns
- Add table of contents
- Track changes, comments, footnotes/endnotes

**When to use:** All document creation, basic-to-moderate editing, and formatting tasks.

### Path 2: Python Scripts (ALWAYS available, for advanced operations)

Python utility scripts under `SKILL_DIR/scripts/` handle advanced DOCX operations that MCP tools cannot do. These use **only Python standard library** (zipfile + xml.etree.ElementTree) — no pip install needed.

**Setup check:** Verify Python 3: `python3 --version`

**When to use:** Complex styling, table formatting, aesthetic recipes, field insertion, image operations, document validation — operations requiring fine-grained OpenXML control.

### Path 3: Python + zipfile (manual XML editing)

For tasks requiring direct XML manipulation of an existing DOCX file (unzip → edit XML → rezip), use Python standard library directly:

```bash
# Unzip DOCX for XML editing
python3 -c "import zipfile; zipfile.ZipFile('input.docx').extractall('/tmp/docx_work/')"

# Read/modify XML files directly
# Key files: word/document.xml, word/styles.xml, word/header1.xml, word/footer1.xml

# Repack into DOCX
python3 -c "
import zipfile, os
with zipfile.ZipFile('output.docx', 'w', zipfile.ZIP_DEFLATED) as z:
    for root, dirs, files in os.walk('/tmp/docx_work/'):
        for f in files:
            fp = os.path.join(root, f)
            z.write(fp, os.path.relpath(fp, '/tmp/docx_work/'))
"
```

**When to use:** Precise XML-level edits on existing documents (fix element ordering, merge styles.xml, adjust sectPr, etc.)

## Pipeline Routing

Route by checking: does the user have an input .docx file?

```
User task
├─ No input file → Pipeline A: CREATE
│   signals: "write", "create", "draft", "generate", "new", "make a report/proposal/memo"
│   → Read references/scenario_a_create.md
│
└─ Has input .docx
    ├─ Replace/fill/modify content → Pipeline B: FILL-EDIT
    │   signals: "fill in", "replace", "update", "change text", "add section", "edit"
    │   → Read references/scenario_b_edit_content.md
    │
    └─ Reformat/apply style/template → Pipeline C: FORMAT-APPLY
        signals: "reformat", "apply template", "restyle", "match this format", "套模板", "排版"
        → Read references/scenario_c_apply_template.md
```

## Pipeline A: Create (Primary Path — MCP Tool)

Read `references/scenario_a_create.md` and `references/design_principles.md` first.

**Step 1: Choose an aesthetic recipe** from `references/typography_guide.md`.

**Step 2: Build the document** using `write_docx` / `mcp__office__write_docx`:

```
write_docx({
  path: "output.docx",
  title: "Document Title",
  content: [
    { type: "heading", level: 1, text: "Chapter 1" },
    { type: "paragraph", text: "Body text here..." },
    { type: "table", headers: ["A", "B"], rows: [["1", "2"]] },
    { type: "page_break" },
    ...
  ],
  styles: { ... },
  page_setup: { margins: { top: 1440, bottom: 1440, left: 1440, right: 1440 } }
})
```

**Step 3 (optional): Apply aesthetic recipe via Python script:**
```bash
python3 SKILL_DIR/scripts/docx_aesthetic.py output.docx --recipe academic-thesis --output styled.docx
```

**Step 4 (optional): Add TOC, fields, headers via Python scripts:**
```bash
python3 SKILL_DIR/scripts/docx_fields.py styled.docx --add-toc '{"position":0,"levels":3}' --output final.docx
python3 SKILL_DIR/scripts/docx_headers_footers.py final.docx --page-numbers center --output final.docx
python3 SKILL_DIR/scripts/docx_validate.py final.docx
```

**Step 5: Preview** (if Python available):
```bash
bash SKILL_DIR/scripts/docx_preview.sh final.docx
```

## Pipeline B: Edit / Fill

Read `references/scenario_b_edit_content.md` first.

**Step 1: Read existing document** using `mcp__office__read_docx`:
```
mcp__office__read_docx({ path: "input.docx" })
```

**Step 2: For simple edits** (text replacement, placeholder fill), use `write_docx` to write modified content.

**Step 3: For advanced edits**, use Python scripts:
```bash
# Add tables
python3 SKILL_DIR/scripts/docx_tables.py input.docx --add '{"rows":3,"cols":4,"style":"three-line"}' --output out.docx

# Add images
python3 SKILL_DIR/scripts/docx_images.py out.docx --add-inline '{"path":"photo.jpg","after_paragraph":5}' --output out.docx

# Add numbered lists
python3 SKILL_DIR/scripts/docx_numbering.py out.docx --add-numbered '{"items":["Step 1","Step 2"]}' --output out.docx
```

**Step 4: For XML-level edits**, use Path 3 (Python unzip/edit/rezip).

## Pipeline C: Apply Template

Read `references/scenario_c_apply_template.md` first.

**Step 1: Read both source and template** using `mcp__office__read_docx`.

**Step 2: Apply aesthetic recipe via Python script:**
```bash
# List available recipes
python3 SKILL_DIR/scripts/docx_aesthetic.py --list-recipes

# Apply a recipe
python3 SKILL_DIR/scripts/docx_aesthetic.py input.docx --recipe chinese-government --output styled.docx

# Or apply custom styles
python3 SKILL_DIR/scripts/docx_styles.py input.docx --recipe apa-7th --output styled.docx
```

**Step 3: Validate** element ordering:
```bash
python3 SKILL_DIR/scripts/docx_validate.py styled.docx
```

## Critical Rules (ALL Paths)

These prevent file corruption — OpenXML is strict about element ordering.

**Element order** (properties always first):

| Parent | Order |
|--------|-------|
| `w:p`  | `pPr` → runs |
| `w:r`  | `rPr` → `t`/`br`/`tab` |
| `w:tbl`| `tblPr` → `tblGrid` → `tr` |
| `w:tr` | `trPr` → `tc` |
| `w:tc` | `tcPr` → `p` (min 1 `<w:p/>`) |
| `w:body` | block content → `sectPr` (LAST child) |

**Font size:** `w:sz` = points × 2 (12pt → `sz="24"`). Margins/spacing in DXA (1 inch = 1440, 1cm ≈ 567).

**Heading styles MUST have OutlineLevel:** When defining heading styles, always include `OutlineLevel` — without this, Word sees them as plain styled text; TOC and navigation pane won't work.

**Direct format contamination:** When copying content from a source document, strip inline `rPr` and `pPr` — keep only `pStyle` reference and `t` text.

**Track changes:** `<w:del>` uses `<w:delText>`, never `<w:t>`. `<w:ins>` uses `<w:t>`, never `<w:delText>`.

**Multi-section headers/footers:** NEVER recreate headers/footers from scratch — copy template header/footer XML byte-for-byte.

## Python Utility Scripts

All scripts use only Python standard library. No pip install required.

### Document creation and validation

| Script | Purpose | Usage |
|--------|---------|-------|
| `docx_create.py` | Create new documents with page setup, styles, content | `python3 SKILL_DIR/scripts/docx_create.py --output doc.docx --content spec.json` |
| `docx_validate.py` | Validate document structure (11 checks) | `python3 SKILL_DIR/scripts/docx_validate.py doc.docx` |
| `docx_preview.sh` | Preview document structure | `bash SKILL_DIR/scripts/docx_preview.sh doc.docx` |

### Styling and formatting

| Script | Purpose | Usage |
|--------|---------|-------|
| `docx_styles.py` | List, add, modify styles; apply recipes | `python3 SKILL_DIR/scripts/docx_styles.py doc.docx --recipe apa-7th --output out.docx` |
| `docx_aesthetic.py` | Apply 13 aesthetic recipes | `python3 SKILL_DIR/scripts/docx_aesthetic.py doc.docx --recipe academic-thesis --output out.docx` |

### Content operations

| Script | Purpose | Usage |
|--------|---------|-------|
| `docx_tables.py` | Create/format tables (三线表, zebra, merge) | `python3 SKILL_DIR/scripts/docx_tables.py doc.docx --add '...' --output out.docx` |
| `docx_headers_footers.py` | Headers/footers, page numbers | `python3 SKILL_DIR/scripts/docx_headers_footers.py doc.docx --page-numbers center --output out.docx` |
| `docx_images.py` | Add/replace/export images | `python3 SKILL_DIR/scripts/docx_images.py doc.docx --add-inline '...' --output out.docx` |
| `docx_numbering.py` | Bullet/numbered lists, Chinese numbering | `python3 SKILL_DIR/scripts/docx_numbering.py doc.docx --add-numbered '...' --output out.docx` |
| `docx_fields.py` | TOC, bookmarks, hyperlinks, fields | `python3 SKILL_DIR/scripts/docx_fields.py doc.docx --add-toc '...' --output out.docx` |

### Conversion and setup

| Script | Purpose | Usage |
|--------|---------|-------|
| `setup.sh` / `setup.ps1` | Environment setup (optional) | `bash SKILL_DIR/scripts/setup.sh --minimal` |
| `env_check.sh` | Quick environment check | `bash SKILL_DIR/scripts/env_check.sh` |
| `doc_to_docx.sh` | Convert .doc → .docx (requires LibreOffice) | `bash SKILL_DIR/scripts/doc_to_docx.sh input.doc output/` |

### 13 Aesthetic Recipes

| Recipe | Body Font | Line Spacing | Use Case |
|--------|-----------|-------------|----------|
| `modern-corporate` | Aptos 11pt | 1.15x | Business reports |
| `academic-thesis` | Times New Roman 12pt + SimSun | 2x | Academic theses |
| `executive-brief` | Aptos 10.5pt | 1.08x | Executive summaries |
| `chinese-government` (GB/T 9704) | 方正小标宋/仿宋 | 28.9磅 | Chinese government docs |
| `minimal-modern` | Cormorant Garamond 11pt | 1.5x | Creative writing |
| `ieee` | Times New Roman 10pt | 1x | IEEE papers |
| `apa-7th` | Aptos/Calibri 12pt | 2x | APA 7th edition |
| `mla-9th` | Times New Roman 12pt | 2x | MLA 9th edition |
| `chicago` | Times New Roman 12pt | 2x | Chicago style |
| `springer-lncs` | Computer Modern 10pt | 1x | LNCS papers |
| `nature` | Arial 8pt | 1x | Nature journal |
| `hbr` | Georgia 11pt | 1.4x | Harvard Business Review |
| `chinese-university-thesis` | 黑体/宋体 | 1.5x | Chinese university thesis |

## Pre-processing (Optional)

- Convert `.doc` → `.docx`: `bash SKILL_DIR/scripts/doc_to_docx.sh input.doc output_dir/`
- Environment check: `bash SKILL_DIR/scripts/env_check.sh`

## References

Load as needed — don't load all at once. Pick the most relevant files for the task.

### Scenario guides (read first for each pipeline)

| File | When |
|------|------|
| `references/scenario_a_create.md` | Pipeline A: creating from scratch |
| `references/scenario_b_edit_content.md` | Pipeline B: editing existing content |
| `references/scenario_c_apply_template.md` | Pipeline C: applying template formatting |

### Design and typography

| File | When |
|------|------|
| `references/typography_guide.md` | Font pairing, sizes, spacing, page layout |
| `references/cjk_typography.md` | CJK fonts, 字号 sizes, GB/T 9704 公文 standard |
| `references/design_principles.md` | Aesthetic foundations: white space, contrast, hierarchy |
| `references/design_good_bad_examples.md` | Good vs Bad typography comparisons |

### OpenXML technical reference

| File | When |
|------|------|
| `references/openxml_element_order.md` | XML element ordering rules (prevents corruption) |
| `references/openxml_units.md` | Unit conversion: DXA, EMU, half-points |
| `references/openxml_namespaces.md` | Namespace declarations |
| `references/openxml_encyclopedia_part1.md` | Document creation, styles, character & paragraph formatting |
| `references/openxml_encyclopedia_part2.md` | Page setup, tables, headers/footers, sections |
| `references/openxml_encyclopedia_part3.md` | TOC, footnotes, fields, track changes, images |

### CJK and templates

| File | When |
|------|------|
| `references/cjk_university_template_guide.md` | Chinese university thesis templates |
| `references/track_changes_guide.md` | Revision marks deep dive |
| `references/troubleshooting.md` | Symptom-driven fixes (13 common problems) |

### C# code samples (for reference only — Path 2 Python scripts are preferred)

These are available under `SKILL_DIR/Samples/` as additional reference material. The Python scripts above cover all the same functionality and are the recommended approach.

| File | Topic |
|------|-------|
| `SKILL_DIR/Samples/DocumentCreationSamples.cs` | Document lifecycle, page setup, multi-section |
| `SKILL_DIR/Samples/StyleSystemSamples.cs` | Styles: Heading chain, CJK 公文, APA 7th |
| `SKILL_DIR/Samples/CharacterFormattingSamples.cs` | RunProperties: fonts, size, bold/italic, color |
| `SKILL_DIR/Samples/ParagraphFormattingSamples.cs` | ParagraphProperties: justification, spacing, tabs |
| `SKILL_DIR/Samples/TableSamples.cs` | Tables: borders, merge, 三线表, zebra |
| `SKILL_DIR/Samples/HeaderFooterSamples.cs` | Headers/footers: page numbers, per-section |
| `SKILL_DIR/Samples/ImageSamples.cs` | Images: inline, floating, text wrapping |
| `SKILL_DIR/Samples/ListAndNumberingSamples.cs` | Numbering: bullets, Chinese 一/（一）/1./(1) |
| `SKILL_DIR/Samples/FieldAndTocSamples.cs` | Fields: TOC, DATE/PAGE/REF/SEQ |
| `SKILL_DIR/Samples/FootnoteAndCommentSamples.cs` | Footnotes, endnotes, comments, bookmarks |
| `SKILL_DIR/Samples/TrackChangesSamples.cs` | Revisions: insertions, deletions |
| `SKILL_DIR/Samples/AestheticRecipeSamples.cs` | 13 aesthetic recipes with exact values |

### XSD validation

| File | When |
|------|------|
| `SKILL_DIR/assets/xsd/wml-subset.xsd` | Structural validation (element ordering) |
| `SKILL_DIR/assets/xsd/business-rules.xsd` | Business rules validation |
