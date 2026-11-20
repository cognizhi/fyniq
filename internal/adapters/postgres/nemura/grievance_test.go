//go:build local

package nemura_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/grievance"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// griefTeam is a company with an Owner, two HR administrators (Hana HR and Hana Two), Cam who files and Sam who is complained about, each with a person record and a login.
type griefTeam struct {
	c                               *company
	hana2, cam, sam                 uuid.UUID // members
	pOwner, pHR, pHana2, pCam, pSam uuid.UUID // people
	now                             time.Time
}

func newGriefTeam(t *testing.T) *griefTeam {
	t.Helper()
	g := &griefTeam{c: newCompany(t), now: today.Add(11 * time.Hour)}
	c := g.c
	class := &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"}
	job := people.Job{Title: "Engineer", Department: "Engineering", Worksite: "SG", EmploymentType: "full_time"}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		for name, dst := range map[string]*uuid.UUID{"Hana Two": &g.hana2, "Cam Chua": &g.cam, "Sam Seah": &g.sam} {
			if *dst, err = nemura.AddMember(ctx, tx, c.org, g.now, actor, name); err != nil {
				return err
			}
		}
		if _, err = nemura.GrantRole(ctx, tx, c.org, g.now, c.owner, g.hana2, authz.HRAdmin, today, nil); err != nil {
			return err
		}
		for _, p := range []struct {
			dst  *uuid.UUID
			name string
			m    uuid.UUID
		}{{&g.pOwner, "Owen Owner", c.owner}, {&g.pHR, "Hana HR", c.hr}, {&g.pHana2, "Hana Two", g.hana2}, {&g.pCam, "Cam Chua", g.cam}, {&g.pSam, "Sam Seah", g.sam}} {
			m := p.m
			if *p.dst, err = nemura.AddPerson(ctx, tx, c.org, g.now, c.owner, people.Person{LegalName: p.name, EmploymentStart: d("2026-01-05"), MemberID: &m}, job, class); err != nil {
				return err
			}
		}
		return nil
	})
	return g
}

// as runs fn as the member, as the API does: the row-level policies see them as the acting person.
func (g *griefTeam) as(member uuid.UUID, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return store(g.c.t, "nemura_api").InOrgAs(context.Background(), g.c.org, member, today, fn)
}

func (g *griefTeam) file(by uuid.UUID, about ...uuid.UUID) (api.GrievanceFiled, error) {
	var out api.GrievanceFiled
	err := g.as(by, func(ctx context.Context, tx pgx.Tx) (err error) {
		out, err = nemura.FileGrievance(ctx, tx, g.c.org, g.now, by, "On the twelfth, in the meeting room, a remark was made about my family that I found demeaning.", about)
		return err
	})
	return out, err
}

func (g *griefTeam) detail(by, id uuid.UUID) (api.GrievanceDetail, error) {
	var out api.GrievanceDetail
	err := g.as(by, func(ctx context.Context, tx pgx.Tx) (err error) {
		out, err = nemura.GrievanceDetail(ctx, tx, g.c.org, g.now, by, id)
		return err
	})
	return out, err
}

