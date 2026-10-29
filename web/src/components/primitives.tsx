// Small, typed presentational primitives shared by the dashboard regions.

import type { ComponentChildren, JSX } from "preact";
import type { JobStatus } from "../api";
import type { Status } from "../store";
import { clamp, classNames } from "../util";
import { AlertIcon, CheckIcon, RetryIcon } from "./icons";

export function Card({
  title,
  actions,
  children,
  class: className,
  bodyClass,
  elevated,
  tight,
}: {
  title?: string;
  actions?: ComponentChildren;
  children: ComponentChildren;
  class?: string;
  bodyClass?: string;
  elevated?: boolean;
  tight?: boolean;
}) {
  return (
    <section class={classNames("card", elevated && "elevated", className)}>
      {title !== undefined && (
        <header class="card-head">
          <h2 class="card-title">{title}</h2>
          <span class="card-head-spacer" />
          {actions}
        </header>
      )}
      <div class={classNames("card-body", tight && "tight", bodyClass)}>{children}</div>
    </section>
  );
}

export function IconButton({
  label,
  onClick,
  children,
  disabled,
  reason,
}: {
  label: string;
  onClick?: () => void;
  children: ComponentChildren;
  disabled?: boolean;
  reason?: string;
}) {
  return (
    <button
      type="button"
      class="icon-btn"
      aria-label={label}
      title={reason ?? label}
      aria-disabled={disabled ? "true" : undefined}
      disabled={disabled}
      onClick={disabled ? undefined : onClick}
    >
      {children}
    </button>
  );
}

export function Chip({
  active,
  onClick,
  children,
  disabled,
  reason,
}: {
  active?: boolean;
  onClick?: () => void;
  children: ComponentChildren;
  disabled?: boolean;
  reason?: string;
}) {
  return (
    <button
      type="button"
      class="chip"
      aria-pressed={active ? "true" : "false"}
      title={reason}
      disabled={disabled}
      onClick={disabled ? undefined : onClick}
    >
      {children}
    </button>
  );
}

export function Switch({
  checked,
  onChange,
  label,
  hint,
  disabled,
  reason,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  label: string;
  hint?: string;
  disabled?: boolean;
  reason?: string;
}) {
  const describedBy = hint ?? reason;
  return (
    <div class="toggle-row">
      <div class="toggle-text">
        <div class={classNames("toggle-name", disabled && "is-disabled")}>{label}</div>
        {describedBy !== undefined && <div class="toggle-hint">{describedBy}</div>}
      </div>
      <button
        type="button"
        class="switch"
        role="switch"
        aria-checked={checked ? "true" : "false"}
        aria-label={label}
        title={reason}
        disabled={disabled}
        onClick={() => {
          if (!disabled) onChange(!checked);
        }}
      />
    </div>
  );
}

export function Stepper({
  label,
  value,
  min,
  max,
  step,
  unit,
  onChange,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step: number;
  unit?: string;
  onChange: (next: number) => void;
}) {
  const round = (input: number) => Math.round(input * 100) / 100;
  const update = (next: number) => onChange(round(clamp(next, min, max)));
  return (
    <div class="field">
      <label class="field-label" for={`step-${label}`}>
        {label}
        {unit !== undefined ? ` (${unit})` : ""}
      </label>
      <div class="stepper">
        <button type="button" aria-label={`Decrease ${label}`} onClick={() => update(value - step)} disabled={value <= min}>
          −
        </button>
        <input
          id={`step-${label}`}
          type="number"
          value={String(value)}
          min={min}
          max={max}
          step={step}
          onChange={(event: JSX.TargetedEvent<HTMLInputElement>) => {
            const parsed = Number(event.currentTarget.value);
            if (Number.isFinite(parsed)) update(parsed);
          }}
        />
        <button type="button" aria-label={`Increase ${label}`} onClick={() => update(value + step)} disabled={value >= max}>
          +
        </button>
      </div>
    </div>
  );
}

const FAILED: JobStatus[] = ["failed", "canceled"];

export function ProgressBar({ value, status }: { value: number; status: JobStatus }) {
  const percent = clamp(Number.isFinite(value) ? value : 0, 0, 100);
  const tone = status === "completed" ? " is-done" : FAILED.includes(status) ? " is-failed" : "";
  return (
    <div class="progress">
      <div class="progress-track" role="progressbar" aria-valuenow={Math.round(percent)} aria-valuemin={0} aria-valuemax={100}>
        <div class={classNames("progress-fill", tone)} style={{ width: `${percent}%` }} />
      </div>
      <span class="progress-value">{Math.round(percent)}%</span>
    </div>
  );
}

export function StatusGlyph({ status }: { status: JobStatus }) {
  if (status === "completed") {
    return (
      <span class="status-glyph is-completed" title="completed" aria-label="completed">
        <CheckIcon />
      </span>
    );
  }
  if (status === "failed") {
    return (
      <span class="status-glyph is-failed" title="failed" aria-label="failed">
        <AlertIcon />
      </span>
    );
  }
  const glyph = status === "running" ? "…" : status === "canceled" ? "×" : "·";
  return (
    <span class={classNames("status-glyph", `is-${status}`)} title={status} aria-label={status}>
      {glyph}
    </span>
  );
}

export function Sparkline({ values }: { values: number[] }) {
  const width = 120;
  const height = 18;
  if (values.length < 2) {
    return (
      <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true" focusable="false">
        <line x1="0" y1={height - 1} x2={width} y2={height - 1} stroke="var(--border)" strokeWidth="1" strokeDasharray="2 3" />
      </svg>
    );
  }
  const step = width / (values.length - 1);
  const points = values
    .map((value, index) => {
      const x = (index * step).toFixed(1);
      const y = (height - (clamp(value, 0, 100) / 100) * (height - 2) - 1).toFixed(1);
      return `${x},${y}`;
    })
    .join(" ");
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden="true" focusable="false">
      <polyline points={points} fill="none" stroke="var(--accent-text)" strokeWidth="1.2" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

/**
 * Uniform empty / loading / error / unsupported presentation. Regions never
 * invent placeholder data: an unavailable endpoint says so explicitly.
 */
export function RegionState({
  status,
  empty,
  emptyText,
  loadingText,
  retry,
  inline,
}: {
  status: Status;
  empty: boolean;
  emptyText: string;
  loadingText?: string;
  retry?: () => void;
  inline?: boolean;
}) {
  if (status === "loading" || status === "idle") {
    if (inline) return <p class="state-inline">{loadingText ?? "Loading…"}</p>;
    return (
      <div class="state" aria-busy="true" aria-live="polite">
        <span class="skeleton" style={{ width: "70%" }} />
        <span class="skeleton" style={{ width: "45%" }} />
        <span class="skeleton" style={{ width: "58%" }} />
        <span class="state-detail">{loadingText ?? "Loading…"}</span>
      </div>
    );
  }

  if (status === "unsupported") {
    return (
      <div class="state" role="status">
        <span class="state-title">Not available on this backend yet</span>
        <span class="state-detail">{emptyText}</span>
      </div>
    );
  }

  if (status === "error") {
    return (
      <div class="state is-error" role="alert">
        <span class="state-title">Could not load this region</span>
        <span class="state-detail">{emptyText}</span>
        {retry !== undefined && (
          <button type="button" class="btn tiny" onClick={retry}>
            <RetryIcon /> Retry
          </button>
        )}
      </div>
    );
  }

  if (empty) {
    if (inline) return <p class="state-inline">{emptyText}</p>;
    return (
      <div class="state">
        <span class="state-detail">{emptyText}</span>
      </div>
    );
  }

  return null;
}
