package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the approval engine (FR-APR-01…05, FR-APR-10, FM-1, rev 0.36)

// ApprovalDiff is one thing a request changes: its value before and after.
type ApprovalDiff struct {
	Field  string `json:"field" doc:"What changes." example:"bank.tolerance_base"`
	Before string `json:"before" doc:"Its value now; empty when it has none." example:"1.00"`
	After  string `json:"after" doc:"Its value once approved; empty when it ends." example:"5.00"`
}

// ApprovalSignature is a signed approval or rejection (FR-APR-03).
type ApprovalSignature struct {
	Signer     string    `json:"signer" format:"uuid" doc:"The user who signed." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	SignerName string    `json:"signer_name" doc:"Their name." example:"Wei Ling Tan"`
	Decision   string    `json:"decision" enum:"approve,reject" doc:"What they decided." example:"approve"`
	Method     string    `json:"method" enum:"session_step_up,passkey,totp" doc:"The second factor that proved it was them: session_step_up is the step-up of their session; passkey and TOTP approvals arrive with p1-users-roles." example:"session_step_up"`
	SignedAt   time.Time `json:"signed_at" doc:"When." example:"2026-10-20T09:30:00Z"`
	Reason     string    `json:"reason,omitempty" doc:"A rejection's reason." example:"Not this month"`
	Voided     bool      `json:"voided" doc:"true once the request was voided because its record changed." example:"false"`
}

// Approval is a change waiting for, or decided by, a second approver (FM-1): what it changes, the envelope signed
// and its signatures.
type Approval struct {
	ID              string              `json:"id" format:"uuid" doc:"The request's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	ObjectType      string              `json:"object_type" enum:"organisation_setting,membership,member_invitation" doc:"What it changes." example:"organisation_setting"`
	ObjectKey       string              `json:"object_key" doc:"Which one: the setting's key, the membership's ID, or the invitation's code hash." example:"bank.tolerance_base"`
	Operation       string              `json:"operation" enum:"organisation_setting.change,role.assign,role.revoke,member_invitation.create" doc:"What executes on its approval." example:"organisation_setting.change"`
	Summary         string              `json:"summary" doc:"What it does, in a sentence." example:"Change Tolerance for base-currency differences from 1.00 to 5.00 from 2026-10-20"`
	Status          string              `json:"status" enum:"pending,approved,rejected,void,expired" doc:"pending waits for approval; approved executed it; void means its record or a dependency changed (FR-APR-05); expired after 7 days." example:"pending"`
	StatusReason    string              `json:"status_reason,omitempty" doc:"Why it was rejected or voided." example:"the setting changed after the request"`
	RequestedBy     string              `json:"requested_by" format:"uuid" doc:"The user who asked for it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	RequestedByName string              `json:"requested_by_name" doc:"Their name." example:"Ah Kow Lim"`
	RequestedAt     time.Time           `json:"requested_at" doc:"When it was asked for." example:"2026-10-20T09:00:00Z"`
	ExpiresAt       time.Time           `json:"expires_at" doc:"When it expires unless decided: 7 days after it was asked for (FR-APR-05)." example:"2026-10-27T09:00:00Z"`
	DecidedAt       *time.Time          `json:"decided_at,omitempty" doc:"When it was approved, rejected or voided." example:"2026-10-20T09:30:00Z"`
	Policy          string              `json:"policy" doc:"The approval policy's version it keeps (G4-057)." example:"founder/organisation_setting@2026-10-20.1"`
	Envelope        string              `json:"envelope" doc:"The approval envelope exactly as signed: RFC 8785 canonical JSON (FR-APR-10)." example:"{\"amounts\":[]}"`
	EnvelopeHash    string              `json:"envelope_hash" doc:"The envelope's SHA-256, in hex: send it to approve or reject exactly what you were shown." example:"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
	Diff            []ApprovalDiff      `json:"diff" doc:"What it changes, before and after (FR-APR-02)."`
	Signatures      []ApprovalSignature `json:"signatures" doc:"Its signed approvals and rejections, in order (FR-APR-03)."`
	YouMayApprove   bool                `json:"you_may_approve" doc:"true when you may approve or reject it now: it is pending, you did not ask for it and you hold the approval right." example:"true"`
	Version         int                 `json:"version" doc:"The request's version." example:"1"`
	InvitationCode  string              `json:"invitation_code,omitempty" doc:"Only in the 202 answer to an invitation: its one-time code, shown once, which works once the invitation is approved." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
}

// waitsForApproval makes write answer 202 Accepted with the request.
func (Approval) waitsForApproval() bool { return true }

// ApprovalPage is a page of approval requests, oldest first.
type ApprovalPage struct {
	Items      []Approval `json:"items" doc:"The requests on this page."`
	NextCursor string     `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// ApprovalDecision approves or rejects exactly the envelope you were shown.
type ApprovalDecision struct {
	EnvelopeHash string `json:"envelope_hash" minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$" doc:"The envelope_hash of the request you were shown: a request whose envelope differs is not signed (FR-APR-03)." example:"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
	Reason       string `json:"reason,omitempty" maxLength:"500" doc:"Why you reject it: needed to reject." example:"Not this month"`
}

// ApprovalPath names a request.
type ApprovalPath struct {
	ApprovalID string `path:"approval_id" format:"uuid" doc:"The request's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
}

type approvalListInput struct {
	PageQuery
	Status string `query:"status" enum:"pending,approved,rejected,void,expired,all" default:"pending" doc:"The requests to list: pending (the default), one other status, or all." example:"pending"`
}

type approvalGetInput struct{ ApprovalPath }

type approvalDecideInput struct {
	ApprovalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ApprovalDecision
}

type approvalListOutput struct{ Body ApprovalPage }

type approvalReadOutput struct {
	ETag string `header:"ETag" doc:"The request's version."`
	Body Approval
}

type approvalWriteOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

// ApprovalCodes are the approval engine's rules (internal/domain/approval).
var ApprovalCodes = []string{approval.CodeOwnRequest, approval.CodeNotPending, approval.CodeExpired, approval.CodeChanged,
	approval.CodeNotApprover, approval.CodePending, approval.CodeReason}

// stepUpMethod is how a session's step-up is recorded as the MFA claim of a signature: the second factor the
// session passed (SEC-01). Passkey and TOTP approvals bound to the envelope arrive with p1-users-roles (task 2.2).
const stepUpMethod = "session_step_up"

// steppedUpPerson returns the caller when they are a person with a Fyniq user who passed step-up within
// authn.StepUpWindow: FM-1 asks it of the requester and of every approver (FR-APR-04).
func (a *API) steppedUpPerson(ctx context.Context, what string) (authn.Principal, error) {
	p, _ := authn.From(ctx)
	if p.Via != authn.ViaSession || p.AI() {
		return p, problem(CodeSessionRequired, what+" needs a person signed in to Fyniq, not an OAuth client or the AI.",
			"Sign in to Fyniq in the browser and do it there.")
	}
	if !p.SteppedUp(a.deps.Clock.Now()) {
		return p, problem(CodeStepUpRequired, what+" needs step-up authentication in the last 5 minutes (FM-1).",
			"Confirm it's you with your passkey or authenticator app, then try again.")
	}
	if p.UserID == uuid.Nil {
		return p, problem(CodeSessionRequired, what+" needs a person with a Fyniq user, who signs it by name.",
			"Sign in to Fyniq in the browser with your own sign-in and do it there.")
	}
	return p, nil
}

// pendsApproval documents an operation that answers 202 Accepted with an approval request when its change needs a
// second approver, and its own body otherwise.
func pendsApproval(a *API, op huma.Operation, done reflect.Type) huma.Operation {
	reg := a.OpenAPI().Components.Schemas
	status := op.DefaultStatus
	if status == 0 {
		status = http.StatusOK
	}
	if op.Responses == nil {
		op.Responses = map[string]*huma.Response{}
	}
	op.Responses[fmt.Sprint(status)] = &huma.Response{Description: http.StatusText(status),
		Content: map[string]*huma.MediaType{"application/json": {Schema: reg.Schema(done, true, "")}}}
	op.Responses["202"] = &huma.Response{Description: "Accepted: the change needs a second approver (FM-1) and waits for approval; nothing changed yet. " +
		"The body is the approval request (rev 0.36).",
		Content: map[string]*huma.MediaType{"application/json": {Schema: reg.Schema(reflect.TypeFor[Approval](), true, "")}}}
	return op
}

// pendingChange is a change that waits for approval: what executes, what it changes and its amounts.
type pendingChange struct {
	ObjectType, ObjectKey, Operation, Summary string
	Execute                                   map[string]any
	Diff                                      []ApprovalDiff
	Amounts                                   []approval.Amount
}

// stored is what a request keeps of its change.
type stored struct {
	Execute map[string]any    `json:"execute"`
	Diff    []ApprovalDiff    `json:"diff"`
	Amounts []approval.Amount `json:"amounts"`
}

func storedOf(r storage.ApprovalRequest) (stored, error) {
	var s stored
	err := json.Unmarshal(r.Change, &s)
	return s, err
}

// envelopeOf builds the envelope of a change from the data in u now: the same builder runs when the change is
// requested and, with its rows locked, when it executes (FR-APR-10).
func (a *API) envelopeOf(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, c pendingChange, requester uuid.UUID, policy string) ([]byte, error) {
	e := approval.Envelope{Organisation: org, ObjectType: c.ObjectType, ObjectID: c.ObjectKey, Operation: c.Operation, Change: c.Execute,
		Amounts: c.Amounts, Policy: policy, TeamMode: approval.TeamModeFounder, RequestedBy: requester, Dependencies: map[string]any{}}
	switch c.ObjectType {
	case approval.ObjectSetting:
		d, ok := settings.Lookup(c.ObjectKey)
		if !ok {
			return nil, fmt.Errorf("approval: no setting %s", c.ObjectKey)
		}
		from, err := time.Parse(time.DateOnly, fmt.Sprint(c.Execute["effective_from"]))
		if err != nil {
			return nil, err
		}
		changes, err := u.Settings().Changes(ctx, d.Key)
		if err != nil {
			return nil, err
		}
		scheduled := []any{}
		for _, ch := range changes {
			if ch.EffectiveFrom.After(from) {
				scheduled = append(scheduled, fmt.Sprintf("%s=%s@%d", dateOf(ch.EffectiveFrom), ch.Value, ch.Version))
			}
		}
		e.ObjectVersion = latest(d, changes)
		e.Dependencies["setting_version"] = e.ObjectVersion
		e.Dependencies["value_in_effect"] = settings.On(d, changes, from).Value
		e.Dependencies["scheduled"] = scheduled
	case approval.ObjectMembership:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		all, err := u.Members().List(ctx, a.deps.Clock.Now())
		if err != nil {
			return nil, err
		}
		status, roles := "absent", []any{}
		if i := slices.IndexFunc(all, func(m storage.Member) bool { return m.ID == id }); i >= 0 {
			m := all[i]
			status, e.ObjectVersion = m.Status, m.Version
			for _, r := range m.Roles {
				live := r.Role
				if r.ExpiresAt != nil {
					live += "@" + r.ExpiresAt.UTC().Format(time.RFC3339)
				}
				roles = append(roles, live)
			}
		}
		e.Dependencies["member_status"] = status
		e.Dependencies["member_version"] = e.ObjectVersion
		e.Dependencies["live_roles"] = roles
	}
	return e.Canonical()
}

// requestApproval records a change that waits for a second approver (FM-1) and returns the request: the
// requester is a person who passed step-up, and the envelope is built in the same unit of work.
func (a *API) requestApproval(ctx context.Context, u storage.UnitOfWork, c pendingChange) (Approval, error) {
	p, err := a.steppedUpPerson(ctx, "A change that needs a second approver")
	if err != nil {
		return Approval{}, err
	}
	policy := approval.Policies[c.ObjectType]
	pending, err := u.Approvals().List(ctx, string(approval.Pending))
	if err != nil {
		return Approval{}, err
	}
	now := a.deps.Clock.Now()
	for _, r := range pending {
		if r.ObjectType == c.ObjectType && r.ObjectKey == c.ObjectKey && approval.StatusAt(requestOf(r), now) == approval.Pending {
			return Approval{}, problem(approval.CodePending, "A change to this record is waiting for approval already: "+r.Summary+".",
				"Approve or reject that request first, or wait for it to expire")
		}
		if r.ObjectType == c.ObjectType && r.ObjectKey == c.ObjectKey { // past its expiry: record that it expired
			if _, err := u.Approvals().Decide(ctx, r.ID, r.Version, string(approval.Expired), "", now); err != nil {
				return Approval{}, err
			}
		}
	}
	envelope, err := a.envelopeOf(ctx, u, p.Organisation, c, p.UserID, policy.Version)
	if err != nil {
		return Approval{}, err
	}
	if c.Diff == nil {
		c.Diff = []ApprovalDiff{}
	}
	if c.Amounts == nil {
		c.Amounts = []approval.Amount{}
	}
	change, err := json.Marshal(stored{Execute: c.Execute, Diff: c.Diff, Amounts: c.Amounts})
	if err != nil {
		return Approval{}, err
	}
	r, err := u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: c.ObjectType, ObjectKey: c.ObjectKey, Operation: c.Operation,
		Summary: c.Summary, Change: change, Envelope: string(envelope), EnvelopeHash: approval.Hash(envelope), PolicyVersion: policy.Version,
		RequestedBy: p.UserID, RequestedAt: now, RequesterMFAMethod: stepUpMethod, RequesterMFAAt: p.StepUpAt, ExpiresAt: now.Add(approval.TTL)})
	if err != nil {
		return Approval{}, err
	}
	return a.approvalOf(ctx, u, r)
}