func grievanceCode(err error) string {
	var r *grievance.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

func (g *griefTeam) member(name string) uuid.UUID {
	for n, m := range map[string]uuid.UUID{"Hana HR": g.c.hr, "Hana Two": g.hana2, "Owen Owner": g.c.owner} {
		if n == name {
			return m
		}
	}
	return uuid.Nil
}

// A grievance is read only by the person who filed it and the person who handles it; the person complained about can never read it, nor can an HR administrator or the Owner who is neither, and the
// inquiry record is the handler's alone (NM-SEC-06). The restrictive row-level policy holds that in the database, whichever role the person has.
// req: NM-SEC-06
// asvs: V8.2.1, V8.3.1, V8.4.1
func TestAGrievanceIsReadOnlyByItsComplainantAndItsHandlerAndNeverByThePersonComplainedAbout(t *testing.T) {
	ctx := context.Background()
	g := newGriefTeam(t)
	filed, err := g.file(g.cam, g.pSam)
	if err != nil || filed.State != "open" || (filed.Handler != "Hana HR" && filed.Handler != "Hana Two") {
		t.Fatalf("filing: %+v %v", filed, err)
	}
	id := uuid.MustParse(filed.ID)
	handler := g.member(filed.Handler)
	other := g.c.hr
	if handler == g.c.hr {
		other = g.hana2
	}
	count := func(member uuid.UUID, sql string) int {
		var n int
		if err := g.as(member, func(ctx context.Context, tx pgx.Tx) error { return tx.QueryRow(ctx, sql).Scan(&n) }); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for name, m := range map[string]uuid.UUID{"the person complained about": g.sam, "an HR administrator who is not the handler": other, "the Owner who is not the handler": g.c.owner, "the payroll member": g.c.pay} {
		if n := count(m, `SELECT count(*) FROM nemura.grievance`); n != 0 {
			t.Errorf("%s reads %d grievances", name, n)
		}
		if n := count(m, `SELECT count(*) FROM nemura.grievance_entry`); n != 0 {
			t.Errorf("%s reads %d inquiry entries", name, n)
		}
		if _, err := g.detail(m, id); grievanceCode(err) != grievance.CodeNotFound {
			t.Errorf("%s reading it through the service: %v", name, err)
		}
	}
	// The complainant and the handler read it; the complainant does not read the inquiry record.
	if n := count(g.cam, `SELECT count(*) FROM nemura.grievance`); n != 1 {
		t.Errorf("the complainant reads %d", n)
	}
	if n := count(g.cam, `SELECT count(*) FROM nemura.grievance_entry`); n != 0 {
		t.Errorf("the complainant reads %d inquiry entries", n)
	}
	if _, err := g.detail(g.cam, id); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("the complainant through the handler's view: %v", err)
	}
	d, err := g.detail(handler, id)
	if err != nil || len(d.Entries) != 1 || d.Entries[0].Kind != "routing" || len(d.About) != 1 || d.About[0].Name != "Sam Seah" || d.Complainant != "Cam Chua" {
		t.Fatalf("the handler's view: %+v %v", d, err)
	}
	// The person complained about is hidden whatever role they are given, and with no acting person nothing is visible.
	g.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, g.c.org, g.now, g.c.owner, g.sam, authz.HRAdmin, today, nil)
		return err
	})
	if n := count(g.sam, `SELECT count(*) FROM nemura.grievance`); n != 0 {
		t.Errorf("Sam as an HR administrator reads %d", n)
	}
	if _, err := g.detail(g.sam, id); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("Sam as an HR administrator: %v", err)
	}
	var n int
	if err := store(t, "nemura_api").InOrg(ctx, g.c.org, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.grievance`).Scan(&n)
	}); err != nil || n != 0 {
		t.Errorf("with no acting person: %d %v", n, err)
	}
	// Writes: nobody but the filer inserts, nobody but the handler changes, the words and the people never change, and nothing is deleted.
	raw := func(member uuid.UUID, role, sql string, args ...any) string {
		t.Helper()
		err := store(t, role).InOrgAs(ctx, g.c.org, member, today, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, sql, args...)
			return err
		})
		return pgErr(err)
	}
	if got := raw(g.sam, "nemura_api", `UPDATE nemura.grievance SET handler_member = $1`, g.sam); got != "" { // matches no row: invisible to Sam, so nothing is updated and nothing is said
		t.Errorf("Sam updating: %q", got)
	}
	if n := count(handler, `SELECT count(*) FROM nemura.grievance WHERE handler_member IS NOT NULL`); n != 1 {
		t.Error("an update by Sam changed a grievance")
	}
	for name, c := range map[string]struct {
		member uuid.UUID
		sql    string
		args   []any
		want   string
	}{
		"the complainant takes the handling":          {g.cam, `UPDATE nemura.grievance SET handler_member = $1::uuid`, []any{g.cam}, ""}, // Cam cannot update: no row matches the update policy
		"the handler names a person it is about":      {handler, `UPDATE nemura.grievance SET subject_people = '{}'`, nil, "42501"},
		"the handler rewrites the words":              {handler, `UPDATE nemura.grievance SET summary = 'Something else entirely that is long enough.'`, nil, "42501"},
		"the handler hands it to the one it is about": {handler, `UPDATE nemura.grievance SET handler_member = $1::uuid`, []any{g.sam}, "NM210"},
		"the handler hands it to a payroll member":    {handler, `UPDATE nemura.grievance SET handler_member = $1::uuid`, []any{g.c.pay}, "NM210"},
		"a delete":                              {handler, `DELETE FROM nemura.grievance`, nil, "42501"},
		"an entry changed":                      {handler, `UPDATE nemura.grievance_entry SET body = 'x'`, nil, "42501"},
		"an entry deleted":                      {handler, `DELETE FROM nemura.grievance_entry`, nil, "42501"},
		"an entry in the handler's name by Sam": {g.sam, `INSERT INTO nemura.grievance_entry (grievance_id, kind, body, by_member, at) SELECT id, 'note', 'x', $1::uuid, now() FROM nemura.grievance`, []any{g.sam}, ""}, // inserts nothing: Sam sees no grievance
	} {
		if got := raw(c.member, "nemura_api", c.sql, c.args...); got != c.want {
			t.Errorf("%s: want %q, got %q", name, c.want, got)
		}
	}
	if n := count(handler, `SELECT count(*) FROM nemura.grievance_entry`); n != 1 {
		t.Errorf("%d inquiry entries after the attempts, want the one routing entry", n)
	}
	// The people who are not parties hold no privilege at all: not the worker, the read-only role or the assistant.
	for _, role := range []string{"nemura_worker", "nemura_readonly", "nemura_assistant", "nemura_executor"} {
		if got := raw(handler, role, `SELECT count(*) FROM nemura.grievance`); got != "42501" {
			t.Errorf("%s reading the grievances: %q", role, got)
		}
		if got := raw(handler, role, `SELECT count(*) FROM nemura.grievance_entry`); got != "42501" {
			t.Errorf("%s reading the inquiry entries: %q", role, got)
		}
	}
	// Each change was audited, and no event carries the words, the person complained about or the complainant's name.
	if err := store(t, "nemura_worker").InOrg(ctx, g.c.org, func(ctx context.Context, tx pgx.Tx) error {
		var filedN, leaked int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%nemura.grievance.filed%'`).Scan(&filedN); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%nemura.grievance%' AND (body::text LIKE '%meeting room%' OR body::text LIKE '%Sam%' OR body::text LIKE '%'||$1||'%')`, g.pSam.String()).Scan(&leaked); err != nil {
			return err
		}
		if filedN != 1 || leaked != 0 {
			t.Errorf("filed events %d, events that carry the words or the person %d", filedN, leaked)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A grievance about the default handler goes to another authorised person, and when nobody is left it waits, visible to the complainant and to an authorised person who is not involved, and
// is taken by one of them; nobody who is the complainant or is complained about takes it (NM-SEC-06).
// req: NM-SEC-06
// asvs: V8.2.1, V8.3.1
func TestAGrievanceAboutTheDefaultHandlerGoesToAnotherAndWaitsWhenNoOneIsLeft(t *testing.T) {
	g := newGriefTeam(t)
	// About both HR administrators: the Owner handles it.
	f, err := g.file(g.cam, g.pHR, g.pHana2)
	if err != nil || f.State != "open" || f.Handler != "Owen Owner" {
		t.Fatalf("about both HR administrators: %+v %v", f, err)
	}
	// About an HR administrator, filed by the other HR administrator: the Owner.
	if f, err := g.file(g.hana2, g.pHR); err != nil || f.Handler != "Owen Owner" {
		t.Errorf("filed by one HR administrator about the other: %+v %v", f, err)
	}
	// About oneself: refused. About a person not in the organisation: refused.
	if _, err := g.file(g.cam, g.pCam); grievanceCode(err) != grievance.CodeInvalid {
		t.Errorf("about oneself: %v", err)
	}
	if _, err := g.file(g.cam, uuid.New()); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("about someone who is not here: %v", err)
	}
	// About everyone who could handle it: it waits.
	w, err := g.file(g.cam, g.pHR, g.pHana2, g.pOwner)
	if err != nil || w.State != "unrouted" || w.Handler != "" {
		t.Fatalf("about everyone authorised: %+v %v", w, err)
	}
	id := uuid.MustParse(w.ID)
	mine := func() api.GrievanceMine {
		var out []api.GrievanceMine
		if err := g.as(g.cam, func(ctx context.Context, tx pgx.Tx) (err error) {
			out, err = nemura.MyGrievances(ctx, tx, g.cam)
			return
		}); err != nil || len(out) == 0 {
			t.Fatalf("my grievances: %v %v", out, err)
		}
		for _, m := range out {
			if m.ID == id.String() {
				return m
			}
		}
		t.Fatal("the waiting grievance is not in the complainant's list")
		return api.GrievanceMine{}
	}
	if m := mine(); m.State != "unrouted" || m.Handler != "" {
		t.Errorf("the complainant's view of a waiting grievance: %+v", m)
	}
	// Those it is about see nothing, even though they are authorised and it is waiting; the complainant cannot take it.
	for name, m := range map[string]uuid.UUID{"Hana HR": g.c.hr, "Hana Two": g.hana2, "the Owner": g.c.owner} {
		var unrouted []api.GrievanceRow
		if err := g.as(m, func(ctx context.Context, tx pgx.Tx) (err error) {
			_, unrouted, err = nemura.HandledGrievances(ctx, tx, m)
			return
		}); err != nil || len(unrouted) != 0 {
			t.Errorf("%s sees %d waiting grievances: %v", name, len(unrouted), err)
		}
		if err := g.as(m, func(ctx context.Context, tx pgx.Tx) error {
			return nemura.TakeGrievance(ctx, tx, g.c.org, g.now, m, id)
		}); grievanceCode(err) != grievance.CodeNotFound {
			t.Errorf("%s taking a grievance about them: %v", name, err)
		}
	}
	// A new HR administrator who is not involved sees it, reads it and takes it; a second take is refused; it is now handled and the complainant is told by whom.
	var hana3 uuid.UUID
	g.c.must(func(ctx context.Context, tx pgx.Tx) error {
		var err error
		if hana3, err = nemura.AddMember(ctx, tx, g.c.org, g.now, actor, "Hana Three"); err != nil {
			return err
		}
		_, err = nemura.GrantRole(ctx, tx, g.c.org, g.now, g.c.owner, hana3, authz.HRAdmin, today, nil)
		return err
	})
	var unrouted []api.GrievanceRow
	if err := g.as(hana3, func(ctx context.Context, tx pgx.Tx) (err error) {
		_, unrouted, err = nemura.HandledGrievances(ctx, tx, hana3)
		return
	}); err != nil || len(unrouted) != 1 {
		t.Fatalf("a new HR administrator sees %d waiting grievances: %v", len(unrouted), err)
	}
	if d, err := g.detail(hana3, id); err != nil || d.State != "unrouted" || len(d.About) != 3 {
		t.Errorf("reading a waiting grievance: %+v %v", d, err)
	}
	if err := g.as(g.cam, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.TakeGrievance(ctx, tx, g.c.org, g.now, g.cam, id)
	}); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("the complainant taking it: %v", err)
	}
	if err := g.as(hana3, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.TakeGrievance(ctx, tx, g.c.org, g.now, hana3, id)
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.as(hana3, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.TakeGrievance(ctx, tx, g.c.org, g.now, hana3, id)
	}); grievanceCode(err) != grievance.CodeState {
		t.Errorf("taking it twice: %v", err)
	}
	if m := mine(); m.State != "open" || m.Handler != "Hana Three" {
		t.Errorf("the complainant's view once taken: %+v", m)
	}
}

