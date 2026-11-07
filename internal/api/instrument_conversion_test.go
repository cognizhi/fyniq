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
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A SAFE converting at the priced round (FR-EQ-18, design D6, G4-044), worked by hand. The founder holds 1,000,000
// shares from before the books start. G4-044's post-money SAFE, S$125,000.00 with a S$2,500,000 cap, is recorded, its
// money in on 2700 on 1 Oct, and the YC template is enabled. The round, 200,000 shares at S$3.00 for S$600,000.00, is
// approved; its conversions draft one transaction: 52,631 shares at 2,500,000 × (1 − 125,000 ÷ 2,500,000) ÷ 1,000,000 =
// S$2.375, S$124,998.63 (52,631 × 2.375 = 124,998.625, RND-SHARE), nothing for the accountant to complete. Asking again
// drafts nothing; a second conversion of the SAFE is refused. Approved, it is never effective before the round. Once
// both are on 15 Oct, it posts Dr 2700 124,998.63 naming the SAFE · Cr 3010, 2700 holds S$1.37 for it, and the cap
// table reads 79.83% founder, 15.97% investor, 4.20% the SAFE holder (G.3).
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
	e.problemIs(conversions(weiling, approved), "draftInstrumentConversions", 403, CodeForbidden) // the Accountant drafts
	e.problemIs(conversions(prep, round.Header.Get("ETag")), "draftInstrumentConversions", 412, CodeStale)   // the round as drafted
	e.problemIs(e.call(http.MethodPost, rpath+"/conversions", prep, nil), "draftInstrumentConversions", 428, CodeIfMatchRequired)

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
	if got := e.call(http.MethodGet, "/v1/instruments/"+safe.str("id"), weiling, nil); got.str("held", "amount") != "1.37" {
		t.Errorf("2700 holds for the SAFE once converted: %s", got.Raw)
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
