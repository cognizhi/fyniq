package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/compliance"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ The control dashboard (CMP-01; p1-compliance-center D1, D9)

// CodeUnknownControl: no control of the registry has that ID.
const CodeUnknownControl = "COMPLIANCE-01"

// FrameworkKey is a framework the registry maps controls to.
type FrameworkKey string

// Schema implements huma.SchemaProvider: one of the registry's framework keys, with an example.
func (FrameworkKey) Schema(huma.Registry) *huma.Schema {
	var keys []any
	for _, f := range compliance.Frameworks {
		keys = append(keys, f.Key)
	}
	return &huma.Schema{Type: "string", Enum: keys, Description: "A framework's key, as Controls.frameworks lists them.", Examples: []any{"soc2"}}
}

// ControlCheck is how one frequency of a control is checked, and what its check last said.
type ControlCheck struct {
	Frequency string  `json:"frequency" enum:"CI,D,N,W,M,Q,S,E" doc:"CI on every change, daily, nightly, weekly, monthly, quarterly, semi-annually, or on an event." example:"N"`
	How       string  `json:"how,omitempty" doc:"What runs it: make <target>, test <Go test> or ci <job> in CI; job <platform job>; check <catalogue check>; verify <verification>; attestation <review>; tofu <module>/<run> for a check that runs in the cloud." example:"verify payments.dual-control"`
	Kind      string  `json:"kind" enum:"ci,job,check,verify,attestation,cloud,none" doc:"ci: in CI, read from the release the platform runs. job: a platform job. check: a check of the catalogue (Appendix C). verify: a verification compliance.run makes nightly. attestation: a review a person signs off. cloud: it runs only in the cloud. none: nothing runs it yet." example:"verify"`
	State     string  `json:"state" enum:"running,planned,manual,cloud" doc:"running: it runs here. planned: it arrives with a later change, named in arrives. manual: checked by hand. cloud: built, run only in the cloud." example:"running"`
	Arrives   string  `json:"arrives,omitempty" doc:"For a planned check, the change it arrives with." example:"p1-mcp-first-party"`
	Status    string  `json:"status" enum:"pass,fail,overdue,not_applicable,not_assessed" doc:"What it last said." example:"fail"`
	Detail    string  `json:"detail" doc:"In plain words: what passed, what it found, or what is missing." example:"1 found in 2 approval(s) and 1 release(s) of the last day: Approved with the signature of whoever prepared it or asked for it"`
	Evidence  *string `json:"evidence,omitempty" format:"uri" doc:"A link to its evidence, where there is one: a CI check's asset of the release the platform runs." example:"https://github.com/cognizhi/fyniq/releases/download/v1.2.0/test-report.zip"`
	Keeps     string  `json:"keeps,omitempty" doc:"What its evidence is and where it is kept, as the registry says." example:"the latest run for the organisation (fyniq.control_run)"`
}

// Control is a control of the registry (PRD-001 Appendix F) with its status for the organisation.
type Control struct {
	ID           string         `json:"id" doc:"The control's ID." example:"CTL-FIN-08"`
	Section      string         `json:"section" doc:"Its section of Appendix F." example:"F.1"`
	Text         string         `json:"control" doc:"What the control holds, as Appendix F words it, without the PRD's revision tags." example:"Payments and payee bank changes are dual-controlled"`
	Owner        string         `json:"owner" enum:"owner,administrator,controller,accountant,dpo,auditor" doc:"The role that answers for it, and that a notice of its failure goes to." example:"controller"`
	Frameworks   []FrameworkKey `json:"frameworks" nullable:"false" doc:"The frameworks it maps to, as frameworks names them."`
	Mapping      string         `json:"mapping" doc:"Its mapping to the frameworks, as Appendix F words it, without the PRD's revision tags." example:"COSO P8; SOC2 CC6.1"`
	Frequency    string         `json:"frequency" doc:"How often it is checked, as Appendix F says." example:"CI, N"`
	Status       string         `json:"status" enum:"pass,fail,overdue,not_applicable,not_assessed" doc:"The worst of its checks': fail, overdue, not assessed, pass; not applicable when none applies." example:"fail"`
	Detail       string         `json:"detail" doc:"In plain words, why: the worst check's words, or that each passed." example:"1 found in 2 approval(s) and 1 release(s) of the last day: Approved with the signature of whoever prepared it or asked for it"`
	FailingSince *string        `json:"failing_since,omitempty" format:"date-time" doc:"While it fails or is overdue, since when." example:"2027-01-02T20:35:00Z"`
	CheckedAt    *string        `json:"checked_at,omitempty" format:"date-time" doc:"When compliance.run last worked out its status; none before its first run." example:"2027-01-03T20:35:00Z"`
	Checks       []ControlCheck `json:"checks" nullable:"false" doc:"Each check, by frequency, with what it last said."`
}

