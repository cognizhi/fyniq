package differential

import (
	"cmp"
	"encoding/json"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
)

// Bills, vendor credits and payments of bills (README, "Bills ..."): each read as the bill and payment services prepare
// them before they call the engine.

// openDocument is a posted document a settlement can settle, as the services find it: its total and rate as its entry
// put them on the control account, and what is open of it, in its currency and as a carrying amount in the base
// currency (the control line less every settlement's base amount).
type openDocument struct {
	kind     string
	total    money.Money
	rate     money.Rate
	open     money.Money
	carrying money.Money
}

// currency is a currency the scenario declares.
func (r *p1) currency(code string) (money.Currency, bool) {
	if _, ok := r.s.Currencies[code]; !ok {
		return money.Currency{}, false
	}
	c, err := money.Lookup(code)
	return c, err == nil
}

// optional reads an amount that may be left out, as zero.
func (r *p1) optional(s string, cur money.Currency) (money.Money, bool) {
	if s == "" {
		return money.Zero(cur), true
	}
	return r.amount(s, cur.Code)
}

// lineNet is a line's net: its amount, or RND-LINE of its quantity × unit price, the part after quantity_before when
// earlier bills took some of a purchase order's line. A line has an amount or a quantity and price, never both.
func lineNet(r *p1, cur money.Currency, amount, quantity, price, before string) (money.Money, bool) {
	switch {
	case amount != "" && (quantity != "" || price != "" || before != ""):
		return money.Money{}, false
	case amount != "":
		return r.amount(amount, cur.Code)
	case quantity == "" || price == "":
		return money.Money{}, false
	}
	q, err := money.ParseQuantity(quantity, "quantity")
	if err != nil {
		return money.Money{}, false
	}
	p, err := money.ParseUnitPrice(price, cur, "unit_price")
	if err != nil {
		return money.Money{}, false
	}
	if before == "" {
		net, err := rounding.Line(q, p)
		return net, err == nil
	}
	b, err := money.ParseQuantity(before, "quantity_before")
	if err != nil {
		return money.Money{}, false
	}
	net, err := rounding.LinePart(b, q, p)
	return net, err == nil
}

type billDoc struct {
	Currency     string `json:"currency"`
	FXRate       string `json:"fx_rate"`
	FXRateSource string `json:"fx_rate_source"`
	Lines        []struct {
		Account        string `json:"account"`
		Amount         string `json:"amount"`
		Quantity       string `json:"quantity"`
		UnitPrice      string `json:"unit_price"`
		QuantityBefore string `json:"quantity_before"`
		GSTCode        string `json:"gst_code"`
		GST            string `json:"gst"`
		GSTBase        string `json:"gst_base"`
		BusinessShare  string `json:"business_share"`
		SpreadMonths   int    `json:"spread_months"`
		SpreadFrom     string `json:"spread_from"`
	} `json:"lines"`
	Withholding *struct {
		Percent string `json:"percent"`
	} `json:"withholding"`
	OpeningItem bool `json:"opening_item"`
}

