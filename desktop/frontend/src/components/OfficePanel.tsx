import { FileText, ClipboardList, FileSignature, Table, BarChart3, BookOpen, Briefcase, Code2 } from "lucide-react";
import { useT } from "../lib/i18n";
import type { WorkspaceType } from "../lib/types";

// Office capability card definition — compact sidebar variant.
interface OfficeCard {
  key: string;         // i18n key suffix + skill name
  icon: React.ReactNode;
  color: string;       // CSS class suffix for the left accent stripe
}

const CARDS: OfficeCard[] = [
  { key: "weeklyReport",   icon: <FileText size={15} />,       color: "blue"   },
  { key: "meetingMinutes", icon: <ClipboardList size={15} />,  color: "green"  },
  { key: "contractDraft",  icon: <FileSignature size={15} />,  color: "purple" },
  { key: "sheetClean",     icon: <Table size={15} />,          color: "orange" },
  { key: "sheetAnalysis",  icon: <BarChart3 size={15} />,      color: "teal"   },
  { key: "templates",      icon: <BookOpen size={15} />,       color: "pink"   },
];

// OfficePanel renders a compact list of office-capability cards inside the
// sidebar. Each item uses the same visual language as sidebar__navitem so it
// feels native. Only visible when workspaceType === "office".
export function OfficePanel({
  onActivateSkill,
  onOpenTemplates,
}: {
  onActivateSkill: (skillName: string) => void;
  onOpenTemplates: () => void;
}) {
  const t = useT();

  const handleClick = (card: OfficeCard) => {
    if (card.key === "templates") {
      onOpenTemplates();
      return;
    }
    const skillNames: Record<string, string> = {
      weeklyReport: "weekly-report",
      meetingMinutes: "meeting-minutes",
      contractDraft: "contract-draft",
      sheetClean: "sheet-clean",
      sheetAnalysis: "sheet-analysis",
    };
    onActivateSkill(skillNames[card.key] ?? card.key);
  };

  return (
    <section className="sidebar__section sidebar__section--office">
      <div className="office-panel">
        <span className="office-panel__label">{t("officePanel.title")}</span>
        <div className="office-panel__list">
          {CARDS.map((card) => (
            <button
              key={card.key}
              className={`office-panel__item office-panel__item--${card.color}`}
              onClick={() => handleClick(card)}
            >
              <span className="office-panel__item-icon">{card.icon}</span>
              <span className="office-panel__item-text">
                {t(`officePanel.${card.key}` as any)}
              </span>
            </button>
          ))}
        </div>
      </div>
    </section>
  );
}

// WorkspaceTypeSwitch is a toggle switch rendered in the sidebar bottom nav
// area. It shows a compact row: icon + label + toggle pill.
export function WorkspaceTypeSwitch({
  value,
  onChange,
}: {
  value: WorkspaceType;
  onChange: (wt: WorkspaceType) => void;
}) {
  const t = useT();
  const isOffice = value === "office";
  return (
    <div className="wst-switch">
      <button
        className={`wst-switch__track${isOffice ? " wst-switch__track--on" : ""}`}
        onClick={() => onChange(isOffice ? "coding" : "office")}
        role="switch"
        aria-checked={isOffice}
        aria-label={t("workspaceType.office")}
        title={isOffice ? t("workspaceType.codingDesc") : t("workspaceType.officeDesc")}
      >
        <span className="wst-switch__thumb" />
      </button>
      <span className={`wst-switch__label${isOffice ? " wst-switch__label--on" : ""}`}>
        {isOffice ? <Briefcase size={13} /> : <Code2 size={13} />}
        <span>{isOffice ? t("workspaceType.office") : t("workspaceType.coding")}</span>
      </span>
    </div>
  );
}
