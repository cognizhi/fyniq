//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A feasibility study keeps its rules whoever writes it (FR-PLN-06; FY302): its own keys, a name, a date, 1 to 10 years,
// a life of 1 to 50 years, its figures as decimal strings within their limits, and a salvage no more than its investment.
// req: FR-PLN-06
// asvs: V8.2.1
func TestAttackFeasibilityStudiesKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack feasibility Pte Ltd")
	api := store(t, "fyniq_api")
	create := func(payload string) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentFeasibilityStudy, Payload: []byte(payload)})
			return err
		})
	}
	good := `{"name":"Copilot for SMEs","date":"2026-11-08","years":5,"discount_rate":"0.18","max_payback":"3","investment":"150000.00",` +
		`"life":5,"salvage":"0.00","working_capital":"0.1","units":"40","unit_growth":"0.5","price":"3600.00","price_growth":"0.03",` +
		`"variable_cost":"700.00","variable_cost_growth":"0.02","fixed":"180000.00","fixed_growth":"-0.08"}`
	for name, payload := range map[string]string{
		"a key of its own":          strings.TrimSuffix(good, `}`) + `,"approved":true}`,
		"no name":                   strings.Replace(good, `"name":"Copilot for SMEs",`, ``, 1),
		"a name too long":           strings.Replace(good, `"Copilot for SMEs"`, `"`+strings.Repeat("x", 81)+`"`, 1),
		"no date":                   strings.Replace(good, `"date":"2026-11-08",`, ``, 1),
		"a date not a date":         strings.Replace(good, `"2026-11-08"`, `"8 Nov 2026"`, 1),
		"11 years":                  strings.Replace(good, `"years":5`, `"years":11`, 1),
		"years as text":             strings.Replace(good, `"years":5`, `"years":"5"`, 1),
		"a life of 0":               strings.Replace(good, `"life":5`, `"life":0`, 1),
		"a rate above 1":            strings.Replace(good, `"discount_rate":"0.18"`, `"discount_rate":"1.5"`, 1),
		"a rate as a number":        strings.Replace(good, `"discount_rate":"0.18"`, `"discount_rate":0.18`, 1),
		"a negative investment":     strings.Replace(good, `"150000.00"`, `"-150000.00"`, 1),
		"a salvage above it":        strings.Replace(good, `"salvage":"0.00"`, `"salvage":"200000.00"`, 1),
		"units with 2 decimals":     strings.Replace(good, `"units":"40"`, `"units":"40.25"`, 1),
		"a change of -100%":         strings.Replace(good, `"fixed_growth":"-0.08"`, `"fixed_growth":"-1"`, 1),
		"a change of 1,000%":        strings.Replace(good, `"price_growth":"0.03"`, `"price_growth":"10"`, 1),
		"a longest payback of 60":   strings.Replace(good, `"max_payback":"3"`, `"max_payback":"60"`, 1),
		"a figure with 7 decimals":  strings.Replace(good, `"price":"3600.00"`, `"price":"3600.0000001"`, 1),
		"working capital above all": strings.Replace(good, `"working_capital":"0.1"`, `"working_capital":"1.1"`, 1),
	} {
		if err := create(payload); storage.CodeOf(err) != storage.CodeFeasibility {
			t.Errorf("a study with %s: %v", name, err)
		}
	}
	if err := create(good); err != nil {
		t.Errorf("a study as its rules allow: %v", err)
	}
}