func requestOf(r storage.ApprovalRequest) approval.Request {
	return approval.Request{Status: approval.Status(r.Status), RequestedBy: r.RequestedBy, ExpiresAt: r.ExpiresAt}
}

// approvalRow is the Appendix D row of the object a request changes: its policy's first step.
func approvalRow(objectType string) authz.Capability {
	return authz.Capability(approval.Policies[objectType].Steps[0].Row)
}

// mayApprove reports whether p holds the approve right (A) on the request's row; the AI never does.
func mayApprove(p authn.Principal, objectType string) bool {
	return authz.Decide(authz.Policy{Operation: "approve", Capability: approvalRow(objectType), Need: authz.Approve}, p.Roles, p.AI()).Allowed
}

// maySee reports whether p may see a request: they asked for it, or hold a right on its object's row.
func maySee(p authn.Principal, r storage.ApprovalRequest) bool {
	if p.UserID != uuid.Nil && p.UserID == r.RequestedBy {
		return true
	}
	return authz.Decide(authz.Policy{Operation: "view", Capability: approvalRow(r.ObjectType), Need: authz.View}, p.Roles, p.AI()).Allowed
}

// approvalOf is a request as the API shows it to the caller.
func (a *API) approvalOf(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest) (Approval, error) {
	p, _ := authn.From(ctx)
	now := a.deps.Clock.Now()
	s, err := storedOf(r)
	if err != nil {
		return Approval{}, err
	}
	sigs, err := u.Approvals().Signatures(ctx, r.ID)
	if err != nil {
		return Approval{}, err
	}
	names, err := memberNames(ctx, u, now)
	if err != nil {
		return Approval{}, err
	}
	status := approval.StatusAt(requestOf(r), now)
	out := Approval{ID: r.ID.String(), ObjectType: r.ObjectType, ObjectKey: r.ObjectKey, Operation: r.Operation, Summary: r.Summary,
		Status: string(status), StatusReason: r.StatusReason, RequestedBy: r.RequestedBy.String(), RequestedByName: names[r.RequestedBy],
		RequestedAt: r.RequestedAt.UTC(), ExpiresAt: r.ExpiresAt.UTC(), DecidedAt: r.DecidedAt, Policy: r.PolicyVersion, Envelope: r.Envelope,
		EnvelopeHash: hex.EncodeToString(r.EnvelopeHash), Diff: s.Diff, Signatures: []ApprovalSignature{}, Version: r.Version,
		YouMayApprove: status == approval.Pending && p.UserID != uuid.Nil && p.UserID != r.RequestedBy && mayApprove(p, r.ObjectType)}
	if out.Diff == nil {
		out.Diff = []ApprovalDiff{}
	}
	for _, sg := range sigs {
		out.Signatures = append(out.Signatures, ApprovalSignature{Signer: sg.Signer.String(), SignerName: names[sg.Signer], Decision: sg.Decision,
			Method: sg.MFAMethod, SignedAt: sg.SignedAt.UTC(), Reason: sg.Reason, Voided: sg.VoidedAt != nil})
	}
	return out, nil
}

