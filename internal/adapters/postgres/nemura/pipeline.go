package nemura

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
	"github.com/cognizhi/fyniq/internal/nemura/retention"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The pipeline (PRD-002 NM-REC-04, NM-REC-05): candidates, their moves, notes, scores and consent, the interview kit, the careers application and the retention of those not progressing. Who reads what
// is the database's (migration 000069): the services run as the acting member, so a missed check here cannot read more.

var noCandidate = &hiring.Refusal{Code: hiring.CodeNotFound, Message: "There is no such candidate."}

// PipelineService is the candidates behind the API.
type PipelineService struct {
	Store *Store
	Clock ports.Clock
}

func (p PipelineService) as(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx, at time.Time) error) error {
	at := p.Clock.Now()
	return p.Store.InOrgAs(ctx, s.Organisation, s.Member, at, func(ctx context.Context, tx pgx.Tx) error { return fn(ctx, tx, at) })
}

const candidateColumns = `c.id, c.requisition_id, c.full_name, c.email, coalesce(c.cv_text, ''), c.stage, coalesce(c.stage_reason, ''), c.stage_at, c.applied_via, c.applied_at, c.consent_future_at, c.consent_future_withdrawn_at`

func scanCandidate(row pgx.Row, withCV bool) (api.Candidate, error) {
	var c api.Candidate
	var rid uuid.UUID
	var stageAt, appliedAt time.Time
	var consent, withdrawn *time.Time
	if err := row.Scan(&c.ID, &rid, &c.Name, &c.Email, &c.CV, &c.Stage, &c.StageReason, &stageAt, &c.AppliedVia, &appliedAt, &consent, &withdrawn); err != nil {
		return c, err
	}
	c.RequisitionID, c.StageAt, c.AppliedAt = rid.String(), stageAt.UTC().Format(time.RFC3339), appliedAt.UTC().Format(time.RFC3339)
	if !withCV {
		c.CV = ""
	}
	if consent != nil {
		c.ConsentFutureAt = consent.UTC().Format(time.RFC3339)
	}
	if withdrawn != nil {
		c.ConsentWithdrawnAt = withdrawn.UTC().Format(time.RFC3339)
	}
	c.ConsentFuture = consent != nil && withdrawn == nil
	if c.Stage == "not_progressing" && !c.ConsentFuture {
		from := stageAt
		if withdrawn != nil && withdrawn.After(from) {
			from = *withdrawn
		}
		c.RetentionUntil = day(from.AddDate(0, hiring.CandidateRetentionMonths, 0))
	}
	return c, nil
}

func oneCandidate(ctx context.Context, tx pgx.Tx, id uuid.UUID, detail bool) (api.Candidate, error) {
	c, err := scanCandidate(tx.QueryRow(ctx, `SELECT `+candidateColumns+` FROM nemura.candidate c WHERE c.id = $1`, id), detail)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, noCandidate
	}
	if err != nil || !detail {
		return c, err
	}
	rows, err := tx.Query(ctx, `SELECT coalesce(m.from_stage, ''), m.to_stage, coalesce(m.reason, ''), coalesce(b.display_name, ''), m.at FROM nemura.candidate_move m LEFT JOIN nemura.member b ON b.organisation_id = m.organisation_id AND b.id = m.by_member WHERE m.candidate_id = $1 ORDER BY m.at, m.id`, id)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var m api.CandidateMove
		var at time.Time
		if err := rows.Scan(&m.From, &m.To, &m.Reason, &m.By, &at); err != nil {
			rows.Close()
			return c, err
		}
		m.At = at.UTC().Format(time.RFC3339)
		c.Moves = append(c.Moves, m)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT n.body, b.display_name, n.at FROM nemura.candidate_note n JOIN nemura.member b ON b.organisation_id = n.organisation_id AND b.id = n.by_member WHERE n.candidate_id = $1 ORDER BY n.at, n.id`, id)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var n api.CandidateNote
		var at time.Time
		if err := rows.Scan(&n.Body, &n.By, &at); err != nil {
			rows.Close()
			return c, err
		}
		n.At = at.UTC().Format(time.RFC3339)
		c.Notes = append(c.Notes, n)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT x.criterion, x.score, coalesce(x.comment, ''), b.display_name, x.at FROM nemura.interview_score x JOIN nemura.member b ON b.organisation_id = x.organisation_id AND b.id = x.member_id WHERE x.candidate_id = $1 ORDER BY x.at, x.id`, id)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var x api.CandidateScore
		var at time.Time
		if err := rows.Scan(&x.Criterion, &x.Score, &x.Comment, &x.By, &at); err != nil {
			rows.Close()
			return c, err
		}
		x.At = at.UTC().Format(time.RFC3339)
		c.Scores = append(c.Scores, x)
	}
	rows.Close()
	rows, err = tx.Query(ctx, `SELECT b.display_name FROM nemura.candidate_interviewer i JOIN nemura.member b ON b.organisation_id = i.organisation_id AND b.id = i.member_id WHERE i.candidate_id = $1 ORDER BY i.assigned_at`, id)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return c, err
		}
		c.InterviewerNames = append(c.InterviewerNames, name)
	}
	return c, rows.Err()
}

