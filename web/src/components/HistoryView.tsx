// HISTORY view: persisted jobs from GET /api/history.

import { loadHistory, useStore } from "../store";
import { DASH, formatBytes, formatRelative, text } from "../util";
import { RetryIcon } from "./icons";
import { Card, RegionState, StatusGlyph } from "./primitives";

export function HistoryView() {
  const state = useStore();
  const history = state.history;

  return (
    <Card
      title="HISTORY"
      actions={
        <button type="button" class="btn tiny" onClick={() => void loadHistory()} title="Reload persisted history">
          <RetryIcon /> Refresh
        </button>
      }
      bodyClass="tight"
    >
      <div class="data-table" role="table" aria-label="Download history">
        <div class="data-row data-head history" role="row">
          <span>WHEN</span>
          <span>SONG / ARTIST</span>
          <span>TAG</span>
          <span>STATUS</span>
          <span>SIZE</span>
          <span aria-label="Path" />
        </div>
        {history.status !== "ready" || history.data.length === 0 ? (
          <RegionState
            status={history.status}
            empty
            emptyText="No persisted history yet — completed jobs are recorded server-side."
            loadingText="Loading history…"
            retry={() => void loadHistory()}
          />
        ) : (
          history.data.map((job) => (
            <div class="data-row history" role="row" key={job.id} title={job.path ?? job.id}>
              <span class="cell-source mono">{formatRelative(job.startedAt)}</span>
              <div class="cell-main">
                <div class="cell-song">{text(job.tab.song)}</div>
                <div class="cell-sub">{text(job.tab.artist)}</div>
              </div>
              <span class="cell-tag">
                <span class="tag">{text(job.tag)}</span>
              </span>
              <span class="cell-source" style={{ display: "flex", alignItems: "center", gap: "6px" }}>
                <StatusGlyph status={job.status} />
                {job.status}
              </span>
              <span class="cell-size">{formatBytes(job.bytes)}</span>
              <span class="cell-source mono" title={job.path ?? DASH}>
                {job.path !== undefined ? "↗" : DASH}
              </span>
            </div>
          ))
        )}
      </div>
    </Card>
  );
}
