//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// disposalBooks gives e's organisation, once, what a disposal posts to: 7050 Gain / (loss) on disposal of assets, which
// the setting names by default, and 1160 Asset disposal clearing, the clearing account the setting recommends (rev 0.151); and periods to the end
// of October 2027. It returns 7050 and 1160.
func (e *env) disposalBooks() (gainLoss, clearing storage.Account) {
	e.t.Helper()
	e.computerEquipment()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if gainLoss, err = u.Accounts().ByCode(ctx, "7050"); err == nil {
			clearing, err = u.Accounts().ByCode(ctx, "1160")
			return err
		}
		if gainLoss, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "7050", Name: "Gain / (loss) on disposal of assets", Type: storage.Revenue}); err != nil {
			return err
		}
		if clearing, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1160", Name: "Asset disposal clearing", Type: storage.Asset}); err != nil {
			return err
		}
		for m := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC); !m.After(time.Date(2027, 10, 1, 0, 0, 0, 0, time.UTC)); m = m.AddDate(0, 1, 0) {
			if _, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: m, EndsOn: m.AddDate(0, 1, -1)}); err != nil {
				return err
			}
		}
		return nil
	})
	return gainLoss, clearing
}

// disposalImpact renders a disposal's ledger impact as account ID → base amount.
func disposalImpact(r response) map[string]string {
	out := map[string]string{}
	lines, _ := r.Body["ledger_impact"].([]any)
	for _, l := range lines {
		m := l.(map[string]any)
		out[m["account_id"].(string)] = m["base_amount"].(map[string]any)["amount"].(string)
	}
	return out
}

