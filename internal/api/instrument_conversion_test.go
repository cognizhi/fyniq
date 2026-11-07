//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/director"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A SAFE converting at the priced round (FR-EQ-18, design D6, G4-044), worked by hand. The founder holds 1,000,000
// shares from before the books start. G4-044's post-money SAFE, S$125,000.00 with a S$2,500,000 cap, is recorded, its
// money in on 2700 on 1 Oct, and the YC template is enabled. The round, 200,000 shares at S$3.00 for S$600,000.00, is
// approved; its conversions draft one transaction: 52,631 shares at 2,500,000 × (1 − 125,000 ÷ 2,500,000) ÷ 1,000,000 =
// S$2.375, S$124,998.63 (52,631 × 2.375 = 124,998.625, RND-SHARE), nothing for the accountant to complete. Asking again
// drafts nothing; a second conversion of the SAFE is refused. Approved, it is never effective before the round. Once
// both are on 15 Oct, it posts Dr 2700 124,998.63 naming the SAFE · Cr 3010, 2700 holds S$1.37 for it, and the cap
// table reads 79.83% founder, 15.97% investor, 4.20% the SAFE holder (G.3). Before it drafts, the preview shows the
// working: f = 5.00%, a capitalisation of 1,052,631.58 and the cap price 2.375; at a price typed in of S$2.00 the SAFE
// takes the round's price, 62,500 shares, for the accountant to check.
// req: FR-EQ-18
// req: RND-SHARE
// req: FR-EQ-20
func TestASAFEConvertsAtTheRound(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	kow := c.as(c.ahkow, authz.Owner, authz.Controller)
	mei := c.as(c.mei, authz.Owner)
	approve := func(req response, token string) response {
		return e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", token, hashOf(req), "If-Match", `"1"`)
	}
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	var holding, monies string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		if _, err = u.UpdateOrganisation(ctx, o.Version, pr); err != nil {
			return err
		}
		h, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2700", Name: "Convertible notes / SAFE", Type: storage.Liability, Role: "convertible-instruments"})
		if err != nil {
			return err
		}
		m, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2040", Name: "Share application monies", Type: storage.Liability, Role: "share-application-monies"})
		holding, monies = h.ID.String(), m.ID.String()
		return err
	})
	ordinary := e.shareCapitalIn(e.books.Org)
	founder := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Tan Wei Ling", "shareholder": true}).str("id")
	fund := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Seedling Ventures Pte. Ltd.", "investor": true}).str("id")
	hatch := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Hatch Capital Pte. Ltd.", "investor": true}).str("id")
	if r := e.call(http.MethodPost, "/v1/share-issues", weiling, map[string]any{"holder_id": founder, "class_account_id": ordinary, "shares": "1000000",
		"price": "0.01", "consideration": "cash", "resolution_refs": "Allotment resolution", "effective_on": "2025-01-02"}); r.Status != http.StatusCreated {
		t.Fatalf("the founder's shares: %d %s", r.Status, r.Raw)
	}

	// attach and ask: a file, then the approval request, approved by Mei.
	asked := func(path string, d response, evidence string) {
		t.Helper()
		if f := e.upload(path+"/evidence", prep, map[string]string{"evidence_type": evidence}, "evidence.pdf", []byte("%PDF-1.7 "+evidence),
			"If-Match", d.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("attaching to %s: %d %s", path, f.Status, f.Raw)
		}
		req := e.call(http.MethodPost, path+"/approval", prep, nil, "If-Match", d.Header.Get("ETag"))
		if a := approve(req, mei); req.Status != http.StatusAccepted || a.Status != http.StatusOK {
			t.Fatalf("approving %s: %d %s; %d %s", path, req.Status, req.Raw, a.Status, a.Raw)
		}
	}
	safe := e.call(http.MethodPost, "/v1/instruments", prep, map[string]any{"form": "post_money_safe", "holder_id": hatch, "amount": "125000.00",
		"signed_on": "2026-06-18", "valuation_cap": "2500000", "trigger": "priced_round", "class_account_id": ordinary,
		"denominator": "post_money_capitalisation", "price_decimals": 4, "template": "yc-sg-post-money-cap@1"})
	if safe.Status != http.StatusCreated {
		t.Fatalf("drafting the SAFE: %d %s", safe.Status, safe.Raw)
	}
	asked("/v1/instruments/"+safe.str("id"), safe, "Signed SAFE")
	en := e.call(http.MethodPost, "/v1/instrument-templates/yc-sg-post-money-cap@1/enablement", prep, map[string]any{})
	asked("/v1/instrument-template-enablements/"+en.str("enablement_id"), en, "Lawyer's confirmation")

	// The money in: the SAFE's on 2700 and the investor's in share application monies, each above the self-approval
	// threshold, so with a bank statement and Ah Kow's approval.
	money := func(memo string, credit map[string]any, amount string) {
		t.Helper()
		j := e.call(http.MethodPost, "/v1/journals", prep, map[string]any{"date": "2026-10-01", "memo": memo,
			"lines": []map[string]any{founderLine(e.books.Bank.ID.String(), amount, "", ""), credit}})
		if j.Status != http.StatusCreated {
			t.Fatalf("%s: %d %s", memo, j.Status, j.Raw)
		}
		if f := e.upload("/v1/journals/"+j.str("id")+"/evidence", prep, map[string]string{"evidence_type": "Bank statement"}, "statement.pdf",
			[]byte("%PDF-1.7 statement"), "If-Match", j.Header.Get("ETag")); f.Status != http.StatusCreated {
			t.Fatalf("its bank statement: %d %s", f.Status, f.Raw)
		}
		req := e.call(http.MethodPost, "/v1/journals/"+j.str("id")+"/post-approval", prep, nil, "If-Match", j.Header.Get("ETag"))
		if r := approve(req, kow); req.Status != http.StatusAccepted || r.Status != http.StatusOK {
			t.Fatalf("posting %s: %d; %d %s", memo, req.Status, r.Status, r.Raw)
		}
	}
	safeMoney := founderLine(holding, "-125000.00", "", "")
	safeMoney["instrument_id"] = safe.str("id")
	money("Hatch Capital SAFE", safeMoney, "125000.00")
	money("Seedling Ventures subscription", founderLine(monies, "-600000.00", fund, ""), "600000.00")

	// The round, approved.
	round := e.call(http.MethodPost, "/v1/equity-transactions", prep, map[string]any{"kind": "issue_cash", "holder_id": fund, "class_account_id": ordinary,
		"shares": "200000", "price": "3.00", "resolution_refs": "Board resolution 15 Oct 2026; shareholders' resolution 15 Oct 2026"})
	if round.Status != http.StatusCreated {
		t.Fatalf("drafting the round: %d %s", round.Status, round.Raw)
	}
	rpath := "/v1/equity-transactions/" + round.str("id")
	conversions := func(token, etag string) response {
		return e.call(http.MethodPost, rpath+"/conversions", token, nil, "If-Match", etag)
	}
	e.problemIs(conversions(prep, round.Header.Get("ETag")), "draftInstrumentConversions", 422, equity.CodeTransaction) // a draft round
	asked(rpath, round, "Board resolution")
	approved := e.call(http.MethodGet, rpath, prep, nil).Header.Get("ETag")
	e.problemIs(conversions(weiling, approved), "draftInstrumentConversions", 403, CodeForbidden)          // the Accountant drafts
	e.problemIs(conversions(prep, round.Header.Get("ETag")), "draftInstrumentConversions", 412, CodeStale) // the round as drafted
	e.problemIs(e.call(http.MethodPost, rpath+"/conversions", prep, nil), "draftInstrumentConversions", 428, CodeIfMatchRequired)

	// The preview at the round, a sandbox: C₀ = 1,000,000, f = 5.00%, the capitalisation 1,000,000 ÷ 0.95 =
	// 1,052,631.578… shown as 1,052,631.58, and the working of G4-044's price; at a price typed in of S$2.00 the round's
	// price is below the cap price, so the SAFE takes it and the accountant checks it. The Board reads none of it.
	preview := e.call(http.MethodGet, "/v1/instruments/conversion-preview?round_id="+round.str("id"), weiling, nil)
	if preview.Status != http.StatusOK || preview.str("shares_before") != "1000000" || preview.str("post_money_part") != "5.00" ||
		preview.str("capitalisation") != "1052631.58" || preview.str("round_price") != "3.00" || preview.str("round_id") != round.str("id") {
		t.Fatalf("the preview at the round: %d %s", preview.Status, preview.Raw)
	}
	w := preview.Body["conversions"].([]any)[0].(map[string]any)
	if w["holder_name"] != "Hatch Capital Pte. Ltd." || w["ownership_at_cap"] != "5.00" || w["cap_price"] != "2.375" || w["price"] != "2.375" ||
		w["shares"] != "52631" || w["converted"].(map[string]any)["amount"] != "124998.63" || w["left"].(map[string]any)["amount"] != "1.37" ||
		w["covered"] != true || w["review"] != nil || w["price_decimals"].(float64) != 4 {
		t.Errorf("G4-044's working: %v", w)
	}
	low := e.call(http.MethodGet, "/v1/instruments/conversion-preview?price=2.00", prep, nil)
	if lw := low.Body["conversions"].([]any)[0].(map[string]any); low.Status != http.StatusOK || low.Body["round_id"] != nil || lw["price"] != "2.00" ||
		lw["shares"] != "62500" || lw["covered"] != false || !strings.Contains(lw["review"].(string), "below its cap price") {
		t.Errorf("the preview at S$2.00: %d %s", low.Status, low.Raw)
	}
	for _, q := range []string{"", "?price=0", "?price=2.00&round_id=" + round.str("id"), "?price=1.1234567"} {
		if r := e.call(http.MethodGet, "/v1/instruments/conversion-preview"+q, prep, nil); r.Status != http.StatusUnprocessableEntity {
			t.Errorf("a preview %q: %d %s", q, r.Status, r.Raw)
		}
	}
	e.problemIs(e.call(http.MethodGet, "/v1/instruments/conversion-preview?price=3.00", e.as(authz.BoardViewer), nil), "previewInstrumentConversions", 403, CodeForbidden)

	// Its conversions: one draft, from the template, nothing to complete.
	drafts := conversions(prep, approved)
	if drafts.Status != http.StatusCreated || len(drafts.Body["items"].([]any)) != 1 {
		t.Fatalf("the conversions: %d %s", drafts.Status, drafts.Raw)
	}
	conv := drafts.Body["items"].([]any)[0].(map[string]any)
	d := conv["details"].(map[string]any)
	if conv["status"] != "draft" || d["kind"] != "instrument_conversion" || d["instrument_id"] != safe.str("id") || d["round_id"] != round.str("id") ||
		d["holder_id"] != hatch || d["class_account_id"] != ordinary || d["shares"] != "52631" || d["price"] != "2.375" || d["review"] != nil ||
		conv["amount"].(map[string]any)["amount"] != "124998.63" {
		t.Fatalf("G4-044's conversion: %v", conv)
	}
	if again := conversions(prep, approved); again.Status != http.StatusCreated || len(again.Body["items"].([]any)) != 0 {
		t.Errorf("asking again: %d %s", again.Status, again.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/equity-transactions", prep, map[string]any{"kind": "instrument_conversion", "instrument_id": safe.str("id"),
		"round_id": round.str("id"), "shares": "52631", "price": "2.375", "resolution_refs": "Board resolution"}); r.Status != http.StatusUnprocessableEntity ||
		!strings.Contains(r.Raw, "a conversion of the instrument stands already") {
		t.Errorf("a second conversion of the SAFE: %d %s", r.Status, r.Raw)
	}
	cpath := "/v1/equity-transactions/" + conv["id"].(string)
	cd := e.call(http.MethodGet, cpath, prep, nil)
	asked(cpath, cd, "Board resolution")
	effective := func(path string, on string) response {
		cur := e.call(http.MethodGet, path, weiling, nil)
		return e.call(http.MethodPost, path+"/effective", weiling, map[string]any{"effective_on": on, "lodgement_ref": "AA2026101500456"}, "If-Match", cur.Header.Get("ETag"))
	}
	e.problemIs(effective(cpath, "2026-10-15"), "recordEquityTransactionEffective", 409, equity.CodeEffective) // the round is not effective yet
	if r := effective(rpath, "2026-10-15"); r.Status != http.StatusOK {
		t.Fatalf("the round effective: %d %s\n%s", r.Status, r.Raw, e.log)
	}
	done := effective(cpath, "2026-10-15")
	if done.Status != http.StatusOK || done.str("status") != "effective" || done.str("share_issue_id") == "" || len(done.Body["entries"].([]any)) != 1 ||
		len(done.Body["movements"].([]any)) != 1 {
		t.Fatalf("the conversion effective: %d %s", done.Status, done.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		entry, err := u.Ledger().Entry(ctx, uuid.MustParse(done.Body["entries"].([]any)[0].(map[string]any)["id"].(string)))
		if err != nil {
			return err
		}
		lines := map[string]string{}
		for _, l := range entry.Lines {
			instrument := ""
			if l.InstrumentID != nil {
				instrument = " " + l.InstrumentID.String()
			}
			lines[l.AccountID.String()] = string(l.BaseAmount) + instrument
		}
		if entry.RuleVersion != "instrument_conversion@1" || entry.Source.Type != "share_issue" || entry.PostingDate.Format(time.DateOnly) != "2026-10-15" ||
			lines[holding] != "124998.6300 "+safe.str("id") || lines[ordinary] != "-124998.6300" {
			t.Errorf("the conversion's entry: %s %s %v", entry.RuleVersion, entry.PostingDate, lines)
		}
		return nil
	})
	if got := e.call(http.MethodGet, "/v1/instruments/"+safe.str("id"), weiling, nil); got.str("held", "amount") != "1.37" ||
		got.str("conversion", "status") != "effective" || got.str("conversion", "transaction_id") != conv["id"] {
		t.Errorf("2700 holds for the SAFE once converted, and its conversion: %s", got.Raw)
	}
	if after := e.call(http.MethodGet, "/v1/instruments/conversion-preview?price=3.00", prep, nil); len(after.Body["conversions"].([]any)) != 0 ||
		after.Body["post_money_part"] != nil {
		t.Errorf("the preview once the SAFE converted: %s", after.Raw)
	}
	if c := e.call(http.MethodGet, "/v1/contacts/"+hatch, weiling, nil); c.Body["shareholder"] != true {
		t.Errorf("the SAFE holder is not marked shareholder: %s", c.Raw)
	}
	table := e.call(http.MethodGet, "/v1/cap-table?at=2026-10-15", weiling, nil)
	want := "Hatch Capital Pte. Ltd. 3010 52631 4.20 4.20|Seedling Ventures Pte. Ltd. 3010 200000 15.97 15.97|Tan Wei Ling 3010 1000000 79.83 79.83"
	if got := strings.Join(capLines(t, table.Body), "|"); got != want || table.str("issued") != "1252631" {
		t.Errorf("the cap table:\n%s\nwant %s (issued %s)", got, want, table.str("issued"))
	}
	if r := e.call(http.MethodGet, "/v1/equity-transactions?kind=instrument_conversion", e.as(authz.Auditor), nil); r.Status != http.StatusOK || len(r.Body["items"].([]any)) != 1 {
		t.Errorf("the conversions, as the Auditor reads them: %d %s", r.Status, r.Raw)
	}
}

