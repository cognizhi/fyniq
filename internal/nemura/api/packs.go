package api

import (
	"context"
	"errors"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/nemura/policy"
)

// CodePolicyBelowStatute is the stable code of a policy that gives less than the statute (NM-PACK-04).
const CodePolicyBelowStatute = policy.CodeBelowStatute

// PackSource is an official page a pack value comes from.
type PackSource struct {
	ID        string `json:"id" doc:"The source's id in the pack." example:"mom-annual-leave"`
	Title     string `json:"title" doc:"The page's title." example:"MOM: Annual leave, eligibility and entitlement"`
	URL       string `json:"url" format:"uri" doc:"The official page." example:"https://www.mom.gov.sg/employment-practices/leave/annual-leave/eligibility-and-entitlement"`
	Section   string `json:"section" doc:"The section the values come from." example:"Eligibility and entitlement"`
	Retrieved string `json:"retrieved" format:"date" doc:"The day the page was read." example:"2026-11-19"`
	Quote     string `json:"quote" doc:"What the page says, as read." example:"Within 7 days after the end of the salary period."`
}

// PackValue is a value that is only researched, and whether the company decided to use it.
type PackValue struct {
	Table   string `json:"table" doc:"The table of the pack." example:"annual_leave"`
	Key     string `json:"key" doc:"The row's key in the table." example:"1"`
	Status  string `json:"status" enum:"researched,verified" doc:"Researched: read from the official page. Verified: checked by a second person on the publisher's side." example:"researched"`
	Decided bool   `json:"decided" doc:"Whether the company recorded a decision to use it." example:"false"`
}

// PackVersion is a version of a pack with what the screen shows of it.
type PackVersion struct {
	ID            string       `json:"id" doc:"The version." example:"sg-people@2026.1"`
	Title         string       `json:"title" doc:"What it holds." example:"Singapore people facts"`
	EffectiveFrom string       `json:"effective_from" format:"date" doc:"Its first day." example:"2026-01-01"`
	ReviewDate    string       `json:"review_date" format:"date" doc:"The day its sources must be checked again." example:"2027-01-31"`
	Verifier      string       `json:"verifier" doc:"Who verified it, or that no one has yet." example:"none yet"`
	Accepted      bool         `json:"accepted" doc:"Whether this organisation accepted it; accepting verifies nothing." example:"false"`
	Researched    int          `json:"researched_values" doc:"How many of its values are only researched." example:"52"`
	Values        []PackValue  `json:"values" doc:"The values that are only researched, with the company's decisions."`
	Sources       []PackSource `json:"sources" doc:"The official pages."`
}

