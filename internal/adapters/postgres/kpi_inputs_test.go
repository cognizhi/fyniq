//go:build local

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-KPI-02 (rev 0.161): a month's customer counts and headcount are entered at version 1 and changed only as their
// next version, each audited with the counts before and after; a change made from a stale read is refused; the three
// customer counts come together or not at all, as whole numbers from zero; and nothing deletes a month (FY244). Another
// organisation never sees them.
// req: FR-KPI-02
// req: QA-ENG-05
// asvs: V8.2.2, V16.3.1
func TestAMonthsKPICountsAreEnteredAndChangedAsVersions(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "founder"}})
	api := store(t, "fyniq_api")
	b := books(t, "KPI counts Pte Ltd")
	other := books(t, "Other KPIs Pte Ltd")
	oct := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	n := func(i int64) *int64 { return &i }
	at := time.Date(2026, 11, 3, 2, 0, 0, 0, time.UTC)
	set := func(org uuid.UUID, c storage.KPICounts, version int) (storage.KPIInput, error) {
		var x storage.KPIInput
		err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			x, err = u.KPIInputs().Set(ctx, oct, c, "user:founder", at, version)
			return err
		})
		return x, err
	}
	first, err := set(b.Org, storage.KPICounts{Paying: n(10), New: n(2), Lost: n(1), Headcount: n(8)}, 0)
	if err != nil || first.Version != 1 || *first.Counts.Paying != 10 || *first.Counts.Headcount != 8 {
		t.Fatalf("entering October: %+v, %v", first, err)
	}
	if _, err := set(b.Org, storage.KPICounts{Headcount: n(9)}, 0); !refusedAs(err, storage.CodeVersion) {
		t.Errorf("entering October twice: %v", err)
	}
	second, err := set(b.Org, storage.KPICounts{Paying: n(11), New: n(3), Lost: n(1), Headcount: n(9)}, 1)
	if err != nil || second.Version != 2 || second.ID != first.ID || *second.Counts.Paying != 11 {
		t.Fatalf("changing October: %+v, %v", second, err)
	}
	if _, err := set(b.Org, storage.KPICounts{Headcount: n(10)}, 1); !refusedAs(err, storage.CodeVersion) {
		t.Errorf("a change from a stale read: %v", err)
	}
	for name, c := range map[string]storage.KPICounts{
		"customers without the new and lost": {Paying: n(5)},
		"a negative count":                   {Paying: n(5), New: n(-1), Lost: n(0)},
		"nothing at all":                     {},
	} {
		if _, err := set(other.Org, c, 0); !refusedAs(err, storage.CodeInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := api.Do(ctx, other.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.KPIInputs().Get(ctx, oct)
		if !errors.Is(err, ports.ErrNotFound) {
			t.Errorf("another organisation reads October's counts: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		xs, err := u.KPIInputs().Range(ctx, oct.AddDate(0, -2, 0), oct)
		if len(xs) != 1 || xs[0].Version != 2 {
			t.Errorf("the months' counts: %+v", xs)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := store(t, "fyniq_readonly").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 1000)
		var trail strings.Builder
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		for _, want := range []string{`"action":"kpi_input.entered"`, `"action":"kpi_input.changed"`, `"paying_customers":10`, `"paying_customers":11`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the counts' audit events lack %s: %s", want, trail.String())
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// FY244 and the grants: a month is written at version 1, changed only as its next version, and never deleted.
	for _, c := range []struct {
		role, sql, code string
	}{
		{"fyniq_api", `UPDATE fyniq.kpi_input SET headcount = 1, version = 5 WHERE month = '2026-10-01'`, "FY244"},
		{"fyniq_api", `UPDATE fyniq.kpi_input SET headcount = 1, version = 3, entered_at = '2026-01-01' WHERE month = '2026-10-01'`, "FY244"},
		{"fyniq_api", `DELETE FROM fyniq.kpi_input WHERE month = '2026-10-01'`, "42501"},
		{"fyniq_worker", `UPDATE fyniq.kpi_input SET headcount = 1 WHERE month = '2026-10-01'`, "42501"},
		{"fyniq_worker", `INSERT INTO fyniq.kpi_input (month, headcount, entered_by, entered_at) VALUES ('2026-09-01', 1, 'x', now())`, "42501"},
	} {
		err := asOrg(context.Background(), conn(t, c.role), b.Org, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), c.sql)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), c.code) {
			t.Errorf("%s: %s: want %s, got %v", c.role, c.sql, c.code, err)
		}
	}
}
