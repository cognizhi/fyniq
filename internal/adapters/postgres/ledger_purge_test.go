//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/retention"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func ymd(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// purgeBooks are books kept from 2010, in financial years from January: a year a period, receivables, suspense and retained earnings, and these entries:
//
//	2010-02-01  E1 a sale of 1,000.00 to the bank
//	2010-03-01  E2 INV-1 owes 500.00 on receivables (an open item of its own source)
//	2010-04-01  E3 PAY-1 receives 200.00 of it
//	2010-05-01  E4 USD 100.00 at 1.35 (135.00) of sales into the bank
//	2010-06-01  E5 rent of 300.00, reversed by E6 on 2011-03-01, after the first boundary
//	2010-07-01  E7 a sale of 70.00, reversed by E8 on 2010-08-01
//	2011-05-01  E9 a sale of 200.00
//	2012-02-01  E10 a sale of 10.00
type purgeBooks struct {
	porttest.Books
	AR, Suspense, RE storage.Account
	Years            map[int]storage.Period
	Entries          map[string]storage.Entry
}

func newPurgeBooks(t *testing.T, name string) purgeBooks {
	t.Helper()
	ctx := signedIn()
	s := store(t, "fyniq_api")
	b := purgeBooks{Books: porttest.NewBooks(t, s, name), Years: map[int]storage.Period{}, Entries: map[string]storage.Entry{}}
	sgd := func(amount string) (storage.Decimal, storage.Decimal, storage.Decimal) {
		return storage.Decimal(amount), "1", storage.Decimal(amount)
	}
	line := func(a storage.Account, amount string) storage.NewLine {
		x, r, base := sgd(amount)
		return storage.NewLine{AccountID: a.ID, Currency: "SGD", Amount: x, FXRate: r, FXRateSource: "base", BaseAmount: base}
	}
	post := func(u storage.UnitOfWork, key, source string, on time.Time, reverses string, lines ...storage.NewLine) error {
		e := storage.NewEntry{PostingDate: on, PostingIdentity: name + ":" + key, RuleVersion: "test@1",
			Source: storage.Source{Type: "manual_journal", ID: source, Version: 1}, Lines: lines}
		if reverses != "" {
			id := b.Entries[reverses].ID
			e.ReversesEntryID = &id
		}
		posted, err := u.Ledger().Append(ctx, e)
		b.Entries[key] = posted
		return err
	}
	err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		start, end := ymd(2010, 1, 1), ymd(2010, 12, 31)
		o.Profile.BooksStart, o.Profile.FirstYearEnd, o.Profile.FYStartMonth = &start, &end, 1
		if _, err := u.UpdateOrganisation(ctx, o.Version, o.Profile); err != nil {
			return err
		}
		if b.AR, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1100", Name: "Receivables", Type: storage.Asset, Role: "accounts-receivable"}); err != nil {
			return err
		}
		if b.Suspense, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2999", Name: "Suspense", Type: storage.Liability, Role: "suspense"}); err != nil {
			return err
		}
		if b.RE, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "3100", Name: "Retained earnings", Type: storage.Equity, Role: "retained-earnings"}); err != nil {
			return err
		}
		for _, y := range []int{2010, 2011, 2012} {
			if b.Years[y], err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: ymd(y, 1, 1), EndsOn: ymd(y, 12, 31)}); err != nil {
				return err
			}
		}
		usd := storage.NewLine{AccountID: b.Bank.ID, Currency: "USD", Amount: "100.00", FXRate: "1.35", FXRateSource: "document", BaseAmount: "135.00"}
		for _, step := range []error{
			post(u, "E1", "E1", ymd(2010, 2, 1), "", line(b.Bank, "1000.00"), line(b.Sales, "-1000.00")),
			post(u, "E2", "INV-1", ymd(2010, 3, 1), "", line(b.AR, "500.00"), line(b.Sales, "-500.00")),
			post(u, "E3", "PAY-1", ymd(2010, 4, 1), "", line(b.Bank, "200.00"), line(b.AR, "-200.00")),
			post(u, "E4", "E4", ymd(2010, 5, 1), "", usd, line(b.Sales, "-135.00")),
			post(u, "E5", "E5", ymd(2010, 6, 1), "", line(b.Rent, "300.00"), line(b.Bank, "-300.00")),
			post(u, "E6", "E5", ymd(2011, 3, 1), "E5", line(b.Rent, "-300.00"), line(b.Bank, "300.00")),
			post(u, "E7", "E7", ymd(2010, 7, 1), "", line(b.Bank, "70.00"), line(b.Sales, "-70.00")),
			post(u, "E8", "E7", ymd(2010, 8, 1), "E7", line(b.Bank, "-70.00"), line(b.Sales, "70.00")),
			post(u, "E9", "E9", ymd(2011, 5, 1), "", line(b.Bank, "200.00"), line(b.Sales, "-200.00")),
			post(u, "E10", "E10", ymd(2012, 2, 1), "", line(b.Bank, "10.00"), line(b.Sales, "-10.00")),
		} {
			if step != nil {
				return step
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the books of %s: %v", name, err)
	}
	return b
}

