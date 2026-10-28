package api

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ vendors' bank details (FR-CT-03, FR-CT-04, SOD-1, FM-1)
//
// A vendor's bank account in one currency changes only through a change of its bank details: prepared as a draft with
// the files that support it, asked for, and approved by a second person who holds A on "Vendor bank details", who
// neither prepared nor asked for it. While a change waits, payments to the vendor are frozen and every founder is told
// (PRD-001 rev 0.152). The account number is sealed (SEC-11) and shown by its last four characters; the full number is
// revealed one account at a time, after step-up, and the reveal is audited.

// PayeeAccount is a vendor's bank account as the API shows it: never the full number.
type PayeeAccount struct {
	Currency    string `json:"currency" doc:"The currency paid into it." example:"SGD"`
	Kind        string `json:"kind" enum:"singapore,overseas" doc:"singapore: a Singapore bank's account, paid by FAST or GIRO; overseas: a bank's account outside Singapore, paid by telegraphic transfer." example:"singapore"`
	BIC         string `json:"bic" doc:"The bank's SWIFT/BIC code." example:"DBSSSGSG"`
	BankCountry string `json:"bank_country" doc:"The bank's country, ISO 3166 two-letter code." example:"SG"`
	AccountName string `json:"account_name" doc:"The name on the account, as the bank holds it." example:"Northwind Analytics Pte. Ltd."`
	NumberKind  string `json:"number_kind" enum:"account,iban" doc:"Whether the number is an IBAN." example:"account"`
	NumberLast4 string `json:"account_number_last4" doc:"The account number's last four characters. The full number is never in a list or a record: revealPayeeBankAccount shows it, after step-up, and the reveal is audited (SEC-11)." example:"6789"`
}

// PayeeBankChange is a change of a vendor's bank account in one currency.
type PayeeBankChange struct {
	ID           string        `json:"id" format:"uuid" doc:"The change's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
	ContactID    string        `json:"contact_id" format:"uuid" doc:"The vendor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
	Status       string        `json:"status" enum:"draft,waiting,approved" doc:"draft: being prepared; waiting: asked for, waiting for approval, and payments to the vendor are frozen; approved: in force, or replaced since (in_force says)." example:"waiting"`
	InForce      bool          `json:"in_force" doc:"Its account is the vendor's account for its currency now." example:"false"`
	Account      PayeeAccount  `json:"account" doc:"The account it puts in force."`
	Replaces     *PayeeAccount `json:"replaces,omitempty" doc:"A draft or waiting change: the account in force it would replace, if any."`
	CallbackNote string        `json:"callback_note,omitempty" doc:"Who called the vendor back, when, and on which number: a number known before, never one from the letter." example:"Called Jane Tan, Northwind finance, on 6123 4567 from our records, 27 Oct 2026 10:15"`
	ApprovalID   string        `json:"approval_id,omitempty" format:"uuid" doc:"The approval request it waits for." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a20"`
	PreparedBy   string        `json:"prepared_by,omitempty" doc:"Who saved its current version: an actor of the audit trail." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	ApprovedBy   string        `json:"approved_by,omitempty" doc:"Who approved it." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	ApprovedAt   string        `json:"approved_at,omitempty" format:"date-time" doc:"When it was approved." example:"2026-10-27T10:30:00+08:00"`
	Checks       []BillCheck   `json:"checks" nullable:"false" doc:"A draft or waiting change: what stops it being asked for or approved (✖), and the warnings it is asked for with (⚠). Empty in a list."`
	Version      int           `json:"version" doc:"The change's version; with its status it makes the ETag." example:"1"`
}

