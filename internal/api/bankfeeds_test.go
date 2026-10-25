//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/bankfeed"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// fakeBank is the provider behind the example-bank connector: each account's transactions, given two to a page; an
// error to answer with instead, when set.
type fakeBank struct {
	mu   sync.Mutex
	txns map[string][]ports.BankDataTransaction
	err  error
}

var bank = &fakeBank{txns: map[string][]ports.BankDataTransaction{}}

func (f *fakeBank) Accounts(context.Context) ([]ports.BankDataAccount, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ports.BankDataAccount
	for id := range f.txns {
		out = append(out, ports.BankDataAccount{ID: id, Currency: "SGD"})
	}
	return out, f.err
}

func (f *fakeBank) Transactions(_ context.Context, account, cursor string) (ports.BankDataPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return ports.BankDataPage{}, f.err
	}
	from := 0
	if cursor != "" {
		from, _ = strconv.Atoi(cursor)
	}
	all := f.txns[account]
	to := min(from+2, len(all))
	return ports.BankDataPage{Transactions: append([]ports.BankDataTransaction(nil), all[from:to]...), Cursor: strconv.Itoa(to), More: to < len(all)}, nil
}

func (f *fakeBank) add(account, amount, balance string, day int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.txns[account] = append(f.txns[account], ports.BankDataTransaction{ID: account + "-" + strconv.Itoa(len(f.txns[account])+1),
		BookingDate: porttest.Day(10, day), Amount: amount, Currency: "SGD", Description: "Transfer", RunningBalance: balance})
}

// fakeBankConnector is an organisation's bank-data connector over fakeBank, whose webhooks carry X-Signature: good.
func fakeBankConnector(t testing.TB) connector.Binding {
	t.Helper()
	m, err := connector.Parse([]byte(`{"id":"example-bank","name":"Example Bank","publisher":"Fyniq","version":"1.0.0","min_platform":"0.1.0",
	  "summary":"A fake bank.","scope":"organisation","capabilities":[{"kind":"bank-data","trigger":"system-rule",
	  "trigger_rule":"The sync schedule.","on_pause":"hold","on_disable":"hold","in_flight":"finish"}],
	  "configuration":{"type":"object","additionalProperties":false,"properties":{}},
	  "secrets":[{"name":"token","title":"Token","description":"Its read-only token.","used_by":["worker"],"rotation_overlap_hours":1}],
	  "provider_scopes":[],"fyniq_scopes":[],"data_classes":["business-figures"],"regions":["SG"],
	  "destinations":[{"host":"bank.example.com"}],"webhooks":[],"health_check":"Counts.","test_action":"Lists the accounts.","local_substitute":"A fake.",
	  "limits":{"requests_per_minute":600,"breaker_failures":5,"breaker_cooldown_seconds":60}}`))
	if err != nil {
		t.Fatal(err)
	}
	return connector.Binding{Manifest: m, BankData: func(connector.Config) (ports.BankData, error) { return bank, nil },
		VerifyWebhook: func(_ connector.Config, h http.Header, _ []byte, _ time.Time) error {
			if h.Get("X-Signature") != "good" {
				return errors.New("bad signature")
			}
			return nil
		}}
}

// enabledBankConnector installs example-bank in e's organisation and takes it to enabled.
func (e *env) enabledBankConnector() storage.InstalledConnector {
	e.t.Helper()
	b, _ := e.api.deps.Connectors.Catalogue.Get("example-bank")
	m := b.Manifest
	var c storage.InstalledConnector
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: m.ID, ManifestVersion: m.Version, ManifestHash: m.Hash(),
			SecretNames: []string{"token"}, Consent: m.Consent(), Position: 1, At: e.clock.Now()}); err != nil {
			return err
		}
		for _, step := range []struct{ s, state string }{{storage.StepConfigure, storage.ConnectorConfigured}, {storage.StepTest, storage.ConnectorTested},
			{storage.StepEnable, storage.ConnectorEnabled}} {
			if step.s == storage.StepTest {
				c.Test = &storage.ConnectorTest{Passed: true, Reason: "The accounts were listed.", At: e.clock.Now()}
			}
			if c, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: step.s, State: step.state,
				Configuration: map[string]any{}, Position: 1, Test: c.Test, ManifestVersion: m.Version, ManifestHash: m.Hash(), SecretNames: []string{"token"},
				Consent: m.Consent(), At: e.clock.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	return c
}

