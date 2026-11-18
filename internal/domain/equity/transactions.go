package equity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Equity transactions (FR-EQ-17; p1-equity-register design D3 to D5, rev 0.205): a cash issue to one subscriber, a
// transfer between two holders, a conversion of one holder's shares between classes, and the reversal of one of these.
// Each is an equity_transaction document: drafted by the Accountant with its evidence, approved by another person who
// holds A on the cap table and never by a holder it benefits (FM-1, FM-9), numbered EQ-, then made effective with
// ACRA's date, which writes its holding movements (and a cash issue its share issue and entry) in one unit of work.
// A wrong transaction is corrected by a reversal, never edited. Issues for non-cash consideration keep founders' money's
// route (FR-EQ-06); SAFE and note conversions and option exercises come with their slices.

// The kinds of equity transaction built so far.
const (
	KindIssueCash       = "issue_cash"
	KindTransfer        = "transfer"
	KindClassConversion = "class_conversion"
	KindReversal        = "reversal"
	// KindInstrumentConversion is a SAFE's or note's conversion into shares at a priced round (FR-EQ-18, design D6),
	// drafted from its round: an issue to the instrument's holder, Dr 2700 · Cr the class with what it converts.
	KindInstrumentConversion = "instrument_conversion"
	// KindExercise is an exercise of a recorded grant's options (FR-EQ-19, design D9): an issue to the grant's holder of
	// its class at its exercise price, from share application monies like a cash issue, at most what is vested and not
	// exercised on the day it is effective.
	KindExercise = "exercise"
)

// TransactionKinds are the kinds a transaction may be.
var TransactionKinds = []string{KindIssueCash, KindTransfer, KindClassConversion, KindReversal, KindInstrumentConversion, KindExercise}

// The codes of equity transactions (ENG-23).
const (
	CodeTransaction = "EQUITY-07" // a transaction that does not fit: its kind, holders, classes, shares, price or references
	CodeEffective   = "EQUITY-08" // made effective on a day it cannot be, twice, before it is paid, or leaving a holder short
	CodeNoMonies    = "EQUITY-09" // a cash issue in an organisation whose chart has no share application monies account
)

// MaxReason is the longest reason a reversal gives.
const MaxReason = 500

// Transaction is an equity transaction as drafted. Which fields it uses depends on its kind:
//   - issue_cash: the subscriber (HolderID), the class, the shares, the price per share in the base currency and the
//     resolutions; its amount is shares × price under RND-LINE;
//   - transfer: the seller, the buyer, the class, the shares and, for information, the price agreed (share duty is the
//     parties' matter);
//   - class_conversion: the holder, the class converted from and to, the shares out and in, the resolutions, and the
//     carrying amount moved between the classes' accounts when they differ [accountant to confirm the basis];
//   - reversal: the transaction it reverses and why;
//   - instrument_conversion: the instrument (its holder and class), the round it converts at, the shares and the
//     conversion price; its amount is shares × price under RND-LINE, which RND-SHARE kept within the instrument's amount;
//   - exercise: the grant (its holder, class and exercise price), the shares and the resolutions; its amount is shares
//     × the exercise price under RND-LINE.
type Transaction struct {
	Kind           string
	HolderID       uuid.UUID
	SellerID       uuid.UUID
	BuyerID        uuid.UUID
	ClassAccountID uuid.UUID
	ClassID        *uuid.UUID
	ToAccountID    uuid.UUID
	ToClassID      *uuid.UUID
	Shares         string
	ToShares       string
	Price          string
	Amount         string
	ResolutionRefs string
	ReversesID     uuid.UUID
	Reason         string
	Note           string
	InstrumentID   uuid.UUID  // an instrument conversion's SAFE or note
	RoundID        uuid.UUID  // and the cash issue, the priced round, it converts at
	GrantID        uuid.UUID  // an exercise's grant of options
	Review         string     // what the accountant completes in a conversion no enabled template covers (⚠), as drafted
	Date           time.Time  // the day it was drafted
	ApprovedOn     *time.Time // the day it was approved; it is never effective earlier
}

