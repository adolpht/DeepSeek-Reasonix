import { useState } from "react";
import {
  SquarePen,
  BookOpen,
  FileText,
  History,
  Trash2,
  Settings as SettingsIcon,
  Home,
  ChevronDown,
  ChevronRight,
  Clock,
  Brain,
  Workflow,
  GitBranch,
  PanelLeftOpen,
  PanelLeftClose,
  SquareTerminal,
  Store,
  Palette,
  Smartphone,
} from "lucide-react";
import type { WorkspaceType } from "../lib/types";
import { useT } from "../lib/i18n";
import { ProjectTree } from "./ProjectTree";
import { OfficePanel } from "./OfficePanel";
import { Tooltip } from "./Tooltip";

// ── Sidebar section (collapsible) ──────────────────────────────
function loadSectionCollapsed(storageKey: string, defaultCollapsed = false): boolean {
  if (typeof window === "undefined") return defaultCollapsed;
  try {
    const v = window.localStorage.getItem(`Rexion.sidebar.section.${storageKey}`);
    if (v === null) return defaultCollapsed;
    return v === "1";
  } catch {
    return defaultCollapsed;
  }
}

function saveSectionCollapsed(storageKey: string, collapsed: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(`Rexion.sidebar.section.${storageKey}`, collapsed ? "1" : "0");
  } catch {
    /* ignore storage failures */
  }
}

