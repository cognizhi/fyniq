package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ bulk operations (FR-INT-11)

// MaxBulkItems is the most items a bulk request takes. FR-INT-11 allows up to 1,000, but every item
// is a unit of work of its own: 1,000 took 5.3 s locally (19 Oct 2026) and would pass FR-INT-11's 10 s
// at Cloud SQL's 10 ms or so a commit, after which a request must answer 202 with an operation. 250
// stay well within it; larger batches wait for the operation resource.
const MaxBulkItems = 250

// BulkResult is what happened to one item of a bulk request.
type BulkResult struct {
	Index   int      `json:"index" doc:"The item's position in the request, from 0." example:"0"`
	Status  int      `json:"status" doc:"What the item's own request would have answered: 201 saved, 200 previewed (nothing saved), or the status of its problem." example:"201"`
	ID      string   `json:"id,omitempty" format:"uuid" doc:"The saved record's ID; absent when the item was refused or previewed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d70"`
	ETag    string   `json:"etag,omitempty" doc:"The saved record's ETag, for If-Match; absent when the item was refused or previewed." example:"\"1-draft\""`
	Problem *Problem `json:"problem,omitempty" doc:"Why the item was refused (RFC 9457), with its stable code; absent when it was saved or previewed."`
}

// BulkResults are a bulk request's results: one per item, in the order sent.
type BulkResults struct {
	Results []BulkResult `json:"results" doc:"One result per item, in the order sent; an item is never left out."`
	Saved   int          `json:"saved" doc:"How many items were saved (or, in a preview, would be)." example:"998"`
	Refused int          `json:"refused" doc:"How many items were refused; each has its problem in results." example:"2"`
}

// bulk runs one command per item, each in a unit of work of its own, so an item the ledger refuses
// takes no other item with it and is reported with its problem (FR-INT-11: a result per item, never a
// silent failure). The Idempotency-Key covers the whole request: it is claimed before the first item
// and completed with the results after the last, so a repeat gets the same results and runs nothing.
// Under a preview each item is previewed on its own and nothing is saved (FR-INT-14).
//
// The request runs while the client waits: FR-INT-11 lets a request answer synchronously when it takes
// under 10 s, which MaxBulkItems items do (TestBulkCreateJournals measures them).
func bulk(ctx context.Context, a *API, key, prefer string, request any, n int,
	item func(ctx context.Context, u storage.UnitOfWork, i int) (BulkResult, error)) (written[BulkResults], error) {
	p, _ := authn.From(ctx)
	client := p.Client + " " + p.Subject
	out := written[BulkResults]{Status: http.StatusOK, Previewed: wantsPreview(prefer)}
	if out.Previewed {
		key = ""
	}
	if key != "" {
		hash, err := requestHash(ctx, request)
		if err != nil {
			return out, err
		}
		var replayed bool
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			now := a.deps.Clock.Now()
			earlier, err := u.Idempotency().Claim(ctx, storage.IdempotencyClaim{Client: client, Key: key, RequestHash: hash,
				CreatedAt: now, ExpiresAt: now.Add(IdempotencyTTL)})
			if err != nil || earlier == nil {
				return err
			}
			replayed = true
			return replay(a, earlier, hash, &out)
		})
		if err != nil || replayed {
			return out, err
		}
	}

	itemCtx := ctx
	if out.Previewed {
		itemCtx = storage.WithPreview(ctx)
	}
	out.Body.Results = make([]BulkResult, n)
	for i := range n {
		var r BulkResult
		var err error
		for attempt := 1; ; attempt++ {
			err = a.deps.Store.Do(itemCtx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				r, err = item(ctx, u, i)
				return err
			})
			var se *storage.Error
			if err == nil || !errors.As(err, &se) || !se.Retryable() || attempt >= retries {
				break
			}
		}
		switch {
		case out.Previewed && errors.Is(err, storage.ErrPreviewed):
			r = BulkResult{Status: http.StatusOK} // a preview keeps no ID and no version
		case err != nil:
			pr := *problemFrom(ctx, err) // a copy, whose field paths point into this request
			if pr.Instance == "" {
				pr.Instance = instance(ctx)
			}
			pr.Errors = slices.Clone(pr.Errors)
			for k, f := range pr.Errors {
				if rest, ok := strings.CutPrefix(f.Field, "body."); ok {
					pr.Errors[k].Field = fmt.Sprintf("body.items[%d].%s", i, rest)
				}
			}
			r = BulkResult{Status: pr.Status, Problem: &pr}
		}
		r.Index = i
		out.Body.Results[i] = r
		if r.Problem == nil {
			out.Body.Saved++
		} else {
			out.Body.Refused++
		}
	}

	if key != "" {
		saved, err := json.Marshal(out.Body)
		if err == nil {
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				return u.Idempotency().Complete(ctx, client, key, out.Status, saved)
			})
		}
		if err != nil {
			// The items are saved and the client gets their results; a repeat with this key is answered
			// "still running" until the key expires, and runs nothing.
			logFailure(ctx, err)
		}
	}
	return out, nil
}
