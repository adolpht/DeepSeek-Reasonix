#!/usr/bin/env python3
"""xlsx_reader.py — Structure discovery tool for XLSX files.

Reads an XLSX file and outputs: sheet names, dimensions, column headers,
first few rows, merged cells, data types. Uses zipfile + xml.etree.ElementTree
(no openpyxl). Output as JSON (--json) or human-readable format.

Usage:
    python3 xlsx_reader.py input.xlsx
    python3 xlsx_reader.py input.xlsx --sheet Sheet1
    python3 xlsx_reader.py input.xlsx --json
    python3 xlsx_reader.py input.xlsx --quality
    python3 xlsx_reader.py input.xlsx --max-rows 20
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
NS = {
    "main": "http://schemas.openxmlformats.org/spreadsheetml/2006/main",
    "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
    "ct": "http://schemas.openxmlformats.org/package/2006/content-types",
}


def _tag(ns_prefix, local):
    """Build a namespaced tag string."""
    return f"{{{NS[ns_prefix]}}}{local}"


def col_letter(n):
    """Convert 1-based column number to Excel letter (A, B, ..., Z, AA, AB, ...)."""
    result = ""
    while n > 0:
        n, rem = divmod(n - 1, 26)
        result = chr(65 + rem) + result
    return result


def col_number(s):
    """Convert Excel column letter to 1-based number."""
    n = 0
    for c in s.upper():
        n = n * 26 + (ord(c) - 64)
    return n


def parse_cell_ref(ref):
    """Parse a cell reference like 'AB12' into (col_letter, row_number)."""
    match = re.match(r"^([A-Z]+)(\d+)$", ref.upper())
    if not match:
        return None, None
    return match.group(1), int(match.group(2))


def read_xlsx(filepath, sheet_filter=None, max_rows=10, quality_only=False):
    """Read an XLSX file and return structured information.

    Args:
        filepath: Path to the XLSX file.
        sheet_filter: If set, only read this sheet name.
        max_rows: Maximum number of data rows to display per sheet.
        quality_only: If True, only run quality audit.

    Returns:
        dict with file structure information.
    """
    filepath = str(filepath)
    if not os.path.isfile(filepath):
        return {"error": f"File not found: {filepath}"}

    result = {"file": filepath, "sheets": []}

    try:
        with zipfile.ZipFile(filepath, "r") as zf:
            namelist = zf.namelist()
            result["zip_entries"] = len(namelist)

            # Detect high-risk content
            warnings = []
            if "xl/vbaProject.bin" in namelist:
                warnings.append("VBA macros detected (xl/vbaProject.bin)")
            if any("xl/pivotTables/" in n for n in namelist):
                warnings.append("Pivot tables detected")
            if any("xl/charts/" in n for n in namelist):
                warnings.append("Charts detected")
            if any("xl/externalLinks/" in n for n in namelist):
                warnings.append("External links detected")
            result["warnings"] = warnings

            # Read workbook.xml for sheet list
            sheets_info = _read_workbook(zf)
            result["sheet_names"] = [s["name"] for s in sheets_info]

            # Read shared strings
            shared_strings = _read_shared_strings(zf)
            result["shared_string_count"] = len(shared_strings)

            # Read styles (for quality audit)
            styles_info = _read_styles(zf) if quality_only else None

            # Process each worksheet
            for sheet_info in sheets_info:
                if sheet_filter and sheet_info["name"] != sheet_filter:
                    continue
                sheet_data = _read_worksheet(
                    zf, sheet_info, shared_strings, max_rows, quality_only, styles_info
                )
                result["sheets"].append(sheet_data)

    except zipfile.BadZipFile:
        return {"error": f"Not a valid ZIP/XLSX file: {filepath}"}
    except Exception as e:
        return {"error": f"Error reading file: {e}"}

    return result


def _read_workbook(zf):
    """Read workbook.xml and workbook.xml.rels to get sheet list."""
    sheets = []

    # Read relationships
    rels = {}
    rels_path = "xl/_rels/workbook.xml.rels"
    if rels_path in zf.namelist():
        try:
            rels_tree = ET.parse(zf.open(rels_path))
            for rel in rels_tree.iter(_tag("rel", "Relationship")):
                rid = rel.get("Id", "")
                target = rel.get("Target", "")
                rels[rid] = target
        except ET.ParseError:
            pass

    # Read workbook.xml
    wb_path = "xl/workbook.xml"
    if wb_path not in zf.namelist():
        return sheets

    try:
        wb_tree = ET.parse(zf.open(wb_path))
        for sheet_elem in wb_tree.iter(_tag("main", "sheet")):
            name = sheet_elem.get("name", "")
            sheet_id = sheet_elem.get("sheetId", "")
            r_id = sheet_elem.get(_tag("r", "id"), "")
            target = rels.get(r_id, "")
            sheets.append({
                "name": name,
                "sheetId": sheet_id,
                "r:id": r_id,
                "target": target,
            })
    except ET.ParseError:
        pass

    return sheets


def _read_shared_strings(zf):
    """Read the shared strings table."""
    strings = []
    ss_path = "xl/sharedStrings.xml"
    if ss_path not in zf.namelist():
        return strings

    try:
        tree = ET.parse(zf.open(ss_path))
        for si in tree.iter(_tag("main", "si")):
            t_elem = si.find(_tag("main", "t"))
            if t_elem is not None and t_elem.text:
                strings.append(t_elem.text)
            else:
                # Handle rich text (multiple <r> runs)
                runs = si.findall(_tag("main", "r"))
                if runs:
                    text = ""
                    for run in runs:
                        rt = run.find(_tag("main", "t"))
                        if rt is not None and rt.text:
                            text += rt.text
                    strings.append(text)
                else:
                    strings.append("")
    except ET.ParseError:
        pass

    return strings


def _read_styles(zf):
    """Read styles.xml for quality audit."""
    info = {"numFmts": {}, "fonts": [], "cellXfs_count": 0}
    styles_path = "xl/styles.xml"
    if styles_path not in zf.namelist():
        return info

    try:
        tree = ET.parse(zf.open(styles_path))

        # numFmts
        for nf in tree.iter(_tag("main", "numFmt")):
            fid = nf.get("numFmtId", "")
            code = nf.get("formatCode", "")
            info["numFmts"][fid] = code

        # fonts - extract colors
        for font in tree.iter(_tag("main", "font")):
            color_elem = font.find(_tag("main", "color"))
            color = color_elem.get("rgb", "") if color_elem is not None else ""
            bold = font.find(_tag("main", "b")) is not None
            info["fonts"].append({"color": color, "bold": bold})

        # cellXfs count
        cellxfs = tree.find(_tag("main", "cellXfs"))
        if cellxfs is not None:
            info["cellXfs_count"] = int(cellxfs.get("count", "0"))
            info["cellXfs"] = []
            for xf in cellxfs.findall(_tag("main", "xf")):
                info["cellXfs"].append({
                    "numFmtId": xf.get("numFmtId", "0"),
                    "fontId": xf.get("fontId", "0"),
                    "fillId": xf.get("fillId", "0"),
                    "borderId": xf.get("borderId", "0"),
                })

    except ET.ParseError:
        pass

    return info


def _resolve_style_role(styles_info, s_index):
    """Resolve a style index to a human-readable role string."""
    if not styles_info or "cellXfs" not in styles_info:
        return ""
    xfs = styles_info.get("cellXfs", [])
    if s_index >= len(xfs):
        return f"s={s_index} (out of range)"
    xf = xfs[s_index]
    font_id = int(xf.get("fontId", "0"))
    fonts = styles_info.get("fonts", [])
    font_info = fonts[font_id] if font_id < len(fonts) else {}
    color = font_info.get("color", "")
    bold = font_info.get("bold", False)

    if color == "000000FF":
        role = "input(blue)"
    elif color == "00008000":
        role = "cross-sheet(green)"
    elif color == "00FF0000":
        role = "ext-link(red)"
    elif bold:
        role = "header(bold)"
    else:
        role = "formula/default(black)"

    numfmt = xf.get("numFmtId", "0")
    fmt_map = {"164": "$", "165": "%", "166": "x", "167": "#,##0", "1": "0"}
    fmt_label = fmt_map.get(numfmt, "")
    if fmt_label:
        role += f" [{fmt_label}]"
    return role


def _read_worksheet(zf, sheet_info, shared_strings, max_rows, quality_only, styles_info):
    """Read a single worksheet and return structured data."""
    sheet_data = {
        "name": sheet_info["name"],
        "dimensions": None,
        "headers": [],
        "rows": [],
        "merged_cells": [],
        "data_types": {},
        "formula_count": 0,
        "quality": None,
    }

    target = sheet_info.get("target", "")
    if not target:
        return sheet_data

    # Normalize path
    ws_path = f"xl/{target}" if not target.startswith("xl/") else target
    if ws_path not in zf.namelist():
        return sheet_data

    try:
        tree = ET.parse(zf.open(ws_path))
    except ET.ParseError:
        sheet_data["error"] = "XML parse error"
        return sheet_data

    # Dimension
    dim = tree.find(_tag("main", "dimension"))
    if dim is not None:
        sheet_data["dimensions"] = dim.get("ref", "")

    # Merged cells
    merge_cells = tree.find(_tag("main", "mergeCells"))
    if merge_cells is not None:
        for mc in merge_cells.findall(_tag("main", "mergeCell")):
            ref = mc.get("ref", "")
            sheet_data["merged_cells"].append(ref)

    # Quality audit
    quality_issues = []
    max_s_seen = -1

    # Read rows
    sheet_data_elem = tree.find(_tag("main", "sheetData"))
    if sheet_data_elem is None:
        return sheet_data

    row_count = 0
    is_header_row = True
    type_counts = {"number": 0, "string": 0, "formula": 0, "boolean": 0, "error": 0, "empty": 0}

    for row_elem in sheet_data_elem.findall(_tag("main", "row")):
        row_num = int(row_elem.get("r", "0"))
        cells = []

        for c_elem in row_elem.findall(_tag("main", "c")):
            ref = c_elem.get("r", "")
            cell_type = c_elem.get("t", "")
            s_attr = c_elem.get("s", None)
            s_index = int(s_attr) if s_attr is not None else None

            # Track max style index for quality audit
            if s_index is not None:
                max_s_seen = max(max_s_seen, s_index)

            f_elem = c_elem.find(_tag("main", "f"))
            v_elem = c_elem.find(_tag("main", "v"))

            value = None
            data_type = "number"

            if f_elem is not None:
                formula_text = f_elem.text or ""
                data_type = "formula"
                type_counts["formula"] += 1
                sheet_data["formula_count"] += 1
                # Check for error-type cells
                if cell_type == "e":
                    error_val = v_elem.text if v_elem is not None else ""
                    quality_issues.append(
                        f"Cell {ref}: error value '{error_val}' in formula cell"
                    )
                    data_type = "error"
                    value = error_val
                else:
                    value = f"={formula_text}"
            elif cell_type == "s":
                # Shared string
                data_type = "string"
                type_counts["string"] += 1
                if v_elem is not None and v_elem.text is not None:
                    idx = int(v_elem.text)
                    if idx < len(shared_strings):
                        value = shared_strings[idx]
                    else:
                        value = f"<invalid ss index {idx}>"
                        quality_issues.append(
                            f"Cell {ref}: shared string index {idx} out of range (max {len(shared_strings) - 1})"
                        )
            elif cell_type == "inlineStr":
                is_elem = c_elem.find(_tag("main", "is"))
                if is_elem is not None:
                    t_elem = is_elem.find(_tag("main", "t"))
                    value = t_elem.text if t_elem is not None else ""
                data_type = "string"
                type_counts["string"] += 1
            elif cell_type == "b":
                data_type = "boolean"
                type_counts["boolean"] += 1
                value = "TRUE" if (v_elem is not None and v_elem.text == "1") else "FALSE"
            elif cell_type == "e":
                data_type = "error"
                type_counts["error"] += 1
                value = v_elem.text if v_elem is not None else "#ERR"
                quality_issues.append(f"Cell {ref}: error value '{value}'")
            else:
                # Number or empty
                if v_elem is not None and v_elem.text is not None:
                    try:
                        # Store numeric value, detect if it looks like a date
                        raw = v_elem.text
                        if "." in raw:
                            value = float(raw)
                        else:
                            value = int(raw)
                        data_type = "number"
                        type_counts["number"] += 1
                    except ValueError:
                        value = v_elem.text
                        data_type = "string"
                        type_counts["string"] += 1
                else:
                    data_type = "empty"
                    type_counts["empty"] += 1

            cell_info = {"ref": ref, "value": value, "type": data_type}
            if s_index is not None and styles_info:
                cell_info["style_role"] = _resolve_style_role(styles_info, s_index)
            cells.append(cell_info)

        # First non-empty row treated as headers
        if is_header_row and cells:
            non_empty = [c for c in cells if c["type"] != "empty"]
            if non_empty:
                sheet_data["headers"] = [
                    c["value"] if c["value"] is not None else "" for c in cells
                ]
                is_header_row = False

        row_count += 1
        if row_count <= max_rows:
            sheet_data["rows"].append({"row": row_num, "cells": cells})

    sheet_data["data_types"] = type_counts
    sheet_data["total_rows"] = row_count

    # Quality audit
    if quality_only or quality_issues:
        if styles_info and max_s_seen >= 0:
            cellxfs_count = styles_info.get("cellXfs_count", 0)
            if max_s_seen >= cellxfs_count:
                quality_issues.append(
                    f"Style index out of range: max s={max_s_seen}, cellXfs count={cellxfs_count}"
                )
        sheet_data["quality"] = {
            "issues": quality_issues,
            "issue_count": len(quality_issues),
        }

    return sheet_data


def format_report(result, quality_only=False):
    """Format the result as a human-readable report."""
    lines = []

    if "error" in result:
        lines.append(f"ERROR: {result['error']}")
        return "\n".join(lines)

    lines.append(f"File   : {result['file']}")
    lines.append(f"Sheets : {', '.join(result.get('sheet_names', []))}")
    lines.append(f"Shared strings : {result.get('shared_string_count', 0)}")

    if result.get("warnings"):
        lines.append("")
        lines.append("Warnings:")
        for w in result["warnings"]:
            lines.append(f"  ! {w}")

    for sheet in result.get("sheets", []):
        lines.append("")
        lines.append(f"=== Sheet: {sheet['name']} ===")
        lines.append(f"Dimensions    : {sheet.get('dimensions', 'N/A')}")
        lines.append(f"Total rows    : {sheet.get('total_rows', 0)}")
        lines.append(f"Formula count : {sheet.get('formula_count', 0)}")

        if sheet.get("merged_cells"):
            lines.append(f"Merged cells  : {', '.join(sheet['merged_cells'])}")

        if sheet.get("data_types"):
            dt = sheet["data_types"]
            lines.append(
                f"Data types    : "
                f"numbers={dt.get('number', 0)}, "
                f"strings={dt.get('string', 0)}, "
                f"formulas={dt.get('formula', 0)}, "
                f"booleans={dt.get('boolean', 0)}, "
                f"errors={dt.get('error', 0)}"
            )

        if sheet.get("headers"):
            lines.append("")
            lines.append("Headers:")
            header_strs = [str(h) if h is not None else "" for h in sheet["headers"]]
            lines.append(f"  {header_strs}")

        if sheet.get("rows") and not quality_only:
            lines.append("")
            lines.append("Data rows (first few):")
            for row in sheet["rows"]:
                cell_strs = []
                for c in row["cells"]:
                    v = c["value"]
                    if v is None:
                        cell_strs.append("")
                    elif c["type"] == "formula":
                        cell_strs.append(str(v))
                    else:
                        cell_strs.append(str(v))
                lines.append(f"  Row {row['row']}: {cell_strs}")

        if sheet.get("quality") and sheet["quality"]["issue_count"] > 0:
            lines.append("")
            lines.append("Quality issues:")
            for issue in sheet["quality"]["issues"]:
                lines.append(f"  ! {issue}")

    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(
        description="XLSX structure discovery tool. Reads an XLSX file and outputs "
        "sheet names, dimensions, headers, data rows, merged cells, and data types."
    )
    parser.add_argument("file", help="Path to the XLSX file to read")
    parser.add_argument("--sheet", help="Only read this sheet name")
    parser.add_argument("--json", action="store_true", help="Output as JSON")
    parser.add_argument("--max-rows", type=int, default=10,
                        help="Maximum data rows to display per sheet (default: 10)")
    parser.add_argument("--quality", action="store_true",
                        help="Run quality audit only (check for issues)")
    args = parser.parse_args()

    result = read_xlsx(
        args.file,
        sheet_filter=args.sheet,
        max_rows=args.max_rows,
        quality_only=args.quality,
    )

    if args.json:
        print(json.dumps(result, indent=2, ensure_ascii=False))
    else:
        print(format_report(result, quality_only=args.quality))


if __name__ == "__main__":
    main()
