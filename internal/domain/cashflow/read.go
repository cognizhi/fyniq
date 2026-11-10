package cashflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/forecasts"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/kpi"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/paymentbatch"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// NotYet are the inputs FR-PLN-09 names that join with their own changes (design D7): listed on every projection, so
// its lowest point is never taken as complete.
var NotYet = []Excluded{
	{What: "Pay runs and CPF", Why: "they join with payroll"},
	{What: "GST returns", Why: "they join once the company is registered for GST"},
	{What: "Corporate income tax", Why: "it joins with the tax filings"},
}

// Inputs are what a projection is laid out from.
type Inputs struct {
	Base     money.Currency
	Today    time.Time
	Opening  money.Money
	Items    []Item
	Buffer   Buffer
	Excluded []Excluded
}

// Project lays the inputs out for days days.
func (in Inputs) Project(days int) (Projection, error) {
	return Project(in.Today, days, in.Opening, in.Items, in.Buffer, in.Excluded)
}

// Read gathers a projection's inputs in u on today (the organisation's), for the days ahead (design D7): the money
// accounts' balance today, valued at today's rates; each open invoice and bill on its expected day at its carrying amount;
// each active recurring invoice's and bill's occurrences in the days, at the total its content comes to today; each
// approved payment batch not yet released on its payment day, its bills taken out of the open ones; and the buffer line.
// It saves nothing.
func Read(ctx context.Context, u storage.UnitOfWork, today time.Time, days int) (Inputs, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return Inputs{}, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return Inputs{}, err
	}
	in := Inputs{Base: base, Today: today}
	cash, err := banking.Cash(ctx, u, today)
	if err != nil {
		return Inputs{}, err
	}
	in.Opening = cash.Total
	for _, name := range cash.Missing {
		in.Excluded = append(in.Excluded, Excluded{What: name, Why: "a money account in a currency with no rate for today, so its balance is left out"})
	}
	last := today.AddDate(0, 0, days-1)
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Inputs{}, err
	}
	var ar, ap []uuid.UUID
	for _, a := range accounts {
		switch string(a.Role) {
		case ledger.RoleAccountsReceivable:
			ar = append(ar, a.ID)
		case ledger.RoleAccountsPayable:
			ap = append(ap, a.ID)
		}
	}
	// Each contact's payment behaviour over the last 12 months: customers' and, apart, the company's with its suppliers.
	customerLate, supplierLate := map[uuid.UUID][]int{}, map[uuid.UUID][]int{}
	since := today.AddDate(-1, 0, 0)
	for _, acc := range ar {
		ss, err := receivables.Settlements(ctx, u, base, acc, since, today)
		if err != nil {
			return Inputs{}, err
		}
		lateness(customerLate, ss)
	}
	for _, acc := range ap {
		ss, err := payables.Settlements(ctx, u, base, acc, since, today)
		if err != nil {
			return Inputs{}, err
		}
		lateness(supplierLate, ss)
	}
	expect := func(it *Item, late []int, planned *time.Time) {
		switch {
		case len(late) > 0:
			it.DaysLate = AverageLate(late)
			it.On, it.Basis = it.DueDate.AddDate(0, 0, it.DaysLate), BasisHistory
		case planned != nil:
			it.On, it.Basis = *planned, BasisPlanned
		default:
			it.On, it.Basis = it.DueDate, BasisDue
		}
		if it.On.Before(today) {
			it.On, it.Basis = today, BasisOverdue
		}
	}

	// The approved batches not yet released, each on its payment day; their bills leave the open ones.
	approved, err := paymentbatch.Approved(ctx, u)
	if err != nil {
		return Inputs{}, err
	}
	settings, err := journal.LoadSettings(ctx, u)
	if err != nil {
		return Inputs{}, err
	}
	batches := map[uuid.UUID]bool{}
	for _, id := range approved {
		if batches[id] {
			continue
		}
		batches[id] = true
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return Inputs{}, err
		}
		b, err := paymentbatch.Evaluate(ctx, u, doc, settings, approved, today)
		if err != nil {
			return Inputs{}, err
		}
		if b.Total.Currency() != base {
			in.Excluded = append(in.Excluded, Excluded{What: "Payment batch " + doc.Number, Why: "it pays in " + b.Total.Currency().Code + ", not the base currency"})
			continue
		}
		it := Item{Kind: KindBatch, ID: doc.ID, Number: doc.Number, On: b.Date, Basis: BasisScheduled, Amount: b.Total.Neg()}
		if it.On.Before(today) {
			it.On, it.Basis = today, BasisOverdue
		}
		in.Items = append(in.Items, it)
	}

	// The open invoices and bills.
	for _, acc := range ar {
		l, err := receivables.Open(ctx, u, base, acc, today)
		if err != nil {
			return Inputs{}, err
		}
		for _, c := range l.Customers {
			for _, x := range c.Items {
				if x.Kind != receivables.KindInvoice || x.Open.Sign() <= 0 || x.Carrying.Sign() <= 0 {
					continue
				}
				it := Item{Kind: KindInvoice, ID: x.DocumentID, Number: x.Number, Contact: c.ID, ContactName: c.Name, DueDate: dueOf(x), Amount: x.Carrying}
				expect(&it, customerLate[c.ID], nil)
				in.Items = append(in.Items, it)
			}
		}
	}
	for _, acc := range ap {
		l, err := payables.Open(ctx, u, base, acc, today)
		if err != nil {
			return Inputs{}, err
		}
		var ids []uuid.UUID
		for _, s := range l.Suppliers {
			for _, x := range s.Items {
				if x.Kind == payables.KindBill {
					ids = append(ids, x.DocumentID)
				}
			}
		}
		plans, err := u.BillPlans().Of(ctx, ids)
		if err != nil {
			return Inputs{}, err
		}
		for _, s := range l.Suppliers {
			for _, x := range s.Items {
				if x.Kind != payables.KindBill || x.Open.Sign() <= 0 || x.Carrying.Sign() <= 0 {
					continue
				}
				if _, inBatch := approved[x.DocumentID]; inBatch {
					continue // its batch pays it
				}
				it := Item{Kind: KindBill, ID: x.DocumentID, Number: x.Number, Contact: s.ID, ContactName: s.Name, DueDate: dueOf(x), Amount: x.Carrying.Neg()}
				expect(&it, supplierLate[s.ID], plans[x.DocumentID].PlannedOn)
				in.Items = append(in.Items, it)
			}
		}
	}

	// The recurring documents' occurrences in the days.
	recurring := func(kind string, id uuid.UUID, name string, s storage.Schedule, next *time.Time, contact uuid.UUID, contactName string,
		total money.Money, terms int, late []int) {
		for on := next; on != nil && !on.After(last); on = journal.NextAfter(s, *on) {
			it := Item{Kind: kind, ID: id, Occurrence: *on, Number: name, Contact: contact, ContactName: contactName, DueDate: on.AddDate(0, 0, terms),
				Amount: total}
			expect(&it, late, nil)
			in.Items = append(in.Items, it)
		}
	}
	var afterName string
	var afterID *uuid.UUID
	for {
		page, err := u.RecurringInvoices().Page(ctx, afterName, afterID, 200)
		if err != nil {
			return Inputs{}, err
		}
		for _, r := range page {
			if r.Status != storage.RecurringActive || r.NextOn == nil {
				continue
			}
			p, why, err := nextInvoice(ctx, u, base, today, r.Content)
			if err != nil {
				return Inputs{}, err
			}
			if why != "" {
				in.Excluded = append(in.Excluded, Excluded{What: "Recurring invoice " + r.Name, Why: why})
				continue
			}
			recurring(KindRecurringInvoice, r.ID, r.Name, r.Schedule, r.NextOn, p.party, p.partyName, p.total, p.terms, customerLate[p.party])
		}
		if len(page) < 200 {
			break
		}
		afterName, afterID = page[len(page)-1].Name, &page[len(page)-1].ID
	}
	afterName, afterID = "", nil
	for {
		page, err := u.RecurringBills().Page(ctx, afterName, afterID, 200)
		if err != nil {
			return Inputs{}, err
		}
		for _, r := range page {
			if r.Status != storage.RecurringActive || r.NextOn == nil {
				continue
			}
			p, why, err := nextBill(ctx, u, base, today, r.Content)
			if err != nil {
				return Inputs{}, err
			}
			if why != "" {
				in.Excluded = append(in.Excluded, Excluded{What: "Recurring bill " + r.Name, Why: why})
				continue
			}
			recurring(KindRecurringBill, r.ID, r.Name, r.Schedule, r.NextOn, p.party, p.partyName, p.total.Neg(), p.terms, supplierLate[p.party])
		}
		if len(page) < 200 {
			break
		}
		afterName, afterID = page[len(page)-1].Name, &page[len(page)-1].ID
	}

	if in.Buffer, err = bufferOf(ctx, u, base, today); err != nil {
		return Inputs{}, err
	}
	in.Excluded = append(in.Excluded, NotYet...)
	return in, nil
}

