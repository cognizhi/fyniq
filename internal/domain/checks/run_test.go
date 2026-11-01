//go:build local

package checks_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/adapters/postgres/pgtest"
	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/chart"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var db *pgtest.DB

func TestMain(m *testing.M) {
	var err error
	if db, err = pgtest.Start(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "the checks' tests need Docker: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	db.Stop()
	os.Exit(code)
}

// The business day of the fixtures: October has ended and November has started, both open.
var now = time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC)

var ctx = storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "founder"}})

func open(t *testing.T, role string) *postgres.Store {
	t.Helper()
	s, err := postgres.Open(context.Background(), db.URL(role), postgres.WithClock(clock.NewFrozen(now)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

// books are an organisation with October and November 2026, and the standard chart when standard is true.
type books struct {
	api, worker *postgres.Store
	org         uuid.UUID
	oct, nov    storage.Period
	codes       map[string]storage.Account
}

var orgs int

func newBooks(t *testing.T, standard bool) books {
	t.Helper()
	orgs++
	b := books{api: open(t, "fyniq_api"), worker: open(t, "fyniq_worker"), codes: map[string]storage.Account{}}
	if !standard {
		pb := porttest.NewBooks(t, b.api, fmt.Sprintf("Checks %d Pte Ltd", orgs))
		b.org, b.oct, b.nov = pb.Org, pb.Oct, pb.Nov
		b.codes["1000"], b.codes["4000"] = pb.Bank, pb.Sales
		return b
	}
	o, err := b.api.CreateOrganisation(ctx, storage.NewOrganisation{Name: fmt.Sprintf("Checks %d Pte Ltd", orgs), BaseCurrency: "SGD"})
	if err != nil {
		t.Fatal(err)
	}
	b.org = o.ID
	accounts, err := chart.Standard()
	if err != nil {
		t.Fatal(err)
	}
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, a := range accounts {
			got, err := u.Accounts().Create(ctx, a.NewAccount)
			if err != nil {
				return err
			}
			b.codes[got.Code] = got
		}
		if b.oct, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(10, 1), EndsOn: porttest.Day(10, 31)}); err != nil {
			return err
		}
		b.nov, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(11, 1), EndsOn: porttest.Day(11, 30)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return b
}

// post posts a balanced SGD entry on day: debit to the first account, credit to the second.
func (b books) post(t *testing.T, id string, day time.Time, debit, credit, amount string) {
	t.Helper()
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: day, PostingIdentity: id, RuleVersion: "test@1",
			Source: storage.Source{Type: "manual_journal", ID: id, Version: 1},
			Lines: []storage.NewLine{
				{AccountID: b.codes[debit].ID, Currency: "SGD", Amount: storage.Decimal(amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal(amount)},
				{AccountID: b.codes[credit].ID, Currency: "SGD", Amount: storage.Decimal("-" + amount), FXRate: "1", FXRateSource: "base", BaseAmount: storage.Decimal("-" + amount)},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// tamper changes the books as only someone around every database control could (QA-ENG-05's attacker), so that the checks
// that a sound ledger never fails have something to find.
func (b books) tamper(t *testing.T, sql string, args ...any) {
	t.Helper()
	c, err := pgx.Connect(context.Background(), db.URL("fyniq"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	tx, err := c.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, s := range []string{"SET LOCAL session_replication_role = replica"} {
		if _, err := tx.Exec(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func (b books) runner() checks.Runner {
	return checks.Runner{Store: b.worker, Clock: clock.NewFrozen(now), Failed: func(_ context.Context, _ uuid.UUID, key string, err error) {
		panic(fmt.Sprintf("check %s failed to run: %v", key, err))
	}}
}

// run runs every check, as Run now does, and returns their states by key.
func (b books) run(t *testing.T) map[string]storage.CheckState {
	t.Helper()
	if _, err := b.runner().Request(ctx, b.org, "user:founder"); err != nil {
		t.Fatal(err)
	}
	return b.states(t)
}

func (b books) states(t *testing.T) map[string]storage.CheckState {
	t.Helper()
	out := map[string]storage.CheckState{}
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		states, err := u.Checks().States(ctx)
		for _, s := range states {
			out[s.Key] = s
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func (b books) trail(t *testing.T) string {
	t.Helper()
	var out strings.Builder
	if err := b.worker.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 100000)
		for _, e := range events {
			out.WriteString(e.Body + "\n")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func want(t *testing.T, states map[string]storage.CheckState, key string, status checks.Status) storage.CheckState {
	t.Helper()
	s, ok := states[key]
	if !ok {
		t.Errorf("%s did not run", key)
		return s
	}
	if checks.Status(s.Status) != status {
		t.Errorf("%s: %s (%d) %q, want %s", key, s.Status, s.Count, s.Detail, status)
	}
	return s
}

func itemsOf(t *testing.T, s storage.CheckState) []checks.Item {
	t.Helper()
	items, err := checks.ItemsOf(s)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// Sound books pass every check of the books, the setup and the ledger; the audit trail's check waits for its nightly
// verification, and says so.
// req: C.1 #1
// req: C.1 #2
// req: C.1 #3
// req: C.1 #4
// req: C.1 #5
// req: C.1 #6
// req: C.1 #7
// req: C.1 #23
// req: C.2 #1
// req: FR-CTL-01
func TestSoundBooksPassTheirChecks(t *testing.T) {
	b := newBooks(t, true)
	b.post(t, "sale-1", porttest.Day(10, 5), "1010", "4010", "100.00")
	b.post(t, "sale-2", porttest.Day(11, 2), "1010", "4010", "50.00")
	states := b.run(t)
	for _, key := range []string{"books.debits-equal-credits", "books.balance-sheet-balances", "books.cash-flow-ties", "books.pl-includes-every-account",
		"books.balance-sheet-includes-every-account", "setup.opening-balances-balance", "setup.system-accounts-exist",
		"books.equity-movement-reconciles", "ledger.balances-match-lines"} {
		s := want(t, states, key, checks.OK)
		if s.Count != 0 || s.RunAt.IsZero() || s.Trigger != storage.CheckOnRequest || s.LedgerRevision != 2 {
			t.Errorf("%s: %+v", key, s)
		}
	}
	chain := want(t, states, "audit.chain-intact", checks.NotAssessed)
	if !strings.Contains(chain.Needs, "audit.verify") {
		t.Errorf("the audit trail's check names %q", chain.Needs)
	}
	if len(states) != len(checks.Catalogue()) {
		t.Errorf("%d checks ran, the catalogue has %d", len(states), len(checks.Catalogue()))
	}
}

// Books changed around the database fail the checks that read what was changed: a balance changed without its lines
// fails debits and credits each month from the change on, and the balances against their lines; a line changed without
// its balances fails the balance sheet, the cash flow when it was a cash account's, and the balances against their lines.
// req: C.1 #1
// req: C.1 #2
// req: C.1 #3
// req: C.2 #1
// req: INV-08
func TestTamperedBooksFailTheirChecks(t *testing.T) {
	months := func(t *testing.T, s storage.CheckState, b books) {
		t.Helper()
		items := itemsOf(t, s)
		if s.Count != 2 || len(items) != 2 || items[0].Label != "October 2026" || items[1].Label != "November 2026" || items[0].Kind != "period" ||
			items[0].ID != b.oct.ID.String() {
			t.Errorf("%s names %+v", s.Key, items)
		}
	}
	t.Run("a balance", func(t *testing.T) {
		b := newBooks(t, true)
		b.post(t, "sale-1", porttest.Day(10, 5), "1010", "4010", "100.00")
		b.tamper(t, "UPDATE fyniq.balance SET base_amount = base_amount + 0.01 WHERE account_id = $1 AND period_id = $2", b.codes["1010"].ID, b.oct.ID)
		states := b.run(t)
		months(t, want(t, states, "books.debits-equal-credits", checks.Error), b)
		if s := want(t, states, "ledger.balances-match-lines", checks.Error); s.Count != 1 {
			t.Errorf("the balances found %d differences", s.Count)
		}
		want(t, states, "books.balance-sheet-balances", checks.OK) // the statements read the lines
	})
	t.Run("a posting to a group account", func(t *testing.T) { // INV-04 refuses it; the P&L leaves a group out
		b := newBooks(t, false)
		b.post(t, "sale-1", porttest.Day(10, 5), "1000", "4000", "30.00")
		for _, table := range []string{"journal_line", "balance"} {
			b.tamper(t, "UPDATE fyniq."+table+" SET account_id = (SELECT id FROM fyniq.account WHERE organisation_id = $1 AND code = '6000') "+
				"WHERE organisation_id = $1 AND account_id = (SELECT id FROM fyniq.account WHERE organisation_id = $1 AND code = '4000')", b.org)
		}
		s := want(t, b.run(t), "books.pl-includes-every-account", checks.Error)
		if items := itemsOf(t, s); len(items) != 1 || items[0].Label != "October 2026" || !strings.Contains(items[0].Detail, "differs from the ledger's by") {
			t.Errorf("the P&L names %+v", items)
		}
	})
	t.Run("a line", func(t *testing.T) {
		b := newBooks(t, true)
		b.post(t, "sale-1", porttest.Day(10, 5), "1010", "4010", "100.00")
		b.tamper(t, "UPDATE fyniq.journal_line SET base_amount = base_amount + 0.01, amount = amount + 0.01 WHERE account_id = $1", b.codes["1010"].ID)
		states := b.run(t)
		months(t, want(t, states, "books.balance-sheet-balances", checks.Error), b)
		want(t, states, "ledger.balances-match-lines", checks.Error)
		want(t, states, "books.debits-equal-credits", checks.OK) // the trial balance reads the balances
		if s := want(t, states, "books.cash-flow-ties", checks.Error); len(itemsOf(t, s)) != 1 || itemsOf(t, s)[0].Label != "October 2026" {
			t.Errorf("the cash flow names %+v", itemsOf(t, s))
		}
	})
}

// An account with a balance and no cash-flow line sits in no section of the balance sheet and no line of the cash flow:
// ⚠, named; and books with none of the system accounts name each role missing.
// req: C.1 #5
// req: C.1 #7
func TestAnUnplacedAccountAndMissingSystemAccountsAreFound(t *testing.T) {
	b := newBooks(t, false)
	b.post(t, "sale-1", porttest.Day(10, 5), "1000", "4000", "100.00")
	states := b.run(t)
	placed := want(t, states, "books.balance-sheet-includes-every-account", checks.Warning)
	if items := itemsOf(t, placed); len(items) == 0 || !strings.Contains(items[0].Detail, "1000 Bank") {
		t.Errorf("the unplaced account: %+v", items)
	}
	roles := want(t, states, "setup.system-accounts-exist", checks.Error)
	if roles.Count != len(storage.SystemRoles) || len(itemsOf(t, roles)) != len(storage.SystemRoles) || itemsOf(t, roles)[0].Kind != "role" {
		t.Errorf("the missing roles: %d %+v", roles.Count, itemsOf(t, roles))
	}
}

// Draft opening balances that do not net to zero cannot post: ✖ until they do.
// req: C.1 #6
// req: FR-GL-06
func TestUnbalancedOpeningBalancesAreFound(t *testing.T) {
	b := newBooks(t, true)
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		start := porttest.Day(10, 1)
		o.Profile.BooksStart = &start
		_, err = u.UpdateOrganisation(ctx, o.Version, o.Profile)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sgd, _ := money.Lookup("SGD")
	amount := func(s string) money.Money {
		m, err := money.Parse(s, sgd)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	save := func(credit string) {
		if _, err := (journal.Service{Store: b.api}).SaveOpening(ctx, b.org, "Opening", []ledger.JournalLine{
			{AccountID: b.codes["1010"].ID, Amount: amount("500.00")}, {AccountID: b.codes["3100"].ID, Amount: amount(credit)}}); err != nil {
			t.Fatal(err)
		}
	}
	save("-400.00")
	s := want(t, b.run(t), "setup.opening-balances-balance", checks.Error)
	if items := itemsOf(t, s); len(items) != 1 || items[0].Kind != "opening_balances" || !strings.Contains(items[0].Detail, "100.00 SGD") {
		t.Errorf("the unbalanced opening balances: %+v", items)
	}
	save("-500.00")
	want(t, b.run(t), "setup.opening-balances-balance", checks.OK)
}

// The audit trail's check reads the nightly verification's outcome: a failed verification is ✖ with its problem, a
// passed one OK, and a run with no new verification keeps what it had.
// req: C.2 #3
// req: INV-12
func TestTheAuditChainCheckReadsTheNightlyVerification(t *testing.T) {
	b := newBooks(t, true)
	verified := func(status, problem string) {
		if err := b.worker.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "audit.verified", ObjectType: "audit_chain", ObjectID: b.org,
				Changes: map[string][2]any{"status": {nil, status}, "problem": {nil, problem}}})
		}); err != nil {
			t.Fatal(err)
		}
	}
	verified("failed", "event 7 does not chain to event 6")
	if _, err := b.runner().Refresh(ctx, b.org); err != nil {
		t.Fatal(err)
	}
	s := want(t, b.states(t), "audit.chain-intact", checks.Error)
	if items := itemsOf(t, s); len(items) != 1 || items[0].Detail != "event 7 does not chain to event 6" {
		t.Errorf("the failed verification: %+v", items)
	}
	want(t, b.run(t), "audit.chain-intact", checks.Error) // no verification since: it keeps what it found
	verified("passed", "")
	if _, err := b.runner().Refresh(ctx, b.org); err != nil {
		t.Fatal(err)
	}
	want(t, b.states(t), "audit.chain-intact", checks.OK)
}

// Books with no open month that has started leave the month checks not assessed, naming what they need.
// req: FR-CTL-01
func TestAMonthCheckWithoutAMonthIsNotAssessed(t *testing.T) {
	b := newBooks(t, true)
	b.tamper(t, "UPDATE fyniq.period SET status = 'closed', closed_revision = 0 WHERE organisation_id = $1", b.org)
	s := want(t, b.run(t), "books.balance-sheet-balances", checks.NotAssessed)
	if s.Needs != "an open month that has started" || s.Setting == "" {
		t.Errorf("not assessed for want of %q in %q", s.Needs, s.Setting)
	}
}

// A run that finds what the last found leaves each check's status, count and items as they were and adds nothing to the
// audit trail; a change of status is audited once (design D11).
// req: FR-CTL-01
// req: AUD-01
func TestARunWithNoChangeChangesNothing(t *testing.T) {
	b := newBooks(t, false)
	b.post(t, "sale-1", porttest.Day(10, 5), "1000", "4000", "100.00")
	first := b.run(t)
	trail := b.trail(t)
	again := b.run(t)
	if got := b.trail(t); strings.Count(got, `"check.status_changed"`) != strings.Count(trail, `"check.status_changed"`) {
		t.Errorf("a rerun that found the same audited %d change(s)", strings.Count(got, `"check.status_changed"`)-strings.Count(trail, `"check.status_changed"`))
	}
	for key, s := range first {
		a := again[key]
		if a.Status != s.Status || a.Count != s.Count || string(a.Items) != string(s.Items) || !a.ChangedAt.Equal(s.ChangedAt) || a.Version < s.Version {
			t.Errorf("%s moved on a rerun: %+v → %+v", key, s, a)
		}
	}
	b.post(t, "sale-2", porttest.Day(10, 6), "1000", "4000", "1.00")
	if n := strings.Count(b.trail(t), `"check.status_changed"`); n != strings.Count(trail, `"check.status_changed"`) {
		t.Errorf("a posting that changes no status was audited as one: %d", n)
	}
}

// A change reruns the checks it concerns within one refresh, and only those; the nightly run runs every check and keeps
// its results; Run now records who asked.
// req: FR-CTL-01
// req: FR-CTL-05
func TestChecksRunOnAChangeNightlyAndOnRequest(t *testing.T) {
	b := newBooks(t, false)
	r := b.runner()
	if _, err := r.Refresh(ctx, b.org); err != nil { // the first refresh runs every check that reacts to a change, never run before
		t.Fatal(err)
	}
	first := b.states(t)
	if _, ok := first["ledger.balances-match-lines"]; ok {
		t.Error("a refresh ran a check that runs only nightly and on request")
	}
	if len(first) == 0 {
		t.Fatal("the first refresh ran nothing")
	}
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "1100", Name: "Accounts receivable", Type: storage.Asset, Role: "accounts-receivable"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rep, err := r.Refresh(ctx, b.org)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(rep.Run.Keys, ","), "setup.system-accounts-exist") || strings.Contains(strings.Join(rep.Run.Keys, ","), "setup.opening") {
		t.Errorf("a new account reran %v", rep.Run.Keys)
	}
	if got := b.states(t)["setup.system-accounts-exist"]; got.Count != len(storage.SystemRoles)-1 {
		t.Errorf("the refresh saw %d roles missing", got.Count)
	}
	if rep, err := r.Refresh(ctx, b.org); err != nil || len(rep.Run.Keys) != 0 {
		t.Errorf("a refresh with nothing new ran %v, %v", rep.Run.Keys, err)
	}
	nightly, err := r.Nightly(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, rep := range nightly {
		if rep.Organisation == b.org && (len(rep.Run.Keys) != len(checks.Catalogue()) || !strings.Contains(string(rep.Run.Results), `"C.1 #7"`)) {
			t.Errorf("the nightly run: %v %s", rep.Run.Keys, rep.Run.Results)
		}
	}
	req, err := r.Request(ctx, b.org, "user:founder")
	if err != nil || req.Run.RequestedBy != "user:founder" || req.Run.Trigger != storage.CheckOnRequest {
		t.Errorf("Run now: %+v, %v", req.Run, err)
	}
}

// The close and the catalogue say one thing (design D2): on the same books, each check the close runs itself agrees with
// the catalogue's check of it, and the close refuses itself on a catalogue check it does not run that found ✖.
// req: FR-CTL-01
// req: FR-GL-07
func TestTheCloseAndTheCatalogueAgree(t *testing.T) {
	for name, tampered := range map[string]bool{"sound books": false, "tampered balances": true} {
		t.Run(name, func(t *testing.T) {
			b := newBooks(t, true)
			b.post(t, "sale-1", porttest.Day(10, 5), "1010", "4010", "100.00")
			if tampered {
				b.tamper(t, "UPDATE fyniq.balance SET base_amount = base_amount + 0.01 WHERE account_id = $1 AND period_id = $2", b.codes["1010"].ID, b.oct.ID)
			}
			states := b.run(t)
			var close []closing.Check
			if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				close, err = closing.Checks(ctx, u, b.oct, porttest.Day(11, 10))
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for _, d := range checks.Catalogue() {
				for _, k := range d.CloseKeys {
					for _, c := range close {
						if c.Key != k {
							continue
						}
						s := states[d.Key]
						closeBad, catalogueBad := c.Severity != closing.OK, s.Status != string(checks.OK)
						if closeBad != catalogueBad {
							t.Errorf("the close's %s is %s, the catalogue's %s is %s", k, c.Severity, d.Key, s.Status)
						}
					}
				}
			}
		})
	}
	b := newBooks(t, false) // missing system accounts: ✖ in the catalogue, which the close does not check itself
	b.run(t)
	var more []closing.Check
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		more, err = checks.ForClose(ctx, u, b.oct, porttest.Day(11, 10))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(more) != 1 || more[0].Key != "checks.setup.system-accounts-exist" || more[0].Severity != closing.Failed {
		t.Errorf("the close's checks from the catalogue: %+v", more)
	}
	cc, err := closing.Service{Store: b.api, Clock: clock.NewFrozen(now), More: checks.ForClose}.RunChecks(ctx, b.org, b.oct.ID)
	if err != nil || cc.Clean {
		t.Errorf("the close ran clean with a system account missing: %+v, %v", cc, err)
	}
}

// The checks of the bank's lines (rev 0.164): a waiting line that cannot post as it stands, dated where no period is or
// with no rate for its day (C.1 #10); a line in suspense (C.1 #11); a line booked after today (C.1 #12). A line that
// merely waits is none of them.
// req: C.1 #10
// req: C.1 #11
// req: C.1 #12
func TestTheBankLinesChecks(t *testing.T) {
	b := newBooks(t, true)
	at := time.Date(2026, 11, 10, 2, 0, 0, 0, time.UTC)
	var sgd, usd storage.MoneyAccount
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, x := range []struct {
			code, name, cur, ident string
			dst                    *storage.MoneyAccount
		}{{"1015", "DBS current", "SGD", "072-123456-7", &sgd}, {"1021", "DBS USD", "USD", "072-999999-9", &usd}} {
			a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: x.code, Name: x.name, Type: storage.Asset,
				AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: x.cur, ReportGroup: "Cash & bank", CashFlowLine: "Cash & cash equivalents"}})
			if err != nil {
				return err
			}
			if *x.dst, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: a.ID, Kind: storage.MoneyBank, Name: x.name,
				Identifier: x.ident, Currency: x.cur, At: at}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"bank.lines-cannot-post", "bank.lines-in-suspense", "bank.lines-in-the-future"} {
		want(t, b.run(t), key, checks.OK)
	}
	statement := func(account *uuid.UUID, file string) []storage.BankLine {
		t.Helper()
		var out []storage.BankLine
		if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := banking.Import(ctx, u, banking.ImportRequest{Format: bankfile.FormatMT940, Data: []byte(file), Account: account,
				Source: storage.MoneySourceFile, At: at}); err != nil {
				return err
			}
			var err error
			out, err = u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: account})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	mt := func(account, open, close string, lines ...string) string {
		s := ":20:S\n:25:" + account + "\n:60F:" + open + "\n"
		for i, l := range lines {
			s += ":61:" + l + "NTRFNONREF//R" + fmt.Sprint(i) + "\n:86:LINE " + fmt.Sprint(i) + "\n"
		}
		return s + ":62F:" + close + "\n"
	}
	// SGD: one line in October (it can post once coded), one in September (no period), one in December (after today, and
	// no period either).
	sgdLines := statement(&sgd.ID, mt("0721234567", "C260901SGD1000,00", "C261231SGD1300,00", "260915C100,00", "261005C100,00", "261201C100,00"))
	// USD: one line in October, and no USD rate for its day.
	usdLines := statement(&usd.ID, mt("072-999999-9", "C261001USD0,00", "C261031USD50,00", "261006C50,00"))
	byDay := map[string]storage.BankLine{}
	for _, l := range append(sgdLines, usdLines...) {
		byDay[l.Currency+" "+l.BookingDate.Format(time.DateOnly)] = l
	}
	states := b.run(t)
	cannot := want(t, states, "bank.lines-cannot-post", checks.Error)
	found := map[string]string{}
	for _, it := range itemsOf(t, cannot) {
		found[it.ID] = it.Detail
	}
	if !strings.Contains(found[byDay["SGD 2026-09-15"].ID.String()], "no period covers 2026-09-15") ||
		!strings.Contains(found[byDay["USD 2026-10-06"].ID.String()], "there is no USD rate for 2026-10-06") ||
		!strings.Contains(found[byDay["SGD 2026-12-01"].ID.String()], "no period covers 2026-12-01") {
		t.Errorf("the lines that cannot post: %v", found)
	}
	if _, ok := found[byDay["SGD 2026-10-05"].ID.String()]; ok || len(found) != 3 {
		t.Errorf("a line that merely waits is listed: %v", found)
	}
	future := want(t, states, "bank.lines-in-the-future", checks.Warning)
	if items := itemsOf(t, future); len(items) != 1 || items[0].ID != byDay["SGD 2026-12-01"].ID.String() {
		t.Errorf("the line after today: %+v", items)
	}
	// The October line posted to suspense waits there for review.
	if _, err := (journal.BankTransactions{Store: b.api, Clock: clock.NewFrozen(now)}).Suspend(ctx, b.org, byDay["SGD 2026-10-05"].ID, at); err != nil {
		t.Fatal(err)
	}
	suspense := want(t, b.run(t), "bank.lines-in-suspense", checks.Warning)
	if items := itemsOf(t, suspense); len(items) != 1 || items[0].ID != byDay["SGD 2026-10-05"].ID.String() || !strings.HasPrefix(items[0].Label, "DBS current, 5 Oct 2026") {
		t.Errorf("the line in suspense: %+v", items)
	}
}
