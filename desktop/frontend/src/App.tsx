import { useCallback, useDeferredValue, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { ShellExpandProvider, useShellExpand } from "./lib/shellExpand";
import {
  Download,
  CircleGauge,
  Eye,
  FileText,
  FileJson,
  GitBranch,
  Pencil,
  PanelRightClose,
  PanelRightOpen,
  TerminalSquare,
  Calendar,
  Newspaper,
  MessageSquare,
} from "lucide-react";
import { asArray } from "./lib/array";
import { clearLegacyLangPref, normalizeLangPref, readLegacyLangPref, t, useI18n, useT } from "./lib/i18n";
import { useController, type Item, type LiveStream } from "./lib/useController";
import { app, onProjectTreeChanged, onTabsChanged } from "./lib/bridge";
import { Transcript, type TranscriptHandle } from "./components/Transcript";
import { Composer } from "./components/Composer";
import { TodoPanel } from "./components/TodoPanel";
import { ApprovalModal } from "./components/ApprovalModal";
import { AskCard } from "./components/AskCard";
import { StatusBar } from "./components/StatusBar";
import { HistoryPanel } from "./components/HistoryPanel";
import { SettingsPanel } from "./components/SettingsPanel";
import { UpdateBanner } from "./components/UpdateBanner";
import { ContextPanel, type CompactionRecord, type MemoryFactBrief } from "./components/ContextPanel";
import { PreviewPanel } from "./components/PreviewPanel";
import { WorkspacePanel } from "./components/WorkspacePanel";
import { Tooltip } from "./components/Tooltip";
import { StartupSplash, shouldShowStartupSplash } from "./components/StartupSplash";
import { OnboardingOverlay, isOnboardingTaskPending, clearOnboardingTaskPending } from "./components/OnboardingOverlay";
import { TabBar } from "./components/TabBar";
import { CopyButton } from "./components/CopyButton";
import { RepoWikiPanel } from "./components/RepoWikiPanel";
import { TemplateLibrary } from "./components/TemplateLibrary";
import { Sidebar } from "./components/Sidebar";
import { HomePanel } from "./components/HomePanel";
import { SkillsBrowser } from "./components/SkillsBrowser";
import { DesignPanel } from "./components/DesignPanel";
import { SessionSync } from "./components/SessionSync";
import { SupervisionOverlay } from "./components/SupervisionOverlay";
import type { SupervisionAction } from "./components/SupervisionOverlay";
import { CalendarPanel } from "./components/CalendarPanel";
import { SideChat } from "./components/SideChat";
import { IMSessionsPanel } from "./components/IMSessionsPanel";
import { SchedulerPanel } from "./components/SchedulerPanel";
import { ResizableDrawer } from "./components/ResizableDrawer";
import { SaveRecipeModal } from "./components/SaveRecipeModal";
import { DailyBriefPanel } from "./components/DailyBriefPanel";
import { AgentCanvas } from "./components/AgentCanvas";
import { WorkflowEditor } from "./components/WorkflowEditor";
import { TerminalPanel } from "./components/TerminalPanel";
import { FloatingWindow } from "./components/FloatingWindow";
import { ProgressStepper } from "./components/ProgressStepper";
import type { Step as ProgressStep } from "./components/ProgressStepper";
import { CommandPalette, type PaletteItem } from "./components/CommandPalette";
import { NotificationCenter, NotificationBell } from "./components/NotificationCenter";
import { diffsFor, docExportPath, parseTodos } from "./lib/tools";
import { shouldShowTodoPanel } from "./lib/todoVisibility";
import type { ComposerInsertRequest, Mode, SessionMeta, SettingsTab, SkillView, TabMeta, WorkflowView, WorkspaceType } from "./lib/types";
import { loadLayoutSize, saveLayoutSize } from "./lib/layoutPreferences";
import {
  applyTheme,
  clearLegacyThemePreference,
  getTheme,
  getThemeStyle,
  isThemeStyle,
  normalizeThemePreference,
  normalizeThemeStyleForTheme,
  readLegacyThemePreference,
  themeForStyle,
  type Theme,
} from "./lib/theme";
import { useWindowStatePersistence } from "./lib/windowState";

const SIDEBAR_COLLAPSED_KEY = "Rexion.sidebar.collapsed";
const SIDEBAR_DEFAULT_WIDTH = 264;
const SIDEBAR_DEFAULT_RATIO = 0.175;
const SIDEBAR_MIN_WIDTH = 228;
const SIDEBAR_MAX_WIDTH = 420;
const CHAT_MIN_WIDTH = 400;
const WORKSPACE_RESIZER_WIDTH = 8;

function isThemeMode(value: string): value is Theme {
  return value === "auto" || value === "light" || value === "dark";
}
const RIGHT_DOCK_MIN_WIDTH = 260;
const RIGHT_DOCK_DEFAULT_WIDTH = 380;
const RIGHT_DOCK_DEFAULT_RATIO = 0.25;
const RIGHT_DOCK_MAX_WIDTH = 860;

type RightDockMode = "preview" | "files" | "changed" | "context" | "calendar" | "dailyBrief" | "imSessions";
const SHOW_CONTEXT_DOCK = true;

type HistoryScopeFilter = { scope: "global" | "project"; workspaceRoot: string };
type DesktopPlatform = "darwin" | "windows" | "linux";
type HistoryViewState =
  | { kind: "history"; source: "scope"; filter: HistoryScopeFilter; sessions: SessionMeta[] }
  | { kind: "history"; source: "all"; sessions: SessionMeta[] }
  | { kind: "trash"; sessions: SessionMeta[] };

function clampSidebarWidth(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, Math.round(width)));
}

function clampRightDockWidth(width: number): number {
  return Math.min(RIGHT_DOCK_MAX_WIDTH, Math.max(RIGHT_DOCK_MIN_WIDTH, Math.round(width)));
}

function viewportWidthFallback(): number {
  if (typeof window === "undefined") return 0;
  const width = Math.round(window.innerWidth || 0);
  return Number.isFinite(width) && width > 0 ? width : 0;
}

function defaultSidebarWidth(): number {
  const width = viewportWidthFallback();
  if (width <= 0) return SIDEBAR_DEFAULT_WIDTH;
  return clampSidebarWidth(width * SIDEBAR_DEFAULT_RATIO);
}

function defaultRightDockWidth(): number {
  const width = viewportWidthFallback();
  if (width <= 0) return RIGHT_DOCK_DEFAULT_WIDTH;
  return clampRightDockWidth(width * RIGHT_DOCK_DEFAULT_RATIO);
}

function resolveRightDockWidth(mainWidth: number, desiredDockWidth: number, minWidth: number): number {
  const budget = Math.max(0, Math.round(mainWidth) - CHAT_MIN_WIDTH - WORKSPACE_RESIZER_WIDTH);
  if (budget < minWidth) return 0;
  const desired = Math.min(RIGHT_DOCK_MAX_WIDTH, Math.max(minWidth, Math.round(desiredDockWidth)));
  return Math.min(budget, desired);
}

function loadSidebarCollapsed(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "1";
  } catch {
    return false;
  }
}

function saveSidebarCollapsed(collapsed: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? "1" : "0");
  } catch {
    /* ignore storage failures */
  }
}

function loadSidebarWidth(): number {
  return loadLayoutSize("sidebarWidth", defaultSidebarWidth(), clampSidebarWidth);
}

function saveSidebarWidth(width: number): void {
  saveLayoutSize("sidebarWidth", width, clampSidebarWidth);
}

function normalizeDesktopPlatform(value: string): DesktopPlatform {
  if (value === "darwin" || value === "windows") return value;
  return "linux";
}

function browserPlatformOverride(): DesktopPlatform | null {
  if (typeof window === "undefined" || window.runtime) return null;
  const value = new URLSearchParams(window.location.search).get("platform");
  if (value === "darwin" || value === "windows" || value === "linux") return value;
  return null;
}

function detectBrowserPlatform(): DesktopPlatform {
  const override = browserPlatformOverride();
  if (override) return override;
  if (typeof navigator === "undefined") return "linux";
  const marker = `${navigator.platform} ${navigator.userAgent}`;
  if (/Win/i.test(marker)) return "windows";
  if (/Mac/i.test(marker)) return "darwin";
  return "linux";
}

function loadRightDockWidth(): number {
  // Migrate from legacy keys if the new key doesn't exist yet.
  const existing = loadLayoutSize("rightDockWidth", -1, clampRightDockWidth);
  if (existing >= 0) return existing;
  // Fall back to the wider of the two legacy widths.
  const legacyTree = loadLayoutSize("rightDockTreeWidth", -1, (v) => v);
  const legacyPreview = loadLayoutSize("rightDockPreviewWidth", -1, (v) => v);
  const legacy = Math.max(legacyTree, legacyPreview);
  if (legacy >= 0) return clampRightDockWidth(legacy);
  return defaultRightDockWidth();
}

function saveRightDockWidth(width: number): void {
  saveLayoutSize("rightDockWidth", width, clampRightDockWidth);
}

function tabWorkspaceTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  if (tab.scope === "project") return tab.workspaceName || tab.workspaceRoot || "Project";
  if (tab.scope === "global") return tab.workspaceName || "Global";
  return tab.workspaceName || tab.workspaceRoot || "Global";
}

function topicTitle(tab?: TabMeta): string {
  if (!tab) return "Global";
  const workspaceTitle = tabWorkspaceTitle(tab);
  const topic = tab.topicTitle || (tab.scope === "global" ? workspaceTitle : "Untitled");
  return topic === workspaceTitle ? workspaceTitle : `${workspaceTitle} / ${topic}`;
}

function topicScopeLabel(tab?: TabMeta): string {
  if (!tab) return t("scope.global");
  if (tab.scope === "global") return tab.workspaceName || t("scope.global");
  return t("scope.project", { name: tab.workspaceName || tab.workspaceRoot || "Project" });
}

function normalizeModeValue(mode?: string): Mode {
  return mode === "plan" || mode === "yolo" ? mode : "normal";
}

function sessionsForScope(sessions: SessionMeta[], filter: HistoryScopeFilter): SessionMeta[] {
  if (filter.scope === "project") {
    return sessions.filter((session) => session.scope === "project" && session.workspaceRoot === filter.workspaceRoot);
  }
  return sessions.filter((session) => (session.scope || "global") === "global");
}

function materializeLiveItems(items: Item[], live?: LiveStream): Item[] {
  if (!live) return items;
  return items.map((item) => {
    if (item.kind !== "assistant" || item.id !== live.id) return item;
    return { ...item, text: live.text, reasoning: live.reasoning, streaming: true };
  });
}

function fence(label: string, value: string): string {
  if (!value.trim()) return "";
  const fenceToken = value.includes("```") ? "````" : "```";
  return `${label}\n${fenceToken}\n${value.trim()}\n${fenceToken}`;
}

