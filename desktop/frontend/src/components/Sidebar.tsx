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
  FolderOpen,
  Puzzle,
  Clock,
  Brain,
  Calendar,
  ListChecks,
  Workflow,
  GitBranch,
  PanelLeftOpen,
  Newspaper,
  SquareTerminal,
  Palette,
} from "lucide-react";
import type { WorkspaceType } from "../lib/types";
import { useT } from "../lib/i18n";
import { ProjectTree } from "./ProjectTree";
import { OfficePanel } from "./OfficePanel";
import { Tooltip } from "./Tooltip";

// ── Sidebar section (collapsible) ──────────────────────────────
function loadSectionCollapsed(storageKey: string): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(`reasonix.sidebar.section.${storageKey}`) === "1";
  } catch {
    return false;
  }
}

function saveSectionCollapsed(storageKey: string, collapsed: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(`reasonix.sidebar.section.${storageKey}`, collapsed ? "1" : "0");
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
    storageKey ? loadSectionCollapsed(storageKey) : defaultCollapsed
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
  workspaceType: WorkspaceType;
  collapsed: boolean;
  /** Whether nav-item tooltips should be suppressed (shown only when sidebar is collapsed) */
  navTooltipDisabled: boolean;
  /** Callback to expand the sidebar when clicking the expand button in collapsed mode */
  onExpand?: () => void;

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

  // Settings tab navigation (for plugin management)
  onOpenSettingsTab: (tab: "mcp" | "skills" | "officePlugins") => void;

  // Bottom nav
  onOpenRepoWiki: () => void;
  onOpenAllHistory: () => void;
  onOpenTrash: () => void;
  onOpenSettings: () => void;
}

