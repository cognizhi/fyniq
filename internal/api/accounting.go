package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/accounting"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ accounting framework and policies (FR-ORG-10; rev 0.62)
//
// The framework and its edition, and each accounting policy's words, are versions effective from the start of a
// financial year. A member who holds the Accountant role confirms the framework and approves each policy, never the
// person who wrote that version (the founders' decision of 21 Oct 2026). A year's statements are "management statements"
// until its framework is confirmed and every policy in force for it is approved (G4-017); the reports read the title and
// basis from here.

// AccountingFrameworkInput records a version of the accounting framework.
type AccountingFrameworkInput struct {
	Framework string `json:"framework" enum:"sfrs_se,frs,sfrs_i" doc:"sfrs_se is SFRS for Small Entities, the recommended one (open question 31); frs is FRS; sfrs_i is SFRS(I)." example:"sfrs_se"`
	Edition   string `json:"edition" minLength:"1" maxLength:"120" doc:"The edition, as the standard names it. SFRS for Small Entities' Third Edition applies to financial years beginning on or after 1 January 2027." example:"Third Edition"`
	FromOn    string `json:"from_on" format:"date" doc:"The first day of the financial year it applies from, until a later version replaces it; at most a year after the current one." example:"2027-01-01"`
}

// AccountingFramework is a recorded version of the accounting framework (FR-ORG-10).
type AccountingFramework struct {
	Version     int        `json:"version" doc:"The framework version: 1, 2, …" example:"2"`
	FromOn      string     `json:"from_on" format:"date" doc:"The first day of the financial year it applies from." example:"2027-01-01"`
	Framework   string     `json:"framework" enum:"sfrs_se,frs,sfrs_i" doc:"The framework's code." example:"sfrs_se"`
	Name        string     `json:"name" doc:"The framework's name, as statements show it." example:"SFRS for Small Entities"`
	Edition     string     `json:"edition" doc:"Its edition." example:"Third Edition"`
	WrittenBy   *Person    `json:"written_by,omitempty" doc:"Who recorded it: they never confirm it."`
	WrittenAt   time.Time  `json:"written_at" doc:"When it was recorded." example:"2026-10-21T09:30:00Z"`
	ConfirmedBy *Person    `json:"confirmed_by,omitempty" doc:"The accountant who confirmed the company may use it; absent until then."`
	ConfirmedAt *time.Time `json:"confirmed_at,omitempty" doc:"When they confirmed it." example:"2026-10-22T03:00:00Z"`
	YouMaySign  bool       `json:"you_may_confirm" doc:"true when the caller may confirm it: they hold the Accountant role, did not record it, and it is not confirmed or replaced." example:"false"`
}

// AccountingPolicyInput records a version of one accounting policy.
type AccountingPolicyInput struct {
	Wording string `json:"wording" minLength:"1" maxLength:"4000" doc:"The policy, as the accountant decided it; where the area does not apply to the company, say so." example:"Subscription revenue is recognised evenly over each billing period."`
	FromOn  string `json:"from_on" format:"date" doc:"The first day of the financial year it applies from, until a later version replaces it; at most a year after the current one." example:"2026-01-01"`
}

// AccountingPolicy is a recorded version of one accounting policy (FR-ORG-10).
type AccountingPolicy struct {
	Area       string     `json:"area" doc:"The policy's area." example:"revenue"`
	Version    int        `json:"version" doc:"The area's policy version: 1, 2, …" example:"1"`
	FromOn     string     `json:"from_on" format:"date" doc:"The first day of the financial year it applies from." example:"2026-01-01"`
	Wording    string     `json:"wording" doc:"The policy." example:"Subscription revenue is recognised evenly over each billing period."`
	WrittenBy  *Person    `json:"written_by,omitempty" doc:"Who recorded it: they never approve it."`
	WrittenAt  time.Time  `json:"written_at" doc:"When it was recorded." example:"2026-10-21T09:30:00Z"`
	ApprovedBy *Person    `json:"approved_by,omitempty" doc:"The accountant who approved it; absent until then."`
	ApprovedAt *time.Time `json:"approved_at,omitempty" doc:"When they approved it." example:"2026-10-22T03:00:00Z"`
	YouMaySign bool       `json:"you_may_approve" doc:"true when the caller may approve it: they hold the Accountant role, did not record it, and it is not approved or replaced." example:"true"`
}

