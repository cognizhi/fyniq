//go:build local

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// checksOf are a period's close checks, by key, as the service runs them on a day.
func checksOf(t *testing.T, s storage.Store, org, period uuid.UUID, today time.Time) map[string]closing.Check {
	t.Helper()
	cc, err := closing.Service{Store: s, Clock: clock.NewFrozen(today)}.RunChecks(context.Background(), org, period)
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

// FR-BNK-10, FR-GL-07, FM-5 and FR-BNK-13 (p1-bank-import-reconcile tasks 6.1 to 6.3, design D9), worked by hand. DBS
// holds three October lines: S$109.00 in (categorised), S$1,350.00 out (not yet) and S$20.00 out (categorised), so its
// book balance at the month end is S$89.00.
//   - The statement says −S$1,261.00: the S$1,350.00 not in the books explains it all, a difference of nil; entered as
//     −S$1,260.00 it leaves S$1.00. With no balance entered, none was imported (BANK-10); once a statement closing at
//     −S$1,260.00 on 31 October arrives, with S$1.00 of interest, it explains itself again.
//   - The preparer never signs (FM-5); another person does; nothing changes once signed.
//   - The close: ✖ while DBS has no signed reconciliation and again once a posting comes after it; ⚠ suspense not at
//     nothing, the S$1,350.00 waiting more than 30 days on 1 December, and a signed reconciliation with a difference.
//   - The cash position on 31 October, once the S$1,350.00 went to suspense: DBS −S$1,261.00 and the USD wallet's USD
//     100.00 at 1.36, S$136.00; −S$1,125.00 in all.
//
// req: FR-BNK-10
// req: FR-GL-07
// req: FM-5
// req: FR-BNK-11
// req: FR-BNK-13
// asvs: V2.3.5
func TestReconciliationIsPreparedThenSignedByAnother(t *testing.T) {
	preparer := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "preparer"}})
	signer := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "controller"}})
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	b := newCategoryBooks(t, "Reconcile Pte Ltd", now)
	s := store(t, "fyniq_api")
	txns := journal.BankTransactions{Store: s}
	whole := func(line storage.BankLine, acc storage.Account) journal.Categorisation {
		return journal.Categorisation{Draft: banktransaction.Draft{LineID: line.ID, Parts: []banktransaction.Part{{AccountID: acc.ID, GSTCode: "NA"}}}, At: now}
	}
	for _, c := range []journal.Categorisation{whole(b.lines[0], b.Sales), whole(b.lines[2], b.Rent)} {
		if _, err := txns.Categorise(preparer, b.Org, c); err != nil {
			t.Fatal(err)
		}
	}
	prepare := func(ctx context.Context, bal *money.Money) (banking.ReconciledPeriod, error) {
		var out banking.ReconciledPeriod
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = banking.Prepare(ctx, u, banking.ReconcileRequest{MoneyAccountID: b.sgd.ID, PeriodID: b.Oct.ID, StatementBalance: bal, At: now})
			return err
		})
		return out, err
	}
	sum := func(v string) *money.Money { m := moneyOf(t, v, "SGD"); return &m }
	two := func(d storage.Decimal) string { // as the currency writes it: 89.0000 is 89.00
		m, err := money.ParseStored(string(d), money.MustLookup("SGD"))
		if err != nil {
			t.Fatal(err)
		}
		return m.String()
	}

	// The close refuses DBS's October before any reconciliation.
	if c := checksOf(t, s, b.Org, b.Oct.ID, porttest.Day(11, 1))["bank-reconciled"]; c.Severity != closing.Failed || c.Detail != "no reconciliation signed off by a second person for DBS current" {
		t.Fatalf("the close before a reconciliation: %+v", c)
	}

	r, err := prepare(preparer, sum("-1261.00"))
	if err != nil || two(r.BookBalance) != "89.00" || two(r.UnreconciledTotal) != "-1350.00" || r.UnreconciledCount != 1 || two(r.Difference) != "0.00" || r.LedgerRevision == 0 {
		t.Fatalf("the reconciliation: %+v %v", r, err)
	}
	if r, err = prepare(preparer, sum("-1260.00")); err != nil || two(r.Difference) != "1.00" || r.Version != 2 {
		t.Fatalf("prepared again, with a difference: %+v %v", r, err)
	}
	var be *banking.Error
	if _, err := prepare(preparer, nil); !errors.As(err, &be) || be.Code != banking.CodeNoStatement {
		t.Fatalf("no statement balance: %v", err)
	}
	if err := s.Do(preparer, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, err := u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: b.sgd.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: porttest.Day(10, 31), PeriodTo: porttest.Day(10, 31), Currency: "SGD", Closing: dec("-1260.00"), At: now,
			Lines: []storage.NewBankLine{{Position: 1, BookingDate: porttest.Day(10, 31), Amount: "1.00", Description: "INTEREST", Fingerprint: fingerprint("rec-interest")}}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if r, err = prepare(preparer, nil); err != nil || two(r.StatementBalance) != "-1260.00" || !r.StatementDate.Equal(porttest.Day(10, 31)) ||
		two(r.UnreconciledTotal) != "-1349.00" || two(r.Difference) != "0.00" {
		t.Fatalf("the statement's own balance: %+v %v", r, err)
	}

	// Signed by another person only; then it never changes.
	sign := func(ctx context.Context) (storage.BankReconciliation, error) {
		var out storage.BankReconciliation
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = banking.SignOff(ctx, u, r.ID, r.Version, now, now)
			return err
		})
		return out, err
	}
	if _, err := sign(preparer); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("signed by its preparer: %v", err)
	}
	signed, err := sign(signer)
	if err != nil || signed.SignedBy != "user:controller" {
		t.Fatalf("signed by another: %+v %v", signed, err)
	}
	if _, err := prepare(preparer, sum("-1260.00")); storage.CodeOf(err) != storage.CodeBanking {
		t.Fatalf("prepared again once signed: %v", err)
	}
	checks := checksOf(t, s, b.Org, b.Oct.ID, porttest.Day(11, 1))
	if c := checks["bank-reconciled"]; c.Severity != closing.OK {
		t.Errorf("the close once signed: %+v", c)
	}
	if _, ok := checks["bank-unmatched"]; ok {
		t.Errorf("a reconciliation that matches: %+v", checks["bank-unmatched"])
	}

	// A posting after the reconciliation; a line to suspense; a line waiting more than 30 days on 1 December.
	if _, err := txns.Suspend(preparer, b.Org, b.lines[1].ID, now); err != nil {
		t.Fatal(err)
	}
	checks = checksOf(t, s, b.Org, b.Oct.ID, porttest.Day(12, 1))
	if c := checks["bank-reconciled"]; c.Severity != closing.Failed || c.Detail != "postings since the reconciliation of DBS current: prepare it again and have it signed off" {
		t.Errorf("a posting since the reconciliation: %+v", c)
	}
	if c := checks["suspense"]; c.Severity != closing.Warning || c.Detail != "2999 holds 1350.00 SGD: classify its lines before the close" {
		t.Errorf("suspense: %+v", c)
	}
	if c := checks["bank-old-lines"]; c.Severity != closing.Warning || c.Detail != "1 line(s) booked on or before 2026-10-31 are not reconciled" {
		t.Errorf("lines waiting: %+v", c)
	}

	// The cash position on 31 October.
	if err := s.Do(preparer, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 20), Rate: "1.35", Quote: "1.35",
			QuoteForm: "base-per-unit"})
		if err != nil {
			return err
		}
		_, err = fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 31), Rate: "1.36", Quote: "1.36",
			QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	usd := importLines(t, b.Org, b.usd, now, storage.NewBankLine{Position: 1, BookingDate: porttest.Day(10, 20), Amount: "100.00", Description: "STRIPE",
		Fingerprint: fingerprint("rec-usd")})
	if _, err := txns.Categorise(preparer, b.Org, whole(usd[0], b.Sales)); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(preparer, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := banking.Cash(ctx, u, porttest.Day(10, 31))
		if err != nil {
			return err
		}
		if len(c.Rows) != 2 || c.Rows[0].Account.Name != "DBS current" || c.Rows[0].Balance.String() != "-1261.00" ||
			c.Rows[1].Balance.String() != "100.00" || c.Rows[1].Value.String() != "136.00" || c.Rows[1].Rate.String() != "1.36" ||
			!c.Rows[1].RateOf.Equal(porttest.Day(10, 31)) || c.Total.String() != "-1125.00" || len(c.Missing) != 0 {
			t.Errorf("the cash position: %+v", c)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Locked with the period.
	if err := s.Do(preparer, b.Org, closePeriod(b.Oct.ID)); err != nil {
		t.Fatal(err)
	}
	if _, err := prepare(preparer, sum("-1260.00")); storage.CodeOf(err) != storage.CodeBanking {
		t.Errorf("prepared in a closed period: %v", err)
	}
}

// Appendix C (p1-bank-import-reconcile task 6.2): each of the close's other bank warnings from a fixture of its own.
// DBS, fed by emailed reports, has had nothing since its import on 25 October, more than 3 days before 1 December
// (C.2 #8); all three lines categorised leave it at −S$1,261.00 at the month end (C.1 #31); a line kept as a possible
// duplicate is unresolved (C.2 #51); and its reconciliation, signed off against a statement of −S$1,260.00 with that
// S$109.00 still unreconciled, leaves −S$108.00 (C.1 #32).
// req: FR-GL-07
// req: FR-BNK-16
// req: QA-RPT-02
func TestTheCloseWarnsOfBankProblems(t *testing.T) {
	preparer := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "preparer"}})
	signer := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "controller"}})
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	b := newCategoryBooks(t, "Bank Warnings Pte Ltd", now)
	s := store(t, "fyniq_api")
	txns := journal.BankTransactions{Store: s}
	for i, acc := range []storage.Account{b.Sales, b.Rent, b.Rent} {
		if _, err := txns.Categorise(preparer, b.Org, journal.Categorisation{Draft: banktransaction.Draft{LineID: b.lines[i].ID,
			Parts: []banktransaction.Part{{AccountID: acc.ID, GSTCode: "NA"}}}, At: now}); err != nil {
			t.Fatal(err)
		}
	}
	balance := moneyOf(t, "-1260.00", "SGD")
	if err := s.Do(preparer, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Banking().AddSource(ctx, storage.NewMoneyAccountSource{MoneyAccountID: b.sgd.ID, Kind: storage.MoneySourceEmail,
			Senders: []string{"ideal@dbs.example"}, ReportFormat: storage.FormatCSV, FromDate: porttest.Day(10, 1), At: now}); err != nil {
			return err
		}
		if _, _, err := u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: b.sgd.ID, Source: storage.MoneySourceEmail, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: porttest.Day(10, 3), PeriodTo: porttest.Day(10, 3), Currency: "SGD", At: now,
			Lines: []storage.NewBankLine{{Position: 1, BookingDate: porttest.Day(10, 3), Amount: "109.00", Reference: "INV-0001", Fingerprint: fingerprint("warn-dup"),
				DuplicateOf: &b.lines[0].ID}}}); err != nil {
			return err
		}
		_, err := banking.Prepare(ctx, u, banking.ReconcileRequest{MoneyAccountID: b.sgd.ID, PeriodID: b.Oct.ID, StatementBalance: &balance, At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(signer, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		recs, err := u.BankMatching().Reconciliations(ctx, b.Oct.ID)
		if err != nil {
			return err
		}
		_, err = banking.SignOff(ctx, u, recs[0].ID, recs[0].Version, now, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	checks := checksOf(t, s, b.Org, b.Oct.ID, porttest.Day(12, 1))
	for key, want := range map[string]string{
		"bank-unmatched":  "A difference is left on DBS current (-108.00 SGD): explain it, or find what is missing",
		"bank-negative":   "DBS current (-1261.00 SGD): check for a missing receipt or a line coded the wrong way",
		"bank-stale":      "Nothing has arrived for more than 3 days for DBS current",
		"bank-duplicates": "1 line(s): keep or exclude each in Banking",
	} {
		if c := checks[key]; c.Severity != closing.Warning || c.Detail != want {
			t.Errorf("%s: %+v", key, c)
		}
	}
	if c := checks["bank-reconciled"]; c.Severity != closing.OK {
		t.Errorf("the reconciliation: %+v", c)
	}
}
