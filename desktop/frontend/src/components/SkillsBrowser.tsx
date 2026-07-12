import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Search,
  Store,
  CheckCircle2,
  Download,
  Trash2,
  RefreshCw,
  Tag,
  User,
  Globe,
  Folder,
  AlertCircle,
  X,
} from "lucide-react";
import type { RegistryEntryView, RegistrySourceView, SkillView } from "../lib/types";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";

// SkillsBrowser is the desktop counterpart to `Rexion skill` CLI. It surfaces
// three views — Installed, Marketplace, Search — all backed by the existing
// bridge methods (BrowseSkills / SearchRegistrySkills / InstallSkillFromRegistry
// / UninstallSkill / Capabilities). The component owns no state machine: each
// view fetches on mount and exposes a manual refresh button for explicit retry.
type BrowserView = "installed" | "marketplace" | "search";

export interface SkillsBrowserProps {
  /** Optional callback when the user installs a skill — used by the parent to
   * refresh any skill-dependent UI (e.g. the capabilities drawer). */
  onSkillsChanged?: () => void;
  /** Optional close handler — when provided, a close button is rendered in
   * the header. Used when the browser is hosted in a modal overlay. */
  onClose?: () => void;
}

export function SkillsBrowser({ onSkillsChanged, onClose }: SkillsBrowserProps) {
  const t = useT();
  const [view, setView] = useState<BrowserView>("marketplace");
  const [installed, setInstalled] = useState<SkillView[]>([]);
  const [market, setMarket] = useState<RegistryEntryView[]>([]);
  const [sources, setSources] = useState<RegistrySourceView[]>([]);
  const [query, setQuery] = useState("");
  const [searchResults, setSearchResults] = useState<RegistryEntryView[] | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Per-entry install/uninstall in-flight tracking so the button can show a
  // spinner and reject double-clicks.
  const [pending, setPending] = useState<Record<string, boolean>>({});

  const loadInstalled = useCallback(async () => {
    try {
      const caps = await app.Capabilities();
      setInstalled(caps.skills ?? []);
    } catch (e) {
      // Installed-list failures are non-fatal — the marketplace view still works.
      setInstalled([]);
      console.warn("SkillsBrowser: load installed failed", e);
    }
  }, []);

  const loadMarket = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [entries, srcs] = await Promise.all([
        app.BrowseSkills(),
        app.RegistrySources(),
      ]);
      setMarket(entries ?? []);
      setSources(srcs ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setMarket([]);
      setSources([]);
    } finally {
      setLoading(false);
    }
  }, []);

  const loadSearch = useCallback(async (q: string) => {
    const trimmed = q.trim();
    if (trimmed === "") {
      setSearchResults(null);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const results = await app.SearchRegistrySkills(trimmed);
      setSearchResults(results ?? []);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
      setSearchResults([]);
    } finally {
      setLoading(false);
    }
  }, []);

  // Initial load for the default marketplace view.
  useEffect(() => {
    void loadMarket();
    void loadInstalled();
  }, [loadMarket, loadInstalled]);

  const handleInstall = useCallback(
    async (name: string, global: boolean) => {
      setPending((p) => ({ ...p, [name]: true }));
      setError(null);
      try {
        await app.InstallSkillFromRegistry(name, global);
        // Refresh both lists so the "installed" badge appears immediately.
        await Promise.all([loadMarket(), loadInstalled()]);
        onSkillsChanged?.();
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        setPending((p) => {
          const next = { ...p };
          delete next[name];
          return next;
        });
      }
    },
    [loadMarket, loadInstalled, onSkillsChanged],
  );

  const handleUninstall = useCallback(
    async (name: string) => {
      setPending((p) => ({ ...p, [name]: true }));
      setError(null);
      try {
        await app.UninstallSkill(name);
        await loadInstalled();
        await loadMarket();
        onSkillsChanged?.();
      } catch (e) {
        setError(e instanceof Error ? e.message : String(e));
      } finally {
        setPending((p) => {
          const next = { ...p };
          delete next[name];
          return next;
        });
      }
    },
    [loadInstalled, loadMarket, onSkillsChanged],
  );

  const tabs = useMemo<Array<{ key: BrowserView; label: string; icon: React.ReactNode }>>(
    () => [
      { key: "marketplace", label: t("skillsBrowser.tabMarketplace"), icon: <Store size={15} /> },
      { key: "installed", label: t("skillsBrowser.tabInstalled"), icon: <CheckCircle2 size={15} /> },
      { key: "search", label: t("skillsBrowser.tabSearch"), icon: <Search size={15} /> },
    ],
    [t],
  );

  return (
    <div className="skills-browser">
      <header className="skills-browser__header">
        <div className="skills-browser__title-row">
          <h1 className="skills-browser__title">
            <Store size={20} />
            <span>{t("skillsBrowser.title")}</span>
          </h1>
          <div className="skills-browser__title-actions">
            <button
              className="skills-browser__refresh"
              onClick={() => {
                if (view === "marketplace") void loadMarket();
                else if (view === "installed") void loadInstalled();
                else if (query.trim()) void loadSearch(query);
              }}
              aria-label={t("skillsBrowser.refresh")}
              title={t("skillsBrowser.refresh")}
            >
              <RefreshCw size={15} />
            </button>
            {onClose && (
              <button
                className="skills-browser__close"
                onClick={onClose}
                aria-label={t("common.close")}
                title={t("common.close")}
              >
                <X size={16} />
              </button>
            )}
          </div>
        </div>
        <p className="skills-browser__subtitle">{t("skillsBrowser.subtitle")}</p>
        <div className="skills-browser__tabs" role="tablist">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              role="tab"
              aria-selected={view === tab.key}
              className={`skills-browser__tab${view === tab.key ? " skills-browser__tab--active" : ""}`}
              onClick={() => setView(tab.key)}
            >
              {tab.icon}
              <span>{tab.label}</span>
            </button>
          ))}
        </div>
      </header>

      {error && (
        <div className="skills-browser__error" role="alert">
          <AlertCircle size={14} />
          <span>{error}</span>
        </div>
      )}

      {view === "search" && (
        <div className="skills-browser__searchbar">
          <Search size={15} />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void loadSearch(query);
            }}
            placeholder={t("skillsBrowser.searchPlaceholder")}
            aria-label={t("skillsBrowser.searchPlaceholder")}
          />
          <button
            className="skills-browser__search-btn"
            onClick={() => void loadSearch(query)}
            disabled={loading || query.trim() === ""}
          >
            {t("skillsBrowser.search")}
          </button>
        </div>
      )}

      <div className="skills-browser__body">
        {loading && view !== "search" && (
          <div className="skills-browser__loading">{t("common.loading")}</div>
        )}

        {view === "marketplace" && (
          <MarketplaceList
            entries={market}
            sources={sources}
            loading={loading}
            pending={pending}
            onInstall={handleInstall}
          />
        )}

        {view === "installed" && (
          <InstalledList
            skills={installed}
            pending={pending}
            onUninstall={handleUninstall}
          />
        )}

        {view === "search" && searchResults !== null && (
          <MarketplaceList
            entries={searchResults}
            sources={[]}
            loading={loading}
            pending={pending}
            onInstall={handleInstall}
            emptyHint={t("skillsBrowser.searchEmpty")}
          />
        )}
      </div>
    </div>
  );
}

