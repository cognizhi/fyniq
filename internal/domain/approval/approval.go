// Package approval is the approval engine's core (PRD-001 §6.5, FR-APR-01…05, FR-APR-10; Founder mode's FM-1,
// §6.4): the approval envelope a person signs, the policies that say who signs it, and the rules a signature
// must meet. Nothing that needs approval executes unless the envelope rebuilt in the executing transaction is,
// byte for byte, the one that was signed.
//
// The envelope is RFC 8785 canonical JSON hashed with SHA-256. It holds the organisation, the object (type,
// ID, version), the operation, the change that executes, its amounts, every dependency version the object type
// declares (Dependencies), the policy version and the requester. A request expires 7 days after it is made,
// by the app's clock (FR-APR-05); the database voids it when its record changes (migration 000030).
//
// A policy says who signs (FR-APR-01, rev 0.40): ordered steps, each a quorum of people who hold the approve right
// (A) on the object's Appendix D row (Rows), narrowed to some roles or to named members; a policy never adds anyone
// Appendix D does not. Each object type has a built-in policy (Policies) until its organisation changes it; a
// change is itself an approval request under the policy for approval policies, which is fixed (FR-APR-09, SOD-8).
// The envelope records the organisation's team mode (§6.4), so a switch changes every envelope; a switch is itself an
// approval request (rev 0.42).
package approval

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// TTL is how long a request waits for its approvals before it expires (FR-APR-05's default).
const TTL = 7 * 24 * time.Hour

// Schema names the envelope's format; a change to the format changes it.
const Schema = "fyniq.approval-envelope/1"

// The object types approvals cover in this slice.
const (
	ObjectSetting    = "organisation_setting" // an organisation setting under dual control (FR-ORG-08, FM-1)
	ObjectMembership = "membership"           // a member's roles after the founder bootstrap (FR-ADM-01, FM-1)
	ObjectInvitation = "member_invitation"    // an invitation with roles after the founder bootstrap (FR-ADM-01, FM-1)
	ObjectPolicy     = "approval_policy"      // an object type's approval policy (FR-APR-09, SOD-8); its key is that object type
	ObjectPack       = "statutory_pack"       // a statutory pack version the organisation activates (ENG-32, SOD-10); its key is the version's id
	ObjectTeamMode   = "team_mode"            // the organisation's team mode (§6.4, FR-ADM-04); its key is TeamModeKey
)

// TeamModeKey is the object key of a team mode switch: the organisation has one team mode.
const TeamModeKey = "organisation"

// The operations: what executes on the final approval.
const (
	OpSettingChange  = "organisation_setting.change"
	OpRoleAssign     = "role.assign"
	OpRoleRevoke     = "role.revoke"
	OpInvite         = "member_invitation.create"
	OpPolicyChange   = "approval_policy.change"
	OpPackActivate   = "statutory_pack.activate"
	OpTeamModeChange = "team_mode.change"
)

// Dependencies are, per object type, the versions and values its envelope holds besides the change: when any
// of them differs at execution, the envelope differs and nothing executes (FR-APR-05, G4-055). A test changes
// each one after the request and expects the block.
var Dependencies = map[string][]string{
	// the setting's latest version, the value in effect on the change's date, and the changes scheduled after it
	ObjectSetting: {"setting_version", "value_in_effect", "scheduled"},
	// the membership's status and version, and the member's live roles with their expiries
	ObjectMembership: {"member_status", "member_version", "live_roles"},
	// nothing but the policy: the invitation does not exist until it is approved
	ObjectInvitation: {},
	// the version of the object type's policy in force: the change is from it
	ObjectPolicy: {"policy_version"},
	// the loaded version's digest and dates, and whether the organisation has activated it already
	ObjectPack: {"digest", "effective_from", "effective_to", "activated"},
	// the team mode's version in force, and the conflicts of §6.3 the members' roles and pending invitations hold
	ObjectTeamMode: {"mode_version", "conflicts"},
}

// Rows are, per object type, the Appendix D row whose approve right (A) a signer holds and on which any right lets a
// person see a request. A policy narrows who signs among them and never adds anyone.
var Rows = map[string]string{
	ObjectSetting:    "Organisation profile, financial year, defaults (rev 0.29)",
	ObjectMembership: "Users, roles, team mode",
	ObjectInvitation: "Users, roles, team mode",
	ObjectPolicy:     "Approval policies, AI catalogue",
	ObjectPack:       "Statutory parameters",
	ObjectTeamMode:   "Users, roles, team mode",
}

