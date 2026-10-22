package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ approval policies (FR-APR-01, FR-APR-09, SOD-8, rev 0.40)

// UserRef is a member named by their user ID.
type UserRef string

// Schema implements huma.SchemaProvider: a UUID string, with an example.
func (UserRef) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Format: "uuid", Description: "A member's user ID.", Examples: []any{"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"}}
}

// ApprovalStep is one ordered step of an approval policy.
type ApprovalStep struct {
	Quorum int        `json:"quorum" doc:"How many people sign this step, each once." example:"1"`
	Roles  []RoleName `json:"roles" doc:"Only people holding one of these roles sign it; empty, with users empty, for anyone who holds the approval right (A) on the object's Appendix D row."`
	Users  []UserRef  `json:"users" doc:"Only these members sign it; empty, with roles empty, for anyone who holds the approval right."`
}

// ApprovalStepChange is one ordered step of a policy you ask for.
type ApprovalStepChange struct {
	Quorum int        `json:"quorum" minimum:"1" maximum:"5" doc:"How many people sign this step, each once: 1 to 5." example:"1"`
	Roles  []RoleName `json:"roles,omitempty" maxItems:"16" doc:"Only people holding one of these roles sign it: roles whose Appendix D cell holds A on the object's row. Never with users."`
	Users  []UserRef  `json:"users,omitempty" maxItems:"16" doc:"Only these members sign it: active members whose roles hold A on the object's row. Never with roles."`
}

// ApprovalPolicyVersion is a version of an organisation's policy, kept for good (FR-APR-09).
type ApprovalPolicyVersion struct {
	Version   string         `json:"version" doc:"The version string the requests made under it record." example:"organisation_setting/v1"`
	Number    int            `json:"number" doc:"Its number, from 1." example:"1"`
	Steps     []ApprovalStep `json:"steps" doc:"Its ordered steps."`
	RequestID string         `json:"request_id" format:"uuid" doc:"The approved request that made it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	ChangedBy string         `json:"changed_by" format:"uuid" doc:"Who asked for it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	ChangedAt time.Time      `json:"changed_at" doc:"When it took effect: when its request was approved." example:"2026-10-20T09:30:00Z"`
}

// ApprovalPolicy is the policy in force for an object type: who signs a request for it.
type ApprovalPolicy struct {
	ObjectType string         `json:"object_type" enum:"organisation_setting,membership,member_invitation,approval_policy,statutory_pack,team_mode,period,journal,opening_balance,bill,bill_payment" doc:"The kind of change it covers." example:"organisation_setting"`
	Row        string         `json:"row" doc:"The Appendix D row whose approval right (A) every signer holds; a policy narrows who signs among them and never adds anyone." example:"Organisation profile, financial year, defaults (rev 0.29)"`
	Version    string         `json:"version" doc:"The version string each request records (G4-057)." example:"founder/organisation_setting@2026-10-20.1"`
	Number     int            `json:"number" doc:"The organisation's version of it; 0 while the built-in policy applies." example:"0"`
	BuiltIn    bool           `json:"built_in" doc:"true while the built-in policy applies: one step with a quorum of one." example:"true"`
	Fixed      bool           `json:"fixed" doc:"true for the policy for approval policies, which no organisation changes, so a change can never lock it out (FR-APR-09)." example:"false"`
	Steps      []ApprovalStep `json:"steps" doc:"Its ordered steps: each needs its quorum before the next is signed (FR-APR-01)."`
}

// ApprovalPolicyDetail is one policy in force, with the organisation's versions of it.
type ApprovalPolicyDetail struct {
	ApprovalPolicy
	History []ApprovalPolicyVersion `json:"history" doc:"The organisation's versions of it, oldest first; empty while the built-in policy applies."`
}

// ApprovalPolicyPage is a page of the policies in force.
type ApprovalPolicyPage struct {
	Items      []ApprovalPolicy `json:"items" doc:"The policy in force for each object type, by object type."`
	NextCursor string           `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6Im1lbWJlcnNoaXAifQ"`
}

// ApprovalPolicyChange asks for a policy's next version.
type ApprovalPolicyChange struct {
	Steps []ApprovalStepChange `json:"steps" minItems:"1" maxItems:"5" doc:"The ordered steps: 1 to 5."`
}

