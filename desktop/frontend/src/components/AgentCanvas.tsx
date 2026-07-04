// Agent execution-flow canvas.
//
// Renders the live tool-call tree as an interactive node graph: each tool
// dispatch is a node, parent→child edges come from the kernel's `parentId`
// nesting (task / pool sub-agents), and node state mirrors ToolStatus. This
// is the "tracking" half — there is no authoring/dag editor here; the graph
// is a projection of what the agent is actually doing.

import { useCallback, useMemo, useState, memo, useRef } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  Handle,
  Position,
  type NodeProps,
  type NodeTypes,
  type Edge,
  type Node,
  MarkerType,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  Loader2,
  Check,
  X,
  Pause,
  Circle,
  GitBranch,
  Clock,
  Cpu,
  Eye,
  Terminal,
  RotateCw,
  Copy,
  Square,
} from "lucide-react";
import { useT, type Translator } from "../lib/i18n";
import type { Item } from "../lib/useController";
import type { WireStep } from "../lib/types";
import {
  buildAgentGraph,
  summarizeGraph,
  type AgentNodeData,
  type AgentNodeStatus,
  type AgentState,
} from "../lib/agentGraph";

// A typed node: data is AgentNodeData, type tag is the literal "agentNode" so
// the nodeTypes map and the NodeProps generic line up with v12's Node<T, U>.
type AgentNodeType = Node<AgentNodeData, "agentNode">;

