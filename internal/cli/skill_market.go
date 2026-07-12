package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"rexion/internal/installsource"
	"rexion/internal/skill"
)

// skill_market.go backs `Rexion skill` — a CLI surface for the official
// Skills marketplace. It exposes search/install/list-remote/update/info
// subcommands that drive installsource.GitHubRegistry and
// installsource.InstallFromRemote, so users can browse and install skills
// without entering a chat session.
//
// The command is read-only with respect to the registry cache: every
// subcommand goes through loadIndex() which transparently serves the
// on-disk cache when fresh and falls back to it when the network is down.
// Only `skill update` forces a refetch.

// skillMarketCommand routes `Rexion skill <sub> [args]`.
func skillMarketCommand(args []string) int {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	rest := args[1:]
	switch sub {
	case "search":
		return skillMarketSearch(rest)
	case "install":
		return skillMarketInstall(rest)
	case "list-remote", "list":
		return skillMarketList(rest)
	case "update":
		return skillMarketUpdate(rest)
	case "info":
		return skillMarketInfo(rest)
	case "help", "-h", "--help", "":
		skillMarketUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown skill subcommand %q\n\n", sub)
		skillMarketUsage()
		return 2
	}
}

// newRegistry constructs the default GitHub-backed registry. It is a one-liner
// kept separate so every subcommand shares the same construction and a future
// config override (custom repo / cache dir) has a single place to hook in.
func newRegistry() *installsource.GitHubRegistry {
	return installsource.NewGitHubRegistry(installsource.GitHubRegistryOptions{})
}

