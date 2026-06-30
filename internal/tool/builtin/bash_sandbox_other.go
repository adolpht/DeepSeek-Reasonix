//go:build !windows

package builtin

import (
	"fmt"
	"os/exec"
	"strconv"

	"reasonix/internal/sandbox"
)

// startWithSandbox starts the command. On non-Windows platforms it applies
// sandbox-specific pre-start configuration (e.g. seccomp BPF on Linux) and
// then starts the command. On macOS, ConfigureCmd is a no-op (Seatbelt wraps
// the command at the argv level). On Linux with seccomp enabled, it inserts
// the --seccomp FD argument into the bwrap argv before starting.
func startWithSandbox(sb sandbox.Spec, cmd *exec.Cmd) (sandbox.CmdSandbox, error) {
	sbx, err := sandbox.ConfigureCmd(sb, cmd)
	if err != nil {
		return nil, fmt.Errorf("configure sandbox: %w", err)
	}

	// If seccomp BPF was attached, insert --seccomp FD into the bwrap argv.
	// The fd references a file in cmd.ExtraFiles that contains the BPF program.
	// We insert it right after the bwrap binary path (index 1) so it appears
	// before any other bwrap options and the shell command.
	if fd := sbx.SeccompFD(); fd > 0 {
		seccompArgs := []string{"--seccomp", strconv.Itoa(fd)}
		// cmd.Args[0] is the binary (bwrap path), rest are bwrap options + shell args.
		args := make([]string, 0, len(cmd.Args)+2)
		args = append(args, cmd.Args[0])
		args = append(args, seccompArgs...)
		args = append(args, cmd.Args[1:]...)
		cmd.Args = args
	}

	if err := cmd.Start(); err != nil {
		_ = sbx.Close()
		return nil, err
	}
	return sbx, nil
}
