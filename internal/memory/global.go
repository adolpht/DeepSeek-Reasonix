package memory

import (
	"path/filepath"
	"strings"
)

// TypeGlobal classifies a cross-project (global) memory — a fact that applies
// to every project the user works on, not just the current one. Global
// memories live under the user config root (~/.config/rexion/memory/) and
// load into every session's prefix via Set.Block.
const TypeGlobal Type = "global"

func init() {
	// Register TypeGlobal in the validTypes map so NormalizeType accepts it
	// and render serialises it faithfully (instead of defaulting to
	// TypeProject). Existing types are unchanged — backward compatible.
	validTypes[TypeGlobal] = true
}

// GlobalStore is the cross-project auto-memory: same shape as Store (one fact
// per Markdown file + MEMORY.md index) but rooted under the user config dir
// (~/.config/rexion/memory/) so the same facts surface in every project. The
// model maintains it through the `remember_global` tool; the index loads into
// the cached system-prompt prefix at boot so the model always knows what
// cross-project knowledge it has saved, and reads individual facts on demand
// with read_file. Like Store, it is plain files the user can edit by hand.
//
// GlobalStore embeds Store to reuse Save / Delete / List / Index / Path
// verbatim — the on-disk shape is identical, only the directory differs — and
// adds Search for substring recall across the global fact set.
type GlobalStore struct {
	Store
}

// GlobalStoreFor resolves the global memory directory under the user config
// root (e.g. ~/.config/rexion/memory/). A "" userDir (config dir unresolvable)
// yields a zero GlobalStore, which all methods treat as a disabled no-op —
// mirroring StoreFor's behaviour for the per-project store.
func GlobalStoreFor(userDir string) GlobalStore {
	if userDir == "" {
		return GlobalStore{}
	}
	return GlobalStore{Store: Store{Dir: filepath.Join(userDir, "memory")}}
}

// Search returns the global memories whose Name, Title, Description, or Body
// contains query (case-insensitive). An empty query returns every saved
// global memory (same as List). Search is intentionally a simple
// strings.Contains scan — no embedding or ranking — so it stays dependency-
// free and predictable; the model decides which hits are relevant.
func (s GlobalStore) Search(query string) []Memory {
	if s.Dir == "" {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return s.List()
	}
	var out []Memory
	for _, m := range s.List() {
		if strings.Contains(strings.ToLower(m.Name), q) ||
			strings.Contains(strings.ToLower(m.Title), q) ||
			strings.Contains(strings.ToLower(m.Description), q) ||
			strings.Contains(strings.ToLower(m.Body), q) {
			out = append(out, m)
		}
	}
	return out
}

// ListByType returns the global memories filtered to a single Type. Used by
// callers that want to scope a view (e.g. only TypeGlobal entries, or only
// TypeUser entries that were promoted to the global store).
func (s GlobalStore) ListByType(t Type) []Memory {
	if s.Dir == "" {
		return nil
	}
	var out []Memory
	for _, m := range s.List() {
		if m.Type == t {
			out = append(out, m)
		}
	}
	return out
}
