package director

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payee"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a repayment (FR-EQ-05; design D10).
const (
	CodeNotRepayment = "FOUNDER-31" // the document is not a repayment to a director or shareholder
	CodeNoRepayTo    = "FOUNDER-32" // the person has no approved SGD account at a Singapore bank to repay into
	CodeNotRepaid    = "FOUNDER-33" // a settlement that is not an approved repayment of the person, or more than is open on it
	CodeNotPosted    = "FOUNDER-34" // only a posted repayment is voided, on or after its date
)

// MaxRepaymentReference is the longest bank reference a repayment keeps: the payment list's.
const MaxRepaymentReference = 140

// RepaymentPart is what a repayment pays of one settlement, in the base currency.
type RepaymentPart struct {
	SettlementID uuid.UUID
	Amount       money.Money
}

// Repayment is what a payment batch's release paid a director or shareholder (FR-EQ-05, FR-BNK-12; design D10): the
// person, the day the bank pays it, the money account it leaves, the batch's reference, and what it pays of each
// settlement approved to be repaid. Only a release records one (FY254).
type Repayment struct {
	ContactID   uuid.UUID
	Date        time.Time
	AccountID   uuid.UUID
	Reference   string
	Settlements []RepaymentPart
	Notes       string
}

type repaymentPayload struct {
	ContactID   uuid.UUID            `json:"contact_id"`
	Date        string               `json:"date"`
	AccountID   uuid.UUID            `json:"money_account_id"`
	Reference   string               `json:"reference,omitempty"`
	Settlements []repaymentPartStore `json:"settlements"`
	Notes       string               `json:"notes,omitempty"`
}

type repaymentPartStore struct {
	SettlementID uuid.UUID   `json:"settlement_id"`
	Amount       money.Money `json:"amount"`
}

// EncodeRepayment is a repayment as a document version stores it. The database reads money_account_id and date to link
// it to its release (FY240).
func EncodeRepayment(r Repayment) ([]byte, error) {
	if len(r.Reference) > MaxRepaymentReference {
		return nil, &Error{CodeSettlement, fmt.Sprintf("reference: %d characters; the most is %d", len(r.Reference), MaxRepaymentReference), "Shorten it"}
	}
	p := repaymentPayload{ContactID: r.ContactID, Date: dayOf(r.Date), AccountID: r.AccountID, Reference: r.Reference, Notes: r.Notes,
		Settlements: []repaymentPartStore{}}
	for _, x := range r.Settlements {
		p.Settlements = append(p.Settlements, repaymentPartStore{SettlementID: x.SettlementID, Amount: x.Amount})
	}
	return json.Marshal(p)
}