// closeYears closes the given years' periods.
func (b purgeBooks) closeYears(t *testing.T, years ...int) {
	t.Helper()
	if err := store(t, "fyniq_api").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, y := range years {
			if _, err := u.Periods().Close(ctx, b.Years[y].ID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// reports are what must not change from a boundary on, as the balance service reads them: each day's position at its
// year's end (year-end applied, FR-GL-10), receivables by source type through the day (INV-07), and the general ledger
// of each account from the day to its year's end, with its opening balance.
func (b purgeBooks) reports(t *testing.T, days ...time.Time) string {
	t.Helper()
	out := map[string]any{}
	sgd := money.MustLookup("SGD")
	if err := store(t, "fyniq_api").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		periods, err := u.Periods().List(ctx)
		if err != nil {
			return err
		}
		for _, day := range days {
			at := day.Format(time.DateOnly)
			if out["position "+at], err = balance.Position(ctx, u, sgd, periods, b.Years[day.Year()]); err != nil {
				return err
			}
			if out["receivables "+at], err = balance.Control(ctx, u, sgd, b.AR.ID, day); err != nil {
				return err
			}
			for _, a := range []storage.Account{b.Bank, b.AR, b.Sales, b.Rent, b.RE, b.Suspense} {
				gl, err := balance.GeneralLedger(ctx, u, sgd, a.ID, day, ymd(day.Year(), 12, 31)) // to the year's end
				if err != nil {
					return err
				}
				out["ledger "+a.Code+" "+at] = gl
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(out)
	return string(raw)
}

// The ledger's purge (AUD-10, design D7b). Switched off, nothing goes. Switched on, the oldest year past its retention
// goes behind a signed boundary: its entries are carried forward to the day before the boundary and deleted, but those a
// later entry reverses; every position and every general ledger from the boundary on, opening balances included, is as
// it was; the balances cache agrees with the lines; the purged periods are marked and stay closed. The next run purges
// the next year, the carry-forward with it; a year still open waits.
// req: AUD-10
// req: INV-07
// req: INV-08
func TestTheLedgerIsPurgedAndEveryReportFromTheBoundaryKept(t *testing.T) {
	ctx := context.Background()
	b := newPurgeBooks(t, "Ledger Purged Pte Ltd")
	b.closeYears(t, 2010, 2011)
	worker := store(t, "fyniq_worker")
	blobs, signer := anchoring(t)
	now := ymd(2026, 11, 9).Add(9 * time.Hour)
	purge := func() retention.Purged {
		t.Helper()
		got, err := retention.PurgeLedger(ctx, worker, blobs, signer, now, b.Org)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	days := []time.Time{ymd(2011, 1, 1), ymd(2011, 3, 1), ymd(2011, 12, 31), ymd(2012, 1, 1), ymd(2012, 6, 30)}
	before := b.reports(t, days...)
	if got := purge(); !got.Boundary.IsZero() {
		t.Fatalf("switched off, purged to %s", got.Boundary)
	}

	switchPurgeOn(t, b.Org)
	got := purge()
	if !got.Boundary.Equal(ymd(2011, 1, 1)) || got.Counts["entries_purged"] != 6 || got.Counts["entries_kept"] != 1 {
		t.Fatalf("the first purge: %+v", got)
	}
	if after := b.reports(t, days...); after != before {
		t.Errorf("a report from the boundary on changed:\n before %s\n after  %s", before, after)
	}
	var bounds []storage.LedgerBoundary
	var carried []storage.Entry
	if err := worker.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if bounds, err = u.Retention().LedgerBoundaries(ctx); err != nil {
			return err
		}
		if _, err = u.Ledger().Entry(ctx, b.Entries["E5"].ID); err != nil { // reversed after the boundary: kept
			return err
		}
		for _, src := range []string{"INV-1", "PAY-1"} { // each open item carried under its own source (INV-07)
			e, err := u.Ledger().ByIdentity(ctx, "ledger_boundary:2011-01-01:manual_journal:"+src)
			if err != nil {
				return err
			}
			carried = append(carried, e)
		}
		if n, err := balance.Differences(ctx, u); err != nil || n != 0 {
			t.Errorf("the balances cache: %d differences, %v", n, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Carried: E1's, INV-1's, PAY-1's and E4's balances, each with its sales in retained earnings; E7 and its reversal
	// net to nothing under their source.
	if len(bounds) != 1 || bounds[0].CarriedRevision == 0 || bounds[0].Counts["carry_forward_entries"] != 4 {
		t.Fatalf("the boundary: %+v", bounds)
	}
	for _, e := range carried {
		if !e.PostingDate.Equal(ymd(2010, 12, 31)) || e.RuleVersion != retention.RuleVersion || e.Source.Type != "manual_journal" {
			t.Errorf("a carried open item: %+v", e)
		}
	}
	raw, err := blobs.Get(ctx, bounds[0].AnchorKey)
	if err != nil {
		t.Fatal(err)
	}
	var signed retention.SignedBoundary
	err = json.NewDecoder(raw).Decode(&signed)
	_ = raw.Close()
	if err != nil || signed.Boundary.Boundary != "2011-01-01" || signed.Boundary.Counts["entries_purged"] != 6 {
		t.Errorf("the signed boundary: %+v %v", signed, err)
	}
	if msg, _ := json.Marshal(signed.Boundary); signer.Verify(signed.Boundary.KeyID, msg, signed.Signature) != nil {
		t.Error("the boundary's signature does not verify")
	}
	var purgedOn *time.Time
	if err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT purged_on FROM fyniq.period WHERE id = $1", b.Years[2010].ID).Scan(&purgedOn)
	}); err != nil || purgedOn == nil {
		t.Errorf("2010 is not marked purged: %v %v", purgedOn, err)
	}
	if err := store(t, "fyniq_api").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Reopen(ctx, b.Years[2010].ID)
		return err
	}); storage.CodeOf(err) != storage.CodeLedgerPurge {
		t.Errorf("a purged period reopened: %v", err)
	}

	// The next year goes the same way, the first carry-forward and the reversed pair with it; 2012 is still open.
	days = days[3:]
	before = b.reports(t, days...)
	if got := purge(); !got.Boundary.Equal(ymd(2012, 1, 1)) {
		t.Fatalf("the second purge: %+v", got)
	}
	if after := b.reports(t, days...); after != before {
		t.Errorf("a report from the second boundary on changed:\n before %s\n after  %s", before, after)
	}
	if got := purge(); !got.Boundary.IsZero() {
		t.Errorf("an open year purged: %+v", got)
	}
}

// The ledger's purge keeps its rules whatever any role tries (000292): no posting is changed or deleted directly, the
// owner's included, even with the purge's flag set by hand; only the worker records a boundary or purges, a year at a
// time, switched on, at least 7 years past and closed, and a boundary commits only with its purge; no one posts a
// carry-forward outside it; the purge deletes nothing its carry-forward does not replace exactly, and never splits a
// reversal from what it reverses; a boundary and a tombstone never change.
// req: AUD-10
// req: INV-02
// req: INV-03
// req: QA-ENG-05
// asvs: V16.4.2, V8.2.3
func TestAttackTheLedgerPurgeKeepsItsRules(t *testing.T) {
	ctx := context.Background()
	b := newPurgeBooks(t, "Ledger Purge Attacks Pte Ltd")
	exec := func(role, stmt string, args ...any) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, stmt, args...); return err })
	}
	inTx := func(role string, stmts ...string) error {
		return asOrg(ctx, conn(t, role), b.Org, func(tx pgx.Tx) error {
			for _, s := range stmts {
				if _, err := tx.Exec(ctx, s); err != nil {
					return err
				}
			}
			return nil
		})
	}
	e1 := b.Entries["E1"].ID
	for _, role := range allRoles {
		wantState(t, role+" deletes a posting", exec(role, "DELETE FROM fyniq.journal_entry WHERE id = $1", e1), "42501", "FY002")
		wantState(t, role+" deletes a line", exec(role, "DELETE FROM fyniq.journal_line WHERE entry_id = $1", e1), "42501", "FY002")
		wantState(t, role+" changes a posting", exec(role, "UPDATE fyniq.journal_entry SET memo = 'x' WHERE id = $1", e1), "42501", "FY002")
		if role != "fyniq_worker" && role != "fyniq_migrator" {
			wantState(t, role+" purges", exec(role, "SELECT fyniq.ledger_purge('2011-01-01', '{}')"), "42501")
			wantState(t, role+" records a boundary", exec(role, "INSERT INTO fyniq.ledger_boundary (boundary, approval, counts, purged_hash, anchor_key, written_at) VALUES ('2011-01-01', 'x', '{}', $1, 'k', now())", make([]byte, 32)), "42501")
		}
	}
	boundary := func(day string) string {
		return "INSERT INTO fyniq.ledger_boundary (boundary, approval, counts, purged_hash, anchor_key, written_at) VALUES ('" + day + "', 'x', '{}', '\\x" +
			"0000000000000000000000000000000000000000000000000000000000000000', 'k', now())"
	}
	wantState(t, "a purge with no boundary", exec("fyniq_worker", "SELECT fyniq.ledger_purge('2011-01-01', '{}')"), "FY305")
	wantState(t, "a boundary switched off", inTx("fyniq_worker", boundary("2011-01-01")), "FY305")
	switchPurgeOn(t, b.Org)
	wantState(t, "a boundary over open periods", inTx("fyniq_worker", boundary("2011-01-01")), "FY305")
	b.closeYears(t, 2010, 2011)
	wantState(t, "a boundary skipping a year", inTx("fyniq_worker", boundary("2012-01-01")), "FY305")
	wantState(t, "a boundary mid-year", inTx("fyniq_worker", boundary("2011-02-01")), "FY305", "23514")
	wantState(t, "a boundary recorded without its purge", inTx("fyniq_worker", boundary("2011-01-01")), "FY305")
	bare := "SELECT fyniq.ledger_purge('2011-01-01', ARRAY['" + e1.String() + "']::uuid[])"
	wantState(t, "a purge with no carry-forward", inTx("fyniq_worker", boundary("2011-01-01"), bare), "FY305")
	split := "SELECT fyniq.ledger_purge('2011-01-01', ARRAY['" + b.Entries["E5"].ID.String() + "']::uuid[])"
	wantState(t, "a purge splitting a reversal", inTx("fyniq_worker", boundary("2011-01-01"), split), "FY305")
	late := "SELECT fyniq.ledger_purge('2011-01-01', ARRAY['" + b.Entries["E9"].ID.String() + "']::uuid[])"
	wantState(t, "a purge of an entry after the boundary", inTx("fyniq_worker", boundary("2011-01-01"), late), "FY305")
	flagged := []string{"SELECT set_config('fyniq.ledger_purging', 'on', true)", "DELETE FROM fyniq.journal_line WHERE entry_id = '" + e1.String() + "'"}
	wantState(t, "the owner deleting with the flag set", inTx("fyniq_migrator", flagged...), "FY002")
	// With a boundary of its own the owner deletes only what the purge could, and the boundary never commits without it.
	wantState(t, "the owner deleting with the flag and a boundary", inTx("fyniq_migrator", append([]string{boundary("2011-01-01")}, flagged...)...), "FY305")

	// No one posts a carry-forward outside the purge, under its rule or its source.
	for name, e := range map[string]storage.NewEntry{
		"its rule":   {RuleVersion: retention.RuleVersion, Source: storage.Source{Type: "manual_journal", ID: "X", Version: 1}},
		"its source": {RuleVersion: "test@1", Source: storage.Source{Type: retention.BoundarySource, ID: "X", Version: 1}},
	} {
		e.PostingDate, e.PostingIdentity = ymd(2012, 3, 1), "forged:"+name
		e.Lines = []storage.NewLine{
			{AccountID: b.Bank.ID, Currency: "SGD", Amount: "1.00", FXRate: "1", FXRateSource: "base", BaseAmount: "1.00"},
			{AccountID: b.Sales.ID, Currency: "SGD", Amount: "-1.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-1.00"},
		}
		err := store(t, "fyniq_api").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, e)
			return err
		})
		if storage.CodeOf(err) != storage.CodeLedgerPurge {
			t.Errorf("a carry-forward under %s outside the purge: %v", name, err)
		}
	}

	// A real purge, then its records never change.
	blobs, signer := anchoring(t)
	if _, err := retention.PurgeLedger(ctx, store(t, "fyniq_worker"), blobs, signer, ymd(2026, 11, 9), b.Org); err != nil {
		t.Fatal(err)
	}
	wantState(t, "a boundary changed", exec("fyniq_migrator", "UPDATE fyniq.ledger_boundary SET approval = 'y'"), "FY305")
	wantState(t, "a boundary deleted", exec("fyniq_migrator", "DELETE FROM fyniq.ledger_boundary"), "FY305", "23503")
	wantState(t, "a tombstone deleted", exec("fyniq_migrator", "DELETE FROM fyniq.ledger_purged_entry"), "FY002")
	wantState(t, "a tombstone changed", exec("fyniq_migrator", "UPDATE fyniq.ledger_purged_entry SET source_id = 'x'"), "FY002")
	wantState(t, "a purged period marked again", exec("fyniq_migrator", "UPDATE fyniq.period SET purged_on = NULL WHERE id = $1", b.Years[2010].ID), "FY305")
	var n int
	if err := asOrg(ctx, conn(t, "fyniq_api"), b.Org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM fyniq.ledger_purged_entry").Scan(&n)
	}); err != nil || n != 6 {
		t.Errorf("tombstones: %d, %v", n, err)
	}
}
