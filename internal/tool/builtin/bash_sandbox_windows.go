//go:build windows

package builtin

import (
	"fmt"
	"os/exec"

	"rexion/internal/sandbox"
)

// startWithSandbox starts the command and assigns it to a Windows Job Object
// sandbox if the spec enforces. It returns the sandbox handle (which must be
// closed after the command completes) and the start error, if any.
func startWithSandbox(sb sandbox.Spec, cmd *exec.Cmd) (sandbox.CmdSandbox, error) {
	sbx, err := sandbox.ConfigureCmd(sb, cmd)
	if err != nil {
		return nil, fmt.Errorf("configure sandbox: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = sbx.Close()
		return nil, err
	}
	if err := sbx.AssignProcess(cmd); err != nil {
		// Process started but job assignment failed; log and continue — the
		// kill-on-close protection is best-effort, and setKillTree (via Cancel)
		// still works as a fallback.
		_ = sbx.Close()
		return nil, err
	}
	return sbx, nil
}