func (p PipelineService) Candidates(ctx context.Context, s api.Session, requisition uuid.UUID) (out []api.Candidate, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, _ time.Time) error {
		rows, err := tx.Query(ctx, `SELECT `+candidateColumns+` FROM nemura.candidate c WHERE c.requisition_id = $1 ORDER BY c.applied_at DESC, c.id`, requisition)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanCandidate(rows, false)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

func (p PipelineService) Candidate(ctx context.Context, s api.Session, id uuid.UUID) (c api.Candidate, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, _ time.Time) (e error) {
		c, e = oneCandidate(ctx, tx, id, true)
		return
	})
	return c, err
}

// addCandidate inserts a candidate at the applied stage with its first move; by is the member who added them, nil for an application on the careers page.
func addCandidate(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by *uuid.UUID, actor audit.Actor, requisition uuid.UUID, via string, in api.CandidateInput) (uuid.UUID, error) {
	if err := hiring.CheckCandidate(in.Name, in.Email, in.CV); err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	var consent *time.Time
	if in.ConsentFuture {
		consent = &at
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.candidate (id, requisition_id, full_name, email, cv_text, stage_at, applied_via, applied_at, consent_future_at, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $6, $8, $9)`,
		id, requisition, strings.TrimSpace(in.Name), strings.TrimSpace(in.Email), orNil(in.CV), at, via, consent, by); err != nil {
		return uuid.Nil, hiringError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.candidate_move (candidate_id, to_stage, by_member, at) VALUES ($1, 'applied', $2, $3)`, id, by, at); err != nil {
		return uuid.Nil, hiringError(err)
	}
	return id, Audit(ctx, tx, org, at, actor, "nemura.candidate.added", audit.Object{Type: "candidate", ID: id}, map[string]audit.Change{"via": {nil, via}, "consent_future": {nil, in.ConsentFuture}}, "")
}

func (p PipelineService) Add(ctx context.Context, s api.Session, requisition uuid.UUID, in api.CandidateInput) (c api.Candidate, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		state, _, err := openRequisition(ctx, tx, requisition)
		if err != nil {
			return err
		}
		if state != "approved" {
			return &hiring.Refusal{Code: hiring.CodeConflict, Message: "Candidates are added to an approved requisition: ask the Owner to approve it first."}
		}
		id, err := addCandidate(ctx, tx, s.Organisation, at, &s.Member, user(s.Member), requisition, "recruiter", in)
		if err != nil {
			return err
		}
		c, err = oneCandidate(ctx, tx, id, true)
		return err
	})
	return c, err
}

