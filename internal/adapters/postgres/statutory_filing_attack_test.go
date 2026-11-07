//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A filing record keeps its rules whoever writes it (FR-TAX-07; FY299): its own keys, an item's key and its outcome's
// fields; an item has at most one live record; a withdrawal names a reversed record of its item, once; and a record,
// which posts nothing, is withdrawn without a reversing entry.
// req: FR-TAX-07
// asvs: V8.2.1
func TestAttackStatutoryFilingsKeepTheirRules(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack filing records Pte Ltd")
	api := store(t, "fyniq_api")
	create := func(payload string) (uuid.UUID, error) {
		var id uuid.UUID
		err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			d, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentStatutoryFiling, Payload: []byte(payload)})
			id = d.ID
			return err
		})
		return id, err
	}
	status := func(id uuid.UUID, to storage.DocumentStatus) error {
		return api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return u.Documents().SetStatus(ctx, id, to) })
	}
	filed := `{"date":"2026-11-20","key":"eci:2026-12-31","outcome":"filed","filed_on":"2026-11-20","reference":"ECI-1"}`
	for name, payload := range map[string]string{
		"a key of its own":              strings.TrimSuffix(filed, `}`) + `,"amount":"1.00"}`,
		"no item":                       strings.Replace(filed, `"key":"eci:2026-12-31",`, ``, 1),
		"an item without its day":       strings.Replace(filed, `"eci:2026-12-31"`, `"eci"`, 1),
		"an unknown outcome":            strings.Replace(filed, `"filed"`, `"lodged"`, 1),
		"filed without its day":         strings.Replace(filed, `"filed_on":"2026-11-20",`, ``, 1),
		"filed without a reference":     strings.Replace(filed, `"ECI-1"`, `""`, 1),
		"filed with a reason":           strings.TrimSuffix(filed, `}`) + `,"reason":"why"}`,
		"not required without a reason": `{"date":"2026-11-20","key":"eci:2026-12-31","outcome":"not_required"}`,
		"not required with a reference": `{"date":"2026-11-20","key":"eci:2026-12-31","outcome":"not_required","reason":"Waived","reference":"X"}`,
		"a withdrawal of nothing":       `{"date":"2026-11-20","key":"eci:2026-12-31","outcome":"withdrawn","reason":"Wrong","withdraws":"` + uuid.NewString() + `"}`,
		"a note too long":               strings.TrimSuffix(filed, `}`) + `,"note":"` + strings.Repeat("x", 1001) + `"}`,
	} {
		if _, err := create(payload); storage.CodeOf(err) != storage.CodeStatutoryFiling {
			t.Errorf("a record with %s: %v", name, err)
		}
	}

	// One live record per item, however it is written; the record is withdrawn without a reversing entry.
	first, err := create(filed)
	if err != nil {
		t.Fatal(err)
	}
	if err := status(first, storage.DocumentPosted); err != nil {
		t.Fatalf("posting the first record: %v", err)
	}
	second, err := create(strings.Replace(filed, `"ECI-1"`, `"ECI-2"`, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := status(second, storage.DocumentPosted); storage.CodeOf(err) != storage.CodeStatutoryFiling {
		t.Errorf("a second live record of the item: %v", err)
	}
	withdrawal := `{"date":"2026-11-21","key":"eci:2026-12-31","outcome":"withdrawn","reason":"Wrong year","withdraws":"` + first.String() + `"}`
	early, err := create(withdrawal)
	if err != nil {
		t.Fatal(err)
	}
	if err := status(early, storage.DocumentPosted); storage.CodeOf(err) != storage.CodeStatutoryFiling {
		t.Errorf("a withdrawal of a record still live: %v", err)
	}
	if err := status(first, storage.DocumentReversed); err != nil {
		t.Fatalf("withdrawing a record, which posts nothing: %v", err)
	}
	if err := status(early, storage.DocumentPosted); err != nil {
		t.Errorf("the withdrawal of a reversed record: %v", err)
	}
	again, err := create(withdrawal)
	if err != nil {
		t.Fatal(err)
	}
	if err := status(again, storage.DocumentPosted); storage.CodeOf(err) != storage.CodeStatutoryFiling {
		t.Errorf("a second withdrawal of the record: %v", err)
	}
	if err := status(second, storage.DocumentPosted); err != nil {
		t.Errorf("a new record once the first is withdrawn: %v", err)
	}
}
