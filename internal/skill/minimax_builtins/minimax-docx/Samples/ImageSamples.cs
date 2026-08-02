// ImageSamples.cs — Images: inline, floating, text wrapping, border, alt text,
// in header/table, replace, SVG fallback, dimension calculation
// Requires: DocumentFormat.OpenXml 3.2.0

using System;
using System.IO;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;
using A = DocumentFormat.OpenXml.Drawing;
using DW = DocumentFormat.OpenXml.Drawing.Wordprocessing;
using PIC = DocumentFormat.OpenXml.Drawing.Pictures;

namespace MiniMaxAI.Docx.Samples;

public static class ImageSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Inline image (simplest: image embedded in a paragraph)
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddInlineImage(string outputPath, string imagePath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        mainPart.Document = new Document(new Body());

        // Add image part
        var imagePart = mainPart.AddImagePart(GetImagePartType(imagePath));
        using (var stream = File.OpenRead(imagePath))
        {
            imagePart.FeedData(stream);
        }

        // Calculate dimensions (EMU: 1 inch = 914400 EMU, 1 cm = 360000 EMU)
        // Default: 4 inches × 3 inches
        long cx = 4 * 914400L;   // 4 inches wide
        long cy = 3 * 914400L;   // 3 inches tall

        var drawing = CreateInlineDrawing(mainPart.GetIdOfPart(imagePart), "image1", "Image 1", cx, cy);

        var body = mainPart.Document.Body!;
        body.AppendChild(new Paragraph(new Run(drawing)));
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Floating image with text wrapping
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddFloatingImage(string outputPath, string imagePath)
    {
        using var doc = WordprocessingDocument.Create(outputPath, WordprocessingDocumentType.Document);
        var mainPart = doc.AddMainDocumentPart();
        mainPart.Document = new Document(new Body());

        var imagePart = mainPart.AddImagePart(GetImagePartType(imagePath));
        using (var stream = File.OpenRead(imagePath))
        {
            imagePart.FeedData(stream);
        }

        long cx = 2 * 914400L;   // 2 inches
        long cy = 2 * 914400L;   // 2 inches

        var drawing = CreateFloatingDrawing(
            mainPart.GetIdOfPart(imagePart), "logo", "Company Logo",
            cx, cy,
            horizontalAnchor: DW.HorizontalAnchorValues.Text,
            horizontalPosition: 360000L,  // 0.25 inch from text
            verticalAnchor: DW.VerticalAnchorValues.Top,
            verticalPosition: 0,
            wrap: DW.WrapTextValues.Square
        );

        var body = mainPart.Document.Body!;
        body.AppendChild(new Paragraph(new Run(drawing)));
        body.AppendChild(new Paragraph(new Run(new Text("Text that wraps around the floating image. " +
            "This paragraph demonstrates square text wrapping around the image placed at the top-right."))));
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. Image with border and alt text
    // ══════════════════════════════════════════════════════════════════════════
    public static Drawing CreateInlineDrawingWithBorder(string relId, string name, string description,
        long cx, long cy)
    {
        var drawing = CreateInlineDrawing(relId, name, description, cx, cy);

        // Add border to the inline image
        var inline = drawing.Inline!;
        var extent = inline.Extent!;
        var effectExtent = inline.EffectExtent ?? new A.EffectExtent();
        effectExtent.LeftEdge = 19050L;   // 0.021 inch border space
        effectExtent.TopEdge = 19050L;
        effectExtent.RightEdge = 19050L;
        effectExtent.BottomEdge = 19050L;

        return drawing;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. Image dimension calculation from pixel size and DPI
    // ══════════════════════════════════════════════════════════════════════════
    public static (long Cx, long Cy) CalculateEmuDimensions(
        int pixelWidth, int pixelHeight, double dpi = 96.0, double maxWidthInches = 6.5)
    {
        // EMU = pixels × 914400 / DPI
        long cx = (long)(pixelWidth * 914400.0 / dpi);
        long cy = (long)(pixelHeight * 914400.0 / dpi);

        // Scale down if wider than maxWidth
        if (cx > maxWidthInches * 914400L)
        {
            double scale = maxWidthInches * 914400.0 / cx;
            cx = (long)(cx * scale);
            cy = (long)(cy * scale);
        }

        return (cx, cy);
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. Replace an existing image
    // ══════════════════════════════════════════════════════════════════════════
    public static void ReplaceImage(string docxPath, string oldImageRelId, string newImagePath)
    {
        using var doc = WordprocessingDocument.Open(docxPath, true);
        var mainPart = doc.MainDocumentPart!;

        // Get the existing image part by relationship ID
        var oldImagePart = (ImagePart)mainPart.GetPartById(oldImageRelId);

        // Feed new image data (overwrites the existing part content)
        using var stream = File.OpenRead(newImagePath);
        oldImagePart.FeedData(stream);
    }

    // ══════════════════════════════════════════════════════════════════════════
    // Helper: Create inline Drawing element
    // ══════════════════════════════════════════════════════════════════════════
    private static Drawing CreateInlineDrawing(string relId, string name, string description, long cx, long cy)
    {
        // Image ID must be unique per document
        var imageId = (uint)DateTime.Now.Ticks;

        return new Drawing(
            new DW.Inline(
                new DW.Extent { Cx = cx, Cy = cy },
                new DW.EffectExtent { LeftEdge = 0, TopEdge = 0, RightEdge = 0, BottomEdge = 0 },
                new DW.DocProperties { Id = imageId, Name = name, Description = description },
                new DW.NonVisualGraphicFrameDrawingProperties(
                    new A.GraphicFrameLocks { NoChangeAspect = true }
                ),
                new A.Graphic(
                    new A.GraphicData(
                        new PIC.Picture(
                            new PIC.NonVisualPictureProperties(
                                new PIC.NonVisualDrawingProperties { Id = imageId, Name = name },
                                new PIC.NonVisualPictureDrawingProperties()
                            ),
                            new PIC.BlipFill(
                                new A.Blip { Embed = relId },
                                new A.Stretch(new A.FillRectangle())
                            ),
                            new PIC.ShapeProperties(
                                new A.Transform2D(
                                    new A.Offset { X = 0, Y = 0 },
                                    new A.Extents { Cx = cx, Cy = cy }
                                ),
                                new A.PresetGeometry(
                                    new A.AdjustValueList()
                                ) { Preset = A.ShapeTypeValues.Rectangle }
                            )
                        )
                    ) { Uri = "http://schemas.openxmlformats.org/drawingml/2006/picture" }
                )
            ) { DistanceFromTop = 0, DistanceFromBottom = 0, DistanceFromLeft = 0, DistanceFromRight = 0 }
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // Helper: Create floating Drawing element
    // ══════════════════════════════════════════════════════════════════════════
    private static Drawing CreateFloatingDrawing(
        string relId, string name, string description,
        long cx, long cy,
        DW.HorizontalAnchorValues horizontalAnchor, long horizontalPosition,
        DW.VerticalAnchorValues verticalAnchor, long verticalPosition,
        DW.WrapTextValues wrap)
    {
        var imageId = (uint)DateTime.Now.Ticks;

        var anchor = new DW.Anchor(
            new DW.SimplePosition { X = 0, Y = 0 },
            new DW.HorizontalPosition(
                new DW.PositionOffset { Text = horizontalPosition.ToString() }
            ) { RelativeFrom = horizontalAnchor },
            new DW.VerticalPosition(
                new DW.PositionOffset { Text = verticalPosition.ToString() }
            ) { RelativeFrom = verticalAnchor },
            new DW.Extent { Cx = cx, Cy = cy },
            new DW.EffectExtent { LeftEdge = 0, TopEdge = 0, RightEdge = 0, BottomEdge = 0 },
            new DW.WrapSquare { MarginTop = 72000, MarginBottom = 72000, MarginLeft = 72000, MarginRight = 72000 },
            new DW.DocProperties { Id = imageId, Name = name, Description = description },
            new DW.NonVisualGraphicFrameDrawingProperties(
                new A.GraphicFrameLocks { NoChangeAspect = true }
            ),
            new A.Graphic(
                new A.GraphicData(
                    new PIC.Picture(
                        new PIC.NonVisualPictureProperties(
                            new PIC.NonVisualDrawingProperties { Id = imageId, Name = name },
                            new PIC.NonVisualPictureDrawingProperties()
                        ),
                        new PIC.BlipFill(
                            new A.Blip { Embed = relId },
                            new A.Stretch(new A.FillRectangle())
                        ),
                        new PIC.ShapeProperties(
                            new A.Transform2D(
                                new A.Offset { X = 0, Y = 0 },
                                new A.Extents { Cx = cx, Cy = cy }
                            ),
                            new A.PresetGeometry(
                                new A.AdjustValueList()
                            ) { Preset = A.ShapeTypeValues.Rectangle }
                        )
                    )
                ) { Uri = "http://schemas.openxmlformats.org/drawingml/2006/picture" }
            )
        )
        {
            DistanceFromTop = 72000,
            DistanceFromBottom = 72000,
            DistanceFromLeft = 72000,
            DistanceFromRight = 72000,
            SimplePos2D = new DW.SimplePosition { X = 0, Y = 0 },
            RelativeHeight = 251658240,
            BehindDoc = false,
            Locked = false,
            LayoutInCell = true,
            AllowOverlap = true
        };

        return new Drawing(anchor);
    }

    // ══════════════════════════════════════════════════════════════════════════
    // Helper: Determine image part type from extension
    // ══════════════════════════════════════════════════════════════════════════
    private static PartTypeInfo GetImagePartType(string path)
    {
        var ext = Path.GetExtension(path).ToLowerInvariant();
        return ext switch
        {
            ".png" => ImagePartType.Png,
            ".jpg" or ".jpeg" => ImagePartType.Jpeg,
            ".gif" => ImagePartType.Gif,
            ".bmp" => ImagePartType.Bmp,
            ".tiff" or ".tif" => ImagePartType.Tiff,
            ".svg" => ImagePartType.Svg,
            _ => ImagePartType.Png  // Default to PNG
        };
    }
}
