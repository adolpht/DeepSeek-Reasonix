import { useCallback, useEffect, useRef, useState } from "react";
import {
  ChevronDown,
  GitBranch,
  GitCommit,
  GitMerge,
  Plus,
  RefreshCw,
  Sparkles,
  Tag,
  Trash2,
  Upload,
  Download,
  Archive,
  ArchiveRestore,
  MoreHorizontal,
  FileText,
  AlertTriangle,
  Check,
  X,
  CornerDownLeft,
} from "lucide-react";
import { app } from "../lib/bridge";
import { t, useT } from "../lib/i18n";
import type {
  BranchView,
  CommitView,
  GitDiffView,
  GitFileStatus,
  GitOperationResult,
  GitStatusView,
  StashEntryView,
  TagView,
} from "../lib/types";
import { AnchoredPopover } from "./AnchoredPopover";
import { Tooltip } from "./Tooltip";

// Status letter → human-readable label
function statusLabel(x: string, y: string): string {
  const code = y || x;
  switch (code) {
    case "M": return t("git.modified");
    case "A": return t("git.added");
    case "D": return t("git.deleted");
    case "R": return t("git.renamed");
    case "C": return t("git.copied");
    case "?": return t("git.untracked");
    case "!": return t("git.ignored");
    case "U": return t("git.unmerged");
    default: return code;
  }
}

function statusBadgeClass(x: string, y: string): string {
  const code = y || x;
  switch (code) {
    case "M": return "scm-badge scm-badge--modified";
    case "A": return "scm-badge scm-badge--added";
    case "D": return "scm-badge scm-badge--deleted";
    case "R": return "scm-badge scm-badge--renamed";
    case "?": return "scm-badge scm-badge--untracked";
    case "U": return "scm-badge scm-badge--conflict";
    default: return "scm-badge";
  }
}

function formatDate(iso: string): string {
  try {
    const d = new Date(iso);
    const now = new Date();
    const diffMs = now.getTime() - d.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    if (diffMin < 1) return t("git.justNow");
    if (diffMin < 60) return t("git.minutesAgo", { n: diffMin });
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return t("git.hoursAgo", { n: diffHr });
    const diffDay = Math.floor(diffHr / 24);
    if (diffDay < 7) return t("git.daysAgo", { n: diffDay });
    return d.toLocaleDateString();
  } catch {
    return iso;
  }
}

type ScmSubView = "status" | "log" | "branches" | "stash" | "tags";

