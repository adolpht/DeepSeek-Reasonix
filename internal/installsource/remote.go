package installsource

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"rexion/internal/config"
	"rexion/internal/skill"
)

// RemoteInstallOptions configures a one-off remote skill install. It is the
// public entry point used by the CLI skill-market subcommand; the heavy
// lifting (canonical path resolution, verifySkill) reuses the installSourceTool
// machinery so remote and local installs land in the exact same layout.
type RemoteInstallOptions struct {
	// ProjectRoot is the active workspace root. Empty falls back to os.Getwd.
	ProjectRoot string
	// HomeDir is the user's home directory. Empty falls back to os.UserHomeDir.
	HomeDir string
	// HTTPClient overrides the default client. When nil a guarded client is
	// built via NewTool (SSRF-safe, 30s timeout).
	HTTPClient *http.Client
	// Logf receives progress lines (download started, clone done, verified).
	// nil disables progress reporting.
	Logf func(format string, args ...any)
}

// remoteFetchTimeout caps a single skill-file download. Skill bodies are small
// (a few KB), but the bound keeps a hung CDN from blocking the install forever.
const remoteFetchTimeout = 30 * time.Second

// InstallFromRemote downloads and installs a skill described by a registry
// entry. It dispatches on entry.SourceType:
//
//   - "file": HTTP-GET the single .md body and write it to the canonical
//     <scope>/skills/<name>/SKILL.md path.
//   - "git":  shallow-clone the repository to a temp dir, scan it for skill
//     candidates, and copy the matching one into the canonical layout.
//
// After the write, verifySkill confirms the new skill is discoverable through
// a freshly built Store. The function returns nil only when verification
// succeeds; a partial write (file copied but not discoverable) is an error.
func InstallFromRemote(opts RemoteInstallOptions, entry SkillRegistryEntry, scope, mode string) error {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	// Reuse NewTool so SSRF guarding, root/home resolution, and the canonical
	// path helpers all match the local install path exactly.
	tool := NewTool(Options{
		ProjectRoot: opts.ProjectRoot,
		HomeDir:     opts.HomeDir,
		HTTPClient:  opts.HTTPClient,
	})
	t := tool.(*installSourceTool)
	return t.installFromRemote(logf, entry, scope, mode)
}

// installFromRemote is the unexported backend. It lives on installSourceTool so
// it can call skillCanonicalPath, skillInstallRoot, and verifySkill directly.
func (t *installSourceTool) installFromRemote(logf func(string, ...any), entry SkillRegistryEntry, scope, mode string) error {
	name := strings.TrimSpace(entry.Name)
	if !config.IsValidSkillName(name) {
		return newErr(ErrInvalidManifest, "invalid skill name %q", name)
	}
	if scope == "" {
		scope = t.normalizeScope(scope)
	}
	// mode only matters for git sources that resolve to a directory; file
	// sources always end up as a single SKILL.md so "copy" is the only sane
	// outcome. We accept "link" for compatibility but treat it as copy here —
	// a remote file has no local path to symlink to.
	if mode == "" {
		mode = "copy"
	}

	// Refuse to clobber an existing install. The user can uninstall first.
	if conflict, err := t.skillConflictTargets(name, scope); err != nil {
		return err
	} else {
		for _, p := range conflict {
			if _, err := lstat(p); err == nil {
				return newErr(ErrAlreadyExists, "skill %q already exists at %s; uninstall it first", name, p)
			}
		}
	}

	canonical, err := t.skillCanonicalPath(name, scope)
	if err != nil {
		return err
	}
	installRoot, err := t.skillInstallRoot(scope)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		return newErr(ErrSourceUnreadable, "create skill dir: %v", err)
	}

	switch strings.ToLower(entry.SourceType) {
	case "", "file":
		logf("downloading %s from %s", name, entry.SourceURL)
		body, err := t.fetchSkillFile(entry.SourceURL)
		if err != nil {
			return err
		}
		// Validate the downloaded content so a 404 HTML page never becomes a
		// "skill". strict=true requires name+description frontmatter.
		cand, err := parseSkillContent(string(body), name, entry.SourceURL, true)
		if err != nil {
			return err
		}
		if err := writeNewFile(canonical, []byte(cand.Content)); err != nil {
			return newErr(ErrSourceUnreadable, "write %s: %v", canonical, err)
		}
		logf("installed %s → %s", name, displayInstallPath(canonical))
	case "git":
		logf("cloning git source for %s", name)
		skillPath, err := t.installFromGit(logf, entry, canonical, installRoot)
		if err != nil {
			return err
		}
		logf("installed %s → %s", name, displayInstallPath(skillPath))
	default:
		return newErr(ErrInvalidManifest, "unsupported source_type %q for skill %q", entry.SourceType, name)
	}

	// Verify the install is discoverable. We build a dummy action to carry the
	// canonical path / warnings back to the caller's logf.
	act := &action{Kind: "skill", Name: name, Scope: scope, Mode: mode}
	if err := t.verifySkill(scope, name, act); err != nil {
		// Verification failed — leave the file on disk for the user to inspect
		// (we never rm -rf), but surface the error so the CLI doesn't claim success.
		for _, w := range act.Warnings {
			logf("warning: %s", w)
		}
		return err
	}
	if act.Discoverable {
		logf("verified: %s is discoverable", name)
	}
	for _, w := range act.Warnings {
		logf("warning: %s", w)
	}
	return nil
}

