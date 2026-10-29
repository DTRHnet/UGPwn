// Application state: a small typed store (no signals package is installed) with
// actions for every dashboard flow. Components subscribe through `useStore`.

import { useEffect, useState } from "preact/hooks";
import {
  api,
  ApiError,
  DEFAULT_OPTIONS,
  errorMessage,
  isUnavailable,
  type Artist,
  type Counts,
  type Job,
  type LogFile,
  type LogLine,
  type Metrics,
  type Options,
  type SearchHit,
  type Source,
  type State,
  type Tab,
} from "./api";
import { extractUrl, humanizeSlug, isTabUrl } from "./util";

export type View = "downloads" | "batch" | "browse" | "search" | "history" | "favorites" | "logs";
export type Status = "idle" | "loading" | "ready" | "error" | "unsupported";
export type Loadable<T> = { status: Status; data: T; error: string | null };
export type Theme = "dark" | "light";
export type Mode = "single" | "batch";

export const idleState = <T,>(data: T): Loadable<T> => ({ status: "idle", data, error: null });
export const loadingState = <T,>(data: T): Loadable<T> => ({ status: "loading", data, error: null });
export const readyState = <T,>(data: T): Loadable<T> => ({ status: "ready", data, error: null });

const EMPTY_COUNTS: Counts = { queued: 0, running: 0, completed: 0, failed: 0 };
const EMPTY_METRICS: Metrics = { totalDownloads: 0, successRate: 0, bytes: 0 };

export type AppState = {
  view: View;
  mode: Mode;
  theme: Theme;

  server: Loadable<State | null>;
  version: string;
  jobs: Job[];
  /** Availability of GET /api/jobs, tracked separately from /api/state. */
  jobsStatus: Status;
  counts: Counts;
  metrics: Metrics;
  sources: Source[];
  options: Options;
  optionsDirty: boolean;

  logs: LogLine[];
  cursor: number;
  logFiles: Loadable<LogFile[]>;
  openLog: { name: string; lines: string[] } | null;

  url: string;
  advancedOpen: boolean;
  rowIndex: number;
  selectedJobId: string | null;
  selectedTab: Tab | null;
  selectedTabs: Tab[];

  favorites: Loadable<Tab[]>;
  history: Loadable<Job[]>;

  letter: string;
  artists: Loadable<Artist[]>;
  activeArtist: Artist | null;
  browseTabs: Loadable<Tab[]>;
  query: string;
  search: Loadable<SearchHit[]>;

  notice: { kind: "info" | "error"; text: string } | null;
};

function initialTheme(): Theme {
  if (typeof localStorage !== "undefined") {
    const stored = localStorage.getItem("echoheist-theme");
    if (stored === "light" || stored === "dark") return stored;
  }
  return "dark";
}

function initialState(): AppState {
  return {
    view: "downloads",
    mode: "single",
    theme: initialTheme(),

    server: idleState<State | null>(null),
    version: "",
    jobs: [],
    jobsStatus: "idle",
    counts: EMPTY_COUNTS,
    metrics: EMPTY_METRICS,
    sources: [],
    options: { ...DEFAULT_OPTIONS },
    optionsDirty: false,

    logs: [],
    cursor: 0,
    logFiles: idleState<LogFile[]>([]),
    openLog: null,

    url: "",
    advancedOpen: false,
    rowIndex: 0,
    selectedJobId: null,
    selectedTab: null,
    selectedTabs: [],

    favorites: idleState<Tab[]>([]),
    history: idleState<Job[]>([]),

    letter: "A",
    artists: idleState<Artist[]>([]),
    activeArtist: null,
    browseTabs: idleState<Tab[]>([]),
    query: "",
    search: idleState<SearchHit[]>([]),

    notice: null,
  };
}

type Listener = (state: AppState) => void;

let current: AppState = initialState();
const listeners = new Set<Listener>();

export function getState(): AppState {
  return current;
}

export function patch(delta: Partial<AppState>): void {
  current = { ...current, ...delta };
  for (const listener of listeners) listener(current);
}

export function useStore(): AppState {
  const [snapshot, setSnapshot] = useState<AppState>(current);
  useEffect(() => {
    const listener: Listener = (state) => setSnapshot(state);
    listeners.add(listener);
    listener(current);
    return () => {
      listeners.delete(listener);
    };
  }, []);
  return snapshot;
}

/* ------------------------------------------------------------------ helpers */

