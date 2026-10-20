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

// The executor numbers the documents of approved proposals with its own grants: it may set a sequence up
// with its defaults and take numbers, but not change a sequence's settings.
// req: FR-ORG-04
// req: SEC-03
// asvs: V13.2.2
func TestTheExecutorNumbersDocuments(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Executor numbers Pte Ltd")
	executor := store(t, "fyniq_executor")
	var number string
	if err := executor.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentJournal, Payload: []byte(`{"memo":"proposal"}`)})
		if err != nil {
			return err
		}
		number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentJournal)
		return err
	}); err != nil || number != "JNL-0001" {
		t.Fatalf("the executor numbering a document: %q, %v", number, err)
	}
	err := executor.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Sequences().Update(ctx, storage.DocumentJournal, 1, storage.SequenceSettings{Prefix: "X-", NextNumber: 9, Digits: 4, GapFree: true})
		return err
	})
	if err == nil {
		t.Error("the executor changed a sequence's settings")
	}
}

// The scheduler reverses each posted journal whose automatic reversal is due, on its reversal date, under its own
// principal and the worker's role; one whose period is not open waits, with an audit event, and is tried again on the
// next run; a run twice posts nothing twice. A reversal dated on or before its journal never posts (rev 0.55).
// req: FR-GL-02
// req: FR-AUT-01
// req: FR-AUT-02
func TestAutomaticReversals(t *testing.T) {
	ctx := context.Background()
	api := journal.Service{Store: store(t, "fyniq_api")}
	b := books(t, "Automatic reversals Pte Ltd")
	posted := func(date, reverseOn time.Time) uuid.UUID {
		t.Helper()
		d := saleDraft(t, b, date, "100.00")
		d.ReverseOn = reverseOn
		doc, err := api.Create(ctx, b.Org, d)
		if err == nil {
			_, err = api.Post(ctx, b.Org, doc.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
		return doc.ID
	}
	accrual := posted(porttest.Day(10, 31), porttest.Day(11, 1))
	early := posted(porttest.Day(10, 5), porttest.Day(10, 6))
	december := posted(porttest.Day(11, 20), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) // no period covers it yet
	later := posted(porttest.Day(11, 2), porttest.Day(11, 30))
	plain := posted(porttest.Day(10, 7), time.Time{})

	// A reversal on or before the journal's date never posts.
	bad := saleDraft(t, b, porttest.Day(10, 9), "5.00")
	bad.ReverseOn = porttest.Day(10, 9)
	doc, err := api.Create(ctx, b.Org, bad)
	if err != nil {
		t.Fatal(err)
	}
	var je *journal.Error
	if _, err := api.Post(ctx, b.Org, doc.ID); !errors.As(err, &je) || je.Code != journal.CodeReverseOn {
		t.Fatalf("a reversal dated on the journal's date: %v", err)
	}

	worker := journal.Service{Store: store(t, "fyniq_worker")}
	run, err := worker.ReverseDue(ctx, b.Org, time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if run.Reversed != 3 || run.Waiting != 1 {
		t.Fatalf("the first run: %+v", run)
	}
	s := store(t, "fyniq_api")
	for id, want := range map[uuid.UUID]storage.DocumentStatus{accrual: storage.DocumentReversed, early: storage.DocumentReversed,
		later: storage.DocumentReversed, december: storage.DocumentPosted, plain: storage.DocumentPosted} {
		if d := document(t, s, b.Org, id); d.Status != want {
			t.Errorf("journal %s is %s; want %s", id, d.Status, want)
		}
	}
	// The accrual's reversal is dated on its reversal date, and its audit events name the scheduler.
	if err := store(t, "fyniq_readonly").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		e, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(ledger.JournalIdentity(accrual, 1, ledger.RuleManualJournal)))
		if err != nil {
			return err
		}
		if !e.PostingDate.Equal(porttest.Day(11, 1)) || e.PeriodID != b.Nov.ID || e.Memo != "Reversal: "+journal.AutoReversalReason {
			t.Errorf("the accrual's reversal: %+v", e)
		}
		events, err := u.Audit().Events(ctx, 0, 10000)
		if err != nil {
			return err
		}
		var trail strings.Builder
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		for _, want := range []string{`"actor":{"type":"scheduler","id":"journals.auto-reverse"}`, `"action":"journal.reversal_waiting"`,
			`"waiting_for":[null,"STORAGE-PERIOD-01"]`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the audit trail lacks %s", want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A second run posts nothing twice; the December reversal still waits.
	again, err := worker.ReverseDue(ctx, b.Org, time.Date(2026, 12, 5, 0, 0, 0, 0, time.UTC))
	if err != nil || again.Reversed != 0 || again.Waiting != 1 {
		t.Fatalf("the second run: %+v %v", again, err)
	}
}

// Year-end without closing entries (FR-GL-10; rev 0.56): with the first financial year ending 31 October, November
// opens the second year. A revenue account opens it at nothing, retained earnings opens it with October's profit, and
// a balance-sheet account carries on; the position at November's end folds October's revenue into retained earnings
// and still nets to zero; a general ledger range across 1 November is refused. Nothing is posted for any of it.
// req: FR-GL-10
func TestYearEndFoldsEarlierYearsIntoRetainedEarnings(t *testing.T) {
	ctx := context.Background()
	s := store(t, "fyniq_api")
	b := books(t, "Year end Pte Ltd")
	var re storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		p := o.Profile
		start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), porttest.Day(10, 31)
		p.BooksStart, p.FirstYearEnd, p.FYStartMonth = &start, &end, 11
		if _, err := u.UpdateOrganisation(ctx, o.Version, p); err != nil {
			return err
		}
		re, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "3100", Name: "Retained earnings", Type: storage.Equity, Role: storage.RoleRetainedEarnings})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	svc := journal.Service{Store: s}
	for _, sale := range []struct {
		on     time.Time
		amount string
	}{{porttest.Day(10, 5), "1000.00"}, {porttest.Day(11, 3), "200.00"}} {
		doc, err := svc.Create(ctx, b.Org, saleDraft(t, b, sale.on, sale.amount))
		if err == nil {
			_, err = svc.Post(ctx, b.Org, doc.ID)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		opening := func(account uuid.UUID, want, wantClosing string) {
			t.Helper()
			gl, err := balance.GeneralLedger(ctx, u, sgd, account, porttest.Day(11, 1), porttest.Day(11, 30))
			if err != nil {
				t.Fatal(err)
			}
			if gl.Opening.String() != want || gl.Closing.String() != wantClosing {
				t.Errorf("account %s in November: opens %s, closes %s; want %s and %s", account, gl.Opening, gl.Closing, want, wantClosing)
			}
		}
		opening(b.Sales.ID, "0.00", "-200.00")   // revenue starts the year from nothing
		opening(re.ID, "-1000.00", "-1000.00")   // and October's profit is in retained earnings
		opening(b.Bank.ID, "1000.00", "1200.00") // a balance-sheet account carries on
		if _, err := balance.GeneralLedger(ctx, u, sgd, b.Sales.ID, porttest.Day(10, 1), porttest.Day(11, 30)); err == nil {
			t.Error("a range across the start of the year was read")
		} else if c := (*balance.CrossesYear)(nil); !errors.As(err, &c) || !c.YearStart.Equal(porttest.Day(11, 1)) {
			t.Errorf("a range across the start of the year: %v", err)
		}
		periods := []storage.Period{b.Oct, b.Nov}
		position := func(through storage.Period) map[uuid.UUID]string {
			t.Helper()
			tb, err := balance.Position(ctx, u, sgd, periods, through)
			if err != nil {
				t.Fatal(err)
			}
			if !tb.Total.IsZero() {
				t.Errorf("the position at %s does not net to zero: %s", through.EndsOn.Format(time.DateOnly), tb.Total)
			}
			out := map[uuid.UUID]string{}
			for _, r := range tb.Rows {
				out[r.AccountID] = r.Base.String()
			}
			return out
		}
		if oct := position(b.Oct); oct[b.Sales.ID] != "-1000.00" || oct[re.ID] != "" {
			t.Errorf("the position at the end of October: %v", oct)
		}
		if nov := position(b.Nov); nov[b.Sales.ID] != "-200.00" || nov[re.ID] != "-1000.00" || nov[b.Bank.ID] != "1200.00" {
			t.Errorf("the position at the end of November: %v", nov)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
