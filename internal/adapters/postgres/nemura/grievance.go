package nemura

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/grievance"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Grievances (PRD-002 NM-SEC-06). Every function here runs inside Store.InOrgAs for the acting member, so the row-level policies of migration 000063 decide what is visible: the complainant and the handler
// read a grievance, only the handler its inquiry record, and a member who is one of the people complained about reads nothing of it, whatever role they hold. The audit events name identities and states
// and never the words, the people complained about or the outcome.

var notYours = &grievance.Refusal{Code: grievance.CodeNotFound, Message: "There is no such grievance in your view."}

func grievanceError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM209", "NM210", "42501":
			return notYours
		case "23514":
			return &grievance.Refusal{Code: grievance.CodeInvalid, Message: "That is not a valid grievance: say what happened in 20 to 4000 characters and name at most five people."}
		}
	}
	return err
}

// candidates are the members who hold a role that may handle a grievance today and are active.
func handlerCandidates(ctx context.Context, tx pgx.Tx, at time.Time) ([]grievance.Candidate, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (a.member_id) a.member_id, a.role FROM nemura.role_assignment a JOIN nemura.member m ON m.organisation_id = a.organisation_id AND m.id = a.member_id
WHERE a.role IN ('hr_admin', 'owner') AND m.deactivated_at IS NULL AND a.effective_from <= $1::date AND (a.effective_to IS NULL OR a.effective_to > $1::date) ORDER BY a.member_id, (a.role = 'hr_admin') DESC`, day(at))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []grievance.Candidate
	for rows.Next() {
		var c grievance.Candidate
		if err := rows.Scan(&c.Member, &c.Role); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// aboutMembers are the logins of the people a grievance is about, by the live person records.
func aboutMembers(ctx context.Context, tx pgx.Tx, people []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := tx.Query(ctx, `SELECT member_id FROM nemura.person WHERE id = ANY($1) AND member_id IS NOT NULL`, people)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var m uuid.UUID
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out[m] = true
	}
	return out, rows.Err()
}

func memberName(ctx context.Context, tx pgx.Tx, m uuid.UUID) string {
	var n string
	if tx.QueryRow(ctx, `SELECT display_name FROM nemura.member WHERE id = $1`, m).Scan(&n) != nil {
		return ""
	}
	return n
}

func entry(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, g uuid.UUID, kind, body string) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.grievance_entry (id, grievance_id, kind, body, by_member, at) VALUES ($1, $2, $3, $4, $5, $6)`, id, g, kind, body, by, at); err != nil {
		return uuid.Nil, grievanceError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.grievance.entry_recorded", audit.Object{Type: "grievance_entry", ID: id}, map[string]audit.Change{"grievance": {nil, g.String()}, "kind": {nil, kind}}, "")
}

