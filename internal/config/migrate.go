package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MigrateReasonixIfNeeded performs a one-time migration of data directories from
// the previous brand name ("Reasonix") to "Rexion". It handles:
//   - ~/.reasonix/ → ~/.rexion/          (home dot-dir)
//   - <UserConfigDir>/Reasonix/ → <UserConfigDir>/Rexion/  (XDG/AppData config dir)
//   - REASONIX_* env vars → REXION_* env vars (backward compat, REXION_* takes priority)
//
// The migration is non-destructive: the old directory is copied then removed, so
// no data is duplicated. A marker file is written to prevent re-migration. If the
// new directory already exists, the old one is left untouched (the user may have
// intentionally kept both).
func MigrateReasonixIfNeeded() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	migrateRenamedDir(filepath.Join(home, ".reasonix"), filepath.Join(home, ".rexion"), "~/.reasonix → ~/.rexion")

	configDir, err := os.UserConfigDir()
	if err != nil {
		return
	}
	migrateRenamedDir(filepath.Join(configDir, "Reasonix"), filepath.Join(configDir, "Rexion"), "config dir Reasonix → Rexion")

	// Backward-compatible environment variable forwarding: if a user still has
	// REASONIX_HOME set (but not REXION_HOME), mirror it so the code that reads
	// REXION_HOME picks it up without the user having to change their shell
	// profile. REXION_* always wins when both are set.
	migrateLegacyEnv("REASONIX_HOME", "REXION_HOME")
	migrateLegacyEnv("REASONIX_CACHE_DIR", "REXION_CACHE_DIR")
	migrateLegacyEnv("REASONIX_DATA_DIR", "REXION_DATA_DIR")
	migrateLegacyEnv("REASONIX_LANG", "REXION_LANG")
	migrateLegacyEnv("REASONIX_THEME", "REXION_THEME")
	migrateLegacyEnv("REASONIX_THEME_STYLE", "REXION_THEME_STYLE")
	migrateLegacyEnv("REASONIX_DEV", "REXION_DEV")
}

// migrateLegacyEnv sets newName to oldName's value when newName is unset but
// oldName is set. This provides backward compatibility for the brand rename
// without requiring users to update their shell profiles immediately.
func migrateLegacyEnv(oldName, newName string) {
	if os.Getenv(newName) == "" {
		if v := os.Getenv(oldName); v != "" {
			os.Setenv(newName, v)
		}
	}
}

// migrateRenamedDir moves srcDir to destDir if: srcDir exists, destDir does not
// exist, and the migration marker is absent. On success a marker file is written
// inside destDir so the migration never runs again.
func migrateRenamedDir(srcDir, destDir, label string) {
	marker := filepath.Join(destDir, ".migrated-from-reasonix")
	if _, err := os.Stat(marker); err == nil {
		return // already migrated
	}
	if info, err := os.Stat(srcDir); err != nil || !info.IsDir() {
		return // source does not exist
	}
	if _, err := os.Stat(destDir); err == nil {
		// dest already exists — don't clobber, just mark as done
		writeMigrationMarker(marker, label)
		return
	}
	// Copy directory tree (rename fails across filesystems on some setups).
	if err := copyDir(srcDir, destDir); err != nil {
		slog.Warn("brand migration: copy failed", "from", srcDir, "to", destDir, "err", err)
		return
	}
	// Remove the old directory tree after a successful copy.
	if err := os.RemoveAll(srcDir); err != nil {
		slog.Warn("brand migration: could not remove old dir", "path", srcDir, "err", err)
	}
	writeMigrationMarker(marker, label)
	slog.Info("brand migration: renamed data directory", "label", label)
}

func writeMigrationMarker(marker, label string) {
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(marker, []byte("Migrated by Rexion from Reasonix: "+label+"\n"), 0o644)
}

