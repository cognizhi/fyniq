package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeHelpNoUser refuses saving tour progress for a caller that is not a person who is a member.
const CodeHelpNoUser = "HLP-01"

// HelpState is how far the caller went with the first-run tour in this organisation (FR-UX-34): not started until they
// answer the offer, then started at a step, finished or skipped. Saving it is the tour's only write (G4-104).
type HelpState struct {
	Tour  string     `json:"tour" enum:"not_started,started,finished,skipped" doc:"Where the caller is with the first-run tour." example:"started"`
	Step  int        `json:"step" minimum:"0" maximum:"6" doc:"The step shown last, from 0; 0 before the tour starts." example:"2"`
	Offer bool       `json:"offer" doc:"Whether to offer the tour on Home: it was never answered, or it stopped part-way, to resume there. Help replays it either way." example:"true"`
	SetAt *time.Time `json:"set_at,omitempty" doc:"When the caller last answered or moved, if ever." example:"2026-11-17T09:30:00Z"`
	ETag  string     `json:"etag" doc:"The state's version, for If-Match." example:"\"9f1c2b7a\""`
}

// TourChange is where the caller is with the tour now.
type TourChange struct {
	Tour string `json:"tour" enum:"started,finished,skipped" doc:"started when the caller starts, resumes or moves to a step; finished at Done; skipped when they decline the offer or stop part-way." example:"started"`
	Step int    `json:"step" minimum:"0" maximum:"6" doc:"The step shown last, from 0. A tour has at most seven." example:"2"`
}

type tourInput struct {
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body TourChange
}

type helpStateOutput struct {
	ETag     string `header:"ETag" doc:"The state's version, for If-Match."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     HelpState
}

type helpStateReadOutput struct {
	ETag string `header:"ETag" doc:"The state's version, for If-Match."`
	Body HelpState
}

func helpStateETag(s *storage.HelpState) string {
	if s == nil {
		return etag("none")
	}
	return etag(s.SetAt.UnixMicro(), s.Tour, s.Step)
}

func helpStateOf(s *storage.HelpState) HelpState {
	if s == nil {
		return HelpState{Tour: "not_started", Offer: true, ETag: helpStateETag(nil)}
	}
	at := s.SetAt.UTC()
	return HelpState{Tour: s.Tour, Step: s.Step, Offer: s.Tour == storage.TourStarted, SetAt: &at, ETag: helpStateETag(s)}
}

func registerHelpState(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.GetHelpState, huma.Operation{
		OperationID: "getHelpState", Method: http.MethodGet, Path: "/v1/help-state", Tags: []string{"Help"},
		Summary: "Get your first-run tour state",
		Description: "Where the caller is with the first-run tour in this organisation (FR-UX-34): offered on Home until they answer it, " +
			"offered again to resume when it stopped part-way, and replayable from Help whatever its state. A caller who is not a " +
			"person gets the not-started state, with no offer. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*helpStateReadOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			out := helpStateOf(nil)
			out.Offer = false
			return &helpStateReadOutput{ETag: out.ETag, Body: out}, nil
		}
		var out HelpState
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			s, ok, err := u.Help().Latest(ctx, p.UserID)
			if ok {
				out = helpStateOf(&s)
			} else {
				out = helpStateOf(nil)
			}
			return err
		})
		return &helpStateReadOutput{ETag: out.ETag, Body: out}, err
	})

	register(a, authz.SetTourState, huma.Operation{
		OperationID: "setTourState", Method: http.MethodPut, Path: "/v1/help-state/tour", Tags: []string{"Help"},
		Summary: "Save where you are with the first-run tour",
		Description: "Saves the caller's own progress through the first-run tour (FR-UX-34): started at a step, finished, or skipped at a " +
			"step. It is the tour's only write: the tour never changes business data (G4-104, QA-UI-06). Side effects: the state and its " +
			"audit event. Needs If-Match with the state's etag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and " +
			"saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeHelpNoUser}, locks...), func(ctx context.Context, in *tourInput) (*helpStateOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
			return nil, problem(CodeHelpNoUser, "Only a person who is a member has a tour of their own.", "Sign in as yourself")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (HelpState, error) {
			s, ok, err := u.Help().Latest(ctx, p.UserID)
			if err != nil {
				return HelpState{}, err
			}
			var current *storage.HelpState
			if ok {
				current = &s
			}
			if err := checkIfMatch(in.IfMatch, helpStateETag(current)); err != nil {
				return HelpState{}, err
			}
			saved, err := u.Help().Set(ctx, storage.HelpState{UserID: p.UserID, Tour: in.Body.Tour, Step: in.Body.Step})
			if err != nil {
				return HelpState{}, err
			}
			return helpStateOf(&saved), nil
		})
		if err != nil {
			return nil, err
		}
		return &helpStateOutput{ETag: w.Body.ETag, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
