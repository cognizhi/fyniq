package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ delegation of approval authority (FR-APR-06; p2-mobile-approvals-automation design D1)
//
// An approver delegates their authority to another member for up to 90 days, for example during leave. A delegation lends no right: the delegate must hold the approval right
// on the object's row themselves, signs as themselves with their own step-up or passkey, and the signature records on whose behalf. It only makes the delegate eligible for a
// step the delegator could sign. Nobody delegates for someone else, and no AI or MCP client makes or ends one (§9.6).

// ApprovalDelegationInput is what an approver delegates.
type ApprovalDelegationInput struct {
	Delegate string `json:"delegate" format:"uuid" doc:"The member who may sign for you: an active member who holds an approval right themselves. A delegation lends no right." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	StartsOn string `json:"starts_on" format:"date" doc:"The first day, in Singapore time." example:"2099-03-10"`
	EndsOn   string `json:"ends_on" format:"date" doc:"The last day, at most 90 days after the first." example:"2099-03-14"`
	Reason   string `json:"reason" minLength:"1" maxLength:"300" doc:"Why, such as the leave it covers." example:"Annual leave"`
}

// ApprovalDelegation is a delegation of approval authority.
type ApprovalDelegation struct {
	ID            string     `json:"id" format:"uuid" doc:"The delegation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	Delegator     string     `json:"delegator" format:"uuid" doc:"Whose authority it lends." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	DelegatorName string     `json:"delegator_name" doc:"Their name." example:"Ah Kow Lim"`
	Delegate      string     `json:"delegate" format:"uuid" doc:"Who may sign for them." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	DelegateName  string     `json:"delegate_name" doc:"Their name." example:"Wei Ling Tan"`
	StartsOn      string     `json:"starts_on" format:"date" doc:"The first day." example:"2099-03-10"`
	EndsOn        string     `json:"ends_on" format:"date" doc:"The last day." example:"2099-03-14"`
	Reason        string     `json:"reason" doc:"Why." example:"Annual leave"`
	Status        string     `json:"status" enum:"active,ended" doc:"active until it is ended; it then lends nothing. A delegation past its last day is active but not in force: see in_force." example:"active"`
	InForce       bool       `json:"in_force" doc:"true when the delegate may sign for the delegator today: active, and between its first and last day." example:"true"`
	EndedAt       *time.Time `json:"ended_at,omitempty" doc:"When it was ended." example:"2099-03-12T09:00:00Z"`
	Version       int        `json:"version" doc:"The delegation's version; it makes the ETag." example:"1"`
	CreatedAt     time.Time  `json:"created_at" doc:"When it was made." example:"2099-03-01T09:00:00Z"`
}

// ApprovalDelegationPage is a page of delegations, oldest first.
type ApprovalDelegationPage struct {
	Items      []ApprovalDelegation `json:"items" nullable:"false" doc:"The delegations."`
	NextCursor string               `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjEifQ"`
}