// FileGrievance files a grievance for the complainant and routes it (NM-SEC-06). The people complained about are named by person; the complainant may not be one of them. Run in InOrgAs for the complainant.
func FileGrievance(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, complainant uuid.UUID, summary string, about []uuid.UUID) (api.GrievanceFiled, error) {
	if r := grievance.Words("summary", summary); r != nil {
		return api.GrievanceFiled{}, r
	}
	about = slices.Compact(slices.SortedFunc(slices.Values(about), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) }))
	if len(about) > grievance.MaxPeople {
		return api.GrievanceFiled{}, &grievance.Refusal{Code: grievance.CodeInvalid, Message: "Name at most five people: if it is about more, describe it in the summary as a practice."}
	}
	if about == nil {
		about = []uuid.UUID{}
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.person WHERE id = ANY($1)`, about).Scan(&n); err != nil {
		return api.GrievanceFiled{}, err
	}
	if n != len(about) {
		return api.GrievanceFiled{}, &grievance.Refusal{Code: grievance.CodeNotFound, Message: "One of the people you named is not in this organisation: choose them from the directory."}
	}
	subjects, err := aboutMembers(ctx, tx, about)
	if err != nil {
		return api.GrievanceFiled{}, err
	}
	if subjects[complainant] {
		return api.GrievanceFiled{}, &grievance.Refusal{Code: grievance.CodeInvalid, Message: "A grievance cannot be about the person who files it: leave yourself out."}
	}
	cands, err := handlerCandidates(ctx, tx, at)
	if err != nil {
		return api.GrievanceFiled{}, err
	}
	id := uuid.Must(uuid.NewV7())
	h, routed := grievance.Route(id, cands, complainant, subjects)
	state, handler, routedAt := "unrouted", (*uuid.UUID)(nil), (*time.Time)(nil)
	if routed {
		state, handler, routedAt = "open", &h, &at
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.grievance (id, complainant_member, subject_people, summary, state, handler_member, filed_at, routed_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, complainant, about, strings.TrimSpace(summary), state, handler, at, routedAt); err != nil {
		return api.GrievanceFiled{}, grievanceError(err)
	}
	if err := Audit(ctx, tx, org, at, user(complainant), "nemura.grievance.filed", audit.Object{Type: "grievance", ID: id}, map[string]audit.Change{"state": {nil, state}, "people": {nil, len(about)}}, ""); err != nil {
		return api.GrievanceFiled{}, err
	}
	out := api.GrievanceFiled{ID: id.String(), State: state}
	body := "Filed: no HR administrator or Owner who is not involved was available, so it waits to be taken."
	if routed {
		out.Handler = memberName(ctx, tx, h)
		body = "Filed and routed to an HR administrator or the Owner who is not involved."
	}
	if _, err := entry(ctx, tx, org, at, complainant, id, "routing", body); err != nil {
		return api.GrievanceFiled{}, err
	}
	if routed {
		return out, grievanceNotice(ctx, tx, "grievance_assigned", h, id)
	}
	return out, nil
}

// grievanceNotice tells a person a grievance is theirs to handle, or that an outcome is written: by identity, with no words and no names.
func grievanceNotice(ctx context.Context, tx pgx.Tx, kind string, member, g uuid.UUID) error {
	var person uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM nemura.person WHERE member_id = $1`, member).Scan(&person); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // a login with no person record gets no notice; the grievance is in their list
		}
		return err
	}
	return notice(ctx, tx, kind, person, g, strings.ReplaceAll(kind, "_", "-")+"-"+member.String()) // one notice for each person it is assigned to
}

