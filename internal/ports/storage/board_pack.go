package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// The board pack and the investor update (PRD-001 FR-RPT-06, FR-EQ-23; p2-reporting-planning design D4 to D6).

// CodeBoardPack is the database's refusal of a published pack changed, or any pack deleted, or a draft changed out of its course (FYR04).
const CodeBoardPack = "STORAGE-BPACK-01"

// CodeInvestorUpdate is the database's refusal of a sent update changed or any update deleted, or one sent from an unpublished pack (FYR05).
const CodeInvestorUpdate = "STORAGE-IUPD-01"

// CodeInvestorRead is the database's refusal of a read recorded for someone the update was not sent to, or changed (FYR06).
const CodeInvestorRead = "STORAGE-IUPD-02"

// CodeBoardPackDraft is the refusal of a second draft of a month (board_pack_one_draft).
const CodeBoardPackDraft = "STORAGE-BPACK-02"

// The statuses of a pack and of an update.
const (
	PackDraft       = "draft"
	PackPublished   = "published"
	UpdateDraft     = "draft"
	UpdateSent      = "sent"
	UpdateMonthly   = "monthly"
	UpdateQuarterly = "quarterly"
)

// BoardPack is one version of a month's pack.
type BoardPack struct {
	ID             uuid.UUID
	Month          time.Time // the first day of the month
	Version        int
	Status         string
	Highlights     string
	Commentary     string
	Asks           string
	Figures        json.RawMessage // the statements and KPIs as read at publication; nil for a draft
	LedgerRevision int64
	Supersedes     *uuid.UUID
	Edit           int
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedBy      string
	UpdatedAt      time.Time
	PublishedBy    string
	PublishedAt    *time.Time
}

// BoardPacks are the monthly packs.
type BoardPacks interface {
	Get(ctx context.Context, id uuid.UUID) (BoardPack, error)
	// List are the packs, the latest month and version first; with publishedOnly, the published ones.
	List(ctx context.Context, publishedOnly bool) ([]BoardPack, error)
	// Latest is the month's latest version, or ports.ErrNotFound.
	Latest(ctx context.Context, month time.Time) (BoardPack, error)
	// CreateDraft makes a month's draft: version 1, or the next after the latest published, which it supersedes.
	CreateDraft(ctx context.Context, month time.Time, by string, at time.Time) (BoardPack, error)
	// EditDraft saves a draft's texts as its next edit after edit, refused with CodeVersion when it is no longer at edit.
	EditDraft(ctx context.Context, id uuid.UUID, highlights, commentary, asks, by string, at time.Time, edit int) (BoardPack, error)
	// Publish freezes a draft with the figures read at a ledger revision, after edit.
	Publish(ctx context.Context, id uuid.UUID, figures json.RawMessage, revision int64, by string, at time.Time, edit int) (BoardPack, error)
}

// InvestorUpdate is an update built from a published pack.
type InvestorUpdate struct {
	ID         uuid.UUID
	PackID     uuid.UUID
	Kind       string
	Title      string
	Highlights string
	Asks       string
	Recipients []uuid.UUID
	Status     string
	Edit       int
	CreatedBy  string
	CreatedAt  time.Time
	UpdatedBy  string
	UpdatedAt  time.Time
	SentBy     string
	SentAt     *time.Time
}

// InvestorUpdateRead is a person's first open of a sent update.
type InvestorUpdateRead struct {
	UpdateID uuid.UUID
	UserID   uuid.UUID
	ReadAt   time.Time
}

// InvestorUpdates are the updates and who read them.
type InvestorUpdates interface {
	Get(ctx context.Context, id uuid.UUID) (InvestorUpdate, error)
	// List are the updates, newest first; with sentOnly, the sent ones.
	List(ctx context.Context, sentOnly bool) ([]InvestorUpdate, error)
	CreateDraft(ctx context.Context, packID uuid.UUID, kind, title string, by string, at time.Time) (InvestorUpdate, error)
	EditDraft(ctx context.Context, id uuid.UUID, title, highlights, asks, by string, at time.Time, edit int) (InvestorUpdate, error)
	// Send fixes the recipients and sends the draft, after edit.
	Send(ctx context.Context, id uuid.UUID, recipients []uuid.UUID, by string, at time.Time, edit int) (InvestorUpdate, error)
	// MarkRead records a recipient's first open; it returns false when they had opened it already.
	MarkRead(ctx context.Context, id, user uuid.UUID, at time.Time) (bool, error)
	Reads(ctx context.Context, id uuid.UUID) ([]InvestorUpdateRead, error)
}
