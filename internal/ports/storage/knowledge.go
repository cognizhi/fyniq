package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// KBReader is who asks the knowledge base: the roles the person holds, as the user guide names them, and the data classes
// their rights let them read (PRD-001 AI-54; p1-knowledge-base design D2). The caller works both out from the person's
// live rights; the database applies them in the query, so a page the reader may not open is not there for them at all.
type KBReader struct {
	Roles   []string
	Classes []string
}

// KBPlatformInfo is the release whose pages are loaded.
type KBPlatformInfo struct {
	Version  string
	Schema   string
	Digest   string
	LoadedAt time.Time
}

// KBEntry is one row of an index: what a page is, in a line.
type KBEntry struct {
	Key       string
	Type      string
	Title     string
	Summary   string
	Status    string
	DataClass string
}

// KBPlatformPage is a platform page as the reader may open it, with the version an answer records.
type KBPlatformPage struct {
	KBEntry
	Aliases    []string
	Body       string
	Sources    []byte // JSON: [{"path":…, "sha256":…}]
	Taint      string
	CompiledBy string
	CompiledAt time.Time
	Digest     string // the page file's SHA-256
	Release    string // the release that shipped it
}

// KBGuideRef is a guide page a compiled page cites: what a citation resolves to, never the compiled page.
type KBGuideRef struct {
	File  string
	Key   string
	Title string
	Draft bool
}

// KBSearch is a full-text search of the knowledge base (AI-58).
type KBSearch struct {
	Terms  []string
	Scope  string // pages, sources or both
	Limit  int
	Reader KBReader
}

// KBHit is one hit: a compiled page, or a raw guide page, the reader may open.
type KBHit struct {
	Kind    string // page or source
	Key     string
	Title   string
	Summary string
	Snippet string
	Rank    float32
	Draft   bool
}

// KnowledgeBase reads the platform knowledge base the release loaded: global tables the services reach only through the
// reader filter (migration 000560), so every call applies the reader's roles and data classes.
type KnowledgeBase interface {
	// PlatformInfo is the loaded release, or false when none is loaded.
	PlatformInfo(ctx context.Context) (KBPlatformInfo, bool, error)
	// PlatformIndex is the pages the reader may open, by type and key.
	PlatformIndex(ctx context.Context, r KBReader) ([]KBEntry, error)
	// PlatformPage is a page, or false when there is none the reader may open.
	PlatformPage(ctx context.Context, key string, r KBReader) (KBPlatformPage, bool, error)
	// PlatformGuides are the guide pages the page cites; none for a page the reader may not open.
	PlatformGuides(ctx context.Context, key string, r KBReader) ([]KBGuideRef, error)
	// PlatformSearch is ranked full-text search over pages, their aliases and the raw guide.
	PlatformSearch(ctx context.Context, s KBSearch) ([]KBHit, error)
}

// CodeKnowledge is the database's refusal of a change to the organisation's knowledge base (FY561): a version, a snapshot
// or a snapshot's pages changed or removed, a note changed other than by its next version, or a finding resolved twice
// (migration 000563).
const CodeKnowledge = "STORAGE-KNOWLEDGE-01"

// The origins of an organisation page's version (AI-56).
const (
	KBOriginRendered    = "rendered"     // from settings, with no model
	KBOriginCompiled    = "compiled"     // by the compiler, as a draft
	KBOriginHumanEdited = "human_edited" // by a person; a recompile never overwrites it
	KBOriginOverlay     = "overlay"      // a person's edit of a platform page, for this organisation only
	KBOriginRollback    = "rollback"     // a new version restoring an old one's content
)

// KBNote is a note, policy or decision an administrator wrote: the source an organisation page is compiled from.
type KBNote struct {
	ID        uuid.UUID
	Key       string
	Kind      string
	Title     string
	Body      string
	DataClass string
	Version   int
	CreatedBy uuid.UUID
	CreatedAt time.Time
	UpdatedBy uuid.UUID
	UpdatedAt time.Time
}

// KBVersion is one version of an organisation page. A version is published when the newest snapshot lists it, and a
// draft when it is newer than the published one.
type KBVersion struct {
	ID            uuid.UUID
	Key           string
	Number        int
	Type          string
	Title         string
	Summary       string
	Aliases       []string
	Body          string
	Sources       json.RawMessage // [{kind, ref, sha256}]
	DataClass     string
	Origin        string
	HumanEdited   bool
	BaseKey       string // an overlay: the platform page it replaces
	BaseDigest    string
	Restores      int // a rollback: the version whose content it restores
	Compiler      string
	PromptVersion string
	AuthorKind    string // person, compiler or system
	AuthorUser    uuid.UUID
	CreatedAt     time.Time
}

// KBSnapshot is a numbered snapshot of what is published.
type KBSnapshot struct {
	ID         uuid.UUID
	Number     int
	ApprovalID *uuid.UUID
	CreatedBy  *uuid.UUID
	Reason     string
	CreatedAt  time.Time
}

// KBLivePage is an organisation page as published, with the snapshot that published it.
type KBLivePage struct {
	KBEntry
	Aliases    []string
	Body       string
	Sources    json.RawMessage
	Origin     string
	Number     int
	BaseKey    string
	BaseDigest string
	Snapshot   int
}

// OrganisationKnowledge is the organisation's own knowledge base (migration 000563): its notes, the versions of its pages,
// the snapshots that publish them and the pages as published, read through the reader's data classes.
type OrganisationKnowledge interface {
	// Note is the note with key, or ports.ErrNotFound.
	Note(ctx context.Context, key string) (KBNote, error)
	Notes(ctx context.Context) ([]KBNote, error)
	// SaveNote makes a note (version 0) or changes it, expecting the version the caller read; a changed note is CodeVersion.
	SaveNote(ctx context.Context, n KBNote, expected int) (KBNote, error)
	// AddVersion records the next version of a page, as written by who it names. Audited.
	AddVersion(ctx context.Context, v KBVersion) (KBVersion, error)
	// Versions are a page's versions, newest first; Latest the newest of every page.
	Versions(ctx context.Context, key string) ([]KBVersion, error)
	Latest(ctx context.Context) ([]KBVersion, error)
	// Published are the live snapshot's page versions, by key (nothing before the first snapshot).
	Published(ctx context.Context) (map[string]int, error)
	// Publish makes the next snapshot: the live pages with these versions in their place. Audited.
	Publish(ctx context.Context, versions map[string]int, approval, by *uuid.UUID, reason string, now time.Time) (KBSnapshot, error)
	Snapshots(ctx context.Context, limit int) ([]KBSnapshot, error)
	// LiveIndex, LivePage and Search read what is published, only in the data classes the reader may read (AI-54).
	LiveIndex(ctx context.Context, classes []string) ([]KBLivePage, error)
	LivePage(ctx context.Context, key string, classes []string) (KBLivePage, bool, error)
	Search(ctx context.Context, terms, classes []string, limit int) ([]KBHit, error)
}
