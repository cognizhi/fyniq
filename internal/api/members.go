package api

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/member"
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
	revoke "github.com/cognizhi/fyniq/internal/tokens"
)

// ------------------------------------------------------------------ members and roles (FR-ADM-01, FM-1, rev 0.35, rev 0.36)

// MemberRole is a role a member holds.
type MemberRole struct {
	Role      RoleName   `json:"role" doc:"The role (PRD-001 §6.2)."`
	ExpiresAt *time.Time `json:"expires_at,omitempty" doc:"When it ends, for an external user; absent while it lasts." example:"2027-03-31T00:00:00Z"`
}

// Member is a member of the organisation with their live roles.
type Member struct {
	ID          string       `json:"id" format:"uuid" doc:"The membership's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
	UserID      string       `json:"user_id" format:"uuid" doc:"The member's user." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	DisplayName string       `json:"display_name" doc:"Their name." example:"Wei Ling Tan"`
	Roles       []MemberRole `json:"roles" doc:"Their live roles, by role."`
}

// MemberPage is a page of members, in the order they joined.
type MemberPage struct {
	Items      []Member `json:"items" doc:"The members on this page."`
	NextCursor string   `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// MemberInvitation is an invitation to the organisation, without its code.
type MemberInvitation struct {
	ID            string     `json:"id" format:"uuid" doc:"The invitation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	DisplayName   string     `json:"display_name" doc:"Who it is for." example:"Wei Ling Tan"`
	Roles         []RoleName `json:"roles" doc:"The roles they will hold."`
	RolesExpireAt *time.Time `json:"roles_expire_at,omitempty" doc:"When those roles end, for an external user." example:"2027-03-31T00:00:00Z"`
	CreatedAt     time.Time  `json:"created_at" doc:"When it was made." example:"2026-10-20T09:00:00Z"`
	ExpiresAt     time.Time  `json:"expires_at" doc:"When it stops working, if unused." example:"2026-10-27T09:00:00Z"`
	WithdrawnAt   *time.Time `json:"withdrawn_at,omitempty" doc:"When it was withdrawn." example:"2026-10-21T09:00:00Z"`
}

// MemberInvitationPage is a page of the pending invitations, oldest first.
type MemberInvitationPage struct {
	Items      []MemberInvitation `json:"items" doc:"The invitations neither accepted, withdrawn nor expired."`
	NextCursor string             `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// NewMemberInvitation is who to invite, with their roles.
type NewMemberInvitation struct {
	DisplayName   string     `json:"display_name" maxLength:"200" doc:"The name of the person you invite." example:"Wei Ling Tan"`
	Roles         []RoleName `json:"roles" minItems:"1" maxItems:"16" doc:"The roles they will hold (PRD-001 §6.2)."`
	RolesExpireAt *time.Time `json:"roles_expire_at,omitempty" doc:"For an external user, such as an auditor: when their roles end. Never on the Owner role." example:"2027-03-31T00:00:00Z"`
}

// MemberInvitationCode is a new invitation with its code, shown once.
type MemberInvitationCode struct {
	MemberInvitation
	Code string `json:"code" doc:"The one-time code. It is shown once and cannot be shown again; give it to the person in person or on a call. Case, spaces and dashes do not matter." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
}

// RoleGrant gives a member a role.
type RoleGrant struct {
	Role              RoleName   `json:"role" doc:"The role (PRD-001 §6.2)."`
	ExpiresAt         *time.Time `json:"expires_at,omitempty" doc:"For an external user: when it ends. Never on the Owner role." example:"2027-03-31T00:00:00Z"`
	SoDOverrideReason string     `json:"sod_override_reason,omitempty" maxLength:"500" doc:"In a team mode that blocks conflicts (Standard): why the member keeps the conflict of §6.3 this role makes. The role change's approver accepts the override, which is reported with the team mode (TEAM-02, rev 0.42)." example:"Founder who also keeps the books until we hire"`
}

