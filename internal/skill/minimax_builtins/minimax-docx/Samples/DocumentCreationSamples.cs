// DocumentCreationSamples.cs — Document lifecycle, settings, properties, page setup, multi-section
// Requires: DocumentFormat.OpenXml 3.2.0
// These samples demonstrate creating, opening, saving DOCX files with proper
// OpenXML SDK patterns including page setup and multi-section documents.

using System;
using System.IO;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class DocumentCreationSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Create a minimal document from scratch
    // ══════════════════════════════════════════════════════════════════════════
    public static void CreateMinimalDocument(string outputPath)
    {
        // WordprocessingDocument.Create opens a writable package.
        // Always use 'using' to dispose the package (flushes and closes the ZIP).
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);

        // Every DOCX needs a MainDocumentPart — it holds document.xml.
        var mainPart = doc.AddMainDocumentPart();

        // The Document root must contain a Body. An empty body is valid.
        mainPart.Document = new Document(new Body());

        // Add a paragraph with a run and text.
        var body = mainPart.Document.Body!;
        body.AppendChild(new Paragraph(
            new Run(
                new Text("Hello, DOCX!")
                { Space = SpaceProcessingModeValues.Preserve } // Preserve whitespace
            )
        ));
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Open an existing document for editing
    // ══════════════════════════════════════════════════════════════════════════
    public static void OpenAndModifyDocument(string inputPath, string outputPath)
    {
        // Open read-write. Use FileAccess.ReadWrite for editing.
        using var doc = WordprocessingDocument.Open(inputPath, true);

        var body = doc.MainDocumentPart!.Document.Body!;

        // Append a new paragraph at the end.
        body.AppendChild(new Paragraph(
            new Run(new Text("Appended text."))
        ));

        // Save explicitly (though Dispose also saves for writable opens).
        doc.MainDocumentPart.Document.Save();

        // If you need a separate output copy, save to a new file:
        // Open from stream or copy the file first, then open the copy.
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. Create from a MemoryStream (useful for in-memory processing)
    // ══════════════════════════════════════════════════════════════════════════
    public static byte[] CreateInMemory()
    {
        using var ms = new MemoryStream();
        using (var doc = WordprocessingDocument.Create(ms, WordprocessingDocumentType.Document))
        {
            var mainPart = doc.AddMainDocumentPart();
            mainPart.Document = new Document(new Body(
                new Paragraph(new Run(new Text("Created in memory.")))
            ));
            // The stream is written on Dispose.
        }
        return ms.ToArray();
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. Add document defaults (DocDefaults in styles.xml)
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddDocDefaults(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        mainPart.Document = new Document(new Body());

        // Create styles part if it doesn't exist.
        var stylesPart = mainPart.StyleDefinitionsPart;
        if (stylesPart == null)
        {
            stylesPart = mainPart.AddNewPart<StyleDefinitionsPart>();
            stylesPart.Styles = new Styles();
        }

        var styles = stylesPart.Styles!;

        // DocDefaults sets the baseline for all text in the document.
        // Order matters: DocDefaults must come before LatentStyles and Style elements.
        styles.InsertAt(new DocDefaults(
            // Default run properties: font 11pt Calibri
            new RunPropertiesDefault(
                new RunProperties(
                    new RunFonts { Ascii = "Calibri", HighAnsi = "Calibri", EastAsia = "宋体", ComplexScript = "Times New Roman" },
                    new FontSize { Val = "22" },         // 11pt × 2 = 22 half-points
                    new FontSizeComplexScript { Val = "22" }
                )
            ),
            // Default paragraph properties: single spacing
            new ParagraphPropertiesDefault(
                new ParagraphProperties(
                    new Spacing { Line = "240", LineRule = LineSpacingRuleValues.Auto }
                    // 240 = single spacing (12pt × 20 twips/pt for auto)
                )
            )
        ), 0);

        styles.Save();
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. Set document properties (core.xml)
    // ══════════════════════════════════════════════════════════════════════════
    public static void SetDocumentProperties(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        mainPart.Document = new Document(new Body());

        // Core properties (author, title, etc.)
        var corePart = mainPart.PackageProperties;
        corePart.Creator = "MiniMax AI";
        corePart.Title = "Sample Document";
        corePart.Description = "Created with OpenXML SDK";
        corePart.Keywords = "sample; docx; openxml";
        corePart.Category = "Documentation";
        corePart.Subject = "OpenXML Samples";
        corePart.Created = DateTime.UtcNow;
        corePart.Modified = DateTime.UtcNow;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 6. Page setup: margins, orientation, page size
    // ══════════════════════════════════════════════════════════════════════════
    public static void SetPageSetup(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        // Add a paragraph first (sectPr is the LAST child of body).
        body.AppendChild(new Paragraph(new Run(new Text("A4 page with standard margins."))));

        // Section properties — MUST be the last child of w:body.
        // Units: DXA (twips). 1 inch = 1440, 1 cm ≈ 567.
        var sectPr = new SectionProperties(
            // Page size: A4 (210mm × 297mm)
            new PageSize { Width = 11906, Height = 16838 },  // A4 in twips
            // Page margins (1 inch all around)
            new PageMargin
            {
                Top = 1440,     // 1 inch
                Bottom = 1440,
                Left = 1440,
                Right = 1440,
                Header = 720,   // 0.5 inch from top
                Footer = 720,   // 0.5 inch from bottom
                Gutter = 0
            },
            // Columns (single column)
            new Columns { Space = 702 },
            // Document grid (for CJK)
            new DocumentGrid { Type = DocumentGridValues.LinesAndChars, LinePitch = 312 }
        );

        body.AppendChild(sectPr);
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 7. Landscape orientation
    // ══════════════════════════════════════════════════════════════════════════
    public static void SetLandscapeOrientation(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        body.AppendChild(new Paragraph(new Run(new Text("Landscape A4 page."))));

        // For landscape, swap width and height AND set Orient.
        var sectPr = new SectionProperties(
            new PageSize
            {
                Width = 16838,  // A4 long edge (297mm)
                Height = 11906, // A4 short edge (210mm)
                Orient = PageOrientationValues.Landscape
            },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 }
        );

        body.AppendChild(sectPr);
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 8. Multi-section document with section breaks
    // ══════════════════════════════════════════════════════════════════════════
    public static void CreateMultiSectionDocument(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        // ── Section 1: Portrait A4 with standard margins ─────────────────────
        var para1 = new Paragraph(
            new ParagraphProperties(
                new ParagraphStyleId { Val = "Heading1" }
            ),
            new Run(new Text("Section 1: Portrait"))
        );
        body.AppendChild(para1);
        body.AppendChild(new Paragraph(new Run(new Text("This is the first section."))));

        // ── Section break: Next Page (start section 2 on a new page) ────────
        // Section break goes in the pPr of the LAST paragraph of the section.
        // This is the correct pattern — NOT a separate empty paragraph.
        var lastParaSection1 = body.AppendChild(new Paragraph(new Run(new Text("End of section 1."))));
        lastParaSection1.ParagraphProperties ??= new ParagraphProperties();
        lastParaSection1.ParagraphProperties.AppendChild(new SectionProperties(
            new PageSize { Width = 11906, Height = 16838 },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 }
        ));

        // ── Section 2: Landscape A4 ─────────────────────────────────────────
        body.AppendChild(new Paragraph(
            new ParagraphProperties(new ParagraphStyleId { Val = "Heading1" }),
            new Run(new Text("Section 2: Landscape"))
        ));
        body.AppendChild(new Paragraph(new Run(new Text("Wide content goes here."))));

        // ── Section break: Continuous (no page break) ───────────────────────
        var lastParaSection2 = body.AppendChild(new Paragraph(new Run(new Text("End of section 2."))));
        lastParaSection2.ParagraphProperties ??= new ParagraphProperties();
        lastParaSection2.ParagraphProperties.AppendChild(new SectionProperties(
            new PageSize { Width = 16838, Height = 11906, Orient = PageOrientationValues.Landscape },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 },
            new SectionType { Val = SectionMarkValues.Continuous }
        ));

        // ── Section 3: Two columns ──────────────────────────────────────────
        body.AppendChild(new Paragraph(
            new ParagraphProperties(new ParagraphStyleId { Val = "Heading1" }),
            new Run(new Text("Section 3: Two Columns"))
        ));
        body.AppendChild(new Paragraph(new Run(new Text("Column content."))));

        // ── Final section properties (always in body, not in pPr) ───────────
        // The LAST section's sectPr goes as a direct child of body.
        body.AppendChild(new SectionProperties(
            new PageSize { Width = 11906, Height = 16838 },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 },
            new Columns { Space = 702, ColumnCount = 2 }
        ));
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 9. US Letter page setup (8.5" × 11")
    // ══════════════════════════════════════════════════════════════════════════
    public static void SetUsLetterSetup(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        body.AppendChild(new Paragraph(new Run(new Text("US Letter page."))));

        // US Letter: 8.5" × 11" = 12240 × 15840 twips
        body.AppendChild(new SectionProperties(
            new PageSize { Width = 12240, Height = 15840 },
            new PageMargin
            {
                Top = 1440, Bottom = 1440, Left = 1440, Right = 1440,
                Header = 720, Footer = 720
            }
        ));
    }
}
