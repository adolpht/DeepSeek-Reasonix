import { useState, useEffect } from "react";
import {
  Sun,
  Moon,
  CloudSun,
  Code2,
  GitCompare,
  Bug,
  TestTube2,
  Table2,
  FileSpreadsheet,
  Presentation,
  FileStack,
  Search,
  FileText,
  Languages,
  CalendarCheck,
  Lightbulb,
  Clock,
  Zap,
  ChevronRight,
} from "lucide-react";
import type { WorkspaceType, SessionMeta, HomePageData } from "../lib/types";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";

// ── Quick action definitions per workspace type ────────────────
interface QuickAction {
  key: string;
  icon: React.ReactNode;
  color: string;
  skillCommand: string;
}

const QUICK_ACTIONS: Record<WorkspaceType, QuickAction[]> = {
  coding: [
    { key: "analyzeProject", icon: <Code2 size={18} />, color: "blue", skillCommand: "/explore" },
    { key: "reviewCode", icon: <GitCompare size={18} />, color: "green", skillCommand: "/review" },
    { key: "fixBug", icon: <Bug size={18} />, color: "red", skillCommand: "" },
    { key: "generateTests", icon: <TestTube2 size={18} />, color: "purple", skillCommand: "" },
  ],
  office: [
    { key: "analyzeSheet", icon: <Table2 size={18} />, color: "green", skillCommand: "/skill sheet-analysis" },
    { key: "weeklyReport", icon: <FileSpreadsheet size={18} />, color: "blue", skillCommand: "/skill weekly-report" },
    { key: "makePpt", icon: <Presentation size={18} />, color: "orange", skillCommand: "" },
    { key: "organizeDoc", icon: <FileStack size={18} />, color: "teal", skillCommand: "/skill sheet-clean" },
  ],
  assistant: [
    { key: "searchResearch", icon: <Search size={18} />, color: "blue", skillCommand: "" },
    { key: "summarizeArticle", icon: <FileText size={18} />, color: "green", skillCommand: "" },
    { key: "translateContent", icon: <Languages size={18} />, color: "purple", skillCommand: "" },
    { key: "scheduleManage", icon: <CalendarCheck size={18} />, color: "orange", skillCommand: "" },
  ],
};

// ── Time-aware greeting ────────────────────────────────────────
function getGreetingKey(): "home.greetingMorning" | "home.greetingAfternoon" | "home.greetingEvening" {
  const hour = new Date().getHours();
  if (hour >= 5 && hour < 12) return "home.greetingMorning";
  if (hour >= 12 && hour < 18) return "home.greetingAfternoon";
  return "home.greetingEvening";
}

function getGreetingIcon() {
  const hour = new Date().getHours();
  if (hour >= 5 && hour < 12) return <Sun size={22} />;
  if (hour >= 12 && hour < 18) return <CloudSun size={22} />;
  return <Moon size={22} />;
}

