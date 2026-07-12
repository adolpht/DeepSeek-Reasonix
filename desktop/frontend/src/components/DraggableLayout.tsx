import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useT } from "../lib/i18n";

export interface LayoutPanel {
  id: string;
  title: string;
  component: React.ReactNode;
  defaultSize?: number;
  minSize?: number;
  maxSize?: number;
  resizable?: boolean;
  collapsible?: boolean;
}

export interface DraggableLayoutProps {
  panels: LayoutPanel[];
  direction?: "horizontal" | "vertical";
  onLayoutChange?: (sizes: Record<string, number>) => void;
  persistenceKey?: string;
}

function loadSizes(key: string, _panelIds: string[]): Record<string, number> | null {
  try {
    const raw = window.localStorage.getItem(`Rexion.layout.${key}`);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return null;
    return parsed as Record<string, number>;
  } catch {
    return null;
  }
}

function saveSizes(key: string, sizes: Record<string, number>): void {
  try {
    window.localStorage.setItem(`Rexion.layout.${key}`, JSON.stringify(sizes));
  } catch { /* ignore */ }
}

export function DraggableLayout({
  panels,
  direction = "horizontal",
  onLayoutChange,
  persistenceKey,
}: DraggableLayoutProps) {
  const t = useT();
  const containerRef = useRef<HTMLDivElement>(null);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [dragging, setDragging] = useState<number | null>(null); // index of divider being dragged
  const [previewSizes, setPreviewSizes] = useState<Record<string, number> | null>(null);

  // Compute initial sizes from defaults or persisted values
  const defaultSizes = useMemo(() => {
    const total = panels.reduce((sum, p) => sum + (p.defaultSize ?? 1), 0);
    const sizes: Record<string, number> = {};
    for (const p of panels) {
      const raw = (p.defaultSize ?? 1) / total;
      sizes[p.id] = raw;
    }
    return sizes;
  }, [panels]);

  const [sizes, setSizes] = useState<Record<string, number>>(() => {
    if (!persistenceKey) return { ...defaultSizes };
    const persisted = loadSizes(persistenceKey, panels.map((p) => p.id));
    if (!persisted) return { ...defaultSizes };
    // Merge persisted with defaults for any new panels
    const merged = { ...defaultSizes };
    for (const p of panels) {
      if (persisted[p.id] !== undefined) merged[p.id] = persisted[p.id];
    }
    return merged;
  });

  const activeSizes = previewSizes ?? sizes;

  // Emit layout change on size updates
  useEffect(() => {
    onLayoutChange?.(sizes);
    if (persistenceKey) saveSizes(persistenceKey, sizes);
  }, [sizes, onLayoutChange, persistenceKey]);

  const toggleCollapse = useCallback((id: string) => {
    setCollapsed((prev) => ({ ...prev, [id]: !prev[id] }));
  }, []);

  const startDrag = useCallback(
    (event: ReactPointerEvent<HTMLDivElement>, dividerIndex: number) => {
      event.preventDefault();
      setDragging(dividerIndex);
      const container = containerRef.current;
      if (!container) return;
      const rect = container.getBoundingClientRect();
      const totalSize = direction === "horizontal" ? rect.width : rect.height;

      // Get the two panels adjacent to this divider
      const leftPanel = panels[dividerIndex];
      const rightPanel = panels[dividerIndex + 1];
      if (!leftPanel || !rightPanel) return;

      const leftMin = (leftPanel.minSize ?? 0.05) * totalSize;
      const rightMin = (rightPanel.minSize ?? 0.05) * totalSize;
      const leftMax = (leftPanel.maxSize ?? 0.95) * totalSize;
      const rightMax = (rightPanel.maxSize ?? 0.95) * totalSize;

      const isLeftCollapsed = collapsed[leftPanel.id];
      const isRightCollapsed = collapsed[rightPanel.id];
      if (isLeftCollapsed || isRightCollapsed) return;

      const startCoord = direction === "horizontal" ? event.clientX : event.clientY;
      const startLeftSize = activeSizes[leftPanel.id] * totalSize;
      const startRightSize = activeSizes[rightPanel.id] * totalSize;

      const onMove = (moveEvent: PointerEvent) => {
        const currentCoord = direction === "horizontal" ? moveEvent.clientX : moveEvent.clientY;
        const delta = currentCoord - startCoord;
        let newLeftSize = startLeftSize + delta;
        let newRightSize = startRightSize - delta;

        // Enforce min/max constraints
        newLeftSize = Math.max(leftMin, Math.min(leftMax, newLeftSize));
        newRightSize = Math.max(rightMin, Math.min(rightMax, newRightSize));

        // Re-adjust to ensure sum stays constant
        const totalTwo = startLeftSize + startRightSize;
        if (newLeftSize + newRightSize !== totalTwo) {
          const diff = totalTwo - newLeftSize - newRightSize;
          newRightSize += diff;
          newRightSize = Math.max(rightMin, newRightSize);
        }

        setPreviewSizes((prev) => ({
          ...prev ?? sizes,
          [leftPanel.id]: newLeftSize / totalSize,
          [rightPanel.id]: newRightSize / totalSize,
        }));
      };

      const onDone = () => {
        setPreviewSizes((prev) => {
          if (prev) setSizes(prev);
          return null;
        });
        setDragging(null);
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onDone);
        window.removeEventListener("pointercancel", onDone);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };

      document.body.style.cursor = direction === "horizontal" ? "col-resize" : "row-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onDone);
      window.addEventListener("pointercancel", onDone);
    },
    [panels, direction, collapsed, activeSizes, sizes],
  );

  const resetDivider = useCallback(
    (dividerIndex: number) => {
      const leftPanel = panels[dividerIndex];
      const rightPanel = panels[dividerIndex + 1];
      if (!leftPanel || !rightPanel) return;
      const totalDefault = (leftPanel.defaultSize ?? 1) + (rightPanel.defaultSize ?? 1);
      setSizes((prev) => ({
        ...prev,
        [leftPanel.id]: (leftPanel.defaultSize ?? 1) / totalDefault,
        [rightPanel.id]: (rightPanel.defaultSize ?? 1) / totalDefault,
      }));
    },
    [panels],
  );

  // Compute effective sizes accounting for collapsed panels
  const effectiveSizes = useMemo(() => {
    const result: Record<string, number> = {};
    const nonCollapsedPanels = panels.filter((p) => !collapsed[p.id]);
    const totalNonCollapsed = nonCollapsedPanels.reduce(
      (sum, p) => sum + (activeSizes[p.id] ?? (p.defaultSize ?? 1)),
      0,
    );
    for (const p of panels) {
      if (collapsed[p.id]) {
        result[p.id] = 0;
      } else {
        const raw = activeSizes[p.id] ?? (p.defaultSize ?? 1);
        result[p.id] = raw / totalNonCollapsed;
      }
    }
    return result;
  }, [panels, collapsed, activeSizes]);

  const isHorizontal = direction === "horizontal";

  return (
    <div
      ref={containerRef}
      className={`draggable-layout draggable-layout--${direction}`}
    >
      {panels.map((panel, index) => {
        const isPanelCollapsed = collapsed[panel.id];
        const sizePercent = effectiveSizes[panel.id] * 100;
        const panelStyle: React.CSSProperties = {
          [isHorizontal ? "width" : "height"]: isPanelCollapsed ? "0px" : `${sizePercent}%`,
          flexShrink: 0,
          overflow: "hidden",
        };

        return (
          <div key={panel.id} className="draggable-layout__panel-wrapper" style={panelStyle}>
            <div className="draggable-layout__panel">
              {(panel.collapsible !== false) && (
                <div className="draggable-layout__panel-header">
                  <button
                    className="draggable-layout__collapse-btn"
                    onClick={() => toggleCollapse(panel.id)}
                    title={isPanelCollapsed ? t("draggableLayout.expand") : t("draggableLayout.collapse")}
                  >
                    {isPanelCollapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
                    <span className="draggable-layout__panel-title">{panel.title}</span>
                  </button>
                </div>
              )}
              {!isPanelCollapsed && (
                <div className="draggable-layout__panel-body">
                  {panel.component}
                </div>
              )}
            </div>

            {/* Divider between panels (not after last panel) */}
            {index < panels.length - 1 && (
              <div
                className={`draggable-layout__divider${dragging === index ? " draggable-layout__divider--active" : ""}`}
                role="separator"
                aria-orientation={isHorizontal ? "vertical" : "horizontal"}
                onPointerDown={(e) => startDrag(e, index)}
                onDoubleClick={() => resetDivider(index)}
              />
            )}
          </div>
        );
      })}
    </div>
  );
}
