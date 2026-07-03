import { Loader2, X, Languages, ListChecks, Sparkles, PenLine, HelpCircle, Wrench, Copy, Check, History, Search, FileText, Image as ImageIcon } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { ClipboardEntry } from "../lib/types";

interface FloatingWindowProps {
  visible: boolean;
  onClose: () => void;
  onSubmitAction: (action: string, text: string) => void;
  result?: string | null;
  loading?: boolean;
}

const CLIPBOARD_ACTIONS = [
  { id: "translate", icon: Languages },
  { id: "summarize", icon: ListChecks },
  { id: "polish", icon: Sparkles },
  { id: "continue", icon: PenLine },
  { id: "explain", icon: HelpCircle },
  { id: "optimize", icon: Wrench },
] as const;

const PREVIEW_MAX_LENGTH = 200;

function truncatePreview(text: string): string {
  if (text.length <= PREVIEW_MAX_LENGTH) return text;
  return text.slice(0, PREVIEW_MAX_LENGTH) + "…";
}

type TabMode = "assist" | "history";

export function FloatingWindow({ visible, onClose, onSubmitAction, result: externalResult, loading: externalLoading }: FloatingWindowProps) {
  const t = useT();
  const [clipboardText, setClipboardText] = useState("");
  const [localLoading, setLocalLoading] = useState(false);
  const [localResult, setLocalResult] = useState("");
  const [copied, setCopied] = useState(false);
  const [activeAction, setActiveAction] = useState<string | null>(null);
  const [tabMode, setTabMode] = useState<TabMode>("assist");
  const [historyEntries, setHistoryEntries] = useState<ClipboardEntry[]>([]);
  const [historyLoading, setHistoryLoading] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [copiedHistory, setCopiedHistory] = useState(false);
  const overlayRef = useRef<HTMLDivElement>(null);

  const loading = externalLoading ?? localLoading;
  const result = externalResult ?? localResult;

  useEffect(() => {
    if (!visible) return;
    setLocalResult("");
    setActiveAction(null);
    setLocalLoading(false);
    setCopied(false);
    setCopiedHistory(false);
    void app.ReadClipboard().then((text: string) => {
      setClipboardText(text || "");
    }).catch(() => {
      setClipboardText("");
    });
  }, [visible]);

  useEffect(() => {
    if (!visible || tabMode !== "history") return;
    loadHistory();
  }, [visible, tabMode]);

  useEffect(() => {
    if (tabMode !== "history") return;
    const timer = setTimeout(() => {
      loadHistory(searchQuery);
    }, 200);
    return () => clearTimeout(timer);
  }, [searchQuery]);

  async function loadHistory(query?: string) {
    setHistoryLoading(true);
    try {
      let entries: ClipboardEntry[];
      if (query && query.trim()) {
        entries = await app.SearchClipboardHistory(query.trim());
      } else {
        entries = await app.ListClipboardHistory(50, 0);
      }
      setHistoryEntries(entries);
    } catch {
      setHistoryEntries([]);
    } finally {
      setHistoryLoading(false);
    }
  }

  useEffect(() => {
    if (!visible) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [visible, onClose]);

  const handleOverlayClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === overlayRef.current) {
        onClose();
      }
    },
    [onClose],
  );

  const handleAction = useCallback(
    (actionId: string) => {
      if (loading || !clipboardText.trim()) return;
      setActiveAction(actionId);
      setLocalLoading(true);
      setLocalResult("");
      onSubmitAction(actionId, clipboardText);
    },
    [loading, clipboardText, onSubmitAction],
  );

  const handleCopyResult = useCallback(async () => {
    if (!result) return;
    try {
      await app.WriteClipboard(result);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // ignore
    }
  }, [result]);

  const handleCopyHistoryEntry = useCallback(async (entry: ClipboardEntry) => {
    try {
      await app.WriteClipboard(entry.content);
      setCopiedHistory(true);
      setTimeout(() => setCopiedHistory(false), 2000);
    } catch {
      // ignore
    }
  }, []);

  function formatTime(timestamp: number): string {
    const date = new Date(timestamp);
    const now = new Date();
    const diff = now.getTime() - date.getTime();
    if (diff < 60000) return t("floatingWindow.justNow");
    if (diff < 3600000) return Math.floor(diff / 60000) + " " + t("floatingWindow.minutesAgo");
    if (diff < 86400000) return Math.floor(diff / 3600000) + " " + t("floatingWindow.hoursAgo");
    return date.toLocaleDateString();
  }

  function getKindIcon(kind: string) {
    switch (kind) {
      case "image":
        return <ImageIcon size={14} />;
      case "file":
        return <FileText size={14} />;
      default:
        return <Copy size={14} />;
    }
  }

  if (!visible) return null;

  return (
    <div
      className="floating-window__overlay"
      ref={overlayRef}
      onClick={handleOverlayClick}
    >
      <div className="floating-window">
        <div className="floating-window__header">
          <span className="floating-window__title">
            {t("floatingWindow.title")}
          </span>
          <div className="floating-window__tabs">
            <button
              className={`floating-window__tab ${tabMode === "assist" ? "floating-window__tab--active" : ""}`}
              onClick={() => setTabMode("assist")}
            >
              <Sparkles size={14} />
              <span>{t("floatingWindow.assist")}</span>
            </button>
            <button
              className={`floating-window__tab ${tabMode === "history" ? "floating-window__tab--active" : ""}`}
              onClick={() => setTabMode("history")}
            >
              <History size={14} />
              <span>{t("floatingWindow.history")}</span>
            </button>
          </div>
          <button
            className="floating-window__close"
            type="button"
            onClick={onClose}
            aria-label={t("common.close")}
          >
            <X size={16} />
          </button>
        </div>

        {tabMode === "assist" && (
          <>
            <div className="floating-window__preview">
              {clipboardText ? (
                <pre className="floating-window__preview-text">{truncatePreview(clipboardText)}</pre>
              ) : (
                <span className="floating-window__preview-empty">
                  {t("floatingWindow.emptyClipboard")}
                </span>
              )}
            </div>

            <div className="floating-window__actions">
              {CLIPBOARD_ACTIONS.map(({ id, icon: Icon }) => (
                <button
                  key={id}
                  type="button"
                  className={[
                    "floating-window__action",
                    activeAction === id && !loading ? " floating-window__action--active" : "",
                  ].join("")}
                  disabled={loading || !clipboardText.trim()}
                  onClick={() => handleAction(id)}
                >
                  <Icon size={15} />
                  <span>{t(`floatingWindow.${id}`)}</span>
                </button>
              ))}
            </div>

            {loading && (
              <div className="floating-window__loading">
                <Loader2 size={16} className="floating-window__spinner" />
                <span>{t("floatingWindow.processing")}</span>
              </div>
            )}

            {result && (
              <div className="floating-window__result">
                <div className="floating-window__result-header">
                  <span className="floating-window__result-label">
                    {t("floatingWindow.result")}
                  </span>
                  <button
                    className="floating-window__copy-btn"
                    type="button"
                    onClick={handleCopyResult}
                    aria-label={t("common.copy")}
                  >
                    {copied ? <Check size={14} /> : <Copy size={14} />}
                    <span>{copied ? t("floatingWindow.copied") : t("common.copy")}</span>
                  </button>
                </div>
                <pre className="floating-window__result-text">{result}</pre>
              </div>
            )}
          </>
        )}

        {tabMode === "history" && (
          <>
            <div className="floating-window__search">
              <Search size={14} />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder={t("floatingWindow.searchHistory")}
                className="floating-window__search-input"
              />
            </div>

            <div className="floating-window__history-list">
              {historyLoading ? (
                <div className="floating-window__loading">
                  <Loader2 size={16} className="floating-window__spinner" />
                  <span>{t("common.loading")}</span>
                </div>
              ) : historyEntries.length === 0 ? (
                <span className="floating-window__preview-empty">
                  {t("floatingWindow.emptyHistory")}
                </span>
              ) : (
                historyEntries.map((entry) => (
                  <div
                    key={entry.id}
                    className="floating-window__history-item"
                    onClick={() => handleCopyHistoryEntry(entry)}
                  >
                    <div className="floating-window__history-kind">
                      {getKindIcon(entry.kind)}
                    </div>
                    <div className="floating-window__history-content">
                      <pre className="floating-window__history-preview">
                        {truncatePreview(entry.preview || entry.content)}
                      </pre>
                      <span className="floating-window__history-time">
                        {formatTime(entry.createdAt)}
                      </span>
                    </div>
                    <button
                      className="floating-window__copy-btn floating-window__copy-btn--small"
                      type="button"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleCopyHistoryEntry(entry);
                      }}
                    >
                      {copiedHistory ? <Check size={12} /> : <Copy size={12} />}
                    </button>
                  </div>
                ))
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
