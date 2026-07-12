import { useMemo, useState } from "react";
import type { MouseEvent as ReactMouseEvent } from "react";
import {
  Plus,
  Search,
  MessageSquare,
  Loader2,
  Pencil,
  X,
  Download,
} from "lucide-react";
import { useT } from "../lib/i18n";
import type { SessionMeta } from "../lib/types";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";

export interface SessionSidebarProps {
  sessions: SessionMeta[];
  activeSessionId?: string;
  isRunning: boolean;
  onSwitchSession: (id: string) => void;
  onNewSession: () => void;
  onCloseSession: (id: string) => void;
  onRenameSession: (id: string, title: string) => void;
  scope?: "global" | "project";
  workspaceRoot?: string;
}

type SessionGroup = "running" | "recent" | "earlier";

function sessionGroup(s: SessionMeta): SessionGroup {
  if (s.current || s.open) return "running";
  const now = Date.now();
  const age = now - s.lastActivityAt;
  if (age < 3600_000) return "recent"; // <1h
  return "earlier";
}

function formatTime(ms: number): string {
  const d = new Date(ms);
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

function sessionDisplayTitle(s: SessionMeta, fallback: string): string {
  return s.title || s.preview || fallback;
}

export function SessionSidebar({
  sessions,
  activeSessionId,
  isRunning,
  onSwitchSession,
  onNewSession,
  onCloseSession,
  onRenameSession,
  scope,
  workspaceRoot,
}: SessionSidebarProps) {
  const tr = useT();
  const [query, setQuery] = useState("");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const [menuSession, setMenuSession] = useState<SessionMeta | null>(null);
  const [menuPoint, setMenuPoint] = useState<ContextMenuPoint | null>(null);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    let list = sessions;
    if (scope === "project" && workspaceRoot) {
      list = list.filter((s) => s.scope === "project" && s.workspaceRoot === workspaceRoot);
    } else if (scope === "global") {
      list = list.filter((s) => (s.scope || "global") === "global");
    }
    if (q) {
      list = list.filter((s) =>
        [s.title, s.preview, s.path, s.topicTitle].some((v) => (v ?? "").toLowerCase().includes(q)),
      );
    }
    return list;
  }, [sessions, query, scope, workspaceRoot]);

  const groups = useMemo(() => {
    const map = new Map<SessionGroup, SessionMeta[]>();
    for (const s of filtered) {
      const g = sessionGroup(s);
      const arr = map.get(g) ?? [];
      arr.push(s);
      map.set(g, arr);
    }
    return map;
  }, [filtered]);

  const startRename = (s: SessionMeta) => {
    if (isRunning) return;
    setEditingId(s.path);
    setRenameDraft(s.title || s.preview || "");
  };
  const commitRename = (path: string) => {
    if (isRunning) return;
    onRenameSession(path, renameDraft.trim());
    setEditingId(null);
  };

  const openContextMenu = (event: ReactMouseEvent<HTMLElement>, s: SessionMeta) => {
    event.preventDefault();
    event.stopPropagation();
    setMenuSession(s);
    setMenuPoint(contextMenuPointFromEvent(event));
  };
  const closeContextMenu = () => {
    setMenuSession(null);
    setMenuPoint(null);
  };

  const contextMenuItems: ContextMenuItem[] = menuSession
    ? [
        {
          key: "rename",
          icon: <Pencil size={13} />,
          label: tr("history.rename"),
          disabled: isRunning,
          onSelect: () => {
            const target = menuSession;
            closeContextMenu();
            startRename(target);
          },
        },
        {
          key: "close",
          icon: <X size={13} />,
          label: tr("common.close"),
          disabled: isRunning,
          onSelect: () => {
            onCloseSession(menuSession.path);
            closeContextMenu();
          },
        },
        {
          key: "export",
          icon: <Download size={13} />,
          label: tr("topicBar.export"),
          onSelect: () => {
            closeContextMenu();
          },
        },
      ]
    : [];

  const renderGroup = (group: SessionGroup, label: string) => {
    const items = groups.get(group);
    if (!items || items.length === 0) return null;
    return (
      <section className="session-sidebar__group">
        <div className="session-sidebar__group-label">{label}</div>
        {items.map((s) => {
          const isActive = s.path === activeSessionId;
          const isEditing = editingId === s.path;
          const status = s.current ? "running" : s.open ? "idle" : "idle";
          return (
            <div
              className={`session-sidebar__item${isActive ? " session-sidebar__item--active" : ""}`}
              key={s.path}
              onClick={() => !isEditing && onSwitchSession(s.path)}
              onContextMenu={(e) => openContextMenu(e, s)}
            >
              <span className={`session-sidebar__status session-sidebar__status--${status}`}>
                {status === "running" ? <Loader2 size={12} className="session-sidebar__spin" /> : <MessageSquare size={12} />}
              </span>
              {isEditing ? (
                <input
                  className="session-sidebar__rename"
                  autoFocus
                  value={renameDraft}
                  onChange={(e) => setRenameDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") commitRename(s.path);
                    if (e.key === "Escape") setEditingId(null);
                  }}
                  onBlur={() => commitRename(s.path)}
                  placeholder={tr("history.namePlaceholder")}
                  onClick={(e) => e.stopPropagation()}
                />
              ) : (
                <span className="session-sidebar__title">{sessionDisplayTitle(s, tr("history.emptySession"))}</span>
              )}
              <span className="session-sidebar__time">{formatTime(s.lastActivityAt)}</span>
            </div>
          );
        })}
      </section>
    );
  };

  return (
    <div className="session-sidebar">
      <div className="session-sidebar__header">
        <h2 className="session-sidebar__heading">{tr("sessionSidebar.title")}</h2>
        <button
          className="session-sidebar__new-btn"
          onClick={onNewSession}
          disabled={isRunning}
          title={tr("sessionSidebar.newSession")}
        >
          <Plus size={14} />
        </button>
      </div>
      <label className="session-sidebar__search">
        <Search size={13} />
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={tr("sessionSidebar.search")}
        />
      </label>
      <div className="session-sidebar__list">
        {filtered.length === 0 ? (
          <div className="session-sidebar__empty">{tr("sessionSidebar.empty")}</div>
        ) : (
          <>
            {renderGroup("running", tr("sessionSidebar.running"))}
            {renderGroup("recent", tr("sessionSidebar.recent"))}
            {renderGroup("earlier", tr("sessionSidebar.earlier"))}
          </>
        )}
      </div>
      <ContextMenu
        open={Boolean(menuSession)}
        point={menuPoint}
        items={contextMenuItems}
        minWidth={180}
        ariaLabel={tr("sessionSidebar.title")}
        onClose={closeContextMenu}
      />
    </div>
  );
}
