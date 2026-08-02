#!/usr/bin/env python3
"""docx_validate.py — Validate a DOCX file's OpenXML structure.

Uses ONLY Python standard library. No third-party dependencies.

Usage:
    python3 docx_validate.py document.docx
    python3 docx_validate.py document.docx --json
    python3 docx_validate.py document.docx --quick
    python3 docx_validate.py document.docx -v
"""

import argparse
import json
import os
import sys
import zipfile
import io
import xml.etree.ElementTree as ET
from collections import defaultdict

# ── Namespaces ───────────────────────────────────────────────────────────────────

NS = {
    "w":   "http://schemas.openxmlformats.org/wordprocessingml/2006/main",
    "r":   "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
    "ct":  "http://schemas.openxmlformats.org/package/2006/content-types",
    "rel": "http://schemas.openxmlformats.org/package/2006/relationships",
    "a":   "http://schemas.openxmlformats.org/drawingml/2006/main",
    "wp":  "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing",
    "pic": "http://schemas.openxmlformats.org/drawingml/2006/picture",
}
for prefix, uri in NS.items():
    ET.register_namespace(prefix, uri)

W = NS["w"]
R = NS["r"]
CT = NS["ct"]
REL = NS["rel"]


def _w(tag):
    return f"{{{W}}}{tag}"


# ── Validation result ────────────────────────────────────────────────────────────

class CheckResult:
    """Result of a single validation check."""
    def __init__(self, name, status, severity="info", details=""):
        self.name = name
        self.status = status  # "pass" or "fail"
        self.severity = severity  # "error", "warning", "info"
        self.details = details

    def to_dict(self):
        return {
            "name": self.name,
            "status": self.status,
            "severity": self.severity,
            "details": self.details,
        }


# ── DOCX Reading ─────────────────────────────────────────────────────────────────

def read_docx(docx_path):
    """Read a DOCX file, return dict of {internal_path: bytes}."""
    files = {}
    with zipfile.ZipFile(docx_path, "r") as zf:
        for name in zf.namelist():
            files[name] = zf.read(name)
    return files


def parse_xml_safe(xml_bytes):
    """Parse XML bytes, returning None on failure."""
    try:
        return ET.fromstring(xml_bytes)
    except ET.ParseError:
        return None


# ── Validation Checks ────────────────────────────────────────────────────────────

def check_zip_integrity(files):
    """Check 1: ZIP integrity and [Content_Types].xml existence."""
    if "[Content_Types].xml" in files:
        return CheckResult("ZIP Integrity", "pass", "info",
                           "File is a valid ZIP archive with [Content_Types].xml")
    return CheckResult("ZIP Integrity", "fail", "error",
                       "Missing [Content_Types].xml — file may not be a valid DOCX")


def check_required_files(files):
    """Check 2: Required files exist."""
    required = ["word/document.xml", "word/styles.xml", "word/settings.xml"]
    missing = [f for f in required if f not in files]
    if not missing:
        return CheckResult("Required Files", "pass", "info",
                           "All required files present: document.xml, styles.xml, settings.xml")
    return CheckResult("Required Files", "fail", "error",
                       f"Missing required files: {', '.join(missing)}")


