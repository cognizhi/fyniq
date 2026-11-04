package differential

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Fixed assets and disposals (README, "Fixed assets ..."): an asset in the register posts nothing itself; its months of
// depreciation are the scheduler's, and a disposal ends them. Each is read as the asset services read it.

// registered is an asset in the register: the document's place in the scenario (its depreciation is scheduled under it),
// what its schedule reads, its purchase day, and the month a disposal ended it (zero while it is in use).
type registered struct {
	index     int
	id        string
	account   uuid.UUID
	asset     ledger.DepreciableAsset
	purchased time.Time
	disposed  time.Time
}

type assetDoc struct {
	Account        string `json:"account"`
	Cost           string `json:"cost"`
	Residual       string `json:"residual"`
	LifeMonths     int    `json:"life_months"`
	DepreciateFrom string `json:"depreciate_from"`
}

// fixedAsset registers an asset as asset.Check reads it: on an active asset account, a cost above zero, a residual
// value of at most the cost, a life of 1 to asset.MaxLifeMonths months, depreciation starting no earlier than the month
// it was bought. The format leaves its source document out: one bought before the books start is an opening asset,
// whose months before then the opening balances carry (B.4), and it depreciates from the books start's month.
func (r *p1) fixedAsset(i int, d Document, date time.Time) bool {
	var x assetDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return false
	}
	if _, ok := r.setting("depreciation"); !ok {
		return false
	}
	if _, ok := r.setting("accumulated_depreciation"); !ok {
		return false
	}
	cost, ok := r.amount(x.Cost, r.base.Code)
	if !ok {
		return false
	}
	residual, ok := r.optional(x.Residual, r.base)
	if !ok {
		return false
	}
	acc, known := r.settings.Accounts[account(x.Account)]
	switch {
	case !known || acc.Type != "asset" || acc.Group || !acc.Active || acc.Money:
		return false
	case x.LifeMonths < 1 || x.LifeMonths > asset.MaxLifeMonths || cost.Sign() <= 0 || residual.Sign() < 0:
		return false
	}
	if above, err := residual.Cmp(cost); err != nil || above > 0 {
		return false
	}
	start := asset.Month(date)
	if x.DepreciateFrom != "" {
		from, _ := time.Parse(time.DateOnly, x.DepreciateFrom)
		if start = asset.Month(from); start.Before(asset.Month(date)) {
			return false
		}
	}
	a := ledger.DepreciableAsset{ID: stableID("document", d.ID), Version: max(d.Version, 1), Cost: cost, Residual: residual, Carried: money.Zero(r.base),
		From: start, End: start.AddDate(0, x.LifeMonths-1, 0)}
	if r.s.Settings.BooksStart != "" {
		books, _ := time.Parse(time.DateOnly, r.s.Settings.BooksStart)
		if books = asset.Month(books); a.From.Before(books) {
			// The months before the books start are the opening balances': ALLOC-EVEN's parts of the whole life for them.
			left, _ := cost.Sub(residual)
			parts, err := rounding.AllocEven(left, x.LifeMonths)
			if err != nil {
				return false
			}
			for m, k := start, 0; m.Before(books) && k < len(parts); m, k = m.AddDate(0, 1, 0), k+1 {
				a.Carried, _ = a.Carried.Add(parts[k])
			}
			a.From = books
		}
	}
	if _, err := ledger.DepreciationSchedule(a); err != nil {
		return false
	}
	r.assets[d.ID] = &registered{index: i, id: d.ID, account: acc.ID, asset: a, purchased: date}
	return true
}

// months is an asset's schedule up to the month before a disposal ended it.
func (a *registered) months() []ledger.Depreciation {
	all, _ := ledger.DepreciationSchedule(a.asset)
	if a.disposed.IsZero() {
		return all
	}
	var out []ledger.Depreciation
	for _, m := range all {
		if m.Month.Before(a.disposed) {
			out = append(out, m)
		}
	}
	return out
}

type disposalDoc struct {
	Asset    string `json:"asset"`
	Proceeds *struct {
		Account string `json:"account"`
		Amount  string `json:"amount"`
		GSTCode string `json:"gst_code"`
	} `json:"proceeds"`
}

// assetDisposal posts the disposal of a whole asset (asset_disposal@1) as the disposal service reads it: its cost off
// the asset account, its accumulated depreciation through the month before (the opening balances' included), and its
// proceeds: money received into a base-currency money account, bearing output GST at a supply code while registered
// (a code given always a supply code);
// an invoice's line on the disposal clearing account; or none, a write-off. No month from the disposal's on depreciates.
func (r *p1) assetDisposal(d Document, date time.Time) (ledger.Entry, bool) {
	var x disposalDoc
	if json.Unmarshal(d.Raw, &x) != nil {
		return ledger.Entry{}, false
	}
	a := r.assets[x.Asset]
	if a == nil || !a.disposed.IsZero() || date.Before(a.purchased) {
		return ledger.Entry{}, false
	}
	month := asset.Month(date)
	accumulated := a.asset.Carried
	for _, m := range a.months() {
		if m.Month.Before(month) {
			accumulated, _ = accumulated.Add(m.Amount)
		}
	}
	p := ledger.AssetDisposal{ID: stableID("document", d.ID), Version: max(d.Version, 1), Date: date, AssetAccountID: a.account, Cost: a.asset.Cost,
		Accumulated: accumulated, Gross: money.Zero(r.base), InvoiceNet: money.Zero(r.base)}
	p.DisposalAccountID, _ = r.setting("disposal")
	if x.Proceeds != nil {
		amount, ok := r.amount(x.Proceeds.Amount, r.base.Code)
		if !ok {
			return ledger.Entry{}, false
		}
		to := account(x.Proceeds.Account)
		if clearing, ok := r.setting("disposal_clearing"); ok && to == clearing {
			p.ClearingAccountID, p.InvoiceNet = &to, amount
		} else {
			p.MoneyAccountID, p.Gross = &to, amount
			// A code given is a supply code whatever the registration (B.6, rev 0.169); while registered the sale needs one.
			if r.registered(date) || x.Proceeds.GSTCode != "" {
				if p.GSTRate, ok = r.supplyRate(x.Proceeds.GSTCode, date); !ok {
					return ledger.Entry{}, false
				}
			}
		}
	}
	e, err := ledger.PostAssetDisposal(p, r.settings)
	if err != nil || !r.fits(e) {
		return ledger.Entry{}, false
	}
	a.disposed = month
	return e, true
}

// depreciation schedules every registered asset's months dated on or before as_of (ENG-05), one entry per asset and
// month, under the asset's place in the scenario.
func (r *p1) depreciation() bool {
	for _, a := range r.assets {
		for _, m := range a.months() {
			if m.Date.After(r.asOf) || m.Amount.IsZero() {
				continue
			}
			e, err := ledger.PostDepreciation(m, r.settings)
			if err != nil {
				return false
			}
			r.pending = append(r.pending, scheduled{index: a.index, doc: a.id, kind: "depreciation", entry: e})
		}
	}
	return true
}
