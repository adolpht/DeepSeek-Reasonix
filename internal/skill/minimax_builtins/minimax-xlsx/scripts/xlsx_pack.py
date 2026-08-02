#!/usr/bin/env python3
"""xlsx_pack.py — Repack a directory of XML files back into an XLSX file.

Validates XML well-formedness of every .xml and .rels file before creating
the ZIP archive. Uses correct compression (deflate for XML, stored for binary).

Usage:
    python3 xlsx_pack.py /tmp/xlsx_work/ output.xlsx
    python3 xlsx_pack.py /tmp/xlsx_work/ output.xlsx --no-validate
"""

import argparse
import os
import sys
import zipfile
import xml.etree.ElementTree as ET
from pathlib import Path


# Files that should use ZIP_STORED (no compression) per OOXML spec
STORED_EXTENSIONS = {".bin", ".emf", ".wmf"}

# Required files for a valid XLSX
REQUIRED_FILES = ["[Content_Types].xml"]


def validate_xml_file(filepath):
    """Validate an XML file is well-formed.

    Args:
        filepath: Path to the XML file.

    Returns:
        (ok, error_message) tuple.
    """
    try:
        tree = ET.parse(filepath)
        return True, None
    except ET.ParseError as e:
        return False, f"XML parse error in {filepath}: {e}"
    except Exception as e:
        return False, f"Error reading {filepath}: {e}"


def collect_files(work_dir):
    """Collect all files in the work directory, preserving relative paths.

    Args:
        work_dir: Path to the unpacked XLSX directory.

    Returns:
        List of (relative_path, absolute_path) tuples.
    """
    work_dir = str(work_dir)
    files = []
    for root, dirs, filenames in os.walk(work_dir):
        # Sort for deterministic ordering
        dirs.sort()
        filenames.sort()
        for filename in filenames:
            abs_path = os.path.join(root, filename)
            rel_path = os.path.relpath(abs_path, work_dir)
            # Normalize to forward slashes (ZIP spec)
            rel_path = rel_path.replace(os.sep, "/")
            files.append((rel_path, abs_path))
    return files


def pack_xlsx(work_dir, output_path, validate=True):
    """Pack a directory of XML files into an XLSX file.

    Args:
        work_dir: Path to the unpacked XLSX directory.
        output_path: Path for the output XLSX file.
        validate: Whether to validate XML well-formedness.

    Returns:
        dict with results.
    """
    work_dir = str(work_dir)
    output_path = str(output_path)

    if not os.path.isdir(work_dir):
        raise FileNotFoundError(f"Work directory not found: {work_dir}")

    result = {
        "work_dir": work_dir,
        "output_path": output_path,
        "files_packed": 0,
        "validation_errors": [],
        "success": False,
    }

    # Collect files
    files = collect_files(work_dir)

    if not files:
        raise ValueError(f"No files found in {work_dir}")

    # Check for required files
    rel_paths = [f[0] for f in files]
    for req in REQUIRED_FILES:
        if req not in rel_paths:
            result["validation_errors"].append(
                f"Missing required file: {req}"
            )

    # Validate XML files
    if validate:
        xml_errors = []
        for rel_path, abs_path in files:
            lower = rel_path.lower()
            if lower.endswith(".xml") or lower.endswith(".rels"):
                ok, error = validate_xml_file(abs_path)
                if not ok:
                    xml_errors.append(error)

        if xml_errors:
            result["validation_errors"].extend(xml_errors)
            return result

    # Ensure output directory exists
    output_parent = os.path.dirname(output_path)
    if output_parent:
        os.makedirs(output_parent, exist_ok=True)

    # Create the ZIP file
    # XLSX files should have entries in a specific order:
    # [Content_Types].xml first, then _rels/.rels, then the rest
    def sort_key(item):
        rel = item[0]
        if rel == "[Content_Types].xml":
            return (0, rel)
        elif rel == "_rels/.rels":
            return (1, rel)
        else:
            return (2, rel)

    sorted_files = sorted(files, key=sort_key)

    with zipfile.ZipFile(output_path, "w", zipfile.ZIP_DEFLATED) as zf:
        for rel_path, abs_path in sorted_files:
            # Determine compression
            _, ext = os.path.splitext(rel_path.lower())
            compress_type = zipfile.ZIP_STORED if ext in STORED_EXTENSIONS else zipfile.ZIP_DEFLATED

            # Read and add file
            with open(abs_path, "rb") as f:
                data = f.read()

            # Determine last modified time — use fixed time for reproducibility
            # but preserve actual file time for correctness
            info = zipfile.ZipInfo.from_file(abs_path, rel_path)
            info.compress_type = compress_type
            zf.writestr(info, data)

            result["files_packed"] += 1

    result["success"] = True
    return result


def main():
    parser = argparse.ArgumentParser(
        description="Repack a directory of XML files into an XLSX file. "
        "Validates XML well-formedness before packing."
    )
    parser.add_argument("work_dir", help="Path to the unpacked XLSX directory")
    parser.add_argument("output", help="Path for the output XLSX file")
    parser.add_argument("--no-validate", action="store_true",
                        help="Skip XML validation before packing")
    args = parser.parse_args()

    try:
        result = pack_xlsx(args.work_dir, args.output, validate=not args.no_validate)

        if result["validation_errors"]:
            print("VALIDATION ERRORS — fix before packing:", file=sys.stderr)
            for error in result["validation_errors"]:
                print(f"  ! {error}", file=sys.stderr)
            sys.exit(1)

        print(f"Packed {result['files_packed']} files -> {result['output_path']}")

    except FileNotFoundError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)
    except ValueError as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)
    except Exception as e:
        print(f"ERROR: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
