package banking

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Auto-reconcile (FR-BNK-14; p1-bank-import-reconcile task 5.1, design D8): after each import or sync, the system
// principal reconciles, without a click, a line that is the missing side of exactly one transfer in transit, a line
// that matches exactly one open invoice or bill (its amount and currency, its number named exactly and its contact
// named by the counterparty), and a line an approved auto-apply rule meets. Each stays within the organisation's
// limits: a line's size (bank.auto_cap_line, S$500 by default) and what one rule reconciles in a day
// (bank.auto_cap_rule_day, S$2,000; the owner's decision of 25 Oct 2026). Anything else stays in the queue for a
// person, as do AI suggestions, which are never applied by themselves (AI-P4). Each reconciliation is a match whose
// basis says what made it; it is undone while its period is open, and a weekly sample enters the review (FM-3).

// AutoActor is the system principal that reconciles lines without a click (FR-AUT-02).
var AutoActor = storage.Actor{Type: "service", ID: "bank.auto-reconcile"}

// Sampler is the scheduler's principal for the weekly sample (FR-AUT-02): the job bank.review-sample.
var Sampler = storage.Actor{Type: "scheduler", ID: "bank.review-sample"}

// CodeNotAuto is an undo of a line the system did not reconcile: a person undoes their own where they made it.
const CodeNotAuto = "BANK-09"

// Why a line stays in the queue.
const (
	LeftNoMatch   = "no_match"  // nothing matches it alone
	LeftAmbiguous = "ambiguous" // more than one thing matches it, or money recorded by hand may be it
	LeftLineCap   = "line_cap"  // the line is above the limit for one line
	LeftRuleCap   = "rule_cap"  // the rule's limit for the day would be passed
	LeftNoRate    = "no_rate"   // a line in another currency with no rate to size it
	LeftRefused   = "refused"   // the posting rules or the documents refused it
	LeftNotQueued = "not_queued"
)

// Reconciled is a line the system reconciled: how (the match's basis), by which rule, and the match.
type Reconciled struct {
	Line  storage.BankLine
	Basis string
	Rule  *Rule
	Match storage.BankMatch
}

// Left is a line left in the queue, and why.
type Left struct {
	Line storage.BankLine
	Why  string
	Err  error // the refusal, for LeftRefused
}

// AutoRun is what a run did.
type AutoRun struct {
	Reconciled []Reconciled
	Left       []Left
}

// limits are the limits in force on a day, in the base currency.
type limits struct {
	line, ruleDay money.Money
}

func loadLimits(ctx context.Context, u storage.UnitOfWork, base money.Currency, on time.Time) (limits, error) {
	read := func(key string) (money.Money, error) {
		def, ok := settings.Lookup(key)
		if !ok {
			return money.Money{}, fmt.Errorf("no setting %s", key)
		}
		changes, err := u.Settings().Changes(ctx, def.Key)
		if err != nil {
			return money.Money{}, err
		}
		return money.ParseStored(settings.On(def, changes, on).Value, base)
	}
	var out limits
	var err error
	if out.line, err = read("bank.auto_cap_line"); err != nil {
		return out, err
	}
	out.ruleDay, err = read("bank.auto_cap_rule_day")
	return out, err
}

// size is a line's size in the base currency: its amount without sign, at the rate for its date when in another
// currency; false when there is no rate.
func size(ctx context.Context, u storage.UnitOfWork, base money.Currency, l storage.BankLine) (money.Money, bool, error) {
	cur, err := money.Lookup(l.Currency)
	if err != nil {
		return money.Money{}, false, err
	}
	amount, err := money.ParseStored(string(l.Amount), cur)
	if err != nil {
		return money.Money{}, false, err
	}
	if amount.Sign() < 0 {
		amount = amount.Neg()
	}
	if cur == base {
		return amount, true, nil
	}
	f, err := fx.Lookup(ctx, u, base, cur, l.BookingDate)
	var fe *fx.Error
	if errors.As(err, &fe) {
		return money.Money{}, false, nil
	}
	if err != nil {
		return money.Money{}, false, err
	}
	b, err := rounding.FX(amount, f.Rate)
	return b, err == nil, err
}

