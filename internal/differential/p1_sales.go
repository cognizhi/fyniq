package differential

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/deposit"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Invoices, credit notes, receipts, deposits, refunds and write-offs (README, "Invoices ..."): each read as the sales
// services prepare them before they call the engine.

// issued is a posted invoice as a credit note finds it: its date (the credit note's tax point), and what it charged in
// its currency, before any deposit it deducts, and what its issued credit notes credited of that (FR-AR-04, rev 0.155).
type issued struct {
	date     time.Time
	charged  money.Money
	credited money.Money
}

// supplyRate is the GST rate a supply coded code bears on day, as invoice.Prepare and deposit.Prepare decide it: a
// supply code only, its standard rate while registered, "0" for a code the return reports at no GST, none otherwise.
func (r *p1) supplyRate(code string, day time.Time) (string, bool) {
	c, ok := pack.Code(r.codes, code)
	if !ok || c.Direction != pack.DirSupply {
		return "", false
	}
	return r.gstRate(code, day)
}

type invoiceDoc struct {
	Currency     string `json:"currency"`
	FXRate       string `json:"fx_rate"`
	FXRateSource string `json:"fx_rate_source"`
	Invoice      string `json:"invoice"`
	OpeningItem  bool   `json:"opening_item"`
	Lines        []struct {
		Account      string `json:"account"`
		Amount       string `json:"amount"`
		Quantity     string `json:"quantity"`
		UnitPrice    string `json:"unit_price"`
		Discount     string `json:"discount"`
		GSTCode      string `json:"gst_code"`
		SpreadMonths int    `json:"spread_months"`
		SpreadFrom   string `json:"spread_from"`
	} `json:"lines"`
	Deposits []struct {
		Deposit string `json:"deposit"`
		Amount  string `json:"amount"`
	} `json:"deposits"`
}

