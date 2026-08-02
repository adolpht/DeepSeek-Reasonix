#!/usr/bin/env python3
"""docx_styles.py — Read, add, modify, and apply styles in a DOCX file.

Uses ONLY Python standard library. No third-party dependencies.

Usage:
    python3 docx_styles.py input.docx --list
    python3 docx_styles.py input.docx --recipe academic-thesis --output styled.docx
    python3 docx_styles.py input.docx --add-style '{"name":"MyStyle","type":"paragraph","basedOn":"Normal","fontSize":14,"bold":true}'
    python3 docx_styles.py input.docx --show "Heading 1"
"""

import argparse
import json
import os
import sys
import re
import copy
import zipfile
import io
import shutil
import xml.etree.ElementTree as ET

# ── Namespaces ───────────────────────────────────────────────────────────────────

NS = {
    "w":   "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r":   "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "ct":  "http://schemas.openxmlformats.org/package/2006/content-types",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
    "cp":  "http://schemas.openxmlformats.org/package/2006/metadata/core-properties",
    "ep":  "http://schemas.openxmlformats.org/officeDocument/2006/extended-properties",
}
for prefix, uri in NS.items():
    ET.register_namespace(prefix, uri)

W = NS["w"]
R = NS["r"]
CT = NS["ct"]
REL = NS["rel"]

def _w(tag):
    return f"{{{W}}}{tag}"


# ── Aesthetic Recipes (same as docx_create.py) ───────────────────────────────────

