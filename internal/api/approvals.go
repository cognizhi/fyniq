package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/notify"
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
	Method     string    `json:"method" enum:"session_step_up,passkey,totp" doc:"The second factor that proved it was them: session_step_up is the step-up of their session (at sign-in or with a passkey); passkey is a passkey that signed this request's envelope hash itself (FR-APR-03, rev 0.44)." example:"session_step_up"`
	SignedAt   time.Time `json:"signed_at" doc:"When." example:"2026-10-20T09:30:00Z"`
	Reason     string    `json:"reason,omitempty" doc:"A rejection's reason." example:"Not this month"`
	Voided     bool      `json:"voided" doc:"true once the request was voided because its record changed." example:"false"`
	PasskeyID  string    `json:"passkey_id,omitempty" format:"uuid" doc:"For a passkey signature: the passkey that signed the envelope's hash; its assertion is kept so it can be verified again (FR-APR-03)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f40"`
}

// Approval is a change waiting for, or decided by, a second approver (FM-1): what it changes, the envelope signed
// and its signatures.
type Approval struct {
	ID              string              `json:"id" format:"uuid" doc:"The request's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	ObjectType      string              `json:"object_type" enum:"organisation_setting,membership,member_invitation,approval_policy,statutory_pack,team_mode,period,journal,opening_balance,bill,bill_payment,credit_application,supplier_refund,purchase_order,document_template,invoice,customer_receipt,write_off,customer_credit_application,customer_refund,customer_deposit,recurring_invoice,connector,bank_rule,expense_claim" doc:"What it changes." example:"organisation_setting"`
	ObjectKey       string              `json:"object_key" doc:"Which one: the setting's key, the membership's ID, the invitation's code hash, the object type whose approval policy changes, the statutory pack version, or organisation for the team mode." example:"bank.tolerance_base"`
	Operation       string              `json:"operation" enum:"organisation_setting.change,role.assign,role.revoke,member_invitation.create,approval_policy.change,statutory_pack.activate,team_mode.change,period.reopen,journal.post,journal.reverse,journal.edit,opening_balance.edit,bill.post,bill.void,bill.edit,bill_payment.void,credit_application.void,supplier_refund.void,purchase_order.approve,document_template.publish,invoice.post,invoice.void,invoice.edit,customer_receipt.void,write_off.post,write_off.void,customer_credit_application.void,customer_refund.void,customer_deposit.void,recurring_invoice.approve,connector.change,bank_rule.auto_apply,expense_claim.post" doc:"What executes on its approval." example:"organisation_setting.change"`
	Summary         string              `json:"summary" doc:"What it does, in a sentence." example:"Change Tolerance for base-currency differences from 1.00 to 5.00 from 2026-10-20"`
	Status          string              `json:"status" enum:"pending,approved,rejected,void,expired" doc:"pending waits for approval; approved executed it; void means its record, a dependency, its approval policy or the team mode changed (FR-APR-05), an earlier approver lost their right, or its requester withdrew it, as status_reason says; expired after 7 days." example:"pending"`
	StatusReason    string              `json:"status_reason,omitempty" doc:"Why it was rejected or voided." example:"the setting changed after the request"`
	RequestedBy     string              `json:"requested_by" format:"uuid" doc:"The user who asked for it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	RequestedByName string              `json:"requested_by_name" doc:"Their name." example:"Ah Kow Lim"`
	RequestedAt     time.Time           `json:"requested_at" doc:"When it was asked for." example:"2026-10-20T09:00:00Z"`
	ExpiresAt       time.Time           `json:"expires_at" doc:"When it expires unless decided: 7 days after it was asked for (FR-APR-05)." example:"2026-10-27T09:00:00Z"`
	DecidedAt       *time.Time          `json:"decided_at,omitempty" doc:"When it was approved, rejected or voided." example:"2026-10-20T09:30:00Z"`
	Policy          string              `json:"policy" doc:"The approval policy's version it keeps (G4-057)." example:"founder/organisation_setting@2026-10-20.1"`
	Steps           []ApprovalProgress  `json:"steps" doc:"The steps of that policy, in order, with the approvals each has (FR-APR-01, rev 0.40)."`
	CurrentStep     int                 `json:"current_step" doc:"The step the next signature signs, from 1; 0 once every step has its quorum." example:"1"`
	Envelope        string              `json:"envelope" doc:"The approval envelope exactly as signed: RFC 8785 canonical JSON (FR-APR-10)." example:"{\"amounts\":[]}"`
	EnvelopeHash    string              `json:"envelope_hash" doc:"The envelope's SHA-256, in hex: send it to approve or reject exactly what you were shown." example:"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
	Diff            []ApprovalDiff      `json:"diff" doc:"What it changes, before and after (FR-APR-02)."`
	Signatures      []ApprovalSignature `json:"signatures" doc:"Its signed approvals and rejections, in order (FR-APR-03)."`
	YouMayApprove   bool                `json:"you_may_approve" doc:"true when you may approve or reject it now: it is pending, you did not ask for it and you hold the approval right." example:"true"`
	Version         int                 `json:"version" doc:"The request's version." example:"1"`
	InvitationCode  string              `json:"invitation_code,omitempty" doc:"Only in the 202 answer to an invitation: its one-time code, shown once, which works once the invitation is approved." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
}

// ApprovalProgress is one step of a request's policy and the approvals it has.
type ApprovalProgress struct {
	ApprovalStep
	Approvals int `json:"approvals" doc:"The approvals signed in this step, not voided." example:"0"`
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
	EnvelopeHash string           `json:"envelope_hash" minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$" doc:"The envelope_hash of the request you were shown: a request whose envelope differs is not signed (FR-APR-03)." example:"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`
	Reason       string           `json:"reason,omitempty" maxLength:"500" doc:"Why you reject it: needed to reject." example:"Not this month"`
	Passkey      *ApprovalPasskey `json:"passkey,omitempty" doc:"Sign with a passkey: your authenticator's answer to this request's challenge (POST /v1/approvals/{approval_id}/challenge). Without it, your session's step-up in the last 5 minutes signs (rev 0.44)."`
}