def check_element_ordering(files, verbose=False):
    """Check 3: Element ordering within paragraphs and tables."""
    issues = []
    if "word/document.xml" not in files:
        return CheckResult("Element Ordering", "fail", "error", "document.xml not found")

    doc_root = parse_xml_safe(files["word/document.xml"])
    if doc_root is None:
        return CheckResult("Element Ordering", "fail", "error", "document.xml is not valid XML")

    body = doc_root.find(_w("body"))
    if body is None:
        return CheckResult("Element Ordering", "fail", "error", "No w:body element found")

    # Check paragraph ordering: pPr should come before runs
    p_count = 0
    for p in body.iter(_w("p")):
        p_count += 1
        children = list(p)
        ppr_idx = -1
        first_run_idx = len(children)
        for i, child in enumerate(children):
            if child.tag == _w("pPr"):
                ppr_idx = i
            elif child.tag == _w("r"):
                if i < first_run_idx:
                    first_run_idx = i

        if ppr_idx > 0 and first_run_idx < ppr_idx:
            issues.append(f"Paragraph {p_count}: pPr appears after runs")

        # Check rPr before t within runs
        for r in p.findall(_w("r")):
            r_children = list(r)
            rpr_idx = -1
            first_t_idx = len(r_children)
            for i, child in enumerate(r_children):
                if child.tag == _w("rPr"):
                    rpr_idx = i
                elif child.tag == _w("t"):
                    if i < first_t_idx:
                        first_t_idx = i
            if rpr_idx > 0 and first_t_idx < rpr_idx:
                issues.append(f"Paragraph {p_count}: rPr appears after t in a run")

    # Check table ordering: tblPr → tblGrid → tr
    for tbl in body.iter(_w("tbl")):
        children = list(tbl)
        order = []
        for child in children:
            if child.tag == _w("tblPr"):
                order.append("tblPr")
            elif child.tag == _w("tblGrid"):
                order.append("tblGrid")
            elif child.tag == _w("tr"):
                order.append("tr")
            else:
                order.append("other")

        if order and order[0] != "tblPr":
            issues.append("Table: tblPr is not the first child")
        if "tblGrid" in order and "tblPr" in order:
            if order.index("tblGrid") < order.index("tblPr"):
                issues.append("Table: tblGrid appears before tblPr")

    # Check tr ordering: trPr before tc
    tr_count = 0
    for tr in body.iter(_w("tr")):
        tr_count += 1
        children = list(tr)
        trpr_idx = -1
        first_tc_idx = len(children)
        for i, child in enumerate(children):
            if child.tag == _w("trPr"):
                trpr_idx = i
            elif child.tag == _w("tc"):
                if i < first_tc_idx:
                    first_tc_idx = i
        if trpr_idx > 0 and first_tc_idx < trpr_idx:
            issues.append(f"Row {tr_count}: trPr appears after tc")

    # Check tc ordering: tcPr before content
    for tc in body.iter(_w("tc")):
        children = list(tc)
        tcpr_idx = -1
        first_content_idx = len(children)
        for i, child in enumerate(children):
            if child.tag == _w("tcPr"):
                tcpr_idx = i
            elif child.tag in (_w("p"), _w("tbl"), _w("sdt")):
                if i < first_content_idx:
                    first_content_idx = i
        if tcpr_idx > 0 and first_content_idx < tcpr_idx:
            issues.append("Cell: tcPr appears after content")

    # Check sectPr is last child of body or inside pPr
    body_children = list(body)
    for i, child in enumerate(body_children[:-1]):
        if child.tag == _w("sectPr"):
            issues.append("sectPr is not the last child of body")

    if not issues:
        return CheckResult("Element Ordering", "pass", "info",
                           f"Element ordering is correct ({p_count} paragraphs, checked pPr/run/rPr/t, tbl, tr, tc, sectPr)")
    return CheckResult("Element Ordering", "fail", "warning",
                       f"{len(issues)} ordering issue(s): {'; '.join(issues[:5])}")


def check_style_references(files, verbose=False):
    """Check 4: All pStyle/rStyle references point to existing styles."""
    if "word/document.xml" not in files or "word/styles.xml" not in files:
        return CheckResult("Style References", "fail", "error", "Missing document.xml or styles.xml")

    doc_root = parse_xml_safe(files["word/document.xml"])
    styles_root = parse_xml_safe(files["word/styles.xml"])
    if doc_root is None or styles_root is None:
        return CheckResult("Style References", "fail", "error", "Invalid XML in document.xml or styles.xml")

    # Collect defined style IDs
    defined_ids = set()
    for style in styles_root.findall(_w("style")):
        sid = style.get(_w("styleId"), "")
        if sid:
            defined_ids.add(sid)

    # Check pStyle references
    issues = []
    for pPr in doc_root.iter(_w("pPr")):
        pStyle = pPr.find(_w("pStyle"))
        if pStyle is not None:
            val = pStyle.get(_w("val"), "")
            if val and val not in defined_ids:
                issues.append(f"pStyle '{val}' not defined")

    # Check rStyle references
    for rPr in doc_root.iter(_w("rPr")):
        rStyle = rPr.find(_w("rStyle"))
        if rStyle is not None:
            val = rStyle.get(_w("val"), "")
            if val and val not in defined_ids:
                issues.append(f"rStyle '{val}' not defined")

    if not issues:
        return CheckResult("Style References", "pass", "info",
                           f"All style references valid ({len(defined_ids)} styles defined)")
    return CheckResult("Style References", "fail", "warning",
                       f"{len(issues)} undefined style reference(s): {'; '.join(issues[:5])}")


