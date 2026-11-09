//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A GST forecast keeps its rules whoever writes it (FR-TAX-05; FY298): its own keys, its day, a value in Singapore
// dollars above zero with cents, and a basis of 1 to 1,000 characters.
// req: FR-TAX-05
// asvs: V8.2.1
func TestAttackGSTForecastsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack GST forecasts Pte Ltd")
	api := store(t, "fyniq_api")
	create := func(payload string) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentGSTForecast, Payload: []byte(payload)})
			return err
		})
	}
	good := `{"date":"2026-11-20","forecast_on":"2026-11-20","value":{"amount":"1200000.00","currency":"SGD"},"basis":"Signed contracts"}`
	for name, payload := range map[string]string{
		"a key of its own":   strings.TrimSuffix(good, `}`) + `,"probability":"high"}`,
		"no day":             strings.Replace(good, `"forecast_on":"2026-11-20",`, ``, 1),
		"a day not a date":   strings.Replace(good, `"forecast_on":"2026-11-20"`, `"forecast_on":"20 Nov"`, 1),
		"another currency":   strings.Replace(good, `"SGD"`, `"USD"`, 1),
		"nothing":            strings.Replace(good, `"1200000.00"`, `"0.00"`, 1),
		"below zero":         strings.Replace(good, `"1200000.00"`, `"-1.00"`, 1),
		"no cents":           strings.Replace(good, `"1200000.00"`, `"1200000"`, 1),
		"a number":           strings.Replace(good, `"1200000.00"`, `1200000.00`, 1),
		"no basis":           strings.Replace(good, `"Signed contracts"`, `""`, 1),
		"a basis too long":   strings.Replace(good, `"Signed contracts"`, `"`+strings.Repeat("x", 1001)+`"`, 1),
		"a basis not a text": strings.Replace(good, `"Signed contracts"`, `["Signed contracts"]`, 1),
		"a value not money":  strings.Replace(good, `{"amount":"1200000.00","currency":"SGD"}`, `"1200000.00"`, 1),
	} {
		if err := create(payload); storage.CodeOf(err) != storage.CodeGSTForecast {
			t.Errorf("a forecast with %s: %v", name, err)
		}
	}
	if err := create(good); err != nil {
		t.Errorf("a forecast as its rules allow: %v", err)
	}
}