// bill posts a bill (bill@1) or, credit, a vendor credit (vendor_credit@1). Each line's input tax is decided by the
// bill service's own rules (FR-AP-20) for a tax invoice addressed to the company, the line's business share claimed
// as bill.Prepare claims it. Only a bill withholds tax (FR-AP-19), and its spread lines release monthly.
func (r *p1) bill(i int, d Document, date time.Time, credit bool) (ledger.Entry, bool) {
	var x billDoc
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
	b := ledger.Bill{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Currency: cur, Rate: rate}
	registered := r.registered(date)
	for _, l := range x.Lines {
		net, ok := lineNet(r, cur, l.Amount, l.Quantity, l.UnitPrice, l.QuantityBefore)
		if !ok {
			return ledger.Entry{}, false
		}
		gst, ok := r.optional(l.GST, cur)
		if !ok {
			return ledger.Entry{}, false
		}
		line := ledger.BillLine{AccountID: account(l.Account), Net: net, GST: gst}
		if l.GSTBase != "" {
			stated, ok := r.amount(l.GSTBase, r.base.Code)
			if !ok || cur == r.base {
				return ledger.Entry{}, false
			}
			line.StatedBase = &stated
		}
		if l.BusinessShare != "" && !rounding.IsShare(l.BusinessShare) {
			return ledger.Entry{}, false
		}
		dec, purchase := bill.DecideReceipt(bill.InputTax{}, gst, l.GSTCode, r.codes, registered, date, nil, r.base)
		if !purchase {
			return ledger.Entry{}, false
		}
		line.Claimable = dec.Condition == bill.Claimable && dec.Reason != bill.ReasonNoGST
		if share := cmp.Or(l.BusinessShare, "100"); line.Claimable && !isPercent(share, 100) {
			if isPercent(share, 0) {
				line.Claimable = false
			} else {
				line.Shares = []string{share}
			}
		}
		if l.SpreadMonths != 0 || l.SpreadFrom != "" {
			from := date
			if l.SpreadFrom != "" {
				from, _ = time.Parse(time.DateOnly, l.SpreadFrom)
			}
			line.Spread = &ledger.Spread{From: firstOfMonth(from), Months: l.SpreadMonths}
		}
		b.Lines = append(b.Lines, line)
	}
	if credit && x.OpeningItem {
		return ledger.Entry{}, false // a supplier's credit note is never an opening item
	}
	if credit {
		e, err := ledger.PostVendorCredit(b, r.settings)
		if err != nil || !r.fits(e) {
			return ledger.Entry{}, false
		}
		r.hold(d.ID, date, e, ledger.RoleAccountsPayable)
		return e, true
	}
	if x.Withholding != nil {
		b.Withholding = &ledger.Withholding{RatePercent: x.Withholding.Percent}
	}
	if x.OpeningItem { // FR-GL-06 (rev 0.257): received before the books start, it posts at the books start
		start, ok := r.booksStart()
		if !ok {
			return ledger.Entry{}, false
		}
		b.Opening = &start
		e, err := ledger.PostOpeningBill(b, r.settings)
		if err != nil || !r.fits(e) {
			return ledger.Entry{}, false
		}
		r.keepOpen(d.ID, "bill", e, ledger.RoleAccountsPayable)
		r.hold(d.ID, date, e, ledger.RoleAccountsPayable)
		return e, true
	}
	e, err := ledger.PostBill(b, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	releases, err := ledger.BillReleases(b, e, r.settings)
	if err != nil || !r.schedule(i, d.ID, releases, ledger.PostBillRelease) {
		return ledger.Entry{}, false
	}
	r.keepOpen(d.ID, "bill", e, ledger.RoleAccountsPayable)
	r.hold(d.ID, date, e, ledger.RoleAccountsPayable)
	return e, true
}

// keepOpen records a posted document's line on the control account with role, which settlements then settle.
func (r *p1) keepOpen(id, kind string, e ledger.Entry, role string) {
	for _, l := range e.Lines {
		if r.settings.Accounts[l.AccountID].Role != role {
			continue
		}
		total, carrying := l.Amount, l.Base
		if role != ledger.RoleAccountsReceivable { // a credit to a liability is what is owed
			total, carrying = total.Neg(), carrying.Neg()
		}
		r.open[id] = &openDocument{kind: kind, total: total, rate: l.Rate, open: total, carrying: carrying}
		return
	}
}

type settlementDoc struct {
	Account      string `json:"account"`
	Currency     string `json:"currency"`
	FXRate       string `json:"fx_rate"`
	FXRateSource string `json:"fx_rate_source"`
	BaseAmount   string `json:"base_amount"`
	Unapplied    string `json:"unapplied"`
	Parts        []struct {
		Document string `json:"document"`
		Amount   string `json:"amount"`
		InFull   bool   `json:"in_full"`
	} `json:"parts"`
}

// tolerance is the settlement tolerances on day (FR-BNK-08, open question 32): the settings' or the defaults, and
// what the month's earlier settlements cleared.
func (r *p1) tolerance(day time.Time) (subledger.Tolerance, bool) {
	set := map[string]string{}
	if r.s.Settings != nil {
		set = r.s.Settings.Tolerance
	}
	read := func(key, def string) (money.Money, bool) {
		m, err := money.ParseStored(cmp.Or(set[key], def), r.base)
		return m, err == nil
	}
	base, ok1 := read("base", "1.00")
	document, ok2 := read("per_document", "50.00")
	month, ok3 := read("per_month", "200.00")
	used, ok := r.used[day.Format("2006-01")]
	if !ok {
		used = money.Zero(r.base)
	}
	return subledger.Tolerance{Base: base, Minimum: base, Percent: cmp.Or(set["percent"], "3"), Document: document, Month: month, Used: used},
		ok1 && ok2 && ok3
}

// settlement reads a payment of documents of kind as the payment service prepares it (billpayment.Prepare): each part
// settles a posted document in the payment's currency, at most what it has open; the part that settles all that is
// open clears its carrying amount, and one in full within tolerance clears the rest as a difference.
func (r *p1) settlement(d Document, date time.Time, kind string) (ledger.BillPayment, bool) {
	var x settlementDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.BillPayment{}, false
	}
	cur, ok := r.currency(x.Currency)
	if !ok {
		return ledger.BillPayment{}, false
	}
	p := ledger.BillPayment{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Account: account(x.Account), Currency: cur}
	switch {
	case x.FXRate == "" && x.FXRateSource == "":
	case cur == r.base || x.FXRate == "" || x.FXRateSource == "":
		return ledger.BillPayment{}, false
	default:
		rate, err := money.ParseRate(x.FXRate, cur, r.base, x.FXRateSource)
		if err != nil {
			return ledger.BillPayment{}, false
		}
		p.Rate = &rate
	}
	if x.BaseAmount != "" {
		paid, ok := r.amount(x.BaseAmount, r.base.Code)
		if !ok {
			return ledger.BillPayment{}, false
		}
		p.Paid = &paid
	}
	if p.Unapplied, ok = r.optional(x.Unapplied, cur); !ok {
		return ledger.BillPayment{}, false
	}
	tol, ok := r.tolerance(date)
	if !ok {
		return ledger.BillPayment{}, false
	}
	seen := map[string]bool{}
	for _, part := range x.Parts {
		o := r.open[part.Document]
		if o == nil || o.kind != kind || seen[part.Document] || o.total.Currency() != cur {
			return ledger.BillPayment{}, false
		}
		seen[part.Document] = true
		amount, ok := r.amount(part.Amount, cur.Code)
		if !ok {
			return ledger.BillPayment{}, false
		}
		paid := ledger.PaidBill{BillID: stableID("document", part.Document), Amount: amount, StoredRate: o.rate}
		c, err := amount.Cmp(o.open)
		switch {
		case err != nil || c > 0:
			return ledger.BillPayment{}, false
		case c == 0:
			left := o.carrying
			paid.Clears = &left
		case part.InFull:
			short, diff, total, err := subledger.Difference(amount, o.open, o.total, o.rate)
			if err != nil {
				return ledger.BillPayment{}, false
			}
			if allowed, _, err := tol.Allows(diff, total, cur != r.base); err != nil || !allowed {
				return ledger.BillPayment{}, false
			}
			if tol.Take(diff) != nil {
				return ledger.BillPayment{}, false
			}
			left := o.carrying
			paid.Clears, paid.Short = &left, &short
		}
		p.Bills = append(p.Bills, paid)
	}
	return p, true
}

