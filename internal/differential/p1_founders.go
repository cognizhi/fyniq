package differential

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The founders' money (README, "Costs before the books start" to "Conversions of founders' balances to shares"): each
// read as the director services read it before they call the engine. The engine checks the person against the
// scenario's people, and the accounts against their roles.

type founderDoc struct {
	Incurred       string `json:"incurred"`
	Person         string `json:"person"`
	Account        string `json:"account"`
	Gross          string `json:"gross"`
	GST            string `json:"gst"`
	ClaimGST       bool   `json:"claim_gst"`
	Amount         string `json:"amount"`
	Credit         string `json:"credit"`
	Class          string `json:"class"`
	Price          string `json:"price"`
	FairValuePrice string `json:"fair_value_price"`
	Difference     string `json:"difference"`
	Settlements    []struct {
		Account string `json:"account"`
		Amount  string `json:"amount"`
	} `json:"settlements"`
}

func (r *p1) founderDoc(d Document) (founderDoc, bool) {
	var x founderDoc
	return x, json.Unmarshal(d.Raw, &x) == nil
}

// preIncorporationCost posts a cost a founder paid before the books start, as the accountant decided it
// (pre_incorporation_cost@1): it posts on or after the books start and was paid before it; its GST claimed only while
// registered on the day it posts.
func (r *p1) preIncorporationCost(d Document, date time.Time) (ledger.Entry, bool) {
	x, ok := r.founderDoc(d)
	if !ok || r.s.Settings.BooksStart == "" {
		return ledger.Entry{}, false
	}
	books, _ := time.Parse(time.DateOnly, r.s.Settings.BooksStart)
	incurred, err := time.Parse(time.DateOnly, x.Incurred)
	if err != nil || date.Before(books) || !incurred.Before(books) {
		return ledger.Entry{}, false
	}
	gross, ok := r.amount(x.Gross, r.base.Code)
	if !ok {
		return ledger.Entry{}, false
	}
	c := ledger.PreIncorporationCost{ID: stableID("document", d.ID), Version: max(d.Version, 1), PostOn: date, Party: stableID("party", x.Person),
		Account: account(x.Account), Gross: gross, ClaimGST: x.ClaimGST, Registered: r.registered(date)}
	if x.GST != "" { // none shown leaves it unset
		if c.GST, ok = r.amount(x.GST, r.base.Code); !ok {
			return ledger.Entry{}, false
		}
	}
	e, err := ledger.PostPreIncorporationCost(c, r.settings)
	return e, err == nil
}

// directorFee posts a director's fee the shareholders voted, owed to the director on deferred remuneration
// (director_fee@1), on the day they became entitled to it.
func (r *p1) directorFee(d Document, date time.Time) (ledger.Entry, bool) {
	x, ok := r.founderDoc(d)
	if !ok {
		return ledger.Entry{}, false
	}
	amount, ok := r.amount(x.Amount, r.base.Code)
	if !ok {
		return ledger.Entry{}, false
	}
	e, err := ledger.PostDirectorFee(ledger.DirectorFee{ID: stableID("document", d.ID), Version: max(d.Version, 1), EntitledOn: date,
		Party: stableID("party", x.Person), Account: account(x.Account), Amount: amount}, r.settings)
	return e, err == nil
}

// directorWaiver posts what a director or shareholder waived of what the company owes them (director_settlement@1), on
// the day it was approved.
func (r *p1) directorWaiver(d Document, date time.Time) (ledger.Entry, bool) {
	x, ok := r.founderDoc(d)
	if !ok {
		return ledger.Entry{}, false
	}
	amount, ok := r.amount(x.Amount, r.base.Code)
	if !ok || !r.within(amount, x.Person, account(x.Account), date) { // at most what is owed (FR-EQ-05)
		return ledger.Entry{}, false
	}
	e, err := ledger.PostDirectorWaiver(ledger.DirectorWaiver{ID: stableID("document", d.ID), Version: max(d.Version, 1), PostOn: date,
		Party: stableID("party", x.Person), Account: account(x.Account), Credit: account(x.Credit), Amount: amount}, r.settings)
	return e, err == nil
}

