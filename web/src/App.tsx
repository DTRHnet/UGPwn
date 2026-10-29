// Application shell: header band, three columns, footer, and the global
// keyboard map (/, j/k, Space, Enter, Ctrl+Enter).

import { useEffect } from "preact/hooks";
import type { Tab } from "./api";
import { ConsoleCard } from "./components/ConsoleCard";
import { FavoritesView } from "./components/FavoritesView";
import { Footer } from "./components/Footer";
import { Header } from "./components/Header";
import { HistoryView } from "./components/HistoryView";
import { LogsView } from "./components/LogsView";
import { OptionsCard } from "./components/OptionsCard";
import { QueueCard } from "./components/QueueCard";
import { RightPanel } from "./components/RightPanel";
import { Sidebar } from "./components/Sidebar";
import { SourceBrowser } from "./components/SourceBrowser";
import { UrlBar } from "./components/UrlBar";
import {
  dismissNotice,
  getState,
  initTheme,
  queueFromUrl,
  queueTabs,
  selectJob,
  selectRow,
  selectTab,
  setView,
  startSync,
  toggleTabSelection,
  useStore,
  type AppState,
} from "./store";
import { classNames, clamp } from "./util";

function listTabs(state: AppState): Tab[] {
  switch (state.view) {
    case "favorites":
      return state.favorites.data;
    case "browse":
    case "batch":
    case "search":
      if (state.query.trim().length > 0 && state.search.status !== "idle") {
        return state.search.data.map((hit) => hit.tab);
      }
      return state.browseTabs.data;
    default:
      return [];
  }
}

function focusedTab(state: AppState): Tab | null {
  if (state.view === "downloads") {
    const job = state.jobs[state.rowIndex];
    return job !== undefined ? job.tab : state.selectedTab;
  }
  const tabs = listTabs(state);
  return tabs[state.rowIndex] ?? state.selectedTab;
}

function useKeyboard() {
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target;
      const typing =
        target instanceof HTMLElement && ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
      const state = getState();

      if (event.key === "Escape" && typing) {
        (target as HTMLElement).blur();
        return;
      }
      if (typing) return;

      if (event.key === "/") {
        event.preventDefault();
        if (state.view !== "browse" && state.view !== "batch" && state.view !== "search") {
          setView("browse");
        }
        window.setTimeout(() => {
          const input = document.getElementById("global-search");
          if (input instanceof HTMLInputElement) input.focus();
        }, 0);
        return;
      }

      const count = state.view === "downloads" ? state.jobs.length : listTabs(state).length;

      if (event.key === "j" || event.key === "k") {
        if (count === 0) return;
        event.preventDefault();
        const next = clamp(state.rowIndex + (event.key === "j" ? 1 : -1), 0, count - 1);
        selectRow(next);
        if (state.view === "downloads") {
          const job = state.jobs[next];
          if (job !== undefined) selectJob(job.id);
        }
        return;
      }

      const tab = focusedTab(state);

      if (event.key === " " && tab !== null) {
        event.preventDefault();
        toggleTabSelection(tab);
        selectTab(tab);
        return;
      }

      if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
        event.preventDefault();
        if (tab !== null) void queueTabs([tab]);
        else if (state.url.trim().length > 0) void queueFromUrl();
        return;
      }

      if (event.key === "Enter" && tab !== null && tab.url.length > 0) {
        event.preventDefault();
        window.open(tab.url, "_blank", "noopener");
      }
    };

    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

function Center() {
  const state = useStore();

  switch (state.view) {
    case "downloads":
      return (
        <>
          <UrlBar />
          <OptionsCard />
          <QueueCard rowIndex={state.rowIndex} onFocusRow={selectRow} />
          <ConsoleCard />
        </>
      );
    case "batch":
      return (
        <>
          <SourceBrowser mode="batch" rowIndex={state.rowIndex} onFocusRow={selectRow} />
          <QueueCard rowIndex={state.rowIndex} onFocusRow={selectRow} />
          <ConsoleCard />
        </>
      );
    case "browse":
    case "search":
      return (
        <>
          <SourceBrowser mode="browse" rowIndex={state.rowIndex} onFocusRow={selectRow} />
          <QueueCard rowIndex={state.rowIndex} onFocusRow={selectRow} />
          <ConsoleCard />
        </>
      );
    case "history":
      return (
        <>
          <HistoryView />
          <ConsoleCard />
        </>
      );
    case "favorites":
      return (
        <>
          <FavoritesView rowIndex={state.rowIndex} />
          <ConsoleCard />
        </>
      );
    case "logs":
      return (
        <>
          <LogsView />
          <ConsoleCard />
        </>
      );
    default:
      return null;
  }
}

export function App() {
  const state = useStore();

  useEffect(() => {
    initTheme();
    return startSync();
  }, []);

  useKeyboard();

  return (
    <div class="shell">
      <Header />
      <div class="workspace">
        <Sidebar />
        <main class="center scroll" aria-label="Workspace">
          {state.notice !== null && (
            <div class={classNames("notice", state.notice.kind === "error" && "is-error")} role="status">
              <span class="notice-text">{state.notice.text}</span>
              <button type="button" class="btn tiny ghost" onClick={dismissNotice} aria-label="Dismiss message">
                ✕
              </button>
            </div>
          )}
          <Center />
        </main>
        <RightPanel />
      </div>
      <Footer />
    </div>
  );
}