def check_relationship_integrity(files, verbose=False):
    """Check 5: All rId references have matching .rels entries."""
    if "word/document.xml" not in files:
        return CheckResult("Relationship Integrity", "fail", "error", "document.xml not found")

    rels_path = "word/_rels/document.xml.rels"
    if rels_path not in files:
        return CheckResult("Relationship Integrity", "fail", "warning",
                           "word/_rels/document.xml.rels not found")

    doc_root = parse_xml_safe(files["word/document.xml"])
    rels_root = parse_xml_safe(files[rels_path])
    if doc_root is None or rels_root is None:
        return CheckResult("Relationship Integrity", "fail", "error", "Invalid XML in document or rels")

    # Collect defined rIds
    defined_rids = set()
    for rel in rels_root.findall(f"{{{REL}}}Relationship"):
        rid = rel.get("Id", "")
        if rid:
            defined_rids.add(rid)

    # Find rId references in document.xml
    issues = []
    for elem in doc_root.iter():
        rid_val = elem.get(f"{{{R}}}id", "")
        if rid_val and rid_val not in defined_rids:
            tag_local = elem.tag.split("}")[1] if "}" in elem.tag else elem.tag
            issues.append(f"rId '{rid_val}' in <{tag_local}> not found in .rels")

    if not issues:
        return CheckResult("Relationship Integrity", "pass", "info",
                           f"All rId references valid ({len(defined_rids)} relationships defined)")
    return CheckResult("Relationship Integrity", "fail", "warning",
                       f"{len(issues)} missing relationship(s): {'; '.join(issues[:5])}")


def check_image_references(files, verbose=False):
    """Check 6: All blipFill references have matching image files."""
    if "word/document.xml" not in files:
        return CheckResult("Image References", "pass", "info", "No document.xml to check")

    doc_root = parse_xml_safe(files["word/document.xml"])
    if doc_root is None:
        return CheckResult("Image References", "fail", "error", "document.xml is not valid XML")

    # Find all blip references (a:blip with r:embed)
    blip_ns = "{http://schemas.openxmlformats.org/drawingml/2006/main}"
    issues = []
    image_count = 0

    for blip in doc_root.iter(f"{blip_ns}blip"):
        embed = blip.get(f"{{{R}}}embed", "")
        if not embed:
            continue
        image_count += 1

        # Resolve rId to target file via .rels
        rels_path = "word/_rels/document.xml.rels"
        if rels_path not in files:
            issues.append(f"Image rId '{embed}' — rels file not found")
            continue

        rels_root = parse_xml_safe(files[rels_path])
        if rels_root is None:
            issues.append(f"Image rId '{embed}' — rels file is invalid XML")
            continue

        target = None
        for rel in rels_root.findall(f"{{{REL}}}Relationship"):
            if rel.get("Id", "") == embed:
                target = rel.get("Target", "")
                break

        if target is None:
            issues.append(f"Image rId '{embed}' — no matching relationship")
            continue

        # Check if the image file exists in the ZIP
        image_path = f"word/{target}" if not target.startswith("word/") else target
        if image_path not in files:
            issues.append(f"Image '{target}' (rId '{embed}') — file not found in archive")

    if not issues:
        if image_count == 0:
            return CheckResult("Image References", "pass", "info", "No image references found")
        return CheckResult("Image References", "pass", "info",
                           f"All {image_count} image reference(s) valid")
    return CheckResult("Image References", "fail", "warning",
                       f"{len(issues)} missing image(s): {'; '.join(issues[:5])}")