// Apply records an application made on the careers page. The target function names the organisation and requisition for a page that is on and a post that is open, and nothing otherwise.
func (p PipelineService) Apply(ctx context.Context, slug string, post uuid.UUID, in api.CandidateInput) (bool, error) {
	var org, requisition uuid.UUID
	err := p.Store.pool.QueryRow(ctx, `SELECT organisation_id, requisition_id FROM nemura.careers_application_target($1, $2)`, slug, post).Scan(&org, &requisition)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	at := p.Clock.Now()
	err = p.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		_, err := addCandidate(ctx, tx, org, at, nil, audit.Actor{Type: "service", ID: "careers-application"}, requisition, "careers_page", in)
		return err
	})
	return err == nil, err
}

func (p PipelineService) Move(ctx context.Context, s api.Session, id uuid.UUID, to, reason string) (c api.Candidate, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		var from string
		if err := tx.QueryRow(ctx, `SELECT stage FROM nemura.candidate WHERE id = $1 FOR UPDATE`, id).Scan(&from); errors.Is(err, pgx.ErrNoRows) {
			return noCandidate
		} else if err != nil {
			return err
		}
		if to == "hired" {
			return &hiring.Refusal{Code: hiring.CodeConflict, Message: "A candidate is hired by the offer, not by a move: record the offer's acceptance."}
		}
		if err := hiring.CheckMove(from, to, reason); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE nemura.candidate SET stage = $2, stage_reason = $3, stage_at = $4 WHERE id = $1`, id, to, orNil(strings.TrimSpace(reason)), at); err != nil {
			return hiringError(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.candidate_move (candidate_id, from_stage, to_stage, reason, by_member, at) VALUES ($1, $2, $3, $4, $5, $6)`, id, from, to, orNil(strings.TrimSpace(reason)), s.Member, at); err != nil {
			return hiringError(err)
		}
		if err := Audit(ctx, tx, s.Organisation, at, user(s.Member), "nemura.candidate.moved", audit.Object{Type: "candidate", ID: id}, map[string]audit.Change{"stage": {from, to}}, strings.TrimSpace(reason)); err != nil {
			return err
		}
		c, err = oneCandidate(ctx, tx, id, true)
		return err
	})
	return c, err
}

func (p PipelineService) Note(ctx context.Context, s api.Session, id uuid.UUID, body string) (c api.Candidate, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		if err := hiring.CheckNote(body); err != nil {
			return err
		}
		if _, err := oneCandidate(ctx, tx, id, false); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.candidate_note (candidate_id, body, by_member, at) VALUES ($1, $2, $3, $4)`, id, strings.TrimSpace(body), s.Member, at); err != nil {
			return hiringError(err)
		}
		if err := Audit(ctx, tx, s.Organisation, at, user(s.Member), "nemura.candidate.noted", audit.Object{Type: "candidate", ID: id}, nil, ""); err != nil {
			return err
		}
		c, err = oneCandidate(ctx, tx, id, true)
		return err
	})
	return c, err
}

func (p PipelineService) Assign(ctx context.Context, s api.Session, id, member uuid.UUID) (c api.Candidate, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		if _, err := oneCandidate(ctx, tx, id, false); err != nil {
			return err
		}
		var holds bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nemura.role_assignment a WHERE a.member_id = $1 AND a.role = 'interviewer' AND a.effective_from <= $2::date AND (a.effective_to IS NULL OR a.effective_to > $2::date))`, member, day(at)).Scan(&holds); err != nil {
			return err
		}
		if !holds {
			return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "That member does not hold the Interviewer role: give them the role first."}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.candidate_interviewer (candidate_id, member_id, assigned_by, assigned_at) VALUES ($1, $2, $3, $4)`, id, member, s.Member, at); err != nil {
			return hiringError(err)
		}
		if err := Audit(ctx, tx, s.Organisation, at, user(s.Member), "nemura.candidate.interviewer_assigned", audit.Object{Type: "candidate", ID: id}, map[string]audit.Change{"interviewer": {nil, member.String()}}, ""); err != nil {
			return err
		}
		c, err = oneCandidate(ctx, tx, id, true)
		return err
	})
	return c, err
}

func (p PipelineService) Score(ctx context.Context, s api.Session, id uuid.UUID, criterion string, score int, comment string) (c api.Candidate, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		if err := hiring.CheckScore(score, comment); err != nil {
			return err
		}
		cand, err := oneCandidate(ctx, tx, id, false)
		if err != nil {
			return err // an interviewer who is not assigned reads no candidate: there is none for them
		}
		var criteria []string
		if err := tx.QueryRow(ctx, `SELECT criteria FROM nemura.interview_kit WHERE requisition_id = $1`, cand.RequisitionID).Scan(&criteria); errors.Is(err, pgx.ErrNoRows) {
			return &hiring.Refusal{Code: hiring.CodeConflict, Message: "The requisition has no interview kit yet: the recruiter saves one first."}
		} else if err != nil {
			return err
		}
		known := false
		for _, k := range criteria {
			known = known || k == criterion
		}
		if !known {
			return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "Score a criterion of the kit's scorecard."}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.interview_score (candidate_id, member_id, criterion, score, comment, at) VALUES ($1, $2, $3, $4, $5, $6)`, id, s.Member, criterion, score, orNil(strings.TrimSpace(comment)), at); err != nil {
			return hiringError(err)
		}
		if err := Audit(ctx, tx, s.Organisation, at, user(s.Member), "nemura.candidate.scored", audit.Object{Type: "candidate", ID: id}, map[string]audit.Change{"criterion": {nil, criterion}, "score": {nil, score}}, ""); err != nil {
			return err
		}
		c, err = oneCandidate(ctx, tx, id, true)
		return err
	})
	return c, err
}

