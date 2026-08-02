// DataVaultPanel shows where all user data lives and provides one-click export
// and purge actions. All visible text is routed through the i18n dictionary.
import { useCallback, useState } from "react";
import { AlertTriangle, Archive, Database, Download, HardDrive, Trash2 } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";

export interface DataVaultLocation {
  label: string;
  path: string;
  kind: string; // "session" | "memory" | "config" | "credential" | "cache" | "plugin" | "workflow"
  size?: number; // bytes
  removable: boolean;
}

interface DataVaultView {
  locations: DataVaultLocation[];
  totalSize: number;
}

export function DataVaultPanel({ onClose }: { onClose: () => void }) {
  const t = useT();
  const [view, setView] = useState<DataVaultView | null>(null);
  const [exporting, setExporting] = useState(false);
  const [purging, setPurging] = useState(false);
  const [confirmPurge, setConfirmPurge] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    try {
      const v = await app.DataVault();
      setView(v);
    } catch {
      setView({ locations: [], totalSize: 0 });
    }
  }, []);

  // Load on mount
  useCallback(() => { void refresh(); }, [refresh])();

  const handleExport = useCallback(async () => {
    setExporting(true);
    setError(null);
    setSuccess(null);
    try {
      const outPath = await app.DataVaultExport();
      setSuccess(t("datavault.exportSuccess", { path: outPath }));
    } catch (e) {
      setError(String(e));
    } finally {
      setExporting(false);
    }
  }, [t]);

  const handlePurge = useCallback(async () => {
    setPurging(true);
    setError(null);
    setSuccess(null);
    try {
      await app.DataVaultPurge();
      setSuccess(t("datavault.purgeSuccess"));
      setConfirmPurge(false);
      void refresh();
    } catch (e) {
      setError(String(e));
    } finally {
      setPurging(false);
    }
  }, [t, refresh]);

  const locations = view?.locations ?? [];

  const kindIcon = (kind: string) => {
    switch (kind) {
      case "session": return <Database size={14} />;
      case "memory": return <HardDrive size={14} />;
      case "config": return <Archive size={14} />;
      case "credential": return <AlertTriangle size={14} />;
      case "cache": return <Archive size={14} />;
      default: return <Database size={14} />;
    }
  };

  const fmtSize = (bytes?: number) => {
    if (!bytes) return "-";
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
  };

  return (
    <div className="datavault-panel">
      <div className="datavault-panel__header">
        <h2>{t("datavault.title")}</h2>
        <button type="button" className="datavault-panel__close" onClick={onClose}>✕</button>
      </div>

      <p className="datavault-panel__desc">{t("datavault.description")}</p>

      {error && <div className="datavault-panel__error">{error}</div>}
      {success && <div className="datavault-panel__success">{success}</div>}

      <div className="datavault-panel__locations">
        <div className="datavault-panel__locations-header">
          <h3>{t("datavault.locations")}</h3>
          <span className="datavault-panel__total">
            {t("datavault.totalSize", { size: fmtSize(view?.totalSize) })}
          </span>
        </div>
        {locations.length === 0 ? (
          <div className="datavault-panel__empty">{t("datavault.noLocations")}</div>
        ) : (
          locations.map((loc) => (
            <div className="datavault-panel__location" key={loc.path}>
              <span className="datavault-panel__location-icon">{kindIcon(loc.kind)}</span>
              <span className="datavault-panel__location-info">
                <span className="datavault-panel__location-label">{loc.label}</span>
                <span className="datavault-panel__location-path">{loc.path}</span>
              </span>
              <span className="datavault-panel__location-size">{fmtSize(loc.size)}</span>
            </div>
          ))
        )}
      </div>

      <div className="datavault-panel__actions">
        <button
          type="button"
          className="datavault-panel__export"
          disabled={exporting}
          onClick={handleExport}
        >
          <Download size={14} />
          {exporting ? t("datavault.exporting") : t("datavault.export")}
        </button>

        {!confirmPurge ? (
          <button
            type="button"
            className="datavault-panel__purge"
            onClick={() => setConfirmPurge(true)}
          >
            <Trash2 size={14} />
            {t("datavault.purge")}
          </button>
        ) : (
          <div className="datavault-panel__confirm">
            <AlertTriangle size={14} />
            <span>{t("datavault.purgeConfirm")}</span>
            <button type="button" className="datavault-panel__purge-confirm" disabled={purging} onClick={handlePurge}>
              {purging ? t("datavault.purging") : t("datavault.purgeYes")}
            </button>
            <button type="button" className="datavault-panel__purge-cancel" onClick={() => setConfirmPurge(false)}>
              {t("datavault.purgeNo")}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
