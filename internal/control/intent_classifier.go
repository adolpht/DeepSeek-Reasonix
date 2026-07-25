package control

import (
	"strings"
	"unicode"

	"rexion/internal/skill"
)

// IntentType represents the classified workspace intent.
type IntentType string

const (
	IntentCoding    IntentType = "coding"
	IntentOffice    IntentType = "office"
	IntentAssistant IntentType = "assistant"
	IntentUnknown   IntentType = ""
)

// IntentResult holds the output of intent classification.
type IntentResult struct {
	Intent          IntentType `json:"intent"`
	Confidence      float64    `json:"confidence"`
	MatchedKeywords []string   `json:"matchedKeywords,omitempty"`
	MatchedSkill    string     `json:"matchedSkill,omitempty"`
}

// intentKeywords maps each intent to its trigger keywords (lowercased).
// Keywords are a mix of Chinese and English terms commonly used for each mode.
var intentKeywords = map[IntentType][]string{
	IntentCoding: {
		// Chinese
		"代码", "函数", "编译", "测试", "重构", "调试", "编程", "开发", "实现", "修复",
		"bug", "部署", "接口", "模块", "框架", "库", "依赖", "版本",
		// English
		"code", "function", "compile", "test", "refactor", "debug", "deploy",
		"api", "git", "commit", "branch", "merge", "build", "lint",
		"implement", "fix", "program", "develop", "module", "framework",
	},
	IntentOffice: {
		// Chinese
		"周报", "表格", "ppt", "合同", "纪要", "分析", "文档", "报告", "报表",
		"模板", "报表", "幻灯片", "演示", "word", "excel", "汇报", "总结",
		// English
		"report", "spreadsheet", "presentation", "contract", "minutes",
		"meeting", "doc", "slide", "document", "template", "weekly",
	},
	IntentAssistant: {
		// Chinese
		"帮我查", "提醒我", "整理", "发邮件", "安排", "搜索", "调研", "翻译",
		"总结", "待办", "日程", "计划", "通知", "推荐", "比较",
		// English
		"schedule", "remind", "organize", "email", "search", "research",
		"translate", "summarize", "todo", "calendar", "notify", "recommend",
	},
}

// skillIntentMap maps skill names to their target intent type.
var skillIntentMap = map[string]IntentType{
	"generate-ppt":     IntentOffice,
	"doc-write":        IntentOffice,
	"sheet-create":     IntentOffice,
	"ppt-create":       IntentOffice,
	"research-report":  IntentAssistant,
	"explore":          IntentCoding,
	"review":           IntentCoding,
	"security-review":  IntentCoding,
	"test":             IntentCoding,
	"analyze-project":  IntentCoding,
	"init":             IntentCoding,
}

// ClassifyIntent classifies the user's input into one of three workspace intents
// using a simple keyword-matching rule engine. This is the first-pass classifier
// that does not depend on LLM — fast, deterministic, and sufficient for common cases.
func ClassifyIntent(input string, skills []skill.Skill) IntentResult {
	normalized := normalizeInput(input)
	scores := make(map[IntentType]float64)
	var matchedKeywords []string

	// Score each intent category by keyword hit count.
	for intent, keywords := range intentKeywords {
		for _, kw := range keywords {
			if strings.Contains(normalized, kw) {
				scores[intent]++
				matchedKeywords = append(matchedKeywords, kw)
			}
		}
	}

	// Boost score for skill name / description matches.
	var matchedSkill string
	for _, s := range skills {
		if mapped, ok := skillIntentMap[s.Name]; ok {
			nameLower := strings.ToLower(s.Name)
			// Check if the user mentioned the skill name directly.
			if strings.Contains(normalized, nameLower) {
				scores[mapped] += 2
				matchedSkill = s.Name
				continue
			}
			// Check if the user input overlaps with the skill description keywords.
			descLower := strings.ToLower(s.Description)
			descWords := tokenize(descLower)
			inputWords := tokenize(normalized)
			overlap := 0
			for _, dw := range descWords {
				for _, iw := range inputWords {
					if dw == iw && len(dw) >= 2 {
						overlap++
					}
				}
			}
			if overlap >= 2 {
				scores[mapped] += float64(overlap) * 0.5
				if matchedSkill == "" {
					matchedSkill = s.Name
				}
			}
		}
	}

	// Determine the highest-scoring intent.
	var best IntentType
	var bestScore float64
	for _, intent := range []IntentType{IntentCoding, IntentOffice, IntentAssistant} {
		if scores[intent] > bestScore {
			bestScore = scores[intent]
			best = intent
		}
	}

	// Confidence is the ratio of the best score to (best + second-best).
	// If no keywords matched, return unknown.
	if bestScore == 0 {
		return IntentResult{Intent: IntentUnknown, Confidence: 0}
	}

	// Compute second-best score for confidence.
	var secondBest float64
	for _, intent := range []IntentType{IntentCoding, IntentOffice, IntentAssistant} {
		if intent != best && scores[intent] > secondBest {
			secondBest = scores[intent]
		}
	}

	confidence := bestScore / (bestScore + secondBest + 1)
	if confidence > 1 {
		confidence = 1
	}

	// Deduplicate matched keywords.
	seen := make(map[string]bool)
	var unique []string
	for _, kw := range matchedKeywords {
		if !seen[kw] {
			seen[kw] = true
			unique = append(unique, kw)
		}
	}

	return IntentResult{
		Intent:          best,
		Confidence:      confidence,
		MatchedKeywords: unique,
		MatchedSkill:    matchedSkill,
	}
}

// normalizeInput lowercases and strips punctuation for matching.
func normalizeInput(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(' ')
		}
	}
	return b.String()
}

// tokenize splits a string into lowercase word tokens (length >= 2).
func tokenize(s string) []string {
	var tokens []string
	for _, word := range strings.Fields(s) {
		w := strings.ToLower(strings.TrimFunc(word, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
		if len(w) >= 2 {
			tokens = append(tokens, w)
		}
	}
	return tokens
}
