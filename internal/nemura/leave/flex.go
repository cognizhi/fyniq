package leave

import "time"

// The arrangements a flexible work request can ask for (the Tripartite Guidelines name flexi-time, flexi-place and flexi-load).
const (
	FlexiTime  = "flexi_time"
	FlexiPlace = "flexi_place"
	FlexiLoad  = "flexi_load"
)

// Arrangements lists them.
var Arrangements = []string{FlexiTime, FlexiPlace, FlexiLoad}

// Outcomes of the employer's written reply.
const (
	Accepted    = "accepted"
	Rejected    = "rejected"
	Alternative = "alternative"
)

// AddMonths is a day this many months on, clamped to the month's last day: two months after 31 December is 28 February, never 3 March, so a deadline is never later
// than the guidelines give.
func AddMonths(d time.Time, months int) time.Time {
	y, m := d.Year(), int(d.Month())+months
	y += (m - 1) / 12
	m = (m-1)%12 + 1
	last := time.Date(y, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(y, time.Month(m), min(d.Day(), last), 0, 0, 0, 0, time.UTC)
}

// ReplyDue is the last day for the employer's written reply: the pack's months after the formal receipt (NM-LV-10). A clarification or a change of type never
// restarts it.
func ReplyDue(received time.Time, months int) time.Time { return AddMonths(received, months) }

// CheckReply validates the employer's reply: every reply is written, and a refusal or an alternative gives its reason (the business grounds).
func CheckReply(outcome, written string) error {
	switch outcome {
	case Accepted, Rejected, Alternative:
	default:
		return invalid("The outcome is accepted, rejected or alternative: choose one.")
	}
	if len(trimmed(written)) == 0 || len(written) > 4000 {
		return invalid("The written reply is required and at most 4,000 characters: write the decision and, for a refusal or an alternative, the reason.")
	}
	if outcome != Accepted && len(trimmed(written)) < 20 {
		return invalid("A refusal or an alternative gives its reason: write the business grounds, so the person can understand the decision.")
	}
	return nil
}

func trimmed(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
