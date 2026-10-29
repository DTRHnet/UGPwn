// QUEUE card: live job list with per-row cancel/retry and bulk actions.

import {
  cancelJob,
  clearFinishedJobs,
  notify,
  queueTabs,
  retryJob,
  selectJob,
  useStore,
} from "../store";
import { formatBytes, classNames, formatRelative, text } from "../util";
import { CloseIcon, PlayIcon, RetryIcon } from "./icons";
import { ProgressBar, RegionState, StatusGlyph } from "./primitives";

export function QueueCard({
  rowIndex,
  onFocusRow,
}: {
  rowIndex: number;
  onFocusRow: (index: number) => void;
}) {
  const state = useStore();
  const jobs = state.jobs;
  const jobsStatus = state.jobsStatus;
  const queuedOrRunning = jobs.filter((job) => job.status === "queued" || job.status === "running");
  const selection = state.selectedTabs;

  const startAll = () => {
    if (selection.length > 0) {
      void queueTabs(selection);
      return;
    }
    notify("info", `${queuedOrRunning.length} job(s) queued — the backend starts them automatically.`);
  };

  return (
    <section class="card">
      <header class="card-head">
        <h2 class="card-title">QUEUE ({jobs.length})</h2>
        <span class="muted" style={{ fontSize: "10.5px" }}>
          {queuedOrRunning.length > 0
            ? `${queuedOrRunning.length} active · starts automatically`
            : "idle"}
        </span>
        <span class="card-head-spacer" />
        <button
          type="button"
          class="btn tiny"
          onClick={startAll}
          disabled={selection.length === 0 && queuedOrRunning.length === 0}
          title={
            selection.length > 0
              ? `Queue the ${selection.length} selected track(s)`
              : "Select tracks in Browse or Batch mode (Space), then start all"
          }
        >
          <PlayIcon /> Start all{selection.length > 0 ? ` (${selection.length})` : ""}
        </button>
        <button
          type="button"
          class="btn tiny"
          onClick={() => void clearFinishedJobs()}
          disabled={jobs.every((job) => job.status === "queued" || job.status === "running") && jobs.length > 0}
          title="Remove finished jobs from the live list"
        >
          Clear
        </button>
      </header>

      <div class="queue-table">
        <div class="queue-row queue-head" role="row">
          <span class="cell-index">#</span>
          <span>SONG / ARTIST</span>
          <span class="cell-tag">TAG</span>
          <span class="cell-source">SOURCE</span>
          <span>PROGRESS</span>
          <span class="cell-size">SIZE</span>
          <span />
          <span class="row-actions" style={{ justifyContent: "flex-end" }}>
            ACTIONS
          </span>
        </div>

        {jobs.length === 0 ? (
          <RegionState
            status={jobsStatus}
            empty
            emptyText="No jobs yet. Paste a tab URL and press HEIST IT, or queue tracks from Browse."
            loadingText="Loading jobs…"
          />
        ) : (
          jobs.map((job, index) => {
            const selected = state.selectedJobId === job.id;
            const focused = index === rowIndex;
            const canCancel = job.status === "queued" || job.status === "running";
            const canRetry = job.status === "failed" || job.status === "canceled";
            return (
              <div
                key={job.id}
                class={classNames("queue-row", selected && "is-selected", focused && "is-focus")}
                role="row"
                tabIndex={0}
                aria-selected={selected ? "true" : "false"}
                onClick={() => {
                  selectJob(job.id);
                  onFocusRow(index);
                }}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    selectJob(job.id);
                    onFocusRow(index);
                  }
                }}
                title={job.message ?? job.path ?? job.id}
              >
                <span class="cell-index">{index + 1}</span>
                <div class="cell-main">
                  <div class="cell-song">{text(job.tab.song)}</div>
                  <div class="cell-sub">
                    {text(job.tab.artist)}
                    {job.status === "failed" && job.message !== undefined ? ` · ${job.message}` : ""}
                  </div>
                </div>
                <span class="cell-tag">
                  <span class="tag">{text(job.tag)}</span>
                </span>
                <span class="cell-source">{text(job.source)}</span>
                <ProgressBar value={job.progress} status={job.status} />
                <span class="cell-size">{formatBytes(job.bytes)}</span>
                <span title={formatRelative(job.startedAt)}>
                  <StatusGlyph status={job.status} />
                </span>
                <span class="row-actions">
                  {canCancel && (
                    <button
                      type="button"
                      class="icon-btn"
                      aria-label={`Cancel ${text(job.tab.song)}`}
                      title="Cancel this job"
                      onClick={(event) => {
                        event.stopPropagation();
                        void cancelJob(job.id);
                      }}
                    >
                      <CloseIcon />
                    </button>
                  )}
                  {canRetry && (
                    <button
                      type="button"
                      class="icon-btn"
                      aria-label={`Retry ${text(job.tab.song)}`}
                      title="Retry with the same tab and options"
                      onClick={(event) => {
                        event.stopPropagation();
                        void retryJob(job.id);
                      }}
                    >
                      <RetryIcon />
                    </button>
                  )}
                </span>
              </div>
            );
          })
        )}
      </div>
    </section>
  );
}
