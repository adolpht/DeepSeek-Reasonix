package instruction

import (
	"context"
	"strings"

	"rexion/internal/memory"
)

// VerifyCheck is a host-observable project check extracted from structured
// project memory. It is runtime-only and is not serialized into prompts.
type VerifyCheck struct {
	Command    string
	SourcePath string
	Line       int
}

type contextKey struct{}

func WithChecks(ctx context.Context, checks []VerifyCheck) context.Context {
	if len(checks) == 0 {
		return ctx
	}
	cp := append([]VerifyCheck(nil), checks...)
	return context.WithValue(ctx, contextKey{}, cp)
}

func FromContext(ctx context.Context) []VerifyCheck {
	checks, ok := ctx.Value(contextKey{}).([]VerifyCheck)
	if !ok || len(checks) == 0 {
		return nil
	}
	return append([]VerifyCheck(nil), checks...)
}

// ExtractHostChecks reads only the structured "Rexion host checks" section.
// Ordinary project instructions remain guidance and do not become hard gates.
func ExtractHostChecks(docs []memory.Source) []VerifyCheck {
	seen := map[string]bool{}
	var checks []VerifyCheck
	for _, doc := range docs {
		inSection := false
		for i, raw := range strings.Split(doc.Body, "\n") {
			line := strings.TrimRight(raw, "\r")
			if heading, ok := markdownHeading(line); ok {
				inSection = strings.EqualFold(heading, "Rexion host checks")
				continue
			}
			if !inSection {
				continue
			}
			command, ok := verifyBullet(line)
			if !ok || seen[command] {
				continue
			}
			seen[command] = true
			checks = append(checks, VerifyCheck{
				Command:    command,
				SourcePath: doc.Path,
				Line:       i + 1,
			})
		}
	}
	return checks
}

func markdownHeading(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#") {
		return "", false
	}
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i == 0 || i >= len(line) || line[i] != ' ' {
		return "", false
	}
	heading := strings.TrimSpace(line[i+1:])
	heading = strings.TrimSpace(strings.TrimRight(heading, "#"))
	return heading, heading != ""
}

// ExtractReadinessLevel reads the readiness level from the first
// memory document that specifies it under "Rexion host checks" or
// as a top-level setting. The level is set via:
//
//	- readiness: verified
//	- readiness: merge-ready
//
// An empty or unset level defaults to basic.
func ExtractReadinessLevel(docs []memory.Source) string {
	for _, doc := range docs {
		inSection := false
		for _, raw := range strings.Split(doc.Body, "\n") {
			line := strings.TrimRight(raw, "\r")
			if heading, ok := markdownHeading(line); ok {
				inSection = strings.EqualFold(heading, "Rexion host checks")
				continue
			}
			if !inSection {
				continue
			}
			val, ok := readinessBullet(line)
			if !ok {
				continue
			}
			return val
		}
	}
	return ""
}

func verifyBullet(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if len(line) < 2 || (line[:2] != "- " && line[:2] != "* ") {
		return "", false
	}
	body := strings.TrimSpace(line[2:])
	const prefix = "verify:"
	if len(body) < len(prefix) || !strings.EqualFold(body[:len(prefix)], prefix) {
		return "", false
	}
	command := strings.TrimSpace(body[len(prefix):])
	return command, command != ""
}

func readinessBullet(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if len(line) < 2 || (line[:2] != "- " && line[:2] != "* ") {
		return "", false
	}
	body := strings.TrimSpace(line[2:])
	const prefix = "readiness:"
	if len(body) < len(prefix) || !strings.EqualFold(body[:len(prefix)], prefix) {
		return "", false
	}
	val := strings.TrimSpace(body[len(prefix):])
	return val, val != ""
}

// WithReadinessLevel embeds a readiness level string into the context.
func WithReadinessLevel(ctx context.Context, level string) context.Context {
	return context.WithValue(ctx, readinessKey{}, level)
}

// ReadinessLevelFromContext returns the embedded readiness level from context.
func ReadinessLevelFromContext(ctx context.Context) string {
	v, _ := ctx.Value(readinessKey{}).(string)
	return v
}

type readinessKey struct{}

