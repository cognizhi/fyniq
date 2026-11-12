package compliance

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type catalogue struct{ g storage.Guards }

func (c catalogue) Guards(context.Context) (storage.Guards, error) { return c.g, nil }

// schema.verify names each guard changed by hand: the role, table and privilege of a grant, the trigger and its table,
// the policy, and the table whose row-level security changed (design D4).
// req: CTL-FIN-02
// req: CTL-AUD-01
// req: CTL-AI-01
func TestSchemaVerifyNamesEachDifference(t *testing.T) {
	release := storage.Guards{
		Grants:      []string{"fyniq_ai        USAGE     schema fyniq", "fyniq_api       SELECT    journal_entry", "fyniq_api       INSERT    account (code, name)"},
		Triggers:    []string{"CREATE TRIGGER require_audit BEFORE INSERT OR UPDATE ON fyniq.account FOR EACH ROW EXECUTE FUNCTION fyniq.require_audit()"},
		Policies:    []string{"account | tenant | * | (organisation_id = current_org()) | (organisation_id = current_org())"},
		RowSecurity: []string{"account | enabled | forced"},
	}
	if diffs, err := VerifySchema(context.Background(), catalogue{release}, release); err != nil || diffs != nil {
		t.Fatalf("the release's own guards: %v, %v", diffs, err)
	}
	changed := storage.Guards{
		Grants: []string{"fyniq_ai        USAGE     schema fyniq", "fyniq_ai        INSERT    journal_entry",
			"fyniq_api       SELECT    journal_entry", "fyniq_api       UPDATE    journal_line", "fyniq_api       INSERT    account (code, name)"},
		Policies:    release.Policies,
		RowSecurity: []string{"account | enabled | not forced"},
	}
	diffs, err := VerifySchema(context.Background(), catalogue{changed}, release)
	if err == nil {
		t.Fatal("a database changed by hand passed")
	}
	for _, want := range []string{
		"fyniq_ai holds INSERT on journal_entry, which the release does not grant",
		"fyniq_api holds UPDATE on journal_line, which the release does not grant",
		"the trigger require_audit on account is missing",
		"row-level security on account now enabled and not forced",
		"row-level security on account should be enabled and forced",
	} {
		if !slices.Contains(diffs, want) || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q among %q", want, diffs)
		}
	}
	if len(diffs) != 5 {
		t.Errorf("want 5 differences, got %d: %q", len(diffs), diffs)
	}
	// A grant the release makes and the database lost is a difference too.
	lost := release
	lost.Grants = release.Grants[:2]
	if diffs := CompareGuards(release, lost); !slices.Equal(diffs, []string{"fyniq_api no longer holds INSERT on account (code, name), which the release grants"}) {
		t.Errorf("a lost grant: %q", diffs)
	}
}
