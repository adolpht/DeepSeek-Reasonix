#!/usr/bin/env python3
"""docx_create.py — Create new DOCX files from scratch with full page setup, styles, and content.

Uses ONLY Python standard library (zipfile, xml.etree.ElementTree, etc.).
No third-party dependencies.

Usage examples:
    python3 docx_create.py --output report.docx --page-size A4 --margins "1440,1440,1440,1440" --orientation portrait
    python3 docx_create.py --output report.docx --content content.json
    python3 docx_create.py --output report.docx --recipe academic-thesis --title "My Thesis"
"""

import argparse
import json
import os
import sys
import re
import uuid
import datetime
import zipfile
import io
import xml.etree.ElementTree as ET

# ── OpenXML Namespaces ──────────────────────────────────────────────────────────

NS = {
    "w":   "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r":   "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "wp":  "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing",
    "a":   "http://schemas.openxmlformats.org/drawingml/2006/main",
    "pic": "http://schemas.openxmlformats.org/drawingml/2006/picture",
    "ct":  "http://schemas.openxmlformats.org/package/2006/content-types",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
    "dcmitype": "http://purl.org/dc/terms/",
    "cp":   "http://schemas.openxmlformats.org/package/2006/metadata/core-properties",
    "ep":   "http://schemas.openxmlformats.org/officeDocument/2006/extended-properties",
}

# Register all namespaces so ET doesn't invent ns0, ns1, etc.
for prefix, uri in NS.items():
    ET.register_namespace(prefix, uri)

W = NS["w"]
R = NS["r"]
CT = NS["ct"]
REL = NS["rel"]
CP = NS["cp"]
DC = NS["dcmitype"]
EP = NS["ep"]

# ── Page Sizes (width, height in DXA/twips) ─────────────────────────────────────

PAGE_SIZES = {
    "Letter": (12240, 15840),
    "A4":     (11906, 16838),
    "A3":     (16838, 23811),
    "B5":     (9977, 14173),
    "16K":    (9787, 15118),
}

# ── Aesthetic Recipes ────────────────────────────────────────────────────────────

RECIPES = {
    "modern-corporate": {
        "page_size": "A4", "margins": [1440, 1440, 1440, 1440], "orientation": "portrait",
        "body_font_ascii": "Calibri", "body_font_ea": "微软雅黑", "body_font_cs": "Times New Roman",
        "body_font_size": 22, "line_spacing": 276, "line_rule": "auto",
        "h1_font_ascii": "Calibri", "h1_font_ea": "微软雅黑", "h1_font_size": 32,
        "h1_color": "1F3864", "h1_before": 360, "h1_after": 120,
        "h2_font_ascii": "Calibri", "h2_font_ea": "微软雅黑", "h2_font_size": 28,
        "justification": "left", "first_indent": 0,
    },
    "academic-thesis": {
        "page_size": "A4", "margins": [1440, 1440, 1800, 1440], "orientation": "portrait",
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 24, "line_spacing": 480, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 32,
        "h1_color": "000000", "h1_before": 480, "h1_after": 240,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 28,
        "justification": "both", "first_indent": 720,
    },
    "executive-brief": {
        "page_size": "Letter", "margins": [1080, 1080, 1260, 1260], "orientation": "portrait",
        "body_font_ascii": "Arial", "body_font_ea": "微软雅黑", "body_font_cs": "Arial",
        "body_font_size": 20, "line_spacing": 240, "line_rule": "auto",
        "h1_font_ascii": "Arial", "h1_font_ea": "微软雅黑", "h1_font_size": 28,
        "h1_color": "333333", "h1_before": 240, "h1_after": 80,
        "h2_font_ascii": "Arial", "h2_font_ea": "微软雅黑", "h2_font_size": 24,
        "justification": "left", "first_indent": 0,
    },
    "chinese-government": {
        "page_size": "A4", "margins": [2268, 1701, 1701, 1531], "orientation": "portrait",
        "body_font_ascii": "FangSong", "body_font_ea": "仿宋", "body_font_cs": "FangSong",
        "body_font_size": 44, "line_spacing": 570, "line_rule": "exact",
        "h1_font_ascii": "SimSun", "h1_font_ea": "小标宋", "h1_font_size": 44,
        "h1_color": "000000", "h1_before": 0, "h1_after": 0,
        "h2_font_ascii": "SimHei", "h2_font_ea": "黑体", "h2_font_size": 36,
        "justification": "both", "first_indent": 888,
    },
    "minimal-modern": {
        "page_size": "A4", "margins": [2160, 2160, 1800, 1800], "orientation": "portrait",
        "body_font_ascii": "Helvetica Neue", "body_font_ea": "苹方-简", "body_font_cs": "Helvetica Neue",
        "body_font_size": 22, "line_spacing": 360, "line_rule": "auto",
        "h1_font_ascii": "Helvetica Neue", "h1_font_ea": "苹方-简", "h1_font_size": 36,
        "h1_color": "333333", "h1_before": 600, "h1_after": 200,
        "h2_font_ascii": "Helvetica Neue", "h2_font_ea": "苹方-简", "h2_font_size": 28,
        "justification": "left", "first_indent": 0,
    },
    "ieee": {
        "page_size": "Letter", "margins": [1080, 0, 1134, 1134], "orientation": "portrait",
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 20, "line_spacing": 240, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 24,
        "h1_color": "000000", "h1_before": 180, "h1_after": 60,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 22,
        "justification": "both", "first_indent": 0,
    },
    "apa-7th": {
        "page_size": "Letter", "margins": [1440, 1440, 1440, 1440], "orientation": "portrait",
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 24, "line_spacing": 480, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 24,
        "h1_color": "000000", "h1_before": 0, "h1_after": 0,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 24,
        "justification": "left", "first_indent": 720,
    },
    "mla-9th": {
        "page_size": "Letter", "margins": [1440, 1440, 1440, 1440], "orientation": "portrait",
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 24, "line_spacing": 480, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 24,
        "h1_color": "000000", "h1_before": 0, "h1_after": 0,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 24,
        "justification": "left", "first_indent": 720,
    },
    "chicago": {
        "page_size": "Letter", "margins": [1440, 1440, 1440, 1440], "orientation": "portrait",
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 24, "line_spacing": 480, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 28,
        "h1_color": "000000", "h1_before": 360, "h1_after": 240,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 24,
        "justification": "left", "first_indent": 720,
    },
    "springer-lncs": {
        "page_size": "A4", "margins": [1134, 1134, 1134, 1134], "orientation": "portrait",
        "body_font_ascii": "Computer Modern", "body_font_ea": "宋体", "body_font_cs": "Computer Modern",
        "body_font_size": 20, "line_spacing": 240, "line_rule": "auto",
        "h1_font_ascii": "Computer Modern", "h1_font_ea": "黑体", "h1_font_size": 24,
        "h1_color": "000000", "h1_before": 180, "h1_after": 60,
        "h2_font_ascii": "Computer Modern", "h2_font_ea": "黑体", "h2_font_size": 22,
        "justification": "both", "first_indent": 0,
    },
    "nature": {
        "page_size": "A4", "margins": [1134, 1134, 1134, 1134], "orientation": "portrait",
        "body_font_ascii": "Arial", "body_font_ea": "微软雅黑", "body_font_cs": "Arial",
        "body_font_size": 16, "line_spacing": 240, "line_rule": "auto",
        "h1_font_ascii": "Arial", "h1_font_ea": "微软雅黑", "h1_font_size": 20,
        "h1_color": "000000", "h1_before": 120, "h1_after": 40,
        "h2_font_ascii": "Arial", "h2_font_ea": "微软雅黑", "h2_font_size": 18,
        "justification": "both", "first_indent": 0,
    },
    "hbr": {
        "page_size": "Letter", "margins": [1440, 1440, 1260, 1260], "orientation": "portrait",
        "body_font_ascii": "Georgia", "body_font_ea": "微软雅黑", "body_font_cs": "Georgia",
        "body_font_size": 22, "line_spacing": 300, "line_rule": "auto",
        "h1_font_ascii": "Georgia", "h1_font_ea": "微软雅黑", "h1_font_size": 36,
        "h1_color": "C41230", "h1_before": 480, "h1_after": 200,
        "h2_font_ascii": "Georgia", "h2_font_ea": "微软雅黑", "h2_font_size": 28,
        "justification": "left", "first_indent": 0,
    },
}


