package differential

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"

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
	if !ok {
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
	for i, s := range x.Settlements {
		amount, ok := r.amount(s.Amount, r.base.Code)
		if !ok {
			return ledger.Entry{}, false
		}
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
	if !ok {
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
	return e, err == nil
}