function notice(kind: "info" | "error", text: string): void {
  patch({ notice: { kind, text } });
}

export function dismissNotice(): void {
  patch({ notice: null });
}

function applyTheme(theme: Theme): void {
  if (typeof document !== "undefined") {
    document.documentElement.dataset.theme = theme;
  }
  if (typeof localStorage !== "undefined") {
    localStorage.setItem("echoheist-theme", theme);
  }
}

export function initTheme(): void {
  applyTheme(current.theme);
}

export function toggleTheme(): void {
  const theme: Theme = current.theme === "dark" ? "light" : "dark";
  applyTheme(theme);
  patch({ theme });
}

function isActive(job: Job): boolean {
  return job.status === "queued" || job.status === "running";
}

export function hasActiveJobs(state: AppState = current): boolean {
  return state.jobs.some(isActive);
}

/* --------------------------------------------------------------- data flows */

export async function refreshState(): Promise<void> {
  try {
    const state = await api.state();
    patch({
      server: readyState(state),
      version: state.version || "",
      jobs: state.jobs ?? [],
      // /api/state carries the job list too, so the queue is readable while idle
      // even though only /api/jobs streams log lines.
      jobsStatus: "ready",
      counts: state.counts ?? EMPTY_COUNTS,
      metrics: state.metrics ?? EMPTY_METRICS,
      sources: state.sources ?? [],
      options: current.optionsDirty ? current.options : (state.options ?? current.options),
    });
  } catch (error) {
    if (isUnavailable(error)) {
      patch({
        server: {
          status: "unsupported",
          data: current.server.data,
          error: "GET /api/state is not available on this build yet.",
        },
      });
      return;
    }
    patch({
      server: { status: "error", data: current.server.data, error: errorMessage(error) },
    });
  }
}

export async function pollJobs(): Promise<void> {
  try {
    const response = await api.jobsSince(current.cursor);
    const known = new Set(current.logs.map((line) => line.seq));
    const fresh = (response.logs ?? []).filter((line) => !known.has(line.seq));
    const jobs = response.jobs ?? [];
    const counts: Counts = {
      queued: jobs.filter((job) => job.status === "queued").length,
      running: jobs.filter((job) => job.status === "running").length,
      completed: jobs.filter((job) => job.status === "completed").length,
      failed: jobs.filter((job) => job.status === "failed").length,
    };
    patch({
      jobs,
      jobsStatus: "ready",
      counts,
      logs: fresh.length > 0 ? [...current.logs, ...fresh].slice(-500) : current.logs,
      cursor: typeof response.cursor === "number" ? response.cursor : current.cursor,
    });
  } catch (error) {
    if (isUnavailable(error)) {
      patch({ jobsStatus: "unsupported" });
      return;
    }
    patch({ jobsStatus: "error" });
    notice("error", `Polling failed: ${errorMessage(error)}`);
  }
}

export async function queueTabs(tabs: Tab[], options?: Options): Promise<void> {
  if (tabs.length === 0) {
    notice("error", "Nothing selected to queue.");
    return;
  }
  try {
    const response = await api.queue(tabs, options ?? current.options);
    notice("info", `Queued ${tabs.length} track${tabs.length === 1 ? "" : "s"} (${response.ids.length} job${response.ids.length === 1 ? "" : "s"}).`);
    patch({ selectedTabs: [] });
    await pollJobs();
  } catch (error) {
    if (isUnavailable(error)) {
      notice("error", "POST /api/jobs is not available on this backend yet.");
      return;
    }
    notice("error", `Could not queue: ${errorMessage(error)}`);
  }
}

/**
 * Builds a queue entry from a bare URL. Only the URL is authoritative: numeric
 * ids and metadata stay empty/zero so the UI shows "—" rather than inventing
 * track data the backend has not resolved yet.
 */
