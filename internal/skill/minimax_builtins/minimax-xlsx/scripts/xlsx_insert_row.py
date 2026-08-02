#!/usr/bin/env python3
"""xlsx_insert_row.py — Insert a row with data/formulas into an unpacked XLSX.

Internally calls xlsx_shift_rows.py to make room, then fills the new row
with the specified data, formulas, and styling.

Usage:
    python3 xlsx_insert_row.py /tmp/xlsx_work/ --at 5 \\
        --sheet "Budget FY2025" --text A=Utilities \\
        --values B=3000 C=3000 D=3500 E=3500 \\
        --formula 'F=SUM(B{row}:E{row})' --copy-style-from 4

The --text flag adds text values (stored in sharedStrings.xml).
The --values flag adds numeric values.
The --formula flag adds a formula with {row} placeholder.
The --copy-style-from copies the row-level style and per-cell styles from
the specified row number.
"""

import argparse
import os
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path

# Import shift_rows from sibling module
try:
    from xlsx_shift_rows import shift_rows, find_sheet_files
except ImportError:
    # When running from a different directory, add the script's directory to path
    _script_dir = os.path.dirname(os.path.abspath(__file__))
    if _script_dir not in sys.path:
        sys.path.insert(0, _script_dir)
    from xlsx_shift_rows import shift_rows, find_sheet_files

# OOXML namespaces
NS_MAIN = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
NS_R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
NS_REL = "http://schemas.openxmlformats.org/package/2006/relationships"


def _m(local):
    return f"{{{NS_MAIN}}}{local}"


def _r(local):
    return f"{{{NS_R}}}{local}"


def add_shared_string(work_dir, text):
    """Add a string to sharedStrings.xml and return its index."""
    ss_path = os.path.join(work_dir, "xl", "sharedStrings.xml")
    if not os.path.isfile(ss_path):
        root = ET.Element(_m("sst"))
        root.set("count", "0")
        root.set("uniqueCount", "0")
        tree = ET.ElementTree(root)
    else:
        tree = ET.parse(ss_path)
        root = tree.getroot()

    # Check if string already exists
    existing_count = 0
    for si in root.findall(_m("si")):
        t_elem = si.find(_m("t"))
        if t_elem is not None and t_elem.text == text:
            return existing_count
        existing_count += 1

    # Append new string
    si = ET.SubElement(root, _m("si"))
    t = ET.SubElement(si, _m("t"))
    t.text = text

    new_count = existing_count + 1
    root.set("count", str(new_count))
    root.set("uniqueCount", str(new_count))

    ET.indent(root, space="  ")
    tree.write(ss_path, encoding="UTF-8", xml_declaration=True)

    return existing_count


def find_sheet_xml(work_dir, sheet_name):
    """Find the XML file path for a named sheet."""
    wb_path = os.path.join(work_dir, "xl", "workbook.xml")
    rels_path = os.path.join(work_dir, "xl", "_rels", "workbook.xml.rels")

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

    if not os.path.isfile(wb_path):
        return None

    try:
        wb_tree = ET.parse(wb_path)
        for sheet_elem in wb_tree.iter(_m("sheet")):
            if sheet_elem.get("name", "") == sheet_name:
                r_id = sheet_elem.get(_r("id"), "")
                target = rels.get(r_id, "")
                if target:
                    xml_path = os.path.join(work_dir, "xl", target)
                    if os.path.isfile(xml_path):
                        return xml_path
    except ET.ParseError:
        pass

    return None


def get_row_styles(xml_path, row_num):
    """Get per-cell styles from a specific row.

    Returns a dict mapping column letter -> style index string.
    """
    tree = ET.parse(xml_path)
    root = tree.getroot()
    sheet_data = root.find(_m("sheetData"))
    if sheet_data is None:
        return {}, None

    cell_styles = {}
    row_style = None

    for row_elem in sheet_data.findall(_m("row")):
        if int(row_elem.get("r", "0")) == row_num:
            row_style = row_elem.get("s", None)
            # Also get row-level attributes
            row_attrs = {}
            for attr in ["ht", "customHeight", "hidden"]:
                val = row_elem.get(attr)
                if val is not None:
                    row_attrs[attr] = val

            for c in row_elem.findall(_m("c")):
                ref = c.get("r", "")
                col_match = re.match(r"^([A-Z]+)", ref)
                if col_match:
                    col = col_match.group(1)
                    s = c.get("s", None)
                    if s is not None:
                        cell_styles[col] = s
            return cell_styles, row_style

    return {}, None


