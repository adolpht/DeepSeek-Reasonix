#!/usr/bin/env python3
"""docx_aesthetic.py — Apply predefined aesthetic recipes to DOCX files.

Uses ONLY Python standard library. No third-party dependencies.

Each recipe sets: page margins, font families, font sizes, line spacing,
heading styles, color palette, and (optionally) header/footer style.

Usage:
    python3 docx_aesthetic.py input.docx --recipe academic-thesis --output styled.docx
    python3 docx_aesthetic.py --show-recipe academic-thesis
    python3 docx_aesthetic.py --list-recipes
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


# ── Aesthetic Recipes ────────────────────────────────────────────────────────────
# Values match AestheticRecipeSamples.cs exactly.

RECIPES = {
    "modern-corporate": {
        "name": "Modern Corporate",
        "source": "Corporate design best practice",
        "page_size": "A4",
        "page_width_twips": 11906, "page_height_twips": 16838,
        "margin_top": 1440, "margin_bottom": 1440,
        "margin_left": 1440, "margin_right": 1440,
        "body_font_ascii": "Calibri", "body_font_ea": "微软雅黑",
        "body_font_cs": "Calibri",
        "body_font_size": 22,  # 11pt
        "line_spacing": 276, "line_rule": "auto",  # 1.15x
        "h1_font_ascii": "Calibri", "h1_font_ea": "微软雅黑",
        "h1_font_size": 32,  # 16pt
        "h1_color": "1F3864",  # navy
        "h1_before": 360, "h1_after": 120,
        "h1_bold": True, "h1_italic": False, "h1_justify": "left",
        "h2_font_ascii": "Calibri", "h2_font_ea": "微软雅黑",
        "h2_font_size": 28,  # 14pt
        "h2_color": "2E4057",  # slate
        "h2_bold": True, "h2_italic": False, "h2_justify": "left",
        "justification": "left", "first_indent": 0,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "academic-thesis": {
        "name": "Academic Thesis",
        "source": "University thesis formatting standard",
        "page_size": "A4",
        "page_width_twips": 11906, "page_height_twips": 16838,
        "margin_top": 1440, "margin_bottom": 1440,
        "margin_left": 1800, "margin_right": 1440,
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体",
        "body_font_cs": "Times New Roman",
        "body_font_size": 24,  # 12pt
        "line_spacing": 480, "line_rule": "auto",  # 2x
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体",
        "h1_font_size": 32,  # 16pt (14pt bold in C# but thesis typically 16pt)
        "h1_color": "000000",
        "h1_before": 480, "h1_after": 240,
        "h1_bold": True, "h1_italic": False, "h1_justify": "center",
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体",
        "h2_font_size": 28,  # 14pt
        "h2_color": "000000",
        "h2_bold": True, "h2_italic": False, "h2_justify": "left",
        "justification": "both", "first_indent": 720,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "executive-brief": {
        "name": "Executive Brief",
        "source": "McKinsey/BCG briefing style",
        "page_size": "Letter",
        "page_width_twips": 12240, "page_height_twips": 15840,
        "margin_top": 1080, "margin_bottom": 1080,
        "margin_left": 1260, "margin_right": 1260,
        "body_font_ascii": "Arial", "body_font_ea": "微软雅黑",
        "body_font_cs": "Arial",
        "body_font_size": 20,  # 10pt
        "line_spacing": 240, "line_rule": "auto",  # 1x
        "h1_font_ascii": "Arial", "h1_font_ea": "微软雅黑",
        "h1_font_size": 28,  # 14pt
        "h1_color": "2E3440",  # charcoal
        "h1_before": 240, "h1_after": 80,
        "h1_bold": True, "h1_italic": False, "h1_justify": "left",
        "h2_font_ascii": "Arial", "h2_font_ea": "微软雅黑",
        "h2_font_size": 24,  # 12pt
        "h2_color": "2E3440",
        "h2_bold": True, "h2_italic": False, "h2_justify": "left",
        "justification": "left", "first_indent": 0,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "chinese-government": {
        "name": "Chinese Government (GB/T 9704)",
        "source": "GB/T 9704-2012 党政机关公文格式",
        "page_size": "A4",
        "page_width_twips": 11906, "page_height_twips": 16838,
        "margin_top": 2268, "margin_bottom": 1701,
        "margin_left": 1701, "margin_right": 1531,
        # 37mm top, 35mm bottom, 28mm left, 26mm right
        "body_font_ascii": "FangSong", "body_font_ea": "仿宋",
        "body_font_cs": "FangSong",
        "body_font_size": 44,  # 二号 = 22pt
        "line_spacing": 570, "line_rule": "exact",  # 28.5pt exact
        "h1_font_ascii": "SimSun", "h1_font_ea": "小标宋",
        "h1_font_size": 44,  # 二号 = 22pt
        "h1_color": "000000",
        "h1_before": 0, "h1_after": 0,
        "h1_bold": True, "h1_italic": False, "h1_justify": "center",
        "h2_font_ascii": "SimHei", "h2_font_ea": "黑体",
        "h2_font_size": 36,  # 三号 = 18pt
        "h2_color": "000000",
        "h2_bold": True, "h2_italic": False, "h2_justify": "left",
        "justification": "both", "first_indent": 888,
        "has_page_numbers": True, "page_number_format": "—1—",
    },
    "minimal-modern": {
        "name": "Minimal Modern",
        "source": "Modern minimalist design",
        "page_size": "A4",
        "page_width_twips": 11906, "page_height_twips": 16838,
        "margin_top": 2160, "margin_bottom": 2160,
        "margin_left": 1800, "margin_right": 1800,
        "body_font_ascii": "Helvetica Neue", "body_font_ea": "苹方-简",
        "body_font_cs": "Helvetica Neue",
        "body_font_size": 22,  # 11pt
        "line_spacing": 360, "line_rule": "auto",  # 1.5x
        "h1_font_ascii": "Helvetica Neue", "h1_font_ea": "苹方-简",
        "h1_font_size": 36,  # 18pt (light weight via font name)
        "h1_color": "333333",
        "h1_before": 600, "h1_after": 200,
        "h1_bold": False, "h1_italic": False, "h1_justify": "left",
        "h2_font_ascii": "Helvetica Neue", "h2_font_ea": "苹方-简",
        "h2_font_size": 28,  # 14pt
        "h2_color": "555555",
        "h2_bold": False, "h2_italic": False, "h2_justify": "left",
        "justification": "left", "first_indent": 0,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "ieee": {
        "name": "IEEE Conference",
        "source": "IEEE conference paper template",
        "page_size": "Letter",
        "page_width_twips": 12240, "page_height_twips": 15840,
        "margin_top": 1080, "margin_bottom": 0,
        "margin_left": 1134, "margin_right": 1134,
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体",
        "body_font_cs": "Times New Roman",
        "body_font_size": 20,  # 10pt
        "line_spacing": 240, "line_rule": "auto",  # single
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体",
        "h1_font_size": 24,  # 12pt small caps
        "h1_color": "000000",
        "h1_before": 180, "h1_after": 60,
        "h1_bold": True, "h1_italic": False, "h1_justify": "center",
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体",
        "h2_font_size": 22,  # 11pt italic
        "h2_color": "000000",
        "h2_bold": False, "h2_italic": True, "h2_justify": "left",
        "justification": "both", "first_indent": 0,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "apa-7th": {
        "name": "APA 7th Edition",
        "source": "APA Publication Manual 7th ed.",
        "page_size": "Letter",
        "page_width_twips": 12240, "page_height_twips": 15840,
        "margin_top": 1440, "margin_bottom": 1440,
        "margin_left": 1440, "margin_right": 1440,
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体",
        "body_font_cs": "Times New Roman",
        "body_font_size": 24,  # 12pt
        "line_spacing": 480, "line_rule": "auto",  # double
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体",
        "h1_font_size": 24,  # 12pt (same size, centered bold)
        "h1_color": "000000",
        "h1_before": 0, "h1_after": 0,
        "h1_bold": True, "h1_italic": False, "h1_justify": "center",
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体",
        "h2_font_size": 24,  # 12pt (left bold)
        "h2_color": "000000",
        "h2_bold": True, "h2_italic": False, "h2_justify": "left",
        "justification": "left", "first_indent": 720,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "mla-9th": {
        "name": "MLA 9th Edition",
        "source": "MLA Handbook 9th ed.",
        "page_size": "Letter",
        "page_width_twips": 12240, "page_height_twips": 15840,
        "margin_top": 1440, "margin_bottom": 1440,
        "margin_left": 1440, "margin_right": 1440,
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体",
        "body_font_cs": "Times New Roman",
        "body_font_size": 24,  # 12pt
        "line_spacing": 480, "line_rule": "auto",  # double
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体",
        "h1_font_size": 24,  # 12pt centered
        "h1_color": "000000",
        "h1_before": 0, "h1_after": 0,
        "h1_bold": False, "h1_italic": False, "h1_justify": "center",
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体",
        "h2_font_size": 24,  # 12pt left bold italic
        "h2_color": "000000",
        "h2_bold": True, "h2_italic": True, "h2_justify": "left",
        "justification": "left", "first_indent": 720,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "chicago": {
        "name": "Chicago/Turabian",
        "source": "Chicago Manual of Style 17th ed.",
        "page_size": "Letter",
        "page_width_twips": 12240, "page_height_twips": 15840,
        "margin_top": 1440, "margin_bottom": 1440,
        "margin_left": 1440, "margin_right": 1440,
        "body_font_ascii": "Times New Roman", "body_font_ea": "宋体",
        "body_font_cs": "Times New Roman",
        "body_font_size": 24,  # 12pt
        "line_spacing": 480, "line_rule": "auto",  # double
        "h1_font_ascii": "Times New Roman", "h1_font_ea": "黑体",
        "h1_font_size": 28,  # 14pt centered bold
        "h1_color": "000000",
        "h1_before": 360, "h1_after": 240,
        "h1_bold": True, "h1_italic": False, "h1_justify": "center",
        "h2_font_ascii": "Times New Roman", "h2_font_ea": "黑体",
        "h2_font_size": 24,  # 12pt centered italic
        "h2_color": "000000",
        "h2_bold": False, "h2_italic": True, "h2_justify": "center",
        "justification": "left", "first_indent": 720,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "springer-lncs": {
        "name": "Springer LNCS",
        "source": "Springer LNCS author guidelines",
        "page_size": "A4",
        "page_width_twips": 11906, "page_height_twips": 16838,
        "margin_top": 1134, "margin_bottom": 1134,
        "margin_left": 1134, "margin_right": 1134,
        # 1.27cm ≈ 0.79in ≈ 1134 twips
        "body_font_ascii": "Computer Modern", "body_font_ea": "宋体",
        "body_font_cs": "Computer Modern",
        "body_font_size": 20,  # 10pt
        "line_spacing": 240, "line_rule": "auto",  # single
        "h1_font_ascii": "Computer Modern", "h1_font_ea": "黑体",
        "h1_font_size": 24,  # 12pt bold
        "h1_color": "000000",
        "h1_before": 180, "h1_after": 60,
        "h1_bold": True, "h1_italic": False, "h1_justify": "left",
        "h2_font_ascii": "Computer Modern", "h2_font_ea": "黑体",
        "h2_font_size": 22,  # 11pt bold
        "h2_color": "000000",
        "h2_bold": True, "h2_italic": False, "h2_justify": "left",
        "justification": "both", "first_indent": 0,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "nature": {
        "name": "Nature",
        "source": "Nature formatting guidelines",
        "page_size": "A4",
        "page_width_twips": 11906, "page_height_twips": 16838,
        "margin_top": 1134, "margin_bottom": 1134,
        "margin_left": 1134, "margin_right": 1134,
        "body_font_ascii": "Arial", "body_font_ea": "微软雅黑",
        "body_font_cs": "Arial",
        "body_font_size": 16,  # 8pt
        "line_spacing": 240, "line_rule": "auto",  # single
        "h1_font_ascii": "Arial", "h1_font_ea": "微软雅黑",
        "h1_font_size": 20,  # 10pt bold
        "h1_color": "000000",
        "h1_before": 120, "h1_after": 40,
        "h1_bold": True, "h1_italic": False, "h1_justify": "left",
        "h2_font_ascii": "Arial", "h2_font_ea": "微软雅黑",
        "h2_font_size": 18,  # 9pt bold
        "h2_color": "000000",
        "h2_bold": True, "h2_italic": False, "h2_justify": "left",
        "justification": "both", "first_indent": 0,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "hbr": {
        "name": "Harvard Business Review",
        "source": "Harvard Business Review style",
        "page_size": "Letter",
        "page_width_twips": 12240, "page_height_twips": 15840,
        "margin_top": 1440, "margin_bottom": 1440,
        "margin_left": 1260, "margin_right": 1260,
        "body_font_ascii": "Georgia", "body_font_ea": "微软雅黑",
        "body_font_cs": "Georgia",
        "body_font_size": 22,  # 11pt
        "line_spacing": 300, "line_rule": "auto",  # 1.25x
        "h1_font_ascii": "Georgia", "h1_font_ea": "微软雅黑",
        "h1_font_size": 36,  # 18pt
        "h1_color": "C41230",  # HBR red
        "h1_before": 480, "h1_after": 200,
        "h1_bold": True, "h1_italic": False, "h1_justify": "left",
        "h2_font_ascii": "Georgia", "h2_font_ea": "微软雅黑",
        "h2_font_size": 28,  # 14pt
        "h2_color": "333333",
        "h2_bold": True, "h2_italic": False, "h2_justify": "left",
        "justification": "left", "first_indent": 0,
        "has_page_numbers": True, "page_number_format": "1",
    },
    "chinese-university-thesis": {
        "name": "Chinese University Thesis",
        "source": "Chinese university thesis formatting standard",
        "page_size": "A4",
        "page_width_twips": 11906, "page_height_twips": 16838,
        "margin_top": 1418, "margin_bottom": 1418,
        "margin_left": 1418, "margin_right": 1418,
        # 2.5cm ≈ 1418 twips all around
        "body_font_ascii": "SimSun", "body_font_ea": "宋体",
        "body_font_cs": "SimSun",
        "body_font_size": 24,  # 小四号 = 12pt
        "line_spacing": 360, "line_rule": "auto",  # 1.5x
        "h1_font_ascii": "SimHei", "h1_font_ea": "黑体",
        "h1_font_size": 32,  # 三号 = 16pt
        "h1_color": "000000",
        "h1_before": 360, "h1_after": 200,
        "h1_bold": True, "h1_italic": False, "h1_justify": "left",
        "h2_font_ascii": "SimHei", "h2_font_ea": "黑体",
        "h2_font_size": 28,  # 四号 = 14pt
        "h2_color": "000000",
        "h2_bold": True, "h2_italic": False, "h2_justify": "left",
        "justification": "both", "first_indent": 480,
        "has_page_numbers": True, "page_number_format": "1",
        # Title: 黑体小二号 = 18pt = 36 half-pt
        "title_font_ascii": "SimHei", "title_font_ea": "黑体",
        "title_font_size": 36,
    },
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


def _clear_children(elem):
    """Remove all children from an element."""
    for child in list(elem):
        elem.remove(child)


# ── Apply recipe ─────────────────────────────────────────────────────────────────

def apply_recipe(files, recipe_name):
    """Apply an aesthetic recipe to a DOCX file's styles and page margins."""
    recipe = RECIPES.get(recipe_name)
    if not recipe:
        raise ValueError(f"Unknown recipe: {recipe_name}")

    _apply_styles(files, recipe)
    _apply_page_margins(files, recipe)

    return recipe["name"]


