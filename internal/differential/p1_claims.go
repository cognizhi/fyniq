package differential

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Expense claims, reimbursements and cash advances (README, "Expense claims ..."): each read as the claim services
// prepare them before they call the engine.

// advance is a posted cash advance as a claim or a refund finds it: what is still open of it, in the base currency.
type advance struct {
	open money.Money
}

// mileageRate is the rate per km for a vehicle in force on day (settings.mileage_rates), as claim.Prepare reads the
// organisation's mileage setting; false when none is set.
func (r *p1) mileageRate(vehicle string, day time.Time) (money.Money, bool) {
	var in MileageRate
	var since time.Time
	for _, x := range r.s.Settings.MileageRates {
		from, err := time.Parse(time.DateOnly, x.From)
		if x.Vehicle != vehicle || err != nil || from.After(day) || in.Rate != "" && from.Before(since) {
			continue
		}
		in, since = x, from
	}
	if in.Rate == "" {
		return money.Money{}, false
	}
	m, err := money.Parse(in.Rate, r.base)
	return m, err == nil
}

type claimDoc struct {
	Currency     string `json:"currency"`
	FXRate       string `json:"fx_rate"`
	FXRateSource string `json:"fx_rate_source"`
	Advance      string `json:"advance"`
	Loan         string `json:"loan"`
	Lines        []struct {
		Account       string `json:"account"`
		Amount        string `json:"amount"`
		Quantity      string `json:"quantity"`
		UnitPrice     string `json:"unit_price"`
		GSTCode       string `json:"gst_code"`
		GST           string `json:"gst"`
		GSTBase       string `json:"gst_base"`
		BusinessShare string `json:"business_share"`
		Distance      string `json:"distance"`
		Vehicle       string `json:"vehicle"`
	} `json:"lines"`
}

