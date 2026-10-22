package journal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/credits"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// BillPayments runs the commands of payments recorded by hand (FR-AP-02, FR-AR-18; the owner's decision of 22 Oct
// 2026) against a store: each in one unit of work, the document, its entry from the rule bill_payment@1 and its
// allocations together (ENG-02). A payment is recorded and posted at once, as the money has left already; it is
// undone by a void, a reversing entry and reversing allocations, which reopens the bills (FR-GL-12, FR-AR-18).
type BillPayments struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// CheckedPayment is a payment as recording it would post it, written nowhere: what the rule reads, the entry, the
// settlements, and the problems that stop it.
type CheckedPayment struct {
	Prepared    billpayment.Prepared
	Entry       ledger.Entry
	Settlements []ledger.Settlement
	Problems    ledger.Problems
}

// Recorded is a recorded payment: its document, its entry and its allocations.
type Recorded struct {
	Payment     storage.Document
	Entry       storage.Entry
	Allocations []storage.Allocation
}

func checkPayment(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (CheckedPayment, error) {
	p, probs, err := billpayment.Prepare(ctx, u, doc, s)
	if err != nil {
		return CheckedPayment{}, err
	}
	out := CheckedPayment{Prepared: p, Problems: probs}
	entry, settled, err := ledger.PostBillPayment(p.Payment, s)
	if ps, ok := err.(ledger.Problems); ok {
		for _, x := range ps {
			if x.Code == ledger.CodeMissingRate && probs.Has(fx.CodeNoRate) {
				continue // the cascade found no rate: FX-01 says so, in place of the engine's LEDGER-07
			}
			if x.Code == ledger.CodePaymentAmount && x.Field == "bills" && (probs.Has(billpayment.CodeBill) || probs.Has(billpayment.CodeOverpaid)) {
				continue // the bills were refused: their own problems say so, in place of "it settles nothing"
			}
			out.Problems = append(out.Problems, x)
		}
	} else if err != nil {
		return CheckedPayment{}, err
	}
	if len(out.Problems) == 0 {
		out.Entry, out.Settlements = entry, settled
	}
	return out, nil
}

// lockBills locks the bills a payment names, in one order, so two payments of the same bills wait for each other
// instead of deadlocking, and neither settles more than is open (ENG-47).
func lockBills(ctx context.Context, u storage.UnitOfWork, ids []uuid.UUID) error {
	ids = slices.Clone(ids)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	for i, id := range ids {
		if i > 0 && id == ids[i-1] {
			continue
		}
		if _, err := u.Documents().Lock(ctx, id); err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
	}
	return nil
}

func partIDs(d billpayment.Draft) []uuid.UUID {
	var out []uuid.UUID
	for _, x := range d.Bills {
		out = append(out, x.BillID)
	}
	return out
}

// Check runs the rule over a payment as it would be recorded, and writes nothing: the ledger impact a person sees
// before recording it, and the problems that would stop it.
func (s BillPayments) Check(ctx context.Context, org uuid.UUID, d billpayment.Draft) (CheckedPayment, error) {
	b, err := billpayment.Encode(d)
	if err != nil {
		return CheckedPayment{}, err
	}
	var out CheckedPayment
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		doc := storage.Document{ID: uuid.Nil, Type: storage.DocumentBillPayment, Version: 1, Status: storage.DocumentDraft, Payload: b}
		out, err = checkPayment(ctx, u, doc, settings)
		return err
	})
	return out, err
}

// Record records a payment and posts it in one transaction: the document, its number (PAY-, gap-free), its entry, an
// allocation for each bill it settles, and the document's change to posted. The bills are locked first. A payment the
// rules refuse writes nothing and answers every problem.
func (s BillPayments) Record(ctx context.Context, org uuid.UUID, d billpayment.Draft) (Recorded, error) {
	b, err := billpayment.Encode(d)
	if err != nil {
		return Recorded{}, err
	}
	var out Recorded
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if err := lockBills(ctx, u, partIDs(d)); err != nil {
			return err
		}
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBillPayment, Payload: b})
		if err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		c, err := checkPayment(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		if len(c.Problems) > 0 {
			return c.Problems
		}
		if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentBillPayment); err != nil {
			return err
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
			return err
		}
		for _, x := range c.Settlements {
			a, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: x.BillID, On: d.Date,
				Currency: x.Amount.Currency().Code, Amount: storage.Decimal(x.Amount.String()), BaseCurrency: settings.Base.Code,
				BaseAmount: storage.Decimal(x.Base.String()), RealisedFX: storage.Decimal(x.RealisedFX.String())})
			if err != nil {
				return err
			}
			out.Allocations = append(out.Allocations, a)
		}
		if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		out.Payment = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

