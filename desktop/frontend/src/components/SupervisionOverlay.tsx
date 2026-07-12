// SupervisionOverlay is a floating panel that supervises Computer Use / desktop
// automation. It shows the action the agent is about to perform (with a
// screenshot), exposes Pause / Resume / Abort controls, keeps a rolling history
// of the last 10 actions, and highlights sensitive operations (red border) with
// a secondary confirm button.
//
// The component is purely presentational — it receives the current pending
// action, history, and control callbacks as props. The parent (App) wires it to
// the agent runtime. When `active` is false the overlay renders nothing.

import { useState } from "react";
import { AlertTriangle, Camera, ChevronDown, ChevronUp, Pause, Play, Square, X } from "lucide-react";
import { useT } from "../lib/i18n";
import type { DictKey, Translator } from "../lib/i18n";

export type SupervisionActionStatus = "pending" | "running" | "done" | "error" | "aborted";

export interface SupervisionAction {
  /** Unique id for the action (used as React key). */
  id: string;
  /** Tool name, e.g. "computer_click" / "computer_type". */
  tool: string;
  /** Human-readable summary of what the agent is about to do. */
  description: string;
  /** Optional base64-encoded PNG screenshot (no data: prefix). */
  screenshot?: string;
  /** True for destructive / sensitive operations (close window, overwrite, send). */
  sensitive?: boolean;
  /** Epoch ms when the action was recorded. */
  timestamp: number;
  /** Lifecycle status. */
  status: SupervisionActionStatus;
}

export interface SupervisionOverlayProps {
  /** Whether the overlay is visible. */
  active: boolean;
  /** The action about to execute, or null if idle. */
  pendingAction: SupervisionAction | null;
  /** Rolling history (caller keeps last N; overlay caps to 10 for display). */
  history: SupervisionAction[];
  /** Whether the agent is currently paused (waiting for user). */
  paused: boolean;
  /** Pause the agent before the next action. */
  onPause: () => void;
  /** Resume the agent after a pause. */
  onResume: () => void;
  /** Abort the current run entirely. */
  onAbort: () => void;
  /** Confirm a sensitive pending action. */
  onConfirmSensitive: () => void;
  /** Dismiss / collapse the overlay (optional). */
  onDismiss?: () => void;
}

const MAX_HISTORY = 10;

