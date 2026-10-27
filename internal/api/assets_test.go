//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// computerEquipment gives e's organisation 1510 Computer equipment, an asset account of the register, with 6610
// Depreciation and 1590 Accumulated depreciation by role, once, and returns 1510.
func (e *env) computerEquipment() storage.Account {
	e.t.Helper()
	var a storage.Account
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if a, err = u.Accounts().ByCode(ctx, "1510"); err == nil {
			return nil
		}
		if a, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1510", Name: "Computer equipment", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{ReportGroup: "Property, plant & equipment"}}); err != nil {
			return err
		}
		if _, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "6610", Name: "Depreciation", Type: storage.Expense,
			Role: storage.AccountRole(ledger.RoleDepreciation)}); err != nil {
			return err
		}
		_, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1590", Name: "Accumulated depreciation", Type: storage.Asset,
			Role: storage.AccountRole(ledger.RoleAccumulatedDepreciation)})
		return err
	})
	return a
}

// booksFrom sets e's books start to the first of October 2026.
func (e *env) booksFrom() {
	e.t.Helper()
	start, end := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		_, err = u.UpdateOrganisation(ctx, o.Version, pr)
		return err
	})
}

// laptopBill posts a bill of the supplier whose first line is a laptop on 1510, net in currency at rate ("" for the
// base currency) with no GST, and whose second line is rent; it returns the bill's ID.
func (e *env) laptopBill(net, currency, rate string) string {
	e.t.Helper()
	acc := e.computerEquipment()
	d := map[string]any{"supplier_id": e.supplier(), "supplier_ref": "LP-" + net + currency, "date": "2026-10-14", "currency": currency,
		"lines": []map[string]any{
			{"account_id": acc.ID.String(), "description": "MacBook Pro 14", "gst_code": "ZP",
				"net": map[string]any{"amount": net, "currency": currency}, "gst": map[string]any{"amount": "0.00", "currency": currency}},
			{"account_id": e.books.Rent.ID.String(), "description": "Desk rent", "gst_code": "ZP",
				"net": map[string]any{"amount": "100.00", "currency": currency}, "gst": map[string]any{"amount": "0.00", "currency": currency}},
		}}
	if rate != "" {
		d["fx_rate"] = rate
	}
	r := e.call(http.MethodPost, "/v1/bills", e.as(authz.Accountant), d)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("createBill = %d: %s", r.Status, r.Raw)
	}
	// Posted by the command itself: above the self-approval threshold, Founder mode would ask another person (FM-2).
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if _, err := (journal.Bills{Store: e.store}).Post(ctx, e.books.Org, uuid.MustParse(r.str("id"))); err != nil {
		e.t.Fatalf("post the bill: %v", err)
	}
	return r.str("id")
}