// dueOf is an open item's due date, else its date.
func dueOf(x subledger.Item) time.Time {
	if x.DueDate.IsZero() {
		return x.Date
	}
	return x.DueDate
}

// lateness adds each settlement's days late to its party's.
func lateness(by map[uuid.UUID][]int, ss []subledger.Settlement) {
	for _, s := range ss {
		if s.DueDate.IsZero() || s.Party == uuid.Nil {
			continue
		}
		by[s.Party] = append(by[s.Party], int(s.On.Sub(s.DueDate).Hours()/24))
	}
}

// occurrence is what one occurrence of a recurring document comes to: its party, its total in the base currency
// (above zero), and its payment terms in days.
type occurrence struct {
	party     uuid.UUID
	partyName string
	total     money.Money
	terms     int
}

// nextInvoice is what a recurring invoice's content comes to if it were dated today: its customer, its total at
// today's rate and GST, and the days to its due date. why says what stops it, such as a currency with no rate today.
func nextInvoice(ctx context.Context, u storage.UnitOfWork, base money.Currency, today time.Time, content []byte) (occurrence, string, error) {
	d, err := invoice.ReadContent(content, base)
	if err != nil {
		return occurrence{}, "its content cannot be read", nil
	}
	d.Date, d.DueDate = today, time.Time{}
	payload, err := invoice.Encode(d)
	if err != nil {
		return occurrence{}, err.Error(), nil
	}
	p, probs, err := invoice.Prepare(ctx, u, storage.Document{Type: storage.DocumentInvoice, Date: today.Format(time.DateOnly), Payload: payload}, base)
	if err != nil {
		return occurrence{}, "", err
	}
	if len(probs) > 0 {
		return occurrence{}, probs[0].Message, nil
	}
	t, err := ledger.BaseTotalsOf(p.Invoice, base)
	if err != nil {
		return occurrence{}, err.Error(), nil
	}
	return occurrence{party: d.CustomerID, partyName: p.Customer.LegalName, total: t.Total, terms: termsOf(today, p.DueDate)}, "", nil
}