// FR-FA-03 through the API (p1-fixed-assets tasks 3.1 and 3.2; rev 0.149), worked by hand: S$3,600 laptops bought on 14
// October 2026 over 36 months, S$100 a month, disposed of on 15 October 2027 after 12 months (S$1,200 accumulated):
//   - one sold for S$2,000 into the bank: Dr 1590 1,200; Dr the bank 2,000; Cr 1510 3,600; Dr 7050 400, a loss;
//   - one written off: Dr 1590 1,200; Cr 1510 3,600; Dr 7050 2,400;
//   - half of one sold on an invoice for S$1,500: Dr 1590 600; Dr 1160 1,500; Cr 1510 1,800; Cr 7050 300, a gain; the
//     other half (S$1,800, S$600 depreciated) depreciates on at S$50 a month over its last 24 months;
//   - one sold for S$2,000 on 10 September 2027, recorded on 15 October after September's month posted: September is
//     reversed on its own date, so S$1,100 accumulated through August: a loss of S$500.
//
// The asset sold whole shows disposed; a disposal names the line of the invoice once; in Founder mode a void is asked for.
// req: FR-FA-03
// req: ledger/posting-rules Asset disposal
// req: ledger/fixed-assets Proceeds received or invoiced (rev 0.149)
// req: ledger/fixed-assets Where the gain or loss goes (rev 0.149)
// req: ledger/fixed-assets Partial disposal
// req: FM-1
func TestDisposingOfFixedAssets(t *testing.T) {
	e := newEnv(t)
	accountant := e.as(authz.Accountant)
	gainLoss, clearing := e.disposalBooks()
	bank := e.chartMoneyAccount()
	equipment, accumulated := e.computerEquipment(), e.accountByCode("1590")
	var laptops [4]struct{ id, tag string }
	for i := range laptops {
		laptops[i].id, _ = e.registered("3600.00")
	}
	e.clock.Set(time.Date(2027, 10, 15, 4, 0, 0, 0, time.UTC))
	if _, err := journal.DepreciateDueAll(context.Background(), e.store, e.clock.Now()); err != nil {
		t.Fatal(err)
	}
	for i := range laptops {
		g := e.call(http.MethodGet, "/v1/fixed-assets/"+laptops[i].id, accountant, nil)
		if g.str("accumulated_depreciation", "amount") != "1200.00" || g.str("depreciated_to") != "2027-09" {
			t.Fatalf("laptop %d after 12 months: %s", i+1, g.Raw)
		}
		laptops[i].tag = g.Header.Get("ETag")
	}
	dispose := func(i int, body map[string]any, headers ...string) response {
		return e.call(http.MethodPost, "/v1/fixed-assets/"+laptops[i].id+"/disposals", accountant, body, append([]string{"If-Match", laptops[i].tag}, headers...)...)
	}

	// Sold for S$2,000 into the bank: a loss of S$400.
	sale := map[string]any{"date": "2027-10-15", "proceeds": "money", "money_account_id": bank.ID.String(), "amount": sgd("2000.00"), "notes": "Sold to a staff member"}
	if p := dispose(0, sale, "Prefer", "return=preview"); p.Status != http.StatusOK || p.str("gain_or_loss", "amount") != "-400.00" {
		t.Fatalf("preview = %d: %s", p.Status, p.Raw)
	}
	r := dispose(0, sale)
	if r.Status != http.StatusCreated || r.str("number") != "DSP-0001" || r.str("gain_or_loss", "amount") != "-400.00" || r.str("net_book_value", "amount") != "2400.00" ||
		r.str("accumulated_depreciation", "amount") != "1200.00" || r.str("asset_number") != "FA-0001" || r.Body["whole"] != true {
		t.Fatalf("disposeFixedAsset = %d: %s", r.Status, r.Raw)
	}
	if got := disposalImpact(r); got[accumulated.ID.String()] != "1200.00" || got[bank.ID.String()] != "2000.00" || got[equipment.ID.String()] != "-3600.00" ||
		got[gainLoss.ID.String()] != "400.00" {
		t.Errorf("the sale's entry: %v", got)
	}
	sold := e.call(http.MethodGet, "/v1/fixed-assets/"+laptops[0].id, accountant, nil)
	if sold.str("status") != "disposed" || sold.str("cost", "amount") != "0.00" || numberAt(sold, "version") != 2 {
		t.Errorf("the asset sold: %s", sold.Raw)
	}
	e.problemIs(dispose(0, sale, "If-Match", sold.Header.Get("ETag")), "disposeFixedAsset", 409, asset.CodeDisposed)

	// Written off: a loss of S$2,400.
	w := dispose(1, map[string]any{"date": "2027-10-15", "proceeds": "none"})
	if w.Status != http.StatusCreated || w.str("gain_or_loss", "amount") != "-2400.00" || disposalImpact(w)[gainLoss.ID.String()] != "2400.00" {
		t.Errorf("the write-off: %d %s", w.Status, w.Raw)
	}

	// Half sold on an invoice for S$1,500: a gain of S$300; the rest depreciates on at S$50 a month.
	inv := e.call(http.MethodPost, "/v1/invoices", accountant, map[string]any{"customer_id": e.customer(), "reference": "Laptop", "date": "2027-10-15",
		"currency": "SGD", "lines": []map[string]any{{"account_id": clearing.ID.String(), "description": "Used MacBook Pro 14", "gst_code": "SR",
			"quantity": "1", "unit_price": "1500.00"}}})
	if inv.Status != http.StatusCreated {
		t.Fatalf("createInvoice = %d: %s", inv.Status, inv.Raw)
	}
	if is := e.call(http.MethodPost, "/v1/invoices/"+inv.str("id")+"/issue", accountant, nil, "If-Match", inv.Header.Get("ETag")); is.Status != http.StatusOK {
		t.Fatalf("issueInvoice = %d: %s", is.Status, is.Raw)
	}
	half := map[string]any{"date": "2027-10-15", "cost": sgd("1800.00"), "proceeds": "invoice", "invoice_id": inv.str("id"), "invoice_line": 1}
	h := dispose(2, half)
	if h.Status != http.StatusCreated || h.str("gain_or_loss", "amount") != "300.00" || h.Body["whole"] != false || h.str("invoice_number") == "" {
		t.Fatalf("half on an invoice: %d %s", h.Status, h.Raw)
	}
	if got := disposalImpact(h); got[accumulated.ID.String()] != "600.00" || got[clearing.ID.String()] != "1500.00" || got[equipment.ID.String()] != "-1800.00" ||
		got[gainLoss.ID.String()] != "-300.00" {
		t.Errorf("the invoiced half's entry: %v", got)
	}
	rest := e.call(http.MethodGet, "/v1/fixed-assets/"+laptops[2].id, accountant, nil)
	months, _ := rest.Body["depreciation"].([]any)
	last := months[len(months)-1].(map[string]any)
	if rest.str("status") != "in_use" || rest.str("cost", "amount") != "1800.00" || rest.str("accumulated_depreciation", "amount") != "600.00" ||
		rest.str("net_book_value", "amount") != "1200.00" || last["month"] != "2029-09" || last["amount"].(map[string]any)["amount"] != "50.00" {
		t.Errorf("the half left: %s", rest.Raw)
	}
	// The invoice's line is the proceeds of one disposal only.
	e.problemIs(dispose(3, half), "disposeFixedAsset", 422, asset.CodeProceeds)
	// The clearing account the setting names must be in the chart: from the 15th it names 1999, which is not.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, asset.ClearingSetting, "1160", "1999", time.Date(2027, 10, 15, 0, 0, 0, 0, time.UTC), 1)
		return err
	})
	if p := dispose(3, half, "Prefer", "return=preview"); p.problemCode() != asset.CodeProceeds || !strings.Contains(p.Raw, "not set, or not in the chart") {
		t.Errorf("no clearing account in the chart: %d %s", p.Status, p.Raw)
	}

	// Recorded late: sold on 10 September, after September's month posted. September is reversed; a loss of S$500.
	late := dispose(3, map[string]any{"date": "2027-09-10", "proceeds": "money", "money_account_id": bank.ID.String(), "amount": sgd("2000.00")})
	if late.Status != http.StatusCreated || late.str("accumulated_depreciation", "amount") != "1100.00" || late.str("gain_or_loss", "amount") != "-500.00" {
		t.Fatalf("a disposal recorded late: %d %s", late.Status, late.Raw)
	}
	lateAsset := e.call(http.MethodGet, "/v1/fixed-assets/"+laptops[3].id, accountant, nil)
	reversed := 0
	for _, m := range lateAsset.Body["depreciation"].([]any) {
		if x := m.(map[string]any); x["status"] == "reversed" && x["month"] == "2027-09" {
			reversed++
		}
	}
	if reversed != 1 {
		t.Errorf("September's month reversed: %s", lateAsset.Raw)
	}

	// A date after today, a share above the cost.
	fresh, freshTag := e.registered("1500.00")
	e.problemIs(e.call(http.MethodPost, "/v1/fixed-assets/"+fresh+"/disposals", accountant, map[string]any{"date": "2027-10-16", "proceeds": "none"},
		"If-Match", freshTag), "disposeFixedAsset", 422, asset.CodeDisposalDate)
	e.problemIs(e.call(http.MethodPost, "/v1/fixed-assets/"+fresh+"/disposals", accountant, map[string]any{"date": "2027-10-15", "proceeds": "none",
		"cost": sgd("1500.01")}, "If-Match", freshTag), "disposeFixedAsset", 422, asset.CodeDisposalShare)

	// The register lists them; in Founder mode a void is asked for, not done.
	if l := e.call(http.MethodGet, "/v1/asset-disposals?asset_id="+laptops[0].id, e.as(authz.Bookkeeper), nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("listAssetDisposals: %s", l.Raw)
	}
	g := e.call(http.MethodGet, "/v1/asset-disposals/"+r.str("id"), accountant, nil)
	e.problemIs(e.call(http.MethodPost, "/v1/asset-disposals/"+r.str("id")+"/void", accountant, map[string]any{"date": "2027-10-15", "reason": "Not sold"},
		"If-Match", g.Header.Get("ETag")), "voidAssetDisposal", 409, CodeDisposalVoidApproval)

	// Every entry replays as it posted (ENG-06).
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := journal.Replay(ctx, u)
		if err == nil && len(r.Mismatches) != 0 {
			t.Errorf("replay: %+v", r.Mismatches)
		}
		return err
	})
}