function formatDuration(ms?: number): string {
  if (ms == null) return "";
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function stateLabel(t: Translator, s: AgentNodeStatus): string {
  switch (s) {
    case "running":
      return t("trace.stateRunning");
    case "done":
      return t("trace.stateDone");
    case "error":
      return t("trace.stateError");
    case "stopped":
      return t("trace.stateStopped");
    case "pending":
      return t("trace.statePending");
    default:
      return s;
  }
}

function StatusIcon({ status }: { status: AgentNodeStatus }) {
  switch (status) {
    case "running":
      return <Loader2 size={14} className="agent-node__spin" />;
    case "done":
      return <Check size={14} />;
    case "error":
      return <X size={14} />;
    case "stopped":
      return <Pause size={14} />;
    default:
      return <Circle size={12} />;
  }
}

// status → CSS modifier + dot color (for the minimap + node border).
function statusMod(s: AgentNodeStatus): string {
  switch (s) {
    case "running":
      return "running";
    case "done":
      return "done";
    case "error":
      return "error";
    case "stopped":
      return "stopped";
    case "pending":
      return "pending";
    default:
      return "root";
  }
}
// Read a CSS variable from :root so the minimap follows the active theme.
function readVar(name: string, fallback: string): string {
  if (typeof window === "undefined") return fallback;
  const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return v || fallback;
}
function statusColor(s: AgentNodeStatus): string {
  switch (s) {
    case "running":
      return readVar("--trace-running", "#d97757");
    case "done":
      return readVar("--trace-done", "#74b87a");
    case "error":
      return readVar("--trace-error", "#e0696a");
    case "stopped":
      return readVar("--trace-stopped", "#d9a441");
    default:
      return readVar("--trace-pending", "#858b96");
  }
}

// Status filter values for the canvas toolbar. "all" is the no-filter default;
// the rest mirror AgentNodeStatus minus "root"/"stopped" (root is structural,
// stopped folds under the error/pending buckets for filtering purposes).
type StatusFilter = "all" | "running" | "done" | "error" | "pending";

function filterLabel(t: Translator, s: StatusFilter): string {
  switch (s) {
    case "all":
      return t("trace.filterAll");
    case "running":
      return t("trace.filterRunning");
    case "done":
      return t("trace.filterDone");
    case "error":
      return t("trace.filterError");
    case "pending":
      return t("trace.filterPending");
  }
}

const STATUS_FILTERS: StatusFilter[] = ["all", "running", "done", "error", "pending"];

// ── Custom node ───────────────────────────────────────────────
const AgentNode = memo(function AgentNode({ data, selected }: NodeProps<AgentNodeType>) {
  const t = useT();
  const isRoot = data.kind === "root";
  const isStep = data.kind === "step";
  const mod = statusMod(data.status);
  return (
    <div
      className={`agent-node agent-node--${mod}${isRoot ? " agent-node--root" : ""}${isStep ? " agent-node--step" : ""}${data.isSubagent ? " agent-node--sub" : ""}${selected ? " agent-node--sel" : ""}`}
    >
      <Handle type="target" position={Position.Left} className="agent-node__handle" />
      <div className="agent-node__head">
        <span className={`agent-node__icon agent-node__icon--${mod}`}>
          <StatusIcon status={data.status} />
        </span>
        <span className="agent-node__name" title={isRoot ? t("trace.root") : data.label}>
          {isRoot ? t("trace.root") : data.label}
        </span>
        {!isRoot && !isStep && data.durationMs != null && (
          <span className="agent-node__dur">
            <Clock size={11} />
            {formatDuration(data.durationMs)}
          </span>
        )}
      </div>
      {!isRoot && !isStep && (
        <div className="agent-node__meta">
          {data.isSubagent && (
            <span className="agent-node__chip agent-node__chip--sub">
              <GitBranch size={10} />
              {data.profile?.model ?? "sub-agent"}
            </span>
          )}
          {data.readOnly && !data.isSubagent && (
            <span className="agent-node__chip">
              <Eye size={10} />ro
            </span>
          )}
          {data.error && <span className="agent-node__chip agent-node__chip--err">!</span>}
        </div>
      )}
      <Handle type="source" position={Position.Right} className="agent-node__handle" />
    </div>
  );
});

const nodeTypes: NodeTypes = { agentNode: AgentNode };

const defaultEdgeOptions: Partial<Edge> = {
  type: "smoothstep",
  markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
  style: { stroke: "var(--trace-edge)", strokeWidth: 1.5 },
};

// ── Detail side panel ─────────────────────────────────────────
function NodeDetail({
  node,
  onClose,
  onRetryTool,
  onCloseAgent,
}: {
  node: AgentNodeType | null;
  onClose: () => void;
  onRetryTool?: (toolId: string, toolName: string, args: string) => void;
  onCloseAgent?: (agentId: string) => void;
}) {
  const t = useT();
  const errRef = useRef<HTMLPreElement>(null);
  const [copied, setCopied] = useState(false);
  if (!node) {
    return (
      <aside className="agent-detail agent-detail--empty">
        <div className="agent-detail__hint">{t("trace.selectHint")}</div>
        <button className="agent-detail__close" onClick={onClose}>✕</button>
      </aside>
    );
  }
  const d = node.data;
  // Allow detail for tool, step, and agent nodes. Root stays a hint.
  if (d.kind !== "tool" && d.kind !== "step" && d.kind !== "agent") {
    return (
      <aside className="agent-detail agent-detail--empty">
        <div className="agent-detail__hint">{t("trace.selectHint")}</div>
        <button className="agent-detail__close" onClick={onClose}>✕</button>
      </aside>
    );
  }
  const isStep = d.kind === "step";
  const isAgent = d.kind === "agent";
  const args = d.args?.trim() || "";
  const err = d.error?.trim();

  const copyError = useCallback(async () => {
    if (!err) return;
    try {
      await navigator.clipboard.writeText(err);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch { /* clipboard may be unavailable */ }
  }, [err]);

  return (
    <aside className="agent-detail">
      <header className="agent-detail__head">
        <div className="agent-detail__title">
          {isStep ? <GitBranch size={14} /> : isAgent ? <GitBranch size={14} /> : <Terminal size={14} />}
          <span>{isStep ? d.label : isAgent ? d.label : d.toolName}</span>
        </div>
        <button className="agent-detail__close" onClick={onClose}>✕</button>
      </header>
      <div className="agent-detail__body">
        <div className="agent-detail__row">
          <span className="agent-detail__k">{t("trace.status")}</span>
          <span className={`agent-node__chip agent-node__chip--${statusMod(d.status)}`}>
            {stateLabel(t, d.status)}
          </span>
        </div>
        {!isStep && !isAgent && d.durationMs != null && (
          <div className="agent-detail__row">
            <span className="agent-detail__k"><Clock size={12} />{t("trace.duration")}</span>
            <span className="agent-detail__v">{formatDuration(d.durationMs)}</span>
          </div>
        )}
        {!isStep && !isAgent && d.profile?.model && (
          <div className="agent-detail__row">
            <span className="agent-detail__k"><Cpu size={12} />{t("trace.model")}</span>
            <span className="agent-detail__v">{d.profile.model}</span>
          </div>
        )}
        {!isStep && !isAgent && d.profile?.effort && (
          <div className="agent-detail__row">
            <span className="agent-detail__k">{t("trace.effort")}</span>
            <span className="agent-detail__v">{d.profile.effort}</span>
          </div>
        )}
        {!isStep && !isAgent && args && (
          <div className="agent-detail__section">
            <div className="agent-detail__k">{t("trace.args")}</div>
            <pre className="agent-detail__pre">{args}</pre>
          </div>
        )}
        {!isStep && !isAgent && err && (
          <div className="agent-detail__section">
            <div className="agent-detail__k agent-detail__k--err">
              {t("trace.error")}
              <button
                className="agent-detail__copy"
                onClick={copyError}
                title={copied ? t("trace.copied") : t("trace.copyError")}
                aria-label={t("trace.copyError")}
              >
                {copied ? <Check size={11} /> : <Copy size={11} />}
              </button>
            </div>
            <pre ref={errRef} className="agent-detail__pre agent-detail__pre--err">{err}</pre>
            {onRetryTool && d.status === "error" && (
              <button
                className="agent-detail__action"
                onClick={() => onRetryTool(node.id, d.toolName ?? "", args)}
              >
                <RotateCw size={11} /> {t("trace.retry")}
              </button>
            )}
          </div>
        )}
        {isAgent && onCloseAgent && d.status === "running" && d.agentId && (
          <div className="agent-detail__section">
            <button
              className="agent-detail__action agent-detail__action--danger"
              onClick={() => onCloseAgent(d.agentId!)}
            >
              <Square size={11} /> {t("trace.stopAgent")}
            </button>
          </div>
        )}
      </div>
    </aside>
  );
}

// ── Canvas ───────────────────────────────────────────────────
export function AgentCanvas({
  items,
  running,
  steps = [],
  agents = new Map(),
  onRetryTool,
  onCloseAgent,
  onClose,
}: {
  items: Item[];
  running: boolean;
  steps?: WireStep[];
  agents?: Map<string, AgentState>;
  onRetryTool?: (toolId: string, toolName: string, args: string) => void;
  onCloseAgent?: (agentId: string) => void;
  onClose?: () => void;
}) {
  const t = useT();
  const [selected, setSelected] = useState<AgentNodeType | null>(null);

  // P2-11: derive a stable signature of the tool/agent/step graph so the
  // expensive buildAgentGraph re-runs only when the structure changes, not
  // on every streaming text delta that mutates the items array reference.
  // Streaming text only changes the assistant bubble content — irrelevant to
  // the graph — so we filter to tool items + step/agent keys for the memo.
  const toolSignature = useMemo(() => {
    let sig = "";
    for (const it of items) {
      if (it.kind === "tool") {
        sig += `|${it.id}:${it.name}:${it.status}:${it.parentId ?? ""}:${it.turnIndex ?? 0}`;
      }
    }
    return sig;
  }, [items]);
  const agentSignature = useMemo(() => {
    let sig = "";
    for (const [id, st] of agents) sig += `|${id}:${st.kind}`;
    return sig;
  }, [agents]);
  const stepSignature = useMemo(() => {
    let sig = "";
    for (const s of steps) sig += `|${s.id}:${s.status}`;
    return sig;
  }, [steps]);

  const { nodes, edges } = useMemo(
    () => buildAgentGraph(items, running, steps, agents),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [toolSignature, agentSignature, stepSignature, running, items, agents, steps],
  );
  const summary = useMemo(() => summarizeGraph(items), [toolSignature]);

  // Search + status filter state. searchTerm matches data.label or toolName;
  // statusFilter narrows to a single AgentNodeStatus (or "all"). Non-matching
  // nodes get the .agent-node--dimmed class via node.className so React Flow
  // applies it to the wrapper without touching the memoised AgentNode body.
  const [searchTerm, setSearchTerm] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const hasFilter = searchTerm.trim() !== "" || statusFilter !== "all";

  const matchingIds = useMemo(() => {
    if (!hasFilter) return null;
    const term = searchTerm.trim().toLowerCase();
    const ids = new Set<string>();
    for (const n of nodes) {
      const d = n.data;
      let ok = true;
      if (term) {
        const label = (d.label ?? "").toLowerCase();
        const toolName = (d.toolName ?? "").toLowerCase();
        if (!label.includes(term) && !toolName.includes(term)) ok = false;
      }
      if (ok && statusFilter !== "all" && d.status !== statusFilter) ok = false;
      if (ok) ids.add(n.id);
    }
    return ids;
  }, [nodes, searchTerm, statusFilter, hasFilter]);

  const styledNodes = useMemo(() => {
    if (matchingIds == null) return nodes;
    return nodes.map((n) =>
      matchingIds.has(n.id)
        ? n
        : { ...n, className: `${n.className ?? ""} agent-node--dimmed`.trim() },
    );
  }, [nodes, matchingIds]);

  const resetFilter = useCallback(() => {
    setSearchTerm("");
    setStatusFilter("all");
  }, []);

  const onNodeClick = useCallback((_: unknown, node: AgentNodeType) => {
    setSelected(node);
  }, []);

  return (
    <div className="agent-canvas">
      <header className="agent-canvas__bar">
        <div className="agent-canvas__title">
          <GitBranch size={15} />
          <span>{t("trace.title")}</span>
        </div>
        <div className="agent-canvas__stats">
          <span className="agent-stat">
            <span className="agent-stat__k">{t("trace.calls")}</span>
            <span className="agent-stat__v">{summary.total}</span>
          </span>
          {summary.running > 0 && (
            <span className="agent-stat agent-stat--running">
              <Loader2 size={11} className="agent-node__spin" />
              {summary.running}
            </span>
          )}
          {summary.done > 0 && (
            <span className="agent-stat agent-stat--done">
              <Check size={11} />{summary.done}
            </span>
          )}
          {summary.error > 0 && (
            <span className="agent-stat agent-stat--error">
              <X size={11} />{summary.error}
            </span>
          )}
          <span className="agent-stat">
            <span className="agent-stat__k">{t("trace.depth")}</span>
            <span className="agent-stat__v">{summary.maxDepth}</span>
          </span>
        </div>
        {onClose && (
          <button
            type="button"
            className="agent-canvas__close"
            onClick={onClose}
            aria-label={t("trace.close")}
            title={t("trace.close")}
          >
            <X size={14} />
          </button>
        )}
      </header>

      {summary.total === 0 ? (
        <div className="agent-canvas__empty">
          <GitBranch size={28} />
          <p>{t("trace.empty")}</p>
        </div>
      ) : (
        <div className="agent-canvas__flow">
          <div className="agent-canvas__toolbar">
            <input
              className="agent-canvas__search"
              type="text"
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              placeholder={t("trace.searchPlaceholder")}
            />
            <div className="agent-canvas__filter-group">
              {STATUS_FILTERS.map((s) => (
                <button
                  key={s}
                  className={`agent-canvas__filter-btn${statusFilter === s ? " agent-canvas__filter-btn--on" : ""}`}
                  onClick={() => setStatusFilter(s)}
                >
                  {filterLabel(t, s)}
                </button>
              ))}
            </div>
            <button
              className="agent-canvas__filter-btn"
              onClick={resetFilter}
              disabled={!hasFilter}
            >
              {t("trace.reset")}
            </button>
          </div>
          <ReactFlow
            nodes={styledNodes}
            edges={edges}
            nodeTypes={nodeTypes}
            defaultEdgeOptions={defaultEdgeOptions}
            onNodeClick={onNodeClick}
            fitView
            fitViewOptions={{ padding: 0.18 }}
            minZoom={0.1}
            proOptions={{ hideAttribution: true }}
          >
            <Background gap={18} size={1} />
            <Controls showInteractive={false} />
            <MiniMap
              pannable
              zoomable
              nodeColor={(n) => statusColor((n.data as AgentNodeData).status)}
              nodeStrokeWidth={2}
              maskColor="rgba(110,118,129,0.18)"
            />
          </ReactFlow>
          {selected && (
            <NodeDetail
              node={selected}
              onClose={() => setSelected(null)}
              onRetryTool={onRetryTool}
              onCloseAgent={onCloseAgent}
            />
          )}
        </div>
      )}
    </div>
  );
}
