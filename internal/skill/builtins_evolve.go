package skill

import _ "embed"

// evolve 内置 skill 的 body 内容，通过 go:embed 嵌入，保持原始 Markdown 格式。
//
//go:embed evolve_bodies/evolve.md
var builtinEvolveBody string

//go:embed evolve_bodies/evolve_pm.md
var builtinEvolvePMBody string

//go:embed evolve_bodies/evolve_worker.md
var builtinEvolveWorkerBody string

//go:embed evolve_bodies/evolve_reviewer.md
var builtinEvolveReviewerBody string

// builtinEvolveSkills 返回进化任务引擎的四个内置 skill：
//   - evolve          (inline)   项目组协调者，编排 PM→Worker→Reviewer 流程
//   - evolve-pm       (subagent) 将模糊目标拆解为可验收子任务清单 + DoD
//   - evolve-worker   (subagent) 执行子任务并产出可验收交付物
//   - evolve-reviewer (subagent) 按 DoD 逐条验收，返回 pass/fail + 反馈
func builtinEvolveSkills() []Skill {
	return []Skill{
		{
			Name:        "evolve",
			Description: "进化任务引擎——给Agent一个模糊目标，通过项目组协作（PM拆解→Worker执行→Reviewer验收循环）达成可交付结果。主Agent扮演协调者，调用evolve-pm/evolve-worker/evolve-reviewer三个子Agent完成分工协作。",
			Body:        builtinEvolveBody,
			Scope:       ScopeBuiltin,
			Path:        "(builtin)",
			RunAs:       RunInline,
		},
		{
			Name:         "evolve-pm",
			Description:  "进化任务PM角色——将模糊目标拆解为可执行、可验收的子任务清单，每个子任务附带明确的DoD（验收标准）。只读调查，不执行修改。",
			Body:         builtinEvolvePMBody,
			Scope:        ScopeBuiltin,
			Path:         "(builtin)",
			RunAs:        RunSubagent,
			AllowedTools: []string{"read_file", "grep", "glob", "ls"},
		},
		{
			Name:        "evolve-worker",
			Description: "进化任务Worker角色——执行具体子任务并产出可验收的交付物。拥有完整的工具访问权限（文件读写、bash、编辑等），可以修改代码、运行命令、创建文件。",
			Body:        builtinEvolveWorkerBody,
			Scope:       ScopeBuiltin,
			Path:        "(builtin)",
			RunAs:       RunSubagent,
		},
		{
			Name:         "evolve-reviewer",
			Description:  "进化任务Reviewer角色——根据DoD逐条验收Worker的产出，返回pass/fail+反馈的严格JSON。只读验证，可运行测试/检查命令。",
			Body:         builtinEvolveReviewerBody,
			Scope:        ScopeBuiltin,
			Path:         "(builtin)",
			RunAs:        RunSubagent,
			AllowedTools: []string{"read_file", "grep", "glob", "ls", "bash"},
		},
	}
}
