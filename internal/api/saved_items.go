package api

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/saveditem"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var savedItemCodes = []string{saveditem.CodePath, saveditem.CodeArea, saveditem.CodeTitle, saveditem.CodeQuery, saveditem.CodeLimit}

// CodeSavedNoUser refuses saving for a caller that is not a person who is a member.
const CodeSavedNoUser = "SVD-06"

// SavedItem is a favourite or a saved view of the caller's (FR-UX-12): an address and the name they gave it. Opening it shows
// what the caller's roles let them see, and a view's link is its address, which shares no data.
type SavedItem struct {
	ID      string    `json:"id" doc:"The item's ID: a favourite's address, or a view's own ID; send it to take the item away." example:"3f9c2b7a-61d0-4b1e-9d1e-0a1b2c3d4e5f"`
	Kind    string    `json:"kind" enum:"favourite,view" doc:"A page pinned, or a page with the filters, columns and sort its address carries." example:"view"`
	Title   string    `json:"title" doc:"The name the caller gave it." example:"Overdue this quarter"`
	Path    string    `json:"path" doc:"The page's path in the web app." example:"/sales/invoices"`
	Query   string    `json:"query,omitempty" doc:"A view's filters, columns and sort, as the page's address carries them; the link to share is path and query." example:"?status=overdue&sort=due"`
	SavedAt time.Time `json:"saved_at" doc:"When it was last saved." example:"2026-11-20T09:30:00Z"`
}

