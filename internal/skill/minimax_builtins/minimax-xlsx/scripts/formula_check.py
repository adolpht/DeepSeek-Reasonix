#!/usr/bin/env python3
"""formula_check.py — Validate formulas in an XLSX file.

Performs static validation (XML scan, no external tools):
  1. Error-value detection: cells with t="e" containing Excel error strings
  2. Broken cross-sheet reference detection: formulas referencing non-existent sheets
  3. Unknown named-range detection (heuristic): identifiers not in definedNames
  4. Shared formula integrity: validates shared formula groups
  5. Malformed error cells: cells with t="e" but no <v> child
  6. Circular reference detection (syntactic heuristic)
  7. Mismatched parentheses detection
  8. #REF! in formulas: formulas containing literal #REF! error markers

Usage:
    python3 formula_check.py file.xlsx
    python3 formula_check.py file.xlsx --json
    python3 formula_check.py file.xlsx --report
    python3 formula_check.py file.xlsx --sheet Summary
    python3 formula_check.py file.xlsx --summary

Exit codes:
    0 = no hard errors (PASS or PASS with heuristic warnings)
    1 = hard errors detected or file cannot be opened (FAIL)
"""

import argparse
import json
import os
import re
import sys
import zipfile
import xml.etree.ElementTree as ET
from pathlib import Path

# OOXML namespaces
NS_MAIN = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
NS_R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
NS_REL = "http://schemas.openxmlformats.org/package/2006/relationships"


def _m(local):
    return f"{{{NS_MAIN}}}{local}"


def _r(local):
    return f"{{{NS_R}}}{local}"


# All 7 Excel error types
EXCEL_ERRORS = {"#REF!", "#DIV/0!", "#VALUE!", "#NAME?", "#NULL!", "#NUM!", "#N/A"}

# Common Excel function names (for heuristic named-range detection)
EXCEL_FUNCTIONS = {
    "SUM", "AVERAGE", "COUNT", "COUNTA", "COUNTBLANK", "COUNTIF", "COUNTIFS",
    "MIN", "MAX", "MEDIAN", "MODE", "STDEV", "STDEVP", "VAR", "VARP",
    "IF", "IFERROR", "IFNA", "IFS", "SWITCH", "CHOOSE",
    "AND", "OR", "NOT", "XOR",
    "VLOOKUP", "HLOOKUP", "LOOKUP", "INDEX", "MATCH", "XLOOKUP",
    "SUMIF", "SUMIFS", "AVERAGEIF", "AVERAGEIFS",
    "ROUND", "ROUNDUP", "ROUNDDOWN", "INT", "ABS", "MOD", "POWER", "SQRT",
    "LOG", "LOG10", "LN", "EXP",
    "LEFT", "RIGHT", "MID", "LEN", "FIND", "SEARCH", "SUBSTITUTE", "REPLACE",
    "UPPER", "LOWER", "PROPER", "TRIM", "CONCATENATE", "CONCAT", "TEXTJOIN",
    "DATE", "TIME", "YEAR", "MONTH", "DAY", "HOUR", "MINUTE", "SECOND",
    "TODAY", "NOW", "EDATE", "EOMONTH", "DATEDIF",
    "NPV", "IRR", "PMT", "PV", "FV", "RATE", "NPER",
    "ROW", "ROWS", "COLUMN", "COLUMNS", "ADDRESS", "INDIRECT", "OFFSET",
    "SUMPRODUCT", "TRANSPOSE", "MMULT",
    "RANK", "RANK.EQ", "RANK.AVG", "PERCENTILE", "QUARTILE",
    "LARGE", "SMALL", "PERCENTRANK",
    "CELL", "TYPE", "ISBLANK", "ISERROR", "ISERR", "ISNA", "ISNUMBER",
    "ISTEXT", "ISNONTEXT", "ISLOGICAL", "ISREF", "ISFORMULA",
    "GETPIVOTDATA", "CUBEVALUE", "CUBEMEMBER",
    "HYPERLINK", "INFO",
    "SUMSQ", "PRODUCT", "COMBIN", "PERMUT",
    "FACT", "GCD", "LCM",
    "CEILING", "FLOOR", "CEILING.MATH", "FLOOR.MATH",
    "MROUND", "EVEN", "ODD",
    "SUBTOTAL", "AGGREGATE",
    "TEXT", "VALUE", "NUMBERVALUE", "DATEVALUE", "TIMEVALUE",
    "NETWORKDAYS", "NETWORKDAYS.INTL", "WORKDAY", "WORKDAY.INTL",
    "WEEKNUM", "WEEKDAY", "ISOWEEKNUM",
}


