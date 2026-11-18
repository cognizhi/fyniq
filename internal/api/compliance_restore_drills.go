package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RestoreDrillView is a full restore drill (DR-06, CTL-RES-02 Q; p1-operations-dr design D2).
type RestoreDrillView struct {
	ID              string `json:"id" format:"uuid" doc:"The drill." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	HeldOn          string `json:"held_on" format:"date" doc:"When it was held." example:"2026-11-15"`
	Scenario        string `json:"scenario" enum:"project_loss,corruption,zone_failure" doc:"The scenario it rehearsed (NFR-DR-01, NFR-DR-02): the loss of the production project (the weekly export restored into an isolated project), database corruption or operator error (point-in-time recovery), or a zone's failure (failover)." example:"project_loss"`
	RestoredFrom    string `json:"restored_from" doc:"What the copy was made from: the weekly export or the point in time." example:"The weekly export of 2026-11-08"`
	DataLostMinutes int    `json:"data_lost_minutes" doc:"The data the copy lacked, in minutes before the drill's starting point (NFR-DR-01)." example:"10080"`
	TookMinutes     int    `json:"took_minutes" doc:"How long the recovery took, in minutes (NFR-DR-02)." example:"185"`
	Ledger          bool   `json:"ledger" doc:"The ledger's verification passed on the copy (NFR-DR-03)." example:"true"`
	AuditChain      bool   `json:"audit_chain" doc:"The audit chain's verification passed on the copy (NFR-DR-03)." example:"true"`
	Documents       bool   `json:"documents" doc:"Documents and evidence opened from the copy." example:"true"`
	Keys            bool   `json:"keys" doc:"The keys decrypted the sealed fields and signed an anchor." example:"true"`
	SignIn          bool   `json:"sign_in" doc:"People signed in to the copy." example:"true"`
	Tick            bool   `json:"tick" doc:"The Scheduler's tick ran the platform jobs." example:"true"`
	Outbox          bool   `json:"outbox" doc:"The outbox's unfinished work was reconciled (outbox-after-restore)." example:"true"`
	Recovered       bool   `json:"recovered" doc:"Whether the drill counts as recovered: every check passed (DR-06)." example:"true"`
	Findings        string `json:"findings" doc:"What it found and what changed." example:"Importing the export took 40 minutes; the runbook now starts it first."`
	RecordedBy      string `json:"recorded_by" doc:"Who recorded it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	RecordedAt      string `json:"recorded_at" format:"date-time" doc:"When it was recorded." example:"2026-11-15T09:30:00Z"`
}

// maxDrillMinutes is the most minutes a drill records: a year's.
const maxDrillMinutes = 525600

// RestoreDrillInput records a full restore drill.
type RestoreDrillInput struct {
	HeldOn          string `json:"held_on" format:"date" doc:"When it was held, today or earlier." example:"2026-11-15"`
	Scenario        string `json:"scenario" enum:"project_loss,corruption,zone_failure" doc:"The scenario it rehearsed (docs/runbooks/restore-drill.md)." example:"project_loss"`
	RestoredFrom    string `json:"restored_from" minLength:"1" maxLength:"500" doc:"What the copy was made from: the weekly export or the point in time." example:"The weekly export of 2026-11-08"`
	DataLostMinutes int    `json:"data_lost_minutes" minimum:"0" maximum:"525600" doc:"The data the copy lacked, in minutes (NFR-DR-01)." example:"10080"`
	TookMinutes     int    `json:"took_minutes" minimum:"1" maximum:"525600" doc:"How long the recovery took, in minutes (NFR-DR-02)." example:"185"`
	Ledger          bool   `json:"ledger" doc:"The ledger's verification passed on the copy." example:"true"`
	AuditChain      bool   `json:"audit_chain" doc:"The audit chain's verification passed on the copy." example:"true"`
	Documents       bool   `json:"documents" doc:"Documents and evidence opened from the copy." example:"true"`
	Keys            bool   `json:"keys" doc:"The keys decrypted the sealed fields and signed an anchor." example:"true"`
	SignIn          bool   `json:"sign_in" doc:"People signed in to the copy." example:"true"`
	Tick            bool   `json:"tick" doc:"The Scheduler's tick ran the platform jobs." example:"true"`
	Outbox          bool   `json:"outbox" doc:"The outbox's unfinished work was reconciled." example:"true"`
	Findings        string `json:"findings" minLength:"1" maxLength:"8000" doc:"What it found and what changed. Never a secret, a token, a full NRIC or a bank number (SEC-15)." example:"Importing the export took 40 minutes; the runbook now starts it first."`
}

type (
	restoreDrillsOutput struct {
		Body struct {
			Items []RestoreDrillView `json:"items" nullable:"false" doc:"The drills, oldest first."`
		}
	}
	restoreDrillInput struct {
		KeyHeader
		PreferHeader
		Body RestoreDrillInput
	}
	restoreDrillOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     RestoreDrillView
	}
)

