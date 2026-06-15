import { useState } from "react";
import { BookOpen } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import { ResizableDrawer } from "./ResizableDrawer";
import { Tooltip } from "./Tooltip";

// RepoWikiPanel is the Repo Wiki drawer — a dedicated UI entry point that
// invokes the /analyze-project skill to generate a comprehensive documentation
// suite for the current project. It provides an optional path input and a
// launch button that submits the slash command into the active conversation.

export function RepoWikiPanel({
  onClose,
  cwd,
}: {
  onClose: () => void;
  cwd?: string;
}) {
  const t = useT();
  const [path, setPath] = useState(cwd ?? "");
  const [launching, setLaunching] = useState(false);

  const launch = async () => {
    setLaunching(true);
    try {
      await app.RunAnalyzeProject(path);
      onClose();
    } catch {
      // Submit failures surface in the transcript; just re-enable the button.
      setLaunching(false);
    }
  };

  return (
    <ResizableDrawer onClose={onClose} subtle>
      <header className="drawer__head">
        <div>
          <div className="drawer__title">{t("repoWiki.title")}</div>
          <div className="drawer__summary">{t("repoWiki.summary")}</div>
        </div>
        <Tooltip label={t("common.close")}>
          <button className="chip" onClick={onClose}>
            ✕
          </button>
        </Tooltip>
      </header>

      <div className="drawer__body">
        <section className="rw-section">
          <p className="rw-description">{t("repoWiki.description")}</p>

          <label className="rw-label" htmlFor="rw-path">
            {t("repoWiki.projectPath")}
          </label>
          <input
            id="rw-path"
            className="rw-input"
            type="text"
            placeholder={t("repoWiki.pathPlaceholder")}
            value={path}
            onChange={(e) => setPath(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !launching) void launch();
            }}
          />
          <p className="rw-hint">{t("repoWiki.pathHint")}</p>

          <button
            className="btn btn--primary rw-launch"
            disabled={launching}
            onClick={() => void launch()}
          >
            <BookOpen size={15} />
            <span>{launching ? t("repoWiki.launching") : t("repoWiki.launch")}</span>
          </button>
        </section>
      </div>
    </ResizableDrawer>
  );
}
