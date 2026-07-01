import { useState, useMemo } from "react";
import { FileText, Table, BarChart3, Code2, Search, ExternalLink, Download, Pencil, ArrowUpDown, Filter, Check } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { CodeViewer } from "./CodeViewer";
import { DiffView } from "./DiffView";

// ── Types ──────────────────────────────────────────────────────────────────

export type RichToolCardKind = "document" | "spreadsheet" | "chart" | "code" | "search_result";

export interface RichToolCardProps {
  kind: RichToolCardKind;
  // common
  title?: string;
  filePath?: string;
  // document / spreadsheet / chart
  previewUrl?: string;
  // spreadsheet
  tableData?: { headers: string[]; rows: string[][] };
  // code
  codeContent?: string;
  codeLanguage?: string;
  diffContent?: string;
  diffOriginal?: string;
  diffModified?: string;
  // search result
  searchResults?: { title: string; snippet: string; url: string }[];
  // callbacks
  onOpen?: () => void;
  onExport?: () => void;
  onApplyChanges?: () => void;
}

// ── Kind → icon mapping ────────────────────────────────────────────────────

const KIND_ICON: Record<RichToolCardKind, typeof FileText> = {
  document: FileText,
  spreadsheet: Table,
  chart: BarChart3,
  code: Code2,
  search_result: Search,
};

// ── Sub-renderers ──────────────────────────────────────────────────────────

function DocumentBody({ previewUrl, filePath, t }: { previewUrl?: string; filePath?: string; t: ReturnType<typeof useT> }) {
  const [opened, setOpened] = useState(false);
  const handleOpen = async () => {
    if (filePath) {
      try { await app.OpenInOSDefault(filePath); } catch { /* ignore */ }
    }
    setOpened(true);
  };
  return (
    <div className="rich-tool-card__preview">
      {previewUrl && (
        // eslint-disable-next-line @next/next/no-img-element
        <img className="rich-tool-card__thumb" src={previewUrl} alt={filePath} />
      )}
      {!previewUrl && filePath && (
        <div className="rich-tool-card__file-icon">
          <FileText size={32} />
        </div>
      )}
      {filePath && <span className="rich-tool-card__filename">{filePath.split(/[/\\]/).pop()}</span>}
      <div className="rich-tool-card__actions">
        <button className="chip" onClick={handleOpen} disabled={opened}>
          <ExternalLink size={14} />
          <span>{opened ? t("richToolCard.opened") : t("richToolCard.open")}</span>
        </button>
        <button className="chip" onClick={() => filePath && void app.OpenInOSDefault(filePath)}>
          <Download size={14} />
          <span>{t("richToolCard.export")}</span>
        </button>
        <button className="chip" onClick={handleOpen}>
          <Pencil size={14} />
          <span>{t("richToolCard.edit")}</span>
        </button>
      </div>
    </div>
  );
}

