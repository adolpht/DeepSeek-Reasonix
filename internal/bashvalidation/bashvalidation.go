// Package bashvalidation provides command-level semantic validation for the
// bash tool. It is a safety net below the permission Policy: before a bash
// command reaches the policy layer, it passes through this validation pipeline
// which can block, warn, or allow it based on the command's semantic intent.
//
// The pipeline has four stages, run in order:
//
//  1. read-only validation  — block write-like commands when the mode is read-only
//  2. destructive warning   — flag highly destructive commands (rm -rf /, fork bombs, etc.)
//  3. path validation       — detect suspicious path patterns (../, ~/)
//  4. sed validation        — block sed -i in read-only mode
//
// Each stage returns an independent result; the caller combines them to decide
// whether to allow, warn, or block.
package bashvalidation

import (
	"fmt"
	"strings"
)

// PermissionMode represents the active permission level, ordered so that
// ReadOnly < WorkspaceWrite < DangerFullAccess < Allow.
type PermissionMode int

const (
	// ReadOnly blocks all write-like commands.
	ReadOnly PermissionMode = iota
	// WorkspaceWrite allows writes within the workspace.
	WorkspaceWrite
	// DangerFullAccess allows all writes.
	DangerFullAccess
	// Allow is unrestricted.
	Allow
)

// String returns a human-readable name for the mode.
func (m PermissionMode) String() string {
	switch m {
	case ReadOnly:
		return "read-only"
	case WorkspaceWrite:
		return "workspace-write"
	case DangerFullAccess:
		return "danger-full-access"
	case Allow:
		return "allow"
	default:
		return "unknown"
	}
}

// ValidationResult is the outcome of validating a single bash command.
type ValidationResult struct {
	// Allow is true when the command passed validation. When false, Block
	// or Warn contains the reason.
	Allow bool
	// Block is a non-empty reason when the command must be blocked.
	Block string
	// Warn is a non-empty message when the command is allowed but flagged.
	Warn string
}

// allowed is the zero-value pass result.
var allowed = ValidationResult{Allow: true}

// Validate runs the full validation pipeline on a bash command and returns
// the combined result. workspaceRoot is used for path-scope checks; it may
// be empty to skip workspace boundary validation.
func Validate(command string, mode PermissionMode, workspaceRoot string) ValidationResult {
	command = strings.TrimSpace(command)
	if command == "" {
		return allowed
	}

	// Stage 1: read-only validation
	if r := validateReadOnly(command, mode); r.Block != "" {
		return r
	}

	// Stage 2: destructive command warning
	destructiveWarn := checkDestructive(command)

	// Stage 3: path validation
	if r := validatePaths(command, workspaceRoot); r.Block != "" {
		return r
	}

	// Stage 4: sed validation
	if r := validateSed(command, mode); r.Block != "" {
		return r
	}

	// Collect warnings from all stages
	var warns []string
	if destructiveWarn.Warn != "" {
		warns = append(warns, destructiveWarn.Warn)
	}
	if r := validatePaths(command, workspaceRoot); r.Warn != "" {
		warns = append(warns, r.Warn)
	}
	if len(warns) > 0 {
		return ValidationResult{Allow: true, Warn: strings.Join(warns, "; ")}
	}

	return allowed
}

// ---------------------------------------------------------------------------
// Stage 1: readOnlyValidation
// ---------------------------------------------------------------------------

// writeCommands perform filesystem writes and are blocked in read-only mode.
var writeCommands = map[string]bool{
	"cp": true, "mv": true, "rm": true, "mkdir": true, "rmdir": true,
	"touch": true, "chmod": true, "chown": true, "chgrp": true,
	"ln": true, "install": true, "tee": true,
	"truncate": true, "shred": true, "mkfifo": true, "mknod": true, "dd": true,
}

// stateModifyingCommands alter system state beyond the filesystem.
var stateModifyingCommands = map[string]bool{
	"apt": true, "apt-get": true, "yum": true, "dnf": true, "pacman": true,
	"brew": true, "pip": true, "pip3": true, "npm": true, "yarn": true,
	"pnpm": true, "bun": true, "cargo": true, "gem": true, "go": true,
	"rustup": true, "docker": true,
	"systemctl": true, "service": true,
	"mount": true, "umount": true,
	"kill": true, "pkill": true, "killall": true,
	"reboot": true, "shutdown": true, "halt": true, "poweroff": true,
	"useradd": true, "userdel": true, "usermod": true,
	"groupadd": true, "groupdel": true,
	"crontab": true, "at": true,
}

