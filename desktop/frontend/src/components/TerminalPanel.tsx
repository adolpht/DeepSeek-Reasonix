import { useCallback, useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { Plus, Eraser, X, ChevronDown, TerminalSquare } from "lucide-react";
import "@xterm/xterm/css/xterm.css";
import type { TerminalView } from "../lib/types";
import { app, onTerminalOutput } from "../lib/bridge";
import { useT } from "../lib/i18n";

interface TerminalPanelProps {
  cwd?: string;
}

export function TerminalPanel({ cwd }: TerminalPanelProps) {
  const t = useT();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const termRef = useRef<Terminal | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const sessionIdRef = useRef<string | null>(null);
  const [shells, setShells] = useState<string[]>([]);
  const [shell, setShell] = useState<string>("");
  const [running, setRunning] = useState(false);
  const [dropdownOpen, setDropdownOpen] = useState(false);

  // Start a PTY session and wire up input/output.
  const startSession = useCallback(
    async (selectedShell: string) => {
      const term = termRef.current;
      const fit = fitRef.current;
      if (!term || !fit) return;

      // Kill any existing session first.
      const prev = sessionIdRef.current;
      if (prev) {
        await app.TerminalKill(prev).catch(() => {});
        sessionIdRef.current = null;
      }

      term.clear();
      term.writeln(`\x1b[2m${t("terminal.starting")}…\x1b[0m`);

      let cols = 80;
      let rows = 24;
      try {
        const dims = fit.proposeDimensions();
        if (dims) {
          cols = Math.max(dims.cols, 10);
          rows = Math.max(dims.rows, 4);
        }
      } catch {
        // ignore — use defaults
      }

      try {
        const view: TerminalView = await app.TerminalStart(cwd ?? "", selectedShell, cols, rows);
        sessionIdRef.current = view.id;
        setRunning(true);
        term.writeln(`\x1b[2m${view.shell}  PID ${view.pid}  ${view.cwd}\x1b[0m`);
        term.focus();
      } catch (err) {
        term.writeln(`\x1b[31m${t("terminal.startFailed")}: ${err}\x1b[0m`);
        setRunning(false);
      }
    },
    [cwd, t],
  );

  // One-time terminal setup.
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const term = new Terminal({
      fontFamily: "'Cascadia Code', 'JetBrains Mono', 'Fira Code', Consolas, 'Courier New', monospace",
      fontSize: 13,
      cursorBlink: true,
      scrollback: 5000,
      allowProposedApi: true,
      theme: {
        background: "#1a1b26",
        foreground: "#c0caf5",
        cursor: "#c0caf5",
        selectionBackground: "#33467c",
        black: "#15161e",
        red: "#f7768e",
        green: "#9ece6a",
        yellow: "#e0af68",
        blue: "#7aa2f7",
        magenta: "#bb9af7",
        cyan: "#7dcfff",
        white: "#a9b1d6",
        brightBlack: "#414868",
        brightRed: "#f7768e",
        brightGreen: "#9ece6a",
        brightYellow: "#e0af68",
        brightBlue: "#7aa2f7",
        brightMagenta: "#bb9af7",
        brightCyan: "#7dcfff",
        brightWhite: "#c0caf5",
      },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new WebLinksAddon());
    term.open(container);
    fit.fit();

    termRef.current = term;
    fitRef.current = fit;

    // Pipe user input → PTY stdin.
    const inputDisp = term.onData((data) => {
      const id = sessionIdRef.current;
      if (id) void app.TerminalWrite(id, data).catch(() => {});
    });

    // Pipe terminal resize → PTY resize.
    const resizeDisp = term.onResize(({ cols, rows }) => {
      const id = sessionIdRef.current;
      if (id) void app.TerminalResize(id, cols, rows).catch(() => {});
    });

    // Subscribe to PTY output events.
    const unsub = onTerminalOutput((e) => {
      if (e.id !== sessionIdRef.current) return;
      if (e.exit) {
        term.writeln(`\r\n\x1b[2m${t("terminal.exited")} (code ${e.code})\x1b[0m`);
        sessionIdRef.current = null;
        setRunning(false);
      } else if (e.data) {
        term.write(e.data);
      }
    });

    // Auto-fit on container resize.
    const ro = new ResizeObserver(() => {
      try {
        fit.fit();
      } catch {
        // ignore — terminal may be mid-dispose
      }
    });
    ro.observe(container);

    // Load available shells, then auto-start with the default.
    (async () => {
      try {
        const list = await app.TerminalShells();
        setShells(list);
        const initial = list[0] ?? "";
        setShell(initial);
        await startSession(initial);
      } catch {
        // ignore
      }
    })();

    return () => {
      inputDisp.dispose();
      resizeDisp.dispose();
      unsub();
      ro.disconnect();
      const id = sessionIdRef.current;
      if (id) void app.TerminalKill(id).catch(() => {});
      sessionIdRef.current = null;
      term.dispose();
      termRef.current = null;
      fitRef.current = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Restart when shell selection changes (after initial load).
  const handleShellChange = useCallback(
    (next: string) => {
      setShell(next);
      setDropdownOpen(false);
      void startSession(next);
    },
    [startSession],
  );

  const handleNew = useCallback(() => {
    void startSession(shell);
  }, [shell, startSession]);

  const handleClear = useCallback(() => {
    termRef.current?.clear();
  }, []);

  const handleClose = useCallback(async () => {
    const id = sessionIdRef.current;
    if (id) {
      await app.TerminalKill(id).catch(() => {});
      sessionIdRef.current = null;
      setRunning(false);
      termRef.current?.writeln(`\r\n\x1b[2m${t("terminal.closed")}\x1b[0m`);
    }
  }, [t]);

  const shellLabel = (s: string) => {
    if (!s) return t("terminal.default");
    const base = s.split(/[/\\]/).pop() ?? s;
    return base.replace(/\.(exe|EXE)$/, "");
  };

  return (
    <div className="terminal-panel">
      <div className="terminal-panel__toolbar">
        <div className="terminal-panel__title">
          <TerminalSquare size={15} />
          <span>{t("sidebar.terminal")}</span>
          {running && <span className="terminal-panel__dot" />}
        </div>
        <div className="terminal-panel__actions">
          {/* Shell selector */}
          <div className="terminal-panel__select">
            <button
              className="terminal-panel__select-btn"
              onClick={() => setDropdownOpen((o) => !o)}
              title={t("terminal.selectShell")}
            >
              <span>{shellLabel(shell)}</span>
              <ChevronDown size={13} />
            </button>
            {dropdownOpen && (
              <>
                <div className="terminal-panel__select-overlay" onClick={() => setDropdownOpen(false)} />
                <div className="terminal-panel__select-menu">
                  {shells.map((s) => (
                    <button
                      key={s}
                      className={"terminal-panel__select-item" + (s === shell ? " is-active" : "")}
                      onClick={() => handleShellChange(s)}
                    >
                      {shellLabel(s)}
                    </button>
                  ))}
                </div>
              </>
            )}
          </div>
          <button className="terminal-panel__btn" onClick={handleNew} title={t("terminal.newSession")}>
            <Plus size={14} />
          </button>
          <button className="terminal-panel__btn" onClick={handleClear} title={t("terminal.clear")}>
            <Eraser size={14} />
          </button>
          <button
            className="terminal-panel__btn terminal-panel__btn--danger"
            onClick={handleClose}
            title={t("terminal.closeSession")}
            disabled={!running}
          >
            <X size={14} />
          </button>
        </div>
      </div>
      <div className="terminal-panel__term" ref={containerRef} />
    </div>
  );
}