RECIPES = {
    "modern-corporate": {
        "body_font_ascii": "Calibri", "body_font_ea": "微软雅黑", "body_font_cs": "Times New Roman",
        "body_font_size": 22, "line_spacing": 276, "line_rule": "auto",
        "h1_font_ascii": "Calibri", "h1_font_ea": "微软雅黑", "h1_font_size": 32,
        "h1_color": "1F3864", "h1_before": 360, "h1_after": 120,
        "h2_font_ascii": "Calibri", "h2_font_ea": "微软雅黑", "h2_font_size": 28,
        "justification": "left", "first_indent": 0,
    },
    "academic-thesis": {
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 24, "line_spacing": 480, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 32,
        "h1_color": "000000", "h1_before": 480, "h1_after": 240,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 28,
        "justification": "both", "first_indent": 720,
    },
    "executive-brief": {
        "body_font_ascii": "Arial", "body_font_ea": "微软雅黑", "body_font_cs": "Arial",
        "body_font_size": 20, "line_spacing": 240, "line_rule": "auto",
        "h1_font_ascii": "Arial", "h1_font_ea": "微软雅黑", "h1_font_size": 28,
        "h1_color": "333333", "h1_before": 240, "h1_after": 80,
        "h2_font_ascii": "Arial", "h2_font_ea": "微软雅黑", "h2_font_size": 24,
        "justification": "left", "first_indent": 0,
    },
    "chinese-government": {
        "body_font_ascii": "FangSong", "body_font_ea": "仿宋", "body_font_cs": "FangSong",
        "body_font_size": 44, "line_spacing": 570, "line_rule": "exact",
        "h1_font_ascii": "SimSun", "h1_font_ea": "小标宋", "h1_font_size": 44,
        "h1_color": "000000", "h1_before": 0, "h1_after": 0,
        "h2_font_ascii": "SimHei", "h2_font_ea": "黑体", "h2_font_size": 36,
        "justification": "both", "first_indent": 888,
    },
    "minimal-modern": {
        "body_font_ascii": "Helvetica Neue", "body_font_ea": "苹方-简", "body_font_cs": "Helvetica Neue",
        "body_font_size": 22, "line_spacing": 360, "line_rule": "auto",
        "h1_font_ascii": "Helvetica Neue", "h1_font_ea": "苹方-简", "h1_font_size": 36,
        "h1_color": "333333", "h1_before": 600, "h1_after": 200,
        "h2_font_ascii": "Helvetica Neue", "h2_font_ea": "苹方-简", "h2_font_size": 28,
        "justification": "left", "first_indent": 0,
    },
    "ieee": {
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 20, "line_spacing": 240, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 24,
        "h1_color": "000000", "h1_before": 180, "h1_after": 60,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 22,
        "justification": "both", "first_indent": 0,
    },
    "apa-7th": {
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 24, "line_spacing": 480, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 24,
        "h1_color": "000000", "h1_before": 0, "h1_after": 0,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 24,
        "justification": "left", "first_indent": 720,
    },
    "mla-9th": {
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 24, "line_spacing": 480, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 24,
        "h1_color": "000000", "h1_before": 0, "h1_after": 0,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 24,
        "justification": "left", "first_indent": 720,
    },
    "chicago": {
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体", "body_font_cs": "Times New Roman",
        "body_font_size": 24, "line_spacing": 480, "line_rule": "auto",
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体", "h1_font_size": 28,
        "h1_color": "000000", "h1_before": 360, "h1_after": 240,
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体", "h2_font_size": 24,
        "justification": "left", "first_indent": 720,
    },
    "springer-lncs": {
        "body_font_ascii": "Computer Modern", "body_font_ea": "宋体", "body_font_cs": "Computer Modern",
        "body_font_size": 20, "line_spacing": 240, "line_rule": "auto",
        "h1_font_ascii": "Computer Modern", "h1_font_ea": "黑体", "h1_font_size": 24,
        "h1_color": "000000", "h1_before": 180, "h1_after": 60,
        "h2_font_ascii": "Computer Modern", "h2_font_ea": "黑体", "h2_font_size": 22,
        "justification": "both", "first_indent": 0,
    },
    "nature": {
        "body_font_ascii": "Arial", "body_font_ea": "微软雅黑", "body_font_cs": "Arial",
        "body_font_size": 16, "line_spacing": 240, "line_rule": "auto",
        "h1_font_ascii": "Arial", "h1_font_ea": "微软雅黑", "h1_font_size": 20,
        "h1_color": "000000", "h1_before": 120, "h1_after": 40,
        "h2_font_ascii": "Arial", "h2_font_ea": "微软雅黑", "h2_font_size": 18,
        "justification": "both", "first_indent": 0,
    },
    "hbr": {
        "body_font_ascii": "Georgia", "body_font_ea": "微软雅黑", "body_font_cs": "Georgia",
        "body_font_size": 22, "line_spacing": 300, "line_rule": "auto",
        "h1_font_ascii": "Georgia", "h1_font_ea": "微软雅黑", "h1_font_size": 36,
        "h1_color": "C41230", "h1_before": 480, "h1_after": 200,
        "h2_font_ascii": "Georgia", "h2_font_ea": "微软雅黑", "h2_font_size": 28,
        "justification": "left", "first_indent": 0,
    },
}


# ── DOCX I/O Helpers ─────────────────────────────────────────────────────────────

def read_docx(docx_path):
    """Read a DOCX file, return dict of {internal_path: bytes}."""
    files = {}
    with zipfile.ZipFile(docx_path, "r") as zf:
        for name in zf.namelist():
            files[name] = zf.read(name)
    return files


def write_docx(files, output_path):
    """Write a dict of {internal_path: bytes} to a DOCX file."""
    with zipfile.ZipFile(output_path, "w", zipfile.ZIP_DEFLATED) as zf:
        for name, data in sorted(files.items()):
            zf.writestr(name, data)


def parse_xml(xml_bytes):
    return ET.fromstring(xml_bytes)


def serialize_xml(root):
    ET.indent(root, space="  ")
    tree = ET.ElementTree(root)
    buf = io.BytesIO()
    tree.write(buf, encoding="UTF-8", xml_declaration=True)
    return buf.getvalue()


# ── Style Operations ─────────────────────────────────────────────────────────────

def get_styles_root(files):
    """Get or create styles.xml root."""
    if "word/styles.xml" in files:
        return parse_xml(files["word/styles.xml"])
    # Create minimal styles.xml
    root = ET.Element(_w("styles"))
    return root


