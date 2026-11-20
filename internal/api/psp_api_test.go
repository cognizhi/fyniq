//go:build local

package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// providerAccount installs a Stripe connector in e's organisation and makes a payment gateway money account fed by it, once.
func (e *env) providerAccount() (storage.MoneyAccount, storage.InstalledConnector) {
	e.t.Helper()
	acc := e.chartMoneyAccount()
	var c storage.InstalledConnector
	var m storage.MoneyAccount
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		b, _ := e.api.deps.Connectors.Catalogue.Get("stripe")
		man := b.Manifest
		if c, err = u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: man.ID, ManifestVersion: man.Version, ManifestHash: man.Hash(),
			SecretNames: []string{"api_key", "webhook_secret"}, Consent: man.Consent(), Position: 1, At: e.clock.Now()}); err != nil {
			return err
		}
		if m, err = u.Banking().CreateMoneyAccount(ctx, storage.NewMoneyAccount{AccountID: acc.ID, Kind: storage.MoneyPaymentGateway, Name: "Stripe " + acc.Code,
			Identifier: "SGD-" + acc.Code, Currency: "SGD", At: e.clock.Now()}); err != nil {
			return err
		}
		_, err = u.Banking().AddSource(ctx, storage.NewMoneyAccountSource{MoneyAccountID: m.ID, Kind: storage.MoneySourceConnector, ConnectorID: &c.ID,
			FromDate: porttest.Day(10, 1), At: e.clock.Now()})
		return err
	})
	return m, c
}

// pspVersion is a provider account's version, 0 when it has no setup.
func (e *env) pspVersion(m uuid.UUID) int {
	e.t.Helper()
	v := 0
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.PSP().Account(ctx, m)
		if err == nil {
			v = a.Version
		}
		return nil
	})
	return v
}