// memberNames are the names of the organisation's active members, by user.
func memberNames(ctx context.Context, u storage.UnitOfWork, now time.Time) (map[uuid.UUID]string, error) {
	ms, err := u.Members().List(ctx, now)
	out := make(map[uuid.UUID]string, len(ms))
	for _, m := range ms {
		out[m.UserID] = m.DisplayName
	}
	return out, err
}

// blocked is a decision that ends a request without executing it: the request is voided or expired in a unit of
// work of its own, so the end is kept, and the caller gets the problem.
type blocked struct {
	id      uuid.UUID
	version int
	status  approval.Status
	reason  string
	problem *Problem
}

func (b *blocked) Error() string { return b.problem.Detail }

// end records a blocked request's end, if it is still pending at the version the decision saw.
func (a *API) end(ctx context.Context, b *blocked) error {
	p, _ := authn.From(ctx)
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.Approvals().Lock(ctx, b.id)
		if err != nil || r.Status != string(approval.Pending) || r.Version != b.version {
			return err
		}
		_, err = u.Approvals().Decide(ctx, b.id, b.version, string(b.status), b.reason, a.deps.Clock.Now())
		return err
	})
	if err != nil {
		return err
	}
	return b.problem
}

// sign checks that the caller may sign the request now and returns it locked, with what it keeps: they passed
// step-up, hold the approve right, did not ask for it, and sent the hash of its envelope.
func (a *API) sign(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, ifMatch, hash string) (storage.ApprovalRequest, error) {
	r, err := u.Approvals().Lock(ctx, id)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && !maySee(p, r)) {
		return r, problem(CodeNotFound, "No approval request with that ID exists in this organisation.", "List the approvals to find their IDs")
	}
	if err != nil {
		return r, err
	}
	if !mayApprove(p, r.ObjectType) {
		return r, problem(approval.CodeNotApprover, "You hold no approval right on "+string(approvalRow(r.ObjectType))+" (Appendix D).",
			"Ask a member who holds it, such as an Owner, to decide it")
	}
	now := a.deps.Clock.Now()
	// Where the request stands says more than a stale ETag: decided, void or expired come first.
	if x := approval.CheckSigner(requestOf(r), p.UserID, now); x != nil {
		pr := problem(x.Code, strings.ToUpper(x.Message[:1])+x.Message[1:]+".", x.Fix)
		if x.Code == approval.CodeExpired {
			return r, &blocked{id: r.ID, version: r.Version, status: approval.Expired, problem: pr}
		}
		return r, pr
	}
	if err := checkIfMatch(ifMatch, etag(r.Version)); err != nil {
		return r, err
	}
	if hash != hex.EncodeToString(r.EnvelopeHash) {
		return r, problem(approval.CodeChanged, "The envelope you were shown is not the request's.",
			"Read the request again and decide what it holds now")
	}
	return r, nil
}

