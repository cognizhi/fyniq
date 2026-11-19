//go:build local

package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/bankfeed"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/domain/banking"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/psp"
)

// fakeLedgerProvider is a payment provider with a balance-transaction history in SGD: it is the bank-data feed of the
// clearing account (a gross line and a fee line for each transaction, as Stripe's adapter makes them) and the provider's
// objects, so a sweep and a reconciliation run against it as against Stripe.
type fakeLedgerProvider struct {
	mu       sync.Mutex
	txns     []ports.ProviderObject
	payouts  map[string][]string // payout ID to the balance transactions it settles
	balance  string
	status   map[string]string
	failList error
}

func bt(id, category, gross, fee, net string, day int, source ports.ProviderKind, src string) ports.ProviderObject {
	return ports.ProviderObject{Kind: ports.ProviderBalanceTransaction, ID: id, Currency: "SGD", Type: category, Category: category, Amount: gross, Fee: fee, Net: net,
		Created: porttest.Day(10, day).Add(10 * time.Hour), Links: map[ports.ProviderKind][]string{source: {src}}}
}

func (f *fakeLedgerProvider) Name() string { return "example-ledger-psp" }
func (f *fakeLedgerProvider) Accounts(context.Context) ([]ports.BankDataAccount, error) {
	return []ports.BankDataAccount{{ID: "SGD", Currency: "SGD", Name: "SGD"}}, nil
}
func (f *fakeLedgerProvider) Transactions(_ context.Context, _, cursor string, _ time.Time) (ports.BankDataPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out ports.BankDataPage
	out.Cursor = "all" // the provider sends everything every time, with no running balance: import's reference check keeps each line once
	_ = cursor
	for _, t := range f.txns {
		out.Transactions = append(out.Transactions, ports.BankDataTransaction{ID: t.ID, BookingDate: t.Created, Amount: t.Amount, Currency: "SGD", Counterparty: "Provider",
			Description: t.Category + " " + t.ID})
		if t.Fee != "0.00" {
			neg := "-" + t.Fee
			if strings.HasPrefix(t.Fee, "-") {
				neg = strings.TrimPrefix(t.Fee, "-")
			}
			out.Transactions = append(out.Transactions, ports.BankDataTransaction{ID: t.ID + ":fee", BookingDate: t.Created, Amount: neg, Currency: "SGD", Counterparty: "Provider",
				Description: "Provider fee " + t.ID})
		}
	}
	return out, nil
}
func (f *fakeLedgerProvider) Object(_ context.Context, kind ports.ProviderKind, id string) (ports.ProviderObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if kind != ports.ProviderPayout {
		return ports.ProviderObject{}, ports.ErrProviderNotFound
	}
	for _, t := range f.txns {
		if t.Category == "payout" && t.Links[ports.ProviderPayout][0] == id {
			return ports.ProviderObject{Kind: kind, ID: id, Status: "paid", Currency: "SGD", Amount: strings.TrimPrefix(t.Amount, "-"), Created: t.Created,
				Links: map[ports.ProviderKind][]string{ports.ProviderBalanceTransaction: f.payouts[id]}}, nil
		}
	}
	return ports.ProviderObject{}, ports.ErrProviderNotFound
}
func (f *fakeLedgerProvider) List(_ context.Context, kind ports.ProviderKind, from, to time.Time, _ string) (ports.ProviderPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failList != nil {
		return ports.ProviderPage{}, f.failList
	}
	var page ports.ProviderPage
	for _, t := range f.txns {
		if t.Created.Before(from) || !t.Created.Before(to) {
			continue
		}
		switch kind {
		case ports.ProviderBalanceTransaction:
			page.Objects = append(page.Objects, t)
		case ports.ProviderPayout:
			if t.Category == "payout" {
				page.Objects = append(page.Objects, ports.ProviderObject{Kind: kind, ID: t.Links[ports.ProviderPayout][0], Status: "paid", Currency: "SGD",
					Amount: strings.TrimPrefix(t.Amount, "-"), Created: t.Created})
			}
		}
	}
	return page, nil
}
func (f *fakeLedgerProvider) Balance(context.Context) ([]ports.ProviderBalance, error) {
	return []ports.ProviderBalance{{Currency: "SGD", Amount: f.balance}}, nil
}

