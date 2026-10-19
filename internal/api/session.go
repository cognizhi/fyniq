package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// SignInProvider is a provider people may sign in with.
type SignInProvider struct {
	ID   string `json:"id" doc:"The provider's ID: the web app sends the browser to /api/auth/signin?provider=<id> to sign in with it." example:"google"`
	Name string `json:"name" doc:"The provider's name, as the sign-in page shows it." example:"Google"`
}

// SignInProviders are the providers people may sign in with.
type SignInProviders struct {
	Items      []SignInProvider `json:"items" doc:"The providers, in the order to offer them; empty when sign-in is not configured."`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6Imdvb2dsZSJ9"`
}

type signInProvidersInput struct {
	PageQuery
}

// SessionInfo is the caller's own session.
type SessionInfo struct {
	Provider       string     `json:"provider,omitempty" doc:"The sign-in provider of a browser session (FR-ADM-08); absent for a local dev token." example:"google"`
	UserID         string     `json:"user_id,omitempty" format:"uuid" doc:"Your Fyniq user; absent until you have one (a new installation before setup, or before you accept an invitation)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	OrganisationID string     `json:"organisation_id,omitempty" format:"uuid" doc:"The organisation your session is in; absent when you are a member of none." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	Roles          []RoleName `json:"roles" doc:"Your roles in that organisation (PRD-001 §7.1); they are read on every request, so a change applies at once."`
	SuperAdmin     bool       `json:"super_admin" doc:"Whether you are a Super admin of this installation (FR-ADM-07)." example:"false"`
	SteppedUpUntil *time.Time `json:"stepped_up_until,omitempty" doc:"Until when your last step-up (a second factor at sign-in) counts for the actions that need one; absent when it does not." example:"2026-10-15T09:05:00Z"`
	Features       *Features  `json:"features,omitempty" doc:"The organisation's feature switches today (FR-ORG-06); absent when you are a member of none."`
}

// Features are the organisation's feature switches on a day (FR-ORG-06): the web app shows only what is on.
type Features struct {
	MultiCurrency bool `json:"multi_currency" doc:"Documents and accounts in other currencies." example:"true"`
	Payroll       bool `json:"payroll" doc:"Payroll and its pages." example:"false"`
	Projects      bool `json:"projects" doc:"Projects and time." example:"false"`
	Portals       bool `json:"portals" doc:"Portals for customers and investors." example:"false"`
	AIAssistant   bool `json:"ai_assistant" doc:"The AI assistant." example:"false"`
}

// featuresOn reads the organisation's feature switches today.
func featuresOn(ctx context.Context, a *API, u storage.UnitOfWork) (*Features, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return nil, err
	}
	changes, err := u.Settings().Changes(ctx, "")
	if err != nil {
		return nil, err
	}
	on := func(key string) bool {
		d, _ := settings.Lookup(key)
		return settings.On(d, changes, today).Value == "true"
	}
	return &Features{MultiCurrency: on("features.multi_currency"), Payroll: on("features.payroll"), Projects: on("features.projects"),
		Portals: on("features.portals"), AIAssistant: on("features.ai_assistant")}, nil
}

// RoleName is a role of PRD-001 §7.1, as the API names it.
type RoleName string

// Schema implements huma.SchemaProvider: a string, with an example.
func (RoleName) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A role of PRD-001 §7.1, such as owner, accountant or bookkeeper.", Examples: []any{"owner"}}
}

type signInProvidersOutput struct{ Body SignInProviders }

type sessionOutput struct{ Body SessionInfo }

func registerSession(a *API) {
	register(a, authz.ListSignInProviders, huma.Operation{
		OperationID: "listSignInProviders", Method: http.MethodGet, Path: "/v1/sign-in-providers", Tags: []string{"Session"},
		Summary: "List the sign-in providers",
		Description: "The providers people may sign in to the web app with (FR-ADM-08): Google and Zoho through Identity Platform, or the " +
			"local sign-in page in local mode, a page at a time (cursor pagination). Public: the sign-in page needs it before anyone is signed " +
			"in. Read-only; no side effects.",
		Security: []map[string][]string{{}}, // no credential: an empty requirement
	}, []string{CodeCursor}, func(_ context.Context, in *signInProvidersInput) (*signInProvidersOutput, error) {
		all := []SignInProvider{}
		if a.deps.SignIn != nil {
			for _, p := range a.deps.SignIn.Providers() {
				all = append(all, SignInProvider{ID: p.ID, Name: p.Name})
			}
		}
		items, next, err := pageOf(all, func(p SignInProvider) string { return p.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &signInProvidersOutput{Body: SignInProviders{Items: items, NextCursor: next}}, nil
	})
	register(a, authz.GetSession, huma.Operation{
		OperationID: "getSession", Method: http.MethodGet, Path: "/v1/session", Tags: []string{"Session"},
		Summary: "Get your session",
		Description: "Who you are signed in as: the provider, your user, the organisation your session is in, your roles there, whether " +
			"you are a Super admin, until when your step-up counts (SEC-05, FR-ADM-07), and the organisation's feature switches (FR-ORG-06). The web app reads it after signing in; a 401 " +
			"means it should show the sign-in page. Needs a person signed in; an OAuth client or the AI is refused. Read-only; no side effects.",
	}, []string{CodeSessionRequired, CodeUnavailable}, func(ctx context.Context, _ *struct{}) (*sessionOutput, error) {
		p, err := signedInPerson(ctx, "Reading your session")
		if err != nil {
			return nil, err
		}
		out := SessionInfo{Provider: p.Provider, Roles: make([]RoleName, len(p.Roles))}
		for i, r := range p.Roles {
			out.Roles[i] = RoleName(r)
		}
		if p.UserID != uuid.Nil {
			out.UserID = p.UserID.String()
		}
		if p.Organisation != uuid.Nil {
			out.OrganisationID = p.Organisation.String()
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				out.Features, err = featuresOn(ctx, a, u)
				return err
			})
			if err != nil {
				return nil, err
			}
		}
		if a.deps.Installation != nil {
			_, err := a.deps.Installation.SuperAdminFor(ctx, p.Issuer, p.Subject)
			if err != nil && !errors.Is(err, ports.ErrNotFound) {
				return nil, err
			}
			out.SuperAdmin = err == nil
		}
		if p.SteppedUp(a.deps.Clock.Now()) {
			until := p.StepUpAt.Add(authn.StepUpWindow)
			out.SteppedUpUntil = &until
		}
		return &sessionOutput{Body: out}, nil
	})
}
