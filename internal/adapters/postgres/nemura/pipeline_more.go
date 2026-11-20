package nemura

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
)

// What NM-REC-04 and NM-REC-05 left (PRD-002 rev 0.74): an interviewer's own candidates, the members who can be assigned, and a candidate's own look at their data through a link the hiring team makes.
// Who reads what is the database's (migration 000071): the interviewer's list is the candidates the row-level policy lets them read, and the candidate's look is two functions that answer for a live token
// and nothing else.

// MyInterviews is the candidates assigned to the caller, with the scorecard's criteria and the criteria the caller has scored.
func (p PipelineService) MyInterviews(ctx context.Context, s api.Session) (out []api.MyInterview, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, _ time.Time) error {
		rows, err := tx.Query(ctx, `SELECT c.id, c.full_name, c.stage, c.requisition_id, r.title, coalesce(k.criteria, '{}'::text[]),
  coalesce((SELECT array_agg(DISTINCT x.criterion ORDER BY x.criterion) FROM nemura.interview_score x WHERE x.candidate_id = c.id AND x.member_id = $1), '{}'::text[])
FROM nemura.candidate_interviewer i
JOIN nemura.candidate c ON c.organisation_id = i.organisation_id AND c.id = i.candidate_id
JOIN nemura.requisition r ON r.organisation_id = c.organisation_id AND r.id = c.requisition_id
LEFT JOIN nemura.interview_kit k ON k.organisation_id = c.organisation_id AND k.requisition_id = c.requisition_id
WHERE i.member_id = $1 ORDER BY c.stage_at DESC, c.id`, s.Member)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m api.MyInterview
			if err := rows.Scan(&m.CandidateID, &m.Name, &m.Stage, &m.RequisitionID, &m.RequisitionTitle, &m.Criteria, &m.Scored); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// Interviewers is the members who hold the Interviewer role today, for the page that assigns them.
func (p PipelineService) Interviewers(ctx context.Context, s api.Session) (out []api.Interviewer, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT m.id, m.display_name FROM nemura.role_assignment a JOIN nemura.member m ON m.organisation_id = a.organisation_id AND m.id = a.member_id
WHERE a.role = 'interviewer' AND a.effective_from <= $1::date AND (a.effective_to IS NULL OR a.effective_to > $1::date) ORDER BY m.display_name, m.id`, day(at))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var i api.Interviewer
			if err := rows.Scan(&i.MemberID, &i.Name); err != nil {
				return err
			}
			out = append(out, i)
		}
		return rows.Err()
	})
	return out, err
}

// The candidate's access link: its days, and the prefix that keeps its hash apart from any other token's.
const (
	candidateAccessDefaultDays = 14
	candidateAccessMaxDays     = 30
	candidateAccessPrefix      = "nemura-candidate-access|"
)

func candidateAccessHash(token string) []byte {
	sum := sha256.Sum256([]byte(candidateAccessPrefix + token))
	return sum[:]
}

// CreateAccess makes a link for a candidate to look at their own data, for the days chosen (1 to 30, 14 by default). The token is shown once; only its hash is kept.
func (p PipelineService) CreateAccess(ctx context.Context, s api.Session, candidate uuid.UUID, days int) (out api.CandidateAccessCreated, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		if days == 0 {
			days = candidateAccessDefaultDays
		}
		if days < 1 || days > candidateAccessMaxDays {
			return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "A candidate's link works for 1 to 30 days: choose how long the candidate may look."}
		}
		if _, err := oneCandidate(ctx, tx, candidate, false); err != nil {
			return err
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		token := s.Organisation.String() + "." + base64.RawURLEncoding.EncodeToString(raw)
		exp := at.Add(time.Duration(days) * 24 * time.Hour)
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.candidate_access (id, candidate_id, token_hash, expires_at, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6)`, id, candidate, candidateAccessHash(token), exp, s.Member, at); err != nil {
			return hiringError(err)
		}
		out = api.CandidateAccessCreated{ID: id.String(), Token: token, ExpiresAt: exp.UTC().Format(time.RFC3339), Path: "/my-application"}
		return Audit(ctx, tx, s.Organisation, at, user(s.Member), "nemura.candidate.access_link_created", audit.Object{Type: "candidate", ID: candidate}, map[string]audit.Change{"link": {nil, id.String()}, "expires_at": {nil, exp.UTC().Format(time.RFC3339)}}, "")
	})
	return out, err
}

