package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/proposal"
)

// Proposing (PRD-001 §9.5, AI-P3, AI-01; p1-ai-proposals-core design D3): the AI's one write, a proposal of a catalogue
// task type for a person to approve. The AI service makes it with its turn's token, which may propose only while the
// organisation has a task type on (authn.AIProposeScope) and does nothing else. The API keeps it as a Draft, checks it,
// dry-runs its command as the person in a unit of work it rolls back, and keeps the impact: the proposal is Pending, or
// Invalid with why in plain words. Nothing is changed until a person approves it.

// The codes of a proposal's refusal.
const (
	CodeProposeNotAllowed = "AI-PROPOSAL-01" // the turn may not propose, or the task type is not on for the organisation
	CodeProposalType      = "AI-PROPOSAL-02" // not a task type of the release, on the never-list, or not built
	CodeProposalRevises   = "AI-PROPOSAL-04" // revises names no proposal of the person's that waits, of the same task type
)

// AIEvidenceRef is a record or document the AI relied on.
type AIEvidenceRef struct {
	Kind  string `json:"kind" pattern:"^[a-z][a-z_]{1,40}$" doc:"The kind of record, such as task, invoice or document." example:"task"`
	ID    string `json:"id" minLength:"1" maxLength:"100" doc:"Its ID, or the key it is known by." example:"agm-2027"`
	Title string `json:"title,omitempty" maxLength:"200" doc:"What it is, in a few words." example:"Hold the AGM"`
}

// AIPropose is a proposal the AI service makes for its turn.
type AIPropose struct {
	TaskType  string          `json:"task_type" pattern:"^[a-z]+\\.[a-z]+(_[a-z]+)*$" maxLength:"56" doc:"The catalogue's task type (contracts/ai/tasks)." example:"sec.create_task"`
	Payload   map[string]any  `json:"payload" doc:"What to do, as the task type's input schema describes it: what executes once a person approves it."`
	Rationale string          `json:"rationale" maxLength:"4000" doc:"Why, in the model's words: shown to the approver apart from the platform's summary, as AI text." example:"The AGM is due by 30 June 2027 and has no task yet."`
	Evidence  []AIEvidenceRef `json:"evidence,omitempty" maxItems:"50" doc:"The records and documents it relied on."`
	Model     string          `json:"model" minLength:"1" maxLength:"200" doc:"The model the turn ran on." example:"claude-opus-5-5"`
	Prompt    string          `json:"prompt" pattern:"^[a-z][a-z_]{1,30}@[1-9][0-9]{0,3}$" doc:"The system prompt the turn ran with." example:"chat@2"`
	Revises   string          `json:"revises,omitempty" format:"uuid" doc:"A proposal made by a turn for the same person, of the same task type, still waiting, which this one replaces: once this one is Pending, that one is Superseded and its approval request void (§9.5)." example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0c"`
}

