// FootnoteAndCommentSamples.cs — Footnotes, endnotes, comments (4-file system),
// bookmarks, hyperlinks (internal + external)
// Requires: DocumentFormat.OpenXml 3.2.0

using System;
using DocumentFormat.OpenXml;
using DocumentFormat.OpenXml.Packaging;
using DocumentFormat.OpenXml.Wordprocessing;

namespace MiniMaxAI.Docx.Samples;

public static class FootnoteAndCommentSamples
{
    // ══════════════════════════════════════════════════════════════════════════
    // 1. Add a footnote
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddFootnote(WordprocessingDocument doc, Paragraph targetParagraph, string footnoteText)
    {
        var mainPart = doc.MainDocumentPart!;
        var footnotePart = mainPart.FootnotesPart ?? mainPart.AddNewPart<FootnotesPart>();

        if (footnotePart.Footnotes == null)
            footnotePart.Footnotes = new Footnotes();

        // Generate footnote reference ID
        int footnoteId = footnotePart.Footnotes.Elements<Footnote>()
            .Select(f => f.Id?.Value ?? 0)
            .DefaultIfEmpty(0)
            .Max() + 1;

        // Add footnote content
        footnotePart.Footnotes.AppendChild(new Footnote(
            new Paragraph(
                new ParagraphProperties(
                    new ParagraphStyleId { Val = "FootnoteText" }
                ),
                new Run(
                    new RunProperties(
                        new RunStyle { Val = "FootnoteReference" }
                    ),
                    new FootnoteReferenceMark()
                ),
                new Run(
                    new RunProperties(
                        new RunStyle { Val = "FootnoteText" }
                    ),
                    new Text(footnoteText)
                    { Space = SpaceProcessingModeValues.Preserve }
                )
            )
        ) { Id = footnoteId, Type = FootnoteEndnoteValues.Normal });

        // Add reference in the target paragraph
        var run = new Run(
            new RunProperties(
                new RunStyle { Val = "FootnoteReference" }
            ),
            new FootnoteReference { Id = footnoteId }
        );
        targetParagraph.AppendChild(run);
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 2. Add an endnote
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddEndnote(WordprocessingDocument doc, Paragraph targetParagraph, string endnoteText)
    {
        var mainPart = doc.MainDocumentPart!;
        var endnotePart = mainPart.EndnotesPart ?? mainPart.AddNewPart<EndnotesPart>();

        if (endnotePart.Endnotes == null)
            endnotePart.Endnotes = new Endnotes();

        int endnoteId = endnotePart.Endnotes.Elements<Endnote>()
            .Select(e => e.Id?.Value ?? 0)
            .DefaultIfEmpty(0)
            .Max() + 1;

        endnotePart.Endnotes.AppendChild(new Endnote(
            new Paragraph(
                new ParagraphProperties(
                    new ParagraphStyleId { Val = "EndnoteText" }
                ),
                new Run(
                    new RunProperties(new RunStyle { Val = "EndnoteReference" }),
                    new EndnoteReferenceMark()
                ),
                new Run(
                    new RunProperties(new RunStyle { Val = "EndnoteText" }),
                    new Text(endnoteText) { Space = SpaceProcessingModeValues.Preserve }
                )
            )
        ) { Id = endnoteId, Type = FootnoteEndnoteValues.Normal });

        var run = new Run(
            new RunProperties(new RunStyle { Val = "EndnoteReference" }),
            new EndnoteReference { Id = endnoteId }
        );
        targetParagraph.AppendChild(run);
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 3. Add a comment (requires 4 parts: comments, commentsExtended,
    //    commentsIds, commentsExtensible)
    // ══════════════════════════════════════════════════════════════════════════
    public static void AddComment(WordprocessingDocument doc, string author, string commentText,
        int commentId, DateTime date)
    {
        var mainPart = doc.MainDocumentPart!;
        var commentsPart = mainPart.WordprocessingCommentsPart
            ?? mainPart.AddNewPart<WordprocessingCommentsPart>();

        if (commentsPart.Comments == null)
            commentsPart.Comments = new Comments();

        commentsPart.Comments.AppendChild(new Comment(
            new Paragraph(
                new ParagraphProperties(
                    new ParagraphStyleId { Val = "CommentText" }
                ),
                new Run(new Text(commentText))
            )
        )
        {
            Id = commentId.ToString(),
            Author = author,
            Date = date,
            Initials = author.Substring(0, Math.Min(2, author.Length))
        });

        commentsPart.Comments.Save();
    }

    // Mark a range of runs as a comment anchor
    public static void AddCommentAnchor(Paragraph paragraph, int commentId)
    {
        // CommentRangeStart before the content
        paragraph.InsertAt(new CommentRangeStart { Id = commentId.ToString() }, 0);

        // CommentRangeEnd + CommentReference after the content
        paragraph.AppendChild(new CommentRangeEnd { Id = commentId.ToString() });
        var refRun = new Run(
            new RunProperties(new RunStyle { Val = "CommentReference" }),
            new CommentReference { Id = commentId.ToString() }
        );
        paragraph.AppendChild(refRun);
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 4. Bookmark (internal cross-reference target)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateBookmarkedParagraph(string bookmarkName, string text)
    {
        var para = new Paragraph();

        // BookmarkStart at the beginning
        para.AppendChild(new BookmarkStart { Name = bookmarkName, Id = bookmarkName.GetHashCode().ToString() });

        // Content
        para.AppendChild(new Run(new Text(text)));

        // BookmarkEnd at the end
        para.AppendChild(new BookmarkEnd { Id = bookmarkName.GetHashCode().ToString() });

        return para;
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 5. External hyperlink
    // ══════════════════════════════════════════════════════════════════════════
    public static Run CreateExternalHyperlinkRun(WordprocessingDocument doc, string url, string displayText)
    {
        var mainPart = doc.MainDocumentPart!;

        // Create relationship to the URL
        var relId = mainPart.AddHyperlinkRelationship(new Uri(url, UriKind.Absolute), true).Id;

        // Hyperlink element wraps runs
        var hyperlink = new Hyperlink(
            new Run(
                new RunProperties(
                    new RunStyle { Val = "Hyperlink" },
                    new Color { Val = "0563C1" },
                    new Underline { Val = UnderlineValues.Single }
                ),
                new Text(displayText)
            )
        ) { Id = relId };

        // Note: Hyperlinks go in the paragraph, not as a Run.
        // This method returns a wrapper — caller should use CreateExternalHyperlink instead.
        // See CreateExternalHyperlinkParagraph for the correct usage.
        throw new InvalidOperationException("Use CreateExternalHyperlinkParagraph instead.");
    }

    // Correct: Create a paragraph containing a hyperlink
    public static Paragraph CreateExternalHyperlinkParagraph(
        WordprocessingDocument doc, string url, string displayText)
    {
        var mainPart = doc.MainDocumentPart!;
        var relId = mainPart.AddHyperlinkRelationship(new Uri(url, UriKind.Absolute), true).Id;

        return new Paragraph(
            new Hyperlink(
                new Run(
                    new RunProperties(
                        new RunStyle { Val = "Hyperlink" },
                        new Color { Val = "0563C1" },
                        new Underline { Val = UnderlineValues.Single }
                    ),
                    new Text(displayText)
                )
            ) { Id = relId }
        );
    }

    // ══════════════════════════════════════════════════════════════════════════
    // 6. Internal hyperlink (to a bookmark within the document)
    // ══════════════════════════════════════════════════════════════════════════
    public static Paragraph CreateInternalHyperlinkParagraph(string bookmarkName, string displayText)
    {
        return new Paragraph(
            new Hyperlink(
                new Run(
                    new RunProperties(
                        new RunStyle { Val = "Hyperlink" },
                        new Color { Val = "0563C1" },
                        new Underline { Val = UnderlineValues.Single }
                    ),
                    new Text(displayText)
                )
            ) { Anchor = bookmarkName }
        );
    }
}
