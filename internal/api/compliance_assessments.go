package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	compliancedomain "github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The register of security assessments (SEC-25, CTL-CHG-12; rev 0.617; p2-security-certification design D4): penetration tests and other
// assessments of the platform, with each finding's severity, due day, fix and retest, or an Owner's acceptance. Only ever added.

var (
	assessmentKinds = []string{"penetration_test", "vulnerability_assessment", "other"}
	severities      = []string{"critical", "high", "medium", "low", "info"}
	findingSteps    = []string{"fixed", "retested", "accepted"}
	reportHashHex   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// FindingStepView is a step a finding took.
type FindingStepView struct {
	Step       string `json:"step" enum:"fixed,retested,accepted" doc:"fixed, then retested; or accepted by an Owner, with a reason." example:"fixed"`
	On         string `json:"on" format:"date" doc:"The day it happened." example:"2026-11-12"`
	Reason     string `json:"reason,omitempty" doc:"An acceptance's reason." example:"The affected page is retired next month"`
	RecordedBy string `json:"recorded_by" doc:"Who recorded it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	RecordedAt string `json:"recorded_at" format:"date-time" doc:"When." example:"2026-11-12T02:00:00Z"`
}

// SecurityFindingView is a finding of an assessment.
type SecurityFindingView struct {
	FindingID string            `json:"finding_id" format:"uuid" doc:"The finding." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	Ref       string            `json:"ref" doc:"The tester's reference." example:"PT-2026-07"`
	Title     string            `json:"title" doc:"What it is." example:"Reflected script in the search page"`
	Severity  string            `json:"severity" enum:"critical,high,medium,low,info" doc:"How serious." example:"high"`
	FoundOn   string            `json:"found_on" format:"date" doc:"The day it was found." example:"2026-11-02"`
	DueOn     string            `json:"due_on,omitempty" format:"date" doc:"The day it is due: 7 days for Critical, 30 High, 90 Medium, 180 Low from the day found; none for info." example:"2026-12-02"`
	State     string            `json:"state" enum:"open,fixed,retested,accepted" doc:"Where it stands." example:"open"`
	Overdue   bool              `json:"overdue" doc:"true when a Critical or High finding is past its day: open after its due day, or fixed and not retested 30 days on." example:"false"`
	Steps     []FindingStepView `json:"steps" nullable:"false" doc:"The steps it took, in order."`
}

// SecurityAssessmentView is an assessment with its findings.
type SecurityAssessmentView struct {
	AssessmentID   string                `json:"assessment_id" format:"uuid" doc:"The assessment." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
	Kind           string                `json:"kind" enum:"penetration_test,vulnerability_assessment,other" doc:"What it was." example:"penetration_test"`
	Tester         string                `json:"tester" doc:"The firm or person." example:"Example Security Pte Ltd"`
	Independent    bool                  `json:"independent" doc:"true when the testers are not the people who build and run Fyniq." example:"true"`
	Scope          string                `json:"scope" doc:"What it covered." example:"The web app, the API and the MCP server on staging"`
	StartedOn      string                `json:"started_on" format:"date" doc:"The first day." example:"2026-10-19"`
	EndedOn        string                `json:"ended_on" format:"date" doc:"The last day." example:"2026-11-02"`
	Summary        string                `json:"summary" doc:"What it concluded." example:"No critical finding; one high"`
	ReportSHA256   string                `json:"report_sha256,omitempty" doc:"The report's SHA-256, so it is shown to be the one received." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	ReportLocation string                `json:"report_location,omitempty" doc:"Where the report is kept; it stays outside Fyniq." example:"The security folder of the shared drive"`
	RecordedBy     string                `json:"recorded_by" doc:"Who recorded it, as records name actors." example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	RecordedAt     string                `json:"recorded_at" format:"date-time" doc:"When." example:"2026-11-03T02:00:00Z"`
	Findings       []SecurityFindingView `json:"findings" nullable:"false" doc:"Its findings."`
}

// SecurityAssessmentRegister is the register, the latest ended first.
type SecurityAssessmentRegister struct {
	Items           []SecurityAssessmentView `json:"items" nullable:"false" doc:"The assessments, the latest ended first."`
	LastIndependent string                   `json:"last_independent_on,omitempty" format:"date" doc:"The day the last independent assessment ended." example:"2026-11-02"`
	NextDue         string                   `json:"next_due_on,omitempty" format:"date" doc:"The day the next independent test is due: 12 months after the last." example:"2027-11-02"`
	OverdueFindings int                      `json:"overdue_findings" doc:"How many Critical or High findings are past their day." example:"0"`
}

// SecurityFindingInput is a finding to record.
type SecurityFindingInput struct {
	Ref      string `json:"ref" minLength:"1" maxLength:"40" doc:"The tester's reference." example:"PT-2026-07"`
	Title    string `json:"title" minLength:"1" maxLength:"300" doc:"What it is." example:"Reflected script in the search page"`
	Severity string `json:"severity" enum:"critical,high,medium,low,info" doc:"How serious." example:"high"`
	FoundOn  string `json:"found_on,omitempty" format:"date" doc:"The day it was found; the assessment's last day when left out." example:"2026-11-02"`
}

// SecurityAssessmentInput is an assessment to record, with its findings.
type SecurityAssessmentInput struct {
	Kind           string                 `json:"kind" enum:"penetration_test,vulnerability_assessment,other" doc:"What it was." example:"penetration_test"`
	Tester         string                 `json:"tester" minLength:"1" maxLength:"300" doc:"The firm or person." example:"Example Security Pte Ltd"`
	Independent    bool                   `json:"independent" doc:"true when the testers are not the people who build and run Fyniq." example:"true"`
	Scope          string                 `json:"scope" minLength:"1" maxLength:"2000" doc:"What it covered." example:"The web app, the API and the MCP server on staging"`
	StartedOn      string                 `json:"started_on" format:"date" doc:"The first day." example:"2026-10-19"`
	EndedOn        string                 `json:"ended_on" format:"date" doc:"The last day, not after today." example:"2026-11-02"`
	Summary        string                 `json:"summary" minLength:"1" maxLength:"4000" doc:"What it concluded." example:"No critical finding; one high"`
	ReportSHA256   string                 `json:"report_sha256,omitempty" pattern:"^[0-9a-f]{64}$" doc:"The report's SHA-256, lower-case hex." example:"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"`
	ReportLocation string                 `json:"report_location,omitempty" maxLength:"500" doc:"Where the report is kept." example:"The security folder of the shared drive"`
	Findings       []SecurityFindingInput `json:"findings,omitempty" maxItems:"200" doc:"Its findings."`
}

// SecurityFindingStepInput is the next step of a finding.
type SecurityFindingStepInput struct {
	Step   string `json:"step" enum:"fixed,retested,accepted" doc:"fixed, then retested; or accepted, before the retest, with a reason." example:"fixed"`
	On     string `json:"on" format:"date" doc:"The day it happened, not after today." example:"2026-11-12"`
	Reason string `json:"reason,omitempty" maxLength:"500" doc:"An acceptance's reason: why the risk is taken." example:"The affected page is retired next month"`
}

type (
	securityRegisterOutput struct{ Body SecurityAssessmentRegister }
	// SecurityAssessmentPath names an assessment (exported: huma reads only an exported embedded struct).
	SecurityAssessmentPath struct {
		AssessmentID string `path:"assessment_id" format:"uuid" doc:"The assessment." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e11"`
	}
	// SecurityFindingPath names a finding.
	SecurityFindingPath struct {
		FindingID string `path:"finding_id" format:"uuid" doc:"The finding." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5e10"`
	}
	securityAssessmentInput struct {
		KeyHeader
		PreferHeader
		Body SecurityAssessmentInput
	}
	securityFindingInput struct {
		SecurityAssessmentPath
		KeyHeader
		PreferHeader
		Body SecurityFindingInput
	}
	securityStepInput struct {
		SecurityFindingPath
		KeyHeader
		PreferHeader
		Body SecurityFindingStepInput
	}
	securityWrite[T any] struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     T
	}
)

