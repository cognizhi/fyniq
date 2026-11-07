package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ your sign-ins (FR-ADM-08, rev 0.196)

// SignInIdentity is one of your sign-ins: an account at a provider that signs you in to Fyniq.
type SignInIdentity struct {
	ID           string     `json:"id" pattern:"^[0-9a-f]{32}$" doc:"The sign-in's ID: derived from the account at the provider, never the account itself." example:"9b2e4f6a8c0d1e3f5a7b9c1d3e5f7a9b"`
	Provider     string     `json:"provider" doc:"The sign-in provider the account is at." example:"google"`
	ProviderName string     `json:"provider_name" doc:"The provider's name as the sign-in page shows it; its ID when it is no longer offered." example:"Google"`
	AddedAt      time.Time  `json:"added_at" doc:"When it first signed you in, or was linked." example:"2026-10-20T09:00:00Z"`
	LinkedAt     *time.Time `json:"linked_at,omitempty" doc:"When you linked it to your account; absent for the one you joined or set up Fyniq with." example:"2026-11-06T09:00:00Z"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty" doc:"When it last signed you in; absent when it never has." example:"2026-11-06T09:30:00Z"`
	Current      bool       `json:"current" doc:"true for the one this session signed in with: it cannot be removed from this session." example:"true"`
}

// SignInIdentityPage is a page of your sign-ins: the one you joined or set up with first, then those you linked, in order.
type SignInIdentityPage struct {
	Items      []SignInIdentity `json:"items" doc:"Your sign-ins on this page."`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjliMmU0ZjZhIn0"`
}

type identityListInput struct{ PageQuery }

// IdentityPath names one of your sign-ins.
type IdentityPath struct {
	IdentityID string `path:"identity_id" pattern:"^[0-9a-f]{32}$" doc:"The sign-in's ID." example:"9b2e4f6a8c0d1e3f5a7b9c1d3e5f7a9b"`
}

type identityListOutput struct{ Body SignInIdentityPage }

type identityOutput struct{ Body SignInIdentity }

// identityID is the public ID of a sign-in: a hash of its issuer and subject, so the account at the provider is not shown.
func identityID(issuer, subject string) string {
	h := sha256.Sum256([]byte("fyniq-identity\x00" + issuer + "\x00" + subject))
	return hex.EncodeToString(h[:16])
}

func (a *API) identityOf(i storage.Identity, p authn.Principal) SignInIdentity {
	out := SignInIdentity{ID: identityID(i.Issuer, i.Subject), Provider: i.Provider, ProviderName: i.Provider, AddedAt: i.CreatedAt.UTC(),
		Current: i.Issuer == p.Issuer && i.Subject == p.Subject}
	if a.deps.SignIn != nil {
		for _, s := range a.deps.SignIn.Providers() {
			if s.ID == i.Provider {
				out.ProviderName = s.Name
			}
		}
	}
	if !i.LinkedAt.IsZero() {
		t := i.LinkedAt.UTC()
		out.LinkedAt = &t
	}
	if !i.LastUsedAt.IsZero() {
		t := i.LastUsedAt.UTC()
		out.LastUsedAt = &t
	}
	return out
}

// identityPerson returns the caller when they are a person with a Fyniq user in a browser session, with their sign-ins.
func (a *API) identityPerson(ctx context.Context, what string) (authn.Principal, storage.Sessions, []storage.Identity, error) {
	p, err := signedInPerson(ctx, what)
	if err != nil {
		return p, nil, nil, err
	}
	sessions, ok := a.deps.Installation.(storage.Sessions)
	if p.UserID == uuid.Nil || len(p.SessionHash) == 0 || !ok {
		return p, nil, nil, problem(CodeSessionRequired, what+" needs a person with a Fyniq user, signed in to Fyniq in the browser.",
			"Sign in to Fyniq in the browser and do it there.")
	}
	is, err := sessions.Identities(ctx, p.UserID)
	return p, sessions, is, err
}