// ── Component ──────────────────────────────────────────────────
export function Sidebar({
  workspaceType,
  collapsed,
  navTooltipDisabled,
  onExpand,
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
  onOpenSettingsTab,
  onOpenRepoWiki,
  onOpenAllHistory,
  onOpenTrash,
  onOpenSettings,
}: SidebarProps) {
  const t = useT();

  // Render collapsed mini toolbar
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
          {workspaceType === "coding" && (
            <Tooltip label={t("sidebar.fileManagement")} side="right">
              <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("files"); }}>
                <FolderOpen size={16} />
              </button>
            </Tooltip>
          )}
          {workspaceType === "office" && (
            <>
              <Tooltip label={t("officePanel.productDesign")} side="right">
                <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onActivateSkill("product-design"); }}>
                  <Palette size={16} />
                </button>
              </Tooltip>
              <Tooltip label={t("sidebar.templates")} side="right">
                <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onOpenTemplates(); }}>
                  <FileText size={16} />
                </button>
              </Tooltip>
            </>
          )}
          {workspaceType === "assistant" && (
            <>
              <Tooltip label={t("sidebar.dailyBrief")} side="right">
                <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("dailyBrief"); }}>
                  <Newspaper size={16} />
                </button>
              </Tooltip>
              <Tooltip label={t("sidebar.assistantTodos")} side="right">
                <button className="sidebar__collapsed-btn" onClick={() => { onExpand?.(); onNavigate("todos"); }}>
                  <ListChecks size={16} />
                </button>
              </Tooltip>
            </>
          )}
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
      {/* ── New conversation ── */}
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

      {/* ── 首页 (Home) ── */}
      <SidebarSection title={t("sidebar.home")} storageKey="home">
        <SidebarNavItem
          icon={<Home size={15} />}
          label={t("sidebar.home")}
          tooltipDisabled={navTooltipDisabled}
          onClick={() => onNavigate("home")}
          active={activePage === "home"}
        />
      </SidebarSection>

      {/* ── 工作台 (Workbench) — dynamic by workspaceType ── */}
      <SidebarSection title={t("sidebar.workbench")} storageKey="workbench">
        {workspaceType === "coding" && (
          <>
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
          </>
        )}
        {workspaceType === "office" && (
          <>
            <OfficePanel
              onActivateSkill={onActivateSkill}
              onOpenTemplates={onOpenTemplates}
            />
          </>
        )}
        {workspaceType === "assistant" && (
          <div className="sidebar__placeholder">
            <SidebarNavItem
              icon={<Newspaper size={15} />}
              label={t("sidebar.dailyBrief")}
              tooltipDisabled={navTooltipDisabled}
              onClick={() => onNavigate("dailyBrief")}
              active={activePage === "dailyBrief"}
            />
            <SidebarNavItem
              icon={<Calendar size={15} />}
              label={t("sidebar.assistantSchedule")}
              tooltipDisabled={navTooltipDisabled}
              onClick={() => onNavigate("calendar")}
              active={activePage === "calendar"}
            />
            <SidebarNavItem
              icon={<ListChecks size={15} />}
              label={t("sidebar.assistantTodos")}
              tooltipDisabled={navTooltipDisabled}
              onClick={() => onNavigate("todos")}
              active={activePage === "todos"}
            />
          </div>
        )}
      </SidebarSection>

      {/* ── 资源 (Resources) — dynamic by workspaceType ── */}
      <SidebarSection title={t("sidebar.resources")} storageKey="resources">
        {workspaceType === "coding" && (
          <>
            <SidebarNavItem
              icon={<FolderOpen size={15} />}
              label={t("sidebar.fileManagement")}
              tooltipDisabled={navTooltipDisabled}
              onClick={() => onNavigate("files")}
            />
            <SidebarNavItem
              icon={<Puzzle size={15} />}
              label={t("sidebar.devPlugins")}
              tooltipDisabled={navTooltipDisabled}
              onClick={() => onOpenSettingsTab("officePlugins")}
            />
          </>
        )}
        {workspaceType === "office" && (
          <>
            <SidebarNavItem
              icon={<FileText size={15} />}
              label={t("sidebar.templates")}
              tooltipDisabled={navTooltipDisabled}
              onClick={onOpenTemplates}
            />
            <SidebarNavItem
              icon={<Puzzle size={15} />}
              label={t("sidebar.officePlugins")}
              tooltipDisabled={navTooltipDisabled}
              onClick={() => onOpenSettingsTab("officePlugins")}
            />
          </>
        )}
        {workspaceType === "assistant" && (
          <>
            <SidebarNavItem
              icon={<FolderOpen size={15} />}
              label={t("sidebar.allFiles")}
              tooltipDisabled={navTooltipDisabled}
              onClick={() => onNavigate("files")}
            />
            <SidebarNavItem
              icon={<Puzzle size={15} />}
              label={t("sidebar.lifePlugins")}
              tooltipDisabled={navTooltipDisabled}
              onClick={() => onOpenSettingsTab("officePlugins")}
            />
          </>
        )}
      </SidebarSection>

      {/* ── 管理 (Management) ── */}
      <SidebarSection title={t("sidebar.management")} storageKey="management">
        {/* 子分组1：任务管理 */}
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
        <div className="sidebar__subgroup-divider" />
        {/* 子分组2：记忆与历史 */}
        <SidebarNavItem
          icon={<Brain size={15} />}
          label={t("sidebar.memory")}
          tooltipDisabled={navTooltipDisabled}
          onClick={() => onNavigate("memory")}
        />
        <SidebarNavItem
          icon={<BookOpen size={15} />}
          label={t("sidebar.repoWiki")}
          tooltipDisabled={navTooltipDisabled}
          onClick={onOpenRepoWiki}
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
        <div className="sidebar__subgroup-divider" />
        {/* 子分组3：系统 */}
        <SidebarNavItem
          icon={<SettingsIcon size={15} />}
          label={t("topbar.settings")}
          tooltipDisabled={navTooltipDisabled}
          onClick={onOpenSettings}
        />
      </SidebarSection>
    </aside>
  );
}
