#!/usr/bin/env python3
"""docx_fields.py — Add fields, TOC, bookmarks, and hyperlinks to DOCX files.

Uses ONLY Python standard library. No third-party dependencies.

Usage:
    python3 docx_fields.py input.docx --add-toc '{"position":0,"levels":3}' --output out.docx
    python3 docx_fields.py input.docx --add-field '{"type":"DATE","format":"yyyy-MM-dd","position":0}' --output out.docx
    python3 docx_fields.py input.docx --add-bookmark '{"name":"chapter1","paragraph_index":5}' --output out.docx
    python3 docx_fields.py input.docx --add-hyperlink '{"url":"https://example.com","text":"Click","paragraph_index":3}' --output out.docx
    python3 docx_fields.py input.docx --add-ref '{"bookmark":"chapter1","text":"Chapter 1","paragraph_index":8}' --output out.docx
    python3 docx_fields.py input.docx --add-field '{"type":"PAGE","position":0}' --output out.docx
    python3 docx_fields.py input.docx --update-fields-on-open --output out.docx
    python3 docx_fields.py input.docx --list
"""

import argparse
import json
import os
import sys
import copy
import uuid
import zipfile
import io
import datetime
import xml.etree.ElementTree as ET

# ── Namespaces ───────────────────────────────────────────────────────────────────

NS = {
    "w":   "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r":   "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "ct":  "http://schemas.openxmlformats.org/package/2006/content-types",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
    "wp":  "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing",
    "mc":  "http://schemas.openxmlformats.org/markup-compatibility/2006",
}
for prefix, uri in NS.items():
    ET.register_namespace(prefix, uri)

W = NS["w"]
R = NS["r"]
CT = NS["ct"]
REL = NS["rel"]
MC = NS["mc"]


def _w(tag):
    return f"{{{W}}}{tag}"


def _r(tag):
    return f"{{{R}}}{tag}"


# ── DOCX I/O ─────────────────────────────────────────────────────────────────────

def read_docx(docx_path):
    files = {}
    with zipfile.ZipFile(docx_path, "r") as zf:
        for name in zf.namelist():
            files[name] = zf.read(name)
    return files


def write_docx(files, output_path):
    with zipfile.ZipFile(output_path, "w", zipfile.ZIP_DEFLATED) as zf:
        for name, data in sorted(files.items()):
            zf.writestr(name, data)


def parse_xml(xml_bytes):
    return ET.fromstring(xml_bytes)


def serialize_xml(root):
    ET.indent(root, space="  ")
    buf = io.BytesIO()
    ET.ElementTree(root).write(buf, encoding="UTF-8", xml_declaration=True)
    return buf.getvalue()


# ── XML Helpers ──────────────────────────────────────────────────────────────────

def w_elem(local, attrib=None, text=None):
    e = ET.Element(_w(local))
    if attrib:
        for k, v in attrib.items():
            e.set(_w(k) if not k.startswith("{") else k, str(v))
    if text is not None:
        e.text = str(text)
    return e


def _ensure_sectpr_last(body):
    """Ensure sectPr is the last child of body."""
    sect_pr = body.find(_w("sectPr"))
    if sect_pr is not None:
        body.remove(sect_pr)
        body.append(sect_pr)


def _get_paragraphs(body):
    """Get all w:p children of body."""
    return [child for child in body if child.tag == _w("p")]


def _insert_at_paragraph_index(body, element, paragraph_index):
    """Insert an element at the given paragraph index.

    paragraph_index: -1 = at end (before sectPr), 0 = at beginning, N = after Nth paragraph.
    """
    paragraphs = _get_paragraphs(body)

    if paragraph_index == -1 or paragraph_index >= len(paragraphs):
        sect_pr = body.find(_w("sectPr"))
        if sect_pr is not None:
            idx = list(body).index(sect_pr)
            body.insert(idx, element)
        else:
            body.append(element)
    elif paragraph_index == 0:
        body.insert(0, element)
    else:
        target_p = paragraphs[paragraph_index - 1]
        idx = list(body).index(target_p)
        body.insert(idx + 1, element)


# ── Field Code Builders ──────────────────────────────────────────────────────────

