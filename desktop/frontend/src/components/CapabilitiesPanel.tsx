import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { MessageSquare, Mail, FileText, Table, Presentation, Search, Cloud } from "lucide-react";
import { asArray } from "../lib/array";
import { app, openExternal } from "../lib/bridge";
import { useI18n, useT, type Locale } from "../lib/i18n";
import type { CapabilitiesView, MCPServerInput, RegistryEntryView, RegistrySourceView, ServerView, SkillRootSkillView, SkillRootView, SkillView } from "../lib/types";
import { InlineConfirmButton } from "./InlineConfirmButton";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";

// CapabilitiesPanel is the desktop MCP & Skills drawer — the GUI counterpart to
// the CLI's /mcp + /skill, aligning with Claude Code's Customize → Connectors:
// each server shows a connected/failed dot, transport, and tool/prompt/resource
// counts, with add / remove / retry; skills list their scope and run mode.
type CapTab = "servers" | "skills";

export function CapabilitiesPanel({
  onClose,
  initialTab = "servers",
}: {
  onClose: () => void;
  initialTab?: CapTab;
}) {
  const t = useT();
  const [view, setView] = useState<CapabilitiesView | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [tab, setTab] = useState<CapTab>(initialTab);
  const [skillQuery, setSkillQuery] = useState("");
  const [expandedSkills, setExpandedSkills] = useState<Set<string>>(() => new Set());
  const [expandedErrors, setExpandedErrors] = useState<Set<string>>(() => new Set());
  const [expandedServers, setExpandedServers] = useState<Set<string>>(() => new Set());
  const [expandedServerTools, setExpandedServerTools] = useState<Set<string>>(() => new Set());

  const reload = useCallback(async () => {
    setView(normalizeCapabilitiesView(await app.Capabilities().catch(() => ({ servers: [], skills: [], skillRoots: [] }))));
  }, []);
  useEffect(() => {
    void reload();
  }, [reload]);
  // No polling: servers that are "initializing" will be refreshed when the
  // user navigates back to this panel or manually clicks refresh. This
  // eliminates the 2.5s polling that kept the WebView2 process alive.

  // mutate runs an MCP edit, re-reads the snapshot, and surfaces any failure as an
  // inline banner (a connect error, a missing binary, a bad URL).
  const mutate = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr(null);
    try {
      await fn();
      await reload();
      return true;
    } catch (e) {
      setErr(String((e as Error)?.message ?? e));
      await reload();
      return false;
    } finally {
      setBusy(false);
    }
  };

  const summary = useMemo(() => {
    if (!view) return "";
    return t("caps.summary", {
      connected: view.servers.filter((s) => s.status === "connected").length,
      failed: view.servers.filter((s) => s.status === "failed").length,
      skills: view.skills.length,
    });
  }, [view, t]);

  const filteredSkills = useMemo(() => {
    if (!view) return [];
    const q = skillQuery.trim().toLowerCase();
    if (!q) return view.skills;
    return view.skills.filter((sk) => {
      const text = [sk.name, `/${sk.name}`, sk.description, sk.scope, sk.runAs].join(" ").toLowerCase();
      return text.includes(q);
    });
  }, [view, skillQuery]);
  const skillSummary = useMemo(() => {
    if (!view) return "";
    return skillListSummary(view.skills, filteredSkills, skillQuery.trim().length > 0, t);
  }, [filteredSkills, skillQuery, t, view]);

  const serverGroups = useMemo(() => {
    const servers = sortServersForDisplay(view?.servers ?? []);
    return {
      failed: servers.filter((s) => s.status === "failed"),
      active: servers.filter((s) => s.status !== "failed"),
    };
  }, [view]);

  const toggleSkill = useCallback((name: string) => {
    setExpandedSkills((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  const toggleError = useCallback((name: string) => {
    setExpandedErrors((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  const toggleServer = useCallback((name: string) => {
    setExpandedServers((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  const toggleServerTools = useCallback((name: string) => {
    setExpandedServerTools((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }, []);

  return (
    <ResizableDrawer onClose={onClose} subtle>
        <header className="drawer__head">
          <div>
            <div className="drawer__title">{t("caps.title")}</div>
            {view && <div className="drawer__summary">{summary}</div>}
          </div>
          <Tooltip label={t("common.close")}>
            <button className="chip" onClick={onClose}>
              ✕
            </button>
          </Tooltip>
          <Tooltip label={t("caps.refresh")}>
            <button className="chip" disabled={busy} onClick={() => void reload()}>
              ↻
            </button>
          </Tooltip>
        </header>

        {!view ? (
          <div className="empty">{t("caps.loading")}</div>
        ) : (
          <div className="drawer__body">
            {err && <div className="banner banner--error">{err}</div>}

            <div className="cap-tabs" role="tablist" aria-label={t("caps.title")}>
              <button
                className={`cap-tab${tab === "servers" ? " cap-tab--active" : ""}`}
                role="tab"
                aria-selected={tab === "servers"}
                onClick={() => setTab("servers")}
              >
                {t("caps.connectorsTab")}
              </button>
              <button
                className={`cap-tab${tab === "skills" ? " cap-tab--active" : ""}`}
                role="tab"
                aria-selected={tab === "skills"}
                onClick={() => setTab("skills")}
              >
                {t("caps.skillsTab")}
              </button>
            </div>

            {tab === "servers" ? (
              <section className="mem-section">
                <div className="mem-section__actions">
                  {!adding && (
                    <button className="btn btn--small" disabled={busy} onClick={() => setAdding(true)}>
                      {t("caps.addServer")}
                    </button>
                  )}
                </div>
                {serverGroups.failed.length > 0 && (
                  <FailedServersNotice
                    servers={serverGroups.failed}
                    expanded={expandedErrors}
                    onToggle={toggleError}
                    onRetry={(name) => void mutate(() => app.ReconnectMCPServer(name))}
                    onConfirmClearAuth={(name) => void mutate(() => app.ClearMCPServerAuthentication(name))}
                    onConfirm={(name) => void mutate(() => app.RemoveMCPServer(name))}
                    busy={busy}
                  />
                )}
                {view.servers.length === 0 && !adding && (
                  <div className="mem-empty">{t("caps.noServers")}</div>
                )}
                <ServerGroup
                  busy={busy}
                  servers={serverGroups.active}
                  expanded={expandedServers}
                  expandedTools={expandedServerTools}
                  editing={editing}
                  onConfirm={(name) => void mutate(() => app.RemoveMCPServer(name))}
                  onEdit={(name) => {
                    setEditing(name);
                  }}
                  onCancelEdit={() => setEditing(null)}
                  onRetry={(name) => void mutate(() => app.ReconnectMCPServer(name))}
                  onReconnect={(name) => void mutate(() => app.ReconnectMCPServer(name))}
                  onConfirmClearAuth={(name) => void mutate(() => app.ClearMCPServerAuthentication(name))}
                  onToggle={(name, on) => void mutate(() => app.SetMCPServerEnabled(name, on))}
                  onUpdate={(name, input) =>
                    void mutate(() => app.UpdateMCPServer(name, input)).then((ok) => {
                      if (ok) setEditing(null);
                    })
                  }
                  onToggleDetails={toggleServer}
                  onToggleTools={toggleServerTools}
                />
                {adding ? (
                  <AddServerForm busy={busy} onCancel={() => setAdding(false)} onAdd={async (input) => (await mutate(() => app.AddMCPServer(input))) && setAdding(false)} />
                ) : null}
              </section>
            ) : (
              <section className="mem-section">
                <div className="cap-search">
                  <input
                    className="mem-input"
                    type="search"
                    placeholder={t("caps.searchSkills")}
                    value={skillQuery}
                    onChange={(e) => setSkillQuery(e.target.value)}
                  />
                </div>
                <SkillSources
                  roots={view.skillRoots ?? []}
                  busy={busy}
                  onAdd={() => mutate(async () => {
                    const path = await app.PickSkillFolder();
                    if (path) await app.AddSkillPath(path);
                  })}
                  onRefresh={() => mutate(() => app.RefreshSkills())}
                  onRemove={(path) => mutate(() => app.RemoveSkillPath(path))}
                />
                <div className="cap-skills-head">
                  <div className="cap-skills-head__copy">
                    <div className="cap-skills-head__title">{t("caps.skills")}</div>
                    <div className="cap-skills-head__summary">{skillSummary}</div>
                  </div>
                </div>
                {view.skills.length === 0 ? (
                  <div className="mem-empty">{t("caps.noSkills")}</div>
                ) : filteredSkills.length === 0 ? (
                  <div className="mem-empty">{t("caps.noSkillMatches")}</div>
                ) : (
                  <div className="cap-skills">
                    {filteredSkills.map((sk) => (
                      <SkillRow
                        key={sk.name}
                        skill={sk}
                        busy={busy}
                        expanded={expandedSkills.has(sk.name)}
                        onToggle={() => toggleSkill(sk.name)}
                        onToggleEnabled={(enabled) => void mutate(() => app.SetSkillEnabled(sk.name, enabled))}
                      />
                    ))}
                  </div>
                )}
              </section>
            )}
          </div>
        )}
    </ResizableDrawer>
  );
}

function normalizeCapabilitiesView(view: CapabilitiesView | null | undefined): CapabilitiesView {
  return {
    servers: sortServersForDisplay(
      asArray(view?.servers).map((server) => ({
        ...server,
        args: asArray(server.args),
        envKeys: asArray(server.envKeys),
        toolList: asArray(server.toolList),
      })),
    ),
    skills: asArray(view?.skills),
    skillRoots: asArray(view?.skillRoots).map((root) => ({
      ...root,
      removable: Boolean(root.removable),
      skillItems: asArray(root.skillItems),
    })),
  };
}

function sortServersForDisplay(servers: ServerView[]): ServerView[] {
  return [...servers].sort((a, b) => {
    const priority = serverDisplayPriority(a) - serverDisplayPriority(b);
    if (priority !== 0) return priority;
    return a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
  });
}

function serverDisplayPriority(server: ServerView): number {
  if (server.status === "failed" || server.authStatus === "required") return 0;
  if (server.builtIn) return 1;
  if (server.status !== "disabled") return 2;
  return 3;
}

function skillListSummary(skills: SkillView[], filtered: SkillView[], searching: boolean, t: ReturnType<typeof useT>): string {
  if (searching) {
    return t("caps.skillsSummaryMatches", { matched: filtered.length, total: skills.length });
  }
  const parts = [t("caps.skillsSummaryAvailable", { skills: skills.length })];
  const scopes = ["project", "custom", "global", "builtin"];
  for (const scope of scopes) {
    const count = skills.filter((skill) => skill.scope === scope).length;
    if (count > 0) parts.push(skillScopeSummary(scope, count, t));
  }
  return parts.join(" · ");
}

function skillScopeSummary(scope: string, count: number, t: ReturnType<typeof useT>): string {
  switch (scope) {
    case "builtin":
      return t("caps.skillsSummaryBuiltin", { count });
    case "project":
      return t("caps.skillsSummaryProject", { count });
    case "custom":
      return t("caps.skillsSummaryCustom", { count });
    case "global":
      return t("caps.skillsSummaryGlobal", { count });
    default:
      return `${count} ${scope}`;
  }
}

function skillSourceSummary(active: number, missing: number, empty: number, t: ReturnType<typeof useT>): string {
  const parts: string[] = [];
  if (active > 0) parts.push(t("caps.sourcesSummaryActive", { active }));
  if (missing > 0) parts.push(t("caps.sourcesSummaryMissing", { missing }));
  if (empty > 0) parts.push(t("caps.sourcesSummaryEmpty", { empty }));
  return parts.length > 0 ? parts.join(" · ") : t("caps.sourcesSummaryNone");
}

function SkillSources({
  roots,
  busy,
  onAdd,
  onRefresh,
  onRemove,
}: {
  roots: SkillRootView[];
  busy: boolean;
  onAdd: () => void;
  onRefresh: () => void;
  onRemove: (path: string) => void;
}) {
  const t = useT();
  const [expanded, setExpanded] = useState(false);
  const [showDiagnostics, setShowDiagnostics] = useState(false);
  const [expandedRootSkills, setExpandedRootSkills] = useState<Set<string>>(() => new Set());
  const [fullRootSkills, setFullRootSkills] = useState<Set<string>>(() => new Set());
  const primaryRoots = roots.filter(isPrimarySkillRoot);
  const diagnosticRoots = roots.filter((root) => !isPrimarySkillRoot(root));
  const diagnosticsVisible = expanded && showDiagnostics;
  const shownRoots = diagnosticsVisible ? [...primaryRoots, ...diagnosticRoots] : primaryRoots;
  const summaryRoots = diagnosticsVisible ? roots : primaryRoots;
  const active = summaryRoots.filter((root) => root.skills > 0).length;
  const missing = summaryRoots.filter((root) => root.status === "missing").length;
  const empty = summaryRoots.filter((root) => root.status === "ok" && root.skills === 0).length;
  const toggleRootSkills = (key: string) => {
    setExpandedRootSkills((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };
  const toggleRootSkillFull = (key: string) => {
    setFullRootSkills((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };
  return (
    <div className={`cap-sources${expanded ? " cap-sources--expanded" : ""}`}>
      <div className="cap-sources__head">
        <div className="cap-sources__copy">
          <div className="cap-sources__title">{t("caps.sources")}</div>
          <div className="cap-sources__summary">{skillSourceSummary(active, missing, empty, t)}</div>
        </div>
        {!expanded && (
          <div className="cap-sources__actions">
            <button className="btn btn--small" type="button" onClick={() => setExpanded(true)} aria-expanded={expanded}>
              {t("caps.manageSkillSources")}
            </button>
          </div>
        )}
      </div>
      {expanded && (
        <>
          <div className="cap-sources__manage">
            <div className="cap-sources__manage-actions">
              <button className="btn btn--small" disabled={busy} onClick={onRefresh}>
                {t("caps.refreshSkills")}
              </button>
              <button className="btn btn--small" disabled={busy} onClick={onAdd}>
                {t("caps.addSkillFolder")}
              </button>
            </div>
            <button
              className="btn btn--small"
              type="button"
              onClick={() => {
                setShowDiagnostics(false);
                setExpanded(false);
              }}
              aria-expanded={expanded}
            >
              {t("common.collapse")}
            </button>
          </div>
          {shownRoots.length === 0 ? (
            <div className="mem-empty">{t("caps.noSkillRoots")}</div>
          ) : (
            <div className="cap-source-list">
              {shownRoots.map((root) => {
                const key = skillRootKey(root);
                const rootSkills = root.skillItems ?? [];
                const rootSkillsExpanded = expandedRootSkills.has(key);
                const rootSkillsFull = fullRootSkills.has(key);
                const canShowRootSkills = rootSkills.length > 0;
                const canRemoveRoot = root.removable;
                return (
                  <div className={`cap-source cap-source--${skillRootTone(root)}`} key={key}>
                    <span className={`cap-dot cap-dot--${skillRootDot(root)}`} />
                    <div className="cap-source__text">
                      <div className="cap-source__head">
                        <div className="cap-source__label" title={root.dir}>
                          {skillRootLabel(root)}
                        </div>
                      </div>
                      <div className="cap-source__meta">
                        <span>{skillRootStatus(root, t)}</span>
                        <span>{t("caps.skillRootCount", { skills: root.skills })}</span>
                        {root.configured && <span>{t("caps.skillRootConfigured")}</span>}
                      </div>
                      {(canShowRootSkills || canRemoveRoot) && (
                        <div className="cap-source-actions">
                          <>
                            {canShowRootSkills && (
                              <button
                                className="btn btn--small"
                                disabled={busy}
                                type="button"
                                aria-expanded={rootSkillsExpanded}
                                onClick={() => toggleRootSkills(key)}
                              >
                                {rootSkillsExpanded ? t("caps.hideSkills") : t("caps.showSkills")}
                              </button>
                              )}
                              {canRemoveRoot && (
                                <InlineConfirmButton
                                  label={t("caps.skillRootRemove")}
                                  confirmLabel={t("caps.skillRootConfirmRemove")}
                                  cancelLabel={t("common.cancel")}
                                  disabled={busy}
                                  danger
                                  onConfirm={() => onRemove(root.dir)}
                                />
                              )}
                            </>
                        </div>
                      )}
                      {rootSkillsExpanded && rootSkills.length > 0 && (
                        <SkillRootSkillsList
                          skills={rootSkills}
                          showAll={rootSkillsFull}
                          onToggleAll={() => toggleRootSkillFull(key)}
                        />
                      )}
                      {root.warning && <div className="cap-source__warning">{root.warning}</div>}
                    </div>
                    <div className="cap-source__badges">
                      {skillRootBadges(root, t).map((badge) => (
                        <span className={`cap-source-badge cap-source-badge--${badge.tone}`} key={badge.label}>
                          {badge.label}
                        </span>
                      ))}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
          {diagnosticRoots.length > 0 && (
            <button className="cap-diagnostics" type="button" onClick={() => setShowDiagnostics((v) => !v)}>
              {diagnosticsVisible ? t("caps.hideDiagnostics") : t("caps.showDiagnostics", { count: diagnosticRoots.length })}
            </button>
          )}
        </>
      )}
    </div>
  );
}

const skillRootPreviewLimit = 5;

function SkillRootSkillsList({
  skills,
  showAll,
  onToggleAll,
}: {
  skills: SkillRootSkillView[];
  showAll: boolean;
  onToggleAll: () => void;
}) {
  const t = useT();
  const visible = showAll ? skills : skills.slice(0, skillRootPreviewLimit);
  return (
    <div className="cap-source-skills">
      {visible.map((skill) => (
        <div className="cap-source-skill" key={`${skill.scope}:${skill.name}`}>
          <div className="cap-source-skill__head">
            <span className="cap-source-skill__name">/{skill.name}</span>
            <span className="cap-source-skill__badges">
              <span className={`cap-skill-badge cap-skill-badge--${skill.scope}`}>{skillScopeLabel(skill.scope, t)}</span>
              {skill.runAs === "subagent" && <span className="cap-skill-badge cap-skill-badge--run">{t("caps.subagent")}</span>}
            </span>
          </div>
          {skill.description && <div className="cap-source-skill__desc">{skill.description}</div>}
        </div>
      ))}
      {skills.length > skillRootPreviewLimit && (
        <button className="cap-source-skills__more" type="button" onClick={onToggleAll}>
          {showAll ? t("common.collapse") : t("caps.skillRootShowAllSkills", { count: skills.length })}
        </button>
      )}
    </div>
  );
}

function skillRootKey(root: SkillRootView): string {
  return `${root.scope}:${root.priority}:${root.dir}`;
}

function isPrimarySkillRoot(root: SkillRootView): boolean {
  return root.skills > 0 || root.configured || Boolean(root.warning);
}

function skillRootTone(root: SkillRootView): "active" | "empty" | "problem" {
  if (root.warning || root.status === "inactive" || root.status === "unreadable") return "problem";
  if (root.skills > 0) return "active";
  return "empty";
}

function skillRootDot(root: SkillRootView): "connected" | "disabled" | "failed" {
  const tone = skillRootTone(root);
  if (tone === "active") return "connected";
  if (tone === "empty") return "disabled";
  return "failed";
}

function skillRootStatus(root: SkillRootView, t: ReturnType<typeof useT>): string {
  if (root.status === "ok" && root.skills > 0) return t("caps.skillRootActive");
  if (root.status === "ok") return t("caps.skillRootEmpty");
  return root.status;
}

function skillRootLabel(root: SkillRootView): string {
  return root.dir;
}

function skillRootBadges(root: SkillRootView, t: ReturnType<typeof useT>): Array<{ label: string; tone: "scope" | "builtin" | "configured" | "missing" }> {
  const badges: Array<{ label: string; tone: "scope" | "builtin" | "configured" | "missing" }> = [
    { label: skillScopeLabel(root.scope, t), tone: "scope" },
    root.scope === "custom"
      ? { label: root.configured ? t("caps.skillRootUserConfigured") : t("caps.skillRootConfiguredPath"), tone: "configured" }
      : { label: t("caps.skillRootBuiltinPath"), tone: "builtin" },
  ];
  if (root.status === "missing") {
    badges.push({ label: t("caps.skillRootMissing"), tone: "missing" });
  }
  return badges;
}

function ServerGroup({
  servers,
  expanded,
  expandedTools,
  busy,
  editing,
  onConfirm,
  onEdit,
  onCancelEdit,
  onRetry,
  onReconnect,
  onConfirmClearAuth,
  onToggle,
  onUpdate,
  onToggleDetails,
  onToggleTools,
}: {
  servers: ServerView[];
  expanded: Set<string>;
  expandedTools: Set<string>;
  busy: boolean;
  editing: string | null;
  onConfirm: (name: string) => void;
  onEdit: (name: string) => void;
  onCancelEdit: () => void;
  onRetry: (name: string) => void;
  onReconnect: (name: string) => void;
  onConfirmClearAuth: (name: string) => void;
  onToggle: (name: string, on: boolean) => void;
  onUpdate: (name: string, input: MCPServerInput) => void;
  onToggleDetails: (name: string) => void;
  onToggleTools: (name: string) => void;
}) {
  if (servers.length === 0) return null;
  return (
    <div className="cap-server-group">
      {servers.map((s) => (
        <ServerRow
          key={s.name}
          s={s}
          expanded={expanded.has(s.name)}
          toolsExpanded={expandedTools.has(s.name)}
          busy={busy}
          editing={editing === s.name}
          onConfirm={() => onConfirm(s.name)}
          onEdit={() => onEdit(s.name)}
          onCancelEdit={onCancelEdit}
          onRetry={() => onRetry(s.name)}
          onReconnect={() => onReconnect(s.name)}
          onConfirmClearAuth={() => onConfirmClearAuth(s.name)}
          onToggle={(on) => onToggle(s.name, on)}
          onUpdate={(input) => onUpdate(s.name, input)}
          onToggleDetails={() => onToggleDetails(s.name)}
          onToggleTools={() => onToggleTools(s.name)}
        />
      ))}
    </div>
  );
}

function FailedServersNotice({
  servers,
  expanded,
  busy,
  onToggle,
  onRetry,
  onConfirmClearAuth,
  onConfirm,
}: {
  servers: ServerView[];
  expanded: Set<string>;
  busy: boolean;
  onToggle: (name: string) => void;
  onRetry: (name: string) => void;
  onConfirmClearAuth: (name: string) => void;
  onConfirm: (name: string) => void;
}) {
  const t = useT();
  return (
    <div className="cap-failures" role="status">
      <div className="cap-failures__head">
        <div>
          <div className="cap-failures__title">{t("caps.failureTitle", { failed: servers.length })}</div>
          <div className="cap-failures__hint">{t("caps.failureHint")}</div>
        </div>
      </div>
      <div className="cap-failures__list">
        {servers.map((s) => {
          const open = expanded.has(s.name);
          const error = s.error || t("caps.failed");
          const actionLabel = serverActionLabel(s, t);
          const handlePrimaryAction = () => {
            if (shouldOpenAuth(s)) {
              openExternal((s.authUrl || "").trim());
              return;
            }
            onRetry(s.name);
          };
          return (
            <div className="cap-failure" key={s.name}>
              <div className="cap-failure__main">
                <span className="cap-dot cap-dot--failed" />
                <div className="cap-failure__text">
                  <div className="cap-failure__name">{s.name}</div>
                  <div className="cap-failure__summary">{s.authStatus === "required" ? t("caps.authRequiredSummary") : summarizeServerError(error, t)}</div>
                </div>
              </div>
              <div className="cap-failure__actions">
                <button className="btn btn--small" disabled={busy} onClick={handlePrimaryAction}>
                  {actionLabel}
                </button>
                {canClearAuth(s) && (
                  <InlineConfirmButton
                    label={t("caps.clearAuth")}
                    confirmLabel={t("caps.confirmClearAuth")}
                    cancelLabel={t("common.cancel")}
                    disabled={busy}
                    onConfirm={() => onConfirmClearAuth(s.name)}
                  />
                )}
                <button className="btn btn--small" onClick={() => onToggle(s.name)} aria-expanded={open}>
                  {open ? t("common.collapse") : t("caps.showLog")}
                </button>
                {!s.builtIn && (
                  <InlineConfirmButton
                    label={t("caps.remove")}
                    confirmLabel={t("caps.confirmRemove")}
                    cancelLabel={t("common.cancel")}
                    disabled={busy}
                    danger
                    onConfirm={() => onConfirm(s.name)}
                  />
                )}
              </div>
              {open && (
                <div className="cap-failure__logbox">
                  <div className="cap-failure__logbar">
                    <span>{t("caps.rawLog")}</span>
                    <button className="btn btn--small" onClick={() => void navigator.clipboard?.writeText(error)}>
                      {t("caps.copyLog")}
                    </button>
                  </div>
                  <pre className="cap-failure__log">{error}</pre>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function ServerRow({
  s,
  expanded,
  toolsExpanded,
  busy,
  editing,
  onConfirm,
  onEdit,
  onCancelEdit,
  onRetry,
  onReconnect,
  onConfirmClearAuth,
  onToggle,
  onUpdate,
  onToggleDetails,
  onToggleTools,
}: {
  s: ServerView;
  expanded: boolean;
  toolsExpanded: boolean;
  busy: boolean;
  editing: boolean;
  onConfirm: () => void;
  onEdit: () => void;
  onCancelEdit: () => void;
  onRetry: () => void;
  onReconnect: () => void;
  onConfirmClearAuth: () => void;
  onToggle: (on: boolean) => void;
  onUpdate: (input: MCPServerInput) => void;
  onToggleDetails: () => void;
  onToggleTools: () => void;
}) {
  const t = useT();
  const actionLabel = serverActionLabel(s, t);
  const tools = s.toolList ?? [];
  let sub =
    s.status === "failed"
      ? s.error || t("caps.failed")
      : s.status === "initializing"
        ? t("caps.initializing")
      : s.status === "deferred"
        ? t("caps.deferred")
      : s.status === "disabled"
        ? s.configured && !s.autoStart
          ? t("caps.disabledAutoStart")
          : t("caps.disabled")
        : t("caps.counts", { tools: s.tools, prompts: s.prompts, resources: s.resources });
  if (s.authStatus === "possible" && s.status !== "failed") {
    sub = `${sub} · ${t("caps.authPossibleShort")}`;
  }
  const enabled = s.status === "connected" || s.status === "deferred" || s.status === "initializing";
  const handlePrimaryAction = () => {
    if (shouldOpenAuth(s)) {
      openExternal((s.authUrl || "").trim());
      return;
    }
    onRetry();
  };
  return (
    <div className={`cap-server-entry${s.status === "disabled" ? " cap-server-entry--disabled" : ""}`}>
      <Tooltip label={s.error} disabled={!s.error} fill block>
        <div className={`cap-row${s.status === "disabled" ? " cap-row--disabled" : ""}`}>
          <Tooltip label={expanded ? t("caps.collapseDetails") : t("caps.expandDetails")}>
            <button
              className="cap-disclosure"
              aria-expanded={expanded}
              onClick={onToggleDetails}
            >
              {expanded ? "⌄" : "›"}
            </button>
          </Tooltip>
          <span className={`cap-dot cap-dot--${s.status}`} />
          <div className="cap-row__text">
            <div className="cap-row__head">
              <span className="cap-row__name">{s.name}</span>
              <span className="cap-row__transport">{s.transport}</span>
              {s.builtIn && <span className="cap-row__builtin">{t("caps.builtIn")}</span>}
            </div>
            <div className="cap-row__sub">{sub}</div>
          </div>
          <div className="cap-row__actions">
            {s.status === "failed" ? (
              <button className="btn btn--small" disabled={busy} onClick={handlePrimaryAction}>
                {actionLabel}
              </button>
            ) : s.status === "initializing" ? (
              <span className="cap-row__pending">{t("caps.initializingShort")}</span>
            ) : (
              <Tooltip label={enabled ? t("caps.disable") : t("caps.enable")}>
                <label className="cap-switch">
                  <input
                    type="checkbox"
                    checked={enabled}
                    disabled={busy}
                    onChange={(e) => onToggle(e.target.checked)}
                  />
                  <span className="cap-switch__track" />
                </label>
              </Tooltip>
            )}
          </div>
        </div>
      </Tooltip>
      {expanded && (
        <ServerDetails
          s={s}
          tools={tools}
          busy={busy}
          onConfirm={onConfirm}
          onConnectNow={onRetry}
          onReconnect={onReconnect}
          onConfirmClearAuth={onConfirmClearAuth}
          toolsExpanded={toolsExpanded}
          editing={editing}
          onEdit={onEdit}
          onCancelEdit={onCancelEdit}
          onUpdate={onUpdate}
          onToggleTools={onToggleTools}
        />
      )}
    </div>
  );
}

function ServerDetails({
  s,
  tools,
  busy,
  onConfirm,
  onConnectNow,
  onReconnect,
  onConfirmClearAuth,
  toolsExpanded,
  editing,
  onEdit,
  onCancelEdit,
  onUpdate,
  onToggleTools,
}: {
  s: ServerView;
  tools: ServerView["toolList"];
  busy: boolean;
  onConfirm: () => void;
  onConnectNow: () => void;
  onReconnect: () => void;
  onConfirmClearAuth: () => void;
  toolsExpanded: boolean;
  editing: boolean;
  onEdit: () => void;
  onCancelEdit: () => void;
  onUpdate: (input: MCPServerInput) => void;
  onToggleTools: () => void;
}) {
  const t = useT();
  const command = serverCommand(s);
  const canEditConfig = s.configured && !s.builtIn;
  const canConnectNow = s.status === "deferred" || s.status === "disabled";
  const canReconnect = s.status === "connected";
  const canShowTools = (s.tools ?? 0) > 0 || (tools?.length ?? 0) > 0;
  const showClearAuth = canClearAuth(s);
  const authLabel = serverAuthLabel(s, t);
  if (editing && canEditConfig) {
    return (
      <div className="cap-server-details">
        <EditServerForm s={s} busy={busy} onCancel={onCancelEdit} onSave={onUpdate} />
      </div>
    );
  }
  return (
    <div className="cap-server-details">
      <div className="cap-detail-grid">
        <div className="cap-detail">
          <span className="cap-detail__label">{t("caps.status")}</span>
          <span className="cap-detail__value">{serverStatusLabel(s, t)}</span>
        </div>
        <div className="cap-detail">
          <span className="cap-detail__label">{t("caps.transport")}</span>
          <span className="cap-detail__value">{s.transport}</span>
        </div>
        {authLabel && (
          <div className="cap-detail">
            <span className="cap-detail__label">{t("caps.auth")}</span>
            <span className="cap-detail__value">{authLabel}</span>
          </div>
        )}
        {command && (
          <div className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{s.transport === "stdio" ? t("caps.command") : t("caps.url")}</span>
            <span className="cap-detail__code">{command}</span>
          </div>
        )}
        {s.envKeys && s.envKeys.length > 0 && (
          <div className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.envKeys")}</span>
            <span className="cap-detail__value">{s.envKeys.join(", ")}</span>
          </div>
        )}
      </div>
      <div className="cap-detail-actions">
        {canConnectNow && (
          <button className="btn btn--small" disabled={busy} onClick={onConnectNow}>
            {t("caps.connectNow")}
          </button>
        )}
        {canReconnect && (
          <button className="btn btn--small" disabled={busy} onClick={onReconnect}>
            {t("caps.reconnect")}
          </button>
        )}
        {canShowTools && (
          <button className="btn btn--small" disabled={busy} onClick={onToggleTools} aria-expanded={toolsExpanded}>
            {toolsExpanded ? t("caps.hideTools") : t("caps.showTools")}
          </button>
        )}
        {showClearAuth && (
          <InlineConfirmButton
            label={t("caps.clearAuth")}
            confirmLabel={t("caps.confirmClearAuth")}
            cancelLabel={t("common.cancel")}
            disabled={busy}
            onConfirm={onConfirmClearAuth}
          />
        )}
        {canEditConfig && (
          <>
            <button className="btn btn--small" disabled={busy} onClick={onEdit}>
              {t("caps.editConfig")}
            </button>
            <InlineConfirmButton
              label={t("caps.remove")}
              confirmLabel={t("caps.confirmRemove")}
              cancelLabel={t("common.cancel")}
              disabled={busy}
              danger
              onConfirm={onConfirm}
            />
          </>
        )}
      </div>
      {toolsExpanded && (
        tools && tools.length > 0 ? (
          <div className="cap-tool-list">
            <div className="cap-tool-list__title">{t("caps.tools")}</div>
            {tools.map((tool) => (
              <div className="cap-tool" key={tool.name}>
                <div className="cap-tool__name">{tool.name}</div>
                {tool.description && <div className="cap-tool__desc">{tool.description}</div>}
              </div>
            ))}
          </div>
        ) : (
          <div className="cap-tool-empty">{t("caps.noToolDetails")}</div>
        )
      )}
    </div>
  );
}

function EditServerForm({
  s,
  busy,
  onCancel,
  onSave,
}: {
  s: ServerView;
  busy: boolean;
  onCancel: () => void;
  onSave: (input: MCPServerInput) => void;
}) {
  const t = useT();
  const initialTransport = normalizeTransportValue(s.transport);
  const [transport, setTransport] = useState(initialTransport);
  const [command, setCommand] = useState(initialTransport === "stdio" ? serverCommand(s) : "");
  const [url, setUrl] = useState(initialTransport === "stdio" ? "" : s.url || serverCommand(s));
  const [env, setEnv] = useState("");
  const isStdio = transport === "stdio";
  const ready = isStdio ? command.trim() !== "" : url.trim() !== "";

  const submit = () => {
    const parts = command.trim().split(/\s+/).filter(Boolean);
    const envText = env.trim();
    onSave({
      name: s.name,
      transport,
      command: isStdio ? (parts[0] ?? "") : "",
      args: isStdio ? parts.slice(1) : [],
      url: isStdio ? "" : url.trim(),
      env: envText === "" ? null : parseEnvText(envText),
    });
  };

  return (
    <div className="cap-config-edit">
      <div className="cap-detail-grid">
        <div className="cap-detail">
          <span className="cap-detail__label">{t("caps.name")}</span>
          <span className="cap-detail__value">{s.name}</span>
        </div>
        <label className="cap-detail cap-detail--select">
          <span className="cap-detail__label">{t("caps.transport")}</span>
          <select className="mem-select" value={transport} disabled={busy} onChange={(e) => setTransport(e.target.value)}>
            <option value="stdio">stdio</option>
            <option value="http">http</option>
            <option value="sse">sse</option>
          </select>
        </label>
        {isStdio ? (
          <label className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.command")}</span>
            <input className="mem-input" value={command} disabled={busy} onChange={(e) => setCommand(e.target.value)} placeholder={t("caps.commandPlaceholder")} />
          </label>
        ) : (
          <label className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.url")}</span>
            <input className="mem-input" value={url} disabled={busy} onChange={(e) => setUrl(e.target.value)} placeholder={t("caps.urlPlaceholder")} />
          </label>
        )}
        <label className="cap-detail cap-detail--wide">
          <span className="cap-detail__label">{t("caps.envLabel")}</span>
          <textarea className="mem-textarea cap-config-edit__env" value={env} disabled={busy} onChange={(e) => setEnv(e.target.value)} placeholder={t("caps.envPlaceholder")} spellCheck={false} />
        </label>
        {s.envKeys && s.envKeys.length > 0 && (
          <div className="cap-detail cap-detail--wide">
            <span className="cap-detail__label">{t("caps.envKeys")}</span>
            <span className="cap-detail__value">{s.envKeys.join(", ")}</span>
            <span className="cap-edit-hint">{t("caps.envPreserveHint")}</span>
          </div>
        )}
      </div>
      <div className="cap-detail-actions">
        <button className="btn btn--small" disabled={busy} onClick={onCancel}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" disabled={busy || !ready} onClick={submit}>
          {t("caps.saveConfig")}
        </button>
      </div>
    </div>
  );
}

function serverCommand(s: ServerView): string {
  if (s.transport === "stdio") return [s.command, ...(s.args ?? [])].filter(Boolean).join(" ").trim();
  return (s.url || "").trim();
}

function normalizeTransportValue(transport: string): string {
  return transport === "http" || transport === "sse" ? transport : "stdio";
}

function parseEnvText(env: string): Record<string, string> {
  const envMap: Record<string, string> = {};
  for (const line of env.split("\n")) {
    const eq = line.indexOf("=");
    if (eq > 0) envMap[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
  }
  return envMap;
}

function serverStatusLabel(s: ServerView, t: ReturnType<typeof useT>): string {
  switch (s.status) {
    case "connected":
      return t("caps.connected");
    case "deferred":
      return t("caps.deferred");
    case "initializing":
      return t("caps.initializing");
    case "disabled":
      return s.configured && !s.autoStart ? t("caps.disabledAutoStart") : t("caps.disabled");
    case "failed":
      if (s.authStatus === "required") return t("caps.authRequired");
      return t("caps.failed");
    default:
      return s.status;
  }
}

function summarizeServerError(error: string, t: ReturnType<typeof useT>): string {
  const normalized = error.replace(/\s+/g, " ").trim();
  const plugin = normalized.match(/plugin "([^"]+)"/i)?.[1];
  if (plugin === "codegraph" && normalized.includes("context deadline exceeded")) {
    return t("caps.codegraphWarming");
  }
  const npmCode = normalized.match(/\bnpm error code ([A-Z0-9_]+)/i)?.[1];
  const errno = normalized.match(/\berrno (-?\d+)/i)?.[1];
  const reason = npmCode
    ? `npm ${npmCode}${errno ? ` (${errno})` : ""}`
    : normalized.split(/(?:\.\s+|\n)/)[0];
  const summary = plugin ? `${plugin}: ${reason}` : reason;
  return summary.length > 180 ? `${summary.slice(0, 176).trim()}…` : summary;
}

function serverActionLabel(s: ServerView, t: ReturnType<typeof useT>): string {
  const err = (s.error || "").toLowerCase();
  if (shouldOpenAuth(s)) return t("caps.reauthorize");
  if (
    err.includes("command not found") ||
    err.includes("executable file not found") ||
    err.includes("no such file") ||
    err.includes("enoent")
  ) {
    return t("caps.checkCommand");
  }
  return t("caps.retry");
}

function serverAuthLabel(s: ServerView, t: ReturnType<typeof useT>): string {
  if (s.authStatus === "required") return t("caps.authRequired");
  if (s.authStatus === "possible") return t("caps.authPossible");
  return "";
}

function shouldOpenAuth(s: ServerView): boolean {
  const url = (s.authUrl || "").trim();
  return s.authStatus === "required" && /^https?:\/\//i.test(url);
}

function canClearAuth(s: ServerView): boolean {
  if (!s.configured || s.builtIn) return false;
  return Boolean(s.authConfigured || s.authStatus === "required" || s.authStatus === "possible" || isRemoteTransport(s.transport));
}

function isRemoteTransport(transport?: string): boolean {
  const value = (transport || "").trim().toLowerCase();
  return value === "http" || value === "streamable-http" || value === "sse";
}

function SkillRow({
  skill,
  busy,
  expanded,
  onToggle,
  onToggleEnabled,
}: {
  skill: SkillView;
  busy: boolean;
  expanded: boolean;
  onToggle: () => void;
  onToggleEnabled: (enabled: boolean) => void;
}) {
  const t = useT();
  const summary = summarizeSkillDescription(skill.description);
  const canExpand = summary !== skill.description;
  return (
    <div
      className={`cap-skill-card${expanded ? " cap-skill-card--expanded" : ""}${canExpand ? " cap-skill-card--expandable" : ""}${!skill.enabled ? " cap-skill-card--disabled" : ""}`}
    >
      <div className="cap-skill-card__top">
        <button className="cap-skill-card__toggle" type="button" onClick={onToggle} aria-expanded={expanded}>
          <span className="cap-skill-card__head">
            <span className="cap-skill-card__icon">/</span>
            <span className="cap-skill-card__main">
              <span className="cap-skill-card__command">{skill.name}</span>
              <span className="cap-skill-card__badges">
                <span className={`cap-skill-badge cap-skill-badge--${skill.scope}`}>{skillScopeLabel(skill.scope, t)}</span>
                {skill.runAs === "subagent" && <span className="cap-skill-badge cap-skill-badge--run">{t("caps.subagent")}</span>}
                {!skill.enabled && <span className="cap-skill-badge cap-skill-badge--off">{t("caps.skillDisabled")}</span>}
              </span>
            </span>
          </span>
        </button>
        <Tooltip label={skill.enabled ? t("caps.disableSkill") : t("caps.enableSkill")}>
          <label className="cap-switch">
            <input
              type="checkbox"
              checked={skill.enabled}
              disabled={busy}
              onChange={(e) => onToggleEnabled(e.target.checked)}
            />
            <span className="cap-switch__track" />
          </label>
        </Tooltip>
      </div>
      <div className="cap-skill-card__desc">{expanded ? skill.description : summary}</div>
      {canExpand && <div className="cap-skill-card__more">{expanded ? t("common.collapse") : t("common.expand")}</div>}
    </div>
  );
}

function skillScopeLabel(scope: string, t: ReturnType<typeof useT>): string {
  switch (scope) {
    case "builtin":
      return t("caps.skillScopeBuiltin");
    case "project":
      return t("caps.skillScopeProject");
    case "custom":
      return t("caps.skillScopeCustom");
    case "global":
      return t("caps.skillScopeGlobal");
    default:
      return scope;
  }
}

function summarizeSkillDescription(description: string): string {
  const normalized = description.replace(/\s+/g, " ").trim();
  if (normalized.length <= 132) return normalized;
  const sentence = normalized.match(/^.{48,132}?[。.!?；;，,]/u)?.[0]?.trim();
  if (sentence && sentence.length >= 48) return sentence.replace(/[。.!?；;，,]$/u, "");
  return `${normalized.slice(0, 128).trim()}…`;
}

function AddServerForm({
  busy,
  onCancel,
  onAdd,
}: {
  busy: boolean;
  onCancel: () => void;
  onAdd: (input: MCPServerInput) => void;
}) {
  const t = useT();
  const [name, setName] = useState("");
  const [transport, setTransport] = useState("stdio");
  const [command, setCommand] = useState("");
  const [url, setUrl] = useState("");
  const [env, setEnv] = useState("");

  const isStdio = transport === "stdio";
  const ready = name.trim() !== "" && (isStdio ? command.trim() !== "" : url.trim() !== "");

  const submit = () => {
    const parts = command.trim().split(/\s+/).filter(Boolean);
    const envMap: Record<string, string> = {};
    for (const line of env.split("\n")) {
      const eq = line.indexOf("=");
      if (eq > 0) envMap[line.slice(0, eq).trim()] = line.slice(eq + 1).trim();
    }
    onAdd({
      name: name.trim(),
      transport,
      command: isStdio ? (parts[0] ?? "") : "",
      args: isStdio ? parts.slice(1) : [],
      url: isStdio ? "" : url.trim(),
      env: envMap,
    });
  };

  return (
    <div className="prov-card prov-card--edit">
      <input className="mem-input" placeholder={t("caps.namePlaceholder")} value={name} onChange={(e) => setName(e.target.value)} />
      <label className="set-label">{t("caps.transport")}</label>
      <select className="mem-select" value={transport} onChange={(e) => setTransport(e.target.value)}>
        <option value="stdio">stdio</option>
        <option value="http">http</option>
        <option value="sse">sse</option>
      </select>
      {isStdio ? (
        <input className="mem-input" placeholder={t("caps.commandPlaceholder")} value={command} onChange={(e) => setCommand(e.target.value)} />
      ) : (
        <input className="mem-input" placeholder={t("caps.urlPlaceholder")} value={url} onChange={(e) => setUrl(e.target.value)} />
      )}
      <label className="set-label">{t("caps.envLabel")}</label>
      <textarea className="mem-textarea" value={env} onChange={(e) => setEnv(e.target.value)} placeholder={t("caps.envPlaceholder")} spellCheck={false} />
      <div className="prov-card__actions">
        <button className="btn btn--small" onClick={onCancel} disabled={busy}>
          {t("common.cancel")}
        </button>
        <button className="btn btn--primary btn--small" onClick={submit} disabled={busy || !ready}>
          {t("caps.add")}
        </button>
      </div>
    </div>
  );
}

// MCPServersSettingsPage is a self-contained MCP servers management page
// embedded inside the settings centre.
export function MCPServersSettingsPage() {
	const t = useT();
	const [view, setView] = useState<CapabilitiesView | null>(null);
	const [busy, setBusy] = useState(false);
	const [err, setErr] = useState<string | null>(null);
	const [adding, setAdding] = useState(false);
	const [editing, setEditing] = useState<string | null>(null);
	const [expandedErrors, setExpandedErrors] = useState<Set<string>>(() => new Set());
	const [expandedServers, setExpandedServers] = useState<Set<string>>(() => new Set());
	const [expandedServerTools, setExpandedServerTools] = useState<Set<string>>(() => new Set());

	const reload = useCallback(async () => {
		setView(normalizeCapabilitiesView(await app.Capabilities().catch(() => ({ servers: [], skills: [], skillRoots: [] }))));
	}, []);
	useEffect(() => { void reload(); }, [reload]);

	const mutate = async (fn: () => Promise<unknown>) => {
		setBusy(true);
		setErr(null);
		try {
			await fn();
			await reload();
			return true;
		} catch (e) {
			setErr(String((e as Error)?.message ?? e));
			await reload();
			return false;
		} finally {
			setBusy(false);
		}
	};

	const serverGroups = useMemo(() => {
		const servers = sortServersForDisplay(view?.servers ?? []);
		return {
			failed: servers.filter((s) => s.status === "failed"),
			active: servers.filter((s) => s.status !== "failed"),
		};
	}, [view]);

	const toggleError = useCallback((name: string) => {
		setExpandedErrors((prev) => { const next = new Set(prev); if (next.has(name)) next.delete(name); else next.add(name); return next; });
	}, []);
	const toggleServer = useCallback((name: string) => {
		setExpandedServers((prev) => { const next = new Set(prev); if (next.has(name)) next.delete(name); else next.add(name); return next; });
	}, []);
	const toggleServerTools = useCallback((name: string) => {
		setExpandedServerTools((prev) => { const next = new Set(prev); if (next.has(name)) next.delete(name); else next.add(name); return next; });
	}, []);

	const summary = useMemo(() => {
		if (!view) return "";
		const connected = view.servers.filter((s) => s.status === "connected").length;
		const failed = view.servers.filter((s) => s.status === "failed").length;
		return t("caps.summary", { connected, failed, skills: 0 }).replace(/· \d+ skills/, "").trim();
	}, [view, t]);

	if (!view) return <div className="empty">{t("caps.loading")}</div>;

	return (
		<section className="mem-section">
			{err && <div className="banner banner--error">{err}</div>}
			{view.servers.length > 0 && (
				<div className="drawer__summary" style={{ marginBottom: 12 }}>{summary}</div>
			)}
			<div className="mem-section__actions">
				{!adding && (
					<button className="btn btn--small" disabled={busy} onClick={() => setAdding(true)}>
						{t("caps.addServer")}
					</button>
				)}
			</div>
			{serverGroups.failed.length > 0 && (
				<FailedServersNotice
					servers={serverGroups.failed}
					expanded={expandedErrors}
					busy={busy}
					onToggle={toggleError}
					onRetry={(name) => void mutate(() => app.ReconnectMCPServer(name))}
					onConfirmClearAuth={(name) => void mutate(() => app.ClearMCPServerAuthentication(name))}
					onConfirm={(name) => void mutate(() => app.RemoveMCPServer(name))}
				/>
			)}
			{view.servers.length === 0 && !adding && (
				<div className="mem-empty">{t("caps.noServers")}</div>
			)}
			<ServerGroup
				busy={busy}
				servers={serverGroups.active}
				expanded={expandedServers}
				expandedTools={expandedServerTools}
				editing={editing}
				onConfirm={(name) => void mutate(() => app.RemoveMCPServer(name))}
				onEdit={(name) => { setEditing(name); }}
				onCancelEdit={() => setEditing(null)}
				onRetry={(name) => void mutate(() => app.ReconnectMCPServer(name))}
				onReconnect={(name) => void mutate(() => app.ReconnectMCPServer(name))}
				onConfirmClearAuth={(name) => void mutate(() => app.ClearMCPServerAuthentication(name))}
				onToggle={(name, on) => void mutate(() => app.SetMCPServerEnabled(name, on))}
				onUpdate={(name, input) =>
					void mutate(() => app.UpdateMCPServer(name, input)).then((ok) => {
						if (ok) setEditing(null);
					})
				}
				onToggleDetails={toggleServer}
				onToggleTools={toggleServerTools}
			/>
			{adding ? (
				<AddServerForm busy={busy} onCancel={() => setAdding(false)} onAdd={async (input) => (await mutate(() => app.AddMCPServer(input))) && setAdding(false)} />
			) : null}
		</section>
	);
}

// SkillsSettingsPage is a self-contained skills management page embedded inside
// the settings centre.
export function SkillsSettingsPage() {
	const t = useT();
	const [view, setView] = useState<CapabilitiesView | null>(null);
	const [busy, setBusy] = useState(false);
	const [err, setErr] = useState<string | null>(null);
	const [skillQuery, setSkillQuery] = useState("");
	const [expandedSkills, setExpandedSkills] = useState<Set<string>>(() => new Set());
	const [skillTab, setSkillTab] = useState<"installed" | "marketplace">("installed");
	const [registryEntries, setRegistryEntries] = useState<RegistryEntryView[]>([]);
	const [registrySources, setRegistrySources] = useState<RegistrySourceView[]>([]);
	const [registryLoading, setRegistryLoading] = useState(false);
	const [registryQuery, setRegistryQuery] = useState("");
	const [installing, setInstalling] = useState<string | null>(null);

	const reload = useCallback(async () => {
		setView(normalizeCapabilitiesView(await app.Capabilities().catch(() => ({ servers: [], skills: [], skillRoots: [] }))));
	}, []);
	useEffect(() => { void reload(); }, [reload]);

	const loadRegistry = useCallback(async () => {
		setRegistryLoading(true);
		try {
			const [entries, sources] = await Promise.all([
				app.BrowseSkills().catch(() => []),
				app.RegistrySources().catch(() => []),
			]);
			setRegistryEntries(entries);
			setRegistrySources(sources);
		} finally {
			setRegistryLoading(false);
		}
	}, []);

	useEffect(() => {
		if (skillTab === "marketplace" && registryEntries.length === 0 && !registryLoading) {
			void loadRegistry();
		}
	}, [skillTab, registryEntries.length, registryLoading, loadRegistry]);

	const mutate = async (fn: () => Promise<unknown>) => {
		setBusy(true);
		setErr(null);
		try {
			await fn();
			await reload();
			return true;
		} catch (e) {
			setErr(String((e as Error)?.message ?? e));
			await reload();
			return false;
		} finally {
			setBusy(false);
		}
	};

	const handleInstall = async (name: string, global: boolean) => {
		setInstalling(name);
		setErr(null);
		try {
			await app.InstallSkillFromRegistry(name, global);
			await reload();
			await loadRegistry();
		} catch (e) {
			setErr(String((e as Error)?.message ?? e));
		} finally {
			setInstalling(null);
		}
	};

	const handleSearchRegistry = async () => {
		const q = registryQuery.trim();
		if (!q) { void loadRegistry(); return; }
		setRegistryLoading(true);
		try {
			const entries = await app.SearchRegistrySkills(q).catch(() => []);
			setRegistryEntries(entries);
		} finally {
			setRegistryLoading(false);
		}
	};

	const filteredSkills = useMemo(() => {
		if (!view) return [];
		const q = skillQuery.trim().toLowerCase();
		if (!q) return view.skills;
		return view.skills.filter((sk) => {
			const text = [sk.name, "/" + sk.name, sk.description, sk.scope, sk.runAs].join(" ").toLowerCase();
			return text.includes(q);
		});
	}, [view, skillQuery]);

	const skillSummary = useMemo(() => {
		if (!view) return "";
		return skillListSummary(view.skills, filteredSkills, skillQuery.trim().length > 0, t);
	}, [filteredSkills, skillQuery, t, view]);

	const toggleSkill = useCallback((name: string) => {
		setExpandedSkills((prev) => { const next = new Set(prev); if (next.has(name)) next.delete(name); else next.add(name); return next; });
	}, []);

	if (!view) return <div className="empty">{t("caps.loading")}</div>;

	return (
		<section className="mem-section">
			{err && <div className="banner banner--error">{err}</div>}

			{/* Tab switcher */}
			<div className="cap-tabs">
				<button
					className={`cap-tab${skillTab === "installed" ? " cap-tab--active" : ""}`}
					onClick={() => setSkillTab("installed")}
				>
					{t("caps.skills")}
				</button>
				<button
					className={`cap-tab${skillTab === "marketplace" ? " cap-tab--active" : ""}`}
					onClick={() => setSkillTab("marketplace")}
				>
					{t("caps.marketplace") ?? "Marketplace"}
				</button>
			</div>

			{skillTab === "installed" ? (
				<>
					<div className="cap-search">
						<input
							className="mem-input"
							type="search"
							placeholder={t("caps.searchSkills")}
							value={skillQuery}
							onChange={(e) => setSkillQuery(e.target.value)}
						/>
					</div>
					<SkillSources
						roots={view.skillRoots ?? []}
						busy={busy}
						onAdd={() => mutate(async () => {
							const path = await app.PickSkillFolder();
							if (path) await app.AddSkillPath(path);
						})}
						onRefresh={() => mutate(() => app.RefreshSkills())}
						onRemove={(path) => mutate(() => app.RemoveSkillPath(path))}
					/>
					<div className="cap-skills-head">
						<div className="cap-skills-head__copy">
							<div className="cap-skills-head__title">{t("caps.skills")}</div>
							<div className="cap-skills-head__summary">{skillSummary}</div>
						</div>
					</div>
					{view.skills.length === 0 ? (
						<div className="mem-empty">{t("caps.noSkills")}</div>
					) : filteredSkills.length === 0 ? (
						<div className="mem-empty">{t("caps.noSkillMatches")}</div>
					) : (
						<div className="cap-skills">
							{filteredSkills.map((sk) => (
								<SkillRow
									key={sk.name}
									skill={sk}
								busy={busy}
								expanded={expandedSkills.has(sk.name)}
								onToggle={() => toggleSkill(sk.name)}
								onToggleEnabled={(enabled) => void mutate(() => app.SetSkillEnabled(sk.name, enabled))}
							/>
						))}
					</div>
				)}
				</>
			) : (
				/* Marketplace tab */
				<>
					<div className="cap-search">
						<input
							className="mem-input"
							type="search"
							placeholder={t("caps.searchRegistry") ?? "Search marketplace..."}
							value={registryQuery}
							onChange={(e) => setRegistryQuery(e.target.value)}
							onKeyDown={(e) => { if (e.key === "Enter") void handleSearchRegistry(); }}
						/>
						<button className="cap-search-btn" onClick={() => void handleSearchRegistry()} disabled={registryLoading}>
							{registryLoading ? "..." : (t("caps.search") ?? "Search")}
						</button>
					</div>

					{/* Sources */}
					{registrySources.length > 0 && (
						<div className="cap-registry-sources">
							<div className="cap-registry-sources__title">{t("caps.sources") ?? "Sources"}</div>
							{registrySources.map((src) => (
								<div key={src.name} className="cap-registry-source">
									<span className="cap-registry-source__name">{src.name}</span>
									{src.trusted && <span className="cap-registry-source__badge cap-registry-source__badge--official">{t("caps.official") ?? "Official"}</span>}
									<span className="cap-registry-source__type">[{src.type}]</span>
								</div>
							))}
						</div>
					)}

					{/* Entries */}
					{registryLoading && registryEntries.length === 0 ? (
						<div className="mem-empty">{t("caps.loading")}</div>
					) : registryEntries.length === 0 ? (
						<div className="mem-empty">{t("caps.noRegistryEntries") ?? "No skills found in the marketplace. Check your network connection or add custom sources in Rexion.toml."}</div>
					) : (
						<div className="cap-skills">
							{registryEntries.map((entry) => (
								<div key={entry.name} className="cap-registry-entry">
									<div className="cap-registry-entry__header">
										<span className="cap-registry-entry__name">{entry.name}</span>
										{entry.installed ? (
											<span className="cap-registry-entry__badge cap-registry-entry__badge--installed">{t("caps.installed") ?? "Installed"}</span>
										) : installing === entry.name ? (
											<span className="cap-registry-entry__badge cap-registry-entry__badge--installing">{t("caps.installing") ?? "Installing..."}</span>
										) : (
											<div className="cap-registry-entry__actions">
												<button
													className="cap-registry-entry__btn cap-registry-entry__btn--install"
													onClick={() => void handleInstall(entry.name, false)}
													disabled={busy || installing !== null}
												>
													{t("caps.install") ?? "Install"}
												</button>
												<button
													className="cap-registry-entry__btn cap-registry-entry__btn--install-global"
													onClick={() => void handleInstall(entry.name, true)}
													disabled={busy || installing !== null}
													title={t("caps.installGlobal") ?? "Install globally for all projects"}
												>
													{t("caps.installGlobal") ?? "Global"}
												</button>
											</div>
										)}
									</div>
									{entry.description && <div className="cap-registry-entry__desc">{entry.description}</div>}
									<div className="cap-registry-entry__meta">
										{entry.source && <span className="cap-registry-entry__source">[{entry.source}]</span>}
										{entry.author && <span className="cap-registry-entry__author">{entry.author}</span>}
										{entry.runAs === "subagent" && <span className="cap-registry-entry__runas">subagent</span>}
									</div>
									{entry.tags && entry.tags.length > 0 && (
										<div className="cap-registry-entry__tags">
											{entry.tags.map((tag) => (
												<span key={tag} className="cap-registry-entry__tag">{tag}</span>
											))}
										</div>
									)}
								</div>
							))}
						</div>
					)}
				</>
			)}
	</section>
	);
}

// ─────────────────────────────────────────────────────────────────────────────
// OfficePluginsSettingsPage — a dedicated settings page listing the official
// office-flavoured MCP plugins (IM, mail, calendar, office docs, sheet, slides,
// search) with one-click enable and a simplified env-var form. It reuses the
// generic AddMCPServer/UpdateMCPServer/RemoveMCPServer/SetMCPServerEnabled
// bridge calls — no backend changes required — but presents each plugin with
// only the env variables it actually needs (sourced from each plugin's main.go
// header comment) instead of the generic KEY=VALUE textarea.
// ─────────────────────────────────────────────────────────────────────────────

type LocalizedText = { zh: string; en: string };

type OfficePluginEnvVar = {
	key: string;
	label: LocalizedText;
	hint?: LocalizedText;
	placeholder?: LocalizedText;
	required?: boolean;
	secret?: boolean;
};

type OfficePluginDef = {
	id: string;        // plugin name in Rexion.toml [[plugins]].name
	command: string;   // Rexion-plugin-<id>
	icon: ReactNode;
	color: string;     // accent class suffix for the left stripe
	title: LocalizedText;
	desc: LocalizedText;
	envVars: OfficePluginEnvVar[];
	autoStartTool?: string; // raw MCP tool name to call after plugin connects
};

const OFFICE_PLUGINS: OfficePluginDef[] = [
	{
		id: "im",
		command: "Rexion-plugin-im",
		icon: <MessageSquare size={16} />,
		color: "blue",
		title: { zh: "IM 即时通讯", en: "IM Messaging" },
		desc: {
			zh: "接收企业微信 / 飞书 / 钉钉的远程指令并回推执行结果。钉钉/飞书支持 Stream 长连接模式,无需公网 IP。",
			en: "Receive remote commands from WeCom / Feishu / DingTalk and push results back. DingTalk/Feishu support Stream long-connection mode (no public IP needed).",
		},
		autoStartTool: "auto_start",
		envVars: [
			{ key: "IM_BOT_PORT", label: { zh: "HTTP 监听端口", en: "HTTP listen port" }, placeholder: { zh: "9876", en: "9876" } },
			{
				key: "IM_BOT_TOKEN",
				label: { zh: "Webhook 验证 Token", en: "Webhook verification token" },
				hint: { zh: "可选;填写后需在 IM 平台回调 URL 上带 ?token=<该值>", en: "Optional; IM platform must append ?token=<value> to the callback URL" },
				secret: true,
			},
			{ key: "IM_WECOM_KEY", label: { zh: "企业微信 Webhook Key", en: "WeCom Webhook Key" }, secret: true },
			{ key: "IM_FEISHU_KEY", label: { zh: "飞书 Webhook Key", en: "Feishu Webhook Key" }, secret: true },
			{ key: "IM_DINGTALK_KEY", label: { zh: "钉钉 Access Token", en: "DingTalk Access Token" }, secret: true },
			{ key: "IM_DINGTALK_SECRET", label: { zh: "钉钉签名密钥", en: "DingTalk Sign Secret" }, secret: true },
			{
				key: "IM_DINGTALK_APP_KEY",
				label: { zh: "钉钉企业应用 AppKey (Stream 模式)", en: "DingTalk AppKey (Stream mode)" },
				hint: { zh: "Stream 长连接模式专用,无需公网 IP;与 Webhook 模式二选一", en: "Stream long-connection mode only, no public IP needed; mutually exclusive with Webhook mode" },
				secret: true,
			},
			{
				key: "IM_DINGTALK_APP_SECRET",
				label: { zh: "钉钉企业应用 AppSecret (Stream 模式)", en: "DingTalk AppSecret (Stream mode)" },
				hint: { zh: "Stream 长连接模式专用,无需公网 IP", en: "Stream long-connection mode only, no public IP needed" },
				secret: true,
			},
			{
				key: "IM_FEISHU_APP_ID",
				label: { zh: "飞书企业应用 App ID (Stream 模式)", en: "Feishu App ID (Stream mode)" },
				hint: { zh: "Stream 长连接模式专用,无需公网 IP;与 Webhook 模式二选一", en: "Stream long-connection mode only, no public IP needed; mutually exclusive with Webhook mode" },
				secret: true,
			},
			{
				key: "IM_FEISHU_APP_SECRET",
				label: { zh: "飞书企业应用 App Secret (Stream 模式)", en: "Feishu App Secret (Stream mode)" },
				hint: { zh: "Stream 长连接模式专用,无需公网 IP", en: "Stream long-connection mode only, no public IP needed" },
				secret: true,
			},
		],
	},
	{
		id: "dws",
		command: "Rexion-plugin-dws",
		icon: <Cloud size={16} />,
		color: "blue",
		title: { zh: "钉钉工作台 (dws)", en: "DingTalk Workspace (dws)" },
		desc: {
			zh: "通过 dws CLI 操作钉钉全产品能力:通讯录/日历/文档/AI表格/群聊/待办/审批/考勤/邮件/云盘/听记/知识库等。启用后自动检测安装和认证状态,未认证时自动跳转浏览器授权(管理员审批即可使用)。",
			en: "Operate DingTalk full product suite via dws CLI: contacts/calendar/docs/AI tables/chat/todo/approval/attendance/mail/drive/minutes/wiki etc. Auto-checks install & auth on enable; opens browser for OAuth if unauthenticated (admin approval = ready to use).",
		},
		autoStartTool: "dws_check",
		envVars: [
			{
				key: "DWS_PATH",
				label: { zh: "dws 可执行文件路径", en: "dws executable path" },
				hint: { zh: "可选;默认从 PATH 查找 dws,若安装路径不在 PATH 中可手动指定", en: "Optional; defaults to 'dws' from PATH. Specify if dws is installed outside PATH" },
				placeholder: { zh: "dws", en: "dws" },
			},
		],
	},
	{
		id: "mail",
		command: "Rexion-plugin-mail",
		icon: <Mail size={16} />,
		color: "green",
		title: { zh: "邮件", en: "Mail" },
		desc: {
			zh: "通过 IMAP 读取邮件、SMTP 发送邮件,支持分类与搜索。",
			en: "Read mail via IMAP, send via SMTP, with classify and search tools.",
		},
		envVars: [
			{ key: "MAIL_IMAP_HOST", label: { zh: "IMAP 服务器地址", en: "IMAP server address" }, placeholder: { zh: "imap.gmail.com:993", en: "imap.gmail.com:993" }, required: true },
			{ key: "MAIL_IMAP_USER", label: { zh: "IMAP 用户名", en: "IMAP username" }, required: true },
			{ key: "MAIL_IMAP_PASS", label: { zh: "IMAP 密码 / 应用专用密码", en: "IMAP password / App Password" }, secret: true, required: true },
			{ key: "MAIL_SMTP_HOST", label: { zh: "SMTP 服务器地址", en: "SMTP server address" }, placeholder: { zh: "smtp.gmail.com:587", en: "smtp.gmail.com:587" } },
			{ key: "MAIL_SMTP_USER", label: { zh: "SMTP 用户名", en: "SMTP username" } },
			{ key: "MAIL_SMTP_PASS", label: { zh: "SMTP 密码", en: "SMTP password" }, secret: true },
		],
	},
	{
		id: "office",
		command: "Rexion-plugin-office",
		icon: <FileText size={16} />,
		color: "orange",
		title: { zh: "文档处理", en: "Documents" },
		desc: {
			zh: "读写 docx、Markdown 转 PDF、渲染模板。PDF 转换需系统安装 pandoc。",
			en: "Read/write docx, Markdown to PDF, render templates. PDF needs pandoc installed.",
		},
		envVars: [],
	},
	{
		id: "sheet",
		command: "Rexion-plugin-sheet",
		icon: <Table size={16} />,
		color: "teal",
		title: { zh: "表格处理", en: "Spreadsheets" },
		desc: {
			zh: "读写 xlsx/csv,支持查询、聚合与图表生成。",
			en: "Read/write xlsx/csv with query, aggregation and chart tools.",
		},
		envVars: [],
	},
	{
		id: "slides",
		command: "Rexion-plugin-slides",
		icon: <Presentation size={16} />,
		color: "pink",
		title: { zh: "幻灯片", en: "Slides" },
		desc: {
			zh: "创建与编辑 pptx,支持主题、图表与 PDF 导出。基于 python-pptx(免费开源、无水印),启动时自动安装。",
			en: "Create and edit pptx with themes, charts and PDF export. Uses python-pptx (free open-source, no watermark), auto-installed on startup.",
		},
		envVars: [],
	},
	{
		id: "search",
		command: "Rexion-plugin-search",
		icon: <Search size={16} />,
		color: "blue",
		title: { zh: "网页搜索", en: "Web Search" },
		desc: {
			zh: "网页搜索、正文抽取与对比表格。web_search 需要 API Key。",
			en: "Web search, content extraction and comparison tables. web_search needs an API key.",
		},
		envVars: [
			{ key: "SEARCH_API_KEY", label: { zh: "搜索 API Key", en: "Search API Key" }, secret: true, required: true },
			{
				key: "SEARCH_API_PROVIDER",
				label: { zh: "搜索服务提供商", en: "Search provider" },
				hint: { zh: "可选:serpapi 或 bing", en: "Optional: serpapi or bing" },
				placeholder: { zh: "serpapi", en: "serpapi" },
			},
		],
	},
];

function pickLocaleText(kv: LocalizedText, locale: Locale): string {
	return locale === "zh" ? kv.zh : kv.en;
}

function officePluginStatusLabel(s: ServerView | undefined, locale: Locale): { text: string; tone: string } {
	if (!s) return { text: locale === "zh" ? "未配置" : "Not configured", tone: "neutral" };
	switch (s.status) {
		case "connected":
			return { text: locale === "zh" ? "已连接" : "Connected", tone: "project" };
		case "failed":
			return { text: locale === "zh" ? "连接失败" : "Failed", tone: "feedback" };
		case "initializing":
			return { text: locale === "zh" ? "启动中" : "Initializing", tone: "neutral" };
		case "deferred":
			return { text: locale === "zh" ? "待命" : "Deferred", tone: "neutral" };
		case "disabled":
			return { text: locale === "zh" ? "已禁用" : "Disabled", tone: "neutral" };
		default:
			return { text: s.status, tone: "neutral" };
	}
}

// OfficePluginsSettingsPage is a self-contained office-plugin management page
// embedded inside the settings centre.
export function OfficePluginsSettingsPage() {
	const { locale } = useI18n();
	const t = useT();
	const [view, setView] = useState<CapabilitiesView | null>(null);
	const [busy, setBusy] = useState(false);
	const [err, setErr] = useState<string | null>(null);

	const reload = useCallback(async () => {
		setView(normalizeCapabilitiesView(await app.Capabilities().catch(() => ({ servers: [], skills: [], skillRoots: [] }))));
	}, []);
	useEffect(() => { void reload(); }, [reload]);

	const mutate = async (fn: () => Promise<unknown>) => {
		setBusy(true);
		setErr(null);
		try {
			await fn();
			await reload();
			return true;
		} catch (e) {
			setErr(String((e as Error)?.message ?? e));
			await reload();
			return false;
		} finally {
			setBusy(false);
		}
	};

	const serverByName = useMemo(() => {
		const m = new Map<string, ServerView>();
		for (const s of view?.servers ?? []) m.set(s.name, s);
		return m;
	}, [view]);

	const configuredCount = useMemo(
		() => OFFICE_PLUGINS.filter((p) => serverByName.has(p.id)).length,
		[serverByName],
	);

	if (!view) return <div className="empty">{t("caps.loading")}</div>;

	return (
		<section className="mem-section">
			{err && <div className="banner banner--error">{err}</div>}
			<div className="drawer__summary" style={{ marginBottom: 12 }}>
				{locale === "zh"
					? `已配置 ${configuredCount}/${OFFICE_PLUGINS.length} 个办公插件`
					: `${configuredCount}/${OFFICE_PLUGINS.length} office plugins configured`}
			</div>
			<div className="office-plugins-grid">
				{OFFICE_PLUGINS.map((def) => (
					<OfficePluginCard
						key={def.id}
						def={def}
						server={serverByName.get(def.id)}
						busy={busy}
						locale={locale}
						onEnable={(env) => void mutate(() => app.AddMCPServer({
							name: def.id, transport: "stdio", command: def.command, args: [], url: "", env,
							...(def.autoStartTool ? { autoStartTool: def.autoStartTool } : {}),
						}))}
						onRemove={() => void mutate(() => app.RemoveMCPServer(def.id))}
						onToggle={(on) => void mutate(() => app.SetMCPServerEnabled(def.id, on))}
					/>
				))}
			</div>
		</section>
	);
}

function OfficePluginCard({
	def,
	server,
	busy,
	locale,
	onEnable,
	onRemove,
	onToggle,
}: {
	def: OfficePluginDef;
	server: ServerView | undefined;
	busy: boolean;
	locale: Locale;
	onEnable: (env: Record<string, string>) => void;
	onRemove: () => void;
	onToggle: (on: boolean) => void;
}) {
	const t = useT();
	const [expanded, setExpanded] = useState(false);
	const [envDraft, setEnvDraft] = useState<Record<string, string>>({});
	const configured = Boolean(server?.configured);
	const status = officePluginStatusLabel(server, locale);
	const enabled = server?.status === "connected" || server?.status === "deferred" || server?.status === "initializing";
	const hasEnv = def.envVars.length > 0;

	// For already-configured plugins the backend replaces env wholesale on
	// UpdateMCPServer, and existing secret values are not readable from the
	// frontend. Rather than risk silently wiping keys, configured plugins are
	// managed via enable/disable + remove; credentials are re-entered by
	// removing and re-adding (mirrors the KeyField "clear + set" pattern).
	const canSave = !configured && (!hasEnv || def.envVars.every((v) => !v.required || (envDraft[v.key] ?? "").trim() !== ""));

	const handleSave = () => {
		const env: Record<string, string> = {};
		for (const v of def.envVars) {
			const val = (envDraft[v.key] ?? "").trim();
			if (val) env[v.key] = val;
		}
		onEnable(env);
	};

	return (
		<article className={`provider-access-card provider-access-card--office provider-access-card--${def.color}`}>
			<div className="provider-access-card__head">
				<div className="provider-access-card__identity">
					<div className="provider-access-card__title">
						<span className="provider-access-card__icon">{def.icon}</span>
						{pickLocaleText(def.title, locale)}
						<span className={`badge badge--${status.tone}`}>{status.text}</span>
					</div>
					<div className="provider-access-card__desc">{pickLocaleText(def.desc, locale)}</div>
				</div>
				<div className="provider-access-card__actions">
					{configured ? (
						<Tooltip label={enabled ? t("caps.disable") : t("caps.enable")}>
							<label className="cap-switch">
								<input
									type="checkbox"
									checked={enabled}
									disabled={busy}
									onChange={(e) => onToggle(e.target.checked)}
								/>
								<span className="cap-switch__track" />
							</label>
						</Tooltip>
					) : (
						<button
							className="btn btn--primary btn--small"
							disabled={busy || !canSave}
							onClick={handleSave}
						>
							{locale === "zh" ? "启用" : "Enable"}
						</button>
					)}
				</div>
			</div>

			<div className="provider-access-meta">
				<span className="provider-model-chip provider-model-chip--mono">{def.command}</span>
				{hasEnv ? (
					<span>{locale === "zh" ? `${def.envVars.length} 个配置项` : `${def.envVars.length} settings`}</span>
				) : (
					<span>{locale === "zh" ? "无需配置" : "No setup required"}</span>
				)}
			</div>

			{!configured && hasEnv && (
				<div className="provider-card-block">
					<button
						type="button"
						className="btn btn--small"
						aria-expanded={expanded}
						onClick={() => setExpanded((v) => !v)}
					>
						{expanded ? t("common.collapse") : (locale === "zh" ? "配置" : "Configure")}
					</button>
					{expanded && (
						<div className="provider-editor provider-editor--office">
							{def.envVars.map((v) => (
								<div key={v.key} className="settings-field settings-field--stacked">
									<div className="settings-field__copy">
										<div className="settings-field__label">
											{pickLocaleText(v.label, locale)}
											{v.required && <span className="badge badge--feedback">*</span>}
										</div>
										{v.hint && <div className="settings-field__hint">{pickLocaleText(v.hint, locale)}</div>}
									</div>
									<div className="settings-field__control">
										<input
											className="mem-input"
											type={v.secret ? "password" : "text"}
											placeholder={v.placeholder ? pickLocaleText(v.placeholder, locale) : (v.secret ? "••••••" : "")}
											value={envDraft[v.key] ?? ""}
											disabled={busy}
											onChange={(e) => setEnvDraft((prev) => ({ ...prev, [v.key]: e.target.value }))}
										/>
									</div>
								</div>
							))}
							<div className="prov-card__actions">
								<button
									className="btn btn--primary btn--small"
									disabled={busy || !canSave}
									onClick={handleSave}
								>
									{locale === "zh" ? "保存并启用" : "Save & enable"}
								</button>
								<button className="btn btn--small" disabled={busy} onClick={() => setExpanded(false)}>
									{t("common.cancel")}
								</button>
							</div>
						</div>
					)}
				</div>
			)}

			{configured && (
				<div className="provider-card-block">
					{hasEnv && (
						<div className="provider-card-status provider-card-status--warn">
							{locale === "zh"
								? "如需修改凭证,请先移除再重新配置。"
								: "To change credentials, remove and reconfigure."}
						</div>
					)}
					{server?.error && (
						<div className="provider-card-status provider-card-status--warn">{server.error}</div>
					)}
					<div className="prov-card__actions">
						<InlineConfirmButton
							label={t("caps.remove")}
							confirmLabel={t("caps.confirmRemove")}
							cancelLabel={t("common.cancel")}
							disabled={busy}
							danger
							onConfirm={onRemove}
						/>
					</div>
				</div>
			)}
		</article>
	);
}