// ApprovalPasskey is your authenticator's answer to a request's challenge: it signs the request's envelope hash.
type ApprovalPasskey struct {
	ChallengeID string       `json:"challenge_id" format:"uuid" doc:"The request's challenge it answers." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f41"`
	Credential  WebAuthnJSON `json:"credential" doc:"The PublicKeyCredential navigator.credentials.get returned, as its toJSON() writes it."`
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

type approvalChallengeInput struct {
	ApprovalPath
	IfMatchHeader
}

type approvalWithdrawInput struct {
	ApprovalPath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

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
	approval.CodeNotApprover, approval.CodePending, approval.CodeReason, approval.CodeAuthority, approval.CodeNotYourStep}

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

// signer returns the person who signs a decision: with a passkey answer, a person in a browser session, whose passkey
// signs the envelope's hash itself, and their name for their authenticator; without one, a person who passed step-up in
// the last 5 minutes (FR-APR-03, FR-APR-04).
func (a *API) signer(ctx context.Context, what string, pk *ApprovalPasskey) (authn.Principal, string, error) {
	if pk == nil {
		p, err := a.steppedUpPerson(ctx, what)
		return p, "", err
	}
	p, _, name, err := a.passkeyPerson(ctx, what)
	return p, name, err
}

// withPasskey makes a decision a passkey's: it takes the request's challenge, which must be over this request's envelope
// hash, checks the authenticator's answer, records it and keeps the assertion with the signature (FR-APR-03, G4-056).
// It runs in the deciding unit of work, so a decision that fails leaves the challenge unused.
func (a *API) withPasskey(ctx context.Context, u storage.UnitOfWork, p authn.Principal, name string, r storage.ApprovalRequest,
	pk *ApprovalPasskey, sig *storage.NewApprovalSignature) error {
	if pk == nil {
		return nil
	}
	id, err := parseID("passkey.challenge_id", pk.ChallengeID)
	if err != nil {
		return err
	}
	response, err := credentialBytes(pk.Credential)
	if err != nil {
		return err
	}
	now := a.deps.Clock.Now()
	c, err := u.Passkeys().Take(ctx, id, p.SessionHash, storage.PurposeApproval, now)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && (c.UserID != p.UserID || len(c.Challenge) != 48 || !bytes.Equal(c.Challenge[16:], r.EnvelopeHash))) {
		return problem(CodeChallenge, "That challenge is unknown, another session's, for another request or envelope, used already or expired.",
			"Ask for a new challenge for this request and answer it within 5 minutes.")
	}
	if err != nil {
		return err
	}
	user, err := passkeyUser(ctx, u, p, name)
	if err != nil {
		return err
	}
	got, err := a.passkeys.Verify(user, c.Challenge, response)
	if err != nil {
		return a.passkeyRefused(ctx, "decideApproval", err)
	}
	if err := u.Passkeys().AnswerApproval(ctx, storage.PasskeyAnswer{ChallengeID: id, SessionHash: p.SessionHash, PasskeyID: got.Passkey.ID,
		SignCount: got.SignCount, BackupState: got.BackupState, Now: now}); err != nil {
		return err
	}
	sig.MFAMethod, sig.MFAAt = "passkey", now
	sig.Passkey = &storage.PasskeyAssertion{ChallengeID: id, PasskeyID: got.Passkey.ID, AuthenticatorData: got.AuthenticatorData,
		ClientData: got.ClientData, Signature: got.Signature}
	return nil
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
	// Content is what the change needs beside its envelope, too large to sign in it, such as an edit's lines: the
	// envelope holds its SHA-256 in Execute instead (rev 0.54).
	Content []byte
	// Then are steps after the policy's own for this change only, such as finance's on an expense claim above its
	// category's limit (FR-AP-17): the request keeps them with the policy's.
	Then []approval.Step
}

// stored is what a request keeps of its change.
type stored struct {
	Execute map[string]any    `json:"execute"`
	Diff    []ApprovalDiff    `json:"diff"`
	Amounts []approval.Amount `json:"amounts"`
	Content json.RawMessage   `json:"content,omitempty"`
}

// intIn is the whole number a change holds at key, as storedOf reads it.
func intIn(x map[string]any, key string) (int64, error) {
	switch n := x[key].(type) {
	case json.Number:
		return n.Int64()
	case int:
		return int64(n), nil
	}
	return 0, fmt.Errorf("api: the change holds no whole number %s", key)
}

// storedOf reads what a request keeps of its change, its numbers as exact integers, so the envelope rebuilt from it is
// the one signed (FR-APR-10).
func storedOf(r storage.ApprovalRequest) (stored, error) {
	var s stored
	d := json.NewDecoder(bytes.NewReader(r.Change))
	d.UseNumber()
	err := d.Decode(&s)
	return s, err
}