// The limits of a policy.
const (
	MaxSteps  = 5 // ordered steps
	MaxQuorum = 5 // people in one step
)

// Step is one ordered step of a policy: Quorum people who hold A on the object's row sign the same envelope; Roles,
// when given, narrows them to people holding one of those roles, and Users to those members (never both).
type Step struct {
	Quorum int
	Roles  []string
	Users  []uuid.UUID
}

// Policy says who approves an object type (FR-APR-01): its ordered steps. In Founder mode the requester never signs
// (FM-1) and every signer passes step-up (FR-APR-04), whatever the policy.
type Policy struct {
	ObjectType string
	Version    string // recorded in the envelope; a pending request keeps it (G4-057)
	Number     int    // the organisation's version of it; 0 for the built-in policy
	Steps      []Step
	Fixed      bool // no organisation changes it: the policy for approval policies
}

// Label is the version string of an organisation's version n of an object type's policy.
func Label(objectType string, n int) string { return fmt.Sprintf("%s/v%d", objectType, n) }

// Policies are the built-in policies of the object types, which apply until an organisation changes one: one step,
// with a quorum of one, of anyone who holds A on the object's row (the Owner for settings and roles; the Owner and
// the Controller for policies and statutory packs, Appendix D). The policy for approval policies is fixed, so no change can lock an
// organisation out of changing its policies (rev 0.40).
var Policies = map[string]Policy{
	ObjectSetting:    {ObjectType: ObjectSetting, Version: "founder/organisation_setting@2026-10-20.1", Steps: []Step{{Quorum: 1}}},
	ObjectMembership: {ObjectType: ObjectMembership, Version: "founder/membership@2026-10-20.1", Steps: []Step{{Quorum: 1}}},
	ObjectInvitation: {ObjectType: ObjectInvitation, Version: "founder/member_invitation@2026-10-20.1", Steps: []Step{{Quorum: 1}}},
	ObjectPolicy:     {ObjectType: ObjectPolicy, Version: "founder/approval_policy@2026-10-20.1", Steps: []Step{{Quorum: 1}}, Fixed: true},
	ObjectPack:       {ObjectType: ObjectPack, Version: "founder/statutory_pack@2026-10-20.1", Steps: []Step{{Quorum: 1}}},
	ObjectTeamMode:   {ObjectType: ObjectTeamMode, Version: "founder/team_mode@2026-10-20.1", Steps: []Step{{Quorum: 1}}},
}

// Amount is an amount and its currency in an envelope: a decimal string, never a binary number (ENG-10).
type Amount struct {
	Amount, Currency string
}

// Envelope is what an approver signs (FR-APR-10).
type Envelope struct {
	Organisation  uuid.UUID
	ObjectType    string
	ObjectID      string
	ObjectVersion int
	Operation     string
	Change        map[string]any // what executes, as strings, booleans, integers and lists of them
	Amounts       []Amount
	Dependencies  map[string]any // every name Dependencies declares for the object type
	Policy        string         // the policy's version
	TeamMode      string
	RequestedBy   uuid.UUID
}

// Canonical is the envelope as signed: RFC 8785 JSON. It refuses an envelope that leaves out a dependency its
// object type declares, or names one it does not.
func (e Envelope) Canonical() ([]byte, error) {
	declared, ok := Dependencies[e.ObjectType]
	if !ok {
		return nil, fmt.Errorf("approval: no object type %q", e.ObjectType)
	}
	if len(e.Dependencies) != len(declared) {
		return nil, fmt.Errorf("approval: a %s envelope holds %d dependencies, not the %d it declares", e.ObjectType, len(e.Dependencies), len(declared))
	}
	deps := map[string]any{}
	for _, name := range declared {
		v, ok := e.Dependencies[name]
		if !ok {
			return nil, fmt.Errorf("approval: a %s envelope leaves out its dependency %s", e.ObjectType, name)
		}
		deps[name] = v
	}
	amounts := make([]any, 0, len(e.Amounts))
	for _, a := range e.Amounts {
		amounts = append(amounts, map[string]any{"amount": a.Amount, "currency": a.Currency})
	}
	change := e.Change
	if change == nil {
		change = map[string]any{}
	}
	return Canonical(map[string]any{
		"schema":       Schema,
		"organisation": e.Organisation.String(),
		"object":       map[string]any{"type": e.ObjectType, "id": e.ObjectID, "version": e.ObjectVersion},
		"operation":    e.Operation,
		"change":       change,
		"amounts":      amounts,
		"dependencies": deps,
		"policy":       e.Policy,
		"team_mode":    e.TeamMode,
		"requested_by": e.RequestedBy.String(),
	})
}

