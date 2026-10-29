// FAVORITES view: saved tabs from GET /api/favorites.

import { loadFavorites, selectRow, useStore } from "../store";
import { RetryIcon } from "./icons";
import { Card } from "./primitives";
import { TrackList } from "./TrackList";

export function FavoritesView({ rowIndex }: { rowIndex: number }) {
  const state = useStore();
  const favorites = state.favorites;

  return (
    <Card
      title="FAVORITES"
      actions={
        <button type="button" class="btn tiny" onClick={() => void loadFavorites()} title="Reload saved tracks">
          <RetryIcon /> Refresh
        </button>
      }
      bodyClass="tight"
    >
      <TrackList
        tabs={favorites.data}
        status={favorites.status}
        emptyText="Nothing saved yet — press the star on any track to keep it here."
        rowIndex={rowIndex}
        onFocusRow={selectRow}
        retry={() => void loadFavorites()}
        remove
      />
    </Card>
  );
}
