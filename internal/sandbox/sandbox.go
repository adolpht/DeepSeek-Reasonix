// Package sandbox wraps a shell command in an OS-level jail so the model's
// `bash` calls are confined: it may read freely but write only inside the
// workspace (plus temp and toolchain caches) and reach the network only when
// allowed. This is the *enforcement* layer beneath the permission rules
// (*policy*): a permitted command still cannot escape the box.
//
// macOS uses Seatbelt (sandbox-exec); Linux uses bubblewrap (bwrap) with
// optional seccomp BPF filters; on every other OS, or when the OS tooling is
// missing, Command falls back to running the command unwrapped (see Available).
// Confining the in-process file-writer built-ins is handled separately, in
// package tool/builtin.
package sandbox

import "os/exec"

// Spec describes how to confine one command. The zero value (Mode == "") does
// not enforce, so an unconfigured caller runs commands unchanged.
type Spec struct {
	// Mode is "enforce" to wrap the command, anything else (incl. "off" and "")
	// to run it unwrapped. Deprecated: use SandboxMode instead for fine-grained control.
	Mode string
	// SandboxMode is the confinement level: "read-only", "workspace-write", or "full-access".
	// When set, it takes precedence over Mode for determining sandbox behavior.
	// Empty defaults to "workspace-write" when Mode is "enforce".
	SandboxMode SandboxMode
	// WriteRoots are directories the command may write to (the workspace root
	// plus any configured extras). Temp dirs and common toolchain caches are
	// added automatically so builds and package managers keep working.
	WriteRoots []string
	// Network allows network egress from inside the sandbox. Off blocks it so a
	// command cannot exfiltrate or fetch; many dev commands (module/package
	// downloads) need it, so it defaults on at the config layer.
	Network bool
	// BwrapPath overrides the bwrap binary path (Linux only). When empty,
	// bwrap is resolved from PATH. Ignored on non-Linux platforms.
	BwrapPath string
	// Seccomp enables the seccomp BPF filter inside the bubblewrap sandbox
	// (Linux only). The filter blocks dangerous system calls (mount, ptrace,
	// bpf, etc.) in all modes; read-only mode additionally blocks
	// filesystem-modifying calls (creat, chmod, mkdir, etc.). When false,
	// no seccomp filter is applied. Ignored on non-Linux platforms.
	Seccomp bool
}

// enforce reports whether the spec asks for confinement.
func (s Spec) enforce() bool {
	if s.SandboxMode == SandboxFullAccess {
		return false
	}
	return s.Mode == "enforce" || s.SandboxMode != ""
}

// EffectiveMode returns the effective sandbox mode, considering both
// the legacy Mode field and the new SandboxMode field.
func (s Spec) EffectiveMode() SandboxMode {
	if s.SandboxMode != "" {
		return s.SandboxMode
	}
	// Backward compatibility: if Mode is "enforce" and SandboxMode is unset,
	// default to workspace-write.
	if s.Mode == "enforce" {
		return SandboxWorkspaceWrite
	}
	// Mode is "off" or empty — full access.
	return SandboxFullAccess
}

// ResolveBwrapPath returns the bwrap binary path: spec.BwrapPath if set,
// otherwise the result of looking up "bwrap" on PATH. Returns ("", error) if
// bwrap cannot be found.
func (s Spec) ResolveBwrapPath() (string, error) {
	if s.BwrapPath != "" {
		return s.BwrapPath, nil
	}
	return exec.LookPath("bwrap")
}

// CmdSandbox manages the OS-level sandbox resources attached to a running
// command. On Windows it wraps a Job Object; on Linux it may carry a seccomp
// BPF file descriptor. The caller should call AssignProcess after cmd.Start
// succeeds, and Close (or Kill) when the command is done.
type CmdSandbox interface {
	// AssignProcess assigns the process in cmd to the sandbox's OS resource
	// (e.g. a Windows Job Object). Must be called after cmd.Start succeeds.
	AssignProcess(cmd *exec.Cmd) error
	// Close releases the sandbox's OS resources. On Windows with a
	// kill-on-close Job Object, this terminates the process tree.
	Close() error
	// Kill terminates all processes in the sandbox and releases resources.
	Kill() error
	// SeccompFD returns the file descriptor number for bwrap's --seccomp
	// argument, or 0 if no seccomp filter is attached. On Linux with seccomp
	// enabled, this is the fd of the BPF program file passed via ExtraFiles.
	SeccompFD() int
}

// noopSandbox is the CmdSandbox implementation for platforms without
// OS-level sandbox resource management (macOS Seatbelt and Linux bwrap
// wrap the command at the argv level, so no post-start setup is needed).
type noopSandbox struct{}

func (noopSandbox) AssignProcess(_ *exec.Cmd) error { return nil }
func (noopSandbox) Close() error                    { return nil }
func (noopSandbox) Kill() error                     { return nil }
func (noopSandbox) SeccompFD() int                  { return 0 }

// ConfigureCmd applies sandbox-specific settings to an exec.Cmd after it has
// been created but before it is started. On Windows it creates a Job Object
// with kill-on-close; on Linux it may attach a seccomp BPF filter via
// ExtraFiles when spec.Seccomp is true; on macOS it is a no-op (Seatbelt
// wraps the command at the argv level).
//
// The returned CmdSandbox must be used by the caller: call AssignProcess
// after cmd.Start succeeds, and Close or Kill when the command is done.
func ConfigureCmd(spec Spec, cmd *exec.Cmd) (CmdSandbox, error) {
	return configureCmdOS(spec, cmd)
}