// Hash is SHA-256 of the canonical envelope.
func Hash(canonical []byte) []byte {
	h := sha256.Sum256(canonical)
	return h[:]
}

// The codes of the approval rules.
const (
	CodeOwnRequest  = "APR-01" // the requester never approves their own request (FM-1)
	CodeNotPending  = "APR-02" // the request was decided already: approved, rejected, void or expired
	CodeExpired     = "APR-03" // the request is past its expiry (FR-APR-05)
	CodeChanged     = "APR-04" // the rebuilt envelope differs: the record or a dependency changed; nothing executed (FR-APR-10)
	CodeNotApprover = "APR-05" // the person holds no approve right on the object's Appendix D row
	CodePending     = "APR-06" // a request for the same object is waiting already
	CodeReason      = "APR-07" // a rejection needs a reason
	CodeSteps       = "APR-08" // a policy's steps are not ones that can be signed (FR-APR-01)
	CodeFixed       = "APR-09" // the policy for approval policies is fixed (FR-APR-09)
	CodeAuthority   = "APR-10" // an earlier approver no longer holds the right their step needs; nothing executed (FR-APR-10)
	CodeNotYourStep = "APR-11" // the request's current step needs someone else (FR-APR-01)
	CodeNotYours    = "APR-12" // only its requester withdraws a request
	CodeReviewOwn   = "APR-13" // a high-risk action is reviewed by someone other than the one who acted (FM-3)
	CodeReviewed    = "APR-14" // the action was reviewed already (FM-3)
)

// ReviewOverdue is how long a high-risk action may wait for its review before it is overdue in Founder mode (FM-3).
const ReviewOverdue = 14 * 24 * time.Hour

// Status is where a request stands.
type Status string

// The statuses.
const (
	Pending  Status = "pending"
	Approved Status = "approved" // every step signed; the change executed in the same transaction
	Rejected Status = "rejected"
	Void     Status = "void" // its record or a dependency changed (FR-APR-05)
	Expired  Status = "expired"
)

// Problem is one rule that failed.
type Problem struct {
	Code, Message, Fix string
}

// Request is what the rules need of a request.
type Request struct {
	Status      Status
	RequestedBy uuid.UUID
	ExpiresAt   time.Time
}

// StatusAt is the request's status at now: a pending request past its expiry is expired.
func StatusAt(r Request, now time.Time) Status {
	if r.Status == Pending && !now.Before(r.ExpiresAt) {
		return Expired
	}
	return r.Status
}

// CheckSigner returns the problem with signer signing r at now, or nil. That the signer holds the approve right
// and passed step-up is the caller's check, which knows the session; this is what the request decides.
func CheckSigner(r Request, signer uuid.UUID, now time.Time) *Problem {
	switch StatusAt(r, now) {
	case Expired:
		return &Problem{CodeExpired, fmt.Sprintf("the request expired at %s", r.ExpiresAt.UTC().Format(time.RFC3339)),
			"Ask for the change again: a request waits 7 days for its approvals"}
	case Pending:
	case Void:
		return &Problem{CodeChanged, "the record or one of its dependencies changed after the request, so the request is void",
			"Read the record again and ask for the change again if it is still needed"}
	default:
		return &Problem{CodeNotPending, fmt.Sprintf("the request is %s already", r.Status), "Nothing to do; ask for the change again if it is still needed"}
	}
	if signer == r.RequestedBy {
		return &Problem{CodeOwnRequest, "you asked for this change, so another founder approves it (FM-1)",
			"Ask another member who holds the approval right to approve it"}
	}
	return nil
}

// Eligible reports whether a person with roles may sign step s, besides holding A on the object's row: the step
// names none, one of their roles, or them.
func Eligible(s Step, user uuid.UUID, roles []string) bool {
	if len(s.Roles) == 0 && len(s.Users) == 0 {
		return true
	}
	if slices.Contains(s.Users, user) {
		return true
	}
	for _, r := range roles {
		if slices.Contains(s.Roles, r) {
			return true
		}
	}
	return false
}

// Signature is what the step rules need of a signature that is not voided.
type Signature struct {
	Step    int // 1-based
	Signer  uuid.UUID
	Approve bool
}

