// TableSamples.cs — Tables: borders, grid, cell properties, margins, row height,
// header repeat, merge (H+V), nested, floating, three-line 三线表, zebra striping
// Requires: DocumentFormat.OpenXml 3.2.0

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class TableSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Basic table with borders and grid
    // ══════════════════════════════════════════════════════════════════════════
    public static Table CreateBasicTable(int rows, int cols)
    {
        // Table border style
        var tblBorders = new TableBorders(
            new TopBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
            new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
            new LeftBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
            new RightBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
            new InsideHorizontalBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
            new InsideVerticalBorder { Val = BorderValues.Single, Size = 4, Color = "000000" }
        );

        // Table grid: defines column widths
        var tblGrid = new TableGrid();
        for (int c = 0; c < cols; c++)
        {
            tblGrid.AppendChild(new GridColumn { Width = "2400" });  // ~1.67 inches each
        }

        var table = new Table(
            new TableProperties(
                tblBorders,
                new TableWidth { Width = "5000", Type = TableWidthUnitValues.PctA }
            ),
            tblGrid
        );

        for (int r = 0; r < rows; r++)
        {
            var row = new TableRow();
            for (int c = 0; c < cols; c++)
            {
                row.AppendChild(new TableCell(
                    new TableCellProperties(
                        new TableCellWidth { Width = "2400", Type = TableWidthUnitValues.Dxa }
                    ),
                    new Paragraph(new Run(new Text($"R{r + 1}C{c + 1}")))
                ));
            }
            table.AppendChild(row);
        }

        return table;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Cell margins and row height
    // ══════════════════════════════════════════════════════════════════════════
    public static Table CreateTableWithCellMargins()
    {
        var table = new Table(
            new TableProperties(
                new TableBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideHorizontalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideVerticalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" }
                ),
                // Cell margins for the entire table
                new TableCellMarginDefault(
                    new TopMargin { Width = "108", Type = TableWidthUnitValues.Dxa },    // 0.075"
                    new BottomMargin { Width = "108", Type = TableWidthUnitValues.Dxa },
                    new LeftMargin { Width = "180", Type = TableWidthUnitValues.Dxa },   // 0.125"
                    new RightMargin { Width = "180", Type = TableWidthUnitValues.Dxa }
                )
            ),
            new TableGrid(
                new GridColumn { Width = "3000" },
                new GridColumn { Width = "3000" }
            )
        );

        // Row with exact height
        var row1 = new TableRow(
            new TableRowProperties(
                new TableRowHeight { Val = 720, HeightType = HeightRuleValues.Exact }  // 0.5 inch exact
            ),
            new TableCell(new Paragraph(new Run(new Text("Row 1, Cell 1")))),
            new TableCell(new Paragraph(new Run(new Text("Row 1, Cell 2"))))
        );

        // Row with at-least height
        var row2 = new TableRow(
            new TableRowProperties(
                new TableRowHeight { Val = 360, HeightType = HeightRuleValues.AtLeast }  // Min 0.25 inch
            ),
            new TableCell(new Paragraph(new Run(new Text("Row 2, Cell 1")))),
            new TableCell(new Paragraph(new Run(new Text("Row 2, Cell 2"))))
        );

        table.AppendChild(row1);
        table.AppendChild(row2);
        return table;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. Header row repeat (repeat on each page)
    // ══════════════════════════════════════════════════════════════════════════
    public static Table CreateTableWithHeaderRow()
    {
        var table = new Table(
            new TableProperties(
                new TableBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideHorizontalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideVerticalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" }
                )
            ),
            new TableGrid(
                new GridColumn { Width = "3000" },
                new GridColumn { Width = "3000" }
            )
        );

        // Header row: RepeatHeaderRows = 1
        var headerRow = new TableRow(
            new TableRowProperties(
                new RepeatHeaderRows()
            ),
            CreateHeaderCell("Column A"),
            CreateHeaderCell("Column B")
        );

        table.AppendChild(headerRow);

        // Data rows
        for (int i = 1; i <= 5; i++)
        {
            table.AppendChild(new TableRow(
                new TableCell(new Paragraph(new Run(new Text($"Data {i}-A")))),
                new TableCell(new Paragraph(new Run(new Text($"Data {i}-B"))))
            ));
        }

        return table;
    }

    private static TableCell CreateHeaderCell(string text)
    {
        return new TableCell(
            new TableCellProperties(
                new Shading { Val = ShadingPatternValues.Clear, Fill = "4472C4" }
            ),
            new Paragraph(
                new ParagraphProperties(new Justification { Val = JustificationValues.Center }),
                new Run(
                    new RunProperties(new Bold(), new Color { Val = "FFFFFF" }),
                    new Text(text)
                )
            )
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. Horizontal merge (spanning columns)
    // ══════════════════════════════════════════════════════════════════════════
    public static Table CreateTableWithHorizontalMerge()
    {
        var table = new Table(
            new TableProperties(
                new TableBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideHorizontalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideVerticalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" }
                )
            ),
            new TableGrid(
                new GridColumn { Width = "2000" },
                new GridColumn { Width = "2000" },
                new GridColumn { Width = "2000" }
            )
        );

        // Row 1: single cell spanning all 3 columns
        var row1 = new TableRow();
        row1.AppendChild(new TableCell(
            new TableCellProperties(
                new GridSpan { Val = 3 },  // Span 3 columns
                new TableCellWidth { Width = "6000", Type = TableWidthUnitValues.Dxa }
            ),
            new Paragraph(
                new ParagraphProperties(new Justification { Val = JustificationValues.Center }),
                new Run(new RunProperties(new Bold()), new Text("Full-width header"))
            )
        ));
        table.AppendChild(row1);

        // Row 2: normal 3 cells
        var row2 = new TableRow();
        row2.AppendChild(new TableCell(new Paragraph(new Run(new Text("A")))));
        row2.AppendChild(new TableCell(new Paragraph(new Run(new Text("B")))));
        row2.AppendChild(new TableCell(new Paragraph(new Run(new Text("C")))));
        table.AppendChild(row2);

        return table;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. Vertical merge (spanning rows)
    // ══════════════════════════════════════════════════════════════════════════
    public static Table CreateTableWithVerticalMerge()
    {
        var table = new Table(
            new TableProperties(
                new TableBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideHorizontalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideVerticalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" }
                )
            ),
            new TableGrid(
                new GridColumn { Width = "3000" },
                new GridColumn { Width = "3000" }
            )
        );

        // Row 1: left cell starts vertical merge
        var row1 = new TableRow();
        row1.AppendChild(new TableCell(
            new TableCellProperties(
                new VerticalMerge { Val = MergedCellValues.Restart }  // Start merge
            ),
            new Paragraph(new Run(new Text("Group A")))
        ));
        row1.AppendChild(new TableCell(new Paragraph(new Run(new Text("Item 1")))));
        table.AppendChild(row1);

        // Row 2: left cell continues vertical merge (empty, no text)
        var row2 = new TableRow();
        row2.AppendChild(new TableCell(
            new TableCellProperties(
                new VerticalMerge { Val = MergedCellValues.Continue }  // Continue merge
            ),
            new Paragraph()  // Empty paragraph required
        ));
        row2.AppendChild(new TableCell(new Paragraph(new Run(new Text("Item 2")))));
        table.AppendChild(row2);

        // Row 3: left cell continues vertical merge
        var row3 = new TableRow();
        row3.AppendChild(new TableCell(
            new TableCellProperties(
                new VerticalMerge { Val = MergedCellValues.Continue }
            ),
            new Paragraph()
        ));
        row3.AppendChild(new TableCell(new Paragraph(new Run(new Text("Item 3")))));
        table.AppendChild(row3);

        return table;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 6. Nested table
    // ══════════════════════════════════════════════════════════════════════════
    public static Table CreateNestedTable()
    {
        var innerTable = new Table(
            new TableProperties(
                new TableBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 2, Color = "999999" },
                    new BottomBorder { Val = BorderValues.Single, Size = 2, Color = "999999" },
                    new InsideHorizontalBorder { Val = BorderValues.Single, Size = 2, Color = "999999" },
                    new InsideVerticalBorder { Val = BorderValues.Single, Size = 2, Color = "999999" }
                )
            ),
            new TableGrid(new GridColumn { Width = "2000" }, new GridColumn { Width = "2000" }),
            new TableRow(
                new TableCell(new Paragraph(new Run(new Text("Inner A")))),
                new TableCell(new Paragraph(new Run(new Text("Inner B"))))
            ),
            new TableRow(
                new TableCell(new Paragraph(new Run(new Text("Inner C")))),
                new TableCell(new Paragraph(new Run(new Text("Inner D"))))
            )
        );

        var outerTable = new Table(
            new TableProperties(
                new TableBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
                    new LeftBorder { Val = BorderValues.Single, Size = 4, Color = "000000" },
                    new RightBorder { Val = BorderValues.Single, Size = 4, Color = "000000" }
                )
            ),
            new TableGrid(new GridColumn { Width = "5000" }, new GridColumn { Width = "5000" }),
            new TableRow(
                new TableCell(new Paragraph(new Run(new Text("Outer cell with nested table:")))),
                new TableCell(
                    new Paragraph(),  // Empty paragraph before nested table
                    innerTable,
                    new Paragraph()   // Empty paragraph after nested table
                )
            )
        );

        return outerTable;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 7. Three-line 三线表 style (academic/scientific tables)
    // ══════════════════════════════════════════════════════════════════════════
    // Three-line table: thick top border, thin header-bottom, thick bottom.
    // No vertical borders. Common in Chinese academic papers.
    public static Table CreateThreeLineTable()
    {
        var table = new Table(
            new TableProperties(
                new TableBorders(
                    // Thick top rule (1.5pt = 12 eighth-points)
                    new TopBorder { Val = BorderValues.Single, Size = 12, Color = "000000" },
                    // Thick bottom rule
                    new BottomBorder { Val = BorderValues.Single, Size = 12, Color = "000000" },
                    // No left/right/inside-vertical borders
                    new LeftBorder { Val = BorderValues.None, Size = 0, Color = "auto" },
                    new RightBorder { Val = BorderValues.None, Size = 0, Color = "auto" },
                    new InsideVerticalBorder { Val = BorderValues.None, Size = 0, Color = "auto" },
                    // Thin inside horizontal (0.75pt = 6 eighth-points)
                    new InsideHorizontalBorder { Val = BorderValues.None, Size = 0, Color = "auto" }
                ),
                new TableWidth { Width = "5000", Type = TableWidthUnitValues.PctA }
            ),
            new TableGrid(
                new GridColumn { Width = "2500" },
                new GridColumn { Width = "2500" },
                new GridColumn { Width = "2500" }
            )
        );

        // Header row with bottom border (the middle line of the 三线表)
        var headerRow = new TableRow(
            new TableRowProperties(new RepeatHeaderRows())
        );
        foreach (var header in new[] { "指标", "数值", "单位" })
        {
            headerRow.AppendChild(new TableCell(
                new TableCellProperties(
                    new TableCellBorders(
                        new BottomBorder { Val = BorderValues.Single, Size = 6, Color = "000000" }
                    )
                ),
                new Paragraph(
                    new ParagraphProperties(new Justification { Val = JustificationValues.Center }),
                    new Run(new RunProperties(new Bold()), new Text(header))
                )
            ));
        }
        table.AppendChild(headerRow);

        // Data rows (no borders between)
        for (int i = 1; i <= 3; i++)
        {
            var row = new TableRow();
            row.AppendChild(new TableCell(new Paragraph(new Run(new Text($"指标{i}")))));
            row.AppendChild(new TableCell(new Paragraph(
                new ParagraphProperties(new Justification { Val = JustificationValues.Center }),
                new Run(new Text($"{i * 10}")))));
            row.AppendChild(new TableCell(new Paragraph(new Run(new Text("kg")))));
            table.AppendChild(row);
        }

        return table;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 8. Zebra striping (alternating row colors)
    // ══════════════════════════════════════════════════════════════════════════
    public static Table CreateZebraStripedTable(int rows, int cols)
    {
        var table = new Table(
            new TableProperties(
                new TableBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new LeftBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new RightBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideHorizontalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new InsideVerticalBorder { Val = BorderValues.Single, Size = 4, Color = "auto" }
                )
            ),
            new TableGrid()
        );

        for (int c = 0; c < cols; c++)
        {
            table.TableGrid!.AppendChild(new GridColumn { Width = "2000" });
        }

        for (int r = 0; r < rows; r++)
        {
            var row = new TableRow();
            for (int c = 0; c < cols; c++)
            {
                var cell = new TableCell();
                // Alternate row shading (skip header row 0)
                if (r > 0 && r % 2 == 0)
                {
                    cell.TableCellProperties = new TableCellProperties(
                        new Shading { Val = ShadingPatternValues.Clear, Fill = "F2F2F2" }
                    );
                }
                cell.AppendChild(new Paragraph(new Run(new Text($"R{r + 1}C{c + 1}"))));
                row.AppendChild(cell);
            }
            table.AppendChild(row);
        }

        return table;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 9. Floating table (text wrapping around table)
    // ══════════════════════════════════════════════════════════════════════════
    public static Table CreateFloatingTable()
    {
        var table = new Table(
            new TableProperties(
                new TableBorders(
                    new TopBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new BottomBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new LeftBorder { Val = BorderValues.Single, Size = 4, Color = "auto" },
                    new RightBorder { Val = BorderValues.Single, Size = 4, Color = "auto" }
                ),
                new TableWidth { Width = "3000", Type = TableWidthUnitValues.Dxa },
                new TablePositionProperties
                {
                    HorizontalAnchor = HorizontalAnchorValues.Text,
                    VerticalAnchor = VerticalAnchorValues.Text,
                    TablePositionX = 360,    // 0.25" from text
                    TablePositionY = 0
                }
            ),
            new TableGrid(
                new GridColumn { Width = "3000" }
            ),
            new TableRow(
                new TableCell(new Paragraph(new Run(new Text("Floating table cell"))))
            )
        );

        return table;
    }
}
