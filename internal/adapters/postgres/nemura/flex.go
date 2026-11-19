package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/leave"
)

// FlexAsk is a formal flexible work arrangement request (NM-LV-10).
type FlexAsk struct {
	Person      uuid.UUID
	Arrangement string
	From        time.Time
	To          *time.Time
	Reason      string
}

func flexError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM151":
			return &leave.Refusal{Code: leave.CodeConflict, Message: "That request is closed or evidence: it keeps its words, its receipt date and its deadline, and is answered once."}
		case "NM152":
			return &leave.Refusal{Code: leave.CodeRole, Message: "Nobody answers their own flexible work request: another member of the people team does."}
		}
	}
	return leaveError(err)
}

// RequestFlex records a formal request with its receipt date (the day it is made) and the reply deadline the pack's months give. The receipt date never restarts.
func RequestFlex(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, v pack.Version, a FlexAsk) (uuid.UUID, time.Time, error) {
	known := false
	for _, k := range leave.Arrangements {
		known = known || k == a.Arrangement
	}
	if !known {
		return uuid.Nil, time.Time{}, &leave.Refusal{Code: leave.CodeInvalid, Message: "The arrangement is flexi-time, flexi-place or flexi-load: choose one."}
	}
	if n := len(a.Reason); n == 0 || n > 2000 {
		return uuid.Nil, time.Time{}, &leave.Refusal{Code: leave.CodeInvalid, Message: "The reason is required and at most 2,000 characters: say why the arrangement is wanted."}
	}
	if a.To != nil && a.To.Before(a.From) {
		return uuid.Nil, time.Time{}, &leave.Refusal{Code: leave.CodeInvalid, Message: "The end is before the start: choose an end on or after " + day(a.From) + ", or leave it open."}
	}
	received := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	months, ok := pack.FlexibleWorkMonths(v, "reply_months", received)
	if !ok {
		return uuid.Nil, time.Time{}, &leave.Refusal{Code: leave.CodeInvalid, Message: "The country pack has no reply window for flexible work requests on that day."}
	}
	due := leave.ReplyDue(received, months)
	id := uuid.Must(uuid.NewV7())
	var to any
	if a.To != nil {
		to = day(*a.To)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.flex_request (id, person_id, arrangement, start_date, end_date, reason, received_on, reply_due, pack_version, requested_by, requested_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, id, a.Person, a.Arrangement, day(a.From), to, a.Reason, day(received), day(due), v.ID, by, at); err != nil {
		return uuid.Nil, time.Time{}, flexError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.flex_request.received", audit.Object{Type: "flex_request", ID: id}, map[string]audit.Change{"person": {nil, a.Person.String()},
		"arrangement": {nil, a.Arrangement}, "received_on": {nil, day(received)}, "reply_due": {nil, day(due)}}, ""); err != nil {
		return uuid.Nil, time.Time{}, err
	}
	return id, due, notice(ctx, tx, "flex_requested", a.Person, id, "flex-requested")
}

// ReplyFlex records the employer's written reply, once, by someone other than the person (NM-LV-10). A refusal or an alternative gives its reason.
func ReplyFlex(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, request uuid.UUID, outcome, written string) error {
	if err := leave.CheckReply(outcome, written); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "UPDATE nemura.flex_request SET state = 'replied', outcome = $2, written_reply = $3, replied_by = $4, replied_at = $5 WHERE id = $1 AND state = 'received'", request, outcome, written, by, at)
	if err != nil {
		return flexError(err)
	}
	if tag.RowsAffected() != 1 {
		return &leave.Refusal{Code: leave.CodeConflict, Message: "That request has no reply to give: it is answered or withdrawn already, or there is no such request."}
	}
	var person uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT person_id FROM nemura.flex_request WHERE id = $1", request).Scan(&person); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.flex_request.replied", audit.Object{Type: "flex_request", ID: request}, map[string]audit.Change{"outcome": {nil, outcome}}, ""); err != nil {
		return err
	}
	return notice(ctx, tx, "flex_replied", person, request, "flex-replied")
}

// WithdrawFlex withdraws a request that still waits.
func WithdrawFlex(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, request uuid.UUID) error {
	tag, err := tx.Exec(ctx, "UPDATE nemura.flex_request SET state = 'withdrawn', withdrawn_at = $2 WHERE id = $1 AND state = 'received'", request, at)
	if err != nil {
		return flexError(err)
	}
	if tag.RowsAffected() != 1 {
		return &leave.Refusal{Code: leave.CodeConflict, Message: "That request is answered or withdrawn already: there is nothing to withdraw."}
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.flex_request.withdrawn", audit.Object{Type: "flex_request", ID: request}, nil, "")
}

// FlexRow is a request as it is listed.
type FlexRow struct {
	ID, Person                    uuid.UUID
	PersonName, Arrangement       string
	From                          time.Time
	To                            *time.Time
	Reason, State, Outcome, Reply string
	Received, Due                 time.Time
	RepliedAt                     *time.Time
}

// FlexScope is whose requests a caller reads: their own, or everyone's for the people team.
type FlexScope struct {
	Member uuid.UUID
	All    bool
}

// FlexRequests lists requests in a scope, those waiting first by deadline.
func FlexRequests(ctx context.Context, tx pgx.Tx, s FlexScope) ([]FlexRow, error) {
	rows, err := tx.Query(ctx, `SELECT f.id, f.person_id, coalesce(nullif(p.preferred_name, ''), p.legal_name), f.arrangement, f.start_date, f.end_date, f.reason, f.state, coalesce(f.outcome, ''),
 coalesce(f.written_reply, ''), f.received_on, f.reply_due, f.replied_at
FROM nemura.flex_request f JOIN nemura.person p ON p.organisation_id = f.organisation_id AND p.id = f.person_id
WHERE ($2 OR p.member_id = $1) ORDER BY (f.state <> 'received'), f.reply_due, f.id`, s.Member, s.All)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FlexRow
	for rows.Next() {
		var r FlexRow
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.Arrangement, &r.From, &r.To, &r.Reason, &r.State, &r.Outcome, &r.Reply, &r.Received, &r.Due, &r.RepliedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// FlexFacts are the requests still waiting for a reply, for N.1 #5.
func FlexFacts(ctx context.Context, tx pgx.Tx) ([]checks.FlexRequest, error) {
	rows, err := tx.Query(ctx, "SELECT person_id::text, reply_due FROM nemura.flex_request WHERE state = 'received'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []checks.FlexRequest
	for rows.Next() {
		var f checks.FlexRequest
		if err := rows.Scan(&f.Person, &f.Due); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
