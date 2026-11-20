package journal

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MovementRow is one asset's line of the asset movement schedule (FR-FA-04): its cost and its accumulated depreciation at the start of the run, what moved, and at the end.
// Closing = opening + additions − disposals for the cost, and closing = opening + charge − disposals for the accumulated depreciation, exactly.
type MovementRow struct {
	AssetID                                           uuid.UUID
	Number, Name                                      string
	AccountID                                         uuid.UUID
	PurchasedOn                                       time.Time
	OpeningCost, Additions, DisposedCost, ClosingCost money.Money
	OpeningAcc, Charge, DisposedAcc, ClosingAcc       money.Money
}

// NBVOpening is the asset's net book value at the start of the run.
func (r MovementRow) NBVOpening() money.Money { n, _ := r.OpeningCost.Sub(r.OpeningAcc); return n }

// NBVClosing is the asset's net book value at the end of the run.
func (r MovementRow) NBVClosing() money.Money { n, _ := r.ClosingCost.Sub(r.ClosingAcc); return n }

// AssetMovement is the movement schedule of the assets from one day to another, tied to the ledger.
type AssetMovement struct {
	Rows []MovementRow
	// Register is the register reconciled with the ledger at the end of the run (INV-07): each asset account of the register and 1590, through the balance service (ENG-04).
	Register []RegisterAccount
}

// FixedAssetMovement is the movement schedule of the registered assets from the day from to the day to, both inclusive (FR-FA-04): each asset's cost and accumulated depreciation
// as at the day before the run and at its end, as AssetFiguresAt reads them from the documents and the entries; the cost added by an asset bought inside the run; and the
// depreciation the run charged (the entries dated inside it, net of their reversals). What left, by a disposal, is what is left over, so the columns foot exactly. The register
// at the end is reconciled with the ledger, so that what the register does not explain (a line on an asset account in no asset) is shown, not hidden (INV-07).
func FixedAssetMovement(ctx context.Context, u storage.UnitOfWork, from, to time.Time) (AssetMovement, error) {
	s, err := loadSettings(ctx, u)
	if err != nil {
		return AssetMovement{}, err
	}
	zero := money.Zero(s.Base)
	opening, err := AssetFiguresAt(ctx, u, s.Base, from.AddDate(0, 0, -1))
	if err != nil {
		return AssetMovement{}, err
	}
	closing, err := AssetFiguresAt(ctx, u, s.Base, to)
	if err != nil {
		return AssetMovement{}, err
	}
	before := map[uuid.UUID]AssetFigures{}
	for _, f := range opening {
		before[f.Asset.ID] = f
	}
	var out AssetMovement
	for _, f := range closing {
		a, err := asset.Read(f.Asset)
		if err != nil {
			return AssetMovement{}, err
		}
		o, had := before[f.Asset.ID]
		r := MovementRow{AssetID: f.Asset.ID, Number: f.Asset.Number, Name: a.Name, AccountID: f.AccountID, PurchasedOn: f.PurchasedOn,
			OpeningCost: zero, Additions: zero, OpeningAcc: zero, ClosingCost: f.Cost, ClosingAcc: f.Accumulated}
		if had {
			r.OpeningCost, r.OpeningAcc = o.Cost, o.Accumulated
		} else if !f.Opening && !f.PurchasedOn.Before(from) {
			r.Additions = f.Bought // bought inside the run
		}
		entries, err := u.Ledger().OfSource(ctx, f.Asset.ID)
		if err != nil {
			return AssetMovement{}, err
		}
		r.Charge = zero
		for _, e := range entries {
			if e.Source.Type != ledger.SourceAssetDepreciation || e.PostingDate.Before(from) || e.PostingDate.After(to) {
				continue
			}
			part, err := depreciated(e, s.Base)
			if err != nil {
				return AssetMovement{}, err
			}
			if e.ReversesEntryID != nil {
				part = part.Neg()
			}
			if r.Charge, err = r.Charge.Add(part); err != nil {
				return AssetMovement{}, err
			}
		}
		// What a disposal took off inside the run: the cost and the accumulated depreciation that are left over.
		cost, _ := r.OpeningCost.Add(r.Additions)
		r.DisposedCost, _ = cost.Sub(r.ClosingCost)
		acc, _ := r.OpeningAcc.Add(r.Charge)
		r.DisposedAcc, _ = acc.Sub(r.ClosingAcc)
		out.Rows = append(out.Rows, r)
	}
	slices.SortStableFunc(out.Rows, func(a, b MovementRow) int {
		return strings.Compare(a.Number, b.Number)
	})
	if out.Register, err = RegisterAt(ctx, u, to); err != nil {
		return AssetMovement{}, err
	}
	return out, nil
}

