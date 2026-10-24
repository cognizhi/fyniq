package salesdoc

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
		if got := Delivery(c.s, false); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
	if got := Delivery(send(storage.EmailAccepted, storage.EventSoftBounce), true); got != Viewed {
		t.Errorf("a send whose link was opened: %s, want viewed", got)
	}
	if got := Delivery(nil, true); got != NotSent {
		t.Errorf("no send: %s", got)
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

// A link's token is 32 random bytes in base64url (43 characters), different each time, and only its SHA-256 is kept
// (the threat model's T1, T2); anything else is refused before a lookup: too short or long, padded, or not base64url.
// req: FR-AR-06
// asvs: V7.2.3
func TestALinkTokenIsRandomAndKeptHashed(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		token, hash, err := NewToken()
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(token))
		if len(token) != 43 || hash != hex.EncodeToString(sum[:]) || seen[token] {
			t.Fatalf("token %q, hash %s", token, hash)
		}
		if h, ok := TokenHash(token); !ok || h != hash {
			t.Fatalf("TokenHash(%q) = %s, %v", token, h, ok)
		}
		seen[token] = true
	}
	good, _, _ := NewToken()
	for _, bad := range []string{"", "abc", good + "=", good + "A", good[:42], strings.Replace(good, good[:1], "+", 1), strings.Replace(good, good[:1], "/", 1),
		"../../etc/passwd", good[:42] + "\n"} {
		if _, ok := TokenHash(bad); ok {
			t.Errorf("%q passed", bad)
		}
	}
}

// A link opens its document while it is live: not revoked, before its expiry, its document posted at the link's
// version, and an invoice not paid in full.
// req: FR-AR-06
func TestALinkIsLiveUntilItEnds(t *testing.T) {
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	doc := storage.Document{ID: uuid.Must(uuid.NewV7()), Version: 1, Status: storage.DocumentPosted}
	link := storage.DocumentLink{DocumentID: doc.ID, DocumentVersion: 1, ExpiresAt: now.AddDate(0, 0, LinkDays)}
	revoked := link
	revoked.RevokedAt = &now
	edited, voided := doc, doc
	edited.Version, voided.Status = 2, storage.DocumentReversed
	for name, c := range map[string]struct {
		l       storage.DocumentLink
		d       storage.Document
		settled bool
		at      time.Time
		want    bool
	}{
		"live":                {link, doc, false, now, true},
		"revoked":             {revoked, doc, false, now, false},
		"on its expiry":       {link, doc, false, link.ExpiresAt, false},
		"its document edited": {link, edited, false, now, false},
		"its document voided": {link, voided, false, now, false},
		"an invoice paid":     {link, doc, true, now, false},
		"another document":    {link, storage.Document{ID: uuid.Must(uuid.NewV7()), Version: 1, Status: storage.DocumentPosted}, false, now, false},
	} {
		if got := Live(c.l, c.d, c.settled, c.at); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}