// writeRedirections are shell operators that indicate writes.
var writeRedirections = []string{">", ">>", ">&"}

// containsWriteRedirection checks if a command has write redirections,
// handling the case where > appears as a standalone argument token
// rather than embedded in a larger string.
func containsWriteRedirection(command string) bool {
	fields := strings.Fields(command)
	for _, f := range fields {
		for _, redir := range writeRedirections {
			if f == redir || strings.HasPrefix(f, redir) {
				return true
			}
		}
	}
	return false
}

// containsWriteRedirectionRaw checks the raw command string for redirection
// patterns, used by ClassifyIntent where we need to detect > even when it's
// not a separate token (e.g., "echo hello > file" has > as a standalone token,
// but "echo hello> file" does not).
func containsWriteRedirectionRaw(command string) bool {
	for _, redir := range writeRedirections {
		if strings.Contains(command, redir) {
			return true
		}
	}
	return false
}

// gitReadOnlySubcommands are git subcommands that do not modify state.
var gitReadOnlySubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
	"branch": true, "tag": true, "stash": true,
	"remote": true, "fetch": true,
	"ls-files": true, "ls-tree": true, "cat-file": true,
	"rev-parse": true, "describe": true, "shortlog": true,
	"blame": true, "bisect": true, "reflog": true, "config": true,
}

// validateReadOnly blocks write-like commands when mode is read-only.
func validateReadOnly(command string, mode PermissionMode) ValidationResult {
	if mode != ReadOnly {
		return allowed
	}

	first := extractFirstCommand(command)

	if writeCommands[first] {
		return ValidationResult{Block: fmt.Sprintf("command '%s' modifies the filesystem and is not allowed in read-only mode", first)}
	}
	if stateModifyingCommands[first] {
		return ValidationResult{Block: fmt.Sprintf("command '%s' modifies system state and is not allowed in read-only mode", first)}
	}

	// Recurse into sudo to check the inner command.
	if first == "sudo" {
		inner := extractSudoInner(command)
		if inner != "" {
			return validateReadOnly(inner, mode)
		}
	}

	// Check for write redirections.
	if containsWriteRedirection(command) {
		return ValidationResult{Block: fmt.Sprintf("command contains write redirection which is not allowed in read-only mode")}
	}

	// Check for git commands that modify state.
	if first == "git" {
		return validateGitReadOnly(command)
	}

	return allowed
}

func validateGitReadOnly(command string) ValidationResult {
	parts := strings.Fields(command)
	// Skip past "git" and any flags (e.g., "git -C /path")
	// Some git flags take arguments (e.g., -C <path>), skip those too.
	var subcmd string
	i := 1
	for i < len(parts) {
		if strings.HasPrefix(parts[i], "-") {
			// Skip the flag and its argument if it takes one.
			// Git flags that take arguments: -C, -c, --exec-path, --html-path, etc.
			flag := parts[i]
			i++
			// Known flags that take an argument
			if flag == "-C" || flag == "-c" || strings.HasPrefix(flag, "--exec-path") ||
				strings.HasPrefix(flag, "--html-path") || strings.HasPrefix(flag, "--man-path") ||
				strings.HasPrefix(flag, "--info-path") {
				if i < len(parts) {
					i++
				}
			}
			continue
		}
		subcmd = parts[i]
		break
	}
	if subcmd == "" {
		return allowed // bare "git" is fine
	}
	if gitReadOnlySubcommands[subcmd] {
		return allowed
	}
	return ValidationResult{Block: fmt.Sprintf("git subcommand '%s' modifies repository state and is not allowed in read-only mode", subcmd)}
}

// ---------------------------------------------------------------------------
// Stage 2: destructiveCommandWarning
// ---------------------------------------------------------------------------