// TaxAsset is what the capital allowance schedule needs of an asset (FR-FA-04): the cost it was bought for, the day, the election, and whether any disposal of it stands.
type TaxAsset struct {
	AssetID     uuid.UUID
	Number      string
	Name        string
	AccountID   uuid.UUID
	PurchasedOn time.Time
	Cost        money.Money
	Election    *asset.TaxElection
	Disposed    bool
	// Sale is the one live disposal of the whole asset, with its proceeds net of GST in the base currency; nil for an asset not sold whole (a part disposal leaves Disposed set
	// and Sale nil, and the schedule lists it apart).
	Sale *TaxSale
}

// TaxSale is the sale of a whole asset as the capital allowance schedule reads it (FR-FA-04, rev 0.603): its day and its proceeds net of GST.
type TaxSale struct {
	On       time.Time
	Proceeds money.Money
}

// taxSales are the assets sold whole by one live disposal, by asset: the proceeds are the money received less its output GST (RND-GST-INCL), the invoice line's net, or nothing for a
// write-off. An asset with more than one live disposal, or one of a share of its cost, has none.
func taxSales(ctx context.Context, u storage.UnitOfWork, base money.Currency) (map[uuid.UUID]*TaxSale, error) {
	live := map[uuid.UUID][]asset.Disposal{}
	forever := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	if err := subledger.Pages(ctx, u, storage.DocumentAssetDisposal, forever, func(doc storage.Document) error {
		if doc.Status != storage.DocumentPosted {
			return nil
		}
		voided, err := subledger.VoidedBy(ctx, u, ledger.AssetDisposalIdentity(doc.ID, doc.Version, ledger.RuleAssetDisposal), forever)
		if err != nil || voided {
			return err
		}
		x, err := asset.ReadDisposal(doc)
		if err != nil {
			return err
		}
		live[x.AssetID] = append(live[x.AssetID], x)
		return nil
	}); err != nil {
		return nil, err
	}
	out := map[uuid.UUID]*TaxSale{}
	for id, ds := range live {
		if len(ds) != 1 || !ds[0].Whole {
			continue
		}
		d := ds[0]
		net := money.Zero(base)
		switch d.Proceeds {
		case asset.ProceedsMoney:
			if d.Gross != nil {
				net = *d.Gross
				if d.GSTRate != "" {
					gst, err := rounding.GSTIncl(*d.Gross, d.GSTRate)
					if err != nil {
						return nil, err
					}
					if net, err = net.Sub(gst); err != nil {
						return nil, err
					}
				}
			}
		case asset.ProceedsInvoice:
			if d.InvoiceNet != nil {
				net = *d.InvoiceNet
			}
		}
		out[id] = &TaxSale{On: d.Date, Proceeds: net}
	}
	return out, nil
}

// TaxAssets are the registered assets as the capital allowance schedule reads them, in number order: each asset's cost is the one it was bought for, an asset with a live
// disposal of any part of it is marked disposed (its balancing allowance or charge is the tax agent's, [accountant to confirm]), and a voided asset is not in them.
func TaxAssets(ctx context.Context, u storage.UnitOfWork) ([]TaxAsset, error) {
	s, err := loadSettings(ctx, u)
	if err != nil {
		return nil, err
	}
	figures, err := AssetFiguresAt(ctx, u, s.Base, time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, err
	}
	sales, err := taxSales(ctx, u, s.Base)
	if err != nil {
		return nil, err
	}
	var out []TaxAsset
	for _, f := range figures {
		a, err := asset.Read(f.Asset)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Asset.Number, err)
		}
		out = append(out, TaxAsset{AssetID: f.Asset.ID, Number: f.Asset.Number, Name: a.Name, AccountID: f.AccountID, PurchasedOn: f.PurchasedOn, Cost: f.Bought,
			Election: a.Tax, Disposed: !f.Bought.Equal(f.Cost) || a.Disposed, Sale: sales[f.Asset.ID]})
	}
	slices.SortStableFunc(out, func(a, b TaxAsset) int {
		if c := a.PurchasedOn.Compare(b.PurchasedOn); c != 0 {
			return c
		}
		return strings.Compare(a.Number, b.Number)
	})
	return out, nil
}
