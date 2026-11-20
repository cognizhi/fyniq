package api

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ practice organisations (FR-UX-36; the owner's decision of 21 Nov 2026)
//
// A separate, clearly labelled organisation an Owner opens to try a task without touching the real books: only an Owner of an
// organisation that keeps books, one at a time, with empty books and nothing copied. The database's function decides both rules
// (migration 000711) and the creator reads nothing of any other tenant; the API adds the step-up, the idempotency key and the audit
// events of both organisations.

// PracticeOrganisation is the practice organisation opened.
type PracticeOrganisation struct {
	ID           string `json:"id" format:"uuid" doc:"The practice organisation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b01"`
	Name         string `json:"name" doc:"Its name, always labelled as a practice organisation." example:"Practice: Northwind Analytics Pte. Ltd."`
	BaseCurrency string `json:"base_currency" doc:"Its books' currency: the organisation it was opened from." example:"SGD"`
	Practice     bool   `json:"practice" doc:"Always true: this organisation is for trying a task, and holds no real books." example:"true"`
}

// NewPracticeOrganisation is what to call the practice organisation.
type NewPracticeOrganisation struct {
	Name string `json:"name,omitempty" maxLength:"150" doc:"A name after the label \"Practice: \"; the organisation's own name when left out." example:"Northwind Analytics Pte. Ltd."`
}

type createPracticeInput struct {
	KeyHeader
	PreferHeader
	Body NewPracticeOrganisation
}

type practiceOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PracticeOrganisation
}

// MyOrganisation is an organisation the caller works in.
type MyOrganisation struct {
	ID       string `json:"id" format:"uuid" doc:"The organisation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b01"`
	Name     string `json:"name" doc:"Its name." example:"Northwind Analytics Pte. Ltd."`
	Practice bool   `json:"practice" doc:"true for a practice organisation (FR-UX-36)." example:"false"`
	Sandbox  bool   `json:"sandbox" doc:"true for a sandbox organisation, made for integration testing with the sample company (FR-INT-18)." example:"false"`
	Current  bool   `json:"current" doc:"true for the one this session is in." example:"true"`
}

