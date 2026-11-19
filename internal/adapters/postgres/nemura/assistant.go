package nemura

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/assistant"
)

func assistantError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM178":
			return &assistant.Refusal{Code: assistant.CodeConflict, Message: pg.Message + "."}
		}
	}
	return err
}

// AISettingsRow is an organisation's assistant settings. An organisation that has never saved any has the assistant off.
type AISettingsRow struct {
	Enabled   bool
	Route     string
	TaskTypes []string
	OwnPay    bool
	UpdatedAt time.Time
}

// AISettings reads the organisation's settings: off, with no route and no task type, until an Owner changes them.
func AISettings(ctx context.Context, tx pgx.Tx) (AISettingsRow, error) {
	r := AISettingsRow{Route: "none", TaskTypes: []string{}}
	err := tx.QueryRow(ctx, "SELECT enabled, route, task_types, own_pay, updated_at FROM nemura.ai_settings").Scan(&r.Enabled, &r.Route, &r.TaskTypes, &r.OwnPay, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return AISettingsRow{Route: "none", TaskTypes: []string{}}, nil
	}
	if r.TaskTypes == nil {
		r.TaskTypes = []string{}
	}
	return r, err
}

// SetAISettings saves the settings with its audit event. A task type outside the catalogue and an own-pay explanation are refused: the second approver that own pay needs arrives with the Fyniq link
// (NM-AI-06), and the database refuses it too. A route that is not the scripted one needs evidence of processing in Singapore, which this build does not have.
func SetAISettings(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, in AISettingsRow, allowScripted bool) error {
	for _, t := range in.TaskTypes {
		if _, ok := assistant.Lookup(t); !ok {
			return &assistant.Refusal{Code: assistant.CodeUnknown, Message: "task_types: " + t + " is not a task type of the catalogue."}
		}
	}
	if in.OwnPay {
		return &assistant.Refusal{Code: assistant.CodeInvalid, Message: "own_pay: explaining a person's own payslip figures with a model needs the Owner and a second approver, which arrives when Nemura is linked to Fyniq. Until then the assistant explains a payslip line from its rule and the page shows the figures."}
	}
	if in.Route != "none" && in.Route != "scripted" {
		return &assistant.Refusal{Code: assistant.CodeInvalid, Message: "route: choose none or scripted. A real model route needs processing evidenced in Singapore for its provider, model and endpoint."}
	}
	if in.Route == "scripted" && !allowScripted {
		return &assistant.Refusal{Code: assistant.CodeInvalid, Message: "route: the scripted model is for local mode only and is not part of this build."}
	}
	if in.Enabled && in.Route == "none" {
		return &assistant.Refusal{Code: assistant.CodeInvalid, Message: "route: turning the assistant on needs a model route."}
	}
	tt := slices.Clone(in.TaskTypes)
	slices.Sort(tt)
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.ai_settings (enabled, route, task_types, own_pay, updated_by, updated_at) VALUES ($1, $2, $3, false, $4, $5)
ON CONFLICT (organisation_id) DO UPDATE SET enabled = EXCLUDED.enabled, route = EXCLUDED.route, task_types = EXCLUDED.task_types, own_pay = false, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
		in.Enabled, in.Route, tt, by, at); err != nil {
		return assistantError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.ai_settings.changed", audit.Object{Type: "ai_settings", ID: org}, map[string]audit.Change{"enabled": {nil, in.Enabled}, "route": {nil, in.Route}, "task_types": {nil, tt}}, "")
}

// ProposalInput is what the assistant's runtime proposes for the person it acts for.
type ProposalInput struct {
	Asker         uuid.UUID
	Task          string
	Payload       map[string]any
	Rationale     string
	ModelID       string
	PromptVersion string
}