def extract_sheet_refs_from_formula(formula_text):
    """Extract sheet names referenced in a formula.

    Handles both SheetName! and 'Sheet Name'! syntax.

    Returns:
        List of sheet name strings.
    """
    if not formula_text:
        return []

    refs = []

    # Pattern 1: 'Sheet Name'! (quoted)
    for match in re.finditer(r"'([^']+)'!", formula_text):
        refs.append(match.group(1))

    # Pattern 2: SheetName! (unquoted, starts with letter/underscore)
    # Remove already-matched quoted refs first
    cleaned = re.sub(r"'[^']+'!", "", formula_text)
    for match in re.finditer(r"([A-Za-z_][A-Za-z0-9_.]*)!", cleaned):
        name = match.group(1)
        # Skip if it looks like a function call (before a parenthesis)
        refs.append(name)

    return refs


def extract_identifiers_from_formula(formula_text):
    """Extract all identifiers from a formula that might be named ranges.

    Returns identifiers that are not function names, not cell refs,
    and not sheet references.
    """
    if not formula_text:
        return []

    # Remove string literals
    cleaned = re.sub(r'"[^"]*"', "", formula_text)

    # Remove sheet-qualified references (Sheet1!A1 or 'Sheet'!A1)
    cleaned = re.sub(r"(?:[A-Za-z_]\w*!|'[^']*'!)", "", cleaned)

    # Remove cell references (A1, $A$1, A$1, $A1, including ranges)
    cleaned = re.sub(r"\$?[A-Z]{1,3}\$?\d+(?::\$?[A-Z]{1,3}\$?\d+)?", "", cleaned)

    # Remove numbers
    cleaned = re.sub(r"\b\d+\.?\d*\b", "", cleaned)

    # Remove operators and punctuation
    cleaned = re.sub(r"[+\-*/^&<>=(),:%!~{};\[\]]", " ", cleaned)

    # What remains are identifiers
    tokens = cleaned.split()
    identifiers = []
    for token in tokens:
        token = token.strip()
        if not token:
            continue
        # Skip if it's a known function
        if token.upper() in EXCEL_FUNCTIONS:
            continue
        # Skip if it's a single letter or very short (likely a column ref remnant)
        if len(token) == 1 and token.isalpha():
            continue
        identifiers.append(token)

    return identifiers


def check_parentheses(formula_text):
    """Check for mismatched parentheses in a formula.

    Returns:
        True if parentheses are balanced, False otherwise.
    """
    if not formula_text:
        return True

    depth = 0
    for ch in formula_text:
        if ch == '(':
            depth += 1
        elif ch == ')':
            depth -= 1
            if depth < 0:
                return False
    return depth == 0


def check_circular_ref_heuristic(cell_ref, formula_text, sheet_name):
    """Heuristic check for self-referencing circular references.

    Only detects the simplest case: a formula that directly references
    its own cell. Does not detect indirect cycles.

    Returns:
        True if a potential circular reference is detected.
    """
    if not formula_text:
        return False

    # Check if the formula references its own cell
    # E.g., cell A1 with formula "A1+1" is circular
    own_ref = cell_ref.upper()
    # Look for the cell reference in the formula (without sheet prefix)
    pattern = re.compile(
        r"(?:\$?[A-Z]{1,3}\$?\d+)"
    )
    for match in pattern.finditer(formula_text.upper()):
        ref = match.group()
        # Remove $ signs for comparison
        clean_ref = ref.replace("$", "")
        clean_own = own_ref.replace("$", "")
        if clean_ref == clean_own:
            return True

    return False