// dayStart is the start of at's day in the organisation's time zone.
func dayStart(o storage.Organisation, at time.Time) time.Time {
	loc, err := time.LoadLocation(o.Profile.TimeZone)
	if err != nil || o.Profile.TimeZone == "" {
		loc = time.UTC
	}
	y, m, d := at.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// plan is what a run will do with a line.
type plan struct {
	basis    string
	transfer *storage.BankTransfer
	doc      *Suggestion
	rule     *Rule
}

// decide works out, in u and writing nothing, what the system may do with a line on its own.
func decide(ctx context.Context, u storage.UnitOfWork, line storage.BankLine, rules []Rule, lim limits, base money.Currency, since time.Time) (plan, string, error) {
	if line.Status != storage.LineUnreconciled {
		return plan{}, LeftNotQueued, nil
	}
	sz, ok, err := size(ctx, u, base, line)
	if err != nil || !ok {
		return plan{}, LeftNoRate, err
	}
	if c, _ := sz.Cmp(lim.line); c > 0 {
		return plan{}, LeftLineCap, nil
	}
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return plan{}, "", err
	}
	amount, err := money.ParseStored(string(line.Amount), cur)
	if err != nil {
		return plan{}, "", err
	}

	// The missing side of exactly one transfer in transit.
	transit, err := u.BankMatching().InTransit(ctx)
	if err != nil {
		return plan{}, "", err
	}
	var sides []storage.BankTransfer
	for _, t := range transit {
		near := !line.BookingDate.Before(t.OnDate.AddDate(0, 0, -journal.RecordedWindow)) && !line.BookingDate.After(t.OnDate.AddDate(0, 0, journal.RecordedWindow))
		out := t.FromLineID == nil && t.FromAccountID == line.MoneyAccountID && t.FromCurrency == line.Currency && equalAmount(t.FromAmount, amount.Neg())
		in := t.ToLineID == nil && t.ToAccountID == line.MoneyAccountID && t.ToCurrency == line.Currency && equalAmount(t.ToAmount, amount)
		if near && (out || in) {
			sides = append(sides, t)
		}
	}
	if len(sides) > 1 {
		return plan{}, LeftAmbiguous, nil
	}
	if len(sides) == 1 {
		return plan{basis: storage.BasisTransfer, transfer: &sides[0]}, "", nil
	}

	// Exactly one open document: its amount, its number named exactly, and its contact named by the counterparty.
	suggested, err := Suggest(ctx, u, line, nil)
	if err != nil {
		return plan{}, "", err
	}
	var exact []Suggestion
	for _, s := range suggested {
		if s.Found == FoundRecorded {
			return plan{}, LeftAmbiguous, nil // the money may be recorded by hand already: a person decides
		}
		if s.Found != FoundReference || s.Open == nil || s.Contact == "" {
			continue
		}
		size := amount
		if size.Sign() < 0 {
			size = size.Neg()
		}
		party := foldRef(line.Counterparty + " " + line.Description)
		if s.Open.Equal(size) && strings.Contains(party, foldRef(s.Contact)) {
			exact = append(exact, s)
		}
	}
	if len(exact) > 1 {
		return plan{}, LeftAmbiguous, nil
	}
	if len(exact) == 1 {
		return plan{basis: storage.BasisExact, doc: &exact[0]}, "", nil
	}

	// An approved auto-apply rule, within its limit for the day.
	r, ok := First(rules, line)
	if !ok || !r.AutoApply {
		return plan{}, LeftNoMatch, nil
	}
	used, err := ruleUsed(ctx, u, base, r.ID, since)
	if err != nil {
		return plan{}, "", err
	}
	if total, _ := used.Add(sz); func() bool { c, _ := total.Cmp(lim.ruleDay); return c > 0 }() {
		return plan{}, LeftRuleCap, nil
	}
	return plan{basis: storage.BasisRule, rule: &r}, "", nil
}

func equalAmount(d storage.Decimal, m money.Money) bool {
	x, err := money.ParseStored(string(d), m.Currency())
	return err == nil && x.Equal(m)
}

// ruleUsed is what a rule has reconciled since a time, still standing, in the base currency.
func ruleUsed(ctx context.Context, u storage.UnitOfWork, base money.Currency, rule uuid.UUID, since time.Time) (money.Money, error) {
	matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{SystemOnly: true, Since: &since})
	if err != nil {
		return money.Money{}, err
	}
	total := money.Zero(base)
	for _, m := range matches {
		if m.RuleID == nil || *m.RuleID != rule {
			continue
		}
		l, err := u.Banking().Line(ctx, m.LineID)
		if err != nil {
			return money.Money{}, err
		}
		if sz, ok, err := size(ctx, u, base, l); err != nil {
			return money.Money{}, err
		} else if ok {
			total, _ = total.Add(sz)
		}
	}
	return total, nil
}

