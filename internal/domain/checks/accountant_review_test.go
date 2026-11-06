//go:build local

package checks_test

import (
	"context"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// C.2 #61: in Founder mode, a calendar quarter ended more than 60 days ago with no signed-off accountant review of its
// last month warns; one ended 60 days ago or less does not yet (FM-7; the owner's decision of 6 Nov 2026). On 10 Nov
// 2026, with books from June, Apr–Jun is overdue (due by 29 Aug) and Jul–Sep is not yet (due by 29 Nov).
// req: C.2 #61
// req: FM-7
// req: controls/platform-checks A quarter unreviewed after 60 days
func TestAQuarterWithoutTheAccountantsReviewWarns(t *testing.T) {
	b := newBooks(t, true)
	const key = "controls.accountant-review-overdue"
	if err := b.api.Do(ctx, b.org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Create(ctx, storage.NewPeriod{StartsOn: porttest.Day(6, 1), EndsOn: porttest.Day(6, 30)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s := want(t, b.run(t), key, checks.Warning)
	items := itemsOf(t, s)
	if len(items) != 1 || items[0].Label != "Apr–Jun 2026 accountant review" || items[0].Detail != "No signed-off review of June 2026; due by 29 Aug 2026" {
		t.Errorf("the items: %+v", items)
	}
}
