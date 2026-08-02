---
name: minimax-xlsx
description: "Excel 电子表格创建、读取、分析、编辑和验证 — MCP 工具驱动，支持 XML 模板创建、零格式损失编辑、公式验证、专业财务格式"
runAs: subagent
allowed-tools: read_file, ls, glob, grep, bash, write_file, write_sheet, mcp__sheet__read_sheet, mcp__sheet__write_sheet, mcp__sheet__query_sheet
license: MIT
metadata:
  version: "2.0"
  category: productivity
  author: MiniMaxAI
---

# MiniMax XLSX Skill

Handle the request directly. Do NOT spawn sub-agents. Always write the output file the user requests.

## Execution Paths (Priority Order)

### Path 1: MCP Tools (ALWAYS available, PREFERRED)

The `write_sheet` / `mcp__sheet__write_sheet`, `read_sheet` / `mcp__sheet__read_sheet`, and `query_sheet` / `mcp__sheet__query_sheet` tools handle most spreadsheet operations without any external dependencies.

**Capabilities:**
- **READ**: Read sheet data, headers, dimensions, cell values
- **CREATE**: Create new workbooks with sheets, data, formulas, formatting
- **EDIT**: Modify existing cells, add formulas, change formatting
- **QUERY**: Aggregate data (SUM, AVG, COUNT, etc.) across ranges
- **CHART**: Generate charts from data

**When to use:** All spreadsheet creation, data entry, simple-to-moderate editing, and analysis tasks.

### Path 2: Python Scripts (for XML-level operations)

For tasks requiring direct XML manipulation (unpack → edit XML → pack), use the Python scripts under `SKILL_DIR/scripts/`. These use only Python standard library — no openpyxl or pandas required.

**Setup check:** Verify Python 3 is available: `python3 --version`. Scripts are at `SKILL_DIR/scripts/`.

**When to use:** Precise XML-level edits, formula validation, row/column insertion with formula shifting, zero-format-loss editing of existing files.

### Path 3: Python + pandas (for data analysis)

For complex data analysis, use pandas (if available). For basic analysis, Path 1's `query_sheet` is sufficient.

**When to use:** Custom aggregation, statistical analysis, pivot tables, data transformation.

## Task Routing

| Task | Primary Path | Fallback |
|------|-------------|----------|
| **READ** — analyze existing data | MCP `read_sheet` / `query_sheet` | Path 2: `xlsx_reader.py` |
| **CREATE** — new xlsx from scratch | MCP `write_sheet` | Path 2: XML template |
| **EDIT** — modify existing xlsx | MCP `write_sheet` (simple) / Path 2 (XML-level) | Path 2: unpack→edit→pack |
| **FIX** — repair broken formulas | Path 2: XML unpack→fix→pack | MCP `write_sheet` to rewrite |
| **VALIDATE** — check formulas | Path 2: `formula_check.py` | Manual formula review |

## READ — Analyze data

**Primary (MCP):**
```
mcp__sheet__read_sheet({ path: "input.xlsx", sheet: "Sheet1", max_rows: 1000 })
mcp__sheet__query_sheet({ path: "input.xlsx", query: "SELECT * FROM Sheet1 WHERE A > 100" })
```

**Advanced (Python):** If `SKILL_DIR/scripts/` exists:
```bash
python3 SKILL_DIR/scripts/xlsx_reader.py input.xlsx
```

**Formatting rule**: When the user specifies decimal places (e.g. "2 decimal places"), apply that format to ALL numeric values. Never output `12875` when `12875.00` is required.

**Aggregation rule**: Always compute sums/means/counts directly from the data — never re-derive column values before aggregation.

## CREATE — New spreadsheet

**Primary (MCP):**
```
mcp__sheet__write_sheet({
  path: "output.xlsx",
  sheet: "Sheet1",
  headers: ["A", "B", "C"],
  rows: [[1, 2, 3], [4, 5, 6]],
  formulas: { "C2": "=SUM(A2:B2)" }
})
```

**Advanced (XML template):** If `SKILL_DIR/scripts/` exists, read `references/create.md` + `references/format.md`:
```bash
# Copy SKILL_DIR/templates/minimal_xlsx/ → edit XML directly → pack
python3 SKILL_DIR/scripts/xlsx_pack.py /tmp/xlsx_work/ output.xlsx
```
Every derived value MUST be an Excel formula, never a hardcoded number.

