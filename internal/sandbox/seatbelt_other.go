//go:build !darwin && !windows

package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
)

// Command wraps the command in a bubblewrap (bwrap) sandbox when spec.Mode is
// "enforce" and bwrap is available. The permission layer still gates the call.
//
// When bwrap is unavailable the command runs unconfined (boot and acp warn
// about this once at startup).
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	if !spec.enforce() {
		return sh.argv(command), false
	}
	bwrap, err := spec.ResolveBwrapPath()
	if err == nil {
		argv := append([]string{bwrap}, bwrapArgs(spec, sh, command)...)
		return argv, true
	}
	// enforce requested but bwrap unavailable — boot/acp already warned at
	// startup; fall back to unconfined (the false result signals "not sandboxed").
	return sh.argv(command), false
}

// Available reports whether an OS sandbox is available on this platform.
// On Linux, this checks for bubblewrap (bwrap) on PATH.
func Available() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

// bwrapArgs builds the bubblewrap command-line arguments based on the effective
// sandbox mode. read-only mounts everything read-only and blocks network;
// workspace-write mounts root read-only with writable workspace roots;
// full-access should not reach here (enforce returns false).
func bwrapArgs(spec Spec, sh Shell, command string) []string {
	mode := spec.EffectiveMode()

	switch mode {
	case SandboxReadOnly:
		// All filesystem read-only, no network
		args := []string{
			"--ro-bind", "/", "/",
			"--dev", "/dev",
			"--proc", "/proc",
			"--tmpfs", "/tmp",
			"--unshare-net",
			"--die-with-parent",
		}
		return append(args, sh.argv(command)...)

	case SandboxWorkspaceWrite:
		// Read-only root + writable workspace roots + cache dirs.
		args := []string{
			"--ro-bind", "/", "/",
			"--dev", "/dev",
			"--proc", "/proc",
			"--die-with-parent",
		}

		// /tmp needs to be writable for builds, temp files, etc.
		args = append(args, "--bind", os.TempDir(), os.TempDir())

		// Network: deny by default, allow if spec.Network is true.
		if !spec.Network {
			args = append(args, "--unshare-net")
		}

		// Writable workspace roots.
		for _, root := range spec.WriteRoots {
			args = append(args, "--bind", root, root)
		}

		// Common toolchain cache directories (matching macOS seatbelt's
		// writeAllowDirs). These are bind-mounted writable so builds, package
		// managers, and language toolchains keep working inside the sandbox.
		for _, dir := range cacheDirs() {
			args = append(args, "--bind", dir, dir)
		}

		return append(args, sh.argv(command)...)

	default:
		// full-access: shouldn't reach here
		return sh.argv(command)
	}
}

// cacheDirs returns the list of common toolchain cache directories that should
// be writable inside the sandbox. Only directories that actually exist on the
// host are included (bwrap fails if the source path doesn't exist).
func cacheDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		for _, sub := range []string{
			".cache",
			".npm",
			".cargo",
			".rustup",
			"go",
			".local/share/go",
			".pip",
			".poetry",
			".conda",
		} {
			dirs = append(dirs, filepath.Join(home, sub))
		}
	}
	// XDG cache directory.
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		dirs = append(dirs, xdg)
	}

	// Filter to only existing directories.
	var out []string
	for _, d := range dirs {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			out = append(out, d)
		}
	}
	return out
}