export function SourceControlPanel({ refreshKey, onStatusChange }: { refreshKey?: number; onStatusChange?: (count: number) => void }) {
  const t = useT();
  const [status, setStatus] = useState<GitStatusView | null>(null);
  const [loading, setLoading] = useState(false);
  const [subView, setSubView] = useState<ScmSubView>("status");
  const [commitMsg, setCommitMsg] = useState("");
  const [opResult, setOpResult] = useState<GitOperationResult | null>(null);
  const commitInputRef = useRef<HTMLTextAreaElement>(null);

  // Sub-view data
  const [commits, setCommits] = useState<CommitView[]>([]);
  const [branches, setBranches] = useState<BranchView[]>([]);
  const [stashes, setStashes] = useState<StashEntryView[]>([]);
  const [tags, setTags] = useState<TagView[]>([]);

  // Diff preview
  const [diffView, setDiffView] = useState<GitDiffView | null>(null);
  const [diffLoading, setDiffLoading] = useState(false);

  // Branch picker
  const branchBtnRef = useRef<HTMLButtonElement>(null);
  const [branchPickerOpen, setBranchPickerOpen] = useState(false);
  const [newBranchName, setNewBranchName] = useState("");
  const [showNewBranch, setShowNewBranch] = useState(false);

  // More menu
  const moreBtnRef = useRef<HTMLButtonElement>(null);
  const [moreOpen, setMoreOpen] = useState(false);

  // Merge/rebase dialog
  const [mergeDialog, setMergeDialog] = useState<"merge" | "rebase" | null>(null);
  const [mergeBranch, setMergeBranch] = useState("");

  // Tag dialog
  const [tagDialog, setTagDialog] = useState(false);
  const [tagName, setTagName] = useState("");
  const [tagMessage, setTagMessage] = useState("");

  // AI commit message generation
  const [generating, setGenerating] = useState(false);

  const loadStatus = useCallback(async () => {
    setLoading(true);
    try {
      const s = await app.GitStatus();
      // Ensure array fields are never null (Go may return null for empty slices)
      if (s) {
        s.staged = s.staged ?? [];
        s.unstaged = s.unstaged ?? [];
        s.untracked = s.untracked ?? [];
        s.conflicted = s.conflicted ?? [];
      }
      setStatus(s);
      if (s && onStatusChange) {
        onStatusChange(s.staged.length + s.unstaged.length + s.untracked.length + s.conflicted.length);
      } else if (onStatusChange) {
        onStatusChange(0);
      }
    } catch {
      setStatus(null);
      if (onStatusChange) onStatusChange(0);
    } finally {
      setLoading(false);
    }
  }, [onStatusChange]);

  const loadLog = useCallback(async () => {
    try {
      const c = await app.GitLog(50);
      setCommits((c ?? []).map((cv) => ({ ...cv, refs: cv.refs ?? [] })));
    } catch {
      setCommits([]);
    }
  }, []);

  const loadBranches = useCallback(async () => {
    try {
      const b = await app.GitBranches();
      setBranches(b ?? []);
    } catch {
      setBranches([]);
    }
  }, []);

  const loadStashes = useCallback(async () => {
    try {
      const s = await app.GitStashList();
      setStashes(s ?? []);
    } catch {
      setStashes([]);
    }
  }, []);

  const loadTags = useCallback(async () => {
    try {
      const tg = await app.GitTags();
      setTags(tg ?? []);
    } catch {
      setTags([]);
    }
  }, []);

  // Load status on mount and when refreshKey changes
  useEffect(() => {
    void loadStatus();
  }, [loadStatus, refreshKey]);

  // Load sub-view data when switching
  useEffect(() => {
    if (subView === "log") void loadLog();
    else if (subView === "branches") void loadBranches();
    else if (subView === "stash") void loadStashes();
    else if (subView === "tags") void loadTags();
  }, [subView, loadLog, loadBranches, loadStashes, loadTags]);

  const execOp = useCallback(async (op: () => Promise<GitOperationResult>, reload = true) => {
    setOpResult(null);
    const result = await op();
    setOpResult(result);
    if (result.success && reload) {
      await loadStatus();
      if (subView === "log") await loadLog();
      else if (subView === "branches") await loadBranches();
      else if (subView === "stash") await loadStashes();
      else if (subView === "tags") await loadTags();
    }
    // Auto-clear after 3s
    setTimeout(() => setOpResult(null), 3000);
    return result;
  }, [loadStatus, loadLog, loadBranches, loadStashes, loadTags, subView]);

  const handleStage = useCallback((paths: string[]) => {
    void execOp(() => app.GitAdd(paths));
  }, [execOp]);

  const handleUnstage = useCallback((paths: string[]) => {
    void execOp(() => app.GitReset(paths));
  }, [execOp]);

  const handleCommit = useCallback(() => {
    if (!commitMsg.trim()) return;
    void execOp(async () => {
      const result = await app.GitCommit(commitMsg.trim());
      if (result.success) setCommitMsg("");
      return result;
    });
  }, [commitMsg, execOp]);

  const handleGenerateCommitMsg = useCallback(() => {
    setGenerating(true);
    app.GitGenerateCommitMessage().then((msg) => {
      if (msg) setCommitMsg(msg);
    }).catch((e) => {
      setOpResult({ success: false, message: String(e) });
    }).finally(() => {
      setGenerating(false);
    });
  }, []);

  const handleDiff = useCallback((path: string, staged: boolean) => {
    setDiffLoading(true);
    setDiffView(null);
    app.GitDiff(path, staged).then((d) => {
      setDiffView(d);
      setDiffLoading(false);
    }).catch(() => setDiffLoading(false));
  }, []);

  const handleRestore = useCallback((paths: string[]) => {
    void execOp(() => app.GitRestore(paths));
  }, [execOp]);

  const handlePush = useCallback(() => {
    void execOp(() => app.GitPush(""));
  }, [execOp]);

  const handlePull = useCallback(() => {
    void execOp(() => app.GitPull());
  }, [execOp]);

  const handleFetch = useCallback(() => {
    void execOp(() => app.GitFetch());
  }, [execOp]);

  const handleCheckout = useCallback((branch: string) => {
    void execOp(() => app.GitCheckout(branch, false));
    setBranchPickerOpen(false);
  }, [execOp]);

  const handleCreateBranch = useCallback(() => {
    if (!newBranchName.trim()) return;
    void execOp(async () => {
      const result = await app.GitCheckout(newBranchName.trim(), true);
      if (result.success) {
        setNewBranchName("");
        setShowNewBranch(false);
        setBranchPickerOpen(false);
      }
      return result;
    });
  }, [newBranchName, execOp]);

  const handleStashPush = useCallback(() => {
    void execOp(() => app.GitStashPush(""));
  }, [execOp]);

  const handleStashPop = useCallback((index: number) => {
    void execOp(() => app.GitStashPop(index));
  }, [execOp]);

  const handleStashApply = useCallback((index: number) => {
    void execOp(() => app.GitStashApply(index));
  }, [execOp]);

  const handleMerge = useCallback(() => {
    if (!mergeBranch.trim()) return;
    void execOp(() => app.GitMerge(mergeBranch.trim(), true));
    setMergeDialog(null);
    setMergeBranch("");
  }, [mergeBranch, execOp]);

  const handleRebase = useCallback(() => {
    if (!mergeBranch.trim()) return;
    void execOp(() => app.GitRebase(mergeBranch.trim()));
    setMergeDialog(null);
    setMergeBranch("");
  }, [mergeBranch, execOp]);

  const handleCreateTag = useCallback(() => {
    if (!tagName.trim()) return;
    void execOp(() => app.GitCreateTag(tagName.trim(), tagMessage.trim()));
    setTagDialog(false);
    setTagName("");
    setTagMessage("");
  }, [tagName, tagMessage, execOp]);

  const handleDeleteTag = useCallback((name: string) => {
    void execOp(() => app.GitDeleteTag(name));
  }, [execOp]);

  const handleDeleteBranch = useCallback((name: string) => {
    void execOp(() => app.GitDeleteBranch(name, false));
  }, [execOp]);

  // --- Render: header with branch + actions ---
  const renderHeader = () => (
    <div className="scm-header">
      <div className="scm-branch-row">
        <button
          ref={branchBtnRef}
          className="scm-branch-btn"
          onClick={() => setBranchPickerOpen((v) => !v)}
        >
          <GitBranch size={14} />
          <span className="scm-branch-name">{status?.branch || "-"}</span>
          {status?.ahead ? <span className="scm-aheadbehind scm-aheadbehind--ahead">↑{status.ahead}</span> : null}
          {status?.behind ? <span className="scm-aheadbehind scm-aheadbehind--behind">↓{status.behind}</span> : null}
          <ChevronDown size={12} />
        </button>
        {branchPickerOpen && renderBranchPicker()}
      </div>
      <div className="scm-actions">
        <Tooltip label={t("git.refresh")}>
          <button className="scm-icon-btn" onClick={() => void loadStatus()}><RefreshCw size={14} /></button>
        </Tooltip>
        <Tooltip label={t("git.pull")}>
          <button className="scm-icon-btn" onClick={handlePull}><Download size={14} /></button>
        </Tooltip>
        <Tooltip label={t("git.push")}>
          <button className="scm-icon-btn" onClick={handlePush}><Upload size={14} /></button>
        </Tooltip>
        <Tooltip label={t("git.fetch")}>
          <button className="scm-icon-btn" onClick={handleFetch}><RefreshCw size={13} /></button>
        </Tooltip>
        <div style={{ position: "relative" }}>
          <Tooltip label={t("git.more")}>
            <button ref={moreBtnRef} className="scm-icon-btn" onClick={() => setMoreOpen((v) => !v)}><MoreHorizontal size={14} /></button>
          </Tooltip>
          {moreOpen && renderMoreMenu()}
        </div>
      </div>
    </div>
  );

  const renderBranchPicker = () => {
    const local = branches.filter((b) => !b.isRemote);
    const remote = branches.filter((b) => b.isRemote);
    return (
      <AnchoredPopover open={branchPickerOpen} anchorRef={branchBtnRef} className="scm-branch-popover" onClose={() => setBranchPickerOpen(false)}>
        <div className="scm-branch-picker">
          {local.map((b) => (
            <button
              key={b.name}
              className={`scm-branch-item${b.isCurrent ? " scm-branch-item--current" : ""}`}
              onClick={() => { if (!b.isCurrent) handleCheckout(b.name); }}
              disabled={b.isCurrent}
            >
              <GitBranch size={12} />
              <span>{b.name}</span>
              {b.isCurrent && <Check size={12} className="scm-check" />}
              {!b.isCurrent && (
                <button className="scm-branch-del" onClick={(e) => { e.stopPropagation(); handleDeleteBranch(b.name); }} title={t("git.deleteBranch")}>
                  <Trash2 size={11} />
                </button>
              )}
            </button>
          ))}
          {remote.length > 0 && (
            <>
              <div className="scm-branch-group">{t("git.remoteBranches")}</div>
              {remote.map((b) => (
                <button key={b.name} className="scm-branch-item" onClick={() => handleCheckout(b.name)}>
                  <GitBranch size={12} />
                  <span>{b.name.replace(/^remotes\//, "")}</span>
                </button>
              ))}
            </>
          )}
          <div className="scm-new-branch">
            {showNewBranch ? (
              <div className="scm-new-branch-form">
                <input
                  value={newBranchName}
                  onChange={(e) => setNewBranchName(e.target.value)}
                  placeholder={t("git.newBranchPlaceholder")}
                  onKeyDown={(e) => { if (e.key === "Enter") handleCreateBranch(); if (e.key === "Escape") setShowNewBranch(false); }}
                  autoFocus
                />
                <button className="scm-icon-btn" onClick={handleCreateBranch}><Check size={14} /></button>
                <button className="scm-icon-btn" onClick={() => setShowNewBranch(false)}><X size={14} /></button>
              </div>
            ) : (
              <button className="scm-branch-item" onClick={() => setShowNewBranch(true)}>
                <Plus size={12} />
                <span>{t("git.createBranch")}</span>
              </button>
            )}
          </div>
        </div>
      </AnchoredPopover>
    );
  };

  const renderMoreMenu = () => (
    <AnchoredPopover open={moreOpen} anchorRef={moreBtnRef} className="scm-more-popover" onClose={() => setMoreOpen(false)}>
      <div className="scm-more-menu">
        <button onClick={() => { setMergeDialog("merge"); setMoreOpen(false); }}>
          <GitMerge size={14} /> {t("git.merge")}
        </button>
        <button onClick={() => { setMergeDialog("rebase"); setMoreOpen(false); }}>
          <RefreshCw size={14} /> {t("git.rebase")}
        </button>
        <button onClick={() => { handleStashPush(); setMoreOpen(false); }}>
          <Archive size={14} /> {t("git.stash")}
        </button>
        <button onClick={() => { setTagDialog(true); setMoreOpen(false); }}>
          <Tag size={14} /> {t("git.createTag")}
        </button>
        <button onClick={() => { void app.GitInit(); setMoreOpen(false); }}>
          <GitBranch size={14} /> {t("git.initRepo")}
        </button>
      </div>
    </AnchoredPopover>
  );

  // --- Render: sub-view tabs ---
  const renderTabs = () => (
    <div className="scm-tabs">
      {(["status", "log", "branches", "stash", "tags"] as ScmSubView[]).map((v) => (
        <button
          key={v}
          className={`scm-tab${subView === v ? " scm-tab--active" : ""}`}
          onClick={() => setSubView(v)}
        >
          {t(`git.tab.${v}`)}
          {v === "status" && status && (status.staged.length + status.unstaged.length + status.untracked.length + status.conflicted.length) > 0 && (
            <span className="scm-tab-count">{status.staged.length + status.unstaged.length + status.untracked.length + status.conflicted.length}</span>
          )}
          {v === "stash" && status && status.stashCount > 0 && (
            <span className="scm-tab-count">{status.stashCount}</span>
          )}
        </button>
      ))}
    </div>
  );

  // --- Render: status view (staged/unstaged/untracked/conflicted) ---
  const renderStatus = () => {
    if (!status) return <div className="scm-empty">{t("git.noStatus")}</div>;
    if (!status.gitAvailable) return <div className="scm-empty scm-empty--err">{t("git.unavailable")}{status.gitErr ? `: ${status.gitErr}` : ""}</div>;

    return (
      <div className="scm-status">
        {/* Commit input */}
        <div className="scm-commit-area">
          <textarea
            ref={commitInputRef}
            className="scm-commit-input"
            value={commitMsg}
            onChange={(e) => setCommitMsg(e.target.value)}
            placeholder={t("git.commitPlaceholder")}
            rows={2}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); handleCommit(); }
            }}
          />
          <div className="scm-commit-actions">
            <Tooltip label={t("git.generateCommitMsg")}>
              <button
                className="scm-icon-btn scm-ai-btn"
                onClick={handleGenerateCommitMsg}
                disabled={generating || (status.staged.length + status.unstaged.length + status.untracked.length + status.conflicted.length === 0)}
              >
                <Sparkles size={14} className={generating ? "scm-ai-spin" : ""} />
              </button>
            </Tooltip>
            <button className="scm-commit-btn" onClick={handleCommit} disabled={!commitMsg.trim() || (status.staged.length + status.unstaged.length + status.untracked.length + status.conflicted.length === 0)}>
              <GitCommit size={14} />
              {t("git.commit")}
            </button>
          </div>
        </div>

        {/* Conflicted */}
        {status.conflicted.length > 0 && renderFileGroup(t("git.conflicted"), status.conflicted, true, false)}

        {/* Staged */}
        {renderFileGroup(t("git.staged"), status.staged, true, true)}

        {/* Unstaged */}
        {renderFileGroup(t("git.unstaged"), status.unstaged, false, false)}

        {/* Untracked */}
        {renderFileGroup(t("git.untracked"), status.untracked, false, false)}
      </div>
    );
  };

  const renderFileGroup = (label: string, files: GitFileStatus[], canUnstage: boolean, canStage: boolean) => {
    if (files.length === 0) return null;
    return (
      <div className="scm-group">
        <div className="scm-group-header">
          <span>{label}</span>
          <span className="scm-group-count">{files.length}</span>
          {canStage && (
            <button className="scm-group-action" onClick={() => handleStage([])} title={t("git.stageAll")}>
              <Plus size={12} />
            </button>
          )}
          {canUnstage && (
            <button className="scm-group-action" onClick={() => handleUnstage([])} title={t("git.unstageAll")}>
              <X size={12} />
            </button>
          )}
        </div>
        {files.map((f) => (
          <div key={f.path} className="scm-file">
            <button className="scm-file-path" onClick={() => handleDiff(f.path, canUnstage)}>
              <FileText size={13} />
              <span className="scm-file-name">{f.path.split("/").pop()}</span>
              <span className="scm-file-dir">{f.path.includes("/") ? f.path.substring(0, f.path.lastIndexOf("/")) : ""}</span>
            </button>
            <span className={statusBadgeClass(f.x, f.y)}>{statusLabel(f.x, f.y)}</span>
            <div className="scm-file-actions">
              {canStage && (
                <button className="scm-file-action" onClick={() => handleStage([f.path])} title={t("git.stage")}>
                  <Plus size={12} />
                </button>
              )}
              {canUnstage && (
                <button className="scm-file-action" onClick={() => handleUnstage([f.path])} title={t("git.unstage")}>
                  <MinusIcon />
                </button>
              )}
              {!canUnstage && !canStage && f.y === "M" && (
                <button className="scm-file-action" onClick={() => handleRestore([f.path])} title={t("git.discard")}>
                  <X size={12} />
                </button>
              )}
              {f.y === "?" && (
                <button className="scm-file-action" onClick={() => handleStage([f.path])} title={t("git.stage")}>
                  <Plus size={12} />
                </button>
              )}
            </div>
          </div>
        ))}
      </div>
    );
  };

  // --- Render: log view ---
  const renderLog = () => {
    if (commits.length === 0) return <div className="scm-empty">{t("git.noCommits")}</div>;
    return (
      <div className="scm-log">
        {commits.map((c) => (
          <button key={c.hash} className="scm-commit-entry" onClick={() => {
            setDiffLoading(true);
            app.GitShowCommit(c.hash).then((d) => { setDiffView(d); setDiffLoading(false); }).catch(() => setDiffLoading(false));
          }}>
            <div className="scm-commit-hash">{c.shortHash}</div>
            <div className="scm-commit-info">
              <div className="scm-commit-subject">{c.subject}</div>
              <div className="scm-commit-meta">
                <span>{c.author}</span>
                <span>{formatDate(c.date)}</span>
                {c.refs && c.refs.map((r) => (
                  <span key={r} className={`scm-ref${r.startsWith("tag:") ? " scm-ref--tag" : ""}`}>{r.replace(/^tag:\s*/, "")}</span>
                ))}
              </div>
            </div>
          </button>
        ))}
      </div>
    );
  };

  // --- Render: branches view ---
  const renderBranches = () => {
    const local = branches.filter((b) => !b.isRemote);
    const remote = branches.filter((b) => b.isRemote);
    return (
      <div className="scm-branches">
        <div className="scm-group">
          <div className="scm-group-header">{t("git.localBranches")}</div>
          {local.map((b) => (
            <div key={b.name} className={`scm-branch-row${b.isCurrent ? " scm-branch-row--current" : ""}`}>
              <GitBranch size={13} />
              <span className="scm-branch-label">{b.name}</span>
              {b.isCurrent && <span className="scm-current-badge">{t("git.current")}</span>}
              {b.upstream && <span className="scm-upstream">{b.upstream}</span>}
              {(b.ahead > 0 || b.behind > 0) && (
                <span className="scm-aheadbehind">
                  {b.ahead > 0 && <span className="scm-aheadbehind--ahead">↑{b.ahead}</span>}
                  {b.behind > 0 && <span className="scm-aheadbehind--behind">↓{b.behind}</span>}
                </span>
              )}
              {!b.isCurrent && (
                <button className="scm-file-action" onClick={() => handleCheckout(b.name)} title={t("git.switchTo")}>
                  <CornerDownLeft size={12} />
                </button>
              )}
              {!b.isCurrent && (
                <button className="scm-file-action" onClick={() => handleDeleteBranch(b.name)} title={t("git.deleteBranch")}>
                  <Trash2 size={12} />
                </button>
              )}
            </div>
          ))}
        </div>
        {remote.length > 0 && (
          <div className="scm-group">
            <div className="scm-group-header">{t("git.remoteBranches")}</div>
            {remote.map((b) => (
              <div key={b.name} className="scm-branch-row">
                <GitBranch size={13} />
                <span className="scm-branch-label">{b.name.replace(/^remotes\//, "")}</span>
              </div>
            ))}
          </div>
        )}
      </div>
    );
  };

  // --- Render: stash view ---
  const renderStash = () => {
    if (stashes.length === 0) return <div className="scm-empty">{t("git.noStashes")}</div>;
    return (
      <div className="scm-stashes">
        {stashes.map((s) => (
          <div key={s.index} className="scm-stash-entry">
            <Archive size={13} />
            <span className="scm-stash-msg">{s.message}</span>
            <button className="scm-file-action" onClick={() => handleStashPop(s.index)} title={t("git.stashPop")}>
              <ArchiveRestore size={12} />
            </button>
            <button className="scm-file-action" onClick={() => handleStashApply(s.index)} title={t("git.stashApply")}>
              <Check size={12} />
            </button>
          </div>
        ))}
      </div>
    );
  };

  // --- Render: tags view ---
  const renderTags = () => {
    return (
      <div className="scm-tags">
        <div className="scm-group-header">
          <span>{t("git.tags")}</span>
          <button className="scm-group-action" onClick={() => setTagDialog(true)} title={t("git.createTag")}>
            <Plus size={12} />
          </button>
        </div>
        {tags.length === 0 && <div className="scm-empty">{t("git.noTags")}</div>}
        {tags.map((tg) => (
          <div key={tg.name} className="scm-tag-entry">
            <Tag size={13} />
            <span className="scm-tag-name">{tg.name}</span>
            <span className="scm-tag-hash">{tg.hash}</span>
            {tg.subject && <span className="scm-tag-subject">{tg.subject}</span>}
            <button className="scm-file-action" onClick={() => handleDeleteTag(tg.name)} title={t("git.deleteTag")}>
              <Trash2 size={12} />
            </button>
          </div>
        ))}
      </div>
    );
  };

  // --- Render: diff preview ---
  const renderDiff = () => {
    if (diffLoading) return <div className="scm-diff scm-diff--loading">{t("git.loadingDiff")}</div>;
    if (!diffView) return null;
    if (diffView.err) return <div className="scm-diff scm-diff--err">{diffView.err}</div>;
    return (
      <div className="scm-diff">
        <div className="scm-diff-header">
          <span>{diffView.path || t("git.fullDiff")}</span>
          <button className="scm-icon-btn" onClick={() => setDiffView(null)}><X size={14} /></button>
        </div>
        <pre className="scm-diff-content">{diffView.content}</pre>
      </div>
    );
  };

  // --- Render: merge/rebase dialog ---
  const renderMergeDialog = () => {
    if (!mergeDialog) return null;
    return (
      <div className="scm-dialog-overlay" onClick={() => setMergeDialog(null)}>
        <div className="scm-dialog" onClick={(e) => e.stopPropagation()}>
          <h3>{mergeDialog === "merge" ? t("git.mergeTitle") : t("git.rebaseTitle")}</h3>
          <input
            value={mergeBranch}
            onChange={(e) => setMergeBranch(e.target.value)}
            placeholder={t("git.branchNamePlaceholder")}
            autoFocus
          />
          <div className="scm-dialog-actions">
            <button onClick={() => setMergeDialog(null)}>{t("git.cancel")}</button>
            <button className="scm-dialog-primary" onClick={mergeDialog === "merge" ? handleMerge : handleRebase}>
              {mergeDialog === "merge" ? t("git.merge") : t("git.rebase")}
            </button>
          </div>
        </div>
      </div>
    );
  };

  // --- Render: tag dialog ---
  const renderTagDialog = () => {
    if (!tagDialog) return null;
    return (
      <div className="scm-dialog-overlay" onClick={() => setTagDialog(false)}>
        <div className="scm-dialog" onClick={(e) => e.stopPropagation()}>
          <h3>{t("git.createTagTitle")}</h3>
          <input
            value={tagName}
            onChange={(e) => setTagName(e.target.value)}
            placeholder={t("git.tagNamePlaceholder")}
            autoFocus
          />
          <input
            value={tagMessage}
            onChange={(e) => setTagMessage(e.target.value)}
            placeholder={t("git.tagMessagePlaceholder")}
          />
          <div className="scm-dialog-actions">
            <button onClick={() => setTagDialog(false)}>{t("git.cancel")}</button>
            <button className="scm-dialog-primary" onClick={handleCreateTag}>{t("git.createTag")}</button>
          </div>
        </div>
      </div>
    );
  };

  // --- Operation result toast ---
  const renderOpResult = () => {
    if (!opResult) return null;
    return (
      <div className={`scm-toast${opResult.success ? " scm-toast--success" : " scm-toast--error"}`}>
        {opResult.success ? <Check size={14} /> : <AlertTriangle size={14} />}
        <span>{opResult.success ? (opResult.message || t("git.success")) : (opResult.message || t("git.failed"))}</span>
      </div>
    );
  };

  if (!status && !loading) return <div className="scm-empty">{t("git.noStatus")}</div>;

  return (
    <div className="scm-panel">
      {renderHeader()}
      {renderTabs()}
      <div className="scm-content">
        {subView === "status" && renderStatus()}
        {subView === "log" && renderLog()}
        {subView === "branches" && renderBranches()}
        {subView === "stash" && renderStash()}
        {subView === "tags" && renderTags()}
      </div>
      {diffView && renderDiff()}
      {renderMergeDialog()}
      {renderTagDialog()}
      {renderOpResult()}
    </div>
  );
}

// Simple minus icon (lucide doesn't have a standalone one this small)
function MinusIcon() {
  return <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"><path d="M5 12h14" /></svg>;
}
