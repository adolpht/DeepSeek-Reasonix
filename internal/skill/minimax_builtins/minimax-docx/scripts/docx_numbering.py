#!/usr/bin/env python3
"""docx_numbering.py — Create and manage numbered/bulleted lists in DOCX files.

Uses ONLY Python standard library. No third-party dependencies.

Usage:
    python3 docx_numbering.py input.docx --add-bullets '{"items":["Item 1","Item 2"],"position":0}' --output out.docx
    python3 docx_numbering.py input.docx --add-numbered '{"items":["Step 1","Step 2"],"format":"decimal","position":0}' --output out.docx
    python3 docx_numbering.py input.docx --add-numbered '{"items":["章节一","章节二"],"format":"chineseCounting","position":0}' --output out.docx
    python3 docx_numbering.py input.docx --add-multilevel '{"items":[{"level":0,"text":"Ch 1"},{"level":1,"text":"Sec 1.1"}]}' --output out.docx
    python3 docx_numbering.py input.docx --list-defs
"""

import argparse
import json
import os
import sys
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
CT = NS["ct"]
REL = NS["rel"]


def _w(tag):
    return f"{{{W}}}{tag}"


# ── Numbering format mappings ────────────────────────────────────────────────────

NUMFMT_MAP = {
    "decimal":                "decimal",
    "decimalParenthesis":     "decimalEnclosedParen",
    "lowerLetter":            "lowerLetter",
    "upperLetter":            "upperLetter",
    "lowerRoman":             "lowerRoman",
    "upperRoman":             "upperRoman",
    "bullet":                 "bullet",
    "chineseCounting":        "ideographTraditional",
    "chineseCountingThousand": "chineseCountingThousand",
    "ideographTraditional":   "ideographTraditional",
    "ideographZodiac":        "ideographZodiac",
    "decimalFullWidth":       "decimalFullWidth",
    "decimalEnclosedCircle":  "decimalEnclosedCircle",
}

LEVEL_TEXT_MAP = {
    "decimal":                "%1.",
    "decimalParenthesis":     "%1)",
    "lowerLetter":            "%1)",
    "upperLetter":            "%1)",
    "lowerRoman":             "%1.",
    "upperRoman":             "%1.",
    "bullet":                 "\u2022",
    "chineseCounting":        "%1\u3001",
    "chineseCountingThousand": "%1\u3001",
    "ideographTraditional":   "%1\u3001",
    "ideographZodiac":        "%1\u3001",
    "decimalFullWidth":       "%1.",
    "decimalEnclosedCircle":  "%1",
}

BULLET_SYMBOL_FONTS = {
    "bullet": ("Symbol", "Symbol"),
}


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


def _max_abstract_num_id(numbering_root):
    """Find the highest abstractNumId in the numbering root."""
    max_id = -1
    for an in numbering_root.findall(_w("abstractNum")):
        aid = int(an.get(_w("abstractNumId"), "0"))
        if aid > max_id:
            max_id = aid
    return max_id


def _max_num_id(numbering_root):
    """Find the highest numId in the numbering root."""
    max_id = 0
    for n in numbering_root.findall(_w("num")):
        nid = int(n.get(_w("numId"), "0"))
        if nid > max_id:
            max_id = nid
    return max_id


def _get_or_create_numbering(files):
    """Get or create word/numbering.xml. Return (numbering_root, modified_files_flag)."""
    if "word/numbering.xml" in files:
        return parse_xml(files["word/numbering.xml"]), False
    # Create minimal numbering.xml
    root = w_elem("numbering")
    return root, True


def _ensure_numbering_rels(files):
    """Ensure word/_rels/document.xml.rels has a relationship to numbering.xml."""
    rels_path = "word/_rels/document.xml.rels"
    if rels_path not in files:
        return

    rels_root = parse_xml(files[rels_path])
    numbering_type = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"

    for rel in rels_root.findall(f"{{{REL}}}Relationship"):
        if rel.get("Type") == numbering_type:
            return  # Already exists

    # Find the next available rId
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
    new_rel = ET.SubElement(rels_root, f"{{{REL}}}Relationship")
    new_rel.set("Id", new_rid)
    new_rel.set("Type", numbering_type)
    new_rel.set("Target", "numbering.xml")
    files[rels_path] = serialize_xml(rels_root)


def _ensure_content_type(files):
    """Ensure [Content_Types].xml has an override for numbering.xml."""
    ct_path = "[Content_Types].xml"
    if ct_path not in files:
        return
    ct_root = parse_xml(files[ct_path])
    numbering_ct = "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"

    for override in ct_root.findall(f"{{{CT}}}Override"):
        if override.get("PartName") == "/word/numbering.xml":
            return

    new_override = ET.SubElement(ct_root, f"{{{CT}}}Override")
    new_override.set("PartName", "/word/numbering.xml")
    new_override.set("ContentType", numbering_ct)
    files[ct_path] = serialize_xml(ct_root)


