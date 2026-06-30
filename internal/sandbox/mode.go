package sandbox

// SandboxMode defines the confinement level for tool execution.
type SandboxMode string

const (
	// SandboxReadOnly means all file system access is read-only and network is blocked.
	// Suitable for code review and suggestions.
	SandboxReadOnly SandboxMode = "read-only"

	// SandboxWorkspaceWrite allows writes only inside the workspace roots and blocks network.
	// This is the default mode for daily coding.
	SandboxWorkspaceWrite SandboxMode = "workspace-write"

	// SandboxFullAccess disables all sandbox restrictions.
	// Use only in trusted environments.
	SandboxFullAccess SandboxMode = "full-access"
)

// IsReadOnly reports whether the mode is read-only.
func (m SandboxMode) IsReadOnly() bool { return m == SandboxReadOnly }

// IsWorkspaceWrite reports whether the mode is workspace-write.
func (m SandboxMode) IsWorkspaceWrite() bool { return m == SandboxWorkspaceWrite }

// IsFullAccess reports whether the mode is full-access.
func (m SandboxMode) IsFullAccess() bool { return m == SandboxFullAccess }

// NormalizeSandboxMode normalizes a sandbox mode string. Empty or unrecognized
// values default to SandboxWorkspaceWrite (safe default).
func NormalizeSandboxMode(s string) SandboxMode {
	switch SandboxMode(s) {
	case SandboxReadOnly, SandboxWorkspaceWrite, SandboxFullAccess:
		return SandboxMode(s)
	default:
		return SandboxWorkspaceWrite
	}
}

// ShouldAllowNetwork reports whether network access should be permitted
// under the given sandbox mode.
func ShouldAllowNetwork(mode SandboxMode) bool {
	return mode == SandboxFullAccess
}

// ShouldAllowWrites reports whether write operations should be permitted
// under the given sandbox mode.
func ShouldAllowWrites(mode SandboxMode) bool {
	return mode == SandboxWorkspaceWrite || mode == SandboxFullAccess
}
