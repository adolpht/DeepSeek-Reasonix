import { Code2, FileSpreadsheet, Bot } from "lucide-react";
import { t } from "../lib/i18n";
import type { WorkspaceType } from "../lib/types";
import { DiffView } from "./DiffView";
import { DocPreviewer } from "./DocPreviewer";
import { SheetViewer } from "./SheetViewer";
import { SlideViewer } from "./SlideViewer";

interface PreviewPanelProps {
  workspaceType: WorkspaceType;
  activeFilePath?: string;
  diffOriginal?: string;
  diffModified?: string;
  tabId?: string;
}

function fileExt(path: string): string {
  return path.split(/[/\\]/).pop()?.split(".").pop()?.toLowerCase() ?? "";
}

export function PreviewPanel({ workspaceType, activeFilePath, diffOriginal, diffModified, tabId }: PreviewPanelProps) {
  // ── Coding mode: diff preview ────────────────────────────────────────────
  if (workspaceType === "coding") {
    if (diffOriginal !== undefined && diffModified !== undefined) {
      return (
        <div className="preview-panel">
          <div className="preview-panel__header">
            <Code2 size={16} />
            <span>{t("rightDock.previewCoding")}</span>
          </div>
          <div className="preview-panel__body">
            <DiffView original={diffOriginal} modified={diffModified} maxHeight={400} />
          </div>
        </div>
      );
    }
    return (
      <div className="preview-panel">
        <div className="preview-panel__placeholder">
          <Code2 size={28} />
          <h3 className="preview-panel__title">{t("rightDock.previewCoding")}</h3>
          <p className="preview-panel__desc">{t("rightDock.previewCodingDesc")}</p>
        </div>
      </div>
    );
  }

  // ── Office mode: document preview ────────────────────────────────────────
  if (workspaceType === "office") {
    if (activeFilePath) {
      const ext = fileExt(activeFilePath);
      const isSheet = ["xlsx", "xls", "csv"].includes(ext);
      const isSlide = ["pptx", "ppt"].includes(ext);

      return (
        <div className="preview-panel">
          <div className="preview-panel__header">
            <FileSpreadsheet size={16} />
            <span>{t("rightDock.previewOffice")}</span>
          </div>
          <div className="preview-panel__body">
            {isSheet ? (
              <SheetViewer path={activeFilePath} tabId={tabId} compact />
            ) : isSlide ? (
              <SlideViewer filePath={activeFilePath} tabId={tabId} />
            ) : (
              <DocPreviewer path={activeFilePath} tabId={tabId} compact />
            )}
          </div>
        </div>
      );
    }
    return (
      <div className="preview-panel">
        <div className="preview-panel__placeholder">
          <FileSpreadsheet size={28} />
          <h3 className="preview-panel__title">{t("rightDock.previewOffice")}</h3>
          <p className="preview-panel__desc">{t("rightDock.previewOfficeDesc")}</p>
        </div>
      </div>
    );
  }

  // ── Assistant mode: summary preview ──────────────────────────────────────
  return (
    <div className="preview-panel">
      <div className="preview-panel__placeholder">
        <Bot size={28} />
        <h3 className="preview-panel__title">{t("rightDock.previewAssistant")}</h3>
        <p className="preview-panel__desc">{t("rightDock.previewAssistantDesc")}</p>
      </div>
    </div>
  );
}
