// Package registry provides a skill marketplace / registry system that lets
// users discover and install community and official skill packs from curated
// sources (Git repositories, local directories, or JSON index URLs).
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Source represents a skill registry source (e.g. a GitHub repo or JSON index).
type Source struct {
	Name        string `json:"name"`         // Display name, e.g. "Rexion Official"
	URL         string `json:"url"`          // Git repo URL or JSON index URL
	Type        string `json:"type"`         // "git", "index", or "local"
	Description string `json:"description"`  // One-line description
	Trusted     bool   `json:"trusted"`      // Official/verified source
}

// Entry describes a single skill available from a registry source.
type Entry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Source      string   `json:"source"`       // Source name this entry comes from
	RunAs       string   `json:"run_as"`       // "inline" or "subagent"
	Tags        []string `json:"tags"`         // e.g. ["coding", "office", "analysis"]
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Path        string   `json:"path"`         // Relative path within the source repo
	Installed   bool     `json:"installed"`    // Populated locally, not from remote
}

// Index is the JSON structure served by a "index"-type registry source.
type Index struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Entries     []Entry `json:"entries"`
}

// BuiltinSources returns the curated list of built-in registry sources.
// Domestic (China-accessible) sources are listed first for users in China.
func BuiltinSources() []Source {
	return []Source{
		{
			Name:        "Skill Store (Gitee)",
			URL:         "https://gitee.com/shishishe/skill/raw/main/index.json",
			Type:        "index",
			Description: "国内技能商店 — 243个精选技能，涵盖内容创作、视频制作、电商营销、PPT生成等",
			Trusted:     true,
		},
		{
			Name:        "SkillsMP",
			URL:         "https://skillsmp.com/api/v1/skills",
			Type:        "index",
			Description: "全球最大的 Agent 技能市场 — 280,000+ 开源技能，支持 Claude Code / Codex CLI / ChatGPT",
			Trusted:     true,
		},
		{
			Name:        "OpenAgentSkill",
			URL:         "https://www.openagentskill.com/api/skills",
			Type:        "index",
			Description: "开放 Agent 技能注册表 — 20,000+ 技能，带任务匹配和安全审计",
			Trusted:     true,
		},
		{
			Name:        "Rexion Official",
			URL:         "https://raw.githubusercontent.com/Rexion/skills/main/index.json",
			Type:        "index",
			Description: "Rexion 官方技能集合 — 编程、办公、生产力技能",
			Trusted:     true,
		},
	}
}

// CacheDir returns the directory used for caching registry data.
func CacheDir(homeDir string) string {
	return filepath.Join(homeDir, ".rexion", "registry-cache")
}

// Registry provides skill discovery from multiple sources.
type Registry struct {
	sources  []Source
	homeDir  string
	client   *http.Client
	cacheTTL time.Duration
}

// Options configures a Registry.
type Options struct {
	HomeDir  string
	Sources  []Source   // Additional user-configured sources (merged with builtins)
	CacheTTL time.Duration // How long to trust cached index data (default 1h)
}

// New creates a new Registry.
func New(opts Options) *Registry {
	sources := BuiltinSources()
	// Merge user sources (by name dedup)
	existing := map[string]bool{}
	for _, s := range sources {
		existing[s.Name] = true
	}
	for _, s := range opts.Sources {
		if !existing[s.Name] {
			sources = append(sources, s)
		}
	}

	ttl := opts.CacheTTL
	if ttl == 0 {
		ttl = time.Hour
	}

	return &Registry{
		sources:  sources,
		homeDir:  opts.HomeDir,
		client:   &http.Client{Timeout: 30 * time.Second},
		cacheTTL: ttl,
	}
}

// Sources returns all configured registry sources.
func (r *Registry) Sources() []Source {
	return r.sources
}

// ListEntries fetches and returns all available skill entries from all sources.
// It uses cached data when available and fresh; fetches from remote on cache miss.
func (r *Registry) ListEntries(ctx context.Context) ([]Entry, error) {
	var all []Entry
	for _, src := range r.sources {
		entries, err := r.entriesFromSource(ctx, src)
		if err != nil {
			// Log but don't fail — one source being down shouldn't block others
			continue
		}
		all = append(all, entries...)
	}
	return all, nil
}

// SearchEntries searches available skills by name, description, or tags.
func (r *Registry) SearchEntries(ctx context.Context, query string) ([]Entry, error) {
	all, err := r.ListEntries(ctx)
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var matched []Entry
	for _, e := range all {
		if strings.Contains(strings.ToLower(e.Name), query) ||
			strings.Contains(strings.ToLower(e.Description), query) ||
			strings.Contains(strings.ToLower(e.Author), query) ||
			containsTag(e.Tags, query) {
			matched = append(matched, e)
		}
	}
	return matched, nil
}

// GetEntry finds a specific skill entry by name.
func (r *Registry) GetEntry(ctx context.Context, name string) (*Entry, error) {
	all, err := r.ListEntries(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		if e.Name == name {
			return &e, nil
		}
	}
	return nil, fmt.Errorf("skill %q not found in any registry source", name)
}

// InstallSkill downloads a skill from a registry source and installs it.
// Returns the installed skill file path.
func (r *Registry) InstallSkill(ctx context.Context, entry Entry, installDir string) (string, error) {
	if entry.Source == "" {
		return "", fmt.Errorf("entry has no source")
	}

	// Find the source
	var src *Source
	for i := range r.sources {
		if r.sources[i].Name == entry.Source {
			src = &r.sources[i]
			break
		}
	}
	if src == nil {
		return "", fmt.Errorf("source %q not found", entry.Source)
	}

	switch src.Type {
	case "index":
		return r.installFromIndex(ctx, *src, entry, installDir)
	case "git":
		return r.installFromGit(ctx, *src, entry, installDir)
	case "local":
		return r.installFromLocal(*src, entry, installDir)
	default:
		return "", fmt.Errorf("unsupported source type: %s", src.Type)
	}
}