// ApprovalPolicyPath names a policy by the object type it covers.
type ApprovalPolicyPath struct {
	ObjectType string `path:"object_type" maxLength:"40" doc:"The kind of change the policy covers." example:"organisation_setting"`
}

type approvalPolicyListInput struct{ PageQuery }

type approvalPolicyGetInput struct{ ApprovalPolicyPath }

type approvalPolicyChangeInput struct {
	ApprovalPolicyPath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body ApprovalPolicyChange
}

type approvalPolicyListOutput struct{ Body ApprovalPolicyPage }

type approvalPolicyReadOutput struct {
	ETag string `header:"ETag" doc:"The policy's number, for If-Match: \"0\" while the built-in policy applies."`
	Body ApprovalPolicyDetail
}

type approvalPolicyChangeOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

// PolicyCodes are the policy rules.
var PolicyCodes = []string{approval.CodeSteps, approval.CodeFixed}

func domainSteps(steps []storage.ApprovalStep) []approval.Step {
	out := make([]approval.Step, len(steps))
	for i, s := range steps {
		out[i] = approval.Step{Quorum: s.Quorum, Roles: s.Roles, Users: s.Users}
	}
	return out
}

func storageSteps(steps []approval.Step) []storage.ApprovalStep {
	out := make([]storage.ApprovalStep, len(steps))
	for i, s := range steps {
		out[i] = storage.ApprovalStep{Quorum: s.Quorum, Roles: s.Roles, Users: s.Users}
		if out[i].Roles == nil {
			out[i].Roles = []string{}
		}
		if out[i].Users == nil {
			out[i].Users = []uuid.UUID{}
		}
	}
	return out
}

func apiStep(s storage.ApprovalStep) ApprovalStep {
	out := ApprovalStep{Quorum: s.Quorum, Roles: []RoleName{}, Users: []UserRef{}}
	for _, r := range s.Roles {
		out.Roles = append(out.Roles, RoleName(r))
	}
	for _, u := range s.Users {
		out.Users = append(out.Users, UserRef(u.String()))
	}
	return out
}

// policyFor returns the policy in force for an object type, with the organisation's versions of it: its latest
// version, or the built-in policy until it has one.
func (a *API) policyFor(ctx context.Context, u storage.UnitOfWork, objectType string) (approval.Policy, []storage.ApprovalPolicy, error) {
	builtin, ok := approval.Policies[objectType]
	if !ok {
		return approval.Policy{}, nil, fmt.Errorf("approval: no object type %q", objectType)
	}
	if builtin.Fixed {
		return builtin, nil, nil
	}
	all, err := u.ApprovalPolicies().List(ctx)
	if err != nil {
		return approval.Policy{}, nil, err
	}
	var versions []storage.ApprovalPolicy
	for _, v := range all {
		if v.ObjectType == objectType {
			versions = append(versions, v)
		}
	}
	if len(versions) == 0 {
		return builtin, nil, nil
	}
	last := versions[len(versions)-1]
	return approval.Policy{ObjectType: objectType, Version: approval.Label(objectType, last.Version), Number: last.Version,
		Steps: domainSteps(last.Steps)}, versions, nil
}

// checkSteps checks steps as the next policy for objectType against the active members now (FR-APR-01).
func (a *API) checkSteps(ctx context.Context, u storage.UnitOfWork, objectType string, steps []approval.Step) ([]approval.FieldProblem, error) {
	ms, err := u.Members().List(ctx, a.deps.Clock.Now())
	if err != nil {
		return nil, err
	}
	members := make([]approval.Member, len(ms))
	for i, m := range ms {
		roles := memberRoles(m)
		members[i] = approval.Member{UserID: m.UserID, Roles: roles, Approves: approves(roles, objectType)}
	}
	return approval.CheckSteps(approval.Rows[objectType], steps, func(role string) bool {
		_, known := authz.Roles[authz.Role(role)]
		return known && approves([]string{role}, objectType)
	}, members), nil
}

func policyOf(p approval.Policy) ApprovalPolicy {
	out := ApprovalPolicy{ObjectType: p.ObjectType, Row: approval.Rows[p.ObjectType], Version: p.Version, Number: p.Number, BuiltIn: p.Number == 0,
		Fixed: p.Fixed, Steps: []ApprovalStep{}}
	for _, s := range storageSteps(p.Steps) {
		out.Steps = append(out.Steps, apiStep(s))
	}
	return out
}

