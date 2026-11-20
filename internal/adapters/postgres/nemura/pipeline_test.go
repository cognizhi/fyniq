//go:build local

package nemura_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
)

type hiringCo struct {
	*company
	svc        nemura.PipelineService
	now        time.Time
	req        uuid.UUID
	iv, iv2    uuid.UUID // interviewers
	outsider   uuid.UUID // a member with no hiring role
	sess       func(m uuid.UUID) api.Session
	candidates []uuid.UUID
}

func newHiringCo(t *testing.T) *hiringCo {
	c := &hiringCo{company: newCompany(t), now: today.Add(11 * time.Hour)}
	c.svc = nemura.PipelineService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(c.now)}
	c.sess = func(m uuid.UUID) api.Session { return api.Session{Organisation: c.org, Member: m} }
	c.must(func(ctx context.Context, tx pgx.Tx) (err error) {
		for name, dst := range map[string]*uuid.UUID{"Ian Interviewer": &c.iv, "Ivy Interviewer": &c.iv2, "Olive Outsider": &c.outsider} {
			if *dst, err = nemura.AddMember(ctx, tx, c.org, c.now, actor, name); err != nil {
				return err
			}
		}
		for _, m := range []uuid.UUID{c.iv, c.iv2} {
			if _, err = nemura.GrantRole(ctx, tx, c.org, c.now, c.hr, m, authz.Interviewer, today, nil); err != nil {
				return err
			}
		}
		in := api.RequisitionInput{Title: "Analyst", Department: "Finance", Reason: "new", SalaryMin: "5000.00", SalaryMax: "6000.00", TargetStart: "2099-06-01"}
		if c.req, err = nemura.RequestRequisition(ctx, tx, c.org, c.now, c.hr, in); err != nil {
			return err
		}
		return nemura.DecideRequisition(ctx, tx, c.org, c.now, c.owner, c.req, true, "")
	})
	return c
}

