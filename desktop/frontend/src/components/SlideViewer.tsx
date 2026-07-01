import { useCallback, useEffect, useState } from "react";
import { Grid, X, ChevronLeft, ChevronRight, ExternalLink, Download } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { DocPreviewPage } from "../lib/types";

interface SlideViewerProps {
  filePath: string;
  totalPages?: number;
  tabId?: string;
}

export function SlideViewer({ filePath, totalPages, tabId }: SlideViewerProps) {
  const t = useT();
  const [pages, setPages] = useState<DocPreviewPage[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [activePage, setActivePage] = useState<number | null>(null);
  const [exporting, setExporting] = useState(false);

  // Load all page thumbnails
  useEffect(() => {
    let cancelled = false;
    setPages(null);
    setErr(null);
    setActivePage(null);

    if (!filePath.trim()) {
      setErr(t("slideViewer.noPreview"));
      return;
    }

    // First call to determine total pages
    app
      .RenderDocPreview(filePath, 1)
      .then((first) => {
        if (cancelled) return;
        if (!first.length) {
          setErr(t("slideViewer.noPreview"));
          return;
        }
        const total = totalPages ?? first[0].total ?? 1;
        if (total <= 1) {
          setPages(first);
          return;
        }
        // Fetch remaining pages
        const promises = [];
        for (let p = 2; p <= total; p++) {
          promises.push(app.RenderDocPreview(filePath, p));
        }
        Promise.all(promises)
          .then((rest) => {
            if (cancelled) return;
            const all = [first, ...rest].flat();
            setPages(all);
          })
          .catch(() => {
            if (!cancelled) {
              // Fall back to first page only if remaining fail
              setPages(first);
            }
          });
      })
      .catch((e: unknown) => {
        if (!cancelled) setErr(String(e ?? t("slideViewer.noPreview")));
      });

    return () => {
      cancelled = true;
    };
  }, [filePath, totalPages, t]);

  const openOverlay = useCallback((page: number) => {
    setActivePage(page);
  }, []);

  const closeOverlay = useCallback(() => {
    setActivePage(null);
  }, []);

  const goPrev = useCallback(() => {
    setActivePage((p) => (p !== null && p > 1 ? p - 1 : p));
  }, []);

  const goNext = useCallback(() => {
    setActivePage((p) => {
      if (p === null || !pages) return p;
      const maxPage = Math.max(...pages.map((pg) => pg.page));
      return p < maxPage ? p + 1 : p;
    });
  }, [pages]);

  // Keyboard navigation in overlay
  useEffect(() => {
    if (activePage === null) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        closeOverlay();
      } else if (e.key === "ArrowLeft") {
        goPrev();
      } else if (e.key === "ArrowRight") {
        goNext();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [activePage, closeOverlay, goPrev, goNext]);

  const handleOpenDefault = useCallback(() => {
    void app.OpenInOSDefault(filePath);
  }, [filePath]);

  const handleExport = useCallback(async () => {
    setExporting(true);
    try {
      const name = filePath.split(/[/\\]/).filter(Boolean).pop() ?? "file";
      await app.ExportToWorkspace(tabId ?? "", `exports/${name}`, "");
    } catch {
      // silently ignore export errors
    } finally {
      setExporting(false);
    }
  }, [filePath, tabId]);

  const activePageData =
    activePage !== null && pages ? pages.find((p) => p.page === activePage) : null;

  const maxPage = pages ? Math.max(...pages.map((p) => p.page)) : 0;

  return (
    <div className="slide-viewer">
      {/* Header */}
      <div className="slide-viewer__header">
        <Grid size={14} />
        <span className="slide-viewer__title">
          {t("slideViewer.title")} · {pages ? `${pages.length} ${t("slideViewer.pages")}` : ""}
        </span>
      </div>

      {/* Loading / Error / Empty */}
      {err && <div className="slide-viewer__empty">{err}</div>}
      {!err && pages === null && (
        <div className="slide-viewer__empty">{t("common.loading")}</div>
      )}
      {!err && pages && pages.length === 0 && (
        <div className="slide-viewer__empty">{t("slideViewer.noPreview")}</div>
      )}

      {/* Thumbnail Grid */}
      {!err && pages && pages.length > 0 && (
        <div className="slide-viewer__grid">
          {pages.map((pg) => (
            <button
              key={pg.page}
              className="slide-viewer__slide"
              onClick={() => openOverlay(pg.page)}
              title={t("slideViewer.pageNum", { page: pg.page })}
            >
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                className="slide-viewer__thumb"
                src={pg.url}
                alt={t("slideViewer.pageNum", { page: pg.page })}
                loading="lazy"
              />
              <span className="slide-viewer__page-num">{pg.page}</span>
            </button>
          ))}
        </div>
      )}

      {/* Bottom Action Bar */}
      {!err && pages && pages.length > 0 && (
        <div className="slide-viewer__actions">
          <button className="chip" onClick={handleOpenDefault} title={t("slideViewer.openDefault")}>
            <ExternalLink size={14} />
            <span>{t("slideViewer.openDefault")}</span>
          </button>
          <button
            className="chip"
            onClick={() => void handleExport()}
            disabled={exporting}
            title={t("slideViewer.exportToWorkspace")}
          >
            <Download size={14} />
            <span>{exporting ? t("common.loading") : t("slideViewer.exportToWorkspace")}</span>
          </button>
        </div>
      )}

      {/* Full-page Overlay */}
      {activePage !== null && activePageData && (
        <div className="slide-viewer__overlay" onClick={closeOverlay}>
          <div
            className="slide-viewer__overlay-content"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Close button */}
            <button
              className="slide-viewer__overlay-close"
              onClick={closeOverlay}
              aria-label={t("common.close")}
            >
              <X size={20} />
            </button>

            {/* Prev button */}
            {activePage > 1 && (
              <button
                className="slide-viewer__nav slide-viewer__nav--prev"
                onClick={goPrev}
                aria-label={t("slideViewer.prevPage")}
              >
                <ChevronLeft size={24} />
              </button>
            )}

            {/* Image */}
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              className="slide-viewer__full-img"
              src={activePageData.url}
              alt={t("slideViewer.pageNum", { page: activePage })}
            />

            {/* Next button */}
            {activePage < maxPage && (
              <button
                className="slide-viewer__nav slide-viewer__nav--next"
                onClick={goNext}
                aria-label={t("slideViewer.nextPage")}
              >
                <ChevronRight size={24} />
              </button>
            )}

            {/* Page indicator */}
            <div className="slide-viewer__overlay-indicator">
              {t("slideViewer.pageOf", { current: activePage, total: maxPage })}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