# ── XML Helpers ──────────────────────────────────────────────────────────────────

def _w(tag):
    """Prepend w: namespace to a local tag name."""
    return f"{{{W}}}{tag}"

def _r(tag):
    return f"{{{R}}}{tag}"

def make_elem(tag, attrib=None, text=None):
    """Create an Element with optional attributes and text."""
    e = ET.Element(tag)
    if attrib:
        for k, v in attrib.items():
            e.set(k, str(v))
    if text is not None:
        e.text = str(text)
    return e

def w_elem(local, attrib=None, text=None):
    return make_elem(_w(local), attrib, text)

def r_elem(local, attrib=None, text=None):
    return make_elem(_r(local), attrib, text)


# ── Core XML Builders ────────────────────────────────────────────────────────────

def build_content_types():
    """[Content_Types].xml"""
    root = make_elem(f"{{{CT}}}Types")
    defaults = make_elem(f"{{{CT}}}Default", {"Extension": "rels", "ContentType": "application/vnd.openxmlformats-package.relationships+xml"})
    root.append(defaults)
    defaults2 = make_elem(f"{{{CT}}}Default", {"Extension": "xml", "ContentType": "application/xml"})
    root.append(defaults2)
    overrides = [
        ("/word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"),
        ("/word/styles.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"),
        ("/word/settings.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"),
        ("/word/fontTable.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.fontTable+xml"),
        ("/word/numbering.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"),
        ("/docProps/app.xml", "application/vnd.openxmlformats-officedocument.extended-properties+xml"),
        ("/docProps/core.xml", "application/vnd.openxmlformats-package.core-properties+xml"),
    ]
    for part_name, content_type in overrides:
        root.append(make_elem(f"{{{CT}}}Override", {"PartName": part_name, "ContentType": content_type}))
    return root


def build_rels():
    """_rels/.rels"""
    root = make_elem(f"{{{REL}}}Relationships")
    rels = [
        ("rId1", "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument", "word/document.xml"),
        ("rId2", "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties", "docProps/core.xml"),
        ("rId3", "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties", "docProps/app.xml"),
    ]
    for rid, rel_type, target in rels:
        root.append(make_elem(f"{{{REL}}}Relationship", {"Id": rid, "Type": rel_type, "Target": target}))
    return root


def build_document_rels(header_ids=None, footer_ids=None, image_ids=None):
    """word/_rels/document.xml.rels"""
    root = make_elem(f"{{{REL}}}Relationships")
    rels = [
        ("rId1", "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles", "styles.xml"),
        ("rId2", "http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings", "settings.xml"),
        ("rId3", "http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable", "fontTable.xml"),
        ("rId4", "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering", "numbering.xml"),
    ]
    rid_counter = [5]

    def add_rels(items, rel_type_prefix):
        for rid, target in items:
            rels.append((rid, rel_type_prefix, target))

    if header_ids:
        for hid, target in header_ids:
            rels.append((hid, "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header", target))

    if footer_ids:
        for fid, target in footer_ids:
            rels.append((fid, "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer", target))

    if image_ids:
        for iid, target, ctype in image_ids:
            rels.append((iid, "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image", target))

    for rid, rel_type, target in rels:
        root.append(make_elem(f"{{{REL}}}Relationship", {"Id": rid, "Type": rel_type, "Target": target}))
    return root