func fakeLedgerConnector(t testing.TB, f *fakeLedgerProvider) connector.Binding {
	t.Helper()
	m, err := connector.Parse([]byte(`{"id":"example-ledger-psp","name":"Example provider","publisher":"Fyniq","version":"1.0.0","min_platform":"0.1.0",
	  "summary":"A fake payment provider.","scope":"organisation","capabilities":[
	  {"kind":"payment-provider","trigger":"system-rule","trigger_rule":"A verified webhook or the sweep.","on_pause":"hold","on_disable":"hold","in_flight":"finish","parser_version":"example@1"},
	  {"kind":"bank-data","trigger":"system-rule","trigger_rule":"The sync schedule.","on_pause":"hold","on_disable":"hold","in_flight":"finish"}],
	  "configuration":{"type":"object","additionalProperties":false,"properties":{}},
	  "secrets":[{"name":"token","title":"Token","description":"Its read-only key.","used_by":["worker"],"rotation_overlap_hours":1}],
	  "provider_scopes":[],"fyniq_scopes":[],"data_classes":[],"regions":["SG"],
	  "destinations":[{"host":"psp.example.com"}],"webhooks":[],"health_check":"Counts.","test_action":"Reads.","local_substitute":"A fake.",
	  "limits":{"requests_per_minute":6000,"breaker_failures":50,"breaker_cooldown_seconds":60}}`))
	if err != nil {
		t.Fatal(err)
	}
	return connector.Binding{Manifest: m, BankData: func(connector.Config) (ports.BankData, error) { return f, nil },
		PaymentProvider: func(connector.Config) (ports.PaymentProvider, error) { return f, nil }}
}

