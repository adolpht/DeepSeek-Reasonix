import { useCallback, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { X, ArrowUpFromLine, Send } from "lucide-react";
import { useT } from "../lib/i18n";
import type { Item } from "../lib/useController";
import { Transcript } from "./Transcript";

export interface SideChatProps {
  visible: boolean;
  contextItems: Item[];
  onSend: (text: string) => void;
  onClose: () => void;
  onPromoteToMain: (text: string) => void;
}

const SIDE_CHAT_MIN_WIDTH = 280;
const SIDE_CHAT_DEFAULT_WIDTH = 380;
const SIDE_CHAT_MAX_WIDTH = 600;

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
  contextItems,
  onSend,
  onClose,
  onPromoteToMain,
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

  return (
    <aside
      className={`side-chat${resizing ? " side-chat--resizing" : ""}`}
      style={{ "--side-chat-width": `${width}px` } as React.CSSProperties}
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
      <div className="side-chat__header">
        <span className="side-chat__title">{t("sideChat.title")}</span>
        <div className="side-chat__header-actions">
          <button
            className="side-chat__icon-btn"
            onClick={() => {
              if (input.trim()) onPromoteToMain(input.trim());
            }}
            title={t("sideChat.promoteToMain")}
          >
            <ArrowUpFromLine size={14} />
          </button>
          <button className="side-chat__icon-btn" onClick={onClose} title={t("sideChat.close")}>
            <X size={14} />
          </button>
        </div>
      </div>
      <div className="side-chat__body">
        {contextItems.length === 0 ? (
          <div className="side-chat__empty">{t("sideChat.placeholder")}</div>
        ) : (
          <Transcript items={contextItems} onPrompt={() => {}} questionNavigator={false} />
        )}
      </div>
      <div className="side-chat__footer">
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
      </div>
    </aside>
  );
}