type transactionPayload struct {
	Date           string `json:"date"`
	Kind           string `json:"kind"`
	HolderID       string `json:"holder_id,omitempty"`
	SellerID       string `json:"seller_id,omitempty"`
	BuyerID        string `json:"buyer_id,omitempty"`
	ClassAccountID string `json:"class_account_id,omitempty"`
	ClassID        string `json:"class_id,omitempty"`
	ToAccountID    string `json:"to_class_account_id,omitempty"`
	ToClassID      string `json:"to_class_id,omitempty"`
	Shares         string `json:"shares,omitempty"`
	ToShares       string `json:"to_shares,omitempty"`
	Price          string `json:"price,omitempty"`
	Amount         string `json:"amount,omitempty"`
	ResolutionRefs string `json:"resolution_refs,omitempty"`
	ReversesID     string `json:"reverses_id,omitempty"`
	InstrumentID   string `json:"instrument_id,omitempty"`
	RoundID        string `json:"round_id,omitempty"`
	GrantID        string `json:"grant_id,omitempty"`
	Review         string `json:"review,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Note           string `json:"note,omitempty"`
	ApprovedOn     string `json:"approved_on,omitempty"`
}

func idText(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func optText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// EncodeTransaction is a transaction's payload as saved.
func EncodeTransaction(t Transaction) ([]byte, error) {
	p := transactionPayload{Date: t.Date.Format(time.DateOnly), Kind: t.Kind, HolderID: idText(t.HolderID), SellerID: idText(t.SellerID),
		BuyerID: idText(t.BuyerID), ClassAccountID: idText(t.ClassAccountID), ClassID: optText(t.ClassID), ToAccountID: idText(t.ToAccountID),
		ToClassID: optText(t.ToClassID), Shares: t.Shares, ToShares: t.ToShares, Price: t.Price, Amount: t.Amount, ResolutionRefs: t.ResolutionRefs,
		ReversesID: idText(t.ReversesID), Reason: t.Reason, Note: t.Note, InstrumentID: idText(t.InstrumentID), RoundID: idText(t.RoundID),
		GrantID: idText(t.GrantID), Review: t.Review}
	if t.ApprovedOn != nil {
		p.ApprovedOn = t.ApprovedOn.Format(time.DateOnly)
	}
	return json.Marshal(p)
}

// ReadTransaction reads an equity transaction's version as it was saved.
func ReadTransaction(doc storage.Document) (Transaction, error) {
	if doc.Type != storage.DocumentEquityTransaction {
		return Transaction{}, &Error{CodeNotRecord, fmt.Sprintf("document %s is a %s, not an equity transaction", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p transactionPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return Transaction{}, &Error{CodeTransaction, "the saved equity transaction cannot be read: " + err.Error(), "Draft it again"}
	}
	id := func(s string) uuid.UUID {
		x, _ := uuid.Parse(s)
		return x
	}
	opt := func(s string) *uuid.UUID {
		if x, err := uuid.Parse(s); err == nil {
			return &x
		}
		return nil
	}
	t := Transaction{Kind: p.Kind, HolderID: id(p.HolderID), SellerID: id(p.SellerID), BuyerID: id(p.BuyerID), ClassAccountID: id(p.ClassAccountID),
		ClassID: opt(p.ClassID), ToAccountID: id(p.ToAccountID), ToClassID: opt(p.ToClassID), Shares: p.Shares, ToShares: p.ToShares, Price: p.Price,
		Amount: p.Amount, ResolutionRefs: p.ResolutionRefs, ReversesID: id(p.ReversesID), Reason: p.Reason, Note: p.Note,
		InstrumentID: id(p.InstrumentID), RoundID: id(p.RoundID), GrantID: id(p.GrantID), Review: p.Review}
	t.Date, _ = time.Parse(time.DateOnly, p.Date)
	if on, err := time.Parse(time.DateOnly, p.ApprovedOn); err == nil {
		t.ApprovedOn = &on
	}
	return t, nil
}

// Parties are the holders a transaction moves shares of, or for: those it benefits, who never approve it (FM-9).
func (t Transaction) Parties() []uuid.UUID {
	var out []uuid.UUID
	for _, id := range []uuid.UUID{t.HolderID, t.SellerID, t.BuyerID} {
		if id != uuid.Nil {
			out = append(out, id)
		}
	}
	return out
}

// CheckTransaction checks a transaction as drafted in u, every problem at once (ENG-22), and returns it as it is saved:
// a class left out is the account's first recorded class, and a cash issue's amount is shares × price under RND-LINE.
func CheckTransaction(ctx context.Context, u storage.UnitOfWork, t Transaction, s ledger.Settings) (Transaction, error) {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodeTransaction, Field: field, Message: msg, Fix: fix})
	}
	contact := func(field string, id uuid.UUID, ok func(storage.Contact) bool, what, fix string) {
		if id == uuid.Nil {
			add(field, "names no contact", fix)
			return
		}
		c, err := u.Contacts().Get(ctx, id)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			add(field, "the contact does not exist in this organisation", fix)
		case err != nil:
			probs = append(probs, ledger.Problem{Code: CodeTransaction, Field: field, Message: err.Error(), Fix: "Try again"})
		case !ok(c):
			add(field, c.LegalName+" is not "+what, fix)
		}
	}
	holder := func(c storage.Contact) bool { return c.Director || c.Shareholder || c.Investor }
	subscriber := func(c storage.Contact) bool { return c.Shareholder || c.Investor }
	class := func(accountField, classField string, account uuid.UUID, id *uuid.UUID) *uuid.UUID {
		if a, ok := s.Accounts[account]; !ok || !a.ShareCapital || a.Group || !a.Active {
			add(accountField, "the class is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
			return id
		}
		if id == nil {
			def, err := DefaultClass(ctx, u, account)
			if err != nil {
				probs = append(probs, ledger.Problem{Code: CodeTransaction, Field: classField, Message: err.Error(), Fix: "Try again"})
			}
			return def
		}
		c, err := Class(ctx, u, *id)
		if err != nil || c.Class.AccountID != account {
			add(classField, "is not a recorded class of the account", "Choose a recorded class of that account, or leave it out")
		}
		return id
	}
	shares := func(field, v string) {
		if !wholeShares.MatchString(v) {
			add(field, fmt.Sprintf("%q is not a whole number of shares above zero", v), "Enter the number of shares, such as 10000")
		}
	}
	refs := func(required bool) {
		switch n := utf8.RuneCountInString(strings.TrimSpace(t.ResolutionRefs)); {
		case n == 0 && required:
			add("resolution_refs", "the transaction names no resolution", "Name the shareholders' approval and the board resolution")
		case n > MaxResolutionRefs:
			add("resolution_refs", fmt.Sprintf("the references have %d characters; the most is %d", n, MaxResolutionRefs), "Shorten them")
		}
	}
	if n := utf8.RuneCountInString(t.Note); n > MaxNote {
		add("note", fmt.Sprintf("the note has %d characters; the most is %d", n, MaxNote), "Shorten it")
	}
	unused := func(field string, set bool) {
		if set {
			add(field, "a "+strings.ReplaceAll(t.Kind, "_", " ")+" has no "+strings.ReplaceAll(strings.TrimSuffix(field, "_id"), "_", " "), "Leave it out")
		}
	}
	switch t.Kind {
	case KindIssueCash:
		contact("holder_id", t.HolderID, subscriber, "marked shareholder or investor", "Choose the subscriber, a contact marked shareholder or investor")
		t.ClassID = class("class_account_id", "class_id", t.ClassAccountID, t.ClassID)
		shares("shares", t.Shares)
		price, err := money.ParseUnitPrice(t.Price, s.Base, "price")
		if err != nil || price.Sign() <= 0 {
			add("price", fmt.Sprintf("%q is not a price per share above zero in %s", t.Price, s.Base), "Enter the price per share, such as 2.40")
		} else if len(probs) == 0 {
			q, err := money.ParseQuantity(t.Shares, "shares")
			if err != nil {
				return t, err
			}
			amount, err := rounding.Line(q, price)
			if err != nil {
				return t, err
			}
			t.Amount, t.Price = amount.String(), price.String()
		}
		refs(true)
		unused("seller_id", t.SellerID != uuid.Nil)
		unused("buyer_id", t.BuyerID != uuid.Nil)
		unused("to_class_account_id", t.ToAccountID != uuid.Nil)
		unused("reverses_id", t.ReversesID != uuid.Nil)
	case KindTransfer:
		contact("seller_id", t.SellerID, holder, "a holder", "Choose the holder the shares come from")
		contact("buyer_id", t.BuyerID, subscriber, "marked shareholder or investor", "Choose the holder the shares go to, a contact marked shareholder or investor")
		if t.SellerID != uuid.Nil && t.SellerID == t.BuyerID {
			add("buyer_id", "the buyer is the seller", "Choose another holder")
		}
		t.ClassID = class("class_account_id", "class_id", t.ClassAccountID, t.ClassID)
		shares("shares", t.Shares)
		if t.Price != "" {
			if price, err := money.ParseUnitPrice(t.Price, s.Base, "price"); err != nil || price.Sign() < 0 {
				add("price", fmt.Sprintf("%q is not a price per share in %s", t.Price, s.Base), "Enter the price agreed per share, such as 2.40, or leave it out")
			} else {
				t.Price = price.String()
			}
		}
		t.Amount = ""
		refs(false)
		unused("holder_id", t.HolderID != uuid.Nil)
		unused("to_class_account_id", t.ToAccountID != uuid.Nil)
		unused("reverses_id", t.ReversesID != uuid.Nil)
	case KindClassConversion:
		contact("holder_id", t.HolderID, holder, "a holder", "Choose the holder whose shares convert")
		t.ClassID = class("class_account_id", "class_id", t.ClassAccountID, t.ClassID)
		t.ToClassID = class("to_class_account_id", "to_class_id", t.ToAccountID, t.ToClassID)
		if t.ClassAccountID == t.ToAccountID && (t.ClassID == nil && t.ToClassID == nil || t.ClassID != nil && t.ToClassID != nil && *t.ClassID == *t.ToClassID) {
			add("to_class_id", "the shares convert into the class they are in", "Choose the class they convert into")
		}
		shares("shares", t.Shares)
		shares("to_shares", t.ToShares)
		switch {
		case t.ClassAccountID != t.ToAccountID:
			if m, err := money.ParseField(t.Amount, s.Base, "amount"); err != nil || m.Sign() <= 0 {
				add("amount", "the classes' accounts differ, so the conversion moves their carrying amount", "Enter the carrying amount of the shares converted, in "+s.Base.Code)
			} else {
				t.Amount = m.String()
			}
		case t.Amount != "":
			add("amount", "the classes share one account, so the conversion moves no share capital", "Leave the amount out")
		}
		refs(true)
		unused("seller_id", t.SellerID != uuid.Nil)
		unused("buyer_id", t.BuyerID != uuid.Nil)
		unused("reverses_id", t.ReversesID != uuid.Nil)
	case KindReversal:
		switch n := utf8.RuneCountInString(strings.TrimSpace(t.Reason)); {
		case n == 0:
			add("reason", "the reversal gives no reason", "Say what was wrong, such as the ACRA extract shows another number of shares")
		case n > MaxReason:
			add("reason", fmt.Sprintf("the reason has %d characters; the most is %d", n, MaxReason), "Shorten it")
		}
		if err := checkReversed(ctx, u, t.ReversesID); err != nil {
			var p ledger.Problems
			if !errors.As(err, &p) {
				return t, err
			}
			probs = append(probs, p...)
		}
		for _, f := range []struct {
			field string
			set   bool
		}{{"holder_id", t.HolderID != uuid.Nil}, {"seller_id", t.SellerID != uuid.Nil}, {"buyer_id", t.BuyerID != uuid.Nil},
			{"class_account_id", t.ClassAccountID != uuid.Nil}, {"to_class_account_id", t.ToAccountID != uuid.Nil}, {"shares", t.Shares != ""},
			{"price", t.Price != ""}, {"amount", t.Amount != ""}} {
			unused(f.field, f.set)
		}
	case KindExercise:
		if err := checkExercise(ctx, u, &t, s, add); err != nil {
			return t, err
		}
		t.ClassID = class("class_account_id", "class_id", t.ClassAccountID, t.ClassID)
		shares("shares", t.Shares)
		refs(true)
		unused("seller_id", t.SellerID != uuid.Nil)
		unused("buyer_id", t.BuyerID != uuid.Nil)
		unused("to_class_account_id", t.ToAccountID != uuid.Nil)
		unused("reverses_id", t.ReversesID != uuid.Nil)
	case KindInstrumentConversion:
		if err := checkConversion(ctx, u, &t, s, add); err != nil {
			return t, err
		}
		refs(false)
		unused("seller_id", t.SellerID != uuid.Nil)
		unused("buyer_id", t.BuyerID != uuid.Nil)
		unused("to_class_account_id", t.ToAccountID != uuid.Nil)
		unused("reverses_id", t.ReversesID != uuid.Nil)
	default:
		add("kind", fmt.Sprintf("%q is not a kind of equity transaction Fyniq records", t.Kind), "Choose issue_cash, transfer, class_conversion, exercise or reversal")
	}
	if t.Kind != KindExercise {
		unused("grant_id", t.GrantID != uuid.Nil)
	}
	if t.Kind != KindInstrumentConversion {
		unused("instrument_id", t.InstrumentID != uuid.Nil)
		unused("round_id", t.RoundID != uuid.Nil)
	}
	if len(probs) > 0 {
		return t, probs
	}
	return t, nil
}

// checkReversed checks that a reversal names an effective transaction that is not a reversal and is not reversed yet.
func checkReversed(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) error {
	bad := func(msg, fix string) error {
		return ledger.Problems{{Code: CodeTransaction, Field: "reverses_id", Message: msg, Fix: fix}}
	}
	if id == uuid.Nil {
		return bad("the reversal names no transaction", "Choose the effective transaction it undoes")
	}
	doc, err := u.Documents().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) || err == nil && doc.Type != storage.DocumentEquityTransaction {
		return bad("is not an equity transaction of this organisation", "Choose the effective transaction it undoes")
	}
	if err != nil {
		return err
	}
	t, err := ReadTransaction(doc)
	if err != nil {
		return err
	}
	if t.Kind == KindReversal {
		return bad("a reversal is not reversed", "Record the transaction again instead")
	}
	moved, err := u.Holdings().OfTransaction(ctx, id)
	if err != nil {
		return err
	}
	if len(moved) == 0 {
		return bad("the transaction is not effective, so there is nothing to reverse", "Leave a draft alone, or let its approval lapse")
	}
	all, err := u.Holdings().List(ctx)
	if err != nil {
		return err
	}
	for _, m := range all {
		if m.ReversesID != nil && slices.ContainsFunc(moved, func(x storage.HoldingMovement) bool { return x.ID == *m.ReversesID }) {
			return bad("the transaction is reversed already", "Nothing to do: its reversal is in the register")
		}
	}
	return nil
}

// DraftTransactionIn saves a checked transaction to approve in u: a draft, to attach its evidence to.
func DraftTransactionIn(ctx context.Context, u storage.UnitOfWork, t Transaction, s ledger.Settings) (storage.Document, Transaction, error) {
	t, err := CheckTransaction(ctx, u, t, s)
	if err != nil {
		return storage.Document{}, t, err
	}
	if t.Kind == KindInstrumentConversion { // an instrument converts once (FR-EQ-18)
		taken, err := converted(ctx, u)
		if err != nil {
			return storage.Document{}, t, err
		}
		if taken[t.InstrumentID] {
			return storage.Document{}, t, ledger.Problems{{Code: CodeTransaction, Field: "instrument_id", Message: "a conversion of the instrument stands already",
				Fix: "Change that conversion, or reverse it first"}}
		}
	}
	t.ApprovedOn = nil
	b, err := EncodeTransaction(t)
	if err != nil {
		return storage.Document{}, t, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentEquityTransaction, Payload: b})
	return doc, t, err
}

// DraftedTransaction reads an equity transaction not approved yet, its row locked until commit (ENG-47): CodeRecorded
// when it is approved.
func DraftedTransaction(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Document, Transaction, error) {
	doc, err := Drafted(ctx, u, storage.DocumentEquityTransaction, id)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code == CodeRecorded {
			e.Message, e.Fix = "the transaction is approved, so it changes no more", "Correct it with a reversal once it is effective"
		}
		return doc, Transaction{}, err
	}
	t, err := ReadTransaction(doc)
	return doc, t, err
}

// ReviseTransactionIn saves a new version of a transaction not approved yet, in u; the day it was drafted stays.
func ReviseTransactionIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, t Transaction, s ledger.Settings) (storage.Document, Transaction, error) {
	doc, was, err := DraftedTransaction(ctx, u, id)
	if err != nil {
		return doc, t, err
	}
	t.Date = was.Date
	if t.Kind == KindInstrumentConversion && was.Kind == KindInstrumentConversion {
		t.Review = was.Review // what the accountant completes stays with the draft until it is approved
	}
	if t, err = CheckTransaction(ctx, u, t, s); err != nil {
		return doc, t, err
	}
	t.ApprovedOn = nil
	b, err := EncodeTransaction(t)
	if err != nil {
		return doc, t, err
	}
	doc, err = u.Documents().Revise(ctx, id, b)
	return doc, t, err
}

// ApproveTransactionIn approves a drafted transaction in u, the approval's unit of work, on today: checked again, with
// its evidence attached (EQUITY-06), numbered EQ- (gap-free), the day of its approval saved, so it is never effective
// earlier. It posts nothing: the transaction moves shares when it is made effective.
func ApproveTransactionIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, today time.Time, s ledger.Settings) (storage.Document, error) {
	return approveTransaction(ctx, u, id, today, s, true)
}

// ApproveImportedIn approves a transaction the workbook import drafted (FR-MIG-01; rev 0.262) on the day the workbook
// says it happened: its evidence is the workbook the migration keeps as M1's evidence, which its migration record names,
// and the import's approved commit is its approval [accountant to confirm].
func ApproveImportedIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, on time.Time, s ledger.Settings) (storage.Document, error) {
	return approveTransaction(ctx, u, id, on, s, false)
}

func approveTransaction(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, today time.Time, s ledger.Settings, evidence bool) (storage.Document, error) {
	doc, t, err := DraftedTransaction(ctx, u, id)
	if err != nil {
		return doc, err
	}
	if n, err := attached(ctx, u, id); err != nil {
		return doc, err
	} else if n == 0 && evidence {
		return doc, &Error{CodeNoEvidence, "the transaction has no evidence attached", "Attach the resolutions, the instrument of transfer or the ACRA extract, then ask again"}
	}
	if t, err = CheckTransaction(ctx, u, t, s); err != nil {
		return doc, err
	}
	number, err := u.Sequences().Number(ctx, doc.ID, storage.DocumentEquityTransaction)
	if err != nil {
		return doc, err
	}
	y, m, d := today.Date()
	on := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	t.ApprovedOn = &on
	b, err := EncodeTransaction(t)
	if err != nil {
		return doc, err
	}
	if doc, err = u.Documents().Revise(ctx, id, b); err != nil {
		return doc, err
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
		return doc, err
	}
	doc.Number, doc.Status = number, storage.DocumentPosted
	return doc, nil
}

// HoldingKey is one holding: a holder's shares of one class of one account. Class is the class the shares count in:
// the one they name, or for shares allotted before their account had a recorded class, its first one (design D2).
type HoldingKey struct {
	Holder, Account uuid.UUID
	Class           uuid.UUID // uuid.Nil while the account has no recorded class
}

// Holdings are every holding on day, from the holding movements alone (FR-EQ-17), with no empty one.
func Holdings(ctx context.Context, u storage.UnitOfWork, day time.Time) (map[HoldingKey]apd.Decimal, error) {
	moves, err := Moves(ctx, u)
	if err != nil {
		return nil, err
	}
	out := map[HoldingKey]apd.Decimal{}
	for _, m := range moves {
		if m.On.After(day) {
			continue
		}
		sum := out[m.Key]
		if _, err := apd.BaseContext.Add(&sum, &sum, &m.Shares); err != nil {
			return nil, err
		}
		out[m.Key] = sum
	}
	for k, v := range out {
		if v.IsZero() {
			delete(out, k)
		}
	}
	return out, nil
}

// Holding is what holder holds of a class on day.
func Holding(ctx context.Context, u storage.UnitOfWork, holder, account uuid.UUID, class *uuid.UUID, day time.Time) (apd.Decimal, error) {
	all, err := Holdings(ctx, u, day)
	if err != nil {
		return apd.Decimal{}, err
	}
	k := HoldingKey{Holder: holder, Account: account}
	if class != nil {
		k.Class = *class
	} else if def, err := DefaultClass(ctx, u, account); err != nil {
		return apd.Decimal{}, err
	} else if def != nil {
		k.Class = *def
	}
	return all[k], nil
}

// Transactions are every equity transaction, drafts included, oldest first.
func Transactions(ctx context.Context, u storage.UnitOfWork) ([]storage.Document, error) {
	return all(ctx, u, storage.DocumentEquityTransaction)
}

// PartiesOf are the holders a transaction moves shares of or for; a reversal's are those of the transaction it
// reverses (FM-9).
func PartiesOf(ctx context.Context, u storage.UnitOfWork, t Transaction) ([]uuid.UUID, error) {
	if t.Kind != KindReversal {
		return t.Parties(), nil
	}
	doc, err := u.Documents().Get(ctx, t.ReversesID)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	was, err := ReadTransaction(doc)
	if err != nil {
		return nil, err
	}
	return was.Parties(), nil
}

// PartiesSince are the holders with an equity transaction of their own waiting for approval (asked, by document ID) or
// approved since since: those an approval benefits (FM-9).
func PartiesSince(ctx context.Context, u storage.UnitOfWork, asked map[string]bool, since time.Time) (map[uuid.UUID]bool, error) {
	docs, err := Transactions(ctx, u)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]bool{}
	for _, doc := range docs {
		waiting := doc.Status == storage.DocumentDraft && asked[doc.ID.String()]
		recent := doc.Status == storage.DocumentPosted && doc.PostedAt != nil && !doc.PostedAt.Before(since)
		if !waiting && !recent {
			continue
		}
		t, err := ReadTransaction(doc)
		if err != nil {
			return nil, err
		}
		parties, err := PartiesOf(ctx, u, t)
		if err != nil {
			return nil, err
		}
		for _, p := range parties {
			out[p] = true
		}
	}
	return out, nil
}