def list_styles(styles_root):
    """List all styles in the document."""
    result = []
    for style in styles_root.findall(_w("style")):
        style_id = style.get(_w("styleId"), "")
        style_type = style.get(_w("type"), "")
        name_elem = style.find(_w("name"))
        name = name_elem.get(_w("val"), "") if name_elem is not None else ""
        based_on_elem = style.find(_w("basedOn"))
        based_on = based_on_elem.get(_w("val"), "") if based_on_elem is not None else ""
        result.append({
            "id": style_id,
            "name": name,
            "type": style_type,
            "basedOn": based_on,
        })
    return result


def show_style(styles_root, style_name):
    """Show detailed info about a specific style."""
    for style in styles_root.findall(_w("style")):
        name_elem = style.find(_w("name"))
        if name_elem is not None and name_elem.get(_w("val"), "") == style_name:
            return _style_to_dict(style)
        if style.get(_w("styleId"), "") == style_name:
            return _style_to_dict(style)
    return None


def _style_to_dict(style):
    """Convert a style element to a detailed dict."""
    info = {}
    info["id"] = style.get(_w("styleId"), "")
    info["type"] = style.get(_w("type"), "")
    name_elem = style.find(_w("name"))
    if name_elem is not None:
        info["name"] = name_elem.get(_w("val"), "")
    based_on = style.find(_w("basedOn"))
    if based_on is not None:
        info["basedOn"] = based_on.get(_w("val"), "")
    next_style = style.find(_w("next"))
    if next_style is not None:
        info["next"] = next_style.get(_w("val"), "")

    # Paragraph properties
    pPr = style.find(_w("pPr"))
    if pPr is not None:
        info["paragraphProperties"] = _pPr_to_dict(pPr)

    # Run properties
    rPr = style.find(_w("rPr"))
    if rPr is not None:
        info["runProperties"] = _rPr_to_dict(rPr)

    return info


def _pPr_to_dict(pPr):
    d = {}
    spacing = pPr.find(_w("spacing"))
    if spacing is not None:
        d["spacing"] = {k.split("}")[1] if "}" in k else k: v for k, v in spacing.attrib.items()}
    jc = pPr.find(_w("jc"))
    if jc is not None:
        d["justification"] = jc.get(_w("val"), "")
    ind = pPr.find(_w("ind"))
    if ind is not None:
        d["indentation"] = {k.split("}")[1] if "}" in k else k: v for k, v in ind.attrib.items()}
    outlineLvl = pPr.find(_w("outlineLvl"))
    if outlineLvl is not None:
        d["outlineLevel"] = outlineLvl.get(_w("val"), "")
    return d


def _rPr_to_dict(rPr):
    d = {}
    rFonts = rPr.find(_w("rFonts"))
    if rFonts is not None:
        d["fonts"] = {k.split("}")[1] if "}" in k else k: v for k, v in rFonts.attrib.items()}
    sz = rPr.find(_w("sz"))
    if sz is not None:
        val = int(sz.get(_w("val"), "0"))
        d["fontSize"] = f"{val / 2}pt ({val} half-pts)"
    b = rPr.find(_w("b"))
    if b is not None:
        d["bold"] = True
    i = rPr.find(_w("i"))
    if i is not None:
        d["italic"] = True
    color = rPr.find(_w("color"))
    if color is not None:
        d["color"] = color.get(_w("val"), "")
    return d