// JoinRequest redeems an invitation.
type JoinRequest struct {
	InvitationCode string `json:"invitation_code" maxLength:"64" doc:"The code you were given. Case, spaces and dashes do not matter." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
	DisplayName    string `json:"display_name" maxLength:"200" doc:"Your name, used when your sign-in has no Fyniq user yet." example:"Wei Ling Tan"`
}

// Joined is the organisation you joined, with your roles there.
type Joined struct {
	OrganisationID string     `json:"organisation_id" format:"uuid" doc:"The organisation you joined; your session is in it from your next request." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d02"`
	UserID         string     `json:"user_id" format:"uuid" doc:"Your Fyniq user." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	MemberID       string     `json:"member_id" format:"uuid" doc:"Your membership." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
	Roles          []RoleName `json:"roles" doc:"Your roles there."`
}

// MemberPath names a member.
type MemberPath struct {
	MemberID string `path:"member_id" format:"uuid" doc:"The membership's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
}

// InvitationPath names an invitation.
type InvitationPath struct {
	InvitationID string `path:"invitation_id" format:"uuid" doc:"The invitation's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
}

type memberListInput struct{ PageQuery }

type invitationListInput struct{ PageQuery }

type invitationCreateInput struct{ Body NewMemberInvitation }

type invitationWithdrawInput struct {
	InvitationPath
	PreferHeader
}

type roleAssignInput struct {
	MemberPath
	KeyHeader
	PreferHeader
	Body RoleGrant
}

type roleRevokeInput struct {
	MemberPath
	Role string `path:"role" maxLength:"40" doc:"The role to end." example:"bookkeeper"`
	PreferHeader
}

type joinInput struct{ Body JoinRequest }

type memberListOutput struct{ Body MemberPage }

type invitationListOutput struct{ Body MemberInvitationPage }

type invitationCodeOutput struct {
	Status int
	Body   any // a MemberInvitationCode, or the Approval it waits for (202)
}

type invitationWriteOutput struct {
	Applied string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body    MemberInvitation
}

type memberWriteOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     any    // a Member, or the Approval it waits for (202)
}

type joinOutput struct{ Body Joined }

// MemberCodes are the members' rules (internal/domain/member).
var MemberCodes = []string{member.CodeRole, member.CodeName, member.CodeRoles, member.CodeExpiry, member.CodeLastOwner, member.CodeHeld}

// invitationConflict refuses an invitation whose roles hold a conflict the team mode blocks: an invitation is never
// overridden, so the person is invited with roles that do not conflict and given the other role with an override.
func invitationConflict(ctx context.Context, u storage.UnitOfWork, name string, roles []string) error {
	cs := authz.RoleConflicts(authzRoles(roles))
	if len(cs) == 0 {
		return nil
	}
	mode, _, _, err := teamModeOf(ctx, u)
	if err != nil || !team.Blocks(mode) {
		return err
	}
	return problem(team.CodeConflict, fmt.Sprintf("Inviting %s as %s would make %s (%s), which %s mode blocks.", name, strings.Join(roles, ", "),
		cs[0].SoD, strings.Join(roleStrings(cs[0].Roles), ", "), mode),
		"Invite them with roles that do not conflict, then give the other role with sod_override_reason once they join")
}

// approvalCodes are what asking for a change that waits for approval can return (FM-1, rev 0.36).
var approvalCodes = []string{CodeSessionRequired, CodeStepUpRequired, approval.CodePending, storage.CodeApproval}

func memberOf(m storage.Member) Member {
	out := Member{ID: m.ID.String(), UserID: m.UserID.String(), DisplayName: m.DisplayName, Roles: []MemberRole{}}
	for _, r := range m.Roles {
		out.Roles = append(out.Roles, MemberRole{Role: RoleName(r.Role), ExpiresAt: r.ExpiresAt})
	}
	return out
}

