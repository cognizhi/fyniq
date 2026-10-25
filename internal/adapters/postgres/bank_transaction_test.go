//go:build local

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// categoryBooks are bankedBooks with the accounts a categorised line posts to by role: 2200, 1210, 2400, 2999 and
// 7030, registered for GST from 1 October.
type categoryBooks struct {
	bankedBooks
	GST, Prepaid, Deferred, Suspense, FX storage.Account
}

func newCategoryBooks(t *testing.T, name string, now time.Time) categoryBooks {
	t.Helper()
	b := categoryBooks{bankedBooks: newBankedBooks(t, name, now)}
	if err := store(t, "fyniq_api").Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		for _, a := range []struct {
			to *storage.Account
			n  storage.NewAccount
		}{
			{&b.GST, storage.NewAccount{Code: "2200", Name: "GST control", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleGSTControl)}},
			{&b.Prepaid, storage.NewAccount{Code: "1210", Name: "Prepayments", Type: storage.Asset, Role: storage.AccountRole(ledger.RolePrepayments)}},
			{&b.Deferred, storage.NewAccount{Code: "2400", Name: "Deferred revenue", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleDeferredRevenue)}},
			{&b.Suspense, storage.NewAccount{Code: "2999", Name: "Suspense - ask accountant", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleSuspense)}},
			{&b.FX, storage.NewAccount{Code: "7030", Name: "FX & rounding differences", Type: storage.Expense, Role: storage.AccountRole(ledger.RoleFXRounding)}},
		} {
			if *a.to, err = u.Accounts().Create(ctx, a.n); err != nil {
				return err
			}
		}
		_, err = u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: porttest.Day(10, 1)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