// fetchSkillFile downloads a single .md body. It reuses the tool's SSRF-guarded
// client so a malicious entry can't reach cloud metadata / internal services.
func (t *installSourceTool) fetchSkillFile(sourceURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), remoteFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, newErr(ErrSourceUnreadable, "%s: %v", sourceURL, err)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Rexion-install/1.0")
	}
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, newErr(ErrSourceUnreadable, "%s: %v", sourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, newErr(ErrAuthRequired, "%s: HTTP %d", sourceURL, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newErr(ErrSourceUnreadable, "%s: HTTP %d", sourceURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, defaultFetchLimit))
	if err != nil {
		return nil, newErr(ErrSourceUnreadable, "%s: read body: %v", sourceURL, err)
	}
	return body, nil
}

// installFromGit clones the entry's repository shallowly into a temp dir,
// scans it for skill candidates, and copies the matching one into the canonical
// layout. The temp dir is removed afterwards; a failed copy leaves the temp
// dir in place for debugging only when REXION_DEBUG_REMOTE=1.
func (t *installSourceTool) installFromGit(logf func(string, ...any), entry SkillRegistryEntry, canonical, installRoot string) (string, error) {
	repoURL := strings.TrimSpace(entry.SourceURL)
	if repoURL == "" {
		return "", newErr(ErrInvalidManifest, "git skill %q has empty source_url", entry.Name)
	}
	tmp, err := os.MkdirTemp("", "rexion-skill-*")
	if err != nil {
		return "", newErr(ErrSourceUnreadable, "create temp dir: %v", err)
	}
	keepTemp := os.Getenv("REXION_DEBUG_REMOTE") == "1"
	if !keepTemp {
		defer os.RemoveAll(tmp)
	}

	// git clone --depth 1 keeps the download small. We disable interactive
	// prompts and credential helpers so a private repo fails fast instead of
	// hanging on a TTY that doesn't exist.
	cmd := exec.Command("git", "clone", "--depth", "1", "--quiet", repoURL, tmp)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		keepForDebug := strings.TrimSpace(string(out))
		if keepForDebug == "" {
			keepForDebug = err.Error()
		}
		return "", newErr(ErrSourceUnreadable, "git clone %s: %s", repoURL, keepForDebug)
	}
	logf("cloned %s", repoURL)

	// Scan the clone for skill candidates. strict=false so a missing description
	// on a sibling skill doesn't block installing the one we asked for.
	cands, err := scanSkillRoot(tmp, false)
	if err != nil {
		return "", newErr(ErrInvalidManifest, "scan %s: %v", repoURL, err)
	}
	var match *skillCandidate
	for i := range cands {
		if cands[i].Name == entry.Name {
			match = &cands[i]
			break
		}
	}
	if match == nil {
		var found []string
		for _, c := range cands {
			found = append(found, c.Name)
		}
		return "", newErr(ErrManifestMissing, "skill %q not found in %s; candidates: [%s]", entry.Name, repoURL, strings.Join(found, ", "))
	}

	// Directory-layout skill: copy the whole <name>/ folder. Flat skill: write
	// the single file to the canonical SKILL.md path so future installs all
	// use the directory layout.
	if match.IsDir {
		dst := filepath.Join(installRoot, match.Name)
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return "", newErr(ErrSourceUnreadable, "create %s: %v", dst, err)
		}
		if err := copyDir(match.SourcePath, dst); err != nil {
			return "", err
		}
		return filepath.Join(dst, skill.SkillFile), nil
	}
	if err := writeNewFile(canonical, []byte(match.Content)); err != nil {
		return "", newErr(ErrSourceUnreadable, "write %s: %v", canonical, err)
	}
	return canonical, nil
}

// displayInstallPath shortens an absolute install path for readable log lines.
// It trims a known home prefix to "~" so logs read like ~/.rexion/skills/…
// instead of an opaque absolute path. When the path doesn't match the home
// prefix it is returned unchanged.
func displayInstallPath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}