def build_font_table():
    """word/fontTable.xml"""
    root = w_elem("fonts")
    fonts = [
        ("Calibri", "Calibri", "宋体", "Times New Roman"),
        ("Times New Roman", "Times New Roman", "宋体", "Times New Roman"),
        ("Arial", "Arial", "微软雅黑", "Arial"),
        ("FangSong", "FangSong", "仿宋", "FangSong"),
        ("SimSun", "SimSun", "宋体", "SimSun"),
        ("SimHei", "SimHei", "黑体", "SimHei"),
    ]
    for ascii_f, hAnsi_f, ea_f, cs_f in fonts:
        font = w_elem("font", {"{{{http://www.w3.org/2001/XMLSchema-instance}}type": "w:fontElt"})
        # Simpler: use attrib-free font element
        font = w_elem("font")
        font.set(_w("name"), ascii_f)
        font.append(w_elem("charset", {_w("val"): "00"}))  # ANSI
        font.append(w_elem("charset", {_w("val"): "86"}))  # East Asian
        # Embedding info omitted for simplicity
        root.append(font)
    return root


def build_settings():
    """word/settings.xml"""
    root = w_elem("settings")
    root.append(w_elem("defaultTabStop", {_w("val"): "720"}))
    root.append(w_elem("characterSpacingControl", {_w("val"): "compressPunctuation"}))
    # Compatibility settings
    compat = w_elem("compat")
    compat.append(w_elem("compatSetting", {_w("name"): "compatId", _w("val"): "0", _w("uri"): "http://schemas.microsoft.com/office/word"}))
    root.append(compat)
    return root


def build_numbering():
    """word/numbering.xml — basic bullet and decimal numbering definitions."""
    root = w_elem("numbering")
    # Bullet list abstract numbering
    absNumBullet = w_elem("abstractNum", {_w("abstractNumId"): "0"})
    absNumBullet.append(w_elem("multiLevelType", {_w("val"): "hybridMultilevel"}))
    # Level 0
    lvl0 = w_elem("lvl", {_w("ilvl"): "0", _w("tplc"): "04090001"})
    lvl0.append(w_elem("start", {_w("val"): "1"}))
    lvl0.append(w_elem("numFmt", {_w("val"): "bullet"}))
    lvl0.append(w_elem("lvlText", {_w("val"): ""}))  # bullet char
    lvl0.append(w_elem("lvlJc", {_w("val"): "left"}))
    pPr = w_elem("pPr")
    pPr.append(w_elem("ind", {_w("left"): "720", _w("hanging"): "360"}))
    lvl0.append(pPr)
    rPr = w_elem("rPr")
    rPr.append(w_elem("rFonts", {_w("ascii"): "Symbol", _w("hAnsi"): "Symbol", _w("hint"): "default"}))
    lvl0.append(rPr)
    absNumBullet.append(lvl0)
    root.append(absNumBullet)

    # Decimal numbering abstract numbering
    absNumDecimal = w_elem("abstractNum", {_w("abstractNumId"): "1"})
    absNumDecimal.append(w_elem("multiLevelType", {_w("val"): "hybridMultilevel"}))
    lvl0d = w_elem("lvl", {_w("ilvl"): "0", _w("tplc"): "0409000F"})
    lvl0d.append(w_elem("start", {_w("val"): "1"}))
    lvl0d.append(w_elem("numFmt", {_w("val"): "decimal"}))
    lvl0d.append(w_elem("lvlText", {_w("val"): "%1."}))
    lvl0d.append(w_elem("lvlJc", {_w("val"): "left"}))
    pPr2 = w_elem("pPr")
    pPr2.append(w_elem("ind", {_w("left"): "720", _w("hanging"): "360"}))
    lvl0d.append(pPr2)
    absNumDecimal.append(lvl0d)
    root.append(absNumDecimal)

    # Numbering instances
    root.append(w_elem("num", {_w("numId"): "1"}).append(w_elem("abstractNumId", {_w("val"): "0"})) or root[-1] if False else _add_num_inst(root, "1", "0"))
    _add_num_inst(root, "2", "1")
    return root

def _add_num_inst(root, numId, abstractNumId):
    num = w_elem("num", {_w("numId"): numId})
    num.append(w_elem("abstractNumId", {_w("val"): abstractNumId}))
    root.append(num)


def build_core_xml(title="", creator="", subject="", description="", keywords=""):
    """docProps/core.xml"""
    root = ET.Element(f"{{{CP}}}coreProperties")
    ET.register_namespace("cp", CP)
    ET.register_namespace("dc", "http://purl.org/dc/elements/1.1/")
    ET.register_namespace("dcterms", DC)
    ET.register_namespace("dcmitype", "http://purl.org/dc/dcmitype/")
    if creator:
        e = ET.SubElement(root, "{http://purl.org/dc/elements/1.1/}creator")
        e.text = creator
    if title:
        e = ET.SubElement(root, "{http://purl.org/dc/elements/1.1/}title")
        e.text = title
    if subject:
        e = ET.SubElement(root, f"{{{CP}}}subject")
        e.text = subject
    if description:
        e = ET.SubElement(root, "{http://purl.org/dc/elements/1.1/}description")
        e.text = description
    if keywords:
        e = ET.SubElement(root, f"{{{CP}}}keywords")
        e.text = keywords
    now = datetime.datetime.utcnow().strftime("%Y-%m-%dT%H:%M:%SZ")
    created = ET.SubElement(root, f"{{{DC}}}created")
    created.set("{http://www.w3.org/2001/XMLSchema-instance}type", "dcterms:W3CDTF")
    created.text = now
    modified = ET.SubElement(root, f"{{{DC}}}modified")
    modified.set("{http://www.w3.org/2001/XMLSchema-instance}type", "dcterms:W3CDTF")
    modified.text = now
    return root


