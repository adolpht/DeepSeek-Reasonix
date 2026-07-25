package skill

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed minimax_builtins/minimax-docx minimax_builtins/minimax-xlsx minimax_builtins/minimax-pdf minimax_builtins/pptx-generator
var minimaxEmbedFS embed.FS

// MiniMaxBuiltinNames returns the names of embedded MiniMax office skills.
func MiniMaxBuiltinNames() []string {
	return []string{"minimax-docx", "minimax-xlsx", "minimax-pdf", "pptx-generator"}
}

// MaterializeMiniMaxBuiltins writes embedded MiniMax skills to the global
// skill directory (~/.rexion/skills/). Existing skills are never overwritten,
// so the operation is idempotent — only skills that lack a SKILL.md on disk
// are materialized. This includes SKILL.md, references/, templates/, design/,
// and scripts/ subdirectories.
func (s *Store) MaterializeMiniMaxBuiltins() {
	if s.disableBuiltins {
		return
	}
	globalDir := filepath.Join(s.homeDir, ".rexion", SkillsDirname)
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		fmt.Fprintf(s.stderr, "warning: could not create global skills dir %s: %v\n", globalDir, err)
		return
	}
	for _, name := range MiniMaxBuiltinNames() {
		if s.disabledName(name) {
			continue
		}
		targetDir := filepath.Join(globalDir, name)
		// Skip if already exists (user may have customized or a later version is installed).
		if _, err := os.Stat(filepath.Join(targetDir, SkillFile)); err == nil {
			continue
		}
		if err := s.materializeEmbeddedSkill(name, targetDir); err != nil {
			fmt.Fprintf(s.stderr, "warning: could not materialize MiniMax skill %q: %v\n", name, err)
		}
	}
}

// materializeEmbeddedSkill extracts one embedded skill directory tree to targetDir.
func (s *Store) materializeEmbeddedSkill(name, targetDir string) error {
	embedDir := "minimax_builtins/" + name
	return fs.WalkDir(minimaxEmbedFS, embedDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Compute the relative path within the skill directory.
		relPath := strings.TrimPrefix(path, embedDir+"/")
		if relPath == embedDir {
			// Root entry — just ensure the target directory exists.
			return os.MkdirAll(targetDir, 0o755)
		}
		targetPath := filepath.Join(targetDir, relPath)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		// Skip if file already exists (idempotent — never overwrite).
		if _, err := os.Stat(targetPath); err == nil {
			return nil
		}
		data, err := fs.ReadFile(minimaxEmbedFS, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o644)
	})
}
