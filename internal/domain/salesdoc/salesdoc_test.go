package salesdoc

import (
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A document's delivery comes from its latest send (FR-AR-06; the owner's decision of 24 Oct 2026): not sent before
// one; sent while the message waits or the provider took it and says no more, as ZeptoMail never reports delivered
// (FR-INT-21); delivered on a delivery or a complaint; bounced when the provider refused it, or on a bounce, a
// suppression or a failure; a later delivery after a soft bounce counts.
// req: FR-AR-06
// req: FR-INT-21
func TestDeliveryFollowsTheLatestSend(t *testing.T) {
	send := func(status, event string) *storage.DocumentSend {
		return &storage.DocumentSend{MessageStatus: status, LastEvent: event}
	}
	for name, c := range map[string]struct {
		s    *storage.DocumentSend
		want string
	}{
		"no send":                   {nil, NotSent},
		"queued":                    {send(storage.EmailQueued, ""), Sent},
		"accepted, nothing more":    {send(storage.EmailAccepted, ""), Sent},
		"unknown":                   {send(storage.EmailUnknown, ""), Sent},
		"delayed":                   {send(storage.EmailAccepted, storage.EventDelayed), Sent},
		"delivered":                 {send(storage.EmailAccepted, storage.EventDelivered), Delivered},
		"complained":                {send(storage.EmailAccepted, storage.EventComplaint), Delivered},
		"refused by every provider": {send(storage.EmailFailed, ""), Bounced},
		"hard bounce":               {send(storage.EmailAccepted, storage.EventHardBounce), Bounced},
		"soft bounce":               {send(storage.EmailAccepted, storage.EventSoftBounce), Bounced},
		"suppressed":                {send(storage.EmailAccepted, storage.EventSuppressed), Bounced},
		"failed":                    {send(storage.EmailAccepted, storage.EventFailed), Bounced},
	} {
		if got := Delivery(c.s); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

// A rate shows as a percentage: 9%, 8.5%, 0% for zero-rated; anything unreadable as it was.
// req: FR-AR-05
func TestRatesShowAsPercentages(t *testing.T) {
	for rate, want := range map[string]string{"0.09": "9%", "0.085": "8.5%", "0": "0%", "0.0700": "7%", "x": "x"} {
		if got := percent(rate); got != want {
			t.Errorf("%s: %s, want %s", rate, got, want)
		}
	}
}
