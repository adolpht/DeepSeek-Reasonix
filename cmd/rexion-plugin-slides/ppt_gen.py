#!/usr/bin/env python3
"""
PPT generation module using python-pptx (MIT License).
This replaces the commercial unioffice library with a free open-source alternative.
"""

import json
import sys
import os
from pathlib import Path

try:
    from pptx import Presentation
    from pptx.util import Inches, Pt
    from pptx.dml.color import RGBColor
    from pptx.enum.text import PP_ALIGN
    from pptx.enum.shapes import MSO_SHAPE
except ImportError:
    print(json.dumps({"error": "python-pptx not installed. Run: pip install python-pptx"}))
    sys.exit(1)


# Color palette
DARK_BLUE = RGBColor(0x1B, 0x2A, 0x4A)
MED_BLUE = RGBColor(0x2E, 0x4E, 0x7A)
LIGHT_BLUE = RGBColor(0x3A, 0x7B, 0xD5)
TEAL = RGBColor(0x00, 0x96, 0x88)
WHITE = RGBColor(0xFF, 0xFF, 0xFF)
LIGHT_GRAY = RGBColor(0xEC, 0xEF, 0xF1)
DARK_GRAY = RGBColor(0x33, 0x33, 0x33)
ORANGE = RGBColor(0xE6, 0x7E, 0x22)
GREEN = RGBColor(0x27, 0xAE, 0x60)


def get_theme_colors(style):
    """Get color scheme based on style."""
    themes = {
        "professional": {
            "title_color": DARK_BLUE,
            "body_color": DARK_GRAY,
            "bg_color": None,  # White background
            "accent": LIGHT_BLUE,
        },
        "creative": {
            "title_color": TEAL,
            "body_color": DARK_GRAY,
            "bg_color": None,
            "accent": ORANGE,
        },
        "minimal": {
            "title_color": DARK_GRAY,
            "body_color": RGBColor(0x66, 0x66, 0x66),
            "bg_color": None,
            "accent": LIGHT_GRAY,
        },
    }
    return themes.get(style, themes["professional"])


def parse_outline(md):
    """Parse Markdown outline into slide structures."""
    lines = md.split("\n")
    slides = []
    current = None

    for raw_line in lines:
        line = raw_line.rstrip("\r")
        trimmed = line.strip()

        if not trimmed:
            continue

        # Single # header = presentation title (skip, handled separately)
        if trimmed.startswith("#") and not trimmed.startswith("##"):
            continue

        # ## header = new slide
        if trimmed.startswith("## "):
            if current:
                slides.append(current)
            current = {"title": trimmed[3:].strip(), "content": []}
            continue

        # Content belongs to current slide
        if current:
            current["content"].append(trimmed)

    if current:
        slides.append(current)

    return slides


