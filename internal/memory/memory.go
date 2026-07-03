package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Set is everything memory loaded for one session: the hierarchical docs and a
// handle to the auto-memory store (whose index is captured at load time). It is
// assembled once at boot and folded into the system prompt by Compose. CWD and
// UserDir are retained so the controller can resolve quick-add targets without
// re-deriving discovery context.
//
// PKM holds the personal-knowledge-base sources (people.md / projects.md /
// preferences.md / writing_style.md under ~/.reasonix/memory/). It is populated
// by Load only when Options.PKMEnabled is true and the files exist; it is
// rendered as its own <personal-knowledge> section by Block, ahead of Docs, so
// the cache-stable system prefix stays byte-stable across sessions that don't
// touch the PKM files.
type Set struct {
	Docs    []Source // REASONIX.md / AGENTS.md, ascending precedence
	PKM     []Source // personal knowledge base (~/.reasonix/memory/*.md)
	Store   Store    // auto-memory store (may be a zero/disabled Store)
	Index   string   // MEMORY.md contents at load time
	CWD     string   // project working dir used for discovery
	UserDir string   // user config root (may be "")
}

// Options configures discovery. CWD defaults to "." and UserDir is the user
// config root (config.MemoryUserDir()); a "" UserDir disables user-global docs
// and the auto-memory store. PKMEnabled gates whether the personal knowledge
// base under ~/.reasonix/memory/ is loaded — it defaults to false so callers
// (and existing tests) that don't set it keep the historical Docs-only shape.
type Options struct {
	CWD        string
	UserDir    string
	PKMEnabled bool
}

// Load discovers all memory for a session: the hierarchical docs, the optional
// personal knowledge base, and the auto-memory index. It is best-effort and
// never errors — missing files just mean less memory — so boot can call it
// unconditionally.
func Load(opts Options) *Set {
	cwd := opts.CWD
	if cwd == "" {
		cwd = "."
	}
	store := StoreFor(opts.UserDir, cwd)
	set := &Set{
		Docs:    discoverDocs(cwd, opts.UserDir),
		Store:   store,
		Index:   store.Index(),
		CWD:     cwd,
		UserDir: opts.UserDir,
	}
	if opts.PKMEnabled {
		set.PKM = loadPKMFiles()
	}
	return set
}

// pkmFileOrder is the fixed load/render order for the four PKM files. It is
// byte-stable across sessions so DeepSeek's automatic prefix cache stays warm:
// writing_style first (rarely changed, most reusable), preferences next, then
// the more volatile people/projects lists. Renaming or reordering here would
// invalidate every cached prefix — do not change it without a migration.
var pkmFileOrder = []string{
	"writing_style.md",
	"preferences.md",
	"people.md",
	"projects.md",
}

// PKMFileOrder returns the fixed load/render order of the PKM files. It is the
// exported accessor for pkmFileOrder, used by the desktop panel to render the
// editor tabs in the same order the model sees them.
func PKMFileOrder() []string {
	return pkmFileOrder
}

// ValidPKMFileName reports whether name is one of the four PKM files. Used by
// the desktop SavePKMFile path to refuse writes outside the PKM directory.
func ValidPKMFileName(name string) bool {
	for _, n := range pkmFileOrder {
		if n == name {
			return true
		}
	}
	return false
}

// loadPKMFiles reads the four PKM files from ~/.reasonix/memory/ in the fixed
// pkmFileOrder. Missing or unreadable files are skipped silently so a partial
// PKM (or none at all) never breaks boot. Bodies are trimmed; empty files are
// dropped to keep the rendered block free of dead sections.
func loadPKMFiles() []Source {
	dir, err := MemoryDir()
	if err != nil {
		return nil
	}
	var out []Source
	for _, name := range pkmFileOrder {
		path := filepath.Join(dir, name)
		body, ok := readPKMFile(path)
		if !ok {
			continue
		}
		out = append(out, Source{Path: path, Scope: ScopePKM, Body: body})
	}
	return out
}

// readPKMFile reads and trims a PKM file, returning ok=false when it is absent,
// unreadable, or whitespace-only (so a freshly scaffolded file with only HTML
// comments still contributes once the user adds real content — the trimmed body
// carries the comments too, but we drop truly empty files).
func readPKMFile(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	body := strings.TrimSpace(string(b))
	if body == "" {
		return "", false
	}
	return body, true
}

