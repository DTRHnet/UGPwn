// Typed client for the echoheist dashboard API (see ../../CONTRACT.md).
// Every request carries the run token from the served <meta> tag.

export type Tab = {
  id: number;
  songId: number;
  artistId: number;
  song: string;
  artist: string;
  type: string;
  url: string;
  author: string;
  fileExtension: string;
  fileSize: number;
  rating: number;
  votes: number;
  difficulty: string;
  tuning: string;
  key: string;
  description: string;
  date: string;
  views: number;
  favorites: number;
};

export type Format = "gp7" | "gp6" | "gp5" | "gpx" | "gp";

export type Options = {
  format: Format;
  versions: string[];
  include: string[];
  timeoutSec: number;
  retries: number;
  delaySec: number;
  verify: boolean;
};

export type JobStatus = "queued" | "running" | "completed" | "failed" | "canceled";

export type Job = {
  id: string;
  tab: Tab;
  status: JobStatus;
  message?: string;
  path?: string;
  bytes: number;
  progress: number;
  source: string;
  tag: string;
  startedAt: string;
  endedAt?: string | null;
};

export type LogLevel = "info" | "warn" | "error" | "debug";

export type LogLine = {
  seq: number;
  jobId: string;
  level: LogLevel;
  text: string;
  at: string;
};

export type Source = { id: string; name: string; enabled: boolean; reason?: string };

export type Metrics = { totalDownloads: number; successRate: number; bytes: number };

export type Counts = { queued: number; running: number; completed: number; failed: number };

export type Config = { downloadDir: string; workerPath: string; logDir: string };

export type State = {
  version: string;
  config: Config;
  jobs: Job[];
  counts: Counts;
  metrics: Metrics;
  sources: Source[];
  options: Options;
};

export type Artist = { id: number; name: string; url: string; tabCount: number };

export type JobsResponse = { jobs: Job[]; logs: LogLine[]; cursor: number };

export type ArtistsResponse = { items: Artist[]; source: string };

export type TabsResponse = { items: Tab[]; source: string };

export type SearchHit = { artist: Artist; tab: Tab };

export type SearchResponse = {
  items: SearchHit[];
  artists: Artist[];
  count: number;
  artistCount: number;
};

export type LogFile = { name: string; size: number; modified: string };

export type LogFileResponse = { name: string; lines: string[] };

export const DEFAULT_OPTIONS: Options = {
  format: "gp7",
  versions: ["official", "gp-user"],
  include: ["tabs"],
  timeoutSec: 30,
  retries: 5,
  delaySec: 1.5,
  verify: true,
};

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

/** True when the backend simply has not implemented this route yet. */
export function isUnavailable(error: unknown): boolean {
  return error instanceof ApiError && (error.status === 404 || error.status === 501);
}

export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return String(error);
}

function readToken(): string {
  if (typeof document === "undefined") return "";
  const meta = document.querySelector('meta[name="echoheist-token"]');
  return meta?.getAttribute("content") ?? "";
}

const TOKEN = readToken();

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("X-Echoheist-Token", TOKEN);
  if (init.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  let response: Response;
  try {
    response = await fetch(`/api${path}`, {
      ...init,
      headers,
      credentials: "same-origin",
      cache: "no-store",
    });
  } catch (cause) {
    throw new ApiError(0, `Network error contacting the dashboard: ${errorMessage(cause)}`);
  }

  const raw = await response.text();
  let payload: unknown = null;
  if (raw.length > 0) {
    try {
      payload = JSON.parse(raw) as unknown;
    } catch {
      payload = null;
    }
  }

  if (!response.ok) {
    const detail =
      payload !== null && typeof payload === "object" && "error" in payload
        ? String((payload as { error: unknown }).error)
        : `HTTP ${response.status} ${response.statusText}`.trim();
    throw new ApiError(response.status, detail);
  }

  return payload as T;
}

const json = (body: unknown): RequestInit => ({ method: "POST", body: JSON.stringify(body) });

export const api = {
  state: () => request<State>("/state"),

  jobsSince: (since: number) => request<JobsResponse>(`/jobs?since=${since}`),

  queue: (tabs: Tab[], options?: Options) =>
    request<{ ids: string[] }>("/jobs", json(options ? { tabs, options } : { tabs })),

  cancel: (id: string) =>
    request<{ ok: true }>(`/jobs/${encodeURIComponent(id)}`, { method: "DELETE" }),

  retry: (id: string) =>
    request<{ id: string }>(`/jobs/${encodeURIComponent(id)}/retry`, { method: "POST" }),

  clear: () => request<{ removed: number }>("/jobs/clear", { method: "POST" }),

  history: (limit = 100) => request<{ items: Job[] }>(`/history?limit=${limit}`),

  favorites: () => request<{ items: Tab[] }>("/favorites"),

  addFavorite: (tab: Tab) => request<{ ok: true }>("/favorites", json({ tab })),

  removeFavorite: (id: number) =>
    request<{ ok: true }>(`/favorites/${id}`, { method: "DELETE" }),

  sources: () => request<{ items: Source[] }>("/sources"),

  logFiles: () => request<{ files: LogFile[] }>("/logs"),

  logFile: (name: string) =>
    request<LogFileResponse>(`/logs/${encodeURIComponent(name)}`),

  artists: () => request<ArtistsResponse>("/artists"),

  artistsByLetter: (letter: string) =>
    request<ArtistsResponse>(`/artists?letter=${encodeURIComponent(letter)}`),

  artistTabs: (id: number, refresh = false) =>
    request<TabsResponse>(`/artists/${id}${refresh ? "?refresh=1" : ""}`),

  fetchArtists: (letter: string) =>
    request<ArtistsResponse>("/fetch/artists", json({ letter })),

  fetchTabs: (artistId: number) =>
    request<TabsResponse>("/fetch/tabs", json({ artistId })),

  search: (q: string, letter = "") =>
    request<SearchResponse>(
      `/search?q=${encodeURIComponent(q)}&letter=${encodeURIComponent(letter)}`,
    ),

  config: () => request<Config>("/config"),

  saveConfig: (patch: { downloadDir?: string; logDir?: string }) =>
    request<Config>("/config", { method: "PUT", body: JSON.stringify(patch) }),

  health: () => request<{ ok: boolean; time: string; jobs: number }>("/health"),
};