function SpreadsheetBody({ tableData, t }: { tableData?: { headers: string[]; rows: string[][] }; t: ReturnType<typeof useT> }) {
  const [sortCol, setSortCol] = useState<number | null>(null);
  const [sortAsc, setSortAsc] = useState(true);
  const [filterOpen, setFilterOpen] = useState(false);
  const [filterText, setFilterText] = useState("");

  const displayRows = useMemo(() => {
    if (!tableData) return [];
    let rows = tableData.rows.slice(0, 10);
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

  const handleSort = (col: number) => {
    if (sortCol === col) {
      setSortAsc((v) => !v);
    } else {
      setSortCol(col);
      setSortAsc(true);
    }
  };

  if (!tableData) return null;
  return (
    <div className="rich-tool-card__table-wrap">
      <div className="rich-tool-card__table-toolbar">
        <button className="chip chip--icon" onClick={() => setFilterOpen((v) => !v)} title={t("richToolCard.filter")}>
          <Filter size={14} />
        </button>
      </div>
      {filterOpen && (
        <input
          className="rich-tool-card__filter-input"
          value={filterText}
          onChange={(e) => setFilterText(e.target.value)}
          placeholder={t("richToolCard.filterPlaceholder")}
        />
      )}
      <div className="rich-tool-card__table-scroll">
        <table className="rich-tool-card__table">
          <thead>
            <tr>
              {tableData.headers.map((h, i) => (
                <th key={i} onClick={() => handleSort(i)}>
                  <span>{h}</span>
                  <ArrowUpDown size={12} className={`rich-tool-card__sort-icon${sortCol === i ? " rich-tool-card__sort-icon--active" : ""}`} />
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {displayRows.map((row, ri) => (
              <tr key={ri}>
                {row.map((cell, ci) => (
                  <td key={ci}>{cell}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="rich-tool-card__actions">
        <button className="chip" onClick={() => { /* open externally */ }}>
          <ExternalLink size={14} />
          <span>{t("richToolCard.open")}</span>
        </button>
        <button className="chip" onClick={() => { /* export */ }}>
          <Download size={14} />
          <span>{t("richToolCard.export")}</span>
        </button>
      </div>
    </div>
  );
}

function ChartBody({ previewUrl, t }: { previewUrl?: string; t: ReturnType<typeof useT> }) {
  const [saved, setSaved] = useState(false);
  const handleSave = () => {
    if (!previewUrl) return;
    const a = document.createElement("a");
    a.href = previewUrl;
    a.download = "chart.png";
    a.click();
    setSaved(true);
    setTimeout(() => setSaved(false), 1200);
  };
  return (
    <div className="rich-tool-card__preview">
      {previewUrl && (
        // eslint-disable-next-line @next/next/no-img-element
        <img className="rich-tool-card__chart-img" src={previewUrl} alt="chart" />
      )}
      <div className="rich-tool-card__actions">
        <button className="chip" onClick={handleSave} disabled={saved}>
          <Download size={14} />
          <span>{saved ? t("richToolCard.saved") : t("richToolCard.saveAsPng")}</span>
        </button>
      </div>
    </div>
  );
}

function CodeBody({ codeContent, codeLanguage, diffOriginal, diffModified, onApplyChanges, t }: {
  codeContent?: string;
  codeLanguage?: string;
  diffOriginal?: string;
  diffModified?: string;
  onApplyChanges?: () => void;
  t: ReturnType<typeof useT>;
}) {
  const isDiff = diffOriginal !== undefined && diffModified !== undefined;
  return (
    <div className="rich-tool-card__code-wrap">
      {isDiff ? (
        <DiffView original={diffOriginal!} modified={diffModified!} language={codeLanguage} maxHeight={300} />
      ) : codeContent ? (
        <CodeViewer value={codeContent} language={codeLanguage} maxHeight={300} />
      ) : null}
      {onApplyChanges && (
        <div className="rich-tool-card__actions">
          <button className="chip" onClick={onApplyChanges}>
            <Check size={14} />
            <span>{t("richToolCard.applyChanges")}</span>
          </button>
        </div>
      )}
    </div>
  );
}

function SearchResultBody({ results }: { results?: { title: string; snippet: string; url: string }[] }) {
  if (!results || results.length === 0) return null;
  return (
    <ul className="rich-tool-card__search-list">
      {results.map((r, i) => (
        <li key={i} className="rich-tool-card__search-item">
          <a className="rich-tool-card__search-title" href={r.url} target="_blank" rel="noopener noreferrer">
            {r.title}
          </a>
          <p className="rich-tool-card__search-snippet">{r.snippet}</p>
          <span className="rich-tool-card__search-url">{r.url}</span>
        </li>
      ))}
    </ul>
  );
}

// ── Main component ─────────────────────────────────────────────────────────

export function RichToolCard(props: RichToolCardProps) {
  const {
    kind,
    title,
    filePath,
    previewUrl,
    tableData,
    codeContent,
    codeLanguage,
    diffOriginal,
    diffModified,
    searchResults,
    onOpen,
    onExport,
    onApplyChanges,
  } = props;

  const t = useT();
  const Icon = KIND_ICON[kind];
  const kindLabel: Record<RichToolCardKind, string> = {
    document: t("richToolCard.kindDocument"),
    spreadsheet: t("richToolCard.kindSpreadsheet"),
    chart: t("richToolCard.kindChart"),
    code: t("richToolCard.kindCode"),
    search_result: t("richToolCard.kindSearchResult"),
  };

  return (
    <div className={`rich-tool-card rich-tool-card--${kind}`}>
      {/* header */}
      <div className="rich-tool-card__header">
        <span className="rich-tool-card__header-icon">
          <Icon size={14} />
        </span>
        <span className="rich-tool-card__header-kind">{kindLabel[kind]}</span>
        {title && <span className="rich-tool-card__header-title">{title}</span>}
      </div>

      {/* body */}
      <div className="rich-tool-card__body">
        {kind === "document" && (
          <DocumentBody previewUrl={previewUrl} filePath={filePath} t={t} />
        )}
        {kind === "spreadsheet" && (
          <SpreadsheetBody tableData={tableData} t={t} />
        )}
        {kind === "chart" && (
          <ChartBody previewUrl={previewUrl} t={t} />
        )}
        {kind === "code" && (
          <CodeBody
            codeContent={codeContent}
            codeLanguage={codeLanguage}
            diffOriginal={diffOriginal}
            diffModified={diffModified}
            onApplyChanges={onApplyChanges}
            t={t}
          />
        )}
        {kind === "search_result" && (
          <SearchResultBody results={searchResults} />
        )}
      </div>

      {/* footer (shared actions for document / spreadsheet / chart) */}
      {(kind === "document" || kind === "spreadsheet" || kind === "chart") && (onOpen || onExport) && (
        <div className="rich-tool-card__footer">
          {onOpen && (
            <button className="chip" onClick={onOpen}>
              <ExternalLink size={14} />
              <span>{t("richToolCard.open")}</span>
            </button>
          )}
          {onExport && (
            <button className="chip" onClick={onExport}>
              <Download size={14} />
              <span>{t("richToolCard.export")}</span>
            </button>
          )}
        </div>
      )}
      {kind === "code" && onApplyChanges && (
        <div className="rich-tool-card__footer">
          <button className="chip" onClick={onApplyChanges}>
            <Check size={14} />
            <span>{t("richToolCard.applyChanges")}</span>
          </button>
        </div>
      )}
    </div>
  );
}