def _make_field_runs(field_code, result_text=""):
    """Create the runs for a complex field: begin → instrText → separate → result → end."""
    runs = []

    # begin
    r1 = w_elem("r")
    r1.append(w_elem("fldChar", {"fldCharType": "begin"}))
    runs.append(r1)

    # instrText
    r2 = w_elem("r")
    instrText = w_elem("instrText")
    instrText.text = f" {field_code} "
    instrText.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
    r2.append(instrText)
    runs.append(r2)

    # separate
    r3 = w_elem("r")
    r3.append(w_elem("fldChar", {"fldCharType": "separate"}))
    runs.append(r3)

    # result text (placeholder)
    r4 = w_elem("r")
    if result_text:
        t = w_elem("t")
        t.text = result_text
        t.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
        r4.append(t)
    runs.append(r4)

    # end
    r5 = w_elem("r")
    r5.append(w_elem("fldChar", {"fldCharType": "end"}))
    runs.append(r5)

    return runs


def _make_field_paragraph(field_code, result_text="", style=None):
    """Create a paragraph containing a complex field."""
    p = w_elem("p")
    if style:
        pPr = w_elem("pPr")
        pPr.append(w_elem("pStyle", {"val": style}))
        p.append(pPr)
    for run in _make_field_runs(field_code, result_text):
        p.append(run)
    return p


# ── TOC ──────────────────────────────────────────────────────────────────────────

def add_toc(files, spec):
    """Add a Table of Contents using w:sdt (structured document tag).

    spec = {"position": int, "levels": int}
    """
    levels = spec.get("levels", 3)
    position = spec.get("position", 0)

    doc_root = parse_xml(files["word/document.xml"])
    body = doc_root.find(_w("body"))

    # Build the SDT wrapping the TOC field
    sdt = w_elem("sdt")

    # sdtPr (SDT properties)
    sdtPr = w_elem("sdtPr")
    doc_part_uniq = w_elem("docPartUnique")
    doc_part_gallery = w_elem("docPartGallery", {"val": "Table of Contents"})
    doc_part_uniq.append(doc_part_gallery)
    doc_part_obj = w_elem("docPartObj")
    doc_part_obj.append(doc_part_uniq)
    sdtPr.append(doc_part_obj)
    sdt.append(sdtPr)

    # sdtContent
    sdtContent = w_elem("sdtContent")

    # TOC heading paragraph
    toc_heading = w_elem("p")
    pPr = w_elem("pPr")
    pPr.append(w_elem("pStyle", {"val": "TOCHeading"}))
    toc_heading.append(pPr)
    r_heading = w_elem("r")
    t_heading = w_elem("t")
    t_heading.text = "Table of Contents"
    r_heading.append(t_heading)
    toc_heading.append(r_heading)
    sdtContent.append(toc_heading)

    # TOC field paragraph
    toc_para = w_elem("p")
    for run in _make_field_runs(
        f'TOC \\o "1-{levels}" \\h \\z \\u',
        ""
    ):
        toc_para.append(run)
    sdtContent.append(toc_para)

    sdt.append(sdtContent)

    _insert_at_paragraph_index(body, sdt, position)
    _ensure_sectpr_last(body)
    files["word/document.xml"] = serialize_xml(doc_root)

    # Set updateFields in settings.xml so Word prompts to update
    _set_update_fields(files)

    return True


# ── Fields (DATE, PAGE, NUMPAGES, TIME, SEQ, PAGEREF, STYLEREF) ─────────────────

FIELD_RESULT_DEFAULTS = {
    "DATE":      lambda fmt: datetime.datetime.now().strftime(fmt or "%Y-%m-%d"),
    "PAGE":      lambda fmt: "1",
    "NUMPAGES":  lambda fmt: "1",
    "TIME":      lambda fmt: datetime.datetime.now().strftime(fmt or "%H:%M"),
    "SEQ":       lambda fmt: "1",
    "PAGEREF":   lambda fmt: "1",
    "STYLEREF":  lambda fmt: "",
}


