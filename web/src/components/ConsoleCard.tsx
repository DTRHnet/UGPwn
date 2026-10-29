// OUTPUT console: level-coloured, auto-scrolling log tail for the live session,
// plus read-only access to on-disk log files from /api/logs.

import { useEffect, useRef } from "preact/hooks";
import { clearConsole, notify, useStore } from "../store";
import { classNames, formatTime } from "../util";
import { FolderIcon } from "./icons";
import { RegionState } from "./primitives";

export function ConsoleCard() {
  const state = useStore();
  const scroller = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);

  const lines = state.logs;
  const fileLines = state.openLog;

  useEffect(() => {
    const node = scroller.current;
    if (node === null || !pinned.current) return;
    node.scrollTop = node.scrollHeight;
  }, [lines.length, fileLines]);

  const onScroll = () => {
    const node = scroller.current;
    if (node === null) return;
    pinned.current = node.scrollHeight - node.scrollTop - node.clientHeight < 40;
  };

  return (
    <section class="card">
      <header class="card-head">
        <h2 class="card-title">OUTPUT</h2>
        {fileLines !== null && (
          <span class="muted" style={{ fontSize: "10.5px" }}>
            viewing {fileLines.name} ({fileLines.lines.length} lines)
          </span>
        )}
        <span class="card-head-spacer" />
        <button
          type="button"
          class="btn tiny"
          title="Server-side path — open it in your file manager (the browser cannot open local folders)"
          onClick={() =>
            notify(
              "info",
              state.server.data?.config.downloadDir !== undefined
                ? `Downloads live in ${state.server.data.config.downloadDir} on the machine running the dashboard.`
                : "Downloads live in the configured --download-dir on the machine running the dashboard.",
            )
          }
        >
          <FolderIcon /> Open folder
        </button>
        <button
          type="button"
          class="btn tiny"
          title="Clear this view (server-side log files are untouched)"
          onClick={clearConsole}
        >
          Clear
        </button>
      </header>

      <div class="console" ref={scroller} onScroll={onScroll} role="log" aria-live="polite" aria-label="Worker output">
        {fileLines !== null ? (
          fileLines.lines.map((line, index) => (
            <div class="console-line level-info" key={`${fileLines.name}-${index}`}>
              <span class="console-time">{String(index + 1).padStart(4, "0")}</span>
              <span class="console-level">file</span>
              <span class="console-text">{line}</span>
            </div>
          ))
        ) : lines.length === 0 ? (
          <RegionState
            status={state.jobsStatus}
            empty
            inline
            emptyText="No output yet — worker events and server log lines stream here."
            loadingText="Waiting for output…"
          />
        ) : (
          lines.map((line) => (
            <div class={classNames("console-line", `level-${line.level}`)} key={line.seq}>
              <span class="console-time">{formatTime(line.at)}</span>
              <span class="console-level">{line.level}</span>
              <span class="console-text">{line.text}</span>
            </div>
          ))
        )}
      </div>
    </section>
  );
}
