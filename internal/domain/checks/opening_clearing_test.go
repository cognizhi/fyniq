//go:build local

package checks_test

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/checks"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// C.2 #63 (FR-GL-06; rev 0.257): 3900 Opening balance clearing holds nothing before anything posts to it; an opening
// invoice of S$1,350.00 posted without the opening balances that carry it leaves 3900 at S$-1,350.00, a warning naming
// the account; once the opening balances carry the receivable on 3900 instead of 1100, it nets to zero again.
// req: C.2 #63
// req: FR-GL-06
func TestTheOpeningBalanceClearingAccountNetsToZero(t *testing.T) {
	b := newBooks(t, true)
	want(t, b.run(t), "books.opening-clearing", checks.OK)

	b.post(t, "opening-item", porttest.Day(10, 1), "1100", "3900", "1350.00")
	s := want(t, b.run(t), "books.opening-clearing", checks.Warning)
	items := itemsOf(t, s)
	if len(items) != 1 || items[0].ID != b.codes["3900"].ID.String() || !strings.Contains(items[0].Detail, "-1350.00") ||
		!strings.Contains(s.Detail, "3900 Opening balance clearing holds -1350.00 SGD") {
		t.Errorf("the clearing account's warning: %q %+v", s.Detail, items)
	}

	b.post(t, "opening-balances", porttest.Day(10, 1), "3900", "3100", "1350.00")
	want(t, b.run(t), "books.opening-clearing", checks.OK)
}