def add_field(files, spec):
    """Add a field paragraph. spec = {"type": "DATE", "format": "...", "position": int}"""
    field_type = spec.get("type", "PAGE")
    fmt = spec.get("format", "")
    position = spec.get("position", -1)

    # Build field code string
    if field_type == "DATE":
        field_code = f'DATE \\@ "{fmt}" \\h' if fmt else "DATE \\h"
        result_text = FIELD_RESULT_DEFAULTS["DATE"](fmt)
    elif field_type == "PAGE":
        field_code = "PAGE"
        result_text = "1"
    elif field_type == "NUMPAGES":
        field_code = "NUMPAGES"
        result_text = "1"
    elif field_type == "TIME":
        field_code = f'TIME \\@ "{fmt}"' if fmt else "TIME"
        result_text = FIELD_RESULT_DEFAULTS["TIME"](fmt)
    elif field_type == "SEQ":
        seq_id = spec.get("sequence", "Figure")
        field_code = f"SEQ {seq_id} \\* ARABIC"
        result_text = "1"
    elif field_type == "PAGEREF":
        bm = spec.get("bookmark", "")
        field_code = f"PAGEREF {bm}"
        result_text = "1"
    elif field_type == "STYLEREF":
        style_id = spec.get("styleId", "Heading1")
        field_code = f'STYLEREF "{style_id}"'
        result_text = ""
    else:
        field_code = field_type
        result_text = ""

    p = _make_field_paragraph(field_code, result_text)

    doc_root = parse_xml(files["word/document.xml"])
    body = doc_root.find(_w("body"))
    _insert_at_paragraph_index(body, p, position)
    _ensure_sectpr_last(body)
    files["word/document.xml"] = serialize_xml(doc_root)

    return True


# ── Bookmark ─────────────────────────────────────────────────────────────────────

def add_bookmark(files, spec):
    """Add a bookmark to a paragraph. spec = {"name": "chapter1", "paragraph_index": int}"""
    name = spec.get("name", "bookmark1")
    para_idx = spec.get("paragraph_index", 0)

    doc_root = parse_xml(files["word/document.xml"])
    body = doc_root.find(_w("body"))
    paragraphs = _get_paragraphs(body)

    if para_idx < 0 or para_idx >= len(paragraphs):
        print(f"Warning: paragraph_index {para_idx} out of range, appending to last paragraph", file=sys.stderr)
        para_idx = len(paragraphs) - 1

    target_p = paragraphs[para_idx]

    # Generate a unique bookmark ID
    bm_id = _next_bookmark_id(doc_root)

    # Insert bookmarkStart and bookmarkEnd into the paragraph
    bm_start = w_elem("bookmarkStart", {"id": str(bm_id), "name": name})
    bm_end = w_elem("bookmarkEnd", {"id": str(bm_id)})

    # Insert bookmarkStart at the beginning (after pPr if present)
    pPr = target_p.find(_w("pPr"))
    if pPr is not None:
        idx = list(target_p).index(pPr)
        target_p.insert(idx + 1, bm_start)
    else:
        target_p.insert(0, bm_start)

    # Append bookmarkEnd before the end
    target_p.append(bm_end)

    files["word/document.xml"] = serialize_xml(doc_root)
    return True


def _next_bookmark_id(doc_root):
    """Find the next available bookmark ID in the document."""
    max_id = 0
    for bm_start in doc_root.iter(_w("bookmarkStart")):
        try:
            bid = int(bm_start.get(_w("id"), "0"))
            if bid > max_id:
                max_id = bid
        except ValueError:
            pass
    return max_id + 1


# ── Hyperlink ────────────────────────────────────────────────────────────────────

def add_hyperlink(files, spec):
    """Add a hyperlink. spec = {"url": "...", "text": "...", "paragraph_index": int}"""
    url = spec.get("url", "https://example.com")
    text = spec.get("text", url)
    para_idx = spec.get("paragraph_index", -1)

    # Add relationship to document.xml.rels
    rels_path = "word/_rels/document.xml.rels"
    if rels_path not in files:
        print("Error: word/_rels/document.xml.rels not found", file=sys.stderr)
        return False

    rels_root = parse_xml(files[rels_path])

    # Find next rId
    max_rid = 0
    for rel in rels_root.findall(f"{{{REL}}}Relationship"):
        rid = rel.get("Id", "rId0")
        try:
            num = int(rid.replace("rId", ""))
            if num > max_rid:
                max_rid = num
        except ValueError:
            pass

    new_rid = f"rId{max_rid + 1}"
    hyperlink_type = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"

    new_rel = ET.SubElement(rels_root, f"{{{REL}}}Relationship")
    new_rel.set("Id", new_rid)
    new_rel.set("Type", hyperlink_type)
    new_rel.set("Target", url)
    new_rel.set("TargetMode", "External")
    files[rels_path] = serialize_xml(rels_root)

    # Build hyperlink paragraph
    p = w_elem("p")
    hyperlink = w_elem("hyperlink")
    hyperlink.set(f"{{{R}}}id", new_rid)

    r = w_elem("r")
    rPr = w_elem("rPr")
    rPr.append(w_elem("rStyle", {"val": "Hyperlink"}))
    r.append(rPr)
    t = w_elem("t")
    t.text = text
    if text and (text[0] == ' ' or text[-1] == ' '):
        t.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
    r.append(t)
    hyperlink.append(r)
    p.append(hyperlink)

    doc_root = parse_xml(files["word/document.xml"])
    body = doc_root.find(_w("body"))
    _insert_at_paragraph_index(body, p, para_idx)
    _ensure_sectpr_last(body)
    files["word/document.xml"] = serialize_xml(doc_root)

    return True


