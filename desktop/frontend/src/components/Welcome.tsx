import logoWordmark from "../assets/logo-wordmark.svg";
import { useT } from "../lib/i18n";
import type { WorkspaceType } from "../lib/types";
import { Code, FileText, Sparkles, ArrowRight } from "lucide-react";

// Welcome is the empty-state landing: brand, a one-liner, the input affordances
// (/ commands, @ files, Enter), and a few clickable example prompts that send
// immediately so a first turn is one click away.
// Supports three workspace types: coding, office, assistant — each with distinct
// theme color, icon, tagline, and example prompts.

const EXAMPLE_KEYS: Record<WorkspaceType, [string, string, string]> = {
  coding: ["welcome.ex1", "welcome.ex2", "welcome.ex3"],
  office: ["welcome.officeEx1", "welcome.officeEx2", "welcome.officeEx3"],
  assistant: ["welcome.assistantEx1", "welcome.assistantEx2", "welcome.assistantEx3"],
};

const TAGLINE_KEYS: Record<WorkspaceType, string> = {
  coding: "welcome.taglineCoding",
  office: "welcome.taglineOffice",
  assistant: "welcome.taglineAssistant",
};

const MODE_TITLE_KEYS: Record<WorkspaceType, string> = {
  coding: "workspaceType.coding",
  office: "workspaceType.office",
  assistant: "workspaceType.assistant",
};

const ICONS: Record<WorkspaceType, React.ReactNode> = {
  coding: <Code size={28} strokeWidth={1.8} />,
  office: <FileText size={28} strokeWidth={1.8} />,
  assistant: <Sparkles size={28} strokeWidth={1.8} />,
};

export function Welcome({ onPrompt, workspaceType = "coding" }: { onPrompt: (text: string) => void; workspaceType?: WorkspaceType }) {
  const t = useT();
  const examples = EXAMPLE_KEYS[workspaceType].map((k) => t(k as any));
  const tagline = t(TAGLINE_KEYS[workspaceType] as any);
  const modeTitle = t(MODE_TITLE_KEYS[workspaceType] as any);
  const icon = ICONS[workspaceType];

  return (
    <div className={`welcome welcome--${workspaceType}`}>
      {/* Decorative glow background */}
      <div className="welcome__glow" aria-hidden="true" />

      {/* Mode badge */}
      <div className={`welcome__badge welcome__badge--${workspaceType}`}>
        {icon}
      </div>

      {/* Mode title */}
      <div className={`welcome__mode-title welcome__mode-title--${workspaceType}`}>
        {modeTitle}
      </div>

      {/* Brand logo */}
      <img src={logoWordmark} className="welcome__logo" alt="Reasonix" />

      {/* Tagline */}
      <div className="welcome__tag">{tagline}</div>

      {/* Input hints */}
      <div className="welcome__hints">
        <span>
          <kbd>/</kbd> {t("welcome.hintCommands")}
        </span>
        <span>
          <kbd>@</kbd> {t("welcome.hintFiles")}
        </span>
        <span>
          <kbd>⏎</kbd> {t("welcome.hintSend")}
        </span>
      </div>

      {/* Example prompts */}
      <div className="welcome__examples">
        {examples.map((ex) => (
          <button key={ex} className="welcome__ex" onClick={() => onPrompt(ex)}>
            <span className="welcome__ex-text">{ex}</span>
            <ArrowRight size={15} className="welcome__ex-arrow" />
          </button>
        ))}
      </div>
    </div>
  );
}
