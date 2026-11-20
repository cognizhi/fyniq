package nemura

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/assistant"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
)

// RequestFactsOf reads the request a draft is for, as the person who asked for it: a letter request still open, or a flexible-work request not yet replied to. The request's own words are returned as
// data for the writer to quote; nothing here is an instruction.
func RequestFactsOf(ctx context.Context, tx pgx.Tx, task string, id uuid.UUID) (assistant.RequestFacts, error) {
	var f assistant.RequestFacts
	switch task {
	case "people.draft_letter":
		var data []byte
		var state string
		err := tx.QueryRow(ctx, `SELECT p.legal_name, q.purpose, q.data, q.state FROM nemura.letter_request q JOIN nemura.person p ON p.organisation_id = q.organisation_id AND p.id = q.person_id WHERE q.id = $1`, id).Scan(&f.Person, &f.Purpose, &data, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return f, &assistant.Refusal{Code: assistant.CodeNotFound, Message: "There is no such letter request."}
		}
		if err != nil {
			return f, err
		}
		if state != "requested" && state != "checked" {
			return f, &assistant.Refusal{Code: assistant.CodeConflict, Message: "That letter request is closed: a draft is for a request still open."}
		}
		var raw map[string]any
		_ = json.Unmarshal(data, &raw)
		f.Kind, f.Data = "letter", map[string]string{}
		for k, v := range raw {
			if s, ok := v.(string); ok {
				f.Data[k] = s
			}
		}
	case "people.draft_fwa_reply":
		var state string
		var from time.Time
		var to *time.Time
		err := tx.QueryRow(ctx, `SELECT p.legal_name, q.arrangement, q.start_date, q.end_date, q.reason, q.state FROM nemura.flex_request q JOIN nemura.person p ON p.organisation_id = q.organisation_id AND p.id = q.person_id WHERE q.id = $1`, id).Scan(&f.Person, &f.Arrangement, &from, &to, &f.Reason, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return f, &assistant.Refusal{Code: assistant.CodeNotFound, Message: "There is no such flexible-work request."}
		}
		if err != nil {
			return f, err
		}
		if state != "received" {
			return f, &assistant.Refusal{Code: assistant.CodeConflict, Message: "That flexible-work request has been replied to or withdrawn: a draft is for a request still open."}
		}
		f.Kind, f.From = "flex", day(from)
		if to != nil {
			f.To = day(*to)
		}
	case "people.draft_job_post", "people.draft_interview_kit":
		// Only the role and the team are read, as the person who asked: never the salary range, never a candidate (NM-AI-05).
		var state string
		err := tx.QueryRow(ctx, `SELECT title, department, state FROM nemura.requisition WHERE id = $1`, id).Scan(&f.Title, &f.Department, &state)
		if errors.Is(err, pgx.ErrNoRows) {
			return f, &assistant.Refusal{Code: assistant.CodeNotFound, Message: "There is no such requisition."}
		}
		if err != nil {
			return f, err
		}
		if state != "requested" && state != "approved" {
			return f, &assistant.Refusal{Code: assistant.CodeConflict, Message: "That requisition is closed: a draft is for a role still being hired for."}
		}
		f.Kind = "requisition"
	default:
		return f, &assistant.Refusal{Code: assistant.CodeUnknown, Message: task + " is not a task type of the catalogue."}
	}
	return f, nil
}

func kindOf(task string) string {
	switch task {
	case "people.draft_letter":
		return "letter"
	case "people.draft_job_post":
		return "job_post"
	case "people.draft_interview_kit":
		return "interview_kit"
	}
	return "flex_reply"
}

// subjectOf is the ID a proposal's final text names: a request for the two drafts of P1, a requisition for the hiring drafts.
func subjectOf(task string, final map[string]any) (uuid.UUID, error) {
	key := "request_id"
	if task == "people.draft_job_post" || task == "people.draft_interview_kit" {
		key = "requisition_id"
	}
	s, _ := final[key].(string)
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, &assistant.Refusal{Code: assistant.CodeInvalid, Message: key + ": not a record."}
	}
	return id, nil
}