def add_style(styles_root, style_spec):
    """Add a custom style from a JSON spec dict."""
    name = style_spec.get("name", "CustomStyle")
    style_type = style_spec.get("type", "paragraph")
    based_on = style_spec.get("basedOn", "Normal" if style_type == "paragraph" else "")
    font_size = style_spec.get("fontSize")
    bold = style_spec.get("bold", False)
    italic = style_spec.get("italic", False)
    color = style_spec.get("color")
    font_ascii = style_spec.get("fontAscii")
    font_ea = style_spec.get("fontEastAsia")

    # Generate styleId from name (remove spaces)
    style_id = re.sub(r'\s+', '', name)

    # Check if style already exists
    for existing in styles_root.findall(_w("style")):
        if existing.get(_w("styleId"), "") == style_id:
            # Remove existing to replace it
            styles_root.remove(existing)
            break

    style = ET.SubElement(styles_root, _w("style"))
    style.set(_w("type"), style_type)
    style.set(_w("styleId"), style_id)

    # name
    name_elem = ET.SubElement(style, _w("name"))
    name_elem.set(_w("val"), name)

    # basedOn
    if based_on:
        based_elem = ET.SubElement(style, _w("basedOn"))
        based_elem.set(_w("val"), based_on)

    # qFormat
    ET.SubElement(style, _w("qFormat"))

    # Paragraph properties for heading styles
    if style_type == "paragraph":
        pPr = ET.SubElement(style, _w("pPr"))
        # If name contains "heading" or "Heading", add outlineLevel
        name_lower = name.lower()
        if "heading" in name_lower:
            m = re.search(r'(\d+)', name)
            level = int(m.group(1)) - 1 if m else 0
            ET.SubElement(pPr, _w("keepNext"))
            ET.SubElement(pPr, _w("keepLines"))
            ol = ET.SubElement(pPr, _w("outlineLvl"))
            ol.set(_w("val"), str(level))
            sp = ET.SubElement(pPr, _w("spacing"))
            sp.set(_w("before"), "360")
            sp.set(_w("after"), "80")

    # Run properties
    if font_size or bold or italic or color or font_ascii or font_ea:
        rPr = ET.SubElement(style, _w("rPr"))
        if font_ascii or font_ea:
            rf = ET.SubElement(rPr, _w("rFonts"))
            if font_ascii:
                rf.set(_w("ascii"), font_ascii)
                rf.set(_w("hAnsi"), font_ascii)
            if font_ea:
                rf.set(_w("eastAsia"), font_ea)
        if font_size:
            sz = ET.SubElement(rPr, _w("sz"))
            sz.set(_w("val"), str(int(font_size * 2)))
            szCs = ET.SubElement(rPr, _w("szCs"))
            szCs.set(_w("val"), str(int(font_size * 2)))
        if bold:
            ET.SubElement(rPr, _w("b"))
            ET.SubElement(rPr, _w("bCs"))
        if italic:
            ET.SubElement(rPr, _w("i"))
            ET.SubElement(rPr, _w("iCs"))
        if color:
            c = ET.SubElement(rPr, _w("color"))
            c.set(_w("val"), color)

    return style_id


def apply_recipe(styles_root, recipe_name):
    """Apply an aesthetic recipe — modify DocDefaults and key styles."""
    recipe = RECIPES.get(recipe_name)
    if not recipe:
        raise ValueError(f"Unknown recipe: {recipe_name}")

    # Update or create DocDefaults
    doc_defaults = styles_root.find(_w("docDefaults"))
    if doc_defaults is None:
        doc_defaults = ET.Element(_w("docDefaults"))
        # Insert at beginning
        styles_root.insert(0, doc_defaults)

    # Run properties default
    rPrDefault = doc_defaults.find(_w("rPrDefault"))
    if rPrDefault is None:
        rPrDefault = ET.SubElement(doc_defaults, _w("rPrDefault"))
    rPr = rPrDefault.find(_w("rPr"))
    if rPr is None:
        rPr = ET.SubElement(rPrDefault, _w("rPr"))
    else:
        # Clear existing
        for child in list(rPr):
            rPr.remove(child)

    rFonts = ET.SubElement(rPr, _w("rFonts"))
    rFonts.set(_w("ascii"), recipe["body_font_ascii"])
    rFonts.set(_w("hAnsi"), recipe["body_font_ascii"])
    rFonts.set(_w("eastAsia"), recipe["body_font_ea"])
    rFonts.set(_w("cs"), recipe.get("body_font_cs", recipe["body_font_ascii"]))
    sz = ET.SubElement(rPr, _w("sz"))
    sz.set(_w("val"), str(recipe["body_font_size"]))
    szCs = ET.SubElement(rPr, _w("szCs"))
    szCs.set(_w("val"), str(recipe["body_font_size"]))

    # Paragraph properties default
    pPrDefault = doc_defaults.find(_w("pPrDefault"))
    if pPrDefault is None:
        pPrDefault = ET.SubElement(doc_defaults, _w("pPrDefault"))
    pPr2 = pPrDefault.find(_w("pPr"))
    if pPr2 is None:
        pPr2 = ET.SubElement(pPrDefault, _w("pPr"))
    else:
        for child in list(pPr2):
            pPr2.remove(child)
    spacing = ET.SubElement(pPr2, _w("spacing"))
    spacing.set(_w("line"), str(recipe["line_spacing"]))
    spacing.set(_w("lineRule"), recipe["line_rule"])

    # Update or create Normal style
    _update_or_create_normal(styles_root, recipe)

    # Update Heading 1
    _update_or_create_heading(styles_root, 1, recipe)

    # Update Heading 2
    _update_or_create_heading(styles_root, 2, recipe)

    # Add recipe-specific styles for Chinese government
    if recipe_name == "chinese-government":
        _add_chinese_govt_styles(styles_root, recipe)


