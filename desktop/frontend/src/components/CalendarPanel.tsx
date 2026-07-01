import { useCallback, useEffect, useMemo, useState } from "react";
import { Calendar, ChevronLeft, ChevronRight, Plus, Check, Clock } from "lucide-react";
import { useT } from "../lib/i18n";
import { app } from "../lib/bridge";
import type { TodoView } from "../lib/types";

// ── Types ────────────────────────────────────────────────────────
type ViewMode = "month" | "week" | "day";

interface CalendarPanelProps {
  tabId?: string;
  onNavigate?: (page: string) => void;
}

// ── Helpers ──────────────────────────────────────────────────────
function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function isSameDay(a: Date, b: Date): boolean {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();
}

function formatISODate(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function getMonthGrid(year: number, month: number): Date[] {
  const first = new Date(year, month, 1);
  const startDow = first.getDay(); // 0=Sun
  const cells: Date[] = [];
  for (let i = 0; i < startDow; i++) {
    cells.push(new Date(year, month, 1 - startDow + i));
  }
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  for (let d = 1; d <= daysInMonth; d++) {
    cells.push(new Date(year, month, d));
  }
  while (cells.length < 42) {
    cells.push(new Date(year, month + 1, cells.length - startDow - daysInMonth + 1));
  }
  return cells;
}

function getWeekDays(anchor: Date): Date[] {
  const dow = anchor.getDay();
  const start = new Date(anchor.getFullYear(), anchor.getMonth(), anchor.getDate() - dow);
  return Array.from({ length: 7 }, (_, i) => new Date(start.getFullYear(), start.getMonth(), start.getDate() + i));
}

const PRIORITY_COLORS: Record<string, string> = {
  high: "#e0696a",
  urgent: "#e5484d",
  medium: "#d9a441",
  low: "#74b87a",
};

// ── Component ────────────────────────────────────────────────────
export function CalendarPanel(_props: CalendarPanelProps) {
  const t = useT();
  const today = useMemo(() => startOfDay(new Date()), []);

  // View state
  const [view, setView] = useState<ViewMode>("month");
  const [anchorDate, setAnchorDate] = useState<Date>(today);
  const [selectedDate, setSelectedDate] = useState<Date>(today);

  // Data state
  const [todos, setTodos] = useState<TodoView[]>([]);
  const [showAddForm, setShowAddForm] = useState(false);
  const [newTitle, setNewTitle] = useState("");
  const [newPriority, setNewPriority] = useState<string>("medium");

  // Fetch todos
  const fetchTodos = useCallback(() => {
    app.ListTodos().then(setTodos).catch(() => {});
  }, []);

  useEffect(() => {
    fetchTodos();
  }, [fetchTodos]);

  // Derived: todos grouped by date
  const todosByDate = useMemo(() => {
    const map = new Map<string, TodoView[]>();
    for (const todo of todos) {
      if (todo.dueDate) {
        const existing = map.get(todo.dueDate) ?? [];
        existing.push(todo);
        map.set(todo.dueDate, existing);
      }
    }
    return map;
  }, [todos]);

  // Todos for selected date
  const selectedDateKey = formatISODate(selectedDate);
  const selectedTodos = useMemo(() => {
    const fromMap = todosByDate.get(selectedDateKey) ?? [];
    return fromMap.sort((a, b) => {
      const pOrder: Record<string, number> = { urgent: 0, high: 1, medium: 2, low: 3 };
      return (pOrder[a.priority] ?? 3) - (pOrder[b.priority] ?? 3);
    });
  }, [todosByDate, selectedDateKey]);

  // Navigation helpers
  const prevMonth = () => setAnchorDate((d) => new Date(d.getFullYear(), d.getMonth() - 1, 1));
  const nextMonth = () => setAnchorDate((d) => new Date(d.getFullYear(), d.getMonth() + 1, 1));
  const prevWeek = () => setAnchorDate((d) => new Date(d.getFullYear(), d.getMonth(), d.getDate() - 7));
  const nextWeek = () => setAnchorDate((d) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + 7));
  const prevDay = () => setAnchorDate((d) => new Date(d.getFullYear(), d.getMonth(), d.getDate() - 1));
  const nextDay = () => setAnchorDate((d) => new Date(d.getFullYear(), d.getMonth(), d.getDate() + 1));

  const goToday = () => {
    setAnchorDate(today);
    setSelectedDate(today);
  };

  // Month/Year display
  const monthLabel = anchorDate.toLocaleDateString(undefined, { year: "numeric", month: "long" });
  const weekLabel = (() => {
    const days = getWeekDays(anchorDate);
    const s = days[0];
    const e = days[6];
    return `${s.getMonth() + 1}/${s.getDate()} – ${e.getMonth() + 1}/${e.getDate()}`;
  })();
  const dayLabel = anchorDate.toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" });

  // Toggle todo completion
  const toggleTodo = async (todo: TodoView) => {
    const nextStatus = todo.status === "completed" ? "pending" : "completed";
    try {
      await app.UpdateTodo(todo.id, todo.title, todo.description, todo.dueDate, todo.priority, nextStatus);
      fetchTodos();
    } catch {
      /* silent */
    }
  };

  // Add todo
  const handleAddTodo = async () => {
    const title = newTitle.trim();
    if (!title) return;
    try {
      await app.CreateTodo(title, "", selectedDateKey, newPriority);
      setNewTitle("");
      setNewPriority("medium");
      setShowAddForm(false);
      fetchTodos();
    } catch {
      /* silent */
    }
  };

  // Weekday header
  const WEEKDAYS = useMemo(() => {
    const base = new Date(2026, 5, 28); // Sun
    return Array.from({ length: 7 }, (_, i) => {
      const d = new Date(base.getFullYear(), base.getMonth(), base.getDate() + i);
      return d.toLocaleDateString(undefined, { weekday: "narrow" });
    });
  }, []);

  // ── Render ─────────────────────────────────────────────────────
  return (
    <div className="calendar-panel">
      {/* Header: view switcher + navigation */}
      <div className="calendar-panel__header">
        <div className="calendar-panel__view-switcher">
          {(["month", "week", "day"] as ViewMode[]).map((v) => (
            <button
              key={v}
              className={`calendar-panel__view-btn${view === v ? " calendar-panel__view-btn--active" : ""}`}
              onClick={() => setView(v)}
            >
              {t(`calendarPanel.${v}`)}
            </button>
          ))}
        </div>
        <div className="calendar-panel__nav">
          <button className="calendar-panel__nav-btn" onClick={view === "month" ? prevMonth : view === "week" ? prevWeek : prevDay}>
            <ChevronLeft size={14} />
          </button>
          <button className="calendar-panel__nav-label" onClick={goToday}>
            <Calendar size={13} />
            <span>{view === "month" ? monthLabel : view === "week" ? weekLabel : dayLabel}</span>
          </button>
          <button className="calendar-panel__nav-btn" onClick={view === "month" ? nextMonth : view === "week" ? nextWeek : nextDay}>
            <ChevronRight size={14} />
          </button>
        </div>
      </div>

      {/* Calendar grid */}
      <div className="calendar-panel__body">
        {view === "month" && (
          <>
            {/* Weekday header */}
            <div className="calendar-panel__weekdays">
              {WEEKDAYS.map((wd, i) => (
                <span key={i} className="calendar-panel__weekday">{wd}</span>
              ))}
            </div>
            {/* Month grid */}
            <div className="calendar-panel__grid">
              {getMonthGrid(anchorDate.getFullYear(), anchorDate.getMonth()).map((date, i) => {
                const key = formatISODate(date);
                const dayTodos = todosByDate.get(key) ?? [];
                const isCurrentMonth = date.getMonth() === anchorDate.getMonth();
                const isToday = isSameDay(date, today);
                const isSelected = isSameDay(date, selectedDate);
                return (
                  <button
                    key={i}
                    className={[
                      "calendar-panel__day",
                      !isCurrentMonth && "calendar-panel__day--other",
                      isToday && "calendar-panel__day--today",
                      isSelected && "calendar-panel__day--selected",
                    ]
                      .filter(Boolean)
                      .join(" ")}
                    onClick={() => setSelectedDate(date)}
                  >
                    <span className="calendar-panel__day-num">{date.getDate()}</span>
                    {dayTodos.length > 0 && (
                      <span className="calendar-panel__day-dots">
                        {dayTodos.slice(0, 3).map((_, di) => (
                          <span key={di} className="calendar-panel__day-dot" />
                        ))}
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          </>
        )}

        {view === "week" && (
          <div className="calendar-panel__week">
            {getWeekDays(anchorDate).map((date, i) => {
              const key = formatISODate(date);
              const dayTodos = todosByDate.get(key) ?? [];
              const isToday = isSameDay(date, today);
              const isSelected = isSameDay(date, selectedDate);
              return (
                <div
                  key={i}
                  className={[
                    "calendar-panel__week-col",
                    isToday && "calendar-panel__week-col--today",
                    isSelected && "calendar-panel__week-col--selected",
                  ]
                    .filter(Boolean)
                    .join(" ")}
                  onClick={() => setSelectedDate(date)}
                >
                  <div className="calendar-panel__week-header">
                    <span className="calendar-panel__week-wd">{WEEKDAYS[i]}</span>
                    <span className={`calendar-panel__week-date${isToday ? " calendar-panel__week-date--today" : ""}`}>
                      {date.getDate()}
                    </span>
                  </div>
                  <div className="calendar-panel__week-todos">
                    {dayTodos.slice(0, 4).map((todo) => (
                      <div
                        key={todo.id}
                        className="calendar-panel__week-todo"
                        style={{ borderLeftColor: PRIORITY_COLORS[todo.priority] ?? PRIORITY_COLORS.medium }}
                      >
                        {todo.title}
                      </div>
                    ))}
                    {dayTodos.length > 4 && (
                      <span className="calendar-panel__week-more">+{dayTodos.length - 4}</span>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {view === "day" && (
          <div className="calendar-panel__day-view">
            <div className="calendar-panel__day-view-header">
              <span>{dayLabel}</span>
              {isSameDay(anchorDate, today) && (
                <span className="calendar-panel__today-badge">{t("calendarPanel.today")}</span>
              )}
            </div>
            {selectedTodos.length === 0 ? (
              <div className="calendar-panel__empty">{t("calendarPanel.noTodos")}</div>
            ) : (
              selectedTodos.map((todo) => (
                <TodoItem key={todo.id} todo={todo} onToggle={() => toggleTodo(todo)} />
              ))
            )}
          </div>
        )}
      </div>

      {/* Todo list for selected date (shown in month/week view) */}
      {(view === "month" || view === "week") && (
        <div className="calendar-panel__todos">
          <div className="calendar-panel__todos-header">
            <span className="calendar-panel__todos-date">
              {selectedDate.toLocaleDateString(undefined, { month: "short", day: "numeric" })}
            </span>
            <button className="calendar-panel__add-btn" onClick={() => setShowAddForm((v) => !v)}>
              <Plus size={13} />
              <span>{t("calendarPanel.addTodo")}</span>
            </button>
          </div>

          {showAddForm && (
            <div className="calendar-panel__add-form">
              <input
                className="calendar-panel__add-input"
                value={newTitle}
                onChange={(e) => setNewTitle(e.target.value)}
                placeholder={t("calendarPanel.addTodo")}
                onKeyDown={(e) => e.key === "Enter" && handleAddTodo()}
                autoFocus
              />
              <div className="calendar-panel__add-row">
                <select className="calendar-panel__add-select" value={newPriority} onChange={(e) => setNewPriority(e.target.value)}>
                  <option value="high">{t("calendarPanel.priorityHigh")}</option>
                  <option value="medium">{t("calendarPanel.priorityMedium")}</option>
                  <option value="low">{t("calendarPanel.priorityLow")}</option>
                </select>
                <button className="calendar-panel__add-submit" onClick={handleAddTodo}>
                  <Plus size={13} />
                </button>
              </div>
            </div>
          )}

          {selectedTodos.length === 0 ? (
            <div className="calendar-panel__empty">{t("calendarPanel.noTodos")}</div>
          ) : (
            selectedTodos.map((todo) => (
              <TodoItem key={todo.id} todo={todo} onToggle={() => toggleTodo(todo)} />
            ))
          )}
        </div>
      )}
    </div>
  );
}

// ── Todo item sub-component ──────────────────────────────────────
function TodoItem({ todo, onToggle }: { todo: TodoView; onToggle: () => void }) {
  const isDone = todo.status === "completed";
  return (
    <div className={`calendar-panel__todo${isDone ? " calendar-panel__todo--done" : ""}`}>
      <button className="calendar-panel__todo-check" onClick={onToggle}>
        {isDone ? <Check size={12} /> : <span className="calendar-panel__todo-circle" />}
      </button>
      <span
        className="calendar-panel__todo-priority"
        style={{ backgroundColor: PRIORITY_COLORS[todo.priority] ?? PRIORITY_COLORS.medium }}
      />
      <span className="calendar-panel__todo-title">{todo.title}</span>
      {todo.dueDate && (
        <span className="calendar-panel__todo-due">
          <Clock size={10} />
          {todo.dueDate}
        </span>
      )}
    </div>
  );
}