// PayeeBankDetails is a vendor's bank details: its accounts in force and every change.
type PayeeBankDetails struct {
	ContactID string            `json:"contact_id" format:"uuid" doc:"The vendor." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
	Frozen    bool              `json:"payments_frozen" doc:"A change waits for approval: no payment date can be planned for the vendor's bills, and a payment recorded is flagged (FR-CT-03)." example:"false"`
	InForce   []PayeeBankChange `json:"in_force" nullable:"false" doc:"The approved change in force for each currency, by currency."`
	Changes   []PayeeBankChange `json:"changes" nullable:"false" doc:"Every change, newest first."`
}

// PayeeBankChangeDraft is a change as sent.
type PayeeBankChangeDraft struct {
	Currency      string `json:"currency" minLength:"3" maxLength:"3" doc:"The currency paid into the account: a change is for one currency, and keeps it." example:"SGD"`
	Kind          string `json:"kind" enum:"singapore,overseas" doc:"singapore: a Singapore bank (bank_country SG); overseas: a bank outside Singapore." example:"singapore"`
	BIC           string `json:"bic" minLength:"8" maxLength:"11" doc:"The bank's SWIFT/BIC code, 8 or 11 characters, whose country is the bank's." example:"DBSSSGSG"`
	BankCountry   string `json:"bank_country" minLength:"2" maxLength:"2" doc:"The bank's country, ISO 3166 two-letter code." example:"SG"`
	AccountName   string `json:"account_name" maxLength:"140" doc:"The name on the account, as the bank holds it." example:"Northwind Analytics Pte. Ltd."`
	AccountNumber string `json:"account_number,omitempty" maxLength:"40" doc:"The full number as the vendor's letter prints it; spaces and hyphens are dropped. A Singapore account: 6 to 17 digits. Overseas: 4 to 34 letters and digits, or an IBAN, whose check digits are checked. Needed to prepare a change; left out when replacing a draft, the number saved before stays (unless the bank changes). Sealed on save, never logged or audited (SEC-11)." example:"0123456789"`
	CallbackNote  string `json:"callback_note,omitempty" maxLength:"500" doc:"Who you called back, when, and on which number: one you knew before, never one from the letter (recommended evidence, FR-DOC-06)." example:"Called Jane Tan, Northwind finance, on 6123 4567 from our records, 27 Oct 2026 10:15"`
}

// PayeeBankChangePath names a change in the path.
type PayeeBankChangePath struct {
	ChangeID string `path:"change_id" format:"uuid" doc:"The change's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
}

// RevealedPayeeAccount is one account's full number, revealed after step-up.
type RevealedPayeeAccount struct {
	ChangeID      string `json:"change_id" format:"uuid" doc:"The change whose account it is." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5a10"`
	AccountNumber string `json:"account_number" doc:"The full number, as kept: capital letters and digits. Do not copy it anywhere it would be kept." example:"0123456789"`
}

type payeeDetailsInput struct{ ContactPath }

// PayeeBankChangeNew is a new change: its vendor and the account.
type PayeeBankChangeNew struct {
	ContactID string `json:"contact_id" format:"uuid" doc:"The vendor whose bank details change." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4db0"`
	PayeeBankChangeDraft
}

type payeeCreateInput struct {
	KeyHeader
	PreferHeader
	Body PayeeBankChangeNew
}

type payeeReplaceInput struct {
	PayeeBankChangePath
	IfMatchHeader
	PreferHeader
	Body PayeeBankChangeDraft
}

type payeeCommandInput struct {
	PayeeBankChangePath
	KeyHeader
	IfMatchHeader
	PreferHeader
}

type payeeDetailsOutput struct{ Body PayeeBankDetails }

type payeeReadOutput struct {
	ETag string `header:"ETag" doc:"The change's version and status, for If-Match."`
	Body PayeeBankChange
}

type payeeWriteOutput struct {
	Status   int
	ETag     string `header:"ETag" doc:"The change's version and status, for If-Match."`
	Location string `header:"Location" doc:"The change's URL."`
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     PayeeBankChange
}

type payeeRequestOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was asked (FR-INT-14)."`
	Body     Approval
}

type payeeRevealOutput struct{ Body RevealedPayeeAccount }