// --- Internal ---

func (r *Registry) entriesFromSource(ctx context.Context, src Source) ([]Entry, error) {
	switch src.Type {
	case "index":
		return r.entriesFromIndex(ctx, src)
	case "git":
		// For git sources, we'd clone and scan — for now, delegate to index
		return nil, fmt.Errorf("git source scanning not yet implemented")
	case "local":
		return r.entriesFromLocal(src)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", src.Type)
	}
}

func (r *Registry) entriesFromIndex(ctx context.Context, src Source) ([]Entry, error) {
	data, err := r.fetchCached(ctx, src)
	if err != nil {
		return nil, err
	}

	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse index from %s: %w", src.Name, err)
	}

	// Stamp each entry with the source name
	for i := range idx.Entries {
		idx.Entries[i].Source = src.Name
	}
	return idx.Entries, nil
}

func (r *Registry) entriesFromLocal(src Source) ([]Entry, error) {
	// Read local directory as a skill root
	entries, err := scanLocalSkillDir(src.URL)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].Source = src.Name
	}
	return entries, nil
}

// fetchCached fetches data from cache if fresh, otherwise from HTTP.
func (r *Registry) fetchCached(ctx context.Context, src Source) ([]byte, error) {
	cachePath := r.cachePath(src)

	// Try cache first
	if info, err := os.Stat(cachePath); err == nil {
		if time.Since(info.ModTime()) < r.cacheTTL {
			return os.ReadFile(cachePath)
		}
	}

	// Fetch from remote
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		// Fall back to stale cache on network error
		if data, err := os.ReadFile(cachePath); err == nil {
			return data, nil
		}
		return nil, fmt.Errorf("fetch %s: %w", src.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Fall back to stale cache
		if data, err := os.ReadFile(cachePath); err == nil {
			return data, nil
		}
		return nil, fmt.Errorf("fetch %s: HTTP %d", src.Name, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Write to cache
	os.MkdirAll(filepath.Dir(cachePath), 0o755)
	os.WriteFile(cachePath, data, 0o644)

	return data, nil
}

func (r *Registry) cachePath(src Source) string {
	safe := safeFilename(src.Name)
	return filepath.Join(CacheDir(r.homeDir), safe+".json")
}

func (r *Registry) installFromIndex(ctx context.Context, src Source, entry Entry, installDir string) (string, error) {
	// Fetch the index to get the base URL
	data, err := r.fetchCached(ctx, src)
	if err != nil {
		return "", err
	}

	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return "", err
	}

	// Find the entry in the index to get its path
	var found *Entry
	for i := range idx.Entries {
		if idx.Entries[i].Name == entry.Name {
			found = &idx.Entries[i]
			break
		}
	}
	if found == nil {
		return "", fmt.Errorf("skill %q not found in index %s", entry.Name, src.Name)
	}

	// Construct the download URL (base URL directory + entry path)
	baseURL := strings.TrimSuffix(src.URL, filepath.Ext(src.URL)) // Remove .json
	baseURL = strings.TrimSuffix(baseURL, "/index")               // Remove /index
	skillURL := baseURL + "/" + found.Path

	// Download the skill content
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, skillURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download skill %s: %w", entry.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download skill %s: HTTP %d", entry.Name, resp.StatusCode)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Install to target directory
	return writeSkillFile(installDir, entry.Name, content)
}

func (r *Registry) installFromGit(ctx context.Context, src Source, entry Entry, installDir string) (string, error) {
	return "", fmt.Errorf("git source installation not yet implemented")
}

func (r *Registry) installFromLocal(src Source, entry Entry, installDir string) (string, error) {
	// Copy from local source directory
	srcPath := filepath.Join(src.URL, entry.Path)
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("read local skill %s: %w", entry.Name, err)
	}
	return writeSkillFile(installDir, entry.Name, content)
}

// writeSkillFile writes a skill file to the install directory.
func writeSkillFile(installDir, name string, content []byte) (string, error) {
	skillDir := filepath.Join(installDir, name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return "", err
	}
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, content, 0o644); err != nil {
		return "", err
	}
	return skillFile, nil
}

// scanLocalSkillDir scans a local directory for skill entries.
func scanLocalSkillDir(dir string) ([]Entry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var result []Entry
	for _, de := range dirEntries {
		if de.IsDir() {
			skillFile := filepath.Join(dir, de.Name(), "SKILL.md")
			if _, err := os.Stat(skillFile); err == nil {
				result = append(result, Entry{
					Name:   de.Name(),
					Path:   filepath.Join(de.Name(), "SKILL.md"),
					RunAs:  "subagent",
					Author: "local",
				})
			}
		} else if strings.HasSuffix(strings.ToLower(de.Name()), ".md") {
			name := strings.TrimSuffix(de.Name(), filepath.Ext(de.Name()))
			result = append(result, Entry{
				Name:   name,
				Path:   de.Name(),
				RunAs:  "inline",
				Author: "local",
			})
		}
	}
	return result, nil
}

func containsTag(tags []string, query string) bool {
	for _, t := range tags {
		if strings.Contains(strings.ToLower(t), query) {
			return true
		}
	}
	return false
}

func safeFilename(name string) string {
	r := strings.NewReplacer(" ", "-", "/", "-", ":", "-", ".", "-")
	return r.Replace(strings.ToLower(name))
}