# ── Build level element ──────────────────────────────────────────────────────────

def _build_level(ilvl, numfmt_key, start=1, level_text=None, left_indent=None, hanging=None):
    """Build a w:lvl element for an abstractNum."""
    fmt_val = NUMFMT_MAP.get(numfmt_key, "decimal")
    if level_text is None:
        level_text = LEVEL_TEXT_MAP.get(numfmt_key, "%1.")

    if left_indent is None:
        left_indent = str((ilvl + 1) * 720)
    if hanging is None:
        hanging = "360" if fmt_val != "bullet" else "360"

    lvl = w_elem("lvl", {"ilvl": str(ilvl)})
    lvl.append(w_elem("start", {"val": str(start)}))
    lvl.append(w_elem("numFmt", {"val": fmt_val}))
    lvl.append(w_elem("lvlText", {"val": level_text}))
    lvl.append(w_elem("lvlJc", {"val": "left"}))

    # Paragraph properties
    pPr = w_elem("pPr")
    pPr.append(w_elem("ind", {"left": left_indent, "hanging": hanging}))
    lvl.append(pPr)

    # Run properties (for bullet, use Symbol font)
    rPr = w_elem("rPr")
    if fmt_val == "bullet":
        rPr.append(w_elem("rFonts", {"ascii": "Symbol", "hAnsi": "Symbol", "hint": "default"}))
    lvl.append(rPr)

    return lvl


# ── Build multi-level level text ─────────────────────────────────────────────────

def _multilevel_level_text(ilvl, fmt_key):
    """Build level text for multi-level decimal (1., 1.1., 1.1.1., etc.)."""
    if fmt_key == "decimal":
        parts = ".".join(f"%{i+1}" for i in range(ilvl + 1))
        return f"{parts}."
    return LEVEL_TEXT_MAP.get(fmt_key, "%1.")


# ── Core operations ──────────────────────────────────────────────────────────────

def add_bullets(files, spec):
    """Add a bullet list. spec = {"items": [...], "position": int}"""
    items = spec.get("items", [])
    position = spec.get("position", -1)

    numbering_root, _ = _get_or_create_numbering(files)
    abstract_num_id = _max_abstract_num_id(numbering_root) + 1
    num_id = _max_num_id(numbering_root) + 1

    # Create abstractNum with bullet level 0
    abstract_num = w_elem("abstractNum", {"abstractNumId": str(abstract_num_id)})
    abstract_num.append(w_elem("multiLevelType", {"val": "hybridMultilevel"}))
    abstract_num.append(_build_level(0, "bullet", start=1))
    # Insert abstractNum at beginning (after existing ones)
    numbering_root.insert(0, abstract_num)

    # Create numbering instance
    num_inst = w_elem("num", {"numId": str(num_id)})
    num_inst.append(w_elem("abstractNumId", {"val": str(abstract_num_id)}))
    numbering_root.append(num_inst)

    files["word/numbering.xml"] = serialize_xml(numbering_root)
    _ensure_numbering_rels(files)
    _ensure_content_type(files)

    # Insert paragraphs into document.xml
    _insert_list_paragraphs(files, items, num_id, 0, position)

    return num_id


def add_numbered(files, spec):
    """Add a numbered list. spec = {"items": [...], "format": "decimal", "position": int}"""
    items = spec.get("items", [])
    fmt = spec.get("format", "decimal")
    position = spec.get("position", -1)

    numbering_root, _ = _get_or_create_numbering(files)
    abstract_num_id = _max_abstract_num_id(numbering_root) + 1
    num_id = _max_num_id(numbering_root) + 1

    abstract_num = w_elem("abstractNum", {"abstractNumId": str(abstract_num_id)})
    abstract_num.append(w_elem("multiLevelType", {"val": "hybridMultilevel"}))
    abstract_num.append(_build_level(0, fmt, start=1))
    numbering_root.insert(0, abstract_num)

    num_inst = w_elem("num", {"numId": str(num_id)})
    num_inst.append(w_elem("abstractNumId", {"val": str(abstract_num_id)}))
    numbering_root.append(num_inst)

    files["word/numbering.xml"] = serialize_xml(numbering_root)
    _ensure_numbering_rels(files)
    _ensure_content_type(files)

    _insert_list_paragraphs(files, items, num_id, 0, position)

    return num_id