def check_numbering_references(files, verbose=False):
    """Check 7: All numPr/numId point to valid numbering definitions."""
    if "word/document.xml" not in files:
        return CheckResult("Numbering References", "pass", "info", "No document.xml to check")

    doc_root = parse_xml_safe(files["word/document.xml"])
    if doc_root is None:
        return CheckResult("Numbering References", "fail", "error", "document.xml is not valid XML")

    # Collect numId references
    referenced_num_ids = set()
    for numPr in doc_root.iter(_w("numPr")):
        numId_elem = numPr.find(_w("numId"))
        if numId_elem is not None:
            val = numId_elem.get(_w("val"), "")
            if val:
                referenced_num_ids.add(val)

    if not referenced_num_ids:
        return CheckResult("Numbering References", "pass", "info", "No numbering references found")

    # Check numbering.xml
    if "word/numbering.xml" not in files:
        return CheckResult("Numbering References", "fail", "warning",
                           f"Paragraphs reference numbering but word/numbering.xml is missing")

    numbering_root = parse_xml_safe(files["word/numbering.xml"])
    if numbering_root is None:
        return CheckResult("Numbering References", "fail", "error", "numbering.xml is not valid XML")

    # Collect defined numIds
    defined_num_ids = set()
    for num in numbering_root.findall(_w("num")):
        nid = num.get(_w("numId"), "")
        if nid:
            defined_num_ids.add(nid)

    issues = []
    for nid in referenced_num_ids:
        if nid == "0":
            continue  # numId=0 means "no numbering"
        if nid not in defined_num_ids:
            issues.append(f"numId '{nid}' not defined in numbering.xml")

    if not issues:
        return CheckResult("Numbering References", "pass", "info",
                           f"All {len(referenced_num_ids)} numbering reference(s) valid")
    return CheckResult("Numbering References", "fail", "warning",
                       f"{len(issues)} undefined numbering reference(s): {'; '.join(issues[:5])}")


def check_font_contamination(files, verbose=False):
    """Check 8: No unexpected inline rPr overwriting heading styles."""
    if "word/document.xml" not in files or "word/styles.xml" not in files:
        return CheckResult("Font Contamination", "pass", "info", "Cannot check — missing files")

    doc_root = parse_xml_safe(files["word/document.xml"])
    styles_root = parse_xml_safe(files["word/styles.xml"])
    if doc_root is None or styles_root is None:
        return CheckResult("Font Contamination", "pass", "info", "Cannot check — invalid XML")

    # Find heading style IDs
    heading_style_ids = set()
    for style in styles_root.findall(_w("style")):
        name_elem = style.find(_w("name"))
        if name_elem is not None:
            name_val = name_elem.get(_w("val"), "").lower()
            if name_val.startswith("heading") or name_val.startswith("toc heading"):
                heading_style_ids.add(style.get(_w("styleId"), ""))

    issues = []
    for p in doc_root.iter(_w("p")):
        pPr = p.find(_w("pPr"))
        if pPr is None:
            continue
        pStyle = pPr.find(_w("pStyle"))
        if pStyle is None:
            continue
        style_val = pStyle.get(_w("val"), "")
        if style_val not in heading_style_ids:
            continue

        # This paragraph uses a heading style — check if runs override fonts
        for r in p.findall(_w("r")):
            rPr = r.find(_w("rPr"))
            if rPr is None:
                continue
            rFonts = rPr.find(_w("rFonts"))
            if rFonts is not None:
                # Heading paragraphs shouldn't have inline font overrides
                issues.append(f"Heading paragraph (style={style_val}) has inline rPr/rFonts override")

    if not issues:
        return CheckResult("Font Contamination", "pass", "info",
                           "No font contamination in heading paragraphs")
    return CheckResult("Font Contamination", "fail", "warning",
                       f"{len(issues)} heading font override(s) found: {'; '.join(str(i) for i in issues[:3])}")