func (p PipelineService) Consent(ctx context.Context, s api.Session, id uuid.UUID, given bool) (c api.Candidate, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		var consent, withdrawn *time.Time
		if err := tx.QueryRow(ctx, `SELECT consent_future_at, consent_future_withdrawn_at FROM nemura.candidate WHERE id = $1 FOR UPDATE`, id).Scan(&consent, &withdrawn); errors.Is(err, pgx.ErrNoRows) {
			return noCandidate
		} else if err != nil {
			return err
		}
		if given && consent != nil || !given && (consent == nil || withdrawn != nil) {
			return &hiring.Refusal{Code: hiring.CodeConflict, Message: "That consent is given once and withdrawn once, and only after it was given: read the candidate again."}
		}
		q, action := `UPDATE nemura.candidate SET consent_future_at = $2 WHERE id = $1`, "nemura.candidate.consent_given"
		if !given {
			q, action = `UPDATE nemura.candidate SET consent_future_withdrawn_at = $2 WHERE id = $1`, "nemura.candidate.consent_withdrawn"
		}
		if _, err := tx.Exec(ctx, q, id, at); err != nil {
			return hiringError(err)
		}
		if err := Audit(ctx, tx, s.Organisation, at, user(s.Member), action, audit.Object{Type: "candidate", ID: id}, nil, ""); err != nil {
			return err
		}
		c, err = oneCandidate(ctx, tx, id, true)
		return err
	})
	return c, err
}

func kitView(questions, criteria []string, note *string, exists bool) api.InterviewKit {
	k := api.InterviewKit{Exists: exists, Questions: append([]string{}, questions...), Criteria: append([]string{}, criteria...), Findings: []api.WordingFinding{}}
	if note != nil {
		k.RequirementNote = *note
	}
	for _, f := range hiring.FairWording(strings.Join(k.Questions, "\n") + "\n" + strings.Join(k.Criteria, "\n")) {
		k.Findings = append(k.Findings, api.WordingFinding{Category: f.Category, Phrase: f.Phrase})
	}
	return k
}

func (p PipelineService) Kit(ctx context.Context, s api.Session, requisition uuid.UUID) (k api.InterviewKit, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, _ time.Time) error {
		var q, c []string
		var note *string
		e := tx.QueryRow(ctx, `SELECT questions, criteria, requirement_note FROM nemura.interview_kit WHERE requisition_id = $1`, requisition).Scan(&q, &c, &note)
		if errors.Is(e, pgx.ErrNoRows) {
			k = kitView(nil, nil, nil, false)
			return nil
		}
		k = kitView(q, c, note, true)
		return e
	})
	return k, err
}

