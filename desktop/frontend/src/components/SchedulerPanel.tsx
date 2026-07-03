import { useCallback, useEffect, useState } from "react";
import { Clock, Pause, Pencil, Play, Plus, RefreshCw, Trash2 } from "lucide-react";
import { useT } from "../lib/i18n";
import { app } from "../lib/bridge";
import type { RecipeView, ScheduledTaskView } from "../lib/types";

interface SchedulerPanelProps {
  tabId?: string;
}

// Cron human-readable descriptions for common patterns.
function describeCron(cron: string): string {
  const t = useT();
  const patterns: Record<string, () => string> = {
    "0 * * * *": () => t("scheduler.cronEveryHour"),
    "0 0 * * *": () => t("scheduler.cronEveryDay"),
    "0 9 * * *": () => t("scheduler.cronEveryDay9am"),
    "0 0 * * 1": () => t("scheduler.cronEveryMonday"),
    "0 0 1 * *": () => t("scheduler.cronEveryMonthFirst"),
  };
  const fn = patterns[cron];
  if (fn) return fn();
  return cron;
}

// Cron shortcut presets.
const CRON_PRESETS: { key: "scheduler.cronEveryHour" | "scheduler.cronEveryDay" | "scheduler.cronEveryDay9am" | "scheduler.cronEveryMonday" | "scheduler.cronEveryMonthFirst"; cron: string }[] = [
  { key: "scheduler.cronEveryHour", cron: "0 * * * *" },
  { key: "scheduler.cronEveryDay", cron: "0 0 * * *" },
  { key: "scheduler.cronEveryDay9am", cron: "0 9 * * *" },
  { key: "scheduler.cronEveryMonday", cron: "0 0 * * 1" },
  { key: "scheduler.cronEveryMonthFirst", cron: "0 0 1 * *" },
];

function formatTime(ms: number, t: (key: "scheduler.justNow" | "scheduler.minutesLater" | "scheduler.hoursLater", vars?: Record<string, string | number>) => string): string {
  if (!ms) return "—";
  const d = new Date(ms);
  const now = Date.now();
  const diff = ms - now;
  if (diff > 0 && diff < 60_000) return t("scheduler.justNow");
  if (diff > 0 && diff < 3_600_000) return t("scheduler.minutesLater", { n: Math.floor(diff / 60_000) });
  if (diff > 0 && diff < 86_400_000) return t("scheduler.hoursLater", { n: Math.floor(diff / 3_600_000) });
  return d.toLocaleString();
}

interface FormData {
  name: string;
  cron: string;
  skill: string;
  parameters: string;
  enabled: boolean;
}

const EMPTY_FORM: FormData = { name: "", cron: "0 * * * *", skill: "", parameters: "{}", enabled: true };