// Propose inserts a proposal as the assistant's role: it refuses when the assistant is off or the task is not enabled, a task the never-list names (again, as each proposal is made), and a payload that
// does not fit the task's strict shape. It writes nothing but the proposal and its audit event, which names the task and the hash, never the text.
func Propose(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, in ProposalInput) (uuid.UUID, error) {
	if what := assistant.Refuse(in.Task); what != "" {
		return uuid.Nil, &assistant.Refusal{Code: assistant.CodeNever, Message: "The assistant never does anything that means " + what + "."}
	}
	task, ok := assistant.Lookup(in.Task)
	if !ok {
		return uuid.Nil, &assistant.Refusal{Code: assistant.CodeUnknown, Message: in.Task + " is not a task type of the catalogue."}
	}
	set, err := AISettings(ctx, tx)
	if err != nil {
		return uuid.Nil, err
	}
	if !set.Enabled || !slices.Contains(set.TaskTypes, in.Task) {
		return uuid.Nil, &assistant.Refusal{Code: assistant.CodeOff, Message: "The assistant is off, or this kind of draft is: the Owner turns it on in the assistant's settings."}
	}
	if err := assistant.Check(task, in.Payload); err != nil {
		return uuid.Nil, err
	}
	body, err := json.Marshal(in.Payload)
	if err != nil {
		return uuid.Nil, err
	}
	sum := sha256.Sum256(body)
	id := uuid.Must(uuid.NewV7())
	var rationale *string
	if in.Rationale != "" {
		rationale = &in.Rationale
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.ai_proposal (id, task_type, task_version, asker, payload, payload_hash, rationale, model_id, prompt_version, created_at, expires_at) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10::timestamptz, $10::timestamptz + interval '72 hours')`,
		id, in.Task, task.Version, in.Asker, string(body), sum[:], rationale, in.ModelID, in.PromptVersion, at); err != nil {
		return uuid.Nil, assistantError(err)
	}
	return id, Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "people-assistant"}, "nemura.ai_proposal.proposed", audit.Object{Type: "ai_proposal", ID: id},
		map[string]audit.Change{"task": {nil, in.Task}, "asker": {nil, in.Asker.String()}, "payload_sha256": {nil, hexOf(sum[:])}}, "")
}

func hexOf(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&15])
	}
	return string(out)
}

// ProposalRow is a proposal as listed.
type ProposalRow struct {
	ID                     uuid.UUID
	Task                   string
	Asker                  uuid.UUID
	AskerName, DeciderName string
	Payload, Final         map[string]any
	Rationale, ModelID     string
	State, DecisionReason  string
	CreatedAt, ExpiresAt   time.Time
	DecidedAt              *time.Time
}

const proposalColumns = `p.id, p.task_type, p.asker, a.display_name, coalesce(d.display_name, ''), p.payload, p.final_payload, coalesce(p.rationale, ''), p.model_id, p.state, coalesce(p.decision_reason, ''), p.created_at, p.expires_at, p.decided_at`
const proposalFrom = ` FROM nemura.ai_proposal p JOIN nemura.member a ON a.organisation_id = p.organisation_id AND a.id = p.asker LEFT JOIN nemura.member d ON d.organisation_id = p.organisation_id AND d.id = p.decided_by`

func scanProposal(row pgx.Row) (ProposalRow, error) {
	var r ProposalRow
	var payload, final []byte
	if err := row.Scan(&r.ID, &r.Task, &r.Asker, &r.AskerName, &r.DeciderName, &payload, &final, &r.Rationale, &r.ModelID, &r.State, &r.DecisionReason, &r.CreatedAt, &r.ExpiresAt, &r.DecidedAt); err != nil {
		return r, err
	}
	if err := json.Unmarshal(payload, &r.Payload); err != nil {
		return r, err
	}
	if final != nil {
		if err := json.Unmarshal(final, &r.Final); err != nil {
			return r, err
		}
	}
	return r, nil
}

// Proposals lists proposals, newest first; state "" lists all.
func Proposals(ctx context.Context, tx pgx.Tx, state string) ([]ProposalRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+proposalColumns+proposalFrom+" WHERE ($1 = '' OR p.state = $1) ORDER BY p.created_at DESC, p.id DESC LIMIT 200", state)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProposalRow
	for rows.Next() {
		r, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ProposalOf reads one proposal.
func ProposalOf(ctx context.Context, tx pgx.Tx, id uuid.UUID) (ProposalRow, error) {
	r, err := scanProposal(tx.QueryRow(ctx, "SELECT "+proposalColumns+proposalFrom+" WHERE p.id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, &assistant.Refusal{Code: assistant.CodeNotFound, Message: "There is no such proposal."}
	}
	return r, err
}

// DecideProposal approves or rejects a pending proposal as a named human. An approval stores the final text the human saw and edited, checked against the task's strict shape, with its SHA-256; the
// executor applies that text and nothing else. A proposal past its 72 hours and one already decided are refused; a tier 1 draft may be decided by the person who asked for it. The audit event holds the hashes, never the text.
func DecideProposal(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, roles []string, id uuid.UUID, approve bool, reason string, final map[string]any) error {
	p, err := ProposalOf(ctx, tx, id)
	if err != nil {
		return err
	}
	task, _ := assistant.Lookup(p.Task)
	if !assistant.MayDecide(task, roles) {
		return &assistant.Refusal{Code: assistant.CodeRole, Message: "This kind of draft is decided by the HR officer, the HR administrator or the Owner."}
	}
	if p.State != "pending" {
		return &assistant.Refusal{Code: assistant.CodeConflict, Message: "That proposal has been decided already."}
	}
	if !approve {
		if reason == "" {
			return &assistant.Refusal{Code: assistant.CodeInvalid, Message: "reason: say why the draft is not used."}
		}
		if _, err := tx.Exec(ctx, "UPDATE nemura.ai_proposal SET state = 'rejected', decided_by = $2, decided_at = $3, decision_reason = $4 WHERE id = $1", id, by, at, reason); err != nil {
			return assistantError(err)
		}
		return Audit(ctx, tx, org, at, user(by), "nemura.ai_proposal.rejected", audit.Object{Type: "ai_proposal", ID: id}, map[string]audit.Change{"task": {nil, p.Task}}, reason)
	}
	if at.After(p.ExpiresAt) {
		return &assistant.Refusal{Code: assistant.CodeConflict, Message: "That proposal waited more than 72 hours and cannot be approved: ask the assistant again."}
	}
	if final == nil {
		final = p.Payload
	}
	if err := assistant.Check(task, final); err != nil {
		return err
	}
	body, err := json.Marshal(final)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	if _, err := tx.Exec(ctx, "UPDATE nemura.ai_proposal SET state = 'approved', decided_by = $2, decided_at = $3, decision_reason = $4, final_payload = $5::jsonb, approved_hash = $6 WHERE id = $1", id, by, at, nilIfEmpty(reason), string(body), sum[:]); err != nil {
		return assistantError(err)
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.ai_proposal.approved", audit.Object{Type: "ai_proposal", ID: id}, map[string]audit.Change{"task": {nil, p.Task}, "approved_sha256": {nil, hexOf(sum[:])}}, ""); err != nil {
		return err
	}
	return enqueueApply(ctx, tx, id)
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