def add_multilevel(files, spec):
    """Add a multi-level list. spec = {"items": [{"level": 0, "text": "..."}, ...]}"""
    items = spec.get("items", [])

    numbering_root, _ = _get_or_create_numbering(files)
    abstract_num_id = _max_abstract_num_id(numbering_root) + 1
    num_id = _max_num_id(numbering_root) + 1

    # Determine unique levels from items
    levels = sorted(set(item.get("level", 0) for item in items))

    abstract_num = w_elem("abstractNum", {"abstractNumId": str(abstract_num_id)})
    abstract_num.append(w_elem("multiLevelType", {"val": "hybridMultilevel"}))

    for ilvl in levels:
        level_text = _multilevel_level_text(ilvl, "decimal")
        left_indent = str((ilvl + 1) * 720)
        abstract_num.append(_build_level(ilvl, "decimal", start=1,
                                         level_text=level_text, left_indent=left_indent))

    numbering_root.insert(0, abstract_num)

    num_inst = w_elem("num", {"numId": str(num_id)})
    num_inst.append(w_elem("abstractNumId", {"val": str(abstract_num_id)}))
    numbering_root.append(num_inst)

    files["word/numbering.xml"] = serialize_xml(numbering_root)
    _ensure_numbering_rels(files)
    _ensure_content_type(files)

    # Insert paragraphs with appropriate levels
    position = spec.get("position", -1)
    doc_root = parse_xml(files["word/document.xml"])
    body = doc_root.find(_w("body"))

    paras = []
    for item in items:
        ilvl = item.get("level", 0)
        text = item.get("text", "")
        paras.append(_make_list_paragraph(num_id, ilvl, text))

    _insert_paragraphs_at_position(body, paras, position)

    # Ensure sectPr stays last
    _ensure_sectpr_last(body)
    files["word/document.xml"] = serialize_xml(doc_root)

    return num_id


def list_defs(files):
    """List all numbering definitions in the document."""
    if "word/numbering.xml" not in files:
        return []

    numbering_root = parse_xml(files["word/numbering.xml"])
    result = []

    for an in numbering_root.findall(_w("abstractNum")):
        an_id = an.get(_w("abstractNumId"), "")
        ml_type = an.find(_w("multiLevelType"))
        ml_val = ml_type.get(_w("val"), "") if ml_type is not None else ""

        levels = []
        for lvl in an.findall(_w("lvl")):
            ilvl = lvl.get(_w("ilvl"), "0")
            nf = lvl.find(_w("numFmt"))
            nf_val = nf.get(_w("val"), "") if nf is not None else ""
            lt = lvl.find(_w("lvlText"))
            lt_val = lt.get(_w("val"), "") if lt is not None else ""
            start = lvl.find(_w("start"))
            start_val = start.get(_w("val"), "") if start is not None else ""
            levels.append({
                "level": ilvl,
                "format": nf_val,
                "levelText": lt_val,
                "start": start_val,
            })

        # Find which num instances reference this abstractNum
        num_ids = []
        for num in numbering_root.findall(_w("num")):
            anid = num.find(_w("abstractNumId"))
            if anid is not None and anid.get(_w("val"), "") == str(an_id):
                num_ids.append(num.get(_w("numId"), ""))

        result.append({
            "abstractNumId": an_id,
            "multiLevelType": ml_val,
            "numIds": num_ids,
            "levels": levels,
        })

    return result


# ── Paragraph creation helpers ───────────────────────────────────────────────────

def _make_list_paragraph(num_id, ilvl, text):
    """Create a paragraph with numPr referencing the given numId and level."""
    p = w_elem("p")
    pPr = w_elem("pPr")

    # Use ListParagraph style if available
    pPr.append(w_elem("pStyle", {"val": "ListParagraph"}))

    numPr = w_elem("numPr")
    numPr.append(w_elem("ilvl", {"val": str(ilvl)}))
    numPr.append(w_elem("numId", {"val": str(num_id)}))
    pPr.append(numPr)

    p.append(pPr)

    # Add text run
    r = w_elem("r")
    t = w_elem("t")
    t.text = text
    if text and (text[0] == ' ' or text[-1] == ' '):
        t.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
    r.append(t)
    p.append(r)

    return p


def _insert_list_paragraphs(files, items, num_id, ilvl, position):
    """Insert list paragraphs into document.xml at the given position."""
    doc_root = parse_xml(files["word/document.xml"])
    body = doc_root.find(_w("body"))

    paras = [_make_list_paragraph(num_id, ilvl, item) for item in items]
    _insert_paragraphs_at_position(body, paras, position)

    _ensure_sectpr_last(body)
    files["word/document.xml"] = serialize_xml(doc_root)