def _apply_styles(files, recipe):
    """Modify word/styles.xml according to the recipe."""
    if "word/styles.xml" not in files:
        return

    styles_root = parse_xml(files["word/styles.xml"])

    # 1. Update DocDefaults
    _update_doc_defaults(styles_root, recipe)

    # 2. Update Normal style
    _update_normal_style(styles_root, recipe)

    # 3. Update Heading 1 style
    _update_heading_style(styles_root, 1, recipe)

    # 4. Update Heading 2 style
    _update_heading_style(styles_root, 2, recipe)

    # 5. Update Title style (for recipes with title overrides)
    _update_title_style(styles_root, recipe)

    # 6. Add recipe-specific styles
    if recipe_name_from_recipe(recipe) == "chinese-government":
        _add_chinese_govt_styles(styles_root, recipe)

    files["word/styles.xml"] = serialize_xml(styles_root)


def recipe_name_from_recipe(recipe):
    """Get the recipe key from the recipe dict."""
    for key, val in RECIPES.items():
        if val is recipe:
            return key
    return ""


def _update_doc_defaults(styles_root, recipe):
    """Update docDefaults with body font and line spacing from recipe."""
    doc_defaults = styles_root.find(_w("docDefaults"))
    if doc_defaults is None:
        doc_defaults = w_elem("docDefaults")
        styles_root.insert(0, doc_defaults)

    # Run properties default
    rPrDefault = doc_defaults.find(_w("rPrDefault"))
    if rPrDefault is None:
        rPrDefault = ET.SubElement(doc_defaults, _w("rPrDefault"))
    rPr = rPrDefault.find(_w("rPr"))
    if rPr is None:
        rPr = ET.SubElement(rPrDefault, _w("rPr"))
    else:
        _clear_children(rPr)

    rf = ET.SubElement(rPr, _w("rFonts"))
    rf.set(_w("ascii"), recipe["body_font_ascii"])
    rf.set(_w("hAnsi"), recipe["body_font_ascii"])
    rf.set(_w("eastAsia"), recipe["body_font_ea"])
    rf.set(_w("cs"), recipe.get("body_font_cs", recipe["body_font_ascii"]))
    sz = ET.SubElement(rPr, _w("sz"))
    sz.set(_w("val"), str(recipe["body_font_size"]))
    szCs = ET.SubElement(rPr, _w("szCs"))
    szCs.set(_w("val"), str(recipe["body_font_size"]))

    # Paragraph properties default
    pPrDefault = doc_defaults.find(_w("pPrDefault"))
    if pPrDefault is None:
        pPrDefault = ET.SubElement(doc_defaults, _w("pPrDefault"))
    pPr = pPrDefault.find(_w("pPr"))
    if pPr is None:
        pPr = ET.SubElement(pPrDefault, _w("pPr"))
    else:
        _clear_children(pPr)
    spacing = ET.SubElement(pPr, _w("spacing"))
    spacing.set(_w("line"), str(recipe["line_spacing"]))
    spacing.set(_w("lineRule"), recipe["line_rule"])


