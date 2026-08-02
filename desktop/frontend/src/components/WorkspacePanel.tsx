import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
  CSSProperties,
  DragEvent as ReactDragEvent,
  KeyboardEvent,
  MouseEvent as ReactMouseEvent,
  PointerEvent as ReactPointerEvent,
} from "react";
import {
  ChevronDown,
  ChevronRight,
  Clipboard,
  Copy,
  FileText,
  Folder,
  FolderOpen,
  FolderPlus,
  FolderTree,
  FolderX,
  GitBranch,
  Maximize2,
  MessageSquarePlus,
  Minimize2,
  Pencil,
  RefreshCw,
  Scissors,
  Search,
  Trash2,
  X,
  FilePlus,
  FileX,
} from "lucide-react";
import { app, onWorkspaceFilesChanged } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { loadLayoutSize, saveLayoutSize } from "../lib/layoutPreferences";
import type { DirEntry, FilePreview, WorkspaceChangesView } from "../lib/types";
import { formatWorkspaceReference, WORKSPACE_REF_DRAG_TYPE } from "../lib/workspaceDrag";
import { CodeViewer } from "./CodeViewer";
import { ContextMenu, contextMenuPointFromEvent, type ContextMenuItem, type ContextMenuPoint } from "./ContextMenu";
import { FloatingMenu, FloatingMenuItems } from "./FloatingMenu";
import { Markdown } from "./Markdown";
import { SourceControlPanel } from "./SourceControlPanel";
import { Tooltip } from "./Tooltip";
import { AnchoredPopover } from "./AnchoredPopover";

const WORKSPACE_TREE_MIN_WIDTH = 208;
const WORKSPACE_TREE_DEFAULT_WIDTH = 240;
const WORKSPACE_TREE_MAX_WIDTH = 340;
const WORKSPACE_PREVIEW_MIN_WIDTH = 360;
const WORKSPACE_PREVIEW_TARGET_WIDTH = 480;
const WORKSPACE_DUAL_PANEL_MIN_WIDTH = WORKSPACE_TREE_MIN_WIDTH + WORKSPACE_PREVIEW_MIN_WIDTH;
const WORKSPACE_DUAL_PANEL_TARGET_WIDTH = WORKSPACE_TREE_DEFAULT_WIDTH + WORKSPACE_PREVIEW_TARGET_WIDTH;
const WORKSPACE_SELECTION_MENU_HEIGHT = 48;
const WORKSPACE_MAX_PREVIEW_TABS = 5;

function revealLabelKey(platform: string): "workspace.revealInFinder" | "workspace.revealInExplorer" | "workspace.revealInFileManager" {
  if (platform === "darwin") return "workspace.revealInFinder";
  if (platform === "windows") return "workspace.revealInExplorer";
  return "workspace.revealInFileManager";
}

function clampWorkspaceTreeWidth(width: number, panelWidth?: number): number {
  const maxForPanel =
    typeof panelWidth === "number" && Number.isFinite(panelWidth)
      ? Math.max(WORKSPACE_TREE_MIN_WIDTH, panelWidth - WORKSPACE_PREVIEW_MIN_WIDTH)
      : WORKSPACE_TREE_MAX_WIDTH;
  const max = Math.min(WORKSPACE_TREE_MAX_WIDTH, maxForPanel);
  return Math.min(max, Math.max(WORKSPACE_TREE_MIN_WIDTH, Math.round(width)));
}

function loadWorkspaceTreeWidth(): number {
  return loadLayoutSize("workspaceTreeWidth", WORKSPACE_TREE_DEFAULT_WIDTH, clampWorkspaceTreeWidth);
}

function saveWorkspaceTreeWidth(width: number): void {
  saveLayoutSize("workspaceTreeWidth", width);
}

function entryPath(dir: string, entry: DirEntry): string {
  const prefix = dir === "" || dir.endsWith("/") ? dir : dir + "/";
  return prefix + entry.name + (entry.isDir ? "/" : "");
}

