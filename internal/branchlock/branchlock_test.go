package branchlock

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// setupTestRepo creates a temporary git repo with an initial commit and
// returns its path. The caller is responsible for cleaning up.
func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, cmd := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		if err := exec.Command(cmd[0], append([]string{"-C", dir}, cmd[1:]...)...).Run(); err != nil {
			t.Fatalf("git init: %v", err)
		}
	}
	// Create an initial commit.
	initial := filepath.Join(dir, "README.md")
	if err := os.WriteFile(initial, []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("write initial file: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "commit", "-m", "init").Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLockDisabledWhenNoGit(t *testing.T) {
	dir := t.TempDir() // not a git repo
	l := New(dir)
	if l.IsEnabled() {
		t.Error("Lock should be disabled in non-git directory")
	}
	// All operations should be no-ops.
	l.Record("foo.txt")
	if err := l.Verify("foo.txt"); err != nil {
		t.Errorf("Verify on disabled lock should return nil, got %v", err)
	}
}

func TestLockRecordAndVerifyNoChange(t *testing.T) {
	dir := setupTestRepo(t)
	l := New(dir)
	if !l.IsEnabled() {
		t.Fatal("Lock should be enabled in git repo")
	}
	file := filepath.Join(dir, "src.go")
	writeFile(t, file, "package main\n")
	l.Record("src.go")

	// No external change — Verify should pass.
	if err := l.Verify("src.go"); err != nil {
		t.Errorf("Verify should pass when file unchanged: %v", err)
	}
}

func TestLockDetectsExternalChange(t *testing.T) {
	dir := setupTestRepo(t)
	l := New(dir)
	file := filepath.Join(dir, "config.yaml")
	writeFile(t, file, "key: value\n")
	l.Record("config.yaml")

	// Simulate external modification.
	writeFile(t, file, "key: changed\n")

	err := l.Verify("config.yaml")
	if err == nil {
		t.Fatal("Verify should detect external change")
	}
	if _, ok := err.(*Conflict); !ok {
		t.Errorf("Error should be *Conflict, got %T", err)
	}
}

func TestLockRecordAfterWriteResets(t *testing.T) {
	dir := setupTestRepo(t)
	l := New(dir)
	file := filepath.Join(dir, "data.txt")
	writeFile(t, file, "v1\n")
	l.Record("data.txt")

	// Agent writes a new version.
	writeFile(t, file, "v2\n")
	// Re-record after write.
	l.Record("data.txt")

	// Verify should pass — the baseline is now v2.
	if err := l.Verify("data.txt"); err != nil {
		t.Errorf("Verify after re-record should pass: %v", err)
	}
}

func TestLockVerifyUnrecordedFile(t *testing.T) {
	dir := setupTestRepo(t)
	l := New(dir)
	// File was never recorded — Verify should be nil (no baseline).
	if err := l.Verify("unseen.txt"); err != nil {
		t.Errorf("Verify on unrecorded file should return nil, got %v", err)
	}
}

func TestLockForget(t *testing.T) {
	dir := setupTestRepo(t)
	l := New(dir)
	file := filepath.Join(dir, "temp.txt")
	writeFile(t, file, "temp\n")
	l.Record("temp.txt")
	l.Forget("temp.txt")

	// After Forget, Verify should return nil (no baseline).
	writeFile(t, file, "changed\n")
	if err := l.Verify("temp.txt"); err != nil {
		t.Errorf("Verify after Forget should return nil, got %v", err)
	}
}

func TestLockReset(t *testing.T) {
	dir := setupTestRepo(t)
	l := New(dir)
	file := filepath.Join(dir, "a.txt")
	writeFile(t, file, "a\n")
	l.Record("a.txt")
	l.Reset()

	// After Reset, Verify should return nil.
	writeFile(t, file, "changed\n")
	if err := l.Verify("a.txt"); err != nil {
		t.Errorf("Verify after Reset should return nil, got %v", err)
	}
}

func TestConflictError(t *testing.T) {
	c := &Conflict{Path: "foo.go", OldHash: "abc123", NewHash: "def456"}
	s := c.Error()
	if s == "" {
		t.Error("Conflict.Error() should not be empty")
	}
	if !contains(s, "foo.go") {
		t.Errorf("Conflict.Error() should mention path: %s", s)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