export function tabFromUrl(raw: string): Tab | null {
  const candidate = extractUrl(raw);
  if (!candidate || !isTabUrl(candidate)) return null;
  const url = new URL(candidate);
  const segments = url.pathname.split("/").filter(Boolean);
  const artistSlug = segments[1] ?? "";
  const songSlug = segments[2] ?? "";
  // The tab id is the trailing number of the song slug; strip it before display
  // so "stairway-to-heaven-guitar-pro-223796" reads as "Stairway To Heaven".
  const idMatch = /-(\d+)$/.exec(songSlug);
  const id = idMatch !== null && idMatch[1] !== undefined ? Number(idMatch[1]) : 0;
  const songName = humanizeSlug(
    idMatch !== null && idMatch.index !== undefined ? songSlug.slice(0, idMatch.index) : songSlug,
  );
  // An id-only artist segment carries no name, so leave it blank instead of
  // rendering a bare number as an artist.
  const artistName = /^\d+$/.test(artistSlug) ? "" : humanizeSlug(artistSlug);
  return {
    id,
    songId: 0,
    artistId: 0,
    song: songName,
    artist: artistName,
    type: "",
    url: candidate,
    author: "",
    fileExtension: "",
    fileSize: 0,
    rating: 0,
    votes: 0,
    difficulty: "",
    tuning: "",
    key: "",
    description: "",
    date: "",
    views: 0,
    favorites: 0,
  };
}

export async function queueFromUrl(): Promise<void> {
  const raw = current.url.trim();
  if (raw.length === 0) {
    notice("error", "Paste an Ultimate Guitar tab URL first.");
    return;
  }
  if (!isTabUrl(raw)) {
    notice("error", "That is not a tabs.ultimate-guitar.com/tab/ URL.");
    return;
  }
  const tab = tabFromUrl(raw);
  if (!tab) {
    notice("error", "Could not read a tab URL from that input.");
    return;
  }
  patch({ selectedTab: tab });
  await queueTabs([tab]);
}

export async function cancelJob(id: string): Promise<void> {
  try {
    await api.cancel(id);
    notice("info", "Job canceled.");
    await pollJobs();
  } catch (error) {
    notice("error", `Could not cancel: ${errorMessage(error)}`);
  }
}

export async function retryJob(id: string): Promise<void> {
  try {
    const response = await api.retry(id);
    notice("info", `Retrying as ${response.id}.`);
    await pollJobs();
  } catch (error) {
    notice("error", `Could not retry: ${errorMessage(error)}`);
  }
}

export async function clearFinishedJobs(): Promise<void> {
  try {
    const response = await api.clear();
    notice("info", `Removed ${response.removed} finished job${response.removed === 1 ? "" : "s"}.`);
    await pollJobs();
  } catch (error) {
    if (isUnavailable(error)) {
      notice("error", "POST /api/jobs/clear is not available on this backend yet.");
      return;
    }
    notice("error", `Could not clear: ${errorMessage(error)}`);
  }
}

export async function loadHistory(): Promise<void> {
  patch({ history: loadingState(current.history.data) });
  try {
    const response = await api.history(100);
    patch({ history: readyState(response.items ?? []) });
  } catch (error) {
    patch({
      history: {
        status: isUnavailable(error) ? "unsupported" : "error",
        data: [],
        error: errorMessage(error),
      },
    });
  }
}

export async function loadFavorites(): Promise<void> {
  patch({ favorites: loadingState(current.favorites.data) });
  try {
    const response = await api.favorites();
    patch({ favorites: readyState(response.items ?? []) });
  } catch (error) {
    patch({
      favorites: {
        status: isUnavailable(error) ? "unsupported" : "error",
        data: [],
        error: errorMessage(error),
      },
    });
  }
}

export async function toggleFavorite(tab: Tab): Promise<void> {
  const isSaved = current.favorites.data.some((item) => item.id === tab.id);
  try {
    if (isSaved) {
      await api.removeFavorite(tab.id);
      patch({ favorites: readyState(current.favorites.data.filter((item) => item.id !== tab.id)) });
      notice("info", `Removed ${tab.song || "track"} from favorites.`);
    } else {
      await api.addFavorite(tab);
      patch({ favorites: readyState([tab, ...current.favorites.data]) });
      notice("info", `Saved ${tab.song || "track"} to favorites.`);
    }
  } catch (error) {
    notice("error", `Favorites: ${errorMessage(error)}`);
  }
}

export async function loadLogFiles(): Promise<void> {
  patch({ logFiles: loadingState(current.logFiles.data) });
  try {
    const response = await api.logFiles();
    patch({ logFiles: readyState(response.files ?? []) });
  } catch (error) {
    patch({
      logFiles: {
        status: isUnavailable(error) ? "unsupported" : "error",
        data: [],
        error: errorMessage(error),
      },
    });
  }
}

