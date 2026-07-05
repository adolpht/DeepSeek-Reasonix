import { useCallback, useEffect, useState } from "react";
import {
  MessageSquare,
  RefreshCw,
  Loader,
  CheckCircle2,
  AlertCircle,
  Clock,
  ChevronRight,
  Inbox,
} from "lucide-react";
import { app } from "../lib/bridge";
import { useT, type DictKey } from "../lib/i18n";
import type {
  IMSessionDetailView,
  IMSessionStatus,
  IMSessionView,
  HistoryMessage,
} from "../lib/types";

// ── Status badge config ──────────────────────────────────────
const STATUS_CONFIG: Record<
  IMSessionStatus,
  { icon: typeof CheckCircle2; className: string; labelKey: DictKey }
> = {
  pending: { icon: Clock, className: "im-sessions__status--pending", labelKey: "imSessions.statusPending" },
  processing: { icon: Loader, className: "im-sessions__status--processing", labelKey: "imSessions.statusProcessing" },
  done: { icon: CheckCircle2, className: "im-sessions__status--done", labelKey: "imSessions.statusDone" },
  failed: { icon: AlertCircle, className: "im-sessions__status--failed", labelKey: "imSessions.statusFailed" },
};

const STATUS_FILTERS: Array<{ value: IMSessionStatus | ""; labelKey: DictKey }> = [
  { value: "", labelKey: "imSessions.filterAll" },
  { value: "processing", labelKey: "imSessions.filterProcessing" },
  { value: "done", labelKey: "imSessions.filterDone" },
  { value: "failed", labelKey: "imSessions.filterFailed" },
];

const PLATFORM_LABELS: Record<string, string> = {
  dingtalk: "钉钉",
  feishu: "飞书",
  wecom: "企业微信",
};

