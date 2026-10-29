// Top band: app mark, mode switch, version chip, theme toggle, CLI helper.

import { DownloadIcon, MoonIcon, SunIcon, TerminalIcon } from "./icons";
import { setMode, toggleTheme, useStore } from "../store";
import { copyText } from "../util";
import { IconButton } from "./primitives";

export function Header() {
  const state = useStore();
  const version = state.version.length > 0 ? state.version : null;
  const config = state.server.data?.config;
  const command =
    config !== undefined
      ? `echoheistv2 --dashboard --worker ${config.workerPath} --download-dir ${config.downloadDir}`
      : "echoheistv2 --dashboard";

  const copyCli = async () => {
    const copied = await copyText(command);
    if (!copied) {
      window.prompt("Copy the CLI command:", command);
    }
  };

  return (
    <header class="topbar">
      <div class="brand">
        <span class="brand-mark">
          <DownloadIcon size={15} />
        </span>
        <span class="brand-text">
          <span class="brand-title">echoheist</span>
          <span class="brand-subtitle">guitar pro downloader</span>
        </span>
      </div>

      <div class="segmented" role="group" aria-label="Transfer mode">
        <button type="button" aria-pressed={state.mode === "single"} onClick={() => setMode("single")}>
          SINGLE DOWNLOAD
        </button>
        <button type="button" aria-pressed={state.mode === "batch"} onClick={() => setMode("batch")}>
          BATCH MODE
        </button>
      </div>

      <span class="topbar-spacer" />

      <div class="topbar-right">
        <span class="version-chip" title="Version reported by GET /api/state">
          echoheist <strong>v{version ?? "—"}</strong>
        </span>
        <IconButton
          label={state.theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}
          onClick={toggleTheme}
        >
          {state.theme === "dark" ? <SunIcon /> : <MoonIcon />}
        </IconButton>
        <IconButton label="Copy CLI command" onClick={() => void copyCli()}>
          <TerminalIcon />
        </IconButton>
      </div>
    </header>
  );
}
