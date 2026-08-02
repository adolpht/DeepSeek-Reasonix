#!/usr/bin/env python3
"""docx_headers_footers.py - Add, modify, and manage headers/footers in DOCX files.

Uses ONLY Python standard library. No third-party dependencies.

Usage:
    python3 docx_headers_footers.py input.docx --page-numbers center --output out.docx
    python3 docx_headers_footers.py input.docx --page-numbers "page-x-of-y" --output out.docx
    python3 docx_headers_footers.py input.docx --header "CONFIDENTIAL" --align right --output out.docx
    python3 docx_headers_footers.py input.docx --page-numbers chinese-govt --output out.docx
    python3 docx_headers_footers.py input.docx --first-page-header "Title Page" --output out.docx
    python3 docx_headers_footers.py input.docx --header-tabs '{"left":"Company","center":"","right":"Date"}' --output out.docx
"""

import argparse
import json
import os
import sys
import re
import copy
import zipfile
import io
import uuid
import xml.etree.ElementTree as ET

NS = {
    "w":   "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r":   "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "ct":  "http://schemas.openxmlformats.org/package/2006/content-types",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
}
for prefix, uri in NS.items():
    ET.register_namespace(prefix, uri)

W = NS["w"]; R = NS["r"]; CT = NS["ct"]; REL = NS["rel"]

def _w(t): return f"{{{W}}}{t}"
def _r(t): return f"{{{R}}}{t}"

def read_docx(p):
    f = {}
    with zipfile.ZipFile(p, "r") as z:
        for n in z.namelist(): f[n] = z.read(n)
    return f

def write_docx(f, p):
    with zipfile.ZipFile(p, "w", zipfile.ZIP_DEFLATED) as z:
        for n, d in sorted(f.items()): z.writestr(n, d)

def parse_xml(b): return ET.fromstring(b)
def serialize_xml(root):
    ET.indent(root, space="  ")
    buf = io.BytesIO()
    ET.ElementTree(root).write(buf, encoding="UTF-8", xml_declaration=True)
    return buf.getvalue()

def w_elem(local, attrib=None):
    e = ET.Element(_w(local))
    if attrib:
        for k, v in attrib.items():
            e.set(_w(k) if not k.startswith("{") else k, str(v))
    return e

def make_run(text=None, field_code=None, bold=False, italic=False,
             font_ascii=None, font_ea=None, font_size=None, color=None,
             preserve_space=False):
    """Create a w:r element. Returns list of runs for field codes."""
    if field_code:
        r1 = w_elem("r"); fc1 = w_elem("fldChar", {"fldCharType": "begin"}); r1.append(fc1)
        r2 = w_elem("r"); it = w_elem("instrText"); it.text = f" {field_code} "; r2.append(it)
        r3 = w_elem("r"); fc2 = w_elem("fldChar", {"fldCharType": "separate"}); r3.append(fc2)
        r4 = w_elem("r"); fc3 = w_elem("fldChar", {"fldCharType": "end"}); r4.append(fc3)
        return [r1, r2, r3, r4]

    r = w_elem("r")
    need_rPr = bold or italic or font_ascii or font_ea or font_size or color
    if need_rPr:
        rPr = w_elem("rPr")
        if font_ascii or font_ea:
            rf = w_elem("rFonts")
            if font_ascii: rf.set(_w("ascii"), font_ascii); rf.set(_w("hAnsi"), font_ascii)
            if font_ea: rf.set(_w("eastAsia"), font_ea)
            rPr.append(rf)
        if font_size:
            sz = w_elem("sz"); sz.set(_w("val"), str(font_size)); rPr.append(sz)
        if bold: rPr.append(w_elem("b"))
        if italic: rPr.append(w_elem("i"))
        if color:
            c = w_elem("color"); c.set(_w("val"), color); rPr.append(c)
        r.append(rPr)
    if text is not None:
        t = w_elem("t"); t.text = text
        if preserve_space or (text and (text[0] == ' ' or text[-1] == ' ')):
            t.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
        r.append(t)
    return r