def build_app_xml(application="docx_create.py", app_version="1.0"):
    """docProps/app.xml"""
    root = ET.Element(f"{{{EP}}}Properties")
    app = ET.SubElement(root, f"{{{EP}}}Application")
    app.text = application
    ver = ET.SubElement(root, f"{{{EP}}}AppVersion")
    ver.text = app_version
    return root


# ── Styles Builder ───────────────────────────────────────────────────────────────

def build_styles(recipe_name=None):
    """word/styles.xml — default styles, optionally modified by a recipe."""
    recipe = RECIPES.get(recipe_name) if recipe_name else None

    root = w_elem("styles")

    # DocDefaults
    docDefaults = w_elem("docDefaults")
    rPrDefault = w_elem("rPrDefault")
    rPr = w_elem("rPr")
    if recipe:
        rPr.append(w_elem("rFonts", {
            _w("ascii"): recipe["body_font_ascii"],
            _w("hAnsi"): recipe["body_font_ascii"],
            _w("eastAsia"): recipe["body_font_ea"],
            _w("cs"): recipe.get("body_font_cs", recipe["body_font_ascii"]),
        }))
        rPr.append(w_elem("sz", {_w("val"): str(recipe["body_font_size"])}))
        rPr.append(w_elem("szCs", {_w("val"): str(recipe["body_font_size"])}))
    else:
        rPr.append(w_elem("rFonts", {_w("ascii"): "Calibri", _w("hAnsi"): "Calibri", _w("eastAsia"): "宋体", _w("cs"): "Times New Roman"}))
        rPr.append(w_elem("sz", {_w("val"): "22"}))
        rPr.append(w_elem("szCs", {_w("val"): "22"}))
    rPrDefault.append(rPr)
    docDefaults.append(rPrDefault)

    pPrDefault = w_elem("pPrDefault")
    pPr2 = w_elem("pPr")
    if recipe:
        pPr2.append(w_elem("spacing", {_w("line"): str(recipe["line_spacing"]), _w("lineRule"): recipe["line_rule"]}))
    else:
        pPr2.append(w_elem("spacing", {_w("after"): "200", _w("line"): "276", _w("lineRule"): "auto"}))
    pPrDefault.append(pPr2)
    docDefaults.append(pPrDefault)
    root.append(docDefaults)

    # LatentStyles
    latentStyles = w_elem("latentStyles", {_w("count"): "276", _w("defLockedState"): "0", _w("defQFormat"): "1"})
    root.append(latentStyles)

    # Normal style
    normal = _make_style("Normal", "paragraph", name="Normal")
    normal_pPr = w_elem("pPr")
    if recipe:
        if recipe["justification"] == "both":
            normal_pPr.append(w_elem("jc", {_w("val"): "both"}))
        if recipe["first_indent"] > 0:
            normal_pPr.append(w_elem("ind", {_w("firstLine"): str(recipe["first_indent"])}))
        normal_pPr.append(w_elem("spacing", {_w("after"): "200", _w("line"): str(recipe["line_spacing"]), _w("lineRule"): recipe["line_rule"]}))
    else:
        normal_pPr.append(w_elem("spacing", {_w("after"): "200", _w("line"): "276", _w("lineRule"): "auto"}))
    normal.append(normal_pPr)
    normal_rPr = w_elem("rPr")
    if recipe:
        normal_rPr.append(w_elem("rFonts", {_w("ascii"): recipe["body_font_ascii"], _w("hAnsi"): recipe["body_font_ascii"], _w("eastAsia"): recipe["body_font_ea"]}))
        normal_rPr.append(w_elem("sz", {_w("val"): str(recipe["body_font_size"])}))
    else:
        normal_rPr.append(w_elem("rFonts", {_w("ascii"): "Calibri", _w("hAnsi"): "Calibri"}))
        normal_rPr.append(w_elem("sz", {_w("val"): "22"}))
    normal.append(normal_rPr)
    root.append(normal)

    # Heading 1-9
    heading_sizes = [32, 28, 26, 24, 22, 22, 22, 22, 22]
    heading_before = [480, 360, 280, 240, 240, 240, 240, 240, 240]
    heading_after = [120, 80, 80, 40, 40, 40, 40, 40, 40]

    for i in range(9):
        level = i + 1
        style_id = f"Heading{level}"
        style_name = f"heading {level}"

        h = _make_style(style_id, "paragraph", name=style_name, based_on="Normal", next="Normal")
        pPr = w_elem("pPr")
        pPr.append(w_elem("keepNext"))
        pPr.append(w_elem("keepLines"))

        if recipe and level == 1:
            pPr.append(w_elem("spacing", {_w("before"): str(recipe["h1_before"]), _w("after"): str(recipe["h1_after"])}))
        elif recipe and level == 2:
            pPr.append(w_elem("spacing", {_w("before"): "280", _w("after"): "80"}))
        else:
            pPr.append(w_elem("spacing", {_w("before"): str(heading_before[i]), _w("after"): str(heading_after[i])}))

        pPr.append(w_elem("outlineLvl", {_w("val"): str(i)}))
        h.append(pPr)

        rPr = w_elem("rPr")
        if recipe and level == 1:
            rPr.append(w_elem("rFonts", {_w("ascii"): recipe["h1_font_ascii"], _w("hAnsi"): recipe["h1_font_ascii"], _w("eastAsia"): recipe["h1_font_ea"]}))
            rPr.append(w_elem("sz", {_w("val"): str(recipe["h1_font_size"])}))
            rPr.append(w_elem("b"))
            rPr.append(w_elem("color", {_w("val"): recipe["h1_color"]}))
        elif recipe and level == 2:
            rPr.append(w_elem("rFonts", {_w("ascii"): recipe["h2_font_ascii"], _w("hAnsi"): recipe["h2_font_ascii"], _w("eastAsia"): recipe["h2_font_ea"]}))
            rPr.append(w_elem("sz", {_w("val"): str(recipe["h2_font_size"])}))
            rPr.append(w_elem("b"))
        else:
            rPr.append(w_elem("rFonts", {_w("ascii"): "Calibri", _w("hAnsi"): "Calibri"}))
            rPr.append(w_elem("sz", {_w("val"): str(heading_sizes[i])}))
            rPr.append(w_elem("b"))
            rPr.append(w_elem("color", {_w("val"): "1F3864"}))
        h.append(rPr)
        root.append(h)

    # Title style
    title_s = _make_style("Title", "paragraph", name="Title", based_on="Normal", next="Normal")
    tpPr = w_elem("pPr")
    tpPr.append(w_elem("spacing", {_w("before"): "0", _w("after"): "0"}))
    tpPr.append(w_elem("jc", {_w("val"): "center"}))
    tpPr.append(w_elem("outlineLvl", {_w("val"): "0"}))
    title_s.append(tpPr)
    trPr = w_elem("rPr")
    trPr.append(w_elem("sz", {_w("val"): "56"}))
    trPr.append(w_elem("b"))
    title_s.append(trPr)
    root.append(title_s)

    # TOCHeading
    tocH = _make_style("TOCHeading", "paragraph", name="TOC Heading", based_on="Normal", next="Normal")
    tocH_rPr = w_elem("rPr")
    tocH_rPr.append(w_elem("sz", {_w("val"): "28"}))
    tocH_rPr.append(w_elem("b"))
    tocH.append(tocH_rPr)
    root.append(tocH)

    # TOC 1-3
    for t in range(1, 4):
        toc = _make_style(f"TOC{t}", "paragraph", name=f"toc {t}", based_on="Normal", next="Normal")
        toc_pPr = w_elem("pPr")
        toc_pPr.append(w_elem("spacing", {_w("after"): "40"}))
        if t == 1:
            toc_pPr.append(w_elem("ind", {_w("left"): "0"}))
        else:
            toc_pPr.append(w_elem("ind", {_w("left"): str(t * 360)}))
        toc.append(toc_pPr)
        root.append(toc)

    # TableGrid style
    tg = _make_style("TableGrid", "table", name="Table Grid")
    tg.append(w_elem("tblPr"))
    tblBorders = w_elem("tblBorders")
    for bname in ["top", "bottom", "left", "right", "insideH", "insideV"]:
        tblBorders.append(w_elem(bname, {_w("val"): "single", _w("sz"): "4", _w("space"): "0", _w("color"): "auto"}))
    tg.append(tblBorders)
    root.append(tg)

    # Header and Footer styles
    for sname, sid in [("Header", "Header"), ("Footer", "Footer")]:
        hs = _make_style(sid, "paragraph", name=sname, based_on="Normal")
        hs_pPr = w_elem("pPr")
        hs_pPr.append(w_elem("spacing", {_w("after"): "0", _w("line"): "240", _w("lineRule"): "auto"}))
        hs.append(hs_pPr)
        root.append(hs)

    # ListParagraph style
    lp = _make_style("ListParagraph", "paragraph", name="List Paragraph", based_on="Normal", next="Normal")
    lp_pPr = w_elem("pPr")
    lp_pPr.append(w_elem("ind", {_w("left"): "720"}))
    lp.append(lp_pPr)
    root.append(lp)

    return root


