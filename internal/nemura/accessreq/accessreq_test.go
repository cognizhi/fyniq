package accessreq

import (
	"errors"
	"testing"
	"time"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// The answer is due 30 days after the request is received, or the person is told in writing within them that it will take longer
// (CMP-PD-04); an answered request, or one with notice in time, is not overdue (NM-SEC-07).
// req: NM-SEC-07
func TestTheAnswerIsDueIn30DaysOrAWrittenNoticeIs(t *testing.T) {
	received := day(2099, 3, 1)
	due := Due(received)
	if !due.Equal(day(2099, 3, 31)) {
		t.Fatalf("due %s", due)
	}
	inTime, late := day(2099, 3, 20), day(2099, 4, 5)
	for name, tc := range map[string]struct {
		now      time.Time
		notice   *time.Time
		answered bool
		want     bool
	}{
		"on the day":              {day(2099, 3, 31), nil, false, false},
		"a day late":              {day(2099, 4, 1), nil, false, true},
		"answered":                {day(2099, 4, 20), nil, true, false},
		"notice in time":          {day(2099, 4, 20), &inTime, false, false},
		"notice after it was due": {day(2099, 4, 20), &late, false, true},
	} {
		if got := Overdue(due, tc.now, tc.notice, tc.answered); got != tc.want {
			t.Errorf("%s: overdue %v, want %v", name, got, tc.want)
		}
	}
}

// A withheld category names one of the recorded exceptions; the other decisions name none; anything else is refused with a plain fix.
// req: NM-SEC-07
func TestWithholdingNeedsARecordedException(t *testing.T) {
	for _, tc := range []struct {
		d  Decision
		ex string
		ok bool
	}{
		{Release, "", true}, {Redact, "", true}, {Withhold, Exceptions[0], true}, {Withhold, "  " + Exceptions[1] + " ", true},
		{Withhold, "", false}, {Withhold, "I would rather not", false}, {Release, Exceptions[0], false}, {"delete", "", false},
	} {
		err := Check(tc.d, tc.ex)
		var r *Refusal
		if (err == nil) != tc.ok || (err != nil && (!errors.As(err, &r) || r.Code != CodeInvalid || r.Message == "")) {
			t.Errorf("%s %q: %v, want ok=%v", tc.d, tc.ex, err, tc.ok)
		}
	}
}
