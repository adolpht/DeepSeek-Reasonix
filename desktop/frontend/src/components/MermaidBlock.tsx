import { useEffect, useRef, useState, memo } from "react";
import mermaid from "mermaid";

// MermaidBlock renders a mermaid diagram definition into an SVG.
// It initialises mermaid once (idempotent) and re-renders whenever the
// source definition changes.  On parse error it falls back to showing the
// raw source so the user can still read the diagram text.

let mermaidInitialised = false;

function initMermaid() {
  if (mermaidInitialised) return;
  mermaid.initialize({
    startOnLoad: false,
    theme: "dark",
    securityLevel: "loose",
    fontFamily: "inherit",
  });
  mermaidInitialised = true;
}

let renderCounter = 0;

export const MermaidBlock = memo(function MermaidBlock({ source }: { source: string }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [svg, setSvg] = useState<string | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    initMermaid();
    let cancelled = false;
    const id = `mermaid-${++renderCounter}`;

    mermaid
      .render(id, source)
      .then(({ svg: result }) => {
        if (!cancelled) {
          setSvg(result);
          setError(false);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setSvg(null);
          setError(true);
        }
      });

    return () => {
      cancelled = true;
      // Clean up the temporary DOM element mermaid creates
      const el = document.getElementById(id);
      if (el) el.remove();
    };
  }, [source]);

  if (error) {
    return (
      <div className="mermaid-block mermaid-block--error">
        <pre className="mermaid-block__source">{source}</pre>
      </div>
    );
  }

  if (svg) {
    return (
      <div
        ref={containerRef}
        className="mermaid-block"
        dangerouslySetInnerHTML={{ __html: svg }}
      />
    );
  }

  return <div className="mermaid-block mermaid-block--loading" />;
});
