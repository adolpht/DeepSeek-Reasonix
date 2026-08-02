// CharacterFormattingSamples.cs — RunProperties: fonts (incl. CJK RunFonts), size, bold/italic,
// all underline types, color, highlight, strike, sub/super script, caps, spacing, shading,
// border, emphasis marks
// Requires: DocumentFormat.OpenXml 3.2.0

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class CharacterFormattingSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Font families with CJK mapping (RunFonts)
    // ══════════════════════════════════════════════════════════════════════════
    // RunFonts has 4 slots — each targets a different character range:
    //   Ascii        → Latin characters (a-z, A-Z, 0-9, punctuation)
    //   HighAnsi     → Extended Latin (accented chars, Eastern European)
    //   EastAsia     → CJK ideographs (Chinese, Japanese, Korean)
    //   ComplexScript → RTL scripts (Arabic, Hebrew), Devanagari, etc.
    public static Run CreateRunWithCjkFonts()
    {
        return new Run(
            new RunProperties(
                new RunFonts
                {
                    Ascii = "Times New Roman",
                    HighAnsi = "Times New Roman",
                    EastAsia = "宋体",            // CJK
                    ComplexScript = "Times New Roman"
                },
                new FontSize { Val = "24" }  // 12pt
            ),
            new Text("中英文混排 Mixed CJK-Latin text")
            { Space = SpaceProcessingModeValues.Preserve }
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Font size (half-points: pt × 2)
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateRunWithSize(double pointSize)
    {
        var halfPoints = (int)(pointSize * 2);
        return new Run(
            new RunProperties(
                new FontSize { Val = halfPoints.ToString() },
                new FontSizeComplexScript { Val = halfPoints.ToString() }
                // Always set FontSizeComplexScript too for consistent sizing
            ),
            new Text($"{pointSize}pt text")
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. Bold and italic
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateBoldItalicRun()
    {
        return new Run(
            new RunProperties(
                new Bold(),
                new Italic(),
                new BoldComplexScript(),    // Also set CS variants
                new ItalicComplexScript()
            ),
            new Text("Bold italic text")
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. All underline types
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateUnderlineShowcase()
    {
        var para = new Paragraph();
        var underlineTypes = new (UnderlineValues val, string label)[]
        {
            (UnderlineValues.Single, "Single"),
            (UnderlineValues.Double, "Double"),
            (UnderlineValues.Thick, "Thick"),
            (UnderlineValues.Dotted, "Dotted"),
            (UnderlineValues.Dash, "Dash"),
            (UnderlineValues.DotDash, "DotDash"),
            (UnderlineValues.DotDotDash, "DotDotDash"),
            (UnderlineValues.Wave, "Wave"),
            (UnderlineValues.DottedHeavy, "DottedHeavy"),
            (UnderlineValues.DashHeavy, "DashHeavy"),
            (UnderlineValues.DashLong, "DashLong"),
            (UnderlineValues.WaveDouble, "WaveDouble"),
            (UnderlineValues.Words, "Words (underline words only)"),
        };

        foreach (var (val, label) in underlineTypes)
        {
            para.AppendChild(new Run(
                new RunProperties(
                    new Underline { Val = val, Color = "auto" }
                ),
                new Text(label + "  ")
                { Space = SpaceProcessingModeValues.Preserve }
            ));
        }

        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. Color and highlight
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateColoredRun()
    {
        return new Run(
            new RunProperties(
                new Color { Val = "FF0000" },             // Red text (hex RGB, no #)
                new Shading
                {
                    Val = ShadingPatternValues.Clear,     // Highlight = Shading with pattern Clear
                    Fill = "FFFF00"                        // Yellow highlight
                }
            ),
            new Text("Red on yellow highlight")
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 6. Strikethrough (single and double)
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateStrikethroughRun()
    {
        return new Run(
            new RunProperties(
                new Strike(),           // Single strikethrough
                new DoubleStrike()     // Double strikethrough (use one or the other)
            ),
            new Text("Strikethrough text")
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 7. Subscript and superscript
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateSubSuperScriptParagraph()
    {
        // Chemical formula: H₂O
        var para = new Paragraph();
        para.AppendChild(new Run(new Text("H")));
        para.AppendChild(new Run(
            new RunProperties(
                new VerticalTextAlignment { Val = VerticalAlignValues.Subscript },
                new FontSize { Val = "18" }  // Slightly smaller for subscript
            ),
            new Text("2")
        ));
        para.AppendChild(new Run(new Text("O")));

        // Mathematical: x² + y² = z²
        para.AppendChild(new Run(new Text("  ")));
        para.AppendChild(new Run(new Text("x")));
        para.AppendChild(new Run(
            new RunProperties(
                new VerticalTextAlignment { Val = VerticalAlignValues.Superscript },
                new FontSize { Val = "18" }
            ),
            new Text("2")
        ));
        para.AppendChild(new Run(new Text(" + y")));
        para.AppendChild(new Run(
            new RunProperties(
                new VerticalTextAlignment { Val = VerticalAlignValues.Superscript },
                new FontSize { Val = "18" }
            ),
            new Text("2")
        ));
        para.AppendChild(new Run(new Text(" = z")));
        para.AppendChild(new Run(
            new RunProperties(
                new VerticalTextAlignment { Val = VerticalAlignValues.Superscript },
                new FontSize { Val = "18" }
            ),
            new Text("2")
        ));

        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 8. Caps (small caps and all caps)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateCapsParagraph()
    {
        var para = new Paragraph();
        para.AppendChild(new Run(
            new RunProperties(new SmallCaps()),
            new Text("Small Caps ")
            { Space = SpaceProcessingModeValues.Preserve }
        ));
        para.AppendChild(new Run(
            new RunProperties(new Caps()),
            new Text("ALL CAPS")
        ));
        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 9. Character spacing (kerning, expanded/condensed)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateSpacingParagraph()
    {
        var para = new Paragraph();

        // Expanded spacing (60 twentieths of a point = 3pt extra)
        para.AppendChild(new Run(
            new RunProperties(
                new CharacterSpacing { Val = 60 }
            ),
            new Text("Expanded ")
            { Space = SpaceProcessingModeValues.Preserve }
        ));

        // Condensed spacing (-40 twentieths of a point)
        para.AppendChild(new Run(
            new RunProperties(
                new CharacterSpacing { Val = -40 }
            ),
            new Text("Condensed ")
            { Space = SpaceProcessingModeValues.Preserve }
        ));

        // Kerning enabled (minimum 12pt for kerning to activate)
        para.AppendChild(new Run(
            new RunProperties(
                new Kern { Val = 24 }  // Half-points: 12pt = 24
            ),
            new Text("With Kerning")
        ));

        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 10. Shading (background color on a run)
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateShadedRun()
    {
        return new Run(
            new RunProperties(
                new Shading
                {
                    Val = ShadingPatternValues.Clear,  // Solid fill
                    Fill = "D9E2F3"                     // Light blue background
                }
            ),
            new Text("Shaded background")
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 11. Text border (outline around characters)
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateBorderedRun()
    {
        return new Run(
            new RunProperties(
                new TextOutlineEffect
                {
                    // Note: TextOutlineEffect is in the DrawingML namespace
                    // For paragraph-level borders on runs, use the older approach
                },
                new Border
                {
                    Val = BorderValues.Single,
                    Size = 4,     // Eighth-points: 4 = 0.5pt
                    Space = 1,
                    Color = "FF0000"
                }
            ),
            new Text("Bordered text")
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 12. Emphasis marks (CJK dot/circle above/below)
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateEmphasisMarkRun()
    {
        return new Run(
            new RunProperties(
                new Emphasis
                {
                    Val = EmphasisMarkValues.Dot    // Dot above each character
                    // Other values: Circle, Comma, UnderDot
                }
            ),
            new Text("强调点标注")
        );
    }
}
