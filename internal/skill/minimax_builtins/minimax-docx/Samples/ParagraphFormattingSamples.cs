// ParagraphFormattingSamples.cs — ParagraphProperties: justification, indentation,
// line/paragraph spacing, keep/widow, outline level, borders, tabs, numbering, bidi, frame
// Requires: DocumentFormat.OpenXml 3.2.0

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class ParagraphFormattingSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Justification (alignment)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateJustifiedParagraph()
    {
        // Both = justified (flush left and right, common in academic/CJK)
        return new Paragraph(
            new ParagraphProperties(
                new Justification { Val = JustificationValues.Both }
            ),
            new Run(new Text("This paragraph is fully justified. The text stretches to fill " +
                "the entire line width, creating even margins on both sides."))
            { Space = SpaceProcessingModeValues.Preserve }
        );
    }

    // All justification values: Left, Center, Right, Both, Distribute
    public static Paragraph CreateAlignedParagraph(JustificationValues alignment, string text)
    {
        return new Paragraph(
            new ParagraphProperties(
                new Justification { Val = alignment }
            ),
            new Run(new Text(text))
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Indentation (left, right, first-line, hanging)
    // ══════════════════════════════════════════════════════════════════════════
    // Units: DXA (twips). 1 inch = 1440, 1 cm ≈ 567.
    public static Paragraph CreateIndentedParagraph()
    {
        return new Paragraph(
            new ParagraphProperties(
                new Indentation
                {
                    Left = "1440",       // 1 inch left indent
                    Right = "720",       // 0.5 inch right indent
                    FirstLine = "720"    // 0.5 inch first-line indent (relative to Left)
                }
            ),
            new Run(new Text("Paragraph with 1\" left, 0.5\" right, and 0.5\" first-line indent."))
        );
    }

    public static Paragraph CreateHangingIndentParagraph()
    {
        // Hanging indent: FirstLine is negative, Left is positive
        return new Paragraph(
            new ParagraphProperties(
                new Indentation
                {
                    Left = "1440",       // 1 inch from left margin
                    Hanging = "720"      // 0.5 inch hanging indent
                    // Hanging = negative FirstLine equivalent
                }
            ),
            new Run(new Text("  This paragraph has a hanging indent. The first line extends " +
                "to the left while subsequent lines are indented. Common in bibliographies."))
            { Space = SpaceProcessingModeValues.Preserve }
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. Line spacing (single, 1.5, double, exact, at-least)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateSingleSpacedParagraph()
    {
        // Single spacing: Line=240, LineRule=Auto
        return new Paragraph(
            new ParagraphProperties(
                new Spacing
                {
                    Line = 240,
                    LineRule = LineSpacingRuleValues.Auto
                }
            ),
            new Run(new Text("Single-spaced paragraph."))
        );
    }

    public static Paragraph CreateDoubleSpacedParagraph()
    {
        // Double spacing: Line=480, LineRule=Auto
        // Formula: multiplier × 240. So 1.5× = 360, 2× = 480.
        return new Paragraph(
            new ParagraphProperties(
                new Spacing
                {
                    Line = 480,
                    LineRule = LineSpacingRuleValues.Auto
                }
            ),
            new Run(new Text("Double-spaced paragraph (APA/MLA standard)."))
        );
    }

    public static Paragraph CreateExactLineSpacingParagraph()
    {
        // Exact line spacing: 28pt (common for 二号 仿宋 in Chinese government docs)
        // In DXA: 28pt × 20 = 560
        return new Paragraph(
            new ParagraphProperties(
                new Spacing
                {
                    Line = 560,
                    LineRule = LineSpacingRuleValues.Exact
                }
            ),
            new Run(new Text("Exactly 28pt line spacing."))
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. Paragraph spacing (before/after)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateSpacedParagraph()
    {
        return new Paragraph(
            new ParagraphProperties(
                new Spacing
                {
                    Before = "480",      // 24pt before (480 twips)
                    After = "240",       // 12pt after (240 twips)
                    BeforeLines = null,  // Use DXA, not lines
                    AfterLines = null
                }
            ),
            new Run(new Text("Paragraph with 24pt before and 12pt after spacing."))
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. Keep with next / Keep lines together
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateKeepWithNextParagraph()
    {
        // KeepNext: keep this paragraph on the same page as the next one.
        // Essential for headings — prevents heading at bottom of page.
        return new Paragraph(
            new ParagraphProperties(
                new KeepNext(),
                new KeepLines()  // KeepLines: don't split this para across pages
            ),
            new Run(new Text("This paragraph stays with the next one."))
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 6. Widow/orphan control
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateWidowControlParagraph()
    {
        // WidowControl = true prevents orphan lines at top/bottom of page
        return new Paragraph(
            new ParagraphProperties(
                new WidowControl { Val = true }
            ),
            new Run(new Text("Widow/orphan control enabled."))
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 7. Outline level (for TOC and navigation)
    // ══════════════════════════════════════════════════════════════════════════
    // CRITICAL: Heading styles MUST include OutlineLevel.
    public static Paragraph CreateOutlineLevelParagraph()
    {
        return new Paragraph(
            new ParagraphProperties(
                new OutlineLevel { Val = 1 },  // Level 1 = equivalent to Heading 2
                new KeepNext()
            ),
            new Run(
                new RunProperties(new Bold(), new FontSize { Val = "28" }),
                new Text("Section Title")
            )
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 8. Paragraph borders
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateBorderedParagraph()
    {
        return new Paragraph(
            new ParagraphProperties(
                new ParagraphBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Space = 1, Color = "000000" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Space = 1, Color = "000000" },
                    new LeftBorder { Val = BorderValues.Single, Size = 8, Space = 4, Color = "4472C4" },
                    new RightBorder { Val = BorderValues.None, Size = 0, Space = 0, Color = "auto" }
                )
            ),
            new Run(new Text("Paragraph with top, bottom, and left borders."))
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 9. Tab stops
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateTabStopParagraph()
    {
        return new Paragraph(
            new ParagraphProperties(
                new Tabs(
                    new TabStop { Val = TabStopValues.Left, Position = 2880 },    // 2" left tab
                    new TabStop { Val = TabStopValues.Center, Position = 5760 },  // 4" center tab
                    new TabStop { Val = TabStopValues.Right, Position = 8640 },   // 6" right tab
                    new TabStop
                    {
                        Val = TabStopValues.Decimal,
                        Position = 11520  // 8" decimal tab (aligns on decimal point)
                    }
                )
            ),
            new Run(new Text("Left")),
            new Run(new TabChar()),
            new Run(new Text("Center")),
            new Run(new TabChar()),
            new Run(new Text("Right")),
            new Run(new TabChar()),
            new Run(new Text("123.45"))
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 10. Numbering reference
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateNumberedParagraph(int numberingId, int level)
    {
        return new Paragraph(
            new ParagraphProperties(
                new NumberingProperties(
                    new NumberingId { Val = numberingId },
                    new NumberingLevelReference { Val = level }
                ),
                new Indentation { Left = ((level + 1) * 720).ToString() }
            ),
            new Run(new Text("Numbered list item"))
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 11. Bidirectional (RTL) text
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateRtlParagraph()
    {
        return new Paragraph(
            new ParagraphProperties(
                new BiDi(),
                new Justification { Val = JustificationValues.Right }
            ),
            new Run(
                new RunProperties(
                    new RunFonts { ComplexScript = "Arial" },
                    new ComplexScript()
                ),
                new Text("النص العربي")  // Arabic text (RTL)
                { Space = SpaceProcessingModeValues.Preserve }
            )
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 12. Frame properties (text wrap around paragraph)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateFramedParagraph()
    {
        return new Paragraph(
            new ParagraphProperties(
                new FrameProperties
                {
                    Width = 2880,                  // 2 inches wide (in twips)
                    Height = 1440,                 // 1 inch tall
                    HorizontalPosition = HorizontalAnchorValues.Margin,
                    VerticalPosition = VerticalAnchorValues.Paragraph,
                    HorizontalPositionAnchor = HorizontalAnchorValues.Margin,
                    XAlignment = HorizontalAlignmentValues.Left,
                    YAlignment = VerticalAlignmentValues.Top,
                    Wrap = TextWrappingValues.Around
                },
                new ParagraphBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
                    new LeftBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
                    new RightBorder { Val = BorderValues.Single, Size = 4, Color = "000000" }
                )
            ),
            new Run(new Text("Framed sidebar text"))
        );
    }
}
