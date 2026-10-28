package journal

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Deferrals are the spreads as at a day (FR-GL-03, ENG-05): each spread line of a posted bill, invoice or bank line with
// its months, and each holding account, 1210 Prepayments and 2400 Deferred revenue, reconciled with the documents that
// put amounts on it (INV-07).
type Deferrals struct {
	AsAt      time.Time
	Schedules []Schedule
	Holdings  []Holding
}

// Schedule is one spread line of a document's current version as at a day: what it put on its holding account, what
// its releases dated by the day took off, and each month. Amounts are in the base currency.
type Schedule struct {
	Holding     string // ledger.RolePrepayments or ledger.RoleDeferredRevenue
	Document    storage.Document
	Line        int
	AccountID   uuid.UUID
	Description string
	Total       money.Money
	Recognised  money.Money
	Remaining   money.Money
	Months      []ScheduleMonth
}

// ScheduleMonth is one month of a schedule and its state as at the day: ReleasePosted, ReleaseReversed, ReleaseWaiting
// (dated by the day, not in the ledger yet) or ReleaseProjected (dated after it: ENG-05's projection, never an actual).
type ScheduleMonth struct {
	Month   time.Time
	Date    time.Time
	Amount  money.Money
	Status  string
	EntryID uuid.UUID
}

// Holding is a holding account reconciled with its documents as at the day (INV-07): what the balance service says it
// holds by source type against what the documents that posted to it still hold. A document's lines put on it directly,
// which no schedule releases, are in the sub-ledger and listed apart in Direct, as a warning (a bill line or a bank line
// entered on 1210 itself).
type Holding struct {
	Role           string
	Account        storage.Account
	Reconciliation subledger.Reconciliation
	Direct         []subledger.Item
	Open           int // the schedules with something remaining
}

// holdingSources are, for each holding account, the source types its documents post with: what its sub-ledger accounts
// for. Anything else (opening balances, manual journals) is named; anything else again is unexplained.
var holdingSources = map[string][]string{
	ledger.RolePrepayments:     {ledger.SourceBill, ledger.SourceBillRelease, ledger.SourceBankTransaction, ledger.SourceBankOutRelease},
	ledger.RoleDeferredRevenue: {ledger.SourceInvoice, ledger.SourceInvoiceRelease, ledger.SourceBankTransaction, ledger.SourceBankInRelease},
}

// credited says whether a holding account's balance is a credit, read negated: deferred revenue.
func credited(role string) bool { return role == ledger.RoleDeferredRevenue }

// releaseHolding is the holding account a release takes its amount off.
func releaseHolding(source string) string {
	if source == ledger.SourceInvoiceRelease || source == ledger.SourceBankInRelease {
		return ledger.RoleDeferredRevenue
	}
	return ledger.RolePrepayments
}

// spreadDocument is the document type whose entries post with a source type, if it is one a holding account's
// sub-ledger reads.
func spreadDocument(source string) (storage.DocumentType, bool) {
	switch source {
	case ledger.SourceBill, ledger.SourceBillRelease:
		return storage.DocumentBill, true
	case ledger.SourceInvoice, ledger.SourceInvoiceRelease:
		return storage.DocumentInvoice, true
	case ledger.SourceBankTransaction, ledger.SourceBankOutRelease, ledger.SourceBankInRelease:
		return storage.DocumentBankTransaction, true
	}
	return "", false
}

// deferralIdentity is the posting identity of the entry a posted document version put its amounts on a holding account
// with.
func deferralIdentity(doc storage.Document) string {
	switch doc.Type {
	case storage.DocumentBill:
		return ledger.BillIdentity(doc.ID, doc.Version, ledger.RuleBill)
	case storage.DocumentInvoice:
		return InvoiceIdentityOf(doc)
	}
	return ledger.BankTransactionIdentity(doc.ID, doc.Version, ledger.RuleBankTransaction)
}