// ── Marketplace list ──────────────────────────────────────────
function MarketplaceList({
  entries,
  sources,
  loading,
  pending,
  onInstall,
  emptyHint,
}: {
  entries: RegistryEntryView[];
  sources: RegistrySourceView[];
  loading: boolean;
  pending: Record<string, boolean>;
  onInstall: (name: string, global: boolean) => void;
  emptyHint?: string;
}) {
  const t = useT();
  if (!loading && entries.length === 0) {
    return (
      <div className="skills-browser__empty">
        {emptyHint ?? t("skillsBrowser.marketplaceEmpty")}
      </div>
    );
  }
  return (
    <>
      {sources.length > 0 && (
        <div className="skills-browser__sources">
          {sources.map((s) => (
            <span key={s.name} className="skills-browser__source-chip" title={s.url}>
              {s.trusted && <Tag size={11} />}
              <span className="skills-browser__source-name">{s.name}</span>
            </span>
          ))}
        </div>
      )}
      <ul className="skills-browser__list">
        {entries.map((e) => (
          <li key={e.name} className="skills-browser__item">
            <div className="skills-browser__item-main">
              <div className="skills-browser__item-head">
                <span className="skills-browser__item-name">{e.name}</span>
                {e.installed && (
                  <span className="skills-browser__badge skills-browser__badge--installed">
                    <CheckCircle2 size={11} />
                    <span>{t("skillsBrowser.installed")}</span>
                  </span>
                )}
                {e.runAs === "subagent" && (
                  <span className="skills-browser__badge">{t("skillsBrowser.subagent")}</span>
                )}
              </div>
              {e.description && (
                <p className="skills-browser__item-desc">{e.description}</p>
              )}
              <div className="skills-browser__item-meta">
                {e.author && (
                  <span className="skills-browser__meta">
                    <User size={11} />
                    <span>{e.author}</span>
                  </span>
                )}
                {e.version && (
                  <span className="skills-browser__meta">
                    <Tag size={11} />
                    <span>v{e.version}</span>
                  </span>
                )}
                {e.source && (
                  <span className="skills-browser__meta">
                    <Folder size={11} />
                    <span>{e.source}</span>
                  </span>
                )}
              </div>
              {e.tags && e.tags.length > 0 && (
                <div className="skills-browser__tags">
                  {e.tags.map((tag) => (
                    <span key={tag} className="skills-browser__tag">{tag}</span>
                  ))}
                </div>
              )}
            </div>
            <div className="skills-browser__item-actions">
              {e.installed ? (
                <span className="skills-browser__done">{t("skillsBrowser.installed")}</span>
              ) : pending[e.name] ? (
                <span className="skills-browser__installing">{t("skillsBrowser.installing")}</span>
              ) : (
                <div className="skills-browser__install-group">
                  <button
                    className="skills-browser__install-btn"
                    onClick={() => void onInstall(e.name, false)}
                    title={t("skillsBrowser.installProject")}
                  >
                    <Download size={13} />
                    <span>{t("skillsBrowser.install")}</span>
                  </button>
                  <button
                    className="skills-browser__install-btn skills-browser__install-btn--global"
                    onClick={() => void onInstall(e.name, true)}
                    title={t("skillsBrowser.installGlobalHint")}
                  >
                    <Globe size={13} />
                    <span>{t("skillsBrowser.installGlobal")}</span>
                  </button>
                </div>
              )}
            </div>
          </li>
        ))}
      </ul>
    </>
  );
}