def insert_row(work_dir, at_row, sheet_name, text_values=None,
               numeric_values=None, formulas=None, copy_style_from=None):
    """Insert a row with data/formulas into an unpacked XLSX.

    First shifts existing rows down using xlsx_shift_rows, then fills
    the new row at --at with the specified content.

    Args:
        work_dir: Path to the unpacked XLSX directory.
        at_row: Row number to insert at (1-based).
        sheet_name: Name of the target sheet.
        text_values: Dict of col_letter -> text string (stored in sharedStrings).
        numeric_values: Dict of col_letter -> numeric value.
        formulas: Dict of col_letter -> formula template with {row}.
        copy_style_from: Row number to copy styles from.

    Returns:
        dict with results.
    """
    work_dir = str(work_dir)
    result = {
        "at_row": at_row,
        "sheet": sheet_name,
        "cells_added": 0,
    }

    # Step 1: Shift rows to make room
    shift_result = shift_rows(work_dir, at_row, 1, is_insert=True, sheet_name=sheet_name)

    # Step 2: Find the worksheet XML
    xml_path = find_sheet_xml(work_dir, sheet_name)
    if xml_path is None:
        raise ValueError(f"Sheet '{sheet_name}' not found after shift")

    # Step 3: Get styles to copy
    cell_styles = {}
    row_style = None
    if copy_style_from is not None:
        cell_styles, row_style = get_row_styles(xml_path, copy_style_from)

    # Step 4: Parse the worksheet and add the new row
    tree = ET.parse(xml_path)
    root = tree.getroot()
    sheet_data = root.find(_m("sheetData"))
    if sheet_data is None:
        raise ValueError("No sheetData found in worksheet")

    # Create the new row element
    row_elem = ET.SubElement(sheet_data, _m("row"))
    row_elem.set("r", str(at_row))

    if row_style is not None:
        row_elem.set("s", row_style)

    # Add text cells
    if text_values:
        for col, text in text_values.items():
            ss_idx = add_shared_string(work_dir, text)
            cell_ref = f"{col}{at_row}"
            c = ET.SubElement(row_elem, _m("c"))
            c.set("r", cell_ref)
            c.set("t", "s")
            if col in cell_styles:
                c.set("s", cell_styles[col])
            v = ET.SubElement(c, _m("v"))
            v.text = str(ss_idx)
            result["cells_added"] += 1

    # Add numeric cells
    if numeric_values:
        for col, value in numeric_values.items():
            cell_ref = f"{col}{at_row}"
            c = ET.SubElement(row_elem, _m("c"))
            c.set("r", cell_ref)
            if col in cell_styles:
                c.set("s", cell_styles[col])
            v = ET.SubElement(c, _m("v"))
            # Format numeric value
            if isinstance(value, float):
                v.text = str(value)
            elif isinstance(value, int):
                v.text = str(value)
            else:
                # Try to convert string to number
                try:
                    num = float(value)
                    if num == int(num):
                        v.text = str(int(num))
                    else:
                        v.text = str(num)
                except ValueError:
                    v.text = str(value)
            result["cells_added"] += 1

    # Add formula cells
    if formulas:
        for col, formula_template in formulas.items():
            formula_text = formula_template.replace("{row}", str(at_row))
            # Remove leading = if present (OOXML formulas don't have it)
            if formula_text.startswith("="):
                formula_text = formula_text[1:]

            cell_ref = f"{col}{at_row}"
            c = ET.SubElement(row_elem, _m("c"))
            c.set("r", cell_ref)
            if col in cell_styles:
                c.set("s", cell_styles[col])
            f = ET.SubElement(c, _m("f"))
            f.text = formula_text
            v = ET.SubElement(c, _m("v"))
            v.text = ""  # Clear cached value
            result["cells_added"] += 1

    # Re-sort the rows in sheetData by row number
    # ET doesn't maintain order after SubElement, so we need to sort
    rows = list(sheet_data.findall(_m("row")))
    rows.sort(key=lambda r: int(r.get("r", "0")))

    # Remove all rows and re-add in order
    for row in rows:
        sheet_data.remove(row)
    for row in rows:
        sheet_data.append(row)

    # Update dimension if present
    dim_elem = root.find(_m("dimension"))
    if dim_elem is not None:
        dim_ref = dim_elem.get("ref", "")
        if dim_ref:
            parts = dim_ref.split(":")
            if len(parts) == 2:
                end_ref = parts[1]
                end_match = re.match(r"^([A-Z]+)(\d+)$", end_ref)
                if end_match:
                    end_row = int(end_match.group(2))
                    if at_row > end_row:
                        end_col = end_match.group(1)
                        dim_elem.set("ref", f"{parts[0]}:{end_col}{at_row}")

    # Write back
    ET.indent(root, space="  ")
    tree.write(xml_path, encoding="UTF-8", xml_declaration=True)

    return result


def parse_kv_args(args_list):
    """Parse key=value arguments into a dict.

    E.g., ['A=Utilities', 'B=Label'] -> {'A': 'Utilities', 'B': 'Label'}
    """
    result = {}
    if args_list is None:
        return result
    for item in args_list:
        if "=" not in item:
            raise ValueError(f"Invalid key=value format: {item}")
        key, value = item.split("=", 1)
        result[key.upper()] = value
    return result


def main():
    parser = argparse.ArgumentParser(
        description="Insert a row with data/formulas into an unpacked XLSX. "
        "Internally calls xlsx_shift_rows.py to make room."
    )
    parser.add_argument("work_dir", help="Path to the unpacked XLSX directory")
    parser.add_argument("--at", type=int, required=True,
                        help="Row number to insert at (1-based)")
    parser.add_argument("--sheet", required=True, help="Sheet name")
    parser.add_argument("--text", nargs="*", help="Text values as Col=Text (e.g., A=Utilities)")
    parser.add_argument("--values", nargs="*", help="Numeric values as Col=Value (e.g., B=3000)")
    parser.add_argument("--formula", nargs="*",
                        help="Formula as Col=Formula (e.g., 'F=SUM(B{row}:E{row})')")
    parser.add_argument("--copy-style-from", type=int,
                        help="Row number to copy cell styles from")
    args = parser.parse_args()

    if args.at < 1:
        print("ERROR: --at must be >= 1", file=sys.stderr)
        sys.exit(1)

    try:
        text_values = parse_kv_args(args.text)
        numeric_values = parse_kv_args(args.values)
        formulas = parse_kv_args(args.formula)
    except ValueError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)

    try:
        result = insert_row(
            args.work_dir,
            at_row=args.at,
            sheet_name=args.sheet,
            text_values=text_values,
            numeric_values=numeric_values,
            formulas=formulas,
            copy_style_from=args.copy_style_from,
        )
        print(f"Inserted row at {result['at_row']} in sheet '{result['sheet']}'")
        print(f"Cells added: {result['cells_added']}")

    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
