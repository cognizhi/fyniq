package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Member is an active member of the organisation, with their name and live roles.
type Member struct {
	Membership
	DisplayName string
	Roles       []RoleAssignment // live: unrevoked and unexpired, by role
}

// MemberInvitation is an invitation to the organisation (FR-ADM-01). Its code is never stored: only its SHA-256.
// Who made it is its audit event's actor.
type MemberInvitation struct {
	ID            uuid.UUID
	DisplayName   string   // who it is for, as the inviter names them
	Roles         []string // the roles they will hold
	RolesExpireAt *time.Time
	CreatedAt     time.Time
	ExpiresAt     time.Time
	AcceptedAt    *time.Time
	WithdrawnAt   *time.Time
}

// NewMemberInvitation is what MemberInvitations.Create needs.
type NewMemberInvitation struct {
	CodeHash      []byte
	DisplayName   string
	Roles         []string
	RolesExpireAt *time.Time // for an external user: when their roles end
	ExpiresAt     time.Time
}

// MemberInvitations are the organisation's invitations. Every change is audited.
type MemberInvitations interface {
	Create(ctx context.Context, n NewMemberInvitation) (MemberInvitation, error)
	// Pending returns the invitations neither accepted, withdrawn nor expired at now, oldest first.
	Pending(ctx context.Context, now time.Time) ([]MemberInvitation, error)
	// Withdraw ends an unused invitation; ports.ErrNotFound when there is no unused one with that ID.
	Withdraw(ctx context.Context, id uuid.UUID, now time.Time) (MemberInvitation, error)
}

// NewJoin is what Installation.JoinOrganisation needs. CodeHash is the SHA-256 of the invitation's code.
type NewJoin struct {
	CodeHash        []byte
	DisplayName     string // the new user's name, when the sign-in has no user yet
	Issuer, Subject string
	Now             time.Time
}

// Joined is a redeemed invitation: the organisation joined, the user (NewUser when made for it) and their
// membership with its roles.
type Joined struct {
	OrganisationID, UserID, MembershipID uuid.UUID
	NewUser                              bool
	Roles                                []string
}
