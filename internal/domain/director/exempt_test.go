package director

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// FR-EQ-11 (design D9; rev 0.167), worked by hand: the company is recorded exempt from 14 Mar 2025, and not exempt from
// 1 Sep 2026, when a corporate investor joined. Before 14 Mar 2025 nothing applies; until 31 Aug 2026 it is exempt;
// from 1 Sep 2026 it is not. A director's account in debit says the loan is allowed while exempt, and cites s162 and
// its exception while not exempt or when nothing is recorded.
// req: FR-EQ-11
// req: equity/founders-money Message for a company that is not exempt
// req: equity/founders-money No status recorded (rev 0.167)
// req: equity/founders-money Status recorded before the corporate profile (rev 0.167)
func TestTheExemptStatusOnADayAndItsWarning(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	statuses := []RecordedStatus{
		{ID: uuid.New(), Status: ExemptStatus{From: day(2025, time.March, 14), Exempt: true}, RecordedAt: day(2025, time.March, 15)},
		{ID: uuid.New(), Status: ExemptStatus{From: day(2026, time.September, 1), Exempt: false}, RecordedAt: day(2026, time.September, 2)},
	}
	if s := ExemptOn(statuses, day(2025, time.March, 13)); s != nil {
		t.Errorf("before the first status: %v", s.Status)
	}
	if s := ExemptOn(statuses, day(2026, time.August, 31)); s == nil || !s.Status.Exempt {
		t.Errorf("on 31 Aug 2026: %v, want exempt", s)
	}
	if s := ExemptOn(statuses, day(2026, time.September, 1)); s == nil || s.Status.Exempt {
		t.Errorf("on 1 Sep 2026: %v, want not exempt", s)
	}

	exempt := DebitWarning(ExemptOn(statuses, day(2026, time.August, 31)))
	notExempt := DebitWarning(ExemptOn(statuses, day(2026, time.October, 1)))
	none := DebitWarning(nil)
	if !strings.Contains(exempt, "exempt private company, so a loan to a director is allowed") || strings.Contains(exempt, "bans loans") {
		t.Errorf("exempt: %s", exempt)
	}
	for name, w := range map[string]string{"not exempt": notExempt, "none recorded": none} {
		for _, want := range []string{"section 162 of the Companies Act bans loans to directors", "approved in general meeting", "repaid within 6 months after the next AGM"} {
			if !strings.Contains(w, want) {
				t.Errorf("%s: %q lacks %q", name, w, want)
			}
		}
	}
	if !strings.HasPrefix(none, "No exempt-private-company status is recorded") {
		t.Errorf("none recorded: %s", none)
	}
}