// directorRepayment posts what a payment batch's release repaid a director or shareholder (director_repayment@1): each
// settlement off their account, the total from a base-currency money account.
func (r *p1) directorRepayment(d Document, date time.Time) (ledger.Entry, bool) {
	x, ok := r.founderDoc(d)
	if !ok {
		return ledger.Entry{}, false
	}
	rp := ledger.DirectorRepayment{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, Party: stableID("party", x.Person),
		Account: account(x.Account)}
	settles := map[uuid.UUID]money.Money{} // by account, together at most what is owed there (FR-EQ-05, PB-25)
	for i, s := range x.Settlements {
		amount, ok := r.amount(s.Amount, r.base.Code)
		if !ok {
			return ledger.Entry{}, false
		}
		sum, ok := settles[account(s.Account)]
		if !ok {
			sum = money.Zero(r.base)
		}
		total, err := sum.Add(amount)
		if err != nil || !r.within(total, x.Person, account(s.Account), date) {
			return ledger.Entry{}, false
		}
		settles[account(s.Account)] = total
		rp.Settlements = append(rp.Settlements, ledger.RepaidSettlement{SettlementID: stableID("settlement", fmt.Sprintf("%s/%d", d.ID, i+1)),
			Account: account(s.Account), Amount: amount})
	}
	e, _, err := ledger.PostDirectorRepayment(rp, r.settings)
	return e, err == nil
}

// directorConversion posts a founder's balance converted to shares (share_issue@1) as director.Settlement.ConversionRule
// reads it: RND-SHARE allots whole shares for the amount at the price, the converted amount comes off their account,
// and share capital is credited with it, or under the fair-value basis with round(shares × fair value per share) and
// the difference to the account chosen.
func (r *p1) directorConversion(d Document, date time.Time) (ledger.Entry, bool) {
	x, ok := r.founderDoc(d)
	if !ok {
		return ledger.Entry{}, false
	}
	amount, ok := r.amount(x.Amount, r.base.Code)
	if !ok || !r.within(amount, x.Person, account(x.Account), date) { // at most what is owed (FR-EQ-05)
		return ledger.Entry{}, false
	}
	price, err := money.ParseUnitPrice(x.Price, r.base, "price")
	if err != nil {
		return ledger.Entry{}, false
	}
	shares, err := rounding.Share(amount, price)
	if err != nil {
		return ledger.Entry{}, false
	}
	c := ledger.Conversion{ID: stableID("document", d.ID), PostOn: date, Party: stableID("party", x.Person), Account: account(x.Account),
		Class: account(x.Class), Converted: shares.Converted, Equity: shares.Converted}
	if x.FairValuePrice != "" {
		fair, err := money.ParseUnitPrice(x.FairValuePrice, r.base, "fair_value_price")
		if err != nil {
			return ledger.Entry{}, false
		}
		var q apd.Decimal
		q.Set(&shares.Count)
		n, err := money.ParseQuantity(q.Text('f'), "shares")
		if err != nil {
			return ledger.Entry{}, false
		}
		if c.Equity, err = rounding.Line(n, fair); err != nil {
			return ledger.Entry{}, false
		}
		c.Difference = account(x.Difference)
	}
	e, err := ledger.PostConversion(c, r.settings)
	if err == nil {
		r.stage(x.Person, x.Class, shares.Count, date) // its n shares join the person's holding (README, "Cap tables")
	}
	return e, err == nil
}

// shareIssueCash posts a cash issue to one subscriber made effective from share application monies (share_issue_cash@1),
// as journal.EquityTransactions reads it (FR-EQ-17, B.7): its amount is shares × price under RND-LINE, and it is refused
// while the subscriber's application monies at the end of its day are less (README, "Cash share issues").
func (r *p1) shareIssueCash(d Document, date time.Time) (ledger.Entry, bool) {
	var x struct {
		Person string `json:"person"`
		Class  string `json:"class"`
		Shares string `json:"shares"`
		Price  string `json:"price"`
	}
	if json.Unmarshal(d.Raw, &x) != nil || !wholeShares.MatchString(x.Shares) {
		return ledger.Entry{}, false
	}
	price, err := money.ParseUnitPrice(x.Price, r.base, "price")
	if err != nil || price.Sign() <= 0 {
		return ledger.Entry{}, false
	}
	q, err := money.ParseQuantity(x.Shares, "shares")
	if err != nil {
		return ledger.Entry{}, false
	}
	amount, err := rounding.Line(q, price)
	if err != nil {
		return ledger.Entry{}, false
	}
	var monies uuid.UUID
	for id, a := range r.settings.Accounts {
		if a.SubscriberAccount() && a.Active && !a.Group {
			monies = id
		}
	}
	if monies == uuid.Nil || !r.within(amount, x.Person, monies, date) { // never effective unpaid
		return ledger.Entry{}, false
	}
	e, err := ledger.PostCashIssue(ledger.CashIssue{ID: stableID("document", d.ID), PostOn: date, Party: stableID("party", x.Person), Monies: monies,
		Class: account(x.Class), Amount: amount}, r.settings)
	if err == nil {
		var n apd.Decimal
		n.Set(q.Decimal())
		r.stage(x.Person, x.Class, n, date)
	}
	return e, err == nil
}

// wholeShares is a whole number of shares above zero, as an equity transaction takes it.
var wholeShares = regexp.MustCompile(`^[1-9][0-9]{0,13}$`)
