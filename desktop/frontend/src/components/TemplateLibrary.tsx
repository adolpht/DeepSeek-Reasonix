import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { FileText, FileType2, RefreshCw, ExternalLink, FolderOpen, Wand2, Upload } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { TemplateMeta } from "../lib/types";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";
import { DocPreviewer } from "./DocPreviewer";

const KINDS: Array<TemplateMeta["kind"]> = ["docx", "xlsx", "md", "tmpl", "txt", "csv"];

// Allowed template extensions for drag-and-drop / paste upload.
const TEMPLATE_EXTENSIONS = new Set([
  ".docx", ".xlsx", ".xlsm", ".csv", ".md", ".markdown", ".tmpl", ".tpl", ".gotmpl", ".txt",
]);

function isTemplateFile(name: string): boolean {
  const dot = name.lastIndexOf(".");
  if (dot < 0) return false;
  return TEMPLATE_EXTENSIONS.has(name.slice(dot).toLowerCase());
}

function kindLabel(kind: TemplateMeta["kind"], t: ReturnType<typeof useT>): string {
  switch (kind) {
    case "docx":
      return t("templates.kindDocx");
    case "xlsx":
      return t("templates.kindXlsx");
    case "md":
      return t("templates.kindMd");
    case "tmpl":
      return t("templates.kindTmpl");
    case "txt":
      return t("templates.kindTxt");
    case "csv":
      return t("templates.kindCsv");
    default:
      return kind;
  }
}

function kindIcon(kind: TemplateMeta["kind"]) {
  if (kind === "docx" || kind === "md" || kind === "txt") return <FileText size={14} />;
  return <FileType2 size={14} />;
}

function formatSize(bytes: number, t: ReturnType<typeof useT>): string {
  if (bytes < 1024) return t("templates.sizeBytes", { n: bytes });
  if (bytes < 1024 * 1024) return t("templates.sizeKB", { n: (bytes / 1024).toFixed(1) });
  return t("templates.sizeMB", { n: (bytes / 1024 / 1024).toFixed(2) });
}

// readFileAsDataURL reads a File and resolves with its data-URL representation.
function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

