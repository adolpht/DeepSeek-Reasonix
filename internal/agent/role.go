package agent

// Role defines the behavior profile of a spawned child agent.
type Role struct {
	Name         string   // Role identifier
	Description  string   // When to use this role
	Model        string   // Model override (empty = inherit parent)
	SystemAddon  string   // Additional system prompt instructions
	Tools        []string // Tool whitelist (empty = all except meta-tools)
	ReadOnly     bool     // Restrict to read-only tools
	MaxSteps     int      // Max tool-call rounds (0 = inherit parent)
	SandboxMode  string   // Sandbox mode override
	WorktreePath string   // 子Agent的Git Worktree路径（空则使用主工作区）
}

// BuiltInRoles returns the predefined role definitions.
var BuiltInRoles = map[string]Role{
	"default": {
		Name:        "default",
		Description: "General-purpose sub-agent with full capabilities",
	},
	"worker": {
		Name:        "worker",
		Description: "Execution agent for implementing changes and fixes",
		SystemAddon: "You are a worker agent focused on implementing code changes and fixing issues. Be concise and action-oriented.",
	},
	"explorer": {
		Name:        "explorer",
		Description: "Read-only research agent for code exploration",
		ReadOnly:    true,
		SystemAddon: "You are an explorer agent focused on reading code, searching patterns, and collecting evidence. Do not modify any files.",
	},
	"monitor": {
		Name:        "monitor",
		Description: "Monitoring agent for long-running commands and polling",
		MaxSteps:    50,
		SystemAddon: "You are a monitoring agent that watches long-running commands and polls for status changes. Be patient and check periodically.",
	},
}

// ResolveRole resolves a role name to a Role, falling back to the "default"
// builtin when name is empty or unrecognized. Custom roles override builtins
// with the same name.
func ResolveRole(name string, custom []Role) Role {
	for _, r := range custom {
		if r.Name == name {
			return r
		}
	}
	if r, ok := BuiltInRoles[name]; ok {
		return r
	}
	return BuiltInRoles["default"]
}
