// TrackChangesSamples.cs — Revisions: insertions (w:t), deletions (w:delText!),
// formatting changes, accept/reject all, move tracking
// Requires: DocumentFormat.OpenXml 3.2.0
//
// CRITICAL RULES:
//   - <w:ins> uses <w:t> for inserted text
//   - <w:del> uses <w:delText> for deleted text (NEVER <w:t>)
//   - Mixing these up corrupts the document

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class TrackChangesSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Insert revision (tracked insertion)
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateInsertionRun(string author, DateTime date, string text)
    {
        // <w:ins> wraps a <w:r> containing <w:t>
        return new Run(
            new RunProperties(
                new RunStyle { Val = "RevisionInsert" }
            ),
            new InsertedRun(
                new Run(
                    new Text(text) { Space = SpaceProcessingModeValues.Preserve }
                )
            )
            {
                Id = Math.Abs(date.Ticks.GetHashCode()).ToString(),
                Author = author,
                Date = date
            }
        );
    }

    // Insert at paragraph level
    public static Paragraph CreateInsertedParagraph(string author, DateTime date, string text)
    {
        var para = new Paragraph(
            new ParagraphProperties(
                new ParagraphMarkRunProperties(
                    new InsertedRun()  // Mark the paragraph mark as inserted
                    {
                        Id = Math.Abs(date.Ticks.GetHashCode()).ToString(),
                        Author = author,
                        Date = date
                    }
                )
            ),
            new InsertedRun(
                new Run(
                    new Text(text) { Space = SpaceProcessingModeValues.Preserve }
                )
            )
            {
                Id = Math.Abs(date.Ticks.GetHashCode()).ToString(),
                Author = author,
                Date = date
            }
        );
        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Delete revision (tracked deletion)
    // ══════════════════════════════════════════════════════════════════════════
    // CRITICAL: <w:del> uses <w:delText>, NOT <w:t>
    public static Run CreateDeletionRun(string author, DateTime date, string text)
    {
        return new Run(
            new RunProperties(
                new RunStyle { Val = "RevisionDelete" }
            ),
            new DeletedRun(
                new Run(
                    new DeletedText(text) { Space = SpaceProcessingModeValues.Preserve }
                    // ^^^ DeletedText (w:delText), NOT Text (w:t) !!!
                )
            )
            {
                Id = Math.Abs(date.Ticks.GetHashCode()).ToString(),
                Author = author,
                Date = date
            }
        );
    }

    // Delete an entire paragraph
    public static Paragraph CreateDeletedParagraph(string author, DateTime date, string originalText)
    {
        // Deleted paragraph: the paragraph mark is wrapped in <w:del>
        var para = new Paragraph(
            new ParagraphProperties(
                new ParagraphMarkRunProperties(
                    new DeletedRun()  // Mark the paragraph mark as deleted
                    {
                        Id = Math.Abs(date.Ticks.GetHashCode()).ToString(),
                        Author = author,
                        Date = date
                    }
                )
            ),
            new DeletedRun(
                new Run(
                    new DeletedText(originalText) { Space = SpaceProcessingModeValues.Preserve }
                )
            )
            {
                Id = Math.Abs(date.Ticks.GetHashCode()).ToString(),
                Author = author,
                Date = date
            }
        );
        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. Formatting change (tracked format change)
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateFormattingChangeRun(
        string author, DateTime date, string text,
        RunProperties oldFormat, RunProperties newFormat)
    {
        return new Run(
            new RunPropertiesChange(
                oldFormat
            )
            {
                Id = Math.Abs(date.Ticks.GetHashCode()).ToString(),
                Author = author,
                Date = date
            },
            new Run(
                newFormat,
                new Text(text) { Space = SpaceProcessingModeValues.Preserve }
            )
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. Accept all changes (remove revision marks, keep inserted, discard deleted)
    // ══════════════════════════════════════════════════════════════════════════
    public static void AcceptAllChanges(WordprocessingDocument doc)
    {
        var body = doc.MainDocumentPart!.Document.Body!;

        // Remove all inserted run wrappers (keep the content)
        foreach (var ins in body.Elements<InsertedRun>().ToList())
        {
            // Move the child run out of the wrapper
            var run = ins.GetFirstChild<Run>();
            if (run != null)
            {
                ins.InsertAfter(run.CloneNode(true), ins);
            }
            ins.Remove();
        }

        // Remove all deleted runs entirely (discard content)
        foreach (var del in body.Elements<DeletedRun>().ToList())
        {
            del.Remove();
        }

        // Remove paragraph-level insertions
        foreach (var pIns in body.Elements<InsertedParagraph>().ToList())
        {
            pIns.Remove();
        }

        // Remove paragraph-level deletions
        foreach (var pDel in body.Elements<DeletedParagraph>().ToList())
        {
            pDel.Remove();
        }

        // Remove formatting changes
        foreach (var rPrChange in body.Descendants<RunPropertiesChange>().ToList())
        {
            rPrChange.Remove();
        }

        foreach (var pPrChange in body.Descendants<ParagraphPropertiesChange>().ToList())
        {
            pPrChange.Remove();
        }

        // Remove empty paragraphs left behind
        foreach (var para in body.Elements<Paragraph>().ToList())
        {
            if (!para.Elements<Run>().Any() && !para.Elements<Hyperlink>().Any()
                && para.Elements<BookmarkStart>().All(bs => true))  // Keep bookmark-only paras
            {
                // Only remove truly empty paragraphs (no bookmarks or other content)
                if (!para.HasChildren || !para.Elements<BookmarkStart>().Any())
                {
                    para.Remove();
                }
            }
        }

        doc.MainDocumentPart.Document.Save();
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. Reject all changes (remove inserted, keep deleted as plain text)
    // ══════════════════════════════════════════════════════════════════════════
    public static void RejectAllChanges(WordprocessingDocument doc)
    {
        var body = doc.MainDocumentPart!.Document.Body!;

        // Remove all inserted runs entirely
        foreach (var ins in body.Elements<InsertedRun>().ToList())
        {
            ins.Remove();
        }

        // Unwrap deleted runs (restore the text as if never deleted)
        foreach (var del in body.Elements<DeletedRun>().ToList())
        {
            var run = del.GetFirstChild<Run>();
            if (run != null)
            {
                // Convert DeletedText back to Text
                foreach (var delText in run.Elements<DeletedText>().ToList())
                {
                    var text = new Text(delText.Text) { Space = delText.Space };
                    delText.InsertAfter(text, delText);
                    delText.Remove();
                }
                del.InsertAfter(run.CloneNode(true), del);
            }
            del.Remove();
        }

        // Remove formatting changes
        foreach (var rPrChange in body.Descendants<RunPropertiesChange>().ToList())
        {
            rPrChange.Remove();
        }

        doc.MainDocumentPart.Document.Save();
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 6. Move tracking (move from / move to)
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateMoveFromRun(string author, DateTime date, string text)
    {
        // <w:moveFrom> marks text being moved away from this location
        return new Run(
            new MoveFromRun(
                new Run(
                    new Text(text) { Space = SpaceProcessingModeValues.Preserve }
                )
            )
            {
                Id = Math.Abs(date.Ticks.GetHashCode()).ToString(),
                Author = author,
                Date = date
            }
        );
    }

    public static Run CreateMoveToRun(string author, DateTime date, string text)
    {
        // <w:moveTo> marks text being moved to this location
        return new Run(
            new MoveToRun(
                new Run(
                    new Text(text) { Space = SpaceProcessingModeValues.Preserve }
                )
            )
            {
                Id = Math.Abs(date.Ticks.GetHashCode()).ToString(),
                Author = author,
                Date = date
            }
        );
    }

    // Move source/run markers (required for move tracking to work)
    public static void AddMoveSourceMarker(Paragraph paragraph, string moveId)
    {
        paragraph.InsertAt(new MoveSource { Id = moveId }, 0);
    }

    public static void AddMoveDestinationMarker(Paragraph paragraph, string moveId)
    {
        paragraph.InsertAt(new MoveDestination { Id = moveId }, 0);
    }
}
