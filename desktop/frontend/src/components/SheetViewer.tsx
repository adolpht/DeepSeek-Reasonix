import { useCallback, useEffect, useMemo, useState } from "react";
import { ExternalLink, Download, ArrowUpDown, Filter, ChevronLeft, ChevronRight } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { DocPreviewPage } from "../lib/types";
import { Tooltip } from "./Tooltip";

const ROWS_PER_PAGE = 20;

export function SheetViewer({
  path,
  tabId,
  compact = false,
}: {
  path: string;
  tabId?: string;
  compact?: boolean;
}) {
  const t = useT();
  const [pages, setPages] = useState<DocPreviewPage[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [sortCol, setSortCol] = useState<number | null>(null);
  const [sortAsc, setSortAsc] = useState(true);
  const [filterText, setFilterText] = useState("");
  const [page, setPage] = useState(0);
  const [showExport, setShowExport] = useState(false);
  const [exportPath, setExportPath] = useState("exports/" + (path.split(/[/\\]/).pop() ?? "file.xlsx"));
  const [exporting, setExporting] = useState(false);

  const name = path.split(/[/\\]/).filter(Boolean).pop() ?? path;

  useEffect(() => {
    let cancelled = false;
    setPages(null);
    setErr(null);
    setPage(0);
    setSortCol(null);
    setSortAsc(true);
    setFilterText("");
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

  // Parse table data from preview pages — the backend may return structured
  // JSON in the url field for xlsx files, or a rendered image URL as fallback.
  const tableData = useMemo<{ headers: string[]; rows: string[][] } | null>(() => {
    if (!pages || pages.length === 0) return null;
    for (const p of pages) {
      if (p.url) {
        try {
          const data = JSON.parse(p.url);
          if (data.headers && data.rows) return data as { headers: string[]; rows: string[][] };
        } catch {
          // Not JSON — it's a rendered image URL, fall through
        }
      }
    }
    return null;
  }, [pages]);

  // Sorted, filtered, paginated rows
  const displayRows = useMemo(() => {
    if (!tableData) return [];
    let rows = tableData.rows;
    if (filterText.trim()) {
      const lower = filterText.toLowerCase();
      rows = rows.filter((r) => r.some((c) => c.toLowerCase().includes(lower)));
    }
    if (sortCol !== null) {
      rows = [...rows].sort((a, b) => {
        const va = a[sortCol] ?? "";
        const vb = b[sortCol] ?? "";
        const cmp = va.localeCompare(vb, undefined, { numeric: true });
        return sortAsc ? cmp : -cmp;
      });
    }
    return rows;
  }, [tableData, sortCol, sortAsc, filterText]);

  const totalPages = Math.max(1, Math.ceil(displayRows.length / ROWS_PER_PAGE));
  const pageRows = displayRows.slice(page * ROWS_PER_PAGE, (page + 1) * ROWS_PER_PAGE);

  const handleSort = useCallback((col: number) => {
    if (sortCol === col) {
      setSortAsc((v) => !v);
    } else {
      setSortCol(col);
      setSortAsc(true);
    }
  }, [sortCol]);

  const handleOpen = useCallback(async () => {
    try {
      await app.OpenInOSDefault(path);
    } catch {
      // ignore
    }
  }, [path]);

  const handleExport = useCallback(async () => {
    setExporting(true);
    try {
      await app.ExportToWorkspace(tabId ?? "", exportPath, "");
      setShowExport(false);
    } catch (e: unknown) {
      setErr(String(e ?? t("docPreview.unsupported")));
    } finally {
      setExporting(false);
    }
  }, [path, tabId, exportPath, t]);

  if (err) return <div className="sheet-viewer sheet-viewer--error">{err}</div>;
  if (!pages && !err) return <div className="sheet-viewer sheet-viewer--loading">{t("common.loading")}</div>;

  return (
    <div className={`sheet-viewer${compact ? " sheet-viewer--compact" : ""}`}>
      {/* Structured table data → render as interactive table */}
      {tableData && (
        <>
          <div className="sheet-viewer__toolbar">
            <div className="sheet-viewer__filter">
              <Filter size={14} />
              <input
                className="sheet-viewer__filter-input"
                value={filterText}
                onChange={(e) => {
                  setFilterText(e.target.value);
                  setPage(0);
                }}
                placeholder={t("richToolCard.filterPlaceholder")}
              />
            </div>
            <span className="sheet-viewer__name" title={path}>{name}</span>
          </div>
          <div className="sheet-viewer__table-scroll">
            <table className="sheet-viewer__table">
              <thead>
                <tr>
                  {tableData.headers.map((h, i) => (
                    <th key={i} onClick={() => handleSort(i)}>
                      <span>{h}</span>
                      <ArrowUpDown
                        size={12}
                        className={`sheet-viewer__sort-icon${sortCol === i ? " sheet-viewer__sort-icon--active" : ""}`}
                      />
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {pageRows.map((row, ri) => (
                  <tr key={ri}>
                    {row.map((cell, ci) => (
                      <td key={ci}>{cell}</td>
                    ))}
                  </tr>
                ))}
                {pageRows.length === 0 && (
                  <tr>
                    <td colSpan={tableData.headers.length} className="sheet-viewer__empty">
                      {t("richToolCard.filterPlaceholder")}
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {totalPages > 1 && (
            <div className="sheet-viewer__pager">
              <button
                className="sheet-viewer__page-btn"
                onClick={() => setPage((p) => Math.max(0, p - 1))}
                disabled={page <= 0}
                aria-label={t("docPreview.previousPage")}
              >
                <ChevronLeft size={14} />
              </button>
              <span className="sheet-viewer__page-indicator">
                {t("docPreview.pageOf", { page: page + 1, total: totalPages })}
              </span>
              <button
                className="sheet-viewer__page-btn"
                onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                disabled={page >= totalPages - 1}
                aria-label={t("docPreview.nextPage")}
              >
                <ChevronRight size={14} />
              </button>
            </div>
          )}
        </>
      )}

      {/* Fallback: no structured data → render as image preview */}
      {!tableData && pages && pages.length > 0 && (
        <div className="sheet-viewer__preview">
          {pages.map((p, i) =>
            p.url ? (
              // eslint-disable-next-line @next/next/no-img-element
              <img key={i} className="sheet-viewer__preview-img" src={p.url} alt={`${name} page ${p.page}`} />
            ) : null,
          )}
        </div>
      )}

      {/* Export dialog */}
      {showExport && (
        <div className="sheet-viewer__export">
          <label className="sheet-viewer__label" htmlFor="sheet-viewer-export-path">
            {t("docPreview.exportPath")}
          </label>
          <input
            id="sheet-viewer-export-path"
            className="sheet-viewer__input"
            value={exportPath}
            onChange={(e) => setExportPath(e.target.value)}
            placeholder="reports/数据表.xlsx"
          />
          <p className="sheet-viewer__hint">{t("docPreview.exportPathHint")}</p>
          <div className="sheet-viewer__export-actions">
            <button className="chip" onClick={() => setShowExport(false)} disabled={exporting}>
              {t("common.cancel")}
            </button>
            <button
              className="chip chip--primary"
              onClick={() => void handleExport()}
              disabled={exporting || !exportPath.trim()}
            >
              {exporting ? t("common.loading") : t("docPreview.exportConfirm")}
            </button>
          </div>
        </div>
      )}

      {/* Action buttons */}
      <div className="sheet-viewer__actions">
        <Tooltip label={t("docPreview.open")}>
          <button className="chip" onClick={() => void handleOpen()}>
            <ExternalLink size={14} />
            <span>{t("richToolCard.open")}</span>
          </button>
        </Tooltip>
        <Tooltip label={t("docPreview.exportToWorkspace")}>
          <button className="chip" onClick={() => setShowExport((v) => !v)}>
            <Download size={14} />
            <span>{t("richToolCard.export")}</span>
          </button>
        </Tooltip>
      </div>
    </div>
  );
}