// requestOpen says whether the request a draft is for is still open, reading nothing of the person: the executor holds no grant on the people tables.
func requestOpen(ctx context.Context, tx pgx.Tx, task string, id uuid.UUID) error {
	var state string
	var err error
	if task == "people.draft_job_post" || task == "people.draft_interview_kit" {
		var st *string
		if err = tx.QueryRow(ctx, "SELECT nemura.requisition_state_for_draft($1)", id).Scan(&st); err != nil {
			return err
		}
		if st == nil {
			return &assistant.Refusal{Code: assistant.CodeNotFound, Message: "There is no such requisition."}
		}
		if *st != "requested" && *st != "approved" {
			return &assistant.Refusal{Code: assistant.CodeConflict, Message: "That requisition is closed: a draft is for a role still being hired for."}
		}
		return nil
	}
	if task == "people.draft_letter" {
		err = tx.QueryRow(ctx, "SELECT state FROM nemura.letter_request WHERE id = $1", id).Scan(&state)
		if err == nil && state != "requested" && state != "checked" {
			return &assistant.Refusal{Code: assistant.CodeConflict, Message: "That letter request is closed: a draft is for a request still open."}
		}
	} else {
		err = tx.QueryRow(ctx, "SELECT state FROM nemura.flex_request WHERE id = $1", id).Scan(&state)
		if err == nil && state != "received" {
			return &assistant.Refusal{Code: assistant.CodeConflict, Message: "That flexible-work request has been replied to or withdrawn: a draft is for a request still open."}
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return &assistant.Refusal{Code: assistant.CodeNotFound, Message: "There is no such request."}
	}
	return err
}

// ApplyProposal is the executor's one command: it attaches an approved proposal's final text to its request as a draft, once. It refuses a proposal that was not approved, one whose approved text no longer
// hashes to what was approved, a payload that no longer fits the task's shape, and a request that is closed. It writes the draft and marks the proposal applied, and calls no command of the people
// team: nothing is checked, issued, signed or replied to (NM-AI-04).
func ApplyProposal(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, id uuid.UUID) error {
	p, err := ProposalOf(ctx, tx, id)
	if err != nil {
		return err
	}
	if p.State == "applied" {
		return &assistant.Refusal{Code: assistant.CodeConflict, Message: "That proposal was applied already."}
	}
	if p.State != "approved" {
		return &assistant.Refusal{Code: assistant.CodeConflict, Message: "Only an approved proposal is applied."}
	}
	task, ok := assistant.Lookup(p.Task)
	if !ok || assistant.Refuse(p.Task) != "" {
		return &assistant.Refusal{Code: assistant.CodeNever, Message: "That task type is not one the assistant may apply."}
	}
	body, err := json.Marshal(p.Final)
	if err != nil {
		return err
	}
	var approved []byte
	if err := tx.QueryRow(ctx, "SELECT approved_hash FROM nemura.ai_proposal WHERE id = $1", id).Scan(&approved); err != nil {
		return err
	}
	if sum := sha256.Sum256(body); !bytes.Equal(sum[:], approved) {
		return &assistant.Refusal{Code: assistant.CodeConflict, Message: "The approved text no longer matches the hash that was approved: it is not applied."}
	}
	if err := assistant.Check(task, p.Final); err != nil {
		return err
	}
	requestID, err := subjectOf(p.Task, p.Final)
	if err != nil {
		return err
	}
	if err := requestOpen(ctx, tx, p.Task, requestID); err != nil {
		return err
	}
	draft := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.ai_draft (id, proposal_id, kind, request_id, payload, approved_hash, applied_at) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)`, draft, id, kindOf(p.Task), requestID, string(body), approved, at); err != nil {
		return assistantError(err)
	}
	if _, err := tx.Exec(ctx, "UPDATE nemura.ai_proposal SET state = 'applied', applied_at = $2 WHERE id = $1", id, at); err != nil {
		return assistantError(err)
	}
	if err := Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "people-executor"}, "nemura.ai_proposal.applied", audit.Object{Type: "ai_proposal", ID: id}, map[string]audit.Change{"task": {nil, p.Task}, "draft": {nil, draft.String()}}, ""); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "people-executor"}, "nemura.ai_draft.attached", audit.Object{Type: "ai_draft", ID: draft}, map[string]audit.Change{"proposal": {nil, id.String()}, "request": {nil, requestID.String()}}, "")
}

// ApprovedProposals lists the approved proposals nobody has applied yet, oldest first: the executor's sweep.
func ApprovedProposals(ctx context.Context, tx pgx.Tx) ([]uuid.UUID, error) {
	rows, err := tx.Query(ctx, "SELECT id FROM nemura.ai_proposal WHERE state = 'approved' ORDER BY decided_at, id LIMIT 100")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DraftRow is a draft attached to a request.
type DraftRow struct {
	ID, Proposal uuid.UUID
	Kind         string
	Payload      map[string]any
	AppliedAt    time.Time
}

// DraftsFor lists the drafts attached to a request, newest first.
func DraftsFor(ctx context.Context, tx pgx.Tx, request uuid.UUID) ([]DraftRow, error) {
	rows, err := tx.Query(ctx, "SELECT id, proposal_id, kind, payload, applied_at FROM nemura.ai_draft WHERE request_id = $1 ORDER BY applied_at DESC, id DESC", request)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DraftRow
	for rows.Next() {
		var r DraftRow
		var body []byte
		if err := rows.Scan(&r.ID, &r.Proposal, &r.Kind, &body, &r.AppliedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(body, &r.Payload); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// enqueueApply asks the executor to apply an approved proposal: the approval's transaction queues it, so a proposal approved is never lost before it is applied.
func enqueueApply(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassExecutor, Kind: "assistant.apply", BusinessKey: "apply:" + id.String(), Payload: []byte(`{"proposal":"` + id.String() + `"}`)})
	return err
}
