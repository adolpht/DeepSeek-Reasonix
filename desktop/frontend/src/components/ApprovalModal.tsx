import { useCallback, useEffect, useRef, useState } from "react";
import { useT } from "../lib/i18n";
import type { WireApproval } from "../lib/types";
import { PromptAction, PromptDetailToggle, PromptShelf } from "./PromptShelf";

const LOW_RISK_PATTERNS = ["read", "list", "search", "grep", "glob", "ls", "cat", "head", "stat", "info", "query", "find", "which", "echo", "pwd"];

function isLowRiskTool(toolName: string): boolean {
  const name = toolName.toLowerCase();
  return LOW_RISK_PATTERNS.some((p) => name.includes(p));
}

function loadAutoApproveTimeout(): number {
  try {
    const val = window.localStorage.getItem("Rexion.approval.autoApproveTimeout");
    if (val != null) {
      const n = parseInt(val, 10);
      if (!isNaN(n)) return n;
    }
  } catch {}
  return 30000;
}

const SESSION_APPROVAL_COUNTS = new Map<string, number>();

function summarizeSideEffect(toolName: string, subject: string): string | null {
  if (!subject.trim()) return null;

  const trimmed = subject.trim();
  let args: Record<string, unknown> | null = null;
  if (trimmed.startsWith("{")) {
    try {
      const parsed = JSON.parse(trimmed);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        args = parsed as Record<string, unknown>;
      }
    } catch {
      // not valid JSON; fall through to plain-text handling
    }
  }

  const pickStr = (obj: Record<string, unknown>, keys: string[]): string | null => {
    for (const k of keys) {
      const v = obj[k];
      if (typeof v === "string" && v.trim()) return v.trim();
      if (Array.isArray(v) && v.length > 0) {
        const joined = v.filter((x): x is string => typeof x === "string").join(", ");
        if (joined.trim()) return joined.trim();
      }
    }
    return null;
  };

  const truncate = (s: string, max = 120): string => {
    const t = s.trim();
    return t.length > max ? t.slice(0, max - 1) + "…" : t;
  };

  const tool = toolName.toLowerCase();

  if (args) {
    if (tool === "write_file" || tool === "edit_file" || tool === "create_file") {
      const p = pickStr(args, ["file_path", "path", "file", "filename"]);
      if (p) return p;
    }
    if (tool === "run_shell" || tool === "bash" || tool === "shell") {
      const c = pickStr(args, ["command", "cmd", "script"]);
      if (c) return truncate(c, 120);
    }
    if (tool === "mcp__office__write_docx") {
      const p = pickStr(args, ["path", "file_path", "output_path"]);
      if (p) return p;
    }
    if (tool === "mcp__sheet__write_sheet") {
      const p = pickStr(args, ["path", "file_path", "output_path"]);
      if (p) return p;
    }
    if (tool === "mcp__slides__create_ppt" || tool === "mcp__slides__add_slide") {
      const p = pickStr(args, ["output_path", "ppt_path", "path"]);
      if (p) return p;
    }
    if (tool === "mcp__mail__send_mail") {
      const to = pickStr(args, ["to", "recipient", "recipients"]);
      if (to) return truncate(to, 80);
    }
    if (tool === "mcp__im__send_message") {
      const platform = pickStr(args, ["platform", "channel"]);
      const content = pickStr(args, ["content", "message", "text"]);
      if (platform && content) return `[${platform}] ${truncate(content, 80)}`;
      if (content) return truncate(content, 120);
      if (platform) return platform;
    }
    // generic fallback for other write tools
    const generic = pickStr(args, ["path", "file", "file_path", "output", "output_path", "target"]);
    if (generic) return generic;
  }

  // plain text fallback
  return truncate(trimmed.replace(/\s+/g, " "), 120);
}

