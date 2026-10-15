//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The rules PRD-001 rev 0.24 decided for manual journals, through the API (p0-manual-journal-rules).

// moneySchema compiles the contract's Money schema, as a client or a model reads it.
func moneySchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	b, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	m := doc["components"].(map[string]any)["schemas"].(map[string]any)["Money"]
	raw, _ := json.Marshal(m)
	parsed, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("money.json", parsed); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("money.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// ENG-44 (rev 0.24): decimal places are counted as written, and the money-string pattern of each
// currency in the contract says so (FR-INT-13): "12345.0" is not a JPY amount, "100" is a USD one. The
// API refuses an amount beyond its currency's decimals with a field error, never rounding it.
// req: ENG-44
// req: FR-INT-13
// req: ENG-24
func TestMoneyStringsFollowTheirCurrency(t *testing.T) {
	s := moneySchema(t)
	for _, c := range []struct {
		amount, currency string
		valid            bool
	}{
		{"12345", "JPY", true}, {"12345.0", "JPY", false}, {"100", "USD", true}, {"100.5", "USD", true}, {"100.50", "USD", true},
		{"100.505", "USD", false}, {"1.234", "KWD", true}, {"1.2345", "KWD", false}, {"-0.01", "SGD", true},
	} {
		err := s.Validate(map[string]any{"amount": c.amount, "currency": c.currency})
		if (err == nil) != c.valid {
			t.Errorf("the contract says %s %s is valid: %v, want %v (%v)", c.currency, c.amount, err == nil, c.valid, err)
		}
	}

	e := newEnv(t)
	draft := func(amount, currency string) map[string]any {
		return map[string]any{"date": "2026-10-20", "lines": []map[string]any{
			{"account_id": e.books.Bank.ID.String(), "amount": map[string]any{"amount": amount, "currency": currency}, "fx_rate": "0.0091"},
			{"account_id": e.books.Sales.ID.String(), "amount": map[string]any{"amount": "-1.00", "currency": "SGD"}}}}
	}
	r := e.call(http.MethodPost, "/v1/journals", e.as(authz.Accountant), draft("12345.0", "JPY"))
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, `"field":"body.lines[0].amount.amount"`) ||
		!strings.Contains(r.Raw, money.CodePrecision) {
		t.Errorf("JPY 12345.0: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/journals", e.as(authz.Accountant), draft("100", "USD")); r.Status != http.StatusCreated {
		t.Errorf("USD 100: %d %s", r.Status, r.Raw)
	}
}

// ENG-03 (rev 0.24): a rate the client sends is the document's, so its source is "document", set when
// the client leaves it out; another source is refused. A posted base-currency line has rate 1 from
// "base", and a rate entered on one is refused at posting.
// req: ENG-03
// req: ENG-24
func TestATypedRateIsTheDocuments(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	line := func(amount, currency string, extra map[string]any) map[string]any {
		l := map[string]any{"amount": map[string]any{"amount": amount, "currency": currency}}
		for k, v := range extra {
			l[k] = v
		}
		return l
	}
	body := func(l1 map[string]any) map[string]any {
		l1["account_id"] = e.books.Bank.ID.String()
		l2 := line("-135.00", "SGD", map[string]any{"account_id": e.books.Sales.ID.String()})
		return map[string]any{"date": "2026-10-20", "lines": []map[string]any{l1, l2}}
	}
	r := e.call(http.MethodPost, "/v1/journals", accountant, body(line("100.00", "USD", map[string]any{"fx_rate": "1.35"})))
	if r.Status != http.StatusCreated || !strings.Contains(r.Raw, `"fx_rate_source":"document"`) {
		t.Fatalf("a rate without a source: %d %s", r.Status, r.Raw)
	}
	posted := e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag"))
	if posted.Status != http.StatusOK || !strings.Contains(posted.Raw, `"fx_rate":"1.35","fx_rate_source":"document"`) ||
		!strings.Contains(posted.Raw, `"fx_rate":"1","fx_rate_source":"base"`) {
		t.Fatalf("the posted rates: %d %s", posted.Status, posted.Raw)
	}
	r = e.call(http.MethodPost, "/v1/journals", accountant, body(line("100.00", "USD", map[string]any{"fx_rate": "1.35", "fx_rate_source": "feed"})))
	if r.Status != http.StatusUnprocessableEntity || !strings.Contains(r.Raw, `"field":"body.lines[0].fx_rate_source"`) {
		t.Errorf("a typed rate claiming the feed: %d %s", r.Status, r.Raw)
	}
	r = e.call(http.MethodPost, "/v1/journals", accountant, body(line("135.00", "SGD", map[string]any{"fx_rate": "1"})))
	if r.Status != http.StatusCreated {
		t.Fatalf("a draft with a rate on its base line: %d %s", r.Status, r.Raw)
	}
	r = e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag"))
	if r.Status != http.StatusUnprocessableEntity || r.problemCode() != ledger.CodeRateOnBase || !strings.Contains(r.Raw, `"field":"lines[0].fx_rate"`) {
		t.Errorf("posting a rate on a base-currency line: %d %s", r.Status, r.Raw)
	}
}

// FR-GL-12 (rev 0.24, from P0 for manual journals): a reversal is dated on or after its journal, and a
// journal is reversed once.
// req: FR-GL-12
func TestAReversalIsDatedOnOrAfterItsJournalAndHappensOnce(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	id, tag := e.postedJournal("10.00") // dated 2026-10-20
	reverse := func(date, etag string) response {
		return e.call(http.MethodPost, "/v1/journals/"+id+"/reverse", accountant, map[string]any{"date": date, "reason": "Duplicate"}, "If-Match", etag)
	}
	if r := reverse("2026-10-19", tag); r.Status != http.StatusUnprocessableEntity || r.problemCode() != ledger.CodeReversalDate {
		t.Fatalf("a reversal the day before: %d %s", r.Status, r.Raw)
	}
	r := reverse("2026-10-20", tag)
	if r.Status != http.StatusOK {
		t.Fatalf("a reversal the same day: %d %s", r.Status, r.Raw)
	}
	if again := reverse("2026-10-21", r.Header.Get("ETag")); again.Status != http.StatusConflict {
		t.Errorf("a second reversal: %d %s", again.Status, again.Raw)
	}
}

// INV-13 from P0, through the storage port: an account created with the share-capital mark reaches the
// engine, which refuses a manual journal line to it.
// req: INV-13
func TestAJournalToShareCapitalIsRefused(t *testing.T) {
	e := newEnv(t)
	var capital storage.Account
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		capital, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "3010", Name: "Share capital, ordinary", Type: storage.Equity,
			Role: storage.RoleShareCapital})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	accountant := e.as(authz.Accountant)
	r := e.call(http.MethodPost, "/v1/journals", accountant, map[string]any{"date": "2026-10-20", "lines": []map[string]any{
		{"account_id": e.books.Bank.ID.String(), "amount": map[string]any{"amount": "1000.00", "currency": "SGD"}},
		{"account_id": capital.ID.String(), "amount": map[string]any{"amount": "-1000.00", "currency": "SGD"}}}})
	if r.Status != http.StatusCreated {
		t.Fatalf("the draft: %d %s", r.Status, r.Raw)
	}
	r = e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag"))
	if r.Status != http.StatusUnprocessableEntity || r.problemCode() != ledger.CodeShareCapital || !strings.Contains(r.Raw, `"field":"lines[1].account_id"`) {
		t.Errorf("a journal to share capital: %d %s", r.Status, r.Raw)
	}
}