function sessionItemsToMarkdown(title: string, items: Item[], live?: LiveStream): string {
  const lines: string[] = [`# ${title.trim() || "Rexion session"}`, ""];
  for (const item of materializeLiveItems(items, live)) {
    switch (item.kind) {
      case "user":
        lines.push("## User", "", item.text.trim(), "");
        break;
      case "assistant":
        lines.push("## Assistant");
        if (item.reasoning.trim()) {
          lines.push("", "### Reasoning", "", item.reasoning.trim());
        }
        if (item.text.trim()) {
          lines.push("", item.text.trim());
        }
        lines.push("");
        break;
      case "tool":
        lines.push(`### Tool: ${item.name}`);
        if (item.args.trim()) lines.push("", fence("Args", item.args));
        if (item.output?.trim()) lines.push("", fence("Output", item.output));
        if (item.error?.trim()) lines.push("", fence("Error", item.error));
        lines.push("");
        break;
      case "phase":
        lines.push(`### Phase`, "", item.text.trim(), "");
        break;
      case "notice":
        lines.push(`### ${item.level === "warn" ? "Warning" : "Notice"}`, "", item.text.trim(), "");
        break;
      case "compaction":
        lines.push("### Context Compaction", "");
        if (item.pending) {
          lines.push("Compaction pending.");
        } else {
          lines.push(`Messages: ${item.messages}`);
          if (item.trigger) lines.push(`Trigger: ${item.trigger}`);
          if (item.summary.trim()) lines.push("", item.summary.trim());
        }
        lines.push("");
        break;
    }
  }
  return lines.join("\n").replace(/\n{3,}/g, "\n\n").trimEnd() + "\n";
}

function sessionItemsToJson(title: string, items: Item[], live?: LiveStream): string {
  return JSON.stringify(
    {
      title,
      exportedAt: new Date().toISOString(),
      items: materializeLiveItems(items, live),
    },
    null,
    2,
  );
}