// drillScenarios are the scenarios a drill rehearses.
var drillScenarios = []string{"project_loss", "corruption", "zone_failure"}

func restoreDrillView(d storage.RestoreDrill) RestoreDrillView {
	return RestoreDrillView{ID: d.ID.String(), HeldOn: d.HeldOn.Format(time.DateOnly), Scenario: d.Scenario, RestoredFrom: d.RestoredFrom,
		DataLostMinutes: d.DataLostMinutes, TookMinutes: d.TookMinutes, Ledger: d.Ledger, AuditChain: d.AuditChain, Documents: d.Documents, Keys: d.Keys,
		SignIn: d.SignIn, Tick: d.Tick, Outbox: d.Outbox, Recovered: d.Recovered(), Findings: d.Findings, RecordedBy: d.RecordedBy,
		RecordedAt: d.RecordedAt.UTC().Format(time.RFC3339)}
}

func registerRestoreDrills(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeRestoreDrill}, locks...)

	register(a, authz.ListRestoreDrills, exempt(huma.Operation{
		OperationID: "listRestoreDrills", Method: http.MethodGet, Path: "/v1/compliance/restore-drills", Tags: []string{"Compliance"},
		Summary: "List the restore drills",
		Description: "The restore drill register (DR-06, CTL-RES-02): each full drill run by docs/runbooks/restore-drill.md, with its scenario, " +
			"what it restored from, the data lost and the time taken, and DR-06's checks on the restored copy; a drill counts as recovered " +
			"only when every check passed. The monthly drill the platform runs itself is the job restore.drill, in the audit trail. " +
			"Read-only; no side effects.",
	}, "cursor", "A few drills a year, read whole as one register."), locks,
		func(ctx context.Context, _ *struct{}) (*restoreDrillsOutput, error) {
			p, _ := authn.From(ctx)
			out := &restoreDrillsOutput{}
			out.Body.Items = []RestoreDrillView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.Compliance().RestoreDrills(ctx)
				for _, d := range all {
					out.Body.Items = append(out.Body.Items, restoreDrillView(d))
				}
				return err
			})
			return out, err
		})

	register(a, authz.RecordRestoreDrill, huma.Operation{
		OperationID: "recordRestoreDrill", Method: http.MethodPost, Path: "/v1/compliance/restore-drills", Tags: []string{"Compliance"},
		Summary: "Record a restore drill",
		Description: "Records a full restore drill (DR-06, CTL-RES-02 Q): its scenario, what it restored from, the data lost and the time " +
			"taken, and whether the ledger's and the audit chain's verifications, documents, keys, sign-in, the tick and the outbox each " +
			"worked on the restored copy. The next full drill is due 3 months after the last that recovered. Side effects: the drill, the " +
			"audit event restore_drill.recorded. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *restoreDrillInput) (*restoreDrillOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (RestoreDrillView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return RestoreDrillView{}, err
			}
			var bad []FieldProblem
			held, err := time.Parse(time.DateOnly, in.Body.HeldOn)
			if err != nil || held.After(today) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.held_on", Message: "is not a day up to today", Fix: "Send the day it was held, YYYY-MM-DD"})
			}
			if !slices.Contains(drillScenarios, in.Body.Scenario) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.scenario", Message: "is not a scenario", Fix: "Use " + strings.Join(drillScenarios, ", ")})
			}
			if in.Body.DataLostMinutes < 0 || in.Body.DataLostMinutes > maxDrillMinutes {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.data_lost_minutes", Message: "is not between 0 and a year's minutes", Fix: "Send the minutes of data the copy lacked"})
			}
			if in.Body.TookMinutes < 1 || in.Body.TookMinutes > maxDrillMinutes {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.took_minutes", Message: "is not between 1 and a year's minutes", Fix: "Send how many minutes the recovery took"})
			}
			if len(bad) > 0 {
				return RestoreDrillView{}, invalid(bad)
			}
			b := in.Body
			d, err := u.Compliance().RecordRestoreDrill(ctx, storage.RestoreDrill{HeldOn: held, Scenario: b.Scenario, RestoredFrom: strings.TrimSpace(b.RestoredFrom),
				DataLostMinutes: b.DataLostMinutes, TookMinutes: b.TookMinutes, Ledger: b.Ledger, AuditChain: b.AuditChain, Documents: b.Documents,
				Keys: b.Keys, SignIn: b.SignIn, Tick: b.Tick, Outbox: b.Outbox, Findings: strings.TrimSpace(b.Findings), RecordedBy: uploader(ctx), RecordedAt: now})
			return restoreDrillView(d), err
		})
		if err != nil {
			return nil, err
		}
		return &restoreDrillOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
