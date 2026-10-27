//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// assetBooks are purchasesBooks with 1510 Computer equipment, 6610 Depreciation and 1590 Accumulated depreciation.
type assetBooks struct {
	purchasesBooks
	Equipment, Depreciation, Accumulated storage.Account
}

func newAssetBooks(t *testing.T, ctx context.Context, s storage.Store, name string) assetBooks {
	t.Helper()
	b := assetBooks{purchasesBooks: newPurchasesBooks(t, ctx, s, name)}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if b.Equipment, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1510", Name: "Computer equipment", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{ReportGroup: "Property, plant & equipment"}}); err != nil {
			return err
		}
		if b.Depreciation, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "6610", Name: "Depreciation", Type: storage.Expense,
			Role: storage.AccountRole(ledger.RoleDepreciation)}); err != nil {
			return err
		}
		b.Accumulated, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1590", Name: "Accumulated depreciation", Type: storage.Asset,
			Role: storage.AccountRole(ledger.RoleAccumulatedDepreciation)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

// laptop posts a bill of 14 October for a laptop of net on 1510, and returns it.
func (b assetBooks) laptop(t *testing.T, ctx context.Context, s storage.Store, ref, net string) storage.Document {
	t.Helper()
	bills := journal.Bills{Store: s}
	doc, err := bills.Create(ctx, b.Org, bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: ref, Date: porttest.Day(10, 14), Currency: money.MustLookup("SGD"),
		Lines: []bill.Line{{AccountID: b.Equipment.ID, Description: "MacBook Pro 14", Net: sgdOf(t, net), GSTCode: "ZP", GST: sgdOf(t, "0.00")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatal(err)
	}
	return doc
}

// depreciationOf is the entry an asset version's depreciation posted for a month ("2026-10"), or ports.ErrNotFound.
func depreciationOf(t *testing.T, s storage.Store, org, id uuid.UUID, version int, month string) (storage.Entry, error) {
	t.Helper()
	m, err := time.Parse("2006-01", month)
	if err != nil {
		t.Fatal(err)
	}
	var e storage.Entry
	err = s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		e, err = u.Ledger().ByIdentity(ctx, ledger.DepreciationIdentity(id, version, m, ledger.RuleAssetDepreciation))
		return err
	})
	return e, err
}