func findingView(f storage.Finding, today time.Time) SecurityFindingView {
	v := SecurityFindingView{FindingID: f.ID.String(), Ref: f.Ref, Title: f.Title, Severity: f.Severity, FoundOn: f.FoundOn.Format(time.DateOnly),
		State: compliancedomain.FindingState(f), Steps: []FindingStepView{}}
	if f.DueOn != nil {
		v.DueOn = f.DueOn.Format(time.DateOnly)
	}
	_, v.Overdue, _ = compliancedomain.FindingOverdue(f, today)
	for _, s := range f.Steps {
		v.Steps = append(v.Steps, FindingStepView{Step: s.Step, On: s.OnDate.Format(time.DateOnly), Reason: s.Reason, RecordedBy: s.RecordedBy,
			RecordedAt: s.RecordedAt.UTC().Format(time.RFC3339)})
	}
	return v
}

func assessmentView(a storage.Assessment, today time.Time) SecurityAssessmentView {
	v := SecurityAssessmentView{AssessmentID: a.ID.String(), Kind: a.Kind, Tester: a.Tester, Independent: a.Independent, Scope: a.Scope,
		StartedOn: a.StartedOn.Format(time.DateOnly), EndedOn: a.EndedOn.Format(time.DateOnly), Summary: a.Summary, ReportSHA256: a.ReportSHA256,
		ReportLocation: a.ReportLocation, RecordedBy: a.RecordedBy, RecordedAt: a.RecordedAt.UTC().Format(time.RFC3339), Findings: []SecurityFindingView{}}
	for _, f := range a.Findings {
		v.Findings = append(v.Findings, findingView(f, today))
	}
	return v
}