// The handler records the inquiry, passes the grievance on with a reason, and writes the outcome once, which the complainant reads; once decided nothing is added and nobody else decides (NM-SEC-06).
// req: NM-SEC-06
// asvs: V8.2.1, V8.3.1
func TestAHandlerRecordsPassesOnAndDecidesAGrievanceAndTheComplainantReadsTheOutcome(t *testing.T) {
	g := newGriefTeam(t)
	f, err := g.file(g.cam, g.pSam)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(f.ID)
	handler := g.member(f.Handler)
	do := func(m uuid.UUID, fn func(ctx context.Context, tx pgx.Tx) error) error { return g.as(m, fn) }
	rec := func(m uuid.UUID, kind, note string) error {
		return do(m, func(ctx context.Context, tx pgx.Tx) error {
			return nemura.RecordGrievanceEntry(ctx, tx, g.c.org, g.now, m, id, kind, note)
		})
	}
	if err := rec(handler, "interview", "Spoke with the complainant for an hour."); err != nil {
		t.Fatal(err)
	}
	if err := rec(handler, "chat", "x"); grievanceCode(err) != grievance.CodeInvalid {
		t.Errorf("an unknown kind: %v", err)
	}
	for name, m := range map[string]uuid.UUID{"the complainant": g.cam, "the person complained about": g.sam, "another HR administrator": map[bool]uuid.UUID{true: g.hana2, false: g.c.hr}[handler == g.c.hr]} {
		if err := rec(m, "note", "x"); grievanceCode(err) != grievance.CodeNotFound {
			t.Errorf("%s adding to the record: %v", name, err)
		}
		if err := do(m, func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideGrievance(ctx, tx, g.c.org, g.now, m, id, "substantiated", "The inquiry found that the remark was made and it was demeaning.")
		}); grievanceCode(err) != grievance.CodeNotFound {
			t.Errorf("%s deciding: %v", name, err)
		}
	}
	// Passing it on: to the next eligible, with the reason in the record; the one who passed it on no longer reads it, and cannot pass it again.
	if err := do(handler, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.HandOverGrievance(ctx, tx, g.c.org, g.now, handler, id, "")
	}); grievanceCode(err) != grievance.CodeInvalid {
		t.Errorf("a handover with no reason: %v", err)
	}
	if err := do(handler, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.HandOverGrievance(ctx, tx, g.c.org, g.now, handler, id, "I work closely with the person it is about.")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := g.detail(handler, id); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("the one who passed it on still reads it: %v", err)
	}
	if err := do(handler, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.HandOverGrievance(ctx, tx, g.c.org, g.now, handler, id, "again")
	}); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("passing it on again: %v", err)
	}
	next := g.hana2
	if handler == g.hana2 {
		next = g.c.hr
	}
	d, err := g.detail(next, id)
	if err != nil || len(d.Entries) != 3 || d.Entries[1].Kind != "interview" || d.Entries[2].Kind != "handover" || !strings.Contains(d.Entries[2].Body, "work closely") {
		t.Fatalf("the next handler's view: %+v %v", d, err)
	}
	// The outcome is written once; the complainant reads it; the record is then closed.
	if err := do(next, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideGrievance(ctx, tx, g.c.org, g.now, next, id, "maybe", "The inquiry found that the remark was made and it was demeaning.")
	}); grievanceCode(err) != grievance.CodeInvalid {
		t.Errorf("an unknown finding: %v", err)
	}
	if err := do(next, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideGrievance(ctx, tx, g.c.org, g.now, next, id, "substantiated", "Too short.")
	}); grievanceCode(err) != grievance.CodeInvalid {
		t.Errorf("an outcome too short: %v", err)
	}
	decide := func() error {
		return do(next, func(ctx context.Context, tx pgx.Tx) error {
			return nemura.DecideGrievance(ctx, tx, g.c.org, g.now.Add(time.Hour), next, id, "substantiated", "The inquiry found that the remark was made and it was demeaning; it was dealt with.")
		})
	}
	if err := decide(); err != nil {
		t.Fatal(err)
	}
	if err := decide(); grievanceCode(err) != grievance.CodeState {
		t.Errorf("deciding twice: %v", err)
	}
	if err := rec(next, "note", "a late note"); grievanceCode(err) != grievance.CodeState {
		t.Errorf("a note after the outcome: %v", err)
	}
	var mine []api.GrievanceMine
	if err := g.as(g.cam, func(ctx context.Context, tx pgx.Tx) (err error) {
		mine, err = nemura.MyGrievances(ctx, tx, g.cam)
		return
	}); err != nil || len(mine) != 1 ||
		mine[0].State != "decided" || mine[0].Finding != "substantiated" || !strings.Contains(mine[0].Outcome, "demeaning") || mine[0].DecidedAt == "" {
		t.Errorf("the complainant's view of the outcome: %+v %v", mine, err)
	}
	// A decided grievance never changes, even by the handler straight to the database.
	err = g.as(next, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE nemura.grievance SET outcome = 'A different outcome that is long enough to pass.'`)
		return err
	})
	if pgErr(err) != "NM209" {
		t.Errorf("changing a decided outcome: %v", err)
	}
}