def _update_or_create_normal(styles_root, recipe):
    """Update or create the Normal style based on recipe."""
    normal = None
    for style in styles_root.findall(_w("style")):
        if style.get(_w("styleId"), "") == "Normal":
            normal = style
            break

    if normal is None:
        normal = ET.SubElement(styles_root, _w("style"))
        normal.set(_w("type"), "paragraph")
        normal.set(_w("styleId"), "Normal")
        name_elem = ET.SubElement(normal, _w("name"))
        name_elem.set(_w("val"), "Normal")

    # Update pPr
    pPr = normal.find(_w("pPr"))
    if pPr is None:
        pPr = ET.SubElement(normal, _w("pPr"))
    else:
        for child in list(pPr):
            pPr.remove(child)

    if recipe["justification"] == "both":
        jc = ET.SubElement(pPr, _w("jc"))
        jc.set(_w("val"), "both")
    if recipe["first_indent"] > 0:
        ind = ET.SubElement(pPr, _w("ind"))
        ind.set(_w("firstLine"), str(recipe["first_indent"]))
    sp = ET.SubElement(pPr, _w("spacing"))
    sp.set(_w("after"), "200")
    sp.set(_w("line"), str(recipe["line_spacing"]))
    sp.set(_w("lineRule"), recipe["line_rule"])

    # Update rPr
    rPr = normal.find(_w("rPr"))
    if rPr is None:
        rPr = ET.SubElement(normal, _w("rPr"))
    else:
        for child in list(rPr):
            rPr.remove(child)
    rf = ET.SubElement(rPr, _w("rFonts"))
    rf.set(_w("ascii"), recipe["body_font_ascii"])
    rf.set(_w("hAnsi"), recipe["body_font_ascii"])
    rf.set(_w("eastAsia"), recipe["body_font_ea"])
    sz = ET.SubElement(rPr, _w("sz"))
    sz.set(_w("val"), str(recipe["body_font_size"]))


