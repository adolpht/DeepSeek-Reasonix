// Workflow DAG visual editor.
//
// Allows users to create, edit, save, load, delete and run workflows as
// interactive directed-acyclic graphs.  Each node represents a step (skill,
// prompt, tool, condition, parallel) and edges define execution order.

import { useCallback, useMemo, useState, memo, useRef, useEffect } from "react";
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
  type OnConnect,
  type Connection,
  addEdge,
  MarkerType,
  useNodesState,
  useEdgesState,
  ReactFlowProvider,
  type OnNodesChange,
  type OnEdgesChange,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  Plus,
  Trash2,
  Play,
  Save,
  Workflow as WorkflowIcon,
  Sparkles,
  MessageSquare,
  Wrench,
  GitBranch,
  Layers,
  ChevronRight,
  FolderOpen,
  ArrowRightLeft,
} from "lucide-react";
import { useT, type DictKey } from "../lib/i18n";
import { X } from "lucide-react";
// ── Types ────────────────────────────────────────────────────

interface WorkflowNodeView {
  id: string;
  label: string;
  kind: "skill" | "prompt" | "tool" | "condition" | "parallel";
  config: string;
  model?: string;
  effort?: string;
  requireApproval?: boolean;
  positionX: number;
  positionY: number;
}

// WorkflowEditorNodeData is the data payload carried by a react-flow node.
// Mirrors WorkflowNodeView minus the id/position (those live on the Node
// wrapper). requireApproval is included so the detail panel's checkbox
// round-trips through the canvas state.
type WorkflowEditorNodeData = {
  label: string;
  kind: string;
  config: string;
  model?: string;
  effort?: string;
  requireApproval?: boolean;
  [key: string]: unknown;
};

interface WorkflowEdgeView {
  id: string;
  source: string;
  target: string;
  label?: string;
}

type WorkflowTrigger = "manual" | "cron" | "event";

interface WorkflowView {
  name: string;
  description: string;
  nodes: WorkflowNodeView[];
  edges: WorkflowEdgeView[];
  trigger?: WorkflowTrigger;
  cronExpr?: string;
  eventType?: string;
  matchRules?: Record<string, string>;
  errorStrategy?: "continue" | "stop";
  version?: number;
  allowedSkills?: string[];
  createdAt: number;
  updatedAt: number;
}

// SkillInfo mirrors desktop SkillView (the subset we need for the picker).
interface SkillInfo {
  name: string;
  description: string;
  scope: string;
  runAs: string;
  enabled: boolean;
}

// ModelOption mirrors desktop ModelInfo.
interface ModelOption {
  ref: string;
  provider: string;
  model: string;
  current: boolean;
}

// CapabilitiesView is the subset of App.Capabilities() we use (skills list).
interface CapabilitiesView {
  skills: SkillInfo[];
}

// RecipeView mirrors desktop RecipeView for the migration flow.
interface RecipeView {
  name: string;
  description: string;
  skill: string;
  params: string;
  trigger: string;
}

// WorkflowRunStateView mirrors desktop WorkflowRunStateView.
interface WorkflowRunStateView {
  workflowName: string;
  tabId: string;
  status: "running" | "completed" | "failed" | "aborted" | "skipped";
  nodes: WorkflowNodeRunView[];
  startedAt: number;
  finishedAt: number;
  error?: string;
}

// WorkflowNodeRunView mirrors desktop WorkflowNodeRunView.
interface WorkflowNodeRunView {
  id: string;
  label: string;
  kind: string;
  status: "running" | "completed" | "failed" | "aborted" | "skipped";
  error?: string;
}

type WorkflowEditorNodeType = Node<WorkflowEditorNodeData, "wfNode">;

// ── Kind metadata ────────────────────────────────────────────

type NodeKind = "skill" | "prompt" | "tool" | "condition" | "parallel";

const KIND_META: Record<NodeKind, { icon: typeof Sparkles; color: string; label: string; labelKey: DictKey }> = {
  skill: { icon: Sparkles, color: "#bc8cff", label: "Skill", labelKey: "wf.kind.skill" },
  prompt: { icon: MessageSquare, color: "#58a6ff", label: "Prompt", labelKey: "wf.kind.prompt" },
  tool: { icon: Wrench, color: "#3fb950", label: "Tool", labelKey: "wf.kind.tool" },
  condition: { icon: GitBranch, color: "#d9a441", label: "Condition", labelKey: "wf.kind.condition" },
  parallel: { icon: Layers, color: "#f78166", label: "Parallel", labelKey: "wf.kind.parallel" },
};

// ── Wails bindings ───────────────────────────────────────────