// ReadRepayment reads a repayment's version as it was saved.
func ReadRepayment(doc storage.Document) (Repayment, error) {
	if doc.Type != storage.DocumentDirectorRepayment {
		return Repayment{}, &Error{CodeNotRepayment, fmt.Sprintf("document %s is a %s, not a repayment to a director", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p repaymentPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Repayment{}, &Error{CodeNotRepayment, "the saved repayment cannot be read: " + err.Error(), "Release the batch's payment again"}
	}
	date, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return Repayment{}, &Error{CodeNotRepayment, fmt.Sprintf("the saved repayment's date %q cannot be read", p.Date), "Release the batch's payment again"}
	}
	r := Repayment{ContactID: p.ContactID, Date: date, AccountID: p.AccountID, Reference: p.Reference, Notes: p.Notes}
	for _, x := range p.Settlements {
		r.Settlements = append(r.Settlements, RepaymentPart{SettlementID: x.SettlementID, Amount: x.Amount})
	}
	return r, nil
}

// RepayTo is where a director or shareholder is repaid (design D10): the linked member's own account (FR-AP-22), else
// the contact's, as a vendor's (FR-CT-03); with the approved SGD account at a Singapore bank in force, and its bank,
// both nil when there is none.
func RepayTo(ctx context.Context, u storage.UnitOfWork, contact uuid.UUID) (payee.Payee, *storage.PayeeBankAccount, *payee.Stored, error) {
	c, err := u.Contacts().Get(ctx, contact)
	if err != nil {
		return payee.Payee{}, nil, nil, err
	}
	who := payee.Vendor(contact)
	if c.MemberUserID != nil {
		who = payee.Person(*c.MemberUserID)
	}
	acc, err := payee.InForceFor(ctx, u, who, "SGD")
	if err != nil || acc == nil {
		return who, nil, nil, err
	}
	change, err := u.Documents().Get(ctx, acc.ChangeID)
	if err != nil {
		return who, nil, nil, err
	}
	bank, err := payee.Read(change)
	if err != nil {
		return who, nil, nil, err
	}
	if bank.Kind != payee.Singapore {
		return who, nil, nil, nil
	}
	return who, acc, &bank, nil
}

// noRepayTo is the problem of a person with no account to repay into.
func noRepayTo(name string) ledger.Problem {
	return ledger.Problem{Code: CodeNoRepayTo, Field: "route", Message: name + " has no approved SGD account at a Singapore bank to repay into",
		Fix: "Have their bank details entered and approved first: the member's own in My bank details, or the contact's in Contacts"}
}

// Repaid is what releases have repaid of each settlement named, net of voids: settlements nothing repaid are not in it.
func Repaid(ctx context.Context, u storage.UnitOfWork, base money.Currency, ids []uuid.UUID) (map[uuid.UUID]money.Money, error) {
	settled, err := u.Allocations().Settled(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]money.Money{}
	for id, d := range settled {
		m, err := money.ParseStored(string(d), base)
		if err != nil {
			return nil, err
		}
		out[id] = m
	}
	return out, nil
}

// Payable is a settlement approved to be repaid, as a batch pays it: its document, what it is for, and what is open on
// it (its amount less what releases repaid).
type Payable struct {
	Doc        storage.Document
	Settlement Settlement
	Open       money.Money
}

// PayableOf reads a settlement a batch names: CodeNotRepaid when it is not a decided repayment.
func PayableOf(ctx context.Context, u storage.UnitOfWork, base money.Currency, id uuid.UUID) (Payable, error) {
	doc, err := u.Documents().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) || err == nil && (doc.Type != storage.DocumentDirectorSettlement || doc.Status != storage.DocumentPosted) {
		return Payable{}, &Error{CodeNotRepaid, fmt.Sprintf("%s is not an approved settlement to repay", id), "Choose a settlement approved to be repaid"}
	}
	if err != nil {
		return Payable{}, err
	}
	s, err := ReadSettlement(doc)
	if err != nil {
		return Payable{}, err
	}
	if s.Route != RouteRepay {
		return Payable{}, &Error{CodeNotRepaid, fmt.Sprintf("%s is a settlement to %s, not to repay", doc.Number, s.Route), "Choose a settlement approved to be repaid"}
	}
	repaid, err := Repaid(ctx, u, base, []uuid.UUID{id})
	if err != nil {
		return Payable{}, err
	}
	open := s.Amount
	if r, ok := repaid[id]; ok {
		if open, err = open.Sub(r); err != nil {
			return Payable{}, err
		}
	}
	return Payable{Doc: doc, Settlement: s, Open: open}, nil
}

// PayableSettlements are the settlements approved to be repaid with something open, oldest first: what a new batch
// suggests (FR-AP-13).
func PayableSettlements(ctx context.Context, u storage.UnitOfWork, base money.Currency) ([]Payable, error) {
	var out []Payable
	var afterDate string
	var afterID *uuid.UUID
	for {
		docs, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentDirectorSettlement, Status: storage.DocumentPosted,
			AfterDate: afterDate, AfterID: afterID, Limit: 500})
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			afterDate, afterID = doc.Date, &doc.ID
			s, err := ReadSettlement(doc)
			if err != nil {
				return nil, err
			}
			if s.Route != RouteRepay {
				continue
			}
			p, err := PayableOf(ctx, u, base, doc.ID)
			if err != nil {
				return nil, err
			}
			if p.Open.Sign() > 0 {
				out = append(out, p)
			}
		}
		if len(docs) < 500 {
			return out, nil
		}
	}
}

// PrepareRepayment is a repayment's version as the rule reads it, with each settlement's account; a settlement that is
// not an approved repayment of the person is CodeNotRepaid.
func PrepareRepayment(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (ledger.DirectorRepayment, error) {
	r, err := ReadRepayment(doc)
	if err != nil {
		return ledger.DirectorRepayment{}, err
	}
	out := ledger.DirectorRepayment{ID: doc.ID, Version: doc.Version, Date: r.Date, Memo: r.Reference, Party: r.ContactID, Account: r.AccountID}
	for _, x := range r.Settlements {
		sd, err := u.Documents().Get(ctx, x.SettlementID)
		if errors.Is(err, ports.ErrNotFound) {
			return ledger.DirectorRepayment{}, &Error{CodeNotRepaid, fmt.Sprintf("settlement %s does not exist in this organisation", x.SettlementID), "Choose a settlement approved to be repaid"}
		}
		if err != nil {
			return ledger.DirectorRepayment{}, err
		}
		s, err := ReadSettlement(sd)
		if err != nil {
			return ledger.DirectorRepayment{}, err
		}
		if s.Route != RouteRepay || s.ContactID != r.ContactID {
			return ledger.DirectorRepayment{}, &Error{CodeNotRepaid, fmt.Sprintf("%s is not a repayment to this person", sd.Number), "Choose the person's settlements approved to be repaid"}
		}
		out.Settlements = append(out.Settlements, ledger.RepaidSettlement{SettlementID: x.SettlementID, Account: s.AccountID, Amount: x.Amount})
	}
	return out, nil
}