var matrixPaymentProviders = map[string]func(e *env, tok string) (response, func() bool){
	// The payment link of an invoice (FR-AR-13): no payment-link connector is enabled in the matrix's organisation, so a role the policy allows is
	// answered PSP-12 (create), PSP-13 (cancel) or not found (read), past its policy; any other role is answered 403 and nothing is asked of a provider.
	"createInvoicePaymentLink": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoices/"+uuid.NewString()+"/payment-link", tok, map[string]any{}), nil
	},
	"getInvoicePaymentLink": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/invoices/"+uuid.NewString()+"/payment-link", tok, nil), nil
	},
	"cancelInvoicePaymentLink": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/invoices/"+uuid.NewString()+"/payment-link/cancellation", tok, map[string]any{"reason": "Not needed"}), nil
	},
	// Model B (FR-PSP-03): reading the mirror needs a right on Bank transactions; issuing the drafts made of it is the right to post on Invoices, and with none to
	// issue it does nothing and the matrix's books are unchanged.
	"getPaymentProviderInvoices": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payment-providers/invoices", tok, nil), nil
	},
	"postPaymentProviderInvoices": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/payment-providers/invoices/post", tok, nil), nil
	},
	"listPaymentProviderAccounts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payment-providers/accounts", tok, nil), nil
	},
	"listPaymentProviderPayouts": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payment-providers/payouts", tok, nil), nil
	},
	"listPaymentProviderEvents": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payment-providers/events", tok, nil), nil
	},
	"listPaymentProviderDisputes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/payment-providers/disputes", tok, nil), nil
	},
	// The matrix's provider account names no dispute loss account, so an allowed role is answered PSP-05, past its policy.
	"prepareDisputeLoss": func(e *env, tok string) (response, func() bool) {
		v, ok := pspMatrixAccounts.Load(e)
		if !ok {
			m, _ := e.providerAccount()
			v = m
			pspMatrixAccounts.Store(e, m)
			e.do(func(ctx context.Context, u storage.UnitOfWork) error {
				c, err := u.Connectors().Organisation(ctx)
				if err != nil {
					return err
				}
				_, err = u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: m.ID, ConnectorID: c[len(c)-1].ID, FeeAccountID: e.books.Rent.ID, At: e.clock.Now()}, 0)
				return err
			})
		}
		m := v.(storage.MoneyAccount)
		return e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_matrix/loss", tok, map[string]any{"money_account_id": m.ID.String()}), nil
	},
	// The matrix's provider account names no disputes account, so an allowed role is answered PSP-05, past its policy.
	"settleDisputeCredit": func(e *env, tok string) (response, func() bool) {
		v, ok := pspMatrixAccounts.Load(e)
		if !ok {
			m, _ := e.providerAccount()
			v = m
			pspMatrixAccounts.Store(e, m)
			e.do(func(ctx context.Context, u storage.UnitOfWork) error {
				c, err := u.Connectors().Organisation(ctx)
				if err != nil {
					return err
				}
				_, err = u.PSP().SaveAccount(ctx, storage.NewPSPAccount{MoneyAccountID: m.ID, ConnectorID: c[len(c)-1].ID, FeeAccountID: e.books.Rent.ID, At: e.clock.Now()}, 0)
				return err
			})
		}
		m := v.(storage.MoneyAccount)
		return e.call(http.MethodPost, "/v1/payment-providers/disputes/dp_matrix/credit-settlement", tok, map[string]any{"money_account_id": m.ID.String(), "credit_note_id": uuid.NewString()}), nil
	},
	// A parked event: the first allowed role resolves it, and the others are answered PSP-07, past their policy.
	"resolvePaymentProviderEvent": func(e *env, tok string) (response, func() bool) {
		if _, ok := pspMatrixAccounts.Load(e); !ok {
			m, _ := e.providerAccount()
			pspMatrixAccounts.Store(e, m)
		}
		var ev storage.PSPEvent
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			var isNew bool
			all, err := u.Connectors().Organisation(ctx)
			if err != nil {
				return err
			}
			c := all[len(all)-1]
			if ev, isNew, err = u.PSPEvents().Receive(ctx, storage.NewPSPEvent{ID: uuid.New(), ConnectorID: c.ID, Provider: "stripe", EventID: "evt_" + uuid.NewString(), EventType: "radar.thing",
				ParserVersion: "stripe@1", Payload: []byte("sealed"), PayloadSHA256: strings.Repeat("c", 64), ReceivedAt: e.clock.Now()}); err != nil || !isNew {
				return err
			}
			_, err = u.PSPEvents().Settle(ctx, ev.ID, storage.PSPParked, "unknown type", e.clock.Now())
			return err
		})
		r := e.call(http.MethodPost, "/v1/payment-providers/events/"+ev.ID.String()+"/resolution", tok, map[string]any{"reason": "Dealt with"})
		return r, func() bool {
			var st string
			e.do(func(ctx context.Context, u storage.UnitOfWork) error {
				x, err := u.PSPEvents().Get(ctx, ev.ID)
				st = x.Status
				return err
			})
			return st == storage.PSPParked
		}
	},
	"setPaymentProviderAccount": func(e *env, tok string) (response, func() bool) {
		v, ok := pspMatrixAccounts.Load(e)
		if !ok {
			m, _ := e.providerAccount()
			v = m
			pspMatrixAccounts.Store(e, m)
		}
		m := v.(storage.MoneyAccount)
		before := e.pspVersion(m.ID)
		hdr := []string{}
		if before > 0 {
			hdr = []string{"If-Match", `"` + strconv.Itoa(before) + `"`}
		}
		r := e.call(http.MethodPut, "/v1/payment-providers/accounts/"+m.ID.String(), tok, map[string]any{"fee_account_id": e.books.Rent.ID.String()}, hdr...)
		return r, func() bool { return e.pspVersion(m.ID) == before }
	},
}

// pspMatrixAccounts is the provider account the matrix sets up, once per environment.
var pspMatrixAccounts sync.Map