function SidebarSection({
  title,
  defaultCollapsed = false,
  storageKey,
  children,
}: {
  title: string;
  defaultCollapsed?: boolean;
  storageKey?: string;
  children: React.ReactNode;
}) {
  const [collapsed, setCollapsed] = useState(() =>
    storageKey ? loadSectionCollapsed(storageKey, defaultCollapsed) : defaultCollapsed
  );
  const toggle = () => {
    setCollapsed((c) => {
      const next = !c;
      if (storageKey) saveSectionCollapsed(storageKey, next);
      return next;
    });
  };
  return (
    <section className="sidebar__section sidebar__section--group">
      <button
        type="button"
        className="sidebar__section-header"
        onClick={toggle}
      >
        {collapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
        <span>{title}</span>
      </button>
      {!collapsed && <div className="sidebar__section-body">{children}</div>}
    </section>
  );
}

// ── Sidebar navigation item ────────────────────────────────────
function SidebarNavItem({
  icon,
  label,
  tooltipDisabled,
  onClick,
  active = false,
}: {
  icon: React.ReactNode;
  label: string;
  tooltipDisabled: boolean;
  onClick: () => void;
  active?: boolean;
}) {
  return (
    <Tooltip label={label} fill side="right" disabled={tooltipDisabled}>
      <button
        className={`sidebar__navitem${active ? " sidebar__navitem--active" : ""}`}
        onClick={onClick}
        aria-current={active ? "page" : undefined}
      >
        {icon}
        <span>{label}</span>
      </button>
    </Tooltip>
  );
}

// ── Props ──────────────────────────────────────────────────────
export interface SidebarProps {
  /** Kept for callers' compatibility; no longer gates which items appear. */
  workspaceType?: WorkspaceType;
  collapsed: boolean;
  /** Whether nav-item tooltips should be suppressed (shown only when sidebar is collapsed) */
  navTooltipDisabled: boolean;
  /** Callback to expand the sidebar when clicking the expand button in collapsed mode */
  onExpand?: () => void;
  /** Callback to collapse the sidebar when clicking the collapse button in expanded mode */
  onCollapse?: () => void;

  // New-session
  onNewSession: () => void;
  isRunning: boolean;

  // Home
  onNavigate: (page: string) => void;
  /** Current navPage (or null) — used to highlight the active sidebar item. */
  activePage?: string | null;

  // ProjectTree
  activeScope?: string;
  activeWorkspaceRoot?: string;
  activeTopicId?: string;
  onOpenTopic: (scope: string, workspaceRoot: string, topicId: string) => Promise<void> | void;
  onOpenProjectHistory: (scope: "global" | "project", workspaceRoot: string) => Promise<void> | void;
  onTopicsChanged: () => Promise<void>;
  onRenameTopic: (topicId: string, title: string) => Promise<void>;
  refreshSignal: number;
  onAddProject: () => Promise<void>;

  // OfficePanel
  onActivateSkill: (name: string) => void;
  onOpenTemplates: () => void;

  // Bottom nav
  onOpenRepoWiki: () => void;
  onOpenAllHistory: () => void;
  onOpenTrash: () => void;
  onOpenSettings: () => void;
}

// ── Component ──────────────────────────────────────────────────
export function Sidebar({
  collapsed,
  navTooltipDisabled,
  onExpand,
  onCollapse,
  onNewSession,
  isRunning,
  onNavigate,
  activePage,
  activeScope,
  activeWorkspaceRoot,
  activeTopicId,
  onOpenTopic,
  onOpenProjectHistory,
  onTopicsChanged,
  onRenameTopic,
  refreshSignal,
  onAddProject,
  onActivateSkill,
  onOpenTemplates,
  onOpenRepoWiki,
  onOpenAllHistory,
  onOpenTrash,
  onOpenSettings,
}: SidebarProps) {
  const t = useT();

  // Render collapsed mini toolbar.
  // Mode-specific items are no longer gated — every capability is reachable
  // in collapsed form too.
  if (collapsed) {
    return (
      <aside className="sidebar sidebar--collapsed" aria-label={t("sidebar.navigation")}>
        <div className="sidebar__collapsed-bar">
          <Tooltip label={t("sidebar.expand")} side="right">
            <button className="sidebar__collapsed-btn" onClick={onExpand}>
              <PanelLeftOpen size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("sidebar.home")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("home"); }}>
              <Home size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("officePanel.productDesign")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onActivateSkill("product-design"); }}>
              <SquarePen size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("sidebar.templates")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onOpenTemplates(); }}>
              <FileText size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("sidebar.trace")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("trace"); }}>
              <Workflow size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("sidebar.workflow")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("workflow"); }}>
              <GitBranch size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("sidebar.skillsMarket")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("skillsMarket"); }}>
              <Store size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("sidebar.designPanel")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("designPanel"); }}>
              <Palette size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("sidebar.sessionSync")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("sessionSync"); }}>
              <Smartphone size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("sidebar.terminal")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("terminal"); }}>
              <SquareTerminal size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("sidebar.allHistory")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); void onOpenAllHistory(); }}>
              <History size={16} />
            </button>
          </Tooltip>
          <Tooltip label={t("topbar.settings")} side="right">
            <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onOpenSettings(); }}>
              <SettingsIcon size={16} />
            </button>
          </Tooltip>
        </div>
      </aside>
    );
  }

  return (
    <aside className="sidebar" aria-label={t("sidebar.navigation")}>
      {/* ── Top: Collapse button + New conversation + Home (fixed) ── */}
      <Tooltip label={t("sidebar.collapse")} fill>
        <button
          className="sidebar__collapse-btn"
          type="button"
          onClick={onCollapse}
          aria-label={t("sidebar.collapse")}
        >
          <PanelLeftClose size={15} />
        </button>
      </Tooltip>
      <Tooltip label={t("topbar.newSession")} fill>
        <button
          className="sidebar__new"
          onClick={() => {
            if (isRunning) return;
            onNewSession();
          }}
        >
          <SquarePen size={15} />
          <span>{t("topbar.newSession")}</span>
        </button>
      </Tooltip>

      <div className="sidebar__topnav">
        <SidebarNavItem
          icon={<Home size={15} />}
          label={t("sidebar.home")}
          tooltipDisabled={navTooltipDisabled}
          onClick={() => onNavigate("home")}
          active={activePage === "home"}
        />
      </div>

      {/* ── Scrollable middle: categorized capabilities ── */}
      <div className="sidebar__scroll">
        {/* ── 会话 (Conversations) ── */}
        <SidebarSection title={t("sidebar.conversations")} storageKey="conversations">
          <ProjectTree
            activeScope={activeScope}
            activeWorkspaceRoot={activeWorkspaceRoot}
            activeTopicId={activeTopicId}
            onOpenTopic={onOpenTopic}
            onOpenProjectHistory={onOpenProjectHistory}
            onTopicsChanged={onTopicsChanged}
            onRenameTopic={onRenameTopic}
            refreshSignal={refreshSignal}
            onAddProject={onAddProject}
          />
        </SidebarSection>

        {/* ── 办公能力 (Office capabilities) — always visible ── */}
        <SidebarSection title={t("officePanel.title")} storageKey="office">
          <OfficePanel onActivateSkill={onActivateSkill} />
        </SidebarSection>

        {/* ── 管理 (Management) ── */}
        <SidebarSection title={t("sidebar.management")} storageKey="management">
          <SidebarNavItem
            icon={<Clock size={15} />}
            label={t("sidebar.scheduledTasks")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => onNavigate("scheduled")}
            active={activePage === "scheduled"}
          />
          <SidebarNavItem
            icon={<SquareTerminal size={15} />}
            label={t("sidebar.terminal")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => onNavigate("terminal")}
            active={activePage === "terminal"}
          />
          <SidebarNavItem
            icon={<Workflow size={15} />}
            label={t("sidebar.trace")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => onNavigate("trace")}
            active={activePage === "trace"}
          />
          <SidebarNavItem
            icon={<GitBranch size={15} />}
            label={t("sidebar.workflow")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => onNavigate("workflow")}
            active={activePage === "workflow"}
          />
          <SidebarNavItem
            icon={<Store size={15} />}
            label={t("sidebar.skillsMarket")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => onNavigate("skillsMarket")}
            active={activePage === "skillsMarket"}
          />
          <SidebarNavItem
            icon={<Palette size={15} />}
            label={t("sidebar.designPanel")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => onNavigate("designPanel")}
            active={activePage === "designPanel"}
          />
          <SidebarNavItem
            icon={<Smartphone size={15} />}
            label={t("sidebar.sessionSync")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => onNavigate("sessionSync")}
            active={activePage === "sessionSync"}
          />
          <SidebarNavItem
            icon={<BookOpen size={15} />}
            label={t("sidebar.repoWiki")}
            tooltipDisabled={navTooltipDisabled}
            onClick={onOpenRepoWiki}
          />
        </SidebarSection>
      </div>

      {/* ── Bottom: 配置 (Config) — fixed ── */}
      <div className="sidebar__bottom">
        <SidebarSection title={t("sidebar.config")} storageKey="config" defaultCollapsed>
          <SidebarNavItem
            icon={<Brain size={15} />}
            label={t("sidebar.memory")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => onNavigate("memory")}
          />
          <SidebarNavItem
            icon={<History size={15} />}
            label={t("sidebar.allHistory")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => void onOpenAllHistory()}
          />
          <SidebarNavItem
            icon={<Trash2 size={15} />}
            label={t("sidebar.trash")}
            tooltipDisabled={navTooltipDisabled}
            onClick={() => void onOpenTrash()}
          />
          <SidebarNavItem
            icon={<FileText size={15} />}
            label={t("sidebar.templates")}
            tooltipDisabled={navTooltipDisabled}
            onClick={onOpenTemplates}
          />
          <div className="sidebar__subgroup-divider" />
          <SidebarNavItem
            icon={<SettingsIcon size={15} />}
            label={t("topbar.settings")}
            tooltipDisabled={navTooltipDisabled}
            onClick={onOpenSettings}
          />
        </SidebarSection>
      </div>
    </aside>
  );
}