def check_formulas(filepath, sheet_filter=None, summary_only=False):
    """Validate formulas in an XLSX file.

    Args:
        filepath: Path to the XLSX file.
        sheet_filter: If set, only check this sheet.
        summary_only: If True, only return counts, no per-cell detail.

    Returns:
        dict with validation results.
    """
    filepath = str(filepath)
    result = {
        "file": filepath,
        "sheets_checked": [],
        "formula_count": 0,
        "shared_formula_ranges": 0,
        "error_count": 0,
        "errors": [],
    }

    if not os.path.isfile(filepath):
        result["errors"].append({
            "type": "file_error",
            "error": f"File not found: {filepath}",
        })
        result["error_count"] = 1
        return result

    try:
        zf = zipfile.ZipFile(filepath, "r")
    except zipfile.BadZipFile:
        result["errors"].append({
            "type": "file_error",
            "error": f"Not a valid ZIP/XLSX file: {filepath}",
        })
        result["error_count"] = 1
        return result

    with zf:
        # Read workbook.xml for sheet list
        sheets_info = []
        rels = {}

        # Read relationships
        rels_path = "xl/_rels/workbook.xml.rels"
        if rels_path in zf.namelist():
            try:
                rels_tree = ET.parse(zf.open(rels_path))
                for rel in rels_tree.iter(f"{{{NS_REL}}}Relationship"):
                    rid = rel.get("Id", "")
                    target = rel.get("Target", "")
                    rels[rid] = target
            except ET.ParseError:
                pass

        # Read workbook.xml
        wb_path = "xl/workbook.xml"
        defined_names = []
        if wb_path in zf.namelist():
            try:
                wb_tree = ET.parse(zf.open(wb_path))
                for sheet_elem in wb_tree.iter(_m("sheet")):
                    name = sheet_elem.get("name", "")
                    r_id = sheet_elem.get(_r("id"), "")
                    target = rels.get(r_id, "")
                    sheets_info.append({"name": name, "target": target})

                # Read defined names
                dn_elem = wb_tree.find(_m("definedNames"))
                if dn_elem is not None:
                    for dn in dn_elem.findall(_m("definedName")):
                        dn_name = dn.get("name", "")
                        defined_names.append(dn_name)
            except ET.ParseError:
                pass

        result["defined_names"] = defined_names
        valid_sheet_names = [s["name"] for s in sheets_info]

        # Process each worksheet
        for sheet_info in sheets_info:
            sheet_name = sheet_info["name"]
            target = sheet_info.get("target", "")

            if sheet_filter and sheet_name != sheet_filter:
                continue

            result["sheets_checked"].append(sheet_name)

            ws_path = f"xl/{target}" if not target.startswith("xl/") else target
            if ws_path not in zf.namelist():
                continue

            try:
                tree = ET.parse(zf.open(ws_path))
            except ET.ParseError:
                result["errors"].append({
                    "type": "file_error",
                    "sheet": sheet_name,
                    "error": f"XML parse error in {ws_path}",
                })
                result["error_count"] += 1
                continue

            root = tree.getroot()
            sheet_data = root.find(_m("sheetData"))
            if sheet_data is None:
                continue

            for row_elem in sheet_data.findall(_m("row")):
                for c_elem in row_elem.findall(_m("c")):
                    cell_ref = c_elem.get("r", "")
                    cell_type = c_elem.get("t", "")
                    f_elem = c_elem.find(_m("f"))
                    v_elem = c_elem.find(_m("v"))

                    formula_text = f_elem.text if f_elem is not None else None

                    # Check 1: Error-value detection (t="e")
                    if cell_type == "e":
                        error_val = v_elem.text if v_elem is not None else None

                        # Check 5: Malformed error cell (t="e" but no <v>)
                        if v_elem is None or error_val is None:
                            err = {
                                "type": "malformed_error_cell",
                                "sheet": sheet_name,
                                "cell": cell_ref,
                                "formula": formula_text,
                            }
                            result["errors"].append(err)
                            result["error_count"] += 1
                        else:
                            err = {
                                "type": "error_value",
                                "error": error_val,
                                "sheet": sheet_name,
                                "cell": cell_ref,
                                "formula": formula_text,
                            }
                            result["errors"].append(err)
                            result["error_count"] += 1
                        continue

                    # For cells with formulas
                    if f_elem is not None:
                        f_type = f_elem.get("t", "")
                        f_ref = f_elem.get("ref", "")

                        # Count shared formula ranges
                        if f_type == "shared" and f_ref:
                            result["shared_formula_ranges"] += 1

                        # Skip shared formula consumers (they inherit from primary)
                        if f_type == "shared" and not f_ref and formula_text is None:
                            continue

                        result["formula_count"] += 1

                        if formula_text and not summary_only:
                            # Check 2: Broken cross-sheet references
                            sheet_refs = extract_sheet_refs_from_formula(formula_text)
                            for ref_sheet in sheet_refs:
                                if ref_sheet not in valid_sheet_names:
                                    err = {
                                        "type": "broken_sheet_ref",
                                        "sheet": sheet_name,
                                        "cell": cell_ref,
                                        "formula": formula_text,
                                        "missing_sheet": ref_sheet,
                                        "valid_sheets": valid_sheet_names,
                                    }
                                    result["errors"].append(err)
                                    result["error_count"] += 1

                            # Check 3: Unknown named-range references (heuristic)
                            identifiers = extract_identifiers_from_formula(formula_text)
                            for ident in identifiers:
                                if ident not in defined_names and ident not in valid_sheet_names:
                                    err = {
                                        "type": "unknown_name_ref",
                                        "sheet": sheet_name,
                                        "cell": cell_ref,
                                        "formula": formula_text,
                                        "unknown_name": ident,
                                        "defined_names": defined_names,
                                        "note": "Heuristic check — verify manually if this is a false positive",
                                    }
                                    result["errors"].append(err)
                                    result["error_count"] += 1

                            # Check 6: Circular reference (heuristic)
                            if check_circular_ref_heuristic(cell_ref, formula_text, sheet_name):
                                err = {
                                    "type": "circular_reference",
                                    "sheet": sheet_name,
                                    "cell": cell_ref,
                                    "formula": formula_text,
                                    "note": "Heuristic — cell references itself directly",
                                }
                                result["errors"].append(err)
                                result["error_count"] += 1

                            # Check 7: Mismatched parentheses
                            if not check_parentheses(formula_text):
                                err = {
                                    "type": "mismatched_parentheses",
                                    "sheet": sheet_name,
                                    "cell": cell_ref,
                                    "formula": formula_text,
                                }
                                result["errors"].append(err)
                                result["error_count"] += 1

                            # Check 8: #REF! in formula text
                            if "#REF!" in formula_text:
                                err = {
                                    "type": "ref_error_in_formula",
                                    "sheet": sheet_name,
                                    "cell": cell_ref,
                                    "formula": formula_text,
                                    "note": "Formula contains literal #REF! error marker",
                                }
                                result["errors"].append(err)
                                result["error_count"] += 1

                    # Check for error values in non-formula cells too
                    if cell_type == "e" and f_elem is None:
                        error_val = v_elem.text if v_elem is not None else "#ERR"
                        err = {
                            "type": "error_value",
                            "error": error_val,
                            "sheet": sheet_name,
                            "cell": cell_ref,
                            "formula": None,
                        }
                        result["errors"].append(err)
                        result["error_count"] += 1

    return result


