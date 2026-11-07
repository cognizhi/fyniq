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

// The kinds of holding movement (FR-EQ-17; p1-equity-register design D3).
const (
	MovementIssue           = "issue"
	MovementTransfer        = "transfer"
	MovementClassConversion = "class_conversion"
	MovementReversal        = "reversal"
)

// HoldingMovement is shares moving into (+) or out of (-) one holder's holding of one class, on the day ACRA's register
// of members shows it: an issue, which names its share issue; a transfer or a conversion between classes, which names
// its equity transaction; or a reversal, which negates one movement of the transaction its equity transaction reverses.
// What a holder holds is the sum of their movements. Written once, never changed.
type HoldingMovement struct {
	ID             uuid.UUID
	Kind           string
	TransactionID  *uuid.UUID // the equity transaction made effective; nil for an issue recorded outside one
	ShareIssueID   *uuid.UUID
	ReversesID     *uuid.UUID
	HolderID       uuid.UUID
	ClassAccountID uuid.UUID
	ClassID        *uuid.UUID
	Shares         Decimal // signed, whole
	EffectiveOn    time.Time
	RecordedBy     uuid.UUID
	RecordedAt     time.Time
}

// Holdings keep the holding movements. Move writes one, audited; the database refuses a movement naming no approved
// equity transaction of its kind, one of a transaction made effective earlier, an issue's movement unlike its share
// issue, a reversal unlike the movement it negates, a class that is not a recorded class of its account, shares moved
// in to a contact who is not a director or shareholder, a date in a closed period, any change or deletion, and at commit
// a transfer or conversion that does not move shares as one does, a reversal of part of a transaction, a holding below
// zero on any day, or a share issue with no movement (CodeHolding).
type Holdings interface {
	Move(ctx context.Context, m HoldingMovement) (HoldingMovement, error)
	// List is every movement, earliest effective date first.
	List(ctx context.Context) ([]HoldingMovement, error)
	// OfTransaction is an equity transaction's movements, empty while it is not effective.
	OfTransaction(ctx context.Context, transactionID uuid.UUID) ([]HoldingMovement, error)
}
