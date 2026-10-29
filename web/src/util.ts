// Small pure helpers shared by the dashboard components.

export const DASH = "—";

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

export function formatBytes(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined || !Number.isFinite(bytes) || bytes <= 0) return DASH;
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value >= 10 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

export function formatCount(count: number | null | undefined): string {
  if (count === null || count === undefined || !Number.isFinite(count)) return DASH;
  if (count < 1000) return String(count);
  if (count < 1_000_000) return `${(count / 1000).toFixed(count < 10_000 ? 1 : 0)}k`;
  return `${(count / 1_000_000).toFixed(1)}M`;
}

/**
 * The contract types `successRate` as a bare number without a stated scale, so
 * accept either a 0..1 fraction or an already-scaled percentage and never invent
 * a value for a non-finite one.
 */
export function formatRate(rate: number | null | undefined): string {
  if (rate === null || rate === undefined || !Number.isFinite(rate)) return DASH;
  const percent = rate <= 1 ? rate * 100 : rate;
  return `${clamp(percent, 0, 100).toFixed(1)}%`;
}

export function formatPercent(value: number | null | undefined): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return DASH;
  return `${Math.round(clamp(value, 0, 100))}%`;
}

export function formatRating(rating: number | null | undefined): string {
  if (rating === null || rating === undefined || !Number.isFinite(rating) || rating <= 0) return DASH;
  return rating.toFixed(1);
}

export function text(value: string | null | undefined): string {
  if (value === null || value === undefined) return DASH;
  const trimmed = value.trim();
  return trimmed.length === 0 ? DASH : trimmed;
}

export function formatTime(iso: string | null | undefined): string {
  if (!iso) return DASH;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime()) || date.getTime() <= 0) return DASH;
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

export function formatRelative(iso: string | null | undefined): string {
  if (!iso) return DASH;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return DASH;
  const seconds = Math.round((Date.now() - date.getTime()) / 1000);
  if (seconds < 5) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)}h ago`;
  return `${Math.floor(seconds / 86_400)}d ago`;
}

export function hashString(value: string): number {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return Math.abs(hash);
}

export function initials(value: string): string {
  const words = value
    .trim()
    .split(/[\s\-_/]+/)
    .filter((word) => word.length > 0);
  if (words.length === 0) return "GP";
  const first = words[0]?.[0] ?? "";
  const last = words.length > 1 ? (words[words.length - 1]?.[0] ?? "") : (words[0]?.[1] ?? "");
  return `${first}${last}`.toUpperCase();
}

/** Deterministic placeholder artwork: same song name always yields the same cover. */
export function gradientFor(seed: string): string {
  const hash = hashString(seed);
  const hue = hash % 360;
  const second = (hue + 38) % 360;
  return `linear-gradient(135deg, hsl(${hue} 48% 26%), hsl(${second} 58% 13%))`;
}

/**
 * Turns a URL slug into display text. Numeric segments are preserved because
 * they are often part of the name ("blink-182", "6-00"); callers strip the
 * trailing tab id themselves since only they know it is an id.
 */
export function humanizeSlug(slug: string): string {
  return slug
    .replace(/-(guitar-pro|tab)$/i, "")
    .split("-")
    .filter((word) => word.length > 0)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(" ");
}

export function extractUrl(raw: string): string | null {
  const match = /https?:\/\/[^\s"'<>]+/i.exec(raw);
  if (match) return match[0];
  const bare = raw.trim();
  if (/^tabs\.ultimate-guitar\.com\//i.test(bare)) return `https://${bare}`;
  return null;
}

export function isTabUrl(raw: string): boolean {
  const candidate = extractUrl(raw);
  if (!candidate) return false;
  try {
    const url = new URL(candidate);
    return (
      url.protocol === "https:" &&
      /(^|\.)ultimate-guitar\.com$/i.test(url.hostname) &&
      /^\/tab\//.test(url.pathname)
    );
  } catch {
    return false;
  }
}

export function classNames(...values: Array<string | false | null | undefined>): string {
  return values.filter((value): value is string => typeof value === "string" && value.length > 0).join(" ");
}

export function shortPath(path: string | null | undefined, segments = 3): string {
  if (!path) return DASH;
  const parts = path.split(/[\\/]/).filter(Boolean);
  if (parts.length <= segments) return path;
  return `…/${parts.slice(-segments).join("/")}`;
}

export async function copyText(value: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(value);
    return true;
  } catch {
    return false;
  }
}

export async function readClipboard(): Promise<string | null> {
  try {
    return await navigator.clipboard.readText();
  } catch {
    return null;
  }
}

export function activeJobs<T extends { status: string }>(jobs: T[]): T[] {
  return jobs.filter((job) => job.status === "queued" || job.status === "running");
}

