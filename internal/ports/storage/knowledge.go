package storage

import (
	"context"
	"time"
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
