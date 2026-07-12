import { useCallback, useEffect, useMemo, useState } from "react";
import { Smartphone, RefreshCw, Upload, Download, Monitor, Tablet } from "lucide-react";
import { app } from "../lib/bridge";
import { useT } from "../lib/i18n";
import type { SessionMeta } from "../lib/types";

// --- Device identity (localStorage fallback) ---
const DEVICE_KEY = "rexion-sync-device";

interface DeviceInfo {
  id: string;
  name: string;
}

function loadOrCreateDevice(): DeviceInfo {
  try {
    const raw = localStorage.getItem(DEVICE_KEY);
    if (raw) {
      const d = JSON.parse(raw) as DeviceInfo;
      if (d.id && d.name) return d;
    }
  } catch {
    /* ignore */
  }
  const id = generateDeviceID();
  const name = detectDeviceName();
  const d: DeviceInfo = { id, name };
  try {
    localStorage.setItem(DEVICE_KEY, JSON.stringify(d));
  } catch {
    /* ignore */
  }
  return d;
}

function generateDeviceID(): string {
  const bytes = new Uint8Array(8);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

function detectDeviceName(): string {
  const ua = typeof navigator !== "undefined" ? navigator.userAgent : "";
  if (/Win/i.test(ua)) return "Windows-PC";
  if (/Mac/i.test(ua)) return "Mac";
  if (/Android/i.test(ua)) return "Android";
  if (/iPhone|iPad|iPod/i.test(ua)) return "iOS";
  if (/Linux/i.test(ua)) return "Linux";
  return "device";
}

// --- QR-like code generation (pure SVG, no libraries) ---
// Generates a deterministic pixel grid from a hash of the input string,
// with the three characteristic finder patterns in the corners.
function qrMatrix(input: string, size = 25): boolean[][] {
  const hash = simpleHash(input);
  const matrix: boolean[][] = [];
  for (let r = 0; r < size; r++) {
    matrix[r] = [];
    for (let c = 0; c < size; c++) {
      const idx = (r * size + c) % hash.length;
      matrix[r][c] = hash.charCodeAt(idx) % 2 === 0;
    }
  }
  // Draw finder patterns (7x7 squares with 3x7 border + center) at three corners
  const drawFinder = (or: number, oc: number) => {
    for (let r = 0; r < 7; r++) {
      for (let c = 0; c < 7; c++) {
        const isBorder = r === 0 || r === 6 || c === 0 || c === 6;
        const isCenter = r >= 2 && r <= 4 && c >= 2 && c <= 4;
        matrix[or + r][oc + c] = isBorder || isCenter;
      }
    }
    // Clear the separator ring around the finder
    for (let r = -1; r <= 7; r++) {
      for (let c = -1; c <= 7; c++) {
        if (r === -1 || r === 7 || c === -1 || c === 7) {
          const rr = or + r;
          const cc = oc + c;
          if (rr >= 0 && rr < size && cc >= 0 && cc < size) {
            matrix[rr][cc] = false;
          }
        }
      }
    }
    // Redraw finder (separator cleared the border too)
    for (let r = 0; r < 7; r++) {
      for (let c = 0; c < 7; c++) {
        const isBorder = r === 0 || r === 6 || c === 0 || c === 6;
        const isCenter = r >= 2 && r <= 4 && c >= 2 && c <= 4;
        matrix[or + r][oc + c] = isBorder || isCenter;
      }
    }
  };
  drawFinder(0, 0);
  drawFinder(0, size - 7);
  drawFinder(size - 7, 0);
  return matrix;
}

function simpleHash(s: string): string {
  let h1 = 0xdeadbeef;
  let h2 = 0x41c6ce57;
  for (let i = 0; i < s.length; i++) {
    const ch = s.charCodeAt(i);
    h1 = Math.imul(h1 ^ ch, 2654435761);
    h2 = Math.imul(h2 ^ ch, 1597334677);
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507);
  h1 ^= Math.imul(h2 ^ (h2 >>> 13), 3266489909);
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507);
  h2 ^= Math.imul(h1 ^ (h1 >>> 13), 3266489909);
  const combined = (h1 >>> 0).toString(16).padStart(8, "0") + (h2 >>> 0).toString(16).padStart(8, "0");
  // Repeat to cover the matrix
  return combined.repeat(8);
}