// AIProposed is what became of a proposal: Pending, waiting for a person, or Invalid, with why.
type AIProposed struct {
	ID            string `json:"id" format:"uuid" example:"0193a3c2-6d0e-7a1b-9c4d-5e6f7a8b9c0d"`
	TaskType      string `json:"task_type" example:"sec.create_task"`
	TaskVersion   int    `json:"task_version" example:"1"`
	Status        string `json:"status" enum:"pending,invalid" doc:"pending: it waits for a person's approval; invalid: it was not kept, as reason says." example:"pending"`
	Reason        string `json:"reason,omitempty" doc:"Why it is invalid, in plain words, for the model to tell the person or correct." example:"payload.title: expected length >= 2"`
	Summary       string `json:"summary,omitempty" doc:"What it would do, in the platform's words (AI-07)." example:"Creates the task Hold the AGM, due 30 June 2027"`
	PayloadSHA256 string `json:"payload_sha256" doc:"The SHA-256 of the payload a person approves, in hex." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	ExpiresAt     string `json:"expires_at" format:"date-time" doc:"When it expires undecided." example:"2026-11-18T09:00:00Z"`
}

type (
	aiProposeInput  struct{ Body AIPropose }
	aiProposeOutput struct{ Body AIProposed }
)

// commands are the release's task types' commands: Deps.Commands, else the release's (proposal.Release).
func (a *API) commands() proposal.Commands {
	if a.deps.Commands != nil {
		return a.deps.Commands
	}
	return proposal.Release
}

func registerProposals(a *API) {
	internalOp(a, huma.Operation{
		OperationID: "proposeAITask", Method: http.MethodPost, Path: internalBase + "/proposals", Tags: []string{"Assistant"},
		Summary: "Propose a change for a person to approve", DefaultStatus: http.StatusCreated,
		Description: "Makes a proposal of a catalogue task type for the turn's person (AI-P3), the AI's only write. The turn's token " +
			"may propose only while the organisation has a task type on (ai.task_types), and does nothing else. The API keeps it as a " +
			"Draft, checks the AI switch, that the task type is on, its payload against the task type's schema and limits, and that the " +
			"person may do it themselves; screens what the model wrote; then dry-runs it as the person in a unit of work it rolls back " +
			"and keeps the impact and the versions it read. It answers Pending, waiting for a person, or Invalid with why. A revision " +
			"names the proposal it replaces: the person's own, of the same task type, still waiting; once the revision is Pending, " +
			"that one is Superseded and its approval request void, in the same transaction. Nothing changes until a person approves " +
			"it, and the approval and the execution are never the AI's.",
	}, []string{CodeProposeNotAllowed, CodeProposalType, CodeProposalRevises, airoute.CodeSwitchOff, CodeScreenIncomplete},
		func(ctx context.Context, in *aiProposeInput) (*aiProposeOutput, error) {
			p, _ := authn.From(ctx)
			if !slices.Contains(p.Scopes, authn.AIProposeScope) {
				return nil, problem(CodeProposeNotAllowed, "This turn may not propose changes: no task type is on for the organisation.",
					"Ask an Owner to turn on a task type in Settings → AI")
			}
			tt, ok := a.catalogue().Get(in.Body.TaskType)
			if !ok {
				return nil, problem(CodeProposalType, in.Body.TaskType+" is not a task type of this release.", "Propose one of the task types the turn was offered")
			}
			if p.Run != uuid.Nil && !slices.Contains(p.AITaskTypes, tt.ID) { // a run proposes its skill's outputs alone (AI-63)
				return nil, problem(CodeProposeNotAllowed, tt.ID+" is not one of the agent's outputs.", "Propose one of the task types the run was offered")
			}
			if why := proposal.Refuse(tt.ID, tt.Command); why != "" { // the catalogue refused it as it loaded; refuse it again by name
				return nil, problem(CodeProposalType, tt.ID+" can never be proposed: it is "+why+" (§9.6).", "Leave it to a person to do")
			}
			cmd, ok := a.commands()[tt.Command]
			if !ok {
				return nil, problem(CodeProposalType, tt.ID+" is not built in this release.", "Propose one of the task types the turn was offered")
			}
			raw, err := json.Marshal(in.Body.Payload)
			if err != nil {
				return nil, err
			}
			payload, sum, err := proposal.Canonical(raw)
			if err != nil {
				return nil, problem(CodeInvalid, "The payload is not a JSON object.", "Send the payload as the task type's input schema describes it")
			}
			revises, err := a.revisable(ctx, p, tt, in.Body.Revises)
			if err != nil {
				return nil, err
			}
			made, err := a.draftProposal(ctx, p, tt, in.Body, payload, sum)
			if err != nil {
				return nil, err
			}
			reason, impact, err := a.checkProposal(ctx, p, tt, cmd, in.Body, payload)
			if err != nil {
				_, _ = a.endProposal(ctx, p, made, "it could not be checked: "+plainError(err), proposal.Impact{}, tt, cmd, nil)
				return nil, err
			}
			if reason, err = a.endProposal(ctx, p, made, reason, impact, tt, cmd, revises); err != nil {
				return nil, err
			}
			out := AIProposed{ID: made.ID.String(), TaskType: tt.ID, TaskVersion: tt.Version, Status: storage.ProposalPending, Summary: impact.Summary,
				PayloadSHA256: sum, ExpiresAt: made.ExpiresAt.UTC().Format(time.RFC3339)}
			if reason != "" {
				out.Status, out.Reason, out.Summary = storage.ProposalInvalid, reason, ""
			}
			return &aiProposeOutput{Body: out}, nil
		})
}

// draftProposal keeps the proposal as a Draft, once the AI is on and the task type is on for the organisation.
func (a *API) draftProposal(ctx context.Context, p authn.Principal, tt proposal.TaskType, in AIPropose, payload, sum string) (storage.AIProposal, error) {
	var made storage.AIProposal
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return err
		}
		if on, err := settingOn(ctx, u, ai.Switch, today); err != nil {
			return err
		} else if on != "true" {
			return problem(airoute.CodeSwitchOff, "The AI is switched off for the organisation.", "Ask an Owner to switch it on in Settings → AI")
		}
		types, err := settingOn(ctx, u, settingTaskTypes, today)
		if err != nil {
			return err
		}
		if !slices.Contains(strings.Split(types, ","), tt.ID) {
			return problem(CodeProposeNotAllowed, tt.ID+" is not turned on for the organisation.", "Ask an Owner to turn it on in Settings → AI")
		}
		evidence := make([]storage.AIEvidence, 0, len(in.Evidence))
		for _, e := range in.Evidence {
			evidence = append(evidence, storage.AIEvidence{Kind: e.Kind, ID: e.ID, Title: e.Title})
		}
		now := a.deps.Clock.Now()
		draft := storage.AIProposal{TaskType: tt.ID, TaskVersion: tt.Version, TaskSHA256: tt.SHA256, Payload: payload,
			PayloadSHA256: sum, ItemCount: batchItems(payload), Rationale: in.Rationale, Evidence: evidence, RiskTier: tt.Tier, RequiredApprovals: 1,
			Agent: "chat", RequestedBy: p.UserID, Model: in.Model, Prompt: in.Prompt, ToolsSHA256: toolsSHA256(),
			ProposedAt: now, ExpiresAt: now.Add(tt.Expiry())}
		if turn, run := p.Turn, p.Run; run != uuid.Nil { // a scheduled agent's, from its run, for the Owner it acts for (AI-63)
			draft.Agent, draft.Run = p.Agent, &run
		} else {
			draft.Turn = &turn
		}
		made, err = u.AIProposals().Create(ctx, draft)
		return err
	})
	return made, err
}

// checkProposal is why a Draft is invalid, or its dry run's impact: its payload against the task type's schema and
// limits, the person's own right to do it, what the model wrote screened as an answer, then the dry run as the person
// in a unit of work rolled back. An error is one it could not finish.
func (a *API) checkProposal(ctx context.Context, p authn.Principal, tt proposal.TaskType, cmd proposal.Command, in AIPropose,
	payload string) (string, proposal.Impact, error) {
	if words, err := schemaErrors(tt.InputSchema, in.Payload, "payload"); err != nil {
		return "", proposal.Impact{}, err
	} else if len(words) > 0 {
		return strings.Join(words, "; "), proposal.Impact{}, nil
	}
	m, err := cmd.Measure(json.RawMessage(payload))
	if err != nil {
		return "the payload cannot be measured: " + plainError(err), proposal.Impact{}, nil
	}
	if why := tt.Within(m); why != "" {
		return why, proposal.Impact{}, nil
	}
	if d := authz.Decide(cmd.Policy(), p.Roles, false); !d.Allowed {
		return "the person it is for may not do this themselves, so it cannot be proposed for them", proposal.Impact{}, nil
	}
	if err := a.screen(ctx, true, append([]string{in.Rationale}, texts(in.Payload)...)...); err != nil {
		var pr *Problem
		if errors.As(err, &pr) && pr.Code == CodeScreened {
			return "screening found " + findingsOf(pr) + " in what the model wrote", proposal.Impact{}, nil
		}
		return "", proposal.Impact{}, err
	}
	var impact proposal.Impact
	var today time.Time
	err = a.deps.Store.Do(storage.WithPreview(ctx), p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if today, err = orgToday(ctx, a, u); err != nil {
			return err
		}
		impact, err = cmd.DryRun(ctx, u, json.RawMessage(payload), proposal.Actor{User: p.UserID, Roles: p.Roles, Today: today})
		return err
	})
	switch {
	case errors.Is(err, storage.ErrPreviewed):
		return "", impact, nil
	case err == nil:
		return "", proposal.Impact{}, errors.New("the dry run committed: it must run in a preview")
	}
	var pr *Problem
	var se *storage.Error
	if errors.As(err, &pr) || errors.As(err, &se) {
		return "the dry run refused it: " + plainError(err), proposal.Impact{}, nil
	}
	return "", proposal.Impact{}, err
}

// endProposal moves a Draft on, in one unit of work, and gives why it is Invalid, or "" when it is Pending: to Invalid
// with reason, or to Pending with the dry run's impact and its approval request (AI-89). A revision's Pending supersedes
// the proposal it replaces, whose approval request the database voids; one that stopped waiting meanwhile leaves the
// revision Invalid.
func (a *API) endProposal(ctx context.Context, p authn.Principal, made storage.AIProposal, reason string, impact proposal.Impact,
	tt proposal.TaskType, cmd proposal.Command, revises *uuid.UUID) (string, error) {
	var why string
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		why = reason
		var earlier storage.AIProposal
		if why == "" && revises != nil {
			var err error
			if earlier, err = u.AIProposals().Get(ctx, *revises); err != nil {
				return err
			}
			if earlier.Status != storage.ProposalPending {
				why = "the proposal it revises stopped waiting meanwhile: it is " + earlier.Status
			}
		}
		if why != "" {
			_, err := u.AIProposals().Invalid(ctx, made.ID, made.Version, cut(why, 1000))
			return err
		}
		body, err := json.Marshal(impact)
		if err != nil {
			return err
		}
		r, err := a.requestProposal(ctx, u, p, made, tt, cmd, impact)
		if err != nil {
			return err
		}
		if _, err = u.AIProposals().Pending(ctx, made.ID, made.Version, body, impact.Base, &r.ID); err != nil {
			return err
		}
		if revises != nil {
			_, err = u.AIProposals().Supersede(ctx, earlier.ID, earlier.Version, made.ID, a.deps.Clock.Now())
		}
		return err
	})
	return why, err
}

// revisable is the proposal a revision replaces, or nil when it revises none: one made by a turn for the same person, of
// the same task type, still waiting (§9.5).
func (a *API) revisable(ctx context.Context, p authn.Principal, tt proposal.TaskType, revises string) (*uuid.UUID, error) {
	if revises == "" {
		return nil, nil
	}
	id, err := uuid.Parse(revises)
	if err != nil {
		return nil, problem(CodeProposalRevises, "revises is not a proposal's ID.", "Name the ID of a proposal of the person's that still waits")
	}
	var why string
	err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		earlier, err := u.AIProposals().Get(ctx, id)
		switch {
		case errors.Is(err, ports.ErrNotFound) || err == nil && (earlier.RequestedBy != p.UserID || earlier.Turn == nil):
			why = "names no proposal made for this person"
		case err != nil:
			return err
		case earlier.TaskType != tt.ID:
			why = "names a proposal of " + earlier.TaskType + ", not " + tt.ID
		case earlier.Status != storage.ProposalPending:
			why = "names a proposal that no longer waits: it is " + earlier.Status
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if why != "" {
		return nil, problem(CodeProposalRevises, "revises "+why+".", "Propose it as a new proposal, or revise one of the person's that still waits")
	}
	return &id, nil
}

// schemaErrors are what in breaks of schema, each naming its field under at.
func schemaErrors(schema json.RawMessage, in map[string]any, at string) ([]string, error) {
	var s huma.Schema
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil, err
	}
	s.PrecomputeMessages()
	res := &huma.ValidateResult{}
	if in == nil {
		in = map[string]any{}
	}
	huma.Validate(huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer), &s, huma.NewPathBuffer([]byte(at), 0),
		huma.ModeWriteToServer, in, res)
	words := make([]string, 0, len(res.Errors))
	for _, e := range res.Errors {
		words = append(words, e.Error())
	}
	return words, nil
}

// batchItems is a batch's items, the payload's top-level items array, or 0 when it is not a batch.
func batchItems(payload string) int {
	var v struct {
		Items []json.RawMessage `json:"items"`
	}
	if json.Unmarshal([]byte(payload), &v) != nil {
		return 0
	}
	return min(len(v.Items), 100)
}

// texts are the free texts of v, for screening.
func texts(v any) []string {
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case map[string]any:
			for _, y := range x {
				walk(y)
			}
		case []any:
			for _, y := range x {
				walk(y)
			}
		}
	}
	walk(v)
	return out
}

// toolsSHA256 is the hash of the read tools the turn was offered, as the registry pins them: each tool's name, version
// and definition's hash.
func toolsSHA256() string {
	var b strings.Builder
	for _, t := range aitool.Default().Tools() {
		fmt.Fprintf(&b, "%s@%d:%s\n", t.Name, t.Version, t.SHA256)
	}
	return hashHex(b.String())
}

// plainError is err's message as a person reads it: a problem's detail and its fields'.
func plainError(err error) string {
	var pr *Problem
	if errors.As(err, &pr) {
		words := []string{strings.TrimSuffix(pr.Detail, ".")}
		for _, f := range pr.Errors {
			words = append(words, f.Field+": "+f.Message)
		}
		return strings.Join(words, "; ")
	}
	var se *storage.Error
	if errors.As(err, &se) {
		return se.Message
	}
	return "an unexpected error"
}

// cut is s at most n bytes, on a rune boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

func hashHex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