func invitationOf(i storage.MemberInvitation) MemberInvitation {
	out := MemberInvitation{ID: i.ID.String(), DisplayName: i.DisplayName, Roles: []RoleName{}, RolesExpireAt: i.RolesExpireAt,
		CreatedAt: i.CreatedAt.UTC(), ExpiresAt: i.ExpiresAt.UTC(), WithdrawnAt: i.WithdrawnAt}
	for _, r := range i.Roles {
		out.Roles = append(out.Roles, RoleName(r))
	}
	return out
}

func roleNames(rs []RoleName) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = string(r)
	}
	return out
}

// memberProblems turns the members' rules into the API's field problems.
func memberProblems(ps []member.Problem) []FieldProblem {
	var out []FieldProblem
	for _, x := range ps {
		out = append(out, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
	}
	return out
}

// needsApproval reports whether a role change needs a second approver: once the organisation has a second active
// member, it waits for approval (FM-1, rev 0.36). It takes the members' lock, so nobody joins until the change or
// the request is saved.
func needsApproval(ctx context.Context, u storage.UnitOfWork) (bool, error) {
	n, err := u.Members().Count(ctx)
	return member.NeedsApproval(n), err
}

func liveRoles(m storage.Member) string {
	var out []string
	for _, r := range m.Roles {
		out = append(out, r.Role)
	}
	return strings.Join(out, ", ")
}

// logBootstrap logs a change the Owner made alone during the founder bootstrap, for the weekly review once another
// founder joins (FR-ADM-01, FM-3).
func (a *API) logBootstrap(ctx context.Context, u storage.UnitOfWork, objectType, objectKey, summary string) error {
	p, _ := authn.From(ctx)
	var actor *uuid.UUID
	if p.UserID != uuid.Nil {
		actor = &p.UserID
	}
	_, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewFounderBootstrap, ObjectType: objectType, ObjectKey: objectKey,
		Summary: summary, Actor: actor, LoggedAt: a.deps.Clock.Now()})
	return err
}

// assignRoleNow gives member id role until expires (nil: no expiry), and revokes their OAuth tokens.
func (a *API) assignRoleNow(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, role string, expires *time.Time) (Member, error) {
	now := a.deps.Clock.Now()
	m, err := findMember(ctx, u, id, now)
	if err != nil {
		return Member{}, err
	}
	if slices.ContainsFunc(m.Roles, func(r storage.RoleAssignment) bool { return r.Role == role }) {
		return Member{}, problem(member.CodeHeld, m.DisplayName+" holds the role "+role+" already.", "Nothing to do; revoke it first to change its expiry")
	}
	if _, err := u.Members().Assign(ctx, id, role, expires); err != nil {
		return Member{}, err
	}
	if err := a.endTokens(ctx, u, m, "role "+role+" assigned"); err != nil {
		return Member{}, err
	}
	m, err = findMember(ctx, u, id, now)
	return memberOf(m), err
}

// revokeRoleNow ends member id's role, and revokes their OAuth tokens; the last Owner keeps the role.
func (a *API) revokeRoleNow(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, role string) (Member, error) {
	now := a.deps.Clock.Now()
	m, err := findMember(ctx, u, id, now)
	if err != nil {
		return Member{}, err
	}
	i := slices.IndexFunc(m.Roles, func(r storage.RoleAssignment) bool { return r.Role == role })
	if i < 0 {
		return Member{}, problem(member.CodeHeld, m.DisplayName+" does not hold the role "+role+".", "List the members to see their roles")
	}
	if err := u.Members().Revoke(ctx, m.Roles[i].ID); err != nil {
		if storage.CodeOf(err) == storage.CodeMembership && role == "owner" {
			return Member{}, problem(member.CodeLastOwner, "The organisation's last Owner keeps the role.", "Make someone else Owner first")
		}
		return Member{}, err
	}
	if err := a.endTokens(ctx, u, m, "role "+role+" revoked"); err != nil {
		return Member{}, err
	}
	m, err = findMember(ctx, u, id, now)
	return memberOf(m), err
}