def create_ppt(outline, title, style, output_path):
    """Create a PowerPoint presentation from Markdown outline."""
    slides_data = parse_outline(outline)
    if not slides_data:
        return {"error": "Outline contains no slides (need at least one ## header)"}

    # Ensure output directory exists
    output_dir = Path(output_path).parent
    if output_dir and not output_dir.exists():
        output_dir.mkdir(parents=True, exist_ok=True)

    prs = Presentation()
    prs.slide_width = Inches(13.333)
    prs.slide_height = Inches(7.5)

    theme = get_theme_colors(style)

    # Title slide
    title_text = title or slides_data[0]["title"] or "Presentation"
    title_slide_layout = prs.slide_layouts[6]  # Blank layout
    slide = prs.slides.add_slide(title_slide_layout)

    # Title text box
    title_box = slide.shapes.add_textbox(Inches(1), Inches(2.5), Inches(11), Inches(1.5))
    tf = title_box.text_frame
    p = tf.paragraphs[0]
    p.alignment = PP_ALIGN.CENTER
    run = p.add_run()
    run.text = title_text
    run.font.size = Pt(36)
    run.font.bold = True
    run.font.color.rgb = theme["title_color"]

    # Subtitle
    sub_box = slide.shapes.add_textbox(Inches(2), Inches(4.2), Inches(9), Inches(0.8))
    sub_tf = sub_box.text_frame
    sub_p = sub_tf.paragraphs[0]
    sub_p.alignment = PP_ALIGN.CENTER
    sub_run = sub_p.add_run()
    sub_run.text = f"{len(slides_data)} slides"
    sub_run.font.size = Pt(18)
    sub_run.font.color.rgb = theme["body_color"]

    # Content slides
    start_idx = 0
    if not title and len(slides_data) > 1 and not slides_data[0]["content"]:
        start_idx = 1  # First slide was used as title and had no body

    for i in range(start_idx, len(slides_data)):
        s = slides_data[i]
        slide = prs.slides.add_slide(prs.slide_layouts[6])

        # Slide title
        title_box = slide.shapes.add_textbox(Inches(0.5), Inches(0.3), Inches(12), Inches(0.8))
        tf = title_box.text_frame
        p = tf.paragraphs[0]
        run = p.add_run()
        run.text = s["title"]
        run.font.size = Pt(28)
        run.font.bold = True
        run.font.color.rgb = theme["title_color"]

        # Slide body
        if s["content"]:
            body_box = slide.shapes.add_textbox(Inches(0.5), Inches(1.3), Inches(12), Inches(5.5))
            body_tf = body_box.text_frame
            body_tf.word_wrap = True

            for j, line in enumerate(s["content"]):
                if j == 0:
                    bp = body_tf.paragraphs[0]
                else:
                    bp = body_tf.paragraphs[0].add_paragraph_before() if j == 0 else body_tf.add_paragraph()

                # Detect bullet points
                clean_line = line.strip()
                is_bullet = clean_line.startswith("- ") or clean_line.startswith("* ")
                if is_bullet:
                    clean_line = clean_line[2:].strip()
                    bp.level = 0

                run = bp.add_run()
                run.text = clean_line
                run.font.size = Pt(18)
                run.font.color.rgb = theme["body_color"]

    prs.save(output_path)
    return {"success": True, "path": output_path, "slides": len(slides_data)}


def add_slide(ppt_path, slide_title, content, layout, notes):
    """Add a slide to an existing presentation."""
    if not os.path.exists(ppt_path):
        return {"error": f"PPT file not found: {ppt_path}"}

    prs = Presentation(ppt_path)

    slide = prs.slides.add_slide(prs.slide_layouts[6])

    if layout == "title":
        # Full-title slide
        title_box = slide.shapes.add_textbox(Inches(1), Inches(2.5), Inches(11), Inches(2))
        tf = title_box.text_frame
        p = tf.paragraphs[0]
        p.alignment = PP_ALIGN.CENTER
        run = p.add_run()
        run.text = slide_title
        run.font.size = Pt(40)
        run.font.bold = True

    elif layout == "title_content":
        # Title at top, content below
        title_box = slide.shapes.add_textbox(Inches(0.5), Inches(0.3), Inches(12), Inches(0.8))
        tf = title_box.text_frame
        p = tf.paragraphs[0]
        run = p.add_run()
        run.text = slide_title
        run.font.size = Pt(28)
        run.font.bold = True

        if content:
            body_box = slide.shapes.add_textbox(Inches(0.5), Inches(1.3), Inches(12), Inches(5.5))
            body_tf = body_box.text_frame
            body_tf.word_wrap = True

            lines = content.split("\n")
            for j, line in enumerate(lines):
                if j == 0:
                    bp = body_tf.paragraphs[0]
                else:
                    bp = body_tf.add_paragraph()

                clean_line = line.strip()
                is_bullet = clean_line.startswith("- ") or clean_line.startswith("* ")
                if is_bullet:
                    clean_line = clean_line[2:].strip()
                    bp.level = 0

                run = bp.add_run()
                run.text = clean_line
                run.font.size = Pt(18)

    # layout == "blank" -> nothing added

    prs.save(ppt_path)
    return {"success": True, "message": f"Added slide '{slide_title}' to {ppt_path}"}


