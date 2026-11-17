package evidence

import (
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func doc(payload string) storage.Document { return storage.Document{Payload: []byte(payload)} }

// A record's transactions complete in the last month a line spreads into, else on its own date (FR-DOC-02).
// req: FR-DOC-02
func TestCompletion(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	for _, c := range []struct {
		payload string
		want    time.Time
	}{
		{`{"date":"2026-12-20","lines":[{"net":"100.00"}]}`, day("2026-12-20")},
		{`{"date":"2027-03-01","lines":[{"spread_months":36,"spread_from":"2027-01"},{"spread_months":12}]}`, day("2029-12-31")},
		{`{"date":"2027-03-10","lines":[{"spread_months":12}]}`, day("2028-02-29")}, // from the record's own month
		{`{"date":"2026-10-14","lines":[{"spread_months":3,"spread_from":"2026-09-20"}]}`, day("2026-11-30")}, // a day in its first month
		{`{"date":"2027-03-10","lines":[{"spread_months":3,"spread_from":"bad"}]}`, day("2027-03-10")},
		{`not json`, time.Time{}},
	} {
		if got := Completion(doc(c.payload)); !got.Equal(c.want) {
			t.Errorf("Completion(%s) = %s; want %s", c.payload, got, c.want)
		}
	}
}

// G4-068: a bill of a December year uploaded on 4 January is kept at least until its year's end plus the retention; a
// contract spread over 2027 to 2029 and uploaded in 2027 until the year its transactions complete plus the retention.
// req: G4-068
func TestRetentionFollowsTheRecordsObligations(t *testing.T) {
	sgt := time.FixedZone("SGT", 8*3600)
	bill := doc(`{"date":"2026-12-20","lines":[{"net":"500.00"}]}`)
	got := retainUntilOf(1, 7, time.Date(2027, 1, 4, 10, 0, 0, 0, sgt), bill, sgt)
	if floor := time.Date(2034, 1, 1, 0, 0, 0, 0, sgt); got.Before(floor) {
		t.Errorf("the bill is kept until %s, before 31 Dec 2026 plus 7 years", got)
	}
	contract := doc(`{"date":"2027-01-05","lines":[{"spread_months":36,"spread_from":"2027-01"}]}`)
	if got, want := retainUntilOf(1, 7, time.Date(2027, 1, 5, 9, 0, 0, 0, sgt), contract, sgt), time.Date(2037, 1, 1, 0, 0, 0, 0, sgt); !got.Equal(want) {
		t.Errorf("the contract is kept until %s; want the end of 2029 plus 7 years, %s", got, want)
	}
	longer := doc(`{"date":"2027-01-05","lines":[{"spread_months":48,"spread_from":"2027-01"}]}`)
	if got := retainUntilOf(1, 7, time.Date(2027, 1, 5, 9, 0, 0, 0, sgt), longer, sgt); !got.Equal(time.Date(2038, 1, 1, 0, 0, 0, 0, sgt)) {
		t.Errorf("a contract lengthened to 2030 is kept until %s", got)
	}
}
