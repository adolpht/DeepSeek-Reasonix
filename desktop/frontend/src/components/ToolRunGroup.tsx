import { memo, useEffect, useState } from "react";
import { ChevronRight } from "lucide-react";
import type { Item } from "../lib/useController";
import { useT } from "../lib/i18n";
import { ProcessStatusIcon, ProcessToolIcon, type ProcessState } from "./ProcessCard";
import { ToolCard } from "./ToolCard";

type ToolItem = Extract<Item, { kind: "tool" }>;

function runState(items: ToolItem[]): ProcessState {
  if (items.some((it) => it.status === "running")) return "running";
  if (items.some((it) => it.status === "error")) return "failed";
  if (items.some((it) => it.status === "stopped")) return "stopped";
  return "done";
}

// ToolRunGroup wraps a burst of consecutive top-level tool calls behind one
// collapsible header so long chains of calls don't bury the conversation. It
// stays open while any call is still running (live progress stays visible) and
// collapses itself once the whole run settles.
export const ToolRunGroup = memo(function ToolRunGroup({
  items,
  subcalls,
  onPreview,
}: {
  items: ToolItem[];
  subcalls: ReadonlyMap<string, ToolItem[]>;
  onPreview?: (path: string, kind: string) => void;
}) {
  const t = useT();
  const running = items.some((it) => it.status === "running");
  const [open, setOpen] = useState(running);
  useEffect(() => {
    setOpen(running);
  }, [running]);

  const state = runState(items);
  const failed = items.filter((it) => it.status === "error").length;
  const names = items.map((it) => it.name).join(" \u00b7 ");
  const label = t("transcript.toolRun", { n: items.length });

  return (
    <div
      className={`tool-run${open ? " tool-run--open" : ""}${running ? " tool-run--running" : ""}`}
      data-state={state}
    >
      <button type="button" className="tool-run__head" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <span className="tool-run__chevron">
          <ChevronRight className={open ? "tool-run__chevron--open" : ""} size={13} />
        </span>
        <span className="tool-run__icon">
          <ProcessToolIcon size={13} />
        </span>
        <span className="tool-run__title">{label}</span>
        <span className="tool-run__names">{names}</span>
        <span className="tool-run__meta">
          {failed > 0 && <span className="tool-run__errors">{t("transcript.toolRunErrors", { n: failed })}</span>}
          <ProcessStatusIcon state={state} label={label} />
        </span>
      </button>
      {open && (
        <div className="tool-run__body">
          {items.map((it) => (
            <ToolCard key={it.id} item={it} subcalls={subcalls.get(it.id)} onPreview={onPreview} />
          ))}
        </div>
      )}
    </div>
  );
});
