package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// NewSandbox is a sandbox organisation to make (FR-INT-18): the Super admin who makes it, the ID they chose (so that a second Super
// admin's approval names it), its name after the label "Sandbox: ", and the installation change that approved it when two Super admins must agree.
type NewSandbox struct {
	By     uuid.UUID
	Change *uuid.UUID
	ID     uuid.UUID
	Name   string // as shown, starting with "Sandbox: "
	At     time.Time
}

// Sandbox is the sandbox organisation made: its ID and its creator's membership and Owner role in it.
type Sandbox struct {
	OrganisationID, MembershipID, AssignmentID uuid.UUID
}

// Sandboxes make sandbox organisations and say whether an organisation is one (FR-INT-18). The flag lives in a registry no application role can write:
// only the database's function, run for an active Super admin (and for the other's approved installation change when there are two), writes it, in
// the statement that makes the organisation, so a real organisation never becomes a sandbox and a sandbox never becomes a real one (CodeSandbox).
type Sandboxes interface {
	// Create makes the organisation in a unit of work of the installation organisation, whose trail records it, with the organisation's own trail
	// holding its creation, its creator's membership and its Owner role.
	Create(ctx context.Context, n NewSandbox) (Sandbox, error)
	// Is says whether the unit of work's own organisation is a sandbox one. The answer never changes for an organisation, so it is read once.
	Is(ctx context.Context) (bool, error)
}