const wails = {
  async listWorkflows(): Promise<WorkflowView[]> {
    const result = await (window as any).go.main.App.ListWorkflows();
    return result ?? [];
  },
  async saveWorkflow(wf: WorkflowView): Promise<void> {
    await (window as any).go.main.App.SaveWorkflow(wf);
  },
  async loadWorkflow(name: string): Promise<WorkflowView | null> {
    const result = await (window as any).go.main.App.LoadWorkflow(name);
    return result ?? null;
  },
  async deleteWorkflow(name: string): Promise<void> {
    await (window as any).go.main.App.DeleteWorkflow(name);
  },
  async runWorkflow(name: string, input?: string): Promise<void> {
    await (window as any).go.main.App.RunWorkflow(name, input ?? "");
  },
  async stopWorkflow(name: string): Promise<void> {
    await (window as any).go.main.App.StopWorkflow(name);
  },
  async getWorkflowRunState(name: string): Promise<WorkflowRunStateView | null> {
    const result = await (window as any).go.main.App.GetWorkflowRunState(name);
    return result ?? null;
  },
  async validateWorkflow(wf: WorkflowView): Promise<string | null> {
    try {
      await (window as any).go.main.App.ValidateWorkflow(wf);
      return null;
    } catch (e: any) {
      return e?.message ?? String(e);
    }
  },
  async duplicateWorkflow(srcName: string, newName: string): Promise<string> {
    const result = await (window as any).go.main.App.DuplicateWorkflow(srcName, newName);
    return result ?? "";
  },
  async capabilities(): Promise<CapabilitiesView> {
    const result = await (window as any).go.main.App.Capabilities();
    return result ?? { skills: [] };
  },
  async models(): Promise<ModelOption[]> {
    const result = await (window as any).go.main.App.Models();
    return result ?? [];
  },
  async listRecipes(): Promise<RecipeView[]> {
    const result = await (window as any).go.main.App.ListRecipes();
    return result ?? [];
  },
  async migrateRecipeToWorkflow(name: string): Promise<void> {
    await (window as any).go.main.App.MigrateRecipeToWorkflow(name);
  },
  async reloadWorkflowTriggers(): Promise<void> {
    await (window as any).go.main.App.ReloadWorkflowTriggers();
  },
};

// ── Converters ───────────────────────────────────────────────

function viewToNodes(views: WorkflowNodeView[]): WorkflowEditorNodeType[] {
  return (views ?? []).map((v) => ({
    id: v.id,
    type: "wfNode" as const,
    position: { x: v.positionX, y: v.positionY },
    data: {
      label: v.label,
      kind: v.kind,
      config: v.config,
      model: v.model,
      effort: v.effort,
      requireApproval: v.requireApproval ?? false,
    },
  }));
}

function viewToEdges(views: WorkflowEdgeView[]): Edge[] {
  return (views ?? []).map((v) => ({
    id: v.id,
    source: v.source,
    target: v.target,
    label: v.label,
  }));
}

function nodesToView(nodes: WorkflowEditorNodeType[]): WorkflowNodeView[] {
  return nodes.map((n) => ({
    id: n.id,
    label: (n.data.label as string) ?? "",
    kind: (n.data.kind as NodeKind) ?? "prompt",
    config: (n.data.config as string) ?? "",
    model: n.data.model as string | undefined,
    effort: n.data.effort as string | undefined,
    requireApproval: (n.data.requireApproval as boolean) ?? false,
    // Round to int — Go's WorkflowNodeView.PositionX/Y are int fields, and
    // encoding/json refuses to unmarshal a fractional number into an int
    // (errors with "cannot unmarshal number 234.567 into Go struct field
    // of type int"). ReactFlow positions are floats; rounding loses at
    // most half a pixel, which is imperceptible on the canvas.
    positionX: Math.round(n.position.x),
    positionY: Math.round(n.position.y),
  }));
}

function edgesToView(edges: Edge[]): WorkflowEdgeView[] {
  return edges.map((e) => ({
    id: e.id,
    source: e.source,
    target: e.target,
    label: e.label as string | undefined,
  }));
}

// ── Custom node component ────────────────────────────────────

const WfNode = memo(function WfNode({ data, selected }: NodeProps<WorkflowEditorNodeType>) {
  const t = useT();
  const kind = (data.kind as NodeKind) ?? "prompt";
  const meta = KIND_META[kind] ?? KIND_META.prompt;
  const Icon = meta.icon;

  return (
    <div
      className={`wf-node wf-node--${kind}${selected ? " wf-node--sel" : ""}`}
    >
      <Handle type="target" position={Position.Top} className="wf-node__handle" />
      <div className="wf-node__head">
        <span className="wf-node__icon" style={{ color: meta.color }}>
          <Icon size={14} />
        </span>
        <span className="wf-node__label">{data.label || t(meta.labelKey)}</span>
      </div>
      <div className="wf-node__kind">{t(meta.labelKey)}</div>
      <Handle type="source" position={Position.Bottom} className="wf-node__handle" />
    </div>
  );
});

const nodeTypes: NodeTypes = { wfNode: WfNode };

const defaultEdgeOptions: Partial<Edge> = {
  type: "smoothstep",
  markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
  style: { stroke: "#6e7681", strokeWidth: 1.5 },
};

// ── Helper ───────────────────────────────────────────────────

let idCounter = 0;
function nextId(): string {
  return `n_${Date.now()}_${++idCounter}`;
}

// parseSkillConfig extracts {name, arguments} from a skill node's config.
// Accepts JSON ({"name":"x","arguments":"y"}) or plain text ("x arg1 arg2"
// or "/x arg1 arg2"); the first token is the skill name, the rest is args.
function parseSkillConfig(config: string): { name: string; arguments: string } {
  if (!config) return { name: "", arguments: "" };
  try {
    const p = JSON.parse(config);
    if (p && typeof p.name === "string") {
      return { name: p.name, arguments: typeof p.arguments === "string" ? p.arguments : "" };
    }
  } catch {
    // not JSON — fall through to plain-text parsing
  }
  const trimmed = config.trim().replace(/^\//, "");
  const spaceIdx = trimmed.indexOf(" ");
  if (spaceIdx < 0) return { name: trimmed, arguments: "" };
  return { name: trimmed.slice(0, spaceIdx), arguments: trimmed.slice(spaceIdx + 1) };
}

// buildSkillConfig composes a skill node's config JSON from name + arguments.
// Empty name + empty arguments yields an empty string (so the node shows as
// "unconfigured" rather than "{}").
function buildSkillConfig(name: string, args: string): string {
  const trimmedName = name.trim();
  const trimmedArgs = args.trim();
  if (!trimmedName && !trimmedArgs) return "";
  if (!trimmedArgs) return JSON.stringify({ name: trimmedName });
  return JSON.stringify({ name: trimmedName, arguments: trimmedArgs });
}

// matchRulesToText serializes a match-rules map to the "key=value\n" form used
// by the trigger bar's textarea. Keys/values containing "=" are preserved as-is
// (split on the first "=" only when parsing back).
function matchRulesToText(rules?: Record<string, string>): string {
  if (!rules) return "";
  return Object.entries(rules)
    .map(([k, v]) => `${k}=${v}`)
    .join("\n");
}

// matchRulesFromText parses the textarea's "key=value\n…" text back into a
// map. Blank lines and lines without "=" are ignored. Returns undefined when
// the map is empty so the saved JSON omits the field (matches Recipe shape).
function matchRulesFromText(text: string): Record<string, string> | undefined {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const eq = trimmed.indexOf("=");
    if (eq < 0) continue;
    const k = trimmed.slice(0, eq).trim();
    const v = trimmed.slice(eq + 1).trim();
    if (k) out[k] = v;
  }
  return Object.keys(out).length > 0 ? out : undefined;
}

