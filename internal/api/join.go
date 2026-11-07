package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ joining by email domain (FR-ADM-08, rev 0.199)

// On the owner's decisions of 6 and 7 Nov 2026: an Owner claims the organisation's email domains (one organisation per
// domain); a Super admin turns joining by domain on for a provider and the domains it accepts; a person who signed in
// through it with a verified email on a claimed domain, and has no Fyniq account, asks to join; an Owner approves, which
// makes them a member with the Employee role. Until then the request grants nothing.

// JoinOffer is the organisation a person signed in without a Fyniq account may ask to join.
type JoinOffer struct {
	OrganisationName string `json:"organisation_name" doc:"The organisation's name." example:"Cognizhi Pte. Ltd."`
	Domain           string `json:"domain" doc:"The email domain the organisation claimed, which your verified email is on." example:"cognizhi.com"`
	Email            string `json:"email" doc:"Your verified email, as your provider gave it." example:"wei.jie@cognizhi.com"`
}

// JoinRequestBody asks to join.
type JoinRequestBody struct {
	DisplayName string `json:"display_name" minLength:"1" maxLength:"200" doc:"Your name, as the organisation will see it." example:"Wei Jie Lim"`
}

// JoinRequestView is a request to join by email domain.
type JoinRequestView struct {
	ID          string     `json:"id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f90"`
	DisplayName string     `json:"display_name" example:"Wei Jie Lim"`
	Email       string     `json:"email" doc:"Their verified email." example:"wei.jie@cognizhi.com"`
	Provider    string     `json:"provider" doc:"The sign-in provider they asked from." example:"google"`
	RequestedAt time.Time  `json:"requested_at" example:"2026-11-07T09:00:00Z"`
	Status      string     `json:"status" enum:"pending,approved,rejected" doc:"pending until an Owner decides it; approved made them a member with the Employee role." example:"pending"`
	DecidedAt   *time.Time `json:"decided_at,omitempty" example:"2026-11-07T10:00:00Z"`
	UserID      string     `json:"user_id,omitempty" format:"uuid" doc:"Their user, once approved." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f91"`
}

// JoinRequestPage is the organisation's requests to join, newest first.
type JoinRequestPage struct {
	Items      []JoinRequestView `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// JoinDomainView is an email domain the organisation claimed for joining.
type JoinDomainView struct {
	Domain    string    `json:"domain" example:"cognizhi.com"`
	ClaimedAt time.Time `json:"claimed_at" example:"2026-11-07T09:00:00Z"`
}

// JoinDomainPage is the organisation's claimed domains.
type JoinDomainPage struct {
	Items      []JoinDomainView `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty" doc:"The cursor of the next page; absent on the last." example:"eyJhZnRlciI6ImNvZ25pemhpLmNvbSJ9"`
}

// JoinRequestPath names a request to join.
type JoinRequestPath struct {
	RequestID string `path:"request_id" format:"uuid" doc:"The request's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f90"`
}

// JoinDomainPath names a claimed domain.
type JoinDomainPath struct {
	Domain string `path:"domain" maxLength:"253" doc:"The email domain, lower case." example:"cognizhi.com"`
}

type joinOfferOutput struct{ Body JoinOffer }

type joinRequestInput struct{ Body JoinRequestBody }

type joinRequestOutput struct{ Body JoinRequestView }

type joinRequestListInput struct{ PageQuery }

type joinRequestListOutput struct{ Body JoinRequestPage }

type joinDomainListInput struct{ PageQuery }

type joinDomainListOutput struct{ Body JoinDomainPage }

type joinDomainOutput struct{ Body JoinDomainView }

func joinRequestOf(r storage.JoinRequest) JoinRequestView {
	v := JoinRequestView{ID: r.ID.String(), DisplayName: r.Name, Email: r.Email, Provider: r.Provider, RequestedAt: r.RequestedAt.UTC(), Status: r.Status}
	if r.DecidedAt != nil {
		t := r.DecidedAt.UTC()
		v.DecidedAt = &t
	}
	if r.UserID != nil {
		v.UserID = r.UserID.String()
	}
	return v
}

// JoinCodes are the refusals of joining by domain.
var JoinCodes = []string{storage.CodeJoin}