// copyDir recursively copies a directory tree from src to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// legacyConfig is the subset of the v0.x (~/.rexion/config.json) schema this
// import carries forward. Fields absent here are dropped on purpose: desktop tab
// state is frontend-owned, and skills already live in the shared ~/.rexion/skills
// root that v1+ also scans, so they need no migration.
type legacyConfig struct {
	APIKey      string                       `json:"apiKey"`
	BaseURL     string                       `json:"baseUrl"`
	Lang        string                       `json:"lang"`
	MCPServers  map[string]legacyMCPServer   `json:"mcpServers"`
	MCPEnv      map[string]map[string]string `json:"mcpEnv"`
	MCPDisabled []string                     `json:"mcpDisabled"`
}

type legacyMCPServer struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	Transport string            `json:"transport"`
	Type      string            `json:"type"`
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers"`
	Disabled  bool              `json:"disabled"`
}

// MigrationResult summarizes a one-time legacy import for the boot-time notice.
type MigrationResult struct {
	From     string
	To       string
	KeyToEnv bool
	Plugins  int
	Warnings []string
}

func (r *MigrationResult) Notice() string {
	var b strings.Builder
	fmt.Fprintf(&b, "migrated your previous configuration: %s → %s", r.From, r.To)
	if r.Plugins > 0 {
		fmt.Fprintf(&b, " (%d MCP server(s))", r.Plugins)
	}
	if r.KeyToEnv {
		b.WriteString("; API key saved to Rexion's credentials store")
	}
	b.WriteString(". The old files were left untouched.")
	for _, w := range r.Warnings {
		b.WriteString("\n  note: " + w)
	}
	return b.String()
}

// MigrateLegacyIfNeeded performs a one-time, non-destructive import of older
// installs into the current user config when the latter does not exist yet. It
// checks v1-era TOML first, then v0.5/v0.x ~/.rexion/config.json, and never
// modifies or deletes the legacy files. Returns nil when there is nothing to
// migrate, or when the current user config already exists.
func MigrateLegacyIfNeeded() (*MigrationResult, error) {
	dest := userConfigPath()
	if dest == "" {
		return nil, nil
	}
	if _, err := os.Stat(dest); err == nil {
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, nil
	}
	if res, err := migrateLegacyTOMLIfNeeded(dest, home); res != nil || err != nil {
		return res, err
	}
	src := filepath.Join(home, ".rexion", "config.json")
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, nil
	}
	var legacy legacyConfig
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}) // tolerate a UTF-8 BOM (some editors add one)
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, fmt.Errorf("parse legacy config %s: %w", src, err)
	}

	cfg := Default()
	res := &MigrationResult{From: src, To: dest}
	if legacy.Lang != "" {
		cfg.Language = legacy.Lang
		_ = cfg.SetDesktopLanguage(legacy.Lang)
	}
	migrateLegacyBaseURL(cfg, legacy.BaseURL)
	cfg.Plugins = legacyPlugins(legacy)
	res.Plugins = len(cfg.Plugins)

	var envLines []string
	if key := strings.TrimSpace(legacy.APIKey); key != "" {
		envLines = append(envLines, "DEEPSEEK_API_KEY="+key)
		res.KeyToEnv = true
		if base := strings.TrimSpace(legacy.BaseURL); base != "" && !strings.Contains(base, "deepseek.com") {
			res.Warnings = append(res.Warnings, "your previous base_url was "+base+
				" — it was applied to the built-in DeepSeek providers; verify models if this endpoint is not DeepSeek-compatible")
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	if err := cfg.WriteFile(dest); err != nil {
		return nil, fmt.Errorf("write %s: %w", dest, err)
	}
	if len(envLines) > 0 {
		if err := writeCredentialsEnv(home, envLines); err != nil {
			return res, fmt.Errorf("write credentials: %w", err)
		}
	}
	return res, nil
}

func migrateLegacyTOMLIfNeeded(dest, home string) (*MigrationResult, error) {
	for _, src := range legacyTOMLPaths(dest, home) {
		if src == "" || filepath.Clean(src) == filepath.Clean(dest) {
			continue
		}
		if _, err := os.Stat(src); err != nil {
			continue
		}
		cfg := Default()
		if err := mergeFile(cfg, src); err != nil {
			return nil, fmt.Errorf("parse legacy config %s: %w", src, err)
		}
		cfg.ConfigVersion = Default().ConfigVersion
		if strings.TrimSpace(cfg.Desktop.CloseBehavior) == "" && strings.TrimSpace(cfg.UI.CloseBehavior) != "" {
			cfg.Desktop.CloseBehavior = cfg.DesktopCloseBehavior()
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, fmt.Errorf("create config dir: %w", err)
		}
		if err := cfg.WriteFile(dest); err != nil {
			return nil, fmt.Errorf("write %s: %w", dest, err)
		}
		return &MigrationResult{From: src, To: dest, Plugins: len(cfg.Plugins)}, nil
	}
	return nil, nil
}

func legacyTOMLPaths(dest, home string) []string {
	paths := []string{filepath.Join(filepath.Dir(dest), "Rexion.toml")}
	if home != "" {
		paths = append(paths, filepath.Join(home, ".rexion", "Rexion.toml"))
	}
	return paths
}

func migrateLegacyBaseURL(cfg *Config, baseURL string) {
	baseURL = strings.TrimSpace(baseURL)
	if cfg == nil || baseURL == "" {
		return
	}
	for i := range cfg.Providers {
		if cfg.Providers[i].APIKeyEnv == "DEEPSEEK_API_KEY" {
			cfg.Providers[i].BaseURL = baseURL
		}
	}
}

func legacyPlugins(legacy legacyConfig) []PluginEntry {
	if len(legacy.MCPServers) == 0 {
		return nil
	}
	disabled := make(map[string]bool, len(legacy.MCPDisabled))
	for _, n := range legacy.MCPDisabled {
		disabled[n] = true
	}
	names := make([]string, 0, len(legacy.MCPServers))
	for n := range legacy.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]PluginEntry, 0, len(names))
	for _, name := range names {
		s := legacy.MCPServers[name]
		pe := PluginEntry{
			Name:    name,
			Type:    normalizeTransport(firstNonEmpty(s.Type, s.Transport)),
			Command: s.Command,
			Args:    s.Args,
			Env:     mergeEnv(s.Env, legacy.MCPEnv[name]),
			URL:     s.URL,
			Headers: s.Headers,
		}
		if s.Disabled || disabled[name] {
			off := false
			pe.AutoStart = &off
		}
		pe, _ = NormalizePluginCommandLine(pe)
		out = append(out, pe)
	}
	return out
}