// ── Node detail panel ────────────────────────────────────────

function NodeDetailPanel({
  node,
  onUpdate,
  onClose,
  skills,
  models,
}: {
  node: WorkflowEditorNodeType;
  onUpdate: (id: string, data: Partial<WorkflowEditorNodeData>) => void;
  onClose: () => void;
  skills: SkillInfo[];
  models: ModelOption[];
}) {
  const t = useT();
  const kind = (node.data.kind as NodeKind) ?? "prompt";
  const meta = KIND_META[kind] ?? KIND_META.prompt;
  const Icon = meta.icon;

  const handleChange = (field: string, value: unknown) => {
    onUpdate(node.id, { [field]: value });
  };

  return (
    <aside className="wf-detail">
      <header className="wf-detail__head">
        <div className="wf-detail__title">
          <Icon size={14} style={{ color: meta.color }} />
          <span>{node.data.label || t(meta.labelKey)}</span>
        </div>
        <button className="wf-detail__close" onClick={onClose}>✕</button>
      </header>
      <div className="wf-detail__body">
        <label className="wf-detail__field">
          <span className="wf-detail__k">{t("wf.label")}</span>
          <input
            className="wf-detail__input"
            value={node.data.label as string}
            onChange={(e) => handleChange("label", e.target.value)}
          />
        </label>

        <label className="wf-detail__field">
          <span className="wf-detail__k">{t("wf.kindLabel")}</span>
          <select
            className="wf-detail__select"
            value={node.data.kind as string}
            onChange={(e) => handleChange("kind", e.target.value)}
          >
            {Object.entries(KIND_META).map(([k, m]) => (
              <option key={k} value={k}>{t(m.labelKey)}</option>
            ))}
          </select>
        </label>

        {kind === "skill" ? (
          <>
            <label className="wf-detail__field">
              <span className="wf-detail__k">{t("wf.skill")}</span>
              <select
                className="wf-detail__select"
                value={parseSkillConfig((node.data.config as string) ?? "").name}
                onChange={(e) => {
                  const prev = parseSkillConfig((node.data.config as string) ?? "");
                  handleChange("config", buildSkillConfig(e.target.value, prev.arguments));
                  // Auto-fill label if the user hasn't customized it.
                  const currentLabel = (node.data.label as string) ?? "";
                  if (!currentLabel || currentLabel === KIND_META.skill.label || prev.name === currentLabel) {
                    handleChange("label", e.target.value);
                  }
                }}
              >
                <option value="">{t("wf.selectSkill")}</option>
                {skills.map((s) => (
                  <option key={s.name} value={s.name}>
                    {s.name}{s.runAs === "subagent" ? " [subagent]" : ""}{s.enabled === false ? " (disabled)" : ""}
                  </option>
                ))}
              </select>
            </label>
            <label className="wf-detail__field">
              <span className="wf-detail__k">{t("wf.arguments")}</span>
              <textarea
                className="wf-detail__textarea"
                value={parseSkillConfig((node.data.config as string) ?? "").arguments}
                onChange={(e) => {
                  const prev = parseSkillConfig((node.data.config as string) ?? "");
                  handleChange("config", buildSkillConfig(prev.name, e.target.value));
                }}
                rows={3}
                placeholder={t("wf.argumentsPlaceholder")}
              />
            </label>
          </>
        ) : (
          <label className="wf-detail__field">
            <span className="wf-detail__k">{t("wf.config")}</span>
            <textarea
              className="wf-detail__textarea"
              value={node.data.config as string}
              onChange={(e) => handleChange("config", e.target.value)}
              rows={4}
              placeholder={
                kind === "prompt"
                  ? t("wf.configPrompt")
                  : kind === "tool"
                    ? t("wf.configTool")
                    : t("wf.configDefault")
              }
            />
          </label>
        )}

        <label className="wf-detail__field">
          <span className="wf-detail__k">{t("wf.model")}</span>
          <select
            className="wf-detail__select"
            value={(node.data.model as string) ?? ""}
            onChange={(e) => handleChange("model", e.target.value)}
          >
            <option value="">{t("wf.modelInherit")}</option>
            {models.map((m) => (
              <option key={m.ref} value={m.ref}>
                {m.ref}{m.current ? ` ${t("wf.modelActive")}` : ""}
              </option>
            ))}
          </select>
        </label>

        <label className="wf-detail__field">
          <span className="wf-detail__k">{t("wf.effort")}</span>
          <select
            className="wf-detail__select"
            value={(node.data.effort as string) ?? ""}
            onChange={(e) => handleChange("effort", e.target.value)}
          >
            <option value="">{t("wf.effortDefault")}</option>
            <option value="low">{t("wf.effortLow")}</option>
            <option value="medium">{t("wf.effortMedium")}</option>
            <option value="high">{t("wf.effortHigh")}</option>
          </select>
        </label>

        {kind !== "condition" && kind !== "parallel" && (
          <label className="wf-detail__field wf-detail__field--check">
            <input
              type="checkbox"
              checked={(node.data.requireApproval as boolean) ?? false}
              onChange={(e) => handleChange("requireApproval", e.target.checked)}
            />
            <span className="wf-detail__k">{t("wf.requireApproval")}</span>
          </label>
        )}
      </div>
    </aside>
  );
}

