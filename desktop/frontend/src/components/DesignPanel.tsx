import { useCallback, useRef, useState } from "react";
import type { DragEvent as ReactDragEvent } from "react";
import { AlertCircle, Code2, FileImage, Loader2, RefreshCw, Upload, X } from "lucide-react";
import { useT } from "../lib/i18n";
import { CodeViewer } from "./CodeViewer";
import { DiffView } from "./DiffView";

// DesignPanel is the design-to-code workbench: drop a mockup (or paste a Figma
// URL) on the left, pick a target framework, hit generate, and the produced
// source lands on the right. A second pass can be diffed against the first via
// the "compare" toggle, which reuses DiffView so the UX matches the editor's
// inline-diff feel. MCP tool calls (design_upload / design_analyze /
// design_to_code / design_compare) are orchestrated by the agent; this panel
// composes the request and renders the result, so the parent wires `onGenerate`
// to feed the panel back the produced code.

export type DesignFramework = "html" | "vue" | "react";
export type DesignStyle = "css" | "tailwind";

export interface DesignConfig {
  imageDataUrl: string | null;
  figmaUrl: string;
  framework: DesignFramework;
  style: DesignStyle;
  componentName: string;
}

export interface DesignPanelProps {
  /** Called when the user clicks "Generate". The parent drives the MCP tools
   *  (design_upload → design_analyze → design_to_code) and feeds results back
   *  via the `generatedCode` / `matchScore` / `status` props below. */
  onGenerate?: (config: DesignConfig) => void;
  /** Controlled result state — when the parent updates these, the panel
   *  re-renders. Keeping them as props (not internal state) lets the panel
   *  reflect agent-driven updates without a re-mount. */
  status?: "idle" | "analyzing" | "generating" | "done" | "error";
  generatedCode?: string;
  matchScore?: number;
  errorMessage?: string;
  /** Optional close handler — when provided, a close button is rendered in
   * the header. Used when the panel is hosted in a modal overlay. */
  onClose?: () => void;
}

const FRAMEWORKS: DesignFramework[] = ["html", "vue", "react"];
const STYLES: DesignStyle[] = ["css", "tailwind"];

