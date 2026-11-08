package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// The considerations a share issue is paid up with (FR-EQ-08).
const (
	ConsiderationCash            = "cash"
	ConsiderationDebtCapitalised = "debt_capitalised"
	ConsiderationServices        = "services"
	ConsiderationSAFE            = "safe_or_note"
)

// ShareIssue is one allotment in the register of share issues (FR-EQ-08; p1-founders-money design D11): the holder, the
// class (a share-capital account), the whole number of shares, the price per share, the amount paid up in the base
// currency, the consideration, the resolution references, ACRA's lodgement reference and the effective date. An issue
// before the books start has no entry; any other names the entry it posted from the source share_issue, and a
// conversion the settlement it decides. Written once, never changed.
type ShareIssue struct {
	ID             uuid.UUID
	HolderID       uuid.UUID
	ClassAccountID uuid.UUID
	ClassID        *uuid.UUID // its share class (FR-EQ-16): the class's first version's document; nil before the account has one
	Shares         Decimal
	Price          Decimal
	Currency       string
	Amount         Decimal
	Consideration  string
	ResolutionRefs string
	LodgementRef   string
	EffectiveOn    time.Time
	SettlementID   *uuid.UUID
	EntryID        *uuid.UUID
	RecordedBy     uuid.UUID
	RecordedAt     time.Time
}

// ShareIssues keep the register of share issues. Record writes an issue with the ID its entry names, audited; the
// database refuses one whose class is not an active share-capital account or whose holder is not a director or
// shareholder, one with no entry dated on or after the books start, one whose entry does not post it from the source
// share_issue on its effective date crediting its class with its amount, any change or deletion, and at commit an entry
// from the source share_issue no issue names (CodeShareIssue).
type ShareIssues interface {
	Record(ctx context.Context, s ShareIssue) (ShareIssue, error)
	// List is every issue, earliest effective date first.
	List(ctx context.Context) ([]ShareIssue, error)
	// OfSettlement is the issue a conversion's settlement made, nil while it has none.
	OfSettlement(ctx context.Context, settlementID uuid.UUID) (*ShareIssue, error)
}
