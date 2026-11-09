//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A forecast keeps its rules whoever writes it (FR-PLN-02; FY300): its own keys at the top and in every row, a name, a
// horizon of 12 to 60 months, every amount and rate a decimal string, and its rows and scenarios within their limits.
// req: FR-PLN-02
// req: FR-PLN-08
// asvs: V8.2.1
func TestAttackForecastsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack forecasts Pte Ltd")
	api := store(t, "fyniq_api")
	create := func(payload string) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentForecast, Payload: []byte(payload)})
			return err
		})
	}
	good := `{"name":"FY2027 plan","horizon":36,"cash_adjustment":"0.00",` +
		`"revenue":{"customers":"78","new_customers":"9","new_growth":"0.04","churn":"0.015","arpa":"120.00","price_increase":"0.05",` +
		`"services":"30000.00","services_growth":"0.01","other":"0","collection":{"this_month":"0.3","next_month":"0.6","month_after":"0.1"}},` +
		`"costs":{"hosting":"0.15","ai":"0.05","contractors":"0.05","fees":"0.012","salary_increase":"0.04"},` +
		`"opex":[{"label":"Rent","base":"2200.00","growth":"0","percent":"0"}],` +
		`"headcount":[{"role":"CTO","function":"R&D","start":"2025-06","salary":"6000.00","cpf":true}],` +
		`"capex":[{"amount":"8000.00","month":"2027-01","life":36}],` +
		`"funding":[{"type":"Loan","month":"2027-02","amount":"100000.00","rate":"0.055","tenor":36}],` +
		`"tax":{"start_up_years":3,"losses_brought_forward":"0.00","payment_lag":9},` +
		`"scenarios":[{"name":"Base","levers":{"new_customers":"1","churn":"1","price":"1","services":"1","opex":"1","direct_costs":"1","fund_amount":"1","hire_delay":0,"fund_delay":0}}]}`
	many := func(row string, n int) string { return strings.TrimSuffix(strings.Repeat(row+",", n), ",") }
	for name, payload := range map[string]string{
		"a key of its own":          strings.TrimSuffix(good, `}`) + `,"posted":true}`,
		"no name":                   strings.Replace(good, `"name":"FY2027 plan",`, ``, 1),
		"a name too long":           strings.Replace(good, `"FY2027 plan"`, `"`+strings.Repeat("x", 81)+`"`, 1),
		"a horizon too short":       strings.Replace(good, `"horizon":36`, `"horizon":11`, 1),
		"a horizon too long":        strings.Replace(good, `"horizon":36`, `"horizon":61`, 1),
		"a horizon as text":         strings.Replace(good, `"horizon":36`, `"horizon":"36"`, 1),
		"a churn as a number":       strings.Replace(good, `"churn":"0.015"`, `"churn":0.015`, 1),
		"a negative ARPA":           strings.Replace(good, `"arpa":"120.00"`, `"arpa":"-120.00"`, 1),
		"a driver of its own":       strings.Replace(good, `"other":"0",`, `"other":"0","bonus":"1",`, 1),
		"two collection shares":     strings.Replace(good, `,"month_after":"0.1"`, ``, 1),
		"a share as a number":       strings.Replace(good, `"next_month":"0.6"`, `"next_month":0.6`, 1),
		"a cost driver missing":     strings.Replace(good, `"fees":"0.012",`, ``, 1),
		"an expense line unnamed":   strings.Replace(good, `"label":"Rent",`, ``, 1),
		"a function of its own":     strings.Replace(good, `"function":"R&D"`, `"function":"Ops"`, 1),
		"a start not a month":       strings.Replace(good, `"start":"2025-06"`, `"start":"June"`, 1),
		"CPF as text":               strings.Replace(good, `"cpf":true`, `"cpf":"yes"`, 1),
		"a life of none":            strings.Replace(good, `"life":36`, `"life":0`, 1),
		"funding of its own kind":   strings.Replace(good, `"type":"Loan"`, `"type":"Gift"`, 1),
		"a tenor too long":          strings.Replace(good, `"tenor":36`, `"tenor":400`, 1),
		"four start-up years":       strings.Replace(good, `"start_up_years":3`, `"start_up_years":4`, 1),
		"a lag too long":            strings.Replace(good, `"payment_lag":9`, `"payment_lag":25`, 1),
		"no scenario":               strings.Replace(good, good[strings.Index(good, `"scenarios":`):len(good)-1], `"scenarios":[]`, 1),
		"a lever as a number":       strings.Replace(good, `"price":"1"`, `"price":1`, 1),
		"a delay too long":          strings.Replace(good, `"hire_delay":0`, `"hire_delay":25`, 1),
		"a buffer too long":         strings.TrimSuffix(good, `}`) + `,"buffer_months":37}`,
		"31 expense lines":          strings.Replace(good, `[{"label":"Rent","base":"2200.00","growth":"0","percent":"0"}]`, `[`+many(`{"label":"Rent","base":"2200.00","growth":"0","percent":"0"}`, 31)+`]`, 1),
		"an amount with 7 decimals": strings.Replace(good, `"amount":"8000.00"`, `"amount":"8000.0000001"`, 1),
	} {
		if err := create(payload); storage.CodeOf(err) != storage.CodeForecast {
			t.Errorf("a forecast with %s: %v", name, err)
		}
	}
	if err := create(good); err != nil {
		t.Errorf("a forecast as its rules allow: %v", err)
	}
	if err := create(strings.TrimSuffix(good, `}`) + `,"buffer_months":6,"last_actual":"2026-10"}`); err != nil {
		t.Errorf("a forecast with its own buffer and last actual month: %v", err)
	}
}
