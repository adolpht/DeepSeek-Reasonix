import { Loader2, X, Languages, ListChecks, Sparkles, PenLine, HelpCircle, Wrench, Copy, Check } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";

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

export function FloatingWindow({ visible, onClose, onSubmitAction, result: externalResult, loading: externalLoading }: FloatingWindowProps) {
  const t = useT();
  const [clipboardText, setClipboardText] = useState("");
  const [localLoading, setLocalLoading] = useState(false);
  const [localResult, setLocalResult] = useState("");
  const [copied, setCopied] = useState(false);
  const [activeAction, setActiveAction] = useState<string | null>(null);
  const overlayRef = useRef<HTMLDivElement>(null);

  // Use external props if provided, otherwise use local state
  const loading = externalLoading ?? localLoading;
  const result = externalResult ?? localResult;

  // Read clipboard when visible becomes true
  useEffect(() => {
    if (!visible) return;
    setLocalResult("");
    setActiveAction(null);
    setLocalLoading(false);
    setCopied(false);
    void app.ReadClipboard().then((text: string) => {
      setClipboardText(text || "");
    }).catch(() => {
      setClipboardText("");
    });
  }, [visible]);

  // ESC to close
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

  // Click outside to close
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
      // The result arrives via the agent event stream; we simulate a
      // completion state after a short delay for UX feedback.
      // In production, the result would be written to clipboard by the
      // agent's final output.
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

  if (!visible) return null;

  return (
    <div
      className="floating-window__overlay"
      ref={overlayRef}
      onClick={handleOverlayClick}
    >
      <div className="floating-window">
        {/* Header */}
        <div className="floating-window__header">
          <span className="floating-window__title">
            {t("floatingWindow.title")}
          </span>
          <button
            className="floating-window__close"
            type="button"
            onClick={onClose}
            aria-label={t("common.close")}
          >
            <X size={16} />
          </button>
        </div>

        {/* Clipboard preview */}
        <div className="floating-window__preview">
          {clipboardText ? (
            <pre className="floating-window__preview-text">{truncatePreview(clipboardText)}</pre>
          ) : (
            <span className="floating-window__preview-empty">
              {t("floatingWindow.emptyClipboard")}
            </span>
          )}
        </div>

        {/* Action buttons */}
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

        {/* Loading / result area */}
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
      </div>
    </div>
  );
}
