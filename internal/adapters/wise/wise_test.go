package wise

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// fakeWise answers the two operations of the extract in contracts/partners/wise.yaml for profile 4242, as Wise's
// reference shows them: amounts as JSON numbers.
func fakeWise(t *testing.T, seen *[]*http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r)
		if r.Header.Get("Authorization") != "Bearer token-1" {
			http.Error(w, "unauthorised", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/2026Q4/profiles/4242/balances":
			if r.URL.Query().Get("types") != "STANDARD" {
				http.Error(w, "types", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`[{"id":200001,"currency":"SGD","type":"STANDARD","name":null,"amount":{"value":1310.86,"currency":"SGD"}},
				{"id":200002,"currency":"USD","type":"STANDARD","name":"Payroll USD","amount":{"value":20.1,"currency":"USD"}}]`))
		case "/2026Q4/profiles/4242/balance-statements/200001/statement.json":
			q := r.URL.Query()
			if q.Get("currency") != "SGD" || q.Get("type") != "COMPACT" || q.Get("intervalStart") == "" || q.Get("intervalEnd") == "" {
				http.Error(w, "query", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"accountHolder":{"type":"BUSINESS"},"transactions":[
				{"type":"DEBIT","date":"2026-10-21T08:47:05.832Z","amount":{"value":-7.76,"currency":"SGD"},
				 "details":{"type":"CARD","description":"Card transaction of 7.76 SGD issued by AWS","merchant":{"name":"AWS EMEA"}},
				 "runningBalance":{"value":1310.86,"currency":"SGD"},"referenceNumber":"CARD-249281"},
				{"type":"CREDIT","date":"2026-10-20T01:00:00Z","amount":{"value":1234567.89,"currency":"SGD"},
				 "details":{"type":"DEPOSIT","description":"Received money from ACME","senderName":"ACME ROBOTICS"},
				 "runningBalance":{"value":1318.62,"currency":"SGD"},"referenceNumber":"DEPOSIT-1001"}],
				"endOfStatementBalance":{"value":1310.86,"currency":"SGD"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The Wise adapter reads a business profile's standard balances and a balance's statement through the operations of
// the published API's extract, under the profile's token; amounts keep the text Wise sent, oldest first; the cursor is
// the statement's end, and a statement never spans more than 469 days. It sends GET only.
// req: FR-BNK-18
// req: FR-BNK-15
// req: banking/bank-transactions Wise or Airwallex account syncs itself
func TestTheWiseAdapterReadsBalancesAndStatements(t *testing.T) {
	var seen []*http.Request
	srv := fakeWise(t, &seen)
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	c := &Client{Base: srv.URL + "/2026Q4", Token: "token-1", Profile: "4242", History: 90 * 24 * time.Hour, HTTP: srv.Client(), Now: func() time.Time { return now }}
	ctx := context.Background()
	accounts, err := c.Accounts(ctx)
	if err != nil || len(accounts) != 2 || accounts[0].ID != "200001" || accounts[0].Name != "SGD balance" || accounts[1].Name != "Payroll USD" {
		t.Fatalf("the balances: %+v, %v", accounts, err)
	}
	page, err := c.Transactions(ctx, "200001", "", time.Time{})
	if err != nil || len(page.Transactions) != 2 || page.More || page.Cursor != now.Format(time.RFC3339Nano) {
		t.Fatalf("the statement: %+v, %v", page, err)
	}
	first, second := page.Transactions[0], page.Transactions[1]
	if first.ID != "DEPOSIT-1001" || first.Amount != "1234567.89" || first.Counterparty != "ACME ROBOTICS" || first.RunningBalance != "1318.62" ||
		second.Amount != "-7.76" || second.Counterparty != "AWS EMEA" || second.Currency != "SGD" {
		t.Fatalf("the transactions: %+v", page.Transactions)
	}
	last := seen[len(seen)-1].URL.Query()
	if last.Get("intervalStart") != now.Add(-90*24*time.Hour).Format(time.RFC3339Nano) {
		t.Errorf("the first sync starts at %s", last.Get("intervalStart"))
	}
	switched := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC) // a source switched to Wise that day starts there
	if _, err := c.Transactions(ctx, "200001", "", switched); err != nil || seen[len(seen)-1].URL.Query().Get("intervalStart") != switched.Format(time.RFC3339Nano) {
		t.Fatalf("from the switch: %v, %v", err, seen[len(seen)-1].URL.Query())
	}
	old := now.Add(-600 * 24 * time.Hour)
	page, err = c.Transactions(ctx, "200001", old.Format(time.RFC3339Nano), time.Time{})
	if err != nil || !page.More || page.Cursor != old.Add(MaxInterval).Format(time.RFC3339Nano) {
		t.Fatalf("a statement over 469 days: %+v, %v", page, err)
	}
	if _, err := c.Transactions(ctx, "999", "", time.Time{}); err == nil {
		t.Error("a balance the profile does not have was read")
	}
	if _, err := (&Client{Base: srv.URL + "/2026Q4", Token: "wrong", Profile: "4242", HTTP: srv.Client(), Now: c.Now}).Accounts(ctx); err == nil ||
		strings.Contains(err.Error(), "unauthorised") {
		t.Errorf("a refused token: %v (the provider's words never pass through)", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/2026Q4/transfers", nil)
	if _, err := c.client().Do(req); !errors.Is(err, ErrMethod) {
		t.Errorf("a POST left: %v", err)
	}
	for _, r := range seen {
		if r.Method != http.MethodGet || r.URL.Path == "/2026Q4/transfers" {
			t.Errorf("sent %s %s", r.Method, r.URL.Path)
		}
	}
}

// The extract of Wise's published document has the operations the adapter calls, read-only, with the fields it reads.
// req: FR-BNK-18
func TestTheWisePartnerDocumentHasWhatTheAdapterReads(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/partners/wise.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Servers []struct {
			URL string `yaml:"url"`
		} `yaml:"servers"`
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Servers) != 2 || doc.Servers[0].URL != ProductionBase || doc.Servers[1].URL != SandboxBase {
		t.Errorf("the servers: %+v", doc.Servers)
	}
	for _, p := range []string{"/profiles/{profileId}/balances", "/profiles/{profileId}/balance-statements/{balanceId}/statement.json"} {
		ops, ok := doc.Paths[p]
		if !ok || ops["get"] == nil || len(ops) != 1 {
			t.Errorf("%s: %v", p, ops)
		}
	}
	for _, field := range []string{"referenceNumber", "runningBalance", "senderName", "intervalStart", "intervalEnd", "COMPACT"} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("the document has no %s", field)
		}
	}
}

// A webhook verifies under Wise's key, with the reference code's own example (its sandbox key, body and signature), and
// a changed body, another key or no signature is refused.
// req: FR-BNK-18
// req: FR-INT-27
// asvs: V10.5.1
func TestAWiseWebhookVerifiesOnlyWithWisesSignature(t *testing.T) {
	body := []byte(`{"data":{"resource":{"id":49983981,"profile_id":16055450,"account_id":14124090,"type":"transfer"},"current_state":"incoming_payment_waiting","previous_state":null,"occurred_at":"2021-08-23T10:12:50Z"},"subscription_id":"90aa8e14-4ef1-4a56-861c-f3c9cde097ea","event_type":"transfers#state-change","schema_version":"2.0.0","sent_at":"2021-08-23T10:12:50Z"}`)
	h := http.Header{}
	h.Set("X-Signature-SHA256", "wKcKCYXAzxNgiu7xmoDm943NUni7Rz33QN8JkEA9dWSGebgndonabgSj18Y4C08OrwVmueGsED2s00M7DtJVcYKOS1i3G4TMVx+mgM3aL9djMBkQtiYNBFUd6wrPI7ZUNHv/TrlKSjTMc+6JFvUvJ7owY3z85e3I4jLRLJowMFvO8kvCJ60+1pY9wDwZvtZ//WS93LrwGjk9Dvwzpmu0w+P4J75tETT5qC3Uv0y5G2yO8SEoO3yNP/tg/BOli02niHb53vEOUWUb9bly6thnfMoXoiV/osoGxgF20R58RlvkAmezyyl1Sv542TfS2DpiwVnmjjjkCyXeSUcKookYLQ==")
	if err := Verify(true, h, body); err != nil {
		t.Fatalf("Wise's own example: %v", err)
	}
	if err := Verify(true, h, append(body, ' ')); !errors.Is(err, ErrSignature) {
		t.Errorf("a changed body: %v", err)
	}
	if err := Verify(false, h, body); !errors.Is(err, ErrSignature) {
		t.Errorf("the production key: %v", err)
	}
	if err := Verify(true, http.Header{}, body); !errors.Is(err, ErrSignature) {
		t.Errorf("no signature: %v", err)
	}
}