// CurrentStep is the 1-based step a request's next signature signs: the first whose quorum of approvals is not met;
// 0 when every step's is.
func CurrentStep(steps []Step, sigs []Signature) int {
	for i, s := range steps {
		n := 0
		for _, g := range sigs {
			if g.Step == i+1 && g.Approve {
				n++
			}
		}
		if n < s.Quorum {
			return i + 1
		}
	}
	return 0
}

// FieldProblem is a problem with one field of a change.
type FieldProblem struct {
	Field, Code, Message, Fix string
}

// Member is what CheckSteps needs of an active member: who they are, their live roles, and whether those roles hold
// A on the object's row.
type Member struct {
	UserID   uuid.UUID
	Roles    []string
	Approves bool
}

// CheckSteps returns the problems with steps as the next version of a policy whose object's row is row, or none. A
// role must be one that holds A on the row (approves); a named member must be an active member whose roles do. Each
// step needs at least its quorum of members who may sign it, and the quorums together no more than the members who
// may sign any step, since a person signs a request once.
func CheckSteps(row string, steps []Step, approves func(role string) bool, members []Member) []FieldProblem {
	var out []FieldProblem
	if len(steps) < 1 || len(steps) > MaxSteps {
		return []FieldProblem{{"body.steps", CodeSteps, fmt.Sprintf("a policy has 1 to %d steps, not %d", MaxSteps, len(steps)),
			fmt.Sprintf("Give the policy between 1 and %d ordered steps", MaxSteps)}}
	}
	signers := map[uuid.UUID]bool{}
	total := 0
	for i, s := range steps {
		at := fmt.Sprintf("body.steps[%d]", i)
		if s.Quorum < 1 || s.Quorum > MaxQuorum {
			out = append(out, FieldProblem{at + ".quorum", CodeSteps, fmt.Sprintf("a step's quorum is 1 to %d people, not %d", MaxQuorum, s.Quorum),
				fmt.Sprintf("Ask for between 1 and %d approvals in the step", MaxQuorum)})
			continue
		}
		total += s.Quorum
		if len(s.Roles) > 0 && len(s.Users) > 0 {
			out = append(out, FieldProblem{at, CodeSteps, "a step names roles or members, not both",
				"Split it into two steps, or keep either the roles or the members"})
			continue
		}
		bad := false
		for j, r := range s.Roles {
			switch {
			case slices.Index(s.Roles, r) < j:
				out, bad = append(out, FieldProblem{fmt.Sprintf("%s.roles[%d]", at, j), CodeSteps, r + " is named twice", "Name each role once"}), true
			case !approves(r):
				out, bad = append(out, FieldProblem{fmt.Sprintf("%s.roles[%d]", at, j), CodeSteps,
					fmt.Sprintf("%s is not a role that approves %s (Appendix D)", r, row), "Name a role that holds A on that row, such as owner"}), true
			}
		}
		for j, u := range s.Users {
			k := slices.IndexFunc(members, func(m Member) bool { return m.UserID == u })
			switch {
			case slices.Index(s.Users, u) < j:
				out, bad = append(out, FieldProblem{fmt.Sprintf("%s.users[%d]", at, j), CodeSteps, "that member is named twice", "Name each member once"}), true
			case k < 0 || !members[k].Approves:
				out, bad = append(out, FieldProblem{fmt.Sprintf("%s.users[%d]", at, j), CodeSteps,
					fmt.Sprintf("%s is not an active member whose roles approve %s", u, row),
					"Name a member who holds a role with A on that row, or give them such a role first"}), true
			}
		}
		if bad {
			continue
		}
		n := 0
		for _, m := range members {
			if m.Approves && Eligible(s, m.UserID, m.Roles) {
				n++
				signers[m.UserID] = true
			}
		}
		if n < s.Quorum {
			out = append(out, FieldProblem{at + ".quorum", CodeSteps, fmt.Sprintf("the step needs %d approvals but only %d members may sign it", s.Quorum, n),
				"Lower the quorum, or name more roles or members"})
		}
	}
	if len(out) == 0 && total > len(signers) {
		out = append(out, FieldProblem{"body.steps", CodeSteps,
			fmt.Sprintf("the steps need %d approvals in all but only %d members may sign them, and a person signs a request once", total, len(signers)),
			"Lower the quorums, or name more roles or members"})
	}
	return out
}