def _update_normal_style(styles_root, recipe):
    """Update or create the Normal style."""
    normal = None
    for style in styles_root.findall(_w("style")):
        if style.get(_w("styleId"), "") == "Normal":
            normal = style
            break

    if normal is None:
        normal = ET.SubElement(styles_root, _w("style"))
        normal.set(_w("type"), "paragraph")
        normal.set(_w("styleId"), "Normal")
        nm = ET.SubElement(normal, _w("name"))
        nm.set(_w("val"), "Normal")

    # Update pPr
    pPr = normal.find(_w("pPr"))
    if pPr is None:
        pPr = ET.SubElement(normal, _w("pPr"))
    else:
        _clear_children(pPr)

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
        _clear_children(rPr)
    rf = ET.SubElement(rPr, _w("rFonts"))
    rf.set(_w("ascii"), recipe["body_font_ascii"])
    rf.set(_w("hAnsi"), recipe["body_font_ascii"])
    rf.set(_w("eastAsia"), recipe["body_font_ea"])
    sz = ET.SubElement(rPr, _w("sz"))
    sz.set(_w("val"), str(recipe["body_font_size"]))
    szCs = ET.SubElement(rPr, _w("szCs"))
    szCs.set(_w("val"), str(recipe["body_font_size"]))


def _update_heading_style(styles_root, level, recipe):
    """Update or create a heading style (1 or 2) based on recipe."""
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
        nm = ET.SubElement(heading, _w("name"))
        nm.set(_w("val"), f"heading {level}")
        based = ET.SubElement(heading, _w("basedOn"))
        based.set(_w("val"), "Normal")
        nxt = ET.SubElement(heading, _w("next"))
        nxt.set(_w("val"), "Normal")

    # Prefix key for recipe lookups
    prefix = f"h{level}_"

    # Update pPr
    pPr = heading.find(_w("pPr"))
    if pPr is None:
        pPr = ET.SubElement(heading, _w("pPr"))
    else:
        _clear_children(pPr)

    ET.SubElement(pPr, _w("keepNext"))
    ET.SubElement(pPr, _w("keepLines"))

    if level == 1:
        sp = ET.SubElement(pPr, _w("spacing"))
        sp.set(_w("before"), str(recipe.get("h1_before", 360)))
        sp.set(_w("after"), str(recipe.get("h1_after", 120)))
    else:
        sp = ET.SubElement(pPr, _w("spacing"))
        sp.set(_w("before"), "280")
        sp.set(_w("after"), "80")

    # Justification for heading
    h_justify = recipe.get(f"h{level}_justify", "left")
    if h_justify != "left":
        jc = ET.SubElement(pPr, _w("jc"))
        jc.set(_w("val"), h_justify)

    ol = ET.SubElement(pPr, _w("outlineLvl"))
    ol.set(_w("val"), str(level - 1))

    # Update rPr
    rPr = heading.find(_w("rPr"))
    if rPr is None:
        rPr = ET.SubElement(heading, _w("rPr"))
    else:
        _clear_children(rPr)

    h_font_ascii = recipe.get(f"h{level}_font_ascii", recipe["body_font_ascii"])
    h_font_ea = recipe.get(f"h{level}_font_ea", recipe["body_font_ea"])
    h_font_size = recipe.get(f"h{level}_font_size", recipe["body_font_size"])
    h_color = recipe.get(f"h{level}_color", "000000")
    h_bold = recipe.get(f"h{level}_bold", True)
    h_italic = recipe.get(f"h{level}_italic", False)

    rf = ET.SubElement(rPr, _w("rFonts"))
    rf.set(_w("ascii"), h_font_ascii)
    rf.set(_w("hAnsi"), h_font_ascii)
    rf.set(_w("eastAsia"), h_font_ea)
    sz = ET.SubElement(rPr, _w("sz"))
    sz.set(_w("val"), str(h_font_size))
    szCs = ET.SubElement(rPr, _w("szCs"))
    szCs.set(_w("val"), str(h_font_size))
    if h_bold:
        ET.SubElement(rPr, _w("b"))
        ET.SubElement(rPr, _w("bCs"))
    if h_italic:
        ET.SubElement(rPr, _w("i"))
        ET.SubElement(rPr, _w("iCs"))
    c = ET.SubElement(rPr, _w("color"))
    c.set(_w("val"), h_color)


