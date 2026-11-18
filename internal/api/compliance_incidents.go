package api

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// IncidentView is a security incident with its steps (SEC-32, p1-security-hardening design D10).
type IncidentView struct {
	ID         string             `json:"id" format:"uuid" doc:"The incident." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Title      string             `json:"title" doc:"What happened, in a few words." example:"An admin signed in from a new device in another country"`
	Summary    string             `json:"summary" doc:"What is known, as it was when the incident was opened." example:"The security alert impossible_travel fired for an Owner at 02:14."`
	Severity   string             `json:"severity" enum:"critical,high,medium,low" doc:"How bad it is: critical when money, personal data, access or the ledger's integrity is lost or at once at risk; high for a credible attempt stopped or exposure without evidence of access; medium for a failing control; low for a false alarm worth keeping." example:"high"`
	DetectedAt string             `json:"detected_at" format:"date-time" doc:"When it was first seen." example:"2026-11-11T02:14:00Z"`
	OpenedBy   string             `json:"opened_by" doc:"Who opened it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	OpenedAt   string             `json:"opened_at" format:"date-time" doc:"When it was opened." example:"2026-11-11T02:20:00Z"`
	Status     string             `json:"status" enum:"open,contained,assessed,notified,recovered,reviewed" doc:"Its latest step; reviewed closes it." example:"contained"`
	Breach     string             `json:"breach_id,omitempty" format:"uuid" doc:"The breach register's breach, when the assessment found personal data involved (CMP-PD-05)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
	Steps      []IncidentStepView `json:"steps" nullable:"false" doc:"Its steps, oldest first."`
}

// IncidentStepView is an incident's step.
type IncidentStepView struct {
	Step   string `json:"step" enum:"contained,assessed,notified,recovered,reviewed" doc:"The step." example:"contained"`
	At     string `json:"at" format:"date-time" doc:"When." example:"2026-11-11T02:40:00Z"`
	By     string `json:"by" doc:"Who, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Note   string `json:"note" doc:"What was found or done." example:"Removed the session; the Owner confirmed the new laptop by phone."`
	Breach string `json:"breach_id,omitempty" format:"uuid" doc:"assessed's: the breach register's breach." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
}

// IncidentInput opens an incident.
type IncidentInput struct {
	Title      string `json:"title" minLength:"1" maxLength:"200" doc:"What happened, in a few words." example:"An admin signed in from a new device in another country"`
	Summary    string `json:"summary" minLength:"1" maxLength:"8000" doc:"What is known now. Never a secret, a token, a full NRIC or a bank number (SEC-15)." example:"The security alert impossible_travel fired for an Owner at 02:14."`
	Severity   string `json:"severity" enum:"critical,high,medium,low" doc:"How bad it is (docs/runbooks/incident-response.md)." example:"high"`
	DetectedAt string `json:"detected_at,omitempty" format:"date-time" doc:"When it was first seen; left out, now. Never later than now." example:"2026-11-11T02:14:00Z"`
}

// IncidentStepInput adds an incident's step.
type IncidentStepInput struct {
	Step   string `json:"step" enum:"contained,assessed,notified,recovered,reviewed" doc:"The step: contained, assessed, notified (after assessed), recovered (after contained), reviewed (after recovered; it closes the incident)." example:"contained"`
	Note   string `json:"note" minLength:"1" maxLength:"8000" doc:"What was found or done. Never a secret, a token, a full NRIC or a bank number (SEC-15)." example:"Removed the session; the Owner confirmed the new laptop by phone."`
	Breach string `json:"breach_id,omitempty" format:"uuid" doc:"With assessed, when personal data was involved: the breach recorded in the breach register (recordBreach)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
}

