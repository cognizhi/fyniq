//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A budget keeps its rules whoever writes it (FR-PLN-01; FY301): its own keys at the top and in every line, a name, its
// kind and year, a revised version naming what it revises, and lines of an account, its dimension values and 1 to 24
// decimal amounts.
// req: FR-PLN-01
// asvs: V8.2.1
func TestAttackBudgetsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack budgets Pte Ltd")
	api := store(t, "fyniq_api")
	create := func(payload string) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBudget, Payload: []byte(payload)})
			return err
		})
	}
	acc := "01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b20"
	good := `{"name":"FY2027 budget","kind":"original","financial_year":"2027-01-01","lines":[{"account_id":"` + acc +
		`","dimension_value_ids":[],"amounts":["10000.00","10000.00","-500.00"]}]}`
	for name, payload := range map[string]string{
		"a key of its own":          strings.TrimSuffix(good, `}`) + `,"approved":true}`,
		"no name":                   strings.Replace(good, `"name":"FY2027 budget",`, ``, 1),
		"a name too long":           strings.Replace(good, `"FY2027 budget"`, `"`+strings.Repeat("x", 81)+`"`, 1),
		"a kind of its own":         strings.Replace(good, `"original"`, `"final"`, 1),
		"revised revising nothing":  strings.Replace(good, `"original"`, `"revised"`, 1),
		"original revising":         strings.TrimSuffix(good, `}`) + `,"revises":"` + acc + `"}`,
		"a year not a date":         strings.Replace(good, `"2027-01-01"`, `"FY2027"`, 1),
		"lines not a list":          `{"name":"FY2027 budget","kind":"original","financial_year":"2027-01-01","lines":{"x":1}}`,
		"an account not an ID":      strings.Replace(good, `"account_id":"`+acc+`"`, `"account_id":"6110"`, 1),
		"a value not an ID":         strings.Replace(good, `"dimension_value_ids":[]`, `"dimension_value_ids":["SALES"]`, 1),
		"no amounts":                strings.Replace(good, `["10000.00","10000.00","-500.00"]`, `[]`, 1),
		"25 amounts":                strings.Replace(good, `["10000.00","10000.00","-500.00"]`, `[`+strings.TrimSuffix(strings.Repeat(`"1.00",`, 25), ",")+`]`, 1),
		"an amount as a number":     strings.Replace(good, `"-500.00"`, `-500.00`, 1),
		"an amount with 7 decimals": strings.Replace(good, `"-500.00"`, `"-500.0000001"`, 1),
		"a line's key of its own":   strings.Replace(good, `"amounts":`, `"note":"x","amounts":`, 1),
	} {
		if err := create(payload); storage.CodeOf(err) != storage.CodeBudget {
			t.Errorf("a budget with %s: %v", name, err)
		}
	}
	if err := create(good); err != nil {
		t.Errorf("a budget as its rules allow: %v", err)
	}
	if err := create(strings.Replace(good, `"kind":"original"`, `"kind":"revised","revises":"`+acc+`"`, 1)); err != nil {
		t.Errorf("a revised budget naming what it revises: %v", err)
	}
}
