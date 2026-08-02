#!/usr/bin/env bash
# docx_preview.sh — Preview DOCX structure using Python stdlib
# Extracts and displays: document structure, styles, header/footer info,
# page setup, images count — all from the ZIP/XML without opening Word.
# Usage: bash docx_preview.sh document.docx

set -euo pipefail

if [ $# -ne 1 ]; then
    echo "Usage: bash docx_preview.sh document.docx"
    exit 1
fi

DOCX_FILE="$(realpath "$1")"

if [ ! -f "$DOCX_FILE" ]; then
    echo "ERROR: File not found: $DOCX_FILE" >&2
    exit 1
fi

# ── Find Python 3 ────────────────────────────────────────────────────────────
PY_CMD=""
if command -v python3 &>/dev/null; then
    PY_CMD="python3"
elif command -v python &>/dev/null; then
    PY_CMD="python"
fi

if [ -z "$PY_CMD" ]; then
    echo "ERROR: Python 3 not found. Required for DOCX preview." >&2
    exit 2
fi

# ── Run the preview script inline ────────────────────────────────────────────
"$PY_CMD" -c '
import zipfile
import xml.etree.ElementTree as ET
import sys
import os

docx_path = sys.argv[1]

# Namespace helpers
NS = {
    "w":   "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r":   "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "wp":  "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing",
    "a":   "http://schemas.openxmlformats.org/drawingml/2006/main",
    "pic": "http://schemas.openxmlformats.org/drawingml/2006/picture",
    "mc":  "http://schemas.openxmlformats.org/markup-compatibility/2006",
}

def qn(prefix, local):
    return "{" + NS[prefix] + "}" + local

def find_all(root, xpath):
    """Simple recursive find all matching tag."""
    results = []
    tag = xpath
    for elem in root.iter():
        if elem.tag == tag:
            results.append(elem)
    return results

def get_text(elem):
    """Get text content from w:t elements."""
    texts = []
    for t in elem.iter(qn("w", "t")):
        if t.text:
            texts.append(t.text)
    return "".join(texts)

try:
    with zipfile.ZipFile(docx_path, "r") as zf:
        names = zf.namelist()

        # ═══ Document Structure ═══════════════════════════════════════════════
        print("=" * 60)
        print("DOCX PREVIEW: " + os.path.basename(docx_path))
        print("=" * 60)

        # File size
        size = os.path.getsize(docx_path)
        print(f"\nFile size: {size:,} bytes")

        # ── Styles ────────────────────────────────────────────────────────────
        print("\n── Styles ───────────────────────────────────────────")
        try:
            styles_xml = zf.read("word/styles.xml")
            styles_root = ET.fromstring(styles_xml)
            style_count = 0
            for style in styles_root.findall(qn("w", "style")):
                style_id = style.get(qn("w", "styleId"), "?")
                style_type = style.get(qn("w", "type"), "?")
                name_elem = style.find(qn("w", "name"))
                style_name = name_elem.get(qn("w", "val"), "?") if name_elem is not None else "?"
                marker = "★" if style_type == "paragraph" and style_name.startswith("Heading") else " "
                print(f"  {marker} {style_id:30s} type={style_type:10s} name={style_name}")
                style_count += 1
            print(f"  Total: {style_count} styles")
        except KeyError:
            print("  (no styles.xml)")

        # ── Page Setup ────────────────────────────────────────────────────────
        print("\n── Page Setup ───────────────────────────────────────")
        try:
            doc_xml = zf.read("word/document.xml")
            doc_root = ET.fromstring(doc_xml)
            body = doc_root.find(qn("w", "body"))
            if body is not None:
                # Check sectPr in body (last section)
                body_sectPr = body.find(qn("w", "sectPr"))
                sectPrs = [body_sectPr] if body_sectPr is not None else []
                # Also check sectPr inside paragraph properties
                for pPr in body.iter(qn("w", "pPr")):
                    sp = pPr.find(qn("w", "sectPr"))
                    if sp is not None:
                        sectPrs.append(sp)

                for i, sectPr in enumerate(sectPrs):
                    print(f"  Section {i+1}:")
                    pgSz = sectPr.find(qn("w", "pgSz"))
                    if pgSz is not None:
                        w = pgSz.get(qn("w", "w"), "?")
                        h = pgSz.get(qn("w", "h"), "?")
                        # Convert twips to inches (1 inch = 1440 twips)
                        try:
                            w_in = int(w) / 1440
                            h_in = int(h) / 1440
                            orient = pgSz.get(qn("w", "orient"), "portrait")
                            print(f"    Size: {w_in:.2f}\" × {h_in:.2f}\" ({orient})")
                        except (ValueError, TypeError):
                            print(f"    Size: {w} × {h} twips")
                    pgMar = sectPr.find(qn("w", "pgMar"))
                    if pgMar is not None:
                        fields = {"top": "Top", "bottom": "Bottom", "left": "Left", "right": "Right",
                                  "header": "Header", "footer": "Footer", "gutter": "Gutter"}
                        parts = []
                        for attr, label in fields.items():
                            val = pgMar.get(qn("w", attr))
                            if val is not None:
                                try:
                                    val_in = int(val) / 1440
                                    parts.append(f"{label}={val_in:.2f}\"")
                                except (ValueError, TypeError):
                                    parts.append(f"{label}={val}twips")
                        if parts:
                            print(f"    Margins: {', '.join(parts)}")
                    cols = sectPr.find(qn("w", "cols"))
                    if cols is not None:
                        num = cols.get(qn("w", "num"), "1")
                        print(f"    Columns: {num}")
        except KeyError:
            print("  (no document.xml)")

        # ── Paragraphs & Tables ───────────────────────────────────────────────
        print("\n── Document Structure ────────────────────────────────")
        try:
            doc_xml = zf.read("word/document.xml")
            doc_root = ET.fromstring(doc_xml)
            body = doc_root.find(qn("w", "body"))
            if body is not None:
                para_count = 0
                table_count = 0
                heading_list = []
                for child in body:
                    tag = child.tag
                    if tag == qn("w", "p"):
                        para_count += 1
                        # Check for heading style
                        pPr = child.find(qn("w", "pPr"))
                        if pPr is not None:
                            pStyle = pPr.find(qn("w", "pStyle"))
                            if pStyle is not None:
                                style_val = pStyle.get(qn("w", "val"), "")
                                if style_val.startswith("Heading") or style_val.startswith("标题"):
                                    text = get_text(child)[:80]
                                    heading_list.append(f"  {style_val}: {text}")
                    elif tag == qn("w", "tbl"):
                        table_count += 1

                print(f"  Paragraphs: {para_count}")
                print(f"  Tables: {table_count}")
                if heading_list:
                    print(f"  Headings ({len(heading_list)}):")
                    for h in heading_list[:20]:
                        print(h)
                    if len(heading_list) > 20:
                        print(f"  ... and {len(heading_list) - 20} more")
        except KeyError:
            pass

        # ── Headers / Footers ────────────────────────────────────────────────
        print("\n── Headers & Footers ────────────────────────────────")
        hf_files = [n for n in names if n.startswith("word/") and (n.endswith(".xml")) and
                     ("header" in n or "footer" in n)]
        if hf_files:
            for hf in sorted(hf_files):
                text_preview = ""
                try:
                    hf_xml = zf.read(hf)
                    hf_root = ET.fromstring(hf_xml)
                    text_preview = get_text(hf_root)[:60]
                except Exception:
                    text_preview = "(parse error)"
                kind = "Header" if "header" in hf else "Footer"
                print(f"  {kind}: {os.path.basename(hf)} → \"{text_preview}\"")
        else:
            print("  (none)")

        # ── Images ────────────────────────────────────────────────────────────
        print("\n── Images & Media ───────────────────────────────────")
        img_exts = {".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".tif", ".emf", ".wmf", ".svg"}
        img_files = [n for n in names if os.path.splitext(n)[1].lower() in img_exts]
        if img_files:
            print(f"  Image count: {len(img_files)}")
            for img in img_files:
                info = zf.getinfo(img)
                print(f"    {os.path.basename(img):40s} {info.file_size:,} bytes")
        else:
            print("  Image count: 0")

        # ── Core Properties ───────────────────────────────────────────────────
        print("\n── Document Properties ──────────────────────────────")
        try:
            core_xml = zf.read("docProps/core.xml")
            core_root = ET.fromstring(core_xml)
            dc_ns = "http://purl.org/dc/elements/1.1/"
            cp_ns = "http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
            dcterms_ns = "http://purl.org/dc/terms/"
            for tag, label in [
                (f"{{{dc_ns}}}creator", "Author"),
                (f"{{{dc_ns}}}title", "Title"),
                (f"{{{dc_ns}}}description", "Description"),
                (f"{{{cp_ns}}}keywords", "Keywords"),
                (f"{{{dcterms_ns}}}created", "Created"),
                (f"{{{dcterms_ns}}}modified", "Modified"),
            ]:
                elem = core_root.find(tag)
                if elem is not None and elem.text:
                    print(f"  {label}: {elem.text}")
        except KeyError:
            print("  (no core.xml)")

        print("\n" + "=" * 60)

except zipfile.BadZipFile:
    print(f"ERROR: {docx_path} is not a valid ZIP/DOCX file", file=sys.stderr)
    sys.exit(1)
except Exception as e:
    print(f"ERROR: {e}", file=sys.stderr)
    sys.exit(1)
' "$DOCX_FILE"
