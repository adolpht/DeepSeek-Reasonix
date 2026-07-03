// Agent execution-flow graph builder.
//
// Turns the flat `state.items` stream (from useController) into a React Flow
// node/edge model. Tool calls carry an optional `parentId` (set by the Go
// kernel on a sub-agent's calls — see internal/agent/task.go `subSink` and
// pool.go) which already encodes the parent→child nesting we visualise here.
//
// Layout is a simple post-order tree: leaves get successive y slots, parents
// sit at the mean y of their children, depth drives x. This keeps related
// branches grouped without pulling in a full dagre/elkjs dependency.

import type { Edge, Node } from "@xyflow/react";
import type { Item } from "./useController";
import type { WireStep } from "./types";

export type AgentNodeStatus =
  | "running"
  | "done"
  | "error"
  | "stopped"
  | "pending"
  | "root";

// AgentNodeData carries every field a node needs to render and to power the
// NodeDetail panel. The `[key: string]: unknown` index signature is required
// by @xyflow/react v12's Node<T> constraint (T must extend Record<string,
// unknown>); typed fields above it still give us autocomplete on the known
// keys, and `extra` is the escape hatch for ad-hoc React Flow metadata.
export interface AgentNodeData {
  label: string;
  kind: "root" | "tool" | "step" | "agent";
  toolName?: string;
  status: AgentNodeStatus;
  durationMs?: number;
  profile?: { model?: string; effort?: string };
  isSubagent?: boolean;
  args?: string;
  readOnly?: boolean;
  error?: string;
  agentId?: string;
  stepId?: string;
  turnIndex?: number;
  extra?: Record<string, unknown>;
  [key: string]: unknown;
}

// Tools whose dispatch spawns a sub-agent — they carry model/effort profiles
// and form the branching points of the call tree.
const SUBAGENT_TOOLS = new Set([
  "task",
  "run_skill",
  "explore",
  "research",
  "review",
  "security_review",
]);

const ROOT_ID = "__session_root__";
const NODE_W = 232;
const NODE_H = 68;
const COL_GAP = 96;
const ROW_GAP = 28;

type ToolItem = Extract<Item, { kind: "tool" }>;

export interface AgentGraph {
  nodes: Node<AgentNodeData, "agentNode">[];
  edges: Edge[];
}

export function buildAgentGraph(
  items: Item[],
  running: boolean,
  steps: WireStep[] = [],
  agents: Map<string, AgentState> = new Map(),
  maxNodes = 200,
): AgentGraph {
  const allTools = items.filter((it): it is ToolItem => it.kind === "tool");

  // Cap the tool count to keep the canvas responsive on long sessions. We
  // keep the LAST maxNodes tools (most recent activity) and then derive
  // childrenByParent / nodeY / nodeDepth ONLY from the kept set, so no edge
  // ever points at a truncated node (the previous implementation computed
  // those maps over the full set and produced dangling edges — P0-3).
  const tools = allTools.length > maxNodes ? allTools.slice(-maxNodes) : allTools;
  const keptIds = new Set(tools.map((t) => t.id));
  const nodes: Node<AgentNodeData, "agentNode">[] = [];
  const edges: Edge[] = [];

  // group children by parent; top-level tools attach to the virtual root.
  // A parentId that points at a truncated tool is treated as top-level so
  // the child still renders (attached to root) rather than vanishing.
  const childrenByParent = new Map<string, ToolItem[]>();
  const topLevel: ToolItem[] = [];
  for (const t of tools) {
    if (t.parentId && keptIds.has(t.parentId)) {
      const arr = childrenByParent.get(t.parentId) ?? [];
      arr.push(t);
      childrenByParent.set(t.parentId, arr);
    } else {
      topLevel.push(t);
    }
  }

  // post-order y assignment: leaves get successive slots, parents take mean.
  const nodeY = new Map<string, number>();
  let leafCount = 0;
  const computeY = (id: string): number => {
    const kids = childrenByParent.get(id) ?? [];
    if (kids.length === 0) {
      const y = leafCount++;
      nodeY.set(id, y);
      return y;
    }
    let sum = 0;
    for (const k of kids) sum += computeY(k.id);
    const y = sum / kids.length;
    nodeY.set(id, y);
    return y;
  };
  for (const t of topLevel) computeY(t.id);
  nodeY.set(ROOT_ID, leafCount ? (leafCount - 1) / 2 : 0);

  // depth + edges via DFS from root.
  const nodeDepth = new Map<string, number>();
  nodeDepth.set(ROOT_ID, 0);
  const place = (id: string, parentId: string, depth: number) => {
    nodeDepth.set(id, depth);
    edges.push({
      id: `e-${parentId}-${id}`,
      source: parentId,
      target: id,
      type: "smoothstep",
    });
    const kids = childrenByParent.get(id) ?? [];
    for (const k of kids) place(k.id, id, depth + 1);
  };
  for (const t of topLevel) place(t.id, ROOT_ID, 1);

  // root node — the session/turn origin.
  nodes.push({
    id: ROOT_ID,
    type: "agentNode" as const,
    position: { x: 0, y: (nodeY.get(ROOT_ID) ?? 0) * (NODE_H + ROW_GAP) },
    data: {
      label: running ? "run" : "session",
      kind: "root",
      status: running ? "running" : "root",
    },
  });

  // plan steps — sit above the root as a vertical chain. When a step carries
  // a turnIndex we also draw a step→tool edge to every top-level tool whose
  // turnIndex matches (P1-7), so users can see which tools realised a step.
  const toolsByTurn = new Map<number, ToolItem[]>();
  for (const t of topLevel) {
    if (t.turnIndex == null) continue;
    const arr = toolsByTurn.get(t.turnIndex) ?? [];
    arr.push(t);
    toolsByTurn.set(t.turnIndex, arr);
  }
  for (let i = 0; i < steps.length; i++) {
    const s = steps[i];
    const st: AgentNodeStatus =
      s.status === "completed" ? "done" : s.status === "in_progress" ? "running" : "pending";
    nodes.push({
      id: s.id,
      type: "agentNode" as const,
      position: { x: 0, y: -(steps.length - i) * (NODE_H + ROW_GAP) },
      data: { label: s.label, kind: "step", status: st, stepId: s.id, turnIndex: s.turnIndex },
    });
    edges.push({
      id: `e-${ROOT_ID}-${s.id}`,
      source: ROOT_ID,
      target: s.id,
      type: "smoothstep",
    });
    // step → tool edges (one per top-level tool sharing the step's turnIndex).
    const linked = toolsByTurn.get(s.turnIndex) ?? [];
    for (const t of linked) {
      edges.push({
        id: `e-${s.id}-${t.id}`,
        source: s.id,
        target: t.id,
        type: "smoothstep",
        style: { stroke: "var(--trace-edge)", strokeDasharray: "4 3", opacity: 0.6 },
      });
    }
  }

  // agent nodes — child agents spawned via spawn_agent tool.
  // They form the root of their own tool sub-tree.
  let agentY = leafCount;
  for (const [agentID, state] of agents) {
    const status: AgentNodeStatus =
      state.kind === "agent_completed" ? "done" :
      state.kind === "agent_closed" ? "stopped" : "running";
    const label = state.role || "agent";
    nodeY.set(agentID, agentY);
    nodeDepth.set(agentID, 0);
    nodes.push({
      id: agentID,
      type: "agentNode" as const,
      position: { x: 0, y: agentY * (NODE_H + ROW_GAP) },
      data: {
        label: label,
        kind: "agent",
        status: status,
        isSubagent: true,
        agentId: agentID,
      },
    });
    edges.push({
      id: `e-${ROOT_ID}-${agentID}`,
      source: ROOT_ID,
      target: agentID,
      type: "smoothstep",
    });
    agentY++;
  }

  // tool nodes.
  for (const t of tools) {
    nodes.push({
      id: t.id,
      type: "agentNode" as const,
      position: {
        x: (nodeDepth.get(t.id) ?? 1) * (NODE_W + COL_GAP),
        y: (nodeY.get(t.id) ?? 0) * (NODE_H + ROW_GAP),
      },
      data: {
        label: t.name,
        kind: "tool",
        toolName: t.name,
        status: t.status,
        durationMs: t.durationMs,
        profile: t.profile,
        isSubagent: SUBAGENT_TOOLS.has(t.name),
        args: t.args,
        readOnly: t.readOnly,
        error: t.error,
        turnIndex: t.turnIndex,
      },
    });
  }

  return { nodes, edges };
}