// Void undoes a posted payment with a reversing entry dated date, with a reason (FR-GL-12), and a reversing allocation
// for each of its allocations on that date, which reopens the bills (FR-AR-18); the payment is marked reversed. The
// date is not before the payment's, and its period must be open (INV-03).
func (s BillPayments) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (Recorded, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out Recorded
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if doc.Type != storage.DocumentBillPayment {
			return &billpayment.Error{Code: billpayment.CodeNotPayment, Message: fmt.Sprintf("document %s is a %s, not a bill payment", doc.ID, doc.Type), Fix: "Open it with its own module"}
		}
		if doc.Status != storage.DocumentPosted {
			return &billpayment.Error{Code: billpayment.CodeNotPosted, Message: fmt.Sprintf("the payment is %s, not posted", doc.Status), Fix: "Only a posted payment is voided"}
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		d, err := billpayment.Read(doc, settings.Base)
		if err != nil {
			return err
		}
		y, m, dd := date.Date()
		date = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
		if date.Before(d.Date) {
			return &billpayment.Error{Code: billpayment.CodeVoidDate, Message: fmt.Sprintf("the void is dated %s, before the payment's %s", date.Format(time.DateOnly), d.Date.Format(time.DateOnly)),
				Fix: "Date the void on or after the payment"}
		}
		if err := lockBills(ctx, u, partIDs(d)); err != nil {
			return err
		}
		applied, err := u.Allocations().Settled(ctx, []uuid.UUID{doc.ID}) // its unapplied amount applied to bills since
		if err != nil {
			return err
		}
		if x, ok := applied[doc.ID]; ok && !isZeroDecimal(string(x)) {
			return &credits.Error{Code: credits.CodeApplied, Message: fmt.Sprintf("%s of what the payment left unapplied is applied to bills", x),
				Fix: "Void those applications first, then void the payment"}
		}
		stored, err := u.Ledger().ByIdentity(ctx, ledger.BillPaymentIdentity(doc.ID, doc.Version, ledger.RuleBillPayment))
		if err != nil {
			return err
		}
		original, err := fromStorage(stored, settings.Base)
		if err != nil {
			return err
		}
		reversal, err := ledger.Reverse(original, date, reason)
		if err != nil {
			return err
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
			return err
		}
		allocs, err := u.Allocations().OfSource(ctx, doc.ID)
		if err != nil {
			return err
		}
		reversed := map[uuid.UUID]bool{}
		for _, a := range allocs {
			if a.ReversesID != nil {
				reversed[*a.ReversesID] = true
			}
		}
		for _, a := range allocs {
			if a.ReversesID != nil || reversed[a.ID] {
				continue
			}
			id := a.ID
			r, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: a.SourceID, SourceVersion: a.SourceVersion, DocumentID: a.DocumentID, On: date,
				Currency: a.Currency, Amount: negated(a.Amount), BaseCurrency: a.BaseCurrency, BaseAmount: negated(a.BaseAmount),
				RealisedFX: negated(a.RealisedFX), ReversesID: &id})
			if err != nil {
				return err
			}
			out.Allocations = append(out.Allocations, r)
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		out.Payment = doc
		return s.after(ctx, u, doc, out.Entry)
	})
	return out, err
}

// negated is a stored decimal with its sign changed, as written: "-12.30" for "12.30", "0.00" stays.
func negated(d storage.Decimal) storage.Decimal {
	s := string(d)
	switch {
	case len(s) > 0 && s[0] == '-':
		return storage.Decimal(s[1:])
	case isZero(s):
		return d
	}
	return storage.Decimal("-" + s)
}

func isZero(s string) bool {
	for _, c := range s {
		if c != '0' && c != '.' {
			return false
		}
	}
	return true
}

func (s BillPayments) after(ctx context.Context, u storage.UnitOfWork, doc storage.Document, e storage.Entry) error {
	if s.AfterPosting == nil {
		return nil
	}
	return s.AfterPosting(ctx, u, doc, e)
}

// replayBillPayment posts a stored payment version again under the rule that posted e (ENG-06), with what the entry and
// the allocations kept: the rate the cascade gave it (ENG-14), each bill's stored rate from its line, and each bill's
// carrying amount the allocation cleared, so a later payment or void of the same bills never makes an old payment differ.
func replayBillPayment(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	d, err := billpayment.Read(doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return ledger.Entry{}, err
	}
	cleared := map[uuid.UUID]storage.Allocation{}
	for _, a := range allocs {
		if a.ReversesID == nil && a.SourceVersion == doc.Version {
			cleared[a.DocumentID] = a
		}
	}
	p := ledger.BillPayment{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: stored.Memo, Account: d.AccountID, Currency: d.Currency, Rate: d.Rate,
		Paid: d.Paid, Unapplied: d.Unapplied}
	var parts []ledger.Line // each bill's line, in order: on 2010, with an amount
	for _, l := range stored.Lines {
		switch {
		case s.Accounts[l.AccountID].Role == ledger.RoleAccountsPayable && !l.Amount.IsZero():
			parts = append(parts, l)
		case l.AccountID == d.AccountID && d.Rate == nil && d.Currency != s.Base && l.Amount.Currency() == d.Currency:
			r := l.Rate // the cascade's rate as the entry kept it
			p.Rate = &r
		}
	}
	for i, x := range d.Bills {
		part := ledger.PaidBill{BillID: x.BillID, Amount: x.Amount, StoredRate: money.Identity(s.Base)}
		if i < len(parts) {
			part.StoredRate = parts[i].Rate
		}
		if a, ok := cleared[x.BillID]; ok {
			base, err := money.ParseStored(string(a.BaseAmount), s.Base)
			if err != nil {
				return ledger.Entry{}, err
			}
			part.Clears = &base
		}
		p.Bills = append(p.Bills, part)
	}
	entry, _, err := ledger.ReplayBillPayment(e.RuleVersion, p, s)
	return entry, err
}