// notifySignInMethod tells p, in their organisation's centre, that a sign-in with provider was removed (FR-ADM-08). Sign-ins
// belong to the installation's trail and the notice to the person's organisation, so it is its own unit of work after the
// change; a failure is logged, since the change is done.
func (a *API) notifySignInMethod(ctx context.Context, p authn.Principal, provider string) {
	if a.deps.Outbox == nil || p.Organisation == uuid.Nil {
		return
	}
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		return notify.SignInMethod(ctx, *a.deps.Outbox, u, p.Organisation, p.UserID, provider, false, a.deps.Clock.Now())
	})
	if err != nil {
		a.deps.Log.Error("api.notify.sign_in_method", "error", err.Error(), "request_id", requestID(ctx))
	}
}

// IdentityCodes are the refusals of removing a sign-in.
var IdentityCodes = []string{storage.CodeIdentity, storage.CodeIdentityStepUp}

func registerIdentities(a *API) {
	register(a, authz.ListIdentities, huma.Operation{
		OperationID: "listIdentities", Method: http.MethodGet, Path: "/v1/session/identities", Tags: []string{"Session"},
		Summary: "List your sign-ins",
		Description: "The accounts at sign-in providers that sign you in to Fyniq, the one you joined or set up with first, then those " +
			"you linked, in order, with the one this session used marked " +
			"(FR-ADM-08). To link another, confirm it's you, then send the browser to /api/auth/signin?provider=<id>&link=1 from Fyniq: " +
			"it signs in with that provider and adds the account to yours. An account is linked only that way, never by a matching " +
			"email. " + authz.ListIdentities.Note + " Read-only; no side effects.",
	}, []string{CodeCursor, CodeSessionRequired}, func(ctx context.Context, in *identityListInput) (*identityListOutput, error) {
		p, _, is, err := a.identityPerson(ctx, "Listing your sign-ins")
		if err != nil {
			return nil, err
		}
		all := []SignInIdentity{}
		for _, i := range is {
			all = append(all, a.identityOf(i, p))
		}
		items, next, err := pageOf(all, func(i SignInIdentity) string { return i.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &identityListOutput{Body: SignInIdentityPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.UnlinkIdentity, exempt(huma.Operation{
		OperationID: "unlinkIdentity", Method: http.MethodDelete, Path: "/v1/session/identities/{identity_id}", Tags: []string{"Session"},
		Summary: "Remove a sign-in",
		Description: "Removes one of your sign-ins: that account at its provider signs you in no more (FR-ADM-08). Never the one this " +
			"session used, nor your last. " + authz.UnlinkIdentity.Note + " Side effects: the removal and its event in the installation's " +
			"audit trail, and a security notice to you.",
	}, "Prefer", "Removing a sign-in only takes away one of your own ways in; it is checked and done at once, and its audit event is the record (FR-INT-14, rev 0.196)."),
		append([]string{CodeStepUpRequired, CodeNotFound, CodeSessionRequired}, IdentityCodes...), func(ctx context.Context, in *IdentityPath) (*identityOutput, error) {
			p, sessions, is, err := a.identityPerson(ctx, "Removing a sign-in")
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			if !p.SteppedUp(now) {
				return nil, problem(CodeStepUpRequired, "Removing a sign-in needs a second factor in the last 5 minutes (FR-ADM-08).",
					"Confirm it's you with your authenticator app or a passkey, then try again.")
			}
			for _, i := range is {
				if identityID(i.Issuer, i.Subject) != in.IdentityID {
					continue
				}
				if _, err := sessions.UnlinkIdentity(ctx, p.SessionHash, i.Issuer, i.Subject, now); errors.Is(err, ports.ErrNotFound) {
					break
				} else if err != nil {
					return nil, err
				}
				a.notifySignInMethod(ctx, p, i.Provider)
				return &identityOutput{Body: a.identityOf(i, p)}, nil
			}
			return nil, problem(CodeNotFound, "You have no sign-in with that ID.", "List your sign-ins to find their IDs")
		})
}
