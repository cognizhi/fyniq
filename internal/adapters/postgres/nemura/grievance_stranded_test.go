//go:build local

package nemura_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/grievance"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func (g *griefTeam) waiting(m uuid.UUID) []api.GrievanceRow {
	var out []api.GrievanceRow
	if err := g.as(m, func(ctx context.Context, tx pgx.Tx) (err error) {
		_, out, err = nemura.HandledGrievances(ctx, tx, m)
		return
	}); err != nil {
		g.c.t.Fatal(err)
	}
	return out
}

func (g *griefTeam) take(m, id uuid.UUID) error {
	return g.as(m, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.TakeGrievance(ctx, tx, g.c.org, g.now, m, id)
	})
}

// When a handler is deactivated the grievance is stranded: an Owner or HR administrator who is neither the complainant nor involved lists it, reads it and takes it over; nobody else gains anything, the
// person it is about still reads nothing whatever role they are given, and the earlier handler reads nothing more (NM-SEC-06, option A of 21 Nov 2026).
// req: NM-SEC-06
// asvs: V8.2.1, V8.3.1, V8.4.1
func TestAGrievanceWhoseHandlerIsDeactivatedIsTakenOverByAnAuthorisedPersonWhoIsNotInvolved(t *testing.T) {
	g := newGriefTeam(t)
	f, err := g.file(g.cam, g.pOwner) // about the Owner: the two HR administrators are the candidates
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(f.ID)
	h := g.member(f.Handler)
	other := g.hana2
	if h == g.hana2 {
		other = g.c.hr
	}
	// While its handler is active nobody else sees it as waiting, and a take is refused.
	if w := g.waiting(other); len(w) != 0 {
		t.Fatalf("the other HR administrator sees %d waiting grievances while the handler is active", len(w))
	}
	if err := g.take(other, id); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("taking a grievance whose handler is active: %v", err)
	}
	// Sam, who is not involved, is given the HR role later: a new authorised person. The payroll member is not authorised.
	g.c.must(func(ctx context.Context, tx pgx.Tx) error {
		_, err := nemura.GrantRole(ctx, tx, g.c.org, g.now, g.c.owner, g.sam, authz.HRAdmin, today, nil)
		return err
	})
	g.c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DeactivateMember(ctx, tx, g.c.org, g.now, g.c.owner, h, today)
	})
	w := g.waiting(other)
	if len(w) != 1 || w[0].ID != id.String() || w[0].Reason != "handler_left" {
		t.Fatalf("the other HR administrator's waiting list: %+v", w)
	}
	// Who sees it: the complainant (their own list; no take), the Owner it is about (nothing), the payroll member (nothing), a deactivated handler (nothing more).
	for name, m := range map[string]uuid.UUID{"the Owner it is about": g.c.owner, "the payroll member": g.c.pay, "the complainant": g.cam} {
		if w := g.waiting(m); len(w) != 0 {
			t.Errorf("%s sees %d waiting grievances", name, len(w))
		}
		if err := g.take(m, id); grievanceCode(err) != grievance.CodeNotFound {
			t.Errorf("%s taking it: %v", name, err)
		}
		if _, err := g.detail(m, id); name != "the complainant" && grievanceCode(err) != grievance.CodeNotFound {
			t.Errorf("%s reading it: %v", name, err)
		}
	}
	// Reading a stranded grievance shows the grievance, never the inquiry record, which is the handler's.
	if d, err := g.detail(other, id); err != nil || d.State != "open" || len(d.About) != 1 || len(d.Entries) != 0 {
		t.Errorf("reading a stranded grievance: %+v %v", d, err)
	}
	// Straight to the database a person who is not the handler may not decide it, give it to another, or hand it to the person it is about.
	for name, sql := range map[string]string{
		"deciding it":          `UPDATE nemura.grievance SET state = 'decided', finding = 'no_finding', outcome = 'A decision written by someone who has not taken it yet.', decided_by = handler_member, decided_at = now()`,
		"giving it to another": `UPDATE nemura.grievance SET handler_member = '` + g.c.owner.String() + `'`,
	} {
		err := g.as(other, func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, sql); return err })
		if err == nil {
			t.Errorf("%s straight to the database was accepted", name)
		}
	}
	// Sam, with an HR role and not involved, may take it too, and the one who takes it first wins.
	if err := g.take(other, id); err != nil {
		t.Fatal(err)
	}
	if err := g.take(g.sam, id); grievanceCode(err) != grievance.CodeNotFound { // it is handled now, so it is no longer in Sam's view
		t.Errorf("a second take: %v", err)
	}
	d, err := g.detail(other, id)
	if err != nil || len(d.Entries) != 2 || d.Entries[1].Kind != "routing" || !strings.Contains(d.Entries[1].Body, "can no longer handle it") {
		t.Fatalf("the new handler's view: %+v %v", d, err)
	}
	if _, err := g.detail(h, id); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("the deactivated handler still reads it: %v", err)
	}
	var mine []api.GrievanceMine
	if err := g.as(g.cam, func(ctx context.Context, tx pgx.Tx) (err error) {
		mine, err = nemura.MyGrievances(ctx, tx, g.cam)
		return
	}); err != nil ||
		len(mine) != 1 || mine[0].Handler == "" || mine[0].Handler == f.Handler {
		t.Errorf("the complainant sees the new handler: %+v %v", mine, err)
	}
	if err := g.as(other, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DecideGrievance(ctx, tx, g.c.org, g.now, other, id, "no_finding", "The inquiry could not establish what was said, and the matter is closed with advice to both.")
	}); err != nil {
		t.Errorf("the new handler deciding: %v", err)
	}
}

