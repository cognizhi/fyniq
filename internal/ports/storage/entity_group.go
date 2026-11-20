package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// GroupEntity is the parent's row for an entity it invited (FR-ENT-04): invited until the entity's Owner accepts, active while both consent, ended after.
type GroupEntity struct {
	ID             uuid.UUID
	Label          string
	EntityOrg      uuid.UUID // uuid.Nil until accepted
	EntityName     string    // the entity's name and base currency when it accepted
	EntityCurrency string
	State          string // invited, active or ended
	InvitedBy      uuid.UUID
	InvitedAt      time.Time
	LinkedBy       uuid.UUID
	LinkedAt       time.Time // zero until accepted
	EndedBy        uuid.UUID
	EndedAt        time.Time // zero until ended
	EndedReason    string
	Version        int
}

// GroupMembership is an entity's consent row: which group it belongs to and what the group may read.
type GroupMembership struct {
	ID          uuid.UUID
	ParentOrg   uuid.UUID
	ParentName  string
	ParentRow   uuid.UUID
	Scopes      []string // figures, intercompany
	State       string   // active or ended
	AcceptedBy  uuid.UUID
	AcceptedAt  time.Time
	EndedBy     uuid.UUID
	EndedAt     time.Time
	EndedReason string
	Version     int
}

// NewGroupInvitation is what EntityGroups.Invite needs: a label for the entity, the hash of the one-time code (SHA-256, 32 bytes) and when it expires (at most 24 hours on).
type NewGroupInvitation struct {
	Label     string
	CodeHash  []byte
	ExpiresAt time.Time
	By        uuid.UUID
	At        time.Time
}

// EndedLink is what ending a link changed: the caller's role in it (parent or entity) and the two rows.
type EndedLink struct {
	Role         string
	EntityRow    uuid.UUID // the parent's group_entity row
	MembershipID uuid.UUID // the entity's group_membership row
}

// EntityFigureLine is one account of an entity in one month: the net base amount, a debit positive.
type EntityFigureLine struct {
	Code, Name, Type, Currency string
	Month                      time.Time // the month's first day
	Net                        Decimal
}

// EntityPeriod is the state of one of the entity's periods.
type EntityPeriod struct {
	Month  time.Time
	Status string // open, soft_locked or closed
}

// EntityFigures is what a group may read of an entity, and nothing more (FR-ENT-04): its accounts' net base movements by month, the state of its periods and the ledger revision
// they were read at.
type EntityFigures struct {
	Currency string
	Revision int64
	Lines    []EntityFigureLine
	Periods  []EntityPeriod
}

// EntityGroups is the link between organisations (FR-ENT-04; p3-multi-entity D2 and D3). Every method is run by the database's own functions, which check the acting person's live Owner
// role in the unit of work's organisation and that both rows of a link are active; the unit of work writes the audit events of both organisations in the same transaction.
// Refusals carry CodeGroup (the person or the organisation may not), CodeGroupCode (a code that is wrong, used, withdrawn or old) or CodeGroupLink (no live link).
type EntityGroups interface {
	// Entities lists the entities this organisation invited as parent, oldest first.
	Entities(ctx context.Context) ([]GroupEntity, error)
	// Memberships lists this organisation's consent rows as an entity: the live one and the ended ones, oldest first.
	Memberships(ctx context.Context) ([]GroupMembership, error)
	// Invite makes an invitation row and registers its code.
	Invite(ctx context.Context, n NewGroupInvitation) (GroupEntity, error)
	// Withdraw ends an invitation nobody accepted; false when there is none open with that ID.
	Withdraw(ctx context.Context, id, by uuid.UUID, reason string, at time.Time) (bool, error)
	// Accept accepts a code in this organisation: the parent's row becomes active and this organisation's consent row is made.
	Accept(ctx context.Context, codeHash []byte, by uuid.UUID, at time.Time) (GroupMembership, error)
	// End ends the link with the other organisation, as the parent or as the entity, with a reason.
	End(ctx context.Context, by, other uuid.UUID, reason string, at time.Time) (EndedLink, error)
	// Figures is the parent's read of a linked entity through the month of through; the read is audited in both organisations' trails.
	Figures(ctx context.Context, entity uuid.UUID, through time.Time, by uuid.UUID) (EntityFigures, error)
}
