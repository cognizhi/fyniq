package api

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
)

// ------------------------------------------------------------------ groups of entities (FR-ENT-04; p3-multi-entity design D2 and D3)
//
// An entity is an organisation and a group is organisations linked by the consent of both Owners: the parent's Owner invites with a one-time code, the entity's Owner
// accepts it in the entity's own organisation, and either ends the link with a reason. A link lets the parent read the entity's accounts' net monthly figures and
// nothing else, and the entity's intercompany offers addressed to it. The database's functions decide who may do what (migration 001350); this layer adds step-up, the
// idempotency key and the audit events of both organisations. Nothing here is on the AI or MCP surface.

// GroupInvitationTTL is how long an invitation's code works.
const GroupInvitationTTL = 24 * time.Hour

// GroupEntityItem is an entity of the caller's group, as the parent sees it.
type GroupEntityItem struct {
	ID                   string     `json:"id" format:"uuid" doc:"The link's ID: the entity's row in this organisation's group." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c01"`
	Label                string     `json:"label" doc:"What this organisation calls the entity." example:"Cognizhi Sdn. Bhd."`
	State                string     `json:"state" enum:"invited,active,ended" doc:"invited until the entity's Owner accepts the code; active while both Owners consent; ended after either ended it." example:"active"`
	EntityOrganisationID string     `json:"entity_organisation_id,omitempty" format:"uuid" doc:"The entity's organisation, once it accepted." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b02"`
	EntityName           string     `json:"entity_name,omitempty" doc:"The entity's name when it accepted." example:"Cognizhi Sdn. Bhd."`
	EntityCurrency       string     `json:"entity_currency,omitempty" doc:"Its base currency when it accepted." example:"MYR"`
	InvitedAt            time.Time  `json:"invited_at" doc:"When the invitation was made." example:"2026-11-21T09:00:00Z"`
	LinkedAt             *time.Time `json:"linked_at,omitempty" doc:"When the entity's Owner accepted." example:"2026-11-21T09:30:00Z"`
	EndedAt              *time.Time `json:"ended_at,omitempty" doc:"When the link ended." example:"2026-11-22T09:00:00Z"`
	EndedReason          string     `json:"ended_reason,omitempty" doc:"Why it ended, in the Owner's words." example:"Sold the subsidiary"`
	Version              int        `json:"version" doc:"The row's version." example:"2"`
}

// GroupScope is what a parent may read of an entity: figures or intercompany.
type GroupScope string

// Schema implements huma.SchemaProvider.
func (GroupScope) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Enum: []any{"figures", "intercompany"}, Description: "figures: each account's net movement by month; intercompany: offers addressed to the entity.", Examples: []any{"figures"}}
}

// GroupMembershipItem is this organisation's own consent to a group, as an entity.
type GroupMembershipItem struct {
	ID                   string       `json:"id" format:"uuid" doc:"The consent's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5d01"`
	ParentOrganisationID string       `json:"parent_organisation_id" format:"uuid" doc:"The group's parent." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b01"`
	ParentName           string       `json:"parent_name" doc:"The parent's name when this organisation accepted." example:"Cognizhi Pte Ltd"`
	Scopes               []GroupScope `json:"scopes" nullable:"false" doc:"What the parent may read: figures (each account's net movement by month, with its code, name and type, the ledger revision and the state of the periods) and intercompany (offers addressed to this organisation). Nothing else of this organisation is readable, and nothing is writable."`
	State                string       `json:"state" enum:"active,ended" doc:"active while both Owners consent." example:"active"`
	AcceptedAt           time.Time    `json:"accepted_at" doc:"When this organisation's Owner accepted." example:"2026-11-21T09:30:00Z"`
	EndedAt              *time.Time   `json:"ended_at,omitempty" doc:"When the link ended." example:"2026-11-22T09:00:00Z"`
	EndedReason          string       `json:"ended_reason,omitempty" doc:"Why it ended." example:"We left the group"`
	Version              int          `json:"version" doc:"The row's version." example:"1"`
}

// GroupView is the caller's place in a group.
type GroupView struct {
	Role        string                `json:"role" enum:"none,parent,entity" doc:"parent: this organisation has invited or holds entities and consolidates them; entity: it belongs to another's group; none: neither." example:"parent"`
	Entities    []GroupEntityItem     `json:"entities" nullable:"false" doc:"As a parent, every entity invited, oldest first, with its state."`
	Memberships []GroupMembershipItem `json:"memberships" nullable:"false" doc:"As an entity, this organisation's consent to the group, the live one and the ended ones, oldest first."`
}