type (
	incidentsOutput struct {
		Body struct {
			Items []IncidentView `json:"items" nullable:"false" doc:"The incidents, oldest first."`
		}
	}
	// IncidentPath names an incident (exported: huma reads only an exported embedded struct).
	IncidentPath struct {
		IncidentID string `path:"incident_id" format:"uuid" doc:"The incident." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	}
	incidentCreateInput struct {
		KeyHeader
		PreferHeader
		Body IncidentInput
	}
	incidentStepInput struct {
		IncidentPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body IncidentStepInput
	}
	incidentOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"The incident's steps, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     IncidentView
	}
)

// incidentSeverities are an incident's severities, worst first.
var incidentSeverities = []string{"critical", "high", "medium", "low"}

func incidentETag(v IncidentView) string { return etag("s" + strconv.Itoa(len(v.Steps))) }

func incidentView(i storage.Incident) IncidentView {
	v := IncidentView{ID: i.ID.String(), Title: i.Title, Summary: i.Summary, Severity: i.Severity, DetectedAt: i.DetectedAt.UTC().Format(time.RFC3339),
		OpenedBy: i.OpenedBy, OpenedAt: i.OpenedAt.UTC().Format(time.RFC3339), Status: "open", Steps: []IncidentStepView{}}
	for _, s := range i.Steps {
		sv := IncidentStepView{Step: s.Step, At: s.At.UTC().Format(time.RFC3339), By: s.By, Note: s.Note}
		if s.Breach != nil {
			sv.Breach, v.Breach = s.Breach.String(), s.Breach.String()
		}
		v.Steps = append(v.Steps, sv)
		v.Status = s.Step
	}
	return v
}

func (a *API) oneIncident(ctx context.Context, u storage.UnitOfWork, id string) (storage.Incident, error) {
	all, err := u.Compliance().Incidents(ctx)
	if err != nil {
		return storage.Incident{}, err
	}
	i := slices.IndexFunc(all, func(x storage.Incident) bool { return x.ID.String() == id })
	if i < 0 {
		return storage.Incident{}, problem(CodeNotFound, "There is no incident "+id+".", "Use an ID from listIncidents.")
	}
	return all[i], nil
}

func registerIncidents(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeIncident}, locks...)
	answer := func(w written[IncidentView]) *incidentOutput {
		if w.Previewed {
			return &incidentOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
		}
		return &incidentOutput{Status: w.Status, ETag: incidentETag(w.Body), Replayed: replayedHeader(w.Replayed), Body: w.Body}
	}

	register(a, authz.ListIncidents, exempt(huma.Operation{
		OperationID: "listIncidents", Method: http.MethodGet, Path: "/v1/compliance/incidents", Tags: []string{"Compliance"},
		Summary: "List the security incidents",
		Description: "The incident register (SEC-32): every security incident with its severity and its steps in the runbook's order " +
			"(docs/runbooks/incident-response.md): contained, assessed (with the breach register's breach when personal data was " +
			"involved), notified, recovered and reviewed, which closes it. Read-only; no side effects.",
	}, "cursor", "A company records few incidents, read whole as one register."), locks,
		func(ctx context.Context, _ *struct{}) (*incidentsOutput, error) {
			p, _ := authn.From(ctx)
			out := &incidentsOutput{}
			out.Body.Items = []IncidentView{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.Compliance().Incidents(ctx)
				for _, i := range all {
					out.Body.Items = append(out.Body.Items, incidentView(i))
				}
				return err
			})
			return out, err
		})

	register(a, authz.OpenIncident, huma.Operation{
		OperationID: "openIncident", Method: http.MethodPost, Path: "/v1/compliance/incidents", Tags: []string{"Compliance"},
		Summary: "Open a security incident",
		Description: "Opens an incident, or a suspected one, as soon as it is seen (SEC-32): its title, what is known, its severity and when " +
			"it was first seen. Its steps follow with stepIncident. Side effects: the incident, the audit event incident.opened. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *incidentCreateInput) (*incidentOutput, error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (IncidentView, error) {
			detected := now
			var bad []FieldProblem
			if in.Body.DetectedAt != "" {
				t, err := time.Parse(time.RFC3339, in.Body.DetectedAt)
				if err != nil || t.After(now) {
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.detected_at", Message: "is not a time up to now",
						Fix: "Send when it was first seen, RFC 3339, or leave it out for now"})
				}
				detected = t
			}
			if !slices.Contains(incidentSeverities, in.Body.Severity) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.severity", Message: "is not a severity", Fix: "Use " + strings.Join(incidentSeverities, ", ")})
			}
			if len(bad) > 0 {
				return IncidentView{}, invalid(bad)
			}
			i, err := u.Compliance().OpenIncident(ctx, storage.Incident{Title: strings.TrimSpace(in.Body.Title), Summary: strings.TrimSpace(in.Body.Summary),
				Severity: in.Body.Severity, DetectedAt: detected, OpenedBy: uploader(ctx), OpenedAt: now})
			return incidentView(i), err
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})

	register(a, authz.StepIncident, huma.Operation{
		OperationID: "stepIncident", Method: http.MethodPost, Path: "/v1/compliance/incidents/{incident_id}/steps", Tags: []string{"Compliance"},
		Summary: "Add an incident's next step",
		Description: "Records that an incident was contained, assessed (naming the breach register's breach when personal data was involved), " +
			"notified (after it was assessed), recovered (after it was contained) or reviewed (after it was recovered: the root-cause review, " +
			"which closes it) (SEC-32). Each step once. Side effects: the step, the audit event incident.<step>. Needs If-Match with its ETag. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), func(ctx context.Context, in *incidentStepInput) (*incidentOutput, error) {
		if strings.TrimSpace(in.IfMatch) == "" {
			return nil, checkIfMatch(in.IfMatch, "")
		}
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (IncidentView, error) {
			i, err := a.oneIncident(ctx, u, in.IncidentID)
			if err != nil {
				return IncidentView{}, err
			}
			if err := checkIfMatch(in.IfMatch, incidentETag(incidentView(i))); err != nil {
				return IncidentView{}, err
			}
			var breach *uuid.UUID
			if in.Body.Breach != "" {
				id, err := uuid.Parse(in.Body.Breach)
				var bad []FieldProblem
				switch {
				case in.Body.Step != "assessed":
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.breach_id", Message: "goes with the assessment, and only with it",
						Fix: "Send breach_id with assessed"})
				case err != nil:
					bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.breach_id", Message: "is not a breach's ID", Fix: "Use an ID from listBreaches"})
				default:
					if _, err := a.oneBreach(ctx, u, id.String()); err != nil {
						bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.breach_id", Message: "names no breach of the register",
							Fix: "Record the breach first (recordBreach), then use its ID"})
					}
				}
				if len(bad) > 0 {
					return IncidentView{}, invalid(bad)
				}
				breach = &id
			}
			if err := u.Compliance().StepIncident(ctx, i.ID, storage.IncidentStep{Step: in.Body.Step, At: now, By: uploader(ctx), Note: strings.TrimSpace(in.Body.Note),
				Breach: breach}); err != nil {
				return IncidentView{}, err
			}
			after, err := a.oneIncident(ctx, u, in.IncidentID)
			return incidentView(after), err
		})
		if err != nil {
			return nil, err
		}
		return answer(w), nil
	})
}
