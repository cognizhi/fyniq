package stripe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/cognizhi/fyniq/internal/ports"
)

func sign(secret string, t time.Time, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	ts := strconv.FormatInt(t.Unix(), 10)
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func header(v string) http.Header { h := http.Header{}; h.Set("Stripe-Signature", v); return h }

// A webhook is accepted only with a v1 signature of its raw body under the secret and a timestamp within five minutes of
// the platform's clock, either way; a changed body, another secret, no v1 or an old or future timestamp is refused. While
// a secret rolls Stripe sends one v1 per secret, and any one verifies; v0 and the debug entries are ignored.
// req: banking/payment-providers Signed webhooks
// asvs: V4.1.5
func TestAWebhookIsAcceptedOnlyWithASignatureOfItsRawBodyAndAFreshTimestamp(t *testing.T) {
	now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	body := []byte(`{"id":"evt_1","type":"invoice.paid"}`)
	good := sign("whsec_a", now, body)
	other := sign("whsec_b", now, body)
	cases := []struct {
		name   string
		secret string
		header string
		body   []byte
		now    time.Time
		ok     bool
	}{
		{"valid", "whsec_a", good, body, now, true},
		{"a changed body", "whsec_a", good, []byte(`{"id":"evt_1","type":"invoice.paid "}`), now, false},
		{"another secret", "whsec_x", good, body, now, false},
		{"no secret set", "", good, body, now, false},
		{"no header", "whsec_a", "", body, now, false},
		{"stale by five minutes and a second", "whsec_a", good, body, now.Add(5*time.Minute + time.Second), false},
		{"future by five minutes and a second", "whsec_a", good, body, now.Add(-5*time.Minute - time.Second), false},
		{"just inside the tolerance", "whsec_a", good, body, now.Add(5 * time.Minute), true},
		{"a rolling secret: one of two v1 entries verifies", "whsec_a", other + ",v1=" + strings.Split(good, "v1=")[1], body, now, true},
		{"v0 is ignored", "whsec_a", "t=" + strconv.FormatInt(now.Unix(), 10) + ",v0=" + strings.Split(good, "v1=")[1], body, now, false},
		{"debug entries do not authenticate", "whsec_a", other + ",payload=abcd,secret=whsec_****,destination=we_1", body, now, false},
		{"v1 that is not hex", "whsec_a", "t=" + strconv.FormatInt(now.Unix(), 10) + ",v1=zz", body, now, false},
	}
	for _, c := range cases {
		err := Verify(c.secret, header(c.header), c.body, c.now)
		if c.ok != (err == nil) || (!c.ok && !errors.Is(err, ErrSignature)) {
			t.Errorf("%s: err = %v, want ok = %v", c.name, err, c.ok)
		}
	}
}

// Minor units become the decimal string of the currency's own scale, never through a float: S$12.50, ¥1,250, BD 1.250.
// req: banking/payment-providers Amounts are never floats
// req: ENG-10
func TestMinorUnitsAreConvertedWithTheCurrencysScale(t *testing.T) {
	for _, c := range []struct {
		units    int64
		cur      string
		want     string
		wantFail bool
	}{
		{1250, "sgd", "12.50", false}, {1250, "JPY", "1250", false}, {1250, "bhd", "1.250", false}, {-9610, "sgd", "-96.10", false},
		{1, "sgd", "0.01", false}, {9007199254740993, "sgd", "90071992547409.93", false}, {1, "xxx", "", true},
	} {
		got, err := Decimal(c.units, c.cur)
		if c.wantFail != (err != nil) || got != c.want {
			t.Errorf("Decimal(%d, %s) = %q, %v; want %q (fail %v)", c.units, c.cur, got, err, c.want, c.wantFail)
		}
	}
}

func fake(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	bts, err := os.ReadFile("testdata/balance_transactions.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("Authorization") != "Bearer rk_test_read" || r.Header.Get("Stripe-Version") != APIVersion {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/balance":
			_, _ = w.Write([]byte(`{"available":[{"amount":28830,"currency":"sgd"},{"amount":1000,"currency":"usd"}],"pending":[{"amount":100,"currency":"sgd"}]}`))
		case "/v1/balance_transactions":
			if r.URL.Query().Get("payout") == "po_1" {
				_, _ = w.Write([]byte(`{"has_more":false,"data":[{"id":"txn_charge2"},{"id":"txn_charge1"}]}`))
				return
			}
			q := r.URL.Query()
			if q.Get("currency") != "sgd" || q.Get("created[gte]") == "" || q.Get("created[lt]") == "" {
				http.Error(w, "query", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(bts)
		case "/v1/payouts/po_1":
			_, _ = w.Write([]byte(`{"id":"po_1","object":"payout","amount":9610,"currency":"sgd","status":"paid","created":1795990000,"automatic":true}`))
		case "/v1/charges/ch_2":
			_, _ = w.Write([]byte(`{"id":"ch_2","object":"charge","amount":10000,"currency":"sgd","status":"succeeded","created":1795800100,"customer":"cus_1","payment_intent":"pi_2"}`))
		case "/v1/invoice_payments":
			if r.URL.Query().Get("payment[payment_intent]") != "pi_2" {
				http.Error(w, "query", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"inpay_1","invoice":"in_2"}]}`))
		case "/v1/invoices/in_1":
			_, _ = w.Write([]byte(`{"id":"in_1","object":"invoice","total":4900,"currency":"sgd","status":"paid","created":1795800000,"customer":"cus_1","subscription":"sub_1"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func client(srv *httptest.Server, now time.Time) *Client {
	return &Client{Base: srv.URL, Key: "rk_test_read", HTTP: srv.Client(), Now: func() time.Time { return now }, History: 30 * 24 * time.Hour}
}

// The balance transactions come as statement lines, oldest first: a charge's gross, its fee as a line of its own with the
// fee's sign, and a payout as one negative line, whose lines sum to the net (S$96.10) and, with the payout, to zero.
func TestBalanceTransactionsComeAsStatementLinesWithTheirFees(t *testing.T) {
	var seen []string
	srv := fake(t, &seen)
	now := time.Unix(1796100000, 0)
	c := client(srv, now)
	page, err := c.Transactions(context.Background(), "SGD", "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range page.Transactions {
		got = append(got, l.ID+"="+l.Amount)
	}
	want := "txn_charge1=49.00 txn_charge2=100.00 txn_charge2:fee=-3.90 txn_payout1=-96.10"
	if strings.Join(got, " ") != want {
		t.Fatalf("lines = %v, want %s", got, want)
	}
	if page.Cursor != strconv.FormatInt(now.Unix(), 10) || page.More {
		t.Errorf("cursor %q more %v: the window ends at now", page.Cursor, page.More)
	}
	for _, l := range page.Transactions {
		if l.Currency != "SGD" || !strings.Contains(l.Description, "Stripe") && l.Counterparty != "Stripe" {
			t.Errorf("line %+v", l)
		}
	}
	if page.Transactions[1].Description != "charge ch_2 Subscription creation" {
		t.Errorf("the charge line names its source: %q", page.Transactions[1].Description)
	}
	// A window longer than a month is read in months, and the cursor continues where the window ended.
	long := client(srv, now.Add(90*24*time.Hour))
	p2, err := long.Transactions(context.Background(), "SGD", strconv.FormatInt(now.Unix(), 10), time.Time{})
	if err != nil || !p2.More {
		t.Fatalf("a long span reads in windows: %+v, %v", p2, err)
	}
	if p2.Cursor != strconv.FormatInt(now.Add(MaxStatementWindow).Unix(), 10) {
		t.Errorf("window end %q", p2.Cursor)
	}
}

// A payout is read with the balance transactions it settles, a charge with its invoice (from Stripe's invoice payments,
// where a charge no longer names it), and the balance is available and pending together, per currency.
func TestObjectsAreFetchedByIDWithTheirLinks(t *testing.T) {
	var seen []string
	srv := fake(t, &seen)
	c := client(srv, time.Unix(1796100000, 0))
	ctx := context.Background()
	po, err := c.Object(ctx, ports.ProviderPayout, "po_1")
	if err != nil || po.Amount != "96.10" || po.Currency != "SGD" || len(po.Links[ports.ProviderBalanceTransaction]) != 2 || po.Links[ports.ProviderBalanceTransaction][0] != "txn_charge1" {
		t.Fatalf("payout %+v, %v", po, err)
	}
	ch, err := c.Object(ctx, ports.ProviderCharge, "ch_2")
	if err != nil || ch.Amount != "100.00" || ch.Links[ports.ProviderInvoice][0] != "in_2" || ch.Links[ports.ProviderCustomer][0] != "cus_1" {
		t.Fatalf("charge %+v, %v", ch, err)
	}
	in, err := c.Object(ctx, ports.ProviderInvoice, "in_1")
	if err != nil || in.Amount != "49.00" || in.Links[ports.ProviderSubscription][0] != "sub_1" || len(in.Raw) == 0 {
		t.Fatalf("invoice %+v, %v", in, err)
	}
	if _, err := c.Object(ctx, ports.ProviderCharge, "ch_missing"); !errors.Is(err, ports.ErrProviderNotFound) {
		t.Errorf("a missing object: %v", err)
	}
	if _, err := c.Object(ctx, ports.ProviderCharge, "ch_1/../x"); err == nil {
		t.Error("an ID that is a path is refused")
	}
	bal, err := c.Balance(ctx)
	if err != nil || len(bal) != 2 || bal[0] != (ports.ProviderBalance{Currency: "SGD", Amount: "289.30"}) || bal[1] != (ports.ProviderBalance{Currency: "USD", Amount: "10.00"}) {
		t.Fatalf("balance %+v, %v", bal, err)
	}
	accts, err := c.Accounts(ctx)
	if err != nil || len(accts) != 2 || accts[0].ID != "SGD" {
		t.Fatalf("accounts %+v, %v", accts, err)
	}
	for _, s := range seen {
		if !strings.HasPrefix(s, "GET ") {
			t.Errorf("a request other than GET: %s", s)
		}
	}
}

// The connector is read-only: the client refuses any method but GET before it leaves, and the port has no method that
// writes. req: FR-INT-27
func TestTheAdapterIsReadOnly(t *testing.T) {
	var seen []string
	srv := fake(t, &seen)
	c := client(srv, time.Unix(1796100000, 0))
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req, _ := http.NewRequest(m, srv.URL+"/v1/charges/ch_2/refund", nil)
		if _, err := c.client().Do(req); !errors.Is(err, ErrMethod) {
			t.Errorf("%s was not refused: %v", m, err)
		}
	}
	if len(seen) != 0 {
		t.Errorf("a refused request reached the provider: %v", seen)
	}
}

// Events: a handled type names its object, an informational one is known and posts nothing, and any other type is unknown,
// for a person to review (UC-PSP-01); a body with no ID is refused.
// req: banking/payment-providers Unknown event type parked
func TestEventsAreKnownHandledOrUnknown(t *testing.T) {
	body, _ := os.ReadFile("testdata/event_payout_paid.json")
	e, err := ParseEvent(nil, body)
	if err != nil || e.ID != "evt_1" || !e.Known || !e.Handled || e.Kind != ports.ProviderPayout || e.ObjectID != "po_1" || e.APIVersion != "2025-09-30.clover" {
		t.Fatalf("%+v, %v", e, err)
	}
	// Model B's invoice, customer and subscription events name the object the mirror fetches (FR-PSP-03); so do a credit note's (FR-PSP-07).
	for typ, kind := range map[string]ports.ProviderKind{"invoice.paid": ports.ProviderInvoice, "customer.updated": ports.ProviderCustomer, "customer.subscription.updated": ports.ProviderSubscription,
		"credit_note.created": ports.ProviderCreditNote, "credit_note.voided": ports.ProviderCreditNote} {
		e, _ := ParseEvent(nil, []byte(`{"id":"evt_9","type":"`+typ+`","data":{"object":{"id":"x_1"}}}`))
		if !e.Known || !e.Handled || e.Kind != kind || e.ObjectID != "x_1" {
			t.Errorf("%s: %+v", typ, e)
		}
	}
	info, _ := ParseEvent(nil, []byte(`{"id":"evt_2","type":"payment_intent.succeeded","data":{"object":{"id":"pi_1"}}}`))
	if !info.Known || info.Handled {
		t.Errorf("informational: %+v", info)
	}
	unknown, _ := ParseEvent(nil, []byte(`{"id":"evt_3","type":"radar.early_fraud_warning.created","data":{"object":{"id":"issfr_1"}}}`))
	if unknown.Known || unknown.Handled || unknown.ID != "evt_3" {
		t.Errorf("unknown: %+v", unknown)
	}
	if _, err := ParseEvent(nil, []byte(`{"type":"invoice.paid"}`)); err == nil {
		t.Error("an event with no ID is refused")
	}
	if _, err := ParseEvent(nil, []byte(`<xml/>`)); err == nil {
		t.Error("a body that is not JSON is refused")
	}
}

// Amounts never pass through a float: the package imports no math and names no float type or float parser.
// req: ENG-10
func TestNoFloatsInTheAdapter(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "stripe.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range f.Imports {
		if strings.Contains(imp.Path.Value, `"math`) {
			t.Errorf("imports %s", imp.Path.Value)
		}
	}
	src, _ := os.ReadFile("stripe.go")
	for _, bad := range []string{"float32", "float64", "ParseFloat", "Float64()"} {
		// the package comment names the rule in prose; code never names a float type
		for _, line := range strings.Split(string(src), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if strings.Contains(line, bad) {
				t.Errorf("code uses %s: %s", bad, line)
			}
		}
	}
}

// The partner contract the Prism mock serves (contracts/partners/stripe.yaml) names every operation the adapter calls, and
// only GET ones.
// req: FR-INT-27
func TestThePartnerContractNamesEveryOperationTheAdapterCalls(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/partners/stripe.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/v1/balance", "/v1/balance_transactions", "/v1/charges/{id}", "/v1/credit_notes/{id}", "/v1/invoice_payments", "/v1/invoices/{id}", "/v1/payouts/{id}"} {
		ops, ok := doc.Paths[p]
		if !ok {
			t.Errorf("the contract has no %s", p)
		}
		for method := range ops {
			if method != "get" && method != "parameters" {
				t.Errorf("%s has a %s operation: the connector is read-only", p, method)
			}
		}
	}
}

// A subscription invoice is mirrored as Stripe reports it (FR-PSP-03, FR-PSP-07): the customer, the subscription, the collection state, and each line with its
// amount as an exact decimal and its service period; tax is counted in the total and not in a line. An invoice with a discount, a credit line, more
// lines than a page, or lines and tax that do not add up is mirrored with why it is unsupported, so a person decides it. req: FR-PSP-03
func TestAnInvoiceIsMirroredWithItsLinesPeriodsAndCollectionState(t *testing.T) {
	raw, err := os.ReadFile("testdata/invoice_subscription.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	o, err := object(ports.ProviderInvoice, doc, raw)
	if err != nil || o.Invoice == nil {
		t.Fatalf("%v %v", o, err)
	}
	i := o.Invoice
	if o.Currency != "SGD" || o.Amount != "109.00" || i.Total != "109.00" || i.AmountPaid != "109.00" || i.CustomerID != "cus_NeZw" || i.CustomerName != "Lumen Health Pte Ltd" ||
		i.SubscriptionID != "sub_1" || i.Number != "ACME-0001" || i.Status != "paid" || i.AttemptCount != 1 || i.NextAttempt != nil || i.Finalized == nil || i.Unsupported != "" {
		t.Fatalf("the invoice: %+v", i)
	}
	if len(i.Lines) != 1 || i.Lines[0].Amount != "100.00" || i.Lines[0].PeriodStart.Format("2006-01-02") != "2026-11-01" || i.Lines[0].PeriodEnd.Format("2006-01-02") != "2026-12-01" {
		t.Errorf("the lines: %+v", i.Lines)
	}
	for name, change := range map[string]func(string) string{
		"a discount": func(s string) string {
			return strings.Replace(s, `"total_discount_amounts":[]`, `"total_discount_amounts":[{"amount":100}]`, 1)
		},
		"a credit line":                    func(s string) string { return strings.Replace(s, `"amount":10000`, `"amount":-10000`, 1) },
		"more lines than one page":         func(s string) string { return strings.Replace(s, `"has_more":false`, `"has_more":true`, 1) },
		"lines and tax that do not add up": func(s string) string { return strings.Replace(s, `"total":10900`, `"total":11000`, 1) },
	} {
		d, _ := decode([]byte(change(string(raw))))
		x, err := object(ports.ProviderInvoice, d, raw)
		if err != nil || x.Invoice == nil || !strings.Contains(x.Invoice.Unsupported, name[strings.Index(name, " ")+1:]) {
			t.Errorf("%s: %+v, %v", name, x.Invoice, err)
		}
	}
	sub := `{"id":"sub_1","customer":"cus_NeZw","status":"active","currency":"sgd","cancel_at_period_end":false,"items":{"data":[
	  {"quantity":2,"price":{"unit_amount":5000,"recurring":{"interval":"month","interval_count":1}},"current_period_start":1793491200,"current_period_end":1796083200},
	  {"quantity":1,"price":{"unit_amount":120000,"recurring":{"interval":"year","interval_count":1}}},
	  {"quantity":1,"price":{"unit_amount":null,"recurring":{"interval":"month","interval_count":1}}}]}}`
	sd, _ := decode([]byte(sub))
	so, err := object(ports.ProviderSubscription, sd, []byte(sub))
	if err != nil || so.Subscription == nil || so.Subscription.Amount != "100.00" || so.Subscription.IntervalUnit != "month" || so.Subscription.Status != "active" ||
		so.Subscription.PeriodEnd == nil {
		t.Errorf("the subscription: %+v, %v", so.Subscription, err)
	}
}

// A credit note against an invoice is read as Stripe reports it (FR-PSP-07; field names [live-verify]): the invoice it credits, its status and reason, each line's amount
// before tax as an exact decimal and the total with tax. One with a discount, a negative line, more lines than a page, no lines, or lines and tax that do not add up to its
// total is read with why it is unsupported, so a person decides it. req: FR-PSP-07
func TestACreditNoteIsReadWithItsInvoiceLinesAndTotal(t *testing.T) {
	raw := []byte(`{"id":"cn_1","object":"credit_note","invoice":"in_1","number":"ACME-0001-CN-01","status":"issued","reason":"order_change","currency":"sgd","created":1793750400,
	  "total":3270,"total_taxes":[{"amount":270}],"lines":{"data":[{"id":"cnli_1","description":"Credit for the workshop","amount":3000,"discount_amounts":[]}],"has_more":false}}`)
	doc, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	o, err := object(ports.ProviderCreditNote, doc, raw)
	if err != nil || o.CreditNote == nil {
		t.Fatalf("%v %v", o, err)
	}
	c := o.CreditNote
	if o.Currency != "SGD" || o.Amount != "32.70" || c.Total != "32.70" || c.InvoiceID != "in_1" || c.Status != "issued" || c.Reason != "order_change" || c.Number != "ACME-0001-CN-01" ||
		len(c.Lines) != 1 || c.Lines[0].Amount != "30.00" || c.Unsupported != "" || len(o.Links[ports.ProviderInvoice]) != 1 {
		t.Fatalf("the credit note: %+v %+v", o, c)
	}
	for name, change := range map[string]func(string) string{
		"a discount": func(s string) string {
			return strings.Replace(s, `"discount_amounts":[]`, `"discount_amounts":[{"amount":100}]`, 1)
		},
		"a negative line":                  func(s string) string { return strings.Replace(s, `"amount":3000`, `"amount":-3000`, 1) },
		"more lines than one page":         func(s string) string { return strings.Replace(s, `"has_more":false`, `"has_more":true`, 1) },
		"lines and tax that do not add up": func(s string) string { return strings.Replace(s, `"total":3270`, `"total":3300`, 1) },
	} {
		d, _ := decode([]byte(change(string(raw))))
		x, err := object(ports.ProviderCreditNote, d, raw)
		if err != nil || x.CreditNote == nil || !strings.Contains(x.CreditNote.Unsupported, name[strings.Index(name, " ")+1:]) {
			t.Errorf("%s: %+v, %v", name, x.CreditNote, err)
		}
	}
}
