package bashvalidation

import "testing"

// ---------------------------------------------------------------------------
// Validate (full pipeline)
// ---------------------------------------------------------------------------

func TestValidateAllowsReadOnlyCommand(t *testing.T) {
	r := Validate("ls -la", ReadOnly, "/workspace")
	if !r.Allow || r.Block != "" {
		t.Fatalf("ls should be allowed in read-only mode: got Allow=%v Block=%q", r.Allow, r.Block)
	}
}

func TestValidateBlocksWriteCommandInReadOnly(t *testing.T) {
	r := Validate("rm /tmp/test", ReadOnly, "/workspace")
	if r.Allow || r.Block == "" {
		t.Fatalf("rm should be blocked in read-only mode: got Allow=%v Block=%q", r.Allow, r.Block)
	}
}

func TestValidateWarnsDestructiveCommand(t *testing.T) {
	r := Validate("rm -rf /tmp/test", WorkspaceWrite, "/workspace")
	if !r.Allow {
		t.Fatalf("rm -rf should be allowed (warn only) in workspace-write: got Allow=%v Block=%q", r.Allow, r.Block)
	}
	if r.Warn == "" {
		t.Fatalf("rm -rf should produce a warning in workspace-write mode")
	}
}

func TestValidateBlocksDestructivePatternInReadOnly(t *testing.T) {
	r := Validate("rm -rf /", ReadOnly, "/workspace")
	// In read-only mode, rm -rf is blocked (not just warned).
	if r.Allow {
		t.Fatalf("rm -rf / should be blocked in read-only mode: got Allow=%v Block=%q Warn=%q", r.Allow, r.Block, r.Warn)
	}
}

func TestValidateAllowsWorkspaceWrite(t *testing.T) {
	r := Validate("touch /workspace/file.txt", WorkspaceWrite, "/workspace")
	if !r.Allow || r.Block != "" {
		t.Fatalf("touch within workspace should be allowed in workspace-write mode")
	}
}

func TestValidateWarnsSystemPathTarget(t *testing.T) {
	r := Validate("touch /etc/config", WorkspaceWrite, "/workspace")
	if !r.Allow {
		t.Fatalf("touch /etc should be allowed (warn) in workspace-write mode")
	}
	if r.Warn == "" {
		t.Fatalf("touch /etc should produce a warning about targeting system path")
	}
}

// ---------------------------------------------------------------------------
// Stage 1: validateReadOnly
// ---------------------------------------------------------------------------

func TestReadOnlyBlocksRM(t *testing.T) {
	r := validateReadOnly("rm /tmp/test", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("rm should be blocked in read-only mode")
	}
}

func TestReadOnlyBlocksCP(t *testing.T) {
	r := validateReadOnly("cp a.txt b.txt", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("cp should be blocked in read-only mode")
	}
}

func TestReadOnlyAllowsLS(t *testing.T) {
	r := validateReadOnly("ls -la", ReadOnly)
	if !r.Allow || r.Block != "" {
		t.Fatalf("ls should be allowed in read-only mode")
	}
}

func TestReadOnlyAllowsGitLog(t *testing.T) {
	r := validateReadOnly("git log --oneline", ReadOnly)
	if !r.Allow || r.Block != "" {
		t.Fatalf("git log should be allowed in read-only mode")
	}
}

func TestReadOnlyBlocksGitCommit(t *testing.T) {
	r := validateReadOnly("git commit -m 'fix'", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("git commit should be blocked in read-only mode")
	}
}

func TestReadOnlyBlocksSudo(t *testing.T) {
	r := validateReadOnly("sudo rm /etc/passwd", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("sudo rm should be blocked in read-only mode")
	}
}

func TestReadOnlyBlocksSudoWithFlags(t *testing.T) {
	r := validateReadOnly("sudo -u root chmod 777 /etc", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("sudo chmod should be blocked in read-only mode")
	}
}

func TestReadOnlyBlocksWriteRedirection(t *testing.T) {
	r := validateReadOnly("echo hello > file.txt", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("write redirection should be blocked in read-only mode")
	}
}

func TestReadOnlyBlocksAppendRedirection(t *testing.T) {
	r := validateReadOnly("echo hello >> file.txt", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("append redirection should be blocked in read-only mode")
	}
}

func TestReadOnlyBlocksApt(t *testing.T) {
	r := validateReadOnly("apt install vim", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("apt should be blocked in read-only mode")
	}
}

func TestReadOnlyBlocksDocker(t *testing.T) {
	r := validateReadOnly("docker run ubuntu", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("docker should be blocked in read-only mode")
	}
}

