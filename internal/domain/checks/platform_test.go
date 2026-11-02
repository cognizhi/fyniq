package checks

import (
	"slices"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// C.2 #37's rates (rev 0.166, the owner's decision of 2 Nov 2026): over 30 days, with 50 or more sent, hard bounces at
// 2% or more and complaints at 0.1% or more; with fewer sent, any complaint.
// req: C.2 #37
func TestTheEmailRates(t *testing.T) {
	for _, c := range []struct {
		counts storage.EmailCounts
		want   []string
	}{
		{storage.EmailCounts{Sent: 49, HardBounces: 5}, nil},
		{storage.EmailCounts{Sent: 49, Complaints: 1}, []string{"complaints"}},
		{storage.EmailCounts{Sent: 50, HardBounces: 1}, []string{"hard_bounces"}},
		{storage.EmailCounts{Sent: 51, HardBounces: 1}, nil},
		{storage.EmailCounts{Sent: 1000, Complaints: 1}, []string{"complaints"}},
		{storage.EmailCounts{Sent: 1001, Complaints: 1}, nil},
		{storage.EmailCounts{Sent: 100, HardBounces: 2, Complaints: 1}, []string{"hard_bounces", "complaints"}},
		{storage.EmailCounts{}, nil},
	} {
		var got []string
		for _, it := range emailRates(c.counts) {
			got = append(got, it.ID)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%+v: %v, want %v", c.counts, got, c.want)
		}
	}
}