## EDIT — Modify existing spreadsheet

**Primary (MCP, for simple edits):**
```
mcp__sheet__write_sheet({ path: "input.xlsx", sheet: "Sheet1", cells: { "B3": "new value" } })
```

**Advanced (XML-level, zero format loss):** Read `references/edit.md` first.

**CRITICAL — EDIT INTEGRITY RULES:**
1. **NEVER create a new `Workbook()`** for edit tasks. Always load the original file.
2. The output MUST contain the **same sheets** as the input (same names, same data).
3. Only modify the specific cells the task asks for — everything else must be untouched.
4. **After saving, verify**: open with `read_sheet` and confirm original sheet names and sample data are present.

**XML edit workflow (if scripts available):**
```bash
python3 SKILL_DIR/scripts/xlsx_unpack.py input.xlsx /tmp/xlsx_work/
# ... edit XML with the Edit tool ...
python3 SKILL_DIR/scripts/xlsx_pack.py /tmp/xlsx_work/ output.xlsx
```

**Add a column (if scripts available):**
```bash
python3 SKILL_DIR/scripts/xlsx_unpack.py input.xlsx /tmp/xlsx_work/
python3 SKILL_DIR/scripts/xlsx_add_column.py /tmp/xlsx_work/ --col G \
    --sheet "Sheet1" --header "% of Total" \
    --formula '=F{row}/$F$10' --formula-rows 2:9 \
    --total-row 10 --total-formula '=SUM(G2:G9)' --numfmt '0.0%' \
    --border-row 10 --border-style medium
python3 SKILL_DIR/scripts/xlsx_pack.py /tmp/xlsx_work/ output.xlsx
```

**Insert a row (if scripts available):**
```bash
python3 SKILL_DIR/scripts/xlsx_unpack.py input.xlsx /tmp/xlsx_work/
python3 SKILL_DIR/scripts/xlsx_insert_row.py /tmp/xlsx_work/ --at 5 \
    --sheet "Budget FY2025" --text A=Utilities \
    --values B=3000 C=3000 D=3500 E=3500 \
    --formula 'F=SUM(B{row}:E{row})' --copy-style-from 4
python3 SKILL_DIR/scripts/xlsx_pack.py /tmp/xlsx_work/ output.xlsx
```

## FIX — Repair broken formulas

Read `references/fix.md` first. Unpack → fix broken `<f>` nodes → pack. Preserve all original sheets and data.

## VALIDATE — Check formulas

**If scripts available:**
```bash
python3 SKILL_DIR/scripts/formula_check.py file.xlsx --json    # JSON output
python3 SKILL_DIR/scripts/formula_check.py file.xlsx --report  # Human-readable report
```
Exit code 0 = all formulas valid.

## Financial Color Standard

| Cell Role | Font Color | Hex Code |
|-----------|-----------|----------|
| Hard-coded input / assumption | Blue | `0000FF` |
| Formula / computed result | Black | `000000` |
| Cross-sheet reference formula | Green | `00B050` |

## Key Rules

1. **Formula-First**: Every calculated cell MUST use an Excel formula, not a hardcoded number
2. **MCP-first**: Use MCP tools by default; fall back to Python scripts only for XML-level operations
3. **Always produce the output file** — this is the #1 priority
4. **Validate before delivery**: If scripts available, run `formula_check.py`; otherwise verify formulas manually

## Utility Scripts (if SKILL_DIR/scripts/ exists)

```bash
python3 SKILL_DIR/scripts/xlsx_reader.py input.xlsx                 # structure discovery
python3 SKILL_DIR/scripts/formula_check.py file.xlsx --json         # formula validation
python3 SKILL_DIR/scripts/formula_check.py file.xlsx --report      # standardized report
python3 SKILL_DIR/scripts/xlsx_unpack.py in.xlsx /tmp/work/         # unpack for XML editing
python3 SKILL_DIR/scripts/xlsx_pack.py /tmp/work/ out.xlsx          # repack after editing
python3 SKILL_DIR/scripts/xlsx_shift_rows.py /tmp/work/ insert 5 1  # shift rows for insertion
python3 SKILL_DIR/scripts/xlsx_add_column.py /tmp/work/ --col G ... # add column with formulas
python3 SKILL_DIR/scripts/xlsx_insert_row.py /tmp/work/ --at 6 ...  # insert row with data
```
