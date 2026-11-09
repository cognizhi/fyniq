//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// calendarItems are a calendar's items as "key due status", in its order.
func calendarItems(t *testing.T, r response) []string {
	t.Helper()
	if r.Status != http.StatusOK {
		t.Fatalf("the calendar: %d %s", r.Status, r.Raw)
	}
	var out []string
	for _, x := range r.Body["deadlines"].([]any) {
		it := x.(map[string]any)
		parts := []string{it["key"].(string)}
		if due, _ := it["due_on"].(string); due != "" {
			parts = append(parts, due)
		}
		out = append(out, strings.Join(append(parts, it["status"].(string)), " "))
	}
	return out
}

// The statutory calendar through the API (FR-TAX-07, CMP-02; p1-gst design D5, D6), worked by hand from the
// sg-deadlines pack's rules. On 15 Oct 2026 no version is in force: not assessed. On 20 Nov 2026, with the financial
// year ending 31 Dec 2026: ECI by 31 Mar 2027, the AGM by 30 Jun 2027 and the annual return by 31 Jul 2027; Form C-S's 30
// Nov 2027 is after the window. Paying staff from 1 Nov 2026 adds CPF each month, November's by 14 Dec 2026 and
// January's by Monday 15 Feb 2027 (the 14th a Sunday), and 2026's employment income by 1 Mar 2027. Sending the
// statements instead moves the AGM to the statements by 31 May 2027; an overseas branch register moves the annual return
// to 31 Aug 2027. Registered for GST from 1 Oct 2026, quarterly: F5 by 31 Jan, 30 Apr, 31 Jul and 31 Oct 2027. ECI is
// recorded as filed with its acknowledgement, a second record refused, then withdrawn; 2026's employment income is
// recorded not required. December 2027's CPF falls in a year without public holidays: not assessed.
// req: FR-TAX-07
// req: CMP-02
// req: FR-DOC-06
// req: QA-RPT-01
func TestTheStatutoryCalendarThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	owner := f.as(f.ahkow, authz.Owner, authz.Accountant)
	calendar := func(query string) response {
		return e.call(http.MethodGet, "/v1/compliance/calendar"+query, owner, nil)
	}
	if got := calendarItems(t, calendar("")); len(got) != 1 || !strings.HasPrefix(got[0], "sg-deadlines:2026-10-15") || !strings.HasSuffix(got[0], "not_assessed") {
		t.Errorf("on 15 Oct 2026: %v", got)
	}

	e.clock.Set(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	if got := calendarItems(t, calendar("")); len(got) != 1 || got[0] != "financial_year:- not_assessed" {
		t.Errorf("without financial years: %v", got)
	}
	start, end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	setting := func(key, from, to string, on time.Time) {
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Settings().Record(ctx, key, from, to, on, 1)
			return err
		})
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		pr := o.Profile
		pr.FYStartMonth, pr.BooksStart, pr.FirstYearEnd = 1, &start, &end
		_, err = u.UpdateOrganisation(ctx, o.Version, pr)
		return err
	})
	r := calendar("")
	want := []string{"eci:2026-12-31 2027-03-31 open", "agm:2026-12-31 2027-06-30 open", "annual_return:2026-12-31 2027-07-31 open"}
	if got := calendarItems(t, r); strings.Join(got, "|") != strings.Join(want, "|") || r.str("from") != "2026-11-01" || r.str("to") != "2027-10-31" ||
		r.str("pack") != "sg-deadlines@2026.1" || r.str("starts") != "2026-11-07" {
		t.Errorf("the year's items:\n got %v\nwant %v\n%s", got, want, r.Raw)
	}
	if it := r.Body["deadlines"].([]any)[0].(map[string]any); it["source"] != "iras-eci" || !strings.Contains(it["why"].(string), "within 3 months") || it["area"] != "tax" {
		t.Errorf("ECI's rule: %v", it)
	}

	// Paying staff from 1 Nov 2026.
	setting("calendar.pays_staff", "false", "true", porttest.Day(11, 1))
	got := calendarItems(t, calendar(""))
	for _, w := range []string{"cpf:2026-11-30 2026-12-14 open", "cpf:2027-01-31 2027-02-15 open", "cpf:2027-09-30 2027-10-14 open", "ir8a_ais:2026-12-31 2027-03-01 open"} {
		if !strings.Contains(strings.Join(got, "|"), w) {
			t.Errorf("paying staff: no %q in %v", w, got)
		}
	}
	if n := strings.Count(strings.Join(got, "|"), "cpf:"); n != 11 || strings.Contains(strings.Join(got, "|"), "cpf:2026-10-31") {
		t.Errorf("paying staff: %d CPF items, want November 2026 to September 2027: %v", n, got)
	}

	// The AGM's route and the branch register.
	setting("corporate.agm_route", "hold", "send_statements", porttest.Day(11, 1))
	setting("corporate.overseas_branch_register", "false", "true", porttest.Day(11, 1))
	got = calendarItems(t, calendar("?from=2027-03-01&to=2027-09-30"))
	if all := strings.Join(got, "|"); strings.Contains(all, "agm:") || !strings.Contains(all, "statements_to_members:2026-12-31 2027-05-31 open") ||
		!strings.Contains(all, "annual_return:2026-12-31 2027-08-31 open") {
		t.Errorf("the statements sent, a branch register abroad: %v", got)
	}

	// Registered for GST from 1 Oct 2026, quarterly.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), GSTNumber: "M90000001X",
			ReturnFrequency: storage.ReturnQuarterly, QuarterStartMonth: 1, AccountingBasis: storage.BasisInvoice, RecordedAt: e.clock.Now()})
		return err
	})
	all := strings.Join(calendarItems(t, calendar("")), "|")
	for _, w := range []string{"gst_f5:2026-12-31 2027-01-31 open", "gst_f5:2027-03-31 2027-04-30 open", "gst_f5:2027-06-30 2027-07-31 open", "gst_f5:2027-09-30 2027-10-31 open"} {
		if !strings.Contains(all, w) {
			t.Errorf("GST: no %q in %s", w, all)
		}
	}

	// ECI recorded as filed: a draft without its acknowledgement cannot be recorded.
	body := map[string]any{"key": "eci:2026-12-31", "outcome": "filed", "filed_on": "2026-11-20", "reference": "ECI-2027-000123"}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/filings", e.as(authz.Bookkeeper), body), "createStatutoryFiling", 403, CodeForbidden)
	bad := e.call(http.MethodPost, "/v1/compliance/filings", owner, map[string]any{"key": "lottery:2026-12-31", "outcome": "filed", "filed_on": "2026-11-21"})
	if bad.Status != 422 || strings.Count(bad.Raw, `"code":"CAL-01"`) != 3 {
		t.Errorf("an unknown obligation, filed tomorrow, without a reference: %d %s", bad.Status, bad.Raw)
	}
	d := e.call(http.MethodPost, "/v1/compliance/filings", owner, body)
	if d.Status != http.StatusCreated || d.str("status") != "draft" || !strings.Contains(d.Raw, `"code":"DOC-10"`) {
		t.Fatalf("the draft: %d %s", d.Status, d.Raw)
	}
	id := d.str("id")
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/filings/"+id+"/record", owner, nil, "If-Match", d.Header.Get("ETag")), "recordStatutoryFiling", 409, "DOC-10")
	up := e.upload("/v1/compliance/filings/"+id+"/evidence", owner, map[string]string{"evidence_type": "IRAS acknowledgement"}, "ack.pdf",
		[]byte("%PDF-1.7 acknowledgement"), "If-Match", d.Header.Get("ETag"))
	if up.Status != http.StatusCreated {
		t.Fatalf("the acknowledgement: %d %s", up.Status, up.Raw)
	}
	cur := e.call(http.MethodGet, "/v1/compliance/filings/"+id, owner, nil)
	rec := e.call(http.MethodPost, "/v1/compliance/filings/"+id+"/record", owner, nil, "If-Match", cur.Header.Get("ETag"))
	if rec.Status != http.StatusOK || rec.str("status") != "recorded" || rec.Body["evidence"] != 1.0 {
		t.Fatalf("recording it: %d %s", rec.Status, rec.Raw)
	}
	r = calendar("")
	if it := r.Body["deadlines"].([]any); !strings.Contains(strings.Join(calendarItems(t, r), "|"), "eci:2026-12-31 2027-03-31 filed") {
		t.Errorf("ECI filed: %v", it)
	}
	// A second record of the item is refused while the first is live.
	second := e.call(http.MethodPost, "/v1/compliance/filings", e.as(authz.TaxAgent), map[string]any{"key": "eci:2026-12-31", "outcome": "not_required", "reason": "Waived"})
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/filings/"+second.str("id")+"/record", e.as(authz.TaxAgent), nil, "If-Match", second.Header.Get("ETag")),
		"recordStatutoryFiling", 409, "CAL-02")

	// 2026's employment income is not required: no evidence is asked for.
	ni := e.call(http.MethodPost, "/v1/compliance/filings", owner, map[string]any{"key": "ir8a_ais:2026-12-31", "outcome": "not_required", "reason": "No one was paid in 2026"})
	if r := e.call(http.MethodPost, "/v1/compliance/filings/"+ni.str("id")+"/record", owner, nil, "If-Match", ni.Header.Get("ETag")); r.Status != http.StatusOK {
		t.Fatalf("recording not required: %d %s", r.Status, r.Raw)
	}

	// Under a recommended rule, a filed record may be recorded without its file, and C.2 #30 lists it.
	setting("evidence.statutory_filing", "required", "recommended", porttest.Day(11, 1))
	ar := e.call(http.MethodPost, "/v1/compliance/filings", owner, map[string]any{"key": "annual_return:2026-12-31", "outcome": "filed", "filed_on": "2026-11-20", "reference": "AR-1"})
	if r := e.call(http.MethodPost, "/v1/compliance/filings/"+ar.str("id")+"/record", owner, nil, "If-Match", ar.Header.Get("ETag")); r.Status != http.StatusOK {
		t.Fatalf("recording without its file under a recommended rule: %d %s", r.Status, r.Raw)
	}
	c := e.checked("evidence.missing")
	if items := itemsOf(c); len(items) != 1 || items[0]["kind"] != "statutory_filing" || items[0]["label"] != "annual_return:2026-12-31" {
		t.Errorf("C.2 #30: %s", c.Raw)
	}

	// After its day, withdrawn, ECI is overdue again.
	e.clock.Set(time.Date(2027, 4, 2, 9, 0, 0, 0, time.UTC))
	w := e.call(http.MethodPost, "/v1/compliance/filings/"+id+"/withdraw", owner, map[string]any{"reason": "Recorded against the wrong year"}, "If-Match", rec.Header.Get("ETag"))
	if w.Status != http.StatusOK || w.str("status") != "withdrawal" || w.str("withdraws") != id || w.str("reason") != "Recorded against the wrong year" {
		t.Fatalf("withdrawing it: %d %s", w.Status, w.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/compliance/filings/"+id+"/withdraw", owner, map[string]any{"reason": "Again"}, "If-Match", "*"), "withdrawStatutoryFiling", 409, "CAL-03")
	all = strings.Join(calendarItems(t, calendar("?from=2027-04-01&to=2027-12-31")), "|")
	if !strings.Contains(all, "eci:2026-12-31 2027-03-31 overdue") || strings.Contains(all, "ir8a_ais:") {
		t.Errorf("after its day, withdrawn: %s", all)
	}
	if h := e.call(http.MethodGet, "/v1/compliance/filings?key=eci:2026-12-31", owner, nil); len(h.Body["items"].([]any)) != 3 {
		t.Errorf("ECI's history: %s", h.Raw)
	}

	// December 2027's CPF falls on 14 Jan 2028, a year without public holidays in the pack.
	all = strings.Join(calendarItems(t, calendar("?from=2027-11-01&to=2028-03-31")), "|")
	if !strings.Contains(all, "cpf:2027-12-31 not_assessed") {
		t.Errorf("CPF in 2028: %s", all)
	}
	e.problemIs(calendar("?from=2026-11-01&to=2028-06-01"), "getStatutoryCalendar", 422, "CAL-04")
	if r := e.call(http.MethodGet, "/v1/compliance/calendar", e.as(authz.DPO), nil); r.Status != http.StatusOK {
		t.Errorf("the DPO views the calendar: %d", r.Status)
	}
}

// filingFixture is the matrix's filing record: a draft with an acknowledgement attached.
type filingFixture struct{ filing, file string }

var matrixFilingOf = map[*env]filingFixture{}

func matrixFiling(e *env) filingFixture {
	e.t.Helper()
	if f, ok := matrixFilingOf[e]; ok {
		return f
	}
	acc := e.as(authz.Accountant)
	d := e.call(http.MethodPost, "/v1/compliance/filings", acc, matrixFilingBody)
	up := e.upload("/v1/compliance/filings/"+d.str("id")+"/evidence", acc, map[string]string{"evidence_type": "IRAS acknowledgement"}, "ack.pdf",
		[]byte("%PDF-1.7 matrix acknowledgement"), "If-Match", d.Header.Get("ETag"))
	if d.Status != http.StatusCreated || up.Status != http.StatusCreated {
		e.t.Fatalf("the matrix's filing record: %d %d %s", d.Status, up.Status, d.Raw)
	}
	out := filingFixture{filing: d.str("id"), file: up.str("id")}
	matrixFilingOf[e] = out
	return out
}

var matrixRecordedFilingOf = map[*env]string{}

// matrixRecordedFiling is the matrix's live record, to withdraw: form C-S not required, which needs no file.
func matrixRecordedFiling(e *env) string {
	e.t.Helper()
	if id, ok := matrixRecordedFilingOf[e]; ok {
		return id
	}
	acc := e.as(authz.Accountant)
	d := e.call(http.MethodPost, "/v1/compliance/filings", acc, map[string]any{"key": "form_cs:2026-12-31", "outcome": "not_required", "reason": "Matrix"})
	r := e.call(http.MethodPost, "/v1/compliance/filings/"+d.str("id")+"/record", acc, nil, "If-Match", d.Header.Get("ETag"))
	if d.Status != http.StatusCreated || r.Status != http.StatusOK {
		e.t.Fatalf("the matrix's live record: %d %d %s", d.Status, r.Status, r.Raw)
	}
	matrixRecordedFilingOf[e] = d.str("id")
	return d.str("id")
}

var matrixFilingBody = map[string]any{"key": "eci:2026-12-31", "outcome": "filed", "filed_on": "2026-10-15", "reference": "ECI-MATRIX"}

// matrixFilingState is what a refused call must leave as it was: the draft, its files and the list.
func matrixFilingState(e *env, f filingFixture) string {
	acc := e.as(authz.Accountant)
	var out []string
	for _, p := range []string{"/v1/compliance/filings/" + f.filing, "/v1/compliance/filings/" + f.filing + "/evidence", "/v1/compliance/filings"} {
		r := e.call(http.MethodGet, p, acc, nil)
		out = append(out, r.Header.Get("ETag")+r.Raw)
	}
	return strings.Join(out, "|")
}

// matrixCalendar are the authorisation matrix's rows for the statutory calendar (FR-TAX-07).
var matrixCalendar = map[string]func(e *env, tok string) (response, func() bool){
	"getStatutoryCalendar": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/calendar", tok, nil), nil
	},
	"listStatutoryFilings": func(e *env, tok string) (response, func() bool) {
		matrixFiling(e)
		return e.call(http.MethodGet, "/v1/compliance/filings", tok, nil), nil
	},
	"createStatutoryFiling": func(e *env, tok string) (response, func() bool) {
		f := matrixFiling(e)
		was := matrixFilingState(e, f)
		r := e.call(http.MethodPost, "/v1/compliance/filings", tok, matrixFilingBody, "Prefer", "return=preview")
		return r, func() bool { return matrixFilingState(e, f) == was }
	},
	"getStatutoryFiling": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/filings/"+matrixFiling(e).filing, tok, nil), nil
	},
	"replaceStatutoryFiling": func(e *env, tok string) (response, func() bool) {
		f := matrixFiling(e)
		was := matrixFilingState(e, f)
		r := e.call(http.MethodPut, "/v1/compliance/filings/"+f.filing, tok, matrixFilingBody, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixFilingState(e, f) == was }
	},
	"recordStatutoryFiling": func(e *env, tok string) (response, func() bool) {
		f := matrixFiling(e)
		was := matrixFilingState(e, f)
		r := e.call(http.MethodPost, "/v1/compliance/filings/"+f.filing+"/record", tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixFilingState(e, f) == was }
	},
	"withdrawStatutoryFiling": func(e *env, tok string) (response, func() bool) {
		f, live := matrixFiling(e), matrixRecordedFiling(e)
		was := matrixFilingState(e, f)
		r := e.call(http.MethodPost, "/v1/compliance/filings/"+live+"/withdraw", tok, map[string]any{"reason": "Matrix"}, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixFilingState(e, f) == was }
	},
	"addStatutoryFilingEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixFiling(e)
		was := matrixFilingState(e, f)
		r := e.upload("/v1/compliance/filings/"+f.filing+"/evidence", tok, map[string]string{"evidence_type": "IRAS acknowledgement"}, "again.pdf",
			[]byte("%PDF-1.7 again"), "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixFilingState(e, f) == was }
	},
	"listStatutoryFilingEvidence": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/compliance/filings/"+matrixFiling(e).filing+"/evidence", tok, nil), nil
	},
	"getStatutoryFilingEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixFiling(e)
		return e.call(http.MethodGet, "/v1/compliance/filings/"+f.filing+"/evidence/"+f.file, tok, nil), nil
	},
	"removeStatutoryFilingEvidence": func(e *env, tok string) (response, func() bool) {
		f := matrixFiling(e)
		was := matrixFilingState(e, f)
		r := e.call(http.MethodDelete, "/v1/compliance/filings/"+f.filing+"/evidence/"+f.file, tok, nil, "If-Match", "*", "Prefer", "return=preview")
		return r, func() bool { return matrixFilingState(e, f) == was }
	},
}