// A candidate moves along the board only as allowed and every move is kept; the hiring team's notes are not an interviewer's to read; an interviewer reads and scores only the candidates assigned to them and
// scores only a criterion of the kit; nobody outside the hiring team and the assigned interviewers reads a candidate (NM-REC-04). The kit's questions are checked for fair wording (NM-REC-03).
// req: NM-REC-03, NM-REC-04
// asvs: V8.2.1, V8.3.1, V4.2.1
func TestACandidateMovesAlongTheBoardAndAnInterviewerReadsAndScoresOnlyTheirAssignedCandidates(t *testing.T) {
	c := newHiringCo(t)
	ctx := context.Background()
	hr := c.sess(c.hr)
	// The kit: a flagged question is kept only with the genuine requirement recorded.
	kit := api.InterviewKit{Questions: []string{"Are you a young digital native?", "Walk us through a forecast you built."}, Criteria: []string{"Analytical skill", "Communication"}}
	if _, err := c.svc.SaveKit(ctx, hr, c.req, kit); hireCode(err) != hiring.CodeWording {
		t.Errorf("a question about age: %v", err)
	}
	kit.Questions[0] = "Tell us about a time you explained a number to a non-accountant."
	if k, err := c.svc.SaveKit(ctx, hr, c.req, kit); err != nil || !k.Exists || len(k.Findings) != 0 {
		t.Fatalf("%+v %v", k, err)
	}
	// Candidates are added to an approved requisition only, and what was given is checked.
	if _, err := c.svc.Add(ctx, hr, c.req, api.CandidateInput{Name: "Chen Yu", Email: "not an email"}); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("a bad email address: %v", err)
	}
	cand, err := c.svc.Add(ctx, hr, c.req, api.CandidateInput{Name: "Chen Yu", Email: "chen.yu@example.com", CV: "Five years in FP&A."})
	if err != nil || cand.Stage != "applied" || cand.ConsentFuture || len(cand.Moves) != 1 || cand.AppliedVia != "recruiter" {
		t.Fatalf("%+v %v", cand, err)
	}
	id := uuid.MustParse(cand.ID)
	// The board: hired is not a move, a step cannot be skipped, not progressing needs a reason.
	for name, tc := range map[string]struct{ to, reason string }{"hired by a move": {"hired", ""}, "applied to offer": {"offer", ""}, "no reason": {"not_progressing", ""}} {
		if _, err := c.svc.Move(ctx, hr, id, tc.to, tc.reason); hireCode(err) == "" {
			t.Errorf("%s was allowed", name)
		}
	}
	if cand, err = c.svc.Move(ctx, hr, id, "screening", ""); err != nil || cand.Stage != "screening" || len(cand.Moves) != 2 || cand.Moves[1].By != "Hana HR" {
		t.Fatalf("%+v %v", cand, err)
	}
	// Nobody else reads the candidate, and an interviewer not assigned reads nothing.
	for name, m := range map[string]uuid.UUID{"a member with no hiring role": c.outsider, "an interviewer not assigned": c.iv} {
		if l, err := c.svc.Candidates(ctx, c.sess(m), c.req); err != nil || len(l) != 0 {
			t.Errorf("%s lists %d candidates: %v", name, len(l), err)
		}
		if _, err := c.svc.Candidate(ctx, c.sess(m), id); hireCode(err) != hiring.CodeNotFound {
			t.Errorf("%s reads the candidate: %v", name, err)
		}
	}
	if _, err := c.svc.Note(ctx, hr, id, "Strong on forecasting."); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Assign(ctx, hr, id, c.outsider); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("assigning a member who is not an interviewer: %v", err)
	}
	if _, err := c.svc.Assign(ctx, hr, id, c.iv); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.Assign(ctx, hr, id, c.iv); hireCode(err) != hiring.CodeConflict {
		t.Errorf("assigning twice: %v", err)
	}
	// The assigned interviewer reads the candidate without the team's notes, scores a criterion of the kit, and cannot score another candidate's or a criterion that is not on the card.
	got, err := c.svc.Candidate(ctx, c.sess(c.iv), id)
	if err != nil || len(got.Notes) != 0 || got.Name != "Chen Yu" {
		t.Fatalf("the interviewer's view: %+v %v", got, err)
	}
	if _, err := c.svc.Score(ctx, c.sess(c.iv), id, "Favourite colour", 3, ""); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("a criterion not on the card: %v", err)
	}
	if _, err := c.svc.Score(ctx, c.sess(c.iv), id, "Analytical skill", 6, ""); hireCode(err) != hiring.CodeInvalid {
		t.Errorf("a score of 6: %v", err)
	}
	if _, err := c.svc.Score(ctx, c.sess(c.iv2), id, "Analytical skill", 4, ""); hireCode(err) != hiring.CodeNotFound {
		t.Errorf("an interviewer not assigned scores: %v", err)
	}
	if got, err = c.svc.Score(ctx, c.sess(c.iv), id, "Analytical skill", 4, "Worked the case well."); err != nil || len(got.Scores) != 1 || got.Scores[0].By != "Ian Interviewer" {
		t.Fatalf("%+v %v", got, err)
	}
	if team, err := c.svc.Candidate(ctx, hr, id); err != nil || len(team.Notes) != 1 || len(team.Scores) != 1 || len(team.InterviewerNames) != 1 {
		t.Errorf("the team's view: %+v %v", team, err)
	}
	// An interviewer cannot move a candidate, note one or add one: the database refuses what the API does not offer.
	for name, run := range map[string]func() error{
		"a move": func() error { _, err := c.svc.Move(ctx, c.sess(c.iv), id, "interview", ""); return err },
		"a note": func() error { _, err := c.svc.Note(ctx, c.sess(c.iv), id, "x"); return err },
		"an add": func() error {
			_, err := c.svc.Add(ctx, c.sess(c.iv), c.req, api.CandidateInput{Name: "A", Email: "a@example.com"})
			return err
		},
	} {
		if err := run(); err == nil {
			t.Errorf("an interviewer made %s", name)
		}
	}
	// What a candidate gave never changes, a score is never rewritten, and a candidate is not deleted straight away.
	for name, q := range map[string]string{
		"a candidate's name changed": `UPDATE nemura.candidate SET full_name = 'Someone Else'`,
		"a candidate deleted":        `DELETE FROM nemura.candidate`,
		"a score rewritten":          `UPDATE nemura.interview_score SET score = 5`,
		"a move deleted":             `DELETE FROM nemura.candidate_move`,
	} {
		if err := c.do(func(ctx context.Context, tx pgx.Tx) error { _, err := tx.Exec(ctx, q); return err }); pgErr(err) != "NM215" && pgErr(err) != "42501" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A candidate who is not progressing is kept six months from the decision unless they consent to be kept for future roles, and not past a withdrawal of that consent; then a report goes to a person, and
// only once it is reviewed is the candidate deleted, with their notes and scores and a proof (NM-REC-05, NM-SEC-05).
// req: NM-REC-05, NM-SEC-05
// asvs: V8.3.4
func TestACandidateNotProgressingIsDeletedSixMonthsLaterOnlyAfterAReviewedReportAndNeverWhileTheyConsentedToBeKept(t *testing.T) {
	c := newHiringCo(t)
	ctx := context.Background()
	hr := c.sess(c.hr)
	add := func(name, email string, keep bool) uuid.UUID {
		cand, err := c.svc.Add(ctx, hr, c.req, api.CandidateInput{Name: name, Email: email, ConsentFuture: keep})
		if err != nil {
			t.Fatal(err)
		}
		return uuid.MustParse(cand.ID)
	}
	drop := func(id uuid.UUID) api.Candidate {
		cand, err := c.svc.Move(ctx, hr, id, "not_progressing", "Not enough experience.")
		if err != nil {
			t.Fatal(err)
		}
		return cand
	}
	gone, kept, young := add("Gone", "gone@example.com", false), add("Kept", "kept@example.com", true), add("Young", "young@example.com", false)
	if _, err := c.svc.Note(ctx, hr, gone, "A note that goes with the candidate."); err != nil {
		t.Fatal(err)
	}
	if g := drop(gone); g.RetentionUntil != day6(c.now) || g.ConsentFuture {
		t.Errorf("the day the data is due: %+v", g)
	}
	if k := drop(kept); k.RetentionUntil != "" || !k.ConsentFuture {
		t.Errorf("a candidate who consented to be kept has no deletion day: %+v", k)
	}
	// Young is decided a month later by a service whose clock is later.
	later := nemura.PipelineService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(c.now.AddDate(0, 1, 0))}
	if _, err := later.Move(ctx, hr, young, "not_progressing", "Withdrew."); err != nil {
		t.Fatal(err)
	}
	worker := store(t, "nemura_worker")
	due := c.now.AddDate(0, 6, 1)
	if del, rep, err := worker.RunCandidateRetention(ctx, c.org, c.now.AddDate(0, 5, 0)); err != nil || del != 0 || rep != 0 {
		t.Fatalf("before six months: %d %d %v", del, rep, err)
	}
	if del, rep, err := worker.RunCandidateRetention(ctx, c.org, due); err != nil || del != 0 || rep != 1 {
		t.Fatalf("a report for review, one candidate due: %d %d %v", del, rep, err)
	}
	var report uuid.UUID
	var dueCount int
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id, due_count FROM nemura.deletion_report WHERE record_type = $1 AND state = 'pending'`, hiring.CandidatesRecordType).Scan(&report, &dueCount); err != nil {
			return err
		}
		return nil
	})
	if dueCount != 1 {
		t.Errorf("only the candidate decided six months ago is due: %d", dueCount)
	}
	// The purge function refuses a candidate who is too young or who consented, even called straight.
	for name, id := range map[string]uuid.UUID{"a candidate decided a month ago": young, "a candidate who consented to be kept": kept} {
		if err := worker.InOrg(ctx, c.org, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT nemura.purge_candidates($1, $2::date)`, []uuid.UUID{id}, due.Format(time.DateOnly))
			return err
		}); pgErr(err) != "NM215" {
			t.Errorf("%s was purged: %v", name, err)
		}
	}
	if del, _, err := worker.RunCandidateRetention(ctx, c.org, due); err != nil || del != 0 {
		t.Fatalf("nothing is deleted before the review: %d %v", del, err)
	}
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		return nemura.ReviewDeletionReport(ctx, tx, c.org, c.now, c.owner, report, due)
	})
	if del, _, err := worker.RunCandidateRetention(ctx, c.org, due); err != nil || del != 1 {
		t.Fatalf("deleted after review: %d %v", del, err)
	}
	var left, proofs int
	super := connect(t, db.SuperURL())
	if err := super.QueryRow(ctx, `SELECT (SELECT count(*) FROM nemura.candidate WHERE id = ANY($1)), (SELECT count(*) FROM nemura.deletion_proof WHERE report_id = $2 AND record_id = $3)`, []uuid.UUID{gone, kept, young}, report, gone).Scan(&left, &proofs); err != nil {
		t.Fatal(err)
	}
	var notes int
	if err := super.QueryRow(ctx, `SELECT count(*) FROM nemura.candidate_note WHERE candidate_id = $1`, gone).Scan(&notes); err != nil || notes != 0 {
		t.Errorf("the candidate's notes went with them: %d %v", notes, err)
	}
	if left != 2 || proofs != 1 {
		t.Errorf("only the due candidate went, with a proof: %d left, %d proofs", left, proofs)
	}
	// Consent is given once and withdrawn once; withdrawn, the six months run from the withdrawal.
	if _, err := later.Consent(ctx, hr, kept, true); hireCode(err) != hiring.CodeConflict {
		t.Errorf("consent given twice: %v", err)
	}
	withdrawer := nemura.PipelineService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(c.now.AddDate(0, 8, 0))}
	if k, err := withdrawer.Consent(ctx, hr, kept, false); err != nil || k.ConsentFuture || k.RetentionUntil != c.now.AddDate(0, 14, 0).Format(time.DateOnly) {
		t.Errorf("withdrawn: %+v %v", k, err)
	}
	if _, err := withdrawer.Consent(ctx, hr, kept, false); hireCode(err) != hiring.CodeConflict {
		t.Errorf("consent withdrawn twice: %v", err)
	}
}