def _update_title_style(styles_root, recipe):
    """Update the Title style if the recipe specifies title overrides."""
    title_font_ascii = recipe.get("title_font_ascii")
    title_font_ea = recipe.get("title_font_ea")
    title_font_size = recipe.get("title_font_size")

    if not title_font_ascii and not title_font_size:
        return  # No title overrides in this recipe

    title_style = None
    for style in styles_root.findall(_w("style")):
        if style.get(_w("styleId"), "") == "Title":
            title_style = style
            break

    if title_style is None:
        return  # No Title style to update

    rPr = title_style.find(_w("rPr"))
    if rPr is None:
        rPr = ET.SubElement(title_style, _w("rPr"))
    else:
        _clear_children(rPr)

    if title_font_ascii:
        rf = ET.SubElement(rPr, _w("rFonts"))
        rf.set(_w("ascii"), title_font_ascii)
        rf.set(_w("hAnsi"), title_font_ascii)
        if title_font_ea:
            rf.set(_w("eastAsia"), title_font_ea)
    if title_font_size:
        sz = ET.SubElement(rPr, _w("sz"))
        sz.set(_w("val"), str(title_font_size))
        szCs = ET.SubElement(rPr, _w("szCs"))
        szCs.set(_w("val"), str(title_font_size))
    ET.SubElement(rPr, _w("b"))
    ET.SubElement(rPr, _w("bCs"))


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
        _clear_children(pPr)
    sp = ET.SubElement(pPr, _w("spacing"))
    sp.set(_w("line"), "560")
    sp.set(_w("lineRule"), "exact")
    jc = ET.SubElement(pPr, _w("jc"))
    jc.set(_w("val"), "both")

    rPr = gov_body.find(_w("rPr"))
    if rPr is None:
        rPr = ET.SubElement(gov_body, _w("rPr"))
    else:
        _clear_children(rPr)
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
        _clear_children(pPr2)
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
        _clear_children(rPr2)
    rf2 = ET.SubElement(rPr2, _w("rFonts"))
    rf2.set(_w("ascii"), "SimSun")
    rf2.set(_w("hAnsi"), "SimSun")
    rf2.set(_w("eastAsia"), "小标宋")
    rf2.set(_w("cs"), "SimSun")
    sz2 = ET.SubElement(rPr2, _w("sz"))
    sz2.set(_w("val"), "44")
    ET.SubElement(rPr2, _w("b"))