// importLines imports a statement of lines into a money account and returns them.
func importLines(t *testing.T, org uuid.UUID, m storage.MoneyAccount, now time.Time, lines ...storage.NewBankLine) []storage.BankLine {
	t.Helper()
	var out []storage.BankLine
	from, to := lines[0].BookingDate, lines[len(lines)-1].BookingDate
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		_, out, err = u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: m.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: from, PeriodTo: to, Currency: m.Currency, Lines: lines, At: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func sum(t *testing.T, s string, cur string) *money.Money {
	m := moneyOf(t, s, cur)
	return &m
}

// lineOf is a line as it now stands.
func lineOf(t *testing.T, org, id uuid.UUID) storage.BankLine {
	t.Helper()
	var l storage.BankLine
	if err := store(t, "fyniq_api").Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		l, err = u.Banking().Line(ctx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return l
}

// FR-BNK-06 and Appendix B.1 (p1-bank-import-reconcile task 4.1): categorising a line posts its bank transaction by
// bank_transaction@1 in the unit of work that moves the line's status and makes its match (ENG-02): the money account
// with the line's signed gross, 2200 with the GST extracted under RND-GST-INCL when registered and the code carries
// GST, the rest to the category. A part on 2999 keeps the line in the queue as suspense until it is classified, which
// reverses its suspense posting; an undo reverses the categorisation and puts the line back. A line in another currency
// takes the cascade's rate for its date, and none refuses it with FX-01. A refusal writes nothing, and the replay
// reproduces every entry.
// req: ledger/posting-rules Posting a bank transaction, money in
// req: ledger/posting-rules Posting a bank transaction, money out
// req: ledger/posting-rules Amounts of a bank, card or cash transaction
// req: FR-BNK-06
// req: FR-BNK-11
// req: ENG-02
// req: FR-GL-12
// req: ENG-06
func TestCategorisingABankLinePostsIt(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	b := newCategoryBooks(t, "Categorise Pte Ltd", now)
	s := store(t, "fyniq_api")
	txns := journal.BankTransactions{Store: s}
	whole := func(line storage.BankLine, acc storage.Account, code string) journal.Categorisation {
		return journal.Categorisation{Draft: banktransaction.Draft{LineID: line.ID, Parts: []banktransaction.Part{{AccountID: acc.ID, GSTCode: code}}}, At: now}
	}

	// Money in, S$109.00 of sales at SR: Dr 1010 109.00; Cr 4000 100.00; Cr 2200 9.00.
	in, err := txns.Categorise(ctx, b.Org, whole(b.lines[0], b.Sales, "SR"))
	if err != nil || legsOf(t, s, b.Org, in.Entry) != "1010 109 SGD = 109; 4000 -100 SGD = -100; 2200 -9 SGD = -9" {
		t.Fatalf("money in: %s %v", legsOf(t, s, b.Org, in.Entry), err)
	}
	if in.Line.Status != storage.LineCategorised || in.Match.Kind != storage.MatchCategorised || *in.Match.DocumentID != in.Transaction.ID ||
		*in.Match.EntryID != in.Entry.ID || in.Transaction.Type != storage.DocumentBankTransaction || in.Transaction.Status != storage.DocumentPosted ||
		in.Entry.Source != (storage.Source{Type: ledger.SourceBankTransaction, ID: in.Transaction.ID.String(), Version: 1}) ||
		!in.Entry.PostingDate.Equal(porttest.Day(10, 3)) || in.Entry.Memo != "DBS current" {
		t.Fatalf("the categorisation: %+v", in)
	}
	if _, err := txns.Categorise(ctx, b.Org, whole(b.lines[0], b.Sales, "SR")); !isBankTxn(err, journal.CodeLineDone) {
		t.Fatalf("a line categorised twice: %v", err)
	}

	// Refusals write nothing: a GST code the pack does not have, none at all, and a money account as the category.
	before := count(t, b.Org, "document")
	for name, c := range map[string]struct {
		cat  journal.Categorisation
		want string
	}{
		"an unknown code": {whole(b.lines[2], b.Rent, "ZZ"), banktransaction.CodeGSTCode},
		"no code":         {whole(b.lines[2], b.Rent, ""), banktransaction.CodeGSTCode},
		"a money account": {whole(b.lines[2], storage.Account{ID: b.usd.AccountID}, "NA"), ledger.CodeBankPart},
		"two rests": {journal.Categorisation{Draft: banktransaction.Draft{LineID: b.lines[2].ID, Parts: []banktransaction.Part{{AccountID: b.Rent.ID, GSTCode: "TX"},
			{AccountID: b.Rent.ID, GSTCode: "TX"}}}, At: now}, banktransaction.CodeRest},
	} {
		_, err := txns.Categorise(ctx, b.Org, c.cat)
		var probs ledger.Problems
		if !errors.As(err, &probs) || !probs.Has(c.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if count(t, b.Org, "document") != before || lineOf(t, b.Org, b.lines[2].ID).Status != storage.LineUnreconciled {
		t.Fatal("a refused categorisation wrote something")
	}

	// Money out to suspense, then classified into rent at TX: the suspense posting is reversed, and Dr 6100 18.35; Dr 2200
	// 1.65 (20 × 0.09 ÷ 1.09 = 1.6514); Cr 1010 20.00.
	held, err := txns.Categorise(ctx, b.Org, whole(b.lines[2], b.Suspense, "NA"))
	if err != nil || held.Line.Status != storage.LineSuspense || held.Match.Kind != storage.MatchSuspense ||
		legsOf(t, s, b.Org, held.Entry) != "1010 -20 SGD = -20; 2999 20 SGD = 20" {
		t.Fatalf("to suspense: %s %+v %v", legsOf(t, s, b.Org, held.Entry), held, err)
	}
	if _, err := txns.Undo(ctx, b.Org, b.lines[2].ID, "Wrong", now); !isBankTxn(err, journal.CodeNotCategory) {
		t.Fatalf("an undo of a line in suspense: %v", err)
	}
	classified, err := txns.Categorise(ctx, b.Org, whole(b.lines[2], b.Rent, "TX"))
	if err != nil || classified.Line.Status != storage.LineCategorised || legsOf(t, s, b.Org, classified.Entry) != "1010 -20 SGD = -20; 6100 18.35 SGD = 18.35; 2200 1.65 SGD = 1.65" {
		t.Fatalf("classified: %s %+v %v", legsOf(t, s, b.Org, classified.Entry), classified, err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, held.Transaction.ID)
		if err != nil || doc.Status != storage.DocumentReversed {
			t.Errorf("the suspense transaction: %+v %v", doc, err)
		}
		_, err = u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(held.Entry.PostingIdentity))
		return err
	}); err != nil {
		t.Fatalf("the suspense posting's reversal: %v", err)
	}

	// Undone: the money in is reversed and the line is back in the queue.
	line, err := txns.Undo(ctx, b.Org, b.lines[0].ID, "Coded to the wrong account", now)
	if err != nil || line.Status != storage.LineUnreconciled {
		t.Fatalf("the undo: %+v %v", line, err)
	}
	if got := balances(t, s, b.Org); got["4000"] != "0.00" || got["2200"] != "1.65" || got["2999"] != "0.00" || got["1010"] != "-20.00" {
		t.Fatalf("after the undo: %v", got)
	}

	// USD 100.00 out of the wallet on 21 October at 1.35, in three parts at TX; a line of 22 October has no rate.
	usd := importLines(t, b.Org, b.usd, now,
		storage.NewBankLine{Position: 1, BookingDate: porttest.Day(10, 21), Amount: "-100.00", Description: "VERCEL", Fingerprint: fingerprint("usd1")},
		storage.NewBankLine{Position: 2, BookingDate: porttest.Day(10, 22), Amount: "-5.00", Description: "FEE", Fingerprint: fingerprint("usd2")})
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, 21), Rate: "1.35", Quote: "1.35",
			QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	split, err := txns.Categorise(ctx, b.Org, journal.Categorisation{Draft: banktransaction.Draft{LineID: usd[0].ID, Parts: []banktransaction.Part{
		{AccountID: b.Rent.ID, Amount: sum(t, "-33.33", "USD"), GSTCode: "TX"},
		{AccountID: b.Rent.ID, Amount: sum(t, "-33.33", "USD"), GSTCode: "TX"},
		{AccountID: b.Rent.ID, GSTCode: "TX"},
	}}, At: now})
	if err != nil || legsOf(t, s, b.Org, split.Entry) !=
		"1021 -100 USD = -135; 6100 41.28 SGD = 41.28; 6100 41.28 SGD = 41.28; 6100 41.29 SGD = 41.29; 2200 11.16 SGD = 11.16; 7030 -0.01 SGD = -0.01" {
		t.Fatalf("USD in three parts: %s %v", legsOf(t, s, b.Org, split.Entry), err)
	}
	_, err = txns.Categorise(ctx, b.Org, whole(usd[1], b.Rent, "TX"))
	var probs ledger.Problems
	if !errors.As(err, &probs) || !probs.Has(fx.CodeNoRate) || probs.Has(ledger.CodeMissingRate) {
		t.Fatalf("a USD line without a rate: %v", err)
	}
	check, err := txns.Check(ctx, b.Org, banktransaction.Draft{LineID: usd[0].ID, Parts: []banktransaction.Part{{AccountID: b.Rent.ID, GSTCode: "NA"}}})
	if err != nil || len(check.Problems) > 0 || len(check.Entry.Lines) != 2 || check.Saved.Rate == nil {
		t.Fatalf("a check of a categorised line's other coding: %+v %v", check, err)
	}

	// The audit trail names the person; the replay reproduces every entry.
	for want, id := range map[string]uuid.UUID{"bank_line.match_made": b.lines[2].ID, "bank_line.match_undone": b.lines[0].ID} {
		if got := actions(t, b.Org, id); !slices.Contains(got, want) {
			t.Errorf("no %s in %v", want, got)
		}
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		if len(r.Mismatches) > 0 || r.Entries != 6 {
			t.Errorf("the replay: %d entries, %+v", r.Entries, r.Mismatches)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := txns.Undo(ctx, b.Org, uuid.New(), "None", now); !errors.Is(err, ports.ErrNotFound) {
		t.Errorf("an undo of no line: %v", err)
	}
}

func isBankTxn(err error, code string) bool {
	var e *banktransaction.Error
	return errors.As(err, &e) && e.Code == code
}

// FR-GL-03 and Appendix B.1: a categorised line's spread part posts to 1210 (money out) or 2400 (money in), and its
// releases move it to the part's account month by month from the posting month: those due when the line is
// categorised at once, the rest by the hourly job bank.releases under the worker's role, a month with no open period
// waiting with an audit event. An undo reverses the releases on their own dates, leaving 1210 and the account at
// nothing. The replay reproduces every release.
// req: ledger/posting-rules Spreading a bank transaction over N months
// req: FR-GL-03
// req: ENG-05
// req: FR-AUT-01
// req: FR-AUT-02
func TestTheSchedulerReleasesASpreadBankTransaction(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	now := clock.NewFrozen(time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC))
	b := newCategoryBooks(t, "Bank Spreads Pte Ltd", now.Now())
	s := store(t, "fyniq_api")
	txns := journal.BankTransactions{Store: s, Clock: now}
	jobs := journal.Releases{Store: store(t, "fyniq_worker")}
	lines := importLines(t, b.Org, b.sgd, now.Now(),
		storage.NewBankLine{Position: 1, BookingDate: porttest.Day(10, 14), Amount: "-1308.00", Description: "ATLASSIAN", Fingerprint: fingerprint("spread1")},
		storage.NewBankLine{Position: 2, BookingDate: porttest.Day(10, 15), Amount: "1200.00", Description: "ACME ANNUAL", Fingerprint: fingerprint("spread2")})
	spread := func(line storage.BankLine, acc storage.Account, code string, months int) journal.Categorisation {
		return journal.Categorisation{Draft: banktransaction.Draft{LineID: line.ID, Parts: []banktransaction.Part{{AccountID: acc.ID, GSTCode: code, Months: months,
			Description: "A year"}}}, At: now.Now()}
	}
	release := func(id uuid.UUID, source, rule, month string) (storage.Entry, error) {
		m, _ := time.Parse("2006-01", month)
		var e storage.Entry
		err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			e, err = u.Ledger().ByIdentity(ctx, ledger.ReleaseIdentity(source, id, 1, 1, m, rule))
			return err
		})
		return e, err
	}

	// 1,308.00 out for four months of software at TX: 1,200.00 on 1210, and October's 300.00 released at once on the
	// line's date; November's is in the future.
	out, err := txns.Categorise(ctx, b.Org, spread(lines[0], b.Rent, "TX", 4))
	if err != nil || legsOf(t, s, b.Org, out.Entry) != "1010 -1308 SGD = -1308; 1210 1200 SGD = 1200; 2200 108 SGD = 108" {
		t.Fatalf("money out, spread: %s %v", legsOf(t, s, b.Org, out.Entry), err)
	}
	r, err := release(out.Transaction.ID, ledger.SourceBankOutRelease, ledger.RuleBankOutRelease, "2026-10")
	if err != nil || legsOf(t, s, b.Org, r) != "6100 300 SGD = 300; 1210 -300 SGD = -300" || !r.PostingDate.Equal(porttest.Day(10, 14)) {
		t.Fatalf("October's release: %s %v", legsOf(t, s, b.Org, r), err)
	}
	if _, err := release(out.Transaction.ID, ledger.SourceBankOutRelease, ledger.RuleBankOutRelease, "2026-11"); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("November's part before November: %v", err)
	}

	// 1,200.00 in for a year of subscriptions, no GST: 2400, with October's 100.00 released at once.
	in, err := txns.Categorise(ctx, b.Org, spread(lines[1], b.Sales, "NA", 12))
	if err != nil || legsOf(t, s, b.Org, in.Entry) != "1010 1200 SGD = 1200; 2400 -1200 SGD = -1200" {
		t.Fatalf("money in, spread: %s %v", legsOf(t, s, b.Org, in.Entry), err)
	}
	if r, err := release(in.Transaction.ID, ledger.SourceBankInRelease, ledger.RuleBankInRelease, "2026-10"); err != nil ||
		legsOf(t, s, b.Org, r) != "2400 100 SGD = 100; 4000 -100 SGD = -100" {
		t.Fatalf("October's revenue: %s %v", legsOf(t, s, b.Org, r), err)
	}

	// The job on 5 November releases November's parts, once; on 5 December, with no December period, they wait.
	run, err := jobs.ReleaseBankTransactionsDue(context.Background(), b.Org, porttest.Day(11, 5))
	if err != nil || run.Released != 2 || run.Waiting != 0 {
		t.Fatalf("the run of 5 November: %+v %v", run, err)
	}
	if run, err = jobs.ReleaseBankTransactionsDue(context.Background(), b.Org, porttest.Day(11, 5)); err != nil || run.Released != 0 {
		t.Fatalf("a second run: %+v %v", run, err)
	}
	if run, err = jobs.ReleaseBankTransactionsDue(context.Background(), b.Org, time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC)); err != nil || run.Waiting != 2 {
		t.Fatalf("the run of 5 December without a period: %+v %v", run, err)
	}
	var bodies []string
	for _, e := range events(t, b.Org) {
		bodies = append(bodies, e.Body)
	}
	trail := strings.Join(bodies, "\n")
	for _, want := range []string{`"actor":{"type":"scheduler","id":"bank.releases"}`, `"action":"bank_transaction.release_waiting"`} {
		if !strings.Contains(trail, want) {
			t.Errorf("the audit trail lacks %s", want)
		}
	}

	// Undone on 20 November: the money out and its releases are reversed on their own dates; 1210 and rent are at
	// nothing, and the job releases nothing more of it.
	now.Set(time.Date(2026, 11, 20, 4, 0, 0, 0, time.UTC))
	if _, err := txns.Undo(ctx, b.Org, lines[0].ID, "Not a prepayment", now.Now()); err != nil {
		t.Fatal(err)
	}
	if got := balances(t, s, b.Org); got["1210"] != "0.00" || got["6100"] != "0.00" || got["2400"] != "-1000.00" || got["4000"] != "-200.00" {
		t.Fatalf("after the undo: %v", got)
	}
	if run, err = jobs.ReleaseBankTransactionsDue(context.Background(), b.Org, porttest.Day(11, 30)); err != nil || run.Released != 0 {
		t.Fatalf("an undone line releases nothing: %+v %v", run, err)
	}
	if _, err := journal.ReleaseBankTransactionsDueAll(context.Background(), store(t, "fyniq_worker"), now.Now()); err != nil {
		t.Errorf("every organisation's run: %v", err)
	}

	// A release of the reversed transaction is refused by the database (FY227).
	if err := store(t, "fyniq_worker").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		m, _ := time.Parse("2006-01", "2026-12")
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(11, 30), RuleVersion: ledger.RuleBankOutRelease,
			PostingIdentity: ledger.ReleaseIdentity(ledger.SourceBankOutRelease, out.Transaction.ID, 1, 1, m, ledger.RuleBankOutRelease),
			Source:          storage.Source{Type: ledger.SourceBankOutRelease, ID: out.Transaction.ID.String(), Version: 1},
			Lines: []storage.NewLine{
				{AccountID: b.Rent.ID, Currency: "SGD", Amount: "300", FXRate: "1", FXRateSource: "base", BaseAmount: "300"},
				{AccountID: b.Prepaid.ID, Currency: "SGD", Amount: "-300", FXRate: "1", FXRateSource: "base", BaseAmount: "-300"},
			}})
		return err
	}); storage.CodeOf(err) != storage.CodeBillRelease {
		t.Fatalf("a release of an undone line: %v", err)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		if len(r.Mismatches) > 0 || r.Entries != 9 {
			t.Errorf("the replay: %d entries, %+v", r.Entries, r.Mismatches)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// FR-BNK-11 (task 4.5): a line that cannot be classified yet posts whole to 2999 and stays in the queue as suspense
// (⚠); classifying it reverses that posting and posts its category, leaving 2999 at nothing and the history intact:
// the suspense entry, its reversal and the category's entry. A chart without a suspense account refuses it.
// req: FR-BNK-11
// req: FR-GL-12
func TestALineInSuspenseIsClassified(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "bookkeeper"}})
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	b := newCategoryBooks(t, "Suspense Pte Ltd", now)
	s := store(t, "fyniq_api")
	txns := journal.BankTransactions{Store: s}

	held, err := txns.Suspend(ctx, b.Org, b.lines[2].ID, now)
	if err != nil || held.Line.Status != storage.LineSuspense || held.Match.Kind != storage.MatchSuspense ||
		legsOf(t, s, b.Org, held.Entry) != "1010 -20 SGD = -20; 2999 20 SGD = 20" {
		t.Fatalf("suspended: %s %+v %v", legsOf(t, s, b.Org, held.Entry), held, err)
	}
	if got := balances(t, s, b.Org); got["2999"] != "20.00" {
		t.Fatalf("2999 while the line waits: %v", got)
	}
	classified, err := txns.Categorise(ctx, b.Org, journal.Categorisation{Draft: banktransaction.Draft{LineID: b.lines[2].ID,
		Parts: []banktransaction.Part{{AccountID: b.Rent.ID, GSTCode: "NA"}}}, At: now})
	if err != nil || classified.Line.Status != storage.LineCategorised {
		t.Fatalf("classified: %+v %v", classified, err)
	}
	if got := balances(t, s, b.Org); got["2999"] != "0.00" || got["6100"] != "20.00" || got["1010"] != "-20.00" {
		t.Fatalf("after classifying: %v", got)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, id := range []string{held.Entry.PostingIdentity, ledger.ReversalIdentity(held.Entry.PostingIdentity), classified.Entry.PostingIdentity} {
			if _, err := u.Ledger().ByIdentity(ctx, id); err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
		}
		matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: &b.lines[2].ID, All: true})
		live := map[string]bool{}
		for _, m := range matches {
			live[m.Kind] = m.UndoneAt == nil
		}
		if err == nil && (len(matches) != 2 || live[storage.MatchSuspense] || !live[storage.MatchCategorised]) {
			t.Errorf("the line's matches: %+v", matches)
		}
		return err
	}); err != nil {
		t.Fatalf("the history: %v", err)
	}

	plain := newBankedBooks(t, "No Suspense Pte Ltd", now)
	_, err = journal.BankTransactions{Store: s}.Suspend(ctx, plain.Org, plain.lines[2].ID, now)
	var probs ledger.Problems
	if !errors.As(err, &probs) || !probs.Has(ledger.CodeSystemRole) {
		t.Errorf("a chart without 2999: %v", err)
	}
}
