package storage

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
)

// CodePeopleLink is the database refusing a change to the link or the projection: a link deleted, started at an epoch that is not the next,
// moved between states it may not, or a person moved to an older version (PRD-001 FR-PPL-15, FR-PPL-16; p1-payroll-link tasks 2.1, 3.2).
const CodePeopleLink = "STORAGE-PEOPLE-LINK-01"

// The states of the link (FR-PPL-16): unlinked is final, and a later link is a new row one epoch higher.
const (
	LinkActive    = "active"
	LinkSuspended = "suspended"
	LinkUnlinking = "unlinking"
	LinkUnlinked  = "unlinked"
)

// PeopleLinkRow is the organisation's link to a Nemura organisation, as Fyniq holds it.
type PeopleLinkRow struct {
	ID                   uuid.UUID
	State                string
	Epoch                int
	NemuraOrganisationID uuid.UUID
	LinkKey              []byte // Nemura's Ed25519 public key for the link's envelopes, given when the link was approved
	ClientID             uuid.UUID
	ApprovalID           uuid.UUID
	Version              int
	CreatedBy            string
	CreatedAt, ChangedAt time.Time
}

// The features of People and HR settings and where each lives (FR-PPL-01).
const (
	FeatureRecords = "records"
	FeaturePayroll = "payroll"
	FeatureLeave   = "leave"
	FeatureLetters = "letters"

	SourceNemura   = "nemura"
	SourceExternal = "external"
	SourceNone     = "none"
)

// PeopleSource is where one feature lives, from a date, and the approval that chose it.
type PeopleSource struct {
	Feature, Source string
	EffectiveFrom   time.Time
	ApprovalID      uuid.UUID
	SetAt           time.Time
}

// InboxEntry is an event received, as the inbox keeps it: identities and a digest, never the payload (FR-PPL-15).
type InboxEntry struct {
	EventID, LinkID uuid.UUID
	Epoch           int
	Sequence        int64
	Type            string
	ObjectType      string
	ObjectID        string
	ObjectVersion   int
	PayloadSHA256   string
	Outcome         string // peoplelink.OutcomeApplied, OutcomeIgnoredOlder or OutcomeRefused
	Code            string // the refusal's code, with OutcomeRefused only
	ReceivedAt      time.Time
}

// InboxCount is how many events of an outcome the link received, and when the last arrived.
type InboxCount struct {
	Outcome string
	N       int
	LastAt  time.Time
}

// PeopleLinks are the link, where each feature lives, the inbox and the employee projection. Every change is audited.
type PeopleLinks interface {
	// Live is the link that is not unlinked, or ports.ErrNotFound.
	Live(ctx context.Context) (PeopleLinkRow, error)
	// Link makes the link: active, at the epoch after the last, audited as people_link.linked. CodePeopleLink when one is live already.
	Link(ctx context.Context, l PeopleLinkRow) (PeopleLinkRow, error)
	// SetState moves the live link to a state at its version, audited as people_link.<state>. CodeVersion when it moved on; CodePeopleLink
	// for a move the database does not allow.
	SetState(ctx context.Context, id uuid.UUID, version int, state string, client uuid.UUID, at time.Time) (PeopleLinkRow, error)
	// SetSource records where a feature lives from a date, audited as people_source.set.
	SetSource(ctx context.Context, s PeopleSource) error
	// Sources are where each feature lives now: the latest row of each.
	Sources(ctx context.Context) ([]PeopleSource, error)
	// Receive records an event in the inbox: false when its ID is there already, so a repeat does nothing.
	Receive(ctx context.Context, e InboxEntry) (bool, error)
	// InboxCounts says how many events of each outcome the link received (FR-PPL-08).
	InboxCounts(ctx context.Context, link uuid.UUID) ([]InboxCount, error)
	// Person is a person of the projection, locked for the change that follows; the zero Person, not Known, when unknown.
	Person(ctx context.Context, id uuid.UUID) (peoplelink.Person, error)
	// People are the projection's people, by name.
	People(ctx context.Context) ([]peoplelink.Person, error)
	// PersonOfUser is the person a Fyniq user was mapped to, or ports.ErrNotFound.
	PersonOfUser(ctx context.Context, user uuid.UUID) (peoplelink.Person, error)
	// SavePerson writes a person's new state, audited as person_projection.changed with the names of the fields that changed, never
	// their values. CodePeopleLink when it goes back to an older version.
	SavePerson(ctx context.Context, p peoplelink.Person) error
}