// AutoReconcile reconciles what it may of the lines named (nil: every line in the queue), each in its own unit of
// work as AutoActor, so one refusal never stops the others. What it does not reconcile stays in the queue, with why.
func AutoReconcile(ctx context.Context, store storage.Store, org uuid.UUID, lineIDs []uuid.UUID, at time.Time) (AutoRun, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &AutoActor, Reason: "Auto-reconciled (FR-BNK-14)"})
	var run AutoRun
	if lineIDs == nil {
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{Statuses: []string{storage.LineUnreconciled}})
			for _, l := range lines {
				lineIDs = append(lineIDs, l.ID)
			}
			return err
		}); err != nil {
			return run, err
		}
	}
	txns := journal.BankTransactions{Store: store}
	for _, id := range lineIDs {
		var p plan
		var why string
		var line storage.BankLine
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if line, err = u.Banking().Line(ctx, id); err != nil {
				return err
			}
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			since := dayStart(o, at)
			lim, err := loadLimits(ctx, u, base, since)
			if err != nil {
				return err
			}
			rules, _, err := Rules(ctx, u)
			if err != nil {
				return err
			}
			p, why, err = decide(ctx, u, line, rules, lim, base, since)
			return err
		}); err != nil {
			return run, err
		}
		if why != "" {
			run.Left = append(run.Left, Left{Line: line, Why: why})
			continue
		}
		var m storage.BankMatch
		var err error
		switch p.basis {
		case storage.BasisTransfer:
			var t journal.Transferred
			if t, err = txns.CompleteTransfer(ctx, org, p.transfer.ID, line.ID, storage.BasisTransfer, at); err == nil {
				m = t.Matches[0]
			}
		case storage.BasisExact:
			var x journal.Matched
			if p.doc.Kind == storage.MatchReceipt {
				x, err = txns.MatchInvoices(ctx, org, journal.InvoiceMatch{LineID: line.ID, CustomerID: p.doc.ContactID, Currency: p.doc.Open.Currency(),
					Invoices: []receipt.Part{{InvoiceID: p.doc.DocumentID, Amount: *p.doc.Open, InFull: true}}, Basis: storage.BasisExact, At: at})
			} else {
				x, err = txns.MatchBills(ctx, org, journal.BillMatch{LineID: line.ID, SupplierID: p.doc.ContactID, Currency: p.doc.Open.Currency(),
					Bills: []billpayment.Part{{BillID: p.doc.DocumentID, Amount: *p.doc.Open, InFull: true}}, Basis: storage.BasisExact, At: at})
			}
			m = x.Match
		case storage.BasisRule:
			var c journal.Categorised
			c, err = txns.Categorise(ctx, org, journal.Categorisation{Draft: p.rule.Draft(line), Basis: storage.BasisRule, RuleID: &p.rule.ID, At: at})
			m = c.Match
		}
		if refused(err) {
			run.Left = append(run.Left, Left{Line: line, Why: LeftRefused, Err: err})
			continue
		}
		if err != nil {
			return run, err
		}
		run.Reconciled = append(run.Reconciled, Reconciled{Line: line, Basis: p.basis, Rule: p.rule, Match: m})
	}
	return run, nil
}

// refused says whether err is a refusal of the posting rules or the documents, which leaves a line for a person, and
// not a failure of the store.
func refused(err error) bool {
	if err == nil {
		return false
	}
	var probs ledger.Problems
	var bt *banktransaction.Error
	var re *receipt.Error
	var be *billpayment.Error
	var se *storage.Error
	return errors.As(err, &probs) || errors.As(err, &bt) || errors.As(err, &re) || errors.As(err, &be) ||
		(errors.As(err, &se) && se.Code != storage.CodeStorageFailure && se.Code != storage.CodeRetry && se.Code != storage.CodeLockTimeout)
}

// UndoAuto undoes what the system reconciled a line to, with a reason, while its period is open: a categorisation by
// a rule is reversed, a receipt or bill payment made from the line voided, and a transfer undone; the line goes back to
// the queue. A line a person matched is undone where they made it (CodeNotAuto).
func UndoAuto(ctx context.Context, store storage.Store, org, lineID uuid.UUID, reason string, at time.Time) error {
	_, err := Unreconcile(ctx, journal.BankTransactions{Store: store}, org, lineID, reason, at, true)
	return err
}