// ControlFramework is a framework the registry maps controls to, and how many it holds.
type ControlFramework struct {
	Key      string `json:"key" doc:"Its key, for the framework filter." example:"soc2"`
	Title    string `json:"title" doc:"Its name." example:"SOC 2"`
	Controls int    `json:"controls" doc:"How many controls map to it." example:"34"`
}

// Controls is the control dashboard: every control the filters keep, the frameworks, and the release the platform runs.
type Controls struct {
	Items      []Control          `json:"items" nullable:"false" doc:"The controls the filters keep, in the registry's order."`
	Frameworks []ControlFramework `json:"frameworks" nullable:"false" doc:"Every framework of the registry, in the order the dashboard lists them."`
	Release    *string            `json:"release,omitempty" doc:"The release the platform runs, whose evidence its CI checks read; none in a development build, where they are not assessed." example:"v1.2.0"`
}

type controlsInput struct {
	Framework string `query:"framework" enum:"sg-statute,pdpa,iso27001,soc2,csa-ce,csa-ct,imda-ai,owasp,coso,isa" doc:"Only the controls mapped to this framework." example:"soc2"`
	Status    string `query:"status" enum:"pass,fail,overdue,not_applicable,not_assessed" doc:"Only the controls with this status." example:"fail"`
}

type controlPath struct {
	ControlID string `path:"control_id" pattern:"^CTL-[A-Z]+-[0-9]{2}$" doc:"The control's ID." example:"CTL-FIN-08"`
}

type controlsOutput struct{ Body Controls }
type controlOutput struct{ Body Control }

func registerControls(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.ListControls, exempt(huma.Operation{
		OperationID: "listControls", Method: http.MethodGet, Path: "/v1/compliance/controls", Tags: []string{"Compliance"},
		Summary: "List the controls and their status",
		Description: "Every control of the registry (PRD-001 Appendix F) with its status for the organisation (CMP-01): pass, fail, " +
			"overdue, not applicable, or not assessed naming what is missing; each check by frequency with what it last said and its " +
			"evidence; its owner role and the frameworks it maps to. A control's status is the worst of its checks', as compliance.run " +
			"last worked it out each night; a check that runs in CI reads the release the platform runs. Filter by framework or status. " +
			"Read-only; no side effects.",
	}, "cursor", "The registry's controls, under a hundred, are read whole: the dashboard and its framework counts need all of them."),
		append([]string{CodeInvalid}, locks...), func(ctx context.Context, in *controlsInput) (*controlsOutput, error) {
			list, err := a.controls(ctx)
			if err != nil {
				return nil, err
			}
			out := &controlsOutput{Body: list}
			out.Body.Items = slices.DeleteFunc(out.Body.Items, func(c Control) bool {
				return in.Framework != "" && !slices.Contains(c.Frameworks, FrameworkKey(in.Framework)) || in.Status != "" && c.Status != in.Status
			})
			return out, nil
		})

	register(a, authz.GetControl, exempt(huma.Operation{
		OperationID: "getControl", Method: http.MethodGet, Path: "/v1/compliance/controls/{control_id}", Tags: []string{"Compliance"},
		Summary:     "Get a control and its status",
		Description: "One control as listControls shows it. A control the registry does not hold is COMPLIANCE-01. Read-only; no side effects.",
	}, "cursor", "One control, not a list: its checks are a handful, one or more per frequency."),
		append([]string{CodeUnknownControl}, locks...), func(ctx context.Context, in *controlPath) (*controlOutput, error) {
			list, err := a.controls(ctx)
			if err != nil {
				return nil, err
			}
			i := slices.IndexFunc(list.Items, func(c Control) bool { return c.ID == in.ControlID })
			if i < 0 {
				return nil, problem(CodeUnknownControl, fmt.Sprintf("There is no control %s in the registry.", in.ControlID),
					"Use an ID from listControls, such as CTL-FIN-08.")
			}
			return &controlOutput{Body: list.Items[i]}, nil
		})
}

