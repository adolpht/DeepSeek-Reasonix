package cli

import (
	"fmt"
	"strings"

	"rexion/internal/registry"
	"rexion/internal/skill"
)

const skillShowMaxLines = 80

func renderSkillList(width int, skills []skill.Skill, disabled map[string]bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("skills (%d)", len(skills)))
	for _, s := range skills {
		name := "/" + s.Name
		scope := "(" + string(s.Scope) + ")"
		tag := ""
		if s.RunAs == skill.RunSubagent {
			tag = "  " + viewStatus("subagent")
		}
		if disabled[s.Name] {
			tag += "  " + viewMeta("disabled")
		}
		used := 2 + viewPadWidth(name, 18) + 1 + visibleWidth(scope) + 2 + visibleWidth(tag)
		desc := viewCompactText(s.Description, viewBudget(width, used))
		fmt.Fprintf(&b, "  %-18s %s  %s%s\n", name, viewMeta(scope), desc, tag)
	}
	b.WriteString(viewHint(viewCompactText("invoke: /<name> [args] · manage: /skills manage · author: /skills new <name>", viewBudget(width, 2))))
	return strings.TrimRight(b.String(), "\n")
}

func renderSkillShow(width int, s skill.Skill, disabled bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", viewHeader("skill:"), viewCompactText(s.Name, viewBudget(width, 7)))
	if s.RunAs == skill.RunSubagent {
		fmt.Fprintf(&b, "  %s  %s\n", viewMeta(string(s.Scope)), viewStatus("subagent"))
	} else {
		fmt.Fprintf(&b, "  %s\n", viewMeta(string(s.Scope)))
	}
	if disabled {
		fmt.Fprintf(&b, "  %s\n", viewMeta("disabled"))
	}
	if strings.TrimSpace(s.Description) != "" {
		fmt.Fprintf(&b, "  %s\n", viewCompactText(s.Description, viewBudget(width, 2)))
	}
	if strings.TrimSpace(s.Path) != "" {
		fmt.Fprintf(&b, "  %s\n", viewMeta(viewCompactPath(s.Path, viewBudget(width, 2))))
	}
	body, extra := viewBodyPreview(s.Body, skillShowMaxLines)
	if strings.TrimSpace(body) != "" {
		b.WriteString("\n")
		b.WriteString(viewProtectLines(body, width))
	}
	if extra > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(viewMore(extra, "lines"))
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderSkillPaths(width int, roots []skill.Root) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("skill paths"))
	for _, r := range roots {
		leftWidth := 2 + 4 + 8 + 1 + 13 + 1
		scope := viewMeta(fmt.Sprintf("%-8s", string(r.Scope)))
		status := fmt.Sprintf("%-13s", string(r.Status))
		if r.Status == skill.StatusOK {
			status = viewStatus(status)
		} else {
			status = viewMeta(status)
		}
		fmt.Fprintf(&b, "  %2d. %s %s %s\n",
			r.Priority+1, scope, status, viewCompactPath(r.Dir, viewBudget(width, leftWidth)))
	}
	b.WriteString(viewHint(viewCompactText("priority: project > custom > global > builtin · configure [skills] paths in Rexion.toml", viewBudget(width, 2))))
	return strings.TrimRight(b.String(), "\n")
}

// --- Registry rendering ---

func renderRegistryEntries(width int, entries []registry.Entry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("skill marketplace (%d available)", len(entries)))
	for _, e := range entries {
		name := e.Name
		tag := ""
		if e.Installed {
			tag = "  " + viewStatus("installed")
		}
		runAs := ""
		if e.RunAs == "subagent" {
			runAs = "  " + viewMeta("subagent")
		}
		source := viewMeta(fmt.Sprintf("[%s]", e.Source))
		used := 2 + viewPadWidth(name, 22) + 1 + visibleWidth(source) + 2 + visibleWidth(tag) + visibleWidth(runAs)
		desc := viewCompactText(e.Description, viewBudget(width, used))
		fmt.Fprintf(&b, "  %-22s %s  %s%s%s\n", name, source, desc, tag, runAs)
	}
	b.WriteString(viewHint(viewCompactText("install: /skills install <name> [--global] · browse: /skills browse · sources: /skills sources", viewBudget(width, 2))))
	return strings.TrimRight(b.String(), "\n")
}

func renderRegistrySources(width int, sources []registry.Source) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("registry sources (%d)", len(sources)))
	for i, s := range sources {
		trust := ""
		if s.Trusted {
			trust = " " + viewStatus("official")
		}
		typ := viewMeta(fmt.Sprintf("[%s]", s.Type))
		fmt.Fprintf(&b, "  %2d. %-24s %s%s\n", i+1, s.Name, typ, trust)
		if strings.TrimSpace(s.Description) != "" {
			fmt.Fprintf(&b, "      %s\n", viewCompactText(s.Description, viewBudget(width, 6)))
		}
		fmt.Fprintf(&b, "      %s\n", viewMeta(viewCompactPath(s.URL, viewBudget(width, 6))))
	}
	b.WriteString(viewHint(viewCompactText("add custom sources in Rexion.toml [registry] section · official sources are built-in", viewBudget(width, 2))))
	return strings.TrimRight(b.String(), "\n")
}