def _apply_page_margins(files, recipe):
    """Modify page margins in document.xml's sectPr."""
    if "word/document.xml" not in files:
        return

    doc_root = parse_xml(files["word/document.xml"])
    body = doc_root.find(_w("body"))
    if body is None:
        return

    # Find sectPr (last child of body)
    sect_pr = body.find(_w("sectPr"))
    if sect_pr is None:
        return

    # Update pgSz
    pg_sz = sect_pr.find(_w("pgSz"))
    if pg_sz is not None:
        pg_sz.set(_w("w"), str(recipe["page_width_twips"]))
        pg_sz.set(_w("h"), str(recipe["page_height_twips"]))

    # Update pgMar
    pg_mar = sect_pr.find(_w("pgMar"))
    if pg_mar is not None:
        pg_mar.set(_w("top"), str(recipe["margin_top"]))
        pg_mar.set(_w("bottom"), str(recipe["margin_bottom"]))
        pg_mar.set(_w("left"), str(recipe["margin_left"]))
        pg_mar.set(_w("right"), str(recipe["margin_right"]))
        pg_mar.set(_w("header"), str(int(recipe["margin_top"] / 2)))
        pg_mar.set(_w("footer"), str(int(recipe["margin_bottom"] / 2)))
        pg_mar.set(_w("gutter"), "0")

    files["word/document.xml"] = serialize_xml(doc_root)