def _make_style(style_id, style_type, name=None, based_on=None, next=None):
    s = w_elem("style", {_w("type"): style_type, _w("styleId"): style_id})
    if name:
        s.append(w_elem("name", {_w("val"): name}))
    if based_on:
        s.append(w_elem("basedOn", {_w("val"): based_on}))
    if next:
        s.append(w_elem("next", {_w("val"): next}))
    # qFormat for quality formatting
    s.append(w_elem("qFormat"))
    return s


# ── Document Body Builders ───────────────────────────────────────────────────────

def make_paragraph(text, style=None, bold=False, italic=False, font_size=None, justify=None):
    """Create a w:p element."""
    p = w_elem("p")
    pPr = None
    if style or justify:
        pPr = w_elem("pPr")
        if style:
            pPr.append(w_elem("pStyle", {_w("val"): style}))
        if justify:
            pPr.append(w_elem("jc", {_w("val"): justify}))
        p.append(pPr)

    # Parse inline formatting markers: **bold** and *italic*
    runs = _parse_inline_formatting(text, bold, italic, font_size)
    for run in runs:
        p.append(run)
    return p


def _parse_inline_formatting(text, default_bold=False, default_italic=False, font_size=None):
    """Parse **bold** and *italic* markers, return list of w:r elements."""
    # Split by **...** for bold, then *...* for italic
    pattern = re.compile(r'(\*\*(.+?)\*\*|\*(.+?)\*)', re.DOTALL)
    runs = []
    last_end = 0
    for m in pattern.finditer(text):
        # Plain text before this match
        if m.start() > last_end:
            plain = text[last_end:m.start()]
            if plain:
                runs.append(_make_run(plain, bold=default_bold, italic=default_italic, font_size=font_size))
        if m.group(2):  # **bold**
            runs.append(_make_run(m.group(2), bold=True, italic=default_italic, font_size=font_size))
        elif m.group(3):  # *italic*
            runs.append(_make_run(m.group(3), bold=default_bold, italic=True, font_size=font_size))
        last_end = m.end()
    # Remaining text
    if last_end < len(text):
        remaining = text[last_end:]
        if remaining:
            runs.append(_make_run(remaining, bold=default_bold, italic=default_italic, font_size=font_size))
    if not runs:
        runs.append(_make_run(text, bold=default_bold, italic=default_italic, font_size=font_size))
    return runs