export function ApprovalModal({
  approval,
  onAnswer,
  onRevisePlan,
  onExitPlan,
}: {
  approval: WireApproval;
  onAnswer: (allow: boolean, session: boolean, persist: boolean) => void;
  onRevisePlan?: (text: string) => void;
  onExitPlan?: () => void;
}) {
  const t = useT();
  const [revisionOpen, setRevisionOpen] = useState(false);
  const [revisionText, setRevisionText] = useState("");
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [countdown, setCountdown] = useState<number | null>(null);
  const [dismissedSuggestion, setDismissedSuggestion] = useState(false);
  const cardRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLTextAreaElement | null>(null);
  const isPlanApproval = approval.tool === "exit_plan_mode";
  const subject = approval.subject.trim();
  const subjectSummary = subject.split("\n").find((line) => line.trim())?.trim() ?? "";
  const summary = summarizeSideEffect(approval.tool, subject);

  const lowRisk = !isPlanApproval && isLowRiskTool(approval.tool);
  const autoTimeout = loadAutoApproveTimeout();
  const autoTimeoutSec = autoTimeout > 0 ? Math.ceil(autoTimeout / 1000) : 0;
  const sessionCount = SESSION_APPROVAL_COUNTS.get(approval.tool) ?? 0;
  const showSuggestion = sessionCount >= 3 && !dismissedSuggestion;

  const handleAnswer = useCallback(
    (allow: boolean, session: boolean, persist: boolean) => {
      if (allow && !session && !persist) {
        const prev = SESSION_APPROVAL_COUNTS.get(approval.tool) ?? 0;
        SESSION_APPROVAL_COUNTS.set(approval.tool, prev + 1);
      }
      setCountdown(null);
      onAnswer(allow, session, persist);
    },
    [approval.tool, onAnswer],
  );

  const choosePlanAction = (key: string) => {
    if (key === "1") setRevisionOpen((open) => !open);
    else if (key === "2") onAnswer(true, false, false);
    else if (key === "3" || key === "Escape") (onExitPlan ?? (() => onAnswer(false, false, false)))();
  };

  const chooseToolAction = (key: string) => {
    if (key === "1") handleAnswer(true, false, false);
    else if (key === "2") handleAnswer(true, true, false);
    else if (key === "3") handleAnswer(true, true, true);
    else if (key === "4" || key === "Escape") handleAnswer(false, false, false);
  };

  useEffect(() => {
    cardRef.current?.focus();
    setRevisionOpen(false);
    setRevisionText("");
    setDetailsOpen(false);
    setDismissedSuggestion(false);
    if (lowRisk && autoTimeout > 0) {
      setCountdown(autoTimeoutSec);
    } else {
      setCountdown(null);
    }
  }, [approval.id, lowRisk, autoTimeout]);

  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const tag = target?.tagName.toLowerCase();
      if (tag === "input" || tag === "textarea" || target?.isContentEditable) return;
      if (event.key !== "1" && event.key !== "2" && event.key !== "3" && event.key !== "4" && event.key !== "Escape") return;
      event.preventDefault();
      if (isPlanApproval) choosePlanAction(event.key);
      else chooseToolAction(event.key);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [isPlanApproval, handleAnswer, onExitPlan]);

  useEffect(() => {
    if (countdown == null || countdown <= 0) {
      if (countdown === 0) handleAnswer(true, false, false);
      return;
    }
    const id = setInterval(() => setCountdown((c) => (c != null ? c - 1 : null)), 1000);
    return () => clearInterval(id);
  }, [countdown, handleAnswer]);

  useEffect(() => {
    if (revisionOpen) inputRef.current?.focus();
  }, [revisionOpen]);

  const submitRevision = () => {
    const text = revisionText.trim();
    if (!text) {
      inputRef.current?.focus();
      return;
    }
    onRevisePlan?.(text);
  };

  // The plan is already shown above as the assistant's reply; this is just the gate.
  if (isPlanApproval) {
    return (
      <PromptShelf
        barRef={cardRef}
        titleId="plan-approval-title"
        title={t("approval.planReady")}
        meta={t("approval.planReadyHint")}
        actions={
          <>
            <PromptAction keyLabel="1" label={t("approval.revisePlan")} onClick={() => setRevisionOpen((open) => !open)} />
            <PromptAction keyLabel="2" label={t("approval.startExecution")} onClick={() => onAnswer(true, false, false)} selected />
            <PromptAction
              keyLabel="3"
              label={t("approval.exitPlan")}
              onClick={() => (onExitPlan ?? (() => onAnswer(false, false, false)))()}
            />
          </>
        }
      >
        {revisionOpen && (
          <div className="plan-revision">
            <textarea
              ref={inputRef}
              className="plan-revision__input"
              value={revisionText}
              rows={3}
              placeholder={t("approval.revisePlanPlaceholder")}
              onChange={(event) => setRevisionText(event.target.value)}
              onKeyDown={(event) => {
                if ((event.metaKey || event.ctrlKey) && event.key === "Enter") submitRevision();
                event.stopPropagation();
              }}
            />
            <div className="plan-revision__actions">
              <button className="btn" onClick={() => setRevisionOpen(false)}>
                {t("common.cancel")}
              </button>
              <button className="btn btn--primary" onClick={submitRevision}>
                {t("approval.sendRevision")}
              </button>
            </div>
          </div>
        )}
      </PromptShelf>
    );
  }

  return (
    <PromptShelf
      barRef={cardRef}
      titleId="tool-approval-title"
      title={t("approval.toolPending")}
      actionsWrap
      meta={
        <>
          <span className="tool__name">{approval.tool}</span>
          {subjectSummary && <span className="prompt-shelf__subject"> · {subjectSummary}</span>}
        </>
      }
      actions={
        <>
          {subject && (
            <PromptDetailToggle
              open={detailsOpen}
              label={t("approval.details")}
              openLabel={t("approval.hideDetails")}
              onClick={() => setDetailsOpen((open) => !open)}
            />
          )}
          <button
            className="prompt-action prompt-action--selected"
            onClick={() => handleAnswer(true, false, false)}
          >
            <span className="prompt-action__key">1</span>
            <span className="prompt-action__label">{t("approval.allowOnce")}</span>
            {countdown != null && countdown > 0 && (
              <span className="approval__countdown">
                <span
                  className="approval__countdown-ring"
                  style={{ "--countdown-pct": `${(countdown / autoTimeoutSec) * 100}%` } as React.CSSProperties}
                >
                  <span className="approval__countdown-sec">{countdown}</span>
                </span>
                <span className="approval__countdown-badge">{t("approval.autoApprove")}</span>
              </span>
            )}
          </button>
          <PromptAction keyLabel="2" label={t("approval.allowSession")} onClick={() => handleAnswer(true, true, false)} />
          <PromptAction keyLabel="3" label={t("approval.allowPersistent")} onClick={() => handleAnswer(true, true, true)} />
          <PromptAction keyLabel="4" label={t("approval.deny")} onClick={() => handleAnswer(false, false, false)} />
        </>
      }
    >
      {summary && (
        <div className="approval__summary" role="note">
          <span className="approval__summary-label">{t("approval.sideEffect")}</span>
          <span className="approval__summary-text">{summary}</span>
        </div>
      )}
      {showSuggestion && (
        <div className="approval__suggestion">
          <span className="approval__suggestion-text">
            {t("approval.suggestSession", { n: sessionCount })}
          </span>
          <button className="approval__suggestion-btn" onClick={() => handleAnswer(true, true, false)}>
            {t("approval.suggestSessionAction")}
          </button>
          <button className="approval__suggestion-dismiss" onClick={() => setDismissedSuggestion(true)} aria-label={t("common.close")}>
            ✕
          </button>
        </div>
      )}
      {detailsOpen && subject && (
        <pre className="approval-subject">{subject}</pre>
      )}
    </PromptShelf>
  );
}