// destructivePatterns match commands that can cause irreversible damage.
var destructivePatterns = []struct {
	pattern string
	warning string
}{
	{"rm -rf /", "recursive forced deletion at root — this will destroy the system"},
	{"rm -rf ~", "recursive forced deletion of home directory"},
	{"rm -rf *", "recursive forced deletion of all files in current directory"},
	{"rm -rf .", "recursive forced deletion of current directory"},
	{"rm -r /", "recursive deletion at root — this will destroy the system"},
	{"rm -r ~", "recursive deletion of home directory"},
	{"mkfs", "filesystem creation will destroy existing data on the device"},
	{"dd if=", "direct disk write — can overwrite partitions or devices"},
	{"> /dev/sd", "writing to raw disk device"},
	{"chmod -R 777", "recursively setting world-writable permissions"},
	{"chmod -R 000", "recursively removing all permissions"},
	{":(){ :|:& };:", "fork bomb — will crash the system"},
}

// alwaysDestructiveCommands are commands that are inherently destructive.
var alwaysDestructiveCommands = map[string]bool{
	"shred": true, "wipefs": true,
}

// checkDestructive warns if a command looks destructive but does not block.
func checkDestructive(command string) ValidationResult {
	for _, dp := range destructivePatterns {
		if strings.Contains(command, dp.pattern) {
			return ValidationResult{Allow: true, Warn: "destructive command detected: " + dp.warning}
		}
	}

	first := extractFirstCommand(command)
	if alwaysDestructiveCommands[first] {
		return ValidationResult{Allow: true, Warn: fmt.Sprintf("command '%s' is inherently destructive and may cause data loss", first)}
	}

	// Catch remaining rm -rf / rm -fr patterns.
	if strings.Contains(command, "rm ") && strings.Contains(command, "-r") && strings.Contains(command, "-f") {
		return ValidationResult{Allow: true, Warn: "recursive forced deletion detected — verify the target path is correct"}
	}

	return allowed
}

// ---------------------------------------------------------------------------
// Stage 3: pathValidation
// ---------------------------------------------------------------------------

// systemPaths that should never be targeted by write commands.
var systemPaths = []string{
	"/etc/", "/usr/", "/var/", "/boot/", "/sys/", "/proc/",
	"/dev/", "/sbin/", "/lib/", "/opt/",
}

// validatePaths checks for suspicious path patterns in the command.
func validatePaths(command string, workspaceRoot string) ValidationResult {
	if strings.Contains(command, "../") {
		if workspaceRoot != "" && !strings.Contains(command, workspaceRoot) {
			return ValidationResult{Allow: true, Warn: "command contains directory traversal pattern '../' — verify the target path resolves within the workspace"}
		}
	}

	if strings.Contains(command, "~/") || strings.Contains(command, "$HOME") {
		return ValidationResult{Allow: true, Warn: "command references home directory — verify it stays within the workspace scope"}
	}

	// In workspace-write mode, warn about write commands targeting system paths.
	first := extractFirstCommand(command)
	if writeCommands[first] || stateModifyingCommands[first] {
		for _, sysPath := range systemPaths {
			if strings.Contains(command, sysPath) {
				return ValidationResult{Allow: true, Warn: fmt.Sprintf("command appears to target system path '%s' — requires elevated permission", sysPath)}
			}
		}
	}

	return allowed
}

// ---------------------------------------------------------------------------
// Stage 4: sedValidation
// ---------------------------------------------------------------------------

// validateSed blocks sed -i (in-place editing) in read-only mode.
func validateSed(command string, mode PermissionMode) ValidationResult {
	first := extractFirstCommand(command)
	if first != "sed" {
		return allowed
	}
	if mode == ReadOnly && strings.Contains(command, " -i") {
		return ValidationResult{Block: "sed -i (in-place editing) is not allowed in read-only mode"}
	}
	return allowed
}

// ---------------------------------------------------------------------------
// CommandIntent — semantic classification of a bash command's intent
// ---------------------------------------------------------------------------

// CommandIntent classifies what a command intends to do.
type CommandIntent int

const (
	IntentUnknown           CommandIntent = iota
	IntentReadOnly                        // ls, cat, grep, etc.
	IntentWrite                           // cp, mv, mkdir, touch, tee
	IntentDestructive                     // rm, shred, truncate
	IntentNetwork                         // curl, wget, ssh
	IntentProcessManagement              // kill, pkill
	IntentPackageManagement              // apt, brew, pip, npm
	IntentSystemAdmin                     // sudo, chmod, chown, mount
)

