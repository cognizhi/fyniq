package nemura

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/hiring"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Hiring (PRD-002 NM-REC-01 to NM-REC-03): requisitions approved by the Owner and a job post for each, checked for fair wording, with the Fair Consideration Framework's advert dates kept as evidence. The audit
// events name identities, states and counts, never the words of a post. Money is NUMERIC(20,4) and a decimal string, never a float.

func hiringError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM213":
			return &hiring.Refusal{Code: hiring.CodeConflict, Message: "That has moved on: a requisition is decided once and a published post never changes. Read it again."}
		case "23505":
			return &hiring.Refusal{Code: hiring.CodeConflict, Message: "That exists already: a requisition has one job post."}
		case "23514":
			return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "That is not a valid requisition or post: check the lengths and the salary range."}
		}
	}
	return err
}

var noRequisition = &hiring.Refusal{Code: hiring.CodeNotFound, Message: "There is no such requisition."}

func dayOrEmpty(t *time.Time) string {
	if t == nil {
		return ""
	}
	return day(*t)
}

// RequestRequisition records a request to hire.
func RequestRequisition(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, in api.RequisitionInput) (uuid.UUID, error) {
	start, err := time.Parse(time.DateOnly, in.TargetStart)
	if err != nil {
		return uuid.Nil, &hiring.Refusal{Code: hiring.CodeInvalid, Message: "The target start date is not a date: write it as 2027-02-01."}
	}
	var replaces *uuid.UUID
	if in.ReplacesID != "" {
		u, err := uuid.Parse(in.ReplacesID)
		if err != nil {
			return uuid.Nil, &hiring.Refusal{Code: hiring.CodeNotFound, Message: "The person to replace is not in this organisation: choose them from the directory."}
		}
		replaces = &u
	}
	if err := hiring.Check(hiring.Requisition{Title: in.Title, Department: in.Department, Reason: in.Reason, ReplacesPerson: replaces != nil, SalaryMin: in.SalaryMin, SalaryMax: in.SalaryMax, TargetStart: start}); err != nil {
		return uuid.Nil, err
	}
	if replaces != nil {
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.person WHERE id = $1`, *replaces).Scan(&n); err != nil {
			return uuid.Nil, err
		}
		if n == 0 {
			return uuid.Nil, &hiring.Refusal{Code: hiring.CodeNotFound, Message: "The person to replace is not in this organisation: choose them from the directory."}
		}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.requisition (id, title, department, reason, replaces_person, salary_min, salary_max, target_start, pass_needed, requested_by, requested_at)
VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8, $9, $10, $11)`, id, strings.TrimSpace(in.Title), strings.TrimSpace(in.Department), in.Reason, replaces, in.SalaryMin, in.SalaryMax, day(start), in.PassNeeded, by, at); err != nil {
		return uuid.Nil, hiringError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.requisition.requested", audit.Object{Type: "requisition", ID: id}, map[string]audit.Change{"reason": {nil, in.Reason}, "pass_needed": {nil, in.PassNeeded}}, "")
}

func loadPost(ctx context.Context, tx pgx.Tx, req uuid.UUID, passNeeded bool) (*api.JobPost, error) {
	var p api.JobPost
	var published, closed, advert, advertClosed *time.Time
	var note, ref, ex, exNote *string
	err := tx.QueryRow(ctx, `SELECT id, title, body, state, published_on, closed_on, requirement_note, advert_posted_on, advert_reference, exemption, exemption_note, advert_closed_on, advert_renewals FROM nemura.job_post WHERE requisition_id = $1`, req).
		Scan(&p.ID, &p.Title, &p.Body, &p.State, &published, &closed, &note, &advert, &ref, &ex, &exNote, &advertClosed, &p.AdvertRenewals)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.PublishedOn, p.ClosedOn, p.AdvertPostedOn, p.AdvertClosedOn = dayOrEmpty(published), dayOrEmpty(closed), dayOrEmpty(advert), dayOrEmpty(advertClosed)
	if advertClosed != nil {
		p.AdvertUsableUntil = day(advertClosed.AddDate(0, hiring.AdvertStaleMonths, 0))
	}
	if note != nil {
		p.RequirementNote = *note
	}
	if ref != nil {
		p.AdvertReference = *ref
	}
	exemption := ""
	if ex != nil {
		exemption, p.Exemption = *ex, *ex
	}
	if exNote != nil {
		p.ExemptionNote = *exNote
	}
	if from, ok := hiring.OfferFrom(passNeeded, advert, exemption); !ok {
		p.OfferBlocked = true
	} else if !from.IsZero() {
		p.OfferFrom = day(from)
	}
	p.Findings = []api.WordingFinding{}
	for _, f := range hiring.FairWording(p.Title + "\n" + p.Body) {
		p.Findings = append(p.Findings, api.WordingFinding{Category: f.Category, Phrase: f.Phrase})
	}
	return &p, nil
}

const requisitionColumns = `r.id, r.title, r.department, r.reason, r.replaces_person, coalesce(p.legal_name, ''), r.salary_min::numeric(20,2)::text, r.salary_max::numeric(20,2)::text, r.target_start, r.pass_needed, r.plan_status, r.state,
  coalesce(rb.display_name, ''), r.requested_at, coalesce(db.display_name, ''), r.decided_at, coalesce(r.decision_reason, '')`
const requisitionFrom = ` FROM nemura.requisition r LEFT JOIN nemura.person p ON p.organisation_id = r.organisation_id AND p.id = r.replaces_person
LEFT JOIN nemura.member rb ON rb.organisation_id = r.organisation_id AND rb.id = r.requested_by LEFT JOIN nemura.member db ON db.organisation_id = r.organisation_id AND db.id = r.decided_by `

func scanRequisition(ctx context.Context, tx pgx.Tx, row pgx.Row) (api.Requisition, error) {
	var r api.Requisition
	var replaces *uuid.UUID
	var start time.Time
	var requested time.Time
	var decided *time.Time
	if err := row.Scan(&r.ID, &r.Title, &r.Department, &r.Reason, &replaces, &r.Replaces, &r.SalaryMin, &r.SalaryMax, &start, &r.PassNeeded, &r.PlanStatus, &r.State, &r.RequestedBy, &requested, &r.DecidedBy, &decided, &r.DecisionReason); err != nil {
		return r, err
	}
	if replaces != nil {
		r.ReplacesID = replaces.String()
	}
	r.TargetStart, r.RequestedAt = day(start), requested.UTC().Format(time.RFC3339)
	if decided != nil {
		r.DecidedAt = decided.UTC().Format(time.RFC3339)
	}
	post, err := loadPost(ctx, tx, uuid.MustParse(r.ID), r.PassNeeded)
	r.JobPost = post
	return r, err
}

// Requisitions lists the requisitions, the newest first, each with its job post.
func Requisitions(ctx context.Context, tx pgx.Tx) ([]api.Requisition, error) {
	rows, err := tx.Query(ctx, `SELECT r.id FROM nemura.requisition r ORDER BY r.requested_at DESC, r.id`)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	var out []api.Requisition
	for _, id := range ids {
		r, err := OneRequisition(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// OneRequisition reads a requisition with its post.
func OneRequisition(ctx context.Context, tx pgx.Tx, id uuid.UUID) (api.Requisition, error) {
	r, err := scanRequisition(ctx, tx, tx.QueryRow(ctx, `SELECT `+requisitionColumns+requisitionFrom+`WHERE r.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, noRequisition
	}
	return r, err
}

// DecideRequisition approves or rejects a requested requisition, once.
func DecideRequisition(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, id uuid.UUID, approve bool, reason string) error {
	var state string
	if err := tx.QueryRow(ctx, `SELECT state FROM nemura.requisition WHERE id = $1 FOR UPDATE`, id).Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return noRequisition
		}
		return err
	}
	if state != "requested" {
		return &hiring.Refusal{Code: hiring.CodeConflict, Message: "That requisition is decided already: it is decided once."}
	}
	if len([]rune(reason)) > 500 {
		return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "The reason is at most 500 characters."}
	}
	next := "rejected"
	if approve {
		next = "approved"
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.requisition SET state = $2, decided_by = $3, decided_at = $4, decision_reason = $5 WHERE id = $1`, id, next, by, at, orNil(strings.TrimSpace(reason))); err != nil {
		return hiringError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.requisition.decided", audit.Object{Type: "requisition", ID: id}, map[string]audit.Change{"state": {"requested", next}}, "")
}

// openRequisition locks a requisition and reports its state and whether it needs a pass.
func openRequisition(ctx context.Context, tx pgx.Tx, id uuid.UUID) (state string, pass bool, err error) {
	err = tx.QueryRow(ctx, `SELECT state, pass_needed FROM nemura.requisition WHERE id = $1 FOR UPDATE`, id).Scan(&state, &pass)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, noRequisition
	}
	return state, pass, err
}

// WriteJobPost writes or changes a draft job post of a requisition that is requested or approved.
func WriteJobPost(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, req uuid.UUID, title, body string) error {
	if err := hiring.CheckPost(title, body); err != nil {
		return err
	}
	state, _, err := openRequisition(ctx, tx, req)
	if err != nil {
		return err
	}
	if state != "requested" && state != "approved" {
		return &hiring.Refusal{Code: hiring.CodeConflict, Message: "That requisition is rejected, filled or cancelled: its post is not written."}
	}
	var id uuid.UUID
	var cur string
	err = tx.QueryRow(ctx, `SELECT id, state FROM nemura.job_post WHERE requisition_id = $1 FOR UPDATE`, req).Scan(&id, &cur)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		id = uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.job_post (id, requisition_id, title, body, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6)`, id, req, strings.TrimSpace(title), strings.TrimSpace(body), by, at); err != nil {
			return hiringError(err)
		}
	case err != nil:
		return err
	case cur != "draft":
		return &hiring.Refusal{Code: hiring.CodeConflict, Message: "That post is published: a published post never changes."}
	default:
		if _, err := tx.Exec(ctx, `UPDATE nemura.job_post SET title = $2, body = $3 WHERE id = $1`, id, strings.TrimSpace(title), strings.TrimSpace(body)); err != nil {
			return hiringError(err)
		}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.job_post.written", audit.Object{Type: "job_post", ID: id}, map[string]audit.Change{"requisition": {nil, req.String()}}, "")
}

// PublishJobPost publishes the draft of an approved requisition; a criterion the wording check flagged is kept only with the genuine occupational requirement recorded (NM-REC-03).
func PublishJobPost(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, req uuid.UUID, requirement string) error {
	state, _, err := openRequisition(ctx, tx, req)
	if err != nil {
		return err
	}
	if state != "approved" {
		return &hiring.Refusal{Code: hiring.CodeConflict, Message: "Only an approved requisition's post is published: ask the Owner to approve the requisition first."}
	}
	var id uuid.UUID
	var title, body, cur string
	if err := tx.QueryRow(ctx, `SELECT id, title, body, state FROM nemura.job_post WHERE requisition_id = $1 FOR UPDATE`, req).Scan(&id, &title, &body, &cur); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &hiring.Refusal{Code: hiring.CodeConflict, Message: "Write the post first."}
		}
		return err
	}
	if cur != "draft" {
		return &hiring.Refusal{Code: hiring.CodeConflict, Message: "That post is published already."}
	}
	found := hiring.FairWording(title + "\n" + body)
	requirement = strings.TrimSpace(requirement)
	if len(found) > 0 && len([]rune(requirement)) < 10 {
		var cats []string
		for _, f := range found {
			cats = append(cats, f.Category)
		}
		return &hiring.Refusal{Code: hiring.CodeWording, Message: "The post names criteria the guidelines ask an employer not to use (" + strings.Join(cats, ", ") + "): describe the skill or duty instead, or record the genuine occupational requirement for each in at least 10 characters."}
	}
	if len(found) == 0 {
		requirement = ""
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.job_post SET state = 'published', published_on = $2, requirement_note = $3 WHERE id = $1`, id, day(at), orNil(requirement)); err != nil {
		return hiringError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.job_post.published", audit.Object{Type: "job_post", ID: id}, map[string]audit.Change{"state": {"draft", "published"}, "flagged": {nil, len(found)}}, "")
}

// RecordJobAdvert records, once, the day a person posted the advert on MyCareersFuture.
func RecordJobAdvert(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, req uuid.UUID, in api.AdvertInput) error {
	postedOn, reference := in.PostedOn, in.Reference
	d, err := time.Parse(time.DateOnly, postedOn)
	if err != nil || d.After(dateOf(at)) {
		return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "The advert's date is a day up to today: write it as 2026-10-05."}
	}
	var closed *time.Time
	if in.ClosedOn != "" {
		c, err := time.Parse(time.DateOnly, in.ClosedOn)
		if err != nil || c.Before(d) {
			return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "The day the advert came down is on or after the day it went up: write it as 2026-10-19."}
		}
		if !hiring.AdvertUsable(&c, dateOf(at)) {
			return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "That advert came down more than 3 months ago, so it cannot be used: post a new advert and record its day."}
		}
		closed = &c
	}
	if len([]rune(reference)) > 100 {
		return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "The reference is at most 100 characters."}
	}
	if _, _, err := openRequisition(ctx, tx, req); err != nil {
		return err
	}
	id, err := postID(ctx, tx, req)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE nemura.job_post SET advert_posted_on = $2, advert_reference = $3, advert_closed_on = $4 WHERE id = $1 AND advert_posted_on IS NULL AND state <> 'closed'`, id, day(d), orNil(strings.TrimSpace(reference)), closed)
	if err != nil {
		return hiringError(err)
	}
	if tag.RowsAffected() == 0 {
		return &hiring.Refusal{Code: hiring.CodeConflict, Message: "The advert's date is recorded already, or the post is closed: it is recorded once."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.job_post.advert_recorded", audit.Object{Type: "job_post", ID: id}, map[string]audit.Change{"posted_on": {nil, day(d)}, "closed_on": {nil, in.ClosedOn}}, "")
}

// RenewJobAdvert records a new advert after a material change (NM-REC-02): the change of occupation, salary, number of vacancies or UEN needs a new advert, and its 14 days start again. The advert
// it replaces is archived as evidence with what changed. Only a post that has an advert recorded can be renewed, only for a day after the one it replaces, and it is audited.
func RenewJobAdvert(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, req uuid.UUID, in api.AdvertInput) error {
	if err := hiring.CheckAdvertChange(in.Change, in.Note); err != nil {
		return err
	}
	d, err := time.Parse(time.DateOnly, in.PostedOn)
	if err != nil || d.After(dateOf(at)) {
		return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "The new advert's date is a day up to today: write it as 2026-10-05."}
	}
	if _, _, err := openRequisition(ctx, tx, req); err != nil {
		return err
	}
	id, err := postID(ctx, tx, req)
	if err != nil {
		return err
	}
	var oldOn *time.Time
	var oldRef *string
	var oldClosed *time.Time
	var state string
	if err := tx.QueryRow(ctx, `SELECT state, advert_posted_on, advert_reference, advert_closed_on FROM nemura.job_post WHERE id = $1 FOR UPDATE`, id).Scan(&state, &oldOn, &oldRef, &oldClosed); err != nil {
		return err
	}
	if state == "closed" || oldOn == nil {
		return &hiring.Refusal{Code: hiring.CodeConflict, Message: "Only a post with an advert recorded, and not closed, has an advert to renew: record the advert's day first."}
	}
	if !d.After(*oldOn) {
		return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "The new advert went up after the one it replaces: check its day."}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.job_advert (job_post_id, posted_on, reference, closed_on, change_kind, change_note, replaced_by, replaced_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, day(*oldOn), oldRef, oldClosed, in.Change, orNil(strings.TrimSpace(in.Note)), by, at); err != nil {
		return hiringError(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.job_post SET advert_posted_on = $2, advert_reference = $3, advert_closed_on = NULL, advert_renewals = advert_renewals + 1 WHERE id = $1`, id, day(d), orNil(strings.TrimSpace(in.Reference))); err != nil {
		return hiringError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.job_post.advert_renewed", audit.Object{Type: "job_post", ID: id}, map[string]audit.Change{"posted_on": {day(*oldOn), day(d)}, "change": {nil, in.Change}}, "")
}

func postID(ctx context.Context, tx pgx.Tx, req uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM nemura.job_post WHERE requisition_id = $1`, req).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, &hiring.Refusal{Code: hiring.CodeConflict, Message: "Write the post first."}
		}
		return uuid.Nil, err
	}
	return id, nil
}

// RecordJobExemption records, once, an exemption from the advert with its evidence, for a role that needs a pass.
func RecordJobExemption(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, req uuid.UUID, kind, note string) error {
	if err := hiring.CheckExemption(kind, note); err != nil {
		return err
	}
	_, pass, err := openRequisition(ctx, tx, req)
	if err != nil {
		return err
	}
	if !pass {
		return &hiring.Refusal{Code: hiring.CodeInvalid, Message: "This role does not need a pass, so no advert applies and there is nothing to exempt."}
	}
	id, err := postID(ctx, tx, req)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE nemura.job_post SET exemption = $2, exemption_note = $3 WHERE id = $1 AND exemption IS NULL AND state <> 'closed'`, id, kind, strings.TrimSpace(note))
	if err != nil {
		return hiringError(err)
	}
	if tag.RowsAffected() == 0 {
		return &hiring.Refusal{Code: hiring.CodeConflict, Message: "An exemption is recorded already, or the post is closed: it is recorded once."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.job_post.exemption_recorded", audit.Object{Type: "job_post", ID: id}, map[string]audit.Change{"exemption": {nil, kind}}, "")
}

// CloseJobPost closes a published post, once.
func CloseJobPost(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, req uuid.UUID) error {
	if _, _, err := openRequisition(ctx, tx, req); err != nil {
		return err
	}
	id, err := postID(ctx, tx, req)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE nemura.job_post SET state = 'closed', closed_on = $2 WHERE id = $1 AND state = 'published'`, id, day(at))
	if err != nil {
		return hiringError(err)
	}
	if tag.RowsAffected() == 0 {
		return &hiring.Refusal{Code: hiring.CodeConflict, Message: "Only a published post is closed, and once."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.job_post.closed", audit.Object{Type: "job_post", ID: id}, map[string]audit.Change{"state": {"published", "closed"}}, "")
}

// HiringService is requisitions and job posts behind the API.
type HiringService struct {
	Store *Store
	Clock ports.Clock
}

var _ api.Hiring = HiringService{}

func (h HiringService) run(ctx context.Context, s api.Session, req uuid.UUID, fn func(ctx context.Context, tx pgx.Tx, at time.Time) error) (api.Requisition, error) {
	var out api.Requisition
	at := h.Clock.Now()
	err := h.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		if err := fn(ctx, tx, at); err != nil {
			return err
		}
		var err error
		out, err = OneRequisition(ctx, tx, req)
		return err
	})
	return out, err
}

func (h HiringService) Requisitions(ctx context.Context, s api.Session) (out []api.Requisition, err error) {
	err = h.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) (err error) { out, err = Requisitions(ctx, tx); return })
	return
}

func (h HiringService) Request(ctx context.Context, s api.Session, in api.RequisitionInput) (api.Requisition, error) {
	var out api.Requisition
	at := h.Clock.Now()
	err := h.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		id, err := RequestRequisition(ctx, tx, s.Organisation, at, s.Member, in)
		if err != nil {
			return err
		}
		out, err = OneRequisition(ctx, tx, id)
		return err
	})
	return out, err
}

func (h HiringService) Decide(ctx context.Context, s api.Session, id uuid.UUID, approve bool, reason string) (api.Requisition, error) {
	return h.run(ctx, s, id, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return DecideRequisition(ctx, tx, s.Organisation, at, s.Member, id, approve, reason)
	})
}

func (h HiringService) WritePost(ctx context.Context, s api.Session, id uuid.UUID, title, body string) (api.Requisition, error) {
	return h.run(ctx, s, id, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return WriteJobPost(ctx, tx, s.Organisation, at, s.Member, id, title, body)
	})
}

func (h HiringService) Publish(ctx context.Context, s api.Session, id uuid.UUID, note string) (api.Requisition, error) {
	return h.run(ctx, s, id, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return PublishJobPost(ctx, tx, s.Organisation, at, s.Member, id, note)
	})
}

func (h HiringService) RecordAdvert(ctx context.Context, s api.Session, id uuid.UUID, in api.AdvertInput) (api.Requisition, error) {
	return h.run(ctx, s, id, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return RecordJobAdvert(ctx, tx, s.Organisation, at, s.Member, id, in)
	})
}

func (h HiringService) RenewAdvert(ctx context.Context, s api.Session, id uuid.UUID, in api.AdvertInput) (api.Requisition, error) {
	return h.run(ctx, s, id, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return RenewJobAdvert(ctx, tx, s.Organisation, at, s.Member, id, in)
	})
}

func (h HiringService) RecordExemption(ctx context.Context, s api.Session, id uuid.UUID, kind, note string) (api.Requisition, error) {
	return h.run(ctx, s, id, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return RecordJobExemption(ctx, tx, s.Organisation, at, s.Member, id, kind, note)
	})
}

func (h HiringService) ClosePost(ctx context.Context, s api.Session, id uuid.UUID) (api.Requisition, error) {
	return h.run(ctx, s, id, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return CloseJobPost(ctx, tx, s.Organisation, at, s.Member, id)
	})
}
