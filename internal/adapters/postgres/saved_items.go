package postgres

import (
	"context"
	"sort"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) SavedItems() storage.SavedItems { return savedItems{u} }

type savedItems struct{ u *unitOfWork }

func itemOf(user uuid.UUID, kind, key, state, title, path, query string, at storage.SavedItem) storage.SavedItem {
	at.UserID, at.Kind, at.Key, at.On, at.Title, at.Path, at.Query = user, kind, key, state == "on", title, path, query
	return at
}

func (r savedItems) Save(ctx context.Context, it storage.SavedItem) (storage.SavedItem, error) {
	if err := r.u.check(); err != nil {
		return storage.SavedItem{}, err
	}
	state := "off"
	if it.On {
		state = "on"
	}
	row, err := r.u.q.InsertUserSavedItem(ctx, sqlcdb.InsertUserSavedItemParams{UserID: it.UserID, Kind: it.Kind, ItemKey: it.Key, State: state,
		Title: it.Title, Path: it.Path, Query: it.Query, SetAt: r.u.store.clock.Now()})
	if err != nil {
		return storage.SavedItem{}, mapError(err)
	}
	out := itemOf(row.UserID, row.Kind, row.ItemKey, row.State, row.Title, row.Path, row.Query, storage.SavedItem{SetAt: row.SetAt})
	if err := r.u.event(ctx, "user_saved_item.set", "user_saved_item", row.ID, nil, map[string][2]any{
		"kind": {nil, out.Kind}, "title": {nil, out.Title}, "path": {nil, out.Path}, "state": {nil, state}}); err != nil {
		return storage.SavedItem{}, err
	}
	return out, nil
}

func (r savedItems) Mine(ctx context.Context, user uuid.UUID) ([]storage.SavedItem, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.LatestUserSavedItems(ctx, user)
	if err != nil {
		return nil, mapError(err)
	}
	// In the order they were last saved, which is the order a person sees them added.
	var out []storage.SavedItem
	for _, x := range rows {
		if x.State == "on" {
			out = append(out, itemOf(x.UserID, x.Kind, x.ItemKey, x.State, x.Title, x.Path, x.Query, storage.SavedItem{SetAt: x.SetAt}))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SetAt.Before(out[j].SetAt) })
	return out, nil
}