def _make_run(text, bold=False, italic=False, font_size=None):
    r = w_elem("r")
    rPr = None
    if bold or italic or font_size:
        rPr = w_elem("rPr")
        if bold:
            rPr.append(w_elem("b"))
            rPr.append(w_elem("bCs"))
        if italic:
            rPr.append(w_elem("i"))
            rPr.append(w_elem("iCs"))
        if font_size:
            rPr.append(w_elem("sz", {_w("val"): str(font_size)}))
            rPr.append(w_elem("szCs", {_w("val"): str(font_size)}))
        r.append(rPr)
    t = w_elem("t")
    t.text = text
    # Preserve whitespace for leading/trailing spaces
    if text and (text[0] == ' ' or text[-1] == ' ' or '\n' in text or '\t' in text):
        t.set("{http://www.w3.org/XML/1998/namespace}space", "preserve")
    r.append(t)
    return r


def make_heading(text, level=1):
    return make_paragraph(text, style=f"Heading{level}")


def make_table(headers, rows, style="TableGrid"):
    """Create a w:tbl element."""
    num_cols = len(headers) if headers else (len(rows[0]) if rows else 0)
    tbl = w_elem("tbl")

    # tblPr
    tblPr = w_elem("tblPr")
    tblPr.append(w_elem("tblStyle", {_w("val"): style}))
    tblPr.append(w_elem("tblW", {_w("w"): "5000", _w("type"): "pct"}))

    # Three-line style overrides
    if style == "three-line":
        tblBorders = w_elem("tblBorders")
        tblBorders.append(w_elem("top", {_w("val"): "single", _w("sz"): "12", _w("space"): "0", _w("color"): "000000"}))
        tblBorders.append(w_elem("bottom", {_w("val"): "single", _w("sz"): "12", _w("space"): "0", _w("color"): "000000"}))
        tblBorders.append(w_elem("left", {_w("val"): "none", _w("sz"): "0", _w("space"): "0", _w("color"): "auto"}))
        tblBorders.append(w_elem("right", {_w("val"): "none", _w("sz"): "0", _w("space"): "0", _w("color"): "auto"}))
        tblBorders.append(w_elem("insideV", {_w("val"): "none", _w("sz"): "0", _w("space"): "0", _w("color"): "auto"}))
        tblBorders.append(w_elem("insideH", {_w("val"): "none", _w("sz"): "0", _w("space"): "0", _w("color"): "auto"}))
        tblPr.append(tblBorders)

    tbl.append(tblPr)

    # tblGrid
    tblGrid = w_elem("tblGrid")
    for _ in range(num_cols):
        tblGrid.append(w_elem("gridCol", {_w("w"): "2400"}))
    tbl.append(tblGrid)

    # Header row
    if headers:
        tr = w_elem("tr")
        trPr = w_elem("trPr")
        trPr.append(w_elem("tblHeader"))
        tr.append(trPr)
        for h in headers:
            tc = w_elem("tc")
            if style == "three-line":
                tcPr = w_elem("tcPr")
                tcBorders = w_elem("tcBorders")
                tcBorders.append(w_elem("bottom", {_w("val"): "single", _w("sz"): "6", _w("space"): "0", _w("color"): "000000"}))
                tcPr.append(tcBorders)
                tc.append(tcPr)
            p = w_elem("p")
            pPr = w_elem("pPr")
            pPr.append(w_elem("jc", {_w("val"): "center"}))
            p.append(pPr)
            r = w_elem("r")
            rPr = w_elem("rPr")
            rPr.append(w_elem("b"))
            r.append(rPr)
            t = w_elem("t")
            t.text = h
            r.append(t)
            p.append(r)
            tc.append(p)
            tr.append(tc)
        tbl.append(tr)

    # Data rows
    for row_data in rows:
        tr = w_elem("tr")
        for cell_text in row_data:
            tc = w_elem("tc")
            tc.append(make_paragraph(str(cell_text)))
            tr.append(tc)
        tbl.append(tr)

    return tbl


def make_page_break():
    """Create a paragraph with a page break."""
    p = w_elem("p")
    r = w_elem("r")
    r.append(w_elem("br", {_w("type"): "page"}))
    p.append(r)
    return p


def make_section_break(sect_type="nextPage", page_size=None, margins=None, orientation="portrait"):
    """Create section properties element.

    sect_type: nextPage, continuous, evenPage, oddPage
    """
    sectPr = w_elem("sectPr")
    if sect_type != "nextPage":
        sectPr.append(w_elem("type", {_w("val"): sect_type}))

    if page_size:
        w, h = PAGE_SIZES.get(page_size, (11906, 16838))
        if orientation == "landscape":
            w, h = h, w
        pgSz = w_elem("pgSz", {_w("w"): str(w), _w("h"): str(h)})
        if orientation == "landscape":
            pgSz.set(_w("orient"), "landscape")
        sectPr.append(pgSz)

    if margins:
        top, bottom, left, right = margins
        pgMar = w_elem("pgMar", {
            _w("top"): str(top), _w("bottom"): str(bottom),
            _w("left"): str(left), _w("right"): str(right),
            _w("header"): str(int(top / 2)), _w("footer"): str(int(bottom / 2)), _w("gutter"): "0"
        })
        sectPr.append(pgMar)

    return sectPr


