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

// HeartbeatDrillView is a heartbeat drill (NFR-OBS-02, CTL-RES-05 Q; p1-operations-dr design D6).
type HeartbeatDrillView struct {
	ID                string `json:"id" format:"uuid" doc:"The drill." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	HeldOn            string `json:"held_on" format:"date" doc:"When it was held." example:"2026-11-15"`
	Environment       string `json:"environment" enum:"staging,production" doc:"Where the tick was paused." example:"staging"`
	PausedMinutes     int    `json:"paused_minutes" doc:"How long the tick was paused, in minutes." example:"75"`
	FirstAlertMinutes *int   `json:"first_alert_minutes,omitempty" doc:"Minutes after the pause the platform owner was first alerted; absent when no alert came." example:"31"`
	EscalationMinutes *int   `json:"escalation_minutes,omitempty" doc:"Minutes after the pause the Owners were alerted; absent when no escalation came." example:"61"`
	Passed            bool   `json:"passed" doc:"Whether the drill passed: the platform owner told within 30 minutes and the Owners within 60 (NFR-OBS-02)." example:"false"`
	Findings          string `json:"findings" doc:"What it found and what changed." example:"The first alert came a minute late: the absence window now starts at the last beat."`
	RecordedBy        string `json:"recorded_by" doc:"Who recorded it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	RecordedAt        string `json:"recorded_at" format:"date-time" doc:"When it was recorded." example:"2026-11-15T09:30:00Z"`
}

// HeartbeatDrillInput records a heartbeat drill.
type HeartbeatDrillInput struct {
	HeldOn            string `json:"held_on" format:"date" doc:"When it was held, today or earlier." example:"2026-11-15"`
	Environment       string `json:"environment" enum:"staging,production" doc:"Where the tick was paused: staging, by the runbook." example:"staging"`
	PausedMinutes     int    `json:"paused_minutes" minimum:"1" maximum:"525600" doc:"How long the tick was paused, in minutes: at least 60, to see the escalation." example:"75"`
	FirstAlertMinutes *int   `json:"first_alert_minutes,omitempty" minimum:"0" maximum:"525600" doc:"Minutes after the pause the platform owner was first alerted; leave out when no alert came." example:"28"`
	EscalationMinutes *int   `json:"escalation_minutes,omitempty" minimum:"0" maximum:"525600" doc:"Minutes after the pause the Owners were alerted; leave out when no escalation came." example:"58"`
	Findings          string `json:"findings" minLength:"1" maxLength:"8000" doc:"What it found and what changed. Never a secret, a token, a full NRIC or a bank number (SEC-15)." example:"Both alerts came on time."`
}

type (
	heartbeatDrillsOutput struct {
		Body struct {
			Items []HeartbeatDrillView `json:"items" nullable:"false" doc:"The drills, oldest first."`
		}
	}
	heartbeatDrillInput struct {
		KeyHeader
		PreferHeader
		Body HeartbeatDrillInput
	}
	heartbeatDrillOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     HeartbeatDrillView
	}
)

// drillEnvironments are where a heartbeat drill pauses the tick.
var drillEnvironments = []string{"staging", "production"}

func heartbeatDrillView(d storage.HeartbeatDrill) HeartbeatDrillView {
	return HeartbeatDrillView{ID: d.ID.String(), HeldOn: d.HeldOn.Format(time.DateOnly), Environment: d.Environment, PausedMinutes: d.PausedMinutes,
		FirstAlertMinutes: d.FirstAlertMinutes, EscalationMinutes: d.EscalationMinutes, Passed: d.Passed(), Findings: d.Findings,
		RecordedBy: d.RecordedBy, RecordedAt: d.RecordedAt.UTC().Format(time.RFC3339)}
}

func registerHeartbeatDrills(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeHeartbeatDrill}, locks...)

	register(a, authz.ListHeartbeatDrills, exempt(huma.Operation{
		OperationID: "listHeartbeatDrills", Method: http.MethodGet, Path: "/v1/compliance/heartbeat-drills", Tags: []string{"Compliance"},
		Summary: "List the heartbeat drills",
		Description: "The heartbeat drill register (NFR-OBS-02, CTL-RES-05): each drill in which the tick was paused, by " +
			"docs/runbooks/heartbeat-drill.md, with how long and how many minutes after the pause the first alert and the escalation " +
			"came; it passes when the platform owner was told within 30 minutes and the Owners within 60. Read-only; no side effects.",
	}, "cursor", "A few drills a year, read whole as one register."), locks,
		func(ctx context.Context, _ *struct{}) (*heartbeatDrillsOutput, error) {
			p, _ := authn.From(ctx)
			out := &heartbeatDrillsOutput{}
			out.Body.Items = []HeartbeatDrillView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.Compliance().HeartbeatDrills(ctx)
				for _, d := range all {
					out.Body.Items = append(out.Body.Items, heartbeatDrillView(d))
				}
				return err
			})
			return out, err
		})

	register(a, authz.RecordHeartbeatDrill, huma.Operation{
		OperationID: "recordHeartbeatDrill", Method: http.MethodPost, Path: "/v1/compliance/heartbeat-drills", Tags: []string{"Compliance"},
		Summary: "Record a heartbeat drill",
		Description: "Records a heartbeat drill (NFR-OBS-02, CTL-RES-05 Q): where and how long the tick was paused, and how many minutes " +
			"after the pause the first alert and the escalation came, if they did. The next drill is due 3 months after the last that " +
			"passed. Side effects: the drill, the audit event tick_drill.recorded. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *heartbeatDrillInput) (*heartbeatDrillOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (HeartbeatDrillView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return HeartbeatDrillView{}, err
			}
			b := in.Body
			var bad []FieldProblem
			held, err := time.Parse(time.DateOnly, b.HeldOn)
			if err != nil || held.After(today) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.held_on", Message: "is not a day up to today", Fix: "Send the day it was held, YYYY-MM-DD"})
			}
			if !slices.Contains(drillEnvironments, b.Environment) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.environment", Message: "is not an environment", Fix: "Use " + strings.Join(drillEnvironments, " or ")})
			}
			if b.PausedMinutes < 1 || b.PausedMinutes > maxDrillMinutes {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.paused_minutes", Message: "is not between 1 and a year's minutes", Fix: "Send how many minutes the tick was paused"})
			}
			for field, m := range map[string]*int{"body.first_alert_minutes": b.FirstAlertMinutes, "body.escalation_minutes": b.EscalationMinutes} {
				if m != nil && (*m < 0 || *m > maxDrillMinutes) {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: field, Message: "is not between 0 and a year's minutes", Fix: "Send the minutes after the pause, or leave it out when no alert came"})
				}
			}
			if len(bad) > 0 {
				slices.SortFunc(bad, func(x, y FieldProblem) int { return strings.Compare(x.Field, y.Field) })
				return HeartbeatDrillView{}, invalid(bad)
			}
			d, err := u.Compliance().RecordHeartbeatDrill(ctx, storage.HeartbeatDrill{HeldOn: held, Environment: b.Environment, PausedMinutes: b.PausedMinutes,
				FirstAlertMinutes: b.FirstAlertMinutes, EscalationMinutes: b.EscalationMinutes, Findings: strings.TrimSpace(b.Findings),
				RecordedBy: uploader(ctx), RecordedAt: now})
			return heartbeatDrillView(d), err
		})
		if err != nil {
			return nil, err
		}
		return &heartbeatDrillOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
