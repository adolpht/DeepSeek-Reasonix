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
  Presentation,
  FileStack,
  Search,
  FileText,
  Languages,
  Lightbulb,
  Clock,
  Zap,
  ArrowRight,
  Trophy,
} from "lucide-react";
import type { WorkspaceType, SessionMeta, HomePageData } from "../lib/types";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { isOnboardingTaskPending } from "./OnboardingOverlay";

// ── Quick action definitions per workspace type ────────────────
interface QuickAction {
  key: string;
  icon: React.ReactNode;
  color: string;
  skillCommand: string;
}

const QUICK_ACTIONS: Record<WorkspaceType, QuickAction[]> = {
  coding: [
    { key: "analyzeProject", icon: <Code2 size={20} />, color: "blue", skillCommand: "explore" },
    { key: "reviewCode", icon: <GitCompare size={20} />, color: "green", skillCommand: "review" },
    { key: "fixBug", icon: <Bug size={20} />, color: "red", skillCommand: "" },
    { key: "generateTests", icon: <TestTube2 size={20} />, color: "purple", skillCommand: "generate-tests" },
  ],
  office: [
    { key: "docWrite", icon: <FileText size={20} />, color: "blue", skillCommand: "minimax-docx" },
    { key: "sheetCreate", icon: <Table2 size={20} />, color: "green", skillCommand: "minimax-xlsx" },
    { key: "makePpt", icon: <Presentation size={20} />, color: "orange", skillCommand: "pptx-generator" },
    { key: "organizeDoc", icon: <FileStack size={20} />, color: "teal", skillCommand: "sheet-clean" },
  ],
  assistant: [
    { key: "searchResearch", icon: <Search size={20} />, color: "blue", skillCommand: "research-report" },
    { key: "summarizeArticle", icon: <FileText size={20} />, color: "green", skillCommand: "" },
    { key: "translateContent", icon: <Languages size={20} />, color: "purple", skillCommand: "" },
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
  if (hour >= 5 && hour < 12) return <Sun size={28} />;
  if (hour >= 12 && hour < 18) return <CloudSun size={28} />;
  return <Moon size={28} />;
}

// ── Relative time formatting ───────────────────────────────────
function relativeTime(
  ms: number,
  t: (key: "home.justNow" | "home.minutesAgo" | "home.hoursAgo" | "home.daysAgo", params?: Record<string, string | number>) => string
): string {
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
  const [onboardingPending, setOnboardingPending] = useState(() => isOnboardingTaskPending());

  useEffect(() => {
    setOnboardingPending(isOnboardingTaskPending());
  }, []);

  useEffect(() => {
    let cancelled = false;
    app.GetHomePageData().then((result) => {
      if (!cancelled) setData(result);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const actions = QUICK_ACTIONS[workspaceType];

  const handleQuickAction = (action: QuickAction) => {
    onSwitchMode(workspaceType);
    if (action.skillCommand) {
      onActivateSkill(action.skillCommand);
    }
  };

  const recentTasks = data?.recentTasks ?? [];
  const suggestedSkills = data?.suggestedSkills ?? [];

  return (
    <div className="home-panel">
      {/* ── Hero / Greeting ── */}
      <header className="home-panel__hero">
        <div className="home-panel__hero-icon">{getGreetingIcon()}</div>
        <div className="home-panel__hero-text">
          <h1 className="home-panel__hero-title">{t(getGreetingKey())}</h1>
          <p className="home-panel__hero-subtitle">
            {t(`home.roleDescription.${workspaceType}` as any)}
          </p>
        </div>
        {onboardingPending ? (
          <div className="home-panel__onboarding" data-variant="progress">
            <div className="home-panel__progress-bar">
              <div className="home-panel__progress-fill" style={{ width: "50%" }} />
            </div>
            <span className="home-panel__progress-text">{t("home.onboardingProgress")}</span>
          </div>
        ) : (
          <div className="home-panel__onboarding" data-variant="done">
            <Trophy size={14} />
            <span>{t("home.onboardingComplete")}</span>
          </div>
        )}
      </header>

      {/* ── Quick actions ── */}
      <section className="home-panel__section">
        <div className="home-panel__section-head">
          <h3 className="home-panel__section-title">{t("home.quickActions")}</h3>
        </div>
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
              <ArrowRight size={14} className="home-panel__action-arrow" />
            </button>
          ))}
        </div>
      </section>

      {/* ── Two-column: recent tasks + suggested skills ── */}
      <div className="home-panel__columns">
        <section className="home-panel__section">
          <div className="home-panel__section-head">
            <h3 className="home-panel__section-title">{t("home.recentTasks")}</h3>
          </div>
          {recentTasks.length > 0 ? (
            <ul className="home-panel__task-list">
              {recentTasks.slice(0, 5).map((task: SessionMeta) => (
                <li key={task.path}>
                  <button
                    className="home-panel__task-item"
                    onClick={() => onNavigateToSession(task.path)}
                  >
                    <span
                      className="home-panel__task-dot"
                      data-state={task.open ? "open" : task.current ? "current" : "closed"}
                    />
                    <span className="home-panel__task-title">
                      {task.title || task.preview}
                    </span>
                    <span className="home-panel__task-time">
                      <Clock size={11} />
                      {relativeTime(task.lastActivityAt, t)}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="home-panel__empty">{t("home.noRecentTasks")}</p>
          )}
        </section>

        <section className="home-panel__section">
          <div className="home-panel__section-head">
            <h3 className="home-panel__section-title">{t("home.suggestedSkills")}</h3>
          </div>
          {suggestedSkills.length > 0 ? (
            <ul className="home-panel__skill-list">
              {suggestedSkills.map((skill) => (
                <li key={skill.name}>
                  <button
                    className="home-panel__skill-item"
                    onClick={() => onActivateSkill(skill.name)}
                  >
                    <Zap size={14} className="home-panel__skill-icon" />
                    <span className="home-panel__skill-info">
                      <span className="home-panel__skill-name">{skill.name}</span>
                      <span className="home-panel__skill-desc">{skill.description}</span>
                    </span>
                    <ArrowRight size={13} className="home-panel__skill-arrow" />
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p className="home-panel__empty">{t("home.noSuggestedSkills")}</p>
          )}
        </section>
      </div>

      {/* ── Daily tip ── */}
      <footer className="home-panel__tip">
        <span className="home-panel__tip-icon">
          <Lightbulb size={15} />
        </span>
        <span className="home-panel__tip-text">
          {data?.dailyTip || t("home.dailyTip")}
        </span>
      </footer>
    </div>
  );
}
