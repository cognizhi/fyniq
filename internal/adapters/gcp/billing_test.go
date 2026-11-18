package gcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Google's billing catalogue is read for Agent Platform's service, found by its name, every page of its SKUs in US
// dollars; prices come from whole units and nanos as exact decimal text (AI-82 (b)) [live-verify].
// req: AI-82
func TestTheBillingCatalogueIsReadExactly(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.URL.Path == "/v1/services" && r.URL.Query().Get("pageToken") == "":
			_ = json.NewEncoder(w).Encode(map[string]any{"services": []any{map[string]any{"name": "services/AAAA", "displayName": "Compute Engine"}},
				"nextPageToken": "p2"})
		case r.URL.Path == "/v1/services":
			_ = json.NewEncoder(w).Encode(map[string]any{"services": []any{map[string]any{"name": "services/C7E2", "displayName": "Vertex AI"}}})
		case r.URL.Path == "/v1/services/C7E2/skus" && r.URL.Query().Get("pageToken") == "":
			_, _ = w.Write([]byte(`{"skus": [{"skuId": "A1", "description": "Gemini Test 3 Pro Input Text Tokens", "serviceRegions": ["asia-southeast1"],
				"pricingInfo": [{"pricingExpression": {"usageUnit": "count", "tieredRates": [{"startUsageAmount": 0,
				"unitPrice": {"currencyCode": "USD", "units": "0", "nanos": 1250}}]}}]}], "nextPageToken": "s2"}`))
		case r.URL.Path == "/v1/services/C7E2/skus":
			_, _ = w.Write([]byte(`{"skus": [{"skuId": "A2", "description": "Gemini Test 3 Pro Output Text Tokens", "serviceRegions": ["global"],
				"pricingInfo": [{"pricingExpression": {"usageUnit": "count", "tieredRates": [{"unitPrice": {"currencyCode": "USD", "units": "2", "nanos": 500000000}}]}}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	b := NewBillingCatalog(srv.URL, nil, srv.Client())
	skus, err := b.SKUs(context.Background())
	if err != nil || len(skus) != 2 || skus[0].ID != "A1" || skus[0].UsageUnit != "count" || skus[0].Tiers[0].Price != "0.00000125" ||
		skus[0].Tiers[0].Currency != "USD" || skus[0].Regions[0] != "asia-southeast1" || skus[1].Tiers[0].Price != "2.5" || skus[1].Tiers[0].StartUsage != "0" {
		t.Fatalf("the SKUs: %v %+v", err, skus)
	}
	if !strings.Contains(strings.Join(asked, " "), "currencyCode=USD") {
		t.Errorf("the requests: %v", asked)
	}
	b.Names = []string{"Agent Platform"}
	if _, err := b.SKUs(context.Background()); err == nil || !strings.Contains(err.Error(), "no service named Agent Platform") {
		t.Errorf("no such service: %v", err)
	}
	for _, c := range []struct{ units, nanos, want string }{{"0", "1250", "0.00000125"}, {"3", "0", "3"}, {"-1", "-500000000", "-1.5"}, {"", "", "0"}} {
		if got, err := moneyText(c.units, c.nanos); err != nil || got != c.want {
			t.Errorf("%s units %s nanos: %q %v", c.units, c.nanos, got, err)
		}
	}
	for _, c := range [][2]string{{"1", "-5"}, {"x", "0"}, {"0", "1000000000"}} {
		if _, err := moneyText(c[0], c[1]); err == nil {
			t.Errorf("%v: read", c)
		}
	}
}