func securityRegister(list []storage.Assessment, today time.Time) SecurityAssessmentRegister {
	out := SecurityAssessmentRegister{Items: []SecurityAssessmentView{}}
	var last *time.Time
	for _, a := range list {
		v := assessmentView(a, today)
		for _, f := range v.Findings {
			if f.Overdue {
				out.OverdueFindings++
			}
		}
		out.Items = append(out.Items, v)
		if a.Independent && (last == nil || a.EndedOn.After(*last)) {
			ended := a.EndedOn
			last = &ended
		}
	}
	if last != nil {
		out.LastIndependent = last.Format(time.DateOnly)
		out.NextDue = last.AddDate(1, 0, 0).Format(time.DateOnly)
	}
	return out
}

// findingOf reads a finding's input: its severity, the day found (the assessment's last day by default) and the day it is due by
// its severity.
func findingOf(in SecurityFindingInput, ended time.Time, today time.Time, field string, now time.Time, by string) (storage.Finding, []FieldProblem) {
	f := storage.Finding{Ref: strings.TrimSpace(in.Ref), Title: strings.TrimSpace(in.Title), Severity: in.Severity, FoundOn: ended, RecordedBy: by, RecordedAt: now}
	var bad []FieldProblem
	if f.Ref == "" || f.Title == "" {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: field, Message: "needs a reference and a title", Fix: "Give the tester's reference and say what the finding is"})
	}
	if !slices.Contains(severities, f.Severity) {
		bad = append(bad, FieldProblem{Code: CodeInvalid, Field: field + ".severity", Message: "is not a severity", Fix: "Use " + strings.Join(severities, ", ")})
	}
	if in.FoundOn != "" {
		d, err := time.Parse(time.DateOnly, in.FoundOn)
		if err != nil || d.After(today) {
			bad = append(bad, FieldProblem{Code: CodeInvalid, Field: field + ".found_on", Message: "is not a day up to today", Fix: "Send the day it was found, YYYY-MM-DD"})
		} else {
			f.FoundOn = d
		}
	}
	f.DueOn = compliancedomain.FindingDue(f.Severity, f.FoundOn)
	return f, bad
}