func day6(t time.Time) string { return t.AddDate(0, 6, 0).Format(time.DateOnly) }

// An application made on the careers page is recorded for a post that is published and approved on a page that is switched on, and for no other; the visitor learns nothing more than whether it was
// recorded (NM-REC-04, NM-REC-05).
// req: NM-REC-04, NM-REC-05
// asvs: V4.2.1, V13.1.1
func TestAnApplicationOnTheCareersPageIsRecordedOnlyForAnOpenPostOfAPageThatIsOn(t *testing.T) {
	c := newHiringCo(t)
	ctx := context.Background()
	hr := c.sess(c.hr)
	careers := nemura.CareersService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(c.now)}
	slug := "apply-" + c.org.String()[:8]
	var post uuid.UUID
	c.must(func(ctx context.Context, tx pgx.Tx) error {
		if err := nemura.WriteJobPost(ctx, tx, c.org, c.now, c.hr, c.req, "Analyst", "You will analyse the books."); err != nil {
			return err
		}
		if err := nemura.PublishJobPost(ctx, tx, c.org, c.now, c.hr, c.req, ""); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT id FROM nemura.job_post WHERE requisition_id = $1`, c.req).Scan(&post)
	})
	in := api.CandidateInput{Name: "Mei Tan", Email: "mei.tan@example.com", CV: "CPA.", ConsentFuture: true}
	if ok, err := c.svc.Apply(ctx, slug, post, in); err != nil || ok {
		t.Fatalf("a page that was never set up: %v %v", ok, err)
	}
	if _, err := careers.Save(ctx, hr, api.CareersSettings{Slug: slug, Enabled: true, Company: "Acme"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := c.svc.Apply(ctx, slug, uuid.New(), in); err != nil || ok {
		t.Fatalf("a post that is not there: %v %v", ok, err)
	}
	if ok, err := c.svc.Apply(ctx, slug, post, api.CandidateInput{Name: "X", Email: "bad"}); hireCode(err) != hiring.CodeInvalid || ok {
		t.Fatalf("an invalid application: %v %v", ok, err)
	}
	if ok, err := c.svc.Apply(ctx, slug, post, in); err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	l, err := c.svc.Candidates(ctx, hr, c.req)
	if err != nil || len(l) != 1 || l[0].AppliedVia != "careers_page" || !l[0].ConsentFuture || l[0].Stage != "applied" {
		t.Fatalf("%+v %v", l, err)
	}
	// Another company's page cannot be used to apply for this company's post.
	other := newHiringCo(t)
	oslug := "other-" + other.org.String()[:8]
	otherCareers := nemura.CareersService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(c.now)}
	if _, err := otherCareers.Save(ctx, other.sess(other.hr), api.CareersSettings{Slug: oslug, Enabled: true, Company: "Other"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := c.svc.Apply(ctx, oslug, post, in); err != nil || ok {
		t.Errorf("applying for a post through another company's page: %v %v", ok, err)
	}
	// Switched off, the page takes no application.
	if _, err := careers.Save(ctx, hr, api.CareersSettings{Slug: slug, Enabled: false, Company: "Acme"}); err != nil {
		t.Fatal(err)
	}
	if ok, err := c.svc.Apply(ctx, slug, post, in); err != nil || ok {
		t.Errorf("a page that is off: %v %v", ok, err)
	}
}