export function SupervisionOverlay({
  active,
  pendingAction,
  history,
  paused,
  onPause,
  onResume,
  onAbort,
  onConfirmSensitive,
  onDismiss,
}: SupervisionOverlayProps) {
  const t = useT();
  const [expanded, setExpanded] = useState(true);
  const [historyOpen, setHistoryOpen] = useState(false);

  if (!active) return null;

  const sensitive = pendingAction?.sensitive === true;
  const recent = history.slice(0, MAX_HISTORY);
  const hasPending = pendingAction != null;

  return (
    <div
      className={`supervision-overlay${sensitive ? " supervision-overlay--sensitive" : ""}${paused ? " supervision-overlay--paused" : ""}`}
      role="dialog"
      aria-modal="false"
      aria-label={t("supervision.label")}
    >
      <div className="supervision-overlay__header">
        <div className="supervision-overlay__title">
          <span className="supervision-overlay__dot" aria-hidden="true" />
          <span>{t("supervision.label")}</span>
          {paused && <span className="supervision-overlay__badge">{t("supervision.paused")}</span>}
        </div>
        <div className="supervision-overlay__header-actions">
          <button
            className="supervision-overlay__icon-btn"
            onClick={() => setExpanded((v) => !v)}
            aria-label={expanded ? t("common.collapse") : t("common.expand")}
            title={expanded ? t("common.collapse") : t("common.expand")}
          >
            {expanded ? <ChevronDown size={16} aria-hidden="true" /> : <ChevronUp size={16} aria-hidden="true" />}
          </button>
          {onDismiss && (
            <button
              className="supervision-overlay__icon-btn"
              onClick={onDismiss}
              aria-label={t("common.close")}
              title={t("common.close")}
            >
              <X size={16} aria-hidden="true" />
            </button>
          )}
        </div>
      </div>

      {expanded && (
        <>
          <div className="supervision-overlay__current">
            {hasPending ? (
              <>
                <div className="supervision-overlay__intent">
                  <span className="supervision-overlay__intent-label">{t("supervision.aboutTo")}</span>
                  <span className="supervision-overlay__intent-tool">{pendingAction.tool}</span>
                  <span className="supervision-overlay__intent-desc">{pendingAction.description}</span>
                </div>
                {sensitive && (
                  <div className="supervision-overlay__sensitive-flag" role="alert">
                    <AlertTriangle size={14} aria-hidden="true" />
                    <span>{t("supervision.sensitive")}</span>
                  </div>
                )}
              </>
            ) : (
              <div className="supervision-overlay__intent">
                <span className="supervision-overlay__intent-label">{t("supervision.idle")}</span>
              </div>
            )}

            {hasPending && pendingAction.screenshot && (
              <div className="supervision-overlay__screenshot">
                <img
                  src={`data:image/png;base64,${pendingAction.screenshot}`}
                  alt={t("supervision.screenshotAlt")}
                />
              </div>
            )}
            {hasPending && !pendingAction.screenshot && (
              <div className="supervision-overlay__screenshot supervision-overlay__screenshot--empty">
                <Camera size={20} aria-hidden="true" />
                <span>{t("supervision.noScreenshot")}</span>
              </div>
            )}
          </div>

          <div className="supervision-overlay__actions">
            {paused ? (
              <button className="btn btn--primary supervision-overlay__btn" onClick={onResume}>
                <Play size={14} aria-hidden="true" />
                {t("supervision.resume")}
              </button>
            ) : (
              <button className="btn supervision-overlay__btn" onClick={onPause} disabled={!hasPending}>
                <Pause size={14} aria-hidden="true" />
                {t("supervision.pause")}
              </button>
            )}
            <button className="btn btn--danger supervision-overlay__btn" onClick={onAbort} disabled={!hasPending && !paused}>
              <Square size={14} aria-hidden="true" />
              {t("supervision.abort")}
            </button>
            {sensitive && hasPending && (
              <button className="btn btn--primary supervision-overlay__btn supervision-overlay__btn--confirm" onClick={onConfirmSensitive}>
                <AlertTriangle size={14} aria-hidden="true" />
                {t("supervision.confirm")}
              </button>
            )}
          </div>

          <div className="supervision-overlay__history">
            <button
              className="supervision-overlay__history-toggle"
              onClick={() => setHistoryOpen((v) => !v)}
              aria-expanded={historyOpen}
            >
              <span>{t("supervision.history")}</span>
              <span className="supervision-overlay__history-count">{recent.length}</span>
              {historyOpen ? <ChevronUp size={14} aria-hidden="true" /> : <ChevronDown size={14} aria-hidden="true" />}
            </button>
            {historyOpen && (
              <ul className="supervision-overlay__history-list">
                {recent.length === 0 && (
                  <li className="supervision-overlay__history-empty">{t("supervision.historyEmpty")}</li>
                )}
                {recent.map((item) => (
                  <li
                    key={item.id}
                    className={`supervision-overlay__history-item${item.sensitive ? " supervision-overlay__history-item--sensitive" : ""}`}
                  >
                    <span className="supervision-overlay__history-time">{formatTime(item.timestamp)}</span>
                    <span className="supervision-overlay__history-tool">{item.tool}</span>
                    <StatusPill status={item.status} t={t} />
                    {item.sensitive && <AlertTriangle size={12} aria-hidden="true" className="supervision-overlay__history-warn" />}
                    <span className="supervision-overlay__history-desc">{item.description}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </>
      )}
    </div>
  );
}

function StatusPill({ status, t }: { status: SupervisionActionStatus; t: Translator }) {
  const label = t(statusKey(status));
  return <span className={`supervision-overlay__status supervision-overlay__status--${status}`}>{label}</span>;
}

function statusKey(status: SupervisionActionStatus): DictKey {
  switch (status) {
    case "pending":
      return "supervision.statusPending";
    case "running":
      return "supervision.statusRunning";
    case "done":
      return "supervision.statusDone";
    case "error":
      return "supervision.statusError";
    case "aborted":
      return "supervision.statusAborted";
    default:
      return "supervision.statusPending";
  }
}

function formatTime(ts: number): string {
  const d = new Date(ts);
  const pad = (n: number) => n.toString().padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