// nextBill is nextInvoice for a recurring bill: what it owes the supplier at today's rate. Tax withheld from a
// non-resident supplier is paid apart, so it stays in.
func nextBill(ctx context.Context, u storage.UnitOfWork, base money.Currency, today time.Time, content []byte) (occurrence, string, error) {
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		return occurrence{}, "its content cannot be read", nil
	}
	payload["date"] = today.Format(time.DateOnly)
	delete(payload, "due_date")
	raw, err := json.Marshal(payload)
	if err != nil {
		return occurrence{}, "", err
	}
	p, probs, err := bill.Prepare(ctx, u, storage.Document{Type: storage.DocumentBill, Date: today.Format(time.DateOnly), Payload: raw}, base)
	var be *bill.Error
	switch {
	case errors.As(err, &be):
		return occurrence{}, be.Message, nil
	case err != nil:
		return occurrence{}, "", err
	case len(probs) > 0:
		return occurrence{}, probs[0].Message, nil
	}
	total, err := p.Draft.Total()
	if err != nil {
		return occurrence{}, "", err
	}
	if total.Currency() != base {
		if p.Bill.Rate == nil {
			return occurrence{}, "its currency has no rate for today", nil
		}
		if total, err = rounding.FX(total, *p.Bill.Rate); err != nil {
			return occurrence{}, "", err
		}
	}
	return occurrence{party: p.Draft.SupplierID, partyName: p.Supplier.LegalName, total: total, terms: termsOf(today, p.DueDate)}, "", nil
}

func termsOf(today, due time.Time) int {
	if due.IsZero() || due.Before(today) {
		return 0
	}
	return int(due.Sub(today).Hours() / 24)
}

// bufferOf is the buffer line on today (design D7): planning.buffer_months × the average monthly net burn (the KPI's
// net_burn) of the last three closed months, fewer while the books have fewer, rounded once (RND-PLAN).
func bufferOf(ctx context.Context, u storage.UnitOfWork, base money.Currency, today time.Time) (Buffer, error) {
	months, err := forecasts.BufferOn(ctx, u, today)
	if err != nil {
		return Buffer{}, err
	}
	out := Buffer{Months: months}
	all, err := u.Periods().List(ctx)
	if err != nil {
		return out, err
	}
	last := -1
	for i, p := range all {
		if p.Status == storage.PeriodClosed {
			last = i
		}
	}
	if last < 0 {
		out.Why = "no month is closed yet"
		return out, nil
	}
	revision, err := u.Ledger().Revision(ctx)
	if err != nil {
		return out, err
	}
	ms, _, err := kpi.Read(ctx, u, base, all, last, all[0].StartsOn, revision)
	if err != nil {
		return out, err
	}
	sum := new(apd.Decimal)
	c := apd.BaseContext.WithPrecision(60)
	for _, m := range ms {
		if _, err := c.Add(sum, sum, m.NetBurn.Decimal()); err != nil {
			return out, err
		}
	}
	avg, err := rounding.PlanQuotient(sum, apd.New(int64(len(ms)), 0), base.MinorUnits)
	if err != nil {
		return out, err
	}
	burn, err := money.FromRounded(avg, base)
	if err != nil {
		return out, err
	}
	out.Burn = &burn
	if sum.Sign() <= 0 {
		out.Why = "the company is not burning cash: the last closed month brought in more cash than it used"
		if len(ms) > 1 {
			out.Why = fmt.Sprintf("the company is not burning cash: the last %d closed months brought in more cash than they used", len(ms))
		}
		return out, nil
	}
	num := new(apd.Decimal)
	if _, err := c.Mul(num, sum, apd.New(int64(months), 0)); err != nil {
		return out, err
	}
	line, err := rounding.PlanQuotient(num, apd.New(int64(len(ms)), 0), base.MinorUnits)
	if err != nil {
		return out, err
	}
	l, err := money.FromRounded(line, base)
	if err != nil {
		return out, err
	}
	out.Line = &l
	return out, nil
}