def _update_or_create_heading(styles_root, level, recipe):
    """Update or create a heading style based on recipe."""
    style_id = f"Heading{level}"
    heading = None
    for style in styles_root.findall(_w("style")):
        if style.get(_w("styleId"), "") == style_id:
            heading = style
            break

    if heading is None:
        heading = ET.SubElement(styles_root, _w("style"))
        heading.set(_w("type"), "paragraph")
        heading.set(_w("styleId"), style_id)
        name_elem = ET.SubElement(heading, _w("name"))
        name_elem.set(_w("val"), f"heading {level}")
        based = ET.SubElement(heading, _w("basedOn"))
        based.set(_w("val"), "Normal")
        nxt = ET.SubElement(heading, _w("next"))
        nxt.set(_w("val"), "Normal")

    # Update pPr
    pPr = heading.find(_w("pPr"))
    if pPr is None:
        pPr = ET.SubElement(heading, _w("pPr"))
    else:
        for child in list(pPr):
            pPr.remove(child)
    ET.SubElement(pPr, _w("keepNext"))
    ET.SubElement(pPr, _w("keepLines"))
    if level == 1:
        sp = ET.SubElement(pPr, _w("spacing"))
        sp.set(_w("before"), str(recipe["h1_before"]))
        sp.set(_w("after"), str(recipe["h1_after"]))
    ol = ET.SubElement(pPr, _w("outlineLvl"))
    ol.set(_w("val"), str(level - 1))

    # Update rPr
    rPr = heading.find(_w("rPr"))
    if rPr is None:
        rPr = ET.SubElement(heading, _w("rPr"))
    else:
        for child in list(rPr):
            rPr.remove(child)
    if level == 1:
        rf = ET.SubElement(rPr, _w("rFonts"))
        rf.set(_w("ascii"), recipe["h1_font_ascii"])
        rf.set(_w("hAnsi"), recipe["h1_font_ascii"])
        rf.set(_w("eastAsia"), recipe["h1_font_ea"])
        sz = ET.SubElement(rPr, _w("sz"))
        sz.set(_w("val"), str(recipe["h1_font_size"]))
        ET.SubElement(rPr, _w("b"))
        c = ET.SubElement(rPr, _w("color"))
        c.set(_w("val"), recipe["h1_color"])
    elif level == 2:
        rf = ET.SubElement(rPr, _w("rFonts"))
        rf.set(_w("ascii"), recipe["h2_font_ascii"])
        rf.set(_w("hAnsi"), recipe["h2_font_ascii"])
        rf.set(_w("eastAsia"), recipe["h2_font_ea"])
        sz = ET.SubElement(rPr, _w("sz"))
        sz.set(_w("val"), str(recipe["h2_font_size"]))
        ET.SubElement(rPr, _w("b"))


def _add_chinese_govt_styles(styles_root, recipe):
    """Add Chinese government (GB/T 9704) specific styles."""
    # 公文正文 style
    gov_body = None
    for s in styles_root.findall(_w("style")):
        if s.get(_w("styleId"), "") == "GovBodyText":
            gov_body = s
            break
    if gov_body is None:
        gov_body = ET.SubElement(styles_root, _w("style"))
        gov_body.set(_w("type"), "paragraph")
        gov_body.set(_w("styleId"), "GovBodyText")
        nm = ET.SubElement(gov_body, _w("name"))
        nm.set(_w("val"), "公文正文")

    pPr = gov_body.find(_w("pPr"))
    if pPr is None:
        pPr = ET.SubElement(gov_body, _w("pPr"))
    else:
        for child in list(pPr):
            pPr.remove(child)
    sp = ET.SubElement(pPr, _w("spacing"))
    sp.set(_w("line"), "560")
    sp.set(_w("lineRule"), "exact")
    jc = ET.SubElement(pPr, _w("jc"))
    jc.set(_w("val"), "both")

    rPr = gov_body.find(_w("rPr"))
    if rPr is None:
        rPr = ET.SubElement(gov_body, _w("rPr"))
    else:
        for child in list(rPr):
            rPr.remove(child)
    rf = ET.SubElement(rPr, _w("rFonts"))
    rf.set(_w("ascii"), "FangSong")
    rf.set(_w("hAnsi"), "FangSong")
    rf.set(_w("eastAsia"), "仿宋")
    rf.set(_w("cs"), "FangSong")
    sz = ET.SubElement(rPr, _w("sz"))
    sz.set(_w("val"), "44")
    szCs = ET.SubElement(rPr, _w("szCs"))
    szCs.set(_w("val"), "44")

    # 公文标题 style
    gov_title = None
    for s in styles_root.findall(_w("style")):
        if s.get(_w("styleId"), "") == "GovTitle":
            gov_title = s
            break
    if gov_title is None:
        gov_title = ET.SubElement(styles_root, _w("style"))
        gov_title.set(_w("type"), "paragraph")
        gov_title.set(_w("styleId"), "GovTitle")
        nm = ET.SubElement(gov_title, _w("name"))
        nm.set(_w("val"), "公文标题")

    pPr2 = gov_title.find(_w("pPr"))
    if pPr2 is None:
        pPr2 = ET.SubElement(gov_title, _w("pPr"))
    else:
        for child in list(pPr2):
            pPr2.remove(child)
    sp2 = ET.SubElement(pPr2, _w("spacing"))
    sp2.set(_w("before"), "0")
    sp2.set(_w("after"), "0")
    sp2.set(_w("line"), "560")
    sp2.set(_w("lineRule"), "exact")
    jc2 = ET.SubElement(pPr2, _w("jc"))
    jc2.set(_w("val"), "center")
    ol = ET.SubElement(pPr2, _w("outlineLvl"))
    ol.set(_w("val"), "0")

    rPr2 = gov_title.find(_w("rPr"))
    if rPr2 is None:
        rPr2 = ET.SubElement(gov_title, _w("rPr"))
    else:
        for child in list(rPr2):
            rPr2.remove(child)
    rf2 = ET.SubElement(rPr2, _w("rFonts"))
    rf2.set(_w("ascii"), "SimSun")
    rf2.set(_w("hAnsi"), "SimSun")
    rf2.set(_w("eastAsia"), "小标宋")
    rf2.set(_w("cs"), "SimSun")
    sz2 = ET.SubElement(rPr2, _w("sz"))
    sz2.set(_w("val"), "44")
    ET.SubElement(rPr2, _w("b"))