// AccountingPolicyArea is one accounting policy FR-ORG-10 asks for, with the version in force for the year.
type AccountingPolicyArea struct {
	Area    string            `json:"area" enum:"revenue,government_grants,leases,development_costs,impairment,safes_and_notes,conversion_measurement,capitalisation_threshold,untaken_leave" doc:"The area's key." example:"revenue"`
	Title   string            `json:"title" doc:"Its name." example:"Revenue"`
	Decides string            `json:"decides" doc:"What the policy decides; Fyniq never writes its words." example:"When revenue is recognised, including subscriptions and fixed-fee projects."`
	Setting string            `json:"setting,omitempty" doc:"The organisation setting that holds the figure the policy approves, when one does." example:"assets.capitalisation_threshold"`
	Policy  *AccountingPolicy `json:"policy,omitempty" doc:"The version in force for the year; absent while none is recorded."`
}

// AccountingCheck is one thing that keeps a year's statements "management statements".
type AccountingCheck struct {
	Code   string `json:"code" doc:"ACCOUNTING-NO-FRAMEWORK, ACCOUNTING-UNCONFIRMED, ACCOUNTING-EDITION, ACCOUNTING-NO-POLICY or ACCOUNTING-UNAPPROVED." example:"ACCOUNTING-UNAPPROVED"`
	Area   string `json:"area,omitempty" doc:"The policy's area; absent for the framework." example:"revenue"`
	Title  string `json:"title" doc:"What is missing." example:"Revenue: not approved"`
	Detail string `json:"detail" doc:"What it is, in a sentence." example:"Version 1 of the revenue policy waits for the accountant's approval."`
	Fix    string `json:"fix" doc:"What to do." example:"A member who holds the Accountant role, other than its writer, approves it"`
}

// AccountingYear is a financial year's accounting framework and policies, and what its statements are titled.
type AccountingYear struct {
	StartsOn             string                 `json:"starts_on" format:"date" doc:"The financial year's first day." example:"2026-01-01"`
	EndsOn               string                 `json:"ends_on" format:"date" doc:"Its last day." example:"2026-12-31"`
	Status               string                 `json:"status" enum:"approved,not_approved" doc:"approved once the framework in force is confirmed and every policy in force is approved." example:"not_approved"`
	StatementTitle       string                 `json:"statement_title" enum:"Management statements,Financial statements" doc:"How the year's statements are titled (G4-017)." example:"Management statements"`
	StatementBasis       string                 `json:"statement_basis,omitempty" doc:"What they are prepared under, once approved; absent until then, so management statements name no framework." example:"Prepared under SFRS for Small Entities (Third Edition)"`
	RecommendedFramework string                 `json:"recommended_framework" doc:"The framework Fyniq assumes until the accountant decides (open question 31)." example:"sfrs_se"`
	RecommendedEdition   string                 `json:"recommended_edition" doc:"The edition of SFRS for Small Entities that applies to the year." example:"Third Edition"`
	Framework            *AccountingFramework   `json:"framework,omitempty" doc:"The framework version in force for the year; absent while none is recorded."`
	Policies             []AccountingPolicyArea `json:"policies" nullable:"false" doc:"Each policy FR-ORG-10 asks for, in its order, with the version in force."`
	Checks               []AccountingCheck      `json:"checks" nullable:"false" doc:"What keeps the statements management statements; empty once approved."`
}

// AccountingVersionPath names a framework version.
type AccountingVersionPath struct {
	Version int `path:"version" minimum:"1" doc:"The framework version." example:"2"`
}

// AccountingAreaPath names an accounting policy's area.
type AccountingAreaPath struct {
	Area string `path:"area" enum:"revenue,government_grants,leases,development_costs,impairment,safes_and_notes,conversion_measurement,capitalisation_threshold,untaken_leave" doc:"The policy's area." example:"revenue"`
}

