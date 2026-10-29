// Inline icon set. Every icon inherits `currentColor` and is decorative unless
// the caller labels the control that wraps it.

import type { JSX } from "preact";

type IconProps = { size?: number; class?: string };

function svg(path: JSX.Element, size: number, className?: string) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      class={className}
    >
      {path}
    </svg>
  );
}

export const DownloadIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M8 2.5v7" />
      <path d="m4.9 6.4 3.1 3.1 3.1-3.1" />
      <path d="M3 12.5h10" />
    </g>,
    size,
    c,
  );

export const QueueIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M3 4.5h10M3 8h10M3 11.5h6" />
    </g>,
    size,
    c,
  );

export const HistoryIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <circle cx="8" cy="8" r="5.4" />
      <path d="M8 5.2V8l2 1.3" />
    </g>,
    size,
    c,
  );

export const StarIcon = ({ size = 14, class: c }: IconProps) =>
  svg(<path d="m8 2.6 1.7 3.5 3.8.5-2.8 2.7.7 3.8L8 11.3 4.6 13.1l.7-3.8-2.8-2.7 3.8-.5z" />, size, c);

export const SourceIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M6.5 9.5 9.5 6.5" />
      <path d="M7.6 4.9l1-1a2.6 2.6 0 0 1 3.7 3.7l-1 1" />
      <path d="M8.4 11.1l-1 1a2.6 2.6 0 0 1-3.7-3.7l1-1" />
    </g>,
    size,
    c,
  );

export const SearchIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <circle cx="7.2" cy="7.2" r="4.2" />
      <path d="m10.4 10.4 2.4 2.4" />
    </g>,
    size,
    c,
  );

export const WrenchIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M9.6 5.1a2.4 2.4 0 0 0 2.6 3.2l-4.7 4.7a1.6 1.6 0 0 1-2.3-2.3z" />
      <path d="m9.6 5.1 1.5-1.5 1.9 1.9-1.5 1.5" />
    </g>,
    size,
    c,
  );

export const ArrowIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M3 8h9" />
      <path d="m8.8 4.8 3.2 3.2-3.2 3.2" />
    </g>,
    size,
    c,
  );

export const EyeIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M1.8 8S4.1 4.2 8 4.2 14.2 8 14.2 8 11.9 11.8 8 11.8 1.8 8 1.8 8Z" />
      <circle cx="8" cy="8" r="1.8" />
    </g>,
    size,
    c,
  );

export const WaveIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M2.5 8h1.2M5.4 5.4v5.2M8 3.6v8.8M10.6 5.4v5.2M13.5 8h-1.2" />
    </g>,
    size,
    c,
  );

export const TranslateIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M2.4 5.2h6M5.4 3.2v2M3.4 5.2c0 2.4 1.8 4.4 4 5.2" />
      <path d="M8.6 12.8 11 6.4l2.4 6.4M9.6 10.8h2.8" />
    </g>,
    size,
    c,
  );

export const SettingsIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <circle cx="8" cy="8" r="2" />
      <path d="M8 2.2v1.4M8 12.4v1.4M2.2 8h1.4M12.4 8h1.4M3.9 3.9l1 1M11.1 11.1l1 1M12.1 3.9l-1 1M4.9 11.1l-1 1" />
    </g>,
    size,
    c,
  );

export const ProfileIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <circle cx="8" cy="5.6" r="2.4" />
      <path d="M3.4 13c.5-2.2 2.4-3.4 4.6-3.4S12.1 10.8 12.6 13" />
    </g>,
    size,
    c,
  );

export const ProxyIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <circle cx="4.4" cy="4.6" r="2" />
      <circle cx="11.6" cy="11.4" r="2" />
      <path d="M4.4 6.6v3.2a1.6 1.6 0 0 0 1.6 1.6h3.6" />
      <path d="M8.4 3.4h2a1.6 1.6 0 0 1 1.6 1.6v4.4" />
    </g>,
    size,
    c,
  );

export const LogsIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M4 2.4h5.2L12 5.2v8.4H4z" />
      <path d="M9.2 2.4v2.8H12" />
      <path d="M6 8.2h4M6 10.6h4" />
    </g>,
    size,
    c,
  );

export const LockIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <rect x="3.4" y="7" width="9.2" height="6.2" rx="1.4" />
      <path d="M5.6 7V5.4a2.4 2.4 0 0 1 4.8 0V7" />
    </g>,
    size,
    c,
  );

export const ClipboardIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <rect x="4" y="3.2" width="8" height="10.4" rx="1.4" />
      <path d="M6.4 3.2V2.4h3.2v.8" />
    </g>,
    size,
    c,
  );

export const UploadIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M8 10.6V3.4" />
      <path d="m5.2 6.2 2.8-2.8 2.8 2.8" />
      <path d="M3 12.2h10" />
    </g>,
    size,
    c,
  );

export const ChevronIcon = ({ size = 12, class: c }: IconProps) =>
  svg(<path d="m6 3.6 4.4 4.4L6 12.4" />, size, c);

export const SunIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <circle cx="8" cy="8" r="2.6" />
      <path d="M8 1.8v1.4M8 12.8v1.4M1.8 8h1.4M12.8 8h1.4M3.6 3.6l1 1M11.4 11.4l1 1M12.4 3.6l-1 1M4.6 11.4l-1 1" />
    </g>,
    size,
    c,
  );

export const MoonIcon = ({ size = 14, class: c }: IconProps) =>
  svg(<path d="M12.8 9.6A5.2 5.2 0 0 1 6.4 3.2a5.4 5.4 0 1 0 6.4 6.4Z" />, size, c);

export const TerminalIcon = ({ size = 14, class: c }: IconProps) =>
  svg(
    <g>
      <rect x="2.2" y="3.4" width="11.6" height="9.2" rx="1.4" />
      <path d="m4.8 6.8 1.8 1.6-1.8 1.6M8.4 10.4h2.6" />
    </g>,
    size,
    c,
  );

export const PlayIcon = ({ size = 12, class: c }: IconProps) =>
  svg(<path d="M5 3.4 12 8l-7 4.6z" fill="currentColor" stroke="none" />, size, c);

export const CloseIcon = ({ size = 12, class: c }: IconProps) =>
  svg(<path d="M4 4l8 8M12 4l-8 8" />, size, c);

export const RetryIcon = ({ size = 12, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M13 8a5 5 0 1 1-1.6-3.7" />
      <path d="M13 2.6v3h-3" />
    </g>,
    size,
    c,
  );

export const FolderIcon = ({ size = 12, class: c }: IconProps) =>
  svg(<path d="M1.8 4.4h4l1.2 1.4h7.2v6.4H1.8z" />, size, c);

export const CheckIcon = ({ size = 10, class: c }: IconProps) =>
  svg(<path d="m3 8.4 3 3 6.4-6.8" stroke-width="1.8" />, size, c);

export const AlertIcon = ({ size = 10, class: c }: IconProps) =>
  svg(
    <g>
      <path d="M8 3.2v5" stroke-width="1.8" />
      <path d="M8 11.4v.6" stroke-width="1.8" />
    </g>,
    size,
    c,
  );
