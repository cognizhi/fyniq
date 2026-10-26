package claim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a reimbursement's refusals (ENG-23). CLAIM-09 to CLAIM-11 are the API's.
const (
	CodeNotReimbursement = "CLAIM-12" // the document is not a reimbursement of expense claims
	CodeNotOwed          = "CLAIM-13" // a claim that is not a posted claim of the claimant, or one named twice
	CodeOverpaid         = "CLAIM-14" // more than the company still owes on a claim
	CodeNotPosted        = "CLAIM-15" // only a posted reimbursement is voided
	CodeVoidDate         = "CLAIM-16" // a void dated before the reimbursement
)

// The longest reference a reimbursement keeps, and the most claims one settles.
const (
	MaxReference = 140
	MaxClaims    = 100
)

// Part is what a reimbursement pays back of one claim, in the base currency.
type Part struct {
	ClaimID uuid.UUID
	Amount  money.Money
}

// Reimbursement is money paid back to a claimant for their claims, recorded by hand until payment batches (FR-AP-07; the
// owner's decisions of 26 Oct 2026, rev 0.147 and 0.148): whose claims, the date the money left, the money account (a
// money chart account, in the base currency) it left, the bank's reference, what it pays of each claim, and notes.
type Reimbursement struct {
	ClaimantID uuid.UUID
	Date       time.Time
	AccountID  uuid.UUID
	Reference  string
	Claims     []Part
	Notes      string
}

type reimbursementPayload struct {
	ClaimantID uuid.UUID     `json:"claimant_id"`
	Date       string        `json:"date,omitempty"`
	AccountID  uuid.UUID     `json:"money_account_id"`
	Reference  string        `json:"reference,omitempty"`
	Claims     []payloadPart `json:"claims"`
	Notes      string        `json:"notes,omitempty"`
}

type payloadPart struct {
	ClaimID uuid.UUID   `json:"claim_id"`
	Amount  money.Money `json:"amount"`
}

// EncodeReimbursement is a reimbursement as a document version stores it.
func EncodeReimbursement(r Reimbursement) ([]byte, error) {
	switch {
	case len(r.Reference) > MaxReference:
		return nil, &Error{CodeTooLong, fmt.Sprintf("reference: %d characters; the most is %d", len(r.Reference), MaxReference), "Shorten it"}
	case len(r.Notes) > MaxNotes:
		return nil, &Error{CodeTooLong, fmt.Sprintf("notes: %d characters; the most is %d", len(r.Notes), MaxNotes), "Shorten them"}
	case len(r.Claims) > MaxClaims:
		return nil, &Error{CodeTooLong, fmt.Sprintf("claims: %d; one reimbursement pays back at most %d", len(r.Claims), MaxClaims),
			"Record it as more than one reimbursement"}
	}
	p := reimbursementPayload{ClaimantID: r.ClaimantID, Date: day(r.Date), AccountID: r.AccountID, Reference: r.Reference, Notes: r.Notes,
		Claims: []payloadPart{}}
	for _, c := range r.Claims {
		p.Claims = append(p.Claims, payloadPart{ClaimID: c.ClaimID, Amount: c.Amount})
	}
	return json.Marshal(p)
}

// ReadReimbursement reads a reimbursement version as it was saved.
func ReadReimbursement(doc storage.Document) (Reimbursement, error) {
	if doc.Type != storage.DocumentClaimReimbursement {
		return Reimbursement{}, &Error{CodeNotReimbursement, fmt.Sprintf("document %s is a %s, not a reimbursement of expense claims", doc.ID, doc.Type),
			"Open it with its own module"}
	}
	var p reimbursementPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Reimbursement{}, &Error{CodeBadDraft, "the saved reimbursement cannot be read: " + err.Error(), "Record it again"}
	}
	r := Reimbursement{ClaimantID: p.ClaimantID, AccountID: p.AccountID, Reference: p.Reference, Notes: p.Notes}
	if p.Date != "" {
		d, err := time.Parse(time.DateOnly, p.Date)
		if err != nil {
			return Reimbursement{}, &Error{CodeBadDraft, fmt.Sprintf("the saved reimbursement cannot be read: the date %q", p.Date), "Record it again"}
		}
		r.Date = d
	}
	for _, c := range p.Claims {
		r.Claims = append(r.Claims, Part(c))
	}
	return r, nil
}

// Balance is where one posted claim stands with its claimant: what the company owes for it on 2150, what
// reimbursements settled of it, net of their voids, and what is still open, all in the base currency.
type Balance struct {
	Owed, Settled, Open money.Money
}

// payableAccount is the chart's account with the expense-claims-payable role (2150), or uuid.Nil.
func payableAccount(ctx context.Context, u storage.UnitOfWork) (uuid.UUID, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	for _, a := range accounts {
		if string(a.Role) == ledger.RoleExpenseClaimsPayable {
			return a.ID, nil
		}
	}
	return uuid.Nil, nil
}