type (
	// ApprovalDelegationPath names a delegation.
	ApprovalDelegationPath struct {
		DelegationID string `path:"delegation_id" format:"uuid" doc:"The delegation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c8a01"`
	}
	approvalDelegationListInput struct {
		PageQuery
		Scope string `query:"scope" enum:"mine,all" default:"mine" doc:"mine: those you delegated or hold; all: every delegation of the organisation, for an Owner." example:"mine"`
	}
	approvalDelegationListOutput  struct{ Body ApprovalDelegationPage }
	approvalDelegationCreateInput struct {
		KeyHeader
		PreferHeader
		Body ApprovalDelegationInput
	}
	approvalDelegationEndInput struct {
		ApprovalDelegationPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	approvalDelegationOutput struct {
		ETag     string `header:"ETag" doc:"The delegation's version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ApprovalDelegation
	}
)

// delegationOf is a delegation as the API shows it, in force on the business day today.
func delegationOf(d storage.ApprovalDelegation, names map[uuid.UUID]string, today time.Time) ApprovalDelegation {
	dom := domainDelegation(d)
	return ApprovalDelegation{ID: d.ID.String(), Delegator: d.Delegator.String(), DelegatorName: names[d.Delegator], Delegate: d.Delegate.String(), DelegateName: names[d.Delegate],
		StartsOn: d.StartsOn.Format("2006-01-02"), EndsOn: d.EndsOn.Format("2006-01-02"), Reason: d.Reason, Status: d.Status, InForce: dom.Live(today), EndedAt: d.EndedAt,
		Version: d.Version, CreatedAt: d.CreatedAt.UTC()}
}

func domainDelegation(d storage.ApprovalDelegation) approval.Delegation {
	return approval.Delegation{ID: d.ID, Delegator: d.Delegator, Delegate: d.Delegate, StartsOn: d.StartsOn, EndsOn: d.EndsOn, Ended: d.Status == "ended"}
}

// businessDay is the business date of now in Singapore, as a date.
func businessDay(now time.Time) time.Time {
	y, m, d := now.In(appclock.Singapore).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// mayApproveSomething reports whether roles hold an approval right on any object's row: the approval engine's A on the Approvals capability.
func mayApproveSomething(roles []authz.Role) bool {
	return authz.Decide(authz.Policy{Operation: "delegate", Capability: authz.Approvals, Need: authz.Approve}, roles, false).Allowed
}

func registerApprovalDelegations(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	people := []string{CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType}
	idem := []string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}

	register(a, authz.ListApprovalDelegations, huma.Operation{
		OperationID: "listApprovalDelegations", Method: http.MethodGet, Path: "/v1/approval-delegations", Tags: []string{"Approvals"},
		Summary: "List delegations of approval authority",
		Description: "The delegations you made or hold (FR-APR-06), oldest first, a page at a time; with scope=all an Owner sees every delegation of the organisation. Each says whether it is in force today. " +
			"Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeSessionRequired}, locks...), func(ctx context.Context, in *approvalDelegationListInput) (*approvalDelegationListOutput, error) {
		p, _ := authn.From(ctx)
		out := ApprovalDelegationPage{Items: []ApprovalDelegation{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.ApprovalDelegations().List(ctx)
			if err != nil {
				return err
			}
			if in.Scope != "all" || !slices.Contains(p.Roles, authz.Owner) {
				all = slices.DeleteFunc(all, func(d storage.ApprovalDelegation) bool { return d.Delegator != p.UserID && d.Delegate != p.UserID })
			}
			names, err := memberNames(ctx, u, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(d storage.ApprovalDelegation) string { return d.ID.String() }, in.PageQuery)
			today := businessDay(a.deps.Clock.Now())
			for _, d := range page {
				out.Items = append(out.Items, delegationOf(d, names, today))
			}
			out.NextCursor = next
			return err
		})
		return &approvalDelegationListOutput{Body: out}, err
	})

	register(a, authz.CreateApprovalDelegation, huma.Operation{
		OperationID: "createApprovalDelegation", Method: http.MethodPost, Path: "/v1/approval-delegations", Tags: []string{"Approvals"}, DefaultStatus: http.StatusCreated,
		Summary: "Delegate your approval authority",
		Description: "Lets another member sign for you for up to 90 days (FR-APR-06), for example while you are on leave. It lends no right: the delegate must hold an approval right themselves, " +
			"signs as themselves with their own step-up or passkey, and each signature records on whose behalf it was made. It makes them eligible for a step you could sign, and for none other; " +
			"you and your delegate never both count towards one step's quorum. You delegate only your own authority. Needs a person signed in who passed step-up in the last 5 minutes and holds an " +
			"approval right. Side effects: the delegation and its audit event, and a notice to you and your delegate. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves " +
			"nothing. On the never-list for the AI and MCP (§9.6).",
	}, append(append(append([]string{CodeNotFound, approval.CodeDelegation}, people...), idem...), locks...),
		func(ctx context.Context, in *approvalDelegationCreateInput) (*approvalDelegationOutput, error) {
			p, err := a.stepUp(ctx, "Delegating approval authority")
			if err != nil {
				return nil, err
			}
			delegate, err := parseID("body.delegate", in.Body.Delegate)
			if err != nil {
				return nil, err
			}
			from, to, err := delegationDays(in.Body)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ApprovalDelegation, error) {
				now := a.deps.Clock.Now()
				members, err := u.Members().List(ctx, now)
				if err != nil {
					return ApprovalDelegation{}, err
				}
				active, approves := false, false
				if i := slices.IndexFunc(members, func(m storage.Member) bool { return m.UserID == delegate }); i >= 0 {
					active = members[i].Status == "active"
					approves = mayApproveSomething(rolesOfMember(members[i]))
				}
				if fp := approval.CheckDelegation(p.UserID, delegate, from, to, businessDay(now), active, approves); len(fp) > 0 {
					return ApprovalDelegation{}, problem(approval.CodeDelegation, "The delegation cannot be made: "+fp[0].Field+" "+fp[0].Message+".", fp[0].Fix, delegationProblems(fp)...)
				}
				d, err := u.ApprovalDelegations().Create(ctx, storage.NewApprovalDelegation{Delegator: p.UserID, Delegate: delegate, StartsOn: from, EndsOn: to,
					Reason: in.Body.Reason, At: now})
				if err != nil {
					return ApprovalDelegation{}, err
				}
				names := map[uuid.UUID]string{}
				for _, m := range members {
					names[m.UserID] = m.DisplayName
				}
				if err := a.notifyDelegation(ctx, u, d, "approval.delegated", names[d.Delegator]+" delegated their approvals to "+names[d.Delegate]+" from "+d.StartsOn.Format("2006-01-02")+" to "+d.EndsOn.Format("2006-01-02")); err != nil {
					return ApprovalDelegation{}, err
				}
				return delegationOf(d, names, businessDay(now)), nil
			})
			if err != nil {
				return nil, err
			}
			return &approvalDelegationOutput{ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.EndApprovalDelegation, huma.Operation{
		OperationID: "endApprovalDelegation", Method: http.MethodPost, Path: "/v1/approval-delegations/{delegation_id}/end", Tags: []string{"Approvals"},
		Summary: "End a delegation of approval authority",
		Description: "Ends a delegation at once (FR-APR-06): its delegate can sign for nobody under it from now on, and what they signed stays. The delegator ends their own; an Owner may end any. " +
			"Over the version you read (If-Match). Needs a person signed in who passed step-up in the last 5 minutes. Side effects: the end and its audit event, and a notice to both. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing. On the never-list for the AI and MCP (§9.6).",
	}, append(append(append([]string{CodeNotFound, approval.CodeDelegationNotFor, CodeIfMatchRequired, CodeStale, storage.CodeVersion}, people...), idem...), locks...),
		func(ctx context.Context, in *approvalDelegationEndInput) (*approvalDelegationOutput, error) {
			p, err := a.stepUp(ctx, "Ending a delegation")
			if err != nil {
				return nil, err
			}
			id, err := parseID("delegation_id", in.DelegationID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ApprovalDelegation, error) {
				d, err := u.ApprovalDelegations().Lock(ctx, id)
				if errors.Is(err, ports.ErrNotFound) || (err == nil && d.Delegator != p.UserID && d.Delegate != p.UserID && !slices.Contains(p.Roles, authz.Owner)) {
					return ApprovalDelegation{}, problem(CodeNotFound, "No delegation with that ID exists for you in this organisation.", "List your delegations to find their IDs")
				}
				if err != nil {
					return ApprovalDelegation{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(d.Version)); err != nil {
					return ApprovalDelegation{}, err
				}
				// The delegate may drop what was lent them, and an Owner may end any; nobody else.
				if d.Delegator != p.UserID && d.Delegate != p.UserID && !slices.Contains(p.Roles, authz.Owner) {
					return ApprovalDelegation{}, problem(approval.CodeDelegationNotFor, "Only the delegator, the delegate or an Owner ends a delegation.", "Ask the delegator or an Owner")
				}
				if d.Status != "active" {
					return ApprovalDelegation{}, problem(approval.CodeDelegationNotFor, "The delegation was ended already.", "Nothing to do; make a new delegation if one is needed")
				}
				now := a.deps.Clock.Now()
				d, err = u.ApprovalDelegations().End(ctx, id, d.Version, p.UserID, now)
				if err != nil {
					return ApprovalDelegation{}, err
				}
				names, err := memberNames(ctx, u, now)
				if err != nil {
					return ApprovalDelegation{}, err
				}
				if err := a.notifyDelegation(ctx, u, d, "approval.delegated", names[d.Delegator]+"'s delegation to "+names[d.Delegate]+" was ended"); err != nil {
					return ApprovalDelegation{}, err
				}
				return delegationOf(d, names, businessDay(now)), nil
			})
			if err != nil {
				return nil, err
			}
			return &approvalDelegationOutput{ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}

// delegationDays reads a delegation's two dates.
func delegationDays(in ApprovalDelegationInput) (from, to time.Time, err error) {
	if from, err = time.Parse("2006-01-02", in.StartsOn); err != nil {
		return from, to, problem(approval.CodeDelegation, "The first day is not a date.", "Write it as YYYY-MM-DD",
			FieldProblem{Code: approval.CodeDelegation, Field: "body.starts_on", Message: "is not a date", Fix: "Write it as YYYY-MM-DD"})
	}
	if to, err = time.Parse("2006-01-02", in.EndsOn); err != nil {
		return from, to, problem(approval.CodeDelegation, "The last day is not a date.", "Write it as YYYY-MM-DD",
			FieldProblem{Code: approval.CodeDelegation, Field: "body.ends_on", Message: "is not a date", Fix: "Write it as YYYY-MM-DD"})
	}
	return from, to, nil
}

// delegationProblems are domain field problems as the API's.
func delegationProblems(fp []approval.FieldProblem) []FieldProblem {
	out := make([]FieldProblem, len(fp))
	for i, f := range fp {
		out[i] = FieldProblem{Code: f.Code, Field: f.Field, Message: f.Message, Fix: f.Fix}
	}
	return out
}

// rolesOfMember are a member's roles as authz roles.
func rolesOfMember(m storage.Member) []authz.Role {
	out := make([]authz.Role, len(m.Roles))
	for i, r := range m.Roles {
		out[i] = authz.Role(r.Role)
	}
	return out
}

// notifyDelegation tells the delegator and the delegate that a delegation was made or ended.
func (a *API) notifyDelegation(ctx context.Context, u storage.UnitOfWork, d storage.ApprovalDelegation, typ, title string) error {
	return a.notify(ctx, u, notify.Event{Type: typ, RecordType: "approval_delegation", RecordID: &d.ID, Title: title,
		Link: "/delegations", Audience: notify.Audience{Users: []uuid.UUID{d.Delegator, d.Delegate}}})
}