// DeferralsAt are the spreads and the holding accounts as at a day (FR-GL-03, INV-07). The documents are those with a
// line on a holding account through the day, from the balance service (ENG-04), each at its current version: one dated
// on or before the day, posted and not voided by then. Its holding lines and its releases dated by the day are what it
// still holds; a month is posted once its release is in the ledger with a date on or before the day. Nothing is said
// for a chart with neither holding account.
func DeferralsAt(ctx context.Context, u storage.UnitOfWork, asAt time.Time) (Deferrals, error) {
	out := Deferrals{AsAt: asAt}
	s, err := loadSettings(ctx, u)
	if err != nil {
		return out, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return out, err
	}
	var roles []string
	byRole := map[string]storage.Account{}
	for _, role := range []string{ledger.RolePrepayments, ledger.RoleDeferredRevenue} {
		for _, a := range accounts {
			if string(a.Role) == role {
				byRole[role] = a
			}
		}
		if _, ok := byRole[role]; ok {
			roles = append(roles, role)
		}
	}
	documents := map[uuid.UUID]storage.DocumentType{}
	var order []uuid.UUID
	for _, role := range roles {
		sources, err := balance.Sources(ctx, u, byRole[role].ID, asAt)
		if err != nil {
			return out, err
		}
		for _, src := range sources {
			t, ok := spreadDocument(src.Type)
			if !ok {
				continue
			}
			id, err := uuid.Parse(src.ID)
			if err != nil {
				return out, err
			}
			if _, seen := documents[id]; !seen {
				documents[id] = t
				order = append(order, id)
			}
		}
	}
	items := map[string][]subledger.Item{}
	direct := map[string][]subledger.Item{}
	open := map[string]int{}
	for _, id := range order {
		doc, err := u.Documents().Get(ctx, id)
		if errors.Is(err, ports.ErrNotFound) {
			continue // a source that names no document: what it put on the holding account is unexplained
		}
		if err != nil {
			return out, err
		}
		if doc.Type != documents[id] || doc.Status == storage.DocumentDraft {
			continue
		}
		identity := deferralIdentity(doc)
		if gone, err := subledger.VoidedBy(ctx, u, identity, asAt); err != nil {
			return out, err
		} else if gone {
			continue
		}
		e, err := u.Ledger().ByIdentity(ctx, identity)
		if errors.Is(err, ports.ErrNotFound) {
			continue // nothing of this version is in the ledger: what the holding accounts hold of it is unexplained
		}
		if err != nil {
			return out, err
		}
		if e.PostingDate.After(asAt) {
			continue // its current version is dated after the day
		}
		releases, err := scheduleOf(ctx, u, doc, s)
		if err != nil {
			return out, err
		}
		schedules, err := schedulesOf(ctx, u, doc, releases, s.Base, asAt)
		if err != nil {
			return out, err
		}
		for _, role := range roles {
			acc := byRole[role]
			_, held, err := subledger.ControlLines(e, acc.ID, s.Base, s.Base)
			if err != nil {
				return out, err
			}
			if credited(role) {
				held = held.Neg()
			}
			holds, scheduled := held, money.Zero(s.Base)
			for _, sc := range schedules {
				if sc.Holding != role {
					continue
				}
				if scheduled, err = scheduled.Add(sc.Total); err != nil {
					return out, err
				}
				if holds, err = holds.Sub(sc.Recognised); err != nil {
					return out, err
				}
				if !sc.Remaining.IsZero() {
					open[role]++
				}
			}
			it := subledger.Item{Kind: string(doc.Type), DocumentID: doc.ID, Number: doc.Number, Date: e.PostingDate, Total: held, Open: holds, Carrying: holds}
			if !holds.IsZero() {
				items[role] = append(items[role], it)
			}
			unscheduled, err := held.Sub(scheduled)
			if err != nil {
				return out, err
			}
			if !unscheduled.IsZero() {
				it.Open, it.Carrying = unscheduled, unscheduled
				direct[role] = append(direct[role], it)
			}
		}
		out.Schedules = append(out.Schedules, schedules...)
	}
	for _, role := range roles {
		acc := byRole[role]
		totals, err := balance.Control(ctx, u, s.Base, acc.ID, asAt)
		if err != nil {
			return out, err
		}
		own := map[string]bool{}
		for _, src := range holdingSources[role] {
			own[src] = true
		}
		r, err := subledger.Reconcile(s.Base, asAt, totals, []subledger.Group{{Items: items[role]}}, credited(role), func(t string) bool { return own[t] })
		if err != nil {
			return out, err
		}
		out.Holdings = append(out.Holdings, Holding{Role: role, Account: acc, Reconciliation: r, Direct: direct[role], Open: open[role]})
	}
	return out, nil
}

// schedulesOf are a document version's spread lines as at a day, from its release schedule: each line's months, those
// whose part is zero left out, with what is recognised by the day and what remains.
func schedulesOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, releases []ledger.Release, base money.Currency, asAt time.Time) ([]Schedule, error) {
	var out []Schedule
	at := map[int]int{} // a line's schedule in out
	for _, r := range releases {
		if r.Amount.IsZero() {
			continue
		}
		i, ok := at[r.Line]
		if !ok {
			i = len(out)
			at[r.Line] = i
			out = append(out, Schedule{Holding: releaseHolding(r.Source), Document: doc, Line: r.Line, AccountID: r.AccountID, Description: r.Description,
				Total: money.Zero(base), Recognised: money.Zero(base), Remaining: money.Zero(base)})
		}
		sc := &out[i]
		m := ScheduleMonth{Month: r.Month, Date: r.Date, Amount: r.Amount, Status: ReleaseProjected}
		e, err := postedRelease(ctx, u, r)
		switch {
		case err == nil && !e.PostingDate.After(asAt):
			m.Status, m.EntryID = ReleasePosted, e.ID
			reversed, err := subledger.VoidedBy(ctx, u, e.PostingIdentity, asAt)
			if err != nil {
				return nil, err
			}
			if reversed {
				m.Status = ReleaseReversed
			} else if sc.Recognised, err = sc.Recognised.Add(r.Amount); err != nil {
				return nil, err
			}
		case err != nil && !errors.Is(err, ports.ErrNotFound):
			return nil, err
		case !r.Date.After(asAt):
			m.Status = ReleaseWaiting
		}
		var err2 error
		if sc.Total, err2 = sc.Total.Add(r.Amount); err2 != nil {
			return nil, err2
		}
		sc.Months = append(sc.Months, m)
	}
	for i := range out {
		var err error
		if out[i].Remaining, err = out[i].Total.Sub(out[i].Recognised); err != nil {
			return nil, err
		}
	}
	return out, nil
}