// Balances are where posted claims stand (design D4) as at asAt: what each owes on 2150 as its entry credited it, less
// what reimbursements dated by then settled of it, net of their voids by then. A claim that is not posted, or is dated
// after asAt, is not in it; a zero asAt counts everything recorded, whatever its date.
func Balances(ctx context.Context, u storage.UnitOfWork, docs []storage.Document, base money.Currency, asAt time.Time) (map[uuid.UUID]Balance, error) {
	payable, err := payableAccount(ctx, u)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]Balance{}
	for _, d := range docs {
		if d.Type != storage.DocumentExpenseClaim || d.Status != storage.DocumentPosted || !asAt.IsZero() && d.Date > day(asAt) {
			continue
		}
		e, err := u.Ledger().ByIdentity(ctx, ledger.ExpenseClaimIdentity(d.ID, d.Version, ledger.RuleExpenseClaim))
		if err != nil {
			return nil, err
		}
		b := Balance{Owed: money.Zero(base), Settled: money.Zero(base)}
		for _, l := range e.Lines {
			if l.AccountID == payable && payable != uuid.Nil {
				x, err := money.ParseStored(string(l.BaseAmount), base)
				if err != nil {
					return nil, err
				}
				b.Owed, _ = b.Owed.Add(x.Neg())
			}
		}
		allocs, err := u.Allocations().OfDocument(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		for _, a := range allocs {
			if !asAt.IsZero() && a.On.After(asAt) {
				continue
			}
			x, err := money.ParseStored(string(a.Amount), base)
			if err != nil {
				return nil, err
			}
			b.Settled, _ = b.Settled.Add(x)
		}
		b.Open, _ = b.Owed.Sub(b.Settled)
		out[d.ID] = b
	}
	return out, nil
}

// PreparedReimbursement is a reimbursement version as the rule reads it.
type PreparedReimbursement struct {
	Draft Reimbursement
	Rule  ledger.ClaimReimbursement
}

// PrepareReimbursement reads one version of a reimbursement for the rule: each claim it settles must be a posted claim of
// its claimant, named once, with at least what it settles still open on it. Problems the rule would refuse come back as
// ledger.Problems with the engine's own, from PostClaimReimbursement; the rule's input is filled either way, for a replay.
func PrepareReimbursement(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency) (PreparedReimbursement, ledger.Problems, error) {
	r, err := ReadReimbursement(doc)
	if err != nil {
		return PreparedReimbursement{}, nil, err
	}
	out := PreparedReimbursement{Draft: r, Rule: ledger.ClaimReimbursement{ID: doc.ID, Version: doc.Version, Date: r.Date, Account: r.AccountID,
		Memo: strings.TrimSpace("Expense claims reimbursed " + r.Reference)}}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	if r.ClaimantID == uuid.Nil {
		add(CodeClaimant, "claimant_id", "the reimbursement names no claimant", "Choose whose claims it pays back")
	}
	var docs []storage.Document
	seen := map[uuid.UUID]bool{}
	for i, c := range r.Claims {
		f := fmt.Sprintf("claims[%d].claim_id", i+1)
		out.Rule.Claims = append(out.Rule.Claims, ledger.PaidClaim{ClaimID: c.ClaimID, Amount: c.Amount})
		d, err := u.Documents().Get(ctx, c.ClaimID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(CodeNotOwed, f, "the claim does not exist in this organisation", "Choose one of the claimant's approved claims")
			continue
		case err != nil:
			return PreparedReimbursement{}, nil, err
		case seen[c.ClaimID]:
			add(CodeNotOwed, f, fmt.Sprintf("claim %s is named twice", d.Number), "Name each claim once, with all it pays back of it")
			continue
		case d.Type != storage.DocumentExpenseClaim || d.Status != storage.DocumentPosted:
			add(CodeNotOwed, f, fmt.Sprintf("%s is not an approved expense claim", cmpOr(d.Number, "the document")), "Choose one of the claimant's approved claims")
			continue
		}
		seen[c.ClaimID] = true
		draft, err := Read(d, base)
		if err != nil {
			return PreparedReimbursement{}, nil, err
		}
		if draft.ClaimantID != r.ClaimantID {
			add(CodeNotOwed, f, fmt.Sprintf("claim %s is another person's", d.Number), "Pay each person back on their own reimbursement")
			continue
		}
		docs = append(docs, d)
	}
	balances, err := Balances(ctx, u, docs, base, time.Time{})
	if err != nil {
		return PreparedReimbursement{}, nil, err
	}
	for i, c := range r.Claims {
		b, ok := balances[c.ClaimID]
		if !ok || c.Amount.Currency() != base {
			continue
		}
		if over, err := c.Amount.Cmp(b.Open); err == nil && over > 0 {
			add(CodeOverpaid, fmt.Sprintf("claims[%d].amount", i+1), fmt.Sprintf("it pays back %s %s of a claim with %s %s still owed on it", c.Amount, base, b.Open, base),
				"Pay back at most what is still owed on the claim")
		}
	}
	return out, probs, nil
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