// fedAccount is an SGD money account that is provider account id, fed by connector c from today.
func (e *env) fedAccount(id string, c storage.InstalledConnector) storage.MoneyAccount {
	e.t.Helper()
	acc := e.chartMoneyAccount()
	var m storage.MoneyAccount
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if m, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: acc.ID, Kind: storage.MoneyWallet, Name: acc.Name, Identifier: id,
			Currency: "SGD", At: e.clock.Now()}); err != nil {
			return err
		}
		_, err = u.Banking().AddSource(ctx, storage.NewMoneyAccountSource{MoneyAccountID: m.ID, Kind: storage.MoneySourceConnector, ConnectorID: &c.ID,
			FromDate: e.clock.Now(), At: e.clock.Now()})
		return err
	})
	return m
}

// feedOf is m's feed as the API reads it: unlocked (Feed is the sync's, and locks it).
func (e *env) feedOf(m storage.MoneyAccount) storage.BankFeed {
	e.t.Helper()
	var f storage.BankFeed
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Banking().Feeds(ctx)
		for _, x := range all {
			if x.MoneyAccountID == m.ID {
				f = x
			}
		}
		return err
	})
	return f
}

func (e *env) linesOf(m storage.MoneyAccount) []storage.BankLine {
	e.t.Helper()
	var out []storage.BankLine
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		out, err = u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &m.ID})
		return err
	})
	return out
}

