// Right panel: track info, preview stub and file details for the current selection.

import type { Tab } from "../api";
import { useStore } from "../store";
import { DASH, formatBytes, formatCount, formatRating, gradientFor, initials, text } from "../util";
import { Card, StatusGlyph } from "./primitives";

const STAVE = (
  <svg class="preview-stub" viewBox="0 0 320 116" role="img" aria-label="Static tablature preview placeholder">
    <rect x="0" y="0" width="320" height="116" fill="transparent" />
    {[26, 38, 50, 62, 74].map((y) => (
      <line key={y} x1="14" y1={y} x2="306" y2={y} stroke="var(--border-strong)" strokeWidth="1" />
    ))}
    {[86, 96, 106].map((y) => (
      <line key={y} x1="14" y1={y} x2="306" y2={y} stroke="var(--border)" strokeWidth="1" />
    ))}
    <g stroke="var(--accent-text)" strokeWidth="1.4" strokeLinecap="round">
      <line x1="14" y1="12" x2="20" y2="24" />
      <line x1="26" y1="24" x2="26" y2="106" />
    </g>
    <g fill="var(--muted)" fontFamily="var(--font-mono)" fontSize="7">
      {["e", "B", "G", "D", "A", "E"].map((label, index) => (
        <text key={label} x="2" y={29 + index * 12}>
          {label}
        </text>
      ))}
    </g>
    <g fill="var(--text)" fontFamily="var(--font-mono)" fontSize="9">
      <text x="38" y="54">0</text>
      <text x="62" y="78">2</text>
      <text x="86" y="66">3</text>
      <text x="110" y="42">0</text>
      <text x="134" y="54">2</text>
      <text x="158" y="90">3</text>
      <text x="182" y="66">2</text>
      <text x="206" y="54">0</text>
      <text x="230" y="78">2</text>
      <text x="254" y="42">0</text>
      <text x="278" y="66">3</text>
    </g>
    <g stroke="var(--muted-dim)" strokeWidth="1">
      <line x1="38" y1="38" x2="38" y2="106" strokeDasharray="2 3" />
      <line x1="86" y1="38" x2="86" y2="106" strokeDasharray="2 3" />
      <line x1="134" y1="38" x2="134" y2="106" strokeDasharray="2 3" />
      <line x1="182" y1="38" x2="182" y2="106" strokeDasharray="2 3" />
      <line x1="230" y1="38" x2="230" y2="106" strokeDasharray="2 3" />
      <line x1="278" y1="38" x2="278" y2="106" strokeDasharray="2 3" />
    </g>
  </svg>
);

function Detail({ label, value }: { label: string; value: string }) {
  return (
    <>
      <span class="kv-key">{label}</span>
      <span class="kv-value" title={value}>
        {value}
      </span>
    </>
  );
}

export function RightPanel() {
  const state = useStore();
  const tab: Tab | null = state.selectedTab;
  const job = state.jobs.find((entry) => entry.id === state.selectedJobId) ?? null;

  const title = tab !== null ? text(tab.song) : DASH;
  const artist = tab !== null ? text(tab.artist) : DASH;

  return (
    <aside class="side scroll" aria-label="Track details">
      <Card title="TRACK INFO">
        <div class="cover-row">
          <div
            class="cover"
            style={tab !== null ? { background: gradientFor(tab.song) } : { background: "var(--panel-raised)" }}
            aria-hidden="true"
          >
            {tab !== null ? initials(tab.song) : "—"}
          </div>
          <div class="cover-meta">
            <div class="cover-title" title={title}>
              {title}
            </div>
            <div class="cover-artist" title={artist}>
              {artist}
            </div>
            <div class="cover-stats">
              <span>{tab !== null ? text(tab.type || tab.fileExtension) : DASH}</span>
              <span class="dot-sep">★ {tab !== null ? formatRating(tab.rating) : DASH}</span>
              <span class="dot-sep">👁 {tab !== null ? formatCount(tab.views) : DASH}</span>
            </div>
          </div>
        </div>
        {tab === null && (
          <p class="state-inline" style={{ marginTop: "10px" }}>
            Nothing selected. Pick a track or a queue row to inspect it.
          </p>
        )}
        {job !== null && (
          <div class="cover-stats" style={{ marginTop: "10px" }}>
            <StatusGlyph status={job.status} />
            <span class="mono">{text(job.tag)}</span>
            <span class="dot-sep">{formatBytes(job.bytes)}</span>
          </div>
        )}
      </Card>

      <Card title="PREVIEW">
        <div class="preview-tabs" role="tablist" aria-label="Preview mode">
          <button type="button" class="chip" role="tab" aria-selected="true" aria-pressed="true">
            Guitar Pro
          </button>
          <button
            type="button"
            class="chip"
            role="tab"
            aria-selected="false"
            aria-pressed="false"
            disabled
            title="Static placeholder — a real tab renderer (alphaTab) is not integrated yet"
          >
            Tab
          </button>
        </div>
        <div class="preview-frame">{STAVE}</div>
        <p class="state-inline" style={{ marginTop: "8px" }}>
          Static illustration — no tab renderer is wired up yet.
        </p>
      </Card>

      <Card title="FILE DETAILS">
        <div class="kv">
          <Detail label="Format" value={tab !== null ? text(tab.fileExtension) : DASH} />
          <Detail label="Size" value={tab !== null && tab.fileSize > 0 ? formatBytes(tab.fileSize) : DASH} />
          <Detail label="Tuning" value={tab !== null ? text(tab.tuning) : DASH} />
          <Detail label="Key" value={tab !== null ? text(tab.key) : DASH} />
          <Detail label="Author" value={tab !== null ? text(tab.author) : DASH} />
          <Detail label="Rating" value={tab !== null ? formatRating(tab.rating) : DASH} />
          <Detail label="Views" value={tab !== null ? formatCount(tab.views) : DASH} />
          <Detail label="Votes" value={tab !== null ? formatCount(tab.votes) : DASH} />
        </div>
      </Card>
    </aside>
  );
}
