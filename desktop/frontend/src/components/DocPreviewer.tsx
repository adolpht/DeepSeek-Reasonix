import { useEffect, useState } from "react";
import { ExternalLink, FolderOpen, Download, Copy, Check } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { DocPreviewPage } from "../lib/types";
import { Tooltip } from "./Tooltip";

// DocPreviewer renders one agent-produced file inline. Images are shown in an
// <img>; everything else (docx/pdf/xlsx) is surfaced as a download chip plus an
// "Open in default app" affordance — full page-by-page rendering waits on an
// external renderer (LibreOffice/pandoc) per roadmap §5.4.
//
// `onExported?` is invoked after a successful ExportToWorkspace call so the
// parent (e.g. ToolCard) can surface the saved path in the transcript.
export function DocPreviewer({
  path,
  tabId,
  onExported,
  compact = false,
}: {
  path: string;
  tabId?: string;
  onExported?: (absPath: string) => void;
  compact?: boolean;
}) {
  const t = useT();
  const [pages, setPages] = useState<DocPreviewPage[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [showExport, setShowExport] = useState(false);
  const [exportPath, setExportPath] = useState("exports/" + (path.split(/[/\\]/).pop() ?? "file.docx"));
  const [exporting, setExporting] = useState(false);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setPages(null);
    setErr(null);
    if (!path.trim()) {
      setErr(t("docPreview.unsupported"));
      return;
    }
    app.RenderDocPreview(path, 1)
      .then((p) => {
        if (!cancelled) setPages(p);
      })
      .catch((e: unknown) => {
        if (!cancelled) setErr(String(e ?? t("docPreview.unsupported")));
      });
    return () => {
      cancelled = true;
    };
  }, [path, t]);

  const name = path.split(/[/\\]/).filter(Boolean).pop() ?? path;
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  const isImage = ["png", "jpg", "jpeg", "gif", "svg", "webp"].includes(ext);

  const copyPath = async () => {
    try {
      await navigator.clipboard.writeText(path);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      // clipboard may be blocked in the webview; silently ignore
    }
  };

  const doExport = async () => {
    setExporting(true);
    try {
      const abs = await app.ExportToWorkspace(tabId ?? "", exportPath, "");
      onExported?.(abs);
      setShowExport(false);
    } catch (e: unknown) {
      setErr(String(e ?? t("docPreview.unsupported")));
    } finally {
      setExporting(false);
    }
  };

  return (
    <div className={`docpreview${compact ? " docpreview--compact" : ""}`}>
      <div className="docpreview__head">
        <span className="docpreview__name" title={path}>{name}</span>
        <div className="docpreview__actions">
          <Tooltip label={copied ? "✓" : t("common.copy")}>
            <button className="chip chip--icon" onClick={copyPath} aria-label={t("common.copy")}>
              {copied ? <Check size={14} /> : <Copy size={14} />}
            </button>
          </Tooltip>
          <Tooltip label={t("docPreview.reveal")}>
            <button
              className="chip chip--icon"
              onClick={() => void app.RevealPath(path)}
              aria-label={t("docPreview.reveal")}
            >
              <FolderOpen size={14} />
            </button>
          </Tooltip>
          <Tooltip label={t("docPreview.open")}>
            <button
              className="chip chip--icon"
              onClick={() => void app.OpenInOSDefault(path)}
              aria-label={t("docPreview.open")}
            >
              <ExternalLink size={14} />
            </button>
          </Tooltip>
          {!compact && (
            <button className="chip" onClick={() => setShowExport((v) => !v)}>
              <Download size={14} />
              <span>{t("docPreview.exportToWorkspace")}</span>
            </button>
          )}
        </div>
      </div>

      {showExport && (
        <div className="docpreview__export">
          <label className="docpreview__label" htmlFor="docpreview-export-path">
            {t("docPreview.exportPath")}
          </label>
          <input
            id="docpreview-export-path"
            className="docpreview__input"
            value={exportPath}
            onChange={(e) => setExportPath(e.target.value)}
            placeholder="reports/周报.docx"
          />
          <p className="docpreview__hint">{t("docPreview.exportPathHint")}</p>
          <div className="docpreview__export-actions">
            <button className="chip" onClick={() => setShowExport(false)} disabled={exporting}>
              {t("common.cancel")}
            </button>
            <button
              className="chip chip--primary"
              onClick={() => void doExport()}
              disabled={exporting || !exportPath.trim()}
            >
              {exporting ? t("common.loading") : t("docPreview.exportConfirm")}
            </button>
          </div>
        </div>
      )}

      <div className="docpreview__body">
        {err && <div className="docpreview__err">{err}</div>}
        {!err && pages === null && <div className="docpreview__loading">{t("common.loading")}</div>}
        {!err && pages && pages.length === 0 && (
          <div className="docpreview__empty">{t("docPreview.unsupported")}</div>
        )}
        {!err && pages && pages.length > 0 && isImage && (
          // eslint-disable-next-line @next/next/no-img-element
          <img className="docpreview__img" src={pages[0].url} alt={name} />
        )}
        {!err && pages && pages.length > 0 && !isImage && (
          <a className="docpreview__download" href={pages[0].url} download={name}>
            <Download size={16} />
            <span>{name}</span>
          </a>
        )}
      </div>
    </div>
  );
}