func TestReadOnlyAllowsGitWithFlags(t *testing.T) {
	r := validateReadOnly("git -C /workspace status", ReadOnly)
	if !r.Allow || r.Block != "" {
		t.Fatalf("git -C /workspace status should be allowed in read-only mode")
	}
}

func TestWorkspaceWriteAllowsWriteCommands(t *testing.T) {
	r := validateReadOnly("touch file.txt", WorkspaceWrite)
	if !r.Allow || r.Block != "" {
		t.Fatalf("touch should be allowed in workspace-write mode")
	}
}

func TestDangerFullAccessAllowsEverything(t *testing.T) {
	r := validateReadOnly("rm -rf /", DangerFullAccess)
	if !r.Allow || r.Block != "" {
		t.Fatalf("rm -rf / should be allowed (no block at this stage) in danger-full-access mode")
	}
}

// ---------------------------------------------------------------------------
// Stage 2: checkDestructive
// ---------------------------------------------------------------------------

func TestDestructiveWarnsRmRfRoot(t *testing.T) {
	r := checkDestructive("rm -rf /")
	if !r.Allow || r.Warn == "" {
		t.Fatalf("rm -rf / should produce a warning")
	}
}

func TestDestructiveWarnsRmRfHome(t *testing.T) {
	r := checkDestructive("rm -rf ~")
	if !r.Allow || r.Warn == "" {
		t.Fatalf("rm -rf ~ should produce a warning")
	}
}

func TestDestructiveWarnsForkBomb(t *testing.T) {
	r := checkDestructive(":(){ :|:& };:")
	if !r.Allow || r.Warn == "" {
		t.Fatalf("fork bomb should produce a warning")
	}
}

func TestDestructiveWarnsMkfs(t *testing.T) {
	r := checkDestructive("mkfs.ext4 /dev/sda1")
	if !r.Allow || r.Warn == "" {
		t.Fatalf("mkfs should produce a warning")
	}
}

func TestDestructiveWarnsShred(t *testing.T) {
	r := checkDestructive("shred /tmp/secret.txt")
	if !r.Allow || r.Warn == "" {
		t.Fatalf("shred should produce a warning")
	}
}

func TestDestructiveWarnsDd(t *testing.T) {
	r := checkDestructive("dd if=/dev/zero of=/dev/sda")
	if !r.Allow || r.Warn == "" {
		t.Fatalf("dd if= should produce a warning")
	}
}

func TestDestructiveAllowsSafeCommand(t *testing.T) {
	r := checkDestructive("ls -la")
	if r.Warn != "" {
		t.Fatalf("ls should not produce a destructive warning: got Warn=%q", r.Warn)
	}
}

func TestDestructiveWarnsRmRfGeneral(t *testing.T) {
	r := checkDestructive("rm -rf /tmp/test")
	if !r.Allow || r.Warn == "" {
		t.Fatalf("rm -rf with general target should produce a warning")
	}
}

// ---------------------------------------------------------------------------
// Stage 3: validatePaths
// ---------------------------------------------------------------------------

func TestPathWarnsTraversalOutsideWorkspace(t *testing.T) {
	r := validatePaths("cat ../../etc/passwd", "/workspace")
	if r.Warn == "" {
		t.Fatalf("../ outside workspace should produce a warning")
	}
}

func TestPathWarnsHomeReference(t *testing.T) {
	r := validatePaths("cat ~/secrets.txt", "/workspace")
	if r.Warn == "" {
		t.Fatalf("~/ reference should produce a warning")
	}
}

func TestPathWarnsDollarHome(t *testing.T) {
	r := validatePaths("cat $HOME/secrets.txt", "/workspace")
	if r.Warn == "" {
		t.Fatalf("$HOME reference should produce a warning")
	}
}

func TestPathWarnsSystemPathTarget(t *testing.T) {
	r := validatePaths("touch /etc/config", "/workspace")
	if r.Warn == "" {
		t.Fatalf("touch /etc should warn about system path")
	}
}

func TestPathAllowsNormalCommand(t *testing.T) {
	r := validatePaths("cat /workspace/file.txt", "/workspace")
	if r.Warn != "" || r.Block != "" {
		t.Fatalf("cat within workspace should be clean")
	}
}

// ---------------------------------------------------------------------------
// Stage 4: validateSed
// ---------------------------------------------------------------------------

func TestSedBlocksInPlaceReadOnly(t *testing.T) {
	r := validateSed("sed -i 's/old/new/g' file.txt", ReadOnly)
	if r.Allow || r.Block == "" {
		t.Fatalf("sed -i should be blocked in read-only mode")
	}
}

