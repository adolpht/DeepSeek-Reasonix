package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/nineya/wordZero/pkg/document"
)

// StylePreset defines the document structure and formatting preset.
// Each preset configures cover page, TOC, headers/footers, page margins,
// and default text formatting.
type StylePreset string

const (
	PresetPlain     StylePreset = "plain"     // No extras; plain paragraphs and headings only
	PresetReport    StylePreset = "report"    // Cover page + TOC + headers/footers + page numbers
	PresetContract  StylePreset = "contract"  // Numbered clauses + signature block + tight margins
	PresetMinutes   StylePreset = "minutes"   // Agenda numbering + checklist-style action items
	PresetLetter    StylePreset = "letter"    // Date right-aligned + salutation + body + closing
)

// TableStyle defines how tables should be rendered in the document.
type TableStyle string

const (
	TableStylePlain       TableStyle = "plain"       // No styling; default wordZero table
	TableStyleProfessional TableStyle = "professional" // Bold blue header row + thin borders + auto-fit
	TableStyleAlternating  TableStyle = "alternating"  // Alternating row shading + bold header
)

// validStylePresets lists accepted style_preset values.
var validStylePresets = map[StylePreset]bool{
	PresetPlain:     true,
	PresetReport:    true,
	PresetContract:  true,
	PresetMinutes:   true,
	PresetLetter:    true,
}

// validTableStyles lists accepted table_style values.
var validTableStyles = map[TableStyle]bool{
	TableStylePlain:       true,
	TableStyleProfessional: true,
	TableStyleAlternating:  true,
}

// applyStylePreset modifies the document according to the selected preset.
// It adds structural elements (cover page, TOC, headers/footers) and
// configures page settings before the body content is added.
func applyStylePreset(doc *document.Document, preset StylePreset, title string) error {
	switch preset {
	case PresetPlain:
		// No structural additions. The document is produced as-is from Markdown.
		return nil

	case PresetReport:
		// ── Cover page ──
		if title != "" {
			coverFormat := &document.TextFormat{
				Bold:     true,
				FontSize: 26,
				FontName: "微软雅黑",
			}
			coverPara := doc.AddFormattedParagraph(title, coverFormat)
			coverPara.SetAlignment(document.AlignCenter)

			// Date line beneath the title.
			datePara := doc.AddParagraph(todayDate())
			datePara.SetAlignment(document.AlignCenter)

			// Spacer before TOC.
			doc.AddParagraph("")
		}

		// ── Table of contents ──
		tocConfig := document.DefaultTOCConfig()
		tocConfig.Title = "目录"
		tocConfig.MaxLevel = 3
		doc.GenerateTOC(tocConfig)

		// ── Page break after TOC ──
		// wordZero doesn't have a direct page-break API, so we add an empty
		// paragraph with spacing to visually separate. The TOC generation
		// already inserts a structural break in the OOXML.

		// ── Headers & footers ──
		if title != "" {
			doc.AddHeader(document.HeaderFooterTypeDefault, title)
		}
		doc.AddFooter(document.HeaderFooterTypeDefault, "— $PAGE —", nil)

		// ── Page settings: A4 portrait, 25mm margins ──
		doc.SetPageMargins(25, 25, 25, 25)

		return nil

	case PresetContract:
		// ── Title ──
		if title != "" {
			titleFormat := &document.TextFormat{
				Bold:     true,
				FontSize: 18,
				FontName: "宋体",
			}
			titlePara := doc.AddFormattedParagraph(title, titleFormat)
			titlePara.SetAlignment(document.AlignCenter)
			doc.AddParagraph("")
		}

		// ── Headers & footers ──
		doc.AddHeader(document.HeaderFooterTypeDefault, "合同文件")
		doc.AddFooter(document.HeaderFooterTypeDefault, "第 $PAGE 页", nil)

		// ── Page settings: A4 portrait, 2cm margins ──
		doc.SetPageMargins(20, 20, 20, 20)

		// ── Signature block placeholder ──
		// Added after body content by the caller (see addContractSignatureBlock).

		return nil

	case PresetMinutes:
		// ── Title ──
		if title != "" {
			titleFormat := &document.TextFormat{
				Bold:     true,
				FontSize: 16,
				FontName: "微软雅黑",
			}
			titlePara := doc.AddFormattedParagraph(title, titleFormat)
			titlePara.SetAlignment(document.AlignCenter)
			doc.AddParagraph("")
		}

		// ── Headers & footers ──
		doc.AddHeader(document.HeaderFooterTypeDefault, "会议纪要")
		doc.AddFooter(document.HeaderFooterTypeDefault, "— $PAGE —", nil)

		// ── Page settings: A4 portrait, 25mm margins ──
		doc.SetPageMargins(25, 25, 25, 25)

		return nil

	case PresetLetter:
		// ── Date right-aligned ──
		datePara := doc.AddParagraph(todayDate())
		datePara.SetAlignment(document.AlignRight)
		doc.AddParagraph("")

		// ── Salutation placeholder ──
		doc.AddParagraph("尊敬的收件人：")
		doc.AddParagraph("")

		// ── Page settings: A4 portrait, 25mm margins ──
		doc.SetPageMargins(25, 25, 25, 25)

		// ── Closing + signature ──
		// Added after body content by the caller (see addLetterClosing).

		return nil

	default:
		return fmt.Errorf("unknown style_preset: %q", preset)
	}
}