# ── Main ─────────────────────────────────────────────────────────────────────────

def process_styles(input_path, output_path=None, list_styles_flag=False,
                   recipe=None, add_style_spec=None, show_style_name=None):
    """Process styles in a DOCX file."""
    files = read_docx(input_path)
    styles_root = get_styles_root(files)

    if list_styles_flag:
        styles = list_styles(styles_root)
        for s in styles:
            print(f"  {s['type']:10s}  {s['id']:20s}  {s['name']:30s}  basedOn: {s.get('basedOn', '-')}")
        return

    if show_style_name:
        info = show_style(styles_root, show_style_name)
        if info:
            print(json.dumps(info, indent=2, ensure_ascii=False))
        else:
            print(f"Style '{show_style_name}' not found.", file=sys.stderr)
            sys.exit(1)
        return

    modified = False

    if recipe:
        apply_recipe(styles_root, recipe)
        modified = True
        print(f"Applied recipe: {recipe}")

    if add_style_spec:
        if os.path.isfile(add_style_spec):
            with open(add_style_spec, "r", encoding="utf-8") as f:
                spec = json.load(f)
        else:
            spec = json.loads(add_style_spec)
        style_id = add_style(styles_root, spec)
        modified = True
        print(f"Added style: {style_id}")

    if modified:
        files["word/styles.xml"] = serialize_xml(styles_root)
        out = output_path or input_path
        write_docx(files, out)
        print(f"Saved: {out}")


def main():
    parser = argparse.ArgumentParser(
        description="Read, add, modify, and apply styles in a DOCX file.",
        epilog="Uses ONLY Python standard library. No third-party dependencies.",
    )
    parser.add_argument("input", help="Input DOCX file path")
    parser.add_argument("--output", default=None, help="Output DOCX file path (default: modify in-place)")
    parser.add_argument("--list", action="store_true", dest="list_styles", help="List all styles")
    parser.add_argument("--recipe", choices=list(RECIPES.keys()), default=None,
                        help="Apply an aesthetic recipe")
    parser.add_argument("--add-style", default=None,
                        help="Add a custom style from JSON spec (file path or inline JSON)")
    parser.add_argument("--show", default=None, help="Show details of a named style")

    args = parser.parse_args()

    if not any([args.list_styles, args.recipe, args.add_style, args.show]):
        parser.print_help()
        sys.exit(1)

    try:
        process_styles(
            input_path=args.input,
            output_path=args.output,
            list_styles_flag=args.list_styles,
            recipe=args.recipe,
            add_style_spec=args.add_style,
            show_style_name=args.show,
        )
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
