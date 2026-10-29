// OPTIONS card: format, versions, include groups plus the ADVANCED grid.
// Copy is deliberately explicit about what the worker enforces versus records.

import type { Format } from "../api";
import { Chip, Stepper, Switch } from "./primitives";
import { setOptions, toggleListOption, useStore } from "../store";

const FORMATS: Array<{ value: Format; label: string }> = [
  { value: "gp7", label: "GP7" },
  { value: "gp6", label: "GP6" },
  { value: "gp5", label: "GP5" },
  { value: "gpx", label: "GPX" },
  { value: "gp", label: "GP" },
];

const VERSIONS: Array<{ value: string; label: string }> = [
  { value: "official", label: "Official" },
  { value: "official2", label: "Official alt" },
  { value: "gp-user", label: "GP user" },
  { value: "tabber", label: "Tabber" },
  { value: "all", label: "All" },
];

const INCLUDE: Array<{ value: string; label: string }> = [
  { value: "tabs", label: "Tabs" },
  { value: "bass", label: "Bass" },
  { value: "rhythm", label: "Rhythm" },
  { value: "drums", label: "Drums" },
  { value: "chords", label: "Chords" },
  { value: "backing", label: "Backing" },
  { value: "lyrics", label: "Lyrics" },
  { value: "tone", label: "Tone" },
];

export function OptionsCard() {
  const state = useStore();
  const options = state.options;

  return (
    <section class="card">
      <header class="card-head">
        <h2 class="card-title">OPTIONS</h2>
        <span class="card-head-spacer" />
        <span class="muted" style={{ fontSize: "10.5px" }}>
          applied to newly queued jobs
        </span>
      </header>

      <div class="card-body">
        <div class="option-grid">
          <div class="option-group">
            <div class="option-label" id="opt-format">
              FORMAT
            </div>
            <div class="chip-row" role="radiogroup" aria-labelledby="opt-format">
              {FORMATS.map((format) => (
                <Chip
                  key={format.value}
                  active={options.format === format.value}
                  onClick={() => setOptions({ format: format.value })}
                  reason={`Prefer ${format.label} when saving`}
                >
                  {format.label}
                </Chip>
              ))}
            </div>
          </div>

          <div class="option-group">
            <div class="option-label" id="opt-versions">
              VERSIONS
            </div>
            <div class="chip-row" role="group" aria-labelledby="opt-versions">
              {VERSIONS.map((version) => (
                <Chip
                  key={version.value}
                  active={options.versions.includes(version.value)}
                  onClick={() => toggleListOption("versions", version.value)}
                >
                  {version.label}
                </Chip>
              ))}
            </div>
          </div>

          <div class="option-group">
            <div class="option-label" id="opt-include">
              INCLUDE
            </div>
            <div class="chip-row" role="group" aria-labelledby="opt-include">
              {INCLUDE.map((entry) => (
                <Chip
                  key={entry.value}
                  active={options.include.includes(entry.value)}
                  onClick={() => toggleListOption("include", entry.value)}
                >
                  {entry.label}
                </Chip>
              ))}
            </div>
          </div>
        </div>

        {state.advancedOpen && (
          <div class="advanced-grid">
            <div class="field">
              <label class="field-label" for="opt-quality">
                Max quality
              </label>
              <select
                id="opt-quality"
                value={options.format}
                onChange={(event) => setOptions({ format: event.currentTarget.value as Format })}
              >
                {FORMATS.map((format) => (
                  <option key={format.value} value={format.value}>
                    {format.label}
                  </option>
                ))}
              </select>
            </div>

            <Stepper
              label="Timeout"
              unit="s"
              value={options.timeoutSec}
              min={5}
              max={300}
              step={5}
              onChange={(timeoutSec) => setOptions({ timeoutSec })}
            />
            <Stepper
              label="Retries"
              value={options.retries}
              min={0}
              max={20}
              step={1}
              onChange={(retries) => setOptions({ retries })}
            />
            <Stepper
              label="Delay"
              unit="s"
              value={options.delaySec}
              min={0}
              max={30}
              step={0.5}
              onChange={(delaySec) => setOptions({ delaySec })}
            />

            <div>
              <Switch
                label="Use proxies"
                checked={false}
                disabled
                reason="Not implemented — this research tool has no proxy egress"
                onChange={() => undefined}
              />
              <Switch
                label="Rotate user-agent"
                checked={false}
                disabled
                reason="Not implemented — the worker uses one fixed browser user-agent"
                onChange={() => undefined}
              />
              <Switch
                label="Verify files"
                checked={options.verify}
                hint="Magic-byte check before a file is saved"
                onChange={(verify) => setOptions({ verify })}
              />
            </div>
          </div>
        )}

        <p class="footnote">
          <strong>Honest limits:</strong> format is a <em>preference</em> — the worker biases the
          saved extension, there is no transcoding. VERSIONS and INCLUDE are <em>recorded</em> in the
          sidecar report and do not filter a single-file download. Timeout, retries, delay and verify
          are enforced by the worker.
        </p>
      </div>
    </section>
  );
}
