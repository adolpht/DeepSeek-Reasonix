import { FileText, Table, Presentation, BarChart3, Palette } from "lucide-react";
import { useT } from "../lib/i18n";

// Office capability card definition — compact sidebar variant.
interface OfficeCard {
  key: string;         // i18n key suffix + skill name
  icon: React.ReactNode;
  color: string;       // CSS class suffix for the left accent stripe
}

const CARDS: OfficeCard[] = [
  { key: "productDesign",  icon: <Palette size={15} />,        color: "indigo" },
  { key: "docWrite",       icon: <FileText size={15} />,       color: "blue"   },
  { key: "sheetCreate",    icon: <Table size={15} />,          color: "green"  },
  { key: "pptCreate",      icon: <Presentation size={15} />,   color: "purple" },
  { key: "sheetClean",     icon: <Table size={15} />,          color: "orange" },
  { key: "sheetAnalysis",  icon: <BarChart3 size={15} />,      color: "teal"   },
];

// OfficePanel renders a compact list of office-capability cards inside the
// sidebar. Each item uses the same visual language as sidebar__navitem so it
// feels native.
export function OfficePanel({
  onActivateSkill,
}: {
  onActivateSkill: (skillName: string) => void;
}) {
  const t = useT();

  const handleClick = (card: OfficeCard) => {
    const skillNames: Record<string, string> = {
      productDesign: "product-design",
      docWrite: "doc-write",
      sheetCreate: "sheet-create",
      pptCreate: "ppt-create",
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