export function DesignPanel({
  onGenerate,
  status = "idle",
  generatedCode = "",
  matchScore,
  errorMessage,
  onClose,
}: DesignPanelProps) {
  const t = useT();
  const [imageDataUrl, setImageDataUrl] = useState<string | null>(null);
  const [figmaUrl, setFigmaUrl] = useState("");
  const [framework, setFramework] = useState<DesignFramework>("html");
  const [style, setStyle] = useState<DesignStyle>("css");
  const [componentName, setComponentName] = useState("DesignComponent");
  const [prevCode, setPrevCode] = useState("");
  const [compareMode, setCompareMode] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const busy = status === "analyzing" || status === "generating";

  const readFileAsDataUrl = (file: File): Promise<string> =>
    new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result as string);
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(file);
    });

  const handleFile = useCallback(
    async (file: File | undefined) => {
      if (!file) return;
      if (!file.type.startsWith("image/")) return;
      const url = await readFileAsDataUrl(file);
      setImageDataUrl(url);
      setFigmaUrl(""); // image and figma are mutually exclusive sources
    },
    [],
  );

  const onDrop = useCallback(
    (e: ReactDragEvent) => {
      e.preventDefault();
      setDragOver(false);
      void handleFile(e.dataTransfer.files?.[0]);
    },
    [handleFile],
  );

  const config: DesignConfig = {
    imageDataUrl,
    figmaUrl,
    framework,
    style,
    componentName,
  };

  const handleGenerate = () => {
    // Snapshot the current code so the next run can diff against it.
    if (generatedCode) setPrevCode(generatedCode);
    onGenerate?.(config);
  };

  const handleClear = () => {
    setImageDataUrl(null);
    setFigmaUrl("");
    setCompareMode(false);
    setPrevCode("");
  };

  const canGenerate =
    !busy && (Boolean(imageDataUrl) || figmaUrl.trim() !== "");

  const codeLanguage =
    framework === "react" ? "tsx" : framework === "vue" ? "vue" : "html";

  const statusLabel =
    status === "analyzing"
      ? t("designPanel.analyzing")
      : status === "generating"
        ? t("designPanel.generating")
        : null;

  return (
    <div className="design-panel">
      <header className="design-panel__head">
        <div className="design-panel__head-icon">
          <Code2 size={18} />
        </div>
        <div className="design-panel__head-text">
          <h2 className="design-panel__title">{t("designPanel.title")}</h2>
          <p className="design-panel__subtitle">{t("designPanel.subtitle")}</p>
        </div>
        {onClose && (
          <button
            className="design-panel__close"
            onClick={onClose}
            aria-label={t("common.close")}
            title={t("common.close")}
          >
            <X size={16} />
          </button>
        )}
      </header>

      <div className="design-panel__body">
        {/* ── Left: design source ── */}
        <section className="design-panel__col design-panel__col--source">
          <div className="design-panel__col-head">
            <FileImage size={14} />
            <span>{t("designPanel.preview")}</span>
          </div>

          {imageDataUrl ? (
            <div className="design-panel__preview">
              <img src={imageDataUrl} alt={t("designPanel.preview")} />
              <button
                className="design-panel__preview-clear"
                onClick={() => setImageDataUrl(null)}
                title={t("designPanel.clear")}
              >
                <X size={14} />
              </button>
            </div>
          ) : (
            <div
              className={`design-panel__drop${dragOver ? " design-panel__drop--over" : ""}`}
              onDragOver={(e) => {
                e.preventDefault();
                setDragOver(true);
              }}
              onDragLeave={() => setDragOver(false)}
              onDrop={onDrop}
              onClick={() => fileInputRef.current?.click()}
              role="button"
              tabIndex={0}
            >
              <Upload size={22} />
              <span>{t("designPanel.dropHint")}</span>
            </div>
          )}
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*"
            className="design-panel__file-input"
            onChange={(e) => void handleFile(e.target.files?.[0])}
          />

          <div className="design-panel__figma">
            <label className="design-panel__label">
              {t("designPanel.figmaUrl")}
            </label>
            <input
              type="url"
              className="design-panel__input"
              value={figmaUrl}
              placeholder={t("designPanel.figmaUrlPlaceholder")}
              onChange={(e) => {
                setFigmaUrl(e.target.value);
                if (e.target.value) setImageDataUrl(null);
              }}
            />
          </div>
        </section>

        {/* ── Right: generated code ── */}
        <section className="design-panel__col design-panel__col--code">
          <div className="design-panel__col-head">
            <Code2 size={14} />
            <span>{t("designPanel.code")}</span>
            {generatedCode && prevCode && (
              <button
                className="design-panel__toggle"
                onClick={() => setCompareMode((v) => !v)}
                data-active={compareMode}
              >
                <RefreshCw size={11} />
                {t("designPanel.compare")}
              </button>
            )}
          </div>

          {status === "error" && errorMessage ? (
            <div className="design-panel__error">
              <AlertCircle size={14} />
              <span>{errorMessage}</span>
            </div>
          ) : busy ? (
            <div className="design-panel__status">
              <Loader2 size={16} className="design-panel__spinner" />
              <span>{statusLabel}</span>
            </div>
          ) : generatedCode ? (
            compareMode && prevCode ? (
              <DiffView
                original={prevCode}
                modified={generatedCode}
                language={codeLanguage}
                maxHeight={420}
              />
            ) : (
              <CodeViewer
                value={generatedCode}
                language={codeLanguage}
                readOnly
                maxHeight={420}
              />
            )
          ) : (
            <div className="design-panel__empty">{t("designPanel.empty")}</div>
          )}

          {typeof matchScore === "number" && status === "done" && (
            <div className="design-panel__score">
              <span className="design-panel__score-label">
                {t("designPanel.matchScore")}
              </span>
              <div className="design-panel__score-bar">
                <div
                  className="design-panel__score-fill"
                  style={{ width: `${matchScore}%` }}
                />
              </div>
              <span className="design-panel__score-value">{matchScore}%</span>
            </div>
          )}
        </section>
      </div>

      {/* ── Controls ── */}
      <footer className="design-panel__controls">
        <div className="design-panel__field">
          <label className="design-panel__label">
            {t("designPanel.framework")}
          </label>
          <div className="design-panel__segmented">
            {FRAMEWORKS.map((f) => (
              <button
                key={f}
                className="design-panel__seg"
                data-active={framework === f}
                onClick={() => setFramework(f)}
              >
                {t(`designPanel.framework${cap(f)}` as any)}
              </button>
            ))}
          </div>
        </div>

        <div className="design-panel__field">
          <label className="design-panel__label">
            {t("designPanel.style")}
          </label>
          <div className="design-panel__segmented">
            {STYLES.map((s) => (
              <button
                key={s}
                className="design-panel__seg"
                data-active={style === s}
                onClick={() => setStyle(s)}
              >
                {t(`designPanel.style${cap(s)}` as any)}
              </button>
            ))}
          </div>
        </div>

        {framework !== "html" && (
          <div className="design-panel__field">
            <label className="design-panel__label">
              {t("designPanel.componentName")}
            </label>
            <input
              type="text"
              className="design-panel__input"
              value={componentName}
              placeholder={t("designPanel.componentNamePlaceholder")}
              onChange={(e) => setComponentName(e.target.value)}
            />
          </div>
        )}

        <div className="design-panel__actions">
          <button
            className="design-panel__btn design-panel__btn--ghost"
            onClick={handleClear}
            disabled={busy}
          >
            {t("designPanel.clear")}
          </button>
          <button
            className="design-panel__btn design-panel__btn--primary"
            onClick={handleGenerate}
            disabled={!canGenerate}
          >
            {busy ? (
              <>
                <Loader2 size={14} className="design-panel__spinner" />
                {statusLabel}
              </>
            ) : (
              t("designPanel.generate")
            )}
          </button>
        </div>
      </footer>
    </div>
  );
}

function cap(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