// controls are the registry's controls with the organisation's states.
func (a *API) controls(ctx context.Context) (Controls, error) {
	registry, err := compliance.Load()
	if err != nil {
		return Controls{}, err
	}
	p, _ := authn.From(ctx)
	var states []storage.ControlState
	if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		states, err = u.Compliance().States(ctx)
		return err
	}); err != nil {
		return Controls{}, err
	}
	byControl := map[string]storage.ControlState{}
	for _, s := range states {
		byControl[s.Control] = s
	}
	out := Controls{Items: []Control{}, Frameworks: []ControlFramework{}}
	if a.deps.Release.Tag != "" {
		tag := a.deps.Release.Tag
		out.Release = &tag
	}
	counts := map[string]int{}
	for _, c := range registry {
		item, err := controlOf(c, byControl[c.ID])
		if err != nil {
			return Controls{}, err
		}
		for _, f := range c.Frameworks {
			counts[f]++
		}
		out.Items = append(out.Items, item)
	}
	for _, f := range compliance.Frameworks {
		out.Frameworks = append(out.Frameworks, ControlFramework{Key: f.Key, Title: f.Title, Controls: counts[f.Key]})
	}
	return out, nil
}

// revTag is a PRD revision tag in Appendix F's words, such as "(rev 0.8)" or "(rev 0.246, the owner's decision of …)":
// the PRD's history, which the dashboard leaves out.
var revTag = regexp.MustCompile(`\s*\(rev \d+\.\d+(?:[,:][^)]*)?\)\s*`)

// untagged is Appendix F's words without their revision tags.
func untagged(s string) string { return strings.TrimSpace(revTag.ReplaceAllString(s, " ")) }

// controlOf is a control of the registry with its state; before its first run, each check is not assessed.
func controlOf(c compliance.Control, s storage.ControlState) (Control, error) {
	owner := map[string]string{"Owner": "owner", "Administrator": "administrator", "Controller": "controller", "Accountant": "accountant",
		"DPO": "dpo", "Auditor": "auditor"}[c.Owner]
	out := Control{ID: c.ID, Section: c.Section, Text: untagged(c.Control), Owner: owner, Frameworks: []FrameworkKey{}, Mapping: untagged(c.Mapping),
		Frequency: c.Frequency, Status: "not_assessed", Detail: "Not worked out yet: compliance.run works out each control's status every night.",
		Checks: []ControlCheck{}}
	for _, f := range c.Frameworks {
		out.Frameworks = append(out.Frameworks, FrameworkKey(f))
	}
	var saved []struct {
		Frequency, How, Status, Detail, Evidence string
	}
	if s.Control != "" {
		out.Status, out.Detail = s.Status, s.Detail
		at := s.RunAt.UTC().Format(time.RFC3339)
		out.CheckedAt = &at
		if s.FailingSince != nil {
			since := s.FailingSince.UTC().Format(time.RFC3339)
			out.FailingSince = &since
		}
		if err := json.Unmarshal(s.Checks, &saved); err != nil {
			return Control{}, err
		}
	}
	for _, ch := range c.Checks {
		kind, _ := ch.Kind()
		k := map[string]string{compliance.KindMake: "ci", compliance.KindTest: "ci", compliance.KindCI: "ci", compliance.KindJob: "job",
			compliance.KindCheck: "check", compliance.KindVerify: "verify", compliance.KindAttestation: "attestation"}[kind]
		switch {
		case ch.Status == "cloud":
			k = "cloud"
		case k == "" || ch.Status != "running":
			k = "none"
		}
		cc := ControlCheck{Frequency: ch.Frequency, How: ch.How, Kind: k, State: ch.Status, Arrives: ch.Arrives, Keeps: ch.Evidence,
			Status: "not_assessed", Detail: "Not worked out yet."}
		if i := slices.IndexFunc(saved, func(x struct{ Frequency, How, Status, Detail, Evidence string }) bool {
			return x.Frequency == ch.Frequency && x.How == ch.How
		}); i >= 0 {
			cc.Status, cc.Detail = saved[i].Status, saved[i].Detail
			if saved[i].Evidence != "" {
				ev := saved[i].Evidence
				cc.Evidence = &ev
			}
		}
		out.Checks = append(out.Checks, cc)
	}
	return out, nil
}