// G4-041, UC-PSP-03, UC-PSP-04, FR-PSP-04, FR-PSP-05, FR-PSP-06: a provider's account is a money account its connector feeds.
// A sweep mirrors its balance transactions, payouts and balance; its reconciler then posts, once each and as the connector,
// the fee lines (to the fee account), the dispute's money (to disputes clearing) and the payout (a transfer to the bank), and
// leaves a charge for the document it settles. The bank's own line then completes the transfer. Each settled payout nets to
// zero in clearing (C.2 #42), and what is left agrees to the provider's own balance (C.2 #50); a charge no one has posted
// breaks both, and a second run posts nothing twice.
// req: banking/payment-providers Payout breakdown
// req: banking/payment-providers Each settled payout nets to zero
// req: banking/payment-providers Dispute moves to disputes clearing
// req: banking/payment-providers Month-end clearing equals the provider balance
// req: C.2 #42
// req: C.2 #50
func TestAProviderAccountIsSweptAndItsLinesReconciledOnce(t *testing.T) {
	e := newEnv(t)
	f := &fakeLedgerProvider{payouts: map[string][]string{"po_1": {"txn_c1", "txn_c2"}}, balance: "31.10"}
	f.txns = []ports.ProviderObject{
		bt("txn_c1", "charge", "100.00", "3.90", "96.10", 10, ports.ProviderCharge, "ch_1"),
		bt("txn_c2", "charge", "100.00", "3.90", "96.10", 10, ports.ProviderCharge, "ch_2"),
		bt("txn_c3", "charge", "100.00", "3.90", "96.10", 11, ports.ProviderCharge, "ch_3"),
		bt("txn_d1", "dispute", "-50.00", "15.00", "-65.00", 11, ports.ProviderDispute, "dp_1"),
		bt("txn_po1", "payout", "-192.20", "0.00", "-192.20", 12, ports.ProviderPayout, "po_1"),
	}
	b := fakeLedgerConnector(t, f)
	cat, err := connector.NewCatalogue(b)
	if err != nil {
		t.Fatal(err)
	}
	rt := &connector.Runtime{Catalogue: cat, Secrets: e.api.deps.Connectors.Runtime.Secrets, Egress: &egress.Guard{Local: true}, Clock: e.clock}
	var c storage.InstalledConnector
	m := b.Manifest
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: m.ID, ManifestVersion: m.Version, ManifestHash: m.Hash(),
			SecretNames: []string{"token"}, Consent: m.Consent(), Position: 1, At: e.clock.Now()}); err != nil {
			return err
		}
		for _, step := range []struct{ s, state string }{{storage.StepConfigure, storage.ConnectorConfigured}, {storage.StepTest, storage.ConnectorTested},
			{storage.StepEnable, storage.ConnectorEnabled}} {
			if step.s == storage.StepTest {
				c.Test = &storage.ConnectorTest{Passed: true, Reason: "Read.", At: e.clock.Now()}
			}
			if c, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: step.s, State: step.state,
				Configuration: map[string]any{}, Position: 1, Test: c.Test, ManifestVersion: m.Version, ManifestHash: m.Hash(), SecretNames: []string{"token"},
				Consent: m.Consent(), At: e.clock.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	// The accounts: the provider's clearing money account fed by the connector, the bank its payouts land in, and the chart
	// accounts for fees and disputes.
	clearing := e.fedAccount("SGD", c)
	bankAcc := e.chartMoneyAccount()
	var bank storage.MoneyAccount
	var disputes storage.Account
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if bank, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: bankAcc.ID, Kind: storage.MoneyBank, Name: bankAcc.Name,
			Identifier: "BANK-" + bankAcc.Code, Currency: "SGD", At: e.clock.Now()}); err != nil {
			return err
		}
		if disputes, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1055", Name: "Payment disputes clearing", Type: storage.Asset}); err != nil {
			return err
		}
		_, err = u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: clearing.ID, ConnectorID: c.ID, PayoutAccountID: &bank.ID,
			FeeAccountID: e.books.Rent.ID, DisputesAccountID: &disputes.ID, At: e.clock.Now()}, 0)
		return err
	})
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	txns := journal.BankTransactions{Store: e.store, Clock: e.clock}
	runner := psp.Runner{Store: worker, Runtime: rt, Clock: e.clock}
	sync := bankfeed.Syncer{Store: worker, Runtime: rt, Clock: e.clock}
	ctx := context.Background()
	lineOf := func(ref string) storage.BankLine {
		t.Helper()
		var out storage.BankLine
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			ls, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &clearing.ID})
			for _, l := range ls {
				if l.Reference == ref {
					out = l
				}
			}
			return err
		})
		if out.ID == uuid.Nil {
			t.Fatalf("no line %s", ref)
		}
		return out
	}
	statuses := func() map[string]string {
		out := map[string]string{}
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			ls, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &clearing.ID})
			for _, l := range ls {
				out[l.Reference] = l.Status
			}
			return err
		})
		return out
	}

	// The bank sync brings the lines; the sweep mirrors what the provider reports (the worker posts nothing); Reconcile now,
	// which is auto-reconcile in the API, posts what is certain.
	if got, err := sync.Sync(ctx, e.books.Org, c.ID); err != nil || got.Added != 9 || len(got.Errors) != 0 {
		t.Fatalf("the sync: %+v, %v", got, err)
	}
	res, err := runner.Run(ctx, e.books.Org, c.ID)
	if err != nil || len(res.Errors) != 0 {
		t.Fatalf("the run: %+v, %v", res, err)
	}
	if res.Transactions != 5 || res.Payouts != 1 {
		t.Fatalf("mirrored %d transactions and %d payouts: %+v", res.Transactions, res.Payouts, res)
	}
	if st := statuses(); st["txn_c1:fee"] != storage.LineUnreconciled || st["txn_po1"] != storage.LineUnreconciled {
		t.Fatalf("the worker posted: %v", st)
	}
	auto, err := banking.AutoReconcile(ctx, txns, e.books.Org, nil, e.clock.Now())
	if err != nil || len(auto.Reconciled) != 6 {
		t.Fatalf("reconcile now: %d reconciled, left %d, %v", len(auto.Reconciled), len(auto.Left), err)
	}
	for _, r := range auto.Reconciled {
		if r.Basis != storage.BasisExact {
			t.Errorf("%s was reconciled on the basis %q", r.Line.Reference, r.Basis)
		}
	}
	st := statuses()
	for ref, want := range map[string]string{
		"txn_c1:fee": storage.LineCategorised, "txn_c2:fee": storage.LineCategorised, "txn_c3:fee": storage.LineCategorised, // fees, once each
		"txn_d1": storage.LineCategorised, "txn_d1:fee": storage.LineCategorised, // the dispute's money and fee, to disputes clearing
		"txn_po1": storage.LineTransferred,                                                                          // the payout, in transit to the bank
		"txn_c1":  storage.LineUnreconciled, "txn_c2": storage.LineUnreconciled, "txn_c3": storage.LineUnreconciled, // charges wait for their documents
	} {
		if st[ref] != want {
			t.Errorf("%s is %q, want %q", ref, st[ref], want)
		}
	}
	// The mirror holds what the provider reported, the payout and what it settles.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		po, err := u.PSP().Payout(ctx, c.ID, "po_1")
		if err != nil || po.Amount != "192.2000" || po.Status != "paid" {
			t.Errorf("payout: %+v, %v", po, err)
		}
		inPayout, err := u.PSP().TxnsOfPayout(ctx, c.ID, "po_1")
		if err != nil || len(inPayout) != 2 {
			t.Errorf("the payout settles %d transaction(s), %v", len(inPayout), err)
		}
		tx, err := u.PSP().Txn(ctx, c.ID, "txn_c1")
		if err != nil || tx.Gross != "100.0000" || tx.Fee != "3.9000" || tx.Net != "96.1000" || tx.Category != "charge" || tx.SourceKind != "charge" || tx.SourceID != "ch_1" {
			t.Errorf("txn: %+v, %v", tx, err)
		}
		bal, err := u.PSP().LatestBalance(ctx, c.ID, "SGD")
		if err != nil || bal.Amount != "31.1000" {
			t.Errorf("balance: %+v, %v", bal, err)
		}
		a, err := u.PSP().Account(ctx, clearing.ID)
		if err != nil || a.SweptThrough == nil || a.SweepError != "" {
			t.Errorf("swept: %+v, %v", a, err)
		}
		return nil
	})
	// The fee's posting: Dr the fee account · Cr the clearing account, once. The dispute's: Dr disputes clearing.
	if bal := e.balanceOf(t, e.books.Rent.ID); bal != "11.70" {
		t.Errorf("fees expensed %s, want 11.70 (3 x 3.90)", bal)
	}
	if bal := e.balanceOf(t, disputes.ID); bal != "65.00" {
		t.Errorf("disputes clearing holds %s, want 65.00 (50.00 and the 15.00 fee)", bal)
	}

	// A second run, and a second sync, post nothing again.
	if res, err = runner.Run(ctx, e.books.Org, c.ID); err != nil || res.Transactions != 0 || res.Payouts != 0 {
		t.Fatalf("a second run: %+v, %v", res, err)
	}
	if again, err := banking.AutoReconcile(ctx, txns, e.books.Org, nil, e.clock.Now()); err != nil || len(again.Reconciled) != 0 {
		t.Fatalf("a second reconcile now: %d reconciled, %v", len(again.Reconciled), err)
	}
	before := len(statuses())
	if got, err := sync.Sync(ctx, e.books.Org, c.ID); err != nil || got.Added != 0 || len(statuses()) != before {
		t.Fatalf("a second sync, which sends every line again: %+v, %v, %d lines of %d", got, err, len(statuses()), before)
	}
	for ref, status := range statuses() {
		if status == storage.LinePossibleDuplicate {
			t.Errorf("%s came again and was marked a possible duplicate", ref)
		}
	}
	// Auto-reconcile's own record of what it did: a provider line is among the auto-reconciliations, on the basis exact.
	feeLine := lineOf("txn_c1:fee")
	var sys []storage.BankMatch
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		sys, err = u.BankMatching().Matches(ctx, storage.BankMatchFilter{SystemOnly: true})
		return err
	})
	if !slices.ContainsFunc(sys, func(m storage.BankMatch) bool {
		return m.LineID == feeLine.ID && m.Basis == storage.BasisExact && m.UndoneAt == nil
	}) {
		t.Errorf("the fee line is not among the auto-reconciliations: %d matches", len(sys))
	}
	// It undoes as any auto-reconciliation does, and Reconcile now posts it again, once.
	if err := banking.UndoAuto(ctx, e.store, e.books.Org, feeLine.ID, "Testing the undo", e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if st := statuses()["txn_c1:fee"]; st != storage.LineUnreconciled || e.balanceOf(t, e.books.Rent.ID) != "7.80" {
		t.Fatalf("after the undo the fee line is %q and fees are %s", st, e.balanceOf(t, e.books.Rent.ID))
	}
	if again, err := banking.AutoReconcile(ctx, txns, e.books.Org, nil, e.clock.Now()); err != nil || len(again.Reconciled) != 1 || e.balanceOf(t, e.books.Rent.ID) != "11.70" {
		t.Fatalf("reconcile now after the undo: %d reconciled, %v, fees %s", len(again.Reconciled), err, e.balanceOf(t, e.books.Rent.ID))
	}
	if bal := e.balanceOf(t, e.books.Rent.ID); bal != "11.70" {
		t.Errorf("a second run changed the fees: %s", bal)
	}

	// The bank's own line completes the payout's transfer (FR-BNK-14: within the cap, the missing side of one transfer).
	var bankLine storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, lines, err := u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: bank.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV,
			ParserVersion: "csv@1", PeriodFrom: porttest.Day(10, 12), PeriodTo: porttest.Day(10, 12), Currency: "SGD", At: e.clock.Now(),
			Lines: []storage.NewBankLine{{Position: 1, BookingDate: porttest.Day(10, 12), Amount: "192.20", Description: "STRIPE PAYOUT",
				Fingerprint: strings.Repeat("ab", 32)}}})
		bankLine = lines[0]
		return err
	})
	run, err := banking.AutoReconcile(ctx, txns, e.books.Org, []uuid.UUID{bankLine.ID}, e.clock.Now())
	if err != nil || len(run.Reconciled) != 1 || run.Reconciled[0].Basis != storage.BasisTransfer {
		t.Fatalf("the bank's line: %+v, %v", run, err)
	}

	// The checks. The charges are unposted: the payout does not net to zero and the books differ from the provider's balance.
	runChecks := func() map[string]storage.CheckState {
		t.Helper()
		r := checks.Runner{Store: worker, Clock: e.clock, Failed: func(_ context.Context, _ uuid.UUID, key string, err error) {
			t.Errorf("check %s failed to run: %v", key, err)
		}}
		if _, err := r.Request(ctx, e.books.Org, "user:founder"); err != nil {
			t.Fatal(err)
		}
		out := map[string]storage.CheckState{}
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			states, err := u.Checks().States(ctx)
			for _, s := range states {
				out[s.Key] = s
			}
			return err
		})
		return out
	}
	states := runChecks()
	if s := states["payments.payout-nets"]; checks.Status(s.Status) != checks.Error || s.Count != 1 {
		t.Errorf("C.2 #42 with unposted charges: %+v", s)
	}
	if s := states["payments.clearing-balance"]; checks.Status(s.Status) != checks.Warning {
		t.Errorf("C.2 #50 with unposted charges: %+v", s)
	}

	// A person posts the three charges (the mirror of task 3.2 does it for a Model B invoice): both checks pass.
	for _, ref := range []string{"txn_c1", "txn_c2", "txn_c3"} {
		if _, err := txns.Categorise(ctx, e.books.Org, journal.Categorisation{Basis: storage.BasisPerson, At: e.clock.Now(),
			Draft: banktransaction.Draft{LineID: lineOf(ref).ID, Parts: []banktransaction.Part{{AccountID: e.books.Sales.ID, GSTCode: "OS"}}}}); err != nil {
			t.Fatal(err)
		}
	}
	states = runChecks()
	if s := states["payments.payout-nets"]; checks.Status(s.Status) != checks.OK {
		t.Errorf("C.2 #42 with every line posted: %+v", s)
	}
	if s := states["payments.clearing-balance"]; checks.Status(s.Status) != checks.OK {
		t.Errorf("C.2 #50 with every line posted: %+v", s)
	}

	// A provider that fails to list leaves the account's sweep error on it, for C.2 #43, and posts nothing.
	f.mu.Lock()
	f.failList = errors.New("the provider answered 503")
	f.mu.Unlock()
	if res, err = runner.Run(ctx, e.books.Org, c.ID); err != nil || len(res.Errors) != 1 {
		t.Fatalf("a failing provider: %+v, %v", res, err)
	}
	if s := runChecks()["bank.sync"]; checks.Status(s.Status) != checks.Warning || !strings.Contains(s.Detail, "behind") {
		t.Errorf("C.2 #43 with a failed sweep: %+v", s)
	}

	// A later transaction comes with the same lines again: only its own two lines are added, and the chain shows no gap.
	f.mu.Lock()
	f.failList = nil
	f.txns = append(f.txns, bt("txn_c4", "charge", "100.00", "3.90", "96.10", 13, ports.ProviderCharge, "ch_4"))
	f.mu.Unlock()
	if got, err := sync.Sync(ctx, e.books.Org, c.ID); err != nil || got.Added != 2 {
		t.Fatalf("a sync with one new transaction: %+v, %v", got, err)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		gaps, err := u.Banking().Gaps(ctx, clearing.ID, true)
		if err != nil || len(gaps) != 0 {
			t.Errorf("gaps: %v, %v", gaps, err)
		}
		return nil
	})
	for ref, status := range statuses() {
		if status == storage.LinePossibleDuplicate {
			t.Errorf("%s was marked a possible duplicate", ref)
		}
	}
}

