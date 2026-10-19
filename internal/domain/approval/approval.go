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
// Policies are code in this slice, each with a version string recorded in the envelope (provisional until
// FR-APR-09's approval-gated policy changes arrive); the team mode is Founder mode, the default at go-live.
package approval

import (
	"crypto/sha256"
	"fmt"
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
)

// The operations: what executes on the final approval.
const (
	OpSettingChange = "organisation_setting.change"
	OpRoleAssign    = "role.assign"
	OpRoleRevoke    = "role.revoke"
	OpInvite        = "member_invitation.create"
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
}

// Step is one ordered step of a policy: Quorum people who hold the approve right (A) on Row, the Appendix D
// capability of the object, sign the same envelope.
type Step struct {
	Row    string
	Quorum int
}

// Policy says who approves an object type (FR-APR-01): its ordered steps, and in Founder mode that the
// requester never signs (FM-1) and every signer passes step-up (FR-APR-04).
type Policy struct {
	ObjectType string
	Version    string // recorded in the envelope; a pending request keeps it (G4-057)
	Mode       string // the team mode it is for
	Steps      []Step
}

// TeamModeFounder is Founder mode (§6.4), the default team mode at go-live.
const TeamModeFounder = "founder"

// Policies are the Founder-mode policies of the object types (provisional until FR-APR-09: policies as
// versioned, approval-gated data). Organisation settings and roles are the Owner's to approve (Appendix D).
var Policies = map[string]Policy{
	ObjectSetting: {ObjectType: ObjectSetting, Version: "founder/organisation_setting@2026-10-20.1", Mode: TeamModeFounder,
		Steps: []Step{{Row: "Organisation profile, financial year, defaults (rev 0.29)", Quorum: 1}}},
	ObjectMembership: {ObjectType: ObjectMembership, Version: "founder/membership@2026-10-20.1", Mode: TeamModeFounder,
		Steps: []Step{{Row: "Users, roles, team mode", Quorum: 1}}},
	ObjectInvitation: {ObjectType: ObjectInvitation, Version: "founder/member_invitation@2026-10-20.1", Mode: TeamModeFounder,
		Steps: []Step{{Row: "Users, roles, team mode", Quorum: 1}}},
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
)

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