// invoice posts an invoice (invoice@1) or, credit, a credit note (credit_note@1), as invoice.Prepare reads it: a credit
// note that names an invoice takes its GST at that invoice's date and credits at most what the invoice's other credit
// notes leave of its total; an invoice deducts posted deposits in its currency, each at most what is left of it, taking
// its RND-DEPOSIT parts. An invoice's spread lines release monthly.
func (r *p1) invoice(i int, d Document, date time.Time, credit bool) (ledger.Entry, bool) {
	var x invoiceDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	cur, ok := r.currency(x.Currency)
	if !ok {
		return ledger.Entry{}, false
	}
	rate, ok := r.rate(cur, x.FXRate, x.FXRateSource)
	if !ok {
		return ledger.Entry{}, false
	}
	taxPoint := date
	var of *issued
	if x.Invoice != "" {
		of = r.issued[x.Invoice]
		if !credit || of == nil || of.charged.Currency() != cur || date.Before(of.date) {
			return ledger.Entry{}, false
		}
		taxPoint = of.date
	}
	inv := ledger.Invoice{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Currency: cur, Rate: rate}
	if r.s.Settings.GST != nil && r.s.Settings.GST.PerLine {
		inv.GSTMethod = rounding.GSTPerLine
	}
	for _, l := range x.Lines {
		var net money.Money
		if l.Amount != "" && l.Discount != "" {
			return ledger.Entry{}, false
		}
		if l.Amount != "" || l.Discount == "" {
			if net, ok = lineNet(r, cur, l.Amount, l.Quantity, l.UnitPrice, ""); !ok {
				return ledger.Entry{}, false
			}
		} else {
			q, err := money.ParseQuantity(l.Quantity, "quantity")
			if err != nil {
				return ledger.Entry{}, false
			}
			p, err := money.ParseUnitPrice(l.UnitPrice, cur, "unit_price")
			if err != nil {
				return ledger.Entry{}, false
			}
			if net, err = rounding.LineDiscounted(q, p, l.Discount); err != nil {
				return ledger.Entry{}, false
			}
		}
		gst, ok := r.supplyRate(l.GSTCode, taxPoint)
		if !ok {
			return ledger.Entry{}, false
		}
		line := ledger.InvoiceLine{AccountID: account(l.Account), Net: net, GSTCode: l.GSTCode, GSTRate: gst}
		if l.SpreadMonths != 0 || l.SpreadFrom != "" {
			from := date
			if l.SpreadFrom != "" {
				from, _ = time.Parse(time.DateOnly, l.SpreadFrom)
			}
			line.Spread = &ledger.Spread{From: firstOfMonth(from), Months: l.SpreadMonths}
		}
		inv.Lines = append(inv.Lines, line)
	}
	if credit && len(x.Deposits) > 0 {
		return ledger.Entry{}, false
	}
	type use struct {
		open   *deposit.Open
		amount money.Money
		base   money.Money
		tax    money.Money
	}
	var uses []use
	seen := map[string]bool{}
	for _, dep := range x.Deposits {
		o := r.deposits[dep.Deposit]
		if o == nil || seen[dep.Deposit] || o.Total.Currency() != cur {
			return ledger.Entry{}, false
		}
		seen[dep.Deposit] = true
		amount, ok := r.amount(dep.Amount, cur.Code)
		if !ok || amount.Sign() <= 0 {
			return ledger.Entry{}, false
		}
		if c, err := amount.Cmp(o.Left); err != nil || c > 0 {
			return ledger.Entry{}, false
		}
		base, tax, err := o.Use(amount)
		if err != nil {
			return ledger.Entry{}, false
		}
		inv.Deposits = append(inv.Deposits, ledger.InvoiceDeposit{DepositID: stableID("document", dep.Deposit), Amount: amount, Base: base, Tax: tax})
		uses = append(uses, use{o, amount, base, tax})
	}
	totals, err := ledger.TotalsOf(inv)
	if err != nil {
		return ledger.Entry{}, false
	}
	if of != nil {
		left, err := of.charged.Sub(of.credited)
		if err != nil {
			return ledger.Entry{}, false
		}
		if c, err := totals.Total.Cmp(left); err != nil || c > 0 {
			return ledger.Entry{}, false
		}
	}
	if x.OpeningItem { // FR-GL-06 (rev 0.257): issued before the books start, it posts at the books start
		start, ok := r.booksStart()
		if !ok {
			return ledger.Entry{}, false
		}
		inv.Opening = &start
		if credit { // rev 0.261: a credit note open at the books start
			e, err := ledger.PostOpeningCreditNote(inv, r.settings)
			if err != nil || !r.fits(e) {
				return ledger.Entry{}, false
			}
			r.hold(d.ID, date, e, ledger.RoleAccountsReceivable)
			return e, true
		}
		e, err := ledger.PostOpeningInvoice(inv, r.settings)
		if err != nil || !r.fits(e) {
			return ledger.Entry{}, false
		}
		r.issued[d.ID] = &issued{date: date, charged: totals.Total, credited: money.Zero(cur)}
		r.keepOpen(d.ID, "invoice", e, ledger.RoleAccountsReceivable)
		r.hold(d.ID, date, e, ledger.RoleAccountsReceivable)
		return e, true
	}
	if credit {
		e, err := ledger.PostCreditNote(inv, r.settings)
		if err != nil || !r.fits(e) {
			return ledger.Entry{}, false
		}
		if of != nil {
			of.credited, _ = of.credited.Add(totals.Total)
		}
		r.hold(d.ID, date, e, ledger.RoleAccountsReceivable)
		return e, true
	}
	e, err := ledger.PostInvoice(inv, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	releases, err := ledger.InvoiceReleases(inv, e, r.settings)
	if err != nil || !r.schedule(i, d.ID, releases, ledger.PostInvoiceRelease) {
		return ledger.Entry{}, false
	}
	for _, u := range uses {
		u.open.Used, _ = u.open.Used.Add(u.amount)
		u.open.Left, _ = u.open.Left.Sub(u.amount)
		u.open.BaseLeft, _ = u.open.BaseLeft.Sub(u.base)
		u.open.GSTLeft, _ = u.open.GSTLeft.Sub(u.tax)
	}
	r.issued[d.ID] = &issued{date: date, charged: totals.Total, credited: money.Zero(cur)}
	r.keepOpen(d.ID, "invoice", e, ledger.RoleAccountsReceivable)
	r.hold(d.ID, date, e, ledger.RoleAccountsReceivable)
	return e, true
}

type depositDoc struct {
	Account      string `json:"account"`
	Currency     string `json:"currency"`
	Amount       string `json:"amount"`
	FXRate       string `json:"fx_rate"`
	FXRateSource string `json:"fx_rate_source"`
	Kind         string `json:"kind"`
	GSTCode      string `json:"gst_code"`
}

// customerDeposit posts a customer deposit (customer_deposit@1) as deposit.Prepare reads it, and keeps it open for the
// invoices that deduct it and the refunds that pay it back.
func (r *p1) customerDeposit(d Document, date time.Time) (ledger.Entry, bool) {
	var x depositDoc
	if json.Unmarshal(d.Raw, &x) != nil || !slices.Contains(deposit.Kinds, x.Kind) {
		return ledger.Entry{}, false
	}
	amount, ok := r.amount(x.Amount, x.Currency)
	if !ok {
		return ledger.Entry{}, false
	}
	rate, ok := r.rate(amount.Currency(), x.FXRate, x.FXRateSource)
	if !ok {
		return ledger.Entry{}, false
	}
	dep := ledger.CustomerDeposit{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Account: account(x.Account),
		Currency: amount.Currency(), Rate: rate, Amount: amount}
	switch x.Kind {
	case deposit.KindSecurity:
		if x.GSTCode != "" {
			return ledger.Entry{}, false
		}
	case deposit.KindPartPayment:
		if dep.GSTRate, ok = r.supplyRate(x.GSTCode, date); !ok {
			return ledger.Entry{}, false
		}
	}
	e, err := ledger.PostCustomerDeposit(dep, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	// What its entry put on the money account, 2200 and 2410, as deposit.OpenDeposit reads them.
	o := &deposit.Open{Kind: x.Kind, Total: amount, Base: money.Zero(r.base), GST: money.Zero(r.base), Net: money.Zero(r.base),
		Used: money.Zero(amount.Currency()), Left: amount}
	for _, l := range e.Lines {
		switch {
		case l.AccountID == dep.Account:
			o.Base = l.Base
		case r.settings.Accounts[l.AccountID].Role == ledger.RoleCustomerDeposits:
			o.Net = l.Base.Neg()
		case r.settings.Accounts[l.AccountID].Role == ledger.RoleGSTControl:
			o.GST = l.Base.Neg()
		}
	}
	o.BaseLeft, o.GSTLeft = o.Base, o.GST
	r.deposits[d.ID] = o
	return e, true
}

// customerReceipt posts money a customer paid (customer_receipt@1): a settlement of open invoices, as a payment of
// bills settles bills, the month's tolerance cap shared with them.
func (r *p1) customerReceipt(d Document, date time.Time) (ledger.Entry, bool) {
	p, ok := r.settlement(d, date, "invoice")
	if !ok {
		return ledger.Entry{}, false
	}
	rc := ledger.CustomerReceipt{ID: p.ID, Version: p.Version, Date: p.Date, Account: p.Account, Currency: p.Currency, Rate: p.Rate, Received: p.Paid,
		Unapplied: p.Unapplied}
	for _, b := range p.Bills {
		rc.Invoices = append(rc.Invoices, ledger.ReceivedInvoice{InvoiceID: b.BillID, Amount: b.Amount, StoredRate: b.StoredRate, Clears: b.Clears, Short: b.Short})
	}
	e, settlements, err := ledger.PostCustomerReceipt(rc, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	r.settled(date, settlements, r.idsOf(d))
	r.holdCredit(d.ID, date, e, ledger.RoleAccountsReceivable, settlements)
	return e, true
}

type refundDoc struct {
	Deposit      string `json:"deposit"`
	Account      string `json:"account"`
	Amount       string `json:"amount"`
	FXRate       string `json:"fx_rate"`
	FXRateSource string `json:"fx_rate_source"`
}

// depositRefund posts a refund of a customer deposit (deposit_refund@1) as the refund service prepares it: at most
// what is left of the deposit, taking its RND-DEPOSIT parts of the deposit's base amount and GST.
func (r *p1) depositRefund(d Document, date time.Time) (ledger.Entry, bool) {
	var x refundDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	o := r.deposits[x.Deposit]
	if o == nil {
		return ledger.Entry{}, false
	}
	cur := o.Total.Currency()
	amount, ok := r.amount(x.Amount, cur.Code)
	if !ok {
		return ledger.Entry{}, false
	}
	rate, ok := r.rate(cur, x.FXRate, x.FXRateSource)
	if !ok {
		return ledger.Entry{}, false
	}
	rf := ledger.CustomerRefund{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Account: account(x.Account), Currency: cur, Rate: rate,
		Credit: ledger.PaidBill{BillID: stableID("document", x.Deposit), Amount: amount, StoredRate: money.Identity(r.base)}}
	var part ledger.DepositPart
	if amount.Sign() > 0 {
		if c, err := amount.Cmp(o.Left); err != nil || c > 0 {
			return ledger.Entry{}, false
		}
		base, tax, err := o.Use(amount)
		if err != nil {
			return ledger.Entry{}, false
		}
		part = ledger.DepositPart{Base: base, Tax: tax}
		rf.Deposit = &part
	}
	e, _, err := ledger.PostDepositRefund(rf, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	o.Used, _ = o.Used.Add(amount)
	o.Left, _ = o.Left.Sub(amount)
	o.BaseLeft, _ = o.BaseLeft.Sub(part.Base)
	o.GSTLeft, _ = o.GSTLeft.Sub(part.Tax)
	return e, true
}

type writeOffDoc struct {
	Invoice string `json:"invoice"`
	Amount  string `json:"amount"`
	Account string `json:"account"`
}

// writeOff posts the write-off of what is left of an invoice (write_off@1) as writeoff.Prepare reads it: at most what is
// open, clearing its carrying amount when it takes all of it.
func (r *p1) writeOff(d Document, date time.Time) (ledger.Entry, bool) {
	var x writeOffDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	o := r.open[x.Invoice]
	if o == nil || o.kind != "invoice" {
		return ledger.Entry{}, false
	}
	amount, ok := r.amount(x.Amount, o.total.Currency().Code)
	if !ok {
		return ledger.Entry{}, false
	}
	w := ledger.WriteOff{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, InvoiceID: stableID("document", x.Invoice), Amount: amount,
		StoredRate: o.rate, Account: account(x.Account)}
	switch c, err := amount.Cmp(o.open); {
	case err != nil || c > 0:
		return ledger.Entry{}, false
	case c == 0:
		left := o.carrying
		w.Clears = &left
	}
	e, s, err := ledger.PostWriteOff(w, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	r.settled(date, []ledger.Settlement{s}, map[string]string{w.InvoiceID.String(): x.Invoice})
	return e, true
}
