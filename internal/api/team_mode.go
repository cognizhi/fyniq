package api

import (
	"context"
	"encoding/json"
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
	"github.com/cognizhi/fyniq/internal/domain/team"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ team modes (§6.3, §6.4, FR-ADM-04, rev 0.42)

// TeamModeConflict is a conflict of §6.3 that a member's roles, or a pending invitation's, hold together.
type TeamModeConflict struct {
	Conflict     string     `json:"conflict" doc:"The conflict (PRD-001 §6.3): SOD-6, administering users or roles and posting or approving transactions, is the one a combination of roles makes; the others are checked on each item." example:"SOD-6"`
	MemberID     string     `json:"member_id,omitempty" format:"uuid" doc:"The member's membership, for a member." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
	UserID       string     `json:"user_id,omitempty" format:"uuid" doc:"The member's user, for a member." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	InvitationID string     `json:"invitation_id,omitempty" format:"uuid" doc:"The pending invitation, for an invitation." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f20"`
	DisplayName  string     `json:"display_name" doc:"The member's or the invitee's name." example:"Wei Ling Tan"`
	Roles        []RoleName `json:"roles" doc:"The roles that make it."`
	Overridden   bool       `json:"overridden" doc:"true when an override for the team mode in force lets the member keep it." example:"false"`
}

// SoDOverride is a conflict a member keeps in a mode that blocks it, with the reason a second approver accepted.
type SoDOverride struct {
	UserID          string    `json:"user_id" format:"uuid" doc:"The member's user." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	DisplayName     string    `json:"display_name" doc:"Their name." example:"Wei Ling Tan"`
	Conflict        string    `json:"conflict" doc:"The conflict (PRD-001 §6.3)." example:"SOD-6"`
	Reason          string    `json:"reason" doc:"Why they keep it." example:"Founder who also keeps the books until we hire"`
	TeamModeVersion int       `json:"team_mode_version" doc:"The version of the team mode it is for: a later switch lists the conflict again." example:"1"`
	ApprovalID      string    `json:"approval_id" format:"uuid" doc:"The approved request that holds it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	CreatedAt       time.Time `json:"created_at" doc:"When it was recorded." example:"2026-10-20T09:30:00Z"`
}

// TeamModeVersion is one version of the organisation's team mode.
type TeamModeVersion struct {
	Version     int       `json:"version" doc:"The version, from 1." example:"1"`
	Mode        string    `json:"mode" enum:"founder,standard,enterprise" doc:"The team mode from then on." example:"standard"`
	ApprovalID  string    `json:"approval_id" format:"uuid" doc:"The approved request that made it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f30"`
	RequestedBy string    `json:"requested_by" format:"uuid" doc:"The user who asked for it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	CreatedAt   time.Time `json:"created_at" doc:"When it took effect." example:"2026-10-20T09:30:00Z"`
}

// TeamMode is the organisation's team mode with the conflicts its members hold.
type TeamMode struct {
	Mode            string             `json:"mode" enum:"founder,standard,enterprise" doc:"Founder mode allows the conflicts of §6.3 with the compensating controls FM-1 to FM-9; Standard and Enterprise mode block them (PRD-001 §6.4)." example:"founder"`
	Version         int                `json:"version" doc:"The team mode's version: 0 until the first switch; it also makes the ETag." example:"0"`
	BlocksConflicts bool               `json:"blocks_conflicts" doc:"true in a mode that blocks conflicts unless they are overridden." example:"false"`
	Conflicts       []TeamModeConflict `json:"conflicts" doc:"The conflicts the members' roles and the pending invitations' roles hold now. A switch to a mode that blocks conflicts waits until each is resolved, or overridden with a reason."`
	Overrides       []SoDOverride      `json:"overrides" doc:"The overrides for the version in force: who keeps a conflict, and why (reported, §6.4)."`
	History         []TeamModeVersion  `json:"history" doc:"Every version, oldest first; empty until the first switch."`
}

// TeamModeOverride overrides one member's conflict in a switch to a mode that blocks it.
type TeamModeOverride struct {
	MemberID string `json:"member_id" format:"uuid" doc:"The member's membership." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f01"`
	Conflict string `json:"conflict" pattern:"^SOD-([1-9]|10)$" doc:"The conflict, as the team mode lists it." example:"SOD-6"`
	Reason   string `json:"reason" maxLength:"500" doc:"Why they keep both sides of it." example:"Founder who also keeps the books until we hire"`
}

// TeamModeSwitch asks for a switch of the team mode.
type TeamModeSwitch struct {
	Mode      string             `json:"mode" enum:"founder,standard,enterprise" doc:"The team mode to switch to. Enterprise mode is not available yet (TEAM-03)." example:"standard"`
	Overrides []TeamModeOverride `json:"overrides,omitempty" maxItems:"100" doc:"For a mode that blocks conflicts: an override with a reason for each member's conflict that stays."`
}

type teamModeSwitchInput struct {
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body TeamModeSwitch
}

type teamModeReadOutput struct {
	ETag string `header:"ETag" doc:"The team mode's version, for If-Match: \"0\" until the first switch."`
	Body TeamMode
}

type teamModeSwitchOutput struct {
	Status   int
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Approval
}

// TeamCodes are the team-mode rules (internal/domain/team).
var TeamCodes = []string{team.CodeConflicts, team.CodeConflict, team.CodeUnavailable, team.CodeSame, team.CodeOverride}

// teamModeOf returns the team mode in force, its version (0 until the first switch) and every version.
func teamModeOf(ctx context.Context, u storage.UnitOfWork) (string, int, []storage.TeamMode, error) {
	all, err := u.TeamModes().List(ctx)
	if err != nil || len(all) == 0 {
		return team.Default, 0, all, err
	}
	last := all[len(all)-1]
	return last.Mode, last.Version, all, nil
}

func authzRoles(roles []string) []authz.Role {
	out := make([]authz.Role, len(roles))
	for i, r := range roles {
		out[i] = authz.Role(r)
	}
	return out
}

func roleStrings(roles []authz.Role) []string {
	out := make([]string, len(roles))
	for i, r := range roles {
		out[i] = string(r)
	}
	return out
}

// memberConflict is a member's conflict with the membership it is on.
type memberConflict struct {
	team.Conflict
	MemberID uuid.UUID
}

// conflictsOf returns the conflicts the active members' live roles and the pending invitations' roles hold now, members
// in the order they joined, then invitations oldest first.
func (a *API) conflictsOf(ctx context.Context, u storage.UnitOfWork) ([]memberConflict, error) {
	now := a.deps.Clock.Now()
	ms, err := u.Members().List(ctx, now)
	if err != nil {
		return nil, err
	}
	var out []memberConflict
	for _, m := range ms {
		for _, c := range authz.RoleConflicts(authzRoles(memberRoles(m))) {
			out = append(out, memberConflict{Conflict: team.Conflict{SoD: c.SoD, UserID: m.UserID, Name: m.DisplayName, Roles: roleStrings(c.Roles)}, MemberID: m.ID})
		}
	}
	invs, err := u.MemberInvitations().Pending(ctx, now)
	if err != nil {
		return nil, err
	}
	for _, i := range invs {
		for _, c := range authz.RoleConflicts(authzRoles(i.Roles)) {
			out = append(out, memberConflict{Conflict: team.Conflict{SoD: c.SoD, InvitationID: i.ID, Name: i.DisplayName, Roles: roleStrings(c.Roles)}})
		}
	}
	return out, nil
}

// overridden reports whether an override for version of the team mode lets user keep conflict.
func overridden(os []storage.SoDOverride, version int, user uuid.UUID, conflict string) bool {
	return slices.ContainsFunc(os, func(o storage.SoDOverride) bool {
		return o.TeamModeVersion == version && o.UserID == user && o.Conflict == conflict
	})
}

// checkConflicts refuses an action that is one side of a conflict its person's roles hold, when the team mode blocks
// conflicts and no override for the mode in force lets them keep it (TEAM-04, SOD-6 at the moment of the action).
func checkConflicts(ctx context.Context, u storage.UnitOfWork, p authn.Principal, what string) error {
	cs := authz.RoleConflicts(p.Roles)
	if len(cs) == 0 {
		return nil
	}
	mode, version, _, err := teamModeOf(ctx, u)
	if err != nil || !team.Blocks(mode) {
		return err
	}
	os, err := u.TeamModes().Overrides(ctx)
	if err != nil {
		return err
	}
	for _, c := range cs {
		if p.UserID == uuid.Nil || !overridden(os, version, p.UserID, c.SoD) {
			return problem(team.CodeAction, fmt.Sprintf("%s is one side of %s, which your roles (%s) hold together and %s mode blocks.", what, c.SoD,
				strings.Join(roleStrings(c.Roles), ", "), mode), "Ask a member who does not administer users or roles to do it, or end one of those roles")
		}
	}
	return nil
}

// roleChangeConflict checks a role change in a mode that blocks conflicts: when the member's roles after it hold a
// conflict, it needs an override with a reason, which the role change's approval makes (a second approver). It returns
// the override the change carries, or nil.
func roleChangeConflict(mode string, m storage.Member, after []string, reason string) (map[string]any, error) {
	reason = strings.TrimSpace(reason)
	cs := authz.RoleConflicts(authzRoles(after))
	if !team.Blocks(mode) || len(cs) == 0 {
		if reason != "" {
			return nil, invalid([]FieldProblem{{Code: team.CodeOverride, Field: "body.sod_override_reason",
				Message: "there is no conflict to override: the roles do not conflict, or the team mode allows it", Fix: "Leave sod_override_reason out"}})
		}
		return nil, nil
	}
	c := cs[0]
	if reason == "" {
		return nil, problem(team.CodeConflict, fmt.Sprintf("%s would hold %s (%s), which %s mode blocks.", m.DisplayName, c.SoD,
			strings.Join(roleStrings(c.Roles), ", "), mode), "Choose a role that does not conflict, or give sod_override_reason: the role change's approver accepts the override")
	}
	if bad := team.CheckReason(reason, "sod_override_reason"); len(bad) > 0 {
		return nil, invalid([]FieldProblem{{Code: bad[0].Code, Field: "body.sod_override_reason", Message: bad[0].Message, Fix: bad[0].Fix}})
	}
	return map[string]any{"user_id": m.UserID.String(), "conflict": c.SoD, "reason": reason}, nil
}

// teamChange is what a team_mode.change request executes.
type teamChange struct {
	Mode      string `json:"mode"`
	Version   int    `json:"version"`
	Overrides []struct {
		UserID   uuid.UUID `json:"user_id"`
		Conflict string    `json:"conflict"`
		Reason   string    `json:"reason"`
	} `json:"overrides"`
}

func teamChangeOf(x map[string]any) (teamChange, error) {
	var c teamChange
	b, err := json.Marshal(x)
	if err == nil {
		err = json.Unmarshal(b, &c)
	}
	return c, err
}

// switchTeamMode adds the approved version and its overrides; the database voids every pending request.
func (a *API) switchTeamMode(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, x map[string]any) error {
	c, err := teamChangeOf(x)
	if err != nil {
		return err
	}
	now := a.deps.Clock.Now()
	if _, err := u.TeamModes().Add(ctx, storage.NewTeamMode{Version: c.Version, Mode: c.Mode, RequestID: r.ID, CreatedBy: r.RequestedBy, CreatedAt: now}); err != nil {
		return err
	}
	for _, o := range c.Overrides {
		if _, err := u.TeamModes().Override(ctx, storage.NewSoDOverride{TeamModeVersion: c.Version, UserID: o.UserID, Conflict: o.Conflict,
			Reason: o.Reason, RequestID: r.ID, CreatedBy: r.RequestedBy, CreatedAt: now}); err != nil {
			return err
		}
	}
	return nil
}

// recordOverride records the override an approved role change carries, for the team mode in force.
func (a *API) recordOverride(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, x map[string]any) error {
	o, ok := x["sod_override"].(map[string]any)
	if !ok {
		return nil
	}
	_, version, _, err := teamModeOf(ctx, u)
	if err != nil {
		return err
	}
	user, err := uuid.Parse(fmt.Sprint(o["user_id"]))
	if err != nil {
		return err
	}
	_, err = u.TeamModes().Override(ctx, storage.NewSoDOverride{TeamModeVersion: version, UserID: user, Conflict: fmt.Sprint(o["conflict"]),
		Reason: fmt.Sprint(o["reason"]), RequestID: r.ID, CreatedBy: r.RequestedBy, CreatedAt: a.deps.Clock.Now()})
	return err
}

func registerTeamMode(a *API) {
	register(a, authz.GetTeamMode, huma.Operation{
		OperationID: "getTeamMode", Method: http.MethodGet, Path: "/v1/team-mode", Tags: []string{"Members"},
		Summary: "Get the team mode",
		Description: "The organisation's team mode (PRD-001 §6.4), the conflicts of §6.3 its members' roles and pending invitations hold now, " +
			"the overrides that let members keep them in a mode that blocks conflicts, and every version. Founder mode until the first switch. " +
			"Its ETag is the version. Read-only; no side effects.",
	}, nil, func(ctx context.Context, _ *struct{}) (*teamModeReadOutput, error) {
		p, _ := authn.From(ctx)
		var out teamModeReadOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			mode, version, history, err := teamModeOf(ctx, u)
			if err != nil {
				return err
			}
			cs, err := a.conflictsOf(ctx, u)
			if err != nil {
				return err
			}
			os, err := u.TeamModes().Overrides(ctx)
			if err != nil {
				return err
			}
			names, err := memberNames(ctx, u, a.deps.Clock.Now())
			if err != nil {
				return err
			}
			t := TeamMode{Mode: mode, Version: version, BlocksConflicts: team.Blocks(mode), Conflicts: []TeamModeConflict{},
				Overrides: []SoDOverride{}, History: []TeamModeVersion{}}
			for _, c := range cs {
				v := TeamModeConflict{Conflict: c.SoD, DisplayName: c.Name, Roles: []RoleName{}}
				for _, r := range c.Roles {
					v.Roles = append(v.Roles, RoleName(r))
				}
				if c.InvitationID != uuid.Nil {
					v.InvitationID = c.InvitationID.String()
				} else {
					v.MemberID, v.UserID = c.MemberID.String(), c.UserID.String()
					v.Overridden = version > 0 && overridden(os, version, c.UserID, c.SoD)
				}
				t.Conflicts = append(t.Conflicts, v)
			}
			for _, o := range os {
				if o.TeamModeVersion == version {
					t.Overrides = append(t.Overrides, SoDOverride{UserID: o.UserID.String(), DisplayName: names[o.UserID], Conflict: o.Conflict,
						Reason: o.Reason, TeamModeVersion: o.TeamModeVersion, ApprovalID: o.RequestID.String(), CreatedAt: o.CreatedAt.UTC()})
				}
			}
			for _, h := range history {
				t.History = append(t.History, TeamModeVersion{Version: h.Version, Mode: h.Mode, ApprovalID: h.RequestID.String(),
					RequestedBy: h.CreatedBy.String(), CreatedAt: h.CreatedAt.UTC()})
			}
			out.Body, out.ETag = t, etag(version)
			return nil
		})
		return &out, err
	})

	register(a, authz.SwitchTeamMode, huma.Operation{
		OperationID: "switchTeamMode", Method: http.MethodPatch, Path: "/v1/team-mode", Tags: []string{"Members"},
		Summary: "Ask to switch the team mode", DefaultStatus: http.StatusAccepted,
		Description: "Asks for a switch of the team mode (PRD-001 §6.4, FR-ADM-04). The switch is never made at once: the answer is 202 with an " +
			"approval request, which a member who holds A on \"Users, roles, team mode\" (an Owner) and is not the requester approves (dual " +
			"control). A switch to a mode that blocks conflicts (Standard) lists every member whose roles conflict and waits until each is " +
			"resolved or overridden: send an override with a reason for each conflict that stays (TEAM-01); a pending invitation whose roles " +
			"conflict is withdrawn first. Enterprise mode is not available yet (TEAM-03). The approval records the overrides, which are " +
			"reported with the team mode, and every pending approval request of the organisation becomes void, since each was asked for " +
			"under the old mode. Asking needs a person signed in who passed step-up in the last 5 minutes and If-Match with the team mode's " +
			"ETag. Side effects: the approval request and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it " +
			"and saves nothing.",
	}, append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeIdempotencyReused, CodeIdempotencyBusy,
		CodeIdempotencyOld, CodeInvalid, storage.CodeVersion, storage.CodeRetry, storage.CodeLockTimeout, storage.CodeApproval,
		CodeSessionRequired, CodeStepUpRequired, approval.CodePending}, TeamCodes...),
		func(ctx context.Context, in *teamModeSwitchInput) (*teamModeSwitchOutput, error) {
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				// ENG-47: the members' lock first, so no role changes while the conflicts are read.
				if _, err := u.Members().Count(ctx); err != nil {
					return Approval{}, err
				}
				mode, version, _, err := teamModeOf(ctx, u)
				if err != nil {
					return Approval{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(version)); err != nil {
					return Approval{}, err
				}
				var bad []FieldProblem
				for _, x := range team.CheckMode(mode, in.Body.Mode) {
					bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
				}
				if len(bad) > 0 {
					if bad[0].Code == team.CodeSame {
						return Approval{}, problem(team.CodeSame, "The organisation is in "+mode+" mode already.", "Nothing to do")
					}
					return Approval{}, invalid(bad)
				}
				cs, err := a.conflictsOf(ctx, u)
				if err != nil {
					return Approval{}, err
				}
				var overrides []team.Override
				for i, o := range in.Body.Overrides {
					j := slices.IndexFunc(cs, func(c memberConflict) bool { return c.MemberID.String() == strings.ToLower(o.MemberID) })
					if j < 0 {
						bad = append(bad, FieldProblem{Code: team.CodeOverride, Field: fmt.Sprintf("body.overrides[%d].member_id", i),
							Message: "no member with that ID holds a conflict", Fix: "Override only the conflicts the team mode lists"})
						continue
					}
					overrides = append(overrides, team.Override{UserID: cs[j].UserID, SoD: o.Conflict, Reason: strings.TrimSpace(o.Reason)})
				}
				var listed []team.Problem
				if team.Blocks(in.Body.Mode) {
					plain := make([]team.Conflict, len(cs))
					for i, c := range cs {
						plain[i] = c.Conflict
					}
					for _, x := range team.CheckOverrides(plain, overrides) {
						if x.Code == team.CodeConflicts {
							listed = append(listed, x)
							continue
						}
						bad = append(bad, FieldProblem{Code: x.Code, Field: "body." + x.Field, Message: x.Message, Fix: x.Fix})
					}
				} else if len(in.Body.Overrides) > 0 {
					bad = append(bad, FieldProblem{Code: team.CodeOverride, Field: "body.overrides", Message: in.Body.Mode + " mode blocks no conflict, so there is nothing to override",
						Fix: "Leave the overrides out"})
				}
				if len(bad) > 0 {
					return Approval{}, invalid(bad)
				}
				if len(listed) > 0 {
					var fields []FieldProblem
					for _, x := range listed {
						fields = append(fields, FieldProblem{Code: x.Code, Field: "body.mode", Message: x.Message, Fix: x.Fix})
					}
					return Approval{}, problem(team.CodeConflicts, fmt.Sprintf("%d conflict(s) block the switch to %s mode; each is listed in errors.", len(listed), in.Body.Mode),
						"Resolve each conflict, or send an override with a reason for each member's conflict that stays", fields...)
				}
				os := []any{}
				var who []string
				for _, o := range overrides {
					os = append(os, map[string]any{"user_id": o.UserID.String(), "conflict": o.SoD, "reason": o.Reason})
					i := slices.IndexFunc(cs, func(c memberConflict) bool { return c.UserID == o.UserID && c.SoD == o.SoD })
					who = append(who, cs[i].Name+" ("+o.SoD+")")
				}
				diff := []ApprovalDiff{{Field: "mode", Before: mode, After: in.Body.Mode}}
				if len(who) > 0 {
					diff = append(diff, ApprovalDiff{Field: "overrides", After: strings.Join(who, ", ")})
				}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectTeamMode, ObjectKey: approval.TeamModeKey,
					Operation: approval.OpTeamModeChange, Summary: fmt.Sprintf("Switch the team mode from %s to %s", mode, in.Body.Mode),
					Execute: map[string]any{"mode": in.Body.Mode, "version": version + 1, "overrides": os}, Diff: diff})
			})
			if err != nil {
				return nil, err
			}
			return &teamModeSwitchOutput{Status: w.Status, Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})
}
