//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// INV-07 for the fixed asset register (p1-fixed-assets task 4.1; design D5, D6), worked by hand: on 31 October 2026,
// 1510 holds a S$3,600.00 laptop registered (its October, S$100.00, posted on 1590), a S$1,200.00 laptop and a S$900.00
// monitor not registered: S$5,700.00, which the register and the lines not in it account for; 1590 holds S$100.00, the
// register's accumulated depreciation. The close says ✔ and ⚠ for the two lines not in the register; the S$1,200.00
// one, at the S$1,000.00 capitalisation threshold, shows the prompt; the S$900.00 one does not. Registered, it leaves
// the list. A manual journal of S$50.00 to 1590 is named at the close (⚠); a disposal on 15 November keeps the register
// and 1510 in agreement as at 30 November.
// req: INV-07
// req: FR-FA-01
// req: ledger/fixed-assets Capitalisation threshold prompt
// req: FR-CTL-05
func TestTheRegisterAgreesWithItsAccounts(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newAssetBooks(t, ctx, s, "Register Pte Ltd")
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "7050", Name: "Gain / (loss) on disposal of assets", Type: storage.Revenue})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now := clock.NewFrozen(porttest.Day(11, 5))
	assets := journal.FixedAssets{Store: s, Clock: now}
	laptop := b.laptop(t, ctx, s, "LP-1", "3600.00")
	second := b.laptop(t, ctx, s, "LP-2", "1200.00")
	monitor := b.laptop(t, ctx, s, "MN-1", "900.00")
	register := func(src storage.Document) storage.Document {
		t.Helper()
		var doc storage.Document
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			doc, err = assets.Register(ctx, u, journal.AssetDraft{SourceID: &src.ID, SourceLine: 1, LifeMonths: 36})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	first := register(laptop)
	at := func(day string) map[string]journal.RegisterAccount {
		t.Helper()
		out := map[string]journal.RegisterAccount{}
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			d, _ := time.Parse(time.DateOnly, day)
			rs, err := journal.RegisterAt(ctx, u, d)
			for _, r := range rs {
				out[r.Account.Code] = r
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	checks := func() map[string]closing.Check {
		t.Helper()
		cc, err := closing.Service{Store: s, Clock: now}.RunChecks(ctx, b.Org, b.Oct.ID)
		if err != nil {
			t.Fatal(err)
		}
		all, err := closing.Read(cc.Result)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]closing.Check{}
		for _, c := range all {
			out[c.Key] = c
		}
		return out
	}

	oct := at("2026-10-31")
	if r := oct["1510"]; r.Reconciliation.Unexplained() || r.Reconciliation.Base.Control.String() != "5700.00" || r.Reconciliation.Base.SubLedger.String() != "5700.00" ||
		len(r.Unregistered) != 2 {
		t.Errorf("1510 on 31 October: %+v", r)
	}
	if r := oct["1590"]; r.Reconciliation.Unexplained() || r.Reconciliation.Base.Control.String() != "100.00" || r.Reconciliation.Base.SubLedger.String() != "100.00" {
		t.Errorf("1590 on 31 October: %+v", r)
	}
	c := checks()
	if c["assets"].Severity != closing.OK || c["assets-unregistered"].Severity != closing.Warning ||
		c["assets-unregistered"].Detail != "BILL-0002 line 1 (1200.00), BILL-0003 line 1 (900.00): register each in Accounting › Fixed assets, so it depreciates" {
		t.Errorf("the close: %+v", c)
	}
	// The prompt on the bills: the S$1,200.00 laptop is at the threshold, the S$900.00 monitor is not.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		set, err := journal.LoadSettings(ctx, u)
		if err != nil {
			return err
		}
		for _, x := range []struct {
			doc  storage.Document
			want bool
		}{{second, true}, {monitor, false}} {
			doc, err := u.Documents().Get(ctx, x.doc.ID)
			if err != nil {
				return err
			}
			lines, at, err := journal.UnregisteredOf(ctx, u, doc, set)
			if err != nil {
				return err
			}
			if len(lines) != 1 || at[0] != x.want {
				t.Errorf("%s: %+v %v", doc.Number, lines, at)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Registered, the laptop leaves the list; a manual journal to 1590 is named.
	register(second)
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, 30), PostingIdentity: "manual_journal:" + uuid.NewString() + ":v1:journal@1",
			RuleVersion: "journal@1", Memo: "Top-up", Source: storage.Source{Type: ledger.SourceManualJournal, ID: uuid.NewString(), Version: 1},
			Lines: []storage.NewLine{
				{AccountID: b.Depreciation.ID, Currency: "SGD", Amount: "50.00", FXRate: "1", FXRateSource: "base", BaseAmount: "50.00"},
				{AccountID: b.Accumulated.ID, Currency: "SGD", Amount: "-50.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-50.00"},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	c = checks()
	if c["assets"].Severity != closing.OK || c["assets-unregistered"].Detail != "BILL-0003 line 1 (900.00): register each in Accounting › Fixed assets, so it depreciates" ||
		c["assets-unsupported"].Severity != closing.Warning || c["assets-unsupported"].Detail != "1590 holds 50.00 from manual journals: register the assets, or have the accountant check the journals" {
		t.Errorf("the close after the journal: %+v", c)
	}

	// Sold on 15 November: as at 30 November the register and 1510 still agree, and so do it and 1590.
	gross := sgdOf(t, "2000.00")
	var bank storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		bank, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1011", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	now.Set(porttest.Day(11, 20))
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := journal.AssetDisposals{Store: s}.Record(ctx, u, journal.DisposalDraft{AssetID: first.ID, Date: porttest.Day(11, 15),
			Proceeds: asset.ProceedsMoney, MoneyAccountID: &bank.ID, Gross: &gross}, porttest.Day(11, 20))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	nov := at("2026-11-30")
	if r := nov["1510"]; r.Reconciliation.Unexplained() || r.Reconciliation.Base.Control.String() != "2100.00" {
		t.Errorf("1510 on 30 November: %+v", r)
	}
	if r := nov["1590"]; r.Reconciliation.Unexplained() {
		t.Errorf("1590 on 30 November: %+v", r)
	}
}