// A sale's money received is matched from the bank as money recorded by hand (FR-FA-03, FR-BNK-07; design D4): the
// statement's line of S$500.00 into the account the disposal recorded it in matches the disposal.
// req: FR-FA-03
// req: FR-BNK-07
func TestADisposalsMoneyIsMatchedFromTheBank(t *testing.T) {
	e := newEnv(t)
	e.disposalBooks()
	m, line := e.bankLine("500.00")
	id, tag := e.registered("1500.00")
	r := e.call(http.MethodPost, "/v1/fixed-assets/"+id+"/disposals", e.as(authz.Accountant), map[string]any{"date": "2026-10-15", "proceeds": "money",
		"money_account_id": m.AccountID.String(), "amount": sgd("500.00")}, "If-Match", tag)
	if r.Status != http.StatusCreated || r.str("gain_or_loss", "amount") != "-1000.00" {
		t.Fatalf("disposeFixedAsset = %d: %s", r.Status, r.Raw)
	}
	if mr := e.call(http.MethodPost, "/v1/bank-lines/"+line.ID.String()+"/match-recorded", e.as(authz.Accountant),
		map[string]any{"document_id": r.str("id")}, "If-Match", etag(line.Version)); mr.Status != 200 {
		t.Fatalf("matching the sale's bank line: %d %s", mr.Status, mr.Raw)
	}
}