type accountingGetInput struct{ OnQuery }

type accountingGetOutput struct{ Body AccountingYear }

type accountingFrameworkInput struct {
	KeyHeader
	PreferHeader
	Body AccountingFrameworkInput
}

type accountingConfirmInput struct {
	AccountingVersionPath
	KeyHeader
	PreferHeader
}

type accountingPolicyInput struct {
	AccountingAreaPath
	KeyHeader
	PreferHeader
	Body AccountingPolicyInput
}

type accountingApproveInput struct {
	AccountingAreaPath
	AccountingVersionPath
	KeyHeader
	PreferHeader
}

type accountingFrameworkOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     AccountingFramework
}

type accountingPolicyOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     AccountingPolicy
}

// AccountingCodes are the accounting framework's and policies' refusals.
var AccountingCodes = []string{accounting.CodeInput, accounting.CodeOwn, accounting.CodeSigned, storage.CodeAccounting, period.CodeNoYear}

// signer is the caller as the audit trail names them, which a version's writer is compared with.
func signer(ctx context.Context) string { return uploader(ctx) }

// maySign says whether the caller holds the right to confirm or approve under policy (the Accountant role).
func maySign(ctx context.Context, policy authz.Policy) bool {
	p, _ := authn.From(ctx)
	return authz.Decide(policy, p.Roles, p.AI()).Allowed
}

func accountingFrameworkOf(ctx context.Context, all []storage.FrameworkVersion, f storage.FrameworkVersion) AccountingFramework {
	out := AccountingFramework{Version: f.Version, FromOn: f.FromOn.Format(time.DateOnly), Framework: f.Framework,
		Name: accounting.FrameworkNames[f.Framework], Edition: f.Edition, WrittenBy: personOf(f.WrittenBy), WrittenAt: f.WrittenAt.UTC()}
	if f.ConfirmedBy != nil {
		out.ConfirmedBy = personOf(*f.ConfirmedBy)
		at := f.ConfirmedAt.UTC()
		out.ConfirmedAt = &at
	}
	out.YouMaySign = maySign(ctx, authz.ConfirmAccountingFramework) &&
		accounting.MaySign("framework", f.WrittenBy, signer(ctx), f.ConfirmedBy, accounting.FrameworkReplaced(all, f)) == nil
	return out
}

func accountingPolicyOf(ctx context.Context, all []storage.PolicyVersion, p storage.PolicyVersion) AccountingPolicy {
	out := AccountingPolicy{Area: p.Area, Version: p.Version, FromOn: p.FromOn.Format(time.DateOnly), Wording: p.Wording,
		WrittenBy: personOf(p.WrittenBy), WrittenAt: p.WrittenAt.UTC()}
	if p.ApprovedBy != nil {
		out.ApprovedBy = personOf(*p.ApprovedBy)
		at := p.ApprovedAt.UTC()
		out.ApprovedAt = &at
	}
	out.YouMaySign = maySign(ctx, authz.ApproveAccountingPolicy) &&
		accounting.MaySign("policy", p.WrittenBy, signer(ctx), p.ApprovedBy, accounting.PolicyReplaced(all, p)) == nil
	return out
}

// signingStepUp refuses a confirmation or an approval without step-up in the last 5 minutes while the organisation's
// accounting.signing_step_up setting is on, as it is by default (the founders' decision of rev 0.63). Off, it asks
// nothing more than the Accountant role and not being the writer.
func (a *API) signingStepUp(ctx context.Context, u storage.UnitOfWork, what string) error {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return err
	}
	d, _ := settings.Lookup("accounting.signing_step_up")
	changes, err := u.Settings().Changes(ctx, d.Key)
	if err != nil {
		return err
	}
	if settings.On(d, changes, today).Value != "true" {
		return nil
	}
	if p, _ := authn.From(ctx); !p.SteppedUp(a.deps.Clock.Now()) {
		return problem(CodeStepUpRequired, what+" needs step-up authentication in the last 5 minutes (FR-ORG-10).",
			"Confirm it's you with your passkey or authenticator app, then try again.")
	}
	return nil
}

