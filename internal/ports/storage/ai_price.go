package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CodeAIPrice is the database's refusal of a price's change (FY333): a price or its rates changed or removed, a rate
// written apart from its price, a state it cannot move to, or a read overwriting an Owner's price (PRD-001 AI-82,
// AI-85; p1-ai-providers task 4.2).
const CodeAIPrice = "STORAGE-AI-PRICE-01"

// AIPriceCatalogue is one download of a public price catalogue (AI-85): the installation's, the same for every
// organisation.
type AIPriceCatalogue struct {
	ID            uuid.UUID
	Source        string
	URL           string
	FetchedAt     time.Time
	SHA256        string // hex
	Size          int64
	Licence       string // what the licence check found; "" when it failed
	LicenceURL    string
	LicenceSHA256 string
	Outcome       string // valid or rejected
	Failure       string
	Entries       int    // the chat models it prices
	Document      string // the file store's key of the document as downloaded
	RecordedAt    time.Time
}

// The outcomes of a catalogue download.
const (
	AIPriceCatalogueValid    = "valid"
	AIPriceCatalogueRejected = "rejected"
)

// AIPriceCatalogues is the installation's record of catalogue downloads: only the worker's daily read writes it.
type AIPriceCatalogues interface {
	// RecordAIPriceCatalogue keeps one download and returns it as recorded.
	RecordAIPriceCatalogue(ctx context.Context, c AIPriceCatalogue) (AIPriceCatalogue, error)
}

// AIPriceRate is one rate of a price: its kind, the prompt length above which it applies (0 for the base rate), its
// unit, and its amount as exact decimal text (AI-85).
type AIPriceRate struct {
	Kind   string
	Above  int64
	Unit   string
	Amount string
}

// AIPrice is one version of a model's price in an organisation (AI-82).
type AIPrice struct {
	ID           uuid.UUID
	ConnectionID uuid.UUID
	Model        string
	Number       int
	Source       string
	CatalogueID  *uuid.UUID
	CatalogueKey string
	Match        string
	Currency     string
	Rates        []AIPriceRate
	Ignored      []string
	Status       string
	Review       string
	ReviewDetail string
	RecordedBy   string
	RecordedAt   time.Time
	Approval     *uuid.UUID // an Owner's price: its second approver's approval
	DecidedBy    string
	DecidedAt    *time.Time
}

// AIPrices keeps the organisation's model prices, version after version, and reads the catalogue downloads.
type AIPrices interface {
	// Current are a connection's versions in force or waiting for a review, and each model's last declined one, by model.
	Current(ctx context.Context, connection uuid.UUID) ([]AIPrice, error)
	// Get is one version; ports.ErrNotFound when the organisation has none such.
	Get(ctx context.Context, id uuid.UUID) (AIPrice, error)
	// Record adds a model's next version with its rates, in force or waiting: the database supersedes what it replaces
	// and refuses to overwrite an Owner's price, or to record one without its approved request (CodeAIPrice). Audited as
	// ai_price.changed, by the recorder.
	Record(ctx context.Context, p AIPrice) (AIPrice, error)
	// Checked records that a model's price was looked for at, and why it got no new price ("" when it did).
	Checked(ctx context.Context, connection uuid.UUID, model, unpriced string, at time.Time) error
	// Decide records an Owner's review of a waiting version: confirm puts it in force, decline keeps the one before.
	// Audited as ai_price.confirmed or ai_price.declined.
	Decide(ctx context.Context, id uuid.UUID, confirm bool, by string, at time.Time) (AIPrice, error)
	// Withdraw withdraws an Owner's price in force, by whom and when (AI-82): audited as ai_price.withdrawn. CodeAIPrice
	// for any other version.
	Withdraw(ctx context.Context, id uuid.UUID, by string, at time.Time) (AIPrice, error)
	// Catalogue is one catalogue download; ports.ErrNotFound when there is none such.
	Catalogue(ctx context.Context, id uuid.UUID) (AIPriceCatalogue, error)
	// LatestCatalogue is source's newest valid download; ports.ErrNotFound when it has none.
	LatestCatalogue(ctx context.Context, source string) (AIPriceCatalogue, error)
	// Catalogues are the downloads, newest first, at most limit.
	Catalogues(ctx context.Context, limit int) ([]AIPriceCatalogue, error)
}
