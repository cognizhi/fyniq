package journal

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RegisterOwn are the sources whose postings to an account of the register its documents account for (INV-07;
// p1-fixed-assets design D6): the purchases, the opening balances, depreciation and disposals. What any other source
// posted there, a manual journal above all, is named (⚠); anything left over is a fault (✖).
func RegisterOwn(sourceType string) bool {
	switch sourceType {
	case ledger.SourceBill, ledger.SourceBankTransaction, ledger.SourceOpeningBalance, ledger.SourceAssetDepreciation, ledger.SourceAssetDisposal:
		return true
	}
	return false
}

// AssetFigures are what one asset puts on the books as at a day: the cost on its account and its accumulated
// depreciation.
type AssetFigures struct {
	Asset       storage.Document
	AccountID   uuid.UUID
	Cost        money.Money
	Accumulated money.Money
}

// disposalAt is a disposal with whether it is live now (posted) and was live on a day (posted then, and not voided by
// then).
type disposalAt struct {
	asset.Disposal
	now, then bool
}

// openingAccumulated is the accumulated depreciation the opening balances carry for an asset bought before the books
// start: what its first versions carried, as long as they changed only its figures (none posted, none disposed of).
func openingAccumulated(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (money.Money, error) {
	var out money.Money
	var from time.Time
	for v := 1; v <= doc.Version; v++ {
		x, err := u.Documents().Version(ctx, doc.ID, v)
		if err != nil {
			return money.Money{}, err
		}
		a, err := asset.Read(x)
		if err != nil {
			return money.Money{}, err
		}
		if v == 1 {
			from = a.From
		}
		if a.DisposalID != nil || !a.From.Equal(from) {
			break
		}
		out = a.Carried
	}
	return out, nil
}

// AssetFiguresAt are every registered asset's figures as at asAt (design D6), from its documents and its entries: the
// cost it was bought for (what is on its account now, with what its live disposals took off) less what disposals live
// then and dated by then took; its accumulated depreciation, what the opening balances carried for it and every month
// posted by then, net of reversals by then, less what those disposals took. An asset bought after asAt is not in them.
func AssetFiguresAt(ctx context.Context, u storage.UnitOfWork, base money.Currency, asAt time.Time) ([]AssetFigures, error) {
	forever := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	disposals := map[uuid.UUID][]disposalAt{}
	if err := subledger.Pages(ctx, u, storage.DocumentAssetDisposal, forever, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		x, err := asset.ReadDisposal(doc)
		if err != nil {
			return err
		}
		voided, err := subledger.VoidedBy(ctx, u, ledger.AssetDisposalIdentity(doc.ID, doc.Version, ledger.RuleAssetDisposal), asAt)
		if err != nil {
			return err
		}
		disposals[x.AssetID] = append(disposals[x.AssetID], disposalAt{Disposal: x, now: doc.Status == storage.DocumentPosted, then: !voided})
		return nil
	}); err != nil {
		return nil, err
	}
	var out []AssetFigures
	err := subledger.Pages(ctx, u, storage.DocumentFixedAsset, forever, func(doc storage.Document) error {
		if doc.Status != storage.DocumentPosted {
			return nil
		}
		a, err := asset.Read(doc)
		if err != nil {
			return err
		}
		if a.Source != nil && a.PurchasedOn.After(asAt) {
			return nil
		}
		cost, acc := a.Cost, money.Zero(base)
		if a.Source == nil {
			if acc, err = openingAccumulated(ctx, u, doc); err != nil {
				return err
			}
		}
		for _, x := range disposals[doc.ID] {
			if x.now { // what the disposal took off, back on, to reach the cost it was bought for
				cost, _ = cost.Add(x.Cost)
			}
			if x.then && !x.Date.After(asAt) {
				cost, _ = cost.Sub(x.Cost)
				acc, _ = acc.Sub(x.Accumulated)
			}
		}
		entries, err := u.Ledger().OfSource(ctx, doc.ID)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Source.Type != ledger.SourceAssetDepreciation || e.PostingDate.After(asAt) {
				continue
			}
			part, err := depreciated(e, base) // a reversal's positive line is its 1590 debit: the same part
			if err != nil {
				return err
			}
			if e.ReversesEntryID != nil {
				part = part.Neg()
			}
			acc, _ = acc.Add(part)
		}
		out = append(out, AssetFigures{Asset: doc, AccountID: a.AccountID, Cost: cost, Accumulated: acc})
		return nil
	})
	return out, err
}

