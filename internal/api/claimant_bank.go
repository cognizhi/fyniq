package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ a person's own bank account (FR-AP-22, FM-1, SEC-11)
//
// Each person keeps their own bank account for reimbursements: a Singapore bank's account in SGD, entered by the person
// under My profile, sealed (SEC-11) and shown by its last four characters. A change takes effect only when someone who
// holds A on "Payment batches", never the person, approves it; while it waits, payments to the person are frozen (PRD-001
// rev 0.154). The person's operations, under /v1/me, are under "Expense claims" (C own) and reach the caller's own
// account only; the approver reads a person's change, its files and its number under "Payment batches" (p1-payments-out
// design D9, D10).

// ClaimantBankChange is a change of a person's own bank account.
type ClaimantBankChange struct {
	ID           string        `json:"id" format:"uuid" doc:"The change's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b10"`
	MemberUserID string        `json:"member_user_id" format:"uuid" doc:"The person whose account it is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Status       string        `json:"status" enum:"draft,waiting,approved" doc:"draft: being prepared; waiting: asked for, waiting for approval, and payments to the person are frozen; approved: in force, or replaced since (in_force says)." example:"waiting"`
	InForce      bool          `json:"in_force" doc:"Its account is the person's account for reimbursements now." example:"false"`
	Account      PayeeAccount  `json:"account" doc:"The account it puts in force: a Singapore bank's, in SGD."`
	Replaces     *PayeeAccount `json:"replaces,omitempty" doc:"A draft or waiting change: the account in force it would replace, if any."`
	ApprovalID   string        `json:"approval_id,omitempty" format:"uuid" doc:"The approval request it waits for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b20"`
	PreparedBy   string        `json:"prepared_by,omitempty" doc:"Who saved its current version: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	ApprovedBy   string        `json:"approved_by,omitempty" doc:"Who approved it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	ApprovedAt   string        `json:"approved_at,omitempty" format:"date-time" doc:"When it was approved." example:"2026-10-28T10:30:00+08:00"`
	Checks       []BillCheck   `json:"checks" nullable:"false" doc:"A draft or waiting change: what stops it being asked for or approved (✖), and its warnings (⚠). Empty in a list."`
	Version      int           `json:"version" doc:"The change's version; with its status it makes the ETag." example:"1"`
}