// GroupInvitation is an invitation made, with its code, shown once.
type GroupInvitation struct {
	GroupEntityItem
	Code      string    `json:"code" doc:"The one-time code for the entity's Owner, valid 24 hours: give it in person or on a call; it is never shown again and never in an email." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
	ExpiresAt time.Time `json:"expires_at" doc:"When the code stops working." example:"2026-11-22T09:00:00Z"`
}

type (
	groupViewOutput  struct{ Body GroupView }
	groupInviteInput struct {
		Body struct {
			Label string `json:"label" minLength:"1" maxLength:"100" doc:"What this organisation calls the entity." example:"Cognizhi Sdn. Bhd."`
		}
	}
	groupInviteOutput struct{ Body GroupInvitation }
	GroupWithdrawPath struct {
		EntityID string `path:"entity_id" format:"uuid" doc:"The invitation's ID (the id of the entity row)." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5c01"`
	}
	groupWithdrawInput struct {
		GroupWithdrawPath
		KeyHeader
		PreferHeader
		Body GroupReason
	}
	groupAcceptInput struct {
		KeyHeader
		PreferHeader
		Body struct {
			Code string `json:"code" minLength:"10" maxLength:"60" doc:"The one-time code the parent's Owner gave, as typed: case, spaces and dashes do not matter." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
		}
	}
	groupEndInput struct {
		KeyHeader
		PreferHeader
		Body struct {
			OrganisationID string `json:"organisation_id" format:"uuid" doc:"The other organisation of the link: as the parent, the entity's; as the entity, the parent's." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5b02"`
			Reason         string `json:"reason" minLength:"1" maxLength:"500" doc:"Why the link ends, in your words; it stays in both audit trails." example:"Sold the subsidiary"`
		}
	}
	groupWithdrawnOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     GroupEntityItem
	}
	groupMembershipOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     GroupMembershipItem
	}
	groupEndedOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     GroupLinkEnded
	}
)

// GroupReason is the reason an Owner gives for ending or withdrawing.
type GroupReason struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why, in your words; it stays in the audit trail." example:"Typed the wrong entity"`
}

// GroupLinkEnded is what ending a link changed.
type GroupLinkEnded struct {
	Role  string `json:"role" enum:"parent,entity" doc:"Which side ended it: the parent or the entity." example:"entity"`
	Ended bool   `json:"ended" doc:"Always true: both rows of the link are ended, so the parent's next read is refused." example:"true"`
}

func groupEntityOf(e storage.GroupEntity) GroupEntityItem {
	out := GroupEntityItem{ID: e.ID.String(), Label: e.Label, State: e.State, EntityName: e.EntityName, EntityCurrency: e.EntityCurrency, InvitedAt: e.InvitedAt.UTC(),
		EndedReason: e.EndedReason, Version: e.Version}
	if e.EntityOrg != uuid.Nil {
		out.EntityOrganisationID = e.EntityOrg.String()
	}
	if !e.LinkedAt.IsZero() {
		t := e.LinkedAt.UTC()
		out.LinkedAt = &t
	}
	if !e.EndedAt.IsZero() {
		t := e.EndedAt.UTC()
		out.EndedAt = &t
	}
	return out
}

func groupMembershipOf(m storage.GroupMembership) GroupMembershipItem {
	out := GroupMembershipItem{ID: m.ID.String(), ParentOrganisationID: m.ParentOrg.String(), ParentName: m.ParentName, State: m.State, AcceptedAt: m.AcceptedAt.UTC(),
		EndedReason: m.EndedReason, Version: m.Version}
	for _, sc := range m.Scopes {
		out.Scopes = append(out.Scopes, GroupScope(sc))
	}
	if out.Scopes == nil {
		out.Scopes = []GroupScope{}
	}
	if !m.EndedAt.IsZero() {
		t := m.EndedAt.UTC()
		out.EndedAt = &t
	}
	return out
}

// groupOwner is the person for a write on a link: signed in to Fyniq, stepped up in the last 5 minutes (the database checks their Owner role in the organisation).
func (a *API) groupOwner(ctx context.Context, what string) (authn.Principal, error) {
	p, err := signedIn(ctx, what)
	if err != nil {
		return p, err
	}
	if p.Class != authn.ClassUser || !p.SteppedUp(a.deps.Clock.Now()) {
		return p, problem(CodeStepUpRequired, what+" needs step-up authentication in the last 5 minutes.", "Confirm it's you with your passkey or authenticator app, then try again.")
	}
	return p, nil
}