// String returns a human-readable name for the intent.
func (i CommandIntent) String() string {
	switch i {
	case IntentReadOnly:
		return "read-only"
	case IntentWrite:
		return "write"
	case IntentDestructive:
		return "destructive"
	case IntentNetwork:
		return "network"
	case IntentProcessManagement:
		return "process-management"
	case IntentPackageManagement:
		return "package-management"
	case IntentSystemAdmin:
		return "system-admin"
	default:
		return "unknown"
	}
}

// semanticReadOnlyCommands are always read-only.
var semanticReadOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "less": true, "more": true,
	"wc": true, "sort": true, "uniq": true,
	"grep": true, "egrep": true, "fgrep": true, "rg": true,
	"find": true, "locate": true, "which": true, "whereis": true,
	"pwd": true, "whoami": true, "id": true, "uname": true, "hostname": true,
	"date": true, "env": true, "printenv": true,
	"echo": true, "printf": true,
	"diff": true, "cmp": true, "comm": true,
	"stat": true, "file": true, "du": true, "df": true,
	"ps": true, "top": true, "htop": true,
	"man": true, "info": true, "help": true,
	"true": true, "false": true, "test": true,
	"basename": true, "dirname": true, "realpath": true, "readlink": true,
}

// semanticDestructiveCommands are always destructive.
var semanticDestructiveCommands = map[string]bool{
	"rm": true, "shred": true, "truncate": true, "wipefs": true,
}

// semanticNetworkCommands perform network operations.
var semanticNetworkCommands = map[string]bool{
	"curl": true, "wget": true, "ssh": true, "scp": true, "rsync": true,
	"nc": true, "ncat": true, "telnet": true,
}

// semanticProcessCommands manage processes.
var semanticProcessCommands = map[string]bool{
	"kill": true, "pkill": true, "killall": true,
}

// semanticPackageCommands manage packages.
var semanticPackageCommands = map[string]bool{
	"apt": true, "apt-get": true, "yum": true, "dnf": true, "pacman": true,
	"brew": true, "pip": true, "pip3": true, "npm": true, "yarn": true,
	"pnpm": true, "bun": true, "cargo": true, "gem": true,
}

// semanticSystemAdminCommands are system administration.
var semanticSystemAdminCommands = map[string]bool{
	"sudo": true, "chmod": true, "chown": true, "chgrp": true,
	"mount": true, "umount": true, "mkfs": true, "fdisk": true,
	"systemctl": true, "service": true,
	"useradd": true, "userdel": true, "usermod": true,
}

// ClassifyIntent returns the semantic intent of a command.
func ClassifyIntent(command string) CommandIntent {
	command = strings.TrimSpace(command)
	if command == "" {
		return IntentUnknown
	}

	first := extractFirstCommand(command)

	// sudo: classify by the inner command.
	if first == "sudo" {
		inner := extractSudoInner(command)
		if inner != "" {
			innerIntent := ClassifyIntent(inner)
			if innerIntent != IntentUnknown {
				return innerIntent
			}
		}
		return IntentSystemAdmin
	}

	if semanticReadOnlyCommands[first] {
		// A normally-read-only command with a write redirection becomes a write.
		if containsWriteRedirectionRaw(command) {
			return IntentWrite
		}
		return IntentReadOnly
	}
	if semanticDestructiveCommands[first] {
		return IntentDestructive
	}
	if semanticNetworkCommands[first] {
		return IntentNetwork
	}
	if semanticProcessCommands[first] {
		return IntentProcessManagement
	}
	if semanticPackageCommands[first] {
		return IntentPackageManagement
	}
	if semanticSystemAdminCommands[first] {
		return IntentSystemAdmin
	}

	// Write commands: cp, mv, mkdir, touch, tee, dd, etc.
	if writeCommands[first] {
		return IntentWrite
	}

	// git: depends on subcommand.
	if first == "git" {
		parts := strings.Fields(command)
		for _, p := range parts[1:] {
			if !strings.HasPrefix(p, "-") {
				if gitReadOnlySubcommands[p] {
					return IntentReadOnly
				}
				return IntentWrite
			}
		}
	}

	// Check for write redirections — if present, the command is a write.
	if containsWriteRedirectionRaw(command) {
		return IntentWrite
	}

	return IntentUnknown
}