def make_paragraph_with_runs(runs, justify=None, style=None, spacing=None):
    p = w_elem("p")
    if style or justify or spacing:
        pPr = w_elem("pPr")
        if style: pPr.append(w_elem("pStyle", {"val": style}))
        if justify: pPr.append(w_elem("jc", {"val": justify}))
        if spacing:
            sp = w_elem("spacing")
            for k, v in spacing.items(): sp.set(_w(k), str(v))
            pPr.append(sp)
        p.append(pPr)
    for run in runs:
        if isinstance(run, list):
            for r in run: p.append(r)
        else:
            p.append(run)
    return p

def get_next_rid(rels):
    mx = 0
    for r in rels:
        m = re.match(r'rId(\d+)', r.get("Id", ""))
        if m: mx = max(mx, int(m.group(1)))
    return mx + 1

def add_relationship(rels, rid, rtype, target):
    r = ET.SubElement(rels, f"{{{REL}}}Relationship")
    r.set("Id", rid); r.set("Type", rtype); r.set("Target", target)

def add_content_type(ct, pn, ctype):
    o = ET.SubElement(ct, f"{{{CT}}}Override")
    o.set("PartName", pn); o.set("ContentType", ctype)

# Header/Footer builders
def build_simple_page_number_footer(justify="center"):
    runs = [make_run(field_code="PAGE")]
    return make_paragraph_with_runs(runs, justify=justify, style="Footer")

def build_page_x_of_y_footer():
    runs = [make_run(text="Page "), make_run(field_code="PAGE"),
            make_run(text=" of "), make_run(field_code="NUMPAGES")]
    return make_paragraph_with_runs(runs, justify="center", style="Footer")

def build_chinese_govt_footer():
    """GB/T 9704: - X - , Si Hao (14pt) FangSong"""
    runs = [
        make_run(text="\u2014 ", font_ascii="FangSong", font_ea="\u4eff\u5b8b",
                 font_size=28, preserve_space=True),
        make_run(field_code="PAGE"),
        make_run(text=" \u2014", font_ascii="FangSong", font_ea="\u4eff\u5b8b",
                 font_size=28, preserve_space=True),
    ]
    return make_paragraph_with_runs(runs, justify="center", style="Footer",
                                    spacing={"before": "0", "after": "0", "line": "312", "lineRule": "exact"})

def build_text_header(text, justify="left"):
    runs = [make_run(text=text)]
    return make_paragraph_with_runs(runs, justify=justify, style="Header")

def build_tab_aligned_header(left_text="", center_text="", right_text=""):
    p = w_elem("p")
    pPr = w_elem("pPr")
    pPr.append(w_elem("pStyle", {"val": "Header"}))
    tabs = w_elem("tabs")
    tabs.append(w_elem("tab", {"val": "right", "pos": "9072"}))
    tabs.append(w_elem("tab", {"val": "center", "pos": "4536"}))
    pPr.append(tabs)
    sp = w_elem("spacing", {"after": "0", "line": "240", "lineRule": "auto"})
    pPr.append(sp)
    p.append(pPr)
    if left_text: p.append(make_run(text=left_text))
    if center_text:
        rt = w_elem("r"); rt.append(w_elem("tab")); p.append(rt)
        p.append(make_run(text=center_text))
    if right_text:
        rt = w_elem("r"); rt.append(w_elem("tab")); p.append(rt)
        p.append(make_run(text=right_text))
    return p

