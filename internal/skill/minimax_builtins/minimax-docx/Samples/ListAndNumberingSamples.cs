// ListAndNumberingSamples.cs — Numbering: bullets, multi-level decimal, custom symbols,
// outline→headings, legal, Chinese 一/（一）/1./(1), restart/continue
// Requires: DocumentFormat.OpenXml 3.2.0

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class ListAndNumberingSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Bullet list (simple unordered list)
    // ══════════════════════════════════════════════════════════════════════════
    public static NumberingDefinitionsPart CreateBulletListNumbering(
        WordprocessingDocument doc, int abstractNumId = 0)
    {
        var numberingPart = doc.MainDocumentPart!.NumberingDefinitionsPart
            ?? doc.MainDocumentPart.AddNewPart<NumberingDefinitionsPart>();

        var numbering = numberingPart.Numbering ?? new Numbering();

        // Abstract numbering definition for bullets
        var abstractNum = new AbstractNum { AbstractNumberId = abstractNumId };
        abstractNum.AppendChild(new Level { LevelIndex = 0, StartNumberingValue = new StartNumberingValue { Val = 1 } });
        abstractNum.GetFirstChild<Level>()!.AppendChild(
            new NumberingFormat { Val = NumberFormatValues.Bullet });
        abstractNum.GetFirstChild<Level>()!.AppendChild(
            new LevelText { Val = "●" });
        abstractNum.GetFirstChild<Level>()!.AppendChild(
            new LevelJustification { Val = LevelJustificationValues.Left });
        abstractNum.GetFirstChild<Level>()!.AppendChild(new ParagraphProperties(
            new Indentation { Left = "720", Hanging = "360" }
        ));
        abstractNum.GetFirstChild<Level>()!.AppendChild(new RunProperties(
            new RunFonts { Ascii = "Symbol", HighAnsi = "Symbol" }
        ));

        numbering.InsertAt(abstractNum, 0);

        // Instance (concrete numbering ID)
        var numId = 1;
        numbering.AppendChild(new NumberingInstance(
            new AbstractNumId { Val = abstractNumId }
        ) { NumberID = numId });

        numberingPart.Numbering = numbering;
        return numberingPart;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Multi-level decimal list (1. / 1.1 / 1.1.1)
    // ══════════════════════════════════════════════════════════════════════════
    public static void CreateMultiLevelDecimalList(
        WordprocessingDocument doc, int abstractNumId = 1, int numId = 2)
    {
        var numberingPart = doc.MainDocumentPart!.NumberingDefinitionsPart
            ?? doc.MainDocumentPart.AddNewPart<NumberingDefinitionsPart>();
        var numbering = numberingPart.Numbering ?? new Numbering();

        var abstractNum = new AbstractNum { AbstractNumberId = abstractNumId };

        // Level 0: 1.
        var level0 = new Level(
            new StartNumberingValue { Val = 1 },
            new NumberingFormat { Val = NumberFormatValues.Decimal },
            new LevelText { Val = "%1." },
            new LevelJustification { Val = LevelJustificationValues.Left },
            new ParagraphProperties(new Indentation { Left = "720", Hanging = "360" }),
            new RunProperties()
        ) { LevelIndex = 0 };

        // Level 1: 1.1
        var level1 = new Level(
            new StartNumberingValue { Val = 1 },
            new NumberingFormat { Val = NumberFormatValues.Decimal },
            new LevelText { Val = "%1.%2." },
            new LevelJustification { Val = LevelJustificationValues.Left },
            new ParagraphProperties(new Indentation { Left = "1440", Hanging = "360" }),
            new RunProperties()
        ) { LevelIndex = 1 };

        // Level 2: 1.1.1
        var level2 = new Level(
            new StartNumberingValue { Val = 1 },
            new NumberingFormat { Val = NumberFormatValues.Decimal },
            new LevelText { Val = "%1.%2.%3." },
            new LevelJustification { Val = LevelJustificationValues.Left },
            new ParagraphProperties(new Indentation { Left = "2160", Hanging = "360" }),
            new RunProperties()
        ) { LevelIndex = 2 };

        abstractNum.AppendChild(level0);
        abstractNum.AppendChild(level1);
        abstractNum.AppendChild(level2);
        numbering.InsertAt(abstractNum, 0);
        numbering.AppendChild(new NumberingInstance(
            new AbstractNumId { Val = abstractNumId }
        ) { NumberID = numId });
        numberingPart.Numbering = numbering;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. Chinese numbering patterns: 一/二/三, （一）/（二）, 1./2./3., (1)/(2)
    // ══════════════════════════════════════════════════════════════════════════
    public static void CreateChineseNumbering(
        WordprocessingDocument doc, int abstractNumId = 2, int numId = 3)
    {
        var numberingPart = doc.MainDocumentPart!.NumberingDefinitionsPart
            ?? doc.MainDocumentPart.AddNewPart<NumberingDefinitionsPart>();
        var numbering = numberingPart.Numbering ?? new Numbering();

        var abstractNum = new AbstractNum { AbstractNumberId = abstractNumId };

        // Level 0: 一、二、三、 (Chinese counting ideograph)
        var level0 = new Level(
            new StartNumberingValue { Val = 1 },
            new NumberingFormat { Val = NumberFormatValues.IdeographTraditional },
            new LevelText { Val = "%1、" },     // "一、"
            new LevelJustification { Val = LevelJustificationValues.Left },
            new ParagraphProperties(new Indentation { Left = "720", Hanging = "420" })
        ) { LevelIndex = 0 };

        // Level 1: （一）（二）（三） (enclosed Chinese)
        var level1 = new Level(
            new StartNumberingValue { Val = 1 },
            new NumberingFormat { Val = NumberFormatValues.IdeographEnclosedCircle },
            new LevelText { Val = "%1、" },     // "（一）"
            new LevelJustification { Val = LevelJustificationValues.Left },
            new ParagraphProperties(new Indentation { Left = "1440", Hanging = "420" })
        ) { LevelIndex = 1 };

        // Level 2: 1. 2. 3. (Arabic decimal)
        var level2 = new Level(
            new StartNumberingValue { Val = 1 },
            new NumberingFormat { Val = NumberFormatValues.Decimal },
            new LevelText { Val = "%1." },
            new LevelJustification { Val = LevelJustificationValues.Left },
            new ParagraphProperties(new Indentation { Left = "2160", Hanging = "360" })
        ) { LevelIndex = 2 };

        // Level 3: (1) (2) (3) (decimal enclosed paren)
        var level3 = new Level(
            new StartNumberingValue { Val = 1 },
            new NumberingFormat { Val = NumberFormatValues.DecimalEnclosedParen },
            new LevelText { Val = "%1" },
            new LevelJustification { Val = LevelJustificationValues.Left },
            new ParagraphProperties(new Indentation { Left = "2880", Hanging = "360" })
        ) { LevelIndex = 3 };

        abstractNum.AppendChild(level0);
        abstractNum.AppendChild(level1);
        abstractNum.AppendChild(level2);
        abstractNum.AppendChild(level3);
        numbering.InsertAt(abstractNum, 0);
        numbering.AppendChild(new NumberingInstance(
            new AbstractNumId { Val = abstractNumId }
        ) { NumberID = numId });
        numberingPart.Numbering = numbering;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. Legal numbering (1. / 1.1 / 1.1.1 with all levels using Arabic)
    // ══════════════════════════════════════════════════════════════════════════
    public static void CreateLegalNumbering(
        WordprocessingDocument doc, int abstractNumId = 3, int numId = 4)
    {
        var numberingPart = doc.MainDocumentPart!.NumberingDefinitionsPart
            ?? doc.MainDocumentPart.AddNewPart<NumberingDefinitionsPart>();
        var numbering = numberingPart.Numbering ?? new Numbering();

        var abstractNum = new AbstractNum { AbstractNumberId = abstractNumId };

        // Legal style: all levels use Arabic numbering regardless of indentation
        for (int i = 0; i < 4; i++)
        {
            var textParts = new string[i + 1];
            for (int j = 0; j <= i; j++) textParts[j] = $"%{j + 1}";
            var levelText = string.Join(".", textParts) + ".";

            var level = new Level(
                new StartNumberingValue { Val = 1 },
                new NumberingFormat { Val = NumberFormatValues.Decimal },
                new LevelText { Val = levelText },
                new LevelJustification { Val = LevelJustificationValues.Left },
                new ParagraphProperties(new Indentation { Left = ((i + 1) * 720).ToString(), Hanging = "360" })
            ) { LevelIndex = i };
            abstractNum.AppendChild(level);
        }

        numbering.InsertAt(abstractNum, 0);
        numbering.AppendChild(new NumberingInstance(
            new AbstractNumId { Val = abstractNumId }
        ) { NumberID = numId });
        numberingPart.Numbering = numbering;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. Restart/continue numbering
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateListParagraph(int numId, int level, string text)
    {
        return new Paragraph(
            new ParagraphProperties(
                new NumberingProperties(
                    new NumberingLevelReference { Val = level },
                    new NumberingId { Val = numId }
                ),
                new Indentation { Left = ((level + 1) * 720).ToString() }
            ),
            new Run(new Text(text))
        );
    }

    // To restart numbering at a specific value, create a new NumberingInstance
    // referencing the same AbstractNum but with a different NumberID.
    public static int RestartNumbering(Numbering numbering, int abstractNumId, int newNumId, int overrideLevel, int startValue)
    {
        var numInstance = new NumberingInstance(
            new AbstractNumId { Val = abstractNumId },
            new LevelOverride(
                new StartOverrideNumberingValue { Val = startValue }
            ) { LevelIndex = overrideLevel }
        ) { NumberID = newNumId };

        numbering.AppendChild(numInstance);
        return newNumId;
    }
}