// skillMarketSearch implements `Rexion skill search [query]`.
// An empty query lists every entry in the catalogue.
func skillMarketSearch(args []string) int {
	fs := flag.NewFlagSet("skill search", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	query := strings.TrimSpace(strings.Join(fs.Args(), " "))
	reg := newRegistry()
	entries, err := reg.Search(query)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(entries) == 0 {
		if query == "" {
			fmt.Println("No skills in the registry yet. Run `Rexion skill update` to refresh.")
		} else {
			fmt.Printf("No skills matched %q.\n", query)
		}
		return 0
	}
	renderSkillRegistryEntries(entries, query)
	return 0
}

// skillMarketInstall implements `Rexion skill install <name> [--global] [--mode copy|link]`.
// It resolves the entry from the registry, then delegates to
// installsource.InstallFromRemote which performs the download/clone and
// verifySkill post-check.
func skillMarketInstall(args []string) int {
	fs := flag.NewFlagSet("skill install", flag.ContinueOnError)
	global := fs.Bool("global", false, "install into ~/.rexion/skills instead of ./.rexion/skills")
	mode := fs.String("mode", "copy", "install mode: copy or link (link only valid for git sources)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "skill install: missing skill name")
		skillMarketUsage()
		return 2
	}
	name := strings.TrimSpace(rest[0])
	if name == "" {
		fmt.Fprintln(os.Stderr, "skill install: missing skill name")
		return 2
	}

	reg := newRegistry()
	entry, err := reg.Fetch(name)
	if err != nil {
		// Distinguish "not in catalogue" from "catalogue unreachable" so the
		// user knows whether to update or fix their network.
		if errors.Is(err, installsource.ErrManifestMissing) {
			fmt.Fprintf(os.Stderr, "skill %q not found in the registry. Run `Rexion skill search %s` to see close matches.\n", name, name)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return 1
	}

	scope := "project"
	if *global {
		scope = "global"
	}
	logf := func(format string, a ...any) { fmt.Printf(format+"\n", a...) }
	err = installsource.InstallFromRemote(installsource.RemoteInstallOptions{
		Logf: logf,
	}, entry, scope, *mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("\n%s installed (%s scope). Invoke it with /%s in chat.\n", entry.Name, scope, entry.Name)
	return 0
}

// skillMarketList implements `Rexion skill list-remote [--category <cat>]`.
// It prints every entry in the catalogue, optionally narrowed to a category.
func skillMarketList(args []string) int {
	fs := flag.NewFlagSet("skill list-remote", flag.ContinueOnError)
	category := fs.String("category", "", "filter by category (e.g. coding, office, devops)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	reg := newRegistry()
	entries, err := reg.List(*category)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(entries) == 0 {
		if *category == "" {
			fmt.Println("No skills in the registry yet. Run `Rexion skill update` to refresh.")
		} else {
			fmt.Printf("No skills in category %q.\n", *category)
		}
		return 0
	}
	renderSkillRegistryEntries(entries, *category)
	return 0
}

// skillMarketUpdate implements `Rexion skill update`. It forces a refetch of
// the index regardless of cache freshness and reports the new entry count.
func skillMarketUpdate(args []string) int {
	fs := flag.NewFlagSet("skill update", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	reg := newRegistry()
	count, err := reg.Refresh()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Registry refreshed: %d skills available.\n", count)
	return 0
}

// skillMarketInfo implements `Rexion skill info <name>`. It prints the full
// details of a single registry entry.
func skillMarketInfo(args []string) int {
	fs := flag.NewFlagSet("skill info", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "skill info: missing skill name")
		return 2
	}
	name := strings.TrimSpace(rest[0])
	reg := newRegistry()
	entry, err := reg.Fetch(name)
	if err != nil {
		if errors.Is(err, installsource.ErrManifestMissing) {
			fmt.Fprintf(os.Stderr, "skill %q not found in the registry.\n", name)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return 1
	}
	renderSkillRegistryInfo(entry)
	return 0
}

// renderSkillRegistryEntries prints a compact list of registry entries to
// stdout. It mirrors the layout used by renderRegistryEntries in skill_view.go
// (which renders the in-session /skills browse view) so the CLI and chat UI
// stay visually consistent.
func renderSkillRegistryEntries(entries []installsource.SkillRegistryEntry, filter string) {
	fmt.Printf("%-22s  %-9s  %-10s  %s\n", "NAME", "TYPE", "CATEGORY", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 78))
	for _, e := range entries {
		desc := e.Description
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		sourceType := e.SourceType
		if sourceType == "" {
			sourceType = "file"
		}
		fmt.Printf("%-22s  %-9s  %-10s  %s\n", e.Name, sourceType, e.Category, desc)
	}
	fmt.Printf("\n%d skill(s)%s.\n", len(entries), scopeSuffix(filter))
	fmt.Println("Install with: Rexion skill install <name> [--global]")
}

// renderSkillRegistryInfo prints the full details of one registry entry.
func renderSkillRegistryInfo(e installsource.SkillRegistryEntry) {
	sourceType := e.SourceType
	if sourceType == "" {
		sourceType = "file"
	}
	fmt.Printf("name:        %s\n", e.Name)
	if e.Description != "" {
		fmt.Printf("description: %s\n", e.Description)
	}
	if e.Category != "" {
		fmt.Printf("category:    %s\n", e.Category)
	}
	fmt.Printf("source_type: %s\n", sourceType)
	if e.SourceURL != "" {
		fmt.Printf("source_url:  %s\n", e.SourceURL)
	}
	if e.Author != "" {
		fmt.Printf("author:      %s\n", e.Author)
	}
	if e.Version != "" {
		fmt.Printf("version:     %s\n", e.Version)
	}
	if len(e.Tags) > 0 {
		fmt.Printf("tags:        %s\n", strings.Join(e.Tags, ", "))
	}
	// Confirm whether this skill is already installed locally so the user
	// gets a clear "install / reinstall / already present" signal.
	if installed := locallyInstalledSkills(); installed != nil {
		if _, ok := installed[e.Name]; ok {
			fmt.Println("\nstatus:      installed locally")
		} else {
			fmt.Println("\nstatus:      not installed (run `Rexion skill install " + e.Name + "`)")
		}
	}
}

// locallyInstalledSkills returns a set of skill names already installed in
// any scope. It is best-effort: when the skill store can't be built (no home
// dir, broken config) it returns nil and the info view omits the status line.
func locallyInstalledSkills() map[string]struct{} {
	store := skill.New(skill.Options{})
	out := map[string]struct{}{}
	for _, s := range store.List() {
		out[s.Name] = struct{}{}
	}
	return out
}

// scopeSuffix builds the trailing qualifier for the summary line, e.g.
// " matching \"review\"" when a filter was supplied.
func scopeSuffix(filter string) string {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return ""
	}
	return fmt.Sprintf(" matching %q", filter)
}

func skillMarketUsage() {
	fmt.Print(`Rexion skill — browse and install skills from the official marketplace

Usage:
  Rexion skill search [query]            search the catalogue (empty query lists all)
  Rexion skill install <name> [--global] install a skill by name
  Rexion skill list-remote [--category c] list every skill in the catalogue
  Rexion skill update                     force-refresh the local cache
  Rexion skill info <name>                show full details for one skill

The marketplace index is cached locally for 24h; ` + "`skill update`" + ` forces a refetch.
Offline runs fall back to the cached copy.
`)
}
