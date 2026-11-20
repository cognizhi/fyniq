//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/notify/reminders"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
)

// FR-AP-08 (p2-bank-feeds-payments task 3.2): a cardholder is told once a day, in one notice, of the lines of their own card that have waited
// longer than claims.receipt_reminder_days (3) for a receipt; a line completed with its receipt, and a day within the limit, tell nothing, and
// nobody else is told.
// req: FR-AP-08
// req: FR-NTF-01
func TestACardholderIsToldOnceADayOfTheirLinesWithNoReceipt(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	travel, _ := e.claimSetup()
	ahkow := c.as(c.ahkow, authz.Employee)
	card := e.cardOf(c.ahkow)
	taxi := e.lineOn(card, "-27.30", nil)
	e.lineOn(card, "-12.80", nil) // booked 20 October 2026
	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	remind := func(on time.Time) int {
		t.Helper()
		d := outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: clock.NewFrozen(on)}
		n, err := reminders.CardReceiptsIn(context.Background(), worker, d, e.books.Org)
		if err != nil {
			t.Fatalf("the reminders on %s: %v", on.Format(time.DateOnly), err)
		}
		return n
	}
	noon := 4 * time.Hour // 12:00 Singapore time
	if n := remind(porttest.Day(10, 22).Add(noon)); n != 0 {
		t.Errorf("two days after the booking: %d notices", n)
	}
	if n := remind(porttest.Day(10, 24).Add(noon)); n != 1 {
		t.Errorf("four days after: %d notices, want 1 for both lines", n)
	}
	if n := remind(porttest.Day(10, 24).Add(noon + time.Hour)); n != 0 {
		t.Errorf("the same day again: %d notices", n)
	}
	// Completed with its receipt, the taxi no longer counts; the meal, still waiting, is told the next day.
	done := e.call(http.MethodPost, "/v1/card-lines/"+taxi.ID.String()+"/complete", ahkow, map[string]any{"category_id": travel, "gst_code": "NR", "description": "Taxi"},
		"If-Match", etag(taxi.Version))
	if done.Status != 200 {
		t.Fatalf("completing the taxi: %d %s", done.Status, done.Raw)
	}
	// Completed but with no file: still no receipt.
	if n := remind(porttest.Day(10, 25).Add(noon)); n != 1 {
		t.Errorf("the next day, the taxi without its file and the meal: %d notices", n)
	}
	if r := e.upload("/v1/card-lines/"+taxi.ID.String()+"/evidence", ahkow, map[string]string{"evidence_type": "receipt"}, "taxi.pdf",
		[]byte("%PDF-1.7 ComfortDelGro, S$27.30"), "If-Match", done.Header.Get("ETag")); r.Status != 201 {
		t.Fatalf("attaching the receipt: %d %s", r.Status, r.Raw)
	}
	if n := remind(porttest.Day(10, 26).Add(noon)); n != 1 {
		t.Errorf("the day after the receipt: %d notices (the meal waits)", n)
	}
	trail := auditTrail(t, e)
	if !strings.Contains(trail, "2 lines of ") || !strings.Contains(trail, " have no receipt yet, the oldest from 20 Oct. Complete each with its receipt.") ||
		!strings.Contains(trail, "1 line of ") || !strings.Contains(trail, " has no receipt yet, the oldest from 20 Oct. Complete it with its receipt.") {
		t.Errorf("the notices: %s", trail)
	}
}