// ── Relative time formatting ───────────────────────────────────
function relativeTime(ms: number, t: (key: "home.justNow" | "home.minutesAgo" | "home.hoursAgo" | "home.daysAgo", params?: Record<string, string | number>) => string): string {
  const diff = Date.now() - ms;
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return t("home.justNow");
  if (minutes < 60) return t("home.minutesAgo", { n: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("home.hoursAgo", { n: hours });
  const days = Math.floor(hours / 24);
  return t("home.daysAgo", { n: days });
}

// ── Props ──────────────────────────────────────────────────────
export interface HomePanelProps {
  workspaceType: WorkspaceType;
  onActivateSkill: (name: string) => void;
  onNavigateToSession: (path: string) => void;
  onSwitchMode: (mode: WorkspaceType) => void;
}

// ── Component ──────────────────────────────────────────────────
export function HomePanel({
  workspaceType,
  onActivateSkill,
  onNavigateToSession,
  onSwitchMode,
}: HomePanelProps) {
  const t = useT();
  const [data, setData] = useState<HomePageData | null>(null);

  useEffect(() => {
    let cancelled = false;
    app.GetHomePageData().then((result) => {
      if (!cancelled) setData(result);
    });
    return () => { cancelled = true; };
  }, []);

  const actions = QUICK_ACTIONS[workspaceType];

  const handleQuickAction = (action: QuickAction) => {
    onSwitchMode(workspaceType);
    if (action.skillCommand) {
      onActivateSkill(action.skillCommand.startsWith("/") ? action.skillCommand.slice(1) : action.skillCommand);
    }
  };

  return (
    <div className="home-panel">
      {/* ── Greeting ── */}
      <section className="home-panel__greeting">
        <span className="home-panel__greeting-icon">{getGreetingIcon()}</span>
        <div className="home-panel__greeting-text">
          <h2 className="home-panel__greeting-title">{t(getGreetingKey())}</h2>
          <p className="home-panel__greeting-subtitle">
            {t(`home.roleDescription.${workspaceType}` as any)}
          </p>
        </div>
      </section>

      {/* ── Quick actions ── */}
      <section className="home-panel__quick-actions">
        <h3 className="home-panel__section-title">{t("home.quickActions")}</h3>
        <div className="home-panel__action-grid">
          {actions.map((action) => (
            <button
              key={action.key}
              className={`home-panel__action-card home-panel__action-card--${action.color}`}
              onClick={() => handleQuickAction(action)}
            >
              <span className="home-panel__action-icon">{action.icon}</span>
              <span className="home-panel__action-label">
                {t(`home.action.${action.key}` as any)}
              </span>
            </button>
          ))}
        </div>
      </section>

      {/* ── Recent tasks ── */}
      <section className="home-panel__recent-tasks">
        <h3 className="home-panel__section-title">{t("home.recentTasks")}</h3>
        {data && data.recentTasks.length > 0 ? (
          <ul className="home-panel__task-list">
            {data.recentTasks.slice(0, 5).map((task: SessionMeta) => (
              <li key={task.path} className="home-panel__task-item">
                <button
                  className="home-panel__task-button"
                  onClick={() => onNavigateToSession(task.path)}
                >
                  <span className="home-panel__task-title">
                    {task.title || task.preview}
                  </span>
                  <span className="home-panel__task-meta">
                    <span className="home-panel__task-status">
                      {task.open
                        ? t("home.taskOpen")
                        : task.current
                          ? t("home.taskCurrent")
                          : t("home.taskClosed")}
                    </span>
                    <span className="home-panel__task-time">
                      <Clock size={11} />
                      {relativeTime(task.lastActivityAt, t)}
                    </span>
                  </span>
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="home-panel__empty">{t("home.noRecentTasks")}</p>
        )}
      </section>

      {/* ── Suggested skills ── */}
      <section className="home-panel__skills">
        <h3 className="home-panel__section-title">{t("home.suggestedSkills")}</h3>
        {data && data.suggestedSkills.length > 0 ? (
          <ul className="home-panel__skill-list">
            {data.suggestedSkills.map((skill) => (
              <li key={skill.name} className="home-panel__skill-item">
                <button
                  className="home-panel__skill-button"
                  onClick={() => onActivateSkill(skill.name)}
                >
                  <Zap size={13} />
                  <span className="home-panel__skill-name">{skill.name}</span>
                  <span className="home-panel__skill-desc">{skill.description}</span>
                  <ChevronRight size={13} className="home-panel__skill-arrow" />
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="home-panel__empty">{t("home.noSuggestedSkills")}</p>
        )}
      </section>

      {/* ── Daily tip ── */}
      <section className="home-panel__daily-tip">
        <span className="home-panel__tip-icon"><Lightbulb size={14} /></span>
        <span className="home-panel__tip-text">
          {data?.dailyTip || t("home.dailyTip")}
        </span>
      </section>
    </div>
  );
}
