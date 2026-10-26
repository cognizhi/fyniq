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
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a cash advance's refusals (ENG-23). CLAIM-20 to CLAIM-27 are the expense policy's.
const (
	CodeNotAdvance     = "CLAIM-28" // the document is not a cash advance
	CodeAdvancePerson  = "CLAIM-29" // an advance to no one, or to someone who is not an active member
	CodeAdvanceRefund  = "CLAIM-30" // a refund of no posted advance of the person, or of more than is open on it
	CodeAdvanceSettled = "CLAIM-31" // a void of an advance that claims or refunds settled, or of one not posted, or dated before it
	CodeClaimAdvance   = "CLAIM-32" // a claim settling what is not a posted advance to its claimant, or one with nothing open
)

// AdvancesSetting is the setting that holds the advances account's code (FR-AP-17): 1250 Staff & director advances.
const AdvancesSetting = "claims.advances_account"

// Advance is money advanced to a member before a trip, which their claims settle (FR-AP-17, UC-EXP-03); or, when Refunds
// names an advance, what is left of that advance coming back. Recorded by hand under the "Payment batches" row (the
// owner's decision of 26 Oct 2026, rev 0.148): to whom (a member's user ID), the date the money moved, the money account
// (a money chart account, in the base currency) it moved through, the amount in the base currency, the bank's
// reference, and notes. Personal marks personal spend on a company card, made from the card's line (FR-AP-18): the
// cardholder owes it and refunds it; no claim settles it.
type Advance struct {
	PersonID  uuid.UUID
	Refunds   *uuid.UUID
	Personal  bool
	Date      time.Time
	AccountID uuid.UUID
	Amount    money.Money
	Reference string
	Notes     string
}

type advancePayload struct {
	PersonID  uuid.UUID    `json:"person_id"`
	Refunds   *uuid.UUID   `json:"refunds,omitempty"`
	Personal  bool         `json:"personal,omitempty"`
	Date      string       `json:"date,omitempty"`
	AccountID uuid.UUID    `json:"money_account_id"`
	Amount    *money.Money `json:"amount,omitempty"`
	Reference string       `json:"reference,omitempty"`
	Notes     string       `json:"notes,omitempty"`
}

// EncodeAdvance is an advance or a refund as a document version stores it.
func EncodeAdvance(a Advance) ([]byte, error) {
	switch {
	case len(a.Reference) > MaxReference:
		return nil, &Error{CodeTooLong, fmt.Sprintf("reference: %d characters; the most is %d", len(a.Reference), MaxReference), "Shorten it"}
	case len(a.Notes) > MaxNotes:
		return nil, &Error{CodeTooLong, fmt.Sprintf("notes: %d characters; the most is %d", len(a.Notes), MaxNotes), "Shorten them"}
	}
	amount := a.Amount
	return json.Marshal(advancePayload{PersonID: a.PersonID, Refunds: a.Refunds, Personal: a.Personal, Date: day(a.Date), AccountID: a.AccountID, Amount: &amount,
		Reference: a.Reference, Notes: a.Notes})
}

// IsAdvance says whether a document is a cash advance or its refund.
func IsAdvance(doc storage.Document) bool { return doc.Type == storage.DocumentCashAdvance }