// registered registers line 1 of a new laptop bill as an Accountant, with a life of 36 months, and returns the asset's
// ID and ETag.
func (e *env) registered(net string) (id, tag string) {
	e.t.Helper()
	bill := e.laptopBill(net, "SGD", "")
	r := e.call(http.MethodPost, "/v1/fixed-assets", e.as(authz.Accountant), map[string]any{"source": map[string]any{"document_id": bill, "line": 1},
		"life_months": 36})
	if r.Status != http.StatusCreated {
		e.t.Fatalf("registerFixedAsset = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// FR-FA-01 through the API (p1-fixed-assets task 1.3): an asset registered from a bill line takes what the line's entry
// put on the asset account; from a bill in another currency, its RND-FX base; a line is registered once, and only a
// line on an asset account; one bought before the books start carries its opening accumulated depreciation; a change is
// the next version; a Bookkeeper reads the register and registers nothing.
// req: FR-FA-01
func TestRegisteringFixedAssets(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)

	// From a bill line: its cost is the line's base amount, its name the line's, its start the purchase month.
	bill := e.laptopBill("3600.00", "SGD", "")
	body := map[string]any{"source": map[string]any{"document_id": bill, "line": 1}, "life_months": 36}
	p := e.call(http.MethodPost, "/v1/fixed-assets", accountant, body, "Prefer", "return=preview")
	if p.Status != http.StatusOK || p.Header.Get("Preference-Applied") != "return=preview" || p.str("cost", "amount") != "3600.00" {
		t.Fatalf("preview = %d: %s", p.Status, p.Raw)
	}
	r := e.call(http.MethodPost, "/v1/fixed-assets", accountant, body)
	if r.Status != http.StatusCreated {
		t.Fatalf("registerFixedAsset = %d: %s", r.Status, r.Raw)
	}
	if r.str("number") != "FA-0001" || r.str("name") != "MacBook Pro 14" || r.str("cost", "amount") != "3600.00" || r.str("start") != "2026-10" ||
		r.str("end") != "2029-09" || r.str("status") != "in_use" || r.str("net_book_value", "amount") != "3600.00" || numberAt(r, "source", "line") != 1 ||
		r.str("account_id") != e.computerEquipment().ID.String() || r.str("purchased_on") != "2026-10-14" {
		t.Errorf("registered: %s", r.Raw)
	}
	id, tag := r.str("id"), r.Header.Get("ETag")
	// Its schedule: 36 months of S$100.00 from October 2026, each on its last day, all in the future on 15 October.
	months, _ := r.Body["depreciation"].([]any)
	if len(months) != 36 {
		t.Fatalf("the schedule: %d months", len(months))
	}
	if m := months[0].(map[string]any); m["month"] != "2026-10" || m["date"] != "2026-10-31" || m["status"] != "projected" ||
		m["amount"].(map[string]any)["amount"] != "100.00" {
		t.Errorf("October: %v", m)
	}
	// On 5 November, October is due: it posted when the job ran, or waits for it.
	e.clock.Set(time.Date(2026, 11, 5, 4, 0, 0, 0, time.UTC))
	if _, err := journal.DepreciateDueAll(context.Background(), e.store, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	g := e.call(http.MethodGet, "/v1/fixed-assets/"+id, accountant, nil)
	if m := g.Body["depreciation"].([]any)[0].(map[string]any); m["status"] != "posted" || m["entry_id"] == nil ||
		g.str("accumulated_depreciation", "amount") != "100.00" || g.str("net_book_value", "amount") != "3500.00" || g.str("depreciated_to") != "2026-10" {
		t.Errorf("after October posted: %s", g.Raw)
	}
	e.clock.Set(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))

	// A line is registered once; a line on an expense account is not an asset's.
	e.problemIs(e.call(http.MethodPost, "/v1/fixed-assets", accountant, body), "registerFixedAsset", 422, asset.CodeSource)
	rent := map[string]any{"source": map[string]any{"document_id": bill, "line": 2}, "life_months": 36}
	e.problemIs(e.call(http.MethodPost, "/v1/fixed-assets", accountant, rent), "registerFixedAsset", 422, asset.CodeSource)
	// Every problem of the asset at once: a life of zero is refused by the contract, a residual above the cost and a
	// start before the purchase by the rules.
	other := e.laptopBill("1200.00", "SGD", "")
	bad := e.call(http.MethodPost, "/v1/fixed-assets", accountant, map[string]any{"source": map[string]any{"document_id": other, "line": 1},
		"life_months": 24, "residual": sgd("1500.00"), "start": "2026-09"})
	if bad.Status != 422 || !hasError(bad, asset.CodeResidual) || !hasError(bad, asset.CodeStart) {
		t.Errorf("a residual above the cost and an early start: %d %s", bad.Status, bad.Raw)
	}

	// From a bill in US dollars: its base at the bill's rate, under RND-FX.
	usd := e.laptopBill("1000.00", "USD", "1.34565")
	f := e.call(http.MethodPost, "/v1/fixed-assets", accountant, map[string]any{"source": map[string]any{"document_id": usd, "line": 1}, "life_months": 36})
	if f.Status != http.StatusCreated || f.str("cost", "amount") != "1345.65" || f.str("cost", "currency") != "SGD" {
		t.Errorf("from a USD bill: %d %s", f.Status, f.Raw)
	}

	// Bought before the books start: its opening figures, depreciating from the books start's month.
	e.booksFrom()
	opening := map[string]any{"opening": map[string]any{"account_id": e.computerEquipment().ID.String(), "purchased_on": "2025-10-03",
		"cost": sgd("3600.00"), "accumulated_depreciation": sgd("1200.00")}, "name": "Dell monitor", "life_months": 36}
	o := e.call(http.MethodPost, "/v1/fixed-assets", accountant, opening)
	if o.Status != http.StatusCreated || o.str("accumulated_depreciation", "amount") != "1200.00" || o.str("net_book_value", "amount") != "2400.00" ||
		o.str("opening_accumulated", "amount") != "1200.00" || o.str("start") != "2025-10" || o.Body["source"] != nil {
		t.Errorf("an asset bought before the books start: %d %s", o.Status, o.Raw)
	}
	late := map[string]any{"opening": map[string]any{"account_id": e.computerEquipment().ID.String(), "purchased_on": "2026-10-03",
		"cost": sgd("3600.00"), "accumulated_depreciation": sgd("0.00")}, "name": "Dell monitor", "life_months": 36}
	e.problemIs(e.call(http.MethodPost, "/v1/fixed-assets", accountant, late), "registerFixedAsset", 422, asset.CodeOpening)

	// A change is the next version, at the asset's ETag.
	path := "/v1/fixed-assets/" + id + "/change"
	e.problemIs(e.call(http.MethodPost, path, accountant, map[string]any{"life_months": 48}), "changeFixedAsset", 428, CodeIfMatchRequired)
	e.problemIs(e.call(http.MethodPost, path, accountant, map[string]any{"life_months": 48}, "If-Match", `"9-posted"`), "changeFixedAsset", 412, CodeStale)
	e.problemIs(e.call(http.MethodPost, path, accountant, map[string]any{"cost": sgd("3000.00")}, "If-Match", tag), "changeFixedAsset", 422, asset.CodeSource)
	c := e.call(http.MethodPost, path, accountant, map[string]any{"life_months": 48, "name": "MacBook Pro 14, Arjun"}, "If-Match", tag)
	if c.Status != http.StatusOK || numberAt(c, "version") != 2 || numberAt(c, "life_months") != 48 || c.str("end") != "2030-09" || c.str("name") != "MacBook Pro 14, Arjun" {
		t.Errorf("changeFixedAsset = %d: %s", c.Status, c.Raw)
	}

	// The register: newest purchase first, narrowed by account; a Bookkeeper reads it and registers nothing.
	bookkeeper := e.as(authz.Bookkeeper)
	l := e.call(http.MethodGet, "/v1/fixed-assets?account_id="+e.computerEquipment().ID.String(), bookkeeper, nil)
	if items, _ := l.Body["items"].([]any); l.Status != 200 || len(items) != 3 {
		t.Errorf("listFixedAssets = %d: %s", l.Status, l.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/fixed-assets/"+id, bookkeeper, nil); g.Status != 200 || g.Header.Get("ETag") != `"2-posted"` {
		t.Errorf("getFixedAsset = %d: %s", g.Status, g.Raw)
	}
	third := e.laptopBill("2000.00", "SGD", "")
	if b := e.call(http.MethodPost, "/v1/fixed-assets", bookkeeper, map[string]any{"source": map[string]any{"document_id": third, "line": 1},
		"life_months": 36}); b.Status != http.StatusForbidden {
		t.Errorf("a Bookkeeper registered: %d %s", b.Status, b.Raw)
	}

	// Voiding the bill leaves the asset with a warning: its cost may no longer be on the account.
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if _, err := (journal.Bills{Store: e.store}).Void(ctx, e.books.Org, uuid.MustParse(bill), time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
		"Wrong supplier"); err != nil {
		t.Fatalf("void the bill: %v", err)
	}
	if w := e.call(http.MethodGet, "/v1/fixed-assets/"+id, accountant, nil); !hasCheck(w, WarnAssetSource) {
		t.Errorf("an asset whose bill was voided: %s", w.Raw)
	}

	// Registering posts nothing, so the ledger replays as it was (ENG-06).
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) != 0 {
			t.Errorf("replay: %+v", r.Mismatches)
		}
		return err
	})
}

