// SOURCES browser: letter → artist → tabs, plus cached search. Used by the
// Downloader's Batch mode and by the Ultimate Guitar / Search Engine nav items.

import { useRef } from "preact/hooks";
import {
  clearTabSelection,
  fetchLetter,
  openArtist,
  queueTabs,
  runSearch,
  setQuery,
  useStore,
} from "../store";
import { classNames } from "../util";
import { DownloadIcon, SearchIcon } from "./icons";
import { Card, RegionState } from "./primitives";
import { TrackList } from "./TrackList";

const LETTERS = ["0-9", ..."ABCDEFGHIJKLMNOPQRSTUVWXYZ".split("")];

export function SourceBrowser({
  mode,
  rowIndex,
  onFocusRow,
}: {
  mode: "browse" | "batch";
  rowIndex: number;
  onFocusRow: (index: number) => void;
}) {
  const state = useStore();
  const searchInput = useRef<HTMLInputElement>(null);

  const searching = state.query.trim().length > 0 && state.search.status !== "idle";
  const tabs = searching ? state.search.data.map((hit) => hit.tab) : state.browseTabs.data;
  const tabsStatus = searching ? state.search.status : state.browseTabs.status;
  const selection = state.selectedTabs;

  return (
    <Card
      title={mode === "batch" ? "BATCH SOURCE" : "ULTIMATE GUITAR"}
      actions={
        <>
          <button
            type="button"
            class="btn tiny"
            onClick={() => void fetchLetter(state.letter)}
            title={`Fetch the ${state.letter} artist list live`}
          >
            Fetch letter
          </button>
          <button
            type="button"
            class="btn tiny"
            onClick={() => void runSearch(state.query)}
            disabled={state.query.trim().length === 0}
            title="Search cached tabs and artists"
          >
            <SearchIcon size={12} /> Search
          </button>
        </>
      }
      bodyClass="tight"
    >
      <div class="toolbar" style={{ marginBottom: "9px" }}>
        <div class="letter-row" role="group" aria-label="Artist letter">
          {LETTERS.map((letter) => (
            <button
              key={letter}
              type="button"
              class="letter-btn"
              aria-pressed={state.letter === letter ? "true" : "false"}
              onClick={() => void fetchLetter(letter)}
            >
              {letter}
            </button>
          ))}
        </div>
      </div>

      <label class="search-field" style={{ marginBottom: "9px" }}>
        <SearchIcon />
        <span class="sr-only">Search</span>
        <input
          id="global-search"
          ref={searchInput}
          type="search"
          value={state.query}
          placeholder="Search cached songs, tuning, difficulty…  (press / to focus)"
          onInput={(event) => setQuery(event.currentTarget.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              void runSearch(event.currentTarget.value);
            }
          }}
        />
        {searching && (
          <button
            type="button"
            class="btn tiny ghost"
            onClick={() => {
              setQuery("");
              searchInput.current?.focus();
            }}
            title="Clear the search filter"
          >
            clear
          </button>
        )}
      </label>

      <div class="split">
        <div class="split-left">
          {state.artists.status !== "ready" || state.artists.data.length === 0 ? (
            <RegionState
              status={state.artists.status}
              empty
              emptyText="Pick a letter and press Fetch letter to load artists."
              loadingText="Loading artists…"
              retry={() => void fetchLetter(state.letter)}
            />
          ) : (
            state.artists.data.map((artist) => (
              <button
                key={artist.id}
                type="button"
                class={classNames("artist-item", state.activeArtist?.id === artist.id && "is-active")}
                onClick={() => void openArtist(artist)}
                title={`Load ${artist.name}'s Guitar Pro tabs`}
              >
                <span class="truncate">{artist.name}</span>
                <span class="artist-count">{artist.tabCount > 0 ? artist.tabCount : ""}</span>
              </button>
            ))
          )}
        </div>

        <div>
          <TrackList
            tabs={tabs}
            status={tabsStatus}
            emptyText={
              searching
                ? "No cached results. Fetch an artist first — search only queries the local cache."
                : "Select an artist to load its Guitar Pro tabs."
            }
            rowIndex={rowIndex}
            onFocusRow={onFocusRow}
            retry={() => {
              if (searching) void runSearch(state.query);
              else if (state.activeArtist !== null) void openArtist(state.activeArtist);
            }}
            showFavorite
          />
        </div>
      </div>

      {mode === "batch" && (
        <div class="toolbar" style={{ marginTop: "10px" }}>
          <span class="muted" style={{ fontSize: "11px" }}>
            {selection.length > 0
              ? `${selection.length} track(s) selected with Space`
              : "Select rows with Space to build a batch"}
          </span>
          <span class="card-head-spacer" />
          <button type="button" class="btn tiny ghost" onClick={clearTabSelection} disabled={selection.length === 0}>
            Clear selection
          </button>
          <button
            type="button"
            class="btn tiny primary"
            disabled={selection.length === 0}
            onClick={() => void queueTabs(selection)}
            title="Queue every selected track with the current options"
          >
            <DownloadIcon size={12} /> Queue {selection.length > 0 ? selection.length : ""}
          </button>
        </div>
      )}
    </Card>
  );
}