def make_toc():
    """Create a TOC field."""
    p = w_elem("p")
    pPr = w_elem("pPr")
    pPr.append(w_elem("pStyle", {_w("val"): "TOCHeading"}))
    p.append(pPr)
    r = w_elem("r")
    t = w_elem("t")
    t.text = "Table of Contents"
    r.append(t)
    p.append(r)

    # TOC field
    p2 = w_elem("p")
    pPr2 = w_elem("pPr")
    pPr2.append(w_elem("pStyle", {_w("val"): "TOC1"}))
    p2.append(pPr2)
    r1 = w_elem("r")
    fldChar1 = w_elem("fldChar", {_w("fldCharType"): "begin"})
    r1.append(fldChar1)
    p2.append(r1)
    r2 = w_elem("r")
    instrText = w_elem("instrText")
    instrText.text = " TOC \\o \"1-3\" \\h \\z \\u "
    r2.append(instrText)
    p2.append(r2)
    r3 = w_elem("r")
    fldChar2 = w_elem("fldChar", {_w("fldCharType"): "separate"})
    r3.append(fldChar2)
    p2.append(r3)
    r4 = w_elem("r")
    fldChar3 = w_elem("fldChar", {_w("fldCharType"): "end"})
    r4.append(fldChar3)
    p2.append(r4)
    return [p, p2]


def make_bullet_list(items):
    """Create bullet list paragraphs."""
    paras = []
    for item in items:
        p = w_elem("p")
        pPr = w_elem("pPr")
        pPr.append(w_elem("pStyle", {_w("val"): "ListParagraph"}))
        pPr.append(w_elem("numPr"))
        numPr = pPr.find(_w("numPr"))
        numPr.append(w_elem("ilvl", {_w("val"): "0"}))
        numPr.append(w_elem("numId", {_w("val"): "1"}))
        p.append(pPr)
        r = w_elem("r")
        t = w_elem("t")
        t.text = item
        r.append(t)
        p.append(r)
        paras.append(p)
    return paras


def make_numbered_list(items, fmt="1."):
    """Create numbered list paragraphs."""
    paras = []
    for item in items:
        p = w_elem("p")
        pPr = w_elem("pPr")
        pPr.append(w_elem("pStyle", {_w("val"): "ListParagraph"}))
        pPr.append(w_elem("numPr"))
        numPr = pPr.find(_w("numPr"))
        numPr.append(w_elem("ilvl", {_w("val"): "0"}))
        numPr.append(w_elem("numId", {_w("val"): "2"}))
        p.append(pPr)
        r = w_elem("r")
        t = w_elem("t")
        t.text = item
        r.append(t)
        p.append(r)
        paras.append(p)
    return paras


# ── Section Properties Builder ───────────────────────────────────────────────────

def make_sect_pr(page_size="A4", margins=None, orientation="portrait"):
    """Build final sectPr element for body."""
    w, h = PAGE_SIZES.get(page_size, (11906, 16838))
    if orientation == "landscape":
        w, h = h, w
    sectPr = w_elem("sectPr")
    pgSz = w_elem("pgSz", {_w("w"): str(w), _w("h"): str(h)})
    if orientation == "landscape":
        pgSz.set(_w("orient"), "landscape")
    sectPr.append(pgSz)

    if margins is None:
        margins = [1440, 1440, 1440, 1440]
    top, bottom, left, right = margins[:4]
    sectPr.append(w_elem("pgMar", {
        _w("top"): str(top), _w("bottom"): str(bottom),
        _w("left"): str(left), _w("right"): str(right),
        _w("header"): str(int(top / 2)), _w("footer"): str(int(bottom / 2)), _w("gutter"): "0"
    }))
    sectPr.append(w_elem("cols", {_w("space"): "702"}))
    sectPr.append(w_elem("docGrid", {_w("type"): "linesAndChars", _w("linePitch"): "312"}))
    return sectPr


# ── Content Builder from JSON ────────────────────────────────────────────────────

def build_body_from_content(content_spec, recipe_name=None):
    """Build document body elements from a JSON content specification."""
    body_elements = []

    for item in content_spec:
        item_type = item.get("type", "paragraph")

        if item_type == "heading":
            level = item.get("level", 1)
            text = item.get("text", "")
            body_elements.append(make_heading(text, level))

        elif item_type == "paragraph":
            text = item.get("text", "")
            style = item.get("style")
            body_elements.append(make_paragraph(text, style=style))

        elif item_type == "table":
            headers = item.get("headers", [])
            rows = item.get("rows", [])
            tbl_style = item.get("style", "TableGrid")
            body_elements.append(make_table(headers, rows, style=tbl_style))

        elif item_type == "page_break":
            body_elements.append(make_page_break())

        elif item_type == "bullet_list":
            items = item.get("items", [])
            body_elements.extend(make_bullet_list(items))

        elif item_type == "numbered_list":
            items = item.get("items", [])
            fmt = item.get("format", "1.")
            body_elements.extend(make_numbered_list(items, fmt))

        elif item_type == "toc":
            body_elements.extend(make_toc())

        elif item_type == "section":
            # Section break + content within section
            sect_setup = item.get("page_setup", {})
            sect_type = sect_setup.get("type", "nextPage")
            ps = sect_setup.get("size")
            mg = sect_setup.get("margins")
            ori = sect_setup.get("orientation", "portrait")
            # Build inner content
            inner_content = item.get("content", [])
            inner_elements = build_body_from_content(inner_content, recipe_name)
            body_elements.extend(inner_elements)
            # Add section break on last paragraph
            if inner_elements:
                last = inner_elements[-1]
                if last.tag == _w("p"):
                    pPr = last.find(_w("pPr"))
                    if pPr is None:
                        pPr = w_elem("pPr")
                        last.insert(0, pPr)
                    pPr.append(make_section_break(sect_type, ps, mg, ori))

        elif item_type == "image":
            # Image placeholder — note: full image support is in docx_images.py
            # Here we add a placeholder paragraph
            path = item.get("path", "")
            width = item.get("width", 4000000)
            body_elements.append(make_paragraph(f"[Image: {path}]"))

    return body_elements


