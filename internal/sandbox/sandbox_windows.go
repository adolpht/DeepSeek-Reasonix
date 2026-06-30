//go:build windows

package sandbox

// Command returns the argv to run `command` through sh. On Windows, the OS-level
// sandbox is enforced via Job Object (process group management with kill-on-close)
// rather than command-line wrapping. The actual Job Object assignment happens in
// ConfigureCmd, which the bash tool calls after creating the exec.Cmd.
//
// The second return value reports whether sandboxing is active; the caller uses
// it to decide whether to apply ConfigureCmd.
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	if !spec.enforce() {
		return sh.argv(command), false
	}
	// On Windows, we return the argv as-is; the actual sandbox enforcement
	// happens in ConfigureCmd which sets up the Job Object on the exec.Cmd.
	return sh.argv(command), spec.EffectiveMode() != SandboxFullAccess
}

// Available reports whether an OS sandbox is available on Windows.
// Job Object is always available on Windows, so this always returns true.
func Available() bool { return true }
