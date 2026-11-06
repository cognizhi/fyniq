package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// BrandFile is one of a brand version's files: its media type, the SHA-256 it is kept under in the blob store, and its
// size in bytes.
type BrandFile struct {
	MediaType string
	SHA256    []byte
	Size      int
}

// BrandVersion is a version of the organisation's logo and brand (FR-ORG-09, rev 0.200): the logo for light and dark
// backgrounds, the square icon (each optional) and the accent colour (empty: the design's default). Print is the PNG
// the render service drew from the light logo for documents and emails (rev 0.201); nil without a light logo, or
// when no render service drew it. Cleaned records, per file kind, what the SVG sanitiser removed. Versions are never
// changed: a change is the next version.
type BrandVersion struct {
	ID                uuid.UUID
	Version           int
	Light, Dark, Icon *BrandFile
	Print             *BrandFile
	Accent            string
	Cleaned           []byte // JSON: {"light": {"elements": {...}, "attributes": n, "external": n}, ...}
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
}

// Brand keeps the organisation's brand versions.
type Brand interface {
	// Versions are every version, newest first.
	Versions(ctx context.Context) ([]BrandVersion, error)
	// Save records b as the version after the latest, by by: CodeVersion when another version came first.
	Save(ctx context.Context, by uuid.UUID, b BrandVersion, at time.Time) (BrandVersion, error)
}