def apply_theme(ppt_path, theme_name):
    """Apply a visual theme to an existing presentation."""
    if not os.path.exists(ppt_path):
        return {"error": f"PPT file not found: {ppt_path}"}

    valid_themes = ["professional", "creative", "minimal"]
    if theme_name not in valid_themes:
        return {"error": f"Unknown theme '{theme_name}'. Choose from: {valid_themes}"}

    prs = Presentation(ppt_path)
    theme = get_theme_colors(theme_name)

    for slide in prs.slides:
        for shape in slide.shapes:
            if shape.has_text_frame:
                for paragraph in shape.text_frame.paragraphs:
                    for run in paragraph.runs:
                        if run.font.bold:
                            run.font.color.rgb = theme["title_color"]
                        else:
                            run.font.color.rgb = theme["body_color"]

    prs.save(ppt_path)
    return {"success": True, "message": f"Applied '{theme_name}' theme to {ppt_path}"}


def export_pdf(ppt_path, output_path):
    """Export PPT to PDF (requires LibreOffice)."""
    if not os.path.exists(ppt_path):
        return {"error": f"PPT file not found: {ppt_path}"}

    if not output_path:
        output_path = ppt_path.rsplit(".", 1)[0] + ".pdf"

    # Try LibreOffice conversion
    import subprocess

    lo_paths = [
        "soffice",
        "libreoffice",
        "/usr/bin/soffice",
        "/usr/bin/libreoffice",
        "C:\\Program Files\\LibreOffice\\program\\soffice.exe",
        "C:\\Program Files (x86)\\LibreOffice\\program\\soffice.exe",
    ]

    lo_path = None
    for p in lo_paths:
        try:
            subprocess.run([p, "--version"], capture_output=True, check=True)
            lo_path = p
            break
        except:
            continue

    if not lo_path:
        return {
            "message": f"PDF export requires LibreOffice. PPT saved at: {ppt_path}",
            "pdf_available": False,
        }

    output_dir = Path(output_path).parent
    try:
        subprocess.run(
            [lo_path, "--headless", "--convert-to", "pdf", "--outdir", str(output_dir), ppt_path],
            capture_output=True,
            check=True,
        )
        return {"success": True, "pdf_path": output_path}
    except Exception as e:
        return {"error": f"LibreOffice conversion failed: {e}"}


def main():
    """Main entry point for JSON-RPC style calls."""
    if len(sys.argv) < 2:
        print(json.dumps({"error": "No action specified"}))
        sys.exit(1)

    action = sys.argv[1]
    args = json.loads(sys.argv[2]) if len(sys.argv) > 2 else {}

    result = {"error": f"Unknown action: {action}"}

    try:
        if action == "create_ppt":
            result = create_ppt(
                outline=args.get("outline", ""),
                title=args.get("title", ""),
                style=args.get("style", "professional"),
                output_path=args.get("output_path", ""),
            )
        elif action == "add_slide":
            result = add_slide(
                ppt_path=args.get("ppt_path", ""),
                slide_title=args.get("title", ""),
                content=args.get("content", ""),
                layout=args.get("layout", "title_content"),
                notes=args.get("notes", ""),
            )
        elif action == "apply_theme":
            result = apply_theme(
                ppt_path=args.get("ppt_path", ""),
                theme_name=args.get("theme", "professional"),
            )
        elif action == "export_pdf":
            result = export_pdf(
                ppt_path=args.get("ppt_path", ""),
                output_path=args.get("output_path", ""),
            )
    except Exception as e:
        result = {"error": str(e)}

    print(json.dumps(result))


if __name__ == "__main__":
    main()