#!/usr/bin/env python3
"""docx_tables.py — Create, edit, and format tables in DOCX files.

Uses ONLY Python standard library. No third-party dependencies.

Usage:
    python3 docx_tables.py input.docx --add '{"rows":3,"cols":4,"headers":["A","B","C","D"],"data":[...]}' --output out.docx
    python3 docx_tables.py input.docx --apply-style 0 --style three-line --output out.docx
    python3 docx_tables.py input.docx --merge 0 '{"from":"A1","to":"D1"}' --output out.docx
    python3 docx_tables.py input.docx --list
"""

import argparse
import json
import os
import sys
import re
import copy
import zipfile
import io
import xml.etree.ElementTree as ET

# ── Namespaces ───────────────────────────────────────────────────────────────────

NS = {
    "w":   "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r":   "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "ct":  "http://schemas.openxmlformats.org/package/2006/content-types",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
}
for prefix, uri in NS.items():
    ET.register_namespace(prefix, uri)

W = NS["w"]
R = NS["r"]

def _w(tag):
    return f"{{{W}}}{tag}"


# ── DOCX I/O ─────────────────────────────────────────────────────────────────────

def read_docx(path):
    files = {}
    with zipfile.ZipFile(path, "r") as zf:
        for name in zf.namelist():
            files[name] = zf.read(name)
    return files

def write_docx(files, path):
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as zf:
        for name, data in sorted(files.items()):
            zf.writestr(name, data)

def parse_xml(b):
    return ET.fromstring(b)

def serialize_xml(root):
    ET.indent(root, space="  ")
    buf = io.BytesIO()
    ET.ElementTree(root).write(buf, encoding="UTF-8", xml_declaration=True)
    return buf.getvalue()


# ── XML Helpers ──────────────────────────────────────────────────────────────────

def w_elem(local, attrib=None):
    e = ET.Element(_w(local))
    if attrib:
        for k, v in attrib.items():
            e.set(_w(k) if not k.startswith("{") else k, str(v))
    return e

def make_paragraph(text, style=None, bold=False, justify=None):
    p = w_elem("p")
    if style or justify:
        pPr = w_elem("pPr")
        if style:
            pPr.append(w_elem("pStyle", {"val": style}))
        if justify:
            pPr.append(w_elem("jc", {"val": justify}))
        p.append(pPr)
    r = w_elem("r")
    if bold:
        rPr = w_elem("rPr")
        rPr.append(w_elem("b"))
        r.append(rPr)
    t = w_elem("t")
    t.text = str(text)
    if text and (text[0] == ' ' or text[-1] == ' '):
        t.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
    r.append(t)
    p.append(r)
    return p


# ── Table Discovery ──────────────────────────────────────────────────────────────

def find_tables(body):
    """Find all w:tbl elements in the body, return list of (index, element)."""
    tables = []
    idx = 0
    for child in body:
        if child.tag == _w("tbl"):
            tables.append((idx, child))
            idx += 1
    return tables


def count_table_rows_cols(tbl):
    """Count rows and columns in a table."""
    rows = tbl.findall(_w("tr"))
    num_rows = len(rows)
    num_cols = 0
    if rows:
        num_cols = len(rows[0].findall(_w("tc")))
    return num_rows, num_cols


# ── Table Creation ───────────────────────────────────────────────────────────────