func TestSedAllowsInPlaceWorkspaceWrite(t *testing.T) {
	r := validateSed("sed -i 's/old/new/g' file.txt", WorkspaceWrite)
	if !r.Allow || r.Block != "" {
		t.Fatalf("sed -i should be allowed in workspace-write mode")
	}
}

func TestSedAllowsNonInPlace(t *testing.T) {
	r := validateSed("sed 's/old/new/g' file.txt", ReadOnly)
	if !r.Allow || r.Block != "" {
		t.Fatalf("non-in-place sed should be allowed in read-only mode")
	}
}

func TestSedAllowsNonSedCommand(t *testing.T) {
	r := validateSed("cat file.txt", ReadOnly)
	if !r.Allow || r.Block != "" {
		t.Fatalf("non-sed command should pass sed validation")
	}
}

// ---------------------------------------------------------------------------
// ClassifyIntent
// ---------------------------------------------------------------------------

func TestClassifyIntentReadOnly(t *testing.T) {
	tests := []struct {
		cmd    string
		intent CommandIntent
	}{
		{"ls -la", IntentReadOnly},
		{"cat file.txt", IntentReadOnly},
		{"grep pattern file", IntentReadOnly},
		{"git log", IntentReadOnly},
		{"git status", IntentReadOnly},
		{"ps aux", IntentReadOnly},
		{"echo hello", IntentReadOnly},
	}
	for _, tt := range tests {
		got := ClassifyIntent(tt.cmd)
		if got != tt.intent {
			t.Errorf("ClassifyIntent(%q) = %v, want %v", tt.cmd, got, tt.intent)
		}
	}
}

func TestClassifyIntentWrite(t *testing.T) {
	tests := []struct {
		cmd    string
		intent CommandIntent
	}{
		{"touch file.txt", IntentWrite},
		{"cp a.txt b.txt", IntentWrite},
		{"mv old.txt new.txt", IntentWrite},
		{"mkdir dir", IntentWrite},
		{"git commit -m fix", IntentWrite},
	}
	for _, tt := range tests {
		got := ClassifyIntent(tt.cmd)
		if got != tt.intent {
			t.Errorf("ClassifyIntent(%q) = %v, want %v", tt.cmd, got, tt.intent)
		}
	}
	// Write redirection: echo hello > file is classified as IntentWrite
	got := ClassifyIntent("echo hello > file.txt")
	if got != IntentWrite {
		t.Errorf("ClassifyIntent('echo hello > file.txt') = %v, want IntentWrite", got)
	}
}

func TestClassifyIntentDestructive(t *testing.T) {
	got := ClassifyIntent("rm /tmp/test")
	if got != IntentDestructive {
		t.Errorf("ClassifyIntent(rm) = %v, want IntentDestructive", got)
	}
}

func TestClassifyIntentNetwork(t *testing.T) {
	got := ClassifyIntent("curl https://example.com")
	if got != IntentNetwork {
		t.Errorf("ClassifyIntent(curl) = %v, want IntentNetwork", got)
	}
}

func TestClassifyIntentProcessManagement(t *testing.T) {
	got := ClassifyIntent("kill -9 1234")
	if got != IntentProcessManagement {
		t.Errorf("ClassifyIntent(kill) = %v, want IntentProcessManagement", got)
	}
}

func TestClassifyIntentPackageManagement(t *testing.T) {
	got := ClassifyIntent("apt install vim")
	if got != IntentPackageManagement {
		t.Errorf("ClassifyIntent(apt) = %v, want IntentPackageManagement", got)
	}
}

func TestClassifyIntentSystemAdmin(t *testing.T) {
	got := ClassifyIntent("chmod 777 file")
	if got != IntentSystemAdmin {
		t.Errorf("ClassifyIntent(chmod) = %v, want IntentSystemAdmin", got)
	}
}

func TestClassifyIntentSudo(t *testing.T) {
	got := ClassifyIntent("sudo rm /etc/passwd")
	if got != IntentDestructive {
		t.Errorf("ClassifyIntent(sudo rm) = %v, want IntentDestructive (inner command)", got)
	}
}

func TestClassifyIntentSudoBare(t *testing.T) {
	got := ClassifyIntent("sudo")
	if got != IntentSystemAdmin {
		t.Errorf("ClassifyIntent(sudo bare) = %v, want IntentSystemAdmin", got)
	}
}

func TestClassifyIntentGoMod(t *testing.T) {
	// "go" is in stateModifyingCommands but not in semanticPackageCommands.
	// ClassifyIntent doesn't have go-specific prefix handling, so it falls through.
	got := ClassifyIntent("go mod tidy")
	if got != IntentUnknown {
		t.Errorf("ClassifyIntent(go mod tidy) = %v, want IntentUnknown (go is state-modifying, not in pkg map)", got)
	}
}

