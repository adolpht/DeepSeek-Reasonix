#!/usr/bin/env python3
"""docx_images.py - Add and manage images in DOCX files.

Uses ONLY Python standard library. No third-party dependencies.

Usage:
    python3 docx_images.py input.docx --add-inline '{"path":"photo.jpg","after_paragraph":0}' --output out.docx
    python3 docx_images.py input.docx --add-floating '{"path":"logo.png","x":0,"y":0,"width":2000000,"height":1000000,"wrap":"square"}' --output out.docx
    python3 docx_images.py input.docx --list
    python3 docx_images.py input.docx --replace 0 --with new_image.png --output out.docx
    python3 docx_images.py input.docx --export-dir ./images/
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
import struct
import hashlib
import base64
import xml.etree.ElementTree as ET

# Namespaces
NS = {
    "w":   "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r":   "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "wp":  "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing",
    "a":   "http://schemas.openxmlformats.org/drawingml/2006/main",
    "pic": "http://schemas.openxmlformats.org/drawingml/2006/picture",
    "ct":  "http://schemas.openxmlformats.org/package/2006/content-types",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
}
for prefix, uri in NS.items():
    ET.register_namespace(prefix, uri)

W = NS["w"]; R = NS["r"]; WP = NS["wp"]; A = NS["a"]; PIC = NS["pic"]; CT = NS["ct"]; REL = NS["rel"]

def _w(t): return f"{{{W}}}{t}"
def _r(t): return f"{{{R}}}{t}"
def _wp(t): return f"{{{WP}}}{t}"
def _a(t): return f"{{{A}}}{t}"
def _pic(t): return f"{{{PIC}}}{t}"

# Image type mapping
IMAGE_TYPES = {
    ".png": ("image/png", "png"), ".jpg": ("image/jpeg", "jpeg"), ".jpeg": ("image/jpeg", "jpeg"),
    ".gif": ("image/gif", "gif"), ".bmp": ("image/bmp", "bmp"),
    ".tiff": ("image/tiff", "tiff"), ".tif": ("image/tiff", "tiff"),
    ".emf": ("image/x-emf", "emf"), ".wmf": ("image/x-wmf", "wmf"), ".svg": ("image/svg+xml", "svg"),
}
EMU_PER_INCH = 914400; EMU_PER_CM = 360000

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

def get_image_type(filepath):
    ext = os.path.splitext(filepath)[1].lower()
    return IMAGE_TYPES.get(ext, ("image/png", "png"))

def detect_image_dimensions(filepath):
    try:
        with open(filepath, "rb") as f: header = f.read(32)
        if header[:8] == b'\x89PNG\r\n\x1a\n':
            with open(filepath, "rb") as f:
                f.seek(16); w = struct.unpack('>I', f.read(4))[0]; h = struct.unpack('>I', f.read(4))[0]
            return w, h
        if header[:2] == b'\xff\xd8':
            with open(filepath, "rb") as f:
                f.read(2)
                while True:
                    marker = f.read(2)
                    if not marker or len(marker) < 2: break
                    if marker[0] != 0xFF: break
                    if marker[1] in (0xC0, 0xC2):
                        f.read(3); h = struct.unpack('>H', f.read(2))[0]; w = struct.unpack('>H', f.read(2))[0]
                        return w, h
                    length = struct.unpack('>H', f.read(2))[0]; f.read(length - 2)
        if header[:6] in (b'GIF87a', b'GIF89a'):
            return struct.unpack('<H', header[6:8])[0], struct.unpack('<H', header[8:10])[0]
        if header[:2] == b'BM':
            return struct.unpack('<I', header[18:22])[0], struct.unpack('<I', header[22:26])[0]
    except Exception: pass
    return None, None

def calculate_emu(pw, ph, dpi=96.0, max_w_inches=6.5):
    cx = int(pw * EMU_PER_INCH / dpi); cy = int(ph * EMU_PER_INCH / dpi)
    mx = int(max_w_inches * EMU_PER_INCH)
    if cx > mx: s = mx / cx; cx = int(cx * s); cy = int(cy * s)
    return cx, cy

def _build_graphic(rel_id, name, image_id, cx, cy):
    graphic = ET.Element(_a("graphic"))
    gd = ET.SubElement(graphic, _a("graphicData")); gd.set("uri", "http://schemas.openxmlformats.org/drawingml/2006/picture")
    pic = ET.SubElement(gd, _pic("pic"))
    nv = ET.SubElement(pic, _pic("nvPicPr"))
    cn = ET.SubElement(nv, _pic("cNvPr")); cn.set("id", str(image_id)); cn.set("name", name)
    ET.SubElement(nv, _pic("cNvPicPr"))
    bf = ET.SubElement(pic, _pic("blipFill"))
    bl = ET.SubElement(bf, _a("blip")); bl.set(_r("embed"), rel_id)
    st = ET.SubElement(bf, _a("stretch")); ET.SubElement(st, _a("fillRect"))
    sp = ET.SubElement(pic, _pic("spPr"))
    xf = ET.SubElement(sp, _a("xfrm"))
    of = ET.SubElement(xf, _a("off")); of.set("x", "0"); of.set("y", "0")
    ex = ET.SubElement(xf, _a("ext")); ex.set("cx", str(cx)); ex.set("cy", str(cy))
    pg = ET.SubElement(sp, _a("prstGeom")); pg.set("prst", "rect"); ET.SubElement(pg, _a("avLst"))
    return graphic

def build_inline_drawing(rel_id, name, desc, cx, cy):
    iid = abs(hash(uuid.uuid4())) % (2**31)
    d = ET.Element(_w("drawing"))
    il = ET.SubElement(d, _wp("inline"))
    il.set("distT", "0"); il.set("distB", "0"); il.set("distL", "0"); il.set("distR", "0")
    ex = ET.SubElement(il, _wp("extent")); ex.set("cx", str(cx)); ex.set("cy", str(cy))
    ef = ET.SubElement(il, _wp("effectExtent")); ef.set("l", "0"); ef.set("t", "0"); ef.set("r", "0"); ef.set("b", "0")
    dp = ET.SubElement(il, _wp("docPr")); dp.set("id", str(iid)); dp.set("name", name); dp.set("descr", desc)
    cg = ET.SubElement(il, _wp("cNvGraphicFramePr"))
    gl = ET.SubElement(cg, _a("graphicFrameLocks")); gl.set("noChangeAspect", "1")
    il.append(_build_graphic(rel_id, name, iid, cx, cy))
    return d

def build_floating_drawing(rel_id, name, desc, cx, cy, x=0, y=0, wrap="square", h_anc="text", v_anc="top"):
    iid = abs(hash(uuid.uuid4())) % (2**31)
    d = ET.Element(_w("drawing"))
    an = ET.SubElement(d, _wp("anchor"))
    an.set("distT", "0"); an.set("distB", "0"); an.set("distL", "0"); an.set("distR", "0")
    an.set("simplePos", "0"); an.set("relativeHeight", "251658240"); an.set("behindDoc", "0")
    an.set("locked", "0"); an.set("layoutInCell", "1"); an.set("allowOverlap", "1")
    sp = ET.SubElement(an, _wp("simplePos")); sp.set("x", "0"); sp.set("y", "0")
    ph = ET.SubElement(an, _wp("positionH")); ph.set("relativeFrom", h_anc)
    po = ET.SubElement(ph, _wp("posOffset")); po.text = str(x)
    pv = ET.SubElement(an, _wp("positionV")); pv.set("relativeFrom", v_anc)
    po2 = ET.SubElement(pv, _wp("posOffset")); po2.text = str(y)
    ex = ET.SubElement(an, _wp("extent")); ex.set("cx", str(cx)); ex.set("cy", str(cy))
    ef = ET.SubElement(an, _wp("effectExtent")); ef.set("l", "0"); ef.set("t", "0"); ef.set("r", "0"); ef.set("b", "0")
    if wrap == "square":
        ws = ET.SubElement(an, _wp("wrapSquare")); ws.set("wrapText", "bothSides")
    elif wrap == "tight":
        wt = ET.SubElement(an, _wp("wrapTight")); wt.set("wrapText", "bothSides")
    elif wrap == "through":
        wth = ET.SubElement(an, _wp("wrapThrough")); wth.set("wrapText", "bothSides")
    elif wrap == "topAndBottom":
        ET.SubElement(an, _wp("wrapTopAndBottom"))
    elif wrap == "none":
        ET.SubElement(an, _wp("wrapNone"))
    dp = ET.SubElement(an, _wp("docPr")); dp.set("id", str(iid)); dp.set("name", name); dp.set("descr", desc)
    cg = ET.SubElement(an, _wp("cNvGraphicFramePr"))
    gl = ET.SubElement(cg, _a("graphicFrameLocks")); gl.set("noChangeAspect", "1")
    an.append(_build_graphic(rel_id, name, iid, cx, cy))
    return d

def get_next_rid(rels):
    mx = 0
    for r in rels:
        m = re.match(r'rId(\d+)', r.get("Id", ""))
        if m: mx = max(mx, int(m.group(1)))
    return mx + 1

def add_rel(rels, rid, rtype, target):
    r = ET.SubElement(rels, f"{{{REL}}}Relationship"); r.set("Id", rid); r.set("Type", rtype); r.set("Target", target)

def add_ct_override(ct, pn, ctype):
    o = ET.SubElement(ct, f"{{{CT}}}Override"); o.set("PartName", pn); o.set("ContentType", ctype)

def add_ct_default(ct, ext, ctype):
    d = ET.SubElement(ct, f"{{{CT}}}Default"); d.set("Extension", ext); d.set("ContentType", ctype)

def find_images(files, rels):
    img_type = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
    return [{"rid": r.get("Id",""), "target": r.get("Target","")} for r in rels if r.get("Type","") == img_type]

def add_inline_image(files, doc, rels, ct, spec):
    fp = spec["path"]; after = spec.get("after_paragraph", -1)
    w_emu = spec.get("width"); h_emu = spec.get("height")
    if not os.path.isfile(fp): raise FileNotFoundError(fp)
    with open(fp, "rb") as f: idata = f.read()
    mtype, ext = get_image_type(fp)
    ih = hashlib.md5(idata).hexdigest()[:8]
    ifn = f"image{ih}.{ext}"; ip = f"word/media/{ifn}"
    files[ip] = idata
    nrid = get_next_rid(rels); rid = f"rId{nrid}"
    add_rel(rels, rid, "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image", f"media/{ifn}")
    add_ct_default(ct, ext, mtype)
    if w_emu and h_emu: cx, cy = w_emu, h_emu
    else:
        pw, ph = detect_image_dimensions(fp)
        cx, cy = calculate_emu(pw, ph) if pw and ph else (4*EMU_PER_INCH, 3*EMU_PER_INCH)
    drw = build_inline_drawing(rid, ifn, f"Image: {os.path.basename(fp)}", cx, cy)
    body = doc.find(_w("body"))
    p = ET.SubElement(body, _w("p")); r = ET.SubElement(p, _w("r")); r.append(drw)
    if after >= 0:
        ch = list(body); pi = None; pc = 0
        for i, c in enumerate(ch):
            if c.tag == _w("p"):
                if pc == after: pi = i; break
                pc += 1
        if pi is not None:
            body.remove(p); body.insert(pi + 1, p)
        else:
            body.remove(p)
            sp = body.find(_w("sectPr"))
            if sp is not None: body.insert(list(body).index(sp), p)
            else: body.append(p)
    return rid

def add_floating_image(files, doc, rels, ct, spec):
    fp = spec["path"]; x = spec.get("x", 0); y = spec.get("y", 0)
    w_emu = spec.get("width", 2*EMU_PER_INCH); h_emu = spec.get("height", 2*EMU_PER_INCH)
    wrap = spec.get("wrap", "square"); h_anc = spec.get("h_anchor", "text"); v_anc = spec.get("v_anchor", "top")
    if not os.path.isfile(fp): raise FileNotFoundError(fp)
    with open(fp, "rb") as f: idata = f.read()
    mtype, ext = get_image_type(fp)
    ih = hashlib.md5(idata).hexdigest()[:8]
    ifn = f"image{ih}.{ext}"; ip = f"word/media/{ifn}"
    files[ip] = idata
    nrid = get_next_rid(rels); rid = f"rId{nrid}"
    add_rel(rels, rid, "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image", f"media/{ifn}")
    add_ct_default(ct, ext, mtype)
    drw = build_floating_drawing(rid, ifn, f"Image: {os.path.basename(fp)}", w_emu, h_emu, x, y, wrap, h_anc, v_anc)
    body = doc.find(_w("body"))
    p = ET.SubElement(body, _w("p")); r = ET.SubElement(p, _w("r")); r.append(drw)
    sp = body.find(_w("sectPr"))
    if sp is not None: body.remove(p); body.insert(list(body).index(sp), p)
    return rid

def replace_image(files, rels, idx, new_fp):
    if not os.path.isfile(new_fp): raise FileNotFoundError(new_fp)
    imgs = find_images(files, rels)
    if idx >= len(imgs): raise IndexError(f"Image index {idx} not found")
    tgt = imgs[idx]["target"]
    ip = f"word/{tgt}" if not tgt.startswith("word/") else tgt
    with open(new_fp, "rb") as f: files[ip] = f.read()

def export_images(files, edir):
    os.makedirs(edir, exist_ok=True); c = 0
    for n, d in files.items():
        if n.startswith("word/media/"):
            fn = os.path.basename(n)
            with open(os.path.join(edir, fn), "wb") as f: f.write(d)
            c += 1; print(f"  Exported: {fn}")
    return c

def main():
    parser = argparse.ArgumentParser(description="Add and manage images in DOCX files.")
    parser.add_argument("input", help="Input DOCX file path")
    parser.add_argument("--output", default=None, help="Output DOCX file path")
    parser.add_argument("--add-inline", default=None, help="Add inline image from JSON spec")
    parser.add_argument("--add-floating", default=None, help="Add floating image from JSON spec")
    parser.add_argument("--list", action="store_true", dest="list_images", help="List images")
    parser.add_argument("--replace", type=int, default=None, help="Replace image at index")
    parser.add_argument("--with", dest="with_file", default=None, help="New image file for replace")
    parser.add_argument("--export-dir", default=None, help="Export images to directory")

    args = parser.parse_args()
    if not any([args.add_inline, args.add_floating, args.list_images, args.replace is not None, args.export_dir]):
        parser.print_help(); sys.exit(1)

    try:
        files = read_docx(args.input)
        doc = parse_xml(files["word/document.xml"])
        drp = "word/_rels/document.xml.rels"
        rels = parse_xml(files[drp]) if drp in files else ET.Element(f"{{{REL}}}Relationships")
        ctp = "[Content_Types].xml"
        ct = parse_xml(files[ctp]) if ctp in files else ET.Element(f"{{{CT}}}Types")
        mod = False

        if args.list_images:
            for i, img in enumerate(find_images(files, rels)):
                tgt = img["target"]; sz = len(files.get(f"word/{tgt}" if not tgt.startswith("word/") else tgt, b""))
                print(f"  Image {i}: {tgt} ({sz} bytes, rid={img['rid']})")
            return

        if args.add_inline:
            s = json.loads(args.add_inline) if not os.path.isfile(args.add_inline) else json.load(open(args.add_inline, encoding="utf-8"))
            rid = add_inline_image(files, doc, rels, ct, s); mod = True
            print(f"Added inline image: rid={rid}")

        if args.add_floating:
            s = json.loads(args.add_floating) if not os.path.isfile(args.add_floating) else json.load(open(args.add_floating, encoding="utf-8"))
            rid = add_floating_image(files, doc, rels, ct, s); mod = True
            print(f"Added floating image: rid={rid}")

        if args.replace is not None:
            if not args.with_file: print("Error: --with is required with --replace", file=sys.stderr); sys.exit(1)
            replace_image(files, rels, args.replace, args.with_file); mod = True
            print(f"Replaced image {args.replace}")

        if args.export_dir:
            c = export_images(files, args.export_dir)
            print(f"Exported {c} images to {args.export_dir}")

        if mod:
            files["word/document.xml"] = serialize_xml(doc)
            files[drp] = serialize_xml(rels); files[ctp] = serialize_xml(ct)
            out = args.output or args.input; write_docx(files, out); print(f"Saved: {out}")
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr); sys.exit(1)

if __name__ == "__main__":
    main()
