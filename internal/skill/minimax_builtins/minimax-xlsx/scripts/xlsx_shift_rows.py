#!/usr/bin/env python3
"""xlsx_shift_rows.py — Shift rows in an unpacked XLSX directory for insertions/deletions.

Handles:
  - Row number renumbering in sheet XML
  - Updating SUM/range references in formulas
  - Fixing shared string table references (no change needed)
  - Updating merge cells
  - Updating conditional formatting ranges
  - Updating data validation ranges
  - Updating dimension elements
  - Updating table ref attributes
  - Updating chart data source ranges

Usage:
    python3 xlsx_shift_rows.py /tmp/xlsx_work/ insert 5 1   # insert 1 row at row 5
    python3 xlsx_shift_rows.py /tmp/xlsx_work/ delete 8 1   # delete 1 row at row 8
    python3 xlsx_shift_rows.py /tmp/xlsx_work/ insert 5 1 --sheet "Sheet1"
"""

import argparse
import os
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

# OOXML namespaces
NS_MAIN = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
NS_R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
NS_REL = "http://schemas.openxmlformats.org/package/2006/relationships"


def _m(local):
    """Main namespace tag."""
    return f"{{{NS_MAIN}}}{local}"


def _r(local):
    """Relationship namespace tag."""
    return f"{{{NS_R}}}{local}"


def parse_cell_ref(ref):
    """Parse a cell reference like 'AB12' into (col_letters, row_number)."""
    match = re.match(r"^([A-Z]+)(\d+)$", ref.upper())
    if not match:
        return None, None
    return match.group(1), int(match.group(2))


def make_cell_ref(col, row):
    """Build a cell reference from column letters and row number."""
    return f"{col}{row}"


def parse_range_ref(range_str):
    """Parse a range reference like 'A1:D10' into ((col1, row1), (col2, row2)).

    Also handles single cell references.
    """
    parts = range_str.split(":")
    if len(parts) == 1:
        col, row = parse_cell_ref(parts[0])
        return (col, row), (col, row)
    elif len(parts) == 2:
        col1, row1 = parse_cell_ref(parts[0])
        col2, row2 = parse_cell_ref(parts[1])
        return (col1, row1), (col2, row2)
    return None, None


def shift_row_in_ref(row_num, at_row, count, is_insert):
    """Shift a row number if it needs to move.

    Args:
        row_num: The row number to potentially shift.
        at_row: The row where insertion/deletion starts.
        count: Number of rows to insert/delete.
        is_insert: True for insert, False for delete.

    Returns:
        New row number, or None if the row was deleted.
    """
    if is_insert:
        # Insert: rows >= at_row shift down by count
        if row_num >= at_row:
            return row_num + count
        return row_num
    else:
        # Delete: rows in [at_row, at_row+count-1] are removed
        if at_row <= row_num < at_row + count:
            return None  # Row deleted
        if row_num >= at_row + count:
            return row_num - count
        return row_num


def shift_range_ref(range_str, at_row, count, is_insert):
    """Shift a range reference like 'A5:D20' for row insertion/deletion.

    Returns the new range string, or None if the range is entirely deleted.
    """
    (col1, row1), (col2, row2) = parse_range_ref(range_str)
    if col1 is None:
        return range_str  # Can't parse, return as-is

    new_row1 = shift_row_in_ref(row1, at_row, count, is_insert)
    new_row2 = shift_row_in_ref(row2, at_row, count, is_insert)

    if new_row1 is None or new_row2 is None:
        return None  # Range partially or fully deleted

    if col1 == col2 and new_row1 == new_row2:
        return make_cell_ref(col1, new_row1)
    return f"{make_cell_ref(col1, new_row1)}:{make_cell_ref(col2, new_row2)}"