// addPresetClosing appends closing elements based on the style preset.
// Contract gets a signature block; letter gets a closing section.
// Other presets get no closing additions.
func addPresetClosing(doc *document.Document, preset StylePreset) {
	switch preset {
	case PresetContract:
		addContractSignatureBlock(doc)
	case PresetLetter:
		addLetterClosing(doc)
	}
}

// addContractSignatureBlock appends a signature placeholder section
// at the end of a contract document.
func addContractSignatureBlock(doc *document.Document) {
	doc.AddParagraph("")
	doc.AddParagraph("")

	sigLabel := &document.TextFormat{Bold: true, FontSize: 12, FontName: "宋体"}
	doc.AddFormattedParagraph("甲方（签章）：", sigLabel)
	doc.AddParagraph("")
	doc.AddFormattedParagraph("乙方（签章）：", sigLabel)
	doc.AddParagraph("")
	doc.AddFormattedParagraph("签署日期：", sigLabel)
}

// addLetterClosing appends a closing section at the end of a letter document.
func addLetterClosing(doc *document.Document) {
	doc.AddParagraph("")
	doc.AddParagraph("")
	doc.AddFormattedParagraph("此致", &document.TextFormat{FontName: "宋体", FontSize: 12})
	doc.AddParagraph("")
	doc.AddFormattedParagraph("敬礼", &document.TextFormat{FontName: "宋体", FontSize: 12})
	doc.AddParagraph("")
	doc.AddParagraph("发送人签名")
	doc.AddParagraph("")
}

// applyTableStyle modifies a document.Table according to the selected style.
func applyTableStyle(tbl *document.Table, style TableStyle) {
	switch style {
	case TableStylePlain:
		// No modifications — wordZero default table rendering.
		return

	case TableStyleProfessional:
		// Header row: bold blue background, thin borders, auto-fit.
		cfg := &document.TableStyleConfig{
			Template:          document.TableStyleTemplateGrid,
			FirstRowHeader:    true,
			BandedRows:        false,
			BandedColumns:     false,
			FirstColumnHeader: false,
		}
		tbl.ApplyTableStyle(cfg)

	case TableStyleAlternating:
		// Alternating row shading with bold header.
		cfg := &document.TableStyleConfig{
			Template:          document.TableStyleTemplateRows1,
			FirstRowHeader:    true,
			BandedRows:        true,
			BandedColumns:     false,
			FirstColumnHeader: false,
		}
		tbl.ApplyTableStyle(cfg)

	default:
		// Unknown style — leave table unstyled.
	}
}

// todayDate returns the current date formatted as "YYYY年MM月DD日" for
// Chinese documents. The agent model typically provides date context via
// the content or title, so this is a best-effort default.
func todayDate() string {
	return time.Now().Format("2006年01月02日")
}

// normalizeStylePreset maps common aliases to the canonical preset name.
func normalizeStylePreset(s string) StylePreset {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "none", "default":
		return PresetPlain
	case "报告", "汇报", "周报":
		return PresetReport
	case "合同", "协议":
		return PresetContract
	case "纪要", "会议纪要", "记录":
		return PresetMinutes
	case "信件", "邮件", "函":
		return PresetLetter
	default:
		return StylePreset(s)
	}
}

// normalizeTableStyle maps common aliases to the canonical table style name.
func normalizeTableStyle(s string) TableStyle {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "none", "default":
		return TableStylePlain
	default:
		return TableStyle(s)
	}
}