func (p PipelineService) SaveKit(ctx context.Context, s api.Session, requisition uuid.UUID, in api.InterviewKit) (k api.InterviewKit, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		if _, _, err := openRequisition(ctx, tx, requisition); err != nil {
			return err
		}
		if _, err := hiring.CheckKit(hiring.Kit{Questions: in.Questions, Criteria: in.Criteria, RequirementNote: in.RequirementNote}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.interview_kit (requisition_id, questions, criteria, requirement_note, updated_by, updated_at) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (organisation_id, requisition_id) DO UPDATE SET questions = EXCLUDED.questions, criteria = EXCLUDED.criteria, requirement_note = EXCLUDED.requirement_note, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
			requisition, in.Questions, in.Criteria, orNil(strings.TrimSpace(in.RequirementNote)), s.Member, at); err != nil {
			return hiringError(err)
		}
		if err := Audit(ctx, tx, s.Organisation, at, user(s.Member), "nemura.interview_kit.saved", audit.Object{Type: "requisition", ID: requisition}, map[string]audit.Change{"questions": {nil, len(in.Questions)}, "criteria": {nil, len(in.Criteria)}}, ""); err != nil {
			return err
		}
		var note *string
		if n := strings.TrimSpace(in.RequirementNote); n != "" {
			note = &n
		}
		k = kitView(in.Questions, in.Criteria, note, true)
		return nil
	})
	return k, err
}

// candidateSource is the candidates' side of the retention engine: those not progressing without the future-roles consent, by identity, the day the six months run from and a fingerprint, deleted only
// through the purge function, which checks the period and the consent again (NM-REC-05, NM-SEC-05).
type candidateSource struct{ now time.Time }

func (c candidateSource) Records(ctx context.Context, tx pgx.Tx) ([]retention.Record, error) {
	rows, err := tx.Query(ctx, `SELECT id, decided_on, fingerprint FROM nemura.candidates_for_retention()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []retention.Record
	for rows.Next() {
		var r retention.Record
		if err := rows.Scan(&r.ID, &r.From, &r.Content); err != nil {
			return nil, err
		}
		r.Subject = r.ID // a hold names the record type or one candidate
		out = append(out, r)
	}
	return out, rows.Err()
}

func (c candidateSource) Delete(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT nemura.purge_candidates($1, $2::date)`, ids, day(c.now))
	return err
}

// CandidateRetentionPolicy is how long the data of a candidate who is not progressing is kept: six months from the decision, unless they consented to be kept for future roles.
var CandidateRetentionPolicy = retention.Policy{RecordType: hiring.CandidatesRecordType, MinMonths: hiring.CandidateRetentionMonths}

// RunCandidateRetention is the nightly job for candidates: it deletes those of a report a person has reviewed, if still due and not held, with a proof of each, then reports what is due now if no
// report is waiting for its review (never silent). It returns how many it deleted and how many reports it made.
func (s *Store) RunCandidateRetention(ctx context.Context, org uuid.UUID, now time.Time) (deleted, reported int, err error) {
	src := candidateSource{now: now}
	err = s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM nemura.deletion_report WHERE record_type = $1 AND state = 'reviewed' ORDER BY created_on, id`, hiring.CandidatesRecordType)
		if err != nil {
			return err
		}
		var reviewed []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			reviewed = append(reviewed, id)
		}
		rows.Close()
		for _, id := range reviewed {
			n, err := RunReviewedDeletion(ctx, tx, org, now, now, id, CandidateRetentionPolicy, src)
			if err != nil {
				return err
			}
			deleted += n
		}
		var pending int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.deletion_report WHERE record_type = $1 AND state = 'pending'`, hiring.CandidatesRecordType).Scan(&pending); err != nil || pending > 0 {
			return err
		}
		id, _, err := ReportDueDeletions(ctx, tx, org, now, now, CandidateRetentionPolicy, src)
		if id != uuid.Nil {
			reported++
		}
		return err
	})
	return deleted, reported, err
}