// ── Component ────────────────────────────────────────────────
export function IMSessionsPanel() {
  const t = useT();
  const [sessions, setSessions] = useState<IMSessionView[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [detail, setDetail] = useState<IMSessionDetailView | null>(null);
  const [statusFilter, setStatusFilter] = useState<IMSessionStatus | "">("");
  const [loading, setLoading] = useState(false);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [error, setError] = useState("");
  const [transcript, setTranscript] = useState<HistoryMessage[] | null>(null);

  // Load session list
  const loadSessions = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await app.ListIMSessions(statusFilter);
      setSessions(result ?? []);
    } catch (e) {
      setError(t("imSessions.loadError") as string);
      setSessions([]);
    } finally {
      setLoading(false);
    }
  }, [statusFilter, t]);

  // Load detail when a session is selected
  const loadDetail = useCallback(async (id: string) => {
    setLoadingDetail(true);
    setTranscript(null);
    try {
      const d = await app.GetIMSession(id);
      setDetail(d);
      // If the session links to an agent transcript, load it for the trace view
      if (d?.agentSession) {
        try {
          const msgs = await app.PreviewSession(d.agentSession);
          setTranscript(msgs ?? []);
        } catch {
          setTranscript([]);
        }
      }
    } catch {
      setDetail(null);
    } finally {
      setLoadingDetail(false);
    }
  }, []);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  const totalCount = sessions.length;

  return (
    <div className="im-sessions">
      <header className="im-sessions__header">
        <h2 className="im-sessions__title">
          <MessageSquare size={15} />
          <span>{t("imSessions.title") as string}</span>
        </h2>
        <button
          type="button"
          className="im-sessions__refresh"
          onClick={loadSessions}
          title={t("imSessions.refresh") as string}
        >
          {loading ? <Loader size={13} className="spin" /> : <RefreshCw size={13} />}
        </button>
      </header>

      {/* Status filter chips */}
      <div className="im-sessions__filters">
        {STATUS_FILTERS.map((f) => (
          <button
            key={f.value || "all"}
            type="button"
            className={`im-sessions__filter${statusFilter === f.value ? " im-sessions__filter--active" : ""}`}
            onClick={() => setStatusFilter(f.value)}
          >
            {t(f.labelKey) as string}
          </button>
        ))}
      </div>

      {error && <div className="im-sessions__error">{error}</div>}

      <div className="im-sessions__body">
        {/* Session list */}
        <div className="im-sessions__list">
          {totalCount === 0 && !loading ? (
            <div className="im-sessions__empty">
              <Inbox size={28} />
              <p>{t("imSessions.empty") as string}</p>
            </div>
          ) : (
            sessions.map((s) => {
              const cfg = STATUS_CONFIG[s.status] ?? STATUS_CONFIG.processing;
              const StatusIcon = cfg.icon;
              const isSelected = s.id === selectedId;
              return (
                <button
                  key={s.id}
                  type="button"
                  className={`im-sessions__row${isSelected ? " im-sessions__row--selected" : ""}`}
                  onClick={() => {
                    setSelectedId(s.id);
                    loadDetail(s.id);
                  }}
                >
                  <div className="im-sessions__row-head">
                    <span className={`im-sessions__status ${cfg.className}`}>
                      <StatusIcon size={11} className={s.status === "processing" ? "spin" : ""} />
                      <span>{t(cfg.labelKey) as string}</span>
                    </span>
                    <span className="im-sessions__platform">
                      {PLATFORM_LABELS[s.platform] ?? s.platform}
                    </span>
                  </div>
                  <div className="im-sessions__row-content">
                    {s.content || t("imSessions.noContent") as string}
                  </div>
                  <div className="im-sessions__row-meta">
                    {s.senderName && <span className="im-sessions__sender">{s.senderName}</span>}
                    <span className="im-sessions__time">{formatTime(s.createdAt)}</span>
                  </div>
                </button>
              );
            })
          )}
        </div>

        {/* Detail pane */}
        {selectedId && (
          <div className="im-sessions__detail">
            {loadingDetail ? (
              <div className="im-sessions__detail-loading">
                <Loader size={20} className="spin" />
              </div>
            ) : detail && detail.id ? (
              <SessionDetail detail={detail} transcript={transcript} />
            ) : (
              <div className="im-sessions__detail-empty">
                <p>{t("imSessions.detailNotFound") as string}</p>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ── Detail sub-component ─────────────────────────────────────
function SessionDetail({
  detail,
  transcript,
}: {
  detail: IMSessionDetailView;
  transcript: HistoryMessage[] | null;
}) {
  const t = useT();
  const cfg = STATUS_CONFIG[detail.status] ?? STATUS_CONFIG.processing;
  const StatusIcon = cfg.icon;

  return (
    <div className="im-sessions__detail-body">
      {/* Header */}
      <div className="im-sessions__detail-head">
        <span className={`im-sessions__status ${cfg.className}`}>
          <StatusIcon size={12} className={detail.status === "processing" ? "spin" : ""} />
          <span>{t(cfg.labelKey) as string}</span>
        </span>
        <span className="im-sessions__platform">
          {PLATFORM_LABELS[detail.platform] ?? detail.platform}
        </span>
      </div>

      {/* Incoming message */}
      <section className="im-sessions__section">
        <h4 className="im-sessions__section-title">{t("imSessions.incomingMessage") as string}</h4>
        <div className="im-sessions__message-box im-sessions__message-box--in">
          {detail.content || t("imSessions.noContent") as string}
        </div>
        <div className="im-sessions__meta-row">
          {detail.senderName && (
            <span><strong>{t("imSessions.sender") as string}:</strong> {detail.senderName}</span>
          )}
          {detail.conversationId && (
            <span className="im-sessions__mono">{detail.conversationId}</span>
          )}
          <span>{formatTime(detail.createdAt)}</span>
        </div>
      </section>

      {/* Result */}
      {detail.result && (
        <section className="im-sessions__section">
          <h4 className="im-sessions__section-title">{t("imSessions.result") as string}</h4>
          <div className="im-sessions__message-box im-sessions__message-box--out">
            {detail.result}
          </div>
          {detail.doneAt ? (
            <div className="im-sessions__meta-row">
              <span>{formatTime(detail.doneAt)}</span>
            </div>
          ) : null}
        </section>
      )}

      {/* Execution trace (agent transcript) */}
      {transcript && transcript.length > 0 && (
        <section className="im-sessions__section">
          <h4 className="im-sessions__section-title">
            {t("imSessions.executionTrace") as string}
            <span className="im-sessions__trace-count">{transcript.length}</span>
          </h4>
          <div className="im-sessions__trace">
            {transcript.map((msg, i) => (
              <TraceEntry key={i} msg={msg} />
            ))}
          </div>
        </section>
      )}

      {detail.agentSession && (!transcript || transcript.length === 0) && (
        <section className="im-sessions__section">
          <h4 className="im-sessions__section-title">{t("imSessions.executionTrace") as string}</h4>
          <p className="im-sessions__trace-empty">{t("imSessions.traceEmpty") as string}</p>
        </section>
      )}
    </div>
  );
}

// ── One entry in the agent transcript ────────────────────────
function TraceEntry({ msg }: { msg: HistoryMessage }) {
  const isUser = msg.role === "user";
  const isAssistant = msg.role === "assistant";
  const isTool = msg.role === "tool" || msg.toolName;
  const isNotice = msg.role === "notice";

  const icon = isUser ? "→" : isAssistant ? "←" : isTool ? "⚙" : isNotice ? "•" : "·";
  const label = isUser
    ? "用户"
    : isAssistant
      ? "助手"
      : isTool
        ? msg.toolName || "工具"
        : isNotice
          ? "通知"
          : msg.role;

  return (
    <div className={`im-sessions__trace-entry im-sessions__trace-entry--${msg.role}`}>
      <span className="im-sessions__trace-icon">{icon}</span>
      <div className="im-sessions__trace-content">
        <span className="im-sessions__trace-label">{label}</span>
        {msg.content && <div className="im-sessions__trace-text">{truncate(msg.content, 300)}</div>}
        {msg.toolCalls && msg.toolCalls.length > 0 && (
          <div className="im-sessions__trace-tools">
            {msg.toolCalls.map((tc) => (
              <span key={tc.id} className="im-sessions__trace-tool">
                {tc.name}
                <ChevronRight size={9} />
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// ── Helpers ──────────────────────────────────────────────────
function formatTime(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) {
    return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  }
  return d.toLocaleDateString(undefined, { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}

function truncate(s: string, n: number): string {
  if (s.length <= n) return s;
  return s.slice(0, n) + "…";
}