function basename(path: string): string {
  const parts = path.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

function parentPath(path: string): string {
  const clean = path.replace(/\/$/, "");
  const parts = clean.split("/").filter(Boolean);
  return parts.slice(0, -1).join("/");
}

function parentDirs(path: string): string[] {
  const parts = path.split("/").filter(Boolean);
  const dirs: string[] = [""];
  let acc = "";
  for (let i = 0; i < parts.length - 1; i++) {
    acc += parts[i] + "/";
    dirs.push(acc);
  }
  return dirs;
}

function languageFor(path: string): string | undefined {
  const name = basename(path).toLowerCase();
  const ext = name.includes(".") ? name.slice(name.lastIndexOf(".") + 1) : name;
  const byExt: Record<string, string> = {
    css: "css",
    go: "go",
    html: "html",
    js: "javascript",
    json: "json",
    jsx: "jsx",
    md: "markdown",
    py: "python",
    rs: "rust",
    sh: "bash",
    toml: "toml",
    ts: "typescript",
    tsx: "tsx",
    yaml: "yaml",
    yml: "yaml",
  };
  return byExt[ext];
}

function renderMediaPreview(preview: FilePreview): JSX.Element | null {
  if (!preview.url) return null;
  if (preview.kind === "image") {
    return (
      <div className="workspace-media workspace-media--image">
        <img src={preview.url} alt={basename(preview.path)} decoding="async" />
      </div>
    );
  }
  if (preview.kind === "pdf" || preview.kind === "docx" || preview.kind === "xlsx" || preview.kind === "csv" || preview.kind === "html") {
    return (
      <iframe
        className="workspace-media workspace-media--pdf"
        src={preview.url}
        title={basename(preview.path)}
      />
    );
  }
  return null;
}

function fenceFor(text: string): string {
  let longest = 0;
  for (const match of text.matchAll(/`+/g)) {
    longest = Math.max(longest, match[0].length);
  }
  return "`".repeat(Math.max(3, longest + 1));
}

function formatSelectionReference(path: string, text: string): string {
  const body = text.replace(/\r\n|\r/g, "\n").trimEnd();
  const fence = fenceFor(body);
  const lang = languageFor(path);
  return `From \`${path}\`:\n\n${fence}${lang ?? ""}\n${body}\n${fence}`;
}

function shortCwd(cwd?: string): string {
  if (!cwd) return "";
  const parts = cwd.split("/").filter(Boolean);
  if (parts.length <= 2) return cwd;
  return "…/" + parts.slice(-2).join("/");
}

function formatBytes(n: number): string {
  if (n >= 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  if (n >= 1024) return `${Math.ceil(n / 1024)} KB`;
  return `${n} B`;
}

export function WorkspacePanel({
  open,
  cwd,
  tabId,
  maximized,
  panelWidth,
  onClose,
  onToggleMaximized,
  onPreviewModeChange,
  onAddToChat,
  onRequestPanelWidth,
  refreshKey,
  initialViewMode = "files",
  showViewTabs = true,
}: {
  open: boolean;
  cwd?: string;
  tabId?: string;
  maximized: boolean;
  panelWidth?: number;
  onClose: () => void;
  onToggleMaximized: () => void;
  onPreviewModeChange?: (active: boolean) => void;
  onAddToChat?: (text: string) => void;
  onRequestPanelWidth?: (width: number) => void;
  refreshKey?: number;
  initialViewMode?: "files" | "changed";
  showViewTabs?: boolean;
}) {
  const t = useT();
  const panelRef = useRef<HTMLElement>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const previewBodyRef = useRef<HTMLDivElement>(null);
  const [entriesByDir, setEntriesByDir] = useState<Record<string, DirEntry[]>>({});
  const [openDirs, setOpenDirs] = useState<Set<string>>(() => new Set([""]));
  const [selectedPath, setSelectedPath] = useState<string | null>(null);
  const [openTabs, setOpenTabs] = useState<string[]>([]);
  const [preview, setPreview] = useState<FilePreview | null>(null);
  const [uncommittedCount, setUncommittedCount] = useState(0);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [viewMode, setViewMode] = useState<"files" | "changed">(initialViewMode);
  const [changes, setChanges] = useState<WorkspaceChangesView | null>(null);
  const [, setLoadingChanges] = useState(false);
  const [selectionMenu, setSelectionMenu] = useState<{ x: number; y: number; text: string; path: string } | null>(null);
  const [treeMenu, setTreeMenu] = useState<{ point: ContextMenuPoint; path: string; isDir: boolean } | null>(null);
  const [treeBlankMenuPoint, setTreeBlankMenuPoint] = useState<ContextMenuPoint | null>(null);
  const [platform, setPlatform] = useState("");
  const changesRequestRef = useRef(0);
  const [filter, setFilter] = useState("");
  const [treeVisible, setTreeVisible] = useState(true);
  const [treeWidth, setTreeWidth] = useState(loadWorkspaceTreeWidth);
  const [treeResizing, setTreeResizing] = useState(false);
  const [recentOpen, setRecentOpen] = useState(false);
  const recentAnchorRef = useRef<HTMLButtonElement>(null);
  const openDirsRef = useRef(openDirs);
  // File operation state
  const [clipboardOp, setClipboardOp] = useState<{ path: string; op: "copy" | "cut" } | null>(null);
  const [renamingPath, setRenamingPath] = useState<string | null>(null);
  const [renameDraft, setRenameDraft] = useState("");
  const [confirmDeletePath, setConfirmDeletePath] = useState<string | null>(null);
  const [creatingIn, setCreatingIn] = useState<{ dir: string; kind: "file" | "dir" } | null>(null);
  const [createDraft, setCreateDraft] = useState("");

  useEffect(() => {
    openDirsRef.current = openDirs;
  }, [openDirs]);

  useEffect(() => {
    let cancelled = false;
    void app.Platform().then((value) => {
      if (!cancelled) setPlatform(value);
    }).catch(() => {});
    return () => { cancelled = true; };
  }, []);

  const loadDir = useCallback(async (dir: string) => {
    const entries = await app.ListDir(dir).catch(() => []);
    setEntriesByDir((prev) => ({ ...prev, [dir]: entries ?? [] }));
  }, []);

  const loadChanges = useCallback(async () => {
    const requestId = changesRequestRef.current + 1;
    changesRequestRef.current = requestId;
    setLoadingChanges(true);
    try {
      const next = await app.WorkspaceChanges();
      if (changesRequestRef.current === requestId) setChanges(next);
    } catch (err) {
      if (changesRequestRef.current === requestId) {
        setChanges({ files: [], gitAvailable: false, gitErr: String((err as Error)?.message ?? err) });
      }
    } finally {
      if (changesRequestRef.current === requestId) setLoadingChanges(false);
    }
  }, []);

  const selectFile = useCallback(
    (path: string) => {
      onRequestPanelWidth?.(WORKSPACE_DUAL_PANEL_TARGET_WIDTH);
      setSelectedPath(path);
      setFilter("");
      setOpenTabs((tabs) => [...tabs.filter((tab) => tab !== path), path].slice(-WORKSPACE_MAX_PREVIEW_TABS));
      const dirs = parentDirs(path);
      setOpenDirs((prev) => new Set([...Array.from(prev), ...dirs]));
      dirs.forEach((dir) => {
        if (!entriesByDir[dir]) void loadDir(dir);
      });
    },
    [entriesByDir, loadDir, onRequestPanelWidth],
  );

  useEffect(() => {
    if (!open) return;
    setEntriesByDir({});
    setOpenDirs(new Set([""]));
    setSelectedPath(null);
    setOpenTabs([]);
    setPreview(null);
    setChanges(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    setFilter("");
    setTreeVisible(true);
    setViewMode(initialViewMode);
    setRenamingPath(null);
    setConfirmDeletePath(null);
    setCreatingIn(null);
    setClipboardOp(null);
    void loadDir("");
  }, [cwd, initialViewMode, loadDir, open]);

  useEffect(() => {
    if (!open) return;
    setViewMode(initialViewMode);
    if (initialViewMode === "changed") void loadChanges();
  }, [initialViewMode, loadChanges, open]);

  useEffect(() => {
    if (!open) return;
    void loadChanges();
  }, [cwd, loadChanges, open]);

  useEffect(() => {
    if (!open || !refreshKey) return;
    void loadChanges();
    openDirsRef.current.forEach((dir) => void loadDir(dir));
  }, [loadChanges, loadDir, open, refreshKey]);

  useEffect(() => {
    if (!selectionMenu && !treeMenu) return;
    const close = () => {
      setSelectionMenu(null);
      setTreeMenu(null);
    };
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") close();
    };
    window.addEventListener("click", close);
    window.addEventListener("resize", close);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("resize", close);
      window.removeEventListener("keydown", onKey);
    };
  }, [selectionMenu, treeMenu]);

  const refreshWorkspaceList = useCallback(() => {
    setTreeBlankMenuPoint(null);
    setSelectionMenu(null);
    setTreeMenu(null);
    setRenamingPath(null);
    setConfirmDeletePath(null);
    setCreatingIn(null);
    if (viewMode === "changed") {
      void loadChanges();
      return;
    }
    const dirs = Array.from(openDirsRef.current);
    setEntriesByDir({});
    dirs.forEach((dir) => void loadDir(dir));
  }, [loadChanges, loadDir, viewMode]);

  // Auto-refresh: subscribe to workspace:files-changed events from the Go backend.
  // Emitted after agent tool calls that modify the filesystem.
  // Debounced: rapid events (e.g. multi-file edits) are coalesced into one refresh.
  useEffect(() => {
    if (!open) return;
    let timer: ReturnType<typeof setTimeout> | null = null;
    const off = onWorkspaceFilesChanged(() => {
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => {
        timer = null;
        void refreshWorkspaceList();
      }, 200);
    });
    return () => {
      off();
      if (timer) clearTimeout(timer);
    };
  }, [open, refreshWorkspaceList]);

  // --- File operation handlers ---

  const closeTreeMenu = useCallback(() => {
    setTreeMenu(null);
    setConfirmDeletePath(null);
  }, []);

  const startRename = useCallback((path: string) => {
    setTreeMenu(null);
    setConfirmDeletePath(null);
    const name = basename(path).replace(/\/$/, "");
    setRenamingPath(path);
    setRenameDraft(name);
  }, []);

  const commitRename = useCallback(async (oldPath: string) => {
    const newName = renameDraft.trim();
    setRenamingPath(null);
    if (!newName) return;
    const dir = parentPath(oldPath);
    const newRel = (dir ? dir + "/" : "") + newName + (oldPath.endsWith("/") ? "/" : "");
    if (newRel === oldPath) return;
    try {
      await app.RenameWorkspacePath(oldPath, newRel);
      await refreshWorkspaceList();
      // If the renamed file was selected, update the selection
      if (selectedPath === oldPath) {
        setSelectedPath(newRel);
        setOpenTabs((tabs) => tabs.map((tab) => tab === oldPath ? newRel : tab));
      }
    } catch {
      // Refresh to show the old name still present
      await refreshWorkspaceList();
    }
  }, [renameDraft, refreshWorkspaceList, selectedPath]);

  const startCreateIn = useCallback((dir: string, kind: "file" | "dir") => {
    setTreeMenu(null);
    setConfirmDeletePath(null);
    setCreatingIn({ dir, kind });
    setCreateDraft(kind === "file" ? t("workspace.newFileName") : t("workspace.newFolderName"));
    // Ensure the target dir is expanded
    setOpenDirs((prev) => new Set([...Array.from(prev), dir]));
  }, [t]);

  const commitCreate = useCallback(async () => {
    if (!creatingIn) return;
    const name = createDraft.trim();
    const target = creatingIn;
    setCreatingIn(null);
    if (!name) return;
    const rel = (target.dir ? target.dir + "/" : "") + name;
    try {
      if (target.kind === "file") {
        await app.CreateWorkspaceFile(rel);
      } else {
        await app.CreateWorkspaceDir(rel);
      }
      await refreshWorkspaceList();
      if (target.kind === "file") {
        selectFile(rel);
      }
    } catch {
      // Refresh to show current state even if create failed
      await refreshWorkspaceList();
    }
  }, [creatingIn, createDraft, refreshWorkspaceList, selectFile]);

  const handleTrash = useCallback(async (path: string) => {
    setTreeMenu(null);
    setConfirmDeletePath(null);
    try {
      await app.TrashWorkspacePath(path);
      await refreshWorkspaceList();
      // If the deleted file was selected, clear the selection
      if (selectedPath === path || (path.endsWith("/") && selectedPath?.startsWith(path))) {
        setSelectedPath(null);
        setOpenTabs((tabs) => tabs.filter((tab) => tab !== path && !tab.startsWith(path)));
        setPreview(null);
      }
    } catch {
      // ignore
    }
  }, [refreshWorkspaceList, selectedPath]);

  const handleCopy = useCallback((path: string) => {
    setClipboardOp({ path, op: "copy" });
    setTreeMenu(null);
    setConfirmDeletePath(null);
  }, []);

  const handleCut = useCallback((path: string) => {
    setClipboardOp({ path, op: "cut" });
    setTreeMenu(null);
    setConfirmDeletePath(null);
  }, []);

  const handlePaste = useCallback(async (targetDir: string) => {
    if (!clipboardOp) return;
    const srcName = basename(clipboardOp.path).replace(/\/$/, "");
    const dstRel = (targetDir ? targetDir + "/" : "") + srcName;
    // Skip if source and destination are the same
    if (dstRel === clipboardOp.path) return;
    const op = clipboardOp;
    setTreeMenu(null);
    setTreeBlankMenuPoint(null);
    setConfirmDeletePath(null);
    try {
      if (op.op === "copy") {
        await app.CopyWorkspaceFile(op.path, dstRel);
      } else {
        await app.RenameWorkspacePath(op.path, dstRel);
      }
      if (op.op === "cut") {
        setClipboardOp(null);
      }
      await refreshWorkspaceList();
    } catch {
      // Refresh to show current state even if paste failed
      await refreshWorkspaceList();
    }
  }, [clipboardOp, refreshWorkspaceList]);

  const refreshSelected = useCallback(() => {
    if (!selectedPath) return;
    let live = true;
    setLoadingPreview(true);
    app
      .ReadFile(selectedPath)
      .then((next) => {
        if (live) setPreview(next);
      })
      .catch((err) => {
        if (live) {
          setPreview({
            path: selectedPath,
            body: "",
            size: 0,
            truncated: false,
            binary: false,
            err: String(err?.message ?? err),
          });
        }
      })
      .finally(() => {
        if (live) setLoadingPreview(false);
      });
    return () => {
      live = false;
    };
  }, [selectedPath]);

  useEffect(() => {
    if (!open || !selectedPath) return;
    return refreshSelected();
  }, [open, refreshSelected, selectedPath]);

  const toggleDir = useCallback(
    (dir: string) => {
      setOpenDirs((prev) => {
        const next = new Set(prev);
        if (next.has(dir)) {
          next.delete(dir);
        } else {
          next.add(dir);
          if (!entriesByDir[dir]) void loadDir(dir);
        }
        return next;
      });
    },
    [entriesByDir, loadDir],
  );

  const closeTab = (path: string) => {
    setOpenTabs((tabs) => {
      const next = tabs.filter((tab) => tab !== path);
      if (selectedPath === path) {
        const replacement = next[next.length - 1] ?? null;
        setSelectedPath(replacement);
        if (!replacement) {
          setPreview(null);
          setTreeVisible(true);
        }
        setSelectionMenu(null);
        setTreeMenu(null);
        setRecentOpen(false);
      }
      return next;
    });
  };

  const breadcrumbDirs = selectedPath ? parentDirs(selectedPath) : [""];
  const pathParts = selectedPath?.split("/").filter(Boolean) ?? [];
  const currentFileName = selectedPath ? basename(selectedPath) : t("workspace.noFile");
  const currentFileDir = selectedPath ? parentPath(selectedPath) : "";
  const recentFiles = useMemo(() => [...openTabs].reverse(), [openTabs]);
  const flattened = useMemo(() => {
    const rows: { path: string; entry: DirEntry }[] = [];
    for (const [dir, entries] of Object.entries(entriesByDir)) {
      for (const entry of entries) {
        rows.push({ path: entryPath(dir, entry), entry });
      }
    }
    const q = filter.trim().toLowerCase();
    if (!q) return null;
    return rows
      .filter((row) => row.path.toLowerCase().includes(q))
      .sort((a, b) => a.path.localeCompare(b.path));
  }, [entriesByDir, filter]);

  const searchPlaceholder = viewMode === "changed" ? t("workspace.filterChanges") : t("workspace.filter");

  const effectiveTreeWidth = useMemo(() => clampWorkspaceTreeWidth(treeWidth, panelWidth), [panelWidth, treeWidth]);
  const previewVisible = openTabs.length > 0 || selectedPath !== null;
  const selectedFileVisible = selectedPath !== null;
  const compactTreeSplit =
    treeVisible && selectedFileVisible && panelWidth !== undefined && panelWidth < WORKSPACE_DUAL_PANEL_MIN_WIDTH;
  const actualTreeVisible = treeVisible;
  const previewModeActive = open && previewVisible;
  const embeddedDockMode = !showViewTabs;
  const showFileTools = showViewTabs || previewVisible;

  const panelStyle = useMemo(
    () =>
      ({
        "--workspace-tree-width": `${effectiveTreeWidth}px`,
        "--workspace-preview-min-width": compactTreeSplit ? "0px" : `${WORKSPACE_PREVIEW_MIN_WIDTH}px`,
      }) as CSSProperties,
    [compactTreeSplit, effectiveTreeWidth],
  );

  useEffect(() => {
    onPreviewModeChange?.(previewModeActive);
  }, [onPreviewModeChange, previewModeActive]);

  useEffect(() => {
    if (open && !treeVisible && !previewVisible) onClose();
  }, [onClose, open, previewVisible, treeVisible]);

  const hideTreeOrClosePanel = useCallback(() => {
    if (previewVisible) {
      setTreeVisible(false);
    } else {
      onClose();
    }
  }, [onClose, previewVisible]);

  const setSavedTreeWidth = useCallback(
    (width: number) => {
      const next = clampWorkspaceTreeWidth(width, panelWidth);
      setTreeWidth(next);
      saveWorkspaceTreeWidth(next);
    },
    [panelWidth],
  );

  const startTreeResize = useCallback(
    (event: ReactPointerEvent<HTMLButtonElement>) => {
      if (!treeVisible) return;
      const rect = panelRef.current?.getBoundingClientRect();
      if (!rect) return;
      event.preventDefault();
      setTreeResizing(true);
      let nextWidth = effectiveTreeWidth;
      const onMove = (moveEvent: PointerEvent) => {
        nextWidth = clampWorkspaceTreeWidth(moveEvent.clientX - rect.left, rect.width);
        setTreeWidth(nextWidth);
      };
      const onDone = () => {
        setTreeWidth(nextWidth);
        saveWorkspaceTreeWidth(nextWidth);
        setTreeResizing(false);
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
    [effectiveTreeWidth, treeVisible],
  );

  const resizeTreeWithKeyboard = useCallback(
    (event: KeyboardEvent<HTMLButtonElement>) => {
      if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
        event.preventDefault();
        setSavedTreeWidth(effectiveTreeWidth + (event.key === "ArrowRight" ? 16 : -16));
      } else if (event.key === "Home") {
        event.preventDefault();
        setSavedTreeWidth(WORKSPACE_TREE_MIN_WIDTH);
      } else if (event.key === "End") {
        event.preventDefault();
        setSavedTreeWidth(WORKSPACE_TREE_MAX_WIDTH);
      }
    },
    [effectiveTreeWidth, setSavedTreeWidth],
  );

  if (!open) return null;

  const selectedTextFromPreview = (): string => {
    const root = previewBodyRef.current;
    const selection = typeof window === "undefined" ? null : window.getSelection();
    if (!root || !selection || selection.rangeCount === 0) return "";
    const range = selection.getRangeAt(0);
    const container = range.commonAncestorContainer;
    const node = container instanceof Element ? container : container.parentElement;
    if (!node || !root.contains(node)) return "";
    return selection.toString();
  };

  const openSelectionMenu = (event: ReactMouseEvent<HTMLDivElement>) => {
    if (!selectedPath || loadingPreview || preview?.err || preview?.binary || preview?.kind) return;
    const text = selectedTextFromPreview();
    if (text.trim() === "") return;
    event.preventDefault();
    event.stopPropagation();
    setSelectionMenu({ x: event.clientX, y: event.clientY, text, path: selectedPath });
  };

  const addSelectionToChat = () => {
    if (!selectionMenu) return;
    onAddToChat?.(formatSelectionReference(selectionMenu.path, selectionMenu.text));
    setSelectionMenu(null);
  };

  const openTreeMenu = (event: ReactMouseEvent<HTMLElement>, path: string, isDir: boolean) => {
    event.preventDefault();
    event.stopPropagation();
    setTreeBlankMenuPoint(null);
    setSelectionMenu(null);
    setConfirmDeletePath(null);
    setTreeMenu({ point: contextMenuPointFromEvent(event), path, isDir });
  };

  const openTreeBlankMenu = (event: ReactMouseEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement | null;
    if (target?.closest(".workspace-tree__row,.workspace-change,button,input,textarea,select")) return;
    event.preventDefault();
    event.stopPropagation();
    setSelectionMenu(null);
    setTreeMenu(null);
    setConfirmDeletePath(null);
    setTreeBlankMenuPoint(contextMenuPointFromEvent(event));
  };

  const startTreeDrag = (event: ReactDragEvent<HTMLElement>, path: string, isDir: boolean) => {
    const ref = formatWorkspaceReference(path, isDir);
    event.dataTransfer.effectAllowed = "copy";
    event.dataTransfer.setData(WORKSPACE_REF_DRAG_TYPE, JSON.stringify({ path, isDir }));
    event.dataTransfer.setData("text/plain", ref);
  };

  const addTreeReferenceToChat = () => {
    if (!treeMenu) return;
    onAddToChat?.(formatWorkspaceReference(treeMenu.path, treeMenu.isDir));
    setTreeMenu(null);
  };

  const addTreeFileToChat = async () => {
    if (!treeMenu || treeMenu.isDir) return;
    const target = treeMenu;
    setTreeMenu(null);
    try {
      const file = await app.ReadFile(target.path);
      if (file.err || file.binary || file.kind) {
        onAddToChat?.(formatWorkspaceReference(target.path, false));
        return;
      }
      const suffix = file.truncated ? `\n\n${t("workspace.truncated")}` : "";
      onAddToChat?.(formatSelectionReference(target.path, file.body) + suffix);
    } catch {
      onAddToChat?.(formatWorkspaceReference(target.path, false));
    }
  };

  const addToGitignore = useCallback(async (path: string) => {
    if (!tabId) return;
    setTreeMenu(null);
    try {
      // Read existing .gitignore content
      const existing = await app.ReadFile(".gitignore");
      const lines = existing.body.split("\n");
      // Normalize path: replace backslashes with forward slashes for .gitignore
      const ignorePath = path.replace(/\\/g, "/");
      // Check if the path (or a pattern matching it) is already in .gitignore
      const alreadyIgnored = lines.some((line) => {
        const trimmed = line.trim();
        return trimmed !== "" && !trimmed.startsWith("#") && trimmed === ignorePath;
      });
      if (alreadyIgnored) return;
      // Append the path to .gitignore
      const newContent = existing.body.endsWith("\n")
        ? existing.body + ignorePath + "\n"
        : existing.body + "\n" + ignorePath + "\n";
      await app.ExportToWorkspace(tabId, ".gitignore", newContent);
    } catch {
      // .gitignore doesn't exist yet — create it
      const ignorePath = path.replace(/\\/g, "/");
      await app.ExportToWorkspace(tabId, ".gitignore", ignorePath + "\n");
    }
  }, [tabId]);

  const renderRows = (dir: string, depth: number): JSX.Element[] => {
    const entries = entriesByDir[dir] ?? [];
    const rows: JSX.Element[] = [];
    // If creating in this dir, show the creation input first
    if (creatingIn && creatingIn.dir === dir) {
      rows.push(
        <div
          key="__creating__"
          className="workspace-tree__row workspace-tree__row--creating"
          style={{ paddingLeft: 8 + (depth + 1) * 14 }}
        >
          {creatingIn.kind === "dir" ? <Folder size={14} className="workspace-tree__icon workspace-tree__icon--dir" /> : <FileText size={14} className="workspace-tree__icon" />}
          <input
            autoFocus
            className="workspace-tree__rename-input"
            value={createDraft}
            onChange={(e) => setCreateDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void commitCreate();
              if (e.key === "Escape") setCreatingIn(null);
            }}
            onBlur={() => void commitCreate()}
          />
        </div>
      );
    }
    for (const entry of entries) {
      const path = entryPath(dir, entry);
      const isOpen = openDirs.has(path);
      const active = selectedPath === path;
      // Inline rename mode
      if (renamingPath === path) {
        const renameRow = (
          <div
            key={path}
            className="workspace-tree__row workspace-tree__row--renaming"
            style={{ paddingLeft: 8 + depth * 14 }}
          >
            {entry.isDir ? (
              isOpen ? <ChevronDown size={13} className="workspace-tree__chev" /> : <ChevronRight size={13} className="workspace-tree__chev" />
            ) : (
              <span className="workspace-tree__chev" />
            )}
            {entry.isDir ? <Folder size={14} className="workspace-tree__icon workspace-tree__icon--dir" /> : <FileText size={14} className="workspace-tree__icon" />}
            <input
              autoFocus
              className="workspace-tree__rename-input"
              value={renameDraft}
              onChange={(e) => setRenameDraft(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void commitRename(path);
                if (e.key === "Escape") setRenamingPath(null);
              }}
              onBlur={() => void commitRename(path)}
            />
          </div>
        );
        rows.push(renameRow);
        if (entry.isDir && isOpen) {
          rows.push(...renderRows(path, depth + 1));
        }
        continue;
      }
      const row = (
        <button
          key={path}
          className={`workspace-tree__row${active ? " workspace-tree__row--active" : ""}`}
          draggable
          onDragStart={(event) => startTreeDrag(event, path, entry.isDir)}
          onClick={() => (entry.isDir ? toggleDir(path) : selectFile(path))}
          onContextMenu={(event) => openTreeMenu(event, path, entry.isDir)}
          style={{ paddingLeft: 8 + depth * 14 }}
        >
          {entry.isDir ? (
            isOpen ? (
              <ChevronDown size={13} className="workspace-tree__chev" />
            ) : (
              <ChevronRight size={13} className="workspace-tree__chev" />
            )
          ) : (
            <span className="workspace-tree__chev" />
          )}
          {entry.isDir ? (
            <Folder size={14} className="workspace-tree__icon workspace-tree__icon--dir" />
          ) : (
            <FileText size={14} className="workspace-tree__icon" />
          )}
          <span className="workspace-tree__name">{entry.name}</span>
        </button>
      );
      rows.push(row);
      if (entry.isDir && isOpen) {
        rows.push(...renderRows(path, depth + 1));
      }
    }
    return rows;
  };

  const isMarkdown = selectedPath?.toLowerCase().endsWith(".md") ?? false;

  const buildTreeMenuItems = useCallback((path: string, isDir: boolean): ContextMenuItem[] => {
    const items: ContextMenuItem[] = [];
    // File management operations
    if (isDir) {
      items.push(
        {
          key: "new-file",
          icon: <FilePlus size={13} />,
          label: t("workspace.newFile"),
          onSelect: () => startCreateIn(path, "file"),
        },
        {
          key: "new-folder",
          icon: <FolderPlus size={13} />,
          label: t("workspace.newFolder"),
          onSelect: () => startCreateIn(path, "dir"),
        },
      );
    }
    items.push(
      {
        key: "copy",
        icon: <Copy size={13} />,
        label: t("workspace.copy"),
        onSelect: () => handleCopy(path),
      },
      {
        key: "cut",
        icon: <Scissors size={13} />,
        label: t("workspace.cut"),
        onSelect: () => handleCut(path),
      },
    );
    if (clipboardOp) {
      // Can paste into a directory, or paste next to a file (into its parent)
      const pasteTarget = isDir ? path : parentPath(path);
      items.push({
        key: "paste",
        icon: <Clipboard size={13} />,
        label: t("workspace.paste"),
        onSelect: () => void handlePaste(pasteTarget),
      });
    }
    items.push(
      {
        key: "rename",
        icon: <Pencil size={13} />,
        label: t("workspace.rename"),
        onSelect: () => startRename(path),
      },
      {
        key: "delete",
        icon: <Trash2 size={13} />,
        label: confirmDeletePath === path ? t("workspace.confirmDelete") : t("workspace.delete"),
        danger: confirmDeletePath === path,
        onSelect: () => {
          if (confirmDeletePath === path) void handleTrash(path);
          else setConfirmDeletePath(path);
        },
      },
    );
    // Separator before chat/reference operations
    items.push({ type: "separator" as const, key: "chat-separator" });
    items.push({
      key: "add-reference",
      icon: <MessageSquarePlus size={13} />,
      label: isDir ? t("workspace.addFolderReferenceToChat") : t("workspace.addFileReferenceToChat"),
      onSelect: addTreeReferenceToChat,
    });
    if (!isDir) {
      items.push({
        key: "add-content",
        icon: <FileText size={13} />,
        label: t("workspace.addFileContentToChat"),
        onSelect: () => void addTreeFileToChat(),
      });
    }
    items.push({ type: "separator" as const, key: "git-separator" });
    items.push({
      key: "add-to-gitignore",
      icon: <FileX size={13} />,
      label: t("workspace.addToGitignore"),
      onSelect: () => void addToGitignore(path),
    });
    items.push({ type: "separator" as const, key: "system-separator" });
    items.push(
      {
        key: "reveal",
        icon: <FolderOpen size={13} />,
        label: t(revealLabelKey(platform)),
        onSelect: () => {
          setTreeMenu(null);
          void app.RevealWorkspacePath(path);
        },
      },
      {
        key: "copy-path",
        icon: <Copy size={13} />,
        label: t("workspace.copyPath"),
        onSelect: () => {
          void navigator.clipboard.writeText(path);
          setTreeMenu(null);
        },
      },
    );
    return items;
  }, [clipboardOp, confirmDeletePath, handleCopy, handleCut, handlePaste, handleTrash, platform, startCreateIn, startRename, t, addTreeReferenceToChat, addTreeFileToChat, addToGitignore]);

  const treeBlankMenuItems: ContextMenuItem[] = [
    {
      key: "new-file",
      icon: <FilePlus size={13} />,
      label: t("workspace.newFile"),
      onSelect: () => {
        setTreeBlankMenuPoint(null);
        startCreateIn("", "file");
      },
    },
    {
      key: "new-folder",
      icon: <FolderPlus size={13} />,
      label: t("workspace.newFolder"),
      onSelect: () => {
        setTreeBlankMenuPoint(null);
        startCreateIn("", "dir");
      },
    },
    ...(clipboardOp
      ? [
          {
            key: "paste",
            icon: <Clipboard size={13} />,
            label: t("workspace.paste"),
            onSelect: () => void handlePaste(""),
          } as ContextMenuItem,
        ]
      : []),
    { type: "separator" as const, key: "refresh-separator" },
    {
      key: "refresh-tree",
      icon: <RefreshCw size={13} />,
      label: t(viewMode === "changed" ? "workspace.refreshChanges" : "workspace.refreshTree"),
      onSelect: refreshWorkspaceList,
    },
  ];

  return (
    <aside
      ref={panelRef}
      className={`workspace-panel${embeddedDockMode ? " workspace-panel--embedded" : ""}${previewVisible && actualTreeVisible ? " workspace-panel--split-preview" : ""}${compactTreeSplit ? " workspace-panel--compact-split" : ""}${actualTreeVisible ? "" : " workspace-panel--tree-hidden"}${previewVisible ? "" : " workspace-panel--preview-hidden"}${treeResizing ? " workspace-panel--tree-resizing" : ""}`}
      aria-label={t("workspace.title")}
      style={panelStyle}
    >
      {previewVisible && <section className="workspace-preview">
        <header className="workspace-preview__head">
          <div className="workspace-current-file" aria-label={t("workspace.currentFile")}>
            <FileText size={15} className="workspace-current-file__icon" />
            <div className="workspace-current-file__text">
              <Tooltip label={selectedPath ?? undefined}>
                <span className="workspace-current-file__name">{currentFileName}</span>
              </Tooltip>
              {currentFileDir && <span className="workspace-current-file__path">{currentFileDir}</span>}
            </div>
            <Tooltip label={t("workspace.recentFiles")}>
              <button
                ref={recentAnchorRef}
                className={`workspace-current-file__recent${recentOpen ? " workspace-current-file__recent--open" : ""}`}
                type="button"
                aria-label={t("workspace.recentFiles")}
                aria-expanded={recentOpen}
                onClick={() => setRecentOpen((open) => !open)}
              >
                <ChevronDown size={13} />
              </button>
            </Tooltip>
          </div>

          <div className="workspace-preview__window-actions">
            <Tooltip label={maximized ? t("workspace.restore") : t("workspace.maximize")}>
              <button className="workspace-iconbtn" onClick={onToggleMaximized}>
                {maximized ? <Minimize2 size={15} /> : <Maximize2 size={15} />}
              </button>
            </Tooltip>
            {selectedPath && (
              <Tooltip label={t("workspace.closePreview")}>
                <button className="workspace-iconbtn" onClick={() => closeTab(selectedPath)}>
                  <X size={15} />
                </button>
              </Tooltip>
            )}
          </div>
          <AnchoredPopover
            open={recentOpen}
            anchorRef={recentAnchorRef}
            onClose={() => setRecentOpen(false)}
            className="workspace-recent-menu"
            align="start"
            offset={6}
            placement="bottom"
          >
            <div className="workspace-recent-menu__title">{t("workspace.recentFiles")}</div>
            <div className="workspace-recent-menu__list">
              {recentFiles.map((path) => (
                <button
                  key={path}
                  type="button"
                  className={`workspace-recent-menu__item${path === selectedPath ? " workspace-recent-menu__item--active" : ""}`}
                  onClick={() => {
                    setSelectedPath(path);
                    setRecentOpen(false);
                  }}
                >
                  <FileText size={14} />
                  <span>
                    <span className="workspace-recent-menu__name">{basename(path)}</span>
                    <span className="workspace-recent-menu__path">{parentPath(path)}</span>
                  </span>
                </button>
              ))}
            </div>
          </AnchoredPopover>
        </header>

        <div className="workspace-preview__meta">
          <Tooltip label={cwd}>
            <button
              className="workspace-crumb"
              onClick={() => {
                setFilter("");
                setTreeVisible(true);
                onRequestPanelWidth?.(WORKSPACE_DUAL_PANEL_TARGET_WIDTH);
                setOpenDirs((prev) => new Set([...Array.from(prev), ""]));
              }}
            >
              {shortCwd(cwd) || t("workspace.title")}
            </button>
          </Tooltip>
          {pathParts.map((part, index) => {
            const isLast = index === pathParts.length - 1;
            const dir = pathParts.slice(0, index + 1).join("/") + "/";
            return (
              <span className="workspace-crumb-group" key={`${part}-${index}`}>
                <span>›</span>
                <Tooltip label={isLast ? (selectedPath ?? undefined) : dir}>
                  <button
                    className={`workspace-crumb${isLast ? " workspace-crumb--current" : ""}`}
                    onClick={() => {
                      if (isLast) return;
                      setTreeVisible(true);
                      onRequestPanelWidth?.(WORKSPACE_DUAL_PANEL_TARGET_WIDTH);
                      setFilter("");
                      setOpenDirs((prev) => new Set([...Array.from(prev), ...breadcrumbDirs, dir]));
                      void loadDir(dir);
                    }}
                  >
                    {part}
                  </button>
                </Tooltip>
              </span>
            );
          })}
          {preview && preview.size > 0 && <span className="workspace-preview__size">{formatBytes(preview.size)}</span>}
        </div>

        <div className="workspace-preview__body" ref={previewBodyRef} onContextMenu={openSelectionMenu}>
          {!selectedPath ? (
            <div className="workspace-empty">{t("workspace.pickFile")}</div>
          ) : loadingPreview ? (
            <div className="workspace-empty">{t("workspace.loading")}</div>
          ) : preview?.err ? (
            <div className="workspace-empty workspace-empty--error">{preview.err}</div>
          ) : preview?.kind ? (
            renderMediaPreview(preview)
          ) : preview?.binary ? (
            <div className="workspace-empty">{t("workspace.binary")}</div>
          ) : preview ? (
            <>
              {preview.truncated && <div className="workspace-note">{t("workspace.truncated")}</div>}
              {isMarkdown ? (
                <Markdown text={preview.body} />
              ) : (
                <CodeViewer value={preview.body || " "} language={languageFor(selectedPath)} />
              )}
            </>
          ) : null}
          {selectionMenu && (
            <FloatingMenu x={selectionMenu.x} y={selectionMenu.y} estimatedHeight={WORKSPACE_SELECTION_MENU_HEIGHT}>
              <FloatingMenuItems
                items={[
                  {
                    icon: <MessageSquarePlus size={14} />,
                    label: t("workspace.addSelectionToChat"),
                    onSelect: addSelectionToChat,
                  },
                ]}
              />
            </FloatingMenu>
          )}
        </div>
      </section>}

      {previewVisible && !actualTreeVisible && (
        <section className="workspace-tree-rail" aria-label={t("workspace.showTree")}>
          <Tooltip label={t("workspace.showTree")} side="right">
            <button
              className="workspace-tree-reveal workspace-iconbtn workspace-iconbtn--on"
              type="button"
              aria-label={t("workspace.showTree")}
              onClick={() => {
                setTreeVisible(true);
                onRequestPanelWidth?.(WORKSPACE_DUAL_PANEL_TARGET_WIDTH);
              }}
            >
              <FolderTree size={15} />
            </button>
          </Tooltip>
        </section>
      )}

      {actualTreeVisible && previewVisible && (
        <button
          className="workspace-tree-resizer"
          type="button"
          role="separator"
          aria-orientation="vertical"
          aria-label={t("workspace.resizeTree")}
          aria-valuemin={WORKSPACE_TREE_MIN_WIDTH}
          aria-valuemax={WORKSPACE_TREE_MAX_WIDTH}
          aria-valuenow={effectiveTreeWidth}
          onPointerDown={startTreeResize}
          onKeyDown={resizeTreeWithKeyboard}
          onDoubleClick={() => setSavedTreeWidth(WORKSPACE_TREE_DEFAULT_WIDTH)}
        />
      )}

      <section className="workspace-files">
        {showFileTools && (
          <div className={`workspace-files__tools${embeddedDockMode ? " workspace-files__tools--embedded" : ""}`}>
            <Tooltip label={previewVisible ? t("workspace.hideTree") : t("workspace.close")}>
              <button
                className="workspace-iconbtn workspace-iconbtn--on"
                type="button"
                aria-label={previewVisible ? t("workspace.hideTree") : t("workspace.close")}
                onClick={hideTreeOrClosePanel}
              >
                {previewVisible ? <FolderX size={15} /> : <X size={15} />}
              </button>
            </Tooltip>
            {showViewTabs && (
              <div className="workspace-files__tabs" role="tablist" aria-label={t("workspace.viewMode")}>
                <button
                  className={viewMode === "files" ? "workspace-files__tab workspace-files__tab--active" : "workspace-files__tab"}
                  onClick={() => setViewMode("files")}
                >
                  {t("workspace.filesTab")}
                </button>
                <button
                  className={viewMode === "changed" ? "workspace-files__tab workspace-files__tab--active" : "workspace-files__tab"}
                  onClick={() => {
                    setViewMode("changed");
                    void loadChanges();
                  }}
                >
                  <GitBranch size={13} />
                  {t("workspace.changedTab")}
                  {uncommittedCount > 0 && <span className="workspace-tab-badge">{uncommittedCount}</span>}
                </button>
              </div>
            )}
            {showViewTabs && (
              <Tooltip label={t("workspace.refreshChanges")}>
                <button className="workspace-iconbtn" onClick={() => void loadChanges()}>
                  <RefreshCw size={14} />
                </button>
              </Tooltip>
            )}
          </div>
        )}

        <div className="workspace-search">
          <Search size={14} />
          <input ref={filterRef} value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={searchPlaceholder} />
        </div>
        {viewMode === "changed" && changes && !changes.gitAvailable && changes.gitErr && (
          <div className="workspace-note workspace-note--compact">{t("workspace.gitUnavailable")}</div>
        )}
        <div className="workspace-tree" onContextMenu={openTreeBlankMenu}>
          {viewMode === "changed"
            ? <SourceControlPanel refreshKey={refreshKey} onStatusChange={setUncommittedCount} />
            : flattened
            ? flattened.map(({ path, entry }) => {
                const dir = parentPath(path);
                return (
                  <button
                    key={path}
                    className={`workspace-tree__row workspace-tree__row--search${selectedPath === path ? " workspace-tree__row--active" : ""}`}
                    draggable
                    onDragStart={(event) => startTreeDrag(event, path, entry.isDir)}
                    onClick={() => (entry.isDir ? toggleDir(path) : selectFile(path))}
                    onContextMenu={(event) => openTreeMenu(event, path, entry.isDir)}
                  >
                    {entry.isDir ? (
                      <Folder size={14} className="workspace-tree__icon workspace-tree__icon--dir" />
                    ) : (
                      <FileText size={14} className="workspace-tree__icon" />
                    )}
                    <span className="workspace-tree__result">
                      <span className="workspace-tree__result-name">{basename(path)}</span>
                      {dir && <span className="workspace-tree__result-dir">{dir}</span>}
                    </span>
                  </button>
                );
              })
            : renderRows("", 0)}
        </div>
      </section>
      <ContextMenu
        open={Boolean(treeMenu)}
        point={treeMenu?.point ?? null}
        items={treeMenu ? buildTreeMenuItems(treeMenu.path, treeMenu.isDir) : []}
        minWidth={200}
        ariaLabel={t("workspace.treeMenu")}
        onClose={closeTreeMenu}
      />
      <ContextMenu
        open={Boolean(treeBlankMenuPoint)}
        point={treeBlankMenuPoint}
        items={treeBlankMenuItems}
        minWidth={150}
        ariaLabel={t("workspace.treeMenu")}
        onClose={() => setTreeBlankMenuPoint(null)}
      />
    </aside>
  );
}