// payeeETag is a change's ETag: its document's version and status (draft or posted), which asking for approval changes
// neither of.
func payeeETag(c PayeeBankChange) string {
	if c.Status == "approved" {
		return etag(c.Version, string(storage.DocumentPosted))
	}
	return etag(c.Version, string(storage.DocumentDraft))
}

func payeeDraftOf(b PayeeBankChangeDraft) payee.Draft {
	return payee.Draft{Currency: strings.ToUpper(strings.TrimSpace(b.Currency)), Kind: b.Kind, BIC: strings.ToUpper(strings.TrimSpace(b.BIC)),
		BankCountry: strings.ToUpper(strings.TrimSpace(b.BankCountry)), AccountName: b.AccountName, Number: b.AccountNumber, CallbackNote: b.CallbackNote}
}

func payeeAccountOf(s payee.Stored) PayeeAccount {
	return PayeeAccount{Currency: s.Currency, Kind: s.Kind, BIC: s.BIC, BankCountry: s.BankCountry, AccountName: s.AccountName,
		NumberKind: s.NumberKind, NumberLast4: s.NumberLast4}
}

// payeeProblems turns a change's failed rules into one problem listing them all.
func payeeProblems(err error) error {
	probs, ok := err.(payee.Problems)
	if !ok || len(probs) == 0 {
		return err
	}
	fields := make([]FieldProblem, len(probs))
	for i, x := range probs {
		fields[i] = FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix}
	}
	return problem(probs[0].Code, fmt.Sprintf("The change of bank details cannot be saved or asked for: %d problem(s), each listed in errors.", len(probs)),
		"Correct them and send it again.", fields...)
}