export function SchedulerPanel(_props: SchedulerPanelProps) {
  const t = useT();
  const [tasks, setTasks] = useState<ScheduledTaskView[]>([]);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [form, setForm] = useState<FormData>({ ...EMPTY_FORM });
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [recipes, setRecipes] = useState<RecipeView[]>([]);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const list = await app.ListScheduledTasks();
      setTasks(list);
    } finally {
      setLoading(false);
    }
  }, []);

  const refreshRecipes = useCallback(async () => {
    try {
      const list = await app.ListRecipes();
      setRecipes(list);
    } catch {
      // Recipes are optional; ignore errors
    }
  }, []);

  useEffect(() => {
    refresh();
    refreshRecipes();
  }, [refresh, refreshRecipes]);

  // Open the form for a new task.
  const handleAdd = () => {
    setEditing(null);
    setForm({ ...EMPTY_FORM });
    setShowForm(true);
  };

  // Open the form for editing an existing task.
  const handleEdit = (task: ScheduledTaskView) => {
    setEditing(task.id);
    setForm({
      name: task.name,
      cron: task.cron,
      skill: task.skill,
      parameters: task.parameters,
      enabled: task.enabled,
    });
    setShowForm(true);
  };

  // Submit the form (create or update).
  const handleSubmit = async () => {
    if (!form.name.trim() || !form.cron.trim() || !form.skill.trim()) return;
    if (editing) {
      await app.UpdateScheduledTask(editing, form.name, form.cron, form.skill, form.parameters, form.enabled);
    } else {
      await app.CreateScheduledTask(form.name, form.cron, form.skill, form.parameters);
    }
    setShowForm(false);
    setEditing(null);
    setForm({ ...EMPTY_FORM });
    refresh();
  };

  // Toggle enabled/disabled.
  const handleToggle = async (task: ScheduledTaskView) => {
    await app.UpdateScheduledTask(task.id, task.name, task.cron, task.skill, task.parameters, !task.enabled);
    refresh();
  };

  // Delete a task.
  const handleDelete = async (id: string) => {
    await app.DeleteScheduledTask(id);
    setConfirmDelete(null);
    refresh();
  };

  // Apply a cron preset.
  const applyPreset = (cron: string) => {
    setForm((f) => ({ ...f, cron }));
  };

  return (
    <div className="scheduler-panel">
      {/* Header */}
      <div className="scheduler-panel__header">
        <div className="scheduler-panel__title-row">
          <Clock size={15} className="scheduler-panel__icon" />
          <span className="scheduler-panel__title">{t("scheduler.title")}</span>
        </div>
        <div className="scheduler-panel__actions">
          <button className="scheduler-panel__btn scheduler-panel__btn--icon" onClick={refresh} title={t("scheduler.refresh")}>
            <RefreshCw size={14} />
          </button>
          <button className="scheduler-panel__btn scheduler-panel__btn--primary" onClick={handleAdd}>
            <Plus size={14} />
            {t("scheduler.addTask")}
          </button>
        </div>
      </div>

      {/* Task list */}
      <div className="scheduler-panel__list">
        {loading && tasks.length === 0 && (
          <div className="scheduler-panel__empty">{t("common.loading")}</div>
        )}
        {!loading && tasks.length === 0 && (
          <div className="scheduler-panel__empty">{t("scheduler.noTasks")}</div>
        )}
        {tasks.map((task) => (
          <div
            key={task.id}
            className={`scheduler-panel__task${task.enabled ? "" : " scheduler-panel__task--disabled"}`}
          >
            <div className="scheduler-panel__task-head">
              <span className="scheduler-panel__task-name">{task.name}</span>
              <span className={`scheduler-panel__badge${task.enabled ? " scheduler-panel__badge--on" : " scheduler-panel__badge--off"}`}>
                {task.enabled ? t("scheduler.enabled") : t("scheduler.disabled")}
              </span>
            </div>
            <div className="scheduler-panel__task-meta">
              <span className="scheduler-panel__task-cron" title={task.cron}>
                <Clock size={12} /> {describeCron(task.cron)}
              </span>
              <span className="scheduler-panel__task-skill">{task.skill}</span>
            </div>
            <div className="scheduler-panel__task-times">
              <span className="scheduler-panel__task-time">
                {t("scheduler.lastRun")}: {formatTime(task.lastRun, t)}
              </span>
              <span className="scheduler-panel__task-time">
                {t("scheduler.nextRun")}: {formatTime(task.nextRun, t)}
              </span>
            </div>
            <div className="scheduler-panel__task-actions">
              <button className="scheduler-panel__btn scheduler-panel__btn--small" onClick={() => handleEdit(task)} title={t("common.edit")}>
                <Pencil size={13} />
              </button>
              <button
                className="scheduler-panel__btn scheduler-panel__btn--small"
                onClick={() => handleToggle(task)}
                title={task.enabled ? t("scheduler.pause") : t("scheduler.resume")}
              >
                {task.enabled ? <Pause size={13} /> : <Play size={13} />}
              </button>
              {confirmDelete === task.id ? (
                <button className="scheduler-panel__btn scheduler-panel__btn--small scheduler-panel__btn--danger" onClick={() => handleDelete(task.id)}>
                  {t("scheduler.confirmDelete")}
                </button>
              ) : (
                <button className="scheduler-panel__btn scheduler-panel__btn--small" onClick={() => setConfirmDelete(task.id)} title={t("common.delete")}>
                  <Trash2 size={13} />
                </button>
              )}
            </div>
          </div>
        ))}
      </div>

      {/* Add/Edit form */}
      {showForm && (
        <div className="scheduler-panel__overlay" onClick={() => setShowForm(false)}>
          <div className="scheduler-panel__form" onClick={(e) => e.stopPropagation()}>
            <div className="scheduler-panel__form-title">
              {editing ? t("scheduler.editTask") : t("scheduler.addTask")}
            </div>

            <label className="scheduler-panel__label">
              {t("scheduler.taskName")}
              <input
                className="scheduler-panel__input"
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                placeholder={t("scheduler.taskNamePlaceholder")}
              />
            </label>

            <label className="scheduler-panel__label">
              {t("scheduler.cronExpression")}
              <input
                className="scheduler-panel__input"
                value={form.cron}
                onChange={(e) => setForm((f) => ({ ...f, cron: e.target.value }))}
                placeholder="0 * * * *"
              />
              <span className="scheduler-panel__hint">{describeCron(form.cron)}</span>
            </label>

            <div className="scheduler-panel__presets">
              {CRON_PRESETS.map((p) => (
                <button
                  key={p.cron}
                  className={`scheduler-panel__preset${form.cron === p.cron ? " scheduler-panel__preset--active" : ""}`}
                  onClick={() => applyPreset(p.cron)}
                >
                  {t(p.key)}
                </button>
              ))}
            </div>

            {recipes.length > 0 && (
              <label className="scheduler-panel__label">
                {t("scheduler.fromRecipe")}
                <select
                  className="scheduler-panel__input"
                  value=""
                  onChange={(e) => {
                    const r = recipes.find((rec) => rec.name === e.target.value);
                    if (r) {
                      setForm((f) => ({
                        ...f,
                        name: f.name || r.name,
                        skill: r.skill,
                        parameters: r.params || "{}",
                      }));
                    }
                  }}
                >
                  <option value="">{t("scheduler.selectRecipe")}</option>
                  {recipes.map((r) => (
                    <option key={r.name} value={r.name}>
                      {r.name} — {r.description || r.skill}
                    </option>
                  ))}
                </select>
              </label>
            )}

            <label className="scheduler-panel__label">
              {t("scheduler.skill")}
              <input
                className="scheduler-panel__input"
                value={form.skill}
                onChange={(e) => setForm((f) => ({ ...f, skill: e.target.value }))}
                placeholder={t("scheduler.skillPlaceholder")}
              />
            </label>

            <label className="scheduler-panel__label">
              {t("scheduler.parameters")}
              <textarea
                className="scheduler-panel__textarea"
                value={form.parameters}
                onChange={(e) => setForm((f) => ({ ...f, parameters: e.target.value }))}
                placeholder='{"key": "value"}'
                rows={3}
              />
            </label>

            {editing && (
              <label className="scheduler-panel__label scheduler-panel__label--row">
                <input
                  type="checkbox"
                  checked={form.enabled}
                  onChange={(e) => setForm((f) => ({ ...f, enabled: e.target.checked }))}
                />
                {t("scheduler.enabled")}
              </label>
            )}

            <div className="scheduler-panel__form-actions">
              <button className="scheduler-panel__btn" onClick={() => setShowForm(false)}>
                {t("common.cancel")}
              </button>
              <button
                className="scheduler-panel__btn scheduler-panel__btn--primary"
                onClick={handleSubmit}
                disabled={!form.name.trim() || !form.cron.trim() || !form.skill.trim()}
              >
                {t("common.save")}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
