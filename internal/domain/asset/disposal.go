package asset

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of a disposal's refusals (ENG-23). ASSET-01 to ASSET-12 are an asset's.
const (
	CodeNotDisposal     = "ASSET-13" // the document is not a disposal
	CodeDisposalDate    = "ASSET-14" // a disposal dated before the asset was bought, after today, or before a month that cannot be reversed
	CodeDisposalShare   = "ASSET-15" // a share of the cost of nothing, or of more than the asset's cost
	CodeProceeds        = "ASSET-16" // proceeds that are not money received, an invoice's line on the clearing account, or none
	CodeDepreciationDue = "ASSET-17" // a month of depreciation before the disposal is due and not posted
	CodeDisposalVoid    = "ASSET-18" // a void of a disposal that is not posted, or after the asset changed or depreciated again
)

// The proceeds of a disposal (the owner's decision of 27 Oct 2026, rev 0.149).
const (
	ProceedsMoney   = "money"   // money received into a money account, recorded with the disposal and matched by bank import
	ProceedsInvoice = "invoice" // a sales invoice's line on the disposal clearing account, which the disposal clears
	ProceedsNone    = "none"    // a write-off
)

// The settings a disposal reads: the account its gain or loss goes to (recommended 7050), and the clearing account a
// sale on an invoice goes through, which the organisation names.
const (
	DisposalSetting = "assets.disposal_account"
	ClearingSetting = "assets.disposal_clearing_account"
)

// Disposal is one version of a disposal (FR-FA-03; design D4): the asset and the version of it it disposes of, its date,
// the share of the cost it takes (the whole cost when Whole), that share's accumulated depreciation through the month
// before and its residual value, the asset's whole accumulated depreciation before it (what a void restores), and its
// proceeds: money received into a money account (the chart account), with its GST code and the rate it bore; or an
// issued invoice's line on the clearing account, with what its entry put there; or none. It keeps the accounts it posts
// to as they stood when it was recorded, so its replay never depends on a setting changed later (ENG-06).
type Disposal struct {
	AssetID           uuid.UUID
	AssetVersion      int
	Date              time.Time
	Whole             bool
	Cost              money.Money
	Accumulated       money.Money
	Residual          money.Money
	Before            money.Money
	Proceeds          string
	MoneyAccountID    *uuid.UUID
	Gross             *money.Money
	GSTCode           string
	GSTRate           string
	InvoiceID         *uuid.UUID
	InvoiceLine       int
	InvoiceNet        *money.Money
	ClearingAccountID *uuid.UUID
	AssetAccountID    uuid.UUID
	DisposalAccountID uuid.UUID
	Notes             string
}

type disposalPayload struct {
	AssetID           uuid.UUID    `json:"asset_id"`
	AssetVersion      int          `json:"asset_version"`
	Date              string       `json:"date"`
	Whole             bool         `json:"whole,omitempty"`
	Cost              *money.Money `json:"cost"`
	Accumulated       *money.Money `json:"accumulated"`
	Residual          *money.Money `json:"residual"`
	Before            *money.Money `json:"before"`
	Proceeds          string       `json:"proceeds"`
	MoneyAccountID    *uuid.UUID   `json:"money_account_id,omitempty"`
	Gross             *money.Money `json:"gross,omitempty"`
	GSTCode           string       `json:"gst_code,omitempty"`
	GSTRate           string       `json:"gst_rate,omitempty"`
	InvoiceID         *uuid.UUID   `json:"invoice_id,omitempty"`
	InvoiceLine       int          `json:"invoice_line,omitempty"`
	InvoiceNet        *money.Money `json:"invoice_net,omitempty"`
	ClearingAccountID *uuid.UUID   `json:"clearing_account_id,omitempty"`
	AssetAccountID    uuid.UUID    `json:"asset_account_id"`
	DisposalAccountID uuid.UUID    `json:"disposal_account_id"`
	Notes             string       `json:"notes,omitempty"`
}

// EncodeDisposal is a disposal as its document stores it.
func EncodeDisposal(d Disposal) ([]byte, error) {
	if len(d.Notes) > MaxNotes {
		return nil, &Error{CodeTooLong, fmt.Sprintf("notes: %d characters; the most is %d", len(d.Notes), MaxNotes), "Shorten them"}
	}
	cost, acc, residual, before := d.Cost, d.Accumulated, d.Residual, d.Before
	return json.Marshal(disposalPayload{AssetID: d.AssetID, AssetVersion: d.AssetVersion, Date: d.Date.Format(time.DateOnly), Whole: d.Whole, Cost: &cost,
		Accumulated: &acc, Residual: &residual, Before: &before, Proceeds: d.Proceeds, MoneyAccountID: d.MoneyAccountID, Gross: d.Gross, GSTCode: d.GSTCode,
		GSTRate: d.GSTRate, InvoiceID: d.InvoiceID, InvoiceLine: d.InvoiceLine, InvoiceNet: d.InvoiceNet, ClearingAccountID: d.ClearingAccountID,
		AssetAccountID: d.AssetAccountID, DisposalAccountID: d.DisposalAccountID, Notes: d.Notes})
}