# ── Show recipe details ──────────────────────────────────────────────────────────

def show_recipe(recipe_name):
    """Print detailed information about a recipe."""
    recipe = RECIPES.get(recipe_name)
    if not recipe:
        print(f"Unknown recipe: {recipe_name}", file=sys.stderr)
        return

    print(f"Recipe: {recipe['name']}")
    print(f"Source: {recipe['source']}")
    print()
    print(f"Page size:      {recipe['page_size']} ({recipe['page_width_twips']}×{recipe['page_height_twips']} twips)")
    print(f"Margins (T/B/L/R): {recipe['margin_top']}/{recipe['margin_bottom']}/{recipe['margin_left']}/{recipe['margin_right']} twips")
    top_mm = round(recipe['margin_top'] / 56.7, 1)
    bot_mm = round(recipe['margin_bottom'] / 56.7, 1)
    left_mm = round(recipe['margin_left'] / 56.7, 1)
    right_mm = round(recipe['margin_right'] / 56.7, 1)
    print(f"Margins (mm):   {top_mm}/{bot_mm}/{left_mm}/{right_mm} mm")
    print()
    print(f"Body font:      {recipe['body_font_ascii']} / {recipe['body_font_ea']}")
    body_pt = recipe['body_font_size'] / 2
    print(f"Body size:      {body_pt}pt ({recipe['body_font_size']} half-pts)")
    line_rule_str = f"{recipe['line_spacing'] / 240:.2f}x" if recipe['line_rule'] == 'auto' else f"{recipe['line_spacing'] / 20}pt exact"
    print(f"Line spacing:   {recipe['line_spacing']} ({line_rule_str})")
    print(f"Justification:  {recipe['justification']}")
    print(f"First indent:   {recipe['first_indent']} twips")
    print()
    h1_pt = recipe['h1_font_size'] / 2
    print(f"Heading 1:      {recipe['h1_font_ascii']} / {recipe['h1_font_ea']}, {h1_pt}pt")
    print(f"  Color:        #{recipe['h1_color']}")
    print(f"  Bold:         {recipe.get('h1_bold', True)}")
    print(f"  Italic:       {recipe.get('h1_italic', False)}")
    print(f"  Justify:      {recipe.get('h1_justify', 'left')}")
    print(f"  Spacing:      before={recipe['h1_before']} after={recipe['h1_after']}")
    print()
    h2_pt = recipe['h2_font_size'] / 2
    print(f"Heading 2:      {recipe['h2_font_ascii']} / {recipe['h2_font_ea']}, {h2_pt}pt")
    print(f"  Color:        #{recipe['h2_color']}")
    print(f"  Bold:         {recipe.get('h2_bold', True)}")
    print(f"  Italic:       {recipe.get('h2_italic', False)}")
    print(f"  Justify:      {recipe.get('h2_justify', 'left')}")

    if recipe.get("title_font_ascii"):
        title_pt = recipe['title_font_size'] / 2
        print()
        print(f"Title:          {recipe['title_font_ascii']} / {recipe.get('title_font_ea', '')}, {title_pt}pt")