// The capitalisation prompt (FR-FA-01; design D5): a posted bill's line on an asset account at or above the threshold
// (S$1,000.00) and in no asset shows ⚠ ASSET-UNREGISTERED, and the register lists it as not in the register yet; a line
// below the threshold is listed, not prompted. Registered, it leaves both.
// req: FR-FA-01
// req: ledger/fixed-assets Capitalisation threshold prompt
func TestPurchasesNotInTheRegisterYet(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	laptop, monitor := e.laptopBill("1200.00", "SGD", ""), e.laptopBill("900.00", "SGD", "")
	prompted := func(bill string) bool {
		return hasCheck(e.call(http.MethodGet, "/v1/bills/"+bill, accountant, nil), journal.WarnUnregistered)
	}
	if !prompted(laptop) || prompted(monitor) {
		t.Errorf("the prompt: the S$1,200 laptop %v, the S$900 monitor %v", prompted(laptop), prompted(monitor))
	}
	l := e.call(http.MethodGet, "/v1/fixed-assets/unregistered", e.as(authz.Bookkeeper), nil)
	items, _ := l.Body["items"].([]any)
	if l.Status != 200 || len(items) != 2 {
		t.Fatalf("listUnregisteredAssetLines = %d: %s", l.Status, l.Raw)
	}
	at := map[string]bool{}
	for _, x := range items {
		m := x.(map[string]any)
		at[m["cost"].(map[string]any)["amount"].(string)] = m["at_threshold"].(bool)
		if m["line"] != float64(1) || m["type"] != "bill" || m["account_id"] != e.computerEquipment().ID.String() {
			t.Errorf("a line not in the register: %v", m)
		}
	}
	if !at["1200.00"] || at["900.00"] {
		t.Errorf("at the threshold: %v", at)
	}
	// A page at a time.
	first := e.call(http.MethodGet, "/v1/fixed-assets/unregistered?page_size=1", accountant, nil)
	if items, _ := first.Body["items"].([]any); len(items) != 1 || first.str("next_cursor") == "" {
		t.Fatalf("the first page: %s", first.Raw)
	}
	second := e.call(http.MethodGet, "/v1/fixed-assets/unregistered?page_size=1&cursor="+first.str("next_cursor"), accountant, nil)
	if items, _ := second.Body["items"].([]any); len(items) != 1 || second.str("next_cursor") != "" ||
		items[0].(map[string]any)["document_id"] == first.Body["items"].([]any)[0].(map[string]any)["document_id"] {
		t.Errorf("the second page: %s", second.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/fixed-assets", accountant, map[string]any{"source": map[string]any{"document_id": laptop, "line": 1},
		"life_months": 36}); r.Status != http.StatusCreated {
		t.Fatalf("registerFixedAsset = %d: %s", r.Status, r.Raw)
	}
	if prompted(laptop) {
		t.Errorf("a registered line is still prompted")
	}
	one := e.call(http.MethodGet, "/v1/fixed-assets/unregistered?document_id="+monitor, accountant, nil)
	if items, _ := one.Body["items"].([]any); len(items) != 1 {
		t.Errorf("the monitor's lines: %s", one.Raw)
	}
	if none := e.call(http.MethodGet, "/v1/fixed-assets/unregistered?document_id="+laptop, accountant, nil); len(none.Body["items"].([]any)) != 0 {
		t.Errorf("the laptop's lines: %s", none.Raw)
	}
}