// ── Installed list ────────────────────────────────────────────
function InstalledList({
  skills,
  pending,
  onUninstall,
}: {
  skills: SkillView[];
  pending: Record<string, boolean>;
  onUninstall: (name: string) => void;
}) {
  const t = useT();
  if (skills.length === 0) {
    return (
      <div className="skills-browser__empty">{t("skillsBrowser.installedEmpty")}</div>
    );
  }
  // Group by scope so users see builtin / project / custom / global clusters.
  const grouped = skills.reduce<Record<string, SkillView[]>>((acc, s) => {
    const key = s.scope || "other";
    (acc[key] ??= []).push(s);
    return acc;
  }, {});
  const scopeOrder = ["builtin", "project", "custom", "global", "other"];
  const scopes = Object.keys(grouped).sort(
    (a, b) => scopeOrder.indexOf(a) - scopeOrder.indexOf(b),
  );
  return (
    <ul className="skills-browser__list">
      {scopes.map((scope) => (
        <li key={scope} className="skills-browser__scope-group">
          <div className="skills-browser__scope-label">
            {t(`skillsBrowser.scope.${scope}` as any) !== `skillsBrowser.scope.${scope}`
              ? t(`skillsBrowser.scope.${scope}` as any)
              : scope}
            <span className="skills-browser__scope-count">{grouped[scope].length}</span>
          </div>
          <ul className="skills-browser__list skills-browser__list--nested">
            {grouped[scope].map((s) => (
              <li key={`${s.scope}-${s.name}`} className="skills-browser__item">
                <div className="skills-browser__item-main">
                  <div className="skills-browser__item-head">
                    <span className="skills-browser__item-name">/{s.name}</span>
                    {s.runAs === "subagent" && (
                      <span className="skills-browser__badge">{t("skillsBrowser.subagent")}</span>
                    )}
                    {!s.enabled && (
                      <span className="skills-browser__badge skills-browser__badge--disabled">
                        {t("skillsBrowser.disabled")}
                      </span>
                    )}
                  </div>
                  {s.description && (
                    <p className="skills-browser__item-desc">{s.description}</p>
                  )}
                </div>
                <div className="skills-browser__item-actions">
                  {pending[s.name] ? (
                    <span className="skills-browser__installing">{t("skillsBrowser.uninstalling")}</span>
                  ) : (
                    <button
                      className="skills-browser__uninstall-btn"
                      onClick={() => void onUninstall(s.name)}
                      title={t("skillsBrowser.uninstall")}
                    >
                      <Trash2 size={13} />
                      <span>{t("skillsBrowser.uninstall")}</span>
                    </button>
                  )}
                </div>
              </li>
            ))}
          </ul>
        </li>
      ))}
    </ul>
  );
}

export default SkillsBrowser;