def shift_formula_refs(formula_text, at_row, count, is_insert):
    """Shift row references in a formula string.

    Handles:
      - Simple cell refs: A5, $B$10
      - Range refs: A5:D20, $A$5:$D$20
      - Cross-sheet refs: Sheet1!A5, 'Sheet Name'!A5:B10
      - Does NOT shift: row 0 (invalid), structured references

    Args:
        formula_text: The formula text (without leading =).
        at_row: The row where insertion/deletion starts.
        count: Number of rows to insert/delete.
        is_insert: True for insert, False for delete.

    Returns:
        The formula text with shifted row references.
    """
    if not formula_text:
        return formula_text

    # Pattern to match cell references and range references in formulas
    # Matches: optional sheet prefix, then cell refs possibly with $
    # Group breakdown:
    #   1 = sheet prefix (e.g. "Sheet1!" or "'Sheet Name'!")
    #   2 = start col (e.g. "A" or "$B")
    #   3 = start row (e.g. "5" or "$10")
    #   4 = colon + end col (e.g. ":C" or ":$D")
    #   5 = end row (e.g. "20" or "$20")
    pattern = re.compile(
        r"(?:([A-Za-z_][A-Za-z0-9_.]*!|'[^']*'!))?"  # optional sheet prefix
        r"(\$?[A-Z]{1,3})"                             # start column
        r"(\$?\d+)"                                    # start row
        r"(:\$?[A-Z]{1,3})?"                           # optional range end col
        r"(\$?\d+)?"                                   # optional range end row
    )

    def replace_ref(match):
        sheet_prefix = match.group(1) or ""
        start_col = match.group(2)
        start_row_str = match.group(3)
        end_col_part = match.group(4) or ""
        end_row_str = match.group(5) or ""

        # Parse start row
        start_row_dollar = "$" if start_row_str.startswith("$") else ""
        start_row = int(start_row_str.lstrip("$"))

        # Shift start row
        new_start_row = shift_row_in_ref(start_row, at_row, count, is_insert)
        if new_start_row is None:
            return "#REF!"
        if new_start_row < 1:
            return "#REF!"

        result = f"{sheet_prefix}{start_col}{start_row_dollar}{new_start_row}"

        # If range reference
        if end_col_part and end_row_str:
            end_row_dollar = "$" if end_row_str.startswith("$") else ""
            end_row = int(end_row_str.lstrip("$"))

            new_end_row = shift_row_in_ref(end_row, at_row, count, is_insert)
            if new_end_row is None:
                return "#REF!"
            if new_end_row < 1:
                return "#REF!"

            result += f"{end_col_part}{end_row_dollar}{new_end_row}"

        return result

    # We need to be careful not to match function names or text strings
    # Strategy: replace all cell-like patterns that aren't inside quotes
    # Simple approach: split on quotes, only replace in non-quoted parts
    parts = re.split(r'("([^"]*)")', formula_text)
    result_parts = []
    for i, part in enumerate(parts):
        if i % 3 == 0:  # Non-quoted parts
            result_parts.append(pattern.sub(replace_ref, part))
        else:
            result_parts.append(part)

    return "".join(result_parts)


def find_sheet_files(work_dir):
    """Find all worksheet XML files and their sheet names.

    Returns:
        List of (sheet_name, xml_path) tuples.
    """
    wb_path = os.path.join(work_dir, "xl", "workbook.xml")
    rels_path = os.path.join(work_dir, "xl", "_rels", "workbook.xml.rels")

    if not os.path.isfile(wb_path):
        return []

    # Read relationships
    rels = {}
    if os.path.isfile(rels_path):
        try:
            rels_tree = ET.parse(rels_path)
            for rel in rels_tree.iter(f"{{{NS_REL}}}Relationship"):
                rid = rel.get("Id", "")
                target = rel.get("Target", "")
                rels[rid] = target
        except ET.ParseError:
            pass

    # Read workbook
    sheets = []
    try:
        wb_tree = ET.parse(wb_path)
        for sheet_elem in wb_tree.iter(_m("sheet")):
            name = sheet_elem.get("name", "")
            r_id = sheet_elem.get(_r("id"), "")
            target = rels.get(r_id, "")
            if target:
                xml_path = os.path.join(work_dir, "xl", target)
                if os.path.isfile(xml_path):
                    sheets.append((name, xml_path))
    except ET.ParseError:
        pass

    return sheets