// UnregisteredLines are the lines of posted bills and categorised bank lines dated by asAt that put a cost on an asset
// account of the register and are in no asset, oldest first: what the register lists as "Not in the register yet"
// (design D5).
func UnregisteredLines(ctx context.Context, u storage.UnitOfWork, s ledger.Settings, asAt time.Time) ([]AssetLine, error) {
	sources, err := u.AssetSources().List(ctx)
	if err != nil {
		return nil, err
	}
	taken := map[uuid.UUID]map[int]bool{}
	for _, x := range sources {
		if taken[x.SourceID] == nil {
			taken[x.SourceID] = map[int]bool{}
		}
		taken[x.SourceID][x.Line] = true
	}
	var out []AssetLine
	for _, t := range []storage.DocumentType{storage.DocumentBill, storage.DocumentBankTransaction} {
		if err := subledger.Pages(ctx, u, t, asAt, func(doc storage.Document) error {
			if doc.Status != storage.DocumentPosted {
				return nil
			}
			lines, err := SourceLines(ctx, u, doc, s)
			if err != nil {
				return err
			}
			for _, l := range lines {
				if !taken[doc.ID][l.Line] {
					out = append(out, l)
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	slices.SortStableFunc(out, func(a, b AssetLine) int { // oldest first, then by number and line
		return cmp.Or(a.Date.Compare(b.Date), cmp.Compare(a.Number, b.Number), cmp.Compare(a.Line, b.Line))
	})
	return out, nil
}

// RegisterAccount is one account reconciled with the fixed asset register as at a day (INV-07; design D6): an asset
// account against the cost of the assets on it and the lines on it not in the register yet, or 1590 against the
// assets' accumulated depreciation.
type RegisterAccount struct {
	Account        storage.Account
	Reconciliation subledger.Reconciliation
	Unregistered   []AssetLine
}

// RegisterAt reconciles the register with the ledger as at asAt (INV-07; design D6): each asset account of the register
// that holds a balance or an asset, through the balance service (ENG-04), and 1590 Accumulated depreciation.
func RegisterAt(ctx context.Context, u storage.UnitOfWork, asAt time.Time) ([]RegisterAccount, error) {
	s, err := loadSettings(ctx, u)
	if err != nil {
		return nil, err
	}
	figures, err := AssetFiguresAt(ctx, u, s.Base, asAt)
	if err != nil {
		return nil, err
	}
	unregistered, err := UnregisteredLines(ctx, u, s, asAt)
	if err != nil {
		return nil, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	var out []RegisterAccount
	add := func(acc storage.Account, credit bool, items []subledger.Item, lines []AssetLine) error {
		totals, err := balance.Control(ctx, u, s.Base, acc.ID, asAt)
		if err != nil {
			return err
		}
		if len(totals) == 0 && len(items) == 0 {
			return nil
		}
		r, err := subledger.Reconcile(s.Base, asAt, totals, []subledger.Group{{Items: items}}, credit, RegisterOwn)
		if err != nil {
			return err
		}
		out = append(out, RegisterAccount{Account: acc, Reconciliation: r, Unregistered: lines})
		return nil
	}
	for _, acc := range accounts {
		var items []subledger.Item
		var lines []AssetLine
		switch {
		case asset.InRegister(acc):
			for _, f := range figures {
				if f.AccountID == acc.ID {
					items = append(items, subledger.Item{DocumentID: f.Asset.ID, Number: f.Asset.Number, Open: f.Cost, Carrying: f.Cost})
				}
			}
			for _, l := range unregistered {
				if l.AccountID == acc.ID {
					lines = append(lines, l)
					items = append(items, subledger.Item{DocumentID: l.DocumentID, Number: l.Number, Open: l.Cost, Carrying: l.Cost})
				}
			}
			if err := add(acc, false, items, lines); err != nil {
				return nil, err
			}
		case string(acc.Role) == ledger.RoleAccumulatedDepreciation:
			for _, f := range figures {
				items = append(items, subledger.Item{DocumentID: f.Asset.ID, Number: f.Asset.Number, Open: f.Accumulated, Carrying: f.Accumulated})
			}
			if err := add(acc, true, items, nil); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// ThresholdSetting is the capitalisation threshold (FR-ORG-04): a purchase of at least it is offered as a fixed asset.
const ThresholdSetting = "assets.capitalisation_threshold"

// WarnUnregistered is a posted bill's or bank line's line on an asset account, at or above the capitalisation threshold,
// that no asset was registered from (FR-FA-01; design D5).
const WarnUnregistered = "ASSET-UNREGISTERED"

// CapitalisationThreshold is the capitalisation threshold in force on a day, in the base currency.
func CapitalisationThreshold(ctx context.Context, u storage.UnitOfWork, base money.Currency, on time.Time) (money.Money, error) {
	def, ok := settings.Lookup(ThresholdSetting)
	if !ok {
		return money.Money{}, fmt.Errorf("no setting %s", ThresholdSetting)
	}
	changes, err := u.Settings().Changes(ctx, ThresholdSetting)
	if err != nil {
		return money.Money{}, err
	}
	return money.Parse(settings.On(def, changes, on).Value, base)
}

// UnregisteredOf are a posted bill's or categorised bank line's lines on an asset account of the register that no asset
// was registered from, each with whether it is at or above the capitalisation threshold on its date.
func UnregisteredOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) ([]AssetLine, []bool, error) {
	lines, err := SourceLines(ctx, u, doc, s)
	if err != nil || len(lines) == 0 {
		return nil, nil, err
	}
	taken, err := u.AssetSources().OfDocument(ctx, doc.ID)
	if err != nil {
		return nil, nil, err
	}
	var out []AssetLine
	var at []bool
	for _, l := range lines {
		if slices.ContainsFunc(taken, func(x storage.AssetSource) bool { return x.Line == l.Line }) {
			continue
		}
		threshold, err := CapitalisationThreshold(ctx, u, s.Base, l.Date)
		if err != nil {
			return nil, nil, err
		}
		above, _ := l.Cost.Cmp(threshold)
		out, at = append(out, l), append(at, above >= 0)
	}
	return out, at, nil
}
