package storage

import (
	"context"
	"encoding/json"
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
	NemuraOrigin         string // where Nemura's web app is served from: a passkey assertion made there names it
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

// PeopleLinkKeys are the public half of Fyniq's credentials for the link, and the running number of what Fyniq sent: the ID of Fyniq's client at Nemura's OAuth server (chosen by Nemura
// when it proposed the link), Fyniq's Ed25519 key for what it sends and the public half of its ES256 client key as a JWK, which Nemura registers.
type PeopleLinkKeys struct {
	LinkID         uuid.UUID
	NemuraClientID string
	LinkPublic     []byte
	ClientPublic   json.RawMessage
	Sequence       int64
}

// PeopleLinkSecrets are the private halves: the Ed25519 key that signs the link's events and the ES256 key (PKCS #8, DER) that signs the token assertion. Only the worker's role reads them.
type PeopleLinkSecrets struct {
	LinkID                     uuid.UUID
	LinkPrivate, ClientPrivate []byte
}

// PeopleLinkAskKey is the credential of the ask at release: the ID Nemura gave Fyniq's read-only client (it chose it at its proposal), and its ES256 key, whose public half Nemura registers. Unlike the
// events' secrets this private half is the API's to read (the API makes the ask) and the worker's not.
type PeopleLinkAskKey struct {
	LinkID     uuid.UUID
	ClientID   string
	Public     json.RawMessage
	PrivateDER []byte
}

// PeopleAccess is what Nemura last said of a sign-in identity (identity.access.changed): whether it is an active member there and whether it holds a role that approves a pay run. A door and a hint, never a grant.
type PeopleAccess struct {
	ID                            uuid.UUID
	Issuer, Subject               string
	PeopleAccess, PayrollApprover bool
	Version                       int64 // the sending event's sequence: a newer one replaces an older
	Epoch                         int   // the link's epoch it came in: a sequence starts again with each link, so only the live link's epoch counts
	ReceivedAt                    time.Time
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
	// InboxSequences are the sequence numbers of Nemura's events the inbox holds between two numbers, in order: what a gap is found against (FR-PPL-08, FR-PPL-15).
	InboxSequences(ctx context.Context, link uuid.UUID, from, to int64) ([]int64, error)
	// RecordReplay keeps that Fyniq asked Nemura to send a run of sequence numbers again, audited as people_link_replay.asked (the run, never a payload).
	RecordReplay(ctx context.Context, link uuid.UUID, from, to int64, at time.Time) error
	// ReplaysSince are the runs Fyniq asked for after a moment: what the link's health calls asked for and not missing.
	ReplaysSince(ctx context.Context, link uuid.UUID, since time.Time) ([][2]int64, error)
	// InboxCounts says how many events of each outcome the link received (FR-PPL-08).
	InboxCounts(ctx context.Context, link uuid.UUID) ([]InboxCount, error)
	// Person is a person of the projection, locked for the change that follows; the zero Person, not Known, when unknown.
	Person(ctx context.Context, id uuid.UUID) (peoplelink.Person, error)
	// People are the projection's people, by name.
	People(ctx context.Context) ([]peoplelink.Person, error)
	// PersonOfUser is the person a Fyniq user was mapped to, or ports.ErrNotFound.
	PersonOfUser(ctx context.Context, user uuid.UUID) (peoplelink.Person, error)
	// SaveKeys keeps the credentials made when the link was approved, audited as people_link.keys_made (that keys exist, never a key).
	SaveKeys(ctx context.Context, k PeopleLinkKeys, s PeopleLinkSecrets) error
	// Keys are the link's public credentials and its sequence, or ports.ErrNotFound for a link made before there were keys.
	Keys(ctx context.Context, link uuid.UUID) (PeopleLinkKeys, error)
	// NextSequence takes the next number of what Fyniq sends on the link, under the row's lock, so events leave in the order they were committed.
	NextSequence(ctx context.Context, link uuid.UUID) (int64, error)
	// Secrets are the link's private keys: the worker's role alone can read them (the API's refused by the database).
	Secrets(ctx context.Context, link uuid.UUID) (PeopleLinkSecrets, error)
	// SaveAskKey keeps the ask's credential made when the link was approved, audited as people_link.ask_key_made (that it exists and which client it is for, never a key).
	SaveAskKey(ctx context.Context, k PeopleLinkAskKey) error
	// AskKey is the live link's ask credential, or ports.ErrNotFound for a link made without one. The API's role reads it; the worker's does not.
	AskKey(ctx context.Context, link uuid.UUID) (PeopleLinkAskKey, error)
	// Access is what Nemura last said of an identity, or ports.ErrNotFound when it has said nothing.
	Access(ctx context.Context, issuer, subject string) (PeopleAccess, error)
	// AccessList is everything Nemura said of identities, oldest first.
	AccessList(ctx context.Context) ([]PeopleAccess, error)
	// SaveAccess keeps what Nemura said of an identity when its version is newer than the one held: false when it is not (nothing changed). Audited as people_link_access.changed with the answers and never the identity.
	SaveAccess(ctx context.Context, a PeopleAccess) (bool, error)
	// SavePerson writes a person's new state, audited as person_projection.changed with the names of the fields that changed, never
	// their values. CodePeopleLink when it goes back to an older version.
	SavePerson(ctx context.Context, p peoplelink.Person) error
}