// ── Main component ───────────────────────────────────────────

export function WorkflowEditor({ onClose }: { onClose?: () => void }) {
  const t = useT();
  const [workflows, setWorkflows] = useState<WorkflowView[]>([]);
  const [currentWorkflow, setCurrentWorkflow] = useState<WorkflowView | null>(null);
  const [selectedNode, setSelectedNode] = useState<WorkflowEditorNodeType | null>(null);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [validationErrors, setValidationErrors] = useState<string | null>(null);
  // Available skills + models loaded once for the node detail panel's pickers.
  // Both come from the active tab's configuration, so they stay valid across
  // workflow switches.
  const [skills, setSkills] = useState<SkillInfo[]>([]);
  const [models, setModels] = useState<ModelOption[]>([]);
  // Raw text for the allowed-skills input. We keep a separate string state so
  // the user can type "a, b," (trailing comma) without the controlled value
  // snapping back to "a, b" on each keystroke — the normalised list is still
  // written to currentWorkflow.allowedSkills on every change, this just keeps
  // the textbox text stable between keystrokes.
  const [allowedSkillsText, setAllowedSkillsText] = useState("");
  // Run state for the current workflow (null when not running / never run).
  const [runState, setRunState] = useState<WorkflowRunStateView | null>(null);
  // Polling timer ref for run state updates.
  const runStateTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Canvas state lives in the main component (not inside the canvas child) so
  // that handleAddNode / handleNodeDataUpdate can mutate it directly without
  // round-tripping through currentWorkflow and a useEffect reset. This is what
  // keeps user-drawn edges, node deletions, and dragged positions from being
  // clobbered when a property edit updates currentWorkflow.
  const [nodes, setNodes, onNodesChange] = useNodesState<WorkflowEditorNodeType>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  // Track the currently-loaded workflow name so the sync effect only fires
  // when the user switches to a *different* workflow (or creates a new one).
  // Property edits / node additions change currentWorkflow's contents but NOT
  // its name, so they must not trigger a canvas reset.
  const loadedWorkflowName = useRef<string | null>(null);

  // Load workflow list + capabilities (skills, models) on mount.
  useEffect(() => {
    (async () => {
      try {
        const [list, caps, modelList] = await Promise.all([
          wails.listWorkflows(),
          wails.capabilities(),
          wails.models(),
        ]);
        setWorkflows(list ?? []);
        setSkills(caps?.skills ?? []);
        setModels(modelList ?? []);
      } catch (e: any) {
        setError(e?.message ?? t("wf.errLoad"));
      }
    })();
  }, [t]);

  const refreshList = useCallback(async () => {
    try {
      const list = await wails.listWorkflows();
      setWorkflows(list ?? []);
    } catch (e: any) {
      setError(e?.message ?? t("wf.errRefresh"));
    }
  }, [t]);

  const handleNewWorkflow = useCallback(() => {
    const name = t("wf.defaultName", { ts: Date.now() });
    const wf: WorkflowView = {
      name,
      description: "",
      nodes: [],
      edges: [],
      createdAt: Date.now(),
      updatedAt: Date.now(),
    };
    setCurrentWorkflow(wf);
    setSelectedNode(null);
  }, [t]);

  const handleSelectWorkflow = useCallback(
    async (name: string) => {
      try {
        setError(null);
        const wf = await wails.loadWorkflow(name);
        if (wf) {
          setCurrentWorkflow(wf);
          setSelectedNode(null);
        }
      } catch (e: any) {
        setError(e?.message ?? t("wf.errLoadOne"));
      }
    },
    [t],
  );

  const handleSave = useCallback(async () => {
    if (!currentWorkflow) return;

    // Read directly from the lifted canvas state — no ref round-trip needed.
    const wf: WorkflowView = {
      ...currentWorkflow,
      nodes: nodesToView(nodes),
      edges: edgesToView(edges),
      updatedAt: Date.now(),
    };

    try {
      await wails.saveWorkflow(wf);
      setCurrentWorkflow(wf);
      await refreshList();
      // Refresh cron schedules so a trigger change takes effect immediately.
      // Safe to fire-and-forget; ReloadWorkflowTriggers is best-effort.
      void wails.reloadWorkflowTriggers();
      setError(null);
      setValidationErrors(null);
    } catch (e: any) {
      const msg = e?.message ?? t("wf.errSave");
      setError(msg);
      // If the error is a validation error, also set validation errors.
      if (msg.includes("validation")) {
        setValidationErrors(msg);
      }
    }
  }, [currentWorkflow, refreshList, t, nodes, edges]);

  const handleDelete = useCallback(async () => {
    if (!currentWorkflow) return;
    try {
      await wails.deleteWorkflow(currentWorkflow.name);
      setCurrentWorkflow(null);
      setSelectedNode(null);
      setNodes([]);
      setEdges([]);
      loadedWorkflowName.current = null;
      await refreshList();
      // Prune the deleted workflow's cron entry on next restart.
      void wails.reloadWorkflowTriggers();
      setError(null);
    } catch (e: any) {
      setError(e?.message ?? t("wf.errDelete"));
    }
  }, [currentWorkflow, refreshList, t, setNodes, setEdges]);

  const handleRun = useCallback(async () => {
    if (!currentWorkflow) return;
    try {
      // Auto-save before running so the backend loads the latest canvas
      // state. Without this, RunWorkflow reads the on-disk file which may
      // be stale (or missing for a newly-created unsaved workflow, causing
      // a confusing "not found" error).
      const wf: WorkflowView = {
        ...currentWorkflow,
        nodes: nodesToView(nodes),
        edges: edgesToView(edges),
        updatedAt: Date.now(),
      };
      await wails.saveWorkflow(wf);
      setCurrentWorkflow(wf);
      await wails.runWorkflow(wf.name);
      setError(null);
      setValidationErrors(null);
      // Start polling run state.
      startRunStatePolling(wf.name);
    } catch (e: any) {
      setError(e?.message ?? t("wf.errRun"));
    }
  }, [currentWorkflow, t, nodes, edges]);

  const handleStop = useCallback(async () => {
    if (!currentWorkflow) return;
    try {
      await wails.stopWorkflow(currentWorkflow.name);
      // One final poll to get the aborted state.
      const rs = await wails.getWorkflowRunState(currentWorkflow.name);
      setRunState(rs);
      stopRunStatePolling();
    } catch (e: any) {
      setError(e?.message ?? t("wf.errStop"));
    }
  }, [currentWorkflow, t]);

  const handleDuplicate = useCallback(async () => {
    if (!currentWorkflow) return;
    try {
      const newName = await wails.duplicateWorkflow(currentWorkflow.name, "");
      await refreshList();
      // Load the duplicated workflow.
      const wf = await wails.loadWorkflow(newName);
      if (wf) {
        setCurrentWorkflow(wf);
        setSelectedNode(null);
      }
      setError(null);
    } catch (e: any) {
      setError(e?.message ?? t("wf.errDuplicate"));
    }
  }, [currentWorkflow, refreshList, t]);

  const handleValidate = useCallback(async () => {
    if (!currentWorkflow) return;
    const wf: WorkflowView = {
      ...currentWorkflow,
      nodes: nodesToView(nodes),
      edges: edgesToView(edges),
    };
    const errMsg = await wails.validateWorkflow(wf);
    setValidationErrors(errMsg);
  }, [currentWorkflow, nodes, edges]);

  // Run state polling: while a workflow is running, poll every 1s.
  const startRunStatePolling = useCallback((name: string) => {
    stopRunStatePolling();
    const poll = async () => {
      try {
        const rs = await wails.getWorkflowRunState(name);
        setRunState(rs);
        if (rs && rs.status !== "running") {
          stopRunStatePolling();
        }
      } catch {
        // Silently ignore — the run may have just finished.
        stopRunStatePolling();
      }
    };
    void poll();
    runStateTimerRef.current = setInterval(poll, 1000);
  }, []);

  const stopRunStatePolling = useCallback(() => {
    if (runStateTimerRef.current !== null) {
      clearInterval(runStateTimerRef.current);
      runStateTimerRef.current = null;
    }
  }, []);

  // Clean up polling on unmount.
  useEffect(() => {
    return () => stopRunStatePolling();
  }, [stopRunStatePolling]);

  // Load run state when switching workflows.
  useEffect(() => {
    if (currentWorkflow) {
      wails.getWorkflowRunState(currentWorkflow.name).then((rs) => {
        setRunState(rs);
        if (rs && rs.status === "running") {
          startRunStatePolling(currentWorkflow.name);
        } else {
          stopRunStatePolling();
        }
      });
    } else {
      setRunState(null);
      stopRunStatePolling();
    }
  }, [currentWorkflow?.name]); // eslint-disable-line react-hooks/exhaustive-deps

  // handleTriggerChange updates the workflow-level trigger fields. An empty
  // trigger value is normalized to "manual" so the saved file always carries
  // an explicit trigger type (mirrors the backend default).
  const handleTriggerChange = useCallback(
    (patch: Partial<Pick<WorkflowView, "trigger" | "cronExpr" | "eventType" | "matchRules">>) => {
      setCurrentWorkflow((prev) => {
        if (!prev) return prev;
        return {
          ...prev,
          trigger: patch.trigger ?? prev.trigger ?? "manual",
          cronExpr: patch.cronExpr ?? (patch.trigger === "cron" ? prev.cronExpr : ""),
          eventType: patch.eventType ?? (patch.trigger === "event" ? prev.eventType : ""),
          matchRules: patch.matchRules ?? (patch.trigger === "event" ? prev.matchRules : undefined),
        };
      });
    },
    [],
  );

  // handleMigrateRecipes converts every existing Recipe into a single-node
  // skill Workflow. Each migrated Workflow keeps the Recipe's trigger config
  // (cron / event), so automation continues to work after the migration.
  // The original Recipes are left in place; the user deletes them manually
  // after verifying the Workflows run correctly.
  const handleMigrateRecipes = useCallback(async () => {
    try {
      const recipes = await wails.listRecipes();
      if (!recipes || recipes.length === 0) {
        setError(t("wf.errNoRecipes"));
        return;
      }
      let ok = 0;
      let fail = 0;
      for (const r of recipes) {
        try {
          await wails.migrateRecipeToWorkflow(r.name);
          ok++;
        } catch {
          fail++;
        }
      }
      await refreshList();
      // Re-register cron triggers so migrated scheduled workflows take
      // effect immediately (otherwise they'd only activate on next restart).
      void wails.reloadWorkflowTriggers();
      if (fail === 0) {
        setError(t("wf.migratedOk", { ok }));
      } else {
        setError(t("wf.migratedPartial", { ok, fail }));
      }
    } catch (e: any) {
      setError(e?.message ?? t("wf.errMigrate"));
    }
  }, [refreshList, t]);

  const handleAddNode = useCallback(
    (kind: NodeKind) => {
      if (!currentWorkflow) return;
      // We add the node by updating the current workflow's nodes.
      // The canvas will pick up the change via the useEffect sync.
      // Label starts empty so the canvas shows the localized kind name as a
      // fallback and the auto-fill logic can detect "not customised".
      const id = nextId();
      const newNode: WorkflowNodeView = {
        id,
        label: "",
        kind,
        config: "",
        // Integer positions — Go's wire struct uses int fields, and fractional
        // values would fail JSON deserialization on save.
        positionX: Math.round(100 + Math.random() * 300),
        positionY: Math.round(100 + Math.random() * 200),
      };
      // Add directly to the canvas state. We do NOT also push into
      // currentWorkflow.nodes — the canvas is the single source of truth
      // between saves, and handleSave reads from the canvas state. Keeping
      // currentWorkflow in sync would require a matching update and risk
      // re-triggering the sync effect; instead currentWorkflow stays as the
      // last-loaded/last-saved snapshot and is only rewritten on Save.
      setNodes((prev) => [
        ...prev,
        {
          id,
          type: "wfNode",
          position: { x: newNode.positionX, y: newNode.positionY },
          data: {
            label: newNode.label,
            kind: newNode.kind,
            config: newNode.config,
          },
        } as WorkflowEditorNodeType,
      ]);
    },
    [currentWorkflow, setNodes],
  );

  const handleNodeDataUpdate = useCallback(
    (nodeId: string, data: Partial<WorkflowEditorNodeData>) => {
      // Update the canvas node directly so the change is visible immediately
      // without round-tripping through currentWorkflow + useEffect (which
      // would clobber user-drawn edges, deletions, and dragged positions).
      setNodes((prev) =>
        prev.map((n) => {
          if (n.id !== nodeId) return n;
          const nd = n.data as WorkflowEditorNodeData;
          return {
            ...n,
            data: {
              ...nd,
              label: typeof data.label === "string" ? data.label : nd.label,
              kind: typeof data.kind === "string" ? data.kind : nd.kind,
              config: typeof data.config === "string" ? data.config : nd.config,
              model: data.model !== undefined ? data.model : nd.model,
              effort: data.effort !== undefined ? data.effort : nd.effort,
              requireApproval:
                typeof data.requireApproval === "boolean" ? data.requireApproval : nd.requireApproval,
            },
          };
        }),
      );
      // Keep the selected-node panel in sync with the updated data.
      setSelectedNode((prev) => {
        if (!prev) return prev;
        return { ...prev, data: { ...prev.data, ...data } };
      });
    },
    [setNodes],
  );

  const kindEntries = useMemo(() => Object.entries(KIND_META) as [NodeKind, typeof KIND_META[NodeKind]][], []);

  // Sync workflow → canvas ONLY when switching to a different workflow.
  // This is the single place that resets the canvas from currentWorkflow.
  // Property edits, node additions, edge draws, and node deletions all mutate
  // the canvas state directly (via setNodes/setEdges) and must NOT trigger a
  // reset — that's why we key on the workflow NAME, not the currentWorkflow
  // object identity (which changes on every property edit).
  useEffect(() => {
    const newName = currentWorkflow?.name ?? null;
    if (newName === loadedWorkflowName.current) return;
    loadedWorkflowName.current = newName;
    if (currentWorkflow) {
      setNodes(viewToNodes(currentWorkflow.nodes));
      setEdges(viewToEdges(currentWorkflow.edges));
      setAllowedSkillsText((currentWorkflow.allowedSkills ?? []).join(", "));
    } else {
      setNodes([]);
      setEdges([]);
      setAllowedSkillsText("");
    }
  }, [currentWorkflow, setNodes, setEdges]);

  return (
    <div className="wf-editor">
      {/* Modal header bar — title + close button. Only rendered when the
          editor is hosted in a modal (onClose provided). */}
      {onClose && (
        <header className="wf-modal__header">
          <div className="wf-modal__title">
            <WorkflowIcon size={15} />
            <span>{t("wf.title")}</span>
          </div>
          <button
            className="wf-modal__close"
            onClick={onClose}
            title={t("wf.closeTitle")}
            aria-label={t("wf.close")}
          >
            <X size={16} />
          </button>
        </header>
      )}

      <div className="wf-editor__body">
      {/* Left sidebar: workflow list */}
      <aside className={`wf-sidebar${sidebarCollapsed ? " wf-sidebar--collapsed" : ""}`}>
        <div className="wf-sidebar__header">
          {!sidebarCollapsed && (
            <span className="wf-sidebar__title">
              <WorkflowIcon size={14} />
              {t("wf.workflows")}
            </span>
          )}
          <button
            className="wf-sidebar__toggle"
            onClick={() => setSidebarCollapsed((c) => !c)}
            title={sidebarCollapsed ? t("wf.expandSidebar") : t("wf.collapseSidebar")}
          >
            <ChevronRight
              size={14}
              style={{ transform: sidebarCollapsed ? "rotate(180deg)" : "rotate(0)" }}
            />
          </button>
        </div>

        {!sidebarCollapsed && (
          <>
            <div className="wf-sidebar__actions">
              <button className="wf-btn wf-btn--primary" onClick={handleNewWorkflow} title={t("wf.newTitle")}>
                <Plus size={13} />
                {t("wf.new")}
              </button>
              <button className="wf-btn wf-btn--ghost" onClick={handleMigrateRecipes} title={t("wf.migrateTitle")}>
                <ArrowRightLeft size={13} />
                {t("wf.migrate")}
              </button>
            </div>

            <ul className="wf-sidebar__list">
              {workflows.map((wf) => (
                <li
                  key={wf.name}
                  className={`wf-sidebar__item${currentWorkflow?.name === wf.name ? " wf-sidebar__item--active" : ""}`}
                  onClick={() => handleSelectWorkflow(wf.name)}
                >
                  <FolderOpen size={13} />
                  <span className="wf-sidebar__item-name">{wf.name}</span>
                </li>
              ))}
              {workflows.length === 0 && (
                <li className="wf-sidebar__empty">{t("wf.empty")}</li>
              )}
            </ul>
          </>
        )}
      </aside>

      {/* Center area */}
      <div className="wf-main">
        {/* Toolbar */}
        <header className="wf-toolbar">
          <div className="wf-toolbar__left">
            <span className="wf-toolbar__name">
              {currentWorkflow?.name ?? t("wf.noneSelected")}
            </span>
          </div>
          <div className="wf-toolbar__center">
            {currentWorkflow && (
              <div className="wf-toolbar__add-group">
                {kindEntries.map(([kind, meta]) => {
                  const Icon = meta.icon;
                  return (
                    <button
                      key={kind}
                      className="wf-btn wf-btn--ghost"
                      onClick={() => handleAddNode(kind)}
                      title={t("wf.addNode", { label: t(meta.labelKey) })}
                    >
                      <Icon size={13} />
                      {t(meta.labelKey)}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
          <div className="wf-toolbar__right">
            {currentWorkflow && (
              <>
                <button className="wf-btn wf-btn--ghost" onClick={handleValidate} title={t("wf.validateTitle")}>
                  <Sparkles size={13} />
                  {t("wf.validate")}
                </button>
                <button className="wf-btn wf-btn--ghost" onClick={handleSave} title={t("wf.saveTitle")}>
                  <Save size={13} />
                  {t("wf.save")}
                </button>
                {runState?.status === "running" ? (
                  <button className="wf-btn wf-btn--danger" onClick={handleStop} title={t("wf.stopTitle")}>
                    <Trash2 size={13} />
                    {t("wf.stop")}
                  </button>
                ) : (
                  <button className="wf-btn wf-btn--ghost" onClick={handleRun} title={t("wf.runTitle")}>
                    <Play size={13} />
                    {t("wf.run")}
                  </button>
                )}
                <button className="wf-btn wf-btn--ghost" onClick={handleDuplicate} title={t("wf.duplicateTitle")}>
                  <Plus size={13} />
                  {t("wf.duplicate")}
                </button>
                <button className="wf-btn wf-btn--danger" onClick={handleDelete} title={t("wf.deleteTitle")}>
                  <Trash2 size={13} />
                </button>
              </>
            )}
          </div>
        </header>

        {/* Trigger bar — workflow-level activation config (mirrors Recipe) */}
        {currentWorkflow && (
          <div className="wf-trigger-bar">
            <span className="wf-trigger-bar__label">{t("wf.trigger")}</span>
            <select
              className="wf-detail__select wf-trigger-bar__select"
              value={currentWorkflow.trigger ?? "manual"}
              onChange={(e) => handleTriggerChange({ trigger: e.target.value as WorkflowTrigger })}
            >
              <option value="manual">{t("wf.triggerManual")}</option>
              <option value="cron">{t("wf.triggerCron")}</option>
              <option value="event">{t("wf.triggerEvent")}</option>
            </select>
            {(currentWorkflow.trigger ?? "manual") === "cron" && (
              <input
                className="wf-detail__input wf-trigger-bar__input"
                value={currentWorkflow.cronExpr ?? ""}
                onChange={(e) => handleTriggerChange({ cronExpr: e.target.value })}
                placeholder={t("wf.cronPlaceholder")}
              />
            )}
            {(currentWorkflow.trigger ?? "manual") === "event" && (
              <>
                <input
                  className="wf-detail__input wf-trigger-bar__input"
                  value={currentWorkflow.eventType ?? ""}
                  onChange={(e) => handleTriggerChange({ eventType: e.target.value })}
                  placeholder={t("wf.eventTypePlaceholder")}
                />
                <textarea
                  className="wf-detail__textarea wf-trigger-bar__rules"
                  value={matchRulesToText(currentWorkflow.matchRules)}
                  onChange={(e) => handleTriggerChange({ matchRules: matchRulesFromText(e.target.value) })}
                  rows={1}
                  placeholder={t("wf.matchRulesPlaceholder")}
                />
              </>
            )}
          </div>
        )}

        {/* Allowed-skills bar — P3 permission whitelist.
            Empty = unrestricted; non-empty = only listed skills may run. */}
        {currentWorkflow && (
          <div className="wf-trigger-bar">
            <span className="wf-trigger-bar__label">{t("wf.allowedSkills")}</span>
            <input
              className="wf-detail__input wf-trigger-bar__input"
              value={allowedSkillsText}
              onChange={(e) => {
                const raw = e.target.value;
                setAllowedSkillsText(raw);
                const list = raw.split(",").map((s) => s.trim()).filter((s) => s.length > 0);
                setCurrentWorkflow((prev) => (prev ? { ...prev, allowedSkills: list } : prev));
              }}
              placeholder={t("wf.allowedSkillsPlaceholder")}
            />
          </div>
        )}

        {/* Error strategy bar — how to handle node failures. */}
        {currentWorkflow && (
          <div className="wf-trigger-bar">
            <span className="wf-trigger-bar__label">{t("wf.errorStrategy")}</span>
            <select
              className="wf-detail__select wf-trigger-bar__select"
              value={currentWorkflow.errorStrategy ?? "continue"}
              onChange={(e) => setCurrentWorkflow((prev) => (prev ? { ...prev, errorStrategy: e.target.value as "continue" | "stop" } : prev))}
            >
              <option value="continue">{t("wf.errorContinue")}</option>
              <option value="stop">{t("wf.errorStop")}</option>
            </select>
          </div>
        )}

        {/* Run state bar — shows execution progress when a workflow is running or recently completed. */}
        {currentWorkflow && runState && (
          <div className={`wf-trigger-bar wf-run-bar wf-run-bar--${runState.status}`}>
            <span className="wf-trigger-bar__label">
              {runState.status === "running" && `⏳ ${t("wf.runRunning")}`}
              {runState.status === "completed" && `✅ ${t("wf.runCompleted")}`}
              {runState.status === "failed" && `❌ ${t("wf.runFailed")}`}
              {runState.status === "aborted" && `⏹ ${t("wf.runAborted")}`}
            </span>
            <span className="wf-run-bar__progress">
              {runState.nodes.filter((n) => n.status === "completed").length}/{runState.nodes.length}
            </span>
            {runState.status === "running" && (
              <button className="wf-btn wf-btn--danger wf-btn--sm" onClick={handleStop}>
                {t("wf.stop")}
              </button>
            )}
          </div>
        )}

        {/* Validation errors bar */}
        {validationErrors && (
          <div className="wf-error-bar wf-error-bar--validation">
            <span>{validationErrors}</span>
            <button onClick={() => setValidationErrors(null)} className="wf-error-bar__close">✕</button>
          </div>
        )}

        {/* Error bar */}
        {error && (
          <div className="wf-error-bar">
            <span>{error}</span>
            <button onClick={() => setError(null)} className="wf-error-bar__close">✕</button>
          </div>
        )}

        {/* Canvas */}
        <div className="wf-canvas">
          {!currentWorkflow ? (
            <div className="wf-canvas__empty">
              <WorkflowIcon size={32} />
              <p>{t("wf.canvasEmpty")}</p>
            </div>
          ) : (
            <div className="wf-canvas__flow">
              <ReactFlowProvider>
                <WorkflowCanvasWithRef
                  nodes={nodes}
                  edges={edges}
                  onNodesChange={onNodesChange}
                  onEdgesChange={onEdgesChange}
                  setEdges={setEdges}
                  onNodeSelect={setSelectedNode}
                />
              </ReactFlowProvider>
              {selectedNode && (
                <NodeDetailPanel
                  node={selectedNode}
                  onUpdate={handleNodeDataUpdate}
                  onClose={() => setSelectedNode(null)}
                  skills={skills}
                  models={models}
                />
              )}
            </div>
          )}
        </div>
      </div>
      </div>
    </div>
  );
}

// ── Canvas wrapper ───────────────────────────────────────────
// A thin presentational wrapper around ReactFlow. All graph state
// (nodes/edges) is owned by the parent WorkflowEditor and passed in as
// props, so the parent can mutate it directly from handleAddNode /
// handleNodeDataUpdate without a useEffect round-trip. This component only
// wires up ReactFlow's interaction callbacks (onConnect, onNodeClick).

function WorkflowCanvasWithRef({
  nodes,
  edges,
  onNodesChange,
  onEdgesChange,
  setEdges,
  onNodeSelect,
}: {
  nodes: WorkflowEditorNodeType[];
  edges: Edge[];
  onNodesChange: OnNodesChange<WorkflowEditorNodeType>;
  onEdgesChange: OnEdgesChange;
  setEdges: React.Dispatch<React.SetStateAction<Edge[]>>;
  onNodeSelect: (node: WorkflowEditorNodeType | null) => void;
}) {
  const onConnect: OnConnect = useCallback(
    (connection: Connection) => {
      const edge: Edge = {
        id: `e_${Date.now()}_${++idCounter}`,
        source: connection.source,
        target: connection.target,
        sourceHandle: connection.sourceHandle ?? undefined,
        targetHandle: connection.targetHandle ?? undefined,
      };
      setEdges((eds) => addEdge(edge, eds));
    },
    [setEdges],
  );

  const onNodeClick = useCallback(
    (_: unknown, node: WorkflowEditorNodeType) => {
      onNodeSelect(node);
    },
    [onNodeSelect],
  );

  const onPaneClick = useCallback(() => {
    onNodeSelect(null);
  }, [onNodeSelect]);

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onConnect={onConnect}
      onNodeClick={onNodeClick}
      onPaneClick={onPaneClick}
      nodeTypes={nodeTypes}
      defaultEdgeOptions={defaultEdgeOptions}
      fitView
      fitViewOptions={{ padding: 0.18 }}
      minZoom={0.1}
      deleteKeyCode="Delete"
      proOptions={{ hideAttribution: true }}
    >
      <Background gap={18} size={1} />
      <Controls showInteractive={false} />
      <MiniMap
        pannable
        zoomable
        nodeColor={(n) => {
          const kind = (n.data as WorkflowEditorNodeData).kind as NodeKind;
          return KIND_META[kind]?.color ?? "#6e7681";
        }}
        nodeStrokeWidth={2}
        maskColor="rgba(110,118,129,0.18)"
      />
    </ReactFlow>
  );
}

export default WorkflowEditor;