// The weekly sample (FR-BNK-14, FM-3; the owner's decision of 25 Oct 2026).
const (
	SampleLargest = 5 // the week's largest auto-reconciliations
	SampleRandom  = 5 // and others at random
)

// Sample puts a week's sample of auto-reconciliations in the organisation's review log, once each: of those the system
// made in the week from weekStart that still stand, the SampleLargest largest in the base currency and SampleRandom
// others chosen as at random, by a hash of the organisation, the week and the match that no one can steer, so a second
// run chooses the same. It returns how many it logged.
func Sample(ctx context.Context, store storage.Store, org uuid.UUID, weekStart, at time.Time) (int, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Sampler, Reason: "The weekly sample of auto-reconciliations (FR-BNK-14, FM-3)"})
	logged := 0
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		base, err := money.Lookup(o.BaseCurrency)
		if err != nil {
			return err
		}
		end := weekStart.AddDate(0, 0, 7)
		matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{SystemOnly: true, Since: &weekStart})
		if err != nil {
			return err
		}
		entries, err := u.ReviewLog().List(ctx)
		if err != nil {
			return err
		}
		done := map[string]bool{}
		for _, e := range entries {
			if e.Kind == storage.ReviewAutoReconciled {
				done[e.ObjectKey] = true
			}
		}
		type item struct {
			m    storage.BankMatch
			line storage.BankLine
			size money.Money
		}
		var items []item
		for _, m := range matches {
			if !m.MadeAt.Before(end) {
				continue
			}
			l, err := u.Banking().Line(ctx, m.LineID)
			if err != nil {
				return err
			}
			sz, ok, err := size(ctx, u, base, l)
			if err != nil {
				return err
			}
			if !ok {
				sz = money.Zero(base)
			}
			items = append(items, item{m, l, sz})
		}
		slices.SortStableFunc(items, func(a, b item) int {
			c, _ := b.size.Cmp(a.size)
			if c == 0 {
				return strings.Compare(a.m.ID.String(), b.m.ID.String())
			}
			return c
		})
		chosen := items[:min(SampleLargest, len(items))]
		rest := slices.Clone(items[len(chosen):])
		// The others in an order no one can steer and anyone can work out again: by the SHA-256 of the organisation, the
		// week and the match, so a second run chooses the same.
		rank := func(x item) []byte {
			h := sha256.Sum256([]byte(org.String() + "|" + weekStart.UTC().Format(time.DateOnly) + "|" + x.m.ID.String()))
			return h[:]
		}
		slices.SortStableFunc(rest, func(a, b item) int { return bytes.Compare(rank(a), rank(b)) })
		chosen = append(chosen, rest[:min(SampleRandom, len(rest))]...)
		for _, x := range chosen {
			if done[x.line.ID.String()] {
				continue
			}
			what := map[string]string{storage.BasisExact: "the one open document it matched", storage.BasisTransfer: "a transfer's other side",
				storage.BasisRule: "an auto-apply rule"}[x.m.Basis]
			if _, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewAutoReconciled, ObjectType: "bank_line",
				ObjectKey: x.line.ID.String(), LoggedAt: at, Summary: fmt.Sprintf("Reconciled by itself on %s: %s %s of %s, by %s",
					x.m.MadeAt.In(time.UTC).Format(time.DateOnly), x.line.Amount, x.line.Currency, x.line.BookingDate.Format(time.DateOnly), what)}); err != nil {
				return err
			}
			logged++
		}
		return nil
	})
	return logged, err
}

// SampleAll puts the previous week's sample in each organisation's review log: the week from the Monday before last,
// in the organisation's time zone, at now. An organisation that fails does not stop the others.
func SampleAll(ctx context.Context, store storage.Store, now time.Time) error {
	orgs, err := store.Organisations(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, org := range orgs {
		var start time.Time
		if err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			today := dayStart(o, now)
			start = today.AddDate(0, 0, -int((today.Weekday()+6)%7)-7) // the Monday of the week before
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			continue
		}
		if _, err := Sample(ctx, store, org, start, now); err != nil && !errors.Is(err, ports.ErrNotFound) {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return errors.Join(errs...)
}
