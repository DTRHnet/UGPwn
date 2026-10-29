// Footer status bar: version, metrics and a sparkline of recent job progress.

import { useStore } from "../store";
import { DASH, formatBytes, formatCount, formatRate } from "../util";
import { Sparkline } from "./primitives";

export function Footer() {
  const state = useStore();
  const ready = state.server.status === "ready";
  const spark = state.jobs.slice(-14).map((job) => job.progress);

  return (
    <footer class="footer" aria-label="Session status">
      <span class="footer-item">
        echoheist <span class="footer-value">v{state.version.length > 0 ? state.version : DASH}</span>
      </span>
      <span class="footer-item">
        downloads <span class="footer-value">{ready ? formatCount(state.metrics.totalDownloads) : DASH}</span>
      </span>
      <span class="footer-item">
        success <span class="footer-value">{ready ? formatRate(state.metrics.successRate) : DASH}</span>
      </span>
      <span class="footer-item">
        bytes <span class="footer-value">{ready ? formatBytes(state.metrics.bytes) : DASH}</span>
      </span>
      <span class="footer-item">
        queue{" "}
        <span class="footer-value">
          {ready ? `${state.counts.queued} queued · ${state.counts.running} running` : DASH}
        </span>
      </span>
      <div class="footer-spark" title="Recent job progress">
        <span class="footer-item">progress</span>
        <Sparkline values={spark} />
      </div>
    </footer>
  );
}
