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
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The calendar's checks and reminders (FR-TAX-07, CMP-02, C.2 #13, C.2 #15, FR-NTF-01; p1-gst design D7, D9, D10), worked
// by hand. Registered for GST monthly from 1 Oct 2026, October's F5 is due on 30 Nov 2026, and November's on 31 Dec 2026,
// which IRAS's one-off extension moves to 9 Jan 2027. Paying staff from November, its CPF is due on 14 Dec 2026. On 23 Nov
// October's return is due in 7 days: C.2 #13 lists it and the reminder goes out, once however often the job runs. On 30
// Nov November's CPF is 14 days away: C.2 #15 lists it, with its reminder. On 1 Dec October's return is overdue: the
// overdue notice goes out once. Recorded as filed, C.2 #13 is clear. The founder who holds every share since 2 Jan 2025
// was to be entered in the register of controllers by 9 Jan 2025: the calendar lists it from the register, linked to
// the holder, and C.2 #15 leaves it to C.2 #27.
// req: FR-TAX-07
// req: CMP-02
// req: C.2 #13
// req: C.2 #15
// req: FR-NTF-01
// req: FR-EQ-25
func TestTheCalendarsChecksAndReminders(t *testing.T) {
	f := newFounders(t)
	e := f.e
	owner := f.as(f.ahkow, authz.Owner, authz.Accountant, authz.Controller)
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		if _, err := u.UpdateOrganisation(ctx, o.Version, pr); err != nil {
			return err
		}
		if _, err := u.Settings().Record(ctx, "calendar.pays_staff", "false", "true", porttest.Day(11, 1), 1); err != nil {
			return err
		}
		_, err = u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "M90000001X",
			ReturnFrequency: storage.ReturnMonthly, QuarterStartMonth: 1, AccountingBasis: storage.BasisInvoice, RecordedAt: e.clock.Now()})
		return err
	})
	founder := e.directorContact("Ah Kow")
	if r := e.call(http.MethodPost, "/v1/share-issues", owner, map[string]any{"holder_id": founder, "class_account_id": e.shareCapitalIn(e.books.Org),
		"shares": "1000000", "price": "0.01", "consideration": "cash", "resolution_refs": "Allotment resolution", "effective_on": "2025-01-02"}); r.Status != http.StatusCreated {
		t.Fatalf("the founder's shares: %d %s", r.Status, r.Raw)
	}

	worker, err := postgres.Open(context.Background(), db.URL("fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	remind := func(on time.Time) int {
		t.Helper()
		d := outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: clock.NewFrozen(on)}
		n, err := reminders.DeadlinesDueIn(context.Background(), worker, d, e.books.Org)
		if err != nil {
			t.Fatalf("the reminders on %s: %v", on.Format(time.DateOnly), err)
		}
		return n
	}
	at := func(day time.Time) { e.clock.Set(day.Add(9 * time.Hour)) }

	// 23 Nov: October's return due in 7 days.
	at(porttest.Day(11, 23))
	all := strings.Join(calendarItems(t, e.call(http.MethodGet, "/v1/compliance/calendar", owner, nil)), "|")
	for _, w := range []string{"gst_f5:2026-10-31 2026-11-30 due_soon", "gst_f5:2026-11-30 2027-01-09 open", "cpf:2026-11-30 2026-12-14 open",
		"register_controller_entry:" + founder + ":2025-01-09 2025-01-09 overdue"} {
		if !strings.Contains(all, w) {
			t.Errorf("on 23 Nov: no %q in %s", w, all)
		}
	}
	c := e.checked("gst.return-due")
	wantStatus(t, c, "due")
	if items := itemsOf(c); len(items) != 1 || items[0]["id"] != "gst_f5:2026-10-31" || !strings.Contains(items[0]["detail"].(string), "Due on 30 Nov 2026, in 7 day(s)") {
		t.Errorf("C.2 #13 on 23 Nov: %s", c.Raw)
	}
	wantStatus(t, e.checked("compliance.deadlines"), "ok")
	if n := remind(porttest.Day(11, 23).Add(time.Hour)); n != 1 {
		t.Errorf("the reminders on 23 Nov: %d, want October's return in 7 days", n)
	}
	if n := remind(porttest.Day(11, 23).Add(2 * time.Hour)); n != 0 {
		t.Errorf("the reminders again on 23 Nov: %d, want none", n)
	}

	// 30 Nov: November's CPF 14 days away, October's return due today.
	at(porttest.Day(11, 30))
	c = e.checked("compliance.deadlines")
	wantStatus(t, c, "due")
	if items := itemsOf(c); len(items) != 1 || items[0]["id"] != "cpf:2026-11-30" || !strings.Contains(items[0]["detail"].(string), "Due on 14 Dec 2026, in 14 day(s)") {
		t.Errorf("C.2 #15 on 30 Nov: %s", c.Raw)
	}
	if n := remind(porttest.Day(11, 30).Add(time.Hour)); n != 1 {
		t.Errorf("the reminders on 30 Nov: %d, want November's CPF in 14 days", n)
	}

	// 1 Dec: October's return overdue, told once.
	at(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	if n := remind(time.Date(2026, 12, 1, 1, 0, 0, 0, time.UTC)); n != 1 {
		t.Errorf("the reminders on 1 Dec: %d, want October's return overdue", n)
	}
	if n := remind(time.Date(2026, 12, 1, 2, 0, 0, 0, time.UTC)); n != 0 {
		t.Errorf("the reminders again on 1 Dec: %d", n)
	}
	trail := auditTrail(t, e)
	if strings.Count(trail, `"deadline.reminder"`) != 2 || strings.Count(trail, `"deadline.overdue"`) != 1 ||
		!strings.Contains(trail, "GST F5 return for 1 Oct 2026 to 31 Oct 2026: due in 7 days, on 30 Nov 2026") ||
		!strings.Contains(trail, "GST F5 return for 1 Oct 2026 to 31 Oct 2026: it was due yesterday, 30 Nov 2026, and nothing is recorded") {
		t.Errorf("the notices: %d reminders, %d overdue", strings.Count(trail, `"deadline.reminder"`), strings.Count(trail, `"deadline.overdue"`))
	}
	c = e.checked("gst.return-due")
	if items := itemsOf(c); len(items) != 1 || !strings.Contains(items[0]["detail"].(string), "Due on 30 Nov 2026, 1 day(s) ago") {
		t.Errorf("C.2 #13 on 1 Dec: %s", c.Raw)
	}

	// Recorded as filed, C.2 #13 is clear.
	d := e.call(http.MethodPost, "/v1/compliance/filings", owner, map[string]any{"key": "gst_f5:2026-10-31", "outcome": "filed", "filed_on": "2026-12-01", "reference": "F5-OCT"})
	up := e.upload("/v1/compliance/filings/"+d.str("id")+"/evidence", owner, map[string]string{"evidence_type": "IRAS acknowledgement"}, "ack.pdf",
		[]byte("%PDF-1.7 F5 acknowledgement"), "If-Match", d.Header.Get("ETag"))
	cur := e.call(http.MethodGet, "/v1/compliance/filings/"+d.str("id"), owner, nil)
	if r := e.call(http.MethodPost, "/v1/compliance/filings/"+d.str("id")+"/record", owner, nil, "If-Match", cur.Header.Get("ETag")); up.Status != 201 || r.Status != http.StatusOK {
		t.Fatalf("recording October's return: %d %d %s", up.Status, r.Status, r.Raw)
	}
	wantStatus(t, e.checked("gst.return-due"), "ok")
}