export async function openLogFile(name: string): Promise<void> {
  try {
    const response = await api.logFile(name);
    patch({ openLog: { name: response.name, lines: response.lines ?? [] } });
  } catch (error) {
    notice("error", `Could not read ${name}: ${errorMessage(error)}`);
  }
}

export async function fetchLetter(letter: string): Promise<void> {
  patch({ letter, artists: loadingState(current.artists.data) });
  try {
    const response = await api.fetchArtists(letter);
    patch({
      artists: readyState(response.items ?? []),
      activeArtist: null,
      browseTabs: idleState<Tab[]>([]),
    });
  } catch (error) {
    patch({
      artists: {
        status: isUnavailable(error) ? "unsupported" : "error",
        data: [],
        error: errorMessage(error),
      },
    });
  }
}

export async function openArtist(artist: Artist): Promise<void> {
  patch({ activeArtist: artist, browseTabs: loadingState<Tab[]>([]), selectedTab: null });
  try {
    const response = await api.fetchTabs(artist.id);
    patch({ browseTabs: readyState(response.items ?? []) });
  } catch (error) {
    patch({
      browseTabs: {
        status: isUnavailable(error) ? "unsupported" : "error",
        data: [],
        error: errorMessage(error),
      },
    });
  }
}

export async function runSearch(query: string): Promise<void> {
  patch({ query, search: loadingState(current.search.data) });
  try {
    const response = await api.search(query, current.letter);
    patch({ search: readyState(response.items ?? []) });
  } catch (error) {
    patch({
      search: {
        status: isUnavailable(error) ? "unsupported" : "error",
        data: [],
        error: errorMessage(error),
      },
    });
  }
}

/* ------------------------------------------------------------------ mutators */

export function setView(view: View): void {
  patch({ view, rowIndex: 0 });
  if (view === "history" && current.history.status !== "ready") void loadHistory();
  if (view === "favorites" && current.favorites.status !== "ready") void loadFavorites();
  if (view === "logs" && current.logFiles.status !== "ready") void loadLogFiles();
}

export function setMode(mode: Mode): void {
  patch({ mode });
}

export function setUrl(url: string): void {
  patch({ url });
}

export function setAdvancedOpen(open: boolean): void {
  patch({ advancedOpen: open });
}

export function setOptions(delta: Partial<Options>): void {
  patch({ options: { ...current.options, ...delta }, optionsDirty: true });
}

export function toggleListOption(field: "versions" | "include", value: string): void {
  const values = current.options[field];
  const next = values.includes(value) ? values.filter((item) => item !== value) : [...values, value];
  setOptions({ [field]: next } as Partial<Options>);
}

export function setQuery(query: string): void {
  patch({ query });
}

export function selectRow(index: number): void {
  patch({ rowIndex: index });
}

export function selectTab(tab: Tab | null): void {
  patch({ selectedTab: tab });
}

export function selectJob(id: string | null): void {
  const job = current.jobs.find((item) => item.id === id) ?? null;
  patch({ selectedJobId: id, selectedTab: job ? job.tab : current.selectedTab });
}

export function toggleTabSelection(tab: Tab): void {
  const exists = current.selectedTabs.some((item) => item.id === tab.id && item.url === tab.url);
  patch({
    selectedTabs: exists
      ? current.selectedTabs.filter((item) => !(item.id === tab.id && item.url === tab.url))
      : [...current.selectedTabs, tab],
  });
}

export function clearTabSelection(): void {
  patch({ selectedTabs: [] });
}

/* ------------------------------------------------------------------ polling */

let timer: number | undefined;
let running = false;

export function startSync(): () => void {
  if (running) return () => undefined;
  running = true;

  const tick = async (): Promise<void> => {
    if (hasActiveJobs()) await pollJobs();
    else await refreshState();

    const degraded = current.server.status === "unsupported";
    const delay = hasActiveJobs() ? 1000 : degraded ? 5000 : 2000;
    timer = window.setTimeout(() => void tick(), delay);
  };

  void tick();

  return () => {
    running = false;
    if (timer !== undefined) window.clearTimeout(timer);
  };
}

export function apiErrorStatus(error: unknown): number {
  return error instanceof ApiError ? error.status : 0;
}

/** Public wrapper so components can surface a transient message. */
export function notify(kind: "info" | "error", text: string): void {
  notice(kind, text);
}

/** Clears the local console buffer; the server-side log stays untouched. */
export function clearConsole(): void {
  patch({ logs: [], openLog: null });
}
