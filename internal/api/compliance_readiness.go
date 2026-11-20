package api

import (
	"context"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ReadinessGap is a control that is not passing, in plain words.
type ReadinessGap struct {
	ID     string `json:"id" doc:"The control's ID." example:"CTL-RES-04"`
	Text   string `json:"control" doc:"What the control holds." example:"Security Command Center high and critical findings fixed within SLA"`
	Status string `json:"status" enum:"fail,overdue,not_assessed" doc:"Why it is a gap: it fails, is overdue, or is not assessed yet." example:"not_assessed"`
	Detail string `json:"detail" doc:"In plain words, why." example:"The verification has not run for this organisation yet."`
}

// FrameworkReadiness is how far a framework's mapped controls hold (CMP-08).
type FrameworkReadiness struct {
	Key           string         `json:"key" doc:"The framework's key, as the controls dashboard's filter has it." example:"csa-ct"`
	Title         string         `json:"title" doc:"Its name." example:"CSA Cyber Trust"`
	Stage         string         `json:"stage" doc:"Where it sits in PRD-001's roadmap of assurance (section 14.8)." example:"Stage 1, with the first employees"`
	Controls      int            `json:"controls" doc:"How many controls map to it." example:"34"`
	Pass          int            `json:"pass" doc:"Of them, those that pass." example:"20"`
	Fail          int            `json:"fail" doc:"Those that fail." example:"1"`
	Overdue       int            `json:"overdue" doc:"Those that are overdue." example:"2"`
	NotAssessed   int            `json:"not_assessed" doc:"Those not assessed yet: a required input is missing." example:"3"`
	NotApplicable int            `json:"not_applicable" doc:"Those that do not apply to this organisation." example:"4"`
	Planned       int            `json:"planned" doc:"Those with a check that arrives with a later change." example:"5"`
	Gaps          []ReadinessGap `json:"gaps" nullable:"false" doc:"The controls that are not passing, with the worst first."`
}

// AnnexANumber is a control number of ISO/IEC 27001:2022 Annex A, such as 5.15 or 8.34.
type AnnexANumber string

// Schema documents the number.
func (AnnexANumber) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Pattern: `^[5-8]\.[0-9]{1,2}$`, Description: "A control number of Annex A: 5.1 to 5.37, 6.1 to 6.8, 7.1 to 7.14 or 8.1 to 8.34.", Examples: []any{"5.15"}}
}

// AnnexAReadiness is how many of ISO/IEC 27001:2022 Annex A's 93 control numbers a control of the registry maps to.
type AnnexAReadiness struct {
	Total     int            `json:"total" doc:"How many numbers Annex A has." example:"93"`
	Covered   []AnnexANumber `json:"covered" nullable:"false" doc:"The numbers some control's mapping names."`
	Uncovered []AnnexANumber `json:"uncovered" nullable:"false" doc:"The numbers no control maps to: gaps for the statement of applicability, whose wording waits for the certification body."`
}

// Readiness is the readiness of each framework, and Annex A's clause numbers without a control (CMP-08). It is a view of how far the registry
// goes, never a certificate: nothing in it says Fyniq is certified.
type Readiness struct {
	Frameworks []FrameworkReadiness `json:"frameworks" nullable:"false" doc:"Every framework of the registry, in the order the dashboard lists them."`
	AnnexA     AnnexAReadiness      `json:"annex_a" doc:"ISO/IEC 27001:2022 Annex A's control numbers, covered and not."`
	Notice     string               `json:"notice" doc:"What this is not." example:"Readiness, not a certificate: nothing here says Fyniq is certified by any body."`
}

type readinessOutput struct{ Body Readiness }

func registerReadiness(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetReadiness, exempt(huma.Operation{
		OperationID: "getReadiness", Method: http.MethodGet, Path: "/v1/compliance/readiness", Tags: []string{"Compliance"},
		Summary: "Get the readiness of each framework",
		Description: "For each framework of the registry (CMP-08), its stage, how many mapped controls pass, fail, are overdue, are not assessed or do not apply, how many have a " +
			"check still to arrive, and the controls that are not passing in plain words; and for ISO/IEC 27001:2022 Annex A the control numbers no control maps to. Readiness, " +
			"never a certificate. Read-only; no side effects.",
	}, "cursor", "Ten frameworks and 93 Annex A numbers, read whole."), locks,
		func(ctx context.Context, _ *struct{}) (*readinessOutput, error) {
			list, err := a.controls(ctx)
			if err != nil {
				return nil, err
			}
			regs, err := compliance.Load()
			if err != nil {
				return nil, err
			}
			out := &readinessOutput{Body: readinessOf(list, regs)}
			return out, nil
		})
}

// readinessOf works the readiness out of the dashboard's controls and the registry.
func readinessOf(list Controls, regs []compliance.Control) Readiness {
	planned := map[string]bool{}
	covered := map[string]bool{}
	for _, c := range regs {
		for _, ch := range c.Checks {
			if ch.Status == "planned" {
				planned[c.ID] = true
			}
		}
		for _, n := range compliance.ISOClauses(c.Mapping) {
			covered[n] = true
		}
	}
	out := Readiness{Frameworks: []FrameworkReadiness{}, Notice: "Readiness, not a certificate: nothing here says Fyniq is certified by any body."}
	for _, f := range compliance.Frameworks {
		fr := FrameworkReadiness{Key: f.Key, Title: f.Title, Stage: compliance.FrameworkStages[f.Key], Gaps: []ReadinessGap{}}
		for _, c := range list.Items {
			if !slices.Contains(c.Frameworks, FrameworkKey(f.Key)) {
				continue
			}
			fr.Controls++
			if planned[c.ID] {
				fr.Planned++
			}
			switch c.Status {
			case "pass":
				fr.Pass++
			case "not_applicable":
				fr.NotApplicable++
			case "fail":
				fr.Fail++
			case "overdue":
				fr.Overdue++
			default:
				fr.NotAssessed++
			}
			if c.Status == "fail" || c.Status == "overdue" || c.Status == "not_assessed" {
				fr.Gaps = append(fr.Gaps, ReadinessGap{ID: c.ID, Text: c.Text, Status: c.Status, Detail: c.Detail})
			}
		}
		rank := map[string]int{"fail": 0, "overdue": 1, "not_assessed": 2}
		slices.SortStableFunc(fr.Gaps, func(x, y ReadinessGap) int { return rank[x.Status] - rank[y.Status] })
		out.Frameworks = append(out.Frameworks, fr)
	}
	out.AnnexA = AnnexAReadiness{Total: len(compliance.AnnexA), Covered: []AnnexANumber{}, Uncovered: []AnnexANumber{}}
	for _, n := range compliance.AnnexA {
		if covered[n] {
			out.AnnexA.Covered = append(out.AnnexA.Covered, AnnexANumber(n))
		} else {
			out.AnnexA.Uncovered = append(out.AnnexA.Uncovered, AnnexANumber(n))
		}
	}
	return out
}
