//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-10 through the API (p1-sales task 1.6): AR ageing as at a day, by customer and currency, with the totals, their
// carrying amounts and the unrealised FX exposure at the day's rate; a currency with no rate says so (FX-01). An SGD invoice of
// 14 October due on 28 October is 33 days past due at 30 November; a USD 100.00 invoice at 1.34 (S$134.00) is worth
// S$136.00 at 1.36, a gain of S$2.00.
// req: FR-AR-10
func TestReceivablesThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	acc := e.as(authz.Accountant)
	e.customer()
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "fixture"}})
	if err := e.store.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, r := range []struct {
			day  int
			rate string
		}{{14, "1.34"}, {30, "1.36"}} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, r.day), Rate: storage.Decimal(r.rate),
				Quote: storage.Decimal(r.rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		_, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(11, 30), Rate: "1.36", Quote: "1.36",
			QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.issuedInvoice("1", "1000.00")
	usd := e.invoiceDraft("1", "100.00")
	usd["currency"] = "USD"
	r := e.call(http.MethodPost, "/v1/invoices", acc, usd)
	if p := e.call(http.MethodPost, "/v1/invoices/"+r.str("id")+"/issue", acc, nil, "If-Match", r.Header.Get("ETag")); p.Status != 200 {
		t.Fatalf("issuing the USD invoice: %d %s", p.Status, p.Raw)
	}

	g := e.call(http.MethodGet, "/v1/receivables?as_at=2026-11-30", e.as(authz.Auditor), nil)
	if g.Status != 200 || g.str("as_at") != "2026-11-30" || len(g.Body["customers"].([]any)) != 1 ||
		!strings.Contains(g.Raw, `{"currency":"SGD","current":{"amount":"0.00","currency":"SGD"},"days_1_30":{"amount":"0.00","currency":"SGD"},"days_31_60":{"amount":"1000.00","currency":"SGD"}`) ||
		!strings.Contains(g.Raw, `"fx_exposure":[{"currency":"USD","open":{"amount":"100.00","currency":"USD"},"carrying":{"amount":"134.00","currency":"SGD"},"fx_rate":"1.36"`) ||
		!strings.Contains(g.Raw, `"unrealised":{"amount":"2.00","currency":"SGD"}`) {
		t.Fatalf("the receivables: %d %s", g.Status, g.Raw)
	}
	// GBP has no rate anywhere (the feed's tests keep it so): an invoice at a typed rate is aged, and its exposure says so.
	gbp := e.invoiceDraft("1", "50.00")
	gbp["currency"], gbp["fx_rate"] = "GBP", "1.70"
	r = e.call(http.MethodPost, "/v1/invoices", acc, gbp)
	if p := e.call(http.MethodPost, "/v1/invoices/"+r.str("id")+"/issue", acc, nil, "If-Match", r.Header.Get("ETag")); p.Status != 200 {
		t.Fatalf("issuing the GBP invoice: %d %s", p.Status, p.Raw)
	}
	if g := e.call(http.MethodGet, "/v1/receivables?as_at=2026-11-30", acc, nil); !strings.Contains(g.Raw, `{"currency":"GBP","open":{"amount":"50.00","currency":"GBP"},"carrying":{"amount":"85.00","currency":"SGD"},"missing":"`) {
		t.Errorf("a currency with no rate: %s", g.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/receivables?as_at=yesterday", acc, nil), "getReceivables", 422, CodeInvalid)
}