func groupReason(s string) (string, error) {
	r := strings.TrimSpace(s)
	if n := utf8.RuneCountInString(r); n < 1 || n > 500 {
		return "", invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.reason", Message: "must be 1 to 500 characters", Fix: "Say why in a sentence"}})
	}
	return r, nil
}

func registerEntityGroups(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	people := []string{CodeStepUpRequired, CodeSessionRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld}
	refusals := []string{storage.CodeGroup, storage.CodeGroupCode, storage.CodeGroupLink, CodeNotFound}

	register(a, authz.GetEntityGroup, huma.Operation{
		OperationID: "getEntityGroup", Method: http.MethodGet, Path: "/v1/group", Tags: []string{"Groups"},
		Summary: "Read this organisation's place in a group",
		Description: "As a parent, the entities it invited with their state; as an entity, its own consent to a group and what the group may read (FR-ENT-04). Only this organisation's own link rows: nothing " +
			"of any other organisation. Read-only; no side effects.",
	}, locks, func(ctx context.Context, in *struct{}) (*groupViewOutput, error) {
		p, _ := authn.From(ctx)
		out := GroupView{Role: "none", Entities: []GroupEntityItem{}, Memberships: []GroupMembershipItem{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			es, err := u.EntityGroups().Entities(ctx)
			if err != nil {
				return err
			}
			ms, err := u.EntityGroups().Memberships(ctx)
			if err != nil {
				return err
			}
			for _, e := range es {
				out.Entities = append(out.Entities, groupEntityOf(e))
				if e.State != "ended" {
					out.Role = "parent"
				}
			}
			for _, m := range ms {
				out.Memberships = append(out.Memberships, groupMembershipOf(m))
				if m.State == "active" {
					out.Role = "entity"
				}
			}
			return nil
		})
		return &groupViewOutput{Body: out}, err
	})

	register(a, authz.InviteGroupEntity, oneTimeSecret(huma.Operation{
		OperationID: "inviteGroupEntity", Method: http.MethodPost, Path: "/v1/group/entities", Tags: []string{"Groups"}, DefaultStatus: http.StatusCreated,
		Summary: "Invite an organisation to be an entity of this group",
		Description: "Makes a one-time code, valid 24 hours, that the Owner of another organisation enters in that organisation to join this one's group (FR-ENT-04). Only an Owner of this organisation, " +
			"stepped up in the last 5 minutes; refused (STORAGE-GROUP-01) for the installation's organisation, a practice or sandbox organisation, and an organisation that is itself an entity. The code is shown once, never " +
			"in an email, and only its hash is kept. Joining lets this organisation read the entity's accounts' net monthly figures and intercompany offers addressed to it, and nothing else; either Owner ends it at any time. " +
			"Side effects: the invitation row and its audit event. It takes no Idempotency-Key and no preview: its code makes it one-time (FR-INT-08).",
	}), append(append([]string{}, people...), append(refusals, locks...)...), func(ctx context.Context, in *groupInviteInput) (*groupInviteOutput, error) {
		p, err := a.groupOwner(ctx, "Inviting an entity")
		if err != nil {
			return nil, err
		}
		label := strings.TrimSpace(in.Body.Label)
		if n := utf8.RuneCountInString(label); n < 1 || n > 100 {
			return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.label", Message: "must be 1 to 100 characters", Fix: "Name the entity as your group's reports show it"}})
		}
		now := a.deps.Clock.Now()
		code, hash := setupcode.New()
		var out GroupInvitation
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			e, err := u.EntityGroups().Invite(ctx, storage.NewGroupInvitation{Label: label, CodeHash: hash, ExpiresAt: now.Add(GroupInvitationTTL), By: p.UserID, At: now})
			out = GroupInvitation{GroupEntityItem: groupEntityOf(e), Code: code, ExpiresAt: now.Add(GroupInvitationTTL).UTC()}
			return err
		})
		if err != nil {
			return nil, err
		}
		return &groupInviteOutput{Body: out}, nil
	})

	register(a, authz.WithdrawGroupInvitation, huma.Operation{
		OperationID: "withdrawGroupInvitation", Method: http.MethodPost, Path: "/v1/group/entities/{entity_id}/withdraw", Tags: []string{"Groups"},
		Summary: "Withdraw an invitation nobody accepted",
		Description: "Ends an invitation whose code was not used: the code stops working at once (FR-ENT-04). Only an Owner of this organisation, stepped up in the last 5 minutes, with a reason. An " +
			"accepted link is ended with endGroupLink instead. Side effects: the ended row and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{}, people...), append(refusals, locks...)...), func(ctx context.Context, in *groupWithdrawInput) (*groupWithdrawnOutput, error) {
		p, err := a.groupOwner(ctx, "Withdrawing an invitation")
		if err != nil {
			return nil, err
		}
		id, err := parseID("entity_id", in.EntityID)
		if err != nil {
			return nil, err
		}
		reason, err := groupReason(in.Body.Reason)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (GroupEntityItem, error) {
			done, err := u.EntityGroups().Withdraw(ctx, id, p.UserID, reason, a.deps.Clock.Now())
			if err != nil {
				return GroupEntityItem{}, err
			}
			if !done {
				return GroupEntityItem{}, problem(CodeNotFound, "There is no open invitation with that ID.", "Read the group to see its invitations")
			}
			es, err := u.EntityGroups().Entities(ctx)
			for _, e := range es {
				if e.ID == id {
					return groupEntityOf(e), err
				}
			}
			return GroupEntityItem{}, err
		})
		if err != nil {
			return nil, err
		}
		return &groupWithdrawnOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.AcceptGroupInvitation, huma.Operation{
		OperationID: "acceptGroupInvitation", Method: http.MethodPost, Path: "/v1/group/accept", Tags: []string{"Groups"}, DefaultStatus: http.StatusCreated,
		Summary: "Accept an invitation into a group, as the entity's Owner",
		Description: "Accepts the one-time code another organisation's Owner gave, in this organisation (FR-ENT-04). Only an Owner of this organisation, stepped up in the last 5 minutes. After it the group's " +
			"parent may read this organisation's accounts' net monthly figures (with the account's code, name and type, the ledger revision and the periods' state) and intercompany offers addressed to this organisation, " +
			"and nothing else: no document, contact, employee, bank, evidence, comment, audit or AI data, and no write. Every read is in both audit trails; either Owner ends the link at any time. A code that is wrong, used, " +
			"withdrawn or older than 24 hours is refused with one answer (STORAGE-GROUP-02). Refused (STORAGE-GROUP-01) for the installation's, a practice and a sandbox organisation, an organisation in a group already " +
			"and an organisation that is the parent of one. Side effects: this organisation's consent row, the parent's row made active, and audit events in both. Idempotent with Idempotency-Key; Prefer: return=preview checks it " +
			"and saves nothing.",
	}, append(append([]string{}, people...), append(refusals, locks...)...), func(ctx context.Context, in *groupAcceptInput) (*groupMembershipOutput, error) {
		p, err := a.groupOwner(ctx, "Accepting an invitation into a group")
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (GroupMembershipItem, error) {
			m, err := u.EntityGroups().Accept(ctx, setupcode.Hash(in.Body.Code), p.UserID, a.deps.Clock.Now())
			return groupMembershipOf(m), err
		})
		if err != nil {
			return nil, err
		}
		return &groupMembershipOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.EndGroupLink, huma.Operation{
		OperationID: "endGroupLink", Method: http.MethodPost, Path: "/v1/group/end", Tags: []string{"Groups"},
		Summary: "End a link of the group, as either Owner",
		Description: "Ends the link with the other organisation, as the parent or as the entity, with a reason (FR-ENT-04). Both rows end together, so the parent's next read of the entity is refused. Figures " +
			"already frozen into the parent's consolidation runs stay there, marked as link ended. Only an Owner of this organisation, stepped up in the last 5 minutes. Refused (STORAGE-GROUP-03) when no live link with that organisation exists. " +
			"Side effects: both rows and an audit event in each organisation. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{}, people...), append(refusals, locks...)...), func(ctx context.Context, in *groupEndInput) (*groupEndedOutput, error) {
		p, err := a.groupOwner(ctx, "Ending a link of the group")
		if err != nil {
			return nil, err
		}
		other, err := parseID("organisation_id", in.Body.OrganisationID)
		if err != nil {
			return nil, err
		}
		reason, err := groupReason(in.Body.Reason)
		if err != nil {
			return nil, err
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (GroupLinkEnded, error) {
			e, err := u.EntityGroups().End(ctx, p.UserID, other, reason, a.deps.Clock.Now())
			return GroupLinkEnded{Role: e.Role, Ended: err == nil}, err
		})
		if err != nil {
			return nil, err
		}
		return &groupEndedOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