def check_empty_paragraphs(files, verbose=False):
    """Check 9: Table cells have at least one w:p."""
    if "word/document.xml" not in files:
        return CheckResult("Empty Cells", "pass", "info", "No document.xml to check")

    doc_root = parse_xml_safe(files["word/document.xml"])
    if doc_root is None:
        return CheckResult("Empty Cells", "fail", "error", "document.xml is not valid XML")

    issues = []
    for tc in doc_root.iter(_w("tc")):
        has_p = tc.find(_w("p")) is not None
        if not has_p:
            issues.append("Table cell has no w:p element")

    if not issues:
        return CheckResult("Empty Cells", "pass", "info", "All table cells contain paragraphs")
    return CheckResult("Empty Cells", "fail", "warning",
                       f"{len(issues)} table cell(s) without paragraphs")


def check_table_structure(files, verbose=False):
    """Check 10: Consistent number of cells per row in each table."""
    if "word/document.xml" not in files:
        return CheckResult("Table Structure", "pass", "info", "No document.xml to check")

    doc_root = parse_xml_safe(files["word/document.xml"])
    if doc_root is None:
        return CheckResult("Table Structure", "fail", "error", "document.xml is not valid XML")

    issues = []
    tbl_count = 0
    for tbl in doc_root.iter(_w("tbl")):
        tbl_count += 1
        rows = tbl.findall(_w("tr"))
        if len(rows) < 2:
            continue

        col_counts = []
        for tr in rows:
            cells = tr.findall(_w("tc"))
            # Account for gridSpan
            span_total = 0
            for tc in cells:
                tcPr = tc.find(_w("tcPr"))
                if tcPr is not None:
                    gridSpan = tcPr.find(_w("gridSpan"))
                    if gridSpan is not None:
                        try:
                            span_total += int(gridSpan.get(_w("val"), "1"))
                            continue
                        except ValueError:
                            pass
                span_total += 1
            col_counts.append(span_total)

        if len(set(col_counts)) > 1:
            issues.append(f"Table {tbl_count}: inconsistent column counts {col_counts}")

    if not issues:
        if tbl_count == 0:
            return CheckResult("Table Structure", "pass", "info", "No tables found")
        return CheckResult("Table Structure", "pass", "info",
                           f"All {tbl_count} table(s) have consistent column structure")
    return CheckResult("Table Structure", "fail", "warning",
                       f"{len(issues)} table structure issue(s): {'; '.join(issues[:3])}")


def check_section_properties(files, verbose=False):
    """Check 11: sectPr only as last child of body or in pPr."""
    if "word/document.xml" not in files:
        return CheckResult("Section Properties", "pass", "info", "No document.xml to check")

    doc_root = parse_xml_safe(files["word/document.xml"])
    if doc_root is None:
        return CheckResult("Section Properties", "fail", "error", "document.xml is not valid XML")

    body = doc_root.find(_w("body"))
    if body is None:
        return CheckResult("Section Properties", "fail", "error", "No w:body element found")

    issues = []
    body_children = list(body)

    # Check: sectPr should be last child of body
    for i, child in enumerate(body_children):
        if child.tag == _w("sectPr") and i < len(body_children) - 1:
            issues.append("sectPr is not the last child of w:body")

    # Check: sectPr inside paragraphs should be in pPr only
    for p in body.iter(_w("p")):
        for child in list(p):
            if child.tag == _w("sectPr"):
                pPr = p.find(_w("pPr"))
                if pPr is None or child not in list(pPr):
                    issues.append("sectPr found in paragraph but outside pPr")

    if not issues:
        return CheckResult("Section Properties", "pass", "info",
                           "Section properties correctly placed")
    return CheckResult("Section Properties", "fail", "warning",
                       f"{len(issues)} sectPr placement issue(s): {'; '.join(issues[:3])}")


# ── Full validation ──────────────────────────────────────────────────────────────

