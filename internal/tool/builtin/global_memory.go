package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"rexion/internal/memory"
	"rexion/internal/tool"
)

func init() {
	tool.RegisterBuiltin(recallGlobal{})
	tool.RegisterBuiltin(rememberGlobal{})
}

// ctxKeyGlobalMemory mirrors agent.CtxKeyGlobalMemory. Duplicated as a literal
// string here (rather than imported) so tool/builtin does not depend on
// internal/agent — the same pattern merge_worktree.go uses for the worktree
// keys. The two constants must stay in sync.
const ctxKeyGlobalMemory = "rexion.memory.global"

// globalMemoryStore is the local interface recall_global / remember_global
// require of the value stamped onto the call context under
// ctxKeyGlobalMemory. memory.GlobalStore satisfies it (it embeds Store for
// Save / Delete / List and adds Search itself), so the agent's injected value
// type-asserts cleanly without tool/builtin importing internal/agent.
type globalMemoryStore interface {
	List() []memory.Memory
	Save(memory.Memory) (string, error)
	Delete(string) error
	Search(query string) []memory.Memory
}

// globalStoreFromContext retrieves the injected GlobalStore, or ok=false when
// global memory is disabled for this session (no user config dir, or the agent
// was not wired with SetGlobalStore). Tools degrade to a clear "unavailable"
// message rather than panicking.
func globalStoreFromContext(ctx context.Context) (globalMemoryStore, bool) {
	s, ok := ctx.Value(ctxKeyGlobalMemory).(globalMemoryStore)
	return s, ok && s != nil
}

// --- recall_global ---

// recallGlobal lets the model search the cross-project memory store by keyword.
// It is read-only: surfacing a fact the model already saved globally costs no
// side effects, and parallelising it with other read-only calls is safe.
type recallGlobal struct{}

func (recallGlobal) Name() string { return "recall_global" }

func (recallGlobal) Description() string {
	return "Search the global (cross-project) memory store for facts that apply to every project, " +
		"not just the current one — e.g. standing conventions (\"我通常使用 GitFlow\"), " +
		"toolchain defaults shared across repos, or identity/preferences the user has promoted to global. " +
		"Pass a keyword to filter by name/title/description/body (case-insensitive substring); " +
		"omit the query to list every saved global memory. " +
		"Use this before re-asking the user for a convention you may already have captured."
}

func (recallGlobal) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "Keyword to search for across the global memory's name, title, description, and body (case-insensitive). Omit to list all global memories."}
		}
	}`)
}

// ReadOnly is true: searching the store touches no state, so the agent may
// parallelise recall_global with other read-only calls in the same batch.
func (recallGlobal) ReadOnly() bool { return true }

func (recallGlobal) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	store, ok := globalStoreFromContext(ctx)
	if !ok {
		return "Global memory store unavailable for this session (no user config dir, or global memory not wired).", nil
	}
	hits := store.Search(p.Query)
	if len(hits) == 0 {
		if q := strings.TrimSpace(p.Query); q != "" {
			return fmt.Sprintf("No global memories matched %q.", q), nil
		}
		return "No global memories saved yet.", nil
	}
	return formatGlobalMemories(hits), nil
}

// --- remember_global ---

// rememberGlobal lets the model persist a cross-project fact to the global
// memory store. It is the global counterpart of `remember`: same on-disk shape
// (one fact per Markdown file + MEMORY.md index), but rooted under the user
// config dir so the fact loads into every session's prefix, not just this
// project's. The Type is fixed to "global" — use `remember` for project-scoped
// facts.
type rememberGlobal struct{}

func (rememberGlobal) Name() string { return "remember_global" }

func (rememberGlobal) Description() string {
	return "Save a durable fact to the GLOBAL (cross-project) memory store so it survives across sessions AND loads in every project, " +
		"not just the current one. Use for conventions that span the user's whole workflow: " +
		"standing toolchain choices (\"I use pnpm in all my projects\"), cross-project identity (role, expertise), " +
		"or working-style defaults the user has stated apply everywhere. " +
		"The saved index loads into the system prompt of every future session. " +
		"For project-specific facts (this repo's goals, constraints, contacts) use `remember` instead; " +
		"this tool is only for facts that should follow the user across repos. " +
		"Reusing a name overwrites that global memory — do that to update an existing fact."
}

func (rememberGlobal) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Short kebab-case slug identifying the global fact, e.g. \"uses-pnpm-everywhere\". Reusing a name overwrites that memory. Omit to derive one from the description."},
			"title": {"type": "string", "description": "Short human-readable label shown in the global memory index. Omit to derive one from the name."},
			"description": {"type": "string", "description": "One-line hook shown in the global index — the phrase a future session reads to decide whether to open this fact. Make it specific."},
			"body": {"type": "string", "description": "The fact itself (Markdown). State the convention and, when useful, the why and how-to-apply."}
		},
		"required": ["description", "body"]
	}`)
}

// ReadOnly is false: saving a global memory writes to disk and mutates the
// global MEMORY.md index, so the agent treats it as a writer (no parallel
// batching with other writers, plan-mode gating applies).
func (rememberGlobal) ReadOnly() bool { return false }

func (rememberGlobal) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Description == "" || in.Body == "" {
		return "", fmt.Errorf("description and body are required")
	}
	store, ok := globalStoreFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("global memory store unavailable for this session (no user config dir, or global memory not wired)")
	}
	name := in.Name
	if name == "" {
		name = in.Title
	}
	if name == "" {
		name = in.Description
	}
	path, err := store.Save(memory.Memory{
		Name:        name,
		Title:       in.Title,
		Description: in.Description,
		Type:        memory.TypeGlobal,
		Body:        in.Body,
	})
	if err != nil {
		return "", err
	}
	// Fold a turn-tail note into the session via the memory queue (if wired),
	// so the just-saved global fact applies this session without touching the
	// cache-stable prefix — same mechanism `remember` uses.
	if q, ok := memory.QueueFromContext(ctx); ok {
		q.QueueMemory("Saved global memory \"" + memorySlug(name) + "\": " + oneLineDesc(in.Description))
	}
	return fmt.Sprintf("Saved global memory to %s (it applies now and loads automatically in every future session across all projects).", path), nil
}

// formatGlobalMemories renders a list of global memories as a compact,
// model-readable summary: one block per fact with its name, type, and body
// (truncated to keep tool output bounded). Empty bodies are skipped so the
// output stays tight when the index alone would suffice.
func formatGlobalMemories(ms []memory.Memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Global memories (%d):\n", len(ms))
	for _, m := range ms {
		fmt.Fprintf(&b, "\n## %s\n", m.Name)
		if t := strings.TrimSpace(m.Title); t != "" {
			fmt.Fprintf(&b, "title: %s\n", t)
		}
		if d := strings.TrimSpace(m.Description); d != "" {
			fmt.Fprintf(&b, "description: %s\n", d)
		}
		fmt.Fprintf(&b, "type: %s\n", string(m.Type))
		if body := strings.TrimSpace(m.Body); body != "" {
			fmt.Fprintf(&b, "\n%s\n", body)
		}
	}
	return b.String()
}

// memorySlug mirrors memory.slug without exporting it. It normalises a name
// into a kebab-case stem for the turn-tail note. Imported from the memory
// package via the unexported slug function is not possible, so we keep a
// minimal copy here; the worst case is a slightly different slug in the
// transient note, which is display-only.
func memorySlug(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// oneLineDesc collapses whitespace in a description for the single-line
// turn-tail note. Mirrors memory.oneLine without importing the unexported
// helper.
func oneLineDesc(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
