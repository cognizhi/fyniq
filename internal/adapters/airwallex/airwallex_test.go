package airwallex

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// fakeAirwallex answers the three operations of contracts/partners/airwallex.yaml: the key exchange, the current
// balances, and an SGD history of three records over two pages, amounts as JSON numbers.
func fakeAirwallex(t *testing.T, logins *int, seen *[]*http.Request) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r)
		switch r.URL.Path {
		case "/api/v1/authentication/login":
			if r.Method != http.MethodPost || r.Header.Get("x-client-id") != "client-1" || r.Header.Get("x-api-key") != "key-1" {
				http.Error(w, `{"code":"credentials_invalid"}`, http.StatusUnauthorized)
				return
			}
			*logins++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"token-` + strconv.Itoa(*logins) + `","expires_at":"2026-10-26T09:30:00+0000"}`))
			return
		}
		if r.Header.Get("Authorization") == "" || !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer token-") {
			http.Error(w, "unauthorised", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/balances/current":
			_, _ = w.Write([]byte(`[{"account_type":"cash","available_amount":1000.5,"currency":"SGD","pending_amount":0,"total_amount":1000.5},
				{"account_type":"cash","available_amount":20,"currency":"USD","pending_amount":0,"total_amount":20},
				{"account_type":"credit","available_amount":5000,"currency":"SGD","total_amount":0}]`))
		case "/api/v1/balances/history":
			q := r.URL.Query()
			if q.Get("currency") != "SGD" || q.Get("account_type") != "cash" || q.Get("from_post_at") == "" || q.Get("to_post_at") == "" {
				http.Error(w, "query", http.StatusBadRequest)
				return
			}
			if q.Get("page") == "" {
				_, _ = w.Write([]byte(`{"has_more":true,"page_after":"bookmark-2","page_before":null,"items":[
					{"id":"h-2","amount":-12.34,"balance":987.66,"currency":"SGD","description":"Pay SGD 12.34.","posted_at":"2026-10-21T03:52:34+0000","source_type":"PAYOUT"},
					{"id":"h-1","amount":1000000.01,"balance":1000000.01,"currency":"SGD","description":"Deposit","posted_at":"2026-10-20T01:00:00+0000","reference":"INV-0138","source_type":"DEPOSIT"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"has_more":false,"page_after":null,"page_before":"bookmark-1","items":[
				{"id":"h-3","amount":0.5,"balance":988.16,"currency":"SGD","description":"Interest","posted_at":"2026-10-22T00:00:00+0000","source_type":"YIELD"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The Airwallex adapter exchanges its scoped key for a token once and reuses it, lists the wallet's cash balances by
// currency, and reads a currency's history in windows of seven days, page by page with Airwallex's bookmark, oldest
// first, amounts as the text Airwallex sent; it sends GET and the token exchange only.
// req: FR-BNK-18
// req: FR-BNK-15
// req: banking/bank-transactions Wise or Airwallex account syncs itself
func TestTheAirwallexAdapterReadsBalancesAndHistory(t *testing.T) {
	logins := 0
	var seen []*http.Request
	srv := fakeAirwallex(t, &logins, &seen)
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	c := &Client{Base: srv.URL, ClientID: "client-1", APIKey: "key-1", History: 5 * 24 * time.Hour, HTTP: srv.Client(), Now: func() time.Time { return now }}
	ctx := context.Background()
	accounts, err := c.Accounts(ctx)
	if err != nil || len(accounts) != 2 || accounts[0].ID != "SGD" || accounts[1].Currency != "USD" {
		t.Fatalf("the balances: %+v, %v", accounts, err)
	}
	page, err := c.Transactions(ctx, "SGD", "", time.Time{})
	if err != nil || !page.More || len(page.Transactions) != 2 || page.Transactions[0].ID != "h-1" || page.Transactions[0].Amount != "1000000.01" ||
		page.Transactions[0].Description != "Deposit INV-0138" || page.Transactions[1].Amount != "-12.34" || page.Transactions[1].RunningBalance != "987.66" {
		t.Fatalf("the first page: %+v, %v", page, err)
	}
	second, err := c.Transactions(ctx, "SGD", page.Cursor, time.Time{})
	if err != nil || second.More || len(second.Transactions) != 1 || second.Transactions[0].Amount != "0.5" {
		t.Fatalf("the second page: %+v, %v", second, err)
	}
	if last := seen[len(seen)-1].URL.Query(); last.Get("page") != "bookmark-2" || last.Get("from_post_at") != now.Add(-5*24*time.Hour).Format(time.RFC3339) {
		t.Errorf("the second page asked %v", last)
	}
	if logins != 1 {
		t.Errorf("the key was exchanged %d times", logins)
	}
	switched := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC) // a source switched to Airwallex that day starts there
	if _, err := c.Transactions(ctx, "SGD", "", switched); err != nil || seen[len(seen)-1].URL.Query().Get("from_post_at") != switched.Format(time.RFC3339) {
		t.Fatalf("from the switch: %v, %v", err, seen[len(seen)-1].URL.Query())
	}
	// A cursor older than a week reads a week, and says more waits.
	c.History = 30 * 24 * time.Hour
	if p, err := c.Transactions(ctx, "SGD", "", time.Time{}); err != nil || !p.More {
		t.Fatalf("a month's history: %+v, %v", p, err)
	}
	if last := seen[len(seen)-1].URL.Query(); last.Get("to_post_at") != now.Add(-23*24*time.Hour).Format(time.RFC3339) {
		t.Errorf("the window ends %s", last.Get("to_post_at"))
	}
	if _, err := (&Client{Base: srv.URL, ClientID: "client-1", APIKey: "wrong", HTTP: srv.Client(), Now: c.Now}).Accounts(ctx); err == nil ||
		strings.Contains(err.Error(), "credentials_invalid") {
		t.Errorf("a refused key: %v (the provider's words never pass through)", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/transfers/create", nil)
	if _, err := c.client().Do(req); !errors.Is(err, ErrMethod) {
		t.Errorf("a POST left: %v", err)
	}
	for _, r := range seen {
		if r.Method != http.MethodGet && r.URL.Path != loginPath {
			t.Errorf("sent %s %s", r.Method, r.URL.Path)
		}
	}
}

// The partner document has the operations the adapter calls, and the fields it reads.
// req: FR-BNK-18
func TestTheAirwallexPartnerDocumentHasWhatTheAdapterReads(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/partners/airwallex.yaml")
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
	for p, m := range map[string]string{loginPath: "post", "/api/v1/balances/current": "get", "/api/v1/balances/history": "get"} {
		if ops := doc.Paths[p]; ops[m] == nil || len(ops) != 1 {
			t.Errorf("%s: %v", p, ops)
		}
	}
	for _, field := range []string{"page_after", "has_more", "posted_at", "balance", "from_post_at", "to_post_at", "expires_at"} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("the document has no %s", field)
		}
	}
}

func sign(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// A webhook verifies with the endpoint's secret over its timestamp and raw body, within five minutes; a forged
// signature, another secret, a changed body and a stale timestamp are refused.
// req: FR-BNK-18
// req: FR-INT-27
// asvs: V10.5.1
func TestAnAirwallexWebhookVerifiesWithinFiveMinutes(t *testing.T) {
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	body := []byte(`{"id":"evt_1","name":"balance.updated","data":{"amount":"999999"}}`)
	ts := strconv.FormatInt(now.Add(-time.Minute).UnixMilli(), 10)
	h := http.Header{}
	h.Set("x-timestamp", ts)
	h.Set("x-signature", sign("whsec-1", ts, body))
	if err := Verify("whsec-1", h, body, now); err != nil {
		t.Fatalf("a good webhook: %v", err)
	}
	if err := Verify("whsec-2", h, body, now); !errors.Is(err, ErrSignature) {
		t.Errorf("another secret: %v", err)
	}
	if err := Verify("whsec-1", h, append(body, ' '), now); !errors.Is(err, ErrSignature) {
		t.Errorf("a changed body: %v", err)
	}
	if err := Verify("whsec-1", h, body, now.Add(5*time.Minute)); !errors.Is(err, ErrStale) {
		t.Errorf("a stale webhook: %v", err)
	}
	h.Set("x-signature", strings.Repeat("0", 64))
	if err := Verify("whsec-1", h, body, now); !errors.Is(err, ErrSignature) {
		t.Errorf("a forged signature: %v", err)
	}
}