def validate_docx(docx_path, quick=False, verbose=False):
    """Run all validation checks on a DOCX file."""
    try:
        files = read_docx(docx_path)
    except zipfile.BadZipFile:
        return [CheckResult("ZIP Integrity", "fail", "error",
                            "File is not a valid ZIP archive")]
    except Exception as e:
        return [CheckResult("ZIP Integrity", "fail", "error", f"Cannot read file: {e}")]

    results = []

    # Always run these two
    results.append(check_zip_integrity(files))
    results.append(check_required_files(files))

    if quick:
        return results

    # Full checks
    results.append(check_element_ordering(files, verbose))
    results.append(check_style_references(files, verbose))
    results.append(check_relationship_integrity(files, verbose))
    results.append(check_image_references(files, verbose))
    results.append(check_numbering_references(files, verbose))
    results.append(check_font_contamination(files, verbose))
    results.append(check_empty_paragraphs(files, verbose))
    results.append(check_table_structure(files, verbose))
    results.append(check_section_properties(files, verbose))

    return results


# ── Output formatting ────────────────────────────────────────────────────────────

def format_results(results, verbose=False):
    """Format validation results as text."""
    lines = []
    errors = 0
    warnings = 0

    for r in results:
        icon = "✓" if r.status == "pass" else "✗"
        severity_tag = ""
        if r.status == "fail":
            if r.severity == "error":
                severity_tag = " [ERROR]"
                errors += 1
            elif r.severity == "warning":
                severity_tag = " [WARN]"
                warnings += 1

        lines.append(f"  {icon} {r.name}{severity_tag}")
        if verbose or r.status == "fail":
            if r.details:
                lines.append(f"      {r.details}")

    # Summary
    lines.append("")
    if errors == 0 and warnings == 0:
        lines.append("PASS — All checks passed")
    elif errors == 0:
        lines.append(f"PASS (with {warnings} warning(s)) — No errors found")
    else:
        lines.append(f"FAIL — {errors} error(s), {warnings} warning(s)")

    return "\n".join(lines)


def format_results_json(results):
    """Format validation results as JSON."""
    data = {
        "checks": [r.to_dict() for r in results],
        "summary": {
            "total": len(results),
            "passed": sum(1 for r in results if r.status == "pass"),
            "failed": sum(1 for r in results if r.status == "fail"),
            "errors": sum(1 for r in results if r.status == "fail" and r.severity == "error"),
            "warnings": sum(1 for r in results if r.status == "fail" and r.severity == "warning"),
        }
    }
    data["summary"]["status"] = "PASS" if data["summary"]["errors"] == 0 else "FAIL"
    return json.dumps(data, indent=2, ensure_ascii=False)


# ── Main ─────────────────────────────────────────────────────────────────────────

def process_validate(input_path, json_output=False, quick=False, verbose=False):
    """Validate a DOCX file and print results."""
    results = validate_docx(input_path, quick=quick, verbose=verbose)

    if json_output:
        print(format_results_json(results))
    else:
        print(f"Validating: {input_path}")
        print()
        print(format_results(results, verbose))

    # Exit code: 0 = all pass, 1 = errors found
    has_errors = any(r.status == "fail" and r.severity == "error" for r in results)
    return 1 if has_errors else 0


def main():
    parser = argparse.ArgumentParser(
        description="Validate a DOCX file's OpenXML structure.",
        epilog="Uses ONLY Python standard library. No third-party dependencies.",
    )
    parser.add_argument("input", help="Input DOCX file path")
    parser.add_argument("--json", action="store_true", dest="json_output",
                        help="Output results as JSON")
    parser.add_argument("--quick", action="store_true",
                        help="Quick check only (ZIP integrity + required files)")
    parser.add_argument("-v", "--verbose", action="store_true",
                        help="Show details for all checks, not just failures")

    args = parser.parse_args()

    try:
        exit_code = process_validate(
            input_path=args.input,
            json_output=args.json_output,
            quick=args.quick,
            verbose=args.verbose,
        )
        sys.exit(exit_code)
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