// MyGrievances lists the grievances the member filed, newest first: their state and, once decided, the written outcome.
func MyGrievances(ctx context.Context, tx pgx.Tx, member uuid.UUID) ([]api.GrievanceMine, error) {
	rows, err := tx.Query(ctx, `SELECT g.id, g.filed_at, g.state, coalesce(m.display_name, ''), coalesce(g.finding, ''), coalesce(g.outcome, ''), g.decided_at
FROM nemura.grievance g LEFT JOIN nemura.member m ON m.organisation_id = g.organisation_id AND m.id = g.handler_member WHERE g.complainant_member = $1 ORDER BY g.filed_at DESC, g.id DESC`, member)
	if err != nil {
		return nil, grievanceError(err)
	}
	defer rows.Close()
	out := []api.GrievanceMine{}
	for rows.Next() {
		var id uuid.UUID
		var filed time.Time
		var decided *time.Time
		g := api.GrievanceMine{}
		if err := rows.Scan(&id, &filed, &g.State, &g.Handler, &g.Finding, &g.Outcome, &decided); err != nil {
			return nil, err
		}
		g.ID, g.FiledAt = id.String(), filed.UTC().Format(time.RFC3339)
		if decided != nil {
			g.DecidedAt = decided.UTC().Format(time.RFC3339)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// HandledGrievances lists what the member handles, and the unrouted ones the policies show an authorised person who is not involved.
func HandledGrievances(ctx context.Context, tx pgx.Tx, member uuid.UUID) (assigned, unrouted []api.GrievanceRow, err error) {
	rows, err := tx.Query(ctx, `SELECT id, filed_at, state, coalesce(handler_member = $1, false) FROM nemura.grievance WHERE handler_member = $1 OR state = 'unrouted' ORDER BY filed_at DESC, id DESC`, member)
	if err != nil {
		return nil, nil, grievanceError(err)
	}
	defer rows.Close()
	assigned, unrouted = []api.GrievanceRow{}, []api.GrievanceRow{}
	for rows.Next() {
		var id uuid.UUID
		var filed time.Time
		var mine bool
		r := api.GrievanceRow{}
		if err := rows.Scan(&id, &filed, &r.State, &mine); err != nil {
			return nil, nil, err
		}
		r.ID, r.FiledAt = id.String(), filed.UTC().Format(time.RFC3339)
		if mine {
			assigned = append(assigned, r)
		} else if r.State == "unrouted" {
			// An unrouted grievance is listed to an authorised person who is not the complainant (the complainant has it in their own list).
			unrouted = append(unrouted, r)
		}
	}
	return assigned, unrouted, rows.Err()
}

// GrievanceDetail reads a grievance and its inquiry record for its handler (or an authorised person reading an unrouted one), and records the read.
func GrievanceDetail(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, reader, id uuid.UUID) (api.GrievanceDetail, error) {
	var d api.GrievanceDetail
	var filed time.Time
	var decided *time.Time
	var complainant uuid.UUID
	var people []uuid.UUID
	var handler *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT g.id::text, g.filed_at, g.state, g.complainant_member, g.summary, g.subject_people, g.handler_member, coalesce(g.finding, ''), coalesce(g.outcome, ''), g.decided_at
FROM nemura.grievance g WHERE g.id = $1`, id).Scan(&d.ID, &filed, &d.State, &complainant, &d.Summary, &people, &handler, &d.Finding, &d.Outcome, &decided)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, notYours
	}
	if err != nil {
		return d, grievanceError(err)
	}
	if complainant == reader { // the complainant reads their own through My grievances, without the inquiry record
		return d, notYours
	}
	d.FiledAt, d.Complainant = filed.UTC().Format(time.RFC3339), memberName(ctx, tx, complainant)
	if decided != nil {
		d.DecidedAt = decided.UTC().Format(time.RFC3339)
	}
	d.About, d.Entries = []api.GrievanceSubject{}, []api.GrievanceEntry{}
	prs, err := tx.Query(ctx, `SELECT id, legal_name FROM nemura.person WHERE id = ANY($1) ORDER BY legal_name`, people)
	if err != nil {
		return d, err
	}
	for prs.Next() {
		var p uuid.UUID
		var s api.GrievanceSubject
		if err := prs.Scan(&p, &s.Name); err != nil {
			prs.Close()
			return d, err
		}
		s.PersonID = p.String()
		d.About = append(d.About, s)
	}
	prs.Close()
	es, err := tx.Query(ctx, `SELECT e.id, e.kind, e.body, coalesce(m.display_name, ''), e.at FROM nemura.grievance_entry e LEFT JOIN nemura.member m ON m.organisation_id = e.organisation_id AND m.id = e.by_member
WHERE e.grievance_id = $1 ORDER BY e.at, e.id`, id)
	if err != nil {
		return d, err
	}
	for es.Next() {
		var eid uuid.UUID
		var e api.GrievanceEntry
		var when time.Time
		if err := es.Scan(&eid, &e.Kind, &e.Body, &e.By, &when); err != nil {
			es.Close()
			return d, err
		}
		e.ID, e.At = eid.String(), when.UTC().Format(time.RFC3339)
		d.Entries = append(d.Entries, e)
	}
	es.Close()
	return d, Audit(ctx, tx, org, at, user(reader), "nemura.grievance.read", audit.Object{Type: "grievance", ID: id}, map[string]audit.Change{"state": {nil, d.State}}, "")
}

// handledBy loads a grievance the member handles, for one that changes it.
func handledBy(ctx context.Context, tx pgx.Tx, member, id uuid.UUID) (state string, complainant uuid.UUID, about []uuid.UUID, err error) {
	err = tx.QueryRow(ctx, `SELECT state, complainant_member, subject_people FROM nemura.grievance WHERE id = $1 AND handler_member = $2 FOR UPDATE`, id, member).Scan(&state, &complainant, &about)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", uuid.Nil, nil, notYours
	}
	return state, complainant, about, grievanceError(err)
}

// RecordGrievanceEntry adds a note, an interview or a finding to the inquiry record of a grievance the member handles.
func RecordGrievanceEntry(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, handler, id uuid.UUID, kind, note string) error {
	if !slices.Contains(grievance.EntryKinds, kind) {
		return &grievance.Refusal{Code: grievance.CodeInvalid, Message: "An entry is a note, an interview or a finding."}
	}
	if n := len(strings.TrimSpace(note)); n < 1 || n > grievance.MaxWords {
		return &grievance.Refusal{Code: grievance.CodeInvalid, Message: "Write what to record, in up to 4000 characters."}
	}
	state, _, _, err := handledBy(ctx, tx, handler, id)
	if err != nil {
		return err
	}
	if state == "decided" {
		return &grievance.Refusal{Code: grievance.CodeState, Message: "That grievance is decided: its record is closed."}
	}
	_, err = entry(ctx, tx, org, at, handler, id, kind, strings.TrimSpace(note))
	return err
}

// DecideGrievance writes the outcome, once, and tells the complainant.
func DecideGrievance(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, handler, id uuid.UUID, finding, outcome string) error {
	if !slices.Contains(grievance.Findings, finding) {
		return &grievance.Refusal{Code: grievance.CodeInvalid, Message: "Choose what the inquiry concludes: substantiated, partly substantiated, not substantiated or no finding."}
	}
	if r := grievance.Words("outcome", outcome); r != nil {
		return r
	}
	state, complainant, _, err := handledBy(ctx, tx, handler, id)
	if err != nil {
		return err
	}
	if state != "open" {
		return &grievance.Refusal{Code: grievance.CodeState, Message: "That grievance is decided already: an outcome is written once."}
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.grievance SET state = 'decided', finding = $2, outcome = $3, decided_by = $4, decided_at = $5 WHERE id = $1`, id, finding, strings.TrimSpace(outcome), handler, at); err != nil {
		return grievanceError(err)
	}
	if err := Audit(ctx, tx, org, at, user(handler), "nemura.grievance.decided", audit.Object{Type: "grievance", ID: id}, map[string]audit.Change{"state": {"open", "decided"}, "finding": {nil, finding}}, ""); err != nil {
		return err
	}
	return grievanceNotice(ctx, tx, "grievance_decided", complainant, id)
}

// HandOverGrievance passes a grievance to the next eligible person, recording why. The handler who passes it on is no longer one.
func HandOverGrievance(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, handler, id uuid.UUID, reason string) error {
	if n := len(strings.TrimSpace(reason)); n < 1 || n > 500 {
		return &grievance.Refusal{Code: grievance.CodeInvalid, Message: "Say why you are passing it on, in up to 500 characters: it goes in the inquiry record."}
	}
	state, complainant, about, err := handledBy(ctx, tx, handler, id)
	if err != nil {
		return err
	}
	if state != "open" {
		return &grievance.Refusal{Code: grievance.CodeState, Message: "That grievance is decided: it is not passed on."}
	}
	subjects, err := aboutMembers(ctx, tx, about)
	if err != nil {
		return err
	}
	cands, err := handlerCandidates(ctx, tx, at)
	if err != nil {
		return err
	}
	next, ok := grievance.Route(id, cands, complainant, subjects, handler)
	if !ok {
		return &grievance.Refusal{Code: grievance.CodeNoHandler, Message: "Nobody else can handle it: every other HR administrator and the Owner is the complainant or is involved. It stays with you."}
	}
	if _, err := entry(ctx, tx, org, at, handler, id, "handover", "Passed on: "+strings.TrimSpace(reason)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.grievance SET handler_member = $2, routed_at = $3 WHERE id = $1`, id, next, at); err != nil {
		return grievanceError(err)
	}
	if err := Audit(ctx, tx, org, at, user(handler), "nemura.grievance.handed_over", audit.Object{Type: "grievance", ID: id}, map[string]audit.Change{"handler": {handler.String(), next.String()}}, ""); err != nil {
		return err
	}
	return grievanceNotice(ctx, tx, "grievance_assigned", next, id)
}

// TakeGrievance makes the member the handler of an unrouted grievance. The policies show it only to an authorised person who is not the complainant and not involved, and the guard refuses anyone else.
func TakeGrievance(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, member, id uuid.UUID) error {
	var state string
	var complainant uuid.UUID
	err := tx.QueryRow(ctx, `SELECT state, complainant_member FROM nemura.grievance WHERE id = $1 FOR UPDATE`, id).Scan(&state, &complainant)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && complainant == member) {
		return notYours
	}
	if err != nil {
		return grievanceError(err)
	}
	if state != "unrouted" {
		return &grievance.Refusal{Code: grievance.CodeState, Message: "That grievance is being handled already: read it again."}
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.grievance SET state = 'open', handler_member = $2, routed_at = $3 WHERE id = $1`, id, member, at); err != nil {
		return grievanceError(err)
	}
	if _, err := entry(ctx, tx, org, at, member, id, "routing", "Taken by an HR administrator or the Owner who is not involved."); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(member), "nemura.grievance.taken", audit.Object{Type: "grievance", ID: id}, map[string]audit.Change{"state": {"unrouted", "open"}}, "")
}

// GrievanceService is the API's side of the channel: each call runs for the session's member as the acting person on the business day.
type GrievanceService struct {
	Store *Store
	Clock ports.Clock
}

func (g GrievanceService) as(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx, at time.Time) error) error {
	at := g.Clock.Now()
	return g.Store.InOrgAs(ctx, s.Organisation, s.Member, at, func(ctx context.Context, tx pgx.Tx) error { return fn(ctx, tx, at) })
}

