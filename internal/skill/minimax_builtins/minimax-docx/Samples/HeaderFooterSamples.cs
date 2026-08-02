// HeaderFooterSamples.cs — Headers/footers: page numbers, "Page X of Y", first/even/odd,
// logo image, table layout, 公文 "-X-" style page numbers, per-section
// Requires: DocumentFormat.OpenXml 3.2.0

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class HeaderFooterSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Simple page number in footer
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddSimplePageNumber(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        // Create footer part
        var footerPart = mainPart.AddNewPart<FooterPart>();
        var footer = new Footer(
            new Paragraph(
                new ParagraphProperties(
                    new Justification { Val = JustificationValues.Center }
                ),
                new Run(
                    // PAGE field: current page number
                    new FieldCode { Text = " PAGE " }
                )
            )
        );
        footerPart.Footer = footer;

        // Reference the footer in section properties
        var footerReference = new FooterReference
        {
            Type = HeaderFooterValues.Default,
            Id = mainPart.GetIdOfPart(footerPart)
        };

        body.AppendChild(new Paragraph(new Run(new Text("Document with page numbers."))));
        body.AppendChild(new SectionProperties(
            footerReference,
            new PageSize { Width = 11906, Height = 16838 },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 }
        ));
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. "Page X of Y" in footer
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddPageXOfY(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        var footerPart = mainPart.AddNewPart<FooterPart>();
        var footer = new Footer(
            new Paragraph(
                new ParagraphProperties(
                    new Justification { Val = JustificationValues.Center }
                ),
                new Run(new Text("Page ")),
                new Run(new FieldCode { Text = " PAGE " }),
                new Run(new Text(" of ")),
                new Run(new FieldCode { Text = " NUMPAGES " })
            )
        );
        footerPart.Footer = footer;

        var footerRef = new FooterReference
        {
            Type = HeaderFooterValues.Default,
            Id = mainPart.GetIdOfPart(footerPart)
        };

        body.AppendChild(new Paragraph(new Run(new Text("Page X of Y footer."))));
        body.AppendChild(new SectionProperties(
            footerRef,
            new PageSize { Width = 11906, Height = 16838 },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 }
        ));
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. First page / even / odd page headers
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddDifferentFirstPageHeaders(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        // First page header (e.g., title page)
        var firstHeaderPart = mainPart.AddNewPart<HeaderPart>();
        firstHeaderPart.Header = new Header(
            new Paragraph(new Run(new Text("First Page Header")))
        );

        // Default header (subsequent pages)
        var defaultHeaderPart = mainPart.AddNewPart<HeaderPart>();
        defaultHeaderPart.Header = new Header(
            new Paragraph(new Run(new Text("Running Header")))
        );

        // Default footer with page number
        var footerPart = mainPart.AddNewPart<FooterPart>();
        footerPart.Footer = new Footer(
            new Paragraph(
                new ParagraphProperties(new Justification { Val = JustificationValues.Center }),
                new Run(new FieldCode { Text = " PAGE " })
            )
        );

        body.AppendChild(new Paragraph(new Run(new Text("First page content."))));
        body.AppendChild(new SectionProperties(
            new HeaderReference { Type = HeaderFooterValues.First, Id = mainPart.GetIdOfPart(firstHeaderPart) },
            new HeaderReference { Type = HeaderFooterValues.Default, Id = mainPart.GetIdOfPart(defaultHeaderPart) },
            new FooterReference { Type = HeaderFooterValues.Default, Id = mainPart.GetIdOfPart(footerPart) },
            new TitlePage(),  // Required: signals that first page is different
            new PageSize { Width = 11906, Height = 16838 },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 }
        ));
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. Header with text and right-aligned page number (tab stop)
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddHeaderWithRightPageNumber(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        var headerPart = mainPart.AddNewPart<HeaderPart>();
        headerPart.Header = new Header(
            new Paragraph(
                new ParagraphProperties(
                    // Right tab at the right margin
                    new Tabs(new TabStop { Val = TabStopValues.Right, Position = 9072 }),  // 6.3" for A4
                    new Spacing { After = "0", Line = "240", LineRule = LineSpacingRuleValues.Auto }
                ),
                new Run(new Text("Document Title")),
                new Run(new TabChar()),
                new Run(new FieldCode { Text = " PAGE " })
            )
        );

        body.AppendChild(new Paragraph(new Run(new Text("Content."))));
        body.AppendChild(new SectionProperties(
            new HeaderReference { Type = HeaderFooterValues.Default, Id = mainPart.GetIdOfPart(headerPart) },
            new PageSize { Width = 11906, Height = 16838 },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 }
        ));
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. 公文 "-X-" style page numbers (GB/T 9704)
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddChineseGovtPageNumbers(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        // 公文 footer: — X — format, centered, 四号 (14pt) 仿宋
        var footerPart = mainPart.AddNewPart<FooterPart>();
        footerPart.Footer = new Footer(
            new Paragraph(
                new ParagraphProperties(
                    new Justification { Val = JustificationValues.Center },
                    new Spacing { Before = "0", After = "0", Line = "312", LineRule = LineSpacingRuleValues.Exact }
                ),
                new Run(
                    new RunProperties(
                        new RunFonts { Ascii = "FangSong", HighAnsi = "FangSong", EastAsia = "仿宋" },
                        new FontSize { Val = "28" }  // 四号 = 14pt × 2 = 28
                    ),
                    new Text("— ")
                    { Space = SpaceProcessingModeValues.Preserve }
                ),
                new Run(
                    new RunProperties(
                        new RunFonts { Ascii = "FangSong", HighAnsi = "FangSong", EastAsia = "仿宋" },
                        new FontSize { Val = "28" }
                    ),
                    new FieldCode { Text = " PAGE " }
                ),
                new Run(
                    new RunProperties(
                        new RunFonts { Ascii = "FangSong", HighAnsi = "FangSong", EastAsia = "仿宋" },
                        new FontSize { Val = "28" }
                    ),
                    new Text(" —")
                    { Space = SpaceProcessingModeValues.Preserve }
                )
            )
        );

        body.AppendChild(new Paragraph(new Run(new Text("公文 with —X— page numbers."))));
        body.AppendChild(new SectionProperties(
            new FooterReference { Type = HeaderFooterValues.Default, Id = mainPart.GetIdOfPart(footerPart) },
            new PageSize { Width = 11906, Height = 16838 },
            new PageMargin { Top = 2268, Bottom = 1701, Left = 1701, Right = 1531 }
            // GB/T 9704 margins: top 37mm, bottom 35mm, left 28mm, right 26mm
        ));
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 6. Per-section headers/footers (different header for each section)
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddPerSectionHeaders(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        // ── Section 1 header ────────────────────────────────────────────────
        var header1Part = mainPart.AddNewPart<HeaderPart>();
        header1Part.Header = new Header(
            new Paragraph(new Run(new Text("Section 1: Introduction")))
        );
        var footer1Part = mainPart.AddNewPart<FooterPart>();
        footer1Part.Footer = new Footer(
            new Paragraph(
                new ParagraphProperties(new Justification { Val = JustificationValues.Center }),
                new Run(new FieldCode { Text = " PAGE " })
            )
        );

        // Section 1 content
        var lastPara1 = body.AppendChild(new Paragraph(new Run(new Text("Content of section 1."))));
        lastPara1.ParagraphProperties ??= new ParagraphProperties();
        lastPara1.ParagraphProperties.AppendChild(new SectionProperties(
            new HeaderReference { Type = HeaderFooterValues.Default, Id = mainPart.GetIdOfPart(header1Part) },
            new FooterReference { Type = HeaderFooterValues.Default, Id = mainPart.GetIdOfPart(footer1Part) },
            new PageSize { Width = 11906, Height = 16838 },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 }
        ));

        // ── Section 2 header (different text) ──────────────────────────────
        var header2Part = mainPart.AddNewPart<HeaderPart>();
        header2Part.Header = new Header(
            new Paragraph(new Run(new Text("Section 2: Analysis")))
        );
        var footer2Part = mainPart.AddNewPart<FooterPart>();
        footer2Part.Footer = new Footer(
            new Paragraph(
                new ParagraphProperties(new Justification { Val = JustificationValues.Center }),
                new Run(new FieldCode { Text = " PAGE " })
            )
        );

        // Section 2 content
        body.AppendChild(new Paragraph(new Run(new Text("Content of section 2."))));

        // Final sectPr (body-level)
        body.AppendChild(new SectionProperties(
            new HeaderReference { Type = HeaderFooterValues.Default, Id = mainPart.GetIdOfPart(header2Part) },
            new FooterReference { Type = HeaderFooterValues.Default, Id = mainPart.GetIdOfPart(footer2Part) },
            new PageSize { Width = 11906, Height = 16838 },
            new PageMargin { Top = 1440, Bottom = 1440, Left = 1440, Right = 1440 }
        ));
    }
}
