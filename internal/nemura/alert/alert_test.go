package alert

import (
	"testing"
	"time"
)

// Only a job under payday. is the pay-day path's; the notice is due an hour after the job died (NM-NFR-05).
// req: NM-NFR-05
func TestOnlyPayDayJobsAreThePayDayPath(t *testing.T) {
	for kind, want := range map[string]bool{"payday.handoff": true, "payday.payment_file": true, "payday.": false, "payday": false, "paydays.x": false, "test.work": false, "leave.accrual": false} {
		if IsPayDay(kind) != want {
			t.Errorf("%s: %v", kind, !want)
		}
	}
	died := time.Date(2099, 1, 1, 9, 0, 0, 0, time.UTC)
	if due := (Payload{FailedAt: died}).DueBy(); !due.Equal(died.Add(time.Hour)) {
		t.Errorf("due %v", due)
	}
	if len(Recipients) != 2 || Recipients[0] != "payroll_admin" || Recipients[1] != "owner" {
		t.Errorf("recipients %v", Recipients)
	}
}
