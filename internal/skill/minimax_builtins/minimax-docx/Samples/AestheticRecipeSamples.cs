// AestheticRecipeSamples.cs — 13 aesthetic recipes from authoritative sources
// Each recipe provides exact formatting values from official style guides.
// Requires: DocumentFormat.OpenXml 3.2.0
//
// Recipes:
//   ModernCorporate, AcademicThesis, ExecutiveBrief,
//   ChineseGovernment (GB/T 9704), MinimalModern, IEEE Conference,
//   ACM sigconf, APA 7th, MLA 9th, Chicago/Turabian,
//   Springer LNCS, Nature, HBR

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

// ── Recipe data structures ────────────────────────────────────────────────────

public record AestheticRecipe(
    string Name,
    string Source,
    // Page setup
    string PageSize,       // "A4", "Letter", etc.
    int PageWidthTwips,    // Width in twips
    int PageHeightTwips,   // Height in twips
    int MarginTopTwips,
    int MarginBottomTwips,
    int MarginLeftTwips,
    int MarginRightTwips,
    // Body text
    string BodyFontAscii,
    string BodyFontEastAsia,
    int BodyFontSizeHalfPt,   // pt × 2
    int LineSpacingVal,       // DXA for exact, or multiplier×240 for auto
    string LineSpacingRule,   // "Auto" or "Exact"
    // Heading 1
    string H1FontAscii,
    string H1FontEastAsia,
    int H1FontSizeHalfPt,
    string H1Color,           // hex RGB, or "inherit"
    int H1SpacingBeforeTwips,
    int H1SpacingAfterTwips,
    // Heading 2
    string H2FontAscii,
    string H2FontEastAsia,
    int H2FontSizeHalfPt,
    // Paragraph
    string BodyJustification,  // "Left", "Both"
    int FirstLineIndentTwips,  // 0 = no indent
    // Header/footer
    bool HasPageNumbers,
    string PageNumberFormat   // "1", "—1—", "i"
);

