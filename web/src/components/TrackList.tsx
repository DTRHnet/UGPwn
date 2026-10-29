// Shared track table used by Browse, Batch, Search and Favorites.

import type { Tab } from "../api";
import type { Status } from "../store";
import {
  queueTabs,
  selectTab,
  toggleFavorite,
  toggleTabSelection,
  useStore,
} from "../store";
import { classNames, formatCount, formatRating, text } from "../util";
import { CheckIcon, DownloadIcon, StarIcon, CloseIcon } from "./icons";
import { RegionState } from "./primitives";

export function TrackList({
  tabs,
  status,
  emptyText,
  rowIndex,
  onFocusRow,
  retry,
  showFavorite,
  remove,
}: {
  tabs: Tab[];
  status: Status;
  emptyText: string;
  rowIndex: number;
  onFocusRow: (index: number) => void;
  retry?: () => void;
  showFavorite?: boolean;
  remove?: boolean;
}) {
  const state = useStore();
  const favoriteIds = new Set(state.favorites.data.map((tab) => tab.id));
  const selectedKeys = new Set(state.selectedTabs.map((tab) => `${tab.id}:${tab.url}`));

  return (
    <div class="data-table" role="table" aria-label="Tracks">
      <div class="data-row data-head" role="row">
        <span aria-hidden="true" />
        <span>SONG / ARTIST</span>
        <span>TYPE</span>
        <span>RATING</span>
        <span>TUNING</span>
        <span aria-label="Actions" />
      </div>

      {tabs.length === 0 ? (
        <RegionState status={status} empty emptyText={emptyText} retry={retry} />
      ) : (
        tabs.map((tab, index) => {
          const key = `${tab.id}:${tab.url}`;
          const checked = selectedKeys.has(key);
          const isFav = favoriteIds.has(tab.id);
          return (
            <div
              key={key}
              class={classNames("data-row", index === rowIndex && "is-focus")}
              role="row"
              tabIndex={0}
              onClick={() => {
                selectTab(tab);
                onFocusRow(index);
              }}
              onKeyDown={(event) => {
                if (event.key === "Enter") {
                  selectTab(tab);
                  onFocusRow(index);
                }
              }}
              title={tab.url}
            >
              <button
                type="button"
                class="check"
                role="checkbox"
                aria-checked={checked ? "true" : "false"}
                aria-label={`Select ${text(tab.song)}`}
                onClick={(event) => {
                  event.stopPropagation();
                  toggleTabSelection(tab);
                }}
              >
                <CheckIcon />
              </button>
              <div class="cell-main">
                <div class="cell-song">{text(tab.song)}</div>
                <div class="cell-sub">
                  {text(tab.artist)}
                  {tab.author.length > 0 ? ` · ${tab.author}` : ""}
                </div>
              </div>
              <span class="cell-tag">
                <span class="tag">{text(tab.type || tab.fileExtension)}</span>
              </span>
              <span class="cell-source mono">
                {formatRating(tab.rating)}
                {tab.votes > 0 ? <span class="muted"> ({formatCount(tab.votes)})</span> : null}
              </span>
              <span class="cell-source">{text(tab.tuning)}</span>
              <span class="row-actions">
                <button
                  type="button"
                  class="icon-btn"
                  aria-label={`Queue ${text(tab.song)}`}
                  title="Queue this track"
                  onClick={(event) => {
                    event.stopPropagation();
                    void queueTabs([tab]);
                  }}
                >
                  <DownloadIcon size={12} />
                </button>
                {showFavorite === true && (
                  <button
                    type="button"
                    class="icon-btn"
                    aria-label={isFav ? `Remove ${text(tab.song)} from favorites` : `Save ${text(tab.song)} to favorites`}
                    title={isFav ? "Remove from favorites" : "Save to favorites"}
                    aria-pressed={isFav ? "true" : "false"}
                    onClick={(event) => {
                      event.stopPropagation();
                      void toggleFavorite(tab);
                    }}
                  >
                    <StarIcon size={12} />
                  </button>
                )}
                {remove === true && (
                  <button
                    type="button"
                    class="icon-btn"
                    aria-label={`Remove ${text(tab.song)} from favorites`}
                    title="Remove from favorites"
                    onClick={(event) => {
                      event.stopPropagation();
                      void toggleFavorite(tab);
                    }}
                  >
                    <CloseIcon />
                  </button>
                )}
              </span>
            </div>
          );
        })
      )}
    </div>
  );
}