// calendar is the organisation's financial years, and its today.
func (a *API) calendar(ctx context.Context, u storage.UnitOfWork) (period.Calendar, time.Time, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return period.Calendar{}, time.Time{}, err
	}
	cal, err := period.CalendarOf(o.Profile)
	if err != nil {
		return period.Calendar{}, time.Time{}, err
	}
	today, err := orgToday(ctx, a, u)
	return cal, today, err
}

// fromDay reads a from_on field.
func fromDay(s string) (time.Time, error) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return d, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.from_on", Message: "is not a date", Fix: "Send the date as YYYY-MM-DD"}})
	}
	return d, nil
}

func refused(ps []accounting.Problem) error {
	if len(ps) == 0 {
		return nil
	}
	fields := make([]FieldProblem, 0, len(ps))
	for _, x := range ps {
		fields = append(fields, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	return invalid(fields)
}

// accountingYear is the financial year that holds on, with its framework and policies.
func (a *API) accountingYear(ctx context.Context, u storage.UnitOfWork, on string) (AccountingYear, error) {
	cal, _, err := a.calendar(ctx, u)
	if err != nil {
		return AccountingYear{}, err
	}
	d, err := readDay(ctx, a, u, on)
	if err != nil {
		return AccountingYear{}, err
	}
	y := cal.Year(max(cal.Number(d), 1))
	fs, err := u.AccountingBasis().Frameworks(ctx)
	if err != nil {
		return AccountingYear{}, err
	}
	ps, err := u.AccountingBasis().Policies(ctx)
	if err != nil {
		return AccountingYear{}, err
	}
	b := accounting.YearOf(y.Span, fs, ps)
	out := AccountingYear{StartsOn: y.StartsOn.Format(time.DateOnly), EndsOn: y.EndsOn.Format(time.DateOnly), Status: "not_approved",
		StatementTitle: b.Title, StatementBasis: b.Basis, RecommendedFramework: accounting.FrameworkSmallEntities,
		RecommendedEdition: b.Edition, Policies: make([]AccountingPolicyArea, 0, len(accounting.Areas)), Checks: []AccountingCheck{}}
	if b.Approved {
		out.Status = "approved"
	}
	var people []*Person
	if b.Framework != nil {
		f := accountingFrameworkOf(ctx, fs, *b.Framework)
		out.Framework = &f
		people = append(people, f.WrittenBy, f.ConfirmedBy)
	}
	for _, ar := range accounting.Areas {
		item := AccountingPolicyArea{Area: ar.Key, Title: ar.Title, Decides: ar.Decides, Setting: ar.Setting}
		if p := b.Policies[ar.Key]; p != nil {
			v := accountingPolicyOf(ctx, ps, *p)
			item.Policy = &v
			people = append(people, v.WrittenBy, v.ApprovedBy)
		}
		out.Policies = append(out.Policies, item)
	}
	for _, c := range b.Checks {
		out.Checks = append(out.Checks, AccountingCheck{Code: c.Code, Area: c.Area, Title: c.Title, Detail: c.Detail, Fix: c.Fix})
	}
	return out, a.name(ctx, u, people...)
}

func registerAccounting(a *API) {
	register(a, authz.GetAccounting, huma.Operation{
		OperationID: "getAccounting", Method: http.MethodGet, Path: "/v1/organisation/accounting", Tags: []string{"Organisation"},
		Summary: "Get a year's accounting framework and policies",
		Description: "The accounting framework and edition, and each accounting policy, in force for the financial year that holds the " +
			"day asked for (FR-ORG-10; PRD-001 rev 0.62), with who recorded, confirmed and approved each version and when. The " +
			"year's statements are titled \"Management statements\" and name no framework until its framework is confirmed and " +
			"every policy is approved by a member who holds the Accountant role, other than its writer; then \"Financial " +
			"statements\" with the basis they are prepared under (G4-017). checks lists what is missing. Read-only; no side effects.",
	}, []string{CodeInvalid, period.CodeNoYear}, func(ctx context.Context, in *accountingGetInput) (*accountingGetOutput, error) {
		p, _ := authn.From(ctx)
		var out accountingGetOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			out.Body, err = a.accountingYear(ctx, u, in.On)
			return err
		})
		return &out, err
	})

	register(a, authz.RecordAccountingFramework, huma.Operation{
		OperationID: "recordAccountingFramework", Method: http.MethodPost, Path: "/v1/organisation/accounting/framework", Tags: []string{"Organisation"},
		DefaultStatus: http.StatusCreated, Summary: "Record the accounting framework",
		Description: "Records the next version of the accounting framework and its edition, from the first day of a financial year " +
			"until a later version replaces it (FR-ORG-10; PRD-001 rev 0.62). A version never changes: a correction is a new " +
			"version, which the accountant confirms again. Side effects: the version and its audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeInvalid}, AccountingCodes...),
		func(ctx context.Context, in *accountingFrameworkInput) (*accountingFrameworkOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AccountingFramework, error) {
				from, err := fromDay(in.Body.FromOn)
				if err != nil {
					return AccountingFramework{}, err
				}
				cal, today, err := a.calendar(ctx, u)
				if err != nil {
					return AccountingFramework{}, err
				}
				edition := strings.TrimSpace(in.Body.Edition)
				if err := refused(accounting.CheckFramework(cal, from, today, in.Body.Framework, edition)); err != nil {
					return AccountingFramework{}, err
				}
				f, err := u.AccountingBasis().RecordFramework(ctx, from, in.Body.Framework, edition)
				if err != nil {
					return AccountingFramework{}, err
				}
				all, err := u.AccountingBasis().Frameworks(ctx)
				if err != nil {
					return AccountingFramework{}, err
				}
				out := accountingFrameworkOf(ctx, all, f)
				return out, a.name(ctx, u, out.WrittenBy)
			})
			if err != nil {
				return nil, err
			}
			return &accountingFrameworkOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ConfirmAccountingFramework, huma.Operation{
		OperationID: "confirmAccountingFramework", Method: http.MethodPost, Path: "/v1/organisation/accounting/framework/{version}/confirm",
		Tags: []string{"Organisation"}, Summary: "Confirm the accounting framework",
		Description: "Records the caller's confirmation, as the accountant, that the company may use a framework version (FR-ORG-10; " +
			"PRD-001 rev 0.62). Needs the Accountant role; never its writer, never twice, and never a version a later one from " +
			"the same year replaces (the founders' decision of 21 Oct 2026; the database refuses the writer too). While the " +
			"accounting.signing_step_up setting is on (the default), it needs step-up in the last 5 minutes. Side effects: " +
			"the confirmation and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStepUpRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, AccountingCodes...),
		func(ctx context.Context, in *accountingConfirmInput) (*accountingFrameworkOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AccountingFramework, error) {
				all, err := u.AccountingBasis().Frameworks(ctx)
				if err != nil {
					return AccountingFramework{}, err
				}
				var cur *storage.FrameworkVersion
				for i := range all {
					if all[i].Version == in.Version {
						cur = &all[i]
					}
				}
				if cur == nil {
					return AccountingFramework{}, problem(CodeNotFound, "No framework version "+strconv.Itoa(in.Version)+" is recorded.",
						"Read the year's framework to find its version.")
				}
				if err := accounting.MaySign("framework", cur.WrittenBy, signer(ctx), cur.ConfirmedBy, accounting.FrameworkReplaced(all, *cur)); err != nil {
					return AccountingFramework{}, err
				}
				if err := a.signingStepUp(ctx, u, "Confirming the framework"); err != nil {
					return AccountingFramework{}, err
				}
				f, err := u.AccountingBasis().ConfirmFramework(ctx, in.Version)
				if err != nil {
					return AccountingFramework{}, err
				}
				*cur = f
				out := accountingFrameworkOf(ctx, all, f)
				return out, a.name(ctx, u, out.WrittenBy, out.ConfirmedBy)
			})
			if err != nil {
				return nil, err
			}
			return &accountingFrameworkOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.RecordAccountingPolicy, huma.Operation{
		OperationID: "recordAccountingPolicy", Method: http.MethodPost, Path: "/v1/organisation/accounting/policies/{area}", Tags: []string{"Organisation"},
		DefaultStatus: http.StatusCreated, Summary: "Record an accounting policy",
		Description: "Records the next version of one accounting policy's words, from the first day of a financial year until a " +
			"later version replaces it (FR-ORG-10; PRD-001 rev 0.62). Fyniq never writes a policy: the words are the accountant's. " +
			"A version never changes: a correction is a new version, which the accountant approves again. Side effects: the " +
			"version and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld,
		CodeInvalid}, AccountingCodes...),
		func(ctx context.Context, in *accountingPolicyInput) (*accountingPolicyOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (AccountingPolicy, error) {
				from, err := fromDay(in.Body.FromOn)
				if err != nil {
					return AccountingPolicy{}, err
				}
				cal, today, err := a.calendar(ctx, u)
				if err != nil {
					return AccountingPolicy{}, err
				}
				wording := strings.TrimSpace(in.Body.Wording)
				if err := refused(accounting.CheckPolicy(cal, from, today, wording)); err != nil {
					return AccountingPolicy{}, err
				}
				p, err := u.AccountingBasis().RecordPolicy(ctx, in.Area, from, wording)
				if err != nil {
					return AccountingPolicy{}, err
				}
				all, err := u.AccountingBasis().Policies(ctx)
				if err != nil {
					return AccountingPolicy{}, err
				}
				out := accountingPolicyOf(ctx, all, p)
				return out, a.name(ctx, u, out.WrittenBy)
			})
			if err != nil {
				return nil, err
			}
			return &accountingPolicyOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.ApproveAccountingPolicy, huma.Operation{
		OperationID: "approveAccountingPolicy", Method: http.MethodPost, Path: "/v1/organisation/accounting/policies/{area}/{version}/approve",
		Tags: []string{"Organisation"}, Summary: "Approve an accounting policy",
		Description: "Records the caller's approval, as the accountant, of a version of one accounting policy (FR-ORG-10; PRD-001 " +
			"rev 0.62). Needs the Accountant role; never its writer, never twice, and never a version a later one from the same " +
			"year replaces (the founders' decision of 21 Oct 2026; the database refuses the writer too). While the " +
			"accounting.signing_step_up setting is on (the default), it needs step-up in the last 5 minutes. Side effects: the " +
			"approval and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStepUpRequired, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}, AccountingCodes...),
		func(ctx context.Context, in *accountingApproveInput) (*accountingPolicyOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (AccountingPolicy, error) {
				all, err := u.AccountingBasis().Policies(ctx)
				if err != nil {
					return AccountingPolicy{}, err
				}
				var cur *storage.PolicyVersion
				for i := range all {
					if all[i].Area == in.Area && all[i].Version == in.Version {
						cur = &all[i]
					}
				}
				if cur == nil {
					return AccountingPolicy{}, problem(CodeNotFound, "No version "+strconv.Itoa(in.Version)+" of the "+in.Area+" policy is recorded.",
						"Read the year's policies to find the version.")
				}
				if err := accounting.MaySign("policy", cur.WrittenBy, signer(ctx), cur.ApprovedBy, accounting.PolicyReplaced(all, *cur)); err != nil {
					return AccountingPolicy{}, err
				}
				if err := a.signingStepUp(ctx, u, "Approving a policy"); err != nil {
					return AccountingPolicy{}, err
				}
				p, err := u.AccountingBasis().ApprovePolicy(ctx, in.Area, in.Version)
				if err != nil {
					return AccountingPolicy{}, err
				}
				*cur = p
				out := accountingPolicyOf(ctx, all, p)
				return out, a.name(ctx, u, out.WrittenBy, out.ApprovedBy)
			})
			if err != nil {
				return nil, err
			}
			return &accountingPolicyOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
