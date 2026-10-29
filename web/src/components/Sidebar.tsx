// Left rail: grouped navigation. Entries with no backend are rendered disabled
// with the reason in both `title` and `aria-disabled`, so nothing looks live
// while doing nothing.

import type { JSX } from "preact";
import {
  ArrowIcon,
  DownloadIcon,
  EyeIcon,
  HistoryIcon,
  LogsIcon,
  ProfileIcon,
  ProxyIcon,
  QueueIcon,
  SearchIcon,
  SettingsIcon,
  SourceIcon,
  StarIcon,
  TranslateIcon,
  WaveIcon,
  WrenchIcon,
} from "./icons";
import { setView, useStore, type View } from "../store";
import { classNames } from "../util";

type NavItem = {
  id: string;
  label: string;
  icon: JSX.Element;
  view?: View;
  badge?: number;
  disabled?: boolean;
  reason?: string;
};

const NOT_IMPLEMENTED = "Not implemented — outside the scope of this research tool";

export function Sidebar() {
  const state = useStore();
  const active = state.counts.queued + state.counts.running;
  const sourcesKnown = state.sources.length > 0;

  const sourceItems: NavItem[] = [
    {
      id: "ultimate-guitar",
      label: "Ultimate Guitar",
      icon: <SourceIcon />,
      view: "browse",
    },
    ...(sourcesKnown
      ? state.sources
          .filter((source) => source.id !== "ultimate-guitar")
          .map<NavItem>((source) => ({
            id: source.id,
            label: source.name,
            icon: <SourceIcon />,
            disabled: !source.enabled,
            reason: source.enabled ? undefined : (source.reason ?? NOT_IMPLEMENTED),
          }))
      : [
          { id: "songsterr", label: "Songsterr", icon: <SourceIcon />, disabled: true, reason: NOT_IMPLEMENTED },
          { id: "guitar-pro-tabs", label: "Guitar Pro Tabs", icon: <SourceIcon />, disabled: true, reason: NOT_IMPLEMENTED },
          { id: "mysongbook", label: "MySongBook", icon: <SourceIcon />, disabled: true, reason: NOT_IMPLEMENTED },
          { id: "musescore", label: "Musescore", icon: <SourceIcon />, disabled: true, reason: NOT_IMPLEMENTED },
        ]),
    { id: "custom-scrapers", label: "Custom Scrapers", icon: <WrenchIcon />, disabled: true, reason: NOT_IMPLEMENTED },
    { id: "cookies-auth", label: "Cookies & Auth", icon: <SettingsIcon />, disabled: true, reason: NOT_IMPLEMENTED },
  ];

  const groups: Array<{ heading: string; items: NavItem[] }> = [
    {
      heading: "CORE",
      items: [
        { id: "downloader", label: "Downloader", icon: <DownloadIcon />, view: "downloads" },
        { id: "batch", label: "Batch Queue", icon: <QueueIcon />, view: "batch", badge: active > 0 ? active : undefined },
        { id: "history", label: "History", icon: <HistoryIcon />, view: "history" },
        { id: "favorites", label: "Favorites", icon: <StarIcon />, view: "favorites" },
      ],
    },
    { heading: "SOURCES", items: sourceItems },
    {
      heading: "TOOLS",
      items: [
        { id: "search-engine", label: "Search Engine", icon: <SearchIcon />, view: "search" },
        { id: "metadata-fixer", label: "Metadata Fixer", icon: <WrenchIcon />, disabled: true, reason: NOT_IMPLEMENTED },
        { id: "gpx-max", label: "GPX → GP MAX", icon: <ArrowIcon />, disabled: true, reason: NOT_IMPLEMENTED },
        { id: "gp-viewer", label: "GP Viewer", icon: <EyeIcon />, disabled: true, reason: NOT_IMPLEMENTED },
        { id: "audio-to-tab", label: "Audio to Tab", icon: <WaveIcon />, disabled: true, reason: NOT_IMPLEMENTED },
        { id: "tab-translator", label: "Tab Translator", icon: <TranslateIcon />, disabled: true, reason: NOT_IMPLEMENTED },
      ],
    },
    {
      heading: "SETTINGS",
      items: [
        { id: "settings", label: "Settings", icon: <SettingsIcon />, disabled: true, reason: NOT_IMPLEMENTED },
        { id: "profiles", label: "Profiles", icon: <ProfileIcon />, disabled: true, reason: NOT_IMPLEMENTED },
        { id: "proxies", label: "Proxies", icon: <ProxyIcon />, disabled: true, reason: `${NOT_IMPLEMENTED} (flaresolverr-style egress is not part of this tool)` },
        { id: "logs", label: "Logs", icon: <LogsIcon />, view: "logs" },
      ],
    },
  ];

  return (
    <nav class="rail scroll" aria-label="Primary">
      {groups.map((group) => (
        <div class="rail-group" key={group.heading}>
          <h3 class="rail-heading">{group.heading}</h3>
          {group.items.map((item) => {
            const isCurrent = item.view !== undefined && item.view === state.view;
            const disabled = item.disabled === true || item.view === undefined;
            return (
              <button
                key={item.id}
                type="button"
                class={classNames("rail-item", isCurrent && "is-active")}
                aria-current={isCurrent ? "page" : undefined}
                aria-disabled={disabled ? "true" : undefined}
                disabled={disabled}
                title={disabled ? (item.reason ?? NOT_IMPLEMENTED) : item.label}
                onClick={() => {
                  if (!disabled && item.view !== undefined) setView(item.view);
                }}
              >
                {item.icon}
                <span class="rail-label">{item.label}</span>
                {item.badge !== undefined && <span class="rail-badge">{item.badge}</span>}
              </button>
            );
          })}
        </div>
      ))}
    </nav>
  );
}