def _insert_paragraphs_at_position(body, paras, position):
    """Insert paragraphs at the given position in the body.

    position: -1 = at end (before sectPr), 0 = at beginning, N = after Nth paragraph.
    """
    # Collect all body children that are paragraphs
    children = list(body)
    sect_pr = None
    para_children = []

    for child in children:
        if child.tag == _w("sectPr"):
            sect_pr = child
        elif child.tag == _w("p"):
            para_children.append(child)

    if position == -1 or position >= len(para_children):
        # Insert before sectPr
        if sect_pr is not None:
            idx = list(body).index(sect_pr)
            for i, p in enumerate(paras):
                body.insert(idx + i, p)
        else:
            for p in paras:
                body.append(p)
    elif position == 0:
        # Insert at the beginning
        for i, p in enumerate(paras):
            body.insert(i, p)
    else:
        # Insert after the Nth paragraph
        # Find the index of the Nth paragraph in body's children
        count = 0
        insert_idx = 0
        for i, child in enumerate(list(body)):
            if child.tag == _w("p"):
                count += 1
                if count == position:
                    insert_idx = i + 1
                    break
        for i, p in enumerate(paras):
            body.insert(insert_idx + i, p)


def _ensure_sectpr_last(body):
    """Ensure sectPr is the last child of body."""
    sect_pr = body.find(_w("sectPr"))
    if sect_pr is not None:
        body.remove(sect_pr)
        body.append(sect_pr)


# ── Main ─────────────────────────────────────────────────────────────────────────

def process_numbering(input_path, output_path=None, add_bullets_spec=None,
                      add_numbered_spec=None, add_multilevel_spec=None,
                      list_defs_flag=False):
    """Process numbering in a DOCX file."""
    files = read_docx(input_path)

    if list_defs_flag:
        defs = list_defs(files)
        if not defs:
            print("No numbering definitions found.")
        for d in defs:
            print(f"  AbstractNum {d['abstractNumId']} ({d['multiLevelType']})")
            print(f"    NumIDs: {', '.join(d['numIds']) or 'none'}")
            for lvl in d["levels"]:
                print(f"    Level {lvl['level']}: fmt={lvl['format']} text=\"{lvl['levelText']}\" start={lvl['start']}")
        return

    modified = False

    if add_bullets_spec:
        if os.path.isfile(add_bullets_spec):
            with open(add_bullets_spec, "r", encoding="utf-8") as f:
                spec = json.load(f)
        else:
            spec = json.loads(add_bullets_spec)
        num_id = add_bullets(files, spec)
        modified = True
        print(f"Added bullet list (numId={num_id}, {len(spec.get('items', []))} items)")

    if add_numbered_spec:
        if os.path.isfile(add_numbered_spec):
            with open(add_numbered_spec, "r", encoding="utf-8") as f:
                spec = json.load(f)
        else:
            spec = json.loads(add_numbered_spec)
        num_id = add_numbered(files, spec)
        modified = True
        fmt = spec.get("format", "decimal")
        print(f"Added numbered list (numId={num_id}, format={fmt}, {len(spec.get('items', []))} items)")

    if add_multilevel_spec:
        if os.path.isfile(add_multilevel_spec):
            with open(add_multilevel_spec, "r", encoding="utf-8") as f:
                spec = json.load(f)
        else:
            spec = json.loads(add_multilevel_spec)
        num_id = add_multilevel(files, spec)
        modified = True
        items = spec.get("items", [])
        levels = sorted(set(it.get("level", 0) for it in items))
        print(f"Added multi-level list (numId={num_id}, levels={levels}, {len(items)} items)")

    if modified:
        out = output_path or input_path
        write_docx(files, out)
        print(f"Saved: {out}")


def main():
    parser = argparse.ArgumentParser(
        description="Create and manage numbered/bulleted lists in DOCX files.",
        epilog="Uses ONLY Python standard library. No third-party dependencies.",
    )
    parser.add_argument("input", help="Input DOCX file path")
    parser.add_argument("--output", default=None,
                        help="Output DOCX file path (default: modify in-place)")
    parser.add_argument("--add-bullets", default=None,
                        help='Add bullet list from JSON spec: \'{"items":["A","B"],"position":0}\'')
    parser.add_argument("--add-numbered", default=None,
                        help='Add numbered list from JSON spec: \'{"items":["A","B"],"format":"decimal","position":0}\'')
    parser.add_argument("--add-multilevel", default=None,
                        help='Add multi-level list from JSON spec: \'{"items":[{"level":0,"text":"Ch 1"}]}\'')
    parser.add_argument("--list-defs", action="store_true",
                        help="List all numbering definitions in the document")

    args = parser.parse_args()

    if not any([args.add_bullets, args.add_numbered, args.add_multilevel, args.list_defs]):
        parser.print_help()
        sys.exit(1)

    try:
        process_numbering(
            input_path=args.input,
            output_path=args.output,
            add_bullets_spec=args.add_bullets,
            add_numbered_spec=args.add_numbered,
            add_multilevel_spec=args.add_multilevel,
            list_defs_flag=args.list_defs,
        )
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