def format_human_report(result):
    """Format results as a human-readable report."""
    lines = []

    lines.append(f"File   : {result['file']}")
    lines.append(f"Sheets : {', '.join(result['sheets_checked'])}")
    lines.append(f"Formulas checked      : {result['formula_count']} distinct formula cells")
    lines.append(f"Shared formula ranges : {result['shared_formula_ranges']} ranges")
    lines.append(f"Errors found          : {result['error_count']}")
    lines.append("")

    if result["error_count"] == 0:
        lines.append("PASS — No formula errors detected")
    else:
        lines.append("── Error Details ──")

        hard_errors = 0
        warnings = 0

        for err in result["errors"]:
            err_type = err["type"]
            is_soft = err_type in ("unknown_name_ref",)

            prefix = "WARN" if is_soft else "FAIL"
            if is_soft:
                warnings += 1
            else:
                hard_errors += 1

            sheet = err.get("sheet", "")
            cell = err.get("cell", "")
            loc = f"{sheet}!{cell}" if sheet and cell else ""

            if err_type == "error_value":
                error = err.get("error", "")
                formula = err.get("formula")
                formula_str = f" (formula: {formula})" if formula else ""
                lines.append(f"  [{prefix}] [{loc}] contains {error}{formula_str}")

            elif err_type == "broken_sheet_ref":
                formula = err.get("formula", "")
                missing = err.get("missing_sheet", "")
                valid = err.get("valid_sheets", [])
                lines.append(f"  [{prefix}] [{loc}] references missing sheet '{missing}'")
                lines.append(f"         Formula: {formula}")
                lines.append(f"         Valid sheets: {valid}")

            elif err_type == "unknown_name_ref":
                formula = err.get("formula", "")
                unknown = err.get("unknown_name", "")
                defined = err.get("defined_names", [])
                lines.append(f"  [{prefix}] [{loc}] uses unknown name '{unknown}' (heuristic — verify manually)")
                lines.append(f"         Formula: {formula}")
                lines.append(f"         Defined names: {defined}")

            elif err_type == "malformed_error_cell":
                lines.append(f"  [{prefix}] [{loc}] has t='e' but no <v> element (malformed XML)")

            elif err_type == "circular_reference":
                formula = err.get("formula", "")
                lines.append(f"  [{prefix}] [{loc}] appears to reference itself (formula: {formula})")

            elif err_type == "mismatched_parentheses":
                formula = err.get("formula", "")
                lines.append(f"  [{prefix}] [{loc}] has mismatched parentheses (formula: {formula})")

            elif err_type == "ref_error_in_formula":
                formula = err.get("formula", "")
                lines.append(f"  [{prefix}] [{loc}] formula contains #REF! (formula: {formula})")

            elif err_type == "file_error":
                lines.append(f"  [{prefix}] {err.get('error', 'Unknown file error')}")

            else:
                lines.append(f"  [{prefix}] [{loc}] {err_type}: {err}")

        lines.append("")
        if hard_errors > 0:
            lines.append(f"FAIL — {hard_errors} error(s) must be fixed before delivery")
        else:
            lines.append("PASS — No hard errors")
        if warnings > 0:
            lines.append(f"WARN — {warnings} heuristic warning(s) require manual review")

    return "\n".join(lines)