// The bank-data port and sync (FR-BNK-15, FR-BNK-16; p1-bank-import-reconcile task 8.2, design D11): the worker pulls
// a connector's accounts page by page from each feed's cursor, as the connector, importing statements with the
// continuity and duplicate checks and moving the cursor with them; the lines wait for Reconcile now; a second sync
// adds only what is new; a provider's refusal is kept on the feed. A provider's webhook, verified by its connector,
// only queues an early sync, whatever its body says; an unknown connector, another connector's name, a bad signature
// and a disabled connector are refused. A bank-data connector's steps need C on "Bank data… connections".
// req: FR-BNK-15
// req: FR-BNK-16
// req: FR-INT-27
// req: banking/bank-transactions Lines that arrive by feed wait for Reconcile now
func TestBankFeedsSyncThroughTheirConnector(t *testing.T) {
	e := newEnv(t)
	c := e.enabledBankConnector()
	m := e.fedAccount("balance-"+uuid.NewString()[:8], c)
	bank.add(m.Identifier, "100.00", "100.00", 1)
	bank.add(m.Identifier, "-30.00", "70.00", 2)
	bank.add(m.Identifier, "5.00", "75.00", 3)
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	s := bankfeed.Syncer{Store: worker, Runtime: e.api.deps.Connectors.Runtime, Clock: e.clock}

	if r := e.api.deps.Connectors.Runtime.TestBankData(context.Background(), c, nil); !r.Passed || !strings.Contains(r.Reason, "account(s) in SGD") {
		t.Fatalf("the connector's test: %+v", r)
	}
	got, err := s.Sync(context.Background(), e.books.Org, c.ID)
	if err != nil || got.Accounts != 1 || got.Added != 3 || len(got.Errors) != 0 {
		t.Fatalf("the first sync: %+v, %v", got, err)
	}
	lines := e.linesOf(m)
	if len(lines) != 3 || lines[0].Reference != m.Identifier+"-1" || lines[2].Status != storage.LineUnreconciled {
		t.Fatalf("the lines: %+v", lines)
	}
	if f := e.feedOf(m); f.Cursor != "3" || f.ConnectorID != c.ID || f.LastError != "" || f.Version != 2 {
		t.Fatalf("the feed after two pages: %+v", f)
	}
	st := e.call(http.MethodGet, "/v1/money-accounts/"+m.ID.String()+"/statements", e.as(authz.Bookkeeper), nil)
	if !strings.Contains(st.Raw, `"source":"connector"`) || !strings.Contains(st.Raw, `"format":"api"`) {
		t.Fatalf("the statements: %s", st.Raw)
	}
	if got, err := s.Sync(context.Background(), e.books.Org, c.ID); err != nil || got.Added != 0 {
		t.Fatalf("a sync with nothing new: %+v, %v", got, err)
	}
	bank.add(m.Identifier, "-5.00", "70.00", 4)
	if got, err := s.Sync(context.Background(), e.books.Org, c.ID); err != nil || got.Added != 1 || len(e.linesOf(m)) != 4 {
		t.Fatalf("a sync with one new: %+v, %v", got, err)
	}
	bank.mu.Lock()
	bank.err = errors.New("the provider answered 503")
	bank.mu.Unlock()
	got, err = s.Sync(context.Background(), e.books.Org, c.ID)
	bank.mu.Lock()
	bank.err = nil
	bank.mu.Unlock()
	if err != nil || len(got.Errors) != 1 || !strings.Contains(e.feedOf(m).LastError, "503") || e.feedOf(m).Cursor != "4" {
		t.Fatalf("a refusal: %+v, %v, feed %+v", got, err, e.feedOf(m))
	}

	// The webhook only prompts a sync.
	mux := http.NewServeMux()
	(&bankfeed.Hooks{Store: e.store, Routes: e.store, Runtime: e.api.deps.Connectors.Runtime,
		Dispatcher: outbox.Dispatcher{Store: e.store, Queue: e.queue, Clock: e.clock}, Clock: e.clock}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	post := func(name string, id uuid.UUID, signature string) int {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/webhooks/bank/"+name+"/"+id.String(),
			strings.NewReader(`{"event":"balances#update","transactions":[{"amount":"999999.00"}]}`))
		req.Header.Set("X-Signature", signature)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	drainWork(t, e)
	before := len(e.linesOf(m))
	if code := post("example-bank", c.ID, "good"); code != http.StatusAccepted {
		t.Fatalf("a verified webhook: %d", code)
	}
	if k, row := nextWorkRow(t, e); k != bankfeed.Kind || !strings.Contains(string(row.Payload), c.ID.String()) {
		t.Fatalf("queued %q", k)
	} else if err := s.Work()(context.Background(), e.books.Org, row); err != nil {
		t.Fatal(err)
	}
	if after := len(e.linesOf(m)); after != before {
		t.Errorf("the webhook's body became lines: %d then %d", before, after)
	}
	for name, c := range map[string]struct {
		connector string
		id        uuid.UUID
		signature string
		want      int
	}{
		"an unknown connector":   {"example-bank", uuid.New(), "good", http.StatusNotFound},
		"another connector":      {"wise", c.ID, "good", http.StatusNotFound},
		"a signature that fails": {"example-bank", c.ID, "forged", http.StatusUnauthorized},
	} {
		if code := post(c.connector, c.id, c.signature); code != c.want {
			t.Errorf("%s: %d; want %d", name, code, c.want)
		}
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		b, _ := e.api.deps.Connectors.Catalogue.Get("example-bank")
		_, err := u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: storage.StepDisable, State: storage.ConnectorDisabled,
			Configuration: map[string]any{}, Position: 1, Test: c.Test, ManifestVersion: b.Manifest.Version, ManifestHash: b.Manifest.Hash(),
			SecretNames: []string{"token"}, Consent: b.Manifest.Consent(), At: e.clock.Now()})
		return err
	})
	if code := post("example-bank", c.ID, "good"); code != http.StatusNotFound {
		t.Errorf("a disabled connector's webhook: %d", code)
	}
	if _, err := s.Sync(context.Background(), e.books.Org, c.ID); !errors.Is(err, connector.ErrNotEnabled) {
		t.Errorf("a disabled connector synced: %v", err)
	}
}

// A bank-data connector's steps need C on "Bank data… connections" as well as on "Integrations" (FR-BNK-15).
// req: FR-BNK-15
func TestABankDataConnectorNeedsTheBankDataRow(t *testing.T) {
	b := fakeBankConnector(t)
	for role, want := range map[authz.Role]bool{authz.Administrator: true, authz.Bookkeeper: false, authz.Controller: false} {
		ctx := authn.WithPrincipal(context.Background(), authn.Principal{Class: authn.ClassUser, Roles: []authz.Role{role}})
		if err := bankDataRow(ctx, b); (err == nil) != want {
			t.Errorf("%s: %v", role, err)
		}
	}
	if err := bankDataRow(context.Background(), fakeConnector(t, "example-x", connector.ScopeOrganisation, "")); err != nil {
		t.Errorf("an email connector: %v", err)
	}
}