// settled takes what a posted settlement settled off its documents, and its tolerances off the month's cap.
func (r *p1) settled(date time.Time, settlements []ledger.Settlement, ids map[string]string) {
	month := date.Format("2006-01")
	for _, s := range settlements {
		r.moveHeld(ids[s.DocumentID.String()], date, s) // what the revaluation reads of it at a month's end
		o := r.open[ids[s.DocumentID.String()]]
		if o == nil {
			continue
		}
		o.open, _ = o.open.Sub(s.Amount)
		o.carrying, _ = o.carrying.Sub(s.Base)
		used, ok := r.used[month]
		if !ok {
			used = money.Zero(r.base)
		}
		r.used[month], _ = used.Add(s.Tolerance)
	}
}

// billPayment posts a payment of bills (bill_payment@1).
func (r *p1) billPayment(d Document, date time.Time) (ledger.Entry, bool) {
	p, ok := r.settlement(d, date, "bill")
	if !ok {
		return ledger.Entry{}, false
	}
	e, settlements, err := ledger.PostBillPayment(p, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	r.settled(date, settlements, r.idsOf(d))
	r.holdCredit(d.ID, date, e, ledger.RoleAccountsPayable, settlements)
	return e, true
}

// idsOf maps the engine's IDs of the documents a settlement names back to the scenario's.
func (r *p1) idsOf(d Document) map[string]string {
	var x struct {
		Parts []struct {
			Document string `json:"document"`
		} `json:"parts"`
	}
	_ = json.Unmarshal(d.Raw, &x)
	out := map[string]string{}
	for _, p := range x.Parts {
		out[stableID("document", p.Document).String()] = p.Document
	}
	return out
}

// isPercent says whether a share is exactly n percent.
func isPercent(share string, n int64) bool {
	d, _, err := apd.NewFromString(share)
	return err == nil && d.Cmp(apd.New(n, 0)) == 0
}

// firstOfMonth is the first day of t's month.
func firstOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

// booksStart is the scenario's books start, which an opening item posts at (FR-GL-06).
func (r *p1) booksStart() (time.Time, bool) {
	if r.s.Settings == nil || r.s.Settings.BooksStart == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.DateOnly, r.s.Settings.BooksStart)
	return t, err == nil
}
