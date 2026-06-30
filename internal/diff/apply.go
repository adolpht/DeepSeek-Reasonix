package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ApplyResult records the outcome of applying a patch.
type ApplyResult struct {
	FilesPatched int
	HunksApplied int
	HunksTotal   int
	Errors       []HunkError
}

// HunkError records a hunk that failed to apply.
type HunkError struct {
	File    string
	HunkIdx int
	Reason  string
}

// ApplyOptions controls patch application behavior.
type ApplyOptions struct {
	FuzzyMatch bool   // enable fuzzy matching (whitespace tolerance)
	RootDir    string // root directory for resolving paths
}

// ApplyPatch applies a PatchSet to the filesystem.
// It applies hunks atomically per file: all hunks must succeed for a file,
// or none are written.
func ApplyPatch(ps *PatchSet, opts ApplyOptions) (*ApplyResult, error) {
	var result ApplyResult

	for _, fp := range ps.Files {
		newResult, err := applyFilePatch(fp, opts)
		if err != nil {
			return nil, err
		}
		result.FilesPatched += newResult.FilesPatched
		result.HunksApplied += newResult.HunksApplied
		result.HunksTotal += newResult.HunksTotal
		result.Errors = append(result.Errors, newResult.Errors...)
	}

	return &result, nil
}

// applyFilePatch applies all hunks for a single file atomically.
// If any hunk fails, the file is not written.
func applyFilePatch(fp FilePatch, opts ApplyOptions) (*ApplyResult, error) {
	result := &ApplyResult{HunksTotal: len(fp.Hunks)}

	// Handle new file creation
	if fp.OldPath == "/dev/null" || fp.OldPath == "" {
		var content strings.Builder
		for _, h := range fp.Hunks {
			for _, l := range h.Lines {
				if l.Type == LineAdd {
					content.WriteString(l.Content)
					content.WriteByte('\n')
				}
			}
		}
		path := ResolvePath(fp.NewPath, opts.RootDir)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(content.String()), 0o644); err != nil {
			return nil, err
		}
		return &ApplyResult{FilesPatched: 1, HunksApplied: len(fp.Hunks), HunksTotal: len(fp.Hunks)}, nil
	}

	// Handle file deletion
	if fp.NewPath == "/dev/null" || fp.NewPath == "" {
		path := ResolvePath(fp.OldPath, opts.RootDir)
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		return &ApplyResult{FilesPatched: 1, HunksApplied: len(fp.Hunks), HunksTotal: len(fp.Hunks)}, nil
	}

	// Read existing file
	path := ResolvePath(fp.OldPath, opts.RootDir)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	// Remove trailing empty line from split if file ends with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Apply hunks from bottom to top to preserve line numbers
	appliedHunks := 0
	var hunkErrors []HunkError

	for i := len(fp.Hunks) - 1; i >= 0; i-- {
		hunk := fp.Hunks[i]
		newLines, ok := applyHunk(lines, hunk, opts.FuzzyMatch)
		if ok {
			lines = newLines
			appliedHunks++
		} else {
			hunkErrors = append(hunkErrors, HunkError{
				File:    fp.OldPath,
				HunkIdx: i,
				Reason:  fmt.Sprintf("hunk %d at line %d: context lines do not match", i+1, hunk.OldStart),
			})
		}
	}

	// Atomic: if any hunk failed, don't write the file
	if len(hunkErrors) > 0 {
		result.Errors = hunkErrors
		return result, nil
	}

	// Write the modified file
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n" // ensure trailing newline
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return nil, err
	}

	result.FilesPatched = 1
	result.HunksApplied = appliedHunks
	return result, nil
}