// MyOrganisationPage is the caller's organisations.
type MyOrganisationPage struct {
	Items      []MyOrganisation `json:"items" nullable:"false" doc:"The organisations the caller is an active member of, the oldest first."`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

type listMyOrganisationsInput struct{ PageQuery }

type switchOrganisationInput struct {
	Body struct {
		OrganisationID string `json:"organisation_id" format:"uuid" doc:"An organisation the caller is an active member of." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b01"`
	}
}

type switchedOutput struct{ Body MyOrganisation }

func (a *API) sessionOrganisations(ctx context.Context) (authn.Principal, storage.SessionOrganisations, error) {
	p, _ := authn.From(ctx)
	so, ok := a.deps.Installation.(storage.SessionOrganisations)
	if len(p.SessionHash) == 0 || !ok || p.UserID == uuid.Nil {
		return p, nil, problem(CodeSessionRequired, "Choosing an organisation needs a person signed in to Fyniq in the browser.", "Sign in to Fyniq in the browser and do it there.")
	}
	return p, so, nil
}

func registerPractice(a *API) {
	register(a, authz.ListMyOrganisations, huma.Operation{
		OperationID: "listMyOrganisations", Method: http.MethodGet, Path: "/v1/session/organisations", Tags: []string{"Session"},
		Summary: "List the organisations you work in",
		Description: "The organisations the caller is an active member of, with their names, a practice one marked as such (FR-UX-36), and the one this session is in. " +
			"Only the caller's own memberships: nothing else of any organisation. Read-only; no side effects.",
	}, append([]string{CodeSessionRequired, CodeCursor}, storage.CodeRetry, storage.CodeLockTimeout), func(ctx context.Context, in *listMyOrganisationsInput) (*struct{ Body MyOrganisationPage }, error) {
		p, so, err := a.sessionOrganisations(ctx)
		if err != nil {
			return nil, err
		}
		rows, err := so.OrganisationsOf(ctx, p.SessionHash, a.deps.Clock.Now())
		if err != nil {
			return nil, err
		}
		all := make([]MyOrganisation, 0, len(rows))
		for _, r := range rows {
			all = append(all, MyOrganisation{ID: r.ID.String(), Name: r.Name, Practice: r.Practice, Sandbox: a.isSandbox(ctx, r.ID), Current: r.ID == p.Organisation})
		}
		items, next, err := pageOf(all, func(o MyOrganisation) string { return o.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &struct{ Body MyOrganisationPage }{Body: MyOrganisationPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.SwitchMyOrganisation, exempt(exempt(huma.Operation{
		OperationID: "switchMyOrganisation", Method: http.MethodPost, Path: "/v1/session/organisation", Tags: []string{"Session"},
		Summary: "Work in another organisation",
		Description: "Makes the organisation the one this session works in from the next request, for a person who is an active member of more than one, such as the " +
			"Owner of a practice organisation (FR-UX-36). Refused, as not found, for an organisation the caller is no active member of. Side effects: the session's choice.",
	}, "Prefer", "Choosing an organisation only changes which of the caller's own organisations this session works in, and is done at once (FR-INT-14)."),
		"Idempotency-Key", "Choosing the same organisation again changes nothing more (FR-INT-08)."), append([]string{CodeSessionRequired, CodeNotFound, CodeMalformed, CodeInvalid}, storage.CodeRetry, storage.CodeLockTimeout), func(ctx context.Context, in *switchOrganisationInput) (*switchedOutput, error) {
		p, so, err := a.sessionOrganisations(ctx)
		if err != nil {
			return nil, err
		}
		org, err := parseID("organisation_id", in.Body.OrganisationID)
		if err != nil {
			return nil, err
		}
		now := a.deps.Clock.Now()
		ok, err := so.SwitchOrganisation(ctx, p.SessionHash, org, now)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, problem(CodeNotFound, "You are no member of an organisation with that ID.", "List the organisations you work in and choose one of them")
		}
		rows, err := so.OrganisationsOf(ctx, p.SessionHash, now)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if r.ID == org {
				return &switchedOutput{Body: MyOrganisation{ID: r.ID.String(), Name: r.Name, Practice: r.Practice, Sandbox: a.isSandbox(ctx, r.ID), Current: true}}, nil
			}
		}
		return nil, problem(CodeNotFound, "You are no member of an organisation with that ID.", "List the organisations you work in and choose one of them")
	})

	register(a, authz.CreatePracticeOrganisation, huma.Operation{
		OperationID: "createPracticeOrganisation", Method: http.MethodPost, Path: "/v1/organisation/practice", Tags: []string{"Organisation"},
		DefaultStatus: http.StatusCreated, Summary: "Open a practice organisation",
		Description: "Opens a separate organisation, labelled \"Practice: …\", where the caller can try a task without touching the real books (FR-UX-36). " +
			"Only an Owner of an organisation that keeps books opens one, and only while they hold none: one at a time. It starts with empty books and " +
			"nothing copied from this one; the caller becomes its Owner and reads nothing else of any other organisation. Needs step-up authentication in the " +
			"last 5 minutes. Side effects: the organisation, its membership and Owner role, and audit events in both organisations' trails. It is deleted " +
			"through the organisation's tenant export and delete. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeStepUpRequired, CodeSessionRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		storage.CodePractice, storage.CodePracticeHeld, CodeSandboxRefused}, storage.CodeRetry, storage.CodeLockTimeout), func(ctx context.Context, in *createPracticeInput) (*practiceOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil || p.Class != authn.ClassUser || p.AI() {
			return nil, problem(CodeSessionRequired, "Only a person signed in to Fyniq opens a practice organisation.", "Sign in as yourself")
		}
		if !p.SteppedUp(a.deps.Clock.Now()) {
			return nil, problem(CodeStepUpRequired, "Opening a practice organisation needs step-up authentication in the last 5 minutes.", "Confirm it's you with your passkey or authenticator app, then try again.")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PracticeOrganisation, error) {
			o, err := u.Organisation(ctx)
			if err != nil {
				return PracticeOrganisation{}, err
			}
			is, err := u.Practices().IsPractice(ctx, p.Organisation)
			if err != nil {
				return PracticeOrganisation{}, err
			}
			if sandbox, err := inSandbox(ctx, u); err != nil || sandbox { // FR-INT-18: a sandbox organisation opens no practice organisation
				if err != nil {
					return PracticeOrganisation{}, err
				}
				return PracticeOrganisation{}, refuseSandbox("Opening a practice organisation")
			}
			if is {
				return PracticeOrganisation{}, problem(storage.CodePractice, "A practice organisation opens no other: open it from the organisation that keeps your books.", "Switch to your own organisation first")
			}
			name := strings.TrimSpace(in.Body.Name)
			if name == "" {
				name = o.Name
			}
			if label := "Practice: " + name; utf8.RuneCountInString(label) > 200 {
				name = string([]rune(name)[:190])
			}
			made, err := u.Practices().Open(ctx, storage.NewPractice{User: p.UserID, Name: "Practice: " + name, Currency: o.BaseCurrency, Now: a.deps.Clock.Now()})
			if err != nil {
				return PracticeOrganisation{}, err
			}
			return PracticeOrganisation{ID: made.OrganisationID.String(), Name: "Practice: " + name, BaseCurrency: o.BaseCurrency, Practice: true}, nil
		})
		if err != nil {
			return nil, err
		}
		return &practiceOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}

// isSandbox says whether an organisation the caller is a member of is a sandbox one (FR-INT-18); a failed read says no, which only costs the label.
func (a *API) isSandbox(ctx context.Context, org uuid.UUID) bool {
	var sandbox bool
	_ = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		sandbox, err = u.Sandboxes().Is(ctx)
		return err
	})
	return sandbox
}
