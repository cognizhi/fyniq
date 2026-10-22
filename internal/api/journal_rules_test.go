//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/journal"
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
	standardMode(e) // a void in Founder mode needs a second person (FM-1)
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

// FR-GL-02 (rev 0.55): a journal may carry the date the scheduler reverses it by itself, after its own date; posting
// refuses one on or before it, and the journal shows the date once posted.
// req: FR-GL-02
func TestAJournalReversesByItselfOnItsDate(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	accrual := e.draft("300.00")
	accrual["date"], accrual["reverse_on"] = "2026-10-31", "2026-10-31"
	r := e.call(http.MethodPost, "/v1/journals", accountant, accrual)
	if r.Status != http.StatusCreated || r.str("reverse_on") != "2026-10-31" {
		t.Fatalf("a draft with a reversal date: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag")), "postJournal", 422, journal.CodeReverseOn)
	accrual["reverse_on"] = "2026-11-01"
	r = e.call(http.MethodPut, "/v1/journals/"+r.str("id"), accountant, accrual, "If-Match", r.Header.Get("ETag"))
	p := e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag"))
	if p.Status != http.StatusOK || p.str("journal", "reverse_on") != "2026-11-01" {
		t.Fatalf("posting an accrual that reverses on 1 November: %d %s", p.Status, p.Raw)
	}
	bad := e.draft("1.00")
	bad["reverse_on"] = "1 Nov"
	e.problemIs(e.call(http.MethodPost, "/v1/journals", accountant, bad), "createJournal", 422, CodeInvalid)
}