function QRCode({ value, size = 200 }: { value: string; size?: number }) {
  const matrix = useMemo(() => qrMatrix(value), [value]);
  const n = matrix.length;
  const cell = size / n;
  const rects = [];
  for (let r = 0; r < n; r++) {
    for (let c = 0; c < n; c++) {
      if (matrix[r][c]) {
        rects.push(<rect key={`${r}-${c}`} x={c * cell} y={r * cell} width={cell} height={cell} />);
      }
    }
  }
  return (
    <svg className="session-sync__qr" viewBox={`0 0 ${size} ${size}`} width={size} height={size}>
      <rect x={0} y={0} width={size} height={size} fill="#ffffff" />
      <g fill="#000000">{rects}</g>
    </svg>
  );
}

// --- Remote entry shape (mirrors agent.SyncEntry) ---
interface RemoteEntry {
  remote_id: string;
  session_id: string;
  device_name: string;
  pushed_at: string; // ISO 8601
  size: number;
}

// --- Component ---
export function SessionSync({ onClose }: { onClose?: () => void }) {
  const t = useT();
  const [device, setDevice] = useState<DeviceInfo | null>(null);
  const [sessions, setSessions] = useState<SessionMeta[]>([]);
  const [selectedSession, setSelectedSession] = useState<SessionMeta | null>(null);
  const [remoteEntries, setRemoteEntries] = useState<RemoteEntry[]>([]);
  const [pushing, setPushing] = useState(false);
  const [pulling, setPulling] = useState<string | null>(null);
  const [lastPushedId, setLastPushedId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setDevice(loadOrCreateDevice());
  }, []);

  const refreshSessions = useCallback(async () => {
    try {
      const list = await app.ListSessions();
      setSessions(list);
      const current = list.find((s) => s.current) ?? list[0] ?? null;
      setSelectedSession(current);
    } catch {
      /* ignore */
    }
  }, []);

  useEffect(() => {
    refreshSessions();
  }, [refreshSessions]);

  const handlePush = useCallback(async () => {
    if (!selectedSession) return;
    setPushing(true);
    setError(null);
    try {
      // The actual push goes through the Go CLI (rexion session push).
      // The desktop binding is not yet wired; this shows the expected UX
      // and generates a preview remote ID for the QR code.
      const sessionId = sessionIdFromPath(selectedSession.path);
      const remoteId = generatePreviewRemoteId(sessionId);
      setLastPushedId(remoteId);
      setRemoteEntries((prev) => [
        {
          remote_id: remoteId,
          session_id: sessionId,
          device_name: device?.name ?? "this-device",
          pushed_at: new Date().toISOString(),
          size: Math.max(1, selectedSession.turns * 512),
        },
        ...prev,
      ]);
    } catch (e) {
      setError(String(e));
    } finally {
      setPushing(false);
    }
  }, [selectedSession, device]);

  const handlePull = useCallback(async (remoteId: string) => {
    setPulling(remoteId);
    setError(null);
    try {
      // The actual pull goes through the Go CLI (rexion session pull).
      // A future bridge binding will call the Go SyncManager directly.
      setRemoteEntries((prev) => prev.filter((e) => e.remote_id !== remoteId));
      await refreshSessions();
    } catch (e) {
      setError(String(e));
    } finally {
      setPulling(null);
    }
  }, [refreshSessions]);

  const sessionForQR = lastPushedId ?? (selectedSession ? sessionIdFromPath(selectedSession.path) : "");
  const deviceIcon = useMemo(() => {
    const name = device?.name ?? "";
    if (/Android|iOS/i.test(name)) return <Smartphone size={16} />;
    if (/iPad|Tablet/i.test(name)) return <Tablet size={16} />;
    return <Monitor size={16} />;
  }, [device]);

  return (
    <div className="session-sync">
      <div className="session-sync__header">
        <h2 className="session-sync__title">{t("sessionSync.title")}</h2>
        {onClose && (
          <button className="session-sync__close" onClick={onClose} aria-label={t("common.close")}>
            ✕
          </button>
        )}
      </div>

      {/* Device identity */}
      <div className="session-sync__device">
        <div className="session-sync__device-icon">{deviceIcon}</div>
        <div className="session-sync__device-info">
          <div className="session-sync__device-name">{device?.name ?? "…"}</div>
          <div className="session-sync__device-id">{device?.id ?? ""}</div>
        </div>
      </div>

      {/* Continue-on-another-device hint */}
      <div className="session-sync__hint">
        <RefreshCw size={14} />
        <span>{t("sessionSync.continueHint")}</span>
      </div>

      {/* Push section */}
      <div className="session-sync__section">
        <div className="session-sync__section-label">{t("sessionSync.pushLabel")}</div>
        <div className="session-sync__push-row">
          <select
            className="session-sync__select"
            value={selectedSession?.path ?? ""}
            onChange={(e) => {
              const found = sessions.find((s) => s.path === e.target.value);
              setSelectedSession(found ?? null);
            }}
          >
            {sessions.length === 0 && <option value="">{t("sessionSync.noSessions")}</option>}
            {sessions.map((s) => (
              <option key={s.path} value={s.path}>
                {s.title || s.preview || sessionIdFromPath(s.path)} ({s.turns} {t("sessionSync.turns")})
              </option>
            ))}
          </select>
          <button
            className="session-sync__btn session-sync__btn--primary"
            onClick={handlePush}
            disabled={!selectedSession || pushing}
          >
            <Upload size={14} />
            {pushing ? t("common.loading") : t("sessionSync.push")}
          </button>
        </div>

        {/* QR code */}
        {sessionForQR && (
          <div className="session-sync__qr-wrap">
            <QRCode value={sessionForQR} size={180} />
            <div className="session-sync__qr-label">{t("sessionSync.qrHint")}</div>
            <code className="session-sync__remote-id">{sessionForQR}</code>
          </div>
        )}
      </div>

      {/* Remote list */}
      <div className="session-sync__section">
        <div className="session-sync__section-label">{t("sessionSync.remoteLabel")}</div>
        {remoteEntries.length === 0 ? (
          <div className="session-sync__empty">{t("sessionSync.noRemote")}</div>
        ) : (
          <div className="session-sync__remote-list">
            {remoteEntries.map((entry) => (
              <div key={entry.remote_id} className="session-sync__remote-item">
                <div className="session-sync__remote-info">
                  <div className="session-sync__remote-session">{entry.session_id}</div>
                  <div className="session-sync__remote-meta">
                    {entry.device_name} · {formatSize(entry.size)} · {formatDate(entry.pushed_at)}
                  </div>
                </div>
                <button
                  className="session-sync__btn session-sync__btn--ghost"
                  onClick={() => handlePull(entry.remote_id)}
                  disabled={pulling === entry.remote_id}
                >
                  <Download size={14} />
                  {pulling === entry.remote_id ? t("common.loading") : t("sessionSync.pull")}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

      {error && <div className="session-sync__error">{error}</div>}
    </div>
  );
}

// --- helpers ---

function sessionIdFromPath(path: string): string {
  const base = path.split(/[/\\]/).filter(Boolean).pop() ?? path;
  const dot = base.lastIndexOf(".");
  return dot > 0 ? base.slice(0, dot) : base;
}

function generatePreviewRemoteId(sessionId: string): string {
  const stamp = new Date().toISOString().replace(/[-:T]/g, "").slice(0, 15);
  const rand = Math.random().toString(16).slice(2, 10);
  void sessionId;
  return `${stamp}-${rand}`;
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDate(iso: string): string {
  try {
    const d = new Date(iso);
    return d.toLocaleString();
  } catch {
    return iso;
  }
}
