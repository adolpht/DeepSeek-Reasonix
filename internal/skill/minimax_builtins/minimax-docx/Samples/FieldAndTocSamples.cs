// FieldAndTocSamples.cs — Fields: TOC, SimpleField vs complex field,
// DATE/PAGE/REF/SEQ/MERGEFIELD/IF/STYLEREF, TOC styles
// Requires: DocumentFormat.OpenXml 3.2.0

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class FieldAndTocSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Insert a Table of Contents (TOC)
    // ══════════════════════════════════════════════════════════════════════════
    // The TOC is a field code that Word updates on open or via Ctrl+A, F9.
    public static Paragraph CreateTocParagraph()
    {
        // SimpleField approach — creates a w:fldSimple element
        // For a TOC that updates properly, use the complex field approach below.
        var para = new Paragraph(
            new Run(
                new FieldCode { Text = " TOC \\o \"1-3\" \\h \\z \\u " }
                // \o "1-3"  → include outline levels 1–3
                // \h       → hyperlinks
                // \z       → hide page numbers in Web view
                // \u       → use paragraph outline level
            )
        );

        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Complex field (proper begin/separate/end structure)
    // ══════════════════════════════════════════════════════════════════════════
    // Complex fields use three runs: fldChar begin, field code, fldChar separate,
    // result text, fldChar end. This is the correct pattern for all fields.
    public static Paragraph CreateComplexFieldParagraph(string fieldCode, string resultText)
    {
        var para = new Paragraph();
        para.AppendChild(new Run(
            new FieldChar { FieldCharType = FieldCharValues.Begin }
        ));
        para.AppendChild(new Run(
            new FieldCode { Text = $" {fieldCode} " }
        ));
        para.AppendChild(new Run(
            new FieldChar { FieldCharType = FieldCharValues.Separate }
        ));
        para.AppendChild(new Run(
            new Text(resultText)
        ));
        para.AppendChild(new Run(
            new FieldChar { FieldCharType = FieldCharValues.End }
        ));
        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. DATE field (current date)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateDateFieldParagraph()
    {
        return CreateComplexFieldParagraph(
            @"DATE \@ ""yyyy-MM-dd"" \h",
            DateTime.Now.ToString("yyyy-MM-dd")
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. PAGE field (current page number)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreatePageFieldParagraph()
    {
        return CreateComplexFieldParagraph("PAGE", "1");
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. REF field (cross-reference to a bookmark)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateRefFieldParagraph(string bookmarkName)
    {
        return CreateComplexFieldParagraph(
            $"REF {bookmarkName} \\h",
            $"[Reference to {bookmarkName}]"
        );
        // \h → hyperlink to the bookmark
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 6. SEQ field (sequential numbering — for figures, tables, equations)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateFigureCaptionWithSeq()
    {
        var para = new Paragraph(
            new ParagraphProperties(
                new Justification { Val = JustificationValues.Center }
            ),
            new Run(new Text("Figure ")),
            new Run(new FieldCode { Text = " SEQ Figure \\* ARABIC " }),
            new Run(new Text(": Description of the figure."))
        );
        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 7. MERGEFIELD (mail merge placeholder)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateMergeFieldParagraph(string fieldName)
    {
        return CreateComplexFieldParagraph(
            $"MERGEFIELD {fieldName}",
            $"«{fieldName}»"
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 8. IF field (conditional field)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateIfFieldParagraph(string mergeField, string compareValue,
        string trueText, string falseText)
    {
        return CreateComplexFieldParagraph(
            $"IF «{mergeField}» = \"{compareValue}\" \"{trueText}\" \"{falseText}\"",
            falseText  // Default to false text as placeholder
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 9. STYLEREF field (text from a heading style — for running headers)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateStyleRefParagraph(string styleId)
    {
        return CreateComplexFieldParagraph(
            $"STYLEREF \"{styleId}\"",
            $"[{styleId} text]"
        );
    }

    // Common pattern: running header with STYLEREF for current heading
    public static Paragraph CreateRunningHeaderParagraph()
    {
        var para = new Paragraph();
        // Left: chapter title from Heading 1
        para.AppendChild(new Run(new FieldCode { Text = " STYLEREF \"Heading1\" " }));
        para.AppendChild(new Run(new TabChar()));
        // Right: page number
        para.AppendChild(new Run(new FieldCode { Text = " PAGE " }));
        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 10. TOC styles (configure TOC 1–3 styles)
    // ══════════════════════════════════════════════════════════════════════════
    public static void DefineTocStyles(Styles styles)
    {
        // TOC 1: top-level entries
        styles.AppendChild(new Style(
            new StyleName { Val = "toc 1" },
            new BasedOn { Val = "Normal" },
            new ParagraphProperties(
                new Spacing { Before = "120", After = "40" },
                new Indentation { Left = "0" }
            ),
            new RunProperties(
                new FontSize { Val = "24" },  // 12pt
                new Color { Val = "1F3864" }   // Dark blue hyperlinks
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "TOC1" });

        // TOC 2: second level
        styles.AppendChild(new Style(
            new StyleName { Val = "toc 2" },
            new BasedOn { Val = "Normal" },
            new ParagraphProperties(
                new Spacing { Before = "40", After = "20" },
                new Indentation { Left = "360" }
            ),
            new RunProperties(
                new FontSize { Val = "22" }  // 11pt
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "TOC2" });

        // TOC 3: third level
        styles.AppendChild(new Style(
            new StyleName { Val = "toc 3" },
            new BasedOn { Val = "Normal" },
            new ParagraphProperties(
                new Spacing { Before = "20", After = "0" },
                new Indentation { Left = "720" }
            ),
            new RunProperties(
                new FontSize { Val = "20" }  // 10pt
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "TOC3" });
    }
}