// Finding is one thing a check found.
type Finding struct {
	Check   int    `json:"check" doc:"The row of Appendix C N.1." example:"3"`
	Area    string `json:"area" doc:"People, Packs or Leave." example:"People"`
	Status  string `json:"status" enum:"fail,warn,due" doc:"Fail blocks, warn needs a look, due is coming up." example:"fail"`
	Subject string `json:"subject" doc:"What it is about: a person's ID or a pack version." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Message string `json:"message" doc:"What is wrong and how to fix it, in plain words." example:"A work pass expires on 2026-12-20: record the renewed pass or the leaving date."`
}

// Packs reads the packs, accepts a version, records decisions, sets the company's policy and runs the checks.
type Packs interface {
	Versions(ctx context.Context, s Session) ([]PackVersion, error)
	Accept(ctx context.Context, s Session, version string) error
	Decide(ctx context.Context, s Session, version, table, key, reason string) error
	SetPolicy(ctx context.Context, s Session, from time.Time, r policy.Rules) (notes []string, err error)
	Policy(ctx context.Context, s Session, on time.Time) (policy.Rules, string, bool, error)
	Checks(ctx context.Context, s Session, on time.Time) ([]checks.Finding, error)
}

func init() {
}

type packsInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}

type packsOutput struct {
	Body struct {
		Packs []PackVersion `json:"packs" doc:"Every version of Nemura's packs."`
	}
}

type acceptInput struct {
	Cookie  string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Version string `path:"version" doc:"The pack version." example:"sg-people@2026.1"`
}

type decideInput struct {
	Cookie  string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Version string `path:"version" doc:"The pack version." example:"sg-people@2026.1"`
	Body    struct {
		Table  string `json:"table" doc:"The table of the pack." example:"annual_leave"`
		Key    string `json:"key" doc:"The row's key." example:"1"`
		Reason string `json:"reason" minLength:"10" maxLength:"2000" doc:"Why the company uses the researched value." example:"We read MOM's page and it matches our contracts."`
	}
}

// PolicyRules is a company's leave policy as the API shows and takes it.
type PolicyRules struct {
	AnnualLeaveDays         []int `json:"annual_leave_days" minItems:"1" maxItems:"60" doc:"Paid days a year by year of service from the first; the last repeats." example:"[14]"`
	SickOutpatientDays      []int `json:"sick_outpatient_days" minItems:"4" maxItems:"4" doc:"Outpatient sick leave days at 3, 4, 5 and 6 or more months of service." example:"[5,8,11,14]"`
	SickHospitalDays        []int `json:"sick_hospital_days" minItems:"4" maxItems:"4" doc:"Hospitalisation leave days at 3, 4, 5 and 6 or more months of service." example:"[15,30,45,60]"`
	ChildcareCitizenDays    int   `json:"childcare_citizen_days" minimum:"0" doc:"Childcare leave days a year for a citizen child under 7." example:"6"`
	ExtendedChildcareDays   int   `json:"extended_childcare_days" minimum:"0" doc:"Extended childcare days a year for a citizen child aged 7 to 12." example:"2"`
	ChildcareNonCitizenDays int   `json:"childcare_non_citizen_days" minimum:"0" doc:"Childcare leave days a year for a child who is not a citizen." example:"2"`
}

type setPolicyInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		From  string      `json:"effective_from" format:"date" doc:"The day the policy starts; the one in force ends that day." example:"2026-12-01"`
		Rules PolicyRules `json:"rules" doc:"The company's entitlements."`
	}
}

type setPolicyOutput struct {
	Body struct {
		Notes []string `json:"notes" doc:"Rules where the policy gives more than the statute."`
	}
}

type policyInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	AsOf   string `query:"as_of" format:"date" required:"false" doc:"The day to show the policy on; today when empty." example:"2026-12-01"`
}

type policyOutput struct {
	Body struct {
		InForce     bool        `json:"in_force" doc:"Whether the company has a policy on the day." example:"true"`
		PackVersion string      `json:"pack_version,omitempty" doc:"The pack version it was checked against." example:"sg-people@2026.1"`
		Rules       PolicyRules `json:"rules" doc:"The entitlements."`
	}
}

type checksInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	AsOf   string `query:"as_of" format:"date" required:"false" doc:"The day to check on; today when empty." example:"2026-12-01"`
}

type checksOutput struct {
	Body struct {
		Findings []Finding `json:"findings" doc:"What the people checks found, ✖ first."`
	}
}

func packError(err error) error {
	var r *people.Refusal
	if errors.As(err, &r) {
		return Err(r.Code, r.Message)
	}
	return huma.Error500InternalServerError("The pack or policy could not be handled.")
}

func (a *API) registerPacks() {
	viewer := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return Session{}, err
		}
		ok, err := a.featureRights(ctx, s, authz.View, authz.Create, authz.Approve)
		if err != nil {
			return Session{}, err
		}
		if !ok {
			return Session{}, Err(CodeRoleRequired, "Only the people team and the Owner see packs, policies and checks.")
		}
		return s, nil
	}
	writer := func(ctx context.Context, cookie string) (Session, error) {
		s, err := a.signedIn(ctx, cookie)
		if err != nil {
			return Session{}, err
		}
		ok, err := a.featureRights(ctx, s, authz.Create, authz.Approve)
		if err != nil {
			return Session{}, err
		}
		if !ok {
			return Session{}, Err(CodeRoleRequired, "Only the HR administrator and the Owner change packs and policies.")
		}
		return s, nil
	}
	huma.Register(a.API, huma.Operation{
		OperationID: "listPacks", Method: "GET", Path: "/v1/packs", Tags: []string{"Packs"}, Summary: "The country packs",
		Description: "Lists each version of Nemura's packs with its sources (official page, section, day read and quote), who verified it, and the values that are only researched with the company's decisions on them. Whoever sees the settings. It changes nothing.",
		Errors:      []int{401, 403},
	}, func(ctx context.Context, in *packsInput) (*packsOutput, error) {
		s, err := viewer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Packs.Versions(ctx, s)
		if err != nil {
			return nil, packError(err)
		}
		out := &packsOutput{}
		out.Body.Packs = list
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "acceptPack", Method: "POST", Path: "/v1/packs/{version}/acceptances", Tags: []string{"Packs"}, Summary: "Accept a pack version",
		Description: "Records that the organisation accepts a version of a pack. Accepting verifies nothing: the pack's publisher does (NM-PACK-07). The HR administrator or the Owner. It is audited.",
		Errors:      []int{401, 403, 404, 409}, DefaultStatus: 201,
	}, func(ctx context.Context, in *acceptInput) (*struct{}, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		return nil, packErrOrNil(a.d.Packs.Accept(ctx, s, in.Version))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "decidePackValue", Method: "POST", Path: "/v1/packs/{version}/decisions", Tags: []string{"Packs"}, Summary: "Decide to use a researched value",
		Description: "Records the company's decision, with its reason, to use a value that is only researched, so a calculation may read it (NM-PACK-07). A verified value needs none. The HR administrator or the Owner. It is audited.",
		Errors:      []int{401, 403, 404, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *decideInput) (*struct{}, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		return nil, packErrOrNil(a.d.Packs.Decide(ctx, s, in.Version, in.Body.Table, in.Body.Key, in.Body.Reason))
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "setPolicy", Method: "PUT", Path: "/v1/policy", Tags: []string{"Packs"}, Summary: "Set the company's leave policy",
		Description: "Starts the company's leave policy on a day, ending the one in force. It is checked rule by rule against the statute of the pack in force: a policy that gives less on any rule is refused (NM_POLICY_BELOW_STATUTE) with every rule it breaks; one that gives more says so. The HR administrator or the Owner. It is audited.",
		Errors:      []int{401, 403, 409, 422},
	}, func(ctx context.Context, in *setPolicyInput) (*setPolicyOutput, error) {
		s, err := writer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		from, err := date("effective_from", in.Body.From, true)
		if err != nil {
			return nil, err
		}
		r := in.Body.Rules
		notes, err := a.d.Packs.SetPolicy(ctx, s, *from, policy.Rules{AnnualLeaveDays: r.AnnualLeaveDays, SickOutpatientDays: r.SickOutpatientDays, SickHospitalDays: r.SickHospitalDays,
			ChildcareCitizenDays: r.ChildcareCitizenDays, ExtendedChildcareDays: r.ExtendedChildcareDays, ChildcareNonCitizenDays: r.ChildcareNonCitizenDays})
		if err != nil {
			return nil, packError(err)
		}
		out := &setPolicyOutput{}
		out.Body.Notes = notes
		if notes == nil {
			out.Body.Notes = []string{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "getPolicy", Method: "GET", Path: "/v1/policy", Tags: []string{"Packs"}, Summary: "The company's leave policy",
		Description: "Returns the company's leave policy in force on a day (today when none is given) and the pack version it was checked against. Whoever sees the settings. It changes nothing.",
		Errors:      []int{401, 403, 422},
	}, func(ctx context.Context, in *policyInput) (*policyOutput, error) {
		s, err := viewer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		on, err := a.asOf(in.AsOf)
		if err != nil {
			return nil, err
		}
		r, version, found, err := a.d.Packs.Policy(ctx, s, on)
		if err != nil {
			return nil, packError(err)
		}
		out := &policyOutput{}
		out.Body.InForce, out.Body.PackVersion = found, version
		out.Body.Rules = PolicyRules{AnnualLeaveDays: r.AnnualLeaveDays, SickOutpatientDays: r.SickOutpatientDays, SickHospitalDays: r.SickHospitalDays,
			ChildcareCitizenDays: r.ChildcareCitizenDays, ExtendedChildcareDays: r.ExtendedChildcareDays, ChildcareNonCitizenDays: r.ChildcareNonCitizenDays}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listChecks", Method: "GET", Path: "/v1/checks", Tags: []string{"Packs"}, Summary: "The people checks",
		Description: "Runs the people checks that need only the people record and the packs on a day (today when none is given): a work pass about to expire or expired with no renewal or leaving date (N.1 #3), next year's public-holiday calendar missing from 1 December (#8), and a policy that needs a researched value with no recorded decision (#13). Whoever sees the settings. It changes nothing.",
		Errors:      []int{401, 403, 422},
	}, func(ctx context.Context, in *checksInput) (*checksOutput, error) {
		s, err := viewer(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		on, err := a.asOf(in.AsOf)
		if err != nil {
			return nil, err
		}
		list, err := a.d.Packs.Checks(ctx, s, on)
		if err != nil {
			return nil, packError(err)
		}
		out := &checksOutput{}
		out.Body.Findings = []Finding{}
		for _, f := range list {
			out.Body.Findings = append(out.Body.Findings, Finding{Check: f.Check, Area: f.Area, Status: f.Status, Subject: f.Subject, Message: f.Message})
		}
		return out, nil
	})
}

func packErrOrNil(err error) error {
	if err == nil {
		return nil
	}
	return packError(err)
}
