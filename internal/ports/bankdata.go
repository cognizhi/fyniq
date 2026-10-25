package ports

import (
	"context"
	"time"
)

// BankData is a bank-data connector's read-only view of the accounts it was given (FR-BNK-15, FR-BNK-18;
// p1-bank-import-reconcile design D11): the accounts or balances, and their transactions after a cursor. It never
// moves money: a bank-data connector is read-only (FR-INT-27).
type BankData interface {
	// Accounts are the provider's accounts (a Wise balance, an Airwallex wallet currency) the credentials read.
	Accounts(ctx context.Context) ([]BankDataAccount, error)
	// Transactions are an account's settled transactions after cursor, oldest first, at most a page of them, with the
	// cursor to continue from. With no cursor they start at from: the day the account's source became the connector, so
	// a switch from another source continues its chain (p1-bank-import-reconcile design D13); a zero from leaves the
	// connector's own history.
	Transactions(ctx context.Context, account, cursor string, from time.Time) (BankDataPage, error)
}

// BankDataAccount is one account as the provider names it.
type BankDataAccount struct {
	ID       string // the provider's ID, which the money account's identifier names
	Currency string
	Name     string
}

// BankDataTransaction is one settled transaction, with its amount and balance as exact decimal strings in its currency
// (ENG-12): signed, money in positive.
type BankDataTransaction struct {
	ID             string // the provider's reference, unique within the account
	BookingDate    time.Time
	ValueDate      *time.Time
	Amount         string
	Currency       string
	Counterparty   string
	Description    string
	RunningBalance string // "" when the provider gives none
}

// BankDataPage is a page of an account's transactions: Cursor continues after the last; More says another page waits.
type BankDataPage struct {
	Transactions []BankDataTransaction
	Cursor       string
	More         bool
}