// ClaimantBankDetails are a person's own bank details: the account in force and every change.
type ClaimantBankDetails struct {
	MemberUserID string               `json:"member_user_id" format:"uuid" doc:"The person." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Frozen       bool                 `json:"payments_frozen" doc:"A change waits for approval: payments to the person are frozen until it is approved, rejected or withdrawn (FR-AP-22)." example:"false"`
	InForce      *ClaimantBankChange  `json:"in_force,omitempty" doc:"The approved change in force, if any: reimbursements are paid into its account."`
	Changes      []ClaimantBankChange `json:"changes" nullable:"false" doc:"Every change, newest first."`
}

// ClaimantBankChangeDraft is a change of a person's own account as sent: a Singapore bank's account in SGD.
type ClaimantBankChangeDraft struct {
	BIC           string `json:"bic" minLength:"8" maxLength:"11" doc:"The bank's SWIFT/BIC code, 8 or 11 characters: a bank in Singapore." example:"DBSSSGSG"`
	AccountName   string `json:"account_name" maxLength:"140" doc:"The name on the account, as the bank holds it: yours." example:"Tan Ah Kow"`
	AccountNumber string `json:"account_number,omitempty" maxLength:"40" doc:"The full number as your bank shows it, 6 to 17 digits; spaces and hyphens are dropped. Needed to prepare a change; left out when replacing a draft, the number saved before stays (unless the bank changes). Sealed on save, never logged or audited (SEC-11)." example:"0123456789"`
}

// ClaimantBankChangePath names a change of a person's own account in the path.
type ClaimantBankChangePath struct {
	ChangeID string `path:"change_id" format:"uuid" doc:"The change's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b10"`
}

type claimantCreateInput struct {
	KeyHeader
	PreferHeader
	Body ClaimantBankChangeDraft
}

type claimantReplaceInput struct {
	ClaimantBankChangePath
	IfMatchHeader
	PreferHeader
	Body ClaimantBankChangeDraft
}

type claimantCommandInput struct {
	ClaimantBankChangePath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type claimantDetailsOutput struct{ Body ClaimantBankDetails }

type claimantReadOutput struct {
	ETag string `header:"ETag" doc:"The change's version and status, for If-Match."`
	Body ClaimantBankChange
}

type claimantWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The change's version and status, for If-Match."`
	Location string `header:"Location" doc:"The change's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     ClaimantBankChange
}

// claimantDraftOf is a person's change as the domain takes it: a Singapore bank's account in SGD.
func claimantDraftOf(b ClaimantBankChangeDraft) payee.Draft {
	return payee.Draft{Currency: "SGD", Kind: payee.Singapore, BIC: strings.ToUpper(strings.TrimSpace(b.BIC)), BankCountry: "SG",
		AccountName: b.AccountName, Number: b.AccountNumber}
}

// claimantETag is a person's change's ETag, as a vendor's.
func claimantETag(c ClaimantBankChange) string {
	return payeeETag(PayeeBankChange{Status: c.Status, Version: c.Version})
}

// claimantChangeOf is a person's change as the API shows it; with checks, what stops it and its warnings.
func (a *API) claimantChangeOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, waiting map[uuid.UUID]storage.ApprovalRequest,
	checks bool) (ClaimantBankChange, error) {
	c, err := a.payeeChangeOf(ctx, u, doc, waiting, checks)
	if err != nil {
		return ClaimantBankChange{}, err
	}
	s, err := payee.Read(doc)
	if err != nil {
		return ClaimantBankChange{}, err
	}
	return ClaimantBankChange{ID: c.ID, MemberUserID: s.MemberUserID, Status: c.Status, InForce: c.InForce, Account: c.Account, Replaces: c.Replaces,
		ApprovalID: c.ApprovalID, PreparedBy: c.PreparedBy, ApprovedBy: c.ApprovedBy, ApprovedAt: c.ApprovedAt, Checks: c.Checks, Version: c.Version}, nil
}

// claimantChange is a person's change with id, refused for any other kind of document. With person set, it is refused
// unless it is that person's own: not found, so another person's changes are not even confirmed to exist.
func claimantChange(ctx context.Context, u storage.UnitOfWork, id, person uuid.UUID, lock bool) (storage.Document, error) {
	get := u.Documents().Get
	if lock {
		get = u.Documents().Lock
	}
	doc, err := get(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentClaimantBankChange {
		return doc, problem(payee.CodeNotChange, "That record is not a change of a person's bank account.", "Use the change's ID.")
	}
	if person != uuid.Nil {
		s, err := payee.Read(doc)
		if err != nil {
			return doc, err
		}
		if s.MemberUserID != person.String() {
			return doc, ports.ErrNotFound
		}
	}
	return doc, nil
}

// me is the caller as the person whose own bank account is read or changed: a member signed in as themselves.
func me(ctx context.Context) (authn.Principal, error) {
	p, _ := authn.From(ctx)
	if p.UserID == uuid.Nil || p.Class != authn.ClassUser || p.AI() {
		return p, problem(payee.CodeOwnOnly, "A bank account for reimbursements is a person's own: only a member signed in as themselves keeps one.",
			"Sign in as yourself")
	}
	return p, nil
}

// claimantDetails are a person's account in force and every change of it.
func (a *API) claimantDetails(ctx context.Context, u storage.UnitOfWork, person uuid.UUID) (ClaimantBankDetails, error) {
	out := ClaimantBankDetails{MemberUserID: person.String(), Changes: []ClaimantBankChange{}}
	who := payee.Person(person)
	waiting, err := payee.WaitingFor(ctx, u, who, a.deps.Clock.Now())
	if err != nil {
		return out, err
	}
	out.Frozen = len(waiting) > 0
	accounts, err := payee.Accounts(ctx, u, who)
	if err != nil {
		return out, err
	}
	for _, acc := range accounts {
		doc, err := u.Documents().Get(ctx, acc.ChangeID)
		if err != nil {
			return out, err
		}
		c, err := a.claimantChangeOf(ctx, u, doc, waiting, false)
		if err != nil {
			return out, err
		}
		out.Changes = append(out.Changes, c)
		if c.InForce {
			in := c
			out.InForce = &in
		}
	}
	return out, nil
}

// claimantBankExecutable refuses a signer of a person's change who is the person, or who saved its current version
// (FR-AP-22, FM-1); the database refuses both too (FY237).
func claimantBankExecutable(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, signer uuid.UUID) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return err
	}
	s, err := payee.Read(doc)
	if err != nil {
		return err
	}
	if s.MemberUserID == signer.String() || doc.SavedBy == (storage.Actor{Type: "user", ID: signer.String()}).String() {
		return problem(payee.CodeOwnOnly, "This is your own bank account, or you prepared the change, so another person approves it (FM-1).",
			"Ask another person who holds the approval right on payment batches")
	}
	return nil
}

// approveClaimantBankChange puts a person's change in force on its approval, as the approver.
func (a *API) approveClaimantBankChange(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest) (storage.Document, error) {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return storage.Document{}, err
	}
	return payee.Approve(ctx, u, id, a.deps.Clock.Now())
}

// ClaimantBankCodes are the refusals of a person's change's commands.
var ClaimantBankCodes = []string{payee.CodeNotChange, payee.CodeBadDraft, payee.CodeMember, payee.CodePerson, payee.CodeBIC, payee.CodeCountry,
	payee.CodeNumber, payee.CodeName, payee.CodeNotDraft, payee.CodeNoKeys, payee.CodeOwnOnly, storage.CodePayee}

// ClaimantBankApprovalCodes are the codes an approval of a person's change may answer: FM-1, and the change's own checks.
func ClaimantBankApprovalCodes() []string {
	return append([]string{payee.CodeEvidence, payee.CodeWaiting, payee.CodeUnchanged}, ClaimantBankCodes...)
}

func registerClaimantBankDetails(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.GetMyBankDetails, huma.Operation{
		OperationID: "getMyBankDetails", Method: http.MethodGet, Path: "/v1/me/bank-details", Tags: []string{"Expense claims"},
		Summary: "Get your bank account for reimbursements",
		Description: "Your own bank account for reimbursements in force, if any, and every change of it, newest first, with whether payments " +
			"to you are frozen because a change waits for approval (FR-AP-22; PRD-001 rev 0.154). The number shows its last four characters " +
			"only (SEC-11). Read-only; no side effects.",
	}, []string{payee.CodeOwnOnly}, func(ctx context.Context, _ *struct{}) (*claimantDetailsOutput, error) {
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		var out claimantDetailsOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			out.Body, err = a.claimantDetails(ctx, u, p.UserID)
			return err
		})
		return &out, err
	})

	register(a, authz.CreateMyBankChange, huma.Operation{
		OperationID: "createMyBankChange", Method: http.MethodPost, Path: "/v1/me/bank-changes", Tags: []string{"Expense claims"},
		DefaultStatus: http.StatusCreated, Summary: "Prepare a change of your bank account for reimbursements",
		Description: "Saves a draft change of your own bank account for reimbursements (FR-AP-22; PRD-001 rev 0.154): a Singapore bank's " +
			"account in SGD, with the full number, which is sealed (SEC-11) and kept with its last four characters and a keyed fingerprint. " +
			"Nothing changes until the change is asked for and someone who holds A on \"Payment batches\", never you, approves it: attach a " +
			"screenshot of your banking app or your statement's header (evidence type bank_app_screenshot or bank_statement) and ask with " +
			"requestMyBankChangeApproval. Side effects: the change, its sealed number and their audit events, which carry the last four " +
			"characters and the fingerprint only (AUD-02). Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld},
		append(locks, ClaimantBankCodes...)...),
		func(ctx context.Context, in *claimantCreateInput) (*claimantWriteOutput, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ClaimantBankChange, error) {
				doc, err := payee.CreateFor(ctx, u, a.deps.Fields, p.Organisation, payee.Person(p.UserID), claimantDraftOf(in.Body))
				if err != nil {
					return ClaimantBankChange{}, payeeProblems(err)
				}
				if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
					return ClaimantBankChange{}, err
				}
				return a.claimantChangeOf(ctx, u, doc, nil, true)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &claimantWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &claimantWriteOutput{Status: w.Status, ETag: claimantETag(w.Body), Location: "/v1/me/bank-changes/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetMyBankChange, huma.Operation{
		OperationID: "getMyBankChange", Method: http.MethodGet, Path: "/v1/me/bank-changes/{change_id}", Tags: []string{"Expense claims"},
		Summary: "Get a change of your bank account",
		Description: "Returns a change of your own bank account with its status, the account it puts in force and the one it would replace " +
			"(last four characters only), and for a draft or waiting change what stops it being asked for or approved, with its ETag " +
			"(FR-AP-22). Another person's change is not found. Read-only; no side effects.",
	}, []string{CodeNotFound, payee.CodeNotChange, payee.CodeBadDraft, payee.CodeOwnOnly}, func(ctx context.Context, in *ClaimantBankChangePath) (*claimantReadOutput, error) {
		id, err := parseID("change_id", in.ChangeID)
		if err != nil {
			return nil, err
		}
		p, err := me(ctx)
		if err != nil {
			return nil, err
		}
		var out claimantReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := claimantChange(ctx, u, id, p.UserID, false)
			if err != nil {
				return err
			}
			waiting, err := payee.WaitingFor(ctx, u, payee.Person(p.UserID), a.deps.Clock.Now())
			if err != nil {
				return err
			}
			out.Body, err = a.claimantChangeOf(ctx, u, doc, waiting, true)
			out.ETag = claimantETag(out.Body)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceMyBankChange, huma.Operation{
		OperationID: "replaceMyBankChange", Method: http.MethodPut, Path: "/v1/me/bank-changes/{change_id}", Tags: []string{"Expense claims"},
		Summary: "Change a draft change of your bank account",
		Description: "Saves a draft change's new content as its next version (FR-AP-22). Left out, the account number stays as saved, unless " +
			"the bank changes, which needs the number again. Saving voids an approval it waits for: ask again. An approved change never " +
			"changes: prepare another. Side effects: the new version, its sealed number and their audit events. Needs If-Match with the " +
			"change's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType}, append(locks, ClaimantBankCodes...)...),
		func(ctx context.Context, in *claimantReplaceInput) (*claimantWriteOutput, error) {
			id, err := parseID("change_id", in.ChangeID)
			if err != nil {
				return nil, err
			}
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ClaimantBankChange, error) {
				doc, err := claimantChange(ctx, u, id, p.UserID, true)
				if err != nil {
					return ClaimantBankChange{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(doc.Version, string(doc.Status))); err != nil {
					return ClaimantBankChange{}, err
				}
				if doc, err = payee.Revise(ctx, u, a.deps.Fields, p.Organisation, id, claimantDraftOf(in.Body)); err != nil {
					return ClaimantBankChange{}, payeeProblems(err)
				}
				if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
					return ClaimantBankChange{}, err
				}
				return a.claimantChangeOf(ctx, u, doc, nil, true)
			})
			if err != nil {
				return nil, err
			}
			return &claimantWriteOutput{Status: w.Status, ETag: claimantETag(w.Body), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.RequestMyBankChangeApproval, huma.Operation{
		OperationID: "requestMyBankChangeApproval", Method: http.MethodPost, Path: "/v1/me/bank-changes/{change_id}/approval", Tags: []string{"Expense claims"},
		Summary: "Ask for approval of a change of your bank account", DefaultStatus: http.StatusAccepted,
		Description: "Asks for approval of a draft change of your own bank account at the version read (FR-AP-22, FM-1), in every team mode. " +
			"Refused while no screenshot of your banking app or statement header is attached (PAYEE-11), while another change of yours waits " +
			"(PAYEE-13), or when the account is the one in force (PAYEE-14). From now until the change is approved, rejected, withdrawn or " +
			"expires, payments to you are frozen (PAYEE-12). The answer is 202 with the approval request, which a person who holds A on " +
			"\"Payment batches\", is not you and passed step-up approves; the approval puts the account in force and retires the one it " +
			"replaces (the database checks both, FY237). Asking needs you signed in and stepped up, and If-Match with the change's ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired,
		CodeStepUpRequired, approval.CodePending, storage.CodeApproval, payee.CodeEvidence, payee.CodeWaiting, payee.CodeUnchanged}, append(locks, ClaimantBankCodes...)...),
		func(ctx context.Context, in *claimantCommandInput) (*payeeRequestOutput, error) {
			id, err := parseID("change_id", in.ChangeID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := claimantChange(ctx, u, id, p.UserID, true)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(doc.Version, string(doc.Status))); err != nil {
					return Approval{}, err
				}
				if doc.Status != storage.DocumentDraft {
					return Approval{}, problem(payee.CodeNotDraft, "The change is approved already.", "Prepare another change")
				}
				errs, _, err := payee.Checks(ctx, u, doc, a.deps.Clock.Now())
				if err != nil {
					return Approval{}, err
				}
				if len(errs) > 0 {
					return Approval{}, payeeProblems(errs)
				}
				c, err := a.claimantChangeOf(ctx, u, doc, nil, false)
				if err != nil {
					return Approval{}, err
				}
				name, err := memberNames(ctx, u, a.deps.Clock.Now())
				if err != nil {
					return Approval{}, err
				}
				who := name[p.UserID]
				if who == "" {
					who = "A member"
				}
				after := accountWords(c.Account)
				before := ""
				if c.Replaces != nil {
					before = accountWords(*c.Replaces)
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectClaimantBankChange, ObjectKey: doc.ID.String(),
					Operation: approval.OpClaimantBankChange, Summary: fmt.Sprintf("Change %s's bank account for reimbursements to %s", who, after),
					Execute: map[string]any{"change_id": doc.ID.String(), "version": fmt.Sprint(doc.Version)},
					Diff:    []ApprovalDiff{{Field: "SGD account for reimbursements", Before: before, After: after}}})
			})
			if err != nil {
				return nil, err
			}
			return &payeeRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.WithdrawMyBankChange, huma.Operation{
		OperationID: "withdrawMyBankChange", Method: http.MethodPost, Path: "/v1/me/bank-changes/{change_id}/withdraw", Tags: []string{"Expense claims"},
		Summary: "Withdraw a change of your bank account from approval",
		Description: "Withdraws your own change waiting for approval (FR-AP-22): its request becomes void with the reason \"withdrawn by its " +
			"requester\", any approvals it had are voided, and the change is a draft again; your account in force stays as it was, and " +
			"payments to you are no longer held. A person reaches their change's request only this way, never through the approvals, as a " +
			"claimant does their claim's. Another person's change is not found. Needs If-Match with the change's ETag. Side effects: the " +
			"request void and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, payee.CodeNotChange,
		payee.CodeBadDraft, payee.CodeOwnOnly, approval.CodeNotPending, storage.CodeApproval, storage.CodeVersion}, locks...),
		func(ctx context.Context, in *claimantCommandInput) (*claimantWriteOutput, error) {
			id, err := parseID("change_id", in.ChangeID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ClaimantBankChange, error) {
				doc, err := claimantChange(ctx, u, id, p.UserID, true)
				if err != nil {
					return ClaimantBankChange{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(doc.Version, string(doc.Status))); err != nil {
					return ClaimantBankChange{}, err
				}
				waiting, err := payee.WaitingFor(ctx, u, payee.Person(p.UserID), a.deps.Clock.Now())
				if err != nil {
					return ClaimantBankChange{}, err
				}
				r, ok := waiting[id]
				if !ok || r.RequestedBy != p.UserID {
					return ClaimantBankChange{}, problem(approval.CodeNotPending, "The change is not waiting for approval.", "Nothing to withdraw")
				}
				locked, err := u.Approvals().Lock(ctx, r.ID)
				if err != nil {
					return ClaimantBankChange{}, err
				}
				if _, err := u.Approvals().Decide(ctx, locked.ID, locked.Version, string(approval.Void), "withdrawn by its requester", a.deps.Clock.Now()); err != nil {
					return ClaimantBankChange{}, err
				}
				return a.claimantChangeOf(ctx, u, doc, nil, true)
			})
			if err != nil {
				return nil, err
			}
			return &claimantWriteOutput{Status: w.Status, ETag: claimantETag(w.Body), Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	reveal := func(id uuid.UUID, p authn.Principal, person uuid.UUID) func(ctx context.Context, u storage.UnitOfWork) (RevealedPayeeAccount, error) {
		return func(ctx context.Context, u storage.UnitOfWork) (RevealedPayeeAccount, error) {
			doc, err := claimantChange(ctx, u, id, person, false)
			if err != nil {
				return RevealedPayeeAccount{}, err
			}
			acc, err := u.PayeeBankAccounts().OfChange(ctx, id, doc.Version)
			if err != nil {
				return RevealedPayeeAccount{}, err
			}
			number, err := payee.Open(ctx, a.deps.Fields, p.Organisation, acc)
			if err != nil {
				return RevealedPayeeAccount{}, err
			}
			if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "document.bank_account_revealed", ObjectType: "document", ObjectID: id,
				Changes: map[string][2]any{"account_number": {nil, map[string]any{"last4": acc.Last4, "currency": acc.Currency}}}}); err != nil {
				return RevealedPayeeAccount{}, err
			}
			return RevealedPayeeAccount{ChangeID: id.String(), AccountNumber: number}, nil
		}
	}
	revealExempt := func(op huma.Operation) huma.Operation {
		return exempt(exempt(exempt(op,
			"Idempotency-Key", "A reveal changes no record: it shows the number again and records another look in the audit trail, as a repeat should (FR-AP-22)."),
			"Prefer", "A reveal has no change to preview; a preview would show the number, or nothing, without the look being recorded (SEC-11)."),
			"If-Match", "A reveal changes no record: it reads the change's account at its current version, whatever the version the caller read.")
	}

	register(a, authz.RevealMyBankAccount, revealExempt(huma.Operation{
		OperationID: "revealMyBankAccount", Method: http.MethodPost, Path: "/v1/me/bank-changes/{change_id}/reveal", Tags: []string{"Expense claims"},
		Summary: "Reveal your account's full number",
		Description: "Shows the full number of a change of your own bank account at its current version, to check it (FR-AP-22): you signed " +
			"in and stepped up in the last 5 minutes. Another person's change is not found. Side effects: an audit event of the reveal, " +
			"naming the account by its last four characters (SEC-11, AUD-02). Never cached: ask again each time.",
	}), append([]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, payee.CodeNotChange, payee.CodeNoKeys, payee.CodeOwnOnly}, locks...),
		func(ctx context.Context, in *ClaimantBankChangePath) (*payeeRevealOutput, error) {
			id, err := parseID("change_id", in.ChangeID)
			if err != nil {
				return nil, err
			}
			if _, err := me(ctx); err != nil {
				return nil, err
			}
			p, err := a.steppedUpPerson(ctx, "Revealing an account number")
			if err != nil {
				return nil, err
			}
			var out payeeRevealOutput
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				out.Body, err = reveal(id, p, p.UserID)(ctx, u)
				return err
			})
			return &out, err
		})

	register(a, authz.GetClaimantBankChange, huma.Operation{
		OperationID: "getClaimantBankChange", Method: http.MethodGet, Path: "/v1/claimant-bank-changes/{change_id}", Tags: []string{"Expense claims"},
		Summary: "Get a change of a person's bank account",
		Description: "Returns a change of a person's own bank account for reimbursements, for whoever approves it or pays the person (FR-AP-22): " +
			"its status, the account it puts in force and the one it would replace (last four characters only), and for a draft or waiting " +
			"change what stops it being approved and its warnings, such as the same account in force for another payee (FR-CT-04). " +
			"Read-only; no side effects.",
	}, []string{CodeNotFound, payee.CodeNotChange, payee.CodeBadDraft}, func(ctx context.Context, in *ClaimantBankChangePath) (*claimantReadOutput, error) {
		id, err := parseID("change_id", in.ChangeID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out claimantReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := claimantChange(ctx, u, id, uuid.Nil, false)
			if err != nil {
				return err
			}
			s, err := payee.Read(doc)
			if err != nil {
				return err
			}
			who, err := s.Payee()
			if err != nil {
				return err
			}
			waiting, err := payee.WaitingFor(ctx, u, who, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			out.Body, err = a.claimantChangeOf(ctx, u, doc, waiting, true)
			out.ETag = claimantETag(out.Body)
			return err
		})
		return &out, err
	})

	register(a, authz.RevealClaimantBankAccount, revealExempt(huma.Operation{
		OperationID: "revealClaimantBankAccount", Method: http.MethodPost, Path: "/v1/claimant-bank-changes/{change_id}/reveal", Tags: []string{"Expense claims"},
		Summary: "Reveal a person's account's full number",
		Description: "Shows the full number of a change of a person's bank account at its current version, for the approver comparing it " +
			"with the person's screenshot or statement (FR-AP-22): a person who holds A on \"Payment batches\", signed in and stepped up in " +
			"the last 5 minutes. Side effects: an audit event of the reveal, naming the account by its last four characters (SEC-11, " +
			"AUD-02). Never cached: ask again each time.",
	}), append([]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, payee.CodeNotChange, payee.CodeNoKeys}, locks...),
		func(ctx context.Context, in *ClaimantBankChangePath) (*payeeRevealOutput, error) {
			id, err := parseID("change_id", in.ChangeID)
			if err != nil {
				return nil, err
			}
			p, err := a.steppedUpPerson(ctx, "Revealing an account number")
			if err != nil {
				return nil, err
			}
			var out payeeRevealOutput
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				out.Body, err = reveal(id, p, uuid.Nil)(ctx, u)
				return err
			})
			return &out, err
		})
}