def process(input_path, output_path=None, page_numbers=None, header_text=None,
            header_align="left", first_page_header=None, header_tabs=None):
    files = read_docx(input_path)
    doc = parse_xml(files["word/document.xml"])
    body = doc.find(_w("body"))
    if body is None: raise ValueError("No body element found")

    drp = "word/_rels/document.xml.rels"
    rels = parse_xml(files[drp]) if drp in files else ET.Element(f"{{{REL}}}Relationships")
    ctp = "[Content_Types].xml"
    ct = parse_xml(files[ctp]) if ctp in files else ET.Element(f"{{{CT}}}Types")

    sectPr = body.find(_w("sectPr"))
    if sectPr is None: sectPr = w_elem("sectPr"); body.append(sectPr)

    next_rid = get_next_rid(rels)
    hdr_cnt = 1; ftr_cnt = 1; modified = False

    def add_header_part(xml_elems, htype="default"):
        nonlocal next_rid, hdr_cnt, modified
        rid = f"rId{next_rid}"; next_rid += 1
        target = f"header{hdr_cnt}.xml"; hdr_cnt += 1
        hdr_root = w_elem("hdr")
        for c in xml_elems: hdr_root.append(c)
        files[f"word/{target}"] = serialize_xml(hdr_root)
        add_relationship(rels, rid,
                        "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header", target)
        add_content_type(ct, f"/word/{target}",
                        "application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml")
        ref = w_elem("headerReference"); ref.set(_w("type"), htype); ref.set(_r("id"), rid)
        pgSz = sectPr.find(_w("pgSz"))
        if pgSz is not None: sectPr.insert(list(sectPr).index(pgSz), ref)
        else: sectPr.append(ref)
        modified = True
        return rid

    def add_footer_part(xml_elems, ftype="default"):
        nonlocal next_rid, ftr_cnt, modified
        rid = f"rId{next_rid}"; next_rid += 1
        target = f"footer{ftr_cnt}.xml"; ftr_cnt += 1
        ftr_root = w_elem("ftr")
        for c in xml_elems: ftr_root.append(c)
        files[f"word/{target}"] = serialize_xml(ftr_root)
        add_relationship(rels, rid,
                        "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer", target)
        add_content_type(ct, f"/word/{target}",
                        "application/vnd.openxmlformats-officedocument.wordprocessingml.footer+xml")
        ref = w_elem("footerReference"); ref.set(_w("type"), ftype); ref.set(_r("id"), rid)
        pgSz = sectPr.find(_w("pgSz"))
        if pgSz is not None: sectPr.insert(list(sectPr).index(pgSz), ref)
        else: sectPr.append(ref)
        modified = True
        return rid

    if page_numbers:
        if page_numbers == "center":
            add_footer_part([build_simple_page_number_footer("center")])
            print("Added centered page numbers to footer")
        elif page_numbers == "page-x-of-y":
            add_footer_part([build_page_x_of_y_footer()])
            print("Added 'Page X of Y' to footer")
        elif page_numbers == "chinese-govt":
            add_footer_part([build_chinese_govt_footer()])
            print("Added Chinese government page numbers to footer")
        else:
            add_footer_part([build_simple_page_number_footer(page_numbers)])
            print(f"Added page numbers ({page_numbers}) to footer")

    if header_text:
        add_header_part([build_text_header(header_text, justify=header_align)])
        print(f"Added header: '{header_text}' ({header_align})")

    if first_page_header:
        if sectPr.find(_w("titlePg")) is None: sectPr.append(w_elem("titlePg"))
        add_header_part([build_text_header(first_page_header, justify="center")], htype="first")
        print(f"Added first page header: '{first_page_header}'")

    if header_tabs:
        if isinstance(header_tabs, str):
            tabs_spec = json.load(open(header_tabs, encoding="utf-8")) if os.path.isfile(header_tabs) else json.loads(header_tabs)
        else: tabs_spec = header_tabs
        add_header_part([build_tab_aligned_header(
            left_text=tabs_spec.get("left", ""), center_text=tabs_spec.get("center", ""),
            right_text=tabs_spec.get("right", ""))])
        print("Added tab-aligned header")

    if modified:
        files["word/document.xml"] = serialize_xml(doc)
        files[drp] = serialize_xml(rels); files[ctp] = serialize_xml(ct)
        out = output_path or input_path; write_docx(files, out); print(f"Saved: {out}")
    else:
        print("No changes made.")

def main():
    parser = argparse.ArgumentParser(description="Add, modify, and manage headers/footers in DOCX files.")
    parser.add_argument("input", help="Input DOCX file path")
    parser.add_argument("--output", default=None, help="Output DOCX file path")
    parser.add_argument("--page-numbers", default=None,
                        choices=["center", "left", "right", "page-x-of-y", "chinese-govt"])
    parser.add_argument("--header", default=None, help="Add header text")
    parser.add_argument("--align", choices=["left", "center", "right"], default="left")
    parser.add_argument("--first-page-header", default=None)
    parser.add_argument("--header-tabs", default=None)

    args = parser.parse_args()
    if not any([args.page_numbers, args.header, args.first_page_header, args.header_tabs]):
        parser.print_help(); sys.exit(1)

    try:
        process(input_path=args.input, output_path=args.output,
                page_numbers=args.page_numbers, header_text=args.header,
                header_align=args.align, first_page_header=args.first_page_header,
                header_tabs=args.header_tabs)
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr); sys.exit(1)

if __name__ == "__main__":
    main()
