package agents_test

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/agents"
)

// A run's summary says what needs a person, the nearest first, and never "all clear" while anything is due, blocked,
// to review or Not assessed (AI-65; G4-103, G4-110).
// req: AI-65
// req: G4-110
func TestASummaryIsNeverAnAllClearWithAnythingOpen(t *testing.T) {
	due := agents.Finding{Key: "calendar:gst_f5:2026-12-31", Status: agents.FindingDue, Title: "GST return for October to December", DueOn: "2027-01-31"}
	later := agents.Finding{Key: "calendar:annual_return:2026-12-31", Status: agents.FindingDue, Title: "Annual return", DueOn: "2027-07-31"}
	na := agents.Finding{Key: "check:corporate.secretary", Status: agents.FindingNotAssessed, Title: "The company has its secretary"}
	ok := agents.Finding{Key: "check:gst.return-due", Status: agents.FindingOK, Title: "GST returns"}
	for _, c := range []struct {
		findings []agents.Finding
		want     string
	}{
		{[]agents.Finding{later, due, na}, "2 things need you; the nearest is GST return for October to December, due 31 January 2027. 1 item is not assessed yet, so this is not an all clear."},
		{[]agents.Finding{ok, na}, "Nothing is due now. 1 item is not assessed yet, so this is not an all clear."},
		{[]agents.Finding{ok}, "Nothing needs you now."},
	} {
		if got := agents.Summary(c.findings); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
	}
	if s := agents.Summary([]agents.Finding{na}); strings.Contains(s, "all clear") && !strings.Contains(s, "not an all clear") {
		t.Errorf("an all clear with an item not assessed: %q", s)
	}
}

// A run's inputs are its findings' keys, statuses and windows, and the packs read (AI-64): a deadline crossing into a
// window changes them with no data change, and the same findings in another order do not.
// req: AI-64
// req: G4-076
func TestTheInputsChangeWhenADeadlineCrossesAWindow(t *testing.T) {
	packs := map[string]string{"sg-deadlines": "sg-deadlines@2026.1"}
	a := agents.Finding{Key: "calendar:gst_f5:2026-12-31", Status: agents.FindingDue, Window: "14", DueOn: "2027-01-31"}
	b := agents.Finding{Key: "check:corporate.secretary", Status: agents.FindingNotAssessed}
	base := agents.Fingerprint([]agents.Finding{a, b}, packs)
	if agents.Fingerprint([]agents.Finding{b, a}, packs) != base {
		t.Error("the same findings in another order changed the inputs")
	}
	crossed := a
	crossed.Window = "7"
	if agents.Fingerprint([]agents.Finding{crossed, b}, packs) == base {
		t.Error("a deadline crossing into the 7-day window left the inputs unchanged")
	}
	if agents.Fingerprint([]agents.Finding{a, b}, map[string]string{"sg-deadlines": "sg-deadlines@2027.1"}) == base {
		t.Error("a new pack version left the inputs unchanged")
	}
}
