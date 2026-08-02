#!/usr/bin/env python3
"""xlsx_add_column.py — Add a column with formulas and formatting to an unpacked XLSX.

Adds a new column to a worksheet in an unpacked XLSX directory. Handles:
  - Header row with text
  - Formula rows (with {row} placeholder substitution)
  - Total row with total formula
  - Number format (numfmt) registration in styles.xml
  - Border application on specified row
  - Style auto-copied from adjacent column for existing data rows

Usage:
    python3 xlsx_add_column.py /tmp/xlsx_work/ --col G \\
        --sheet "Sheet1" --header "% of Total" \\
        --formula '=F{row}/$F$10' --formula-rows 2:9 \\
        --total-row 10 --total-formula '=SUM(G2:G9)' --numfmt '0.0%' \\
        --border-row 10 --border-style medium
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
    return f"{{{NS_MAIN}}}{local}"


def _r(local):
    return f"{{{NS_R}}}{local}"


def col_number(s):
    """Convert Excel column letter to 1-based number."""
    n = 0
    for c in s.upper():
        n = n * 26 + (ord(c) - 64)
    return n


def col_letter(n):
    """Convert 1-based column number to Excel letter."""
    result = ""
    while n > 0:
        n, rem = divmod(n - 1, 26)
        result = chr(65 + rem) + result
    return result


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


def add_shared_string(work_dir, text):
    """Add a string to sharedStrings.xml and return its index.

    If the string already exists, returns the existing index.
    """
    ss_path = os.path.join(work_dir, "xl", "sharedStrings.xml")
    if not os.path.isfile(ss_path):
        # Create a minimal sharedStrings.xml
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

    # Update counts
    new_count = existing_count + 1
    root.set("count", str(new_count))
    root.set("uniqueCount", str(new_count))

    # Write back
    ET.indent(root, space="  ")
    tree.write(ss_path, encoding="UTF-8", xml_declaration=True)

    return existing_count  # Index of the newly added string


def register_numfmt(work_dir, format_code):
    """Register a custom number format in styles.xml.

    Returns the numFmtId for the format. If the format already exists,
    returns the existing ID.
    """
    styles_path = os.path.join(work_dir, "xl", "styles.xml")
    if not os.path.isfile(styles_path):
        raise FileNotFoundError(f"styles.xml not found: {styles_path}")

    tree = ET.parse(styles_path)
    root = tree.getroot()

    # Check if format already exists
    numfmts = root.find(_m("numFmts"))
    if numfmts is not None:
        for nf in numfmts.findall(_m("numFmt")):
            if nf.get("formatCode", "") == format_code:
                return int(nf.get("numFmtId", "0"))

    # Need to add new format
    if numfmts is None:
        numfmts = ET.SubElement(root, _m("numFmts"))
        # Insert before fonts for proper ordering
        fonts_idx = None
        for i, child in enumerate(root):
            if child.tag == _m("fonts"):
                fonts_idx = i
                break
        if fonts_idx is not None:
            root.remove(numfmts)
            root.insert(fonts_idx, numfmts)

    # Find next available ID (custom IDs start at 164)
    max_id = 163
    for nf in numfmts.findall(_m("numFmt")):
        fid = int(nf.get("numFmtId", "0"))
        max_id = max(max_id, fid)
    new_id = max_id + 1

    # Add the new format
    nf = ET.SubElement(numfmts, _m("numFmt"))
    nf.set("numFmtId", str(new_id))
    nf.set("formatCode", format_code)

    # Update count
    count = len(numfmts.findall(_m("numFmt")))
    numfmts.set("count", str(count))

    # Write back
    ET.indent(root, space="  ")
    tree.write(styles_path, encoding="UTF-8", xml_declaration=True)

    return new_id


def add_border_style(work_dir, border_style="medium"):
    """Add a top-border style to styles.xml and return the new style index.

    The border is applied to the top edge only. Returns the new cellXfs index.
    """
    styles_path = os.path.join(work_dir, "xl", "styles.xml")
    tree = ET.parse(styles_path)
    root = tree.getroot()

    # Add new border
    borders = root.find(_m("borders"))
    if borders is None:
        borders = ET.SubElement(root, _m("borders"))

    border = ET.SubElement(borders, _m("border"))
    left = ET.SubElement(border, _m("left"))
    right = ET.SubElement(border, _m("right"))
    top = ET.SubElement(border, _m("top"))
    top.set("style", border_style)
    bottom = ET.SubElement(border, _m("bottom"))
    diagonal = ET.SubElement(border, _m("diagonal"))

    new_border_id = len(borders.findall(_m("border"))) - 1
    borders.set("count", str(new_border_id + 1))

    # Now we need to add xf entries that clone each existing xf but with the new borderId
    cellxfs = root.find(_m("cellXfs"))
    if cellxfs is None:
        cellxfs = ET.SubElement(root, _m("cellXfs"))

    # For simplicity, create a mapping: for each existing xf, create a clone with new borderId
    # We'll return a dict mapping old xf index -> new xf index
    existing_xfs = list(cellxfs.findall(_m("xf")))
    old_count = len(existing_xfs)
    xf_mapping = {}

    for i, xf in enumerate(existing_xfs):
        import copy
        new_xf = copy.deepcopy(xf)
        new_xf.set("borderId", str(new_border_id))
        new_xf.set("applyBorder", "1")
        cellxfs.append(new_xf)
        xf_mapping[i] = old_count + i

    cellxfs.set("count", str(old_count * 2))

    # Write back
    ET.indent(root, space="  ")
    tree.write(styles_path, encoding="UTF-8", xml_declaration=True)

    return xf_mapping


def add_formula_style(work_dir, numfmt_id=None):
    """Add a formula cell style (black font, optional numfmt) to styles.xml.

    Returns the new cellXfs index.
    """
    styles_path = os.path.join(work_dir, "xl", "styles.xml")
    tree = ET.parse(styles_path)
    root = tree.getroot()

    cellxfs = root.find(_m("cellXfs"))
    if cellxfs is None:
        cellxfs = ET.SubElement(root, _m("cellXfs"))

    # Find the black font (fontId=2 in template, or look for color 00000000)
    fonts = root.find(_m("fonts"))
    black_font_id = 0
    if fonts is not None:
        for i, font in enumerate(fonts.findall(_m("font"))):
            color_elem = font.find(_m("color"))
            if color_elem is not None and color_elem.get("rgb", "") == "00000000":
                black_font_id = i
                break

    old_count = len(cellxfs.findall(_m("xf")))

    xf = ET.SubElement(cellxfs, _m("xf"))
    xf.set("numFmtId", str(numfmt_id) if numfmt_id is not None else "0")
    xf.set("fontId", str(black_font_id))
    xf.set("fillId", "0")
    xf.set("borderId", "0")
    xf.set("xfId", "0")
    xf.set("applyFont", "1")
    if numfmt_id is not None and numfmt_id != 0:
        xf.set("applyNumberFormat", "1")

    cellxfs.set("count", str(old_count + 1))

    ET.indent(root, space="  ")
    tree.write(styles_path, encoding="UTF-8", xml_declaration=True)

    return old_count


def add_column(work_dir, col, sheet_name, header=None, formula=None,
               formula_rows=None, total_row=None, total_formula=None,
               numfmt=None, border_row=None, border_style="medium"):
    """Add a column with formulas and formatting to an unpacked XLSX.

    Args:
        work_dir: Path to the unpacked XLSX directory.
        col: Column letter (e.g., "G").
        sheet_name: Name of the target sheet.
        header: Header text for the new column.
        formula: Formula template with {row} placeholder (e.g., "=F{row}/$F$10").
        formula_rows: Tuple of (start_row, end_row) for formula rows.
        total_row: Row number of the total row.
        total_formula: Formula for the total row.
        numfmt: Number format code (e.g., "0.0%").
        border_row: Row number to apply border to.
        border_style: Border style ("thin", "medium", "thick").

    Returns:
        dict with results.
    """
    result = {
        "col": col,
        "sheet": sheet_name,
        "cells_added": 0,
    }

    # Find sheet XML
    xml_path = find_sheet_xml(work_dir, sheet_name)
    if xml_path is None:
        raise ValueError(f"Sheet '{sheet_name}' not found")

    # Register numfmt if needed
    numfmt_id = None
    formula_style_idx = None
    if numfmt:
        numfmt_id = register_numfmt(work_dir, numfmt)
        formula_style_idx = add_formula_style(work_dir, numfmt_id)

    # Find the style of the adjacent column (for auto-copy)
    adj_col_num = col_number(col) - 1
    adj_col = col_letter(adj_col_num) if adj_col_num > 0 else None

    # Parse the worksheet
    tree = ET.parse(xml_path)
    root = tree.getroot()
    sheet_data = root.find(_m("sheetData"))
    if sheet_data is None:
        raise ValueError("No sheetData found in worksheet")

    # Collect existing rows
    rows_by_num = {}
    for row_elem in sheet_data.findall(_m("row")):
        r = int(row_elem.get("r", "0"))
        rows_by_num[r] = row_elem

    # Determine rows to add formulas to
    formula_row_list = []
    if formula_rows and formula:
        start, end = formula_rows
        formula_row_list = list(range(start, end + 1))

    # Process each existing row
    for row_num in sorted(rows_by_num.keys()):
        row_elem = rows_by_num[row_num]
        needs_cell = False
        cell_value = None
        cell_type = None
        cell_formula = None
        cell_style = None

        if header and row_num == 1:
            # Header row
            ss_idx = add_shared_string(work_dir, header)
            cell_value = str(ss_idx)
            cell_type = "s"
            needs_cell = True
            # Find header style from adjacent column
            for c in row_elem.findall(_m("c")):
                ref = c.get("r", "")
                if ref.startswith(adj_col) if adj_col else False:
                    cell_style = c.get("s", None)
                    break

        elif row_num in formula_row_list and formula:
            # Formula row - substitute {row}
            formula_text = formula.lstrip("=")
            formula_text = formula_text.replace("{row}", str(row_num))
            cell_formula = formula_text
            cell_style = str(formula_style_idx) if formula_style_idx is not None else None
            needs_cell = True

        elif total_row and row_num == total_row and total_formula:
            # Total row
            formula_text = total_formula.lstrip("=")
            cell_formula = formula_text
            cell_style = str(formula_style_idx) if formula_style_idx is not None else None
            needs_cell = True

        if needs_cell:
            # Create the cell element
            cell_ref = f"{col}{row_num}"
            c = ET.SubElement(row_elem, _m("c"))
            c.set("r", cell_ref)

            if cell_type == "s":
                c.set("t", "s")
            if cell_style is not None:
                c.set("s", cell_style)

            if cell_formula:
                f = ET.SubElement(c, _m("f"))
                f.text = cell_formula
                v = ET.SubElement(c, _m("v"))
                v.text = ""
            elif cell_value is not None:
                v = ET.SubElement(c, _m("v"))
                v.text = cell_value

            result["cells_added"] += 1

    # Update dimension if present
    dim_elem = root.find(_m("dimension"))
    if dim_elem is not None:
        dim_ref = dim_elem.get("ref", "")
        if dim_ref:
            # Parse and extend to include new column
            parts = dim_ref.split(":")
            if len(parts) == 2:
                end_ref = parts[1]
                end_col_match = re.match(r"^([A-Z]+)(\d+)$", end_ref)
                if end_col_match:
                    end_col = end_col_match.group(1)
                    end_row = end_col_match.group(2)
                    if col_number(col) > col_number(end_col):
                        dim_elem.set("ref", f"{parts[0]}:{col}{end_row}")

    # Update <cols> if present
    cols_elem = root.find(_m("cols"))
    col_num = col_number(col)
    if cols_elem is not None:
        new_col = ET.SubElement(cols_elem, _m("col"))
        new_col.set("min", str(col_num))
        new_col.set("max", str(col_num))
        new_col.set("width", "14")
        new_col.set("customWidth", "1")

    # Apply border if requested
    if border_row is not None and border_style:
        # Apply top border to all cells in the border_row
        xf_mapping = add_border_style(work_dir, border_style)
        border_row_elem = rows_by_num.get(border_row)
        if border_row_elem is not None:
            for c in border_row_elem.findall(_m("c")):
                s_attr = c.get("s", None)
                if s_attr is not None:
                    s_idx = int(s_attr)
                    if s_idx in xf_mapping:
                        c.set("s", str(xf_mapping[s_idx]))
                else:
                    # Apply default style with border
                    if 0 in xf_mapping:
                        c.set("s", str(xf_mapping[0]))

    # Write back worksheet
    ET.indent(root, space="  ")
    tree.write(xml_path, encoding="UTF-8", xml_declaration=True)

    return result


def main():
    parser = argparse.ArgumentParser(
        description="Add a column with formulas and formatting to an unpacked XLSX."
    )
    parser.add_argument("work_dir", help="Path to the unpacked XLSX directory")
    parser.add_argument("--col", required=True, help="Column letter to add (e.g., G)")
    parser.add_argument("--sheet", required=True, help="Sheet name")
    parser.add_argument("--header", help="Header text for the new column")
    parser.add_argument("--formula", help="Formula template with {row} placeholder (e.g., '=F{row}/$F$10')")
    parser.add_argument("--formula-rows", help="Row range for formulas (e.g., 2:9)")
    parser.add_argument("--total-row", type=int, help="Row number of the total row")
    parser.add_argument("--total-formula", help="Formula for the total row (e.g., '=SUM(G2:G9)')")
    parser.add_argument("--numfmt", help="Number format code (e.g., '0.0%%')")
    parser.add_argument("--border-row", type=int, help="Row to apply border to")
    parser.add_argument("--border-style", default="medium",
                        choices=["thin", "medium", "thick", "hair", "dotted", "dashed"],
                        help="Border style (default: medium)")
    args = parser.parse_args()

    # Parse formula-rows
    formula_rows = None
    if args.formula_rows:
        parts = args.formula_rows.split(":")
        if len(parts) != 2:
            print("ERROR: --formula-rows must be in format start:end (e.g., 2:9)", file=sys.stderr)
            sys.exit(1)
        formula_rows = (int(parts[0]), int(parts[1]))

    try:
        result = add_column(
            args.work_dir,
            col=args.col.upper(),
            sheet_name=args.sheet,
            header=args.header,
            formula=args.formula,
            formula_rows=formula_rows,
            total_row=args.total_row,
            total_formula=args.total_formula,
            numfmt=args.numfmt,
            border_row=args.border_row,
            border_style=args.border_style,
        )
        print(f"Added column {result['col']} to sheet '{result['sheet']}'")
        print(f"Cells added: {result['cells_added']}")

    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
