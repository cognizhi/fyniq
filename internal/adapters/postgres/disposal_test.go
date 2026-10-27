//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-FA-03 and FR-GL-12 (p1-fixed-assets tasks 3.1 and 3.2; design D4), worked by hand: a S$3,600 laptop of 14 October
// 2026, S$100 a month, its October posted. Half of it is sold on 3 November for S$1,090 with GST at 9% (the
// organisation is GST-registered): GST 90.00 (1,090 × 0.09 ÷ 1.09), net S$1,000; the half's accumulated depreciation is
// S$50, its net book value S$1,750, so a loss of S$750: Dr 1590 50; Dr the bank 1,090; Cr 2200 90; Cr 1510 1,800; Dr
// 7050 750. What is left (S$1,800, S$50 depreciated) depreciates S$50 a month over its 35 months from November. Voided on
// 4 November, the disposal's entry is reversed and the asset is as it was, carrying its S$100 from November; its
// November posts once due. A void after the asset changed again is refused; the replay reproduces every entry.
// req: FR-FA-03
// req: FR-GL-12
// req: FR-FA-02
// req: RND-GST-INCL
// req: ledger/fixed-assets Partial disposal
func TestADisposalIsVoidedAndTheAssetRestored(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newAssetBooks(t, ctx, s, "Disposals Pte Ltd")
	var bank storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if bank, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1011", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		if _, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "7050", Name: "Gain / (loss) on disposal of assets", Type: storage.Revenue}); err != nil {
			return err
		}
		_, err = u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: porttest.Day(10, 1)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now := clock.NewFrozen(time.Date(2026, 11, 3, 4, 0, 0, 0, time.UTC))
	assets := journal.FixedAssets{Store: s, Clock: now}
	disposals := journal.AssetDisposals{Store: s, Clock: now}
	src := b.laptop(t, ctx, s, "LP-1", "3600.00")
	var doc storage.Document
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		doc, err = assets.Register(ctx, u, journal.AssetDraft{SourceID: &src.ID, SourceLine: 1, LifeMonths: 36})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := depreciationOf(t, s, b.Org, doc.ID, 1, "2026-10"); err != nil {
		t.Fatalf("October: %v", err)
	}

	// Half sold on 3 November with GST: a loss of S$750.
	gross, half := sgdOf(t, "1090.00"), sgdOf(t, "1800.00")
	nov3 := porttest.Day(11, 3)
	var rec journal.RecordedDisposal
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		rec, err = disposals.Record(ctx, u, journal.DisposalDraft{AssetID: doc.ID, Date: nov3, Cost: &half, Proceeds: asset.ProceedsMoney,
			MoneyAccountID: &bank.ID, Gross: &gross, GSTCode: "SR"}, nov3)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := legsOf(t, s, b.Org, rec.Entry); got != "1590 50 SGD = 50; 1011 1090 SGD = 1090; 2200 -90 SGD = -90; 1510 -1800 SGD = -1800; 7050 750 SGD = 750" {
		t.Errorf("the half sold: %s", got)
	}
	left, err := asset.Read(rec.Asset)
	if err != nil || rec.Asset.Version != 2 || left.Cost.String() != "1800.00" || left.Carried.String() != "50.00" || !left.From.Equal(porttest.Day(11, 1)) ||
		left.DisposalID == nil || *left.DisposalID != rec.Disposal.ID {
		t.Fatalf("what is left: %+v %v", left, err)
	}
	var states []journal.DepreciationState
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		states, err = journal.DepreciationStates(ctx, u, rec.Asset, nov3, money.MustLookup("SGD"))
		return err
	}); err != nil || len(states) != 36 || states[1].Amount.String() != "50.00" || states[1].Version != 2 {
		t.Fatalf("what is left depreciates: %d months, %+v %v", len(states), states[:2], err)
	}

	// Voided on 4 November: the asset as it was, carrying its S$100 from November; November posts once due.
	nov4 := porttest.Day(11, 4)
	var void journal.RecordedDisposal
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		void, err = disposals.Void(storage.WithReason(ctx, "Sold to the wrong buyer"), u, rec.Disposal.ID, nov4, "Sold to the wrong buyer")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	restored, err := asset.Read(void.Asset)
	if err != nil || void.Asset.Version != 3 || restored.Cost.String() != "3600.00" || restored.Carried.String() != "100.00" || restored.DisposalID != nil ||
		void.Disposal.Status != storage.DocumentReversed || legsOf(t, s, b.Org, void.Entry) != "1590 -50 SGD = -50; 1011 -1090 SGD = -1090; 2200 90 SGD = 90; 1510 1800 SGD = 1800; 7050 -750 SGD = -750" {
		t.Fatalf("the void: %+v %s %v", restored, legsOf(t, s, b.Org, void.Entry), err)
	}
	if run, err := (journal.Depreciations{Store: store(t, "fyniq_worker")}).Depreciate(context.Background(), b.Org, doc.ID, porttest.Day(12, 1)); err != nil || run.Posted != 1 {
		t.Fatalf("November after the void: %+v %v", run, err)
	}
	if nov, err := depreciationOf(t, s, b.Org, doc.ID, 3, "2026-11"); err != nil || legsOf(t, s, b.Org, nov) != "6610 100 SGD = 100; 1590 -100 SGD = -100" {
		t.Fatalf("November: %s %v", legsOf(t, s, b.Org, nov), err)
	}

	// A second disposal of half of it, dated 30 November, reverses November's month; once the asset changes again, the
	// disposal is no longer voided.
	dec1 := porttest.Day(12, 1)
	now.Set(dec1)
	half2 := sgdOf(t, "1800.00")
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		rec, err = disposals.Record(ctx, u, journal.DisposalDraft{AssetID: doc.ID, Date: porttest.Day(11, 30), Cost: &half2, Proceeds: asset.ProceedsNone}, dec1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		life := 48
		_, err := assets.Change(ctx, u, doc.ID, journal.AssetChange{LifeMonths: &life})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err = s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := disposals.Void(ctx, u, rec.Disposal.ID, dec1, "Too late")
		return err
	})
	var refused *asset.Error
	if !errors.As(err, &refused) || refused.Code != asset.CodeDisposalVoid {
		t.Errorf("a void after the asset changed: %v", err)
	}

	// The replay reproduces every entry: the months, the disposal, its reversal and the reversal of November's month
	// by the disposal dated 30 November.
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