// AccessLinks lists the links made for a candidate, newest first, with no token.
func (p PipelineService) AccessLinks(ctx context.Context, s api.Session, candidate uuid.UUID) (out []api.CandidateAccessLink, err error) {
	err = p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, _ time.Time) error {
		if _, err := oneCandidate(ctx, tx, candidate, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, created_at, expires_at, revoked_at FROM nemura.candidate_access WHERE candidate_id = $1 ORDER BY created_at DESC, id DESC`, candidate)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l api.CandidateAccessLink
			var created, exp time.Time
			var revoked *time.Time
			if err := rows.Scan(&l.ID, &created, &exp, &revoked); err != nil {
				return err
			}
			l.CreatedAt, l.ExpiresAt = created.UTC().Format(time.RFC3339), exp.UTC().Format(time.RFC3339)
			if revoked != nil {
				l.RevokedAt = revoked.UTC().Format(time.RFC3339)
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, err
}

// RevokeAccess revokes a candidate's link, once.
func (p PipelineService) RevokeAccess(ctx context.Context, s api.Session, link uuid.UUID) error {
	return p.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		var candidate uuid.UUID
		var revoked *time.Time
		err := tx.QueryRow(ctx, `SELECT candidate_id, revoked_at FROM nemura.candidate_access WHERE id = $1 FOR UPDATE`, link).Scan(&candidate, &revoked)
		if errors.Is(err, pgx.ErrNoRows) {
			return &hiring.Refusal{Code: hiring.CodeNotFound, Message: "There is no such link."}
		}
		if err != nil {
			return err
		}
		if revoked != nil {
			return &hiring.Refusal{Code: hiring.CodeConflict, Message: "That link is revoked already."}
		}
		if _, err := tx.Exec(ctx, `UPDATE nemura.candidate_access SET revoked_at = $2 WHERE id = $1`, link, at); err != nil {
			return hiringError(err)
		}
		return Audit(ctx, tx, s.Organisation, at, user(s.Member), "nemura.candidate.access_link_revoked", audit.Object{Type: "candidate", ID: candidate}, map[string]audit.Change{"revoked": {false, true}}, "")
	})
}

var candidateLinkActor = audit.Actor{Type: "service", ID: "candidate-access"}

// ownView is what a live link shows: what the candidate gave and where they stand, never what the team wrote.
func ownView(r pgx.Row) (v api.CandidateOwnView, id uuid.UUID, err error) {
	var cv *string
	var stageAt, appliedAt, exp time.Time
	var consent, withdrawn *time.Time
	var hireStart *time.Time
	var access uuid.UUID
	if err = r.Scan(&access, &id, &v.Name, &v.Email, &cv, &v.Stage, &stageAt, &appliedAt, &v.AppliedVia, &consent, &withdrawn, &v.Role, &hireStart, &exp); err != nil {
		return v, id, err
	}
	v.Found = true
	if cv != nil {
		v.CV = *cv
	}
	v.StageAt, v.AppliedAt, v.LinkExpiresAt = stageAt.UTC().Format(time.RFC3339), appliedAt.UTC().Format(time.RFC3339), exp.UTC().Format(time.RFC3339)
	v.ConsentFuture = consent != nil && withdrawn == nil
	v.Withdrawn = withdrawn != nil
	switch {
	case v.Stage == "not_progressing" && !v.ConsentFuture:
		from := stageAt
		if withdrawn != nil && withdrawn.After(from) {
			from = *withdrawn
		}
		v.RetentionUntil = day(from.AddDate(0, hiring.CandidateRetentionMonths, 0))
	case v.Stage == "hired" && hireStart != nil:
		v.RetentionUntil = day(hireStart.AddDate(0, hiring.CandidateRetentionMonths, 0))
	}
	return v, id, nil
}

const ownViewQuery = `SELECT access_id, candidate_id, full_name, email, cv_text, stage, stage_at, applied_at, applied_via, consent_future_at, consent_future_withdrawn_at, role_title, hire_start, expires_at FROM nemura.candidate_access_view($1, $2)`

// OwnData is what a live link shows its candidate, with no sign-in. A token that is unknown, malformed, expired or revoked is not found, all alike. Every look-up of a live token is audited without the data.
func (p PipelineService) OwnData(ctx context.Context, token string) (api.CandidateOwnView, error) {
	org, ok := LinkOrganisation(token)
	if !ok {
		return api.CandidateOwnView{}, nil
	}
	now := p.Clock.Now()
	var out api.CandidateOwnView
	err := p.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		v, id, err := ownView(tx.QueryRow(ctx, ownViewQuery, token, now))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out = v
		return Audit(ctx, tx, org, now, candidateLinkActor, "nemura.candidate.data_viewed", audit.Object{Type: "candidate", ID: id}, nil, "")
	})
	return out, err
}

// OwnWithdraw is the candidate's withdrawal of the consent to be kept for future roles, through a live link: once, and only after it was given. A link that is not live is told apart from nothing to withdraw
// only by the words, never by the status.
func (p PipelineService) OwnWithdraw(ctx context.Context, token string) (api.CandidateOwnView, error) {
	nothing := &hiring.Refusal{Code: hiring.CodeConflict, Message: "There is no consent of yours to withdraw: the link may have expired, or the consent was not given or is withdrawn already."}
	org, ok := LinkOrganisation(token)
	if !ok {
		return api.CandidateOwnView{}, nothing
	}
	now := p.Clock.Now()
	var out api.CandidateOwnView
	err := p.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		var id *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT nemura.candidate_access_withdraw($1, $2)`, token, now).Scan(&id); err != nil {
			return hiringError(err)
		}
		if id == nil {
			return nothing
		}
		if err := Audit(ctx, tx, org, now, candidateLinkActor, "nemura.candidate.consent_withdrawn", audit.Object{Type: "candidate", ID: *id}, nil, ""); err != nil {
			return err
		}
		v, _, err := ownView(tx.QueryRow(ctx, ownViewQuery, token, now))
		out = v
		return err
	})
	return out, err
}