def create_table(spec):
    """Create a w:tbl element from a JSON spec dict."""
    rows_count = spec.get("rows", 1)
    cols_count = spec.get("cols", 1)
    headers = spec.get("headers", [])
    data = spec.get("data", [])
    tbl_style = spec.get("style", "TableGrid")

    num_cols = len(headers) if headers else cols_count

    tbl = w_elem("tbl")

    # tblPr
    tblPr = w_elem("tblPr")
    tblPr.append(w_elem("tblStyle", {"val": tbl_style}))
    tblPr.append(w_elem("tblW", {"w": "5000", "type": "pct"}))

    # Apply three-line borders if requested
    if tbl_style == "three-line":
        tblBorders = w_elem("tblBorders")
        tblBorders.append(w_elem("top", {"val": "single", "sz": "12", "space": "0", "color": "000000"}))
        tblBorders.append(w_elem("bottom", {"val": "single", "sz": "12", "space": "0", "color": "000000"}))
        tblBorders.append(w_elem("left", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        tblBorders.append(w_elem("right", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        tblBorders.append(w_elem("insideV", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        tblBorders.append(w_elem("insideH", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        tblPr.append(tblBorders)

    tbl.append(tblPr)

    # tblGrid
    tblGrid = w_elem("tblGrid")
    for _ in range(num_cols):
        tblGrid.append(w_elem("gridCol", {"w": "2400"}))
    tbl.append(tblGrid)

    # Header row
    if headers:
        tr = w_elem("tr")
        trPr = w_elem("trPr")
        trPr.append(w_elem("tblHeader"))
        tr.append(trPr)
        for h in headers:
            tc = w_elem("tc")
            if tbl_style == "three-line":
                tcPr = w_elem("tcPr")
                tcBorders = w_elem("tcBorders")
                tcBorders.append(w_elem("bottom", {"val": "single", "sz": "6", "space": "0", "color": "000000"}))
                tcPr.append(tcBorders)
                tc.append(tcPr)
            tc.append(make_paragraph(h, bold=True, justify="center"))
            tr.append(tc)
        tbl.append(tr)

    # Data rows
    for row_data in data:
        tr = w_elem("tr")
        for cell_text in row_data:
            tc = w_elem("tc")
            tc.append(make_paragraph(cell_text))
            tr.append(tc)
        tbl.append(tr)

    # Fill remaining rows if data is empty but rows/cols specified
    existing_rows = len(data) + (1 if headers else 0)
    for _ in range(existing_rows, rows_count):
        tr = w_elem("tr")
        for _ in range(num_cols):
            tc = w_elem("tc")
            tc.append(make_paragraph(""))
            tr.append(tc)
        tbl.append(tr)

    return tbl


# ── Table Style Application ──────────────────────────────────────────────────────

def apply_table_style(tbl, style_name):
    """Apply a visual style to an existing table element."""
    # Remove existing tblBorders if any
    tblPr = tbl.find(_w("tblPr"))
    if tblPr is None:
        tblPr = w_elem("tblPr")
        tbl.insert(0, tblPr)

    existing_borders = tblPr.find(_w("tblBorders"))
    if existing_borders is not None:
        tblPr.remove(existing_borders)

    if style_name == "three-line":
        borders = w_elem("tblBorders")
        borders.append(w_elem("top", {"val": "single", "sz": "12", "space": "0", "color": "000000"}))
        borders.append(w_elem("bottom", {"val": "single", "sz": "12", "space": "0", "color": "000000"}))
        borders.append(w_elem("left", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        borders.append(w_elem("right", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        borders.append(w_elem("insideV", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        borders.append(w_elem("insideH", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        tblPr.append(borders)

        # Add header bottom border on first row cells
        rows = tbl.findall(_w("tr"))
        if rows:
            for tc in rows[0].findall(_w("tc")):
                tcPr = tc.find(_w("tcPr"))
                if tcPr is None:
                    tcPr = w_elem("tcPr")
                    tc.insert(0, tcPr)
                existing_tc_borders = tcPr.find(_w("tcBorders"))
                if existing_tc_borders is not None:
                    tcPr.remove(existing_tc_borders)
                tcBorders = w_elem("tcBorders")
                tcBorders.append(w_elem("bottom", {"val": "single", "sz": "6", "space": "0", "color": "000000"}))
                tcPr.append(tcBorders)

        # Remove cell borders from data rows
        for row in rows[1:]:
            for tc in row.findall(_w("tc")):
                tcPr = tc.find(_w("tcPr"))
                if tcPr is not None:
                    existing_tc_borders = tcPr.find(_w("tcBorders"))
                    if existing_tc_borders is not None:
                        tcPr.remove(existing_tc_borders)

    elif style_name == "zebra":
        borders = w_elem("tblBorders")
        borders.append(w_elem("top", {"val": "single", "sz": "4", "space": "0", "color": "auto"}))
        borders.append(w_elem("bottom", {"val": "single", "sz": "4", "space": "0", "color": "auto"}))
        borders.append(w_elem("left", {"val": "single", "sz": "4", "space": "0", "color": "auto"}))
        borders.append(w_elem("right", {"val": "single", "sz": "4", "space": "0", "color": "auto"}))
        borders.append(w_elem("insideH", {"val": "single", "sz": "4", "space": "0", "color": "auto"}))
        borders.append(w_elem("insideV", {"val": "single", "sz": "4", "space": "0", "color": "auto"}))
        tblPr.append(borders)

        # Apply alternating row shading
        rows = tbl.findall(_w("tr"))
        for i, row in enumerate(rows):
            if i > 0 and i % 2 == 0:
                for tc in row.findall(_w("tc")):
                    tcPr = tc.find(_w("tcPr"))
                    if tcPr is None:
                        tcPr = w_elem("tcPr")
                        tc.insert(0, tcPr)
                    # Remove existing shading
                    existing_shd = tcPr.find(_w("shd"))
                    if existing_shd is not None:
                        tcPr.remove(existing_shd)
                    shd = w_elem("shd", {"val": "clear", "color": "auto", "fill": "F2F2F2"})
                    tcPr.append(shd)

    elif style_name == "grid":
        borders = w_elem("tblBorders")
        borders.append(w_elem("top", {"val": "single", "sz": "4", "space": "0", "color": "000000"}))
        borders.append(w_elem("bottom", {"val": "single", "sz": "4", "space": "0", "color": "000000"}))
        borders.append(w_elem("left", {"val": "single", "sz": "4", "space": "0", "color": "000000"}))
        borders.append(w_elem("right", {"val": "single", "sz": "4", "space": "0", "color": "000000"}))
        borders.append(w_elem("insideH", {"val": "single", "sz": "4", "space": "0", "color": "000000"}))
        borders.append(w_elem("insideV", {"val": "single", "sz": "4", "space": "0", "color": "000000"}))
        tblPr.append(borders)

    elif style_name == "none":
        borders = w_elem("tblBorders")
        borders.append(w_elem("top", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        borders.append(w_elem("bottom", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        borders.append(w_elem("left", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        borders.append(w_elem("right", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        borders.append(w_elem("insideH", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        borders.append(w_elem("insideV", {"val": "none", "sz": "0", "space": "0", "color": "auto"}))
        tblPr.append(borders)


# ── Cell Merge ───────────────────────────────────────────────────────────────────

def parse_cell_ref(ref):
    """Parse a cell reference like 'A1' into (col_index, row_index)."""
    col_str = ""
    row_str = ""
    for ch in ref:
        if ch.isalpha():
            col_str += ch.upper()
        else:
            row_str += ch
    # Convert column letters to 0-based index (A=0, B=1, ..., Z=25, AA=26, ...)
    col = 0
    for ch in col_str:
        col = col * 26 + (ord(ch) - ord('A') + 1)
    col -= 1  # 0-based
    row = int(row_str) - 1  # 0-based
    return col, row


def merge_cells(tbl, merge_spec):
    """Merge cells in a table. merge_spec has 'from' and 'to' cell refs like 'A1' to 'D1'."""
    from_col, from_row = parse_cell_ref(merge_spec["from"])
    to_col, to_row = parse_cell_ref(merge_spec["to"])

    rows = tbl.findall(_w("tr"))

    if from_row == to_row:
        # Horizontal merge (span columns)
        row = rows[from_row]
        cells = row.findall(_w("tc"))
        span = to_col - from_col + 1
        # Set GridSpan on the first cell
        tc = cells[from_col]
        tcPr = tc.find(_w("tcPr"))
        if tcPr is None:
            tcPr = w_elem("tcPr")
            tc.insert(0, tcPr)
        existing_gs = tcPr.find(_w("gridSpan"))
        if existing_gs is not None:
            tcPr.remove(existing_gs)
        gs = w_elem("gridSpan", {"val": str(span)})
        tcPr.append(gs)
        # Remove merged cells (they become "phantom" cells in OpenXML but we remove them)
        for i in range(from_col + 1, to_col + 1):
            if i < len(cells):
                row.remove(cells[i])

    elif from_col == to_col:
        # Vertical merge (span rows)
        for i, row_idx in enumerate(range(from_row, to_row + 1)):
            if row_idx >= len(rows):
                break
            row = rows[row_idx]
            cells = row.findall(_w("tc"))
            if from_col >= len(cells):
                continue
            tc = cells[from_col]
            tcPr = tc.find(_w("tcPr"))
            if tcPr is None:
                tcPr = w_elem("tcPr")
                tc.insert(0, tcPr)
            existing_vm = tcPr.find(_w("vMerge"))
            if existing_vm is not None:
                tcPr.remove(existing_vm)
            vm = w_elem("vMerge")
            if i == 0:
                vm.set(_w("val"), "restart")
            # else: no val attribute means "continue"
            tcPr.append(vm)


# ── Main ─────────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        description="Create, edit, and format tables in DOCX files.",
        epilog="Uses ONLY Python standard library. No third-party dependencies.",
    )
    parser.add_argument("input", help="Input DOCX file path")
    parser.add_argument("--output", default=None, help="Output DOCX file path")
    parser.add_argument("--list", action="store_true", dest="list_tables", help="List tables in document")
    parser.add_argument("--add", default=None, help="Add a table from JSON spec (file or inline)")
    parser.add_argument("--apply-style", type=int, default=None, metavar="TABLE_INDEX",
                        help="Apply style to table at index (0-based)")
    parser.add_argument("--style", choices=["three-line", "zebra", "grid", "none"], default=None,
                        help="Table style to apply")
    parser.add_argument("--merge", type=int, default=None, metavar="TABLE_INDEX",
                        help="Merge cells in table at index")
    parser.add_argument("--merge-spec", default=None,
                        help='Merge spec JSON like \'{"from":"A1","to":"D1"}\'')

    args = parser.parse_args()

    if not any([args.list_tables, args.add, args.apply_style is not None, args.merge is not None]):
        parser.print_help()
        sys.exit(1)

    try:
        files = read_docx(args.input)
        doc_root = parse_xml(files["word/document.xml"])
        body = doc_root.find(_w("body"))
        if body is None:
            print("Error: No body element in document.xml", file=sys.stderr)
            sys.exit(1)

        modified = False

        if args.list_tables:
            tables = find_tables(body)
            for idx, tbl in tables:
                num_rows, num_cols = count_table_rows_cols(tbl)
                print(f"  Table {idx}: {num_rows} rows x {num_cols} cols")
            return

        if args.add:
            spec_str = args.add
            if os.path.isfile(spec_str):
                with open(spec_str, "r", encoding="utf-8") as f:
                    spec = json.load(f)
            else:
                spec = json.loads(spec_str)
            tbl = create_table(spec)
            # Insert before the final sectPr
            sectPr = body.find(_w("sectPr"))
            if sectPr is not None:
                body.insert(list(body).index(sectPr), tbl)
            else:
                body.append(tbl)
            modified = True
            num_rows, num_cols = count_table_rows_cols(tbl)
            print(f"Added table: {num_rows} rows x {num_cols} cols")

        if args.apply_style is not None and args.style:
            tables = find_tables(body)
            tbl_idx = args.apply_style
            if tbl_idx >= len(tables):
                print(f"Error: Table index {tbl_idx} not found (document has {len(tables)} tables)", file=sys.stderr)
                sys.exit(1)
            _, tbl = tables[tbl_idx]
            apply_table_style(tbl, args.style)
            modified = True
            print(f"Applied '{args.style}' style to table {tbl_idx}")

        if args.merge is not None:
            if not args.merge_spec:
                print("Error: --merge-spec is required with --merge", file=sys.stderr)
                sys.exit(1)
            merge_spec = json.loads(args.merge_spec)
            tables = find_tables(body)
            tbl_idx = args.merge
            if tbl_idx >= len(tables):
                print(f"Error: Table index {tbl_idx} not found", file=sys.stderr)
                sys.exit(1)
            _, tbl = tables[tbl_idx]
            merge_cells(tbl, merge_spec)
            modified = True
            print(f"Merged cells in table {tbl_idx}: {merge_spec}")

        if modified:
            files["word/document.xml"] = serialize_xml(doc_root)
            out = args.output or args.input
            write_docx(files, out)
            print(f"Saved: {out}")

    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