// accountByCode is e's organisation's account of a code.
func (e *env) accountByCode(code string) storage.Account {
	e.t.Helper()
	var a storage.Account
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		a, err = u.Accounts().ByCode(ctx, code)
		return err
	})
	return a
}

// writtenOff writes off a new S$1,500 asset on the env's today, and returns the disposal's ID and ETag.
func (e *env) writtenOff() (id, tag string) {
	e.t.Helper()
	e.disposalBooks()
	asset, assetTag := e.registered("1500.00")
	r := e.call(http.MethodPost, "/v1/fixed-assets/"+asset+"/disposals", e.as(authz.Accountant), map[string]any{"date": "2026-10-15", "proceeds": "none"},
		"If-Match", assetTag)
	if r.Status != http.StatusCreated {
		e.t.Fatalf("disposeFixedAsset = %d: %s", r.Status, r.Raw)
	}
	return r.str("id"), r.Header.Get("ETag")
}

// matrixDisposals are the authorisation matrix's rows for disposals of fixed assets.
var matrixDisposals = map[string]func(e *env, tok string) (response, func() bool){
	"listAssetDisposals": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/asset-disposals", tok, nil), nil
	},
	"getAssetDisposal": func(e *env, tok string) (response, func() bool) {
		id, _ := e.writtenOff()
		return e.call(http.MethodGet, "/v1/asset-disposals/"+id, tok, nil), nil
	},
	"disposeFixedAsset": func(e *env, tok string) (response, func() bool) {
		e.disposalBooks()
		id, tag := e.registered("1600.00")
		r := e.call(http.MethodPost, "/v1/fixed-assets/"+id+"/disposals", tok, map[string]any{"date": "2026-10-15", "proceeds": "none"}, "If-Match", tag)
		return r, func() bool {
			return e.call(http.MethodGet, "/v1/fixed-assets/"+id, e.as(authz.Owner), nil).str("status") == "in_use"
		}
	},
	// A void in Founder mode is asked for, so the row voids in an organisation in Standard mode.
	"voidAssetDisposal": func(e *env, tok string) (response, func() bool) {
		s := inStandardMode(e)
		e.tokens.mu.Lock()
		p := e.tokens.m[tok]
		e.tokens.mu.Unlock()
		if p.Organisation == e.books.Org {
			p.Organisation = s.books.Org
			tok = s.principal(p)
		}
		id, tag := s.writtenOff()
		r := s.call(http.MethodPost, "/v1/asset-disposals/"+id+"/void", tok, map[string]any{"date": "2026-10-15", "reason": "Matrix test"}, "If-Match", tag)
		return r, func() bool {
			return s.call(http.MethodGet, "/v1/asset-disposals/"+id, s.as(authz.Accountant), nil).str("status") == "posted"
		}
	},
	"requestAssetDisposalVoid": func(e *env, tok string) (response, func() bool) {
		was := requestsOf(e, approval.ObjectAssetDisposal)
		id, tag := e.writtenOff()
		person := steppedUp(e, tok)
		r := e.call(http.MethodPost, "/v1/asset-disposals/"+id+"/void-approval", person, map[string]any{"date": "2026-10-15", "reason": "Matrix test"},
			"If-Match", tag)
		if r.Status == http.StatusAccepted {
			e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
		}
		return r, func() bool { return requestsOf(e, approval.ObjectAssetDisposal) == was }
	},
}