// joiner returns the caller when they are a person signed in to Fyniq in the browser without a Fyniq account, and the
// store's joining side.
func (a *API) joiner(ctx context.Context, what string) (authn.Principal, storage.Joining, error) {
	p, err := signedInPerson(ctx, what)
	if err != nil {
		return p, nil, err
	}
	j, ok := a.deps.Installation.(storage.Joining)
	if len(p.SessionHash) == 0 || !ok {
		return p, nil, problem(CodeSessionRequired, what+" needs a person signed in to Fyniq in the browser.", "Sign in to Fyniq in the browser and do it there.")
	}
	return p, j, nil
}

func registerJoin(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType}, locks...)

	register(a, authz.GetJoinOffer, huma.Operation{
		OperationID: "getJoinOffer", Method: http.MethodGet, Path: "/v1/session/join-offer", Tags: []string{"Session"},
		Summary: "Find the organisation you may ask to join",
		Description: "For a person signed in without a Fyniq account: the organisation that claimed your verified email's domain, when the " +
			"provider you signed in with lets people join by it (FR-ADM-08). 404 when there is none: ask for an invitation. " +
			authz.GetJoinOffer.Note + " Read-only; no side effects.",
	}, append([]string{CodeSessionRequired, CodeNotFound}, locks...), func(ctx context.Context, _ *struct{}) (*joinOfferOutput, error) {
		p, j, err := a.joiner(ctx, "Finding an organisation to join")
		if err != nil {
			return nil, err
		}
		o, ok, err := j.JoinOffer(ctx, p.SessionHash, a.deps.Clock.Now())
		if err != nil {
			return nil, err
		}
		if !ok || p.UserID != uuid.Nil {
			return nil, problem(CodeNotFound, "No organisation takes a request to join from this sign-in.", "Ask the organisation for an invitation")
		}
		return &joinOfferOutput{Body: JoinOffer{OrganisationName: o.Name, Domain: o.Domain, Email: o.Email}}, nil
	})

	register(a, authz.RequestToJoin, exempt(exempt(huma.Operation{
		OperationID: "requestToJoin", Method: http.MethodPost, Path: "/v1/session/join-requests", Tags: []string{"Session"},
		DefaultStatus: http.StatusCreated, Summary: "Ask to join by your email domain",
		Description: "Asks to join the organisation getJoinOffer names (FR-ADM-08): a request that grants nothing until an Owner approves it, " +
			"which makes you a member with the Employee role. One request waits at a time. " + authz.RequestToJoin.Note + " Side effects: " +
			"the request, its event in the organisation's trail and a security notice to its Owners.",
	}, "Prefer", "Asking to join only records a request an Owner decides; it is checked and done at once (FR-INT-14, rev 0.199)."),
		"Idempotency-Key", "One request waits at a time: sent again, it is refused and changes nothing (FR-INT-08, rev 0.199)."),
		append(append([]string{CodeSessionRequired}, JoinCodes...), writes...), func(ctx context.Context, in *joinRequestInput) (*joinRequestOutput, error) {
			p, j, err := a.joiner(ctx, "Asking to join")
			if err != nil {
				return nil, err
			}
			if p.UserID != uuid.Nil {
				return nil, problem(storage.CodeJoin, "You have a Fyniq account already.", "Ask an Owner of the organisation for an invitation")
			}
			r, err := j.RequestToJoin(ctx, p.SessionHash, strings.TrimSpace(in.Body.DisplayName), a.deps.Clock.Now())
			if err != nil {
				return nil, err
			}
			if a.deps.Outbox != nil {
				err := a.deps.Store.Do(ctx, r.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
					_, err := notify.Emit(ctx, *a.deps.Outbox, u, r.Organisation, notify.Event{Type: notify.JoinRequestType, RecordType: "join_request",
						RecordID: &r.ID, Link: "/settings/members", At: a.deps.Clock.Now(),
						Title:    r.Name + " (" + r.Email + ") asks to join by their email domain: approve or reject it in Members and roles",
						Audience: notify.Audience{Capability: string(authz.UsersAndRoles), Need: "A"}})
					return err
				})
				if err != nil {
					a.deps.Log.Error("api.notify.join_request", "error", err.Error(), "request_id", requestID(ctx))
				}
			}
			return &joinRequestOutput{Body: joinRequestOf(r)}, nil
		})

	register(a, authz.ListJoinRequests, huma.Operation{
		OperationID: "listJoinRequests", Method: http.MethodGet, Path: "/v1/members/join-requests", Tags: []string{"Members"},
		Summary: "List the requests to join by email domain",
		Description: "People who signed in with a verified email on one of the organisation's claimed domains and asked to join, newest first " +
			"(FR-ADM-08). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *joinRequestListInput) (*joinRequestListOutput, error) {
		p, _ := authn.From(ctx)
		out := JoinRequestPage{Items: []JoinRequestView{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.JoinRequests().List(ctx)
			if err != nil {
				return err
			}
			views := make([]JoinRequestView, 0, len(all))
			for _, r := range all {
				views = append(views, joinRequestOf(r))
			}
			out.Items, out.NextCursor, err = pageOf(views, func(v JoinRequestView) string { return v.ID }, in.PageQuery)
			return err
		})
		return &joinRequestListOutput{Body: out}, err
	})

	decide := func(approve bool) func(ctx context.Context, in *JoinRequestPath) (*joinRequestOutput, error) {
		return func(ctx context.Context, in *JoinRequestPath) (*joinRequestOutput, error) {
			p, err := signedInPerson(ctx, "Deciding a request to join")
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			if !p.SteppedUp(now) {
				return nil, problem(CodeStepUpRequired, "Deciding a request to join needs a second factor in the last 5 minutes (FR-ADM-08).",
					"Confirm it's you with your authenticator app or a passkey, then try again.")
			}
			id, err := parseID("request_id", in.RequestID)
			if err != nil {
				return nil, err
			}
			var out storage.JoinRequest
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				var err error
				if out, err = u.JoinRequests().Decide(ctx, p.UserID, id, approve, now); errors.Is(err, ports.ErrNotFound) {
					return problem(CodeNotFound, "The organisation has no pending request to join with that ID.", "List the requests to find their IDs")
				} else if err != nil {
					return err
				}
				if approve && out.UserID != nil {
					return a.notify(ctx, u, notify.Event{Type: "security.roles", RecordType: "join_request", RecordID: &out.ID, Link: "/settings/profile",
						Title: "Your request to join was approved: you are a member, as an Employee", Audience: notify.Audience{Users: []uuid.UUID{*out.UserID}}})
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
			return &joinRequestOutput{Body: joinRequestOf(out)}, nil
		}
	}
	decideNote := " Needs an Owner who passed step-up in the last 5 minutes. Side effects: the decision and its events in the organisation's trail."
	register(a, authz.ApproveJoinRequest, exempt(exempt(huma.Operation{
		OperationID: "approveJoinRequest", Method: http.MethodPost, Path: "/v1/members/join-requests/{request_id}/approve", Tags: []string{"Members"},
		Summary:     "Approve a request to join",
		Description: "Makes the person who asked a Fyniq user with the sign-in they asked from, and a member with the Employee role (FR-ADM-08)." + decideNote + " The person is told.",
	}, "Prefer", "Approving decides a request once; it is checked and done at once (FR-INT-14, rev 0.199)."), "Idempotency-Key",
		"A request is decided once: sent again, it answers that there is no pending request (FR-INT-08, rev 0.199)."),
		append([]string{CodeStepUpRequired, CodeSessionRequired, CodeNotFound}, append(JoinCodes, locks...)...), decide(true))
	register(a, authz.RejectJoinRequest, exempt(exempt(huma.Operation{
		OperationID: "rejectJoinRequest", Method: http.MethodPost, Path: "/v1/members/join-requests/{request_id}/reject", Tags: []string{"Members"},
		Summary:     "Reject a request to join",
		Description: "Rejects a request to join by email domain (FR-ADM-08): the person stays without an account." + decideNote,
	}, "Prefer", "Rejecting decides a request once; it is checked and done at once (FR-INT-14, rev 0.199)."), "Idempotency-Key",
		"A request is decided once: sent again, it answers that there is no pending request (FR-INT-08, rev 0.199)."),
		append([]string{CodeStepUpRequired, CodeSessionRequired, CodeNotFound}, locks...), decide(false))

	register(a, authz.ListJoinDomains, huma.Operation{
		OperationID: "listJoinDomains", Method: http.MethodGet, Path: "/v1/organisation/join-domains", Tags: []string{"Members"},
		Summary:     "List the email domains claimed for joining",
		Description: "The email domains the organisation claimed (FR-ADM-08): people with a verified email on one of them may ask to join, when a Super admin lets their provider. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *joinDomainListInput) (*joinDomainListOutput, error) {
		p, _ := authn.From(ctx)
		out := JoinDomainPage{Items: []JoinDomainView{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.JoinRequests().Domains(ctx)
			if err != nil {
				return err
			}
			views := make([]JoinDomainView, 0, len(all))
			for _, d := range all {
				views = append(views, JoinDomainView{Domain: d.Domain, ClaimedAt: d.ClaimedAt.UTC()})
			}
			out.Items, out.NextCursor, err = pageOf(views, func(v JoinDomainView) string { return v.Domain }, in.PageQuery)
			return err
		})
		return &joinDomainListOutput{Body: out}, err
	})

	domainOp := func(claim bool) func(ctx context.Context, in *JoinDomainPath) (*joinDomainOutput, error) {
		return func(ctx context.Context, in *JoinDomainPath) (*joinDomainOutput, error) {
			p, err := signedInPerson(ctx, "Changing the domains for joining")
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			if !p.SteppedUp(now) {
				return nil, problem(CodeStepUpRequired, "Changing the domains for joining needs a second factor in the last 5 minutes (FR-ADM-08).",
					"Confirm it's you with your authenticator app or a passkey, then try again.")
			}
			d := strings.ToLower(strings.TrimSpace(in.Domain))
			if !domainName.MatchString(d) {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "path.domain", Message: "is not a domain name",
					Fix: "Give the domain as people's email addresses end, such as example.com"}})
			}
			out := JoinDomainView{Domain: d, ClaimedAt: now.UTC()}
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				if claim {
					return u.JoinRequests().Claim(ctx, p.UserID, d, now)
				}
				all, err := u.JoinRequests().Domains(ctx)
				if err != nil {
					return err
				}
				i := slices.IndexFunc(all, func(x storage.JoinDomain) bool { return x.Domain == d })
				if i < 0 {
					return problem(CodeNotFound, "The organisation has not claimed "+d+".", "List the domains for joining")
				}
				out.ClaimedAt = all[i].ClaimedAt.UTC()
				_, err = u.JoinRequests().Release(ctx, d)
				return err
			})
			if err != nil {
				return nil, err
			}
			return &joinDomainOutput{Body: out}, nil
		}
	}
	domainNote := " Needs an Owner who passed step-up in the last 5 minutes. Side effects: the claim and its event in the organisation's trail."
	register(a, authz.ClaimJoinDomain, exempt(exempt(huma.Operation{
		OperationID: "claimJoinDomain", Method: http.MethodPut, Path: "/v1/organisation/join-domains/{domain}", Tags: []string{"Members"},
		Summary:     "Claim an email domain for joining",
		Description: "Claims an email domain, one organisation per domain (FR-ADM-08): people with a verified email on it may ask to join once a Super admin lets their provider; an Owner approves each." + domainNote,
	}, "Prefer", "Claiming is checked and done at once (FR-INT-14, rev 0.199)."), "Idempotency-Key", "Claiming a domain claimed already changes nothing (FR-INT-08, rev 0.199)."),
		append([]string{CodeStepUpRequired, CodeSessionRequired, CodeInvalid}, append(JoinCodes, locks...)...), domainOp(true))
	register(a, authz.ReleaseJoinDomain, exempt(huma.Operation{
		OperationID: "releaseJoinDomain", Method: http.MethodDelete, Path: "/v1/organisation/join-domains/{domain}", Tags: []string{"Members"},
		Summary:     "Release an email domain for joining",
		Description: "Gives up the organisation's claim on an email domain (FR-ADM-08): nobody asks to join by it any more; pending requests stay for an Owner to decide." + domainNote,
	}, "Prefer", "Releasing is checked and done at once (FR-INT-14, rev 0.199)."),
		append([]string{CodeStepUpRequired, CodeSessionRequired, CodeInvalid, CodeNotFound}, locks...), domainOp(false))
}