// balanceOf is a chart account's book balance in the base currency on the clock's day, debit positive.
func (e *env) balanceOf(t *testing.T, account uuid.UUID) string {
	t.Helper()
	var out string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		sgd := money.MustLookup("SGD")
		book, _, err := banking.BookBalance(ctx, u, account, sgd, sgd, e.clock.Now())
		out = book.String()
		return err
	})
	return out
}

// A provider's payout event records the payout and what it settles and queues one sweep of its connector (at most one a
// minute, so a burst of events queues one); the sweep is the worker's handler of psp.run, and nothing is posted by it.
// req: banking/payment-providers Provider transactions stored
func TestAPayoutEventRecordsThePayoutAndQueuesASweep(t *testing.T) {
	e := newEnv(t)
	var c storage.InstalledConnector
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		c, err = u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: "stripe", ManifestVersion: "1.0.0", ManifestHash: strings.Repeat("a", 64),
			SecretNames: []string{"api_key"}, Position: 1, At: e.clock.Now()})
		return err
	})
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	h := psp.Handler{Dispatcher: outbox.Dispatcher{Store: worker, Queue: e.queue, Clock: e.clock}, Clock: e.clock}
	obj := ports.ProviderObject{Kind: ports.ProviderPayout, ID: "po_9", Status: "paid", Currency: "SGD", Amount: "192.20", Created: porttest.Day(10, 12),
		Links: map[ports.ProviderKind][]string{ports.ProviderBalanceTransaction: {"txn_c1", "txn_c2"}}}
	handle := func() {
		t.Helper()
		if err := worker.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			for _, txn := range []string{"txn_c1", "txn_c2"} {
				if _, _, err := u.PSP().RecordTxn(ctx, storage.NewPSPTxn{ConnectorID: c.ID, TxnID: txn, Type: "charge", Category: storage.PSPCatCharge, Currency: "SGD",
					Gross: "100.00", Fee: "3.90", Net: "96.10", ProviderAt: e.clock.Now(), SeenAt: e.clock.Now()}); err != nil {
					return err
				}
			}
			park, err := h.Handle(ctx, u, c, storage.PSPEvent{}, obj)
			if park != "" {
				t.Errorf("parked: %s", park)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	handle()
	handle() // a second event of the same minute
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		po, err := u.PSP().Payout(ctx, c.ID, "po_9")
		if err != nil || po.Amount != "192.2000" || po.Status != "paid" {
			t.Errorf("payout: %+v, %v", po, err)
		}
		in, err := u.PSP().TxnsOfPayout(ctx, c.ID, "po_9")
		if err != nil || len(in) != 2 {
			t.Errorf("settles %d transaction(s): %v", len(in), err)
		}
		return nil
	})
	k, row := nextWorkRow(t, e)
	if k != psp.Kind || !strings.Contains(string(row.Payload), c.ID.String()) {
		t.Fatalf("queued %q %s", k, row.Payload)
	}
	if k := nextWork(t, e); k != "" {
		t.Fatalf("a second event queued %q: one sweep a minute", k)
	}
	// With no provider account set up there is nothing to sweep: the handler of the row does nothing, and fails nothing.
	if err := (psp.Runner{Store: worker, Runtime: e.api.deps.Connectors.Runtime, Clock: e.clock}).Work()(context.Background(), e.books.Org, row); err != nil &&
		!errors.Is(err, connector.ErrNotEnabled) {
		t.Fatalf("the sweep of an unset-up connector: %v", err)
	}
}

