package capture

import (
	"time"

	"github.com/cockroachdb/apd/v3"
)

// SuggestDays is how many days the date a receipt shows may differ from the day a card line was booked and still be suggested for it
// (PRD-001 FR-AP-08, rev 0.549): a card line is booked a day or two after the purchase, and a receipt is sometimes dated the day after.
const SuggestDays = 5

// SuggestsFor reports whether a read document's reading is a receipt worth suggesting for a card line of amount (negative for spend) in
// currency, booked on booking: the same amount to the cent, in the same currency when the reading names one, dated within SuggestDays
// of the booking, of a kind a receipt or bill has. It never attaches anything: a person confirms (FR-AP-08, golden rule 5).
func SuggestsFor(r Reading, amount, currency string, booking time.Time) bool {
	if r.Kind != "receipt" && r.Kind != "bill" {
		return false
	}
	if r.Total.Value == nil || r.Date.Value == nil {
		return false
	}
	if r.Currency.Value != nil && *r.Currency.Value != currency {
		return false
	}
	want, _, err := apd.NewFromString(amount)
	if err != nil {
		return false
	}
	got, _, err := apd.NewFromString(*r.Total.Value)
	if err != nil {
		return false
	}
	var spent apd.Decimal
	if _, err := apd.BaseContext.Abs(&spent, want); err != nil {
		return false
	}
	var seen apd.Decimal
	if _, err := apd.BaseContext.Abs(&seen, got); err != nil {
		return false
	}
	if spent.Cmp(&seen) != 0 {
		return false
	}
	day, err := time.Parse(time.DateOnly, *r.Date.Value)
	if err != nil {
		return false
	}
	gap := day.Sub(time.Date(booking.Year(), booking.Month(), booking.Day(), 0, 0, 0, 0, time.UTC)).Hours() / 24
	return gap >= -SuggestDays && gap <= SuggestDays
}