// TemplateLibrary is the personal-agent template browser drawer (roadmap §5.2).
// It scans `.reasonix/templates/` via ListTemplates, shows a filterable grid,
// and lets the user open a template in the OS default app, reveal it in the
// file manager, or — for text kinds (md/tmpl/txt/csv) — apply it by inserting
// the rendered body into the composer. The "apply" action is intentionally a
// thin pass-through: rendering stays in the office plugin (mcp__office__render_template),
// so the desktop surface doesn't grow its own template engine.
export function TemplateLibrary({
  onClose,
  onApply,
}: {
  onClose: () => void;
  onApply?: (template: TemplateMeta) => void;
}) {
  const t = useT();
  const [items, setItems] = useState<TemplateMeta[]>([]);
  const [filter, setFilter] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [previewPath, setPreviewPath] = useState<string | null>(null);
  const [uploading, setUploading] = useState(false);
  const dropRef = useRef<HTMLDivElement>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setErr(null);
    try {
      const list = await app.ListTemplates("");
      setItems(list);
    } catch (e: unknown) {
      setErr(String(e ?? "list templates failed"));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const handleUpload = useCallback(async () => {
    setUploading(true);
    try {
      await app.UploadTemplate();
      await refresh();
    } catch (e: unknown) {
      // User cancelling the dialog returns an empty string / error — ignore.
      if (String(e) !== "") setErr(String(e));
    } finally {
      setUploading(false);
    }
  }, [refresh]);

  // Handle files dropped onto the template library area.
  const handleDrop = useCallback(async (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const files = Array.from(e.dataTransfer.files).filter((f) => isTemplateFile(f.name));
    if (files.length === 0) return;
    setUploading(true);
    try {
      for (const file of files) {
        const dataURL = await readFileAsDataURL(file);
        await app.UploadTemplateDataURL(file.name, dataURL);
      }
      await refresh();
    } catch (e: unknown) {
      setErr(String(e));
    } finally {
      setUploading(false);
    }
  }, [refresh]);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
  }, []);

  const filtered = useMemo(() => {
    if (!filter) return items;
    return items.filter((it) => it.kind === filter);
  }, [items, filter]);

  const counts = useMemo(() => {
    const m: Record<string, number> = {};
    for (const it of items) m[it.kind] = (m[it.kind] ?? 0) + 1;
    return m;
  }, [items]);

  return (
    <ResizableDrawer onClose={onClose} subtle>
      <header className="drawer__head">
        <div>
          <div className="drawer__title">{t("templates.title")}</div>
          <div className="drawer__summary">{t("templates.summary")}</div>
        </div>
        <div className="drawer__head-actions">
          <Tooltip label={t("templates.upload")}>
            <button className="chip chip--icon chip--primary" onClick={() => void handleUpload()} disabled={uploading} aria-label={t("templates.upload")}>
              <Upload size={14} />
            </button>
          </Tooltip>
          <Tooltip label={t("templates.refresh")}>
            <button className="chip chip--icon" onClick={() => void refresh()} disabled={loading} aria-label={t("templates.refresh")}>
              <RefreshCw size={14} className={loading ? "spin" : ""} />
            </button>
          </Tooltip>
          <Tooltip label={t("common.close")}>
            <button className="chip" onClick={onClose}>✕</button>
          </Tooltip>
        </div>
      </header>

      <div className="drawer__body" ref={dropRef} onDrop={handleDrop} onDragOver={handleDragOver}>
        <div className="tmpl-filter">
          <button
            className={`chip${!filter ? " chip--selected" : ""}`}
            onClick={() => setFilter("")}
          >
            {t("templates.filterAll")}
            <span className="chip__count">{items.length}</span>
          </button>
          {KINDS.filter((k) => (counts[k] ?? 0) > 0).map((k) => (
            <button
              key={k}
              className={`chip${filter === k ? " chip--selected" : ""}`}
              onClick={() => setFilter(k)}
            >
              {kindLabel(k, t)}
              <span className="chip__count">{counts[k]}</span>
            </button>
          ))}
        </div>

        {err && <div className="tmpl__err">{err}</div>}

        {!err && !loading && filtered.length === 0 && (
          <div className="tmpl__empty">
            <p>{t("templates.empty")}</p>
            <p className="tmpl__empty-hint">{t("templates.emptyUploadHint")}</p>
          </div>
        )}

        <ul className="tmpl-list">
          {filtered.map((tmpl) => (
            <li key={tmpl.path} className="tmpl-item">
              <div className="tmpl-item__head">
                <span className="tmpl-item__icon">{kindIcon(tmpl.kind)}</span>
                <span className="tmpl-item__name" title={tmpl.name}>{tmpl.name}</span>
                <span className="tmpl-item__kind">{kindLabel(tmpl.kind, t)}</span>
                <span className="tmpl-item__size">{formatSize(tmpl.size, t)}</span>
                <div className="tmpl-item__actions">
                  <Tooltip label={t("templates.reveal")}>
                    <button
                      className="chip chip--icon"
                      onClick={() => void app.RevealPath(tmpl.path)}
                      aria-label={t("templates.reveal")}
                    >
                      <FolderOpen size={14} />
                    </button>
                  </Tooltip>
                  <Tooltip label={t("templates.open")}>
                    <button
                      className="chip chip--icon"
                      onClick={() => void app.OpenInOSDefault(tmpl.path)}
                      aria-label={t("templates.open")}
                    >
                      <ExternalLink size={14} />
                    </button>
                  </Tooltip>
                  {(tmpl.kind === "md" || tmpl.kind === "tmpl" || tmpl.kind === "txt" || tmpl.kind === "csv") && onApply && (
                    <Tooltip label={t("templates.apply")}>
                      <button
                        className="chip chip--icon chip--primary"
                        onClick={() => onApply(tmpl)}
                        aria-label={t("templates.apply")}
                      >
                        <Wand2 size={14} />
                      </button>
                    </Tooltip>
                  )}
                </div>
              </div>
              {tmpl.description && (
                <p className="tmpl-item__desc">{tmpl.description}</p>
              )}
              <p className="tmpl-item__path" title={tmpl.path}>{tmpl.relPath || tmpl.path}</p>

              {previewPath === tmpl.path && (
                <div className="tmpl-item__preview">
                  <DocPreviewer path={tmpl.path} compact />
                </div>
              )}
              <button
                className="tmpl-item__toggle-preview"
                onClick={() => setPreviewPath((p) => (p === tmpl.path ? null : tmpl.path))}
              >
                {previewPath === tmpl.path ? t("common.collapse") : t("common.expand")}
              </button>
            </li>
          ))}
        </ul>
      </div>
    </ResizableDrawer>
  );
}