// execute carries out an approved request's change in the approving unit of work, audited with the approval
// (AUD-01).
func (a *API) execute(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	audit := storage.AuditFrom(ctx)
	audit.ApprovalIDs = append(slices.Clone(audit.ApprovalIDs), r.ID.String())
	ctx = storage.WithAudit(ctx, audit)
	x := s.Execute
	text := func(k string) string { v, _ := x[k].(string); return v }
	switch r.Operation {
	case approval.OpSettingChange:
		_, err := a.recordSetting(ctx, u, r.ObjectKey, text("value"), text("effective_from"))
		return err
	case approval.OpRoleAssign, approval.OpRoleRevoke:
		id, err := uuid.Parse(text("member_id"))
		if err != nil {
			return err
		}
		if r.Operation == approval.OpRoleRevoke {
			_, err = a.revokeRoleNow(ctx, u, id, text("role"))
			return err
		}
		var expires *time.Time
		if e := text("expires_at"); e != "" {
			t, err := time.Parse(time.RFC3339, e)
			if err != nil {
				return err
			}
			expires = &t
		}
		_, err = a.assignRoleNow(ctx, u, id, text("role"), expires)
		return err
	case approval.OpInvite:
		hash, err := hex.DecodeString(text("code_hash"))
		if err != nil {
			return err
		}
		var roles []string
		if rs, ok := x["roles"].([]any); ok {
			for _, r := range rs {
				roles = append(roles, fmt.Sprint(r))
			}
		}
		var expires *time.Time
		if e := text("roles_expire_at"); e != "" {
			t, err := time.Parse(time.RFC3339, e)
			if err != nil {
				return err
			}
			expires = &t
		}
		_, err = u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: text("display_name"), Roles: roles,
			RolesExpireAt: expires, ExpiresAt: a.deps.Clock.Now().Add(InvitationTTL)})
		return err
	}
	return fmt.Errorf("approval: no operation %s", r.Operation)
}