// IsDisposal says whether a document is a disposal.
func IsDisposal(doc storage.Document) bool { return doc.Type == storage.DocumentAssetDisposal }

// ReadDisposal reads a disposal version as it was saved.
func ReadDisposal(doc storage.Document) (Disposal, error) {
	if !IsDisposal(doc) {
		return Disposal{}, &Error{CodeNotDisposal, fmt.Sprintf("document %s is a %s, not a disposal", doc.ID, doc.Type), "Open it with its own module"}
	}
	var p disposalPayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil || p.Cost == nil || p.Accumulated == nil || p.Residual == nil || p.Before == nil {
		what := "it has no cost or accumulated depreciation"
		if err != nil {
			what = err.Error()
		}
		return Disposal{}, &Error{CodeBadContent, "the saved disposal cannot be read: " + what, "Record it again"}
	}
	on, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return Disposal{}, &Error{CodeBadContent, fmt.Sprintf("the saved disposal cannot be read: the date %q", p.Date), "Record it again"}
	}
	return Disposal{AssetID: p.AssetID, AssetVersion: p.AssetVersion, Date: on, Whole: p.Whole, Cost: *p.Cost, Accumulated: *p.Accumulated,
		Residual: *p.Residual, Before: *p.Before, Proceeds: p.Proceeds, MoneyAccountID: p.MoneyAccountID, Gross: p.Gross, GSTCode: p.GSTCode,
		GSTRate: p.GSTRate, InvoiceID: p.InvoiceID, InvoiceLine: p.InvoiceLine, InvoiceNet: p.InvoiceNet, ClearingAccountID: p.ClearingAccountID,
		AssetAccountID: p.AssetAccountID, DisposalAccountID: p.DisposalAccountID, Notes: p.Notes}, nil
}

// Rule is the disposal as asset_disposal@1 reads it: doc names it, and memo the asset (its number and name).
func (d Disposal) Rule(doc storage.Document, memo string, base money.Currency) ledger.AssetDisposal {
	what := "sold"
	if d.Proceeds == ProceedsNone {
		what = "written off"
	}
	if !d.Whole {
		what = "part " + what
	}
	r := ledger.AssetDisposal{ID: doc.ID, Version: doc.Version, Date: d.Date, Memo: fmt.Sprintf("%s: %s, %s", doc.Number, memo, what),
		AssetAccountID: d.AssetAccountID, Cost: d.Cost, Accumulated: d.Accumulated, DisposalAccountID: d.DisposalAccountID,
		Gross: money.Zero(base), InvoiceNet: money.Zero(base), GSTRate: d.GSTRate}
	switch d.Proceeds {
	case ProceedsMoney:
		r.MoneyAccountID = d.MoneyAccountID
		if d.Gross != nil {
			r.Gross = *d.Gross
		}
	case ProceedsInvoice:
		r.ClearingAccountID = d.ClearingAccountID
		if d.InvoiceNet != nil {
			r.InvoiceNet = *d.InvoiceNet
		}
	}
	return r
}

// Share splits what an asset carries between a share of its cost that is disposed of and what stays, under
// ALLOC-PRORATA by the two parts of the cost: the share's accumulated depreciation and residual value. A share of the
// whole cost takes all of both.
func Share(a Asset, cost, accumulated money.Money) (shareAccumulated, shareResidual money.Money, err error) {
	if cost.Equal(a.Cost) {
		return accumulated, a.Residual, nil
	}
	rest, err := a.Cost.Sub(cost)
	if err != nil {
		return money.Money{}, money.Money{}, err
	}
	w1, ok1 := cost.Minor()
	w2, ok2 := rest.Minor()
	if !ok1 || !ok2 || w1 <= 0 || w2 <= 0 {
		return money.Money{}, money.Money{}, fmt.Errorf("the share %s of a cost of %s does not split it", cost, a.Cost)
	}
	accParts, err := allocProRata(accumulated, w1, w2)
	if err != nil {
		return money.Money{}, money.Money{}, err
	}
	resParts, err := allocProRata(a.Residual, w1, w2)
	if err != nil {
		return money.Money{}, money.Money{}, err
	}
	return accParts, resParts, nil
}

// allocProRata is the first part of amount split under ALLOC-PRORATA by the weights w1 and w2.
func allocProRata(amount money.Money, w1, w2 int64) (money.Money, error) {
	parts, err := rounding.AllocProRata(amount, []int64{w1, w2})
	if err != nil {
		return money.Money{}, err
	}
	return parts[0], nil
}
