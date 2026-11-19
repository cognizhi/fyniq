package hitpay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
)

func sig(salt string, body []byte) string {
	m := hmac.New(sha256.New, []byte(salt))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// A webhook is accepted only with the HMAC-SHA256 of its raw body under the webhook's salt, in hex; a changed body,
// another salt, no header or one that is not hex is refused. HitPay signs no timestamp.
// req: banking/payment-providers Signed webhooks
// asvs: V4.1.5
func TestAWebhookIsAcceptedOnlyWithASignatureOfItsRawBody(t *testing.T) {
	body := []byte(`{"id":"9e9be893","status":"succeeded"}`)
	for _, c := range []struct {
		name, salt, header string
		body               []byte
		ok                 bool
	}{
		{"valid", "salt", sig("salt", body), body, true},
		{"valid in upper-case hex", "salt", strings.ToUpper(sig("salt", body)), body, true},
		{"a changed body", "salt", sig("salt", body), []byte(`{"id":"9e9be893","status":"failed"}`), false},
		{"another salt", "other", sig("salt", body), body, false},
		{"no salt set", "", sig("salt", body), body, false},
		{"no header", "salt", "", body, false},
		{"not hex", "salt", "zz", body, false},
	} {
		h := http.Header{}
		h.Set("Hitpay-Signature", c.header)
		if err := Verify(c.salt, h, c.body); c.ok != (err == nil) || (!c.ok && !errors.Is(err, ErrSignature)) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
}

func fake(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("X-BUSINESS-API-KEY") != "key-1" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/balances":
			_, _ = w.Write([]byte(`[{"currency":"sgd","wallets":{"available":2036.25,"reserve":0.10,"pending":600}},{"currency":"usd","wallets":{"available":1,"reserve":0}}]`))
		case "/v1/charges":
			q := r.URL.Query()
			if q.Get("date_from") == "" || q.Get("status") != "succeeded" && q.Get("status") != "" {
				http.Error(w, "query", http.StatusBadRequest)
				return
			}
			if q.Get("cursor") == "" {
				_, _ = w.Write([]byte(`{"data":[{"id":"ch-1","amount":5450,"fixed_fee":0.3,"discount_fee":35.43,"currency":"sgd","order_reference_number":"INV-0150","closed_at":"2026-11-10T10:00:00+08:00","customer":{"name":"Acme Pte Ltd"}}],"meta":{"next_cursor":"c2"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"ch-2","amount":0.1,"fixed_fee":0,"discount_fee":0,"currency":"sgd","closed_at":"2026-11-11T10:00:00+08:00"},{"id":"ch-usd","amount":9,"currency":"usd","closed_at":"2026-11-11T10:00:00+08:00"}],"meta":{"next_cursor":null}}`))
		case "/v1/payouts":
			_, _ = w.Write([]byte(`{"data":[{"id":"po-1","amount":5414.27,"currency":"sgd","status":"paid","arrival_date":"2026-11-11","reference":"R-1"}],"meta":{"next_cursor":null}}`))
		case "/v1/payouts/po-1":
			_, _ = w.Write([]byte(`{"id":"po-1","amount":5414.27,"currency":"sgd","status":"paid","created_at":"2026-11-10T00:00:00+08:00"}`))
		case "/v1/payouts/po-1/charges":
			_, _ = w.Write([]byte(`{"data":[{"id":"ch-1"}],"meta":{"next_cursor":null}}`))
		case "/v1/charges/ch-1":
			_, _ = w.Write([]byte(`{"id":"ch-1","amount":5450,"currency":"sgd","status":"succeeded","payment_request_id":"pr-9"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func client(srv *httptest.Server) *Client {
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	return &Client{Base: srv.URL, Key: "key-1", HTTP: srv.Client(), Now: func() time.Time { return now }, History: 5 * 24 * time.Hour}
}

// UC-PSP-02: a B2B invoice of S$5,450 paid by PayNow: HitPay's charge is a line for the gross naming the invoice, its fee
// (S$0.30 and S$35.43 = S$35.73) a line of its own, and the payout of S$5,414.27 a negative line; amounts are JSON numbers
// read as exact decimals, a charge in another currency is left to its own account, and the lines net to zero.
// req: ENG-10
func TestChargesFeesAndPayoutsComeAsStatementLines(t *testing.T) {
	var seen []string
	srv := fake(t, &seen)
	c := client(srv)
	page, err := c.Transactions(context.Background(), "SGD", "", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range page.Transactions {
		got = append(got, l.ID+"="+l.Amount)
	}
	if want := "ch-1=5450.00 ch-1:fee=-35.73 po-1=-5414.27"; !strings.HasPrefix(strings.Join(got, " "), want) {
		t.Fatalf("lines = %v, want %s ...", got, want)
	}
	if !strings.Contains(strings.Join(got, " "), "ch-2=0.10") {
		t.Errorf("0.1 reads as exactly S$0.10: %v", got)
	}
	for _, l := range page.Transactions {
		if strings.Contains(l.ID, "usd") {
			t.Errorf("a charge in USD came into the SGD account: %v", l)
		}
	}
	if d := page.Transactions[0].Description; d != "HitPay charge ch-1 INV-0150" || page.Transactions[0].Counterparty != "Acme Pte Ltd" {
		t.Errorf("the line names the invoice reference and the payer: %+v", page.Transactions[0])
	}
	if page.Cursor != "2026-11-12" || page.More {
		t.Errorf("cursor %q more %v: the window ends today and is read again", page.Cursor, page.More)
	}
	sum := 0
	for _, l := range page.Transactions[:3] {
		sum += map[string]int{"5450.00": 545000, "-35.73": -3573, "-5414.27": -541427}[l.Amount]
	}
	if sum != 0 {
		t.Errorf("the charge, its fee and its payout net to %d cents", sum)
	}
}

// A charge and a payout are fetched by ID, the payout with the charges it settles; the balance is available, pending and
// reserve together; nothing but GET is sent. req: FR-INT-27
func TestObjectsBalanceAndReadOnly(t *testing.T) {
	var seen []string
	srv := fake(t, &seen)
	c := client(srv)
	ctx := context.Background()
	po, err := c.Object(ctx, ports.ProviderPayout, "po-1")
	if err != nil || po.Amount != "5414.27" || po.Links[ports.ProviderBalanceTransaction][0] != "ch-1" {
		t.Fatalf("%+v, %v", po, err)
	}
	ch, err := c.Object(ctx, ports.ProviderCharge, "ch-1")
	if err != nil || ch.Amount != "5450.00" || ch.Links[ports.ProviderInvoice][0] != "pr-9" {
		t.Fatalf("%+v, %v", ch, err)
	}
	if _, err := c.Object(ctx, ports.ProviderCharge, "nope"); !errors.Is(err, ports.ErrProviderNotFound) {
		t.Errorf("%v", err)
	}
	if _, err := c.Object(ctx, ports.ProviderSubscription, "x"); err == nil {
		t.Error("recurring billing is not read in P1")
	}
	bal, err := c.Balance(ctx)
	if err != nil || bal[0] != (ports.ProviderBalance{Currency: "SGD", Amount: "2636.35"}) {
		t.Fatalf("%+v, %v", bal, err)
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		req, _ := http.NewRequest(m, srv.URL+"/v1/charges/ch-1/refund", nil)
		if _, err := c.client().Do(req); !errors.Is(err, ErrMethod) {
			t.Errorf("%s: %v", m, err)
		}
	}
	for _, s := range seen {
		if !strings.HasPrefix(s, "GET ") {
			t.Errorf("not a GET: %s", s)
		}
	}
}

// HitPay's event has no ID: Fyniq makes one of the type, the object and a digest of the body, so a redelivery repeats it and
// a changed charge does not; a charge or payout is handled, other known objects are informational, and others unknown.
// req: banking/payment-providers Unknown event type parked
func TestEventsGetAnIDFromTheirBody(t *testing.T) {
	h := func(object, kind string) http.Header {
		x := http.Header{}
		x.Set("Hitpay-Event-Object", object)
		x.Set("Hitpay-Event-Type", kind)
		return x
	}
	a, err := ParseEvent(h("charge", "created"), []byte(`{"id":"ch-1","status":"succeeded"}`))
	if err != nil || !a.Known || !a.Handled || a.Kind != ports.ProviderCharge || a.ObjectID != "ch-1" || a.Type != "charge.created" {
		t.Fatalf("%+v, %v", a, err)
	}
	again, _ := ParseEvent(h("charge", "created"), []byte(`{"id":"ch-1","status":"succeeded"}`))
	changed, _ := ParseEvent(h("charge", "updated"), []byte(`{"id":"ch-1","status":"refunded"}`))
	if again.ID != a.ID || changed.ID == a.ID {
		t.Errorf("ids: %s %s %s", a.ID, again.ID, changed.ID)
	}
	if o, _ := ParseEvent(h("transfer", "paid"), []byte(`{"id":"t-1"}`)); !o.Known || o.Handled {
		t.Errorf("%+v", o)
	}
	if u, _ := ParseEvent(h("widget", "created"), []byte(`{"id":"w-1"}`)); u.Known || u.Handled {
		t.Errorf("%+v", u)
	}
	if _, err := ParseEvent(http.Header{}, []byte(`{}`)); err == nil {
		t.Error("no headers is refused")
	}
}

// Amounts never pass through a float. req: ENG-10
func TestNoFloatsInTheAdapter(t *testing.T) {
	src, _ := os.ReadFile("hitpay.go")
	for _, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		for _, bad := range []string{"float32", "float64", "ParseFloat", "Float64()", `"math"`} {
			if strings.Contains(line, bad) {
				t.Errorf("code uses %s: %s", bad, line)
			}
		}
	}
}

// The partner contract the Prism mock serves (contracts/partners/hitpay.yaml) names every operation the adapter calls, and
// only GET ones.
// req: FR-INT-27
func TestThePartnerContractNamesEveryOperationTheAdapterCalls(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/partners/hitpay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/v1/balances", "/v1/charges", "/v1/charges/{id}", "/v1/payouts", "/v1/payouts/{id}", "/v1/payouts/{id}/charges"} {
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

// A HitPay amount is read as the exact text of the JSON number: 0.1 is S$0.10, a 18-digit amount is kept to the cent, and
// one with more decimals than the currency has, or in an exponent, is refused rather than rounded.
// req: ENG-10
func TestAmountsAreExactOrRefused(t *testing.T) {
	cur, err := money.Lookup("SGD")
	if err != nil {
		t.Fatal(err)
	}
	read := func(raw string) (string, error) {
		var m obj
		if err := decode([]byte(`{"amount":`+raw+`}`), &m); err != nil {
			t.Fatal(err)
		}
		v, err := amount(m, "amount", cur)
		return v.String(), err
	}
	for raw, want := range map[string]string{"0.1": "0.10", "5450": "5450.00", "1234567890123456.78": "1234567890123456.78", "0": "0.00"} {
		if got, err := read(raw); err != nil || got != want {
			t.Errorf("%s: %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"0.001", "1e2", "1.5e1"} {
		if got, err := read(raw); err == nil {
			t.Errorf("%s was read as %q: it must be refused", raw, got)
		}
	}
}