// FR-FA-02, ENG-05 and FR-AUT-01 (p1-fixed-assets task 2.2): a registered asset's months of depreciation post on their
// last days, Dr 6610 · Cr 1590, as "scheduler:assets.depreciation": those due when it is registered at once, the rest by
// the hourly job assets.depreciation under the worker's role, up to each organisation's today and never ahead; a run
// twice posts once; a month with no open period waits, with an audit event, and posts once its period exists. A change
// of life carries what posted into the next version and spreads what is left over the months not posted. The database
// refuses a month that names no registered asset at its current version (FY236), and the replay reproduces each month.
// Worked by hand: S$3,600.00 over 36 months from October 2026 is S$100.00 a month; after three months (S$300.00), a life
// of 48 months spreads S$3,300.00 over the 45 months from January 2027 to September 2030, S$73.33 a month (ALLOC-EVEN).
// req: FR-FA-02
// req: ENG-05
// req: FR-AUT-01
// req: FR-AUT-02
// req: ledger/posting-rules Depreciation
// req: QA-ENG-05
// asvs: V2.3.1
func TestTheSchedulerDepreciatesTheRegister(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newAssetBooks(t, ctx, s, "Depreciation Pte Ltd")
	now := clock.NewFrozen(time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC))
	assets := journal.FixedAssets{Store: s, Clock: now}
	jobs := journal.Depreciations{Store: store(t, "fyniq_worker")}
	src := b.laptop(t, ctx, s, "LP-1", "3600.00")
	var doc storage.Document
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		doc, err = assets.Register(ctx, u, journal.AssetDraft{SourceID: &src.ID, SourceLine: 1, LifeMonths: 36})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Registered on 5 November: October's month posts at once, on its last day.
	oct, err := depreciationOf(t, s, b.Org, doc.ID, 1, "2026-10")
	if err != nil || legsOf(t, s, b.Org, oct) != "6610 100 SGD = 100; 1590 -100 SGD = -100" || !oct.PostingDate.Equal(porttest.Day(10, 31)) ||
		oct.RuleVersion != ledger.RuleAssetDepreciation || oct.Memo != "FA-0001 MacBook Pro 14: depreciation for October 2026 (1 of 36)" {
		t.Fatalf("October: %+v %v", oct, err)
	}
	if _, err := depreciationOf(t, s, b.Org, doc.ID, 1, "2026-11"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("November before its last day: %v", err)
	}
	// The job on the same day posts nothing more; on 5 December it posts November, once.
	if run, err := jobs.DepreciateDue(context.Background(), b.Org, porttest.Day(11, 5)); err != nil || run.Posted != 0 || run.Waiting != 0 {
		t.Fatalf("the run of 5 November: %+v %v", run, err)
	}
	for range 2 {
		if run, err := jobs.DepreciateDue(context.Background(), b.Org, porttest.Day(12, 5)); err != nil || run.Posted > 1 || run.Waiting != 0 {
			t.Fatalf("the runs of 5 December: %+v %v", run, err)
		}
	}
	if _, err := depreciationOf(t, s, b.Org, doc.ID, 1, "2026-11"); err != nil {
		t.Fatalf("November: %v", err)
	}

	// December has no period on 5 January: it waits, with an audit event, and January is never posted ahead.
	jan5 := time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC)
	if run, err := jobs.DepreciateDue(context.Background(), b.Org, jan5); err != nil || run.Posted != 0 || run.Waiting != 1 {
		t.Fatalf("the run of 5 January without December's period: %+v %v", run, err)
	}
	if err := store(t, "fyniq_readonly").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		if err != nil {
			return err
		}
		var trail strings.Builder
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		for _, want := range []string{`"actor":{"type":"scheduler","id":"assets.depreciation"}`, `"action":"fixed_asset.depreciation_waiting"`,
			`"waiting_for":[null,"STORAGE-PERIOD-01"]`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the audit trail lacks %s", want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(12, 1), EndsOn: porttest.Day(12, 31)}); err != nil {
			return err
		}
		_, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.DepreciateDueAll(context.Background(), store(t, "fyniq_worker"), jan5.Add(4*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if dec, err := depreciationOf(t, s, b.Org, doc.ID, 1, "2026-12"); err != nil || !dec.PostingDate.Equal(porttest.Day(12, 31)) {
		t.Fatalf("December once its period exists: %+v %v", dec, err)
	}
	if _, err := depreciationOf(t, s, b.Org, doc.ID, 1, "2027-01"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("January ahead of its last day: %v", err)
	}

	// A life of 48 months from 5 January: version 2 carries S$300.00 from January 2027, S$73.33 a month to September 2030.
	now.Set(jan5)
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		life := 48
		doc, err = assets.Change(ctx, u, doc.ID, journal.AssetChange{LifeMonths: &life})
		return err
	}); err != nil || doc.Version != 2 {
		t.Fatalf("change the life: %+v %v", doc, err)
	}
	var states []journal.DepreciationState
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		states, err = journal.DepreciationStates(ctx, u, doc, jan5, money.MustLookup("SGD"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(states) != 48 || states[2].Status != journal.DepreciationPosted || states[2].Version != 1 || states[3].Status != journal.DepreciationProjected ||
		states[3].Version != 2 || states[3].Amount.String() != "73.33" || !states[47].Month.Equal(time.Date(2030, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the schedule after the change: %d months, %+v", len(states), states[:4])
	}
	if run, err := jobs.Depreciate(context.Background(), b.Org, doc.ID, time.Date(2027, 2, 1, 0, 0, 0, 0, time.UTC)); err != nil || run.Posted != 1 {
		t.Fatalf("January under version 2: %+v %v", run, err)
	}
	if jan, err := depreciationOf(t, s, b.Org, doc.ID, 2, "2027-01"); err != nil || legsOf(t, s, b.Org, jan) != "6610 73.33 SGD = 73.33; 1590 -73.33 SGD = -73.33" {
		t.Fatalf("January: %s %v", legsOf(t, s, b.Org, jan), err)
	}

	// Straight at the store: a month of an earlier version, or of a document that is not an asset, is refused (FY236).
	for what, src := range map[string]storage.Source{
		"version 1, no longer current": {Type: ledger.SourceAssetDepreciation, ID: doc.ID.String(), Version: 1},
		"the bill":                     {Type: ledger.SourceAssetDepreciation, ID: src.ID.String(), Version: 1},
	} {
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC), Source: src,
				RuleVersion: ledger.RuleAssetDepreciation, PostingIdentity: "attack:" + what, Memo: "Depreciation", Lines: []storage.NewLine{
					{AccountID: b.Depreciation.ID, Currency: "SGD", Amount: "1.00", FXRate: "1", FXRateSource: "base", BaseAmount: "1.00"},
					{AccountID: b.Accumulated.ID, Currency: "SGD", Amount: "-1.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-1.00"}}})
			return err
		})
		if storage.CodeOf(err) != storage.CodeAsset {
			t.Errorf("depreciation of %s: %v", what, err)
		}
	}

	// The balance service and the replay agree with what posted: four months, S$373.33.
	if got := balances(t, s, b.Org); got["6610"] != "373.33" || got["1590"] != "-373.33" {
		t.Errorf("balances: %v", got)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) != 0 {
			t.Errorf("replay: %+v", r.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