// envelopeOf builds the envelope of a change from the data in u now: the same builder runs when the change is
// requested and, with its rows locked, when it executes (FR-APR-10).
func (a *API) envelopeOf(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, c pendingChange, requester uuid.UUID, policy string) ([]byte, error) {
	mode, modeVersion, _, err := teamModeOf(ctx, u)
	if err != nil {
		return nil, err
	}
	e := approval.Envelope{Organisation: org, ObjectType: c.ObjectType, ObjectID: c.ObjectKey, Operation: c.Operation, Change: c.Execute,
		Amounts: c.Amounts, Policy: policy, TeamMode: mode, RequestedBy: requester, Dependencies: map[string]any{}}
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
	case approval.ObjectPolicy:
		current, _, err := a.policyFor(ctx, u, c.ObjectKey)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = current.Number
		e.Dependencies["policy_version"] = current.Version
	case approval.ObjectPack:
		v, activated, err := loadedPack(ctx, u, c.ObjectKey)
		if err != nil {
			return nil, err
		}
		to := ""
		if v.EffectiveTo != nil {
			to = dateOf(*v.EffectiveTo)
		}
		if activated != nil {
			e.ObjectVersion = 1
		}
		e.Dependencies["digest"] = v.Digest
		e.Dependencies["effective_from"] = dateOf(v.EffectiveFrom)
		e.Dependencies["effective_to"] = to
		e.Dependencies["activated"] = activated != nil
	case approval.ObjectTeamMode:
		cs, err := a.conflictsOf(ctx, u)
		if err != nil {
			return nil, err
		}
		keys := []string{}
		for _, c := range cs {
			keys = append(keys, c.Key())
		}
		slices.Sort(keys)
		conflicts := make([]any, len(keys))
		for i, k := range keys {
			conflicts[i] = k
		}
		e.ObjectVersion = modeVersion
		e.Dependencies["mode_version"] = modeVersion
		e.Dependencies["conflicts"] = conflicts
	case approval.ObjectPeriod:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		p, err := u.Periods().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		closeID := ""
		if cl, err := u.Closes().Latest(ctx, id); err == nil {
			closeID = cl.ID.String()
		} else if !errors.Is(err, ports.ErrNotFound) {
			return nil, err
		}
		all, err := u.Periods().List(ctx)
		if err != nil {
			return nil, err
		}
		later := false
		for _, x := range all {
			later = later || (x.StartsOn.After(p.EndsOn) && x.Status == storage.PeriodClosed)
		}
		e.ObjectVersion = p.Version
		e.Dependencies["period_version"] = p.Version
		e.Dependencies["status"] = string(p.Status)
		e.Dependencies["close_id"] = closeID
		e.Dependencies["later_closed"] = later
	case approval.ObjectJournal:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		debits, err := a.journalDebits(ctx, u, org, doc)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["journal_version"] = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["debits"] = debits
	case approval.ObjectOpening:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		debits := ""
		if doc.Status == storage.DocumentPosted {
			if debits, err = postedDebits(ctx, u, ledger.OpeningIdentity(doc.ID, doc.Version, ledger.RuleOpeningBalance)); err != nil {
				return nil, err
			}
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["opening_version"] = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["debits"] = debits
	case approval.ObjectBill:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		v, err := a.billView(ctx, u)
		if err != nil {
			return nil, err
		}
		payable, err := a.billPayable(ctx, u, v, doc)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["bill_version"] = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["payable"] = payable
	case approval.ObjectInvoice:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		v, err := a.invoiceView(ctx, u)
		if err != nil {
			return nil, err
		}
		receivable, err := a.invoiceReceivable(ctx, u, v, doc)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["invoice_version"] = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["receivable"] = receivable
	case approval.ObjectOrder:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		p, _, err := a.orders.Check(ctx, u, doc)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["order_version"] = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["total"] = p.TotalBase.String()
	case approval.ObjectTemplate:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		t, err := u.DocumentTemplates().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		from, _, err := templateExecute(c.Execute)
		if err != nil {
			return nil, err
		}
		_, h, err := templateContent(ctx, u, t, from)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = t.Version
		e.Dependencies["template_version"] = t.Version
		e.Dependencies["content"] = hex.EncodeToString(h)
		e.Dependencies["published"] = t.Published
	case approval.ObjectPayment:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		v, err := a.paymentView(ctx, u)
		if err != nil {
			return nil, err
		}
		p, err := a.paymentOf(ctx, u, v, doc, false)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["paid"] = p.Total.Amount + " " + p.Total.Currency
	case approval.ObjectReceipt:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		v, err := a.receiptView(ctx, u)
		if err != nil {
			return nil, err
		}
		r, err := a.receiptOf(ctx, u, v, doc, false)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["received"] = r.Total.Amount + " " + r.Total.Currency
	case approval.ObjectWriteOff:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		v, err := a.writeOffView(ctx, u)
		if err != nil {
			return nil, err
		}
		w, err := a.writeOffOf(ctx, u, v, doc, false)
		if err != nil {
			return nil, err
		}
		carrying := ""
		if w.Carrying != nil {
			carrying = w.Carrying.Amount + " " + w.Carrying.Currency
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["write_off_version"] = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["carrying"] = carrying
	case approval.ObjectApplication:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		x, err := a.applicationOf(ctx, u, doc)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["applied"] = x.Amount.Amount + " " + x.Amount.Currency
	case approval.ObjectCustomerApplication:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		x, err := a.customerApplicationOf(ctx, u, doc)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["applied"] = x.Amount.Amount + " " + x.Amount.Currency
	case approval.ObjectDeposit:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		v, err := a.depositView(ctx, u)
		if err != nil {
			return nil, err
		}
		x, err := a.depositOf(ctx, u, v, doc, false)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["received"] = x.Amount.Amount + " " + x.Amount.Currency
	case approval.ObjectClaim: // the claim's version and status, and what the company owes for it (FR-AP-17)
		version, deps, err := claimDependencies(ctx, u, c.ObjectKey)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = version
		for k, v := range deps {
			e.Dependencies[k] = v
		}
	case approval.ObjectBankRule: // the rule's version, and its conditions and action (FR-BNK-14)
		version, deps, err := ruleDependencies(ctx, u, c.ObjectKey)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = version
		for k, v := range deps {
			e.Dependencies[k] = v
		}
	case approval.ObjectConnector: // the installed connector's version and state, or whether it is installed (FR-INT-26)
		version, deps, err := connectorDependencies(ctx, u, c.ObjectKey)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = version
		for k, v := range deps {
			e.Dependencies[k] = v
		}
	case approval.ObjectRecurringInvoice: // its content version and mode (FR-AR-07)
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		r, err := u.RecurringInvoices().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = r.ContentVersion
		e.Dependencies["content_version"] = strconv.Itoa(r.ContentVersion)
		e.Dependencies["mode"] = r.Mode
	case approval.ObjectCustomerRefund:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		x, err := a.customerRefundOf(ctx, u, doc)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["refunded"] = x.Amount.Amount + " " + x.Amount.Currency
	case approval.ObjectRefund:
		id, err := uuid.Parse(c.ObjectKey)
		if err != nil {
			return nil, err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		x, err := a.refundOf(ctx, u, doc)
		if err != nil {
			return nil, err
		}
		e.ObjectVersion = doc.Version
		e.Dependencies["status"] = string(doc.Status)
		e.Dependencies["refunded"] = x.Amount.Amount + " " + x.Amount.Currency
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
	policy, _, err := a.policyFor(ctx, u, c.ObjectType)
	if err != nil {
		return Approval{}, err
	}
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
	change, err := json.Marshal(stored{Execute: c.Execute, Diff: c.Diff, Amounts: c.Amounts, Content: c.Content})
	if err != nil {
		return Approval{}, err
	}
	steps := policy.Steps
	if len(c.Then) > 0 {
		steps = append(slices.Clone(policy.Steps), c.Then...)
	}
	r, err := u.Approvals().Request(ctx, storage.NewApprovalRequest{ObjectType: c.ObjectType, ObjectKey: c.ObjectKey, Operation: c.Operation,
		Summary: c.Summary, Change: change, Envelope: string(envelope), EnvelopeHash: approval.Hash(envelope), PolicyVersion: policy.Version,
		PolicySteps: storageSteps(steps), RequestedBy: p.UserID, RequestedAt: now, RequesterMFAMethod: stepUpMethod, RequesterMFAAt: p.StepUpAt, ExpiresAt: now.Add(approval.TTL)})
	if err != nil {
		return Approval{}, err
	}
	// Those who may approve it hear that it waits for them (FR-NTF-01), the requester aside (SOD).
	if err := a.notify(ctx, u, notify.Event{Type: "approval.requested", RecordType: "approval_request", RecordID: &r.ID,
		Title: c.Summary + ": waits for your approval", Link: "/approvals/" + r.ID.String(), At: now,
		Audience: notify.Audience{Capability: string(approvalRow(c.ObjectType)), Need: "A", Except: []uuid.UUID{p.UserID}}}); err != nil {
		return Approval{}, err
	}
	return a.approvalOf(ctx, u, r)
}

// notifyDecided tells a request's requester that it was approved or rejected (FR-NTF-01).
func (a *API) notifyDecided(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, how string) error {
	return a.notify(ctx, u, notify.Event{Type: "approval.decided", RecordType: "approval_request", RecordID: &r.ID,
		Title: how + ": " + r.Summary, Link: "/approvals/" + r.ID.String(), Audience: notify.Audience{Users: []uuid.UUID{r.RequestedBy}}})
}

func requestOf(r storage.ApprovalRequest) approval.Request {
	return approval.Request{Status: approval.Status(r.Status), RequestedBy: r.RequestedBy, ExpiresAt: r.ExpiresAt}
}

// approvalRow is the Appendix D row of the object a request changes: A on it signs, any right on it sees.
func approvalRow(objectType string) authz.Capability {
	return authz.Capability(approval.Rows[objectType])
}

// rolesOf are a principal's roles as the policies name them.
func rolesOf(rs []authz.Role) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

// signaturesOf are a request's signatures that are not voided, as the step rules read them.
func signaturesOf(sigs []storage.ApprovalSignature) []approval.Signature {
	var out []approval.Signature
	for _, s := range sigs {
		if s.VoidedAt == nil {
			out = append(out, approval.Signature{Step: s.Step, Signer: s.Signer, Approve: s.Decision == "approve"})
		}
	}
	return out
}

// mayDecide reports whether p may sign a request's current step now: a person other than its requester who has not
// signed it, holds A on its row and is one the step names.
func mayDecide(p authn.Principal, r storage.ApprovalRequest, sigs []storage.ApprovalSignature) bool {
	if p.UserID == uuid.Nil || p.UserID == r.RequestedBy || !mayApprove(p, r.ObjectType) {
		return false
	}
	if slices.ContainsFunc(sigs, func(s storage.ApprovalSignature) bool { return s.Signer == p.UserID }) {
		return false
	}
	step := approval.CurrentStep(domainSteps(r.PolicySteps), signaturesOf(sigs))
	return step > 0 && approval.Eligible(domainSteps(r.PolicySteps)[step-1], p.UserID, rolesOf(p.Roles))
}

// mayApprove reports whether p holds the approve right (A) on the request's row; the AI never does.
func mayApprove(p authn.Principal, objectType string) bool {
	return authz.Decide(authz.Policy{Operation: "approve", Capability: approvalRow(objectType), Need: authz.Approve}, p.Roles, p.AI()).Allowed
}

// maySee reports whether p may see a request: they asked for it, or hold a right on its object's row. On "Expense
// claims" a right on one's own claims only (the Employee's "C (own)") sees only the requests one asked for: one's own
// claims (design D10).
func maySee(p authn.Principal, r storage.ApprovalRequest) bool {
	if p.UserID != uuid.Nil && p.UserID == r.RequestedBy {
		return true
	}
	view := authz.Policy{Operation: "view", Capability: approvalRow(r.ObjectType), Need: authz.View}
	if r.ObjectType == approval.ObjectClaim {
		if mayApprove(p, r.ObjectType) {
			return true
		}
		view.Unqualified = true
	}
	return authz.Decide(view, p.Roles, p.AI()).Allowed
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
		YouMayApprove: status == approval.Pending && mayDecide(p, r, sigs), Steps: []ApprovalProgress{},
		CurrentStep: approval.CurrentStep(domainSteps(r.PolicySteps), signaturesOf(sigs))}
	if out.Diff == nil {
		out.Diff = []ApprovalDiff{}
	}
	for i, st := range r.PolicySteps {
		n := 0
		for _, g := range signaturesOf(sigs) {
			if g.Step == i+1 && g.Approve {
				n++
			}
		}
		out.Steps = append(out.Steps, ApprovalProgress{ApprovalStep: apiStep(st), Approvals: n})
	}
	for _, sg := range sigs {
		v := ApprovalSignature{Signer: sg.Signer.String(), SignerName: names[sg.Signer], Decision: sg.Decision,
			Method: sg.MFAMethod, SignedAt: sg.SignedAt.UTC(), Reason: sg.Reason, Voided: sg.VoidedAt != nil}
		if sg.Passkey != nil {
			v.PasskeyID = sg.Passkey.PasskeyID.String()
		}
		out.Signatures = append(out.Signatures, v)
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

// sign checks that the caller may sign the request now and returns it locked, with what it keeps and the step they
// sign: they passed step-up, hold the approve right, did not ask for it or sign it already, are one its current step
// names, and sent the hash of its envelope.
func (a *API) sign(ctx context.Context, u storage.UnitOfWork, p authn.Principal, id uuid.UUID, ifMatch, hash string) (storage.ApprovalRequest, int, error) {
	r, err := u.Approvals().Lock(ctx, id)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && !maySee(p, r)) {
		return r, 0, problem(CodeNotFound, "No approval request with that ID exists in this organisation.", "List the approvals to find their IDs")
	}
	if err != nil {
		return r, 0, err
	}
	if !mayApprove(p, r.ObjectType) {
		return r, 0, problem(approval.CodeNotApprover, "You hold no approval right on "+string(approvalRow(r.ObjectType))+" (Appendix D).",
			"Ask a member who holds it, such as an Owner, to decide it")
	}
	now := a.deps.Clock.Now()
	// Where the request stands says more than a stale ETag: decided, void or expired come first.
	if x := approval.CheckSigner(requestOf(r), p.UserID, now); x != nil {
		pr := problem(x.Code, strings.ToUpper(x.Message[:1])+x.Message[1:]+".", x.Fix)
		if x.Code == approval.CodeExpired {
			return r, 0, &blocked{id: r.ID, version: r.Version, status: approval.Expired, problem: pr}
		}
		return r, 0, pr
	}
	if err := checkIfMatch(ifMatch, etag(r.Version)); err != nil {
		return r, 0, err
	}
	if hash != hex.EncodeToString(r.EnvelopeHash) {
		return r, 0, problem(approval.CodeChanged, "The envelope you were shown is not the request's.",
			"Read the request again and decide what it holds now")
	}
	sigs, err := u.Approvals().Signatures(ctx, r.ID)
	if err != nil {
		return r, 0, err
	}
	if slices.ContainsFunc(sigs, func(s storage.ApprovalSignature) bool { return s.Signer == p.UserID }) {
		return r, 0, problem(approval.CodeNotYourStep, "You signed this request already, and a person signs a request once.",
			"Ask another member the policy names to sign its next step")
	}
	steps := domainSteps(r.PolicySteps)
	step := approval.CurrentStep(steps, signaturesOf(sigs))
	if step == 0 || !approval.Eligible(steps[step-1], p.UserID, rolesOf(p.Roles)) {
		return r, 0, problem(approval.CodeNotYourStep, fmt.Sprintf("Step %d of the request's policy needs %s.", step, describe(steps[max(step, 1)-1])),
			"Ask a member that step names to sign it; the steps are signed in order")
	}
	return r, step, nil
}

// describe says whom a step needs, in a sentence's words.
func describe(s approval.Step) string {
	switch {
	case len(s.Roles) > 0:
		return fmt.Sprintf("%d approval(s) from someone holding one of these roles: %s", s.Quorum, strings.Join(s.Roles, ", "))
	case len(s.Users) > 0:
		return fmt.Sprintf("%d approval(s) from the members it names", s.Quorum)
	}
	return fmt.Sprintf("%d approval(s) from anyone who holds the approval right", s.Quorum)
}

// recheck is FR-APR-10's last check before an approved change executes: every earlier approver is still an active
// member whose live roles hold A on the object's row and whom their step names. The current signer was checked by
// sign.
func (a *API) recheck(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, sigs []storage.ApprovalSignature, current uuid.UUID) error {
	members, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return err
	}
	steps := domainSteps(r.PolicySteps)
	for _, g := range signaturesOf(sigs) {
		if !g.Approve || g.Signer == current {
			continue
		}
		i := slices.IndexFunc(members, func(m storage.Member) bool { return m.UserID == g.Signer })
		if i >= 0 {
			roles := memberRoles(members[i])
			if approves(roles, r.ObjectType) && approval.Eligible(steps[g.Step-1], g.Signer, roles) {
				continue
			}
		}
		return &blocked{id: r.ID, version: r.Version, status: approval.Void, reason: "an earlier approver no longer holds the right their step needs",
			problem: problem(approval.CodeAuthority, "An earlier approver of this request no longer holds the right their step needs, so nothing was changed and the request is void.",
				"Ask for the change again if it is still needed")}
	}
	return nil
}

// memberRoles are a member's live roles, as the policies name them.
func memberRoles(m storage.Member) []string {
	out := make([]string, len(m.Roles))
	for i, r := range m.Roles {
		out[i] = r.Role
	}
	return out
}

// approves reports whether roles hold A on the row of objectType.
func approves(roles []string, objectType string) bool {
	rs := make([]authz.Role, len(roles))
	for i, r := range roles {
		rs[i] = authz.Role(r)
	}
	return authz.Decide(authz.Policy{Operation: "approve", Capability: approvalRow(objectType), Need: authz.Approve}, rs, false).Allowed
}

// policyChange is what an approval_policy.change request executes.
type policyChange struct {
	Version int                    `json:"version"`
	Steps   []storage.ApprovalStep `json:"steps"`
}

func policyChangeOf(x map[string]any) (policyChange, error) {
	var c policyChange
	b, err := json.Marshal(x)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	return c, err
}

// executable is the last check before an approved change executes, with the request still pending: a policy change
// is added only while the active members can sign its steps, since they may have changed since the request; a pack
// version is activated only while its pinned tests pass and its approver is not its proposer (ENG-32, SOD-10).
func (a *API) executable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored, signer uuid.UUID) error {
	switch r.Operation {
	case approval.OpPackActivate:
		return a.packExecutable(ctx, u, r, signer)
	case approval.OpPeriodReopen:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		if _, _, err := closing.Reopenable(ctx, u, id); err != nil {
			var ce *closing.Error
			if errors.As(err, &ce) {
				return &blocked{id: r.ID, version: r.Version, status: approval.Void, reason: "the period no longer reopens",
					problem: problem(ce.Code, "The period no longer reopens: "+ce.Message+", so nothing was changed and the request is void.", sentence(ce.Fix))}
			}
			return err
		}
		return nil
	case approval.OpClaimPost: // FR-AP-17: nobody approves their own claim
		return claimExecutable(ctx, u, r, signer)
	case approval.OpBillPost: // SOD-3: whoever prepared the bill never approves its post
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		me := (storage.Actor{Type: "user", ID: signer.String()}).String()
		if doc.SavedBy == me {
			return problem(CodeOwnBill, "You prepared this bill, so another person approves it (SOD-3).", "Ask another person who holds the approval right")
		}
		// A draft the scheduler made from a recurring bill was prepared by whoever wrote that version of it, read from the
		// recurring bill's own immutable versions, never from the draft (SOD-3; FR-AP-09, PRD-001 rev 0.84).
		if ref := bill.RecurringOf(doc); ref != nil {
			v, err := u.RecurringBills().Version(ctx, ref.ID, ref.Version)
			if err != nil {
				return err
			}
			if v.EditedBy == me {
				return problem(CodeOwnBill, "You wrote the recurring bill this draft was made from, so another person approves it (SOD-3).",
					"Ask another person who holds the approval right")
			}
		}
		return nil
	case approval.OpRecurringApprove: // SOD-3: whoever wrote the content version never approves it for issue and send
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		version, err := intIn(s.Execute, "content_version")
		if err != nil {
			return err
		}
		v, err := u.RecurringInvoices().Version(ctx, id, int(version))
		if err != nil {
			return err
		}
		if v.EditedBy == (storage.Actor{Type: "user", ID: signer.String()}).String() {
			return problem(CodeOwnInvoice, "You wrote this version of the recurring invoice, so another person approves it (SOD-3).",
				"Ask another person who holds the approval right")
		}
		return nil
	case approval.OpInvoicePost: // SOD-3: whoever prepared the invoice never approves its issue
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		me := (storage.Actor{Type: "user", ID: signer.String()}).String()
		if doc.SavedBy == me {
			return problem(CodeOwnInvoice, "You prepared this invoice, so another person approves it (SOD-3).", "Ask another person who holds the approval right")
		}
		// A draft the scheduler made from a recurring invoice was prepared by whoever wrote that version of it, read from
		// the recurring invoice's own immutable versions, never from the draft (SOD-3; FR-AR-07, PRD-001 rev 0.135).
		if ref := invoice.RecurringOf(doc); ref != nil {
			v, err := u.RecurringInvoices().Version(ctx, ref.ID, ref.Version)
			if err != nil {
				return err
			}
			if v.EditedBy == me {
				return problem(CodeOwnInvoice, "You wrote the recurring invoice this draft was made from, so another person approves it (SOD-3).",
					"Ask another person who holds the approval right")
			}
		}
		return nil
	case approval.OpWriteOffPost: // SOD-9: neither whoever prepared the write-off nor one who recorded a receipt of the invoice approves it
		return a.writeOffApprover(ctx, u, r, signer)
	case approval.OpTemplatePublish: // SOD-3: whoever saved the draft never approves its publication
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		from, _, err := templateExecute(s.Execute)
		if err != nil || from > 0 {
			return err // a rollback publishes a version someone published already; its requester never signs (FM-1)
		}
		t, err := u.DocumentTemplates().Get(ctx, id)
		if err != nil {
			return err
		}
		if t.SavedBy == (storage.Actor{Type: "user", ID: signer.String()}).String() {
			return problem(doctemplate.CodeOwnDraft, "You saved this template's draft, so another person approves its publication (SOD-3).",
				"Ask another person who holds the approval right")
		}
		return nil
	case approval.OpOrderApprove: // SOD-3: whoever prepared the order never approves it
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		if doc.SavedBy == (storage.Actor{Type: "user", ID: signer.String()}).String() {
			return problem(CodeOwnOrder, "You prepared this purchase order, so another person approves it (SOD-3).", "Ask another person who holds the approval right")
		}
		return nil
	case approval.OpJournalPost: // SOD-3: whoever prepared the journal never approves it
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		me := (storage.Actor{Type: "user", ID: signer.String()}).String()
		if doc.SavedBy == me {
			return problem(CodeOwnJournal, "You prepared this journal, so another person approves it (SOD-3).", "Ask another person who holds the approval right")
		}
		// A draft the scheduler made from a recurring journal was prepared by whoever wrote that version of it, read
		// from the recurring journal's own immutable versions, never from the draft (SOD-3; PRD-001 rev 0.60).
		saved, err := journal.ReadSaved(doc)
		if err != nil {
			return err
		}
		if ref := saved.RecurringJournal; ref != nil {
			v, err := u.RecurringJournals().Version(ctx, ref.ID, ref.Version)
			if err != nil {
				return err
			}
			if v.EditedBy == me {
				return problem(CodeOwnJournal, "You wrote the recurring journal this draft was made from, so another person approves it (SOD-3).",
					"Ask another person who holds the approval right")
			}
		}
		return nil
	case approval.OpPolicyChange:
	default:
		return nil
	}
	c, err := policyChangeOf(s.Execute)
	if err != nil {
		return err
	}
	fps, err := a.checkSteps(ctx, u, r.ObjectKey, domainSteps(c.Steps))
	if err != nil || len(fps) == 0 {
		return err
	}
	return &blocked{id: r.ID, version: r.Version, status: approval.Void, reason: "the policy's steps can no longer be signed",
		problem: problem(approval.CodeSteps, "The policy's steps can no longer be signed: "+fps[0].Message+", so nothing was changed and the request is void.",
			"Ask for a policy whose steps the members can sign")}
}

// execute carries out an approved request's change in the approving unit of work, audited with the approval
// (AUD-01); signer is the final approver.
func (a *API) execute(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored, signer uuid.UUID) error {
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
		if _, err = a.assignRoleNow(ctx, u, id, text("role"), expires); err != nil {
			return err
		}
		return a.recordOverride(ctx, u, r, x)
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
		now := a.deps.Clock.Now()
		_, err = u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: text("display_name"), Roles: roles,
			RolesExpireAt: expires, ExpiresAt: now.Add(InvitationTTL), CreatedAt: now})
		return err
	case approval.OpPolicyChange:
		c, err := policyChangeOf(x)
		if err != nil {
			return err
		}
		_, err = u.ApprovalPolicies().Add(ctx, storage.NewApprovalPolicy{ObjectType: r.ObjectKey, Version: c.Version, Steps: c.Steps,
			RequestID: r.ID, CreatedBy: r.RequestedBy, CreatedAt: a.deps.Clock.Now()})
		return err
	case approval.OpPackActivate:
		_, err := u.Packs().Activate(ctx, storage.NewPackActivation{VersionID: r.ObjectKey, ProposedBy: r.RequestedBy, ApprovedBy: signer,
			Approval: r.ID, ActivatedAt: a.deps.Clock.Now()})
		return err
	case approval.OpTeamModeChange:
		return a.switchTeamMode(ctx, u, r, x)
	case approval.OpPeriodReopen:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		if _, _, err = closing.Reopen(storage.WithReason(ctx, text("reason")), u, id, r.ID, text("reason")); err != nil || a.deps.Outbox == nil {
			return err
		}
		pd, err := u.Periods().Get(ctx, id)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		return notify.PeriodReopened(ctx, *a.deps.Outbox, u, o.ID, signer, id, monthName(pd), text("reason")) // FR-NTF-01
	case approval.OpJournalPost, approval.OpJournalReverse:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		if r.Operation == approval.OpJournalPost {
			_, err = a.journals.Post(ctx, o.ID, id)
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.journals.Reverse(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpBillPost, approval.OpBillVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		if r.Operation == approval.OpBillPost {
			_, err = a.bills.Post(ctx, o.ID, id)
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.bills.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpInvoicePost, approval.OpInvoiceVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		if r.Operation == approval.OpInvoicePost {
			_, err = a.invoices.Post(ctx, o.ID, id)
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.invoices.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpInvoiceEdit:
		return a.executeInvoiceEdit(ctx, u, r, s)
	case approval.OpTemplatePublish:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		from, note, err := templateExecute(x)
		if err != nil {
			return err
		}
		t, err := u.DocumentTemplates().Get(ctx, id)
		if err != nil {
			return err
		}
		_, _, err = a.publishTemplate(ctx, u, id, t.Version, from, note)
		return err
	case approval.OpOrderApprove:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		_, err = a.orders.Approve(ctx, o.ID, id)
		return err
	case approval.OpRefundVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.refunds.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpApplicationVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.applications.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpDepositVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.deposits.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpConnectorChange: // FR-INT-26: the organisation connector's step, as asked
		return a.executeConnector(ctx, u, s)
	case approval.OpRuleAutoApply: // FR-BNK-14: the rule applies by itself from now on
		return a.executeRuleAutoApply(ctx, u, r, s)
	case approval.OpClaimPost: // FR-AP-07: the approved claim posts, as its last approver
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		_, err = a.claims.Post(ctx, o.ID, id)
		return err
	case approval.OpRecurringApprove: // FR-AR-07: the version approved for issue and send
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		version, err := intIn(x, "content_version")
		if err != nil {
			return err
		}
		_, err = u.RecurringInvoices().Approve(ctx, id, int(version))
		return err
	case approval.OpCustomerRefundVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.customerRefunds.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpCustomerApplicationVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.customerApplications.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpPaymentVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.payments.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpWriteOffPost, approval.OpWriteOffVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		if r.Operation == approval.OpWriteOffPost {
			_, err = a.writeOffs.Post(ctx, o.ID, id)
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.writeOffs.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpReceiptVoid:
		id, err := uuid.Parse(r.ObjectKey)
		if err != nil {
			return err
		}
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		date, err := time.Parse(time.DateOnly, text("date"))
		if err != nil {
			return err
		}
		_, err = a.receipts.Void(ctx, o.ID, id, date, text("reason"))
		return err
	case approval.OpJournalEdit:
		return a.executeEdit(ctx, u, r, s)
	case approval.OpBillEdit:
		return a.executeBillEdit(ctx, u, r, s)
	case approval.OpOpeningEdit:
		return a.executeOpeningEdit(ctx, u, r, s)
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
		Description: "Signs the request's current step and, once every step of its policy has its quorum, carries out its change in the same " +
			"transaction (FR-APR-01, FR-APR-03, FR-APR-10, FM-1). Needs a person signed in who passed step-up in the last 5 minutes, holds the " +
			"approval right (A) on the object's Appendix D row, is one the current step names (APR-11), did not ask for it and has not signed " +
			"it; and envelope_hash must be the request's, and If-Match its ETag. With passkey, your passkey's answer to the request's " +
			"challenge signs the envelope's hash itself instead of your session's step-up, and the signature keeps the assertion (FR-APR-03, rev 0.44). The envelope is rebuilt from current data with its rows locked: " +
			"if the record, any dependency or the approval policy changed since the request, nothing executes, the request becomes void and " +
			"APR-04 says so (FR-APR-05); if an earlier approver no longer holds the right their step needs, APR-10 does. Side effects: the signed " +
			"approval and its audit event; on the last step, the request approved, the change itself and their audit events, which name the " +
			"approval. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeStale, CodeIfMatchRequired, storage.CodeApproval, storage.CodeVersion, storage.CodeMembership},
		append(append(append(append(append(append(append(locks, ApprovalCodes...), MemberCodes...), SettingCodes...), PolicyCodes...), PackCodes...), PasskeyCodes...),
			append(append(append(JournalApprovalCodes(), BillApprovalCodes()...), PaymentApprovalCodes()...), append(append(append(ApplicationApprovalCodes(), RefundApprovalCodes()...), OrderApprovalCodes()...), append(append(append(TemplateApprovalCodes(), InvoiceApprovalCodes()...), ReceiptApprovalCodes()...), append(append(append(WriteOffApprovalCodes(), CustomerApplicationApprovalCodes()...), CustomerRefundApprovalCodes()...), append(DepositApprovalCodes(), ClaimApprovalCodes()...)...)...)...)...)...)...),
		func(ctx context.Context, in *approvalDecideInput) (*approvalWriteOutput, error) {
			id, err := parseID("approval_id", in.ApprovalID)
			if err != nil {
				return nil, err
			}
			p, name, err := a.signer(ctx, "Approving", in.Body.Passkey)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (decided, error) {
				r, step, err := a.sign(ctx, u, p, id, in.IfMatch, in.Body.EnvelopeHash)
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
				// The policy in force now: a request under an older version differs (FR-APR-10), which the database's voiding
				// on a new version makes rare.
				current, _, err := a.policyFor(ctx, u, r.ObjectType)
				if err != nil {
					return decided{}, err
				}
				envelope, err := a.envelopeOf(ctx, u, p.Organisation, pendingChange{ObjectType: r.ObjectType, ObjectKey: r.ObjectKey,
					Operation: r.Operation, Execute: s.Execute, Amounts: s.Amounts}, r.RequestedBy, current.Version)
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
				signature := storage.NewApprovalSignature{RequestID: r.ID, Step: step, Signer: p.UserID, Decision: "approve",
					EnvelopeHash: r.EnvelopeHash, MFAMethod: stepUpMethod, MFAAt: p.StepUpAt, SignedAt: now}
				if err := a.withPasskey(ctx, u, p, name, r, in.Body.Passkey, &signature); err != nil {
					return decided{}, err
				}
				if _, err := u.Approvals().Sign(ctx, signature); err != nil {
					return decided{}, err
				}
				sigs, err := u.Approvals().Signatures(ctx, r.ID)
				if err != nil {
					return decided{}, err
				}
				if approval.CurrentStep(domainSteps(r.PolicySteps), signaturesOf(sigs)) == 0 {
					if err := a.recheck(ctx, u, r, sigs, p.UserID); err != nil {
						return decided{}, err
					}
					if err := a.executable(ctx, u, r, s, p.UserID); err != nil {
						return decided{}, err
					}
					if r, err = u.Approvals().Decide(ctx, r.ID, r.Version, string(approval.Approved), "", now); err != nil {
						return decided{}, err
					}
					if err := a.execute(ctx, u, r, s, p.UserID); err != nil {
						return decided{}, err
					}
					if err := a.notifyDecided(ctx, u, r, "Approved"); err != nil {
						return decided{}, err
					}
					// FM-3: the change is logged for another founder's weekly review.
					requester, approvalID := r.RequestedBy, r.ID
					if _, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewApprovedChange, ObjectType: r.ObjectType,
						ObjectKey: r.ObjectKey, Summary: r.Summary, Actor: &requester, ApprovalID: &approvalID, LoggedAt: now}); err != nil {
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
		Description: "Signs a rejection of the request's envelope at its current step, with your reason; the change never executes " +
			"(FR-APR-03, FM-1). Needs a person signed in who passed step-up in the last 5 minutes, holds the approval right (A) on the object's " +
			"Appendix D row, is one the current step names, did not ask for it and has not signed it; and envelope_hash must be the request's, " +
			"and If-Match its ETag. With passkey, your passkey signs the envelope's hash itself (FR-APR-03, rev 0.44). Side effects: the signed rejection, the request rejected and their audit events. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeStale, CodeIfMatchRequired, storage.CodeApproval, storage.CodeVersion}, append(append(locks, ApprovalCodes...), PasskeyCodes...)...),
		func(ctx context.Context, in *approvalDecideInput) (*approvalWriteOutput, error) {
			id, err := parseID("approval_id", in.ApprovalID)
			if err != nil {
				return nil, err
			}
			p, name, err := a.signer(ctx, "Rejecting", in.Body.Passkey)
			if err != nil {
				return nil, err
			}
			reason := strings.TrimSpace(in.Body.Reason)
			if reason == "" {
				return nil, invalid([]FieldProblem{{Code: approval.CodeReason, Field: "body.reason", Message: "a rejection needs its reason",
					Fix: "Say why you reject it, so the requester knows what to change"}})
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (decided, error) {
				r, step, err := a.sign(ctx, u, p, id, in.IfMatch, in.Body.EnvelopeHash)
				if err != nil {
					return decided{}, err
				}
				now := a.deps.Clock.Now()
				signature := storage.NewApprovalSignature{RequestID: r.ID, Step: step, Signer: p.UserID, Decision: "reject",
					EnvelopeHash: r.EnvelopeHash, MFAMethod: stepUpMethod, MFAAt: p.StepUpAt, SignedAt: now, Reason: reason}
				if err := a.withPasskey(ctx, u, p, name, r, in.Body.Passkey, &signature); err != nil {
					return decided{}, err
				}
				if _, err := u.Approvals().Sign(ctx, signature); err != nil {
					return decided{}, err
				}
				if r, err = u.Approvals().Decide(ctx, r.ID, r.Version, string(approval.Rejected), reason, now); err != nil {
					return decided{}, err
				}
				if err := a.notifyDecided(ctx, u, r, "Rejected"); err != nil {
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

	register(a, authz.ChallengeApproval, ceremony(huma.Operation{
		OperationID: "challengeApproval", Method: http.MethodPost, Path: "/v1/approvals/{approval_id}/challenge", Tags: []string{"Approvals"},
		Summary: "Start signing a request with a passkey",
		Description: "A challenge for signing the request with your passkey: 16 random bytes followed by the SHA-256 of the request's " +
			"envelope, so your authenticator signs exactly what you approve or reject (FR-APR-03). Pass the options to " +
			"navigator.credentials.get, then send the answer with approve or reject, within 5 minutes; it works once, for this request, " +
			"organisation and envelope only (G4-056). Needs a person signed in to Fyniq in the browser who may decide the request now, and " +
			"If-Match with the request's ETag, so the challenge is over the version you were shown. " +
			"Side effects: the challenge, kept for the answer; nothing else.",
	}), append([]string{CodeNotFound, CodeNoPasskey, CodeStale, CodeIfMatchRequired, storage.CodeApproval}, append(append([]string{}, PasskeyCodes...), ApprovalCodes...)...),
		func(ctx context.Context, in *approvalChallengeInput) (*passkeyChallengeOutput, error) {
			id, err := parseID("approval_id", in.ApprovalID)
			if err != nil {
				return nil, err
			}
			p, _, name, err := a.passkeyPerson(ctx, "Signing a request with a passkey")
			if err != nil {
				return nil, err
			}
			out := passkeyChallengeOutput{CacheControl: "no-store"}
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				r, err := u.Approvals().Get(ctx, id)
				if errors.Is(err, ports.ErrNotFound) || (err == nil && !maySee(p, r)) {
					return problem(CodeNotFound, "No approval request with that ID exists in this organisation.", "List the approvals to find their IDs")
				}
				if err != nil {
					return err
				}
				if !mayApprove(p, r.ObjectType) {
					return problem(approval.CodeNotApprover, "You hold no approval right on "+string(approvalRow(r.ObjectType))+" (Appendix D).",
						"Ask a member who holds it, such as an Owner, to decide it")
				}
				if x := approval.CheckSigner(requestOf(r), p.UserID, a.deps.Clock.Now()); x != nil {
					return problem(x.Code, strings.ToUpper(x.Message[:1])+x.Message[1:]+".", x.Fix)
				}
				if err := checkIfMatch(in.IfMatch, etag(r.Version)); err != nil { // the version you were shown is the one you sign
					return err
				}
				user, err := passkeyUser(ctx, u, p, name)
				if err != nil {
					return err
				}
				if len(user.Passkeys) == 0 {
					return problem(CodeNoPasskey, "You have no passkey to sign with.",
						"Register a passkey first, or approve with your session's step-up")
				}
				nonce := make([]byte, 16)
				_, _ = rand.Read(nonce)
				challenge := append(nonce, r.EnvelopeHash...)
				options, err := a.passkeys.Assertion(user, challenge)
				if err != nil {
					return err
				}
				now := a.deps.Clock.Now()
				cid, err := u.Passkeys().ApprovalChallenge(ctx, storage.NewApprovalChallenge{SessionHash: p.SessionHash, RequestID: r.ID,
					EnvelopeHash: r.EnvelopeHash, Nonce: nonce, Now: now, Expires: now.Add(ChallengeTTL)})
				if err != nil {
					return err
				}
				b, err := json.Marshal(options)
				if err != nil {
					return err
				}
				var doc WebAuthnJSON
				if err := json.Unmarshal(b, &doc); err != nil {
					return err
				}
				out.Body = PasskeyChallenge{ChallengeID: cid.String(), ExpiresAt: now.Add(ChallengeTTL).UTC(), Options: doc}
				return nil
			})
			if err != nil {
				return nil, err
			}
			return &out, nil
		})

	register(a, authz.WithdrawApproval, huma.Operation{
		OperationID: "withdrawApproval", Method: http.MethodPost, Path: "/v1/approvals/{approval_id}/withdraw", Tags: []string{"Approvals"},
		Summary: "Withdraw your request",
		Description: "Withdraws a pending request you asked for: it becomes void with the reason \"withdrawn by its requester\", and any " +
			"signatures it has are voided; the change never executes (rev 0.40). Needs the person who asked for it, signed in to Fyniq, and " +
			"If-Match with its ETag. Side effects: the request void and its audit event. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeSessionRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeStale,
		CodeIfMatchRequired, storage.CodeApproval, storage.CodeVersion, approval.CodeNotYours}, append(locks, ApprovalCodes...)...),
		func(ctx context.Context, in *approvalWithdrawInput) (*approvalWriteOutput, error) {
			id, err := parseID("approval_id", in.ApprovalID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			if p.Via != authn.ViaSession || p.AI() || p.UserID == uuid.Nil {
				return nil, problem(CodeSessionRequired, "Withdrawing a request needs the person who asked for it, signed in to Fyniq.",
					"Sign in to Fyniq in the browser and withdraw it there.")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (decided, error) {
				r, err := u.Approvals().Lock(ctx, id)
				if errors.Is(err, ports.ErrNotFound) || (err == nil && !maySee(p, r)) {
					return decided{}, problem(CodeNotFound, "No approval request with that ID exists in this organisation.", "List the approvals to find their IDs")
				}
				if err != nil {
					return decided{}, err
				}
				if r.RequestedBy != p.UserID {
					return decided{}, problem(approval.CodeNotYours, "Only the person who asked for this change withdraws it.",
						"Reject it with a reason instead, if you hold the approval right")
				}
				now := a.deps.Clock.Now()
				switch approval.StatusAt(requestOf(r), now) {
				case approval.Pending:
				case approval.Expired:
					return decided{}, &blocked{id: r.ID, version: r.Version, status: approval.Expired,
						problem: problem(approval.CodeExpired, "The request expired already.", "Nothing to withdraw")}
				case approval.Void:
					return decided{}, problem(approval.CodeChanged, "The request is void already.", "Nothing to withdraw")
				default:
					return decided{}, problem(approval.CodeNotPending, fmt.Sprintf("The request is %s already.", r.Status), "Nothing to withdraw")
				}
				if err := checkIfMatch(in.IfMatch, etag(r.Version)); err != nil {
					return decided{}, err
				}
				if r, err = u.Approvals().Decide(ctx, r.ID, r.Version, string(approval.Void), "withdrawn by its requester", now); err != nil {
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

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
