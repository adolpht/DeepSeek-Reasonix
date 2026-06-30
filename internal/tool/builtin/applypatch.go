package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/diff"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(applyPatch{}) }

// applyPatch implements the apply_patch tool that applies unified diffs.
type applyPatch struct {
	roots   []string // write confinement roots (set by ConfineWriters)
	workDir string   // directory for resolving relative paths
}

func (applyPatch) Name() string { return "apply_patch" }

func (applyPatch) Description() string {
	return "Apply a unified diff patch to one or more files. " +
		"Supports multi-file patches, fuzzy matching (whitespace tolerance), " +
		"new file creation, and file deletion. " +
		"Prefer this over edit_file for large-scale refactoring or multi-file changes. " +
		"For small, precise edits, prefer edit_file or multi_edit."
}

func (applyPatch) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"patch": {
				"type": "string",
				"description": "Unified diff format patch. Can contain changes to multiple files."
			},
			"fuzzy_match": {
				"type": "boolean",
				"description": "Enable fuzzy matching to tolerate whitespace differences. Default: true."
			}
		},
		"required": ["patch"]
	}`)
}

func (applyPatch) ReadOnly() bool { return false }

func (t applyPatch) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Patch      string `json:"patch"`
		FuzzyMatch *bool  `json:"fuzzy_match"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Patch) == "" {
		return "", fmt.Errorf("patch is required")
	}

	fuzzy := true // default
	if p.FuzzyMatch != nil {
		fuzzy = *p.FuzzyMatch
	}

	// Parse the patch
	ps, err := diff.ParsePatch(p.Patch)
	if err != nil {
		return "", fmt.Errorf("parse patch: %w", err)
	}

	// Validate and confine each file path
	for _, fp := range ps.Files {
		path := diff.ResolvePath(fp.OldPath, "")
		if fp.OldPath == "/dev/null" || fp.OldPath == "" {
			path = diff.ResolvePath(fp.NewPath, "")
		}
		path = resolveIn(t.workDir, path)
		if err := confine(t.roots, path); err != nil {
			return "", err
		}
	}

	// Apply the patch
	result, err := diff.ApplyPatch(ps, diff.ApplyOptions{
		FuzzyMatch: fuzzy,
		RootDir:    t.workDir,
	})
	if err != nil {
		return "", fmt.Errorf("apply patch: %w", err)
	}

	// Format result
	var b strings.Builder
	fmt.Fprintf(&b, "Applied %d/%d hunks across %d file(s)",
		result.HunksApplied, result.HunksTotal, result.FilesPatched)

	if len(result.Errors) > 0 {
		b.WriteString(". Failed hunks:\n")
		for _, e := range result.Errors {
			fmt.Fprintf(&b, "  - %s hunk %d: %s\n", e.File, e.HunkIdx+1, e.Reason)
		}
	}

	return b.String(), nil
}