# ── Main Document Builder ────────────────────────────────────────────────────────

def create_docx(output_path, page_size="A4", margins=None, orientation="portrait",
                recipe=None, title="", creator="", content_json=None):
    """Create a DOCX file with full page setup, styles, and optional content."""
    if margins is None:
        margins = [1440, 1440, 1440, 1440]
    if recipe and recipe in RECIPES:
        r = RECIPES[recipe]
        page_size = r["page_size"]
        margins = r["margins"]
        orientation = r.get("orientation", "portrait")

    # Load content spec if provided
    content_spec = []
    if content_json:
        if os.path.isfile(content_json):
            with open(content_json, "r", encoding="utf-8") as f:
                content_data = json.load(f)
        else:
            content_data = json.loads(content_json)

        # Page setup from content
        if "page_setup" in content_data:
            ps = content_data["page_setup"]
            page_size = ps.get("size", page_size)
            if "margins" in ps:
                margins = ps["margins"]
            orientation = ps.get("orientation", orientation)

        # Recipe from content
        if "styles" in content_data and "recipe" in content_data["styles"]:
            recipe = content_data["styles"]["recipe"]

        content_spec = content_data.get("body", [])

    # Build document.xml
    doc_root = w_elem("document")
    body = w_elem("body")
    doc_root.append(body)

    # Add content
    if content_spec:
        elements = build_body_from_content(content_spec, recipe)
        for elem in elements:
            body.append(elem)
    elif title:
        body.append(make_paragraph(title, style="Title"))

    # Final sectPr — MUST be the last child of w:body
    body.append(make_sect_pr(page_size, margins, orientation))

    # Build all XML parts
    content_types = build_content_types()
    rels = build_rels()
    doc_rels = build_document_rels()
    styles = build_styles(recipe)
    settings = build_settings()
    font_table = build_font_table()
    numbering = build_numbering()
    core = build_core_xml(title=title, creator=creator or "docx_create.py")
    app = build_app_xml()

    # Write DOCX (ZIP)
    with zipfile.ZipFile(output_path, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr("[Content_Types].xml", _xml_to_bytes(content_types))
        zf.writestr("_rels/.rels", _xml_to_bytes(rels))
        zf.writestr("word/_rels/document.xml.rels", _xml_to_bytes(doc_rels))
        zf.writestr("word/document.xml", _xml_to_bytes(doc_root))
        zf.writestr("word/styles.xml", _xml_to_bytes(styles))
        zf.writestr("word/settings.xml", _xml_to_bytes(settings))
        zf.writestr("word/fontTable.xml", _xml_to_bytes(font_table))
        zf.writestr("word/numbering.xml", _xml_to_bytes(numbering))
        zf.writestr("docProps/core.xml", _xml_to_bytes(core))
        zf.writestr("docProps/app.xml", _xml_to_bytes(app))

    return output_path


def _xml_to_bytes(root):
    """Serialize an ElementTree element to XML bytes with declaration."""
    ET.indent(root, space="  ")
    tree = ET.ElementTree(root)
    buf = io.BytesIO()
    tree.write(buf, encoding="UTF-8", xml_declaration=True)
    return buf.getvalue()


# ── CLI Entry Point ──────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        description="Create new DOCX files with full page setup, styles, and content.",
        epilog="Uses ONLY Python standard library. No third-party dependencies.",
    )
    parser.add_argument("--output", required=True, help="Output DOCX file path")
    parser.add_argument("--page-size", choices=list(PAGE_SIZES.keys()), default="A4",
                        help="Page size (default: A4)")
    parser.add_argument("--margins", default=None,
                        help="Page margins in DXA as 'top,bottom,left,right' (e.g. '1440,1440,1440,1440')")
    parser.add_argument("--orientation", choices=["portrait", "landscape"], default="portrait",
                        help="Page orientation")
    parser.add_argument("--recipe", choices=list(RECIPES.keys()), default=None,
                        help="Apply an aesthetic recipe")
    parser.add_argument("--title", default="", help="Document title")
    parser.add_argument("--creator", default="", help="Document creator/author")
    parser.add_argument("--content", default=None,
                        help="JSON content spec file path or inline JSON string")

    args = parser.parse_args()

    margins = None
    if args.margins:
        try:
            margins = [int(x.strip()) for x in args.margins.split(",")]
            if len(margins) != 4:
                print("Error: margins must be 4 comma-separated values (top,bottom,left,right)", file=sys.stderr)
                sys.exit(1)
        except ValueError:
            print("Error: margins must be integers", file=sys.stderr)
            sys.exit(1)

    try:
        result = create_docx(
            output_path=args.output,
            page_size=args.page_size,
            margins=margins,
            orientation=args.orientation,
            recipe=args.recipe,
            title=args.title,
            creator=args.creator,
            content_json=args.content,
        )
        print(f"Created: {result}")
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
