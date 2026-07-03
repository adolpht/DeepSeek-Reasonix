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
} from "lucide-react";
// ── Types ────────────────────────────────────────────────────

interface WorkflowNodeView {
  id: string;
  label: string;
  kind: "skill" | "prompt" | "tool" | "condition" | "parallel";
  config: string;
  model?: string;
  effort?: string;
  positionX: number;
  positionY: number;
}

interface WorkflowEdgeView {
  id: string;
  source: string;
  target: string;
  label?: string;
}

interface WorkflowView {
  name: string;
  description: string;
  nodes: WorkflowNodeView[];
  edges: WorkflowEdgeView[];
  createdAt: number;
  updatedAt: number;
}

// Node data type following the @xyflow/react v12 Node<T, U> pattern.
type WorkflowEditorNodeData = {
  label: string;
  kind: string;
  config: string;
  model?: string;
  effort?: string;
  [key: string]: unknown;
};
type WorkflowEditorNodeType = Node<WorkflowEditorNodeData, "wfNode">;

// ── Kind metadata ────────────────────────────────────────────

type NodeKind = "skill" | "prompt" | "tool" | "condition" | "parallel";

const KIND_META: Record<NodeKind, { icon: typeof Sparkles; color: string; label: string }> = {
  skill: { icon: Sparkles, color: "#bc8cff", label: "Skill" },
  prompt: { icon: MessageSquare, color: "#58a6ff", label: "Prompt" },
  tool: { icon: Wrench, color: "#3fb950", label: "Tool" },
  condition: { icon: GitBranch, color: "#d9a441", label: "Condition" },
  parallel: { icon: Layers, color: "#f78166", label: "Parallel" },
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
    positionX: n.position.x,
    positionY: n.position.y,
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
        <span className="wf-node__label">{data.label || meta.label}</span>
      </div>
      <div className="wf-node__kind">{meta.label}</div>
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

// ── Node detail panel ────────────────────────────────────────

function NodeDetailPanel({
  node,
  onUpdate,
  onClose,
}: {
  node: WorkflowEditorNodeType;
  onUpdate: (id: string, data: Partial<WorkflowEditorNodeData>) => void;
  onClose: () => void;
}) {
  const kind = (node.data.kind as NodeKind) ?? "prompt";
  const meta = KIND_META[kind] ?? KIND_META.prompt;
  const Icon = meta.icon;

  const handleChange = (field: string, value: string) => {
    onUpdate(node.id, { [field]: value });
  };

  return (
    <aside className="wf-detail">
      <header className="wf-detail__head">
        <div className="wf-detail__title">
          <Icon size={14} style={{ color: meta.color }} />
          <span>{node.data.label || meta.label}</span>
        </div>
        <button className="wf-detail__close" onClick={onClose}>✕</button>
      </header>
      <div className="wf-detail__body">
        <label className="wf-detail__field">
          <span className="wf-detail__k">Label</span>
          <input
            className="wf-detail__input"
            value={node.data.label as string}
            onChange={(e) => handleChange("label", e.target.value)}
          />
        </label>

        <label className="wf-detail__field">
          <span className="wf-detail__k">Kind</span>
          <select
            className="wf-detail__select"
            value={node.data.kind as string}
            onChange={(e) => handleChange("kind", e.target.value)}
          >
            {Object.entries(KIND_META).map(([k, m]) => (
              <option key={k} value={k}>{m.label}</option>
            ))}
          </select>
        </label>

        <label className="wf-detail__field">
          <span className="wf-detail__k">Config</span>
          <textarea
            className="wf-detail__textarea"
            value={node.data.config as string}
            onChange={(e) => handleChange("config", e.target.value)}
            rows={4}
            placeholder="JSON or text config…"
          />
        </label>

        <label className="wf-detail__field">
          <span className="wf-detail__k">Model</span>
          <input
            className="wf-detail__input"
            value={(node.data.model as string) ?? ""}
            onChange={(e) => handleChange("model", e.target.value)}
            placeholder="e.g. deepseek-reasoner"
          />
        </label>

        <label className="wf-detail__field">
          <span className="wf-detail__k">Effort</span>
          <select
            className="wf-detail__select"
            value={(node.data.effort as string) ?? ""}
            onChange={(e) => handleChange("effort", e.target.value)}
          >
            <option value="">Default</option>
            <option value="low">Low</option>
            <option value="medium">Medium</option>
            <option value="high">High</option>
          </select>
        </label>
      </div>
    </aside>
  );
}

// ── Main component ───────────────────────────────────────────

export function WorkflowEditor() {
  const [workflows, setWorkflows] = useState<WorkflowView[]>([]);
  const [currentWorkflow, setCurrentWorkflow] = useState<WorkflowView | null>(null);
  const [selectedNode, setSelectedNode] = useState<WorkflowEditorNodeType | null>(null);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Ref to hold the latest graph state from the canvas.
  const graphStateRef = useRef<{ nodes: WorkflowEditorNodeType[]; edges: Edge[] }>({
    nodes: [],
    edges: [],
  });

  // Load workflow list on mount.
  useEffect(() => {
    (async () => {
      try {
        const list = await wails.listWorkflows();
        setWorkflows(list ?? []);
      } catch (e: any) {
        setError(e?.message ?? "Failed to load workflows");
      }
    })();
  }, []);

  const refreshList = useCallback(async () => {
    try {
      const list = await wails.listWorkflows();
      setWorkflows(list ?? []);
    } catch (e: any) {
      setError(e?.message ?? "Failed to refresh workflow list");
    }
  }, []);

  const handleNewWorkflow = useCallback(() => {
    const name = `Workflow ${Date.now()}`;
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
  }, []);

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
        setError(e?.message ?? "Failed to load workflow");
      }
    },
    [],
  );

  const handleSave = useCallback(async () => {
    if (!currentWorkflow) return;

    const { nodes: latestNodes, edges: latestEdges } = graphStateRef.current;

    const wf: WorkflowView = {
      ...currentWorkflow,
      nodes: nodesToView(latestNodes),
      edges: edgesToView(latestEdges),
      updatedAt: Date.now(),
    };

    try {
      await wails.saveWorkflow(wf);
      setCurrentWorkflow(wf);
      await refreshList();
      setError(null);
    } catch (e: any) {
      setError(e?.message ?? "Failed to save workflow");
    }
  }, [currentWorkflow, refreshList]);

  const handleDelete = useCallback(async () => {
    if (!currentWorkflow) return;
    try {
      await wails.deleteWorkflow(currentWorkflow.name);
      setCurrentWorkflow(null);
      setSelectedNode(null);
      await refreshList();
      setError(null);
    } catch (e: any) {
      setError(e?.message ?? "Failed to delete workflow");
    }
  }, [currentWorkflow, refreshList]);

  const handleRun = useCallback(async () => {
    if (!currentWorkflow) return;
    try {
      await wails.runWorkflow(currentWorkflow.name);
    } catch (e: any) {
      setError(e?.message ?? "Failed to run workflow");
    }
  }, [currentWorkflow]);

  const handleAddNode = useCallback(
    (kind: NodeKind) => {
      if (!currentWorkflow) return;
      // We add the node by updating the current workflow's nodes.
      // The canvas will pick up the change via the useEffect sync.
      const id = nextId();
      const meta = KIND_META[kind];
      const newNode: WorkflowNodeView = {
        id,
        label: meta.label,
        kind,
        config: "",
        positionX: 100 + Math.random() * 300,
        positionY: 100 + Math.random() * 200,
      };
      setCurrentWorkflow((prev) => {
        if (!prev) return prev;
        return { ...prev, nodes: [...(prev.nodes ?? []), newNode] };
      });
    },
    [currentWorkflow],
  );

  const handleNodeDataUpdate = useCallback(
    (nodeId: string, data: Partial<WorkflowEditorNodeData>) => {
      // Update the node data in the current workflow so the canvas re-renders.
      setCurrentWorkflow((prev) => {
        if (!prev) return prev;
        const newNodes = (prev.nodes ?? []).map((n) => {
          if (n.id !== nodeId) return n;
          return {
            ...n,
            label: typeof data.label === "string" ? data.label : n.label,
            kind: typeof data.kind === "string" ? (data.kind as WorkflowNodeView["kind"]) : n.kind,
            config: typeof data.config === "string" ? data.config : n.config,
            model: data.model !== undefined ? data.model : n.model,
            effort: data.effort !== undefined ? data.effort : n.effort,
          };
        });
        return { ...prev, nodes: newNodes };
      });
      // Also update the selected node reference so the panel stays in sync.
      setSelectedNode((prev) => {
        if (!prev) return prev;
        return { ...prev, data: { ...prev.data, ...data } };
      });
    },
    [],
  );

  const kindEntries = useMemo(() => Object.entries(KIND_META) as [NodeKind, typeof KIND_META[NodeKind]][], []);

  return (
    <div className="wf-editor">
      {/* Left sidebar: workflow list */}
      <aside className={`wf-sidebar${sidebarCollapsed ? " wf-sidebar--collapsed" : ""}`}>
        <div className="wf-sidebar__header">
          {!sidebarCollapsed && (
            <span className="wf-sidebar__title">
              <WorkflowIcon size={14} />
              Workflows
            </span>
          )}
          <button
            className="wf-sidebar__toggle"
            onClick={() => setSidebarCollapsed((c) => !c)}
            title={sidebarCollapsed ? "Expand sidebar" : "Collapse sidebar"}
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
              <button className="wf-btn wf-btn--primary" onClick={handleNewWorkflow} title="New Workflow">
                <Plus size={13} />
                New
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
                <li className="wf-sidebar__empty">No workflows yet</li>
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
              {currentWorkflow?.name ?? "No workflow selected"}
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
                      title={`Add ${meta.label} node`}
                    >
                      <Icon size={13} />
                      {meta.label}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
          <div className="wf-toolbar__right">
            {currentWorkflow && (
              <>
                <button className="wf-btn wf-btn--ghost" onClick={handleSave} title="Save workflow">
                  <Save size={13} />
                  Save
                </button>
                <button className="wf-btn wf-btn--ghost" onClick={handleRun} title="Run workflow">
                  <Play size={13} />
                  Run
                </button>
                <button className="wf-btn wf-btn--danger" onClick={handleDelete} title="Delete workflow">
                  <Trash2 size={13} />
                </button>
              </>
            )}
          </div>
        </header>

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
              <p>Create or select a workflow to get started</p>
            </div>
          ) : (
            <div className="wf-canvas__flow">
              <ReactFlowProvider>
                <WorkflowCanvasWithRef
                  currentWorkflow={currentWorkflow}
                  onNodeSelect={setSelectedNode}
                  graphStateRef={graphStateRef}
                />
              </ReactFlowProvider>
              {selectedNode && (
                <NodeDetailPanel
                  node={selectedNode}
                  onUpdate={handleNodeDataUpdate}
                  onClose={() => setSelectedNode(null)}
                />
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ── Canvas wrapper that exposes graph state via ref ───────────

function WorkflowCanvasWithRef({
  currentWorkflow,
  onNodeSelect,
  graphStateRef,
}: {
  currentWorkflow: WorkflowView | null;
  onNodeSelect: (node: WorkflowEditorNodeType | null) => void;
  graphStateRef: React.MutableRefObject<{ nodes: WorkflowEditorNodeType[]; edges: Edge[] }>;
}) {
  const [nodes, setNodes, onNodesChange] = useNodesState<WorkflowEditorNodeType>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  // Sync workflow → react-flow.
  useEffect(() => {
    if (currentWorkflow) {
      setNodes(viewToNodes(currentWorkflow.nodes));
      setEdges(viewToEdges(currentWorkflow.edges));
    } else {
      setNodes([]);
      setEdges([]);
    }
  }, [currentWorkflow, setNodes, setEdges]);

  // Keep ref in sync so save can read it.
  useEffect(() => {
    graphStateRef.current = { nodes, edges };
  }, [nodes, edges, graphStateRef]);

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