// FR-PSP-04..06, FR-INT-27: a payment provider's money account is set up through the API by someone with a right on "Bank data…
// connections": the bank account its payouts land in, the expense account its fees post to and the account a disputed amount
// waits in, each checked; an account that is not a provider's is refused; a first setup needs no If-Match and a change needs the
// ETag; it lists with the connector, names and what the books hold; a bookkeeper may read but not set up. Payouts list with the
// transactions they settle and whether they have posted, and parked events list apart from the rest, without their payloads.
// req: banking/payment-providers Payout breakdown
// req: banking/payment-providers Provider transactions stored
func TestAPaymentProvidersAccountIsSetUpThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	admin, bk := e.as(authz.Administrator), e.as(authz.Bookkeeper)
	m, c := e.providerAccount()
	bank := e.newMoneyAccount()
	path := "/v1/payment-providers/accounts/" + m.ID.String()
	body := func(fee, payout, disputes string) map[string]any {
		b := map[string]any{"fee_account_id": fee}
		if payout != "" {
			b["payout_account_id"] = payout
		}
		if disputes != "" {
			b["disputes_account_id"] = disputes
		}
		return b
	}
	var disputes storage.Account
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		disputes, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1055", Name: "Payment disputes clearing", Type: storage.Asset})
		return err
	})

	if r := e.call(http.MethodPut, path, bk, body(e.books.Rent.ID.String(), "", "")); r.Status != http.StatusForbidden {
		t.Fatalf("a bookkeeper set it up: %d %s", r.Status, r.Raw)
	}
	for name, c := range map[string]struct {
		path string
		body map[string]any
		code string
	}{
		"a bank account is not a provider's":    {"/v1/payment-providers/accounts/" + bank.ID.String(), body(e.books.Rent.ID.String(), "", ""), CodePSPNotProvider},
		"a fee to revenue":                      {path, body(e.books.Sales.ID.String(), "", ""), CodePSPFee},
		"a fee to a group":                      {path, body(e.books.Expenses.ID.String(), "", ""), CodePSPFee},
		"payouts to the account itself":         {path, body(e.books.Rent.ID.String(), m.ID.String(), ""), CodePSPPayout},
		"payouts to an account that is unknown": {path, body(e.books.Rent.ID.String(), uuid.NewString(), ""), CodePSPPayout},
		"disputes in a money account":           {path, body(e.books.Rent.ID.String(), "", e.chartMoneyAccount().ID.String()), CodePSPDisputes},
	} {
		if r := e.call(http.MethodPut, c.path, admin, c.body); r.Status != http.StatusUnprocessableEntity || r.problemCode() != c.code {
			t.Errorf("%s: %d %s %s", name, r.Status, r.problemCode(), r.Raw)
		}
	}
	if e.pspVersion(m.ID) != 0 {
		t.Fatal("a refused setup was saved")
	}

	r := e.call(http.MethodPut, path, admin, body(e.books.Rent.ID.String(), bank.ID.String(), disputes.ID.String()))
	if r.Status != http.StatusOK || r.Header.Get("ETag") != `"1"` || r.str("provider") != "stripe" || r.str("payout_account_id") != bank.ID.String() ||
		r.str("disputes_account_name") != "1055 Payment disputes clearing" || r.str("connector_id") != c.ID.String() {
		t.Fatalf("the setup: %d %s %v", r.Status, r.Raw, r.Header)
	}
	if r := e.call(http.MethodPut, path, admin, body(e.books.Rent.ID.String(), "", "")); r.Status != http.StatusPreconditionRequired {
		t.Errorf("a change with no If-Match: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPut, path, admin, body(e.books.Rent.ID.String(), "", ""), "If-Match", `"7"`); r.Status != http.StatusPreconditionFailed {
		t.Errorf("a change at a stale version: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPut, path, admin, body(e.books.Rent.ID.String(), "", ""), "If-Match", `"1"`, "Prefer", "return=preview"); r.Status != http.StatusOK || e.pspVersion(m.ID) != 1 {
		t.Errorf("a preview saved: %d %d", r.Status, e.pspVersion(m.ID))
	}
	if r := e.call(http.MethodPut, path, admin, body(e.books.Rent.ID.String(), "", ""), "If-Match", `"1"`); r.Status != http.StatusOK || r.Header.Get("ETag") != `"2"` ||
		r.str("payout_account_id") != "" {
		t.Errorf("the change: %d %s", r.Status, r.Raw)
	}

	list := e.call(http.MethodGet, "/v1/payment-providers/accounts", bk, nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != http.StatusOK || len(items) != 1 {
		t.Fatalf("the list: %d %s", list.Status, list.Raw)
	}
	if it := items[0].(map[string]any); it["money_account_name"] != m.Name || it["fee_account_name"] == "" || it["book_balance"] == nil {
		t.Errorf("an item: %v", it)
	}

	// A payout and what it settles, and a parked event.
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	at := e.clock.Now()
	if err := worker.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, _, err := u.PSP().RecordTxn(ctx, storage.NewPSPTxn{ConnectorID: c.ID, TxnID: "txn_1", Type: "charge", Category: storage.PSPCatCharge, Currency: "SGD",
			Gross: "100.00", Fee: "3.90", Net: "96.10", ProviderAt: at, SeenAt: at}); err != nil {
			return err
		}
		if _, err := u.PSP().SettleTxn(ctx, c.ID, "txn_1", "po_1"); err != nil {
			return err
		}
		if _, _, err := u.PSP().RecordPayout(ctx, storage.NewPSPPayout{ConnectorID: c.ID, PayoutID: "po_1", Currency: "SGD", Amount: "96.10", Status: "paid",
			Automatic: true, ProviderAt: at, SeenAt: at}); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		ev, isNew, err := u.PSPEvents().Receive(ctx, storage.NewPSPEvent{ID: uuid.New(), ConnectorID: c.ID, Provider: "stripe", EventID: "evt_x", EventType: "radar.thing",
			ParserVersion: "stripe@1", Payload: []byte("sealed"), PayloadSHA256: strings.Repeat("a", 64), ReceivedAt: at})
		if err != nil || !isNew {
			return err
		}
		_, err = u.PSPEvents().Settle(ctx, ev.ID, storage.PSPParked, "Fyniq does not know this event type (radar.thing): a person reviews it, and it posts nothing", at)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pr := e.call(http.MethodGet, "/v1/payment-providers/payouts?money_account_id="+m.ID.String(), bk, nil)
	pitems, _ := pr.Body["items"].([]any)
	if pr.Status != http.StatusOK || len(pitems) != 1 {
		t.Fatalf("payouts: %d %s", pr.Status, pr.Raw)
	}
	po := pitems[0].(map[string]any)
	txns, _ := po["transactions"].([]any)
	if po["payout_id"] != "po_1" || len(txns) != 1 || txns[0].(map[string]any)["posted"] != false || po["payout_line_posted"] != false || po["nets_to_zero"] != nil {
		t.Errorf("the payout: %v", po)
	}
	if amt := po["amount"].(map[string]any); amt["amount"] != "96.10" || amt["currency"] != "SGD" {
		t.Errorf("its amount: %v", amt)
	}
	er := e.call(http.MethodGet, "/v1/payment-providers/events?status=parked", bk, nil)
	eitems, _ := er.Body["items"].([]any)
	if er.Status != http.StatusOK || len(eitems) != 1 || eitems[0].(map[string]any)["event_type"] != "radar.thing" || strings.Contains(er.Raw, "sealed") {
		t.Errorf("events: %d %s", er.Status, er.Raw)
	}
	if r := e.call(http.MethodGet, "/v1/payment-providers/events?status=processed", bk, nil); len(r.Body["items"].([]any)) != 0 {
		t.Errorf("processed events: %s", r.Raw)
	}
}