# ── Cross-reference (REF field) ─────────────────────────────────────────────────

def add_ref(files, spec):
    """Add a cross-reference (REF field) to a bookmark.

    spec = {"bookmark": "chapter1", "text": "Chapter 1", "paragraph_index": int}
    """
    bookmark = spec.get("bookmark", "")
    text = spec.get("text", f"[Reference to {bookmark}]")
    para_idx = spec.get("paragraph_index", -1)

    field_code = f"REF {bookmark} \\h"
    p = _make_field_paragraph(field_code, text)

    doc_root = parse_xml(files["word/document.xml"])
    body = doc_root.find(_w("body"))
    _insert_at_paragraph_index(body, p, para_idx)
    _ensure_sectpr_last(body)
    files["word/document.xml"] = serialize_xml(doc_root)

    return True


# ── Update Fields on Open ────────────────────────────────────────────────────────

def _set_update_fields(files):
    """Set w:updateFields in word/settings.xml so Word prompts to update fields."""
    settings_path = "word/settings.xml"
    if settings_path not in files:
        return

    settings_root = parse_xml(files[settings_path])

    # Check if updateFields already exists
    existing = settings_root.find(_w("updateFields"))
    if existing is not None:
        return

    update_fields = w_elem("updateFields")
    update_fields.set(_w("val"), "true")

    # Insert at the beginning of settings
    settings_root.insert(0, update_fields)
    files[settings_path] = serialize_xml(settings_root)


def update_fields_on_open(files):
    """Set w:updateFields in settings.xml."""
    _set_update_fields(files)
    return True


# ── List all fields ──────────────────────────────────────────────────────────────

def list_fields(files):
    """List all fields in the document."""
    if "word/document.xml" not in files:
        return []

    doc_root = parse_xml(files["word/document.xml"])
    result = []

    # Find all instrText elements (field codes)
    for instr in doc_root.iter(_w("instrText")):
        code = (instr.text or "").strip()
        if code:
            result.append({"type": code.split()[0] if code.split() else code, "code": code})

    # Find all bookmarkStart elements
    for bm_start in doc_root.iter(_w("bookmarkStart")):
        bm_name = bm_start.get(_w("name"), "")
        if bm_name and not bm_name.startswith("_"):
            result.append({"type": "BOOKMARK", "name": bm_name, "id": bm_start.get(_w("id"), "")})

    # Find all hyperlinks
    for hyperlink in doc_root.iter(_w("hyperlink")):
        rid = hyperlink.get(f"{{{R}}}id", "")
        # Try to get text from the run inside
        text = ""
        for t in hyperlink.iter(_w("t")):
            text += (t.text or "")
        result.append({"type": "HYPERLINK", "rId": rid, "text": text})

    return result


# ── Main ─────────────────────────────────────────────────────────────────────────

