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
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// recordClass records a share class on the account with the liquidation it names, as an approved class (the register's own approval is the equity records' test).
func (e *env) recordClass(account string, c equity.ShareClass) {
	e.t.Helper()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		s, err := journal.LoadSettings(ctx, u)
		if err != nil {
			return err
		}
		c.AccountID = uuid.MustParse(account)
		c.Date = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
		doc, err := equity.DraftShareClassIn(ctx, u, c, s)
		if err != nil {
			return err
		}
		return u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted)
	}); err != nil {
		e.t.Fatal(err)
	}
}

// The exit waterfall through the API, worked by hand (FR-EQ-29): a founder holds 8,000,000 ordinary shares and an investor 1,000,000 Series Seed preference shares bought for
// S$2,000,000 with a non-participating preference of 1x. For an exit of S$5,000,000 the investor takes its S$2,000,000 back (converting would give 1/9 of 5,000,000, S$555,555.56) and the
// founder the other S$3,000,000; for S$30,000,000 the investor converts and takes 1/9, S$3,333,333.33, the founder S$26,666,666.67. A class with a cap on its participation is refused by name,
// the page says so while no class has a preference, and a Board viewer holding no grant is refused.
// req: FR-EQ-29
func TestTheExitWaterfallPaysThePreferenceOrTheConversionWhicheverIsMore(t *testing.T) {
	e := newEnv(t)
	e.booksFrom()
	accountant := e.as(authz.Accountant)
	ordinary, pref := e.shareCapitalIn(e.books.Org), e.shareCapital3020()
	founder := e.call(http.MethodPost, "/v1/contacts", accountant, map[string]any{"legal_name": "Ah Kow", "director": true, "shareholder": true}).str("id")
	investor := e.call(http.MethodPost, "/v1/contacts", accountant, map[string]any{"legal_name": "Seedling Ventures Pte. Ltd.", "investor": true, "shareholder": true}).str("id")
	issue := func(holder, account, shares, price string) {
		t.Helper()
		if r := e.call(http.MethodPost, "/v1/share-issues", steppedUp(e, e.as(authz.Owner)), map[string]any{"holder_id": holder, "class_account_id": account, "shares": shares, "price": price,
			"consideration": "cash", "resolution_refs": "Allotment resolution", "effective_on": "2025-06-12"}); r.Status != http.StatusCreated {
			t.Fatalf("issuing %s: %d %s", shares, r.Status, r.Raw)
		}
	}
	issue(founder, ordinary, "8000000", "0.01")
	issue(investor, pref, "1000000", "2.00")

	// While no class has a preference there is nothing to model.
	none := e.call(http.MethodGet, "/v1/exit-waterfall?exit_value=5000000.00&on=2026-10-15", accountant, nil)
	if none.Status != http.StatusOK || none.Body["available"] != false || !strings.Contains(none.str("message"), "No share class records a liquidation preference") {
		t.Fatalf("no preference: %d %s", none.Status, none.Raw)
	}
	e.recordClass(pref, equity.ShareClass{Name: "Series Seed", Kind: "preference", Series: "Seed", VotesPerShare: "1", Liquidation: equity.Liquidation{Multiple: "1", Seniority: 1},
		Dividend: equity.Dividend{Kind: "none"}, AntiDilution: "none"})

	totals := func(r response, key string) map[string]string {
		out := map[string]string{}
		for _, c := range r.Body[key].([]any) {
			for _, h := range c.(map[string]any)["holders"].([]any) {
				hm := h.(map[string]any)
				out[hm["holder_name"].(string)] = hm["total"].(map[string]any)["amount"].(string)
			}
		}
		return out
	}
	small := e.call(http.MethodGet, "/v1/exit-waterfall?exit_value=5000000.00&on=2026-10-15", accountant, nil)
	if small.Status != http.StatusOK || small.Body["available"] != true {
		t.Fatalf("5,000,000: %d %s", small.Status, small.Raw)
	}
	if got := totals(small, "classes"); got["Seedling Ventures Pte. Ltd."] != "2000000.00" || got["Ah Kow"] != "3000000.00" {
		t.Errorf("under the preference: %v", got)
	}
	if got := totals(small, "as_converted"); got["Seedling Ventures Pte. Ltd."] != "555555.56" || got["Ah Kow"] != "4444444.44" {
		t.Errorf("as converted: %v", got)
	}
	big := e.call(http.MethodGet, "/v1/exit-waterfall?exit_value=30000000.00&on=2026-10-15", accountant, nil)
	if got := totals(big, "classes"); got["Seedling Ventures Pte. Ltd."] != "3333333.33" || got["Ah Kow"] != "26666666.67" {
		t.Errorf("converting: %v", got)
	}
	var seed map[string]any
	for _, c := range big.Body["classes"].([]any) {
		if c.(map[string]any)["name"] == "Series Seed" {
			seed = c.(map[string]any)
		}
	}
	if seed["converts"] != true || seed["has_preference"] != true || seed["holders"].([]any)[0].(map[string]any)["invested"].(map[string]any)["amount"] != "2000000.00" {
		t.Errorf("the class: %v", seed)
	}

	// A cap on a participating preference is refused by name, never approximated.
	seedID := uuid.MustParse(seed["class_id"].(string))
	e.recordClass(pref, equity.ShareClass{ClassID: &seedID, Name: "Series Seed", Kind: "preference", Series: "Seed", VotesPerShare: "1",
		Liquidation: equity.Liquidation{Multiple: "1", Participating: true, CapMultiple: "3", Seniority: 1}, Dividend: equity.Dividend{Kind: "none"}, AntiDilution: "none"})
	e.problemIs(e.call(http.MethodGet, "/v1/exit-waterfall?exit_value=5000000.00&on=2026-10-15", accountant, nil), "getExitWaterfall", 422, CodeWaterfallRefused)
	e.problemIs(e.call(http.MethodGet, "/v1/exit-waterfall?exit_value=abc", accountant, nil), "getExitWaterfall", 422, CodeInvalid)
	e.problemIs(e.call(http.MethodGet, "/v1/exit-waterfall?exit_value=5000000.00", e.as(authz.BoardViewer), nil), "getExitWaterfall", 403, "API-AUTHZ-01")
}
