import { useCallback, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { X, ArrowUpFromLine, Send } from "lucide-react";
import { useT } from "../lib/i18n";
import type { Item, LiveStream } from "../lib/useController";
import type { QuestionAnswer, WireApproval, WireAsk } from "../lib/types";
import { Transcript } from "./Transcript";
import { ApprovalModal } from "./ApprovalModal";
import { AskCard } from "./AskCard";

export interface SideChatProps {
  visible: boolean;
  items: Item[];
  live?: LiveStream;
  running: boolean;
  approval?: WireApproval;
  ask?: WireAsk;
  onSend: (text: string) => void;
  onClose: () => void;
  onPromote: () => void;
  onApprove: (id: string, allow: boolean, session: boolean, persist: boolean) => void;
  onAnswer: (id: string, answers: QuestionAnswer[]) => void;
}

const SIDE_CHAT_MIN_WIDTH = 320;
const SIDE_CHAT_DEFAULT_WIDTH = 420;
const SIDE_CHAT_MAX_WIDTH = 640;

function clampWidth(w: number): number {
  return Math.min(SIDE_CHAT_MAX_WIDTH, Math.max(SIDE_CHAT_MIN_WIDTH, Math.round(w)));
}

function loadWidth(): number {
  try {
    const raw = window.localStorage.getItem("Rexion.sideChatWidth");
    if (raw) return clampWidth(parseInt(raw, 10));
  } catch { /* ignore */ }
  return SIDE_CHAT_DEFAULT_WIDTH;
}

function saveWidth(w: number): void {
  try {
    window.localStorage.setItem("Rexion.sideChatWidth", String(w));
  } catch { /* ignore */ }
}

export function SideChat({
  visible,
  items,
  live,
  running,
  approval,
  ask,
  onSend,
  onClose,
  onPromote,
  onApprove,
  onAnswer,
}: SideChatProps) {
  const t = useT();
  const [width, setWidth] = useState(loadWidth);
  const [input, setInput] = useState("");
  const [resizing, setResizing] = useState(false);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const handleSend = useCallback(() => {
    const text = input.trim();
    if (!text) return;
    onSend(text);
    setInput("");
  }, [input, onSend]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  const startResize = useCallback(
    (event: ReactPointerEvent<HTMLDivElement>) => {
      event.preventDefault();
      setResizing(true);
      let nextWidth = width;
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = clampWidth(window.innerWidth - moveEvent.clientX);
        setWidth(nextWidth);
      };
      const onDone = () => {
        setWidth(nextWidth);
        saveWidth(nextWidth);
        setResizing(false);
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
    [width],
  );

  if (!visible) return null;

  const hasContent = items.length > 0 || Boolean(live?.text || live?.reasoning);

  return (
    <aside
      className={`side-chat${resizing ? " side-chat--resizing" : ""}`}
      style={{ "--side-chat-width": `${width}px` } as React.CSSProperties}
      aria-label={t("sideChat.title")}
    >
      <div
        className="side-chat__resizer"
        role="separator"
        aria-orientation="vertical"
        onPointerDown={startResize}
        onDoubleClick={() => {
          const next = SIDE_CHAT_DEFAULT_WIDTH;
          setWidth(next);
          saveWidth(next);
        }}
      />
      <header className="side-chat__header">
        <div className="side-chat__heading">
          <span className="side-chat__title">{t("sideChat.title")}</span>
          {running && <span className="side-chat__dot" aria-hidden="true" />}
        </div>
        <div className="side-chat__header-actions">
          <button
            className="side-chat__icon-btn"
            onClick={onPromote}
            title={t("sideChat.promoteToMain")}
            disabled={!hasContent}
          >
            <ArrowUpFromLine size={14} />
          </button>
          <button className="side-chat__icon-btn" onClick={onClose} title={t("sideChat.close")}>
            <X size={14} />
          </button>
        </div>
      </header>
      <div className="side-chat__body">
        {hasContent ? (
          <Transcript items={items} live={live} onPrompt={() => {}} questionNavigator={false} />
        ) : (
          <div className="side-chat__empty">{t("sideChat.placeholder")}</div>
        )}
      </div>

      {approval && (
        <div className="side-chat__overlay">
          <ApprovalModal
            approval={approval}
            onAnswer={(allow, session, persist) => onApprove(approval.id, allow, session, persist)}
            onExitPlan={() => onApprove(approval.id, false, false, false)}
          />
        </div>
      )}
      {ask && (
        <div className="side-chat__overlay">
          <AskCard
            ask={ask}
            onAnswer={onAnswer}
            onDismiss={() => onAnswer(ask.id, [])}
          />
        </div>
      )}

      <footer className="side-chat__footer">
        <textarea
          ref={inputRef}
          className="side-chat__input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={t("sideChat.placeholder")}
          rows={1}
        />
        <button
          className="side-chat__send-btn"
          onClick={handleSend}
          disabled={!input.trim()}
          title={t("composer.send")}
        >
          <Send size={14} />
        </button>
      </footer>
    </aside>
  );
}