// C.2 #29 (FR-EQ-18): a recorded SAFE or note no conversion stands for is ⚠ once its maturity has passed, or once shares
// were allotted for cash on or after the day it was signed. Today is 15 Oct 2026. The founder's 1,000,000 shares of
// 2 Jan 2025 came before the SAFE signed on 1 Mar 2025, so only the note, which matured on 30 Sep 2026, is for review;
// shares allotted for cash on 1 Jun 2025 put the SAFE up too; a conversion drafted for it takes it off again.
// req: C.2 #29
// req: FR-EQ-18
func TestTheSAFEChecks(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
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
	ordinary := e.shareCapitalIn(e.books.Org)
	founder := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Tan Wei Ling", "shareholder": true}).str("id")
	hatch := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Hatch Capital Pte. Ltd.", "investor": true}).str("id")
	issue := func(holder, shares, on string) {
		t.Helper()
		if r := e.call(http.MethodPost, "/v1/share-issues", weiling, map[string]any{"holder_id": holder, "class_account_id": ordinary, "shares": shares,
			"price": "0.01", "consideration": "cash", "resolution_refs": "Allotment resolution", "effective_on": on}); r.Status != http.StatusCreated {
			t.Fatalf("issuing %s on %s: %d %s", shares, on, r.Status, r.Raw)
		}
	}
	issue(founder, "1000000", "2025-01-02")
	record := func(body map[string]any) string {
		t.Helper()
		body["holder_id"], body["class_account_id"], body["price_decimals"] = hatch, ordinary, 4
		r := e.call(http.MethodPost, "/v1/instruments", prep, body)
		if r.Status != http.StatusCreated {
			t.Fatalf("drafting %v: %d %s", body["form"], r.Status, r.Raw)
		}
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Documents().SetStatus(ctx, uuid.MustParse(r.str("id")), storage.DocumentPosted)
		})
		return r.str("id")
	}
	wantStatus(t, e.checked("equity.instruments-converted"), "ok")
	safe := record(map[string]any{"form": "post_money_safe", "amount": "125000.00", "signed_on": "2025-03-01", "valuation_cap": "2500000",
		"trigger": "priced_round", "denominator": "post_money_capitalisation"})
	note := record(map[string]any{"form": "convertible_note", "amount": "20000.00", "signed_on": "2025-03-01", "interest_rate": "8",
		"maturity_on": "2026-09-30", "trigger": "maturity", "denominator": "pre_money_shares"})
	r := e.checked("equity.instruments-converted")
	wantStatus(t, r, "warning")
	if it := itemNamed(r, note+"/maturity"); it == nil || it["label"] != "Hatch Capital Pte. Ltd., convertible note" || !strings.Contains(it["detail"].(string), "Past its maturity on 30 Sep 2026") ||
		len(itemsOf(r)) != 1 {
		t.Errorf("C.2 #29 for the matured note: %s", r.Raw)
	}
	issue(founder, "200000", "2025-06-01")
	r = e.checked("equity.instruments-converted")
	if it := itemNamed(r, safe+"/round"); it == nil || !strings.Contains(it["detail"].(string), "Shares were allotted for cash on 1 Jun 2025, after it was signed on 1 Mar 2025") ||
		len(itemsOf(r)) != 2 {
		t.Errorf("C.2 #29 for the SAFE a round did not convert: %s", r.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error { // its conversion drafted
		_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentEquityTransaction,
			Payload: []byte(`{"kind":"instrument_conversion","instrument_id":"` + safe + `"}`)})
		return err
	})
	if r := e.checked("equity.instruments-converted"); itemNamed(r, safe+"/round") != nil || len(itemsOf(r)) != 1 {
		t.Errorf("C.2 #29 once the SAFE's conversion is drafted: %s", r.Raw)
	}
}

