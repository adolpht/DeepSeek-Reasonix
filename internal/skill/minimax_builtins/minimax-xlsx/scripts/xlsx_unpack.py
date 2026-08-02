#!/usr/bin/env python3
"""xlsx_unpack.py — Unpack an XLSX file (ZIP) to a directory for XML editing.

Unzips the XLSX, pretty-prints every XML and .rels file, and prints a
categorized inventory of key files plus warnings for high-risk content.

Usage:
    python3 xlsx_unpack.py input.xlsx /tmp/xlsx_work/
    python3 xlsx_unpack.py input.xlsx /tmp/xlsx_work/ --no-pretty
"""

import argparse
import os
import shutil
import sys
import zipfile
import xml.etree.ElementTree as ET
from pathlib import Path


def pretty_print_xml(xml_bytes):
    """Parse XML bytes and return pretty-printed XML string."""
    try:
        root = ET.fromstring(xml_bytes)
        ET.indent(root, space="  ")
        return ET.tostring(root, encoding="unicode", xml_declaration=True)
    except ET.ParseError:
        # Return as-is if parsing fails
        try:
            return xml_bytes.decode("utf-8")
        except UnicodeDecodeError:
            return xml_bytes.decode("latin-1")


def is_xml_file(filename):
    """Check if a filename is an XML or .rels file."""
    lower = filename.lower()
    return lower.endswith(".xml") or lower.endswith(".rels")


def categorize_file(filename):
    """Categorize a file in the XLSX archive for inventory reporting."""
    categories = {
        "workbook": ["xl/workbook.xml"],
        "relationships": [],
        "styles": ["xl/styles.xml"],
        "shared_strings": ["xl/sharedStrings.xml"],
        "worksheets": [],
        "charts": [],
        "pivot_tables": [],
        "pivot_caches": [],
        "tables": [],
        "vba": ["xl/vbaProject.bin"],
        "theme": [],
        "external_links": [],
        "content_types": ["[Content_Types].xml"],
        "root_rels": ["_rels/.rels"],
    }

    if filename.startswith("xl/worksheets/"):
        categories["worksheets"].append(filename)
    elif filename.startswith("xl/_rels/"):
        categories["relationships"].append(filename)
    elif filename.startswith("xl/charts/"):
        categories["charts"].append(filename)
    elif filename.startswith("xl/pivotTables/"):
        categories["pivot_tables"].append(filename)
    elif filename.startswith("xl/pivotCaches/"):
        categories["pivot_caches"].append(filename)
    elif filename.startswith("xl/tables/"):
        categories["tables"].append(filename)
    elif filename.startswith("xl/theme/"):
        categories["theme"].append(filename)
    elif filename.startswith("xl/externalLinks/"):
        categories["external_links"].append(filename)

    for cat, files in categories.items():
        if filename in files:
            return cat
    return "other"


def unpack_xlsx(xlsx_path, output_dir, pretty_print=True):
    """Unpack an XLSX file to a directory.

    Args:
        xlsx_path: Path to the input XLSX file.
        output_dir: Path to the output directory.
        pretty_print: Whether to pretty-print XML files.

    Returns:
        dict with inventory information.
    """
    xlsx_path = str(xlsx_path)
    output_dir = str(output_dir)

    if not os.path.isfile(xlsx_path):
        raise FileNotFoundError(f"Input file not found: {xlsx_path}")

    # Create output directory
    os.makedirs(output_dir, exist_ok=True)

    inventory = {
        "xlsx_file": xlsx_path,
        "output_dir": output_dir,
        "files": [],
        "warnings": [],
        "categories": {},
    }

    with zipfile.ZipFile(xlsx_path, "r") as zf:
        for entry in zf.namelist():
            # Skip directory entries
            if entry.endswith("/"):
                continue

            # Read content
            content = zf.read(entry)

            # Determine output path
            out_path = os.path.join(output_dir, entry)
            out_parent = os.path.dirname(out_path)
            os.makedirs(out_parent, exist_ok=True)

            # Pretty-print XML files
            if pretty_print and is_xml_file(entry):
                try:
                    pretty = pretty_print_xml(content)
                    with open(out_path, "w", encoding="utf-8") as f:
                        f.write(pretty)
                except Exception:
                    # Fall back to raw write
                    with open(out_path, "wb") as f:
                        f.write(content)
            else:
                with open(out_path, "wb") as f:
                    f.write(content)

            # Categorize
            cat = categorize_file(entry)
            inventory["categories"].setdefault(cat, []).append(entry)
            inventory["files"].append(entry)

    # Warnings for high-risk content
    if "vba" in inventory["categories"]:
        inventory["warnings"].append(
            "VBA macros detected (xl/vbaProject.bin) — DO NOT modify this binary file"
        )
    if "pivot_tables" in inventory["categories"]:
        inventory["warnings"].append(
            "Pivot tables detected — editing pivotCacheDefinition requires extreme care"
        )
    if "charts" in inventory["categories"]:
        inventory["warnings"].append(
            "Charts detected — chart data source ranges may need updating after row shifts"
        )
    if "external_links" in inventory["categories"]:
        inventory["warnings"].append(
            "External links detected — do not modify binary .bin files in xl/externalLinks/"
        )

    return inventory


def print_inventory(inventory):
    """Print a human-readable inventory of the unpacked XLSX."""
    print(f"Unpacked: {inventory['xlsx_file']}")
    print(f"Output dir: {inventory['output_dir']}")
    print(f"Total files: {len(inventory['files'])}")
    print()

    # Print categorized files
    label_order = [
        ("content_types", "[Content_Types].xml"),
        ("root_rels", "Root relationships"),
        ("workbook", "Workbook definition"),
        ("relationships", "Relationships (.rels)"),
        ("styles", "Styles"),
        ("shared_strings", "Shared strings"),
        ("worksheets", "Worksheets"),
        ("tables", "Tables"),
        ("charts", "Charts"),
        ("pivot_tables", "Pivot tables"),
        ("pivot_caches", "Pivot caches"),
        ("theme", "Theme"),
        ("vba", "VBA macros"),
        ("external_links", "External links"),
        ("other", "Other files"),
    ]

    for cat_key, label in label_order:
        files = inventory["categories"].get(cat_key, [])
        if files:
            print(f"  {label}:")
            for f in files:
                print(f"    - {f}")

    # Print warnings
    if inventory["warnings"]:
        print()
        print("Warnings:")
        for w in inventory["warnings"]:
            print(f"  ! {w}")


def main():
    parser = argparse.ArgumentParser(
        description="Unpack an XLSX file (ZIP) to a directory for XML editing. "
        "Pretty-prints all XML and .rels files by default."
    )
    parser.add_argument("input", help="Path to the input XLSX file")
    parser.add_argument("output_dir", help="Path to the output directory")
    parser.add_argument("--no-pretty", action="store_true",
                        help="Do not pretty-print XML files")
    args = parser.parse_args()

    try:
        inventory = unpack_xlsx(args.input, args.output_dir, pretty_print=not args.no_pretty)
        print_inventory(inventory)
    except FileNotFoundError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)
    except zipfile.BadZipFile:
        print(f"ERROR: Not a valid ZIP/XLSX file: {args.input}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