// expenseClaim posts an approved expense claim (expense_claim@1, or expense_claim@2 when kept as a loan) as
// claim.Prepare and the claim posting service read it: each receipt line its net and stated GST in the claim's currency,
// its input tax decided by the bill service's rules; each trip RND-MILEAGE of its distance at its vehicle's rate, in the
// base currency; and what it owes settling the advance it names first, up to what is open of it.
func (r *p1) expenseClaim(d Document, date time.Time) (ledger.Entry, bool) {
	var x claimDoc
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
	c := ledger.Claim{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date}
	registered := r.registered(date)
	for _, l := range x.Lines {
		if l.Distance != "" || l.Vehicle != "" { // a trip in the claimant's own vehicle, always in the base currency
			if cur != r.base {
				return ledger.Entry{}, false
			}
			perKm, ok := r.mileageRate(l.Vehicle, date)
			if !ok {
				return ledger.Entry{}, false
			}
			km, err := money.ParseQuantity(l.Distance, "distance")
			if err != nil {
				return ledger.Entry{}, false
			}
			_, amount, err := rounding.Mileage(km, perKm)
			if err != nil {
				return ledger.Entry{}, false
			}
			c.Lines = append(c.Lines, ledger.ClaimLine{AccountID: account(l.Account), Amount: amount, GST: money.Zero(r.base), Mileage: true})
			continue
		}
		net, ok := lineNet(r, cur, l.Amount, l.Quantity, l.UnitPrice, "")
		if !ok {
			return ledger.Entry{}, false
		}
		gst, ok := r.optional(l.GST, cur)
		if !ok {
			return ledger.Entry{}, false
		}
		// A claim's receipt has no business share in the product: the claim service claims its GST whole or not at all.
		if l.BusinessShare != "" && !isPercent(l.BusinessShare, 100) {
			return ledger.Entry{}, false
		}
		dec, purchase := bill.DecideReceipt(bill.InputTax{}, gst, l.GSTCode, r.codes, registered, date, nil, r.base)
		if !purchase {
			return ledger.Entry{}, false
		}
		gross, err := net.Add(gst)
		if err != nil {
			return ledger.Entry{}, false
		}
		line := ledger.ClaimLine{AccountID: account(l.Account), Amount: gross, GST: gst, Rate: rate,
			Claimable: dec.Condition == bill.Claimable && dec.Reason != bill.ReasonNoGST}
		if l.GSTBase != "" {
			stated, ok := r.amount(l.GSTBase, r.base.Code)
			if !ok || cur == r.base {
				return ledger.Entry{}, false
			}
			line.StatedBase = &stated
		}
		c.Lines = append(c.Lines, line)
	}
	post := ledger.PostExpenseClaim
	if x.Loan != "" {
		if x.Advance != "" || !slices.Contains(r.s.People, x.Loan) {
			return ledger.Entry{}, false
		}
		c.Loan, post = stableID("party", x.Loan), ledger.PostExpenseClaimLoan
	}
	var adv *advance
	if x.Advance != "" {
		if adv = r.advances[x.Advance]; adv == nil || adv.open.Sign() <= 0 {
			return ledger.Entry{}, false
		}
		c.Advances, _ = r.setting("advances")
	}
	e, err := post(c, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	if adv != nil { // what it settles of the advance: the least of what it owes and what is open (FR-AP-17)
		owed := money.Zero(r.base)
		for _, l := range e.Lines {
			if r.settings.Accounts[l.AccountID].Role == ledger.RoleExpenseClaimsPayable {
				owed, _ = owed.Add(l.Base.Neg())
			}
		}
		if owed.Sign() > 0 {
			settles := owed
			if over, err := owed.Cmp(adv.open); err == nil && over > 0 {
				settles = adv.open
			}
			c.Settles = &settles
			if e, err = post(c, r.settings); err != nil || !r.fits(e) {
				return ledger.Entry{}, false
			}
			adv.open, _ = adv.open.Sub(settles)
		}
	}
	r.keepOpen(d.ID, "claim", e, ledger.RoleExpenseClaimsPayable)
	return e, true
}

// claimReimbursement posts a reimbursement of claims (claim_reimbursement@1) as claim.PrepareReimbursement reads it: in
// the base currency, from a money account, each part at most what is still owed of a claim on 2150; nothing in full and
// nothing unapplied.
func (r *p1) claimReimbursement(d Document, date time.Time) (ledger.Entry, bool) {
	var x settlementDoc
	if json.Unmarshal(d.Raw, &x) != nil || x.Currency != r.base.Code || x.FXRate != "" || x.FXRateSource != "" || x.BaseAmount != "" || x.Unapplied != "" {
		return ledger.Entry{}, false
	}
	re := ledger.ClaimReimbursement{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Account: account(x.Account)}
	seen := map[string]bool{}
	for _, part := range x.Parts {
		o := r.open[part.Document]
		if o == nil || o.kind != "claim" || seen[part.Document] || part.InFull {
			return ledger.Entry{}, false
		}
		seen[part.Document] = true
		amount, ok := r.amount(part.Amount, r.base.Code)
		if !ok {
			return ledger.Entry{}, false
		}
		if c, err := amount.Cmp(o.open); err != nil || c > 0 {
			return ledger.Entry{}, false
		}
		re.Claims = append(re.Claims, ledger.PaidClaim{ClaimID: stableID("document", part.Document), Amount: amount})
	}
	e, settlements, err := ledger.PostClaimReimbursement(re, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	r.settled(date, settlements, r.idsOf(d))
	return e, true
}

type advanceDoc struct {
	Account  string `json:"account"`
	Amount   string `json:"amount"`
	RefundOf string `json:"refund_of"`
}

// cashAdvance posts a cash advance or, with refund_of, the refund of what is left of one (cash_advance@1), as
// claim.PrepareAdvance reads it: a refund names a posted advance with at least its amount still open.
func (r *p1) cashAdvance(d Document, date time.Time) (ledger.Entry, bool) {
	var x advanceDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	amount, ok := r.amount(x.Amount, r.base.Code)
	if !ok {
		return ledger.Entry{}, false
	}
	a := ledger.CashAdvance{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Account: account(x.Account), Amount: amount}
	a.Advances, _ = r.setting("advances")
	var of *advance
	if x.RefundOf != "" {
		if of = r.advances[x.RefundOf]; of == nil {
			return ledger.Entry{}, false
		}
		if c, err := amount.Cmp(of.open); err != nil || c > 0 {
			return ledger.Entry{}, false
		}
		a.Refund = true
	}
	e, err := ledger.PostCashAdvance(a, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	if of != nil {
		of.open, _ = of.open.Sub(amount)
	} else {
		r.advances[d.ID] = &advance{open: amount}
	}
	return e, true
}