// A grievance is routed past a handler who was deactivated while it was in flight, and waits when no one is left; a login given to a person complained about after the filing hides it from them at once, and
// they can neither take it nor be made its handler (NM-SEC-06).
// req: NM-SEC-06
// asvs: V8.2.1, V8.3.1
func TestAGrievanceIsRoutedPastADeactivatedHandlerAndHiddenFromAPersonGivenALoginAfterFiling(t *testing.T) {
	g := newGriefTeam(t)
	c := g.c
	f, err := g.file(g.cam, g.pOwner) // about the Owner: the two HR administrators are the candidates
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.MustParse(f.ID)
	h := g.member(f.Handler)
	o := g.hana2
	if h == g.hana2 {
		o = c.hr
	}
	// The other HR administrator is deactivated mid-flight: passing it on finds nobody, and it stays with its handler.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DeactivateMember(ctx, tx, c.org, g.now, c.owner, o, today)
	})
	if err := g.as(h, func(ctx context.Context, tx pgx.Tx) error {
		return nemura.HandOverGrievance(ctx, tx, c.org, g.now, h, id, "I would rather someone else looked into this.")
	}); grievanceCode(err) != grievance.CodeNoHandler {
		t.Errorf("passing it on with no one left: %v", err)
	}
	// Then its handler is deactivated too: a grievance filed now waits (nobody eligible), and both wait for a new HR administrator.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.DeactivateMember(ctx, tx, c.org, g.now, c.owner, h, today)
	})
	if f2, err := g.file(g.cam, g.pOwner); err != nil || f2.State != "unrouted" {
		t.Errorf("a grievance filed with every candidate deactivated: %+v %v", f2, err)
	}
	var fresh uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		if fresh, err = nemura.AddMember(ctx, tx, c.org, g.now, actor, "Fresh HR"); err != nil {
			return err
		}
		_, err = nemura.GrantRole(ctx, tx, c.org, g.now, c.owner, fresh, authz.HRAdmin, today, nil)
		return err
	})
	reasons := map[string]int{}
	for _, w := range g.waiting(fresh) {
		reasons[w.Reason]++
	}
	if reasons["handler_left"] != 1 || reasons["unrouted"] != 1 {
		t.Errorf("a new HR administrator's waiting list: %v", reasons)
	}
	// A person complained about with no login: given one later (and an HR role), they read none of it.
	var pPat uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		pPat, err = nemura.AddPerson(ctx, tx, c.org, g.now, c.owner, people.Person{LegalName: "Pat Poe", EmploymentStart: d("2026-01-05")},
			people.Job{Title: "Engineer", Department: "Engineering", Worksite: "SG", EmploymentType: "full_time"}, &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"})
		return
	})
	about, err := g.file(g.cam, pPat)
	if err != nil {
		t.Fatal(err)
	}
	aid := uuid.MustParse(about.ID)
	var pat uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		if pat, err = nemura.AddMember(ctx, tx, c.org, g.now, actor, "Pat Poe"); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE nemura.person SET member_id = $2 WHERE id = $1`, pPat, pat); err != nil {
			return err
		}
		if err = nemura.Audit(ctx, tx, c.org, g.now, actor, "nemura.person.updated", audit.Object{Type: "person", ID: pPat}, nil, ""); err != nil {
			return err
		}
		_, err = nemura.GrantRole(ctx, tx, c.org, g.now, c.owner, pat, authz.HRAdmin, today, nil)
		return err
	})
	if _, err := g.detail(pat, aid); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("a person given a login after the filing reads it: %v", err)
	}
	if err := g.take(pat, aid); grievanceCode(err) != grievance.CodeNotFound {
		t.Errorf("taking it: %v", err)
	}
	for _, w := range g.waiting(pat) { // the others that wait are not about them: they may see those
		if w.ID == aid.String() {
			t.Errorf("sees the grievance about them among those that wait")
		}
	}
	var n int
	if err := g.as(pat, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.grievance WHERE id = $1`, aid).Scan(&n); err != nil {
			return err
		}
		return nil
	}); err != nil || n != 0 {
		t.Errorf("a plain SELECT shows %d rows to the person it is about: %v", n, err)
	}
	// Handing it to them is refused by the guard, from the handler's own session.
	ah := g.member(about.Handler)
	if ah != uuid.Nil {
		err := g.as(ah, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE nemura.grievance SET handler_member = $2 WHERE id = $1`, aid, pat)
			return err
		})
		if pgErr(err) != "NM210" {
			t.Errorf("giving it to the person it is about: %v", err)
		}
	}
}

// The worker learns only which grievances wait and whom to tell, never a word; it tells each authorised person who is not involved once; a decided grievance is reported after five years for a person to
// review, deleted with its inquiry record only once reviewed, kept while a legal hold names it or anyone involved, and refused by the database before its time (NM-SEC-06, NM-SEC-05).
// req: NM-SEC-06, NM-SEC-05
// asvs: V8.3.1, V8.3.2
func TestTheWorkersGrievanceJobsTellWhoMustTakeOneAndDeleteADecidedOneOnlyAfterItsRetentionAndReview(t *testing.T) {
	g := newGriefTeam(t)
	c := g.c
	worker := store(t, "nemura_worker")
	ctx := context.Background()
	// The worker holds no right on the tables themselves.
	if err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		var n int
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.grievance`).Scan(&n)
	}); pgErr(err) != "42501" {
		t.Errorf("the worker reads the grievance table: %v", err)
	}
	// An unrouted grievance (about every authorised person) and a stranded one are announced to the authorised people who are not involved.
	if f, err := g.file(g.cam, g.pHR, g.pHana2, g.pOwner); err != nil || f.State != "unrouted" {
		t.Fatalf("%+v %v", f, err)
	}
	notified := func() map[string]int {
		out := map[string]int{}
		if err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT payload->>'person' FROM nemura.outbox WHERE kind = 'people.notice' AND business_key LIKE 'grievance-waiting:%'`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var p string
				if err := rows.Scan(&p); err != nil {
					return err
				}
				out[p]++
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if n, err := worker.RaiseGrievanceWatch(ctx, c.org, g.now); err != nil || n != 0 {
		t.Fatalf("nobody is eligible: %d %v", n, err) // every authorised person is involved
	}
	var hana3, pHana3 uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		if hana3, err = nemura.AddMember(ctx, tx, c.org, g.now, actor, "Hana Three"); err != nil {
			return err
		}
		if _, err = nemura.GrantRole(ctx, tx, c.org, g.now, c.owner, hana3, authz.HRAdmin, today, nil); err != nil {
			return err
		}
		pHana3, err = nemura.AddPerson(ctx, tx, c.org, g.now, c.owner, people.Person{LegalName: "Hana Three", EmploymentStart: d("2026-01-05"), MemberID: &hana3},
			people.Job{Title: "HR", Department: "People", Worksite: "SG", EmploymentType: "full_time"}, &people.Classification{ContractType: "service", EACovered: true, PartIVClass: "none", Residency: "citizen"})
		return err
	})
	if n, err := worker.RaiseGrievanceWatch(ctx, c.org, g.now); err != nil || n != 1 {
		t.Fatalf("one person to tell: %d %v", n, err)
	}
	if n, err := worker.RaiseGrievanceWatch(ctx, c.org, g.now); err != nil || n != 0 {
		t.Errorf("a second sweep told %d again: %v", n, err)
	}
	if got := notified(); len(got) != 1 || got[pHana3.String()] != 1 {
		t.Errorf("who was told: %v", got)
	}
	// The database refuses the purge before its time, to the API role, and for a grievance not decided.
	if err := store(t, "nemura_api").InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT nemura.purge_grievances(ARRAY[gen_random_uuid()], current_date)`)
		return err
	}); pgErr(err) != "42501" {
		t.Errorf("the API role runs the purge: %v", err)
	}
	// A decided grievance and one held.
	f1, err := g.file(g.cam, g.pSam)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := g.file(g.cam, g.pSam)
	if err != nil {
		t.Fatal(err)
	}
	id1, id2 := uuid.MustParse(f1.ID), uuid.MustParse(f2.ID)
	decide := func(f api.GrievanceFiled, id uuid.UUID) {
		h := g.member(f.Handler)
		if f.Handler == "Hana Three" {
			h = hana3
		}
		if err := g.as(h, func(ctx context.Context, tx pgx.Tx) error {
			if err := nemura.RecordGrievanceEntry(ctx, tx, c.org, g.now, h, id, "note", "Spoke with those involved."); err != nil {
				return err
			}
			return nemura.DecideGrievance(ctx, tx, c.org, g.now, h, id, "not_substantiated", "The inquiry found no evidence that the remark was made, and closed the matter.")
		}); err != nil {
			t.Fatal(err)
		}
	}
	decide(f1, id1)
	decide(f2, id2)
	var hold uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) { // a hold on one of the people complained about keeps the grievance
		hold, err = nemura.PlaceHold(ctx, tx, c.org, g.now, c.owner, grievance.RecordType, &g.pSam, "A claim is expected.", today)
		return
	})
	super := connect(t, db.SuperURL())
	count := func(id uuid.UUID) (n int) {
		if err := super.QueryRow(ctx, `SELECT (SELECT count(*) FROM nemura.grievance WHERE id = $1) + (SELECT count(*) FROM nemura.grievance_entry WHERE grievance_id = $1)`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	early := g.now.AddDate(4, 11, 0)
	if del, rep, err := worker.RunGrievanceRetention(ctx, c.org, early); err != nil || del != 0 || rep != 0 {
		t.Fatalf("before five years: %d %d %v", del, rep, err)
	}
	due := g.now.AddDate(5, 0, 1)
	if del, rep, err := worker.RunGrievanceRetention(ctx, c.org, due); err != nil || del != 0 || rep != 0 {
		t.Fatalf("both are held by the hold on Sam: %d %d %v", del, rep, err) // Sam is involved in both
	}
	if err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT nemura.purge_grievances($1, $2::date)`, []uuid.UUID{id1}, due.Format(time.DateOnly))
		return err
	}); pgErr(err) != "NM211" {
		t.Errorf("a held grievance purged straight through the function: %v", err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ReleaseHold(ctx, tx, c.org, g.now, c.owner, hold, due)
	})
	if del, rep, err := worker.RunGrievanceRetention(ctx, c.org, due); err != nil || del != 0 || rep != 1 {
		t.Fatalf("a report for review: %d %d %v", del, rep, err)
	}
	if del, rep, err := worker.RunGrievanceRetention(ctx, c.org, due); err != nil || del != 0 || rep != 0 {
		t.Errorf("a second night reports again or deletes before review: %d %d %v", del, rep, err)
	}
	if count(id1) == 0 || count(id2) == 0 {
		t.Fatal("a grievance was deleted before its report was reviewed")
	}
	var report uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id FROM nemura.deletion_report WHERE record_type = $1 AND state = 'pending'`, grievance.RecordType).Scan(&report); err != nil {
			return err
		}
		return nemura.ReviewDeletionReport(ctx, tx, c.org, g.now, c.owner, report, due)
	})
	if del, _, err := worker.RunGrievanceRetention(ctx, c.org, due); err != nil || del != 2 {
		t.Fatalf("deleted after review: %d %v", del, err)
	}
	if count(id1) != 0 || count(id2) != 0 {
		t.Errorf("a grievance or its inquiry record is still there: %d %d", count(id1), count(id2))
	}
	var proofs int
	if err := super.QueryRow(ctx, `SELECT count(*) FROM nemura.deletion_proof WHERE report_id = $1`, report).Scan(&proofs); err != nil || proofs != 2 {
		t.Errorf("proofs: %d %v", proofs, err)
	}
	// Not decided, and decided but too young, are refused by the function itself.
	if err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT nemura.purge_grievances(ARRAY(SELECT grievance_id FROM nemura.grievances_waiting($1::date)), $1::date)`, due.Format(time.DateOnly))
		return err
	}); pgErr(err) != "NM211" {
		t.Errorf("an undecided grievance purged: %v", err)
	}
}