export type AgentKind = "agent_spawned" | "agent_progress" | "agent_completed" | "agent_closed";

export interface AgentState {
  kind: AgentKind;
  role?: string;
  output?: string;
}

// Tool call count + per-status breakdown, for the trace header summary.
export interface GraphSummary {
  total: number;
  running: number;
  done: number;
  error: number;
  stopped: number;
  maxDepth: number;
}

export function summarizeGraph(items: Item[]): GraphSummary {
  // Build a parent→children index first so depthOf can recurse correctly even
  // when items arrive out of topology order (sub-agent tool events can land
  // before the parent task dispatch when streams interleave). The previous
  // implementation only looked up `seen` for the parent, but the parent was
  // never inserted until after its own line was processed — so depth was
  // always 1 (P0-2).
  const childrenByParent = new Map<string, ToolItem[]>();
  const toolsById = new Map<string, ToolItem>();
  const topLevel: ToolItem[] = [];
  for (const it of items) {
    if (it.kind !== "tool") continue;
    toolsById.set(it.id, it);
  }
  for (const it of items) {
    if (it.kind !== "tool") continue;
    if (it.parentId && toolsById.has(it.parentId)) {
      const arr = childrenByParent.get(it.parentId) ?? [];
      arr.push(it);
      childrenByParent.set(it.parentId, arr);
    } else {
      topLevel.push(it);
    }
  }
  const seen = new Map<string, number>();
  const depthOf = (id: string, visiting = new Set<string>()): number => {
    if (seen.has(id)) return seen.get(id)!;
    if (visiting.has(id)) return 0; // cycle guard
    visiting.add(id);
    const kids = childrenByParent.get(id) ?? [];
    if (kids.length === 0) {
      seen.set(id, 1);
      return 1;
    }
    let maxChild = 0;
    for (const k of kids) {
      const d = depthOf(k.id, visiting);
      if (d > maxChild) maxChild = d;
    }
    const d = maxChild + 1;
    seen.set(id, d);
    return d;
  };
  let total = 0;
  let running = 0;
  let done = 0;
  let error = 0;
  let stopped = 0;
  let depth = 0;
  for (const it of items) {
    if (it.kind !== "tool") continue;
    total++;
    if (it.status === "running") running++;
    else if (it.status === "done") done++;
    else if (it.status === "error") error++;
    else if (it.status === "stopped") stopped++;
    const d = depthOf(it.id);
    if (d > depth) depth = d;
  }
  return { total, running, done, error, stopped, maxDepth: depth };
}

export { ROOT_ID as AGENT_ROOT_ID };
