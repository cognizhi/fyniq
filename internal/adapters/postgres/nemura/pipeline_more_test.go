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
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

// A line manager reads the requisitions that name them as hiring manager, as counts by stage with no salary and no name, and no one else's; the hiring manager is chosen once, when the requisition is asked
// for, and never changes (NM-REC-01, rev 0.74).
// req: NM-REC-01
// asvs: V8.2.1, V8.3.1
func TestALineManagerReadsOnlyTheRolesThatNameThemAsCountsByStage(t *testing.T) {
	c := newHiringCo(t)
	ctx := context.Background()
	hr := c.sess(c.hr)
	var named uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		login := c.outsider
		manager, err := nemura.AddPerson(ctx, tx, c.org, c.now, c.owner, people.Person{LegalName: "Mia Manager", EmploymentStart: d("2090-01-05"), MemberID: &login}, people.Job{Title: "Head of Product", Department: "Product", Worksite: "SG", EmploymentType: "full_time"}, nil)
		if err != nil {
			return err
		}
		in := api.RequisitionInput{Title: "Designer", Department: "Product", Reason: "new", SalaryMin: "5000.00", SalaryMax: "6000.00", TargetStart: "2099-06-01", HiringManager: manager.String()}
		if named, err = nemura.RequestRequisition(ctx, tx, c.org, c.now, c.hr, in); err != nil {
			return err
		}
		return nemura.DecideRequisition(ctx, tx, c.org, c.now, c.owner, named, true, "")
	})
	for i, name := range []string{"Ann", "Bea", "Cai"} {
		cand, err := c.svc.Add(ctx, hr, named, api.CandidateInput{Name: name, Email: strings.ToLower(name) + "@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			if _, err := c.svc.Move(ctx, hr, mustID(t, cand.ID), "screening", ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := c.svc.Add(ctx, hr, c.req, api.CandidateInput{Name: "Dee", Email: "dee@example.com"}); err != nil { // another role, no hiring manager
		t.Fatal(err)
	}
	h := nemura.HiringService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(c.now)}
	got, err := h.MyRoles(ctx, c.sess(c.outsider))
	if err != nil || len(got) != 1 || got[0].ID != named.String() || got[0].Title != "Designer" || got[0].State != "approved" || got[0].Candidates.Applied != 1 || got[0].Candidates.Screening != 2 || got[0].Candidates.Hired != 0 {
		t.Fatalf("the manager's roles: %+v %v", got, err)
	}
	for name, m := range map[string]uuid.UUID{"the HR administrator who asked": c.hr, "an interviewer": c.iv, "the Owner": c.owner} {
		if l, err := h.MyRoles(ctx, c.sess(m)); err != nil || len(l) != 0 {
			t.Errorf("%s reads a role that does not name them: %+v %v", name, l, err)
		}
	}
	// The hiring manager is a member of this organisation, and is chosen once.
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		in := api.RequisitionInput{Title: "X", Department: "Y", Reason: "new", SalaryMin: "1.00", SalaryMax: "2.00", TargetStart: "2099-06-01", HiringManager: uuid.NewString()}
		_, err := nemura.RequestRequisition(ctx, tx, c.org, c.now, c.hr, in)
		if hireCode(err) != hiring.CodeNotFound {
			t.Errorf("a hiring manager who is not a person with a sign-in: %v", err)
		}
		return nil
	})
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE nemura.requisition SET hiring_manager = $1 WHERE id = $2`, c.iv, named)
		return err
	}); pgErr(err) != "42501" && pgErr(err) != "NM213" {
		t.Errorf("a hiring manager changed: %v", err)
	}
}

// An interviewer's own page lists only the candidates assigned to them, with the scorecard's criteria and the criteria they scored; the hiring team's list of interviewers names the members who hold the role
// today (NM-REC-04, rev 0.74).
// req: NM-REC-04
// asvs: V8.2.1, V4.2.1
func TestAnInterviewerListsOnlyTheirOwnCandidatesAndTheTeamListsTheInterviewers(t *testing.T) {
	c := newHiringCo(t)
	ctx := context.Background()
	hr := c.sess(c.hr)
	kit := api.InterviewKit{Questions: []string{"Walk us through a forecast you built."}, Criteria: []string{"Analytical skill", "Communication"}}
	if _, err := c.svc.SaveKit(ctx, hr, c.req, kit); err != nil {
		t.Fatal(err)
	}
	mine, other := "", ""
	for name, dst := range map[string]*string{"Mine": &mine, "Other": &other} {
		cand, err := c.svc.Add(ctx, hr, c.req, api.CandidateInput{Name: name, Email: strings.ToLower(name) + "@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		*dst = cand.ID
	}
	if _, err := c.svc.Assign(ctx, hr, mustID(t, mine), c.iv); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Assign(ctx, hr, mustID(t, other), c.iv2); err != nil {
		t.Fatal(err)
	}
	l, err := c.svc.MyInterviews(ctx, c.sess(c.iv))
	if err != nil || len(l) != 1 || l[0].CandidateID != mine || l[0].Name != "Mine" || len(l[0].Criteria) != 2 || len(l[0].Scored) != 0 {
		t.Fatalf("an interviewer's list: %+v %v", l, err)
	}
	if _, err := c.svc.Score(ctx, c.sess(c.iv), mustID(t, mine), "Communication", 4, "Clear."); err != nil {
		t.Fatal(err)
	}
	if l, _ = c.svc.MyInterviews(ctx, c.sess(c.iv)); len(l) != 1 || len(l[0].Scored) != 1 || l[0].Scored[0] != "Communication" {
		t.Errorf("the criteria scored: %+v", l)
	}
	if l, err = c.svc.MyInterviews(ctx, c.sess(c.outsider)); err != nil || len(l) != 0 {
		t.Errorf("a member who is no interviewer lists candidates: %+v %v", l, err)
	}
	iv, err := c.svc.Interviewers(ctx, hr)
	if err != nil || len(iv) != 2 || iv[0].Name != "Ian Interviewer" || iv[1].Name != "Ivy Interviewer" {
		t.Errorf("the interviewers to assign: %+v %v", iv, err)
	}
}

// A candidate looks at their own data through a link the hiring team makes: what they gave and where they stand, never the team's notes or scores; the link expires, is revoked once, answers alike for what
// it does not know, and its stored hash opens nothing; the candidate withdraws the consent to be kept once; only the hiring team makes or reads a link; every look-up is audited without the data (NM-REC-05).
// req: NM-REC-05
// asvs: V2.3.1, V8.2.1, V8.3.1, V13.2.1
func TestACandidateLooksAtTheirOwnDataThroughALinkAndNeverSeesTheTeamsNotesOrScores(t *testing.T) {
	c := newHiringCo(t)
	ctx := context.Background()
	hr := c.sess(c.hr)
	if _, err := c.svc.SaveKit(ctx, hr, c.req, api.InterviewKit{Questions: []string{"Walk us through a forecast you built."}, Criteria: []string{"Analytical skill"}}); err != nil {
		t.Fatal(err)
	}
	cand, err := c.svc.Add(ctx, hr, c.req, api.CandidateInput{Name: "Chen Yu", Email: "chen.yu@example.com", CV: "Five years in FP&A.", ConsentFuture: true})
	if err != nil {
		t.Fatal(err)
	}
	id := mustID(t, cand.ID)
	if _, err := c.svc.Note(ctx, hr, id, "SECRET-NOTE about their salary expectations."); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Assign(ctx, hr, id, c.iv); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Score(ctx, c.sess(c.iv), id, "Analytical skill", 2, "SECRET-SCORE comment."); err != nil {
		t.Fatal(err)
	}
	// Only the hiring team makes a link: an assigned interviewer reads the candidate but not the links, and cannot make one.
	if _, err := c.svc.CreateAccess(ctx, c.sess(c.iv), id, 0); err == nil {
		t.Error("an interviewer made an access link")
	}
	if _, err := c.svc.CreateAccess(ctx, hr, id, 31); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("a link for 31 days: %v", err)
	}
	made, err := c.svc.CreateAccess(ctx, hr, id, 0)
	if err != nil || made.Token == "" || made.Path != "/my-application" || !strings.HasPrefix(made.Token, c.org.String()+".") {
		t.Fatalf("%+v %v", made, err)
	}
	links, err := c.svc.AccessLinks(ctx, hr, id)
	if err != nil || len(links) != 1 || links[0].ID != made.ID || links[0].RevokedAt != "" {
		t.Fatalf("%+v %v", links, err)
	}
	if l, err := c.svc.AccessLinks(ctx, c.sess(c.iv), id); err != nil || len(l) != 0 {
		t.Errorf("an interviewer reads the links: %+v %v", l, err)
	}
	// With no session: what they gave and where they stand, nothing the team wrote.
	v, err := c.svc.OwnData(ctx, made.Token)
	if err != nil || !v.Found || v.Name != "Chen Yu" || v.Email != "chen.yu@example.com" || v.CV != "Five years in FP&A." || v.Role != "Analyst" || v.Stage != "applied" || !v.ConsentFuture || v.RetentionUntil != "" {
		t.Fatalf("%+v %v", v, err)
	}
	if strings.Contains(strings.Join([]string{v.Name, v.Email, v.CV, v.Role, v.Stage, v.StageAt}, "|"), "SECRET") {
		t.Error("the team's words reached the candidate")
	}
	// Unknown, malformed, another organisation's and a stored hash all answer alike: not found.
	other := newHiringCo(t)
	for name, tok := range map[string]string{"an unknown token": c.org.String() + ".nope-nope-nope", "no organisation": "nope.nope", "another organisation's prefix": other.org.String() + "." + strings.SplitN(made.Token, ".", 2)[1], "an empty token": ""} {
		if got, err := c.svc.OwnData(ctx, tok); err != nil || got.Found {
			t.Errorf("%s: %+v %v", name, got, err)
		}
	}
	var viaHash int
	var hash string
	if err := connect(t, db.SuperURL()).QueryRow(ctx, `SELECT encode(token_hash, 'hex') FROM nemura.candidate_access WHERE id = $1`, made.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.candidate_access_view($1, $2)`, hash, c.now).Scan(&viaHash)
	})
	if viaHash != 0 {
		t.Error("the stored hash opened the candidate's data")
	}
	// What a member cannot do to a link: rewrite it, delete it; an interviewer reads none.
	for name, q := range map[string]string{"a token rewritten": `UPDATE nemura.candidate_access SET token_hash = sha256('x')`, "an expiry extended": `UPDATE nemura.candidate_access SET expires_at = expires_at + interval '1 day'`, "a link deleted": `DELETE FROM nemura.candidate_access`} {
		if err := c.do(func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err }); pgErr(err) != "NM217" && pgErr(err) != "42501" {
			t.Errorf("%s: %v", name, err)
		}
	}
	var seen int
	if err := store(t, "nemura_api").InOrgAs(ctx, c.org, c.iv, c.now, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.candidate_access`).Scan(&seen)
	}); err != nil || seen != 0 {
		t.Errorf("an interviewer reads the links through SQL: %d %v", seen, err)
	}
	// It expires.
	late := nemura.PipelineService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(c.now.AddDate(0, 0, 15))}
	if got, err := late.OwnData(ctx, made.Token); err != nil || got.Found {
		t.Errorf("an expired link: %+v %v", got, err)
	}
	// The consent is withdrawn once, by the candidate; the six months run from the withdrawal once they are not progressing.
	w, err := c.svc.OwnWithdraw(ctx, made.Token)
	if err != nil || w.ConsentFuture || !w.Withdrawn {
		t.Fatalf("%+v %v", w, err)
	}
	if _, err := c.svc.OwnWithdraw(ctx, made.Token); hireCode(err) != hiring.CodeConflict {
		t.Errorf("a consent withdrawn twice: %v", err)
	}
	if _, err := c.svc.OwnWithdraw(ctx, "nope.nope"); hireCode(err) != hiring.CodeConflict {
		t.Errorf("a withdrawal with no live link: %v", err)
	}
	// A revoked link answers not found, and is revoked once.
	second, err := c.svc.CreateAccess(ctx, hr, id, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.svc.RevokeAccess(ctx, hr, mustID(t, second.ID)); err != nil {
		t.Fatal(err)
	}
	if err := c.svc.RevokeAccess(ctx, hr, mustID(t, second.ID)); hireCode(err) != hiring.CodeConflict {
		t.Errorf("a link revoked twice: %v", err)
	}
	if got, err := c.svc.OwnData(ctx, second.Token); err != nil || got.Found {
		t.Errorf("a revoked link: %+v %v", got, err)
	}
	// Every look-up and withdrawal is audited, without a word the candidate gave or the team wrote.
	var viewed, leaked int
	if err := store(t, "nemura_worker").InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text LIKE '%nemura.candidate.data_viewed%'`).Scan(&viewed); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM nemura.audit_event WHERE body::text ~ '(data_viewed|access_link|consent_withdrawn)' AND (body::text LIKE '%Chen%' OR body::text LIKE '%FP&A%' OR body::text LIKE '%SECRET%' OR body::text LIKE '%'||$1||'%')`, strings.SplitN(made.Token, ".", 2)[1]).Scan(&leaked)
	}); err != nil {
		t.Fatal(err)
	}
	if viewed != 1 || leaked != 0 {
		t.Errorf("audit: %d look-ups of the live link, %d events carrying data or a token", viewed, leaked)
	}
}

// A hired candidate's record (the CV, the notes, the scores, the interviewers and the access links) is deleted six months after the start date through the retention engine, only after a person has reviewed
// the report; the person record and the hire stay (NM-REC-05, NM-SEC-05).
// req: NM-REC-05, NM-SEC-05
// asvs: V8.3.1, V8.2.1
func TestAHiredCandidatesRecordIsDeletedSixMonthsAfterTheStartDateAndThePersonStays(t *testing.T) {
	c := newHiringCo(t)
	ctx := context.Background()
	hr := c.sess(c.hr)
	cand, err := c.svc.Add(ctx, hr, c.req, api.CandidateInput{Name: "Hired Hana", Email: "hired@example.com", CV: "A CV."})
	if err != nil {
		t.Fatal(err)
	}
	id := mustID(t, cand.ID)
	if _, err := c.svc.Note(ctx, hr, id, "A note."); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Assign(ctx, hr, id, c.iv); err != nil {
		t.Fatal(err)
	}
	link, err := c.svc.CreateAccess(ctx, hr, id, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, to := range []string{"screening", "interview", "offer"} {
		if _, err := c.svc.Move(ctx, hr, id, to, ""); err != nil {
			t.Fatal(err)
		}
	}
	hired, err := c.svc.Hire(ctx, hr, id, api.HireInput{StartDate: "2099-04-01", EmploymentType: "full_time"})
	if err != nil || hired.RetentionUntil != "2099-10-01" {
		t.Fatalf("the day the record is due: %+v %v", hired, err)
	}
	if v, _ := c.svc.OwnData(ctx, link.Token); !v.Found || v.Stage != "hired" || v.RetentionUntil != "2099-10-01" {
		t.Errorf("the candidate is told the day: %+v", v)
	}
	worker := store(t, "nemura_worker")
	start := time.Date(2099, 4, 1, 12, 0, 0, 0, time.UTC)
	if del, rep, err := worker.RunCandidateRetention(ctx, c.org, start.AddDate(0, 5, 0)); err != nil || del != 0 || rep != 0 {
		t.Fatalf("before six months from the start: %d %d %v", del, rep, err)
	}
	due := start.AddDate(0, 6, 1)
	// The purge function refuses a hired candidate who is not due, even called straight.
	if err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `SELECT nemura.purge_candidates($1, $2::date)`, []uuid.UUID{id}, start.AddDate(0, 5, 0).Format(time.DateOnly))
		return err
	}); pgErr(err) != "NM215" {
		t.Errorf("a hired candidate purged early: %v", err)
	}
	if del, rep, err := worker.RunCandidateRetention(ctx, c.org, due); err != nil || del != 0 || rep != 1 {
		t.Fatalf("a report for review: %d %d %v", del, rep, err)
	}
	var report uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT id FROM nemura.deletion_report WHERE record_type = $1 AND state = 'pending'`, hiring.CandidatesRecordType).Scan(&report)
	})
	if del, _, err := worker.RunCandidateRetention(ctx, c.org, due); err != nil || del != 0 {
		t.Fatalf("nothing is deleted before the review: %d %v", del, err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ReviewDeletionReport(ctx, tx, c.org, c.now, c.owner, report, due)
	})
	if del, _, err := worker.RunCandidateRetention(ctx, c.org, due); err != nil || del != 1 {
		t.Fatalf("deleted after the review: %d %v", del, err)
	}
	super := connect(t, db.SuperURL())
	var candidates, notes, interviewers, links, hires, persons int
	if err := super.QueryRow(ctx, `SELECT (SELECT count(*) FROM nemura.candidate WHERE id = $1), (SELECT count(*) FROM nemura.candidate_note WHERE candidate_id = $1), (SELECT count(*) FROM nemura.candidate_interviewer WHERE candidate_id = $1),
(SELECT count(*) FROM nemura.candidate_access WHERE candidate_id = $1), (SELECT count(*) FROM nemura.hire WHERE candidate_id = $1), (SELECT count(*) FROM nemura.person WHERE id = $2)`, id, hired.Hire.PersonID).Scan(&candidates, &notes, &interviewers, &links, &hires, &persons); err != nil {
		t.Fatal(err)
	}
	if candidates+notes+interviewers+links != 0 || hires != 1 || persons != 1 {
		t.Errorf("the record went and the hire and the person stayed: candidate %d, notes %d, interviewers %d, links %d, hires %d, persons %d", candidates, notes, interviewers, links, hires, persons)
	}
	if v, err := c.svc.OwnData(ctx, link.Token); err != nil || v.Found {
		t.Errorf("a link of a deleted record: %+v %v", v, err)
	}
	// A candidate is still never deleted by anyone but the retention job, and a candidate in progress never.
	if err := c.do(func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM nemura.candidate`)
		return err
	}); pgErr(err) != "NM215" && pgErr(err) != "42501" {
		t.Errorf("a candidate deleted by a member: %v", err)
	}
}