// applyHunk applies a single hunk to the file lines.
// Returns the new lines and true if successful, or the original lines and false
// if the context lines don't match.
func applyHunk(lines []string, hunk Hunk, fuzzy bool) ([]string, bool) {
	// Find the matching position using context lines
	contextLines := contextLinesOf(hunk)
	if len(contextLines) == 0 && hunk.OldCount > 0 {
		// No context lines to match, use line number
		if hunk.OldStart >= 1 && hunk.OldStart <= len(lines)+1 {
			return applyHunkAt(lines, hunk, hunk.OldStart-1), true
		}
		return lines, false
	}

	// Search for context match starting from hunk.OldStart
	startIdx := hunk.OldStart - 1
	if startIdx < 0 {
		startIdx = 0
	}

	// Try exact match first at the expected position
	if idx := findContextMatch(lines, contextLines, startIdx, false); idx >= 0 {
		return applyHunkAt(lines, hunk, idx), true
	}

	// Try fuzzy match if enabled
	if fuzzy {
		if idx := findContextMatch(lines, contextLines, startIdx, true); idx >= 0 {
			return applyHunkAt(lines, hunk, idx), true
		}
	}

	// Try broader search (hunk line number may be off)
	for i := 0; i < len(lines); i++ {
		if i == startIdx {
			continue
		}
		if findContextMatch(lines, contextLines, i, fuzzy) >= 0 {
			return applyHunkAt(lines, hunk, i), true
		}
	}

	return lines, false
}

// contextLinesOf extracts the context (unchanged) lines from a hunk,
// preserving their relative order within the hunk.
func contextLinesOf(hunk Hunk) []string {
	var ctx []string
	for _, l := range hunk.Lines {
		if l.Type == LineContext {
			ctx = append(ctx, l.Content)
		}
	}
	return ctx
}

// findContextMatch searches for the context lines in the file starting at startIdx.
// It checks whether the context lines appear in order (not necessarily contiguously)
// starting from startIdx. If fuzzy is true, whitespace differences are tolerated.
func findContextMatch(lines []string, context []string, startIdx int, fuzzy bool) int {
	if len(context) == 0 {
		return startIdx
	}

	// Try contiguous match: context lines must appear consecutively
	// starting at startIdx
	if startIdx <= len(lines)-len(context) {
		match := true
		for j, cl := range context {
			if startIdx+j >= len(lines) || !lineMatches(lines[startIdx+j], cl, fuzzy) {
				match = false
				break
			}
		}
		if match {
			return startIdx
		}
	}

	return -1
}

// lineMatches checks if two lines match, optionally with fuzzy whitespace matching.
// Fuzzy matching trims leading/trailing whitespace and normalizes internal
// whitespace runs to a single space, tolerating tab/space mixing and trailing
// whitespace differences.
func lineMatches(fileLine, contextLine string, fuzzy bool) bool {
	if !fuzzy {
		return fileLine == contextLine
	}
	// Fuzzy: trim and normalize whitespace
	return normalizeWhitespace(fileLine) == normalizeWhitespace(contextLine)
}

// normalizeWhitespace trims leading/trailing whitespace and collapses
// internal whitespace runs to a single space.
func normalizeWhitespace(s string) string {
	s = strings.TrimSpace(s)
	// Collapse runs of whitespace (spaces, tabs, etc.) to a single space
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
		} else {
			b.WriteRune(r)
			prevSpace = false
		}
	}
	return b.String()
}

// applyHunkAt applies the hunk at the given line index.
func applyHunkAt(lines []string, hunk Hunk, idx int) []string {
	// Calculate how many old lines to remove
	oldLines := 0
	var newContent []string
	for _, l := range hunk.Lines {
		switch l.Type {
		case LineContext:
			oldLines++
			newContent = append(newContent, l.Content)
		case LineDelete:
			oldLines++
		case LineAdd:
			newContent = append(newContent, l.Content)
		}
	}

	// Replace lines[idx:idx+oldLines] with newContent
	result := make([]string, 0, len(lines)-oldLines+len(newContent))
	result = append(result, lines[:idx]...)
	result = append(result, newContent...)
	result = append(result, lines[idx+oldLines:]...)
	return result
}

// ResolvePath resolves a diff path (e.g., "a/path/to/file") to a filesystem path.
// It strips the a/ or b/ prefix and joins with rootDir if provided.
func ResolvePath(diffPath, rootDir string) string {
	// Strip a/ or b/ prefix
	path := diffPath
	if strings.HasPrefix(path, "a/") {
		path = path[2:]
	} else if strings.HasPrefix(path, "b/") {
		path = path[2:]
	}

	if rootDir != "" {
		return filepath.Join(rootDir, path)
	}
	return path
}
