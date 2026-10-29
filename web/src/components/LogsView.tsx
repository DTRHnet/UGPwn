// LOGS view: server-side log files. Selecting one streams its tail into the
// OUTPUT console below.

import { loadLogFiles, openLogFile, useStore } from "../store";
import { formatBytes, formatRelative } from "../util";
import { LogsIcon, RetryIcon } from "./icons";
import { Card, RegionState } from "./primitives";

export function LogsView() {
  const state = useStore();
  const files = state.logFiles;

  return (
    <Card
      title="LOGS"
      actions={
        <button type="button" class="btn tiny" onClick={() => void loadLogFiles()} title="Reload the log file list">
          <RetryIcon /> Refresh
        </button>
      }
      bodyClass="tight"
    >
      {files.status !== "ready" || files.data.length === 0 ? (
        <RegionState
          status={files.status}
          empty
          emptyText="No log files found in the configured --log-dir."
          loadingText="Listing log files…"
          retry={() => void loadLogFiles()}
        />
      ) : (
        files.data.map((file) => (
          <button
            key={file.name}
            type="button"
            class={state.openLog?.name === file.name ? "log-file is-active" : "log-file"}
            onClick={() => void openLogFile(file.name)}
            title={`Open ${file.name} in the console`}
          >
            <LogsIcon size={12} />
            <span class="truncate">{file.name}</span>
            <span class="log-file-size">
              {formatBytes(file.size)} · {formatRelative(file.modified)}
            </span>
          </button>
        ))
      )}
    </Card>
  );
}