def list_recipes():
    """List all available recipes."""
    for key, recipe in RECIPES.items():
        body_pt = recipe['body_font_size'] / 2
        line_rule_str = f"{recipe['line_spacing'] / 240:.1f}x" if recipe['line_rule'] == 'auto' else f"{recipe['line_spacing'] / 20:.1f}pt"
        print(f"  {key:28s}  {recipe['name']:30s}  {recipe['body_font_ascii']} {body_pt:.0f}pt/{line_rule_str}")


# ── Main ─────────────────────────────────────────────────────────────────────────

def process_aesthetic(input_path=None, output_path=None, recipe_name=None,
                      show_recipe_name=None, list_flag=False):
    """Process aesthetic recipes on a DOCX file."""
    if list_flag:
        list_recipes()
        return

    if show_recipe_name:
        show_recipe(show_recipe_name)
        return

    if not input_path:
        print("Error: input file required when applying a recipe", file=sys.stderr)
        sys.exit(1)

    files = read_docx(input_path)
    applied_name = apply_recipe(files, recipe_name)
    out = output_path or input_path
    write_docx(files, out)
    print(f"Applied recipe: {applied_name}")
    print(f"Saved: {out}")


def main():
    parser = argparse.ArgumentParser(
        description="Apply predefined aesthetic recipes to DOCX files.",
        epilog="Uses ONLY Python standard library. No third-party dependencies.",
    )
    parser.add_argument("input", nargs="?", default=None, help="Input DOCX file path")
    parser.add_argument("--output", default=None,
                        help="Output DOCX file path (default: modify in-place)")
    parser.add_argument("--recipe", choices=list(RECIPES.keys()), default=None,
                        help="Apply an aesthetic recipe")
    parser.add_argument("--show-recipe", default=None,
                        choices=list(RECIPES.keys()),
                        help="Show detailed information about a recipe")
    parser.add_argument("--list-recipes", action="store_true",
                        help="List all available recipes")

    args = parser.parse_args()

    if not any([args.recipe, args.show_recipe, args.list_recipes]):
        parser.print_help()
        sys.exit(1)

    try:
        process_aesthetic(
            input_path=args.input,
            output_path=args.output,
            recipe_name=args.recipe,
            show_recipe_name=args.show_recipe,
            list_flag=args.list_recipes,
        )
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