// endTokens asks for the member's OAuth tokens to be revoked in the same unit of work: their claims are a
// snapshot of the roles, so a role change ends them (AI-38). A session reads the roles on every request.
func (a *API) endTokens(ctx context.Context, u storage.UnitOfWork, m storage.Member, why string) error {
	if a.deps.Outbox == nil {
		return nil
	}
	p, _ := authn.From(ctx)
	return revoke.RevokeAll(ctx, *a.deps.Outbox, u, p.Organisation, m.UserID.String(), why)
}

// findMember returns the active member with id, or the not-found problem.
func findMember(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, now time.Time) (storage.Member, error) {
	all, err := u.Members().List(ctx, now)
	if err != nil {
		return storage.Member{}, err
	}
	i := slices.IndexFunc(all, func(m storage.Member) bool { return m.ID == id })
	if i < 0 {
		return storage.Member{}, problem(CodeNotFound, "No member with that ID exists in this organisation.", "List the members to find their IDs")
	}
	return all[i], nil
}

func registerMembers(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	noKey := " It takes no Idempotency-Key and no preview: its code makes it one-time (FR-INT-08, rev 0.35)."

	register(a, authz.ListMembers, huma.Operation{
		OperationID: "listMembers", Method: http.MethodGet, Path: "/v1/members", Tags: []string{"Members"},
		Summary:     "List the members",
		Description: "The organisation's active members, in the order they joined, with their names and live roles (FR-ADM-01). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *memberListInput) (*memberListOutput, error) {
		p, _ := authn.From(ctx)
		var all []Member
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			ms, err := u.Members().List(ctx, a.deps.Clock.Now())
			for _, m := range ms {
				all = append(all, memberOf(m))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(m Member) string { return m.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &memberListOutput{Body: MemberPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.ListMemberInvitations, huma.Operation{
		OperationID: "listMemberInvitations", Method: http.MethodGet, Path: "/v1/members/invitations", Tags: []string{"Members"},
		Summary:     "List the pending invitations",
		Description: "The invitations neither accepted, withdrawn nor expired, oldest first, without their codes (FR-ADM-01). Read-only; no side effects.",
	}, []string{CodeCursor}, func(ctx context.Context, in *invitationListInput) (*invitationListOutput, error) {
		p, _ := authn.From(ctx)
		var all []MemberInvitation
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			is, err := u.MemberInvitations().Pending(ctx, a.deps.Clock.Now())
			for _, i := range is {
				all = append(all, invitationOf(i))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(i MemberInvitation) string { return i.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &invitationListOutput{Body: MemberInvitationPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.CreateMemberInvitation, oneTimeSecret(pendsApproval(a, huma.Operation{
		OperationID: "createMemberInvitation", Method: http.MethodPost, Path: "/v1/members/invitations", Tags: []string{"Members"},
		DefaultStatus: http.StatusCreated, Summary: "Invite someone to the organisation",
		Description: "Makes a one-time invitation that gives its redeemer the roles named, valid 7 days (FR-ADM-01). Give the code in " +
			"person or on a call; invitation emails arrive later. A role change falls under FM-1: until the organisation has a second " +
			"member its Owner acts alone (the founder bootstrap, rev 0.35); after that the answer is 202 with an approval request and the " +
			"code, which works once another Owner approves the invitation (rev 0.36); asking needs a person signed in who passed step-up " +
			"in the last 5 minutes. An invitation made before keeps its roles. Side effects: the invitation, or the approval request, and " +
			"its audit event." + noKey,
	}, reflect.TypeFor[MemberInvitationCode]())), append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeMembership},
		append(append(append(locks, approvalCodes...), MemberCodes...), team.CodeConflict)...),
		func(ctx context.Context, in *invitationCreateInput) (*invitationCodeOutput, error) {
			p, _ := authn.From(ctx)
			now := a.deps.Clock.Now()
			b := in.Body
			roles := roleNames(b.Roles)
			name := strings.TrimSpace(b.DisplayName)
			if bad := memberProblems(append(member.CheckName(name), member.CheckRoles(roles, b.RolesExpireAt, now)...)); len(bad) > 0 {
				return nil, invalid(bad)
			}
			code, hash := setupcode.New()
			out := &invitationCodeOutput{Status: http.StatusCreated}
			err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				wait, err := needsApproval(ctx, u)
				if err != nil {
					return err
				}
				if err := invitationConflict(ctx, u, name, roles); err != nil {
					return err
				}
				if wait {
					expires := ""
					if b.RolesExpireAt != nil {
						expires = b.RolesExpireAt.UTC().Format(time.RFC3339)
					}
					rs := make([]any, len(roles))
					for i, r := range roles {
						rs[i] = r
					}
					v, err := a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectInvitation, ObjectKey: hex.EncodeToString(hash),
						Operation: approval.OpInvite, Summary: "Invite " + name + " as " + strings.Join(roles, ", "),
						Execute: map[string]any{"display_name": name, "roles": rs, "roles_expire_at": expires, "code_hash": hex.EncodeToString(hash)},
						Diff:    []ApprovalDiff{{Field: "display_name", After: name}, {Field: "roles", After: strings.Join(roles, ", ")}}})
					v.InvitationCode = code
					out.Status, out.Body = http.StatusAccepted, v
					return err
				}
				inv, err := u.MemberInvitations().Create(ctx, storage.NewMemberInvitation{CodeHash: hash, DisplayName: name, Roles: roles,
					RolesExpireAt: b.RolesExpireAt, ExpiresAt: now.Add(InvitationTTL)})
				if err != nil {
					return err
				}
				out.Body = MemberInvitationCode{MemberInvitation: invitationOf(inv), Code: code}
				return a.logBootstrap(ctx, u, approval.ObjectInvitation, inv.ID.String(), "Invite "+name+" as "+strings.Join(roles, ", "))
			})
			if err != nil {
				return nil, err
			}
			return out, nil
		})

	register(a, authz.WithdrawMemberInvitation, huma.Operation{
		OperationID: "withdrawMemberInvitation", Method: http.MethodDelete, Path: "/v1/members/invitations/{invitation_id}", Tags: []string{"Members"},
		Summary: "Withdraw an invitation",
		Description: "Ends an unused invitation: its code stops working (FR-ADM-01). Withdrawing grants nothing, so it is never held back " +
			"for a second approver. Side effects: the withdrawal and its audit event. Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *invitationWithdrawInput) (*invitationWriteOutput, error) {
		id, err := parseID("invitation_id", in.InvitationID)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (MemberInvitation, error) {
			inv, err := u.MemberInvitations().Withdraw(ctx, id, a.deps.Clock.Now())
			if errors.Is(err, ports.ErrNotFound) {
				return MemberInvitation{}, problem(CodeNotFound, "No unused invitation with that ID exists in this organisation.",
					"List the pending invitations; an accepted, withdrawn or expired one needs nothing")
			}
			return invitationOf(inv), err
		})
		if err != nil {
			return nil, err
		}
		return &invitationWriteOutput{Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.AssignRole, pendsApproval(a, huma.Operation{
		OperationID: "assignRole", Method: http.MethodPost, Path: "/v1/members/{member_id}/roles", Tags: []string{"Members"},
		DefaultStatus: http.StatusCreated, Summary: "Give a member a role",
		Description: "Gives a member a role, theirs until it is revoked or, for an external user, until it expires (FR-ADM-01). A role " +
			"change falls under FM-1: until the organisation has a second member its Owner acts alone, their own roles included (the " +
			"founder bootstrap, rev 0.35); after that the answer is 202 with an approval request, and another Owner's approval makes the " +
			"change, if the member's roles did not change meanwhile (rev 0.36); asking needs a person signed in who passed step-up in the " +
			"last 5 minutes. Side effects: the role and its audit event, or the approval request and its audit event; the member's next " +
			"request holds the role, and their OAuth tokens are revoked (AI-38). Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, reflect.TypeFor[Member]()), append([]string{CodeNotFound, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, storage.CodeMembership}, append(append(append(locks, approvalCodes...), MemberCodes...), team.CodeConflict, team.CodeOverride)...),
		func(ctx context.Context, in *roleAssignInput) (*memberWriteOutput, error) {
			id, err := parseID("member_id", in.MemberID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
				now := a.deps.Clock.Now()
				role := string(in.Body.Role)
				if bad := memberProblems(member.CheckRoles([]string{role}, in.Body.ExpiresAt, now)); len(bad) > 0 {
					for i := range bad {
						bad[i].Field = strings.Replace(bad[i].Field, "body.roles[0]", "body.role", 1)
						bad[i].Field = strings.Replace(bad[i].Field, "body.roles_expire_at", "body.expires_at", 1)
					}
					return nil, invalid(bad)
				}
				wait, err := needsApproval(ctx, u)
				if err != nil {
					return nil, err
				}
				m, err := findMember(ctx, u, id, now)
				if err != nil {
					return nil, err
				}
				mode, _, _, err := teamModeOf(ctx, u)
				if err != nil {
					return nil, err
				}
				override, err := roleChangeConflict(mode, m, append(memberRoles(m), role), in.Body.SoDOverrideReason)
				if err != nil {
					return nil, err
				}
				if !wait {
					if override != nil {
						return nil, problem(team.CodeConflict, "An override needs a second approver, and the organisation has one active member.",
							"Choose a role that does not conflict, or invite another member first")
					}
					mm, err := a.assignRoleNow(ctx, u, id, role, in.Body.ExpiresAt)
					if err != nil {
						return nil, err
					}
					return mm, a.logBootstrap(ctx, u, approval.ObjectMembership, id.String(), "Give "+m.DisplayName+" the role "+role)
				}
				if slices.ContainsFunc(m.Roles, func(r storage.RoleAssignment) bool { return r.Role == role }) {
					return nil, problem(member.CodeHeld, m.DisplayName+" holds the role "+role+" already.", "Nothing to do; revoke it first to change its expiry")
				}
				expires := ""
				if in.Body.ExpiresAt != nil {
					expires = in.Body.ExpiresAt.UTC().Format(time.RFC3339)
				}
				after := strings.Trim(liveRoles(m)+", "+role, ", ")
				execute := map[string]any{"member_id": id.String(), "role": role, "expires_at": expires}
				diff := []ApprovalDiff{{Field: "roles", Before: liveRoles(m), After: after}}
				summary := "Give " + m.DisplayName + " the role " + role
				if override != nil {
					execute["sod_override"] = override
					diff = append(diff, ApprovalDiff{Field: "sod_override", After: fmt.Sprintf("%s: %s", override["conflict"], override["reason"])})
					summary += fmt.Sprintf(", overriding %s", override["conflict"])
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectMembership, ObjectKey: id.String(), Operation: approval.OpRoleAssign,
					Summary: summary, Execute: execute, Diff: diff})
			})
			if err != nil {
				return nil, err
			}
			return &memberWriteOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.RevokeRole, pendsApproval(a, huma.Operation{
		OperationID: "revokeRole", Method: http.MethodDelete, Path: "/v1/members/{member_id}/roles/{role}", Tags: []string{"Members"},
		Summary: "End a member's role",
		Description: "Ends a member's role; their next request no longer holds it (FR-ADM-01). The organisation always keeps an Owner. A " +
			"role change falls under FM-1: until the organisation has a second member its Owner acts alone (the founder bootstrap, rev " +
			"0.35); after that the answer is 202 with an approval request, and another Owner's approval makes the change, if the member's " +
			"roles did not change meanwhile (rev 0.36); asking needs a person signed in who passed step-up in the last 5 minutes. Side " +
			"effects: the revocation and its audit event, or the approval request and its audit event, and the member's OAuth tokens are " +
			"revoked (AI-38). Prefer: return=preview checks it and saves nothing.",
	}, reflect.TypeFor[Member]()), append([]string{CodeNotFound, storage.CodeMembership}, append(append(locks, approvalCodes...), MemberCodes...)...),
		func(ctx context.Context, in *roleRevokeInput) (*memberWriteOutput, error) {
			id, err := parseID("member_id", in.MemberID)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, "", in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (any, error) {
				wait, err := needsApproval(ctx, u)
				if err != nil {
					return nil, err
				}
				if !wait {
					mm, err := a.revokeRoleNow(ctx, u, id, in.Role)
					if err != nil {
						return nil, err
					}
					return mm, a.logBootstrap(ctx, u, approval.ObjectMembership, id.String(), "End "+mm.DisplayName+"'s role "+in.Role)
				}
				m, err := findMember(ctx, u, id, a.deps.Clock.Now())
				if err != nil {
					return nil, err
				}
				if !slices.ContainsFunc(m.Roles, func(r storage.RoleAssignment) bool { return r.Role == in.Role }) {
					return nil, problem(member.CodeHeld, m.DisplayName+" does not hold the role "+in.Role+".", "List the members to see their roles")
				}
				var after []string
				for _, r := range m.Roles {
					if r.Role != in.Role {
						after = append(after, r.Role)
					}
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectMembership, ObjectKey: id.String(), Operation: approval.OpRoleRevoke,
					Summary: "End " + m.DisplayName + "'s role " + in.Role,
					Execute: map[string]any{"member_id": id.String(), "role": in.Role},
					Diff:    []ApprovalDiff{{Field: "roles", Before: liveRoles(m), After: strings.Join(after, ", ")}}})
			})
			if err != nil {
				return nil, err
			}
			return &memberWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.AcceptMemberInvitation, exempt(exempt(huma.Operation{
		OperationID: "acceptMemberInvitation", Method: http.MethodPost, Path: "/v1/members", Tags: []string{"Members"},
		DefaultStatus: http.StatusCreated, Summary: "Join an organisation with an invitation",
		Description: "Redeems an invitation: you (a new user, or the one your sign-in belongs to) become a member of the organisation that " +
			"made it, with its roles; your session is in that organisation from your next request (FR-ADM-01). Needs a person signed in. " +
			"Side effects: your user and sign-in identity when new, the membership, the roles and their audit events. A wrong, used, " +
			"withdrawn or expired code, or an organisation you belong to already, changes nothing." + noKey,
	}, "Idempotency-Key", "Its invitation code makes it one-time: sent again, it is refused as used; and the caller has no organisation yet to hold a key in (FR-INT-08, rev 0.35)."),
		"Prefer", "A preview would confirm an invitation code without spending it (FR-INT-14, rev 0.28)."),
		[]string{CodeCodeRefused, CodeSessionRequired, CodeUnavailable, CodeMalformed, CodeTooLarge, CodeMediaType},
		func(ctx context.Context, in *joinInput) (*joinOutput, error) {
			if a.deps.Installation == nil {
				return nil, problem(CodeUnavailable, "The installation's database is not configured.", "Configure the database, then try again.")
			}
			p, err := signedInPerson(ctx, "Joining an organisation")
			if err != nil {
				return nil, err
			}
			name := strings.TrimSpace(in.Body.DisplayName)
			if bad := memberProblems(member.CheckName(name)); len(bad) > 0 {
				return nil, invalid(bad)
			}
			j, err := a.deps.Installation.JoinOrganisation(ctx, storage.NewJoin{CodeHash: setupcode.Hash(in.Body.InvitationCode), DisplayName: name,
				Issuer: p.Issuer, Subject: p.Subject, Now: a.deps.Clock.Now()})
			if storage.CodeOf(err) == storage.CodeGrant {
				a.deps.Log.Warn("api.member.code_refused", "operation", "acceptMemberInvitation", "request_id", requestID(ctx))
				return nil, codeRefused()
			}
			if err != nil {
				return nil, err
			}
			out := Joined{OrganisationID: j.OrganisationID.String(), UserID: j.UserID.String(), MemberID: j.MembershipID.String(), Roles: []RoleName{}}
			for _, r := range j.Roles {
				out.Roles = append(out.Roles, RoleName(r))
			}
			return &joinOutput{Body: out}, nil
		})
}