// matrixAssets are the authorisation matrix's rows for the fixed asset register.
var matrixAssets = map[string]func(e *env, tok string) (response, func() bool){
	// A void in Founder mode is asked for, so the row voids in an organisation in Standard mode (rev 0.151).
	"voidFixedAsset": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.registered("1600.00")
		r := s.call(http.MethodPost, "/v1/fixed-assets/"+id+"/void", tok, map[string]any{"reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/fixed-assets/"+id, s.as(authz.Accountant), nil).str("status") == "in_use"
		}
	},
	"requestFixedAssetVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, approval.ObjectFixedAsset)
		id, tag := e.registered("1600.00")
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/fixed-assets/"+id+"/void-approval", person, map[string]any{"reason": "Matrix test"}, "If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, approval.ObjectFixedAsset) == was }
	},
	"listUnregisteredAssetLines": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/fixed-assets/unregistered", tok, nil), nil
	},
	"listFixedAssets": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/fixed-assets", tok, nil), nil
	},
	"getFixedAsset": func(e *env, tok string) (response, func() bool) {
		id, _ := e.registered("1500.00")
		return e.call(http.MethodGet, "/v1/fixed-assets/"+id, tok, nil), nil
	},
	"registerFixedAsset": func(e *env, tok string) (response, func() bool) {
		bill := e.laptopBill("1600.00", "SGD", "")
		was := len(e.call(http.MethodGet, "/v1/fixed-assets", e.as(authz.Owner), nil).Body["items"].([]any))
		r := e.call(http.MethodPost, "/v1/fixed-assets", tok, map[string]any{"source": map[string]any{"document_id": bill, "line": 1}, "life_months": 36})
		return r, func() bool {
			return len(e.call(http.MethodGet, "/v1/fixed-assets", e.as(authz.Owner), nil).Body["items"].([]any)) == was
		}
	},
	"changeFixedAsset": func(e *env, tok string) (response, func() bool) {
		id, tag := e.registered("1700.00")
		r := e.call(http.MethodPost, "/v1/fixed-assets/"+id+"/change", tok, map[string]any{"life_months": 48}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/fixed-assets/"+id, e.as(authz.Owner), nil).Body["version"] == float64(1)
		}
	},
}

// hasError says whether a problem lists an error with code.
func hasError(r response, code string) bool {
	errs, _ := r.Body["errors"].([]any)
	for _, x := range errs {
		if m, ok := x.(map[string]any); ok && m["code"] == code {
			return true
		}
	}
	return false
}

// numberAt is the number at path in a response's body, -1 when there is none.
func numberAt(r response, path ...string) float64 {
	var v any = r.Body
	for _, p := range path {
		m, _ := v.(map[string]any)
		v = m[p]
	}
	if n, ok := v.(float64); ok {
		return n
	}
	return -1
}