// normalizeTransport maps the v0.x transport names to v1+ plugin types. stdio is
// the default, so it returns "" (RenderTOML then omits the field).
func normalizeTransport(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "http", "streamable-http":
		return "http"
	case "sse":
		return "sse"
	default:
		return ""
	}
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// mergeEnv overlays the per-server env map onto the spec's own env (overlay wins,
// matching v0.x mcpEnv precedence). Returns nil when both are empty.
func mergeEnv(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	return out
}

// writeCredentialsEnv merges lines into the Rexion-owned global credentials
// file (UserCredentialsPath, e.g. %AppData%\Rexion\credentials), replacing any
// existing assignment of the same key, and pins them into the current process env
// so the just-built session resolves the key without a restart. Falls back to
// ~/.env only when the user config dir can't be resolved — never a project .env,
// so a migration keeps secrets out of the user's project tree.
func writeCredentialsEnv(home string, lines []string) error {
	path := UserCredentialsPath()
	if path == "" {
		path = filepath.Join(home, ".env")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	target := make(map[string]bool, len(lines))
	for _, l := range lines {
		if k, _, ok := strings.Cut(l, "="); ok {
			target[strings.TrimSpace(k)] = true
		}
	}
	var kept []string
	if data, err := os.ReadFile(path); err == nil {
		for _, raw := range strings.Split(string(data), "\n") {
			check := strings.TrimPrefix(strings.TrimSpace(raw), "export ")
			if k, _, ok := strings.Cut(check, "="); ok && target[strings.TrimSpace(k)] {
				continue
			}
			kept = append(kept, raw)
		}
		if n := len(kept); n > 0 && kept[n-1] == "" {
			kept = kept[:n-1]
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	var b strings.Builder
	for _, l := range kept {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
		if k, v, ok := strings.Cut(l, "="); ok {
			os.Setenv(strings.TrimSpace(k), v)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