// SavedItemPage is a page of the caller's items.
type SavedItemPage struct {
	Items      []SavedItem `json:"items" nullable:"false" doc:"The caller's favourites and views in this page, in the order they were saved; those in an area the caller no longer holds a right in are left out (SEC-34)."`
	NextCursor string      `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

type listSavedItemsInput struct {
	PageQuery
	Kind string `query:"kind" enum:"favourite,view" doc:"Only favourites, or only views; both when left out." example:"view"`
}

// SaveItemRequest saves a favourite or a view.
type SaveItemRequest struct {
	Kind  string `json:"kind" enum:"favourite,view" doc:"A favourite pins the page; a view keeps its filters, columns and sort too." example:"view"`
	Title string `json:"title" minLength:"1" maxLength:"80" doc:"The name to show." example:"Overdue this quarter"`
	Path  string `json:"path" maxLength:"200" doc:"The page's path in the web app, in an area the caller holds a right in." example:"/sales/invoices"`
	Query string `json:"query,omitempty" maxLength:"255" doc:"A view's filters, columns and sort, as the page's address carries them." example:"?status=overdue&sort=due"`
	ID    string `json:"id,omitempty" maxLength:"200" doc:"A view's ID, to rename or change it; a new view when left out. A favourite's ID is its path." example:"3f9c2b7a-61d0-4b1e-9d1e-0a1b2c3d4e5f"`
}

type saveItemInput struct {
	KeyHeader
	PreferHeader
	Body SaveItemRequest
}

type takeAwayInput struct {
	KeyHeader
	PreferHeader
	Kind string `path:"kind" enum:"favourite,view" doc:"The item's kind." example:"view"`
	ID   string `query:"id" maxLength:"200" doc:"The item's ID: a favourite's path, or a view's ID." example:"3f9c2b7a-61d0-4b1e-9d1e-0a1b2c3d4e5f"`
}

type savedItemOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     SavedItem
}

type savedItemsOutput struct{ Body SavedItemPage }

func savedItemOf(s storage.SavedItem) SavedItem {
	return SavedItem{ID: s.Key, Kind: s.Kind, Title: s.Title, Path: s.Path, Query: s.Query, SavedAt: s.SetAt.UTC()}
}

// mayOpenArea says whether the caller's roles still hold a view right in the area of a saved page.
func mayOpenArea(roles []authz.Role, path string) bool {
	area, ok := saveditem.AreaOf(path)
	if !ok {
		return false
	}
	if area == authz.Membership {
		return true
	}
	return authz.Decide(authz.Policy{Operation: "savedItem", Capability: area, Need: authz.View}, roles, false).Allowed
}

func registerSavedItems(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListSavedItems, huma.Operation{
		OperationID: "listSavedItems", Method: http.MethodGet, Path: "/v1/saved-items", Tags: []string{"Session"},
		Summary: "List your favourites and saved views",
		Description: "The caller's own favourites (pages they pinned) and saved views (a page with the filters, columns and sort its address " +
			"carries), the same on every device (FR-UX-12), a page at a time (cursor pagination). An item in an area the caller no longer holds " +
			"a right in is left out, as every output follows the roles held now (SEC-34). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *listSavedItemsInput) (*savedItemsOutput, error) {
		p, _ := authn.From(ctx)
		all := []SavedItem{}
		if p.UserID != uuid.Nil && p.Class == authn.ClassUser {
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				mine, err := u.SavedItems().Mine(ctx, p.UserID)
				for _, s := range mine {
					if (in.Kind == "" || s.Kind == in.Kind) && mayOpenArea(p.Roles, s.Path) {
						all = append(all, savedItemOf(s))
					}
				}
				return err
			})
			if err != nil {
				return nil, err
			}
		}
		items, next, err := pageOf(all, func(s SavedItem) string { return s.Kind + ":" + s.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &savedItemsOutput{Body: SavedItemPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.SaveSavedItem, huma.Operation{
		OperationID: "saveSavedItem", Method: http.MethodPost, Path: "/v1/saved-items", Tags: []string{"Session"},
		Summary: "Save a favourite or a view",
		Description: "Saves the caller's own favourite (a page) or view (a page with its filters, columns and sort), or changes a view's name or filters " +
			"when its ID is sent (FR-UX-12). An item is an address and a name, never a record's content; the page it opens shows what the caller's " +
			"roles let them see. The page must be in an area the caller holds a right in. At most 50 of each kind. Side effects: the item and its audit " +
			"event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSavedNoUser},
		append(slices.Clone(savedItemCodes), locks...)...), func(ctx context.Context, in *saveItemInput) (*savedItemOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodeSavedNoUser, "Only a person who is a member has favourites and views of their own.", "Sign in as yourself")
		}
		b := in.Body
		if pr := saveditem.Validate(b.Kind, b.Title, b.Path, b.Query); pr != nil {
			return nil, invalid([]FieldProblem{{Code: pr.Code, Field: "body." + pr.Field, Message: pr.Message, Fix: pr.Fix}})
		}
		if !mayOpenArea(p.Roles, b.Path) {
			return nil, invalid([]FieldProblem{{Code: saveditem.CodeArea, Field: "body.path", Message: "That page is not part of your access.", Fix: "Save a page your roles may open"}})
		}
		key := b.Path
		if b.Kind == storage.SavedView {
			key = b.ID
			if key == "" {
				key = uuid.NewString()
			}
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SavedItem, error) {
			mine, err := u.SavedItems().Mine(ctx, p.UserID)
			if err != nil {
				return SavedItem{}, err
			}
			count, known := 0, false
			for _, s := range mine {
				if s.Kind == b.Kind {
					count++
					known = known || s.Key == key
				}
			}
			limit := saveditem.MaxFavourites
			if b.Kind == storage.SavedView {
				limit = saveditem.MaxViews
			}
			if !known && count >= limit {
				return SavedItem{}, invalid([]FieldProblem{{Code: saveditem.CodeLimit, Field: "body.kind", Message: "You already keep the most of these there can be.", Fix: "Take one away first"}})
			}
			saved, err := u.SavedItems().Save(ctx, storage.SavedItem{UserID: p.UserID, Kind: b.Kind, Key: key, On: true, Title: b.Title, Path: b.Path, Query: b.Query})
			return savedItemOf(saved), err
		})
		if err != nil {
			return nil, err
		}
		return &savedItemOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.TakeAwaySavedItem, huma.Operation{
		OperationID: "takeAwaySavedItem", Method: http.MethodDelete, Path: "/v1/saved-items/{kind}", Tags: []string{"Session"},
		Summary: "Take a favourite or a view away",
		Description: "Takes one of the caller's own favourites or views away, by its ID in the query (FR-UX-12). Nothing is deleted: the item's history stays in " +
			"the audit trail, and saving the page again brings it back. Side effects: the change and its audit event. Idempotent with Idempotency-Key.",
	}, append([]string{CodeMalformed, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSavedNoUser, CodeNotFound},
		append(slices.Clone(savedItemCodes), locks...)...), func(ctx context.Context, in *takeAwayInput) (*savedItemOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodeSavedNoUser, "Only a person who is a member has favourites and views of their own.", "Sign in as yourself")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (SavedItem, error) {
			mine, err := u.SavedItems().Mine(ctx, p.UserID)
			if err != nil {
				return SavedItem{}, err
			}
			i := slices.IndexFunc(mine, func(s storage.SavedItem) bool { return s.Kind == in.Kind && s.Key == in.ID })
			if i < 0 {
				return SavedItem{}, problem(CodeNotFound, "You have no such favourite or view.", "List your items and send the ID of one")
			}
			it := mine[i]
			it.On = false
			saved, err := u.SavedItems().Save(ctx, it)
			return savedItemOf(saved), err
		})
		if err != nil {
			return nil, err
		}
		return &savedItemOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