function safeFilename(name: string): string {
  const cleaned = name.trim().replace(/[\\/:*?"<>|]+/g, "-").replace(/\s+/g, " ").slice(0, 80);
  return cleaned || "Rexion-session";
}

function downloadTextFile(filename: string, text: string, mime: string): void {
  const blob = new Blob([text], { type: `${mime};charset=utf-8` });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}


/** Global hotkey handler for shell-expand toggle (Ctrl/Cmd+B). */
function ShellHotkeys() {
  const shellExpand = useShellExpand();
  useEffect(() => {
    if (!shellExpand) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "b") {
        e.preventDefault();
        shellExpand.toggleLast();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [shellExpand]);
  return null;
}

export default function App() {
  const {
    state,
    activeTabId,
    send,
    runShell,
    notice,
    cancel,
    approve,
    answerQuestion,
    answerAutoLearn,
    setControllerMode,
    newSession,
    listSessions,
    listTrashedSessions,
    resumeSession,
    previewSession,
    deleteSession,
    restoreSession,
    purgeTrashedSession,
    renameSession,
    refreshMeta,
    pickWorkspace,
    switchWorkspace,
    rewind,
    setModel,
    setEffort,
    switchTab,
    openProjectTab,
    openGlobalTab,
    closeTab,
    reorderTabs,
    syncActiveTab,
    intentClassified,
    clearIntentClassified,
  } = useController();
  const { locale, setPref: setLocalePref } = useI18n();
  const t = useT();
  const [modesByTab, setModesByTab] = useState<Record<string, Mode>>({});
  const [wsTypeByTab, setWsTypeByTab] = useState<Record<string, WorkspaceType>>({});
  const [tabMetas, setTabMetas] = useState<TabMeta[]>([]);
  const [tabOrderIds, setTabOrderIds] = useState<string[]>([]);
  const [tabRevealSignal, setTabRevealSignal] = useState(0);
  const [startupSplashVisible, setStartupSplashVisible] = useState<boolean>(() => shouldShowStartupSplash());
  // null until the mount probe resolves; true shows the overlay. Probed once —
  // clearing the key mid-session is the Settings panel's job, not the gate's.
  const [needsOnboarding, setNeedsOnboarding] = useState<boolean | null>(null);
  const [onboardingTaskPending, setOnboardingTaskPendingState] = useState(() => isOnboardingTaskPending());
  const [showOnboardingCelebration, setShowOnboardingCelebration] = useState(false);
  const [settingsTarget, setSettingsTarget] = useState<SettingsTab | null>(null);
  const [repoWikiOpen, setRepoWikiOpen] = useState(false);
  const [templatesOpen, setTemplatesOpen] = useState(false);
  const [schedulerOpen, setSchedulerOpen] = useState(false);
  const [traceOpen, setTraceOpen] = useState(false);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(loadSidebarCollapsed);
  const [sidebarWidth, setSidebarWidth] = useState(loadSidebarWidth);
  const [sidebarResizing, setSidebarResizing] = useState(false);
  const [workspacePanelOpen, setWorkspacePanelOpen] = useState(true);
  const [rightDockWidth, setRightDockWidth] = useState(loadRightDockWidth);
  const [workspacePreviewActive, setWorkspacePreviewActive] = useState(false);
  const [workspacePanelResizing, setWorkspacePanelResizing] = useState(false);
  const [workspacePanelMaximized, setWorkspacePanelMaximized] = useState(false);
  const [rightDockMode, setRightDockMode] = useState<RightDockMode>("preview");
  const [terminalDockOpen, setTerminalDockOpen] = useState(false);
  const [terminalDockHeight, setTerminalDockHeight] = useState(() => {
    const saved = loadLayoutSize("terminalDockHeight", 220);
    return Math.min(Math.max(saved, 100), 600);
  });
  // Navigation state for sidebar-driven views (home, calendar, todos, etc.)
  const [navPage, setNavPage] = useState<string | null>(null);
  // Computer Use supervision overlay — tracks pending desktop actions so the
  // user can pause/abort in real time. Wired to tool events when the
  // rexion-plugin-computer plugin is active; inactive (hidden) by default.
  const [supervisionActive, setSupervisionActive] = useState(false);
  const [supervisionPaused, setSupervisionPaused] = useState(false);
  const [supervisionPending, setSupervisionPending] = useState<SupervisionAction | null>(null);
  const [supervisionHistory, setSupervisionHistory] = useState<SupervisionAction[]>([]);
  // Workflow editor is rendered as a modal overlay (not a main-pane page) so
  // it doesn't displace the active conversation while editing flows.
  const [workflowModalOpen, setWorkflowModalOpen] = useState(false);
  // Skills market, design-to-code, and cross-device sync also open as modal
  // overlays so the active conversation stays visible underneath.
  const [skillsMarketModalOpen, setSkillsMarketModalOpen] = useState(false);
  const [designPanelModalOpen, setDesignPanelModalOpen] = useState(false);
  const [sessionSyncModalOpen, setSessionSyncModalOpen] = useState(false);
  // Progress stepper steps are derived from step_progress events in the controller state.
  const progressSteps: ProgressStep[] = state.steps.map((s) => ({
    id: s.id,
    label: s.label,
    status: s.status,
    turnIndex: s.turnIndex,
  }));
  // Clipboard assistant floating window
  const [floatingVisible, setFloatingVisible] = useState(false);
  const [floatingResult, setFloatingResult] = useState<string | null>(null);
  const [floatingLoading, setFloatingLoading] = useState(false);
  const [previewFilePath, setPreviewFilePath] = useState<string | undefined>();
  const [previewDiffOriginal, setPreviewDiffOriginal] = useState<string | undefined>();
  const [previewDiffModified, setPreviewDiffModified] = useState<string | undefined>();
  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [projectRevision, setProjectRevision] = useState(0);
  const [composerInsertRequest, setComposerInsertRequest] = useState<ComposerInsertRequest | null>(null);
  const [saveRecipeOpen, setSaveRecipeOpen] = useState(false);
  const [saveRecipeTabId, setSaveRecipeTabId] = useState<string | undefined>();
  const [saveRecipeSkill, setSaveRecipeSkill] = useState("");
  const [saveRecipeParams, setSaveRecipeParams] = useState("");
  const [desktopPlatform, setDesktopPlatform] = useState<DesktopPlatform>(detectBrowserPlatform);
  const [renamingTopicId, setRenamingTopicId] = useState<string | null>(null);
  const [topicTitleDraft, setTopicTitleDraft] = useState("");
  const [topicExportOpen, setTopicExportOpen] = useState(false);
  // CommandPalette (⌘K/Ctrl+K) and NotificationCenter drawer state.
  const [commandPaletteOpen, setCommandPaletteOpen] = useState(false);
  const [notificationCenterOpen, setNotificationCenterOpen] = useState(false);
  // Palette data: cached skills & workflows, fetched when the palette opens.
  const [paletteSkills, setPaletteSkills] = useState<SkillView[]>([]);
  const [paletteWorkflows, setPaletteWorkflows] = useState<WorkflowView[]>([]);
  // Context panel memory facts, fetched alongside the dock refresh.
  const [contextMemoryFacts, setContextMemoryFacts] = useState<MemoryFactBrief[]>([]);
  const [sideChatOpen, setSideChatOpen] = useState(false);
  const topicRenameSkipCommitRef = useRef(false);
  const topicRenameCommitHandledRef = useRef(false);

  // Derive preview data from the latest completed tool events.
  // - Office mode: track the most recent doc-emitting tool's file path.
  // - Coding mode: track the most recent file-edit tool's diff.
  useEffect(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const it = state.items[i];
      if (it.kind !== "tool" || it.status !== "done" || it.error) continue;
      // Office: doc-emitting tools
      const docPath = docExportPath(it.name, it.args, it.output);
      if (docPath) {
        setPreviewFilePath(docPath);
        return;
      }
      // Coding: file-edit tools
      const diffs = diffsFor(it.name, it.args);
      if (diffs.length > 0) {
        setPreviewDiffOriginal(diffs[0].original);
        setPreviewDiffModified(diffs[0].modified);
        return;
      }
    }
  }, [state.items]);

  // Onboarding task completion: show celebration when first task completes.
  const prevRunningRef = useRef(state.running);
  useEffect(() => {
    const wasRunning = prevRunningRef.current;
    prevRunningRef.current = state.running;
    // Detect transition from running → not running with pending flag.
    if (wasRunning && !state.running && onboardingTaskPending && state.items.length > 0) {
      setShowOnboardingCelebration(true);
      clearOnboardingTaskPending();
      setOnboardingTaskPendingState(false);
    }
  }, [state.running, onboardingTaskPending, state.items.length]);

  // Persist window geometry across launches.
  useWindowStatePersistence();

  useEffect(() => {
    let cancelled = false;
    const override = browserPlatformOverride();
    if (override) {
      setDesktopPlatform(override);
      return () => {
        cancelled = true;
      };
    }
    void app.Platform()
      .then((value) => {
        if (!cancelled) setDesktopPlatform(normalizeDesktopPlatform(value));
      })
      .catch((e) => {
        console.warn("platform probe failed", e);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    const syncDesktopPreferences = async () => {
      const legacyLanguage = readLegacyLangPref();
      const legacyTheme = readLegacyThemePreference();
      if (legacyLanguage || legacyTheme.hasValue) {
        await app.MigrateDesktopPreferences(legacyLanguage, legacyTheme.theme, legacyTheme.style);
        clearLegacyLangPref();
        clearLegacyThemePreference();
      }
      const settings = await app.Settings();
      if (cancelled) return;
      const nextTheme = normalizeThemePreference(settings.desktopTheme);
      const nextStyle = normalizeThemeStyleForTheme(settings.desktopThemeStyle, nextTheme);
      applyTheme(nextTheme, nextStyle, { persist: false });
      setLocalePref(normalizeLangPref(settings.desktopLanguage));
    };
    void syncDesktopPreferences().catch((e) => {
      console.warn("desktop preferences sync failed", e);
    });
    return () => {
      cancelled = true;
    };
  }, [setLocalePref]);

  // Open settings when the native menu item (CmdOrCtrl+,) is activated.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("app:open-settings", () => {
      setSettingsTarget("general");
    });
  }, []);

  // Listen for clipboard hotkey (Ctrl+Shift+R) from the Go backend.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("clipboard-hotkey", () => {
      setFloatingVisible(true);
      setFloatingResult(null);
    });
  }, []);
  // Listen for scheduled task start event: bring window to front.
  // Tab creation is handled by the backend (executeScheduledTask → OpenTabForScheduledTask),
  // so the frontend only needs to ensure the window is visible.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("scheduled_task_started", () => {
      // The backend already created the tab; just ensure the window is shown.
      // wruntime.WindowShow is called by onScheduledTaskStart on the Go side.
    });
  }, []);
  const [pendingPlanRevision, setPendingPlanRevision] = useState<string | null>(null);
  const [footerHeight, setFooterHeight] = useState(0);
  const layoutRef = useRef<HTMLDivElement>(null);
  const footerRef = useRef<HTMLElement>(null);
  const [layoutWidth, setLayoutWidth] = useState(0);
  const preferredWorkspacePanelWidth = rightDockWidth;
  const sidebarRenderWidth = sidebarCollapsed ? 0 : sidebarWidth;
  const measuredMainWidth = layoutWidth > 0 ? Math.max(0, layoutWidth - sidebarRenderWidth) : CHAT_MIN_WIDTH + WORKSPACE_RESIZER_WIDTH + preferredWorkspacePanelWidth;
  const workspacePanelMinWidth = RIGHT_DOCK_MIN_WIDTH;

  const budget = Math.max(0, measuredMainWidth - CHAT_MIN_WIDTH - WORKSPACE_RESIZER_WIDTH);
  const workspacePanelFloating = workspacePanelOpen && !workspacePanelMaximized && budget < workspacePanelMinWidth;

  const resolvedWorkspacePanelWidth = workspacePanelOpen && !workspacePanelMaximized
    ? (workspacePanelFloating ? Math.min(measuredMainWidth, Math.max(workspacePanelMinWidth, preferredWorkspacePanelWidth)) : resolveRightDockWidth(measuredMainWidth, preferredWorkspacePanelWidth, workspacePanelMinWidth))
    : preferredWorkspacePanelWidth;

  const workspacePanelRenderable = workspacePanelOpen && (workspacePanelMaximized || resolvedWorkspacePanelWidth > 0);
  const workspacePanelGridOpen = workspacePanelRenderable && !workspacePanelMaximized && !workspacePanelFloating;
  const workspacePanelRenderWidth = workspacePanelMaximized ? preferredWorkspacePanelWidth : resolvedWorkspacePanelWidth;
  const activeTab = useMemo(
    () => tabMetas.find((tab) => tab.id === activeTabId) ?? tabMetas.find((tab) => tab.active),
    [activeTabId, tabMetas],
  );
  const startupSplashHold = state.meta?.ready !== true && !state.meta?.startupErr;
  const mode = activeTabId ? modesByTab[activeTabId] ?? "normal" : "normal";
  const workspaceType = activeTabId ? wsTypeByTab[activeTabId] ?? "coding" : "coding";
  const setMode = useCallback(
    (next: Mode | ((prev: Mode) => Mode)) => {
      if (!activeTabId) return;
      setModesByTab((current) => {
        const prev = current[activeTabId] ?? "normal";
        const value = typeof next === "function" ? next(prev) : next;
        if (value === prev) return current;
        return { ...current, [activeTabId]: value };
      });
    },
    [activeTabId],
  );
  const topicbarEditing = Boolean(activeTab?.topicId && activeTab.topicId === renamingTopicId);
  const topicbarProjectPrefix = activeTab ? tabWorkspaceTitle(activeTab) : "";
  const visibleTabId = activeTabId;
  const visibleTabs = useMemo(() => {
    const byId = new Map(tabMetas.map((tab) => [tab.id, tab]));
    const ordered = tabOrderIds.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
    const missing = tabMetas.filter((tab) => !tabOrderIds.includes(tab.id));
    return [...ordered, ...missing].map((tab) => ({
      ...tab,
      mode: modesByTab[tab.id] ?? normalizeModeValue(tab.mode),
      active: tab.id === visibleTabId,
    }));
  }, [modesByTab, tabMetas, tabOrderIds, visibleTabId]);

  useEffect(() => {
    const ids = tabMetas.map((tab) => tab.id);
    setTabOrderIds((current) => {
      const next = current.filter((id) => ids.includes(id));
      for (const id of ids) {
        if (!next.includes(id)) next.push(id);
      }
      return next.join("\u0000") === current.join("\u0000") ? current : next;
    });
  }, [tabMetas]);

  useEffect(() => {
    const ids = new Set(tabMetas.map((tab) => tab.id));
    setModesByTab((current) => {
      let changed = false;
      const next: Record<string, Mode> = {};
      for (const tab of tabMetas) {
        const mode = normalizeModeValue(tab.mode);
        next[tab.id] = mode;
        if (current[tab.id] !== mode) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
  }, [tabMetas]);

  // Sync workspaceType from tabMetas → wsTypeByTab (mirrors modesByTab sync).
  useEffect(() => {
    const ids = new Set(tabMetas.map((tab) => tab.id));
    setWsTypeByTab((current) => {
      let changed = false;
      const next: Record<string, WorkspaceType> = {};
      for (const tab of tabMetas) {
        const wt: WorkspaceType = tab.workspaceType === "office" ? "office" : tab.workspaceType === "assistant" ? "assistant" : "coding";
        next[tab.id] = wt;
        if (current[tab.id] !== wt) changed = true;
      }
      for (const id of Object.keys(current)) {
        if (!ids.has(id)) changed = true;
      }
      return changed ? next : current;
    });
  }, [tabMetas]);

  const applyWorkspaceType = useCallback(
    (wt: WorkspaceType) => {
      if (!activeTabId) return;
      setWsTypeByTab((current) => ({ ...current, [activeTabId]: wt }));
      void app.SetWorkspaceType(wt);
      // Always show project workspace (files) regardless of mode.
      setRightDockMode("files");
    },
    [activeTabId],
  );

  const [autoSwitchMode, setAutoSwitchMode] = useState(() => {
    try { return window.localStorage.getItem("Rexion.autoSwitchMode") === "true"; } catch { return false; }
  });

  // Persist autoSwitchMode changes to localStorage.
  useEffect(() => {
    try { window.localStorage.setItem("Rexion.autoSwitchMode", autoSwitchMode ? "true" : "false"); } catch { /* ignore */ }
  }, [autoSwitchMode]);

  // Mode switching UI has been removed: simply consume any intent signal so it
  // doesn't accumulate in controller state. The workspaceType stays at its
  // default and is no longer user-toggleable from the chrome.
  useEffect(() => {
    if (intentClassified) clearIntentClassified();
  }, [intentClassified, clearIntentClassified]);

  // Theme is no longer auto-switched when workspaceType changes.
  // The user's theme and color preferences are preserved across mode switches.

  useEffect(() => {
    if (!renamingTopicId || activeTab?.topicId === renamingTopicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, [activeTab?.topicId, renamingTopicId]);

  const syncModeToController = useCallback((m: Mode) => setControllerMode(m), [setControllerMode]);

  useEffect(() => {
    void app.SetTrayLocale(locale).catch(() => {});
  }, [locale]);

  // applyMode is the single source of truth for the input mode: it updates the
  // local pill and pushes the matching gate state to the controller (plan = read
  // only; yolo = auto-approve every tool call). normal clears both.
  const applyMode = useCallback(
    (m: Mode) => {
      setMode(m);
      void syncModeToController(m);
    },
    [setMode, syncModeToController],
  );
  // Shift+Tab cycles auto(normal) → plan → yolo → auto.
  const cycleMode = useCallback(() => {
    applyMode(mode === "normal" ? "plan" : mode === "plan" ? "yolo" : "normal");
  }, [mode, applyMode]);

  // Switching models rebuilds the controller, which starts in normal mode — so
  // re-apply the current mode, or the pill would say plan/YOLO while the fresh
  // controller silently uses normal gating.
  const switchModel = useCallback(
    async (name: string) => {
      await setModel(name);
      await syncModeToController(mode);
    },
    [setModel, mode, syncModeToController],
  );

  // Startup and workspace/model rebuilds create a fresh controller in normal
  // mode. Re-apply the UI mode once the controller is ready, including the case
  // where the user picked YOLO while boot was still loading and SetBypass was a
  // harmless no-op.
  useEffect(() => {
    if (state.meta?.ready !== true || mode === "normal") return;
    void syncModeToController(mode);
  }, [state.meta, mode, syncModeToController]);

  // The live task list pinned above the composer comes from the most recent
  // successful top-level todo_write result; failed or still-running attempts do
  // not advance the canonical panel state. It stays visible through the final
  // all-completed update, and can be dismissed by the user (the ✕). A dismissal
  // is keyed to that list's id, so a fresh accepted todo_write brings the panel
  // back.
  const todoEntry = useMemo(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const it = state.items[i];
      if (it.kind === "tool" && it.name === "todo_write" && !it.parentId && it.status === "done" && !it.error) {
        return { item: it, index: i };
      }
    }
    return null;
  }, [state.items]);
  const todoItem = todoEntry?.item ?? null;
  const todos = useMemo(() => (todoItem ? parseTodos(todoItem.args) : []), [todoItem]);
  const [dismissedTodo, setDismissedTodo] = useState<string | null>(null);
  const showTodos = shouldShowTodoPanel(todoItem?.id, dismissedTodo, todos);
  const [todoNow, setTodoNow] = useState(() => Date.now());
  const todoSeenRef = useRef<{ id: string; at: number } | null>(null);

  useEffect(() => {
    if (!todoItem) {
      todoSeenRef.current = null;
      return;
    }
    if (todoSeenRef.current?.id !== todoItem.id) {
      todoSeenRef.current = { id: todoItem.id, at: Date.now() };
      setTodoNow(Date.now());
    }
  }, [todoItem]);

  useEffect(() => {
    if (!showTodos) return;
    const id = window.setInterval(() => setTodoNow(Date.now()), 15000);
    return () => window.clearInterval(id);
  }, [showTodos]);

  const todoStale = useMemo(() => {
    if (!showTodos || !todoEntry) return false;
    const after = state.items.slice(todoEntry.index + 1);
    const completedToolsAfter = after.filter(
      (it) => it.kind === "tool" && it.name !== "todo_write" && !it.parentId && (it.status === "done" || it.status === "error"),
    ).length;
    const finalAssistantAfter = after.some((it) => it.kind === "assistant" && !it.streaming && it.text.trim() !== "");
    const readinessNoticeAfter = after.some(
      (it) => it.kind === "notice" && /final-answer readiness|todo_write|complete_step/i.test(it.text),
    );
    const staleByTime = state.running && todoSeenRef.current?.id === todoEntry.item.id && todoNow - todoSeenRef.current.at > 90_000;
    return completedToolsAfter >= 2 || finalAssistantAfter || readinessNoticeAfter || staleByTime;
  }, [showTodos, state.items, state.running, todoEntry, todoNow]);

  // useDeferredValue lets React prioritise Composer input (high-priority) over
  // Transcript re-renders (low-priority) during streaming. When a keystroke
  // and a transcript update collide, the keystroke is processed immediately
  // and the transcript re-render is deferred to idle time.
  const deferredItems = useDeferredValue(state.items);
  // Transcript imperative handle — ProgressStepper uses scrollToTurn to jump
  // to the user message of a given turn and flash it once for confirmation.
  const transcriptRef = useRef<TranscriptHandle>(null);
  const handleStepClick = useCallback((turnIndex: number) => {
    transcriptRef.current?.scrollToTurn(turnIndex);
  }, []);
  const handleToolPreview = useCallback((path: string, _kind: string) => {
    setPreviewFilePath(path);
    setPreviewDiffOriginal(undefined);
    setPreviewDiffModified(undefined);
  }, []);

  // Trace-side actions — invoked by AgentCanvas via NodeDetail.
  //   handleRetryTool: re-send the failed tool call as a slash-shaped prompt so
  //     the agent re-runs it (we can't directly re-invoke a tool from the UI
  //     without a controller API; instead we ask the agent to retry). This is
  //     a pragmatic UX affordance until a real ToolRetry bound method lands.
  //   handleCloseAgent: terminate a spawned child agent via close_agent tool —
  //     but we have no direct tool-invoke API either, so we send a text
  //     instruction. The agent will run close_agent with the right id.
  const handleRetryTool = useCallback((_toolId: string, toolName: string, args: string) => {
    const argPreview = args.length > 200 ? args.slice(0, 200) + "…" : args;
    send(`Please retry the failed "${toolName}" tool call. Previous args:\n${argPreview}`);
  }, [send]);
  const handleCloseAgent = useCallback((agentId: string) => {
    send(`Use the close_agent tool to terminate child agent ${agentId}.`);
  }, [send]);

  const handleSaveRecipeFromTab = useCallback((_tabId: string) => {
    // Extract skill from current tab's state (active tab only for now).
    let skill = "";
    let params = "";
    for (const item of state.items) {
      if (item.kind === "user" && item.text.startsWith("/")) {
        const parts = item.text.split(/\s+/);
        skill = parts[0].slice(1); // strip leading /
        params = parts.slice(1).join(" ");
        break;
      }
    }
    setSaveRecipeSkill(skill);
    setSaveRecipeParams(params);
    setSaveRecipeTabId(_tabId);
    setSaveRecipeOpen(true);
  }, [state.items]);
  const sessionTitle = topicTitle(activeTab);
  const sessionHasContent = state.items.length > 0 || Boolean(state.live?.text || state.live?.reasoning);
  const getSessionMarkdown = useCallback(
    () => sessionItemsToMarkdown(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );
  const getSessionJson = useCallback(
    () => sessionItemsToJson(sessionTitle, state.items, state.live),
    [sessionTitle, state.items, state.live],
  );

  useEffect(() => {
    if (!topicExportOpen) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (!target?.closest(".topicbar__export")) setTopicExportOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [topicExportOpen]);

  const exportSession = useCallback(
    (format: "markdown" | "json") => {
      const base = safeFilename(sessionTitle);
      if (format === "json") downloadTextFile(`${base}.json`, getSessionJson(), "application/json");
      else downloadTextFile(`${base}.md`, getSessionMarkdown(), "text/markdown");
      setTopicExportOpen(false);
    },
    [getSessionJson, getSessionMarkdown, sessionTitle],
  );

  useEffect(() => {
    if (!pendingPlanRevision || state.running) return;
    const text = pendingPlanRevision;
    setPendingPlanRevision(null);
    send(text);
  }, [pendingPlanRevision, send, state.running]);

  // handleSend intercepts the slash commands that need a desktop-native action
  // before they reach the backend: "/model <ref>" rebuilds on that model, and
  // "/memory" opens the Memory tab in the settings centre. Everything else — skills (/init, …),
  // custom commands, bare /model and the other read-only management verbs
  // (/skill, /hooks, /mcp) — goes straight to Submit, which the controller
  // resolves (a turn, or a listing Notice).
  const handleSend = useCallback(
    async (displayText: string, submitText = displayText) => {
      const trimmed = displayText.trim();
      // "!<cmd>" runs a shell command directly, bypassing the model.
      if (trimmed.startsWith("!")) {
        const cmd = trimmed.slice(1).trim();
        if (!cmd) {
          notice("usage: !<command>  (e.g. !ls -la)");
          return;
        }
        runShell(cmd);
        return;
      }
      const model = /^\/model\s+(\S+)$/.exec(trimmed);
      if (model) {
        void switchModel(model[1]);
        return;
      }
      if (trimmed === "/memory") {
        setSettingsTarget("memory");
        return;
      }
      const theme = /^\/theme(?:\s+(\S+))?$/.exec(trimmed);
      if (theme) {
        const arg = theme[1]?.toLowerCase();
        if (!arg) {
          const cur = getTheme();
          notice(t("settings.themeCurrent", { theme: cur, style: getThemeStyle(cur) }));
          return;
        }
        if (isThemeMode(arg)) {
          const next = arg;
          const style = getThemeStyle(next);
          await app.SetDesktopAppearance(next, style);
          applyTheme(next, style);
          notice(t("settings.themeChanged", { theme: next, style }));
          return;
        }
        if (isThemeStyle(arg)) {
          const next = themeForStyle(arg);
          await app.SetDesktopAppearance(next, arg);
          applyTheme(next, arg);
          notice(t("settings.themeChanged", { theme: next, style: arg }));
          return;
        }
        notice(t("settings.themeUnknown", { name: arg }), "warn");
        return;
      }
      await syncModeToController(mode);
      send(trimmed, submitText.trim());
    },
    [switchModel, syncModeToController, mode, send, runShell, notice, t],
  );

  const refreshTabMetas = useCallback(async (): Promise<TabMeta[]> => {
    const tabs = asArray(await app.ListTabs().catch(() => [] as TabMeta[]));
    setTabMetas(tabs);
    return tabs;
  }, []);

  useEffect(() => {
    void refreshTabMetas();
    // Event-driven refresh: listen for backend "tabs:changed" events instead
    // of 2s polling. A 30s heartbeat is kept as a safety net.
    const off = onTabsChanged(() => void refreshTabMetas());
    const id = window.setInterval(() => void refreshTabMetas(), 30000);
    return () => {
      off();
      window.clearInterval(id);
    };
  }, [refreshTabMetas]);

  useEffect(() => {
    return onProjectTreeChanged(() => {
      setProjectRevision((value) => value + 1);
      void refreshTabMetas();
    });
  }, [refreshTabMetas]);

  // ⌘K (macOS) / Ctrl+K (others) opens the CommandPalette. The listener is
  // attached at document level so the palette is reachable from any focus
  // state. We deliberately skip the event when the target is an IME
  // composition or a modal input to avoid hijacking normal typing.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      const meta = e.metaKey || e.ctrlKey;
      if (!meta || e.key !== "k" || e.altKey) return;
      const target = e.target as HTMLElement | null;
      if (target && target.isContentEditable) return;
      e.preventDefault();
      setCommandPaletteOpen((v) => !v);
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);



  // Prefetch skills & workflows when the palette opens so the items are
  // available by the time the user starts typing.
  useEffect(() => {
    if (!commandPaletteOpen) return;
    let cancelled = false;
    void (async () => {
      try {
        const [caps, wfs] = await Promise.all([
          app.Capabilities().catch(() => ({ skills: [] as SkillView[] })),
          app.ListWorkflows().catch(() => [] as WorkflowView[]),
        ]);
        if (!cancelled) {
          setPaletteSkills(caps.skills ?? []);
          setPaletteWorkflows(wfs ?? []);
        }
      } catch { /* ignore */ }
    })();
    return () => { cancelled = true; };
  }, [commandPaletteOpen]);

  // Refresh memory facts when the context dock is shown or dockRefreshKey changes.
  useEffect(() => {
    if (rightDockMode !== "context") return;
    let cancelled = false;
    void (async () => {
      try {
        const mem = await app.Memory().catch(() => null);
        if (!cancelled && mem) {
          setContextMemoryFacts((mem.facts ?? []).map((f) => ({
            name: f.name, title: f.title ?? "", description: f.description, type: f.type,
          })));
        }
      } catch { /* ignore */ }
    })();
    return () => { cancelled = true; };
  }, [rightDockMode, dockRefreshKey]);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const needs = await app.NeedsOnboarding();
        if (!cancelled) setNeedsOnboarding(needs);
      } catch {
        // Bridge unavailable (browser dev seam) — skip the gate; a real key
        // failure still surfaces via the topbar startupError banner.
        if (!cancelled) setNeedsOnboarding(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const el = footerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const update = () => setFooterHeight(Math.round(el.getBoundingClientRect().height));
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const el = layoutRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const update = () => {
      const width = el.getBoundingClientRect().width;
      if (width && Number.isFinite(width)) setLayoutWidth(Math.round(width));
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const startNewSession = useCallback(async () => {
    await newSession();
  }, [newSession]);

  const toggleSidebar = useCallback(() => {
    setSidebarCollapsed((collapsed) => {
      const next = !collapsed;
      saveSidebarCollapsed(next);
      return next;
    });
  }, []);

  const setExpandedSidebarWidth = useCallback((width: number) => {
    const next = clampSidebarWidth(width);
    setSidebarWidth(next);
    saveSidebarWidth(next);
  }, []);

  const startSidebarResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      event.preventDefault();
      setSidebarResizing(true);
      let nextWidth = sidebarWidth;
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = clampSidebarWidth(moveEvent.clientX);
        setSidebarWidth(nextWidth);
      };
      const onDone = () => {
        setSidebarWidth(nextWidth);
        saveSidebarWidth(nextWidth);
        setSidebarResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [sidebarCollapsed, sidebarWidth],
  );

  const resizeSidebarWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (sidebarCollapsed) return;
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setExpandedSidebarWidth(sidebarWidth + (event.key === "ArrowRight" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setExpandedSidebarWidth(SIDEBAR_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setExpandedSidebarWidth(SIDEBAR_MAX_WIDTH);
      }
    },
    [setExpandedSidebarWidth, sidebarCollapsed, sidebarWidth],
  );

  const setSavedWorkspacePanelWidth = useCallback(
    (width: number) => {
      const next = clampRightDockWidth(width);
      setRightDockWidth(next);
      saveRightDockWidth(next);
    },
    [],
  );

  const ensureWorkspacePanelWidth = useCallback(
    (width: number) => {
      const next = clampRightDockWidth(width);
      setRightDockWidth(next);
      saveRightDockWidth(next);
    },
    [],
  );

  const startWorkspacePanelResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!workspacePanelOpen) return;
      event.preventDefault();
      setWorkspacePanelResizing(true);
      const startX = event.clientX;
      const startDockWidth = preferredWorkspacePanelWidth;
      let nextDockWidth = startDockWidth;
      const onMove = (moveEvent: PointerEvent) => {
        const delta = moveEvent.clientX - startX;
        nextDockWidth = startDockWidth - delta;
        setRightDockWidth(clampRightDockWidth(nextDockWidth));
      };
      const onDone = () => {
        setSavedWorkspacePanelWidth(nextDockWidth);
        setWorkspacePanelResizing(false);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [preferredWorkspacePanelWidth, rightDockMode, setSavedWorkspacePanelWidth, workspacePanelOpen, workspacePreviewActive],
  );

  const resizeWorkspacePanelWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(preferredWorkspacePanelWidth + (event.key === "ArrowLeft" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(RIGHT_DOCK_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedWorkspacePanelWidth(RIGHT_DOCK_MAX_WIDTH);
      }
    },
    [preferredWorkspacePanelWidth, setSavedWorkspacePanelWidth],
  );

  const startTerminalDockResize = useCallback(
    (event: ReactPointerEvent<HTMLDivElement>) => {
      if (!terminalDockOpen) return;
      event.preventDefault();
      const startY = event.clientY;
      const startHeight = terminalDockHeight;
      let nextHeight = startHeight;
      const onMove = (moveEvent: PointerEvent) => {
        const delta = startY - moveEvent.clientY;
        nextHeight = Math.min(Math.max(startHeight + delta, 100), 600);
        setTerminalDockHeight(nextHeight);
      };
      const onDone = () => {
        saveLayoutSize("terminalDockHeight", nextHeight, (v) => Math.min(Math.max(v, 100), 600));
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };
      document.body.style.cursor = "row-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [terminalDockHeight, terminalDockOpen],
  );

  const openWorkspacePanel = useCallback(
    (mode: RightDockMode = rightDockMode) => {
      setRightDockMode(mode);
      let nextMaximized = workspacePanelMaximized;
      if (mode === "context") {
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      } else {
        // When user explicitly opens the panel, we do NOT force maximize.
        // If there's not enough room, the panel will open in floating mode
        // over the chat area, preserving the chat area's minimum width
        // and keeping the panel's close button accessible.
        nextMaximized = false;
        setWorkspacePanelMaximized(false);
      }
      if (workspacePanelOpen && workspacePanelMaximized === nextMaximized) {
        return;
      }
      setWorkspacePanelOpen(true);
    },
    [rightDockMode, workspacePanelMaximized, workspacePanelOpen],
  );

  const closeWorkspacePanel = useCallback(() => {
    if (!workspacePanelOpen) {
      return;
    }
    setWorkspacePanelMaximized(false);
    setWorkspacePanelOpen(false);
  }, [workspacePanelOpen]);

  // Global keyboard shortcuts for common operations.
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      const meta = e.metaKey || e.ctrlKey;
      if (!meta) return;
      const target = e.target as HTMLElement | null;
      const tag = target?.tagName?.toLowerCase();
      const inInput = tag === "input" || tag === "textarea" || (target?.isContentEditable ?? false);

      // Ctrl/Cmd+N: New session
      if (!e.shiftKey && !e.altKey && e.key === "n") {
        if (inInput) return;
        e.preventDefault();
        cancel();
        void newSession();
        return;
      }

      // Ctrl/Cmd+L: Focus composer input
      if (!e.shiftKey && !e.altKey && e.key === "l") {
        e.preventDefault();
        const textarea = document.querySelector<HTMLTextAreaElement>(".composer__input");
        if (textarea) textarea.focus();
        return;
      }

      // Ctrl/Cmd+.: Toggle sidebar
      if (!e.shiftKey && !e.altKey && e.key === ".") {
        e.preventDefault();
        setSidebarCollapsed((v) => {
          const next = !v;
          saveSidebarCollapsed(next);
          return next;
        });
        return;
      }

      // Ctrl/Cmd+Shift+L: Toggle right dock visibility
      if (e.shiftKey && !e.altKey && e.key === "L") {
        e.preventDefault();
        if (workspacePanelOpen) {
          closeWorkspacePanel();
        } else {
          openWorkspacePanel("preview");
        }
        return;
      }

      // Ctrl/Cmd+W: Close current tab (only if multiple tabs exist)
      if (!e.shiftKey && !e.altKey && e.key === "w") {
        if (inInput) return;
        if (tabMetas.length <= 1) return;
        e.preventDefault();
        if (activeTabId) void closeTab(activeTabId);
        return;
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [cancel, newSession, setSidebarCollapsed, workspacePanelOpen, closeWorkspacePanel, openWorkspacePanel, tabMetas.length, activeTabId, closeTab]);

  const openRightDockMode = useCallback(
    (mode: RightDockMode) => {
      openWorkspacePanel(mode);
    },
    [openWorkspacePanel],
  );

  // Auto-open workspace panel when preview file path is set.
  useEffect(() => {
    if (previewFilePath) {
      openWorkspacePanel("preview");
    }
  }, [previewFilePath, openWorkspacePanel]);

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${sidebarWidth}px`,
        "--chat-min-width": `${CHAT_MIN_WIDTH}px`,
        "--workspace-width": `${workspacePanelRenderWidth}px`,
        "--workspace-resizer-width": `${WORKSPACE_RESIZER_WIDTH}px`,
      }) as CSSProperties,
    [sidebarWidth, workspacePanelRenderWidth],
  );

  const setWorkspacePanel = useCallback((open: boolean) => {
    if (open) {
      openWorkspacePanel();
    } else {
      closeWorkspacePanel();
    }
  }, [closeWorkspacePanel, openWorkspacePanel]);

  const addWorkspaceTextToComposer = useCallback((text: string) => {
    setComposerInsertRequest({ id: Date.now(), text });
  }, []);

  const handleTabChange = useCallback(async (id: string) => {
    setNavPage(null); // exit home/settings nav when switching tabs
    await switchTab(id);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [refreshTabMetas, switchTab]);

  const handleNewTab = useCallback(async () => {
    setNavPage(null); // exit home/settings nav when creating a new tab
    const activeWorkspaceRoot = activeTab?.workspaceRoot || state.meta?.cwd || "";
    const targetScope = activeTab?.scope === "global" || !activeWorkspaceRoot ? "global" : "project";
    const workspaceRoot = targetScope === "project" ? activeWorkspaceRoot : "";
    const topic = await app.CreateTopic(targetScope, workspaceRoot, "");
    if (targetScope === "global" || !workspaceRoot) {
      await openGlobalTab(topic.id);
    } else {
      await openProjectTab(workspaceRoot, topic.id);
    }
    setProjectRevision((value) => value + 1);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [activeTab?.scope, activeTab?.workspaceRoot, openGlobalTab, openProjectTab, refreshTabMetas, state.meta?.cwd]);

  const handleTabClose = useCallback(async (id: string) => {
    setModesByTab((current) => {
      if (!(id in current)) return current;
      const next = { ...current };
      delete next[id];
      return next;
    });
    setTabMetas((current) => {
      // When closing the last tab, we need to create a new one first so that
      // the backend CloseTab does not reject ("cannot close the last tab").
      // The new tab is created *after* this optimistic update, but the key
      // insight is: we still remove the closed tab from the local array and
      // temporarily show an empty state; the new tab will be added by
      // refreshTabMetas once it is created below.
      if (current.length <= 1) {
        // Create a new tab first, then close the old one.
        // We return the current array unchanged for now; the new tab will
        // appear after handleNewTab + closeTab + refreshTabMetas completes.
        // Kick off the async sequence outside the setter.
        (async () => {
          await handleNewTab();
          await closeTab(id);
          await refreshTabMetas();
          setTabRevealSignal((signal) => signal + 1);
        })();
        return current;
      }
      const closingIndex = current.findIndex((tab) => tab.id === id);
      if (closingIndex < 0) return current;
      const closingTab = current[closingIndex];
      const remaining = current.filter((tab) => tab.id !== id);
      if (!closingTab.active && closingTab.id !== activeTabId) return remaining;
      const nextIndex = Math.min(closingIndex, remaining.length - 1);
      const nextActiveId = remaining[nextIndex]?.id;
      return remaining.map((tab) => ({ ...tab, active: tab.id === nextActiveId }));
    });
    // Only call closeTab directly if there was more than one tab;
    // the single-tab case is handled inside the setter above.
    if (tabMetas.length > 1) {
      await closeTab(id);
      await refreshTabMetas();
      setTabRevealSignal((signal) => signal + 1);
    }
  }, [activeTabId, closeTab, handleNewTab, refreshTabMetas, tabMetas.length]);

  const handleTabsClose = useCallback(async (ids: string[], nextActiveTabId?: string) => {
    const currentIds = tabMetas.map((tab) => tab.id);
    const targets = ids.filter((id, index) => currentIds.includes(id) && ids.indexOf(id) === index);
    if (targets.length === 0) return;
    for (const id of targets) {
      await closeTab(id);
    }
    if (nextActiveTabId && currentIds.includes(nextActiveTabId)) {
      await switchTab(nextActiveTabId);
    }
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [closeTab, refreshTabMetas, switchTab, tabMetas]);

  const handleTabsReorder = useCallback(async (ids: string[]) => {
    setTabOrderIds(ids);
    setTabMetas((current) => {
      const byId = new Map(current.map((tab) => [tab.id, tab]));
      const ordered = ids.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      return ordered.length === current.length ? ordered : current;
    });
    await reorderTabs(ids);
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [refreshTabMetas, reorderTabs]);

  const handleMessageAction = useCallback(async (turn: number, scope: string) => {
    await rewind(turn, scope);
    if (scope === "fork") {
      await refreshTabMetas();
      setProjectRevision((value) => value + 1);
      setTabRevealSignal((signal) => signal + 1);
      return;
    }
    if (scope === "code" || scope === "both") {
      setDockRefreshKey((value) => value + 1);
      setProjectRevision((value) => value + 1);
    }
  }, [refreshTabMetas, rewind]);

  const handleOpenTopic = useCallback(async (scope: string, workspaceRoot: string, topicId: string) => {
    setNavPage(null); // exit home/settings nav when opening a session
    if (scope === "global") {
      await openGlobalTab(topicId);
    } else {
      await openProjectTab(workspaceRoot, topicId);
    }
    await refreshTabMetas();
    setTabRevealSignal((signal) => signal + 1);
  }, [openGlobalTab, openProjectTab, refreshTabMetas]);

  // History drawer: project menus can open a scoped saved-session list. Idle row
  // clicks resume; running row clicks only preview through PreviewSession.
  const openProjectHistory = useCallback(async (scope: "global" | "project", workspaceRoot: string) => {
    const filter = { scope, workspaceRoot };
    setHistView({ kind: "history", source: "scope", filter, sessions: sessionsForScope(await listSessions(), filter) });
  }, [listSessions]);
  const openAllHistory = useCallback(async () => {
    setHistView({ kind: "history", source: "all", sessions: await listSessions() });
  }, [listSessions]);
  const openTrash = useCallback(async () => {
    setHistView({ kind: "trash", sessions: await listTrashedSessions() });
  }, [listTrashedSessions]);

  // Build the CommandPalette items: sessions (tabs) first, then navigation
  // commands, then quick actions. The list is rebuilt whenever the inputs
  // change; the palette itself re-runs fuzzy match on every keystroke.
  const commandPaletteItems = useMemo<PaletteItem[]>(() => {
    const items: PaletteItem[] = [];
    // Sessions group — switch to an existing tab.
    for (const tab of tabMetas) {
      const id = tab.id;
      const title = tab.topicTitle || tab.topicId || id;
      const hint = tab.workspaceRoot || tab.scope;
      items.push({
        id: `tab:${id}`,
        title,
        hint,
        group: t("palette.sessions"),
        keywords: [tab.scope, tab.workspaceType],
        run: () => void handleTabChange(id),
      });
    }
    // Navigation group — jump to a sidebar-driven view.
    const nav: Array<[string, string]> = [
      ["home", t("sidebar.home")],
      ["calendar", t("sidebar.assistantSchedule")],
      ["dailyBrief", t("sidebar.dailyBrief")],
      ["scheduled", t("sidebar.scheduledTasks")],
      ["terminal", t("sidebar.terminal")],
      ["trace", t("sidebar.trace")],
      ["workflow", t("sidebar.workflow")],
    ];
    for (const [page, label] of nav) {
      items.push({
        id: `nav:${page}`,
        title: label,
        group: t("palette.navigation"),
        keywords: [page],
        run: () => {
          if (page === "files") openWorkspacePanel("files");
          else if (page === "memory") setSettingsTarget("memory");
          else if (page === "terminal") {
            if (!workspacePanelRenderable) openWorkspacePanel("files");
            setTerminalDockOpen((v) => !v);
          }
          else if (page === "scheduled") setSchedulerOpen(true);
          else if (page === "trace") setTraceOpen(true);
          else if (page === "calendar" || page === "dailyBrief") openRightDockMode(page);
          else setNavPage(page);
        },
      });
    }
    // Actions group — common one-shot operations.
    items.push({
      id: "act:new",
      title: t("topbar.newSession"),
      group: t("palette.actions"),
      run: () => { cancel(); void startNewSession(); },
    });
    items.push({
      id: "act:settings",
      title: t("topbar.settings"),
      group: t("palette.actions"),
      run: () => setSettingsTarget("general"),
    });
    items.push({
      id: "act:templates",
      title: t("sidebar.templates"),
      group: t("palette.actions"),
      run: () => setTemplatesOpen(true),
    });
    items.push({
      id: "act:history",
      title: t("sidebar.allHistory"),
      group: t("palette.actions"),
      run: () => void openAllHistory(),
    });
    items.push({
      id: "act:repoWiki",
      title: t("sidebar.repoWiki"),
      group: t("palette.actions"),
      run: () => setRepoWikiOpen(true),
    });
    items.push({
      id: "act:trash",
      title: t("sidebar.trash"),
      group: t("palette.actions"),
      run: () => void openTrash(),
    });
    // Skills group — trigger a skill via the composer (/skillname).
    for (const s of paletteSkills) {
      if (!s.enabled) continue;
      items.push({
        id: `skill:${s.name}`,
        title: s.name,
        hint: s.description,
        group: t("palette.skills"),
        keywords: [s.scope, s.runAs],
        run: () => {
          setComposerInsertRequest({ id: Date.now(), text: `/${s.name} ` });
        },
      });
    }
    // Workflows group — open the workflow editor on the named workflow.
    for (const w of paletteWorkflows) {
      items.push({
        id: `wf:${w.name}`,
        title: w.name,
        hint: w.description,
        group: t("palette.workflows"),
        run: () => {
          setWorkflowModalOpen(true);
        },
      });
    }
    return items;
  }, [tabMetas, t, handleTabChange, openWorkspacePanel, cancel, startNewSession, openAllHistory, openTrash, paletteSkills, paletteWorkflows, setComposerInsertRequest]);

  const closeHistory = useCallback(() => setHistView(null), []);
  const onResumeSession = useCallback(
    async (session: SessionMeta) => {
      if (state.running) return;
      setHistView(null);
      const scope = session.scope || (session.workspaceRoot ? "project" : "global");
      let targetTab: TabMeta | undefined;
      if (scope === "project" && session.workspaceRoot && session.topicId) {
        targetTab = await openProjectTab(session.workspaceRoot, session.topicId);
      } else if (scope === "global" && session.topicId) {
        targetTab = await openGlobalTab(session.topicId);
      }
      await resumeSession(session.path, targetTab?.id);
      if (targetTab) {
        await refreshTabMetas();
        setTabRevealSignal((signal) => signal + 1);
      }
    },
    [openGlobalTab, openProjectTab, refreshTabMetas, state.running, resumeSession],
  );
  // Delete / rename act on disk, then re-fetch so the panel reflects the change.
  const onDeleteSession = useCallback(
    async (path: string) => {
      if (state.running) return;
      await deleteSession(path);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    },
    [state.running, deleteSession, listSessions],
  );
  const onRenameSession = useCallback(
    async (path: string, title: string) => {
      if (state.running) return;
      await renameSession(path, title);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    },
    [state.running, renameSession, listSessions],
  );
  const onRestoreTrashedSession = useCallback(
    async (path: string) => {
      await restoreSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [restoreSession, listTrashedSessions],
  );
  const onPurgeTrashedSession = useCallback(
    async (path: string) => {
      await purgeTrashedSession(path);
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );
  const onPurgeAllTrashedSessions = useCallback(
    async (paths: string[]) => {
      const uniquePaths = Array.from(new Set(paths));
      for (const path of uniquePaths) {
        await purgeTrashedSession(path);
      }
      const trashed = await listTrashedSessions();
      setHistView((cur) => (cur === null ? null : { kind: "trash", sessions: trashed }));
    },
    [purgeTrashedSession, listTrashedSessions],
  );

  // Workspace: open the folder chooser and switch projects. The hook resets the
  // transcript and refreshes meta on a pick. A cancel is a no-op.
  const switchFolder = useCallback(async (path?: string) => {
    const picked = path === undefined ? await pickWorkspace() : await switchWorkspace(path);
    if (picked) {
      setProjectRevision((value) => value + 1);
      await refreshTabMetas();
    }
    return picked;
  }, [pickWorkspace, switchWorkspace, refreshTabMetas]);

  const removeWorkspace = useCallback(async (path: string) => {
    await app.RemoveWorkspace(path);
    setProjectRevision((value) => value + 1);
    await refreshTabMetas();
  }, [refreshTabMetas]);

  const refreshProjectsAndTabs = useCallback(async () => {
    setProjectRevision((value) => value + 1);
    const tabs = await refreshTabMetas();
    if (activeTabId && !tabs.some((tab) => tab.id === activeTabId)) {
      await syncActiveTab(true);
    }
  }, [activeTabId, refreshTabMetas, syncActiveTab]);

  const renameTopic = useCallback(async (topicId: string, title: string) => {
    const nextTitle = title.trim();
    if (!topicId || !nextTitle) return;
    await app.RenameTopic(topicId, nextTitle);
    await refreshProjectsAndTabs();
  }, [refreshProjectsAndTabs]);

  const startActiveTopicRename = useCallback(() => {
    if (!activeTab?.topicId) return;
    topicRenameSkipCommitRef.current = false;
    topicRenameCommitHandledRef.current = false;
    setRenamingTopicId(activeTab.topicId);
    setTopicTitleDraft(activeTab.topicTitle || "");
  }, [activeTab?.topicId, activeTab?.topicTitle]);

  const cancelActiveTopicRename = useCallback(() => {
    topicRenameSkipCommitRef.current = true;
    topicRenameCommitHandledRef.current = true;
    setRenamingTopicId(null);
    setTopicTitleDraft("");
  }, []);

  const commitActiveTopicRename = useCallback(async () => {
    if (topicRenameSkipCommitRef.current) {
      topicRenameSkipCommitRef.current = false;
      topicRenameCommitHandledRef.current = false;
      setRenamingTopicId(null);
      return;
    }
    if (topicRenameCommitHandledRef.current) return;
    topicRenameCommitHandledRef.current = true;
    const topicId = renamingTopicId;
    setRenamingTopicId(null);
    if (!topicId) return;
    const nextTitle = topicTitleDraft.trim();
    if (!nextTitle) return;
    try {
      await renameTopic(topicId, nextTitle);
    } catch {
      /* keep the app usable if a stale topic cannot be renamed */
    }
  }, [renameTopic, renamingTopicId, topicTitleDraft]);

  const sidebarExpandBlocked = false;
  const sidebarNavTooltipDisabled = !sidebarCollapsed;
  const workspacePanelResetWidth = RIGHT_DOCK_DEFAULT_WIDTH;
  const workspacePanelMaxWidth = RIGHT_DOCK_MAX_WIDTH;

  return (
    <ShellExpandProvider>
    <ShellHotkeys />
    <div className={`app app--${desktopPlatform}`}>
      <div
        ref={layoutRef}
        className={[
          "layout",
          sidebarCollapsed ? "layout--sidebar-collapsed" : "",
          sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
          workspacePanelGridOpen ? "layout--workspace-open" : "",
          workspacePanelFloating ? "layout--workspace-floating" : "",
          workspacePanelOpen && workspacePanelMaximized ? "layout--workspace-maximized" : "",
          workspacePanelResizing ? "layout--resizing layout--workspace-resizing" : "",
        ]
          .filter(Boolean)
          .join(" ")}
        style={layoutStyle}
      >
        <Sidebar
          collapsed={sidebarCollapsed}
          navTooltipDisabled={sidebarNavTooltipDisabled}
          onExpand={sidebarExpandBlocked ? undefined : toggleSidebar}
          onCollapse={sidebarExpandBlocked ? undefined : toggleSidebar}
          onNewSession={() => { cancel(); void startNewSession(); }}
          isRunning={state.running}
          onNavigate={(page: string) => {
            // "files" is rendered in the right workspace dock, not the main pane;
            // "memory" lives in the settings centre (MemorySettingsPage) — both
            // reuse existing implementations instead of dead navPage branches.
            // "terminal" toggles the embedded terminal in the right dock.
            if (page === "files") {
              openWorkspacePanel("files");
            } else if (page === "memory") {
              setSettingsTarget("memory");
            } else if (page === "terminal") {
              if (!workspacePanelRenderable) openWorkspacePanel("files");
              setTerminalDockOpen((v) => !v);
            } else if (page === "scheduled") {
              setSchedulerOpen(true);
            } else if (page === "trace") {
              setTraceOpen(true);
            } else if (page === "workflow") {
              // Workflow editor opens as a modal overlay so the active
              // conversation stays visible underneath.
              setWorkflowModalOpen(true);
            } else if (page === "skillsMarket") {
              setSkillsMarketModalOpen(true);
            } else if (page === "designPanel") {
              setDesignPanelModalOpen(true);
            } else if (page === "sessionSync") {
              setSessionSyncModalOpen(true);
            } else if (page === "calendar" || page === "dailyBrief") {
              openRightDockMode(page);
            } else {
              setNavPage(page);
            }
          }}
          activePage={navPage}
          activeScope={activeTab?.scope}
          activeWorkspaceRoot={activeTab?.workspaceRoot}
          activeTopicId={activeTab?.topicId}
          onOpenTopic={handleOpenTopic}
          onOpenProjectHistory={openProjectHistory}
          onTopicsChanged={refreshProjectsAndTabs}
          onRenameTopic={renameTopic}
          refreshSignal={projectRevision}
          onAddProject={async () => { await switchFolder(); }}
          onActivateSkill={(name) => { cancel(); void startNewSession().then(() => addWorkspaceTextToComposer(`/skill ${name}`)); }}
          onOpenTemplates={() => setTemplatesOpen(true)}
          onOpenRepoWiki={() => setRepoWikiOpen(true)}
          onOpenAllHistory={openAllHistory}
          onOpenTrash={openTrash}
          onOpenSettings={() => setSettingsTarget("general")}
        />
        <button
          className="sidebar-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("sidebar.resize")}
          aria-valuemin={SIDEBAR_MIN_WIDTH}
          aria-valuemax={SIDEBAR_MAX_WIDTH}
          aria-valuenow={sidebarWidth}
          onPointerDown={startSidebarResize}
          onKeyDown={resizeSidebarWithKeyboard}
          onDoubleClick={() => setExpandedSidebarWidth(defaultSidebarWidth())}
        />

        <section className="chat-pane">
          <header className="workspace-tabs-bar">
            <TabBar
              tabs={visibleTabs}
              activeTabId={visibleTabId}
              revealActiveSignal={tabRevealSignal}
              onTabChange={(id) => void handleTabChange(id)}
              onTabClose={(id) => void handleTabClose(id)}
              onTabsClose={(ids, nextActiveTabId) => void handleTabsClose(ids, nextActiveTabId)}
              onTabsReorder={(ids) => void handleTabsReorder(ids)}
              onNewTab={() => void handleNewTab()}
              onSaveRecipe={handleSaveRecipeFromTab}
            />
            <NotificationBell onClick={() => setNotificationCenterOpen(true)} />
            {!workspacePanelMaximized && (
              <Tooltip
                label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
                className={[
                  "workspace-dock-toggle",
                  workspacePanelRenderable ? "workspace-dock-toggle--open" : "workspace-dock-toggle--closed",
                ].join(" ")}
              >
                <button
                  className="workspace-dock-toggle__button"
                  type="button"
                  onClick={workspacePanelRenderable ? closeWorkspacePanel : () => openWorkspacePanel("files")}
                  aria-label={workspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
                  aria-pressed={workspacePanelRenderable}
                >
                  {workspacePanelRenderable ? <PanelRightClose size={15} /> : <PanelRightOpen size={15} />}
                </button>
              </Tooltip>
            )}
          </header>

          <>
          <header className="topicbar">
            <div className="topicbar__identity">
              <div className="topicbar__title-row">
                {topicbarEditing ? (
                  <div className="topicbar__title-edit">
                    {topicbarProjectPrefix && (
                      <span className="topicbar__title-prefix">{topicbarProjectPrefix} /</span>
                    )}
                    <input
                      autoFocus
                      className="topicbar__title-input"
                      value={topicTitleDraft}
                      onChange={(event) => setTopicTitleDraft(event.target.value)}
                      onKeyDown={(event: KeyboardEvent<HTMLInputElement>) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          void commitActiveTopicRename();
                        }
                        if (event.key === "Escape") {
                          event.preventDefault();
                          cancelActiveTopicRename();
                        }
                      }}
                      onBlur={() => void commitActiveTopicRename()}
                    />
                  </div>
                ) : (
                  <h1>{topicTitle(activeTab)}</h1>
                )}
                <Tooltip label={t("topicBar.renameSession")}>
                  <button
                    className="topicbar__icon-btn"
                    type="button"
                    disabled={!activeTab?.topicId || topicbarEditing}
                    onClick={startActiveTopicRename}
                    aria-label={t("topicBar.renameSession")}
                  >
                    <Pencil size={14} />
                  </button>
                </Tooltip>
              </div>
            </div>
            <div className="topicbar__spacer" />
            <div className="topicbar__actions">
              <Tooltip label={t("sideChat.title")}>
                <button
                  className={`topicbar__action-btn topicbar__action-btn--icon${sideChatOpen ? " is-active" : ""}`}
                  type="button"
                  onClick={() => setSideChatOpen((v) => !v)}
                  aria-label={t("sideChat.title")}
                  aria-pressed={sideChatOpen}
                >
                  <MessageSquare size={14} />
                </button>
              </Tooltip>
              <CopyButton
                getText={getSessionMarkdown}
                label={t("topicBar.copyAll")}
                showLabel={false}
                className="topicbar__action-btn topicbar__action-btn--icon"
              />
              <div className={`topicbar__export${topicExportOpen ? " topicbar__export--open" : ""}`}>
                <Tooltip label={t("topicBar.export")}>
                  <button
                    className="topicbar__action-btn topicbar__action-btn--icon"
                    type="button"
                    disabled={!sessionHasContent}
                    aria-label={t("topicBar.export")}
                    aria-haspopup="menu"
                    aria-expanded={topicExportOpen}
                    onClick={() => setTopicExportOpen((open) => !open)}
                  >
                    <Download size={14} />
                  </button>
                </Tooltip>
                {topicExportOpen && (
                  <div className="topicbar__export-menu" role="menu">
                    <button type="button" role="menuitem" onClick={() => exportSession("markdown")}>
                      <FileText size={13} />
                      <span>{t("topicBar.exportMarkdown")}</span>
                    </button>
                    <button type="button" role="menuitem" onClick={() => exportSession("json")}>
                      <FileJson size={13} />
                      <span>{t("topicBar.exportJson")}</span>
                    </button>
                  </div>
                )}
              </div>
            </div>
          </header>

          {state.meta?.startupErr && (
            <div className="banner banner--error">{t("topbar.startupError", { msg: state.meta.startupErr })}</div>
          )}

          <UpdateBanner />

          <main className="main">
            {state.meta?.ready === false && !state.meta?.startupErr ? (
              <div className="loading-screen">
                <div className="loading-screen__spinner" />
                <span className="loading-screen__text">{t("common.loading")}</span>
              </div>
            ) : navPage === "home" ? (
              <HomePanel
                workspaceType={workspaceType}
                onActivateSkill={(name) => { cancel(); void startNewSession().then(() => addWorkspaceTextToComposer(`/skill ${name}`)); }}
                onNavigateToSession={(_path) => setNavPage(null)}
                onSwitchMode={(mode) => applyWorkspaceType(mode)}
              />
            ) : (
              <>
                {progressSteps.length > 0 && (
                  <ProgressStepper
                    steps={progressSteps}
                    onStepClick={handleStepClick}
                  />
                )}
	              <Transcript
	                ref={transcriptRef}
	                items={deferredItems}
	                live={state.live}
	                footerHeight={footerHeight}
	                onPrompt={send}
	                onRewind={handleMessageAction}
	                checkpoints={state.checkpoints}
	                actionPending={state.messageAction != null}
	                rewindDisabled={state.running || state.messageAction != null || state.approval != null || state.ask != null}
	                onPreview={handleToolPreview}
	                workspaceType={workspaceType}
              />
              </>
            )}
          </main>

          <footer className="footer" ref={footerRef}>
            {showTodos && <TodoPanel todos={todos} stale={todoStale} onDismiss={() => setDismissedTodo(todoItem!.id)} />}
            {state.approval && (
              <ApprovalModal
                approval={state.approval}
                onAnswer={(allow, session, persist) => {
                  // Approving an exit_plan_mode plan leaves plan mode; sync the
                  // tab-local indicator and persisted safe mode immediately.
                  if (state.approval!.tool === "exit_plan_mode" && allow) applyMode("normal");
                  approve(state.approval!.id, allow, session, persist);
                }}
                onRevisePlan={(text) => {
                  setPendingPlanRevision(text);
                  approve(state.approval!.id, false, false, false);
                }}
                onExitPlan={() => {
                  applyMode("normal");
                  approve(state.approval!.id, false, false, false);
                }}
              />
            )}
            {state.ask && (
              <AskCard
                ask={state.ask}
                onAnswer={answerQuestion}
                onDismiss={() => answerQuestion(state.ask!.id, [])}
              />
            )}
            {state.autoLearn && (
              <div className="auto-learn-card prompt-shelf" role="dialog" aria-label={t("autoLearn.title")}>
                <div className="auto-learn-card__head">
                  <span className="auto-learn-card__badge">
                    {t(`autoLearn.${state.autoLearn.type}` as any)}
                  </span>
                  <span className="auto-learn-card__title">{t("autoLearn.title")}</span>
                </div>
                <div className="auto-learn-card__prompt">
                  {t("autoLearn.prompt").replace("{file}", state.autoLearn.targetFile)}
                </div>
                <div className="auto-learn-card__content">
                  <span className="auto-learn-card__content-label">{t("autoLearn.content")}</span>
                  <code className="auto-learn-card__content-body">{state.autoLearn.content}</code>
                </div>
                <div className="auto-learn-card__actions">
                  <button
                    className="chip chip--primary"
                    onClick={() => answerAutoLearn(state.autoLearn, true)}
                  >
                    {t("autoLearn.accept")}
                  </button>
                  <button
                    className="chip"
                    onClick={() => answerAutoLearn(state.autoLearn, false)}
                  >
                    {t("autoLearn.reject")}
                  </button>
                </div>
              </div>
            )}
            <SaveRecipeModal
              isOpen={saveRecipeOpen}
              onClose={() => setSaveRecipeOpen(false)}
              initialSkill={saveRecipeSkill}
              initialParams={saveRecipeParams}
              tabId={saveRecipeTabId}
            />
            <Composer
              running={state.running}
              mode={mode}
              cwd={state.meta?.cwd}
              modelLabel={state.meta?.label ?? t("status.connecting")}
              tabId={activeTabId}
              effort={state.effort}
              onSend={handleSend}
              onCancel={cancel}
              onCycleMode={cycleMode}
              onSetMode={applyMode}
              onSwitchModel={switchModel}
              onSetEffort={setEffort}
              onPickFolder={switchFolder}
              onRemoveWorkspace={removeWorkspace}
              insertRequest={composerInsertRequest}
	              disabled={state.meta?.ready === false || state.messageAction != null || state.approval != null || state.ask != null}
	              decisionPending={state.messageAction != null || state.approval != null || state.ask != null}
              ready={state.meta?.ready === true}
              turnStartAt={state.turnStartAt}
              turnTokens={state.turnTokens}
              retry={state.retry}
              workspaceRefreshSignal={projectRevision}
              workspaceType={workspaceType}
            />
            <StatusBar
              context={state.context}
              usage={state.usage}
              balance={state.balance}
              jobs={state.jobs}
              running={state.running}
              mode={mode}
              cost={state.sessionCost}
              currency={state.sessionCurrency}
            />
          </footer>
          </>
        </section>

        {workspacePanelGridOpen && (
          <button
            className="workspace-panel-resizer"
            type="button"
            role="separator"
            aria-orientation="vertical"
            aria-label={t("rightDock.resize")}
            aria-valuemin={workspacePanelMinWidth}
            aria-valuemax={Math.max(workspacePanelMaxWidth, workspacePanelRenderWidth)}
            aria-valuenow={workspacePanelRenderWidth}
            onPointerDown={startWorkspacePanelResize}
            onKeyDown={resizeWorkspacePanelWithKeyboard}
            onDoubleClick={() => setSavedWorkspacePanelWidth(workspacePanelResetWidth)}
          />
        )}

        {workspacePanelRenderable && (
          <aside
            className={[
              "workbench-dock",
              `workbench-dock--${rightDockMode}`,
            ].join(" ")}
            aria-label={t("rightDock.workbench")}
          >
            <div className="workbench-dock__tools">
              <div className="workbench-dock__tabs" role="tablist" aria-label={t("rightDock.views")}>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "preview"}
                  className={`workbench-dock__tab${rightDockMode === "preview" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("preview")}
                >
                  <Eye size={13} />
                  <span className="workbench-dock__tab-label">{t("rightDock.preview")}</span>
                </button>
                {SHOW_CONTEXT_DOCK && (
                  <button
                    type="button"
                    role="tab"
                    aria-selected={rightDockMode === "context"}
                    className={`workbench-dock__tab${rightDockMode === "context" ? " workbench-dock__tab--active" : ""}`}
                    onClick={() => openRightDockMode("context")}
                  >
                    <CircleGauge size={13} />
                    <span className="workbench-dock__tab-label">{t("rightDock.overview")}</span>
                  </button>
                )}
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "dailyBrief"}
                  className={`workbench-dock__tab${rightDockMode === "dailyBrief" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("dailyBrief")}
                >
                  <Newspaper size={13} />
                  <span className="workbench-dock__tab-label">{t("sidebar.dailyBrief")}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "imSessions"}
                  className={`workbench-dock__tab${rightDockMode === "imSessions" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("imSessions")}
                >
                  <MessageSquare size={13} />
                  <span className="workbench-dock__tab-label">{t("sidebar.imSessions")}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "calendar"}
                  className={`workbench-dock__tab${rightDockMode === "calendar" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("calendar")}
                >
                  <Calendar size={13} />
                  <span className="workbench-dock__tab-label">{t("sidebar.assistantSchedule")}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "files"}
                  className={`workbench-dock__tab${rightDockMode === "files" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("files")}
                >
                  <FileText size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.filesTab")}</span>
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={rightDockMode === "changed"}
                  className={`workbench-dock__tab${rightDockMode === "changed" ? " workbench-dock__tab--active" : ""}`}
                  onClick={() => openRightDockMode("changed")}
                >
                  <GitBranch size={13} />
                  <span className="workbench-dock__tab-label">{t("workspace.changedTab")}</span>
                </button>
              </div>
              <button
                type="button"
                className={`workbench-dock__term-toggle${terminalDockOpen ? " is-active" : ""}`}
                onClick={() => setTerminalDockOpen((v) => !v)}
                title={t("sidebar.terminal")}
              >
                <TerminalSquare size={14} />
              </button>
            </div>
            <div className="workbench-dock__body">
              {rightDockMode === "preview" ? (
                <PreviewPanel
                  workspaceType={workspaceType}
                  activeFilePath={previewFilePath}
                  diffOriginal={previewDiffOriginal}
                  diffModified={previewDiffModified}
                  tabId={activeTabId}
                />
              ) : rightDockMode === "context" ? (
                <ContextPanel
                  tabId={activeTabId}
                  context={state.context}
                  usage={state.usage}
                  sessionCost={state.sessionCost}
                  sessionCurrency={state.sessionCurrency}
                  scopeLabel={topicScopeLabel(activeTab)}
                  refreshKey={dockRefreshKey}
                  compactions={state.items.filter((it): it is Extract<typeof it, { kind: "compaction" }> => it.kind === "compaction").map((c): CompactionRecord => ({ id: c.id, trigger: c.trigger, messages: c.messages, summary: c.summary, archive: c.archive, pending: c.pending }))}
                  memoryFacts={contextMemoryFacts}
                />
              ) : rightDockMode === "dailyBrief" ? (
                <DailyBriefPanel />
              ) : rightDockMode === "imSessions" ? (
                <IMSessionsPanel />
              ) : rightDockMode === "calendar" ? (
                <CalendarPanel tabId={activeTabId} />
              ) : (
                <WorkspacePanel
                  open={workspacePanelRenderable}
                  cwd={state.meta?.cwd}
                  tabId={activeTabId}
                  maximized={workspacePanelMaximized}
                  panelWidth={workspacePanelRenderWidth}
                  onClose={() => setWorkspacePanel(false)}
                  onToggleMaximized={() => setWorkspacePanelMaximized((value) => !value)}
                  onPreviewModeChange={setWorkspacePreviewActive}
                  onAddToChat={addWorkspaceTextToComposer}
                  onRequestPanelWidth={ensureWorkspacePanelWidth}
                  refreshKey={dockRefreshKey}
                  initialViewMode={rightDockMode === "changed" ? "changed" : "files"}
                  showViewTabs={false}
                />
              )}
            </div>
            {terminalDockOpen && (
              <>
                <div
                  className="workbench-dock__term-resizer"
                  role="separator"
                  aria-orientation="horizontal"
                  onPointerDown={startTerminalDockResize}
                />
                <div className="workbench-dock__terminal" style={{ height: terminalDockHeight }}>
                  <TerminalPanel
                    cwd={state.meta?.cwd}
                    onDockClose={() => setTerminalDockOpen(false)}
                  />
                </div>
              </>
            )}
          </aside>
        )}
      </div>

      {histView !== null && (
        <HistoryPanel
          kind={histView.kind}
          sessions={histView.sessions}
          running={state.running}
          onResume={onResumeSession}
          onPreview={previewSession}
          onDelete={onDeleteSession}
          onRename={onRenameSession}
          onRestore={onRestoreTrashedSession}
          onPurge={onPurgeTrashedSession}
          onPurgeAll={onPurgeAllTrashedSessions}
          onClose={closeHistory}
        />
      )}

      {settingsTarget !== null && (
        <SettingsPanel
          initialTab={settingsTarget}
          onClose={() => setSettingsTarget(null)}
          onChanged={() => void refreshMeta()}
          autoSwitchMode={autoSwitchMode}
          onAutoSwitchModeChange={setAutoSwitchMode}
        />
      )}

      {repoWikiOpen && (
        <RepoWikiPanel
          onClose={() => setRepoWikiOpen(false)}
          cwd={state.meta?.cwd}
        />
      )}

      {schedulerOpen && (
        <SchedulerPanel onClose={() => setSchedulerOpen(false)} />
      )}

      {workflowModalOpen && (
        <div className="wf-modal-overlay" role="dialog" aria-modal="true">
          <div className="wf-modal-overlay__dialog">
            <WorkflowEditor onClose={() => setWorkflowModalOpen(false)} />
          </div>
        </div>
      )}

      {skillsMarketModalOpen && (
        <div className="wf-modal-overlay" role="dialog" aria-modal="true">
          <div className="wf-modal-overlay__dialog">
            <SkillsBrowser
              onSkillsChanged={refreshProjectsAndTabs}
              onClose={() => setSkillsMarketModalOpen(false)}
            />
          </div>
        </div>
      )}

      {designPanelModalOpen && (
        <div className="wf-modal-overlay" role="dialog" aria-modal="true">
          <div className="wf-modal-overlay__dialog">
            <DesignPanel onClose={() => setDesignPanelModalOpen(false)} />
          </div>
        </div>
      )}

      {sessionSyncModalOpen && (
        <div className="wf-modal-overlay" role="dialog" aria-modal="true">
          <div className="wf-modal-overlay__dialog">
            <SessionSync onClose={() => setSessionSyncModalOpen(false)} />
          </div>
        </div>
      )}

      <SupervisionOverlay
        active={supervisionActive}
        pendingAction={supervisionPending}
        history={supervisionHistory}
        paused={supervisionPaused}
        onPause={() => setSupervisionPaused(true)}
        onResume={() => setSupervisionPaused(false)}
        onAbort={() => { setSupervisionActive(false); setSupervisionPending(null); setSupervisionHistory([]); }}
        onConfirmSensitive={() => setSupervisionPending((p) => p ? { ...p, status: "running" as const } : p)}
        onDismiss={() => setSupervisionActive(false)}
      />

      {traceOpen && (
        <ResizableDrawer onClose={() => setTraceOpen(false)} subtle wide>
          <AgentCanvas
            items={deferredItems}
            running={state.running}
            steps={state.steps}
            agents={state.agents}
            onRetryTool={handleRetryTool}
            onCloseAgent={handleCloseAgent}
            onClose={() => setTraceOpen(false)}
          />
        </ResizableDrawer>
      )}

      {templatesOpen && (
        <TemplateLibrary
          onClose={() => setTemplatesOpen(false)}
          onApply={(tmpl) => {
            // Apply inserts the template path and a clear instruction into the
            // composer so the agent knows to render it via
            // mcp__office__render_template. We deliberately do NOT route through
            // /skill because user-uploaded templates are not skills — RunSkill
            // would fail to match the name and silently drop the command.
            let prompt: string;
            if (tmpl.kind === "tmpl") {
              // Go text/template — must use render_template to substitute {{.key}} vars.
              prompt = t("templates.applyTmpl", { path: tmpl.relPath });
            } else if (tmpl.kind === "md") {
              // Markdown template — may contain {{.key}} vars, try render_template first.
              prompt = t("templates.applyMd", { path: tmpl.relPath });
            } else {
              // txt/csv — read as reference content.
              prompt = t("templates.applyRef", { path: tmpl.relPath });
            }
            addWorkspaceTextToComposer(prompt);
            setTemplatesOpen(false);
          }}
        />
      )}

      {startupSplashVisible && (
        <StartupSplash hold={startupSplashHold} onDone={() => setStartupSplashVisible(false)} />
      )}

      {needsOnboarding && <OnboardingOverlay onComplete={() => setNeedsOnboarding(false)} />}

      {floatingVisible && (
        <FloatingWindow
          visible={floatingVisible}
          onClose={() => { setFloatingVisible(false); setFloatingResult(null); }}
          onSubmitAction={async (action, text) => {
            setFloatingLoading(true);
            try {
              const skillText = `/skill clipboard-${action}\n${text}`;
              await addWorkspaceTextToComposer(skillText);
              send(skillText);
              // Brief delay for the agent to start, then show result placeholder
              setFloatingResult(t("floatingWindow.processing"));
            } catch {
              setFloatingResult(t("floatingWindow.error"));
            } finally {
              setFloatingLoading(false);
            }
          }}
          result={floatingResult}
          loading={floatingLoading}
        />
      )}

      {showOnboardingCelebration && (
        <div className="onboarding-celebration">
          <div className="onboarding-celebration__card">
            <div className="onboarding-celebration__emoji">🎉</div>
            <div className="onboarding-celebration__title">{t("onboarding.celebrationTitle")}</div>
            <div className="onboarding-celebration__desc">{t("onboarding.celebrationDesc")}</div>
            <button
              className="onboarding-celebration__btn"
              onClick={() => setShowOnboardingCelebration(false)}
            >
              {t("onboarding.celebrationContinue")}
            </button>
            <button
              className="onboarding-celebration__link"
              onClick={() => { setShowOnboardingCelebration(false); setTemplatesOpen(true); }}
            >
              {t("onboarding.celebrationSkills")}
            </button>
          </div>
        </div>
      )}

      <CommandPalette
        open={commandPaletteOpen}
        onClose={() => setCommandPaletteOpen(false)}
        items={commandPaletteItems}
        placeholder={t("palette.placeholder")}
        emptyText={t("palette.empty")}
      />
      <NotificationCenter
        open={notificationCenterOpen}
        onClose={() => setNotificationCenterOpen(false)}
      />
      <SideChat
        visible={sideChatOpen}
        contextItems={state.items}
        onSend={(text) => send(text)}
        onClose={() => setSideChatOpen(false)}
        onPromoteToMain={(text) => { send(text); setSideChatOpen(false); }}
      />
    </div>
    </ShellExpandProvider>
  );
}