// FR-GL-02, FR-AUT-02 (rev 0.58, founder decision 21 Oct 2026): an automatic reversal that waits for a period that is
// not open shows: as the journal's first status, in the list of waiting reversals and in the close checks.
// req: FR-GL-02
// req: FR-AUT-02
// req: C.2 #56
func TestAWaitingAutomaticReversalShows(t *testing.T) {
	e := newEnv(t)
	accountant, controller := e.as(authz.Accountant), e.as(authz.Controller)
	post := func(date, reverseOn string) string {
		t.Helper()
		body := e.draft("300.00")
		body["date"], body["reverse_on"] = date, reverseOn
		r := e.call(http.MethodPost, "/v1/journals", accountant, body)
		p := e.call(http.MethodPost, "/v1/journals/"+r.str("id")+"/post", accountant, nil, "If-Match", r.Header.Get("ETag"))
		if p.Status != http.StatusOK {
			t.Fatalf("posting a journal that reverses on %s: %d %s", reverseOn, p.Status, p.Raw)
		}
		return r.str("id")
	}
	first := func(id string) map[string]any {
		t.Helper()
		c, _ := e.call(http.MethodGet, "/v1/journals/"+id, accountant, nil).Body["checks"].([]any)
		if len(c) == 0 {
			t.Fatalf("journal %s has no statuses", id)
		}
		return c[0].(map[string]any)
	}
	waiting := func() []any {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/journals/waiting-reversals", accountant, nil)
		if r.Status != http.StatusOK || r.Body["next_cursor"] != nil {
			t.Fatalf("listWaitingReversals: %d %s", r.Status, r.Raw)
		}
		return r.Body["items"].([]any)
	}
	reversals := func(period string) map[string]any {
		t.Helper()
		s := e.call(http.MethodGet, "/v1/periods/"+period+"/close", controller, nil)
		r := e.call(http.MethodPost, "/v1/periods/"+period+"/close-checks", controller, nil, "If-Match", s.Header.Get("ETag"))
		if r.Status != http.StatusOK {
			t.Fatalf("runCloseChecks: %d %s", r.Status, r.Raw)
		}
		for _, c := range r.Body["checks"].(map[string]any)["checks"].([]any) {
			if c.(map[string]any)["key"] == "reversals" {
				return c.(map[string]any)
			}
		}
		t.Fatalf("no reversals check: %s", r.Raw)
		return nil
	}

	waits := post("2026-11-20", "2026-12-01") // no period covers December yet
	soon := post("2026-10-20", "2026-11-01")  // November is open: it posts when its day comes
	if c := first(waits); c["severity"] != "warning" || c["code"] != journal.StatusWaiting || c["field"] != "reverse_on" ||
		c["title"] != "Its automatic reversal is waiting" || !strings.Contains(c["fix"].(string), "Open the financial year of 2026-12-01") {
		t.Errorf("a journal whose reversal has no period: %v", c)
	}
	if c := first(soon); c["code"] != journal.StatusPosted {
		t.Errorf("a journal whose reversal's period is open: %v", c)
	}
	if items := waiting(); len(items) != 1 || items[0].(map[string]any)["id"] != waits || items[0].(map[string]any)["reverse_on"] != "2026-12-01" {
		t.Errorf("the waiting reversals: %v", items)
	}
	if c := reversals(e.books.Oct.ID.String()); c["severity"] != "ok" {
		t.Errorf("October's close, where nothing reverses or waits: %v", c)
	}
	c := reversals(e.books.Nov.ID.String())
	if d, _ := c["detail"].(string); c["severity"] != "warning" || c["title"] != "Automatic reversals have not posted" ||
		!strings.Contains(d, "1 journal(s) reverse by themselves on or before 2026-11-30") || !strings.Contains(d, "1 journal(s) dated in the period wait") {
		t.Errorf("November's close, with a reversal due in it and one waiting from it: %v", c)
	}

	// December opens: nothing waits. Once it is closed, the reversal waits again, for a closed month.
	var dec storage.Period
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		dec, err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), EndsOn: time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if c := first(waits); c["code"] != journal.StatusPosted || len(waiting()) != 0 {
		t.Errorf("a journal whose reversal's period opened: %v", c)
	}
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, dec.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if c := first(waits); c["code"] != journal.StatusWaiting || !strings.Contains(c["detail"].(string), "in a closed month") || len(waiting()) != 1 {
		t.Errorf("a journal whose reversal's month is closed: %v", c)
	}

	// A page at a time, earliest reversal first.
	later := post("2026-11-21", "2026-12-02")
	p1 := e.call(http.MethodGet, "/v1/journals/waiting-reversals?page_size=1", accountant, nil)
	next := p1.str("next_cursor")
	p2 := e.call(http.MethodGet, "/v1/journals/waiting-reversals?page_size=1&cursor="+next, accountant, nil)
	if items := p1.Body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != waits || next == "" {
		t.Errorf("the first page: %s", p1.Raw)
	}
	if items := p2.Body["items"].([]any); len(items) != 1 || items[0].(map[string]any)["id"] != later || p2.Body["next_cursor"] != nil {
		t.Errorf("the second page: %s", p2.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/journals/waiting-reversals?cursor=bm90LWEtY3Vyc29y", accountant, nil), "listWaitingReversals", 422, CodeCursor)
}

// ENG-20, ENG-21 (rev 0.57): every journal carries its statuses as the workbook's Journal sheet showed them, in its
// priority order: a draft every problem the posting rules find and whether its month is open, or OK; a posted journal
// OK, or Locked once its month is closed.
// req: ENG-20
// req: ENG-21
// req: ENG-23
func TestJournalsCarryTheWorkbooksStatuses(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	save := func(body map[string]any) string {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/journals", accountant, body)
		if r.Status != http.StatusCreated {
			t.Fatalf("saving a draft: %d %s", r.Status, r.Raw)
		}
		return r.str("id")
	}
	checks := func(id string) []any {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/journals/"+id, accountant, nil)
		c, _ := r.Body["checks"].([]any)
		return c
	}
	first := func(id string) map[string]any {
		t.Helper()
		c := checks(id)
		if len(c) == 0 {
			t.Fatalf("journal %s has no statuses", id)
		}
		return c[0].(map[string]any)
	}

	unbalanced := e.draft("100.00")
	unbalanced["lines"].([]map[string]any)[1]["amount"] = map[string]any{"amount": "-90.00", "currency": "SGD"}
	if c := first(save(unbalanced)); c["severity"] != "error" || c["code"] != "LEDGER-09" || c["title"] != "Entry does not balance" {
		t.Errorf("an unbalanced draft: %v", c)
	}
	undated := e.draft("100.00")
	delete(undated, "date")
	if c := first(save(undated)); c["code"] != "LEDGER-01" || c["title"] != "Enter the date" || c["field"] != "date" {
		t.Errorf("a draft with no date: %v", c)
	}
	december := e.draft("100.00")
	december["date"] = "2026-12-03"
	if c := first(save(december)); c["code"] != "STORAGE-PERIOD-01" || c["severity"] != "error" {
		t.Errorf("a draft in a month with no period: %v", c)
	}
	ready := save(e.draft("100.00"))
	if c := first(ready); c["severity"] != "ok" || c["code"] != "JOURNAL-OK" || c["title"] != "OK" {
		t.Errorf("a draft that can post: %v", c)
	}
	posted, _ := e.postedJournal("50.00")
	if c := first(posted); c["code"] != "JOURNAL-POSTED" {
		t.Errorf("a posted journal: %v", c)
	}
	// The list carries them too.
	l := e.call(http.MethodGet, "/v1/journals?page_size=100", accountant, nil)
	for _, it := range l.Body["items"].([]any) {
		if c, _ := it.(map[string]any)["checks"].([]any); len(c) == 0 {
			t.Errorf("a listed journal has no statuses: %v", it)
		}
	}

	// October closed: the posted journal is locked, and a draft dated in it cannot post.
	ctx := context.Background()
	as := func(who string) context.Context {
		return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: who}})
	}
	checklistDone(t, e.store, e.books.Org, e.books.Oct.ID)
	c, err := e.api.closing.RunChecks(as("status-preparer"), e.books.Org, e.books.Oct.ID)
	if err == nil {
		_, _, err = e.api.closing.Close(as("status-attester"), e.books.Org, e.books.Oct.ID, c.ID, "Reviewed.")
	}
	if err != nil {
		t.Fatal(err)
	}
	if c := first(posted); c["code"] != "JOURNAL-LOCKED" || c["title"] != "Locked (closed month)" || c["severity"] != "ok" {
		t.Errorf("a posted journal in a closed month: %v", c)
	}
	if c := first(ready); c["code"] != "STORAGE-INV-03" || c["title"] != "The month is closed" {
		t.Errorf("a draft in a closed month: %v", c)
	}
}
