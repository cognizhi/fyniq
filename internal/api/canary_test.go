//go:build local

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/canary"
	"github.com/cognizhi/fyniq/internal/clock"
)

// token is a fixed bearer token, as the canary's client would hold one.
type token string

func (t token) Token(context.Context) (string, error) { return string(t), nil }

// rewriting is a transport that changes what one path answers, as a fault between the ledger and its readers would.
type rewriting struct {
	next     http.RoundTripper
	path     string
	from, to string
}

func (r rewriting) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err != nil || req.Method != http.MethodGet || !strings.HasPrefix(req.URL.Path, r.path) {
		return resp, err
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	b = bytes.Replace(b, []byte(r.from), []byte(r.to), 1)
	resp.Body, resp.ContentLength = io.NopCloser(bytes.NewReader(b)), int64(len(b))
	resp.Header.Del("Content-Length")
	return resp, nil
}

// QA-PROD-01: the canary's night (p1-launch-readiness task 6.2, design D8; the owner's decision of 16 Nov 2026 to post
// through the public API as a client of it). In the canary organisation it makes its customer and supplier, then posts
// an invoice of S$100.00 and the receipt that settles it, a bill of S$40.00 and the payment that settles it, and a
// journal, and reads them back: each posted for its total and paid, the trial balance in balance. The second night
// reuses its contacts and posts again. A document that reads back otherwise, and an account the organisation does not
// have, each fail the night, naming what is wrong. The run in the dev cloud, with its integration client, is
// [live-verify] (docs/runbooks/canary.md).
// req: QA-PROD-01
func TestTheCanaryPostsAndVerifiesNightly(t *testing.T) {
	e := newEnv(t)
	e.customer() // the books' receivables and payables accounts, as the standard chart has them
	e.supplier()
	e.moneyAccount() // 1010, a money account
	ctx := context.Background()
	c := canary.Canary{BaseURL: e.srv.URL, Tokens: token(e.as(authz.Accountant)), HTTP: e.srv.Client(),
		Codes: canary.Codes{Bank: "1010", Sales: "4000", Costs: "6100"}}
	day := e.clock.Now().In(clock.Singapore)

	first, err := c.Run(ctx, day)
	if err != nil || first.Day != "2026-10-15" || first.Invoice == "" || first.Receipt == "" || first.Bill == "" || first.Payment == "" || first.Journal == "" {
		t.Fatalf("the first night: %+v %v", first, err)
	}
	second, err := c.Run(ctx, day)
	if err != nil || second.Invoice == first.Invoice || second.Bill == first.Bill {
		t.Fatalf("the second night: %+v %v", second, err)
	}
	for role, name := range map[string]string{"customer": canary.Customer, "vendor": canary.Supplier} {
		r := e.call(http.MethodGet, "/v1/contacts?role="+role+"&limit=100", e.as(authz.Accountant), nil)
		if n := strings.Count(r.Raw, `"legal_name":"`+name+`"`); n != 1 {
			t.Errorf("%d contacts named %s after two nights; want 1", n, name)
		}
	}

	// A fault between the ledger and what reads it: the invoice reads back for another total.
	faulty := c
	faulty.HTTP = &http.Client{Transport: rewriting{next: e.srv.Client().Transport, path: "/v1/invoices/", from: `"total":{"amount":"100.00"`,
		to: `"total":{"amount":"100.01"`}}
	if _, err := faulty.Run(ctx, day); err == nil || !strings.Contains(err.Error(), "the invoice totals 100.01; it was posted for 100.00") {
		t.Errorf("a night whose invoice reads back otherwise: %v", err)
	}
	missing := c
	missing.Codes.Costs = "6999"
	if _, err := missing.Run(ctx, day); err == nil || !strings.Contains(err.Error(), "no account 6999") {
		t.Errorf("a night with an account the organisation lacks: %v", err)
	}
	if _, err := (canary.Canary{}).Run(ctx, day); err == nil {
		t.Error("a canary with nothing configured ran")
	}
}