// payeeChecksOf are a change's problems and warnings as the API lists them.
func payeeChecksOf(errs, warnings payee.Problems) []BillCheck {
	out := []BillCheck{}
	for _, x := range errs {
		out = append(out, BillCheck{Severity: "error", Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	for _, x := range warnings {
		out = append(out, BillCheck{Severity: "warning", Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
	}
	return out
}

// payeeChangeOf is a change as the API shows it; with checks, what stops it and what it would be asked for with.
func (a *API) payeeChangeOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, waiting map[uuid.UUID]storage.ApprovalRequest,
	checks bool) (PayeeBankChange, error) {
	s, err := payee.Read(doc)
	if err != nil {
		return PayeeBankChange{}, err
	}
	r, isWaiting := waiting[doc.ID]
	out := PayeeBankChange{ID: doc.ID.String(), ContactID: s.ContactID, Status: payee.StatusOf(doc, isWaiting), Account: payeeAccountOf(s),
		CallbackNote: s.CallbackNote, PreparedBy: doc.SavedBy, ApprovedBy: doc.PostedBy, Checks: []BillCheck{}, Version: doc.Version}
	if isWaiting {
		out.ApprovalID = r.ID.String()
	}
	if doc.PostedAt != nil {
		out.ApprovedAt = doc.PostedAt.UTC().Format(time.RFC3339)
	}
	own, err := u.PayeeBankAccounts().OfChange(ctx, doc.ID, doc.Version)
	if err != nil {
		return PayeeBankChange{}, err
	}
	out.InForce = own.State == storage.AccountInForce
	if doc.Status == storage.DocumentDraft {
		who, err := s.Payee()
		if err != nil {
			return PayeeBankChange{}, err
		}
		cur, err := payee.InForceFor(ctx, u, who, s.Currency)
		if err != nil {
			return PayeeBankChange{}, err
		}
		if cur != nil {
			in, err := u.Documents().Get(ctx, cur.ChangeID)
			if err != nil {
				return PayeeBankChange{}, err
			}
			if was, err := payee.Read(in); err == nil {
				acc := payeeAccountOf(was)
				out.Replaces = &acc
			}
		}
		if checks {
			errs, warnings, err := payee.Checks(ctx, u, doc, a.deps.Clock.Now())
			if err != nil {
				return PayeeBankChange{}, err
			}
			out.Checks = payeeChecksOf(errs, warnings)
		}
	}
	return out, nil
}

// payeeChange is the change with id, refused for any other kind of document.
func payeeChange(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, lock bool) (storage.Document, error) {
	get := u.Documents().Get
	if lock {
		get = u.Documents().Lock
	}
	doc, err := get(ctx, id)
	if err != nil {
		return doc, err
	}
	if doc.Type != storage.DocumentPayeeBankChange {
		return doc, problem(payee.CodeNotChange, "That record is not a change of bank details.", "Use the change's ID.")
	}
	return doc, nil
}

// notifyFounders tells everyone who holds A on "Vendor bank details" (in Founder mode, every founder) that a vendor's
// bank details are changing or changed (FR-CT-03): always on, and emailed at once.
func (a *API) notifyFounders(ctx context.Context, u storage.UnitOfWork, doc storage.Document, title string) error {
	return a.notify(ctx, u, notify.Event{Type: notify.PayeeBankType, RecordType: "payee_bank_change", RecordID: &doc.ID, Title: title,
		Link: "/purchases/bank-changes/" + doc.ID.String(), Audience: notify.Audience{Capability: string(authz.VendorBankDetails), Need: "A"}})
}

// PayeeCodes are the refusals of a change's commands.
var PayeeCodes = []string{payee.CodeNotChange, payee.CodeBadDraft, payee.CodeVendor, payee.CodeCurrency, payee.CodeBIC, payee.CodeCountry,
	payee.CodeNumber, payee.CodeName, payee.CodeTooLong, payee.CodeNotDraft, payee.CodeNoKeys, storage.CodePayee}

// PayeeApprovalCodes are the codes an approval of a change may answer: SOD-1, and the change's own checks.
func PayeeApprovalCodes() []string {
	return append([]string{payee.CodeOwnChange, payee.CodeEvidence, payee.CodeWaiting, payee.CodeUnchanged}, PayeeCodes...)
}

func registerPayeeBankDetails(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.GetPayeeBankDetails, huma.Operation{
		OperationID: "getPayeeBankDetails", Method: http.MethodGet, Path: "/v1/contacts/{contact_id}/bank-details", Tags: []string{"Contacts"},
		Summary: "Get a vendor's bank details",
		Description: "A vendor's bank accounts in force, one per currency, and every change of them, newest first, with whether payments to " +
			"the vendor are frozen because a change waits for approval (FR-CT-03; PRD-001 rev 0.152). Account numbers show their last four " +
			"characters only (SEC-11). Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *payeeDetailsInput) (*payeeDetailsOutput, error) {
		id, err := parseID("contact_id", in.ContactID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		out := payeeDetailsOutput{Body: PayeeBankDetails{ContactID: in.ContactID, InForce: []PayeeBankChange{}, Changes: []PayeeBankChange{}}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Contacts().Get(ctx, id); err != nil {
				return err
			}
			waiting, err := payee.Waiting(ctx, u, id, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			out.Body.Frozen = len(waiting) > 0
			accounts, err := u.PayeeBankAccounts().OfContact(ctx, id)
			if err != nil {
				return err
			}
			for _, acc := range accounts {
				doc, err := u.Documents().Get(ctx, acc.ChangeID)
				if err != nil {
					return err
				}
				c, err := a.payeeChangeOf(ctx, u, doc, waiting, false)
				if err != nil {
					return err
				}
				out.Body.Changes = append(out.Body.Changes, c)
				if c.InForce {
					out.Body.InForce = append(out.Body.InForce, c)
				}
			}
			slices.SortFunc(out.Body.InForce, func(x, y PayeeBankChange) int { return cmp.Compare(x.Account.Currency, y.Account.Currency) })
			return nil
		})
		return &out, err
	})

	register(a, authz.CreatePayeeBankChange, huma.Operation{
		OperationID: "createPayeeBankChange", Method: http.MethodPost, Path: "/v1/bank-changes", Tags: []string{"Contacts"},
		DefaultStatus: http.StatusCreated, Summary: "Prepare a change of a vendor's bank details",
		Description: "Saves a draft change of a vendor's bank account in one currency (FR-CT-03; PRD-001 rev 0.152): a Singapore account or " +
			"an overseas one, with the full number, which is sealed (SEC-11) and kept with its last four characters and a keyed fingerprint " +
			"(FR-CT-04). Nothing changes until the change is asked for and a second person approves it: attach the vendor's letter (evidence " +
			"type bank_letter, or vendor_confirmation) and ask with requestPayeeBankChangeApproval. Side effects: the change, its sealed " +
			"number and their audit events, which carry the last four characters and the fingerprint only (AUD-02). Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld}, append(locks, PayeeCodes...)...),
		func(ctx context.Context, in *payeeCreateInput) (*payeeWriteOutput, error) {
			id, err := uuid.Parse(in.Body.ContactID)
			if err != nil {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.contact_id", Message: "is not a contact's ID", Fix: "Send the vendor's ID"}})
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (PayeeBankChange, error) {
				doc, err := payee.Create(ctx, u, a.deps.Fields, p.Organisation, id, payeeDraftOf(in.Body.PayeeBankChangeDraft))
				if err != nil {
					return PayeeBankChange{}, payeeProblems(err)
				}
				if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
					return PayeeBankChange{}, err
				}
				return a.payeeChangeOf(ctx, u, doc, nil, true)
			})
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &payeeWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
			}
			return &payeeWriteOutput{Status: w.Status, ETag: payeeETag(w.Body), Location: "/v1/bank-changes/" + w.Body.ID,
				Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.GetPayeeBankChange, huma.Operation{
		OperationID: "getPayeeBankChange", Method: http.MethodGet, Path: "/v1/bank-changes/{change_id}", Tags: []string{"Contacts"},
		Summary: "Get a change of a vendor's bank details",
		Description: "Returns a change with its status, the account it puts in force and the one it would replace (last four characters " +
			"only), and for a draft or waiting change what stops it being asked for or approved and its warnings, with its ETag (FR-CT-03). " +
			"Read-only; no side effects.",
	}, []string{CodeNotFound, payee.CodeNotChange, payee.CodeBadDraft}, func(ctx context.Context, in *PayeeBankChangePath) (*payeeReadOutput, error) {
		id, err := parseID("change_id", in.ChangeID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out payeeReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := payeeChange(ctx, u, id, false)
			if err != nil {
				return err
			}
			s, err := payee.Read(doc)
			if err != nil {
				return err
			}
			contactID, _ := uuid.Parse(s.ContactID)
			waiting, err := payee.Waiting(ctx, u, contactID, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			out.Body, err = a.payeeChangeOf(ctx, u, doc, waiting, true)
			out.ETag = payeeETag(out.Body)
			return err
		})
		return &out, err
	})

	register(a, authz.ReplacePayeeBankChange, huma.Operation{
		OperationID: "replacePayeeBankChange", Method: http.MethodPut, Path: "/v1/bank-changes/{change_id}", Tags: []string{"Contacts"},
		Summary: "Change a draft change of bank details",
		Description: "Saves a draft change's new content as its next version (FR-CT-03). Its vendor and currency stay. Left out, the account " +
			"number stays as saved, unless the bank changes, which needs the number again. Saving voids an approval it waits for: ask " +
			"again. An approved change never changes: prepare another. Side effects: the new version, its sealed number and their audit " +
			"events. Needs If-Match with the change's ETag. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeInvalid, CodeMalformed, CodeTooLarge, CodeMediaType}, append(locks, PayeeCodes...)...),
		func(ctx context.Context, in *payeeReplaceInput) (*payeeWriteOutput, error) {
			id, err := parseID("change_id", in.ChangeID)
			if err != nil {
				return nil, err
			}
			p, _ := authn.From(ctx)
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (PayeeBankChange, error) {
				doc, err := payeeChange(ctx, u, id, true)
				if err != nil {
					return PayeeBankChange{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(doc.Version, string(doc.Status))); err != nil {
					return PayeeBankChange{}, err
				}
				if doc, err = payee.Revise(ctx, u, a.deps.Fields, p.Organisation, id, payeeDraftOf(in.Body)); err != nil {
					return PayeeBankChange{}, payeeProblems(err)
				}
				if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
					return PayeeBankChange{}, err
				}
				return a.payeeChangeOf(ctx, u, doc, nil, true)
			})
			if err != nil {
				return nil, err
			}
			return &payeeWriteOutput{Status: w.Status, ETag: payeeETag(w.Body), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.RequestPayeeBankChangeApproval, huma.Operation{
		OperationID: "requestPayeeBankChangeApproval", Method: http.MethodPost, Path: "/v1/bank-changes/{change_id}/approval", Tags: []string{"Contacts"},
		Summary: "Ask for approval of a change of bank details", DefaultStatus: http.StatusAccepted,
		Description: "Asks for a second person's approval of a draft change at the version read (FR-CT-03, SOD-1, FM-1), in every team mode. " +
			"Refused while the vendor's letter or confirmation is not attached (PAYEE-11), while another change of the same currency waits " +
			"(PAYEE-13), or when the account is the one in force (PAYEE-14); a missing call-back note is a warning. From now until the " +
			"change is approved, rejected, withdrawn or expires, payments to the vendor are frozen (PAYEE-12), and everyone who holds A on " +
			"\"Vendor bank details\" is told at once. The answer is 202 with the approval request, which a person who holds A, neither " +
			"prepared the change nor asked for it, and passed step-up approves; the approval puts the account in force and retires the " +
			"one it replaces (the database checks both, FY237). Asking needs a person signed in who passed step-up, and If-Match with " +
			"the change's ETag. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired,
		CodeStepUpRequired, approval.CodePending, storage.CodeApproval, payee.CodeEvidence, payee.CodeWaiting, payee.CodeUnchanged}, append(locks, PayeeCodes...)...),
		func(ctx context.Context, in *payeeCommandInput) (*payeeRequestOutput, error) {
			id, err := parseID("change_id", in.ChangeID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				doc, err := payeeChange(ctx, u, id, true)
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
				c, err := a.payeeChangeOf(ctx, u, doc, nil, false)
				if err != nil {
					return Approval{}, err
				}
				contactID, _ := uuid.Parse(c.ContactID)
				vendor, err := u.Contacts().Get(ctx, contactID)
				if err != nil {
					return Approval{}, err
				}
				after := accountWords(c.Account)
				before := ""
				if c.Replaces != nil {
					before = accountWords(*c.Replaces)
				}
				summary := fmt.Sprintf("Change %s's %s bank account to %s", vendor.LegalName, c.Account.Currency, after)
				r, err := a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectPayeeBankChange, ObjectKey: doc.ID.String(),
					Operation: approval.OpPayeeBankChange, Summary: summary,
					Execute: map[string]any{"change_id": doc.ID.String(), "version": fmt.Sprint(doc.Version)},
					Diff:    []ApprovalDiff{{Field: c.Account.Currency + " account", Before: before, After: after}}})
				if err != nil {
					return Approval{}, err
				}
				return r, a.notifyFounders(ctx, u, doc, fmt.Sprintf("%s's bank details are changing: payments to it are frozen until a second person approves", vendor.LegalName))
			})
			if err != nil {
				return nil, err
			}
			return &payeeRequestOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.RevealPayeeBankAccount, exempt(exempt(exempt(huma.Operation{
		OperationID: "revealPayeeBankAccount", Method: http.MethodPost, Path: "/v1/bank-changes/{change_id}/reveal", Tags: []string{"Contacts"},
		Summary: "Reveal an account's full number",
		Description: "Shows the full account number of a change at its current version (FR-CT-03; PRD-001 rev 0.152): for the person " +
			"preparing payments, or the approver comparing it with the vendor's letter: a person who holds C or A on \"Vendor bank " +
			"details\", signed in and stepped up in the last 5 minutes; a masked view (M) gets the number masked, and nothing is opened. " +
			"Side effects: an audit event of the reveal, " +
			"naming the account by its last four characters (SEC-11, AUD-02). Never cached: ask again each time.",
	}, "Idempotency-Key", "A reveal changes no record: it shows the number again and records another look in the audit trail, as a repeat should (FR-CT-03)."),
		"Prefer", "A reveal has no change to preview; a preview would show the number, or nothing, without the look being recorded (SEC-11)."),
		"If-Match", "A reveal changes no record: it reads the change's account at its current version, whatever the version the caller read."),
		append([]string{CodeNotFound, CodeSessionRequired, CodeStepUpRequired, payee.CodeNotChange, payee.CodeNoKeys}, locks...),
		func(ctx context.Context, in *PayeeBankChangePath) (*payeeRevealOutput, error) {
			id, err := parseID("change_id", in.ChangeID)
			if err != nil {
				return nil, err
			}
			if d, _ := ctx.Value(decisionKey{}).(authz.Decision); d.Right == authz.Masked { // Appendix D's M: never opened, never revealed
				return &payeeRevealOutput{Body: RevealedPayeeAccount{ChangeID: id.String(), AccountNumber: MaskedValue}}, nil
			}
			p, err := a.steppedUpPerson(ctx, "Revealing an account number")
			if err != nil {
				return nil, err
			}
			var out payeeRevealOutput
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				doc, err := payeeChange(ctx, u, id, false)
				if err != nil {
					return err
				}
				acc, err := u.PayeeBankAccounts().OfChange(ctx, id, doc.Version)
				if err != nil {
					return err
				}
				number, err := payee.Open(ctx, a.deps.Fields, p.Organisation, acc)
				if err != nil {
					return err
				}
				if err := u.Audit().Record(ctx, storage.AuditRecord{Action: "document.bank_account_revealed", ObjectType: "document", ObjectID: id,
					Changes: map[string][2]any{"account_number": {nil, map[string]any{"last4": acc.Last4, "currency": acc.Currency}}}}); err != nil {
					return err
				}
				out.Body = RevealedPayeeAccount{ChangeID: id.String(), AccountNumber: number}
				return nil
			})
			return &out, err
		})
}

// accountWords names an account in an approval's summary and diff: its bank, the last four characters and the name.
func accountWords(x PayeeAccount) string {
	return fmt.Sprintf("%s ••••%s (%s)", x.BIC, x.NumberLast4, x.AccountName)
}

// payeeDependencies are a change's version and status, and the change in force for its payee and currency: a vendor's
// change or a person's.
func (a *API) payeeDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return 0, nil, err
	}
	s, err := payee.Read(doc)
	if err != nil {
		return 0, nil, err
	}
	who, err := s.Payee()
	if err != nil {
		return 0, nil, err
	}
	cur, err := payee.InForceFor(ctx, u, who, s.Currency)
	if err != nil {
		return 0, nil, err
	}
	replaces := ""
	if cur != nil {
		replaces = cur.ChangeID.String()
	}
	return doc.Version, map[string]any{"status": string(doc.Status), "replaces": replaces}, nil
}

// executePayeeBankChange puts the change in force on its approval, as the approver, and tells the founders.
func (a *API) executePayeeBankChange(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest) error {
	id, err := uuid.Parse(r.ObjectKey)
	if err != nil {
		return err
	}
	doc, err := payee.Approve(ctx, u, id, a.deps.Clock.Now())
	if err != nil {
		return err
	}
	return a.notifyFounders(ctx, u, doc, "A change of bank details was approved: "+r.Summary)
}