func policyDetailOf(p approval.Policy, versions []storage.ApprovalPolicy) ApprovalPolicyDetail {
	out := ApprovalPolicyDetail{ApprovalPolicy: policyOf(p), History: []ApprovalPolicyVersion{}}
	for _, v := range versions {
		h := ApprovalPolicyVersion{Version: approval.Label(v.ObjectType, v.Version), Number: v.Version, Steps: []ApprovalStep{},
			RequestID: v.RequestID.String(), ChangedBy: v.CreatedBy.String(), ChangedAt: v.CreatedAt.UTC()}
		for _, s := range v.Steps {
			h.Steps = append(h.Steps, apiStep(s))
		}
		out.History = append(out.History, h)
	}
	return out
}

// lookupPolicy is the object type a path names, or the problem.
func lookupPolicy(objectType string) error {
	if _, ok := approval.Policies[objectType]; !ok {
		return problem(CodeNotFound, fmt.Sprintf("No approval policy covers %q.", objectType),
			"List the approval policies to find the object types they cover")
	}
	return nil
}

func registerApprovalPolicies(a *API) {
	register(a, authz.ListApprovalPolicies, huma.Operation{
		OperationID: "listApprovalPolicies", Method: http.MethodGet, Path: "/v1/approval-policies", Tags: []string{"Approvals"},
		Summary: "List the approval policies",
		Description: "The approval policy in force for each kind of change the approval engine covers: who signs a request, as ordered steps " +
			"with a quorum of people who hold the approval right (A) on the object's Appendix D row, narrowed to roles or named members " +
			"(FR-APR-01). The built-in policy, one step with a quorum of one, applies until the organisation changes it. Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *approvalPolicyListInput) (*approvalPolicyListOutput, error) {
		p, _ := authn.From(ctx)
		var all []ApprovalPolicy
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			types := make([]string, 0, len(approval.Policies))
			for t := range approval.Policies {
				types = append(types, t)
			}
			slices.Sort(types)
			for _, t := range types {
				pol, _, err := a.policyFor(ctx, u, t)
				if err != nil {
					return err
				}
				all = append(all, policyOf(pol))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(v ApprovalPolicy) string { return v.ObjectType }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &approvalPolicyListOutput{Body: ApprovalPolicyPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.GetApprovalPolicy, huma.Operation{
		OperationID: "getApprovalPolicy", Method: http.MethodGet, Path: "/v1/approval-policies/{object_type}", Tags: []string{"Approvals"},
		Summary: "Get an approval policy",
		Description: "The approval policy in force for one kind of change, with every version the organisation has had of it and the approved " +
			"request that made each (FR-APR-01, FR-APR-09). Its ETag is the policy's number. Read-only; no side effects.",
	}, []string{CodeNotFound}, func(ctx context.Context, in *approvalPolicyGetInput) (*approvalPolicyReadOutput, error) {
		if err := lookupPolicy(in.ObjectType); err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out approvalPolicyReadOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			pol, versions, err := a.policyFor(ctx, u, in.ObjectType)
			out.Body, out.ETag = policyDetailOf(pol, versions), etag(pol.Number)
			return err
		})
		return &out, err
	})

	register(a, authz.ChangeApprovalPolicy, huma.Operation{
		OperationID: "changeApprovalPolicy", Method: http.MethodPatch, Path: "/v1/approval-policies/{object_type}", Tags: []string{"Approvals"},
		Summary: "Ask to change an approval policy", DefaultStatus: http.StatusAccepted,
		Description: "Asks for the policy's next version: its ordered steps, each a quorum of 1 to 5 people who hold the approval right (A) on " +
			"the object's Appendix D row, narrowed to roles or to named members, never both (FR-APR-01). A policy never adds anyone Appendix D " +
			"does not, and its steps must be ones the members can sign (APR-08). The change is never made at once: the answer is 202 with an " +
			"approval request, which a member who holds A on \"Approval policies, AI catalogue\" (the Owner or the Controller) and is not the " +
			"requester approves (FR-APR-09, SOD-8). On its approval the version is added, and every pending request of that kind restarts: " +
			"the database voids them (FR-APR-10). The policy for approval policies is fixed (APR-09). Asking needs a person signed in who " +
			"passed step-up in the last 5 minutes and If-Match with the policy's ETag. Side effects: the approval request and its audit event. " +
			"Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeInvalid, storage.CodeVersion, storage.CodeRetry, storage.CodeLockTimeout,
		storage.CodeApproval, CodeSessionRequired, CodeStepUpRequired, approval.CodePending}, PolicyCodes...),
		func(ctx context.Context, in *approvalPolicyChangeInput) (*approvalPolicyChangeOutput, error) {
			if err := lookupPolicy(in.ObjectType); err != nil {
				return nil, err
			}
			if approval.Policies[in.ObjectType].Fixed {
				return nil, problem(approval.CodeFixed, "The policy for approval policies is fixed, so no change can lock the organisation out of changing its policies.",
					"Change the policy of another kind of change")
			}
			steps := make([]approval.Step, len(in.Body.Steps))
			var bad []FieldProblem
			for i, s := range in.Body.Steps {
				steps[i] = approval.Step{Quorum: s.Quorum}
				for _, r := range s.Roles {
					steps[i].Roles = append(steps[i].Roles, string(r))
				}
				for j, ref := range s.Users {
					raw := string(ref)
					id, err := uuid.Parse(raw)
					if err != nil || id.String() != strings.ToLower(raw) {
						bad = append(bad, FieldProblem{Code: approval.CodeSteps, Field: fmt.Sprintf("body.steps[%d].users[%d]", i, j),
							Message: "a member is named by their user ID, a UUID", Fix: "Send the member's user_id from the members list"})
						continue
					}
					steps[i].Users = append(steps[i].Users, id)
				}
			}
			if len(bad) > 0 {
				return nil, invalid(bad)
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				current, _, err := a.policyFor(ctx, u, in.ObjectType)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(current.Number)); err != nil {
					return Approval{}, err
				}
				fps, err := a.checkSteps(ctx, u, in.ObjectType, steps)
				if err != nil {
					return Approval{}, err
				}
				for _, x := range fps {
					bad = append(bad, FieldProblem{Code: x.Code, Field: x.Field, Message: x.Message, Fix: x.Fix})
				}
				if len(bad) > 0 {
					return Approval{}, invalid(bad)
				}
				next := storageSteps(steps)
				before, after := describeSteps(storageSteps(current.Steps)), describeSteps(next)
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectPolicy, ObjectKey: in.ObjectType,
					Operation: approval.OpPolicyChange,
					Summary:   fmt.Sprintf("Change who approves %s: %s", strings.ReplaceAll(in.ObjectType, "_", " "), after),
					Execute:   map[string]any{"object_type": in.ObjectType, "version": current.Number + 1, "steps": stepsValue(next)},
					Diff:      []ApprovalDiff{{Field: "steps", Before: before, After: after}}})
			})
			if err != nil {
				return nil, err
			}
			return &approvalPolicyChangeOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}

// stepsValue is steps as an envelope's change holds them: lists of strings and integers (canonical JSON, FR-APR-10).
func stepsValue(steps []storage.ApprovalStep) []any {
	out := make([]any, len(steps))
	for i, s := range steps {
		roles, users := make([]any, len(s.Roles)), make([]any, len(s.Users))
		for j, r := range s.Roles {
			roles[j] = r
		}
		for j, u := range s.Users {
			users[j] = u.String()
		}
		out[i] = map[string]any{"quorum": s.Quorum, "roles": roles, "users": users}
	}
	return out
}

// describeSteps says what steps need, for a request's summary and diff.
func describeSteps(steps []storage.ApprovalStep) string {
	parts := make([]string, len(steps))
	for i, s := range steps {
		who := "anyone who holds the approval right"
		switch {
		case len(s.Roles) > 0:
			who = strings.Join(s.Roles, " or ")
		case len(s.Users) > 0:
			names := make([]string, len(s.Users))
			for j, u := range s.Users {
				names[j] = u.String()
			}
			who = "members " + strings.Join(names, ", ")
		}
		parts[i] = fmt.Sprintf("step %d: %d of %s", i+1, s.Quorum, who)
	}
	return strings.Join(parts, "; ")
}
