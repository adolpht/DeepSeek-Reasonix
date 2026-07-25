package skill

import _ "embed"

// Tier-1 built-in skill bodies, embedded from markdown files.
// These fill high-value capability gaps: goal definition, threat modeling,
// security best practices, screenshot capture, and browser automation.

//go:embed tier1_bodies/define-goal.md
var builtinDefineGoalBody string

//go:embed tier1_bodies/security-threat-model.md
var builtinSecurityThreatModelBody string

//go:embed tier1_bodies/security-best-practices.md
var builtinSecurityBestPracticesBody string

//go:embed tier1_bodies/screenshot.md
var builtinScreenshotBody string

//go:embed tier1_bodies/playwright.md
var builtinPlaywrightBody string

// builtinTier1Skills returns the tier-1 built-in skills: high-value
// capabilities adapted from OpenAI's skill catalog for Rexion.
func builtinTier1Skills() []Skill {
	readCodeTools := append([]string{"read_file", "ls", "glob", "grep"}, extraReadTools...)
	auditTools := append(append([]string(nil), readCodeTools...), "bash", "write_file")
	return []Skill{
		{
			Name:         "define-goal",
			Description:  "将模糊意图转化为可量化的具体目标——明确成果、验证方法、范围边界和停止条件。适用于用户给出不清晰任务时，先定义目标再执行。Runs as a subagent.",
			Body:         builtinDefineGoalBody,
			Scope:        ScopeBuiltin,
			Path:         "(builtin)",
			RunAs:        RunSubagent,
			AllowedTools: append([]string(nil), readCodeTools...),
		},
		{
			Name:         "security-threat-model",
			Description:  "代码库威胁建模——枚举信任边界、资产、攻击者能力、滥用路径和缓解措施，输出 Markdown 威胁模型报告。Runs as a subagent.",
			Body:         builtinSecurityThreatModelBody,
			Scope:        ScopeBuiltin,
			Path:         "(builtin)",
			RunAs:        RunSubagent,
			AllowedTools: append([]string(nil), auditTools...),
		},
		{
			Name:         "security-best-practices",
			Description:  "语言/框架级安全最佳实践审查和改进建议——支持 Python/JavaScript/Go，提供安全编码规范、漏洞检测和修复建议。Runs as a subagent.",
			Body:         builtinSecurityBestPracticesBody,
			Scope:        ScopeBuiltin,
			Path:         "(builtin)",
			RunAs:        RunSubagent,
			AllowedTools: append([]string(nil), auditTools...),
		},
		{
			Name:         "screenshot",
			Description:  "跨平台桌面截图——全屏/窗口/区域截图，支持 macOS/Linux/Windows。使用操作系统原生命令，无需额外依赖。Runs as a subagent.",
			Body:         builtinScreenshotBody,
			Scope:        ScopeBuiltin,
			Path:         "(builtin)",
			RunAs:        RunSubagent,
			AllowedTools: []string{"read_file", "bash", "write_file"},
		},
		{
			Name:         "playwright",
			Description:  "浏览器自动化——通过 Playwright CLI 实现网页导航、表单填写、截图、数据提取、UI 流程调试。Runs as a subagent.",
			Body:         builtinPlaywrightBody,
			Scope:        ScopeBuiltin,
			Path:         "(builtin)",
			RunAs:        RunSubagent,
			AllowedTools: []string{"read_file", "bash", "write_file"},
		},
	}
}
