#!/usr/bin/env bash
# doc_to_docx.sh — Convert .doc to .docx using LibreOffice headless mode
# Usage: bash doc_to_docx.sh input.doc output_dir/
# Falls back to suggesting manual conversion if LibreOffice unavailable

set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
    echo "Usage: bash doc_to_docx.sh input.doc [output_dir/]"
    echo "  Converts .doc to .docx using LibreOffice headless mode."
    exit 1
fi

INPUT_DOC="$(realpath "$1")"
OUTPUT_DIR="${2:-.}"

# ── Validate input file ──────────────────────────────────────────────────────
if [ ! -f "$INPUT_DOC" ]; then
    echo "ERROR: Input file not found: $INPUT_DOC" >&2
    exit 1
fi

INPUT_EXT="${INPUT_DOC##*.}"
if [ "${INPUT_EXT,,}" != "doc" ]; then
    echo "WARNING: Input file does not have .doc extension. Attempting conversion anyway."
fi

# ── Validate/create output directory ──────────────────────────────────────────
mkdir -p "$OUTPUT_DIR"
OUTPUT_DIR="$(realpath "$OUTPUT_DIR")"

# ── Find LibreOffice ─────────────────────────────────────────────────────────
LO_CMD=""
if command -v libreoffice &>/dev/null; then
    LO_CMD="libreoffice"
elif command -v soffice &>/dev/null; then
    LO_CMD="soffice"
fi

if [ -z "$LO_CMD" ]; then
    echo "ERROR: LibreOffice not found. Cannot convert .doc to .docx automatically." >&2
    echo "" >&2
    echo "To convert manually, use one of these methods:" >&2
    echo "  1. Install LibreOffice: https://libreoffice.org" >&2
    echo "  2. Open in Microsoft Word and Save As .docx" >&2
    echo "  3. Use an online converter (e.g., cloudconvert.com)" >&2
    echo "  4. On macOS: brew install --cask libreoffice" >&2
    echo "  5. On Ubuntu/Debian: sudo apt install libreoffice-writer" >&2
    exit 2
fi

# ── Perform conversion ───────────────────────────────────────────────────────
echo "Converting: $INPUT_DOC → .docx"
echo "Output directory: $OUTPUT_DIR"
echo "Using: $LO_CMD"

# LibreOffice --headless --convert-to docx --outdir <dir> <input>
# Note: LibreOffice uses the input file's directory as a working reference,
# so we run from the output directory to place the result there.
RESULT=$("$LO_CMD" --headless --convert-to docx --outdir "$OUTPUT_DIR" "$INPUT_DOC" 2>&1) || {
    echo "ERROR: LibreOffice conversion failed:" >&2
    echo "$RESULT" >&2
    exit 3
}

# ── Verify output ─────────────────────────────────────────────────────────────
INPUT_BASE="$(basename "${INPUT_DOC%.*}")"
OUTPUT_DOCX="$OUTPUT_DIR/${INPUT_BASE}.docx"

if [ -f "$OUTPUT_DOCX" ]; then
    FILESIZE="$(stat -f%z "$OUTPUT_DOCX" 2>/dev/null || stat -c%s "$OUTPUT_DOCX" 2>/dev/null || echo "unknown")"
    echo "SUCCESS: $OUTPUT_DOCX ($FILESIZE bytes)"
else
    echo "WARNING: Expected output file not found at $OUTPUT_DOCX"
    echo "LibreOffice output:"
    echo "$RESULT"
    exit 4
fi