// ReadAdvance reads an advance or a refund version as it was saved.
func ReadAdvance(doc storage.Document) (Advance, error) {
	if !IsAdvance(doc) {
		return Advance{}, &Error{CodeNotAdvance, fmt.Sprintf("document %s is a %s, not a cash advance", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p advancePayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil || p.Amount == nil {
		what := "it has no amount"
		if err != nil {
			what = err.Error()
		}
		return Advance{}, &Error{CodeBadDraft, "the saved advance cannot be read: " + what, "Record it again"}
	}
	a := Advance{PersonID: p.PersonID, Refunds: p.Refunds, Personal: p.Personal, AccountID: p.AccountID, Amount: *p.Amount, Reference: p.Reference, Notes: p.Notes}
	if p.Date != "" {
		d, err := time.Parse(time.DateOnly, p.Date)
		if err != nil {
			return Advance{}, &Error{CodeBadDraft, fmt.Sprintf("the saved advance cannot be read: the date %q", p.Date), "Record it again"}
		}
		a.Date = d
	}
	return a, nil
}

// AdvanceBalance is where one posted advance stands with its person, in the base currency: what was advanced, what
// their claims settled of it, what they refunded, each net of voids, and what is still open.
type AdvanceBalance struct {
	Advanced, Settled, Refunded, Open money.Money
}

// AdvanceBalances are where posted advances stand as at asAt (design D8): each advance's amount, less what claims and
// refunds dated by then settled of it, net of their voids by then. A refund, an advance that is not posted, or one
// dated after asAt is not in it; a zero asAt counts everything recorded, whatever its date.
func AdvanceBalances(ctx context.Context, u storage.UnitOfWork, docs []storage.Document, base money.Currency, asAt time.Time) (map[uuid.UUID]AdvanceBalance, error) {
	out := map[uuid.UUID]AdvanceBalance{}
	kinds := map[uuid.UUID]storage.DocumentType{} // each settling document's type, read once
	for _, d := range docs {
		if !IsAdvance(d) || d.Status != storage.DocumentPosted || !asAt.IsZero() && d.Date > day(asAt) {
			continue
		}
		a, err := ReadAdvance(d)
		if err != nil {
			return nil, err
		}
		if a.Refunds != nil {
			continue
		}
		b := AdvanceBalance{Advanced: a.Amount, Settled: money.Zero(base), Refunded: money.Zero(base)}
		allocs, err := u.Allocations().OfDocument(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		for _, x := range allocs {
			if !asAt.IsZero() && x.On.After(asAt) {
				continue
			}
			amount, err := money.ParseStored(string(x.Amount), base)
			if err != nil {
				return nil, err
			}
			kind, ok := kinds[x.SourceID]
			if !ok {
				src, err := u.Documents().Get(ctx, x.SourceID)
				if err != nil {
					return nil, err
				}
				kind, kinds[x.SourceID] = src.Type, src.Type
			}
			if kind == storage.DocumentCashAdvance {
				b.Refunded, _ = b.Refunded.Add(amount)
			} else {
				b.Settled, _ = b.Settled.Add(amount)
			}
		}
		b.Open, _ = b.Advanced.Sub(b.Settled)
		b.Open, _ = b.Open.Sub(b.Refunded)
		out[d.ID] = b
	}
	return out, nil
}

// AdvancesAccount is the advances account in force on a day: the account of the chart whose code the setting
// claims.advances_account holds, or uuid.Nil when the chart has none.
func AdvancesAccount(ctx context.Context, u storage.UnitOfWork, on time.Time) (uuid.UUID, error) {
	def, _ := settings.Lookup(AdvancesSetting)
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return uuid.Nil, err
	}
	acc, err := u.Accounts().ByCode(ctx, settings.On(def, changes, on).Value)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return uuid.Nil, nil
	case err != nil:
		return uuid.Nil, err
	}
	return acc.ID, nil
}

// OpenAdvance reads the advance a claim of claimant settles, for the claim (FR-AP-17): it must be a posted advance to
// the claimant, not a refund. Its balance comes back as it stands with every claim and refund recorded; a problem when
// it is not one the claim can settle.
func OpenAdvance(ctx context.Context, u storage.UnitOfWork, id, claimant uuid.UUID, base money.Currency) (AdvanceBalance, *ledger.Problem, error) {
	refuse := func(msg string) (AdvanceBalance, *ledger.Problem, error) {
		return AdvanceBalance{}, &ledger.Problem{Code: CodeClaimAdvance, Field: "advance_id", Message: msg,
			Fix: "Choose one of your open advances, or settle none"}, nil
	}
	doc, err := u.Documents().Get(ctx, id)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return refuse("the advance does not exist in this organisation")
	case err != nil:
		return AdvanceBalance{}, nil, err
	case !IsAdvance(doc) || doc.Status != storage.DocumentPosted:
		return refuse(fmt.Sprintf("%s is not a posted cash advance", cmpOr(doc.Number, "the document")))
	}
	a, err := ReadAdvance(doc)
	if err != nil {
		return AdvanceBalance{}, nil, err
	}
	switch {
	case a.Refunds != nil:
		return refuse(fmt.Sprintf("%s is the refund of an advance, not an advance", doc.Number))
	case a.Personal:
		return refuse(fmt.Sprintf("%s is personal spend on a company card: it is refunded, not settled by a claim", doc.Number))
	case a.PersonID != claimant:
		return refuse(fmt.Sprintf("advance %s was paid to another person", doc.Number))
	}
	b, err := AdvanceBalances(ctx, u, []storage.Document{doc}, base, time.Time{})
	if err != nil {
		return AdvanceBalance{}, nil, err
	}
	return b[doc.ID], nil, nil
}

// PreparedAdvance is an advance or refund version as the rule reads it.
type PreparedAdvance struct {
	Draft Advance
	Rule  ledger.CashAdvance
}

// PrepareAdvance reads one version of an advance or a refund for the rule: an advance is paid to an active member; a
// refund names a posted advance of its person with at least its amount still open; either posts against the advances
// account in force on its date. Problems the rule would refuse come back as ledger.Problems with the engine's own, from
// PostCashAdvance; the rule's input is filled either way, for a replay.
func PrepareAdvance(ctx context.Context, u storage.UnitOfWork, doc storage.Document, base money.Currency) (PreparedAdvance, ledger.Problems, error) {
	a, err := ReadAdvance(doc)
	if err != nil {
		return PreparedAdvance{}, nil, err
	}
	memo := "Cash advance"
	switch {
	case a.Refunds != nil:
		memo = "Cash advance refunded"
	case a.Personal:
		memo = "Personal spend on a company card"
	}
	out := PreparedAdvance{Draft: a, Rule: ledger.CashAdvance{ID: doc.ID, Version: doc.Version, Date: a.Date, Refund: a.Refunds != nil,
		Account: a.AccountID, Amount: a.Amount, Memo: strings.TrimSpace(memo + " " + a.Reference)}}
	if !a.Date.IsZero() {
		if out.Rule.Advances, err = AdvancesAccount(ctx, u, a.Date); err != nil {
			return PreparedAdvance{}, nil, err
		}
	}
	var probs ledger.Problems
	add := func(code, field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	if a.Refunds != nil {
		err := refundable(ctx, u, a, base, add)
		return out, probs, err
	}
	if a.PersonID == uuid.Nil {
		add(CodeAdvancePerson, "person_id", "the advance names no one", "Choose the member it is paid to")
		return out, probs, nil
	}
	m, err := u.Members().Membership(ctx, a.PersonID)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		add(CodeAdvancePerson, "person_id", "the person is not a member of this organisation", "Advance money to a member, whose claims settle it")
	case err != nil:
		return PreparedAdvance{}, nil, err
	case m.Status != "active":
		add(CodeAdvancePerson, "person_id", "the person is no longer a member of this organisation", "Advance money to an active member")
	}
	return out, probs, nil
}

// refundable checks a refund against the advance it names: a posted advance, not a refund, paid to the refund's person,
// with at least the refund still open on it.
func refundable(ctx context.Context, u storage.UnitOfWork, a Advance, base money.Currency, add func(code, field, msg, fix string)) error {
	fix := "Choose the advance being refunded: a posted advance to the person"
	doc, err := u.Documents().Get(ctx, *a.Refunds)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		add(CodeAdvanceRefund, "refunds", "the advance does not exist in this organisation", fix)
		return nil
	case err != nil:
		return err
	case !IsAdvance(doc) || doc.Status != storage.DocumentPosted:
		add(CodeAdvanceRefund, "refunds", fmt.Sprintf("%s is not a posted cash advance", cmpOr(doc.Number, "the document")), fix)
		return nil
	}
	adv, err := ReadAdvance(doc)
	if err != nil {
		return err
	}
	switch {
	case adv.Refunds != nil:
		add(CodeAdvanceRefund, "refunds", fmt.Sprintf("%s is itself a refund", doc.Number), fix)
		return nil
	case adv.PersonID != a.PersonID:
		add(CodeAdvanceRefund, "person_id", fmt.Sprintf("advance %s was paid to another person", doc.Number), "Name the person the advance was paid to")
		return nil
	}
	b, err := AdvanceBalances(ctx, u, []storage.Document{doc}, base, time.Time{})
	if err != nil {
		return err
	}
	open := b[doc.ID].Open
	if a.Amount.Currency() != base {
		return nil // the rule answers it
	}
	if over, err := a.Amount.Cmp(open); err == nil && over > 0 {
		add(CodeAdvanceRefund, "amount", fmt.Sprintf("it refunds %s %s of an advance with %s %s still open on it", a.Amount, base, open, base),
			"Refund at most what is still open on the advance")
	}
	return nil
}