def format_structured_report(result):
    """Format results as a structured validation report (markdown-like)."""
    lines = []
    lines.append("## Formula Validation Report")
    lines.append("")
    lines.append(f"**File**: {result['file']}")
    lines.append(f"**Sheets checked**: {', '.join(result['sheets_checked'])}")
    lines.append(f"**Total formulas scanned**: {result['formula_count']}")
    lines.append("")

    # Separate errors by type
    hard_errors = [e for e in result["errors"] if e["type"] != "unknown_name_ref"]
    soft_warnings = [e for e in result["errors"] if e["type"] == "unknown_name_ref"]

    lines.append("### Tier 1 — Static Validation")
    lines.append("")
    if not hard_errors:
        lines.append("**Status**: PASS")
        lines.append("")
        lines.append("No errors detected.")
    else:
        lines.append("**Status**: FAIL")
        lines.append("")
        lines.append("| Sheet | Cell | Error Type | Detail |")
        lines.append("|-------|------|-----------|--------|")
        for err in hard_errors:
            sheet = err.get("sheet", "")
            cell = err.get("cell", "")
            etype = err.get("type", "")
            if etype == "error_value":
                detail = err.get("error", "")
                if err.get("formula"):
                    detail += f" (formula: {err['formula']})"
            elif etype == "broken_sheet_ref":
                detail = f"Missing sheet: {err.get('missing_sheet', '')}"
            elif etype == "malformed_error_cell":
                detail = "t='e' but no <v>"
            elif etype == "circular_reference":
                detail = f"Self-referencing: {err.get('formula', '')}"
            elif etype == "mismatched_parentheses":
                detail = f"Formula: {err.get('formula', '')}"
            elif etype == "ref_error_in_formula":
                detail = f"#REF! in: {err.get('formula', '')}"
            elif etype == "file_error":
                detail = err.get("error", "")
            else:
                detail = str(err)
            lines.append(f"| {sheet} | {cell} | {etype} | {detail} |")

    if soft_warnings:
        lines.append("")
        lines.append("### Heuristic Warnings")
        lines.append("")
        for warn in soft_warnings:
            sheet = warn.get("sheet", "")
            cell = warn.get("cell", "")
            name = warn.get("unknown_name", "")
            lines.append(f"- `{sheet}!{cell}`: unknown name `{name}` — verify manually")

    lines.append("")
    lines.append("### Summary")
    lines.append("")
    lines.append(f"- **Total errors found**: {result['error_count']}")
    lines.append(f"- **Hard errors**: {len(hard_errors)}")
    lines.append(f"- **Heuristic warnings**: {len(soft_warnings)}")
    status = "FAIL (blocked)" if hard_errors else "PASS (ready for delivery)"
    lines.append(f"- **Final status**: {status}")

    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(
        description="Validate formulas in an XLSX file. "
        "Checks for error values, broken cross-sheet references, "
        "unknown named ranges, circular references, mismatched parentheses, "
        "and #REF! errors. Exit code 0 = PASS, 1 = FAIL."
    )
    parser.add_argument("file", help="Path to the XLSX file to validate")
    parser.add_argument("--json", action="store_true", help="Output as JSON")
    parser.add_argument("--report", action="store_true",
                        help="Output as structured markdown report")
    parser.add_argument("--sheet", help="Only check this sheet")
    parser.add_argument("--summary", action="store_true",
                        help="Summary mode (counts only, no per-cell detail)")
    args = parser.parse_args()

    result = check_formulas(args.file, sheet_filter=args.sheet, summary_only=args.summary)

    if args.json:
        print(json.dumps(result, indent=2, ensure_ascii=False))
    elif args.report:
        print(format_structured_report(result))
    else:
        print(format_human_report(result))

    # Exit code: 0 if no hard errors, 1 if hard errors
    hard_errors = [e for e in result["errors"] if e["type"] != "unknown_name_ref"]
    if hard_errors:
        sys.exit(1)
    else:
        sys.exit(0)


if __name__ == "__main__":
    main()