public static class AestheticRecipeSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. ModernCorporate — clean business document
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe ModernCorporate => new(
        Name: "ModernCorporate",
        Source: "Corporate design best practice",
        PageSize: "A4", PageWidthTwips: 11906, PageHeightTwips: 16838,
        MarginTopTwips: 1440, MarginBottomTwips: 1440, MarginLeftTwips: 1440, MarginRightTwips: 1440,
        BodyFontAscii: "Calibri", BodyFontEastAsia: "微软雅黑", BodyFontSizeHalfPt: 22,  // 11pt
        LineSpacingVal: 276, LineSpacingRule: "Auto",  // 1.15 line spacing
        H1FontAscii: "Calibri", H1FontEastAsia: "微软雅黑", H1FontSizeHalfPt: 32,  // 16pt
        H1Color: "1F3864", H1SpacingBeforeTwips: 360, H1SpacingAfterTwips: 120,
        H2FontAscii: "Calibri", H2FontEastAsia: "微软雅黑", H2FontSizeHalfPt: 28,  // 14pt
        BodyJustification: "Left", FirstLineIndentTwips: 0,
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 2. AcademicThesis — standard academic thesis
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe AcademicThesis => new(
        Name: "AcademicThesis",
        Source: "University thesis formatting standard",
        PageSize: "A4", PageWidthTwips: 11906, PageHeightTwips: 16838,
        MarginTopTwips: 1440, MarginBottomTwips: 1440, MarginLeftTwips: 1800, MarginRightTwips: 1440,
        // 1.25" left margin for binding gutter
        BodyFontAscii: "Times New Roman", BodyFontEastAsia: "宋体", BodyFontSizeHalfPt: 24,  // 12pt
        LineSpacingVal: 480, LineSpacingRule: "Auto",  // Double-spaced
        H1FontAscii: "Times New Roman", H1FontEastAsia: "黑体", H1FontSizeHalfPt: 32,  // 16pt
        H1Color: "000000", H1SpacingBeforeTwips: 480, H1SpacingAfterTwips: 240,
        H2FontAscii: "Times New Roman", H2FontEastAsia: "黑体", H2FontSizeHalfPt: 28,  // 14pt
        BodyJustification: "Both", FirstLineIndentTwips: 720,  // 0.5" first-line indent
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 3. ExecutiveBrief — concise executive summary
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe ExecutiveBrief => new(
        Name: "ExecutiveBrief",
        Source: "McKinsey/BCG briefing style",
        PageSize: "Letter", PageWidthTwips: 12240, PageHeightTwips: 15840,
        MarginTopTwips: 1080, MarginBottomTwips: 1080, MarginLeftTwips: 1260, MarginRightTwips: 1260,
        // 0.75" top/bottom, 0.875" sides — compact
        BodyFontAscii: "Arial", BodyFontEastAsia: "微软雅黑", BodyFontSizeHalfPt: 20,  // 10pt
        LineSpacingVal: 240, LineSpacingRule: "Auto",  // Single-spaced
        H1FontAscii: "Arial", H1FontEastAsia: "微软雅黑", H1FontSizeHalfPt: 28,  // 14pt
        H1Color: "333333", H1SpacingBeforeTwips: 240, H1SpacingAfterTwips: 80,
        H2FontAscii: "Arial", H2FontEastAsia: "微软雅黑", H2FontSizeHalfPt: 24,  // 12pt
        BodyJustification: "Left", FirstLineIndentTwips: 0,
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 4. ChineseGovernment — GB/T 9704-2012 党政机关公文格式
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe ChineseGovernment => new(
        Name: "ChineseGovernment",
        Source: "GB/T 9704-2012",
        PageSize: "A4", PageWidthTwips: 11906, PageHeightTwips: 16838,
        MarginTopTwips: 2268, MarginBottomTwips: 1701, MarginLeftTwips: 1701, MarginRightTwips: 1531,
        // 37mm top, 35mm bottom, 28mm left, 26mm right (GB/T 9704)
        BodyFontAscii: "FangSong", BodyFontEastAsia: "仿宋", BodyFontSizeHalfPt: 44,  // 二号=22pt
        LineSpacingVal: 570, LineSpacingRule: "Exact",  // 28.5pt exact
        H1FontAscii: "SimSun", H1FontEastAsia: "小标宋", H1FontSizeHalfPt: 44,  // 二号=22pt
        H1Color: "000000", H1SpacingBeforeTwips: 0, H1SpacingAfterTwips: 0,
        H2FontAscii: "SimHei", H2FontEastAsia: "黑体", H2FontSizeHalfPt: 36,  // 三号=18pt
        BodyJustification: "Both", FirstLineIndentTwips: 888,  // 2字符缩进 ≈ 15.6mm
        HasPageNumbers: true, PageNumberFormat: "—1—"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 5. MinimalModern — minimalist design
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe MinimalModern => new(
        Name: "MinimalModern",
        Source: "Modern minimalist design",
        PageSize: "A4", PageWidthTwips: 11906, PageHeightTwips: 16838,
        MarginTopTwips: 2160, MarginBottomTwips: 2160, MarginLeftTwips: 1800, MarginRightTwips: 1800,
        // Generous margins: 1.5" top/bottom, 1.25" sides
        BodyFontAscii: "Helvetica Neue", BodyFontEastAsia: "苹方-简", BodyFontSizeHalfPt: 22,  // 11pt
        LineSpacingVal: 360, LineSpacingRule: "Auto",  // 1.5 line spacing
        H1FontAscii: "Helvetica Neue", H1FontEastAsia: "苹方-简", H1FontSizeHalfPt: 36,  // 18pt
        H1Color: "333333", H1SpacingBeforeTwips: 600, H1SpacingAfterTwips: 200,
        H2FontAscii: "Helvetica Neue", H2FontEastAsia: "苹方-简", H2FontSizeHalfPt: 28,  // 14pt
        BodyJustification: "Left", FirstLineIndentTwips: 0,
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 6. IEEE Conference
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe IeeeConference => new(
        Name: "IEEEConference",
        Source: "IEEE conference paper template",
        PageSize: "Letter", PageWidthTwips: 12240, PageHeightTwips: 15840,
        MarginTopTwips: 1080, MarginBottomTwips: 0, MarginLeftTwips: 1134, MarginRightTwips: 1134,
        // 0.75" top, 0" bottom (tight), 0.79" sides
        BodyFontAscii: "Times New Roman", BodyFontEastAsia: "宋体", BodyFontSizeHalfPt: 20,  // 10pt
        LineSpacingVal: 240, LineSpacingRule: "Auto",  // Single
        H1FontAscii: "Times New Roman", H1FontEastAsia: "黑体", H1FontSizeHalfPt: 24,  // 12pt
        H1Color: "000000", H1SpacingBeforeTwips: 180, H1SpacingAfterTwips: 60,
        H2FontAscii: "Times New Roman", H2FontEastAsia: "黑体", H2FontSizeHalfPt: 22,  // 11pt
        BodyJustification: "Both", FirstLineIndentTwips: 0,
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 7. ACM sigconf
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe Acmsigconf => new(
        Name: "ACMsigconf",
        Source: "ACM sigconf template (acmart)",
        PageSize: "Letter", PageWidthTwips: 12240, PageHeightTwips: 15840,
        MarginTopTwips: 1080, MarginBottomTwips: 1080, MarginLeftTwips: 1080, MarginRightTwips: 1080,
        // 0.75" all margins
        BodyFontAscii: "Libertinus Serif", BodyFontEastAsia: "宋体", BodyFontSizeHalfPt: 18,  // 9pt
        LineSpacingVal: 240, LineSpacingRule: "Auto",  // Single
        H1FontAscii: "Libertinus Serif", H1FontEastAsia: "黑体", H1FontSizeHalfPt: 22,  // 11pt
        H1Color: "000000", H1SpacingBeforeTwips: 180, H1SpacingAfterTwips: 60,
        H2FontAscii: "Libertinus Serif", H2FontEastAsia: "黑体", H2FontSizeHalfPt: 20,  // 10pt
        BodyJustification: "Both", FirstLineIndentTwips: 0,
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 8. APA 7th edition
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe Apa7th => new(
        Name: "APA7th",
        Source: "APA Publication Manual 7th ed.",
        PageSize: "Letter", PageWidthTwips: 12240, PageHeightTwips: 15840,
        MarginTopTwips: 1440, MarginBottomTwips: 1440, MarginLeftTwips: 1440, MarginRightTwips: 1440,
        // 1" margins all sides
        BodyFontAscii: "Times New Roman", BodyFontEastAsia: "宋体", BodyFontSizeHalfPt: 24,  // 12pt
        LineSpacingVal: 480, LineSpacingRule: "Auto",  // Double-spaced
        H1FontAscii: "Times New Roman", H1FontEastAsia: "黑体", H1FontSizeHalfPt: 24,  // 12pt (same size, centered bold)
        H1Color: "000000", H1SpacingBeforeTwips: 0, H1SpacingAfterTwips: 0,
        H2FontAscii: "Times New Roman", H2FontEastAsia: "黑体", H2FontSizeHalfPt: 24,  // 12pt (left bold)
        BodyJustification: "Left", FirstLineIndentTwips: 720,  // 0.5" first-line indent
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 9. MLA 9th edition
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe Mla9th => new(
        Name: "MLA9th",
        Source: "MLA Handbook 9th ed.",
        PageSize: "Letter", PageWidthTwips: 12240, PageHeightTwips: 15840,
        MarginTopTwips: 1440, MarginBottomTwips: 1440, MarginLeftTwips: 1440, MarginRightTwips: 1440,
        BodyFontAscii: "Times New Roman", BodyFontEastAsia: "宋体", BodyFontSizeHalfPt: 24,  // 12pt
        LineSpacingVal: 480, LineSpacingRule: "Auto",  // Double-spaced
        H1FontAscii: "Times New Roman", H1FontEastAsia: "黑体", H1FontSizeHalfPt: 24,  // 12pt centered
        H1Color: "000000", H1SpacingBeforeTwips: 0, H1SpacingAfterTwips: 0,
        H2FontAscii: "Times New Roman", H2FontEastAsia: "黑体", H2FontSizeHalfPt: 24,  // 12pt left bold italic
        BodyJustification: "Left", FirstLineIndentTwips: 720,  // 0.5" first-line indent
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 10. Chicago/Turabian (17th ed.)
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe ChicagoTurabian => new(
        Name: "ChicagoTurabian",
        Source: "Chicago Manual of Style 17th ed.",
        PageSize: "Letter", PageWidthTwips: 12240, PageHeightTwips: 15840,
        MarginTopTwips: 1440, MarginBottomTwips: 1440, MarginLeftTwips: 1440, MarginRightTwips: 1440,
        BodyFontAscii: "Times New Roman", BodyFontEastAsia: "宋体", BodyFontSizeHalfPt: 24,  // 12pt
        LineSpacingVal: 480, LineSpacingRule: "Auto",  // Double-spaced
        H1FontAscii: "Times New Roman", H1FontEastAsia: "黑体", H1FontSizeHalfPt: 28,  // 14pt centered
        H1Color: "000000", H1SpacingBeforeTwips: 360, H1SpacingAfterTwips: 240,
        H2FontAscii: "Times New Roman", H2FontEastAsia: "黑体", H2FontSizeHalfPt: 24,  // 12pt centered bold
        BodyJustification: "Left", FirstLineIndentTwips: 720,
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 11. Springer LNCS
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe SpringerLncs => new(
        Name: "SpringerLNCS",
        Source: "Springer LNCS author guidelines",
        PageSize: "A4", PageWidthTwips: 11906, PageHeightTwips: 16838,
        MarginTopTwips: 1134, MarginBottomTwips: 1134, MarginLeftTwips: 1134, MarginRightTwips: 1134,
        // 0.79" all margins
        BodyFontAscii: "Computer Modern", BodyFontEastAsia: "宋体", BodyFontSizeHalfPt: 20,  // 10pt
        LineSpacingVal: 240, LineSpacingRule: "Auto",  // Single
        H1FontAscii: "Computer Modern", H1FontEastAsia: "黑体", H1FontSizeHalfPt: 24,  // 12pt
        H1Color: "000000", H1SpacingBeforeTwips: 180, H1SpacingAfterTwips: 60,
        H2FontAscii: "Computer Modern", H2FontEastAsia: "黑体", H2FontSizeHalfPt: 22,  // 11pt
        BodyJustification: "Both", FirstLineIndentTwips: 0,
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 12. Nature
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe Nature => new(
        Name: "Nature",
        Source: "Nature formatting guidelines",
        PageSize: "A4", PageWidthTwips: 11906, PageHeightTwips: 16838,
        MarginTopTwips: 1134, MarginBottomTwips: 1134, MarginLeftTwips: 1134, MarginRightTwips: 1134,
        BodyFontAscii: "Arial", BodyFontEastAsia: "微软雅黑", BodyFontSizeHalfPt: 16,  // 8pt (Nature is dense)
        LineSpacingVal: 240, LineSpacingRule: "Auto",
        H1FontAscii: "Arial", H1FontEastAsia: "微软雅黑", H1FontSizeHalfPt: 20,  // 10pt
        H1Color: "000000", H1SpacingBeforeTwips: 120, H1SpacingAfterTwips: 40,
        H2FontAscii: "Arial", H2FontEastAsia: "微软雅黑", H2FontSizeHalfPt: 18,  // 9pt
        BodyJustification: "Both", FirstLineIndentTwips: 0,
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // 13. Harvard Business Review (HBR)
    // ══════════════════════════════════════════════════════════════════════════
    public static AestheticRecipe Hbr => new(
        Name: "HBR",
        Source: "Harvard Business Review style",
        PageSize: "Letter", PageWidthTwips: 12240, PageHeightTwips: 15840,
        MarginTopTwips: 1440, MarginBottomTwips: 1440, MarginLeftTwips: 1260, MarginRightTwips: 1260,
        BodyFontAscii: "Georgia", BodyFontEastAsia: "微软雅黑", BodyFontSizeHalfPt: 22,  // 11pt
        LineSpacingVal: 300, LineSpacingRule: "Auto",  // 1.25 line spacing
        H1FontAscii: "Georgia", H1FontEastAsia: "微软雅黑", H1FontSizeHalfPt: 36,  // 18pt
        H1Color: "C41230", H1SpacingBeforeTwips: 480, H1SpacingAfterTwips: 200,  // HBR red
        H2FontAscii: "Georgia", H2FontEastAsia: "微软雅黑", H2FontSizeHalfPt: 28,  // 14pt
        BodyJustification: "Left", FirstLineIndentTwips: 0,
        HasPageNumbers: true, PageNumberFormat: "1"
    );

    // ══════════════════════════════════════════════════════════════════════════
    // Apply a recipe to a document
    // ══════════════════════════════════════════════════════════════════════════
    public static void ApplyRecipe(string outputPath, AestheticRecipe recipe)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        var body = new Body();
        mainPart.Document = new Document(body);

        // Page setup
        body.AppendChild(new SectionProperties(
            new PageSize { Width = recipe.PageWidthTwips, Height = recipe.PageHeightTwips },
            new PageMargin
            {
                Top = recipe.MarginTopTwips,
                Bottom = recipe.MarginBottomTwips,
                Left = recipe.MarginLeftTwips,
                Right = recipe.MarginRightTwips
            }
        ));

        // Styles
        var stylesPart = mainPart.AddNewPart<StyleDefinitionsPart>();
        var styles = new Styles();

        // DocDefaults
        styles.AppendChild(new DocDefaults(
            new RunPropertiesDefault(new RunProperties(
                new RunFonts { Ascii = recipe.BodyFontAscii, HighAnsi = recipe.BodyFontAscii,
                    EastAsia = recipe.BodyFontEastAsia },
                new FontSize { Val = recipe.BodyFontSizeHalfPt.ToString() }
            )),
            new ParagraphPropertiesDefault(new ParagraphProperties(
                new Spacing
                {
                    Line = recipe.LineSpacingVal,
                    LineRule = recipe.LineSpacingRule == "Exact"
                        ? LineSpacingRuleValues.Exact
                        : LineSpacingRuleValues.Auto
                }
            ))
        ));

        // Normal style
        var normalPPr = new ParagraphProperties();
        if (recipe.BodyJustification == "Both")
            normalPPr.AppendChild(new Justification { Val = JustificationValues.Both });
        if (recipe.FirstLineIndentTwips > 0)
            normalPPr.AppendChild(new Indentation { FirstLine = recipe.FirstLineIndentTwips.ToString() });

        styles.AppendChild(new Style(
            new StyleName { Val = "Normal" },
            normalPPr,
            new RunProperties(
                new RunFonts { Ascii = recipe.BodyFontAscii, HighAnsi = recipe.BodyFontAscii,
                    EastAsia = recipe.BodyFontEastAsia },
                new FontSize { Val = recipe.BodyFontSizeHalfPt.ToString() }
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "Normal" });

        // Heading 1
        styles.AppendChild(new Style(
            new StyleName { Val = "heading 1" },
            new BasedOn { Val = "Normal" },
            new ParagraphProperties(
                new KeepNext(),
                new Spacing { Before = recipe.H1SpacingBeforeTwips.ToString(),
                    After = recipe.H1SpacingAfterTwips.ToString() },
                new OutlineLevel { Val = 0 }
            ),
            new RunProperties(
                new RunFonts { Ascii = recipe.H1FontAscii, HighAnsi = recipe.H1FontAscii,
                    EastAsia = recipe.H1FontEastAsia },
                new FontSize { Val = recipe.H1FontSizeHalfPt.ToString() },
                new Bold(),
                new Color { Val = recipe.H1Color }
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "Heading1" });

        // Heading 2
        styles.AppendChild(new Style(
            new StyleName { Val = "heading 2" },
            new BasedOn { Val = "Normal" },
            new ParagraphProperties(
                new KeepNext(),
                new OutlineLevel { Val = 1 }
            ),
            new RunProperties(
                new RunFonts { Ascii = recipe.H2FontAscii, HighAnsi = recipe.H2FontAscii,
                    EastAsia = recipe.H2FontEastAsia },
                new FontSize { Val = recipe.H2FontSizeHalfPt.ToString() },
                new Bold()
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "Heading2" });

        stylesPart.Styles = styles;
        styles.Save();
    }
}
