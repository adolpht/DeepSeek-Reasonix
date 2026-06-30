package diff

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PatchSet represents a complete patch containing multiple file patches.
type PatchSet struct {
	Files []FilePatch
}

// FilePatch represents changes to a single file.
type FilePatch struct {
	OldPath string  // path from --- line
	NewPath string  // path from +++ line
	Hunks   []Hunk
}

// Hunk represents a single diff hunk.
type Hunk struct {
	OldStart int     // line number in old file
	OldCount int     // number of lines from old file
	NewStart int     // line number in new file
	NewCount int     // number of lines in new file
	Lines    []Line  // context + change lines
}

// Line represents a single line in a hunk.
type Line struct {
	Type    LineType
	Content string
}

// LineType classifies a line within a diff hunk.
type LineType int

const (
	// LineContext is an unchanged context line (space prefix).
	LineContext LineType = iota
	// LineAdd is an added line (plus prefix).
	LineAdd
	// LineDelete is a deleted line (minus prefix).
	LineDelete
)

// hunkHeaderRe matches @@ -old_start[,old_count] +new_start[,new_count] @@.
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// ParsePatch parses a unified diff string into a PatchSet.
// It handles:
//   - --- a/path and +++ b/path headers
//   - @@ -old_start,old_count +new_start,new_count @@ hunk headers
//   - Context lines (space prefix), added lines (+), deleted lines (-)
//   - New file creation (--- /dev/null)
//   - File deletion (+++ /dev/null)
//   - Hunks with omitted count (defaults to 1)
//   - Optional function/section labels after @@ markers
func ParsePatch(patch string) (*PatchSet, error) {
	lines := strings.Split(patch, "\n")
	// Remove trailing empty line from split if patch ends with newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	var ps PatchSet
	var fp *FilePatch
	i := 0

	for i < len(lines) {
		line := lines[i]

		// Skip blank lines between file sections
		if strings.TrimSpace(line) == "" {
			i++
			continue
		}

		// Skip git diff header lines (diff --git, index, etc.)
		if strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index ") {
			i++
			continue
		}

		// --- header: start of a new file section
		if strings.HasPrefix(line, "--- ") {
			oldPath := parseDiffPath(line[4:])
			i++

			// Expect +++ header next
			if i >= len(lines) || !strings.HasPrefix(lines[i], "+++ ") {
				return nil, fmt.Errorf("expected +++ header after --- at line %d", i+1)
			}
			newPath := parseDiffPath(lines[i][4:])
			i++

			fp = &FilePatch{
				OldPath: oldPath,
				NewPath: newPath,
			}
			ps.Files = append(ps.Files, *fp)
			fp = &ps.Files[len(ps.Files)-1]

			// Parse hunks for this file
			for i < len(lines) {
				hunkLine := lines[i]

				// Skip blank lines between hunks
				if strings.TrimSpace(hunkLine) == "" {
					i++
					continue
				}

				// If we hit another file header, break out to outer loop
				if strings.HasPrefix(hunkLine, "--- ") || strings.HasPrefix(hunkLine, "diff ") {
					break
				}

				// Expect @@ hunk header
				if !strings.HasPrefix(hunkLine, "@@ ") {
					// Skip unexpected lines (e.g. git binary patch markers)
					i++
					continue
				}

				h, err := parseHunkHeader(hunkLine)
				if err != nil {
					return nil, fmt.Errorf("line %d: %w", i+1, err)
				}
				i++

				// Parse hunk body lines
				h.Lines, i, err = parseHunkBody(lines, i, h.OldCount, h.NewCount)
				if err != nil {
					return nil, fmt.Errorf("hunk at line %d: %w", i+1, err)
				}

				fp.Hunks = append(fp.Hunks, h)
			}
			continue
		}

		// Skip any other unexpected lines
		i++
	}

	if len(ps.Files) == 0 {
		return nil, fmt.Errorf("no file patches found in input")
	}

	return &ps, nil
}

// parseDiffPath extracts the file path from a --- or +++ header line,
// stripping the a/ or b/ prefix and any surrounding quotes.
func parseDiffPath(s string) string {
	s = strings.TrimSpace(s)
	// Strip optional tab-separated timestamp (git format: --- a/file\t2024-01-01)
	if idx := strings.Index(s, "\t"); idx >= 0 {
		s = s[:idx]
	}
	// Strip surrounding quotes (git quotes paths with special chars)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}
	return s
}

// parseHunkHeader parses a @@ -old_start,old_count +new_start,new_count @@ line.
func parseHunkHeader(line string) (Hunk, error) {
	m := hunkHeaderRe.FindStringSubmatch(line)
	if m == nil {
		return Hunk{}, fmt.Errorf("invalid hunk header: %q", line)
	}

	oldStart, err := strconv.Atoi(m[1])
	if err != nil {
		return Hunk{}, fmt.Errorf("invalid old start line: %q", m[1])
	}
	oldCount := 1
	if m[2] != "" {
		oldCount, err = strconv.Atoi(m[2])
		if err != nil {
			return Hunk{}, fmt.Errorf("invalid old count: %q", m[2])
		}
	}

	newStart, err := strconv.Atoi(m[3])
	if err != nil {
		return Hunk{}, fmt.Errorf("invalid new start line: %q", m[3])
	}
	newCount := 1
	if m[4] != "" {
		newCount, err = strconv.Atoi(m[4])
		if err != nil {
			return Hunk{}, fmt.Errorf("invalid new count: %q", m[4])
		}
	}

	return Hunk{
		OldStart: oldStart,
		OldCount: oldCount,
		NewStart: newStart,
		NewCount: newCount,
	}, nil
}

// parseHunkBody reads the lines of a hunk body until the next hunk header,
// file header, or end of input. It returns the parsed lines, the next line
// index, and any error.
func parseHunkBody(lines []string, startIdx int, oldCount, newCount int) ([]Line, int, error) {
	var result []Line
	i := startIdx
	seenOld, seenNew := 0, 0

	for i < len(lines) {
		line := lines[i]

		// Blank line within a hunk is a context line (space prefix was stripped
		// by some tools; treat as context)
		if line == "" {
			// Could be end of patch or a context line with empty content.
			// If we haven't consumed all expected lines, treat as context.
			if seenOld < oldCount || seenNew < newCount {
				result = append(result, Line{Type: LineContext, Content: ""})
				seenOld++
				seenNew++
				i++
				continue
			}
			break
		}

		// Next hunk or file header: stop
		if strings.HasPrefix(line, "@@ ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "diff ") {
			break
		}

		// "\ No newline at end of file" marker: skip
		if strings.HasPrefix(line, "\\ ") {
			i++
			continue
		}

		// Context line (space prefix)
		if len(line) > 0 && line[0] == ' ' {
			result = append(result, Line{Type: LineContext, Content: line[1:]})
			seenOld++
			seenNew++
			i++
			continue
		}

		// Deleted line (- prefix)
		if len(line) > 0 && line[0] == '-' {
			result = append(result, Line{Type: LineDelete, Content: line[1:]})
			seenOld++
			i++
			continue
		}

		// Added line (+ prefix)
		if len(line) > 0 && line[0] == '+' {
			result = append(result, Line{Type: LineAdd, Content: line[1:]})
			seenNew++
			i++
			continue
		}

		// Unknown prefix: treat as context (some tools emit lines without prefix)
		result = append(result, Line{Type: LineContext, Content: line})
		seenOld++
		seenNew++
		i++
	}

	return result, i, nil
}
