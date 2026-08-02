// StyleSystemSamples.cs — Style definitions: Normal/Heading chain, character/table/list styles,
// DocDefaults, latentStyles, CJK 公文 styles, APA 7th, import, inheritance resolution
// Requires: DocumentFormat.OpenXml 3.2.0

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class StyleSystemSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Define Normal and Heading 1–4 styles (the foundation chain)
    // ══════════════════════════════════════════════════════════════════════════
    public static void DefineHeadingChain(string outputPath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        mainPart.Document = new Document(new Body());
        var stylesPart = mainPart.AddNewPart<StyleDefinitionsPart>();
        var styles = new Styles();

        // ── DocDefaults ──────────────────────────────────────────────────────
        styles.AppendChild(new DocDefaults(
            new RunPropertiesDefault(new RunProperties(
                new RunFonts { Ascii = "Calibri", HighAnsi = "Calibri", EastAsia = "宋体", ComplexScript = "Times New Roman" },
                new FontSize { Val = "22" }  // 11pt
            )),
            new ParagraphPropertiesDefault(new ParagraphProperties(
                new Spacing { After = "200", Line = "276", LineRule = LineSpacingRuleValues.Auto }
            ))
        ));

        // ── Normal style ─────────────────────────────────────────────────────
        styles.AppendChild(new Style(
            new StyleName { Val = "Normal" },
            new ParagraphProperties(
                new Spacing { After = "200", Line = "276", LineRule = LineSpacingRuleValues.Auto },
                new Justification { Val = JustificationValues.Left }
            ),
            new RunProperties(
                new RunFonts { Ascii = "Calibri", HighAnsi = "Calibri" },
                new FontSize { Val = "22" }
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "Normal" });

        // ── Heading 1–4 ─────────────────────────────────────────────────────
        // CRITICAL: Every heading style MUST have OutlineLevel — without it,
        // Word treats them as plain styled text (TOC/navigation pane won't work).
        var headingDefs = new (string id, string name, int outlineLevel, string sz, string before, string after)[]
        {
            ("Heading1", "heading 1", 0, "32", "480", "120"),  // 16pt, 24pt before, 6pt after
            ("Heading2", "heading 2", 1, "28", "360", "80"),   // 14pt
            ("Heading3", "heading 3", 2, "26", "280", "80"),   // 13pt
            ("Heading4", "heading 4", 3, "24", "240", "40"),   // 12pt
        };

        foreach (var h in headingDefs)
        {
            styles.AppendChild(new Style(
                new StyleName { Val = h.name },
                new BasedOn { Val = "Normal" },
                new NextParagraphStyle { Val = "Normal" },
                new ParagraphProperties(
                    new KeepNext(),
                    new KeepLines(),
                    new Spacing { Before = h.before, After = h.after },
                    new OutlineLevel { Val = h.outlineLevel }
                ),
                new RunProperties(
                    new RunFonts { Ascii = "Calibri", HighAnsi = "Calibri" },
                    new FontSize { Val = h.sz },
                    new Bold(),
                    new Color { Val = "1F3864" }  // Dark blue
                )
            )
            { Type = StyleValues.Paragraph, StyleId = h.id });
        }

        stylesPart.Styles = styles;
        styles.Save();
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Character styles (inline formatting that can be applied to runs)
    // ══════════════════════════════════════════════════════════════════════════
    public static void DefineCharacterStyles(Styles styles)
    {
        // "Emphasis" character style — italic
        styles.AppendChild(new Style(
            new StyleName { Val = "Emphasis" },
            new RunProperties(
                new Italic(),
                new Color { Val = "404040" }
            )
        )
        { Type = StyleValues.Character, StyleId = "EmphasisChar" });

        // "Strong" character style — bold
        styles.AppendChild(new Style(
            new StyleName { Val = "Strong" },
            new RunProperties(
                new Bold(),
                new Color { Val = "000000" }
            )
        )
        { Type = StyleValues.Character, StyleId = "StrongChar" });

        // "Code" character style — monospace
        styles.AppendChild(new Style(
            new StyleName { Val = "Code" },
            new RunProperties(
                new RunFonts { Ascii = "Consolas", HighAnsi = "Consolas" },
                new FontSize { Val = "20" },  // 10pt
                new Color { Val = "C7254E" }   // Reddish
            )
        )
        { Type = StyleValues.Character, StyleId = "CodeChar" });
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. Table style
    // ══════════════════════════════════════════════════════════════════════════
    public static void DefineTableStyle(Styles styles)
    {
        // "LightGrid" table style with header row shading
        var tableStyle = new Style(
            new StyleName { Val = "Light Grid" },
            new TableStyleProperties(
                // Default table borders
                new TableBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new LeftBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new RightBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideHorizontalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideVerticalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" }
                )
            ),
            // Conditional formatting for first row (header)
            new TableStyleConditionalFormattingRule(
                new TableStyleProperties(
                    new RunProperties(new Bold(), new Color { Val = "FFFFFF" }),
                    new TableCellProperties(
                        new Shading { Val = ShadingPatternValues.Clear, Fill = "4472C4" }
                    )
                )
            )
            { Type = ConditionalFormatValues.FirstRow }
        )
        { Type = StyleValues.Table, StyleId = "LightGrid" };

        styles.AppendChild(tableStyle);
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. List style (for numbered/bulleted lists)
    // ══════════════════════════════════════════════════════════════════════════
    public static void DefineListStyle(Styles styles)
    {
        // "ListParagraph" — paragraph style for list items
        styles.AppendChild(new Style(
            new StyleName { Val = "List Paragraph" },
            new BasedOn { Val = "Normal" },
            new ParagraphProperties(
                new Indentation { Left = "720" }  // 0.5 inch indent
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "ListParagraph" });
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. LatentStyles — controls which built-in styles Word may auto-generate
    // ══════════════════════════════════════════════════════════════════════════
    public static void DefineLatentStyles(Styles styles)
    {
        var latentStyles = new LatentStyles
        {
            Count = 276,    // Number of latent styles
            DefaultLockedState = LockedStateValues.Unlocked,
            DefaultQFormat = true
        };

        // Explicitly lock styles we don't want auto-generated
        latentStyles.AppendChild(new LatentStyle
        {
            Name = "Default Paragraph Font",
            Locked = true
        });

        styles.InsertAfter(latentStyles, styles.GetFirstChild<DocDefaults>());
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 6. CJK 公文 styles (GB/T 9704-2012)
    // ══════════════════════════════════════════════════════════════════════════
    public static void DefineChineseGovtStyles(Styles styles)
    {
        // 公文正文 — 二号 (22pt) 仿宋
        styles.AppendChild(new Style(
            new StyleName { Val = "公文正文" },
            new ParagraphProperties(
                new Spacing { Line = "560", LineRule = LineSpacingRuleValues.Exact },
                // GB/T 9704: 28.5pt line spacing for 二号 text
                new Justification { Val = JustificationValues.Both }
            ),
            new RunProperties(
                new RunFonts { Ascii = "FangSong", HighAnsi = "FangSong", EastAsia = "仿宋", ComplexScript = "FangSong" },
                new FontSize { Val = "44" },  // 二号 = 22pt × 2 = 44 half-points
                new FontSizeComplexScript { Val = "44" }
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "GovBodyText" });

        // 公文标题 — 二号 小标宋
        styles.AppendChild(new Style(
            new StyleName { Val = "公文标题" },
            new ParagraphProperties(
                new Spacing { Before = "0", After = "0", Line = "560", LineRule = LineSpacingRuleValues.Exact },
                new Justification { Val = JustificationValues.Center },
                new OutlineLevel { Val = 0 }  // MUST have OutlineLevel for TOC
            ),
            new RunProperties(
                new RunFonts { Ascii = "SimSun", HighAnsi = "SimSun", EastAsia = "小标宋", ComplexScript = "SimSun" },
                new FontSize { Val = "44" },
                new Bold()
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "GovTitle" });

        // 发文机关标志 — 红色大字
        styles.AppendChild(new Style(
            new StyleName { Val = "发文机关标志" },
            new ParagraphProperties(
                new Justification { Val = JustificationValues.Center },
                new Spacing { After = "0" }
            ),
            new RunProperties(
                new RunFonts { EastAsia = "方正小标宋简体" },
                new FontSize { Val = "52" },  // 26pt
                new Color { Val = "FF0000" },
                new Bold()
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "GovOrgMark" });
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 7. APA 7th edition styles
    // ══════════════════════════════════════════════════════════════════════════
    public static void DefineApa7Styles(Styles styles)
    {
        // APA 7th: Times New Roman 12pt, double-spaced, 1" margins

        // APA Normal
        styles.AppendChild(new Style(
            new StyleName { Val = "APA Normal" },
            new ParagraphProperties(
                new Spacing { After = "0", Line = "480", LineRule = LineSpacingRuleValues.Auto },
                // Double space = 480 (240 × 2)
                new Indentation { FirstLine = "720" }  // 0.5" first-line indent
            ),
            new RunProperties(
                new RunFonts { Ascii = "Times New Roman", HighAnsi = "Times New Roman" },
                new FontSize { Val = "24" }  // 12pt
            )
        )
        { Type = StyleValues.Paragraph, StyleId = "APANormal" });

        // APA Heading 1 (Centered, Bold)
        styles.AppendChild(new Style(
            new StyleName { Val = "APA Heading 1" },
            new BasedOn { Val = "APANormal" },
            new ParagraphProperties(
                new Spacing { Before = "0", After = "0", Line = "480", LineRule = LineSpacingRuleValues.Auto },
                new Justification { Val = JustificationValues.Center },
                new Indentation { FirstLine = "0" },
                new OutlineLevel { Val = 0 }
            ),
            new RunProperties(new Bold())
        )
        { Type = StyleValues.Paragraph, StyleId = "APAHeading1" });

        // APA Heading 2 (Left, Bold)
        styles.AppendChild(new Style(
            new StyleName { Val = "APA Heading 2" },
            new BasedOn { Val = "APANormal" },
            new ParagraphProperties(
                new Justification { Val = JustificationValues.Left },
                new Indentation { FirstLine = "0" },
                new OutlineLevel { Val = 1 }
            ),
            new RunProperties(new Bold())
        )
        { Type = StyleValues.Paragraph, StyleId = "APAHeading2" });

        // APA Heading 3 (Left, Bold Italic)
        styles.AppendChild(new Style(
            new StyleName { Val = "APA Heading 3" },
            new BasedOn { Val = "APANormal" },
            new ParagraphProperties(
                new Justification { Val = JustificationValues.Left },
                new Indentation { FirstLine = "0" },
                new OutlineLevel { Val = 2 }
            ),
            new RunProperties(new Bold(), new Italic())
        )
        { Type = StyleValues.Paragraph, StyleId = "APAHeading3" });
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 8. Style import: copy styles from another document
    // ══════════════════════════════════════════════════════════════════════════
    public static void ImportStylesFromDocument(string sourceDocxPath, string targetDocxPath)
    {
        // Open source to read styles
        using var sourceDoc = WordprocessingDocument.Open(sourceDocxPath, false);
        var sourceStylesPart = sourceDoc.MainDocumentPart?.StyleDefinitionsPart;
        if (sourceStylesPart == null) return;

        var sourceStyles = sourceStylesPart.Styles!;

        // Open target for writing
        using var targetDoc = WordprocessingDocument.Open(targetDocxPath, true);
        var targetMainPart = targetDoc.MainDocumentPart!;
        var targetStylesPart = targetMainPart.StyleDefinitionsPart
            ?? targetMainPart.AddNewPart<StyleDefinitionsPart>();

        var targetStyles = targetStylesPart.Styles ?? new Styles();

        // Copy each style that doesn't already exist in target
        foreach (var style in sourceStyles.Elements<Style>())
        {
            var styleId = style.StyleId;
            if (targetStyles.Elements<Style>().All(s => s.StyleId != styleId))
            {
                // Clone to detach from source document
                targetStyles.AppendChild((Style)style.CloneNode(true));
            }
        }

        targetStylesPart.Styles = targetStyles;
        targetStyles.Save();
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 9. Resolve style inheritance (find effective formatting)
    // ══════════════════════════════════════════════════════════════════════════
    // When applying formatting, you must resolve the inheritance chain:
    // DocDefaults → BasedOn chain → the style itself → direct formatting
    public static RunProperties ResolveEffectiveRunProperties(Styles styles, string styleId)
    {
        var effective = new RunProperties();
        var visited = new HashSet<string>();
        var chain = new List<Style>();

        // Walk the BasedOn chain from the style up to Normal
        var current = styleId;
        while (current != null && !visited.Contains(current))
        {
            visited.Add(current);
            var style = styles.Elements<Style>()
                .FirstOrDefault(s => s.StyleId == current);
            if (style == null) break;
            chain.Add(style);
            current = style.BasedOn?.Val;
        }

        // Apply in reverse order (root first, leaf last)
        chain.Reverse();
        foreach (var style in chain)
        {
            var rPr = style.StyleRunProperties;
            if (rPr == null) continue;
            foreach (var child in rPr.Elements())
            {
                // Remove existing property of same type before adding
                var existing = effective.Elements()
                    .FirstOrDefault(e => e.GetType() == child.GetType());
                existing?.Remove();
                effective.AppendChild((OpenXmlElement)child.CloneNode(true));
            }
        }

        return effective;
    }
}