// A founder's balance turned into a SAFE they hold (FR-EQ-05, FR-EQ-18), worked by hand: Ah Kow lent S$1,000.00 on 5 Oct
// 2026 and holds a recorded SAFE of S$1,000.00. S$600.00 of his loan becomes its money: the request shows Dr 2500 · Cr
// 2700; approved by Mei, it posts on 15 Oct naming him on 2500 and the SAFE on 2700, his statement says "safe", and 2700
// holds S$600.00 for it. A SAFE someone else holds, one not recorded, or more than its amount still to pay, is refused.
// req: FR-EQ-05
// req: FR-EQ-18
func TestAFoundersBalanceBecomesASAFE(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	loan, _ := e.directorChart()
	var holding string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := u.Accounts().Create(ctx, storage.NewAccount{Code: "2700", Name: "Convertible notes / SAFE", Type: storage.Liability, Role: "convertible-instruments"})
		holding = a.ID.String()
		return err
	})
	kow := e.directorContact("Ah Kow")
	if r := e.postFounderJournal("2026-10-05", founderLine(e.books.Bank.ID.String(), "1000.00", "", ""), founderLine(loan, "-1000.00", kow, ledger.OriginLent)); r.Status != http.StatusOK {
		t.Fatalf("Ah Kow lending: %d %s", r.Status, r.Raw)
	}
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	prep := c.as(c.weiling, authz.Accountant)
	ordinary := e.shareCapitalIn(e.books.Org)
	fund := e.call(http.MethodPost, "/v1/contacts", weiling, map[string]any{"legal_name": "Lion Rock Ventures Pte. Ltd.", "investor": true}).str("id")
	instrument := func(holder string, recorded bool) string {
		t.Helper()
		r := e.call(http.MethodPost, "/v1/instruments", prep, map[string]any{"form": "post_money_safe", "holder_id": holder, "amount": "1000.00", "signed_on": "2026-10-10",
			"valuation_cap": "2500000", "trigger": "priced_round", "class_account_id": ordinary, "denominator": "post_money_capitalisation", "price_decimals": 4})
		if r.Status != http.StatusCreated {
			t.Fatalf("drafting a SAFE: %d %s", r.Status, r.Raw)
		}
		if recorded {
			e.do(func(ctx context.Context, u storage.UnitOfWork) error {
				return u.Documents().SetStatus(ctx, uuid.MustParse(r.str("id")), storage.DocumentPosted)
			})
		}
		return r.str("id")
	}
	his, theirs, drafted := instrument(kow, true), instrument(fund, true), instrument(kow, false)
	sgd := func(a string) map[string]any { return map[string]any{"amount": a, "currency": "SGD"} }
	draft := func(amount, safe string) response {
		body := map[string]any{"contact_id": kow, "account_id": loan, "amount": sgd(amount), "route": "safe"}
		if safe != "" {
			body["safe"] = map[string]any{"instrument_id": safe}
		}
		return e.call(http.MethodPost, "/v1/director-settlements", weiling, body)
	}
	for name, x := range map[string]response{"no SAFE": draft("600.00", ""), "someone else's SAFE": draft("600.00", theirs),
		"a SAFE not recorded": draft("600.00", drafted), "more than its amount": draft("1000.01", his)} {
		if x.Status != http.StatusUnprocessableEntity || !strings.Contains(x.Raw, director.CodeSettlement) {
			t.Errorf("a settlement into %s: %d %s", name, x.Status, x.Raw)
		}
	}
	s := draft("600.00", his)
	if s.Status != http.StatusCreated || s.str("route") != director.RouteSAFE || s.str("safe", "instrument_id") != his ||
		!strings.Contains(s.Raw, `"account_id":"`+holding+`","amount":{"amount":"-600.00"`) {
		t.Fatalf("drafting the settlement into his SAFE, with its dry run: %d %s", s.Status, s.Raw)
	}
	path := "/v1/director-settlements/" + s.str("id")
	if f := e.upload(path+"/evidence", weiling, map[string]string{"evidence_type": "Signed SAFE"}, "safe.pdf", []byte("%PDF-1.7 safe"), "If-Match", s.Header.Get("ETag")); f.Status != http.StatusCreated {
		t.Fatalf("attaching the SAFE: %d %s", f.Status, f.Raw)
	}
	req := e.call(http.MethodPost, path+"/approval", weiling, nil, "If-Match", s.Header.Get("ETag"))
	if req.Status != http.StatusAccepted || !strings.Contains(req.Raw, `"debit 2500"`) || !strings.Contains(req.Raw, `"credit 2700"`) {
		t.Fatalf("asking, its legs in the envelope: %d %s", req.Status, req.Raw)
	}
	if a := e.call(http.MethodPost, "/v1/approvals/"+req.str("id")+"/approve", c.as(c.mei, authz.Owner), hashOf(req), "If-Match", `"1"`); a.Status != http.StatusOK {
		t.Fatalf("Mei approving it: %d %s", a.Status, a.Raw)
	}
	done := e.call(http.MethodGet, path, weiling, nil)
	for _, want := range []string{`"status":"decided"`, `"account_id":"` + loan + `","amount":{"amount":"600.00"`, `"party_id":"` + kow + `"`,
		`"account_id":"` + holding + `","amount":{"amount":"-600.00"`} {
		if !strings.Contains(done.Raw, want) {
			t.Errorf("the decided settlement lacks %s: %s", want, done.Raw)
		}
	}
	if got := e.call(http.MethodGet, "/v1/instruments/"+his, weiling, nil); got.str("held", "amount") != "600.00" {
		t.Errorf("2700 holds for his SAFE: %s", got.Raw)
	}
	st := e.call(http.MethodGet, "/v1/director-accounts/"+kow+"/statement?from=2026-10-01&to=2026-10-31", weiling, nil)
	if !strings.Contains(st.Raw, `"origin":"`+ledger.OriginSAFE+`"`) || !strings.Contains(st.Raw, `"closing":{"amount":"-400.00"`) {
		t.Errorf("his statement: %s", st.Raw)
	}
	if again := draft("400.01", his); again.Status != http.StatusUnprocessableEntity || !strings.Contains(again.Raw, "2700 holds 600.00 for it already") {
		t.Errorf("more than the SAFE has still to be paid: %d %s", again.Status, again.Raw)
	}
}