// G4-041: 30 Stripe charges of S$100.00 in a month, fee S$3.90 each (S$117.00 in all), a payout settling 27 of them, and 3 charges
// not yet paid out at the month's end. The fees are expensed once (S$117.00); the payout, above the per-line limit of
// auto-reconcile, waits for a person, who records it as a transfer to the bank, whose own line then completes it; the settled
// payout nets to zero in clearing (C.2 #42), and the S$288.30 left in clearing (3 x S$96.10) agrees to the provider's balance
// not yet paid out (C.2 #50).
// req: banking/payment-providers Each settled payout nets to zero
// req: banking/payment-providers Month-end clearing equals the provider balance
// req: C.2 #42
// req: C.2 #50
func TestThirtyChargesAFeeEachAndAPayoutOfTwentySeven(t *testing.T) {
	e := newEnv(t)
	f := &fakeLedgerProvider{payouts: map[string][]string{"po_1": nil}, balance: "288.30"}
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("txn_c%02d", i)
		f.txns = append(f.txns, bt(id, "charge", "100.00", "3.90", "96.10", 1+i%9, ports.ProviderCharge, fmt.Sprintf("ch_%02d", i)))
		if i <= 27 {
			f.payouts["po_1"] = append(f.payouts["po_1"], id)
		}
	}
	f.txns = append(f.txns, bt("txn_po1", "payout", "-2594.70", "0.00", "-2594.70", 12, ports.ProviderPayout, "po_1")) // 27 x 96.10
	b := fakeLedgerConnector(t, f)
	cat, err := connector.NewCatalogue(b)
	if err != nil {
		t.Fatal(err)
	}
	rt := &connector.Runtime{Catalogue: cat, Secrets: e.api.deps.Connectors.Runtime.Secrets, Egress: &egress.Guard{Local: true}, Clock: e.clock}
	var c storage.InstalledConnector
	m := b.Manifest
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: m.ID, ManifestVersion: m.Version, ManifestHash: m.Hash(),
			SecretNames: []string{"token"}, Consent: m.Consent(), Position: 1, At: e.clock.Now()}); err != nil {
			return err
		}
		for _, step := range []struct{ s, state string }{{storage.StepConfigure, storage.ConnectorConfigured}, {storage.StepTest, storage.ConnectorTested},
			{storage.StepEnable, storage.ConnectorEnabled}} {
			if step.s == storage.StepTest {
				c.Test = &storage.ConnectorTest{Passed: true, Reason: "Read.", At: e.clock.Now()}
			}
			if c, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: step.s, State: step.state,
				Configuration: map[string]any{}, Position: 1, Test: c.Test, ManifestVersion: m.Version, ManifestHash: m.Hash(), SecretNames: []string{"token"},
				Consent: m.Consent(), At: e.clock.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	clearing := e.fedAccount("SGD", c)
	bankAcc := e.chartMoneyAccount()
	var bank storage.MoneyAccount
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if bank, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: bankAcc.ID, Kind: storage.MoneyBank, Name: bankAcc.Name,
			Identifier: "BANK-" + bankAcc.Code, Currency: "SGD", At: e.clock.Now()}); err != nil {
			return err
		}
		_, err = u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: clearing.ID, ConnectorID: c.ID, PayoutAccountID: &bank.ID, FeeAccountID: e.books.Rent.ID, At: e.clock.Now()}, 0)
		return err
	})
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	txns := journal.BankTransactions{Store: e.store, Clock: e.clock}
	ctx := context.Background()
	if got, err := (bankfeed.Syncer{Store: worker, Runtime: rt, Clock: e.clock}).Sync(ctx, e.books.Org, c.ID); err != nil || got.Added != 61 {
		t.Fatalf("the sync: %+v, %v", got, err)
	}
	if res, err := (psp.Runner{Store: worker, Runtime: rt, Clock: e.clock}).Run(ctx, e.books.Org, c.ID); err != nil || len(res.Errors) != 0 || res.Transactions != 31 {
		t.Fatalf("the sweep: %+v, %v", res, err)
	}
	auto, err := banking.AutoReconcile(ctx, txns, e.books.Org, nil, e.clock.Now())
	if err != nil || len(auto.Reconciled) != 30 { // the 30 fees; the payout is over the cap
		t.Fatalf("reconcile now: %d reconciled, %v", len(auto.Reconciled), err)
	}
	if e.balanceOf(t, e.books.Rent.ID) != "117.00" {
		t.Fatalf("fees expensed: %s, want 117.00", e.balanceOf(t, e.books.Rent.ID))
	}
	var payoutLeft bool
	for _, l := range auto.Left {
		if l.Line.Reference == "txn_po1" && l.Why == banking.LeftLineCap {
			payoutLeft = true
		}
	}
	if !payoutLeft {
		t.Fatalf("the payout was not left for a person for its size: %+v", auto.Left)
	}
	lines := map[string]storage.BankLine{}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		ls, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &clearing.ID})
		for _, l := range ls {
			lines[l.Reference] = l
		}
		return err
	})
	for ref, l := range lines {
		if strings.HasPrefix(ref, "txn_c") && !strings.HasSuffix(ref, ":fee") {
			if _, err := txns.Categorise(ctx, e.books.Org, journal.Categorisation{Basis: storage.BasisPerson, At: e.clock.Now(),
				Draft: banktransaction.Draft{LineID: l.ID, Parts: []banktransaction.Part{{AccountID: e.books.Sales.ID, GSTCode: "OS"}}}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A person records the payout as the transfer, and the bank's own line (also over the cap) completes it.
	paid := money.FromMinor(259470, money.MustLookup("SGD"))
	po := lines["txn_po1"]
	tr, err := txns.RecordTransfer(ctx, e.books.Org, journal.TransferRequest{FromLineID: &po.ID, FromAccountID: clearing.ID, ToAccountID: bank.ID, Paid: &paid, Received: &paid,
		Date: po.BookingDate, Basis: storage.BasisPerson, At: e.clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	var bankLine storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, ls, err := u.Banking().Import(ctx, storage.NewBankStatement{MoneyAccountID: bank.ID, Source: storage.MoneySourceFile, Format: storage.FormatCSV, ParserVersion: "csv@1",
			PeriodFrom: porttest.Day(10, 12), PeriodTo: porttest.Day(10, 12), Currency: "SGD", At: e.clock.Now(),
			Lines: []storage.NewBankLine{{Position: 1, BookingDate: porttest.Day(10, 12), Amount: "2594.70", Description: "STRIPE PAYOUT", Fingerprint: strings.Repeat("cd", 32)}}})
		bankLine = ls[0]
		return err
	})
	if _, err := txns.CompleteTransfer(ctx, e.books.Org, tr.Transfer.ID, bankLine.ID, storage.BasisPerson, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	r := checks.Runner{Store: worker, Clock: e.clock, Failed: func(_ context.Context, _ uuid.UUID, key string, err error) {
		t.Errorf("check %s failed to run: %v", key, err)
	}}
	if _, err := r.Request(ctx, e.books.Org, "user:founder"); err != nil {
		t.Fatal(err)
	}
	states := map[string]storage.CheckState{}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Checks().States(ctx)
		for _, s := range all {
			states[s.Key] = s
		}
		return err
	})
	if s := states["payments.payout-nets"]; checks.Status(s.Status) != checks.OK || !strings.Contains(s.Detail, "1 settled payout") {
		t.Errorf("C.2 #42: %+v", s)
	}
	if s := states["payments.clearing-balance"]; checks.Status(s.Status) != checks.OK {
		t.Errorf("C.2 #50: %+v", s)
	}
	if bal := e.balanceOf(t, clearing.AccountID); bal != "288.30" {
		t.Errorf("clearing holds %s, want 288.30 (3 x 96.10)", bal)
	}
}