func TestClassifyIntentUnknown(t *testing.T) {
	got := ClassifyIntent("my_custom_tool --flag")
	if got != IntentUnknown {
		t.Errorf("ClassifyIntent(unknown command) = %v, want IntentUnknown", got)
	}
}

// ---------------------------------------------------------------------------
// RequiredMode
// ---------------------------------------------------------------------------

func TestRequiredModeMapping(t *testing.T) {
	tests := []struct {
		intent CommandIntent
		mode   PermissionMode
	}{
		{IntentReadOnly, ReadOnly},
		{IntentWrite, WorkspaceWrite},
		{IntentDestructive, WorkspaceWrite},
		{IntentNetwork, WorkspaceWrite},
		{IntentProcessManagement, DangerFullAccess},
		{IntentPackageManagement, DangerFullAccess},
		{IntentSystemAdmin, DangerFullAccess},
		{IntentUnknown, WorkspaceWrite},
	}
	for _, tt := range tests {
		got := RequiredMode(tt.intent)
		if got != tt.mode {
			t.Errorf("RequiredMode(%v) = %v, want %v", tt.intent, got, tt.mode)
		}
	}
}

// ---------------------------------------------------------------------------
// IsWithinWorkspace
// ---------------------------------------------------------------------------

func TestIsWithinWorkspaceTrue(t *testing.T) {
	tests := []struct {
		path string
		root string
		want bool
	}{
		{"/workspace/file.txt", "/workspace", true},
		{"/workspace/sub/file.txt", "/workspace", true},
		{"file.txt", "/workspace", true},
		{"sub/file.txt", "/workspace", true},
	}
	for _, tt := range tests {
		got := IsWithinWorkspace(tt.path, tt.root)
		if got != tt.want {
			t.Errorf("IsWithinWorkspace(%q, %q) = %v, want %v", tt.path, tt.root, got, tt.want)
		}
	}
}

func TestIsWithinWorkspaceFalse(t *testing.T) {
	tests := []struct {
		path string
		root string
		want bool
	}{
		{"../../etc/passwd", "/workspace", false},
		{"/other/file.txt", "/workspace/project", false},
	}
	for _, tt := range tests {
		got := IsWithinWorkspace(tt.path, tt.root)
		if got != tt.want {
			t.Errorf("IsWithinWorkspace(%q, %q) = %v, want %v", tt.path, tt.root, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// extractFirstCommand / extractSudoInner
// ---------------------------------------------------------------------------

func TestExtractFirstCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"ls -la", "ls"},
		{"sudo rm file", "sudo"},
		{"CC=gcc make", "make"},
		{"PATH=/usr/bin go build", "go"},
		{"", ""},
		{"  git log", "git"},
	}
	for _, tt := range tests {
		got := extractFirstCommand(tt.cmd)
		if got != tt.want {
			t.Errorf("extractFirstCommand(%q) = %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

func TestExtractSudoInner(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"sudo rm file", "rm file"},
		{"sudo -u root chmod 777 /etc", "chmod 777 /etc"},
		{"sudo -E apt install vim", "apt install vim"},
		{"sudo", ""},
		{"cat file", ""},
	}
	for _, tt := range tests {
		got := extractSudoInner(tt.cmd)
		if got != tt.want {
			t.Errorf("extractSudoInner(%q) = %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// lexicallyNormalize
// ---------------------------------------------------------------------------

func TestLexicallyNormalize(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/workspace/sub/file.txt", "/workspace/sub/file.txt"},
		{"/workspace/../etc/passwd", "/etc/passwd"},
		{"/workspace/./file.txt", "/workspace/file.txt"},
		{"workspace/file.txt", "workspace/file.txt"},
		{"/", "/"},
	}
	for _, tt := range tests {
		got := lexicallyNormalize(tt.path)
		if got != tt.want {
			t.Errorf("lexicallyNormalize(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// PermissionMode ordering
// ---------------------------------------------------------------------------

func TestPermissionModeOrdering(t *testing.T) {
	if ReadOnly >= WorkspaceWrite {
		t.Errorf("ReadOnly should be less than WorkspaceWrite")
	}
	if WorkspaceWrite >= DangerFullAccess {
		t.Errorf("WorkspaceWrite should be less than DangerFullAccess")
	}
	if DangerFullAccess >= Allow {
		t.Errorf("DangerFullAccess should be less than Allow")
	}
}
