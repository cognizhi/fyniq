//go:build local

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A period's balances as at a revision are the sums of its entries' lines up to that revision (FR-RPT-08), however they
// are read (NFR-PERF-02, rev 0.250): with an entry of the period later than the revision, by summing the lines; with
// none, from the balances cache, its dimension sets summed, which the same transaction keeps (INV-08). At each revision
// they are what the entries posted so far add up to, an entry in another period changes nothing, and at the latest they
// are the cache's own figures.
// req: NFR-PERF-02
// req: FR-RPT-08
// req: INV-08
func TestAPeriodsBalancesAsAtARevisionAreItsLinesUpToIt(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "user:as-at"}})
	api := store(t, "fyniq_api")
	b := newFinancialBooks(t, ctx, api, "As At Pte Ltd")
	type posting struct {
		source         string
		month, day     int
		dr, cr, amount string
	}
	manual := ledger.SourceManualJournal
	postings := []posting{
		{ledger.SourceOpeningBalance, 10, 1, "1010", "3010", "10000.00"},
		{manual, 10, 5, "6110", "1010", "500.00"},
		{manual, 11, 2, "6110", "1010", "70.00"}, // another period's: October's balances do not move
		{manual, 10, 9, "1010", "4010", "1000.00"},
		{manual, 10, 12, "6110", "1010", "200.00"},
		{manual, 10, 13, "1010", "6110", "200.00"}, // takes the 200.00 back: 6110 keeps its lines, its sum unchanged
	}
	// want is October's balances after the first n postings: each account's sum and line count, as the lines give them.
	want := func(n int) string {
		sum, count := map[string]int64{}, map[string]int{}
		for _, p := range postings[:n] {
			if p.month != 10 {
				continue
			}
			var cents int64
			_, _ = fmt.Sscanf(strings.ReplaceAll(p.amount, ".", ""), "%d", &cents)
			sum[p.dr] += cents
			sum[p.cr] -= cents
			count[p.dr]++
			count[p.cr]++
		}
		var out []string
		for _, code := range []string{"1010", "3010", "4010", "6110"} {
			if count[code] > 0 {
				out = append(out, fmt.Sprintf("%s %d.%02d x%d", code, sum[code]/100, abs(sum[code]%100), count[code]))
			}
		}
		return strings.Join(out, "; ")
	}
	var revisions []int64
	for _, p := range postings {
		b.post(t, ctx, api, p.source, p.month, p.day, p.dr, p.cr, p.amount)
		if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			r, err := u.Ledger().Revision(ctx)
			revisions = append(revisions, r)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	codes := map[string]string{}
	for code, a := range b.acc {
		codes[a.ID.String()] = code
	}
	got := func(rows []storage.Balance) string {
		var out []string
		for _, code := range []string{"1010", "3010", "4010", "6110"} {
			for _, r := range rows {
				if codes[r.AccountID.String()] == code {
					amount := strings.TrimSuffix(strings.TrimSuffix(string(r.Amount), "00"), ".")
					if !strings.Contains(amount, ".") {
						amount += ".00"
					}
					out = append(out, fmt.Sprintf("%s %s x%d", code, amount, r.LineCount))
				}
			}
		}
		return strings.Join(out, "; ")
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for i, r := range revisions {
			rows, err := u.Balances().ForPeriodAt(ctx, b.Oct.ID, r)
			if err != nil {
				return err
			}
			if g, w := got(rows), want(i+1); g != w {
				t.Errorf("October as at revision %d (after posting %d): %s, want %s", r, i+1, g, w)
			}
		}
		cache, err := u.Balances().ForPeriod(ctx, b.Oct.ID)
		if err != nil {
			return err
		}
		if g, w := got(cache), want(len(postings)); g != w {
			t.Errorf("October's cache: %s, want %s", g, w)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
