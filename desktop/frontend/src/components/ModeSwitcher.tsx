import { Code2, FileText, Bot } from "lucide-react";
import { useT } from "../lib/i18n";
import type { WorkspaceType } from "../lib/types";

interface ModeOption {
  value: WorkspaceType;
  icon: React.ReactNode;
  labelKey: string;
}

const MODES: ModeOption[] = [
  { value: "coding",    icon: <Code2 size={14} />,   labelKey: "workspaceType.coding" },
  { value: "office",    icon: <FileText size={14} />, labelKey: "workspaceType.office" },
  { value: "assistant", icon: <Bot size={14} />,      labelKey: "workspaceType.assistant" },
];

export function ModeSwitcher({
  value,
  onChange,
}: {
  value: WorkspaceType;
  onChange: (wt: WorkspaceType) => void;
}) {
  const t = useT();

  return (
    <div className="mode-switcher" role="tablist" aria-label={t("modeSwitcher.label")}>
      {MODES.map((mode) => {
        const active = value === mode.value;
        return (
          <button
            key={mode.value}
            type="button"
            role="tab"
            aria-selected={active}
            className={`mode-switcher__btn${active ? " mode-switcher__btn--active" : ""}`}
            onClick={() => onChange(mode.value)}
            title={t(`modeSwitcher.${mode.value}Hint` as any)}
          >
            <span className="mode-switcher__icon">{mode.icon}</span>
            <span className="mode-switcher__label">{t(mode.labelKey as any)}</span>
          </button>
        );
      })}
    </div>
  );
}