func registerApprovals(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.ListApprovals, huma.Operation{
		OperationID: "listApprovals", Method: http.MethodGet, Path: "/v1/approvals", Tags: []string{"Approvals"},
		Summary: "List the approval requests",
		Description: "The approval requests you asked for or hold a right on the object of, oldest first: pending ones by default, each " +
			"saying whether you may approve it now (FR-APR-01, FM-1). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *approvalListInput) (*approvalListOutput, error) {
		p, _ := authn.From(ctx)
		now := a.deps.Clock.Now()
		var all []Approval
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			rs, err := u.Approvals().List(ctx, "")
			if err != nil {
				return err
			}
			for _, r := range rs {
				if !maySee(p, r) {
					continue
				}
				if st := approval.StatusAt(requestOf(r), now); in.Status != "all" && string(st) != cmpOr(in.Status, "pending") {
					continue
				}
				v, err := a.approvalOf(ctx, u, r)
				if err != nil {
					return err
				}
				all = append(all, v)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(v Approval) string { return v.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &approvalListOutput{Body: ApprovalPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.GetApproval, huma.Operation{
		OperationID: "getApproval", Method: http.MethodGet, Path: "/v1/approvals/{approval_id}", Tags: []string{"Approvals"},
		Summary: "Get an approval request",
		Description: "One approval request with exactly what an approver signs: the envelope (RFC 8785 canonical JSON) and its SHA-256, what " +
			"it changes before and after, and its signatures (FR-APR-02, FR-APR-03, FR-APR-10). Shown to its requester and to whoever holds a " +
			"right on its object. Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *approvalGetInput) (*approvalReadOutput, error) {
		id, err := parseID("approval_id", in.ApprovalID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out approvalReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.Approvals().Get(ctx, id)
			if errors.Is(err, ports.ErrNotFound) || (err == nil && !maySee(p, r)) {
				return problem(CodeNotFound, "No approval request with that ID exists in this organisation.", "List the approvals to find their IDs")
			}
			if err != nil {
				return err
			}
			out.Body, err = a.approvalOf(ctx, u, r)
			out.ETag = etag(out.Body.Version)
			return err
		})
		return &out, err
	})

	register(a, authz.ApproveApproval, huma.Operation{
		OperationID: "approveApproval", Method: http.MethodPost, Path: "/v1/approvals/{approval_id}/approve", Tags: []string{"Approvals"},
		Summary: "Approve a request",
		Description: "Signs the request's envelope and, once its policy's steps are signed, carries out its change in the same transaction " +
			"(FR-APR-03, FR-APR-10, FM-1). Needs a person signed in who passed step-up in the last 5 minutes, holds the approval right (A) on " +
			"the object's Appendix D row, and did not ask for it; and envelope_hash must be the request's, and If-Match its ETag. The envelope is rebuilt from " +
			"current data with its rows locked: if the record or any dependency changed since the request, nothing executes, the request " +
			"becomes void and APR-04 says so (FR-APR-05). Side effects: the signed approval, the request approved, the change itself and their " +
			"audit events, which name the approval. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeStale, CodeIfMatchRequired, storage.CodeApproval, storage.CodeVersion, storage.CodeMembership},
		append(append(append(locks, ApprovalCodes...), MemberCodes...), SettingCodes...)...),
		func(ctx context.Context, in *approvalDecideInput) (*approvalWriteOutput, error) {
			id, err := parseID("approval_id", in.ApprovalID)
			if err != nil {
				return nil, err
			}
			p, err := a.steppedUpPerson(ctx, "Approving")
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (decided, error) {
				r, err := a.sign(ctx, u, p, id, in.IfMatch, in.Body.EnvelopeHash)
				if err != nil {
					return decided{}, err
				}
				s, err := storedOf(r)
				if err != nil {
					return decided{}, err
				}
				// ENG-47: the request's row first (Lock), then the members' lock for a change to the members.
				if r.ObjectType != approval.ObjectSetting {
					if _, err := u.Members().Count(ctx); err != nil {
						return decided{}, err
					}
				}
				envelope, err := a.envelopeOf(ctx, u, p.Organisation, pendingChange{ObjectType: r.ObjectType, ObjectKey: r.ObjectKey,
					Operation: r.Operation, Execute: s.Execute, Amounts: s.Amounts}, r.RequestedBy, r.PolicyVersion)
				if err != nil {
					return decided{}, err
				}
				if !bytes.Equal(envelope, []byte(r.Envelope)) {
					return decided{}, &blocked{id: r.ID, version: r.Version, status: approval.Void,
						reason: "the record or a dependency changed after the request",
						problem: problem(approval.CodeChanged, "The record or one of its dependencies changed since the request, so its envelope differs: nothing was changed, and the request is void.",
							"Read the record again and ask for the change again if it is still needed")}
				}
				now := a.deps.Clock.Now()
				if _, err := u.Approvals().Sign(ctx, storage.NewApprovalSignature{RequestID: r.ID, Step: 1, Signer: p.UserID, Decision: "approve",
					EnvelopeHash: r.EnvelopeHash, MFAMethod: stepUpMethod, MFAAt: p.StepUpAt, SignedAt: now}); err != nil {
					return decided{}, err
				}
				sigs, err := u.Approvals().Signatures(ctx, r.ID)
				if err != nil {
					return decided{}, err
				}
				if signed(sigs, approval.Policies[r.ObjectType]) {
					if r, err = u.Approvals().Decide(ctx, r.ID, r.Version, string(approval.Approved), "", now); err != nil {
						return decided{}, err
					}
					if err := a.execute(ctx, u, r, s); err != nil {
						return decided{}, err
					}
				}
				v, err := a.approvalOf(ctx, u, r)
				return decided{v}, err
			})
			var b *blocked
			if errors.As(err, &b) {
				if w.Previewed {
					return nil, b.problem
				}
				return nil, a.end(ctx, b)
			}
			if err != nil {
				return nil, err
			}
			return &approvalWriteOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body.Approval}, nil
		})

	register(a, authz.RejectApproval, huma.Operation{
		OperationID: "rejectApproval", Method: http.MethodPost, Path: "/v1/approvals/{approval_id}/reject", Tags: []string{"Approvals"},
		Summary: "Reject a request",
		Description: "Signs a rejection of the request's envelope, with your reason; the change never executes (FR-APR-03, FM-1). Needs a " +
			"person signed in who passed step-up in the last 5 minutes, holds the approval right (A) on the object's Appendix D row, and " +
			"did not ask for it; and envelope_hash must be the request's, and If-Match its ETag. Side effects: the signed rejection, the request rejected and their " +
			"audit events. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeStale, CodeIfMatchRequired, storage.CodeApproval, storage.CodeVersion}, append(locks, ApprovalCodes...)...),
		func(ctx context.Context, in *approvalDecideInput) (*approvalWriteOutput, error) {
			id, err := parseID("approval_id", in.ApprovalID)
			if err != nil {
				return nil, err
			}
			p, err := a.steppedUpPerson(ctx, "Rejecting")
			if err != nil {
				return nil, err
			}
			reason := strings.TrimSpace(in.Body.Reason)
			if reason == "" {
				return nil, invalid([]FieldProblem{{Code: approval.CodeReason, Field: "body.reason", Message: "a rejection needs its reason",
					Fix: "Say why you reject it, so the requester knows what to change"}})
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (decided, error) {
				r, err := a.sign(ctx, u, p, id, in.IfMatch, in.Body.EnvelopeHash)
				if err != nil {
					return decided{}, err
				}
				now := a.deps.Clock.Now()
				if _, err := u.Approvals().Sign(ctx, storage.NewApprovalSignature{RequestID: r.ID, Step: 1, Signer: p.UserID, Decision: "reject",
					EnvelopeHash: r.EnvelopeHash, MFAMethod: stepUpMethod, MFAAt: p.StepUpAt, SignedAt: now, Reason: reason}); err != nil {
					return decided{}, err
				}
				if r, err = u.Approvals().Decide(ctx, r.ID, r.Version, string(approval.Rejected), reason, now); err != nil {
					return decided{}, err
				}
				v, err := a.approvalOf(ctx, u, r)
				return decided{v}, err
			})
			var b *blocked
			if errors.As(err, &b) {
				if w.Previewed {
					return nil, b.problem
				}
				return nil, a.end(ctx, b)
			}
			if err != nil {
				return nil, err
			}
			return &approvalWriteOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body.Approval}, nil
		})
}

// decided is an approval decision's answer: the request, answered 200 (never 202, which a change that waits
// for approval answers).
type decided struct{ Approval }

func (decided) waitsForApproval() bool { return false }

// signed reports whether every step of the policy has its quorum of approvals that are not voided.
func signed(sigs []storage.ApprovalSignature, p approval.Policy) bool {
	for i, step := range p.Steps {
		n := 0
		for _, s := range sigs {
			if s.Step == i+1 && s.Decision == "approve" && s.VoidedAt == nil {
				n++
			}
		}
		if n < step.Quorum {
			return false
		}
	}
	return true
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