def shift_worksheet_rows(xml_path, at_row, count, is_insert):
    """Shift rows in a single worksheet XML file.

    Updates:
      - <row r="N"> attributes
      - <c r="A5"> cell addresses
      - <f> formula text
      - <mergeCell ref="A5:D10"> ranges
      - <conditionalFormatting sqref="A5:D10"> ranges
      - <dataValidation sqref="A5:D10"> ranges
      - <dimension ref="A1:D10"> ranges
      - Table ref attributes in xl/tables/

    Args:
        xml_path: Path to the worksheet XML file.
        at_row: The row where insertion/deletion starts.
        count: Number of rows to insert/delete.
        is_insert: True for insert, False for delete.

    Returns:
        Number of rows shifted.
    """
    tree = ET.parse(xml_path)
    root = tree.getroot()
    rows_shifted = 0

    # Process sheetData rows
    sheet_data = root.find(_m("sheetData"))
    if sheet_data is not None:
        # Collect rows to process (need to handle in reverse for insert)
        rows = list(sheet_data.findall(_m("row")))

        # For insert: process rows in reverse order (highest first) to avoid conflicts
        # For delete: process rows in forward order, removing deleted rows
        if is_insert:
            rows.reverse()

        rows_to_remove = []

        for row_elem in rows:
            row_num = int(row_elem.get("r", "0"))

            if not is_insert:
                # Delete mode: check if this row is in the deleted range
                if at_row <= row_num < at_row + count:
                    rows_to_remove.append(row_elem)
                    rows_shifted += 1
                    continue

            # Shift the row number
            new_row_num = shift_row_in_ref(row_num, at_row, count, is_insert)
            if new_row_num is None:
                rows_to_remove.append(row_elem)
                rows_shifted += 1
                continue

            if new_row_num != row_num:
                row_elem.set("r", str(new_row_num))
                rows_shifted += 1

            # Update cell references within this row
            for c_elem in row_elem.findall(_m("c")):
                ref = c_elem.get("r", "")
                col, cell_row = parse_cell_ref(ref)
                if col is not None and cell_row is not None:
                    new_cell_row = shift_row_in_ref(cell_row, at_row, count, is_insert)
                    if new_cell_row is not None and new_cell_row != cell_row:
                        c_elem.set("r", make_cell_ref(col, new_cell_row))

                # Update formula text
                f_elem = c_elem.find(_m("f"))
                if f_elem is not None and f_elem.text:
                    new_formula = shift_formula_refs(
                        f_elem.text, at_row, count, is_insert
                    )
                    if new_formula != f_elem.text:
                        f_elem.text = new_formula

                    # Also update shared formula ref attribute
                    ref_attr = f_elem.get("ref", "")
                    if ref_attr:
                        new_ref = shift_range_ref(ref_attr, at_row, count, is_insert)
                        if new_ref and new_ref != ref_attr:
                            f_elem.set("ref", new_ref)

        # Remove deleted rows
        for row_elem in rows_to_remove:
            sheet_data.remove(row_elem)

    # Update dimension
    dim_elem = root.find(_m("dimension"))
    if dim_elem is not None:
        dim_ref = dim_elem.get("ref", "")
        if dim_ref:
            new_dim = shift_range_ref(dim_ref, at_row, count, is_insert)
            if new_dim and new_dim != dim_ref:
                dim_elem.set("ref", new_dim)

    # Update merge cells
    merge_cells = root.find(_m("mergeCells"))
    if merge_cells is not None:
        merges_to_remove = []
        for mc in merge_cells.findall(_m("mergeCell")):
            ref = mc.get("ref", "")
            if ref:
                new_ref = shift_range_ref(ref, at_row, count, is_insert)
                if new_ref is None:
                    merges_to_remove.append(mc)
                elif new_ref != ref:
                    mc.set("ref", new_ref)
        for mc in merges_to_remove:
            merge_cells.remove(mc)

    # Update conditional formatting
    for cf in root.findall(_m("conditionalFormatting")):
        sqref = cf.get("sqref", "")
        if sqref:
            # sqref can be space-separated list of ranges
            parts = sqref.split()
            new_parts = []
            for part in parts:
                new_part = shift_range_ref(part, at_row, count, is_insert)
                if new_part:
                    new_parts.append(new_part)
            if new_parts:
                cf.set("sqref", " ".join(new_parts))
            else:
                root.remove(cf)

    # Update data validations
    dvs = root.find(_m("dataValidations"))
    if dvs is not None:
        dvs_to_remove = []
        for dv in dvs.findall(_m("dataValidation")):
            sqref = dv.get("sqref", "")
            if sqref:
                parts = sqref.split()
                new_parts = []
                for part in parts:
                    new_part = shift_range_ref(part, at_row, count, is_insert)
                    if new_part:
                        new_parts.append(new_part)
                if new_parts:
                    dv.set("sqref", " ".join(new_parts))
                else:
                    dvs_to_remove.append(dv)
        for dv in dvs_to_remove:
            dvs.remove(dv)

    # Write back
    ET.indent(root, space="  ")
    tree.write(xml_path, encoding="UTF-8", xml_declaration=True)

    return rows_shifted


