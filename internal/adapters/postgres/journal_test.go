//go:build local

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/seed"
)

// The manual journal end to end: the journal service, the engine and the balance service against
// PostgreSQL (p0-money-posting-engine).

var (
	sgd = money.MustLookup("SGD")
	usd = money.MustLookup("USD")
)

func amount(t *testing.T, s string, c money.Currency) money.Money {
	t.Helper()
	m, err := money.Parse(s, c)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func saleDraft(t *testing.T, b porttest.Books, date time.Time, amt string) journal.Draft {
	return journal.Draft{Date: date, Memo: "Retainer", Lines: []ledger.JournalLine{
		{AccountID: b.Bank.ID, Amount: amount(t, amt, sgd)},
		{AccountID: b.Sales.ID, Amount: amount(t, "-"+amt, sgd), Description: "Acme Analytics"},
	}}
}

func revision(t *testing.T, s storage.Store, org uuid.UUID) int64 {
	t.Helper()
	var rev int64
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		rev, err = u.Ledger().Revision(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return rev
}

func document(t *testing.T, s storage.Store, org, id uuid.UUID) storage.Document {
	t.Helper()
	var d storage.Document
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		d, err = u.Documents().Get(ctx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return d
}

// req: ENG-02
func TestPostingCommitsWithTheDocumentOrNotAtAll(t *testing.T) {
	ctx := context.Background()
	s := store(t, "fyniq_api")
	b := books(t, "ENG-02 Pte Ltd")
	boom := errors.New("the audit event could not be written")
	failing := journal.Service{Store: s, AfterPosting: func(context.Context, storage.UnitOfWork, storage.Document, storage.Entry) error { return boom }}
	doc, err := failing.Create(ctx, b.Org, saleDraft(t, b, porttest.Day(10, 14), "250.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.Post(ctx, b.Org, doc.ID); !errors.Is(err, boom) {
		t.Fatalf("Post with a failing later step = %v; want the step's error", err)
	}
	if d := document(t, s, b.Org, doc.ID); d.Status != storage.DocumentDraft {
		t.Fatalf("the document is %s after a failed posting; want draft", d.Status)
	}
	if rev := revision(t, s, b.Org); rev != 0 {
		t.Fatalf("revision %d after a failed posting; want 0: neither the entry nor the document change was saved", rev)
	}
	if n := count(t, b.Org, "journal_line"); n != 0 {
		t.Fatalf("%d lines after a failed posting", n)
	}
	e, err := journal.Service{Store: s}.Post(ctx, b.Org, doc.ID)
	if err != nil || e.LedgerRevision != 1 {
		t.Fatalf("Post = %+v, %v", e, err)
	}
	if d := document(t, s, b.Org, doc.ID); d.Status != storage.DocumentPosted {
		t.Fatalf("the document is %s after posting; want posted", d.Status)
	}
}

// req: ENG-03
// req: ENG-14
// req: data/data-model Insert-only ledger records
// req: data/data-model Accounting date and recorded time
func TestPostedEntriesCarryTheirProvenance(t *testing.T) {
	ctx := context.Background()
	s := store(t, "fyniq_api")
	b := books(t, "ENG-03 Pte Ltd")
	r, err := money.ParseRate("1.3472", usd, sgd, "document")
	if err != nil {
		t.Fatal(err)
	}
	svc := journal.Service{Store: s}
	doc, err := svc.Create(ctx, b.Org, journal.Draft{Date: porttest.Day(10, 20), Lines: []ledger.JournalLine{
		{AccountID: b.Bank.ID, Amount: amount(t, "37.11", usd), Rate: &r}, // 49.994392 → 49.99
		{AccountID: b.Sales.ID, Amount: amount(t, "-49.99", sgd)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := svc.Post(ctx, b.Org, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := storage.Source{Type: "manual_journal", ID: doc.ID.String(), Version: 1}
	if e.Source != want || e.RuleVersion != "manual_journal@1" || e.LedgerRevision != 1 || e.PeriodID != b.Oct.ID ||
		e.PostingIdentity != ledger.JournalIdentity(doc.ID, 1, ledger.RuleManualJournal) || e.RecordedAt.IsZero() ||
		!e.PostingDate.Equal(porttest.Day(10, 20)) {
		t.Fatalf("entry provenance = %+v", e)
	}
	usdLine := e.Lines[0]
	if usdLine.Currency != "USD" || usdLine.Amount != "37.1100" || usdLine.FXRate != "1.3472000000" ||
		usdLine.FXRateSource != "document" || usdLine.BaseCurrency != "SGD" || usdLine.BaseAmount != "49.9900" {
		t.Fatalf("the USD line = %+v", usdLine)
	}
}

// req: FR-GL-12
// req: QA-ENG-03
// req: data/data-model Insert-only ledger records
func TestReversalUndoesAPostedJournal(t *testing.T) {
	ctx := context.Background()
	s := store(t, "fyniq_api")
	b := books(t, "Reversal Pte Ltd")
	svc := journal.Service{Store: s}
	doc, err := svc.Create(ctx, b.Org, saleDraft(t, b, porttest.Day(10, 5), "1000.00"))
	if err != nil {
		t.Fatal(err)
	}
	original, err := svc.Post(ctx, b.Org, doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	var je *journal.Error
	if _, err := svc.Post(ctx, b.Org, doc.ID); !errors.As(err, &je) || je.Code != journal.CodeNotDraft {
		t.Fatalf("posting it again: want %s, got %v", journal.CodeNotDraft, err)
	}
	rev, err := svc.Reverse(ctx, b.Org, doc.ID, porttest.Day(11, 2), "entered twice")
	if err != nil {
		t.Fatal(err)
	}
	if rev.ReversesEntryID == nil || *rev.ReversesEntryID != original.ID || rev.PeriodID != b.Nov.ID ||
		rev.Memo != "Reversal: entered twice" || rev.Lines[0].BaseAmount != "-1000.0000" {
		t.Fatalf("reversal = %+v", rev)
	}
	if _, err := svc.Reverse(ctx, b.Org, doc.ID, porttest.Day(11, 3), "again"); !errors.As(err, &je) || je.Code != journal.CodeNotPosted {
		t.Fatalf("reversing twice: want %s, got %v", journal.CodeNotPosted, err)
	}
	if d := document(t, s, b.Org, doc.ID); d.Status != storage.DocumentReversed {
		t.Fatalf("document status %s; want reversed", d.Status)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		tb, err := balance.Trial(ctx, u, sgd, b.Oct.ID, b.Nov.ID)
		if err != nil {
			return err
		}
		for _, r := range tb.Rows {
			if !r.Base.IsZero() {
				t.Errorf("account %s nets to %s after the reversal; want 0", r.AccountID, r.Base)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A draft that does not balance is refused by the engine and stays a draft (ENG-22).
	bad := saleDraft(t, b, porttest.Day(10, 6), "10.00")
	bad.Lines[1].Amount = amount(t, "-9.99", sgd)
	doc2, err := svc.Create(ctx, b.Org, bad)
	if err != nil {
		t.Fatal(err)
	}
	var probs ledger.Problems
	if _, err := svc.Post(ctx, b.Org, doc2.ID); !errors.As(err, &probs) || !probs.Has(ledger.CodeUnbalancedBase) {
		t.Fatalf("an unbalanced draft: want %s, got %v", ledger.CodeUnbalancedBase, err)
	}
	if d := document(t, s, b.Org, doc2.ID); d.Status != storage.DocumentDraft {
		t.Fatalf("the refused draft is %s", d.Status)
	}
}

// accountType is the type of a sample account by its code's first digit, the workbook's ranges.
func accountType(code string) storage.AccountType {
	switch code[0] {
	case '1':
		return storage.Asset
	case '2':
		return storage.Liability
	case '3':
		return storage.Equity
	case '4':
		return storage.Revenue
	}
	return storage.Expense
}

// cents reads a seed amount ("1234.56") as integer cents without the engine.
func cents(t *testing.T, s string) int64 {
	t.Helper()
	var whole, frac int64
	if _, err := fmt.Sscanf(strings.Replace(s, ".", " ", 1), "%d %d", &whole, &frac); err != nil {
		t.Fatalf("seed amount %q: %v", s, err)
	}
	return whole*100 + frac
}

// req: ENG-04
// req: INV-09
// req: DEV-04
func TestSampleCompanyGoldenTrialBalance(t *testing.T) {
	ctx := context.Background()
	s := store(t, "fyniq_api")
	vocab, err := seed.LoadVocabulary("../../../docs/design/screens/sample-data.json")
	if err != nil {
		t.Fatal(err)
	}
	company := seed.Generate(vocab, 1, 24) // what `make seed` writes
	org, err := s.CreateOrganisation(ctx, storage.NewOrganisation{Name: company.Organisation.Name, BaseCurrency: company.Organisation.BaseCurrency})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]uuid.UUID{}
	codes := map[uuid.UUID]string{}
	var periods []uuid.UUID
	if err := s.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, a := range company.Accounts {
			acc, err := u.Accounts().Create(ctx, storage.NewAccount{Code: a.Code, Name: a.Name, Type: accountType(a.Code), Role: storage.AccountRole(a.Role)})
			if err != nil {
				return err
			}
			ids[a.Code], codes[acc.ID] = acc.ID, a.Code
		}
		for m := time.January; m <= time.December; m++ {
			start := time.Date(2026, m, 1, 0, 0, 0, 0, time.UTC)
			p, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: start, EndsOn: start.AddDate(0, 1, -1)})
			if err != nil {
				return err
			}
			periods = append(periods, p.ID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The expected totals, from the seed's own decimal strings in integer cents: never from the engine.
	want := map[string]int64{}
	svc := journal.Service{Store: s}
	var firstDoc uuid.UUID
	for i, jnl := range company.Journals {
		date, err := time.Parse(time.DateOnly, jnl.Date)
		if err != nil {
			t.Fatal(err)
		}
		d := journal.Draft{Date: date, Memo: jnl.ID}
		for _, l := range jnl.Lines {
			signed := cents(t, l.Debit) - cents(t, l.Credit)
			want[l.Account] += signed
			d.Lines = append(d.Lines, ledger.JournalLine{AccountID: ids[l.Account], Amount: money.FromMinor(signed, sgd)})
		}
		doc, err := svc.Create(ctx, org.ID, d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Post(ctx, org.ID, doc.ID); err != nil {
			t.Fatalf("%s: %v", jnl.ID, err)
		}
		if i == 0 {
			firstDoc = doc.ID
		}
	}
	// Reverse the first journal: its lines come off every total again.
	first := company.Journals[0]
	date, _ := time.Parse(time.DateOnly, first.Date)
	if _, err := svc.Reverse(ctx, org.ID, firstDoc, date.AddDate(0, 0, 1), "golden test"); err != nil {
		t.Fatal(err)
	}
	for _, l := range first.Lines {
		want[l.Account] -= cents(t, l.Debit) - cents(t, l.Credit)
	}

	var tb balance.TrialBalance
	if err := s.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
		tb, err = balance.Trial(ctx, u, sgd, periods...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !tb.Total.IsZero() {
		t.Fatalf("the trial balance nets to %s; want 0 (INV-09)", tb.Total)
	}
	got := map[string]int64{}
	for _, r := range tb.Rows {
		n, _ := r.Base.Minor()
		got[codes[r.AccountID]] = n
	}
	var lines []string
	for code, w := range want {
		if got[code] != w {
			t.Errorf("account %s: the balance service says %d cents; the seed says %d", code, got[code], w)
		}
		lines = append(lines, fmt.Sprintf("%s %s", code, money.FromMinor(w, sgd)))
	}
	for code := range got {
		if _, ok := want[code]; !ok {
			t.Errorf("account %s has a balance the seed never posted", code)
		}
	}
	sort.Strings(lines)
	golden(t, "sample-company-trial-balance.golden",
		"# The sample company (seed 1, 24 journals; the first reversed): base totals for 2026 by account.\n"+
			"# Computed from the seed's amounts in integer cents, never by the engine; the oracle checks the same figures.\n"+
			strings.Join(lines, "\n")+"\n")

	// The figures the balance service returns are the cache the database proves equal to the lines.
	worker := store(t, "fyniq_worker")
	if err := worker.Do(ctx, org.ID, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := u.Balances().Differences(ctx)
		if err == nil && len(d) != 0 {
			t.Errorf("the cache differs from the lines: %+v", d)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