// DocPath returns the doc-memory file a given scope writes to. To avoid splitting
// a project's memory across conventions, it prefers a file that already exists
// (REASONIX.md / AGENTS.md / CLAUDE.md, in that order); when none exists it
// creates the universal default (AGENTS.md / AGENTS.local.md). ScopeUser →
// <userDir>, ScopeLocal → <cwd> with the *.local.md names, anything else → <cwd>.
// Returns "" for ScopeUser when no user dir is configured.
func (s *Set) DocPath(scope Scope) string {
	dir := s.CWD
	names, def := docNames, defaultDocName
	switch scope {
	case ScopeUser:
		if s.UserDir == "" {
			return ""
		}
		dir = s.UserDir
	case ScopeLocal:
		names, def = localNames, defaultLocalName
	}
	for _, n := range names {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err == nil {
			return p // append to the doc already in use
		}
	}
	return filepath.Join(dir, def)
}

// Empty reports whether the set carries nothing to inject, so Compose can leave
// the base prompt byte-for-byte untouched (and the cache prefix maximal) when
// there is no memory at all. PKM sources count, so a set with only PKM is not
// empty.
func (s *Set) Empty() bool {
	return s == nil || (len(s.Docs) == 0 && len(s.PKM) == 0 && strings.TrimSpace(s.Index) == "")
}

// docScopes are the scopes the panel can target for a quick-add or a new doc.
// Ordered broad → specific for display.
var docScopes = []Scope{ScopeUser, ScopeProject, ScopeLocal}

// allowedDocPaths is the closed set of files WriteDoc / AppendDoc may touch: the
// canonical file for each writable scope, plus every doc already discovered this
// session (so an ancestor or AGENTS.md the user is already editing stays
// editable). Keyed by absolute path. This bounds frontend-driven writes to real
// memory files rather than arbitrary paths.
func (s *Set) allowedDocPaths() map[string]bool {
	allow := map[string]bool{}
	for _, sc := range docScopes {
		if p := s.DocPath(sc); p != "" {
			allow[absOf(p)] = true
		}
	}
	for _, d := range s.Docs {
		allow[absOf(d.Path)] = true
	}
	return allow
}

// WriteDoc overwrites a doc-memory file with body, after checking path is a
// recognized memory file (see allowedDocPaths). It is the save side of the
// desktop panel's in-place editor. The write lands on disk immediately but does
// NOT mutate the cache-stable system prefix — the edit folds into the prefix on
// the next session; to make it apply this session, the controller separately
// queues a turn-tail note. Returns the path written.
func (s *Set) WriteDoc(path, body string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("memory unavailable")
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("no path given")
	}
	if !s.allowedDocPaths()[absOf(path)] {
		return "", fmt.Errorf("refusing to write %q: not a recognized memory file", path)
	}
	return path, writeDocFile(path, body)
}

// Block renders the memory as a single Markdown section, or "" when empty. It is
// deterministic given the same files, which is what keeps it a stable cache
// prefix across sessions that don't change their memory.
//
// The PKM (personal knowledge base) renders first, wrapped in a
// <personal-knowledge> tag so the model can locate it structurally; then come
// the hierarchical Docs, then the auto-memory index. PKM is the most durable
// layer (user-authored, rarely edited), so leading with it keeps the largest
// possible byte-stable prefix for DeepSeek's automatic prefix cache.
func (s *Set) Block() string {
	if s.Empty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("# Memory\n\n")
	b.WriteString("Persistent context loaded from memory files. Treat it as durable, user-authored guidance for this project.\n")

	if len(s.PKM) > 0 {
		b.WriteString("\n<personal-knowledge>\n")
		b.WriteString("## 个人知识库\n")
		b.WriteString("User-authored personal knowledge base (~/.reasonix/memory/). Treat this as the user's standing profile — writing style, preferences, contacts, and projects — and honor it without restating it back.\n")
		for _, d := range s.PKM {
			fmt.Fprintf(&b, "\n### %s\n\n%s\n", d.Path, strings.TrimSpace(d.Body))
		}
		b.WriteString("\n</personal-knowledge>\n")
	}

	for _, d := range s.Docs {
		fmt.Fprintf(&b, "\n## %s (%s)\n\n%s\n", d.Path, d.Scope, strings.TrimSpace(d.Body))
	}

	if idx := strings.TrimSpace(s.Index); idx != "" {
		b.WriteString("\n## Saved memories\n\n")
		b.WriteString("Facts you saved in earlier sessions. They reflect what was true when written and may now be stale — treat them as background, not standing instructions. " +
			"Read the linked file with read_file when one looks relevant, and before acting on one that names a file, function, or flag, verify it still exists. " +
			"Save new durable facts with the `remember` tool; delete ones that turn out wrong with `forget`.\n\n")
		b.WriteString(idx)
		fmt.Fprintf(&b, "\n\n(stored under %s)\n", s.Store.Dir)
	}
	return b.String()
}

