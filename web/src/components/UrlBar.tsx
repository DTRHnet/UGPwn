// URL entry card: the single-download path plus paste/file helpers.

import { useRef } from "preact/hooks";
import { ChevronIcon, ClipboardIcon, LockIcon, UploadIcon } from "./icons";
import {
  notify,
  queueFromUrl,
  setAdvancedOpen,
  setUrl,
  useStore,
} from "../store";
import { extractUrl, isTabUrl } from "../util";

export function UrlBar() {
  const state = useStore();
  const fileInput = useRef<HTMLInputElement>(null);

  const paste = async () => {
    let clipboard = "";
    try {
      clipboard = await navigator.clipboard.readText();
    } catch {
      notify("error", "Clipboard read was blocked by the browser. Paste with Ctrl+V instead.");
      return;
    }
    const found = extractUrl(clipboard);
    if (!found) {
      notify("error", "No URL found on the clipboard.");
      return;
    }
    setUrl(found);
    if (!isTabUrl(found)) notify("error", "That URL is not an Ultimate Guitar /tab/ page.");
  };

  const loadFile = () => {
    fileInput.current?.click();
  };

  const onFile = async (file: File | undefined) => {
    if (file === undefined) return;
    const content = await file.text();
    const found = extractUrl(content);
    if (!found) {
      notify("error", `No URL found in ${file.name}.`);
      return;
    }
    setUrl(found);
    notify("info", `Loaded URL from ${file.name}.`);
  };

  return (
    <section class="card">
      <div class="url-bar">
        <label class="url-field">
          <LockIcon />
          <span class="sr-only">Tab URL</span>
          <input
            type="url"
            value={state.url}
            spellcheck={false}
            autocomplete="off"
            placeholder="https://tabs.ultimate-guitar.com/tab/<artist>/<song>-<id>"
            aria-label="Ultimate Guitar tab URL"
            onInput={(event) => setUrl(event.currentTarget.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
                event.preventDefault();
                void queueFromUrl();
              }
            }}
          />
        </label>
        <div class="url-actions">
          <button type="button" class="btn primary" onClick={() => void queueFromUrl()}>
            HEIST IT
          </button>
          <button type="button" class="btn" onClick={() => void paste()}>
            <ClipboardIcon /> Paste from clipboard
          </button>
          <button type="button" class="btn" onClick={loadFile}>
            <UploadIcon /> Load from file
          </button>
          <input
            ref={fileInput}
            type="file"
            accept=".txt,.csv,.list,text/plain"
            class="sr-only"
            aria-hidden="true"
            tabIndex={-1}
            onChange={(event) => {
              void onFile(event.currentTarget.files?.[0]);
              event.currentTarget.value = "";
            }}
          />
        </div>
      </div>

      <details
        class="disclosure"
        open={state.advancedOpen}
        onToggle={(event) => setAdvancedOpen(event.currentTarget.open)}
      >
        <summary>
          <ChevronIcon /> Advanced options
          <span class="muted">— reveals the ADVANCED grid in OPTIONS below</span>
        </summary>
      </details>
    </section>
  );
}