// RequiredMode returns the minimum PermissionMode required for the given intent.
func RequiredMode(intent CommandIntent) PermissionMode {
	switch intent {
	case IntentReadOnly:
		return ReadOnly
	case IntentWrite:
		return WorkspaceWrite
	case IntentDestructive:
		return WorkspaceWrite
	case IntentNetwork:
		return WorkspaceWrite
	case IntentProcessManagement:
		return DangerFullAccess
	case IntentPackageManagement:
		return DangerFullAccess
	case IntentSystemAdmin:
		return DangerFullAccess
	default:
		return WorkspaceWrite // conservative default for unknown
	}
}

// ---------------------------------------------------------------------------
// Lexical path normalization (no filesystem access)
// ---------------------------------------------------------------------------

// IsWithinWorkspace checks whether a path is lexically within the workspace
// root without touching the filesystem. It normalizes . and .. components
// before comparing, so traversal sequences like /workspace/../../etc cannot
// escape via a naive prefix match. Uses forward-slash semantics regardless
// of the host OS so the check is consistent across platforms.
func IsWithinWorkspace(path, workspaceRoot string) bool {
	// Normalize to forward slashes for cross-platform consistency.
	path = strings.ReplaceAll(path, "\\", "/")
	workspaceRoot = strings.ReplaceAll(workspaceRoot, "\\", "/")

	combined := path
	if !isAbsSlash(combined) {
		combined = workspaceRoot + "/" + combined
	}
	normalized := lexicallyNormalize(combined)
	root := lexicallyNormalize(workspaceRoot)
	if root == "" || root == "/" {
		// If workspace root is "/" or empty, everything is within.
		return true
	}
	rootWithSlash := root
	if !strings.HasSuffix(rootWithSlash, "/") {
		rootWithSlash += "/"
	}
	return normalized == root || strings.HasPrefix(normalized, rootWithSlash)
}

// isAbsSlash reports whether a path is absolute using forward-slash convention
// (starts with /), independent of the host OS.
func isAbsSlash(path string) bool {
	return strings.HasPrefix(path, "/")
}

// lexicallyNormalize resolves . and .. components without touching the
// filesystem. It does not resolve symlinks. Uses forward-slash semantics
// regardless of host OS.
func lexicallyNormalize(path string) string {
	parts := strings.Split(strings.ReplaceAll(path, "\\", "/"), "/")
	var out []string
	for _, p := range parts {
		switch p {
		case "", ".":
			// skip
		case "..":
			if len(out) > 0 && out[len(out)-1] != ".." {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return "/"
	}
	result := strings.Join(out, "/")
	if strings.HasPrefix(path, "/") {
		return "/" + result
	}
	return result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractFirstCommand returns the first word of a command, stripping leading
// environment variable assignments (KEY=val) that bash allows before the
// actual command.
func extractFirstCommand(command string) string {
	fields := strings.Fields(command)
	for _, f := range fields {
		// Skip env-var assignments like CC=gcc or PATH=/usr/bin
		if strings.Contains(f, "=") && !strings.HasPrefix(f, "-") {
			continue
		}
		return strings.ToLower(f)
	}
	return ""
}

// extractSudoInner returns the command after "sudo", skipping any sudo flags.
func extractSudoInner(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 || fields[0] != "sudo" {
		return ""
	}
	// Skip sudo and its flags (e.g., sudo -u root, sudo -E)
	start := 1
	for start < len(fields) && (strings.HasPrefix(fields[start], "-") || strings.Contains(fields[start], "=")) {
		// Some flags take an argument (e.g., -u user, -E)
		// Flags that take arguments: -u, -g, -U, -p, -r, -t, -T, -C, -D
		flagVal := fields[start]
		start++
		if !strings.HasPrefix(flagVal, "-E") && !strings.HasPrefix(flagVal, "-e") &&
			!strings.HasPrefix(flagVal, "-i") && !strings.HasPrefix(flagVal, "-K") &&
			!strings.HasPrefix(flagVal, "-l") && !strings.HasPrefix(flagVal, "-n") &&
			!strings.HasPrefix(flagVal, "-S") && !strings.HasPrefix(flagVal, "-v") {
			// Assume this flag takes an argument; skip next field
			if start < len(fields) && !strings.HasPrefix(fields[start], "-") {
				start++
			}
		}
	}
	if start >= len(fields) {
		return ""
	}
	return strings.Join(fields[start:], " ")
}
