package installsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SkillRegistryEntry describes a single skill available from a registry source.
// It is the DTO returned by SkillRegistry implementations and consumed by the
// remote installer (InstallFromRemote) and the CLI skill-market subcommands.
type SkillRegistryEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"` // e.g. "coding" | "office" | "devops" | "review"
	SourceURL   string   `json:"source_url"`
	SourceType  string   `json:"source_type"` // "file" | "git"
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Tags        []string `json:"tags"`
}

// SkillRegistry is the read-only catalogue surface for the official Skills
// marketplace. Implementations fetch a skills-index.json from a curated source
// (GitHub raw URL, git repo, or local cache) and expose keyword/category lookup.
type SkillRegistry interface {
	// Search returns entries whose name, description, author, or tags contain
	// the query (case-insensitive). An empty query returns the full catalogue.
	Search(query string) ([]SkillRegistryEntry, error)
	// List returns every entry, optionally narrowed to a single category.
	// Pass an empty category to list all.
	List(category string) ([]SkillRegistryEntry, error)
	// Fetch returns the single entry matching name, or an error wrapping
	// ErrManifestMissing when no such entry exists.
	Fetch(name string) (SkillRegistryEntry, error)
}

// skillsIndexFile is the JSON document served at the registry's raw URL.
// Only the `skills` array is required; the other fields are informational.
type skillsIndexFile struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Skills      []SkillRegistryEntry  `json:"skills"`
}

// GitHubRegistryOptions configures a GitHubRegistry. Zero values fall back
// to the official Rexion-skills repository and sensible defaults.
type GitHubRegistryOptions struct {
	// Repo is the "owner/name" of the GitHub repository hosting the index.
	// Default: "esengine/Rexion-skills".
	Repo string
	// Branch is the git ref to fetch from. Default: "main".
	Branch string
	// IndexPath is the path within the repo to the skills-index.json file.
	// Default: "skills-index.json".
	IndexPath string
	// HTTPClient overrides the default client. When nil a new client with a
	// 30s timeout is used.
	HTTPClient *http.Client
	// CacheTTL bounds how long a cached index is trusted before refetch.
	// Default: 24 hours.
	CacheTTL time.Duration
	// CacheDir overrides the on-disk cache location. Default: the user's
	// config dir + "/rexion/cache".
	CacheDir string
}

// DefaultGitHubRepo is the official Rexion skills catalogue repository.
const DefaultGitHubRepo = "esengine/Rexion-skills"

// defaultRegistryCacheTTL matches the task spec: 24 hours.
const defaultRegistryCacheTTL = 24 * time.Hour

// GitHubRegistry fetches the skills index from a GitHub repository's raw
// endpoint, caching it locally so offline runs still serve the last known
// catalogue. It implements SkillRegistry.
type GitHubRegistry struct {
	repo       string
	branch     string
	indexPath  string
	client     *http.Client
	cacheTTL   time.Duration
	cachePath  string
	mu         sync.Mutex
	cached     []SkillRegistryEntry
	cachedAt   time.Time
}

// NewGitHubRegistry constructs a registry backed by a GitHub raw index URL.
// Construction is cheap — the network fetch is deferred until the first
// Search/List/Fetch call.
func NewGitHubRegistry(opts GitHubRegistryOptions) *GitHubRegistry {
	if opts.Repo == "" {
		opts.Repo = DefaultGitHubRepo
	}
	if opts.Branch == "" {
		opts.Branch = "main"
	}
	if opts.IndexPath == "" {
		opts.IndexPath = "skills-index.json"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if opts.CacheTTL == 0 {
		opts.CacheTTL = defaultRegistryCacheTTL
	}
	if opts.CacheDir == "" {
		opts.CacheDir = defaultRegistryCacheDir()
	}
	return &GitHubRegistry{
		repo:      opts.Repo,
		branch:    opts.Branch,
		indexPath: opts.IndexPath,
		client:    opts.HTTPClient,
		cacheTTL:  opts.CacheTTL,
		cachePath: filepath.Join(opts.CacheDir, "skills-index.json"),
	}
}

// defaultRegistryCacheDir resolves ~/.config/rexion/cache on Linux, the
// equivalent %AppData%\rexion\cache on Windows, and ~/Library/Application
// Support/rexion/cache on macOS. Falls back to ~/.rexion/cache when the
// user's config dir can't be resolved (mirrors the legacy path).
func defaultRegistryCacheDir() string {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "rexion", "cache")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".rexion", "cache")
	}
	return filepath.Join(".", ".rexion", "cache")
}

// rawIndexURL builds the raw.githubusercontent.com URL for the configured
// repo/branch/path. This is the canonical "download the file as-is" endpoint.
func (r *GitHubRegistry) rawIndexURL() string {
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", r.repo, r.branch, r.indexPath)
}