func registerSecurityAssessments(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid,
		storage.CodeAssessment}, locks...)
	register(a, authz.ListSecurityAssessments, exempt(huma.Operation{
		OperationID: "listSecurityAssessments", Method: http.MethodGet, Path: "/v1/compliance/security-assessments", Tags: []string{"Compliance"},
		Summary: "List the security assessments",
		Description: "The register of security assessments (SEC-25, CTL-CHG-12): each penetration test or other assessment of the platform with its tester, " +
			"whether they are independent, the scope, dates, the report's SHA-256 and where it is kept, and each finding with its severity, due day, state, fix and retest; " +
			"the day the last independent test ended and the next is due, and how many Critical or High findings are overdue. Read-only; no side effects.",
	}, "cursor", "A few assessments a year, read whole as one register."), locks,
		func(ctx context.Context, _ *struct{}) (*securityRegisterOutput, error) {
			p, _ := authn.From(ctx)
			out := &securityRegisterOutput{}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				list, err := u.Compliance().Assessments(ctx)
				if err != nil {
					return err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return err
				}
				out.Body = securityRegister(list, today)
				return nil
			})
			return out, err
		})

	register(a, authz.CreateSecurityAssessment, huma.Operation{
		OperationID: "createSecurityAssessment", Method: http.MethodPost, Path: "/v1/compliance/security-assessments", Tags: []string{"Compliance"},
		Summary: "Record a security assessment",
		Description: "Records a penetration test or other assessment (SEC-25), with its findings: each is due by its severity (7 days Critical, 30 High, 90 Medium, 180 Low, " +
			"from the day found). The report itself stays outside Fyniq: record its SHA-256 and where it is kept. Side effects: the assessment and its findings, the audit events " +
			"security_assessment.recorded and security_finding.recorded. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, writes, func(ctx context.Context, in *securityAssessmentInput) (*securityWrite[SecurityAssessmentView], error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (SecurityAssessmentView, error) {
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return SecurityAssessmentView{}, err
			}
			b := in.Body
			var bad []FieldProblem
			if !slices.Contains(assessmentKinds, b.Kind) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.kind", Message: "is not a kind of assessment", Fix: "Use " + strings.Join(assessmentKinds, ", ")})
			}
			started, e1 := time.Parse(time.DateOnly, b.StartedOn)
			ended, e2 := time.Parse(time.DateOnly, b.EndedOn)
			if e1 != nil || e2 != nil || ended.Before(started) || ended.After(today) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.ended_on", Message: "is not a day from the first day to today", Fix: "Send the first and last days, YYYY-MM-DD, the last not after today"})
			}
			if b.ReportSHA256 != "" && !reportHashHex.MatchString(b.ReportSHA256) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.report_sha256", Message: "is not a SHA-256", Fix: "Send 64 lower-case hex characters"})
			}
			a1 := storage.Assessment{Kind: b.Kind, Tester: strings.TrimSpace(b.Tester), Independent: b.Independent, Scope: strings.TrimSpace(b.Scope), StartedOn: started,
				EndedOn: ended, Summary: strings.TrimSpace(b.Summary), ReportSHA256: b.ReportSHA256, ReportLocation: strings.TrimSpace(b.ReportLocation),
				RecordedBy: uploader(ctx), RecordedAt: now}
			if a1.Tester == "" || a1.Scope == "" || a1.Summary == "" {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.tester", Message: "needs the tester, the scope and a summary", Fix: "Say who tested, what was covered and what it concluded"})
			}
			seen := map[string]bool{}
			for i, fi := range b.Findings {
				f, fb := findingOf(fi, ended, today, "body.findings["+strconv.Itoa(i)+"]", now, uploader(ctx))
				if seen[f.Ref] {
					fb = append(fb, FieldProblem{Code: CodeInvalid, Field: "body.findings[" + strconv.Itoa(i) + "].ref", Message: "is used twice", Fix: "Give each finding its own reference"})
				}
				seen[f.Ref] = true
				bad = append(bad, fb...)
				a1.Findings = append(a1.Findings, f)
			}
			if len(bad) > 0 {
				return SecurityAssessmentView{}, invalid(bad)
			}
			saved, err := u.Compliance().RecordAssessment(ctx, a1)
			return assessmentView(saved, today), err
		})
		if err != nil {
			return nil, err
		}
		return &securityWrite[SecurityAssessmentView]{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.AddSecurityFinding, huma.Operation{
		OperationID: "addSecurityFinding", Method: http.MethodPost, Path: "/v1/compliance/security-assessments/{assessment_id}/findings", Tags: []string{"Compliance"},
		Summary: "Add a finding to a security assessment",
		Description: "Adds a finding the assessment's tester reported later, such as at a retest (SEC-25): it is due by its severity from the day found. Side effects: the finding, " +
			"the audit event security_finding.recorded. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound}, writes...), func(ctx context.Context, in *securityFindingInput) (*securityWrite[SecurityFindingView], error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (SecurityFindingView, error) {
			id, err := parseID("assessment_id", in.AssessmentID)
			if err != nil {
				return SecurityFindingView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return SecurityFindingView{}, err
			}
			f, bad := findingOf(in.Body, today, today, "body", now, uploader(ctx))
			if len(bad) > 0 {
				return SecurityFindingView{}, invalid(bad)
			}
			saved, err := u.Compliance().AddFinding(ctx, id, f)
			if errors.Is(err, ports.ErrNotFound) {
				return SecurityFindingView{}, problem(CodeNotFound, "There is no assessment "+in.AssessmentID+".", "Use an assessment_id from listSecurityAssessments.")
			}
			return findingView(saved, today), err
		})
		if err != nil {
			return nil, err
		}
		return &securityWrite[SecurityFindingView]{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.StepSecurityFinding, huma.Operation{
		OperationID: "stepSecurityFinding", Method: http.MethodPost, Path: "/v1/compliance/security-findings/{finding_id}/steps", Tags: []string{"Compliance"},
		Summary: "Record the next step of a finding",
		Description: "Records that a finding was fixed, then retested; or that an Owner accepts it, with a reason, before it is retested (SEC-25). A step follows the finding's last, " +
			"and is dated no earlier than the finding or the step before it. Side effects: the step, the audit event security_finding.stepped. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append([]string{CodeNotFound}, writes...), func(ctx context.Context, in *securityStepInput) (*securityWrite[FindingStepView], error) {
		now := a.deps.Clock.Now()
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (FindingStepView, error) {
			id, err := parseID("finding_id", in.FindingID)
			if err != nil {
				return FindingStepView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return FindingStepView{}, err
			}
			var bad []FieldProblem
			if !slices.Contains(findingSteps, in.Body.Step) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.step", Message: "is not a step", Fix: "Use " + strings.Join(findingSteps, ", ")})
			}
			on, err := time.Parse(time.DateOnly, in.Body.On)
			if err != nil || on.After(today) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.on", Message: "is not a day up to today", Fix: "Send the day it happened, YYYY-MM-DD"})
			}
			reason := strings.TrimSpace(in.Body.Reason)
			if (in.Body.Step == "accepted") != (reason != "") || (in.Body.Step == "accepted" && len([]rune(reason)) < 3) {
				bad = append(bad, FieldProblem{Code: CodeInvalid, Field: "body.reason", Message: "is for an acceptance, and an acceptance needs one", Fix: "Say why the risk is taken when accepting; leave it out for a fix or a retest"})
			}
			if len(bad) > 0 {
				return FindingStepView{}, invalid(bad)
			}
			saved, err := u.Compliance().StepFinding(ctx, id, storage.FindingStep{Step: in.Body.Step, OnDate: on, Reason: reason, RecordedBy: uploader(ctx), RecordedAt: now})
			if errors.Is(err, ports.ErrNotFound) {
				return FindingStepView{}, problem(CodeNotFound, "There is no finding "+in.FindingID+".", "Use a finding_id from listSecurityAssessments.")
			}
			return findingStepView(saved), err
		})
		if err != nil {
			return nil, err
		}
		return &securityWrite[FindingStepView]{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}

func findingStepView(s storage.FindingStep) FindingStepView {
	return FindingStepView{Step: s.Step, On: s.OnDate.Format(time.DateOnly), Reason: s.Reason, RecordedBy: s.RecordedBy, RecordedAt: s.RecordedAt.UTC().Format(time.RFC3339)}
}