def process_fields(input_path, output_path=None, add_toc_spec=None, add_field_spec=None,
                   add_bookmark_spec=None, add_hyperlink_spec=None, add_ref_spec=None,
                   update_fields_flag=False, list_flag=False):
    """Process fields in a DOCX file."""
    files = read_docx(input_path)

    if list_flag:
        fields = list_fields(files)
        if not fields:
            print("No fields found.")
        for f in fields:
            ftype = f["type"]
            if ftype == "BOOKMARK":
                print(f"  BOOKMARK  name={f['name']}  id={f['id']}")
            elif ftype == "HYPERLINK":
                print(f"  HYPERLINK rId={f['rId']}  text=\"{f['text']}\"")
            else:
                print(f"  FIELD     type={ftype}  code=\"{f.get('code', '')}\"")
        return

    modified = False

    if add_toc_spec:
        if os.path.isfile(add_toc_spec):
            with open(add_toc_spec, "r", encoding="utf-8") as f:
                spec = json.load(f)
        else:
            spec = json.loads(add_toc_spec)
        add_toc(files, spec)
        modified = True
        levels = spec.get("levels", 3)
        print(f"Added TOC (levels 1-{levels})")

    if add_field_spec:
        if os.path.isfile(add_field_spec):
            with open(add_field_spec, "r", encoding="utf-8") as f:
                spec = json.load(f)
        else:
            spec = json.loads(add_field_spec)
        add_field(files, spec)
        modified = True
        print(f"Added {spec.get('type', 'PAGE')} field")

    if add_bookmark_spec:
        if os.path.isfile(add_bookmark_spec):
            with open(add_bookmark_spec, "r", encoding="utf-8") as f:
                spec = json.load(f)
        else:
            spec = json.loads(add_bookmark_spec)
        add_bookmark(files, spec)
        modified = True
        print(f"Added bookmark: {spec.get('name', 'bookmark1')}")

    if add_hyperlink_spec:
        if os.path.isfile(add_hyperlink_spec):
            with open(add_hyperlink_spec, "r", encoding="utf-8") as f:
                spec = json.load(f)
        else:
            spec = json.loads(add_hyperlink_spec)
        add_hyperlink(files, spec)
        modified = True
        print(f"Added hyperlink: {spec.get('url', '')}")

    if add_ref_spec:
        if os.path.isfile(add_ref_spec):
            with open(add_ref_spec, "r", encoding="utf-8") as f:
                spec = json.load(f)
        else:
            spec = json.loads(add_ref_spec)
        add_ref(files, spec)
        modified = True
        print(f"Added cross-reference to: {spec.get('bookmark', '')}")

    if update_fields_flag:
        update_fields_on_open(files)
        modified = True
        print("Set updateFields on open")

    if modified:
        out = output_path or input_path
        write_docx(files, out)
        print(f"Saved: {out}")


def main():
    parser = argparse.ArgumentParser(
        description="Add fields, TOC, bookmarks, and hyperlinks to DOCX files.",
        epilog="Uses ONLY Python standard library. No third-party dependencies.",
    )
    parser.add_argument("input", help="Input DOCX file path")
    parser.add_argument("--output", default=None,
                        help="Output DOCX file path (default: modify in-place)")
    parser.add_argument("--add-toc", default=None,
                        help='Add TOC from JSON spec: \'{"position":0,"levels":3}\'')
    parser.add_argument("--add-field", default=None,
                        help='Add field from JSON spec: \'{"type":"DATE","format":"yyyy-MM-dd","position":0}\'')
    parser.add_argument("--add-bookmark", default=None,
                        help='Add bookmark from JSON spec: \'{"name":"ch1","paragraph_index":5}\'')
    parser.add_argument("--add-hyperlink", default=None,
                        help='Add hyperlink from JSON spec: \'{"url":"https://...","text":"Click","paragraph_index":3}\'')
    parser.add_argument("--add-ref", default=None,
                        help='Add cross-reference from JSON spec: \'{"bookmark":"ch1","text":"Ch 1","paragraph_index":8}\'')
    parser.add_argument("--update-fields-on-open", action="store_true",
                        help="Set w:updateFields in settings.xml so Word prompts to update")
    parser.add_argument("--list", action="store_true",
                        help="List all fields, bookmarks, and hyperlinks in the document")

    args = parser.parse_args()

    if not any([args.add_toc, args.add_field, args.add_bookmark, args.add_hyperlink,
                args.add_ref, args.update_fields_on_open, args.list]):
        parser.print_help()
        sys.exit(1)

    try:
        process_fields(
            input_path=args.input,
            output_path=args.output,
            add_toc_spec=args.add_toc,
            add_field_spec=args.add_field,
            add_bookmark_spec=args.add_bookmark,
            add_hyperlink_spec=args.add_hyperlink,
            add_ref_spec=args.add_ref,
            update_fields_flag=args.update_fields_on_open,
            list_flag=args.list,
        )
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