// loadIndex returns the catalogue, refreshing the cache when stale. It is safe
// to call concurrently — the mutex serialises refreshes so only one HTTP fetch
// happens at a time, and readers see the cached snapshot otherwise.
func (r *GitHubRegistry) loadIndex() ([]SkillRegistryEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// In-memory hit: still fresh.
	if r.cached != nil && time.Since(r.cachedAt) < r.cacheTTL {
		return r.cached, nil
	}
	// On-disk cache hit: serve it if fresh; else try to refresh, falling back
	// to the stale copy on network failure (offline mode).
	if entries, ok := r.readDiskCache(); ok {
		if time.Since(r.cachedAt) < r.cacheTTL {
			r.cached = entries
			return entries, nil
		}
		if refreshed, err := r.fetchRemote(context.Background()); err == nil {
			r.cached = refreshed
			r.cachedAt = time.Now()
			r.writeDiskCache(refreshed)
			return refreshed, nil
		}
		// Network failed — keep serving the stale cache so the user isn't blocked.
		r.cached = entries
		return entries, nil
	}
	// No cache at all: must fetch or fail.
	entries, err := r.fetchRemote(context.Background())
	if err != nil {
		return nil, err
	}
	r.cached = entries
	r.cachedAt = time.Now()
	r.writeDiskCache(entries)
	return entries, nil
}

// readDiskCache loads the cache file and stamps cachedAt from its mtime.
// Returns ok=false when the file is missing or unparseable.
func (r *GitHubRegistry) readDiskCache() ([]SkillRegistryEntry, bool) {
	info, err := os.Stat(r.cachePath)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(r.cachePath)
	if err != nil {
		return nil, false
	}
	var idx skillsIndexFile
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, false
	}
	r.cachedAt = info.ModTime()
	return idx.Skills, true
}

// writeDiskCache persists the index for offline use. Failures are best-effort:
// the in-memory copy is the source of truth for the current process.
func (r *GitHubRegistry) writeDiskCache(entries []SkillRegistryEntry) {
	idx := skillsIndexFile{Skills: entries}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return
	}
	if dir := filepath.Dir(r.cachePath); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	_ = os.WriteFile(r.cachePath, data, 0o644)
}

// fetchRemote downloads skills-index.json from the configured raw URL. The
// response is capped at defaultFetchLimit so an untrusted mirror can't stream
// gigabytes into the parser.
func (r *GitHubRegistry) fetchRemote(ctx context.Context) ([]SkillRegistryEntry, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultFetchTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.rawIndexURL(), nil)
	if err != nil {
		return nil, newErr(ErrSourceUnreadable, "registry index: %v", err)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Rexion-registry/1.0")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, newErr(ErrSourceUnreadable, "registry index: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newErr(ErrSourceUnreadable, "registry index: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, defaultFetchLimit))
	if err != nil {
		return nil, newErr(ErrSourceUnreadable, "registry index: read body: %v", err)
	}
	var idx skillsIndexFile
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, newErr(ErrInvalidManifest, "registry index: parse: %v", err)
	}
	// Normalise: drop entries without a name, sort by name for stable output.
	out := make([]SkillRegistryEntry, 0, len(idx.Skills))
	for _, e := range idx.Skills {
		if strings.TrimSpace(e.Name) == "" {
			continue
		}
		if e.SourceType == "" {
			e.SourceType = "file"
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Search implements SkillRegistry. The query is matched case-insensitively
// against name, description, author, category, and tags.
func (r *GitHubRegistry) Search(query string) ([]SkillRegistryEntry, error) {
	all, err := r.loadIndex()
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return all, nil
	}
	var matched []SkillRegistryEntry
	for _, e := range all {
		if matchEntry(e, q) {
			matched = append(matched, e)
		}
	}
	return matched, nil
}

// List implements SkillRegistry. An empty category returns every entry;
// otherwise the result is restricted to entries whose Category matches
// case-insensitively.
func (r *GitHubRegistry) List(category string) ([]SkillRegistryEntry, error) {
	all, err := r.loadIndex()
	if err != nil {
		return nil, err
	}
	cat := strings.ToLower(strings.TrimSpace(category))
	if cat == "" {
		return all, nil
	}
	var out []SkillRegistryEntry
	for _, e := range all {
		if strings.ToLower(e.Category) == cat {
			out = append(out, e)
		}
	}
	return out, nil
}

// Fetch implements SkillRegistry.
func (r *GitHubRegistry) Fetch(name string) (SkillRegistryEntry, error) {
	all, err := r.loadIndex()
	if err != nil {
		return SkillRegistryEntry{}, err
	}
	for _, e := range all {
		if e.Name == name {
			return e, nil
		}
	}
	return SkillRegistryEntry{}, newErr(ErrManifestMissing, "skill %q not found in registry", name)
}

// matchEntry reports whether the lowercase query appears in any searchable
// field of the entry. Tags are scanned individually so "review" matches a
// "code-review" tag even though the tag isn't split on punctuation.
func matchEntry(e SkillRegistryEntry, q string) bool {
	if strings.Contains(strings.ToLower(e.Name), q) ||
		strings.Contains(strings.ToLower(e.Description), q) ||
		strings.Contains(strings.ToLower(e.Author), q) ||
		strings.Contains(strings.ToLower(e.Category), q) {
		return true
	}
	for _, tag := range e.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			return true
		}
	}
	return false
}

// Refresh forces a refetch of the index regardless of cache freshness. The
// CLI `skill update` subcommand calls this so users can pull the latest
// catalogue on demand. Returns the entry count after refresh.
func (r *GitHubRegistry) Refresh() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := r.fetchRemote(context.Background())
	if err != nil {
		return 0, err
	}
	r.cached = entries
	r.cachedAt = time.Now()
	r.writeDiskCache(entries)
	return len(entries), nil
}
