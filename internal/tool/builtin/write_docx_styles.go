package builtin

import (
	"fmt"
	"strings"

	"github.com/nineya/wordZero/pkg/document"
)

// ── Style preset types ──────────────────────────────────────

// builtinStylePreset defines the document structure and formatting preset.
type builtinStylePreset string

const (
	builtinPresetPlain    builtinStylePreset = "plain"
	builtinPresetReport   builtinStylePreset = "report"
	builtinPresetContract builtinStylePreset = "contract"
	builtinPresetMinutes  builtinStylePreset = "minutes"
	builtinPresetLetter   builtinStylePreset = "letter"
)

var builtinValidStylePresets = map[builtinStylePreset]bool{
	builtinPresetPlain:    true,
	builtinPresetReport:   true,
	builtinPresetContract: true,
	builtinPresetMinutes:  true,
	builtinPresetLetter:   true,
}

// ── Table style types ────────────────────────────────────────

type builtinTableStyle string

const (
	builtinTableStylePlain        builtinTableStyle = "plain"
	builtinTableStyleProfessional builtinTableStyle = "professional"
	builtinTableStyleAlternating   builtinTableStyle = "alternating"
)

var builtinValidTableStyles = map[builtinTableStyle]bool{
	builtinTableStylePlain:        true,
	builtinTableStyleProfessional: true,
	builtinTableStyleAlternating:  true,
}

// ── Normalization ──────────────────────────────────────────

func builtinNormalizeStylePreset(s string) builtinStylePreset {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "none", "default":
		return builtinPresetPlain
	case "报告", "汇报", "周报":
		return builtinPresetReport
	case "合同", "协议":
		return builtinPresetContract
	case "纪要", "会议纪要", "记录":
		return builtinPresetMinutes
	case "信件", "邮件", "函":
		return builtinPresetLetter
	default:
		return builtinStylePreset(s)
	}
}

func builtinNormalizeTableStyle(s string) builtinTableStyle {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "none", "default":
		return builtinTableStylePlain
	default:
		return builtinTableStyle(s)
	}
}

// ── Style preset application ───────────────────────────────

func applyStylePresetBuiltin(doc *document.Document, preset builtinStylePreset, title string) error {
	switch preset {
	case builtinPresetPlain:
		return nil

	case builtinPresetReport:
		if title != "" {
			coverFormat := &document.TextFormat{
				Bold:     true,
				FontSize: 26,
				FontName: "微软雅黑",
			}
			coverPara := doc.AddFormattedParagraph(title, coverFormat)
			coverPara.SetAlignment(document.AlignCenter)
			doc.AddParagraph("")
		}

		tocConfig := document.DefaultTOCConfig()
		tocConfig.Title = "目录"
		tocConfig.MaxLevel = 3
		doc.GenerateTOC(tocConfig)

		if title != "" {
			doc.AddHeader(document.HeaderFooterTypeDefault, title)
		}
		doc.AddFooter(document.HeaderFooterTypeDefault, "— $PAGE —", nil)
		doc.SetPageMargins(25, 25, 25, 25)
		return nil

	case builtinPresetContract:
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
		doc.AddHeader(document.HeaderFooterTypeDefault, "合同文件")
		doc.AddFooter(document.HeaderFooterTypeDefault, "第 $PAGE 页", nil)
		doc.SetPageMargins(20, 20, 20, 20)
		return nil

	case builtinPresetMinutes:
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
		doc.AddHeader(document.HeaderFooterTypeDefault, "会议纪要")
		doc.AddFooter(document.HeaderFooterTypeDefault, "— $PAGE —", nil)
		doc.SetPageMargins(25, 25, 25, 25)
		return nil

	case builtinPresetLetter:
		datePara := doc.AddParagraph("") // Date placeholder; model injects date via content
		datePara.SetAlignment(document.AlignRight)
		doc.AddParagraph("")
		doc.AddParagraph("尊敬的收件人：")
		doc.AddParagraph("")
		doc.SetPageMargins(25, 25, 25, 25)
		return nil

	default:
		return fmt.Errorf("unknown style_preset: %q", preset)
	}
}

func addPresetClosingBuiltin(doc *document.Document, preset builtinStylePreset) {
	switch preset {
	case builtinPresetContract:
		doc.AddParagraph("")
		doc.AddParagraph("")
		sigLabel := &document.TextFormat{Bold: true, FontSize: 12, FontName: "宋体"}
		doc.AddFormattedParagraph("甲方（签章）：", sigLabel)
		doc.AddParagraph("")
		doc.AddFormattedParagraph("乙方（签章）：", sigLabel)
		doc.AddParagraph("")
		doc.AddFormattedParagraph("签署日期：", sigLabel)

	case builtinPresetLetter:
		doc.AddParagraph("")
		doc.AddParagraph("")
		doc.AddFormattedParagraph("此致", &document.TextFormat{FontName: "宋体", FontSize: 12})
		doc.AddParagraph("")
		doc.AddFormattedParagraph("敬礼", &document.TextFormat{FontName: "宋体", FontSize: 12})
		doc.AddParagraph("")
		doc.AddParagraph("发送人签名")
		doc.AddParagraph("")
	}
}

// ── Table style application ────────────────────────────────

func applyTableStyleBuiltin(tbl *document.Table, style builtinTableStyle) {
	switch style {
	case builtinTableStylePlain:
		return
	case builtinTableStyleProfessional:
		cfg := &document.TableStyleConfig{
			Template:          document.TableStyleTemplateGrid,
			FirstRowHeader:    true,
			BandedRows:        false,
			BandedColumns:     false,
			FirstColumnHeader: false,
		}
		tbl.ApplyTableStyle(cfg)
	case builtinTableStyleAlternating:
		cfg := &document.TableStyleConfig{
			Template:          document.TableStyleTemplateRows1,
			FirstRowHeader:    true,
			BandedRows:        true,
			BandedColumns:     false,
			FirstColumnHeader: false,
		}
		tbl.ApplyTableStyle(cfg)
	}
}
