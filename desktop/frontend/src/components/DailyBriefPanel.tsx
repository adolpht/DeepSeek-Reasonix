import { useCallback, useEffect, useState } from "react";
import {
  CheckCircle2,
  Mail,
  Clock,
  MessageSquare,
  RefreshCw,
  Loader,
} from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { ScheduledTaskView, SessionMeta } from "../lib/types";

// ── Data types ────────────────────────────────────────────────
interface TodoItem {
  id: string;
  title: string;
  due?: string;
}

interface MailItem {
  from: string;
  subject: string;
  date: string;
}

// ── Component ─────────────────────────────────────────────────
export function DailyBriefPanel() {
  const t = useT();
  const [todos, setTodos] = useState<TodoItem[]>([]);
  const [mails, setMails] = useState<MailItem[]>([]);
  const [tasks, setTasks] = useState<ScheduledTaskView[]>([]);
  const [sessions, setSessions] = useState<SessionMeta[]>([]);
  const [loading, setLoading] = useState<Record<string, boolean>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});

  const loadTodos = useCallback(async () => {
    setLoading((p) => ({ ...p, todos: true }));
    setErrors((p) => ({ ...p, todos: "" }));
    try {
      // CalendarPanel uses a dedicated method; for the brief we just
      // call the same backend. ListTodos returns a flat list.
      const result = await (app as any).ListTodos?.() ?? [];
      const pending = (result as any[]).filter((t: any) => t.status === "pending" || !t.completed);
      setTodos(pending.slice(0, 5).map((t: any) => ({
        id: t.id ?? t.name,
        title: t.title ?? t.name ?? t.text ?? "",
        due: t.due ?? t.dueDate,
      })));
    } catch {
      setTodos([]);
      setErrors((p) => ({ ...p, todos: t("dailyBrief.loadError") as string }));
    } finally {
      setLoading((p) => ({ ...p, todos: false }));
    }
  }, [t]);

  const loadMails = useCallback(async () => {
    setLoading((p) => ({ ...p, mails: true }));
    setErrors((p) => ({ ...p, mails: "" }));
    try {
      // Backend dispatches mcp__mail__read_mail via Controller.CallTool
      // and returns at most 5 recent INBOX summaries. When the mail plugin
      // isn't configured or returns no emails, we fall back to an empty
      // list and the section shows its empty-state placeholder.
      const result = await app.GetRecentMailSummaries();
      setMails((result ?? []).slice(0, 5).map((m) => ({
        from: m.from ?? "",
        subject: m.subject ?? "",
        date: m.date ?? "",
      })));
    } catch {
      setMails([]);
      setErrors((p) => ({ ...p, mails: t("dailyBrief.loadError") as string }));
    } finally {
      setLoading((p) => ({ ...p, mails: false }));
    }
  }, [t]);

  const loadTasks = useCallback(async () => {
    setLoading((p) => ({ ...p, tasks: true }));
    setErrors((p) => ({ ...p, tasks: "" }));
    try {
      const result = await app.ListScheduledTasks().catch(() => [] as ScheduledTaskView[]);
      setTasks((result as ScheduledTaskView[]).slice(0, 5));
    } catch {
      setTasks([]);
      setErrors((p) => ({ ...p, tasks: t("dailyBrief.loadError") as string }));
    } finally {
      setLoading((p) => ({ ...p, tasks: false }));
    }
  }, [t]);

  const loadSessions = useCallback(async () => {
    setLoading((p) => ({ ...p, sessions: true }));
    setErrors((p) => ({ ...p, sessions: "" }));
    try {
      const data = await app.GetHomePageData().catch(() => null);
      if (data?.recentTasks) {
        setSessions(data.recentTasks.slice(0, 3));
      }
    } catch {
      setSessions([]);
    } finally {
      setLoading((p) => ({ ...p, sessions: false }));
    }
  }, []);

  useEffect(() => {
    void loadTodos();
    void loadMails();
    void loadTasks();
    void loadSessions();
  }, [loadTodos, loadMails, loadTasks, loadSessions]);

  const completeTodo = async (id: string) => {
    try {
      await (app as any).CompleteTodo?.(id);
      setTodos((prev) => prev.filter((t) => t.id !== id));
    } catch { /* ignore */ }
  };

  const Section = ({
    icon,
    title,
    loading: busy,
    error,
    onRefresh,
    children,
  }: {
    icon: React.ReactNode;
    title: string;
    loading?: boolean;
    error?: string;
    onRefresh: () => void;
    children: React.ReactNode;
  }) => (
    <section className="daily-brief__section">
      <div className="daily-brief__section-head">
        <span className="daily-brief__section-icon">{icon}</span>
        <span className="daily-brief__section-title">{title}</span>
        <button className="daily-brief__refresh" onClick={onRefresh} type="button" title={t("dailyBrief.refresh") as string}>
          {busy ? <Loader size={13} className="spin" /> : <RefreshCw size={13} />}
        </button>
      </div>
      {error && <div className="daily-brief__error">{error}</div>}
      {children}
    </section>
  );

  return (
    <div className="daily-brief">
      <header className="daily-brief__header">
        <h2 className="daily-brief__title">{t("dailyBrief.title")}</h2>
        <span className="daily-brief__date">
          {new Date().toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric" })}
        </span>
      </header>

      <Section
        icon={<CheckCircle2 size={16} />}
        title={t("dailyBrief.pendingTodos") as string}
        loading={loading.todos}
        error={errors.todos}
        onRefresh={loadTodos}
      >
        {todos.length === 0 ? (
          <div className="daily-brief__empty">{t("dailyBrief.noTodos") as string}</div>
        ) : (
          <ul className="daily-brief__list">
            {todos.map((todo) => (
              <li key={todo.id} className="daily-brief__item">
                <span className="daily-brief__item-text">{todo.title}</span>
                {todo.due && <span className="daily-brief__item-meta">{todo.due}</span>}
                <button
                  className="daily-brief__item-action"
                  onClick={() => void completeTodo(todo.id)}
                  type="button"
                  title={t("dailyBrief.completeTodo") as string}
                >
                  <CheckCircle2 size={14} />
                </button>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section
        icon={<Mail size={16} />}
        title={t("dailyBrief.recentMail") as string}
        loading={loading.mails}
        error={errors.mails}
        onRefresh={loadMails}
      >
        {mails.length === 0 ? (
          <div className="daily-brief__empty">{t("dailyBrief.noMail") as string}</div>
        ) : (
          <ul className="daily-brief__list">
            {mails.map((m, i) => (
              <li key={i} className="daily-brief__item">
                <span className="daily-brief__item-text">{m.subject}</span>
                <span className="daily-brief__item-meta">{m.from}</span>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section
        icon={<Clock size={16} />}
        title={t("dailyBrief.scheduledTasks") as string}
        loading={loading.tasks}
        error={errors.tasks}
        onRefresh={loadTasks}
      >
        {tasks.length === 0 ? (
          <div className="daily-brief__empty">{t("dailyBrief.noTasks") as string}</div>
        ) : (
          <ul className="daily-brief__list">
            {tasks.map((task) => (
              <li key={task.id ?? task.name} className="daily-brief__item">
                <span className="daily-brief__item-text">{task.name ?? task.skill}</span>
                <span className="daily-brief__item-meta">{task.cron}</span>
              </li>
            ))}
          </ul>
        )}
      </Section>

      <Section
        icon={<MessageSquare size={16} />}
        title={t("dailyBrief.recentSessions") as string}
        loading={loading.sessions}
        error={errors.sessions}
        onRefresh={loadSessions}
      >
        {sessions.length === 0 ? (
          <div className="daily-brief__empty">{t("dailyBrief.noSessions") as string}</div>
        ) : (
          <ul className="daily-brief__list">
            {sessions.map((s) => (
              <li key={s.path} className="daily-brief__item">
                <span className="daily-brief__item-text">{s.title || s.preview}</span>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </div>
  );
}