// Compose folds the memory block onto the base system prompt and returns the
// durable cached-prefix string. Base stays first (it is the most stable text, so
// it remains a valid cache prefix even when memory changes between sessions);
// memory follows. With no memory, base is returned unchanged.
func Compose(base string, s *Set) string {
	block := s.Block()
	if block == "" {
		return base
	}
	if strings.TrimSpace(base) == "" {
		return block
	}
	return strings.TrimRight(base, "\n") + "\n\n" + block
}

// --- Personal knowledge base (Task 31) ---

// MemoryFile describes one default file in the ~/.reasonix/memory/ knowledge base.
type MemoryFile struct {
	Name    string // file name (e.g. "people.md")
	Content string // initial content when the file is created
}

// DefaultMemoryFiles returns the set of default knowledge-base files to create
// under ~/.reasonix/memory/ when the directory is first initialised.
func DefaultMemoryFiles() []MemoryFile {
	return []MemoryFile{
		{
			Name: "people.md",
			Content: `# 常联系人
<!-- 记录你经常联系的人，Agent 会根据此信息优化沟通建议 -->
<!-- 格式：- 姓名 | 角色 | 联系方式 | 备注 -->
`,
		},
		{
			Name: "projects.md",
			Content: `# 在跟项目
<!-- 记录你当前参与的项目，Agent 会据此提供上下文感知 -->
<!-- 格式：- 项目名 | 状态 | 关键信息 -->
`,
		},
		{
			Name: "preferences.md",
			Content: `# 个人偏好
<!-- 记录你的工作偏好，Agent 会据此调整输出风格 -->
<!-- 例如：语言偏好、输出格式、沟通风格等 -->
`,
		},
		{
			Name: "writing_style.md",
			Content: `# 写作风格
<!-- 记录你的写作风格偏好，Agent 会据此生成更符合你风格的文本 -->
<!-- 例如：正式/随意、简洁/详细、技术/通俗 -->
`,
		},
	}
}

// MemoryDir returns the path to the personal knowledge base directory
// (~/.reasonix/memory/). It uses the user's home directory as the base.
func MemoryDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	return filepath.Join(home, ".reasonix", "memory"), nil
}

// EnsureMemoryDir checks whether the ~/.reasonix/memory/ directory exists and,
// if not, creates it along with the default knowledge-base files. It returns the
// directory path and a boolean indicating whether the directory was newly created.
func EnsureMemoryDir() (string, bool, error) {
	dir, err := MemoryDir()
	if err != nil {
		return "", false, err
	}
	info, err := os.Stat(dir)
	if err == nil && info.IsDir() {
		return dir, false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", false, fmt.Errorf("stat memory dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, fmt.Errorf("create memory dir: %w", err)
	}
	for _, f := range DefaultMemoryFiles() {
		path := filepath.Join(dir, f.Name)
		if _, err := os.Stat(path); err == nil {
			continue // don't overwrite existing files
		}
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			return dir, true, fmt.Errorf("write %s: %w", f.Name, err)
		}
	}
	return dir, true, nil
}

// AppendPKMFile appends a learned preference snippet to one of the four PKM
// files under ~/.reasonix/memory/. name must be a valid PKM filename. The
// snippet is appended at the end of the file under a "## 自动学习" section so
// the user can distinguish hand-written entries from auto-learned ones. The
// directory is ensured to exist (idempotent). Returns the absolute path written.
func AppendPKMFile(name, content string) error {
	name = strings.TrimSpace(name)
	content = strings.TrimSpace(content)
	if !ValidPKMFileName(name) {
		return fmt.Errorf("refusing to append %q: not a PKM file", name)
	}
	if content == "" {
		return nil // nothing to append
	}
	if _, _, err := EnsureMemoryDir(); err != nil {
		return fmt.Errorf("ensure pkm dir: %w", err)
	}
	dir, err := MemoryDir()
	if err != nil {
		return fmt.Errorf("resolve pkm dir: %w", err)
	}
	path := filepath.Join(dir, name)
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", name, err)
	}
	body := string(existing)
	snippet := "\n\n## 自动学习\n\n" + content + "\n"
	// If the file already ends with an auto-learn section, just append the
	// new line into it instead of creating a new section header each time.
	if idx := strings.LastIndex(body, "## 自动学习"); idx >= 0 {
		body = body + "- " + content + "\n"
	} else {
		body = body + snippet
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