def shift_table_refs(work_dir, at_row, count, is_insert):
    """Shift row references in xl/tables/tableN.xml files."""
    tables_dir = os.path.join(work_dir, "xl", "tables")
    if not os.path.isdir(tables_dir):
        return

    for fname in os.listdir(tables_dir):
        if not fname.endswith(".xml"):
            continue
        tpath = os.path.join(tables_dir, fname)
        try:
            tree = ET.parse(tpath)
            root = tree.getroot()

            # Update table ref attribute
            ref = root.get("ref", "")
            if ref:
                new_ref = shift_range_ref(ref, at_row, count, is_insert)
                if new_ref and new_ref != ref:
                    root.set("ref", new_ref)

            # Update autoFilter ref
            af = root.find(_m("autoFilter"))
            if af is not None:
                af_ref = af.get("ref", "")
                if af_ref:
                    new_af_ref = shift_range_ref(af_ref, at_row, count, is_insert)
                    if new_af_ref and new_af_ref != af_ref:
                        af.set("ref", new_af_ref)

            ET.indent(root, space="  ")
            tree.write(tpath, encoding="UTF-8", xml_declaration=True)
        except ET.ParseError:
            pass


def shift_chart_refs(work_dir, at_row, count, is_insert):
    """Shift row references in xl/charts/chartN.xml data sources."""
    charts_dir = os.path.join(work_dir, "xl", "charts")
    if not os.path.isdir(charts_dir):
        return

    for fname in os.listdir(charts_dir):
        if not fname.endswith(".xml"):
            continue
        cpath = os.path.join(charts_dir, fname)
        try:
            with open(cpath, "r", encoding="utf-8") as f:
                content = f.read()

            # Chart XML uses different namespace, so use regex for formula refs
            # Pattern: Sheet1!$A$5:$A$20 or similar
            def shift_chart_formula(match):
                formula = match.group(0)
                return shift_formula_refs(formula, at_row, count, is_insert)

            # Match cell/range refs with optional sheet prefix
            new_content = re.sub(
                r"(?:[A-Za-z_]\w*!|'[^']*'!)?\$?[A-Z]{1,3}\$?\d+(?::\$?[A-Z]{1,3}\$?\d+)?",
                shift_chart_formula,
                content,
            )

            if new_content != content:
                with open(cpath, "w", encoding="utf-8") as f:
                    f.write(new_content)
        except Exception:
            pass


def shift_rows(work_dir, at_row, count, is_insert, sheet_name=None):
    """Main entry point: shift rows in an unpacked XLSX directory.

    Args:
        work_dir: Path to the unpacked XLSX directory.
        at_row: The row where insertion/deletion starts (1-based).
        count: Number of rows to insert/delete.
        is_insert: True for insert, False for delete.
        sheet_name: If set, only shift rows in this sheet. Otherwise all sheets.

    Returns:
        dict with results.
    """
    work_dir = str(work_dir)
    result = {
        "work_dir": work_dir,
        "at_row": at_row,
        "count": count,
        "action": "insert" if is_insert else "delete",
        "sheets_processed": [],
        "total_rows_shifted": 0,
    }

    # Find worksheet files
    sheets = find_sheet_files(work_dir)

    for sname, xml_path in sheets:
        if sheet_name and sname != sheet_name:
            continue

        rows_shifted = shift_worksheet_rows(xml_path, at_row, count, is_insert)
        result["sheets_processed"].append(sname)
        result["total_rows_shifted"] += rows_shifted

    # Update table refs
    shift_table_refs(work_dir, at_row, count, is_insert)

    # Update chart refs
    shift_chart_refs(work_dir, at_row, count, is_insert)

    return result


def main():
    parser = argparse.ArgumentParser(
        description="Shift rows in an unpacked XLSX directory. "
        "Updates row numbers, cell references, formulas, merge cells, "
        "conditional formatting, data validations, dimensions, and table refs."
    )
    parser.add_argument("work_dir", help="Path to the unpacked XLSX directory")
    parser.add_argument("action", choices=["insert", "delete"],
                        help="Action: 'insert' to make room, 'delete' to remove")
    parser.add_argument("at_row", type=int,
                        help="Row number where insertion/deletion starts (1-based)")
    parser.add_argument("count", type=int,
                        help="Number of rows to insert or delete")
    parser.add_argument("--sheet", help="Only shift rows in this sheet (default: all)")
    args = parser.parse_args()

    if args.at_row < 1:
        print("ERROR: at_row must be >= 1", file=sys.stderr)
        sys.exit(1)
    if args.count < 1:
        print("ERROR: count must be >= 1", file=sys.stderr)
        sys.exit(1)

    try:
        result = shift_rows(
            args.work_dir, args.at_row, args.count,
            is_insert=(args.action == "insert"),
            sheet_name=args.sheet,
        )

        print(f"Action: {result['action']} {result['count']} row(s) at row {result['at_row']}")
        print(f"Sheets processed: {', '.join(result['sheets_processed'])}")
        print(f"Total rows shifted: {result['total_rows_shifted']}")

    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