func (g GrievanceService) File(ctx context.Context, s api.Session, summary string, about []uuid.UUID) (api.GrievanceFiled, error) {
	var out api.GrievanceFiled
	err := g.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		out, err = FileGrievance(ctx, tx, s.Organisation, at, s.Member, summary, about)
		return err
	})
	return out, err
}

func (g GrievanceService) Mine(ctx context.Context, s api.Session) ([]api.GrievanceMine, error) {
	var out []api.GrievanceMine
	err := g.as(ctx, s, func(ctx context.Context, tx pgx.Tx, _ time.Time) (err error) {
		out, err = MyGrievances(ctx, tx, s.Member)
		return err
	})
	return out, err
}

func (g GrievanceService) Handled(ctx context.Context, s api.Session) ([]api.GrievanceRow, []api.GrievanceRow, error) {
	var a, u []api.GrievanceRow
	err := g.as(ctx, s, func(ctx context.Context, tx pgx.Tx, _ time.Time) (err error) {
		a, u, err = HandledGrievances(ctx, tx, s.Member)
		return err
	})
	return a, u, err
}

func (g GrievanceService) Detail(ctx context.Context, s api.Session, id uuid.UUID) (api.GrievanceDetail, error) {
	var out api.GrievanceDetail
	err := g.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) (err error) {
		out, err = GrievanceDetail(ctx, tx, s.Organisation, at, s.Member, id)
		return err
	})
	return out, err
}

func (g GrievanceService) Record(ctx context.Context, s api.Session, id uuid.UUID, kind, note string) error {
	return g.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return RecordGrievanceEntry(ctx, tx, s.Organisation, at, s.Member, id, kind, note)
	})
}

func (g GrievanceService) Decide(ctx context.Context, s api.Session, id uuid.UUID, finding, outcome string) error {
	return g.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return DecideGrievance(ctx, tx, s.Organisation, at, s.Member, id, finding, outcome)
	})
}

func (g GrievanceService) HandOver(ctx context.Context, s api.Session, id uuid.UUID, reason string) error {
	return g.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return HandOverGrievance(ctx, tx, s.Organisation, at, s.Member, id, reason)
	})
}

func (g GrievanceService) Take(ctx context.Context, s api.Session, id uuid.UUID) error {
	return g.as(ctx, s, func(ctx context.Context, tx pgx.Tx, at time.Time) error {
		return TakeGrievance(ctx, tx, s.Organisation, at, s.Member, id)
	})
}
