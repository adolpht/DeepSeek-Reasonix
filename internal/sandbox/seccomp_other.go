//go:build !linux && !windows

package sandbox

import "os/exec"

// configureCmdOS is the non-Linux, non-Windows stub for ConfigureCmd.
// Seccomp BPF filtering is Linux-only; on macOS and other platforms this
// is a no-op. macOS Seatbelt wraps the command at the argv level.
func configureCmdOS(_ Spec, _ *exec.Cmd) (CmdSandbox, error) {
	return noopSandbox{}, nil
}
