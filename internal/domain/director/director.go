// Package director is the founders' side of the ledger (PRD-001 §8.22; p1-founders-money): each director's or
// shareholder's account on 2500 Loan from director / shareholder and 2120 Deferred remuneration payable, their balances
// from the balance service (ENG-04), and their statement, each movement with the origin its tax and accounting result
// depends on (FR-EQ-01, design D3 and D4).
package director

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable codes of the director's accounts (ENG-23).
const (
	CodeNotPerson = "FOUNDER-01" // the contact does not exist, or is not a director or shareholder
	CodeDates     = "FOUNDER-02" // a statement's dates are missing or out of order
)

// Error is a refusal of a command, with its code, what is wrong and how to fix it.
type Error struct {
	Code    string
	Message string
	Fix     string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Fix) }

// Person is a director or shareholder with their balances through a day, in the base currency and signed as the ledger
// holds them: a credit (negative) is owed to the person, a debit owed by them.
type Person struct {
	Contact  storage.Contact
	Balances map[uuid.UUID]money.Money // by director's account, every one of the chart's, zero when none
	Net      money.Money               // their sum; a debit means the person owes the company (FR-EQ-11)
}

// Accounts are every director and shareholder with their balances through asAt (FR-EQ-01), in the order they were
// added, and the base amount on the director's accounts that names no one: zero but for lines posted before the
// accounts named their person, which the close names (INV-07).
func Accounts(ctx context.Context, u storage.UnitOfWork, base money.Currency, asAt time.Time) ([]Person, []storage.Account, money.Money, error) {
	unkeyed := money.Zero(base)
	accounts, err := balance.DirectorAccounts(ctx, u)
	if err != nil {
		return nil, nil, unkeyed, err
	}
	balances, err := balance.People(ctx, u, base, asAt)
	if err != nil {
		return nil, nil, unkeyed, err
	}
	byParty := map[uuid.UUID]balance.PersonBalance{}
	for _, b := range balances {
		if b.Party == uuid.Nil {
			unkeyed = b.Net
			continue
		}
		byParty[b.Party] = b
	}
	var out []Person
	for after := uuid.Nil; ; {
		page, err := u.Contacts().Page(ctx, after, "director", 500)
		if err != nil {
			return nil, nil, unkeyed, err
		}
		for _, c := range page {
			out = append(out, personOf(c, accounts, byParty[c.ID], base))
		}
		if len(page) < 500 {
			return out, accounts, unkeyed, nil
		}
		after = page[len(page)-1].ID
	}
}

func personOf(c storage.Contact, accounts []storage.Account, b balance.PersonBalance, base money.Currency) Person {
	p := Person{Contact: c, Balances: map[uuid.UUID]money.Money{}, Net: money.Zero(base)}
	for _, a := range accounts {
		v, ok := b.Accounts[a.ID]
		if !ok {
			v = money.Zero(base)
		}
		p.Balances[a.ID] = v
	}
	if b.Accounts != nil {
		p.Net = b.Net
	}
	return p
}

// Get is one director or shareholder with their balances through asAt.
func Get(ctx context.Context, u storage.UnitOfWork, base money.Currency, id uuid.UUID, asAt time.Time) (Person, []storage.Account, error) {
	c, err := contactOf(ctx, u, id)
	if err != nil {
		return Person{}, nil, err
	}
	accounts, err := balance.DirectorAccounts(ctx, u)
	if err != nil {
		return Person{}, nil, err
	}
	balances, err := balance.People(ctx, u, base, asAt)
	if err != nil {
		return Person{}, nil, err
	}
	for _, b := range balances {
		if b.Party == id {
			return personOf(c, accounts, b, base), accounts, nil
		}
	}
	return personOf(c, accounts, balance.PersonBalance{}, base), accounts, nil
}

func contactOf(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (storage.Contact, error) {
	c, err := u.Contacts().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return storage.Contact{}, &Error{CodeNotPerson, "the contact does not exist in this organisation", "Choose a director or shareholder"}
	}
	if err != nil {
		return storage.Contact{}, err
	}
	if !c.Director && !c.Shareholder {
		return storage.Contact{}, &Error{CodeNotPerson, fmt.Sprintf("%s is not a director or shareholder", c.LegalName),
			"Mark the contact director or shareholder, or choose another"}
	}
	return c, nil
}

// Movement is one line of a person's statement: a line on one of their director's accounts, with where its amount
// came from (FR-EQ-01).
type Movement struct {
	EntryID     uuid.UUID
	Date        time.Time
	AccountID   uuid.UUID
	Origin      string      // one of ledger's Origin values
	Amount      money.Money // base currency, as the ledger holds it: a credit is owed to the person
	Source      storage.Source
	Number      string // the source document's number, when it has one
	Memo        string
	Description string
	Reversal    bool // the line reverses an earlier one
}

// Statement is a person's account from..to (FR-EQ-01): the balance on each director's account before from, the
// movements, and the balance at to.
type Statement struct {
	Person    storage.Contact
	Accounts  []storage.Account
	From, To  time.Time
	Opening   map[uuid.UUID]money.Money
	Movements []Movement
	Closing   map[uuid.UUID]money.Money
}

// StatementOf is a person's statement from..to, both days included.
func StatementOf(ctx context.Context, u storage.UnitOfWork, base money.Currency, id uuid.UUID, from, to time.Time) (Statement, error) {
	if from.IsZero() || to.IsZero() || to.Before(from) {
		return Statement{}, &Error{CodeDates, "the statement needs a start and an end, the end on or after the start",
			"Send from and to as dates, from first"}
	}
	before, accounts, err := Get(ctx, u, base, id, from.AddDate(0, 0, -1))
	if err != nil {
		return Statement{}, err
	}
	st := Statement{Person: before.Contact, Accounts: accounts, From: from, To: to, Opening: before.Balances, Closing: map[uuid.UUID]money.Money{}}
	for k, v := range before.Balances {
		st.Closing[k] = v
	}
	lines, err := balance.PersonLines(ctx, u, id, from, to)
	if err != nil {
		return Statement{}, err
	}
	origins := originReader{ctx: ctx, u: u, saved: map[string][]string{}}
	for _, l := range lines {
		amount, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return Statement{}, err
		}
		origin, err := origins.of(l, amount)
		if err != nil {
			return Statement{}, err
		}
		st.Movements = append(st.Movements, Movement{EntryID: l.EntryID, Date: l.PostingDate, AccountID: l.AccountID, Origin: origin,
			Amount: amount, Source: l.Source, Number: l.Number, Memo: l.Memo, Description: l.Description, Reversal: l.ReversesEntryID != nil})
		if st.Closing[l.AccountID], err = st.Closing[l.AccountID].Add(amount); err != nil {
			return Statement{}, err
		}
	}
	return st, nil
}

// originReader finds where a line's amount came from (design D3): from its source, or for a manual journal or opening
// balance from the origin entered on its line, read once per document version.
type originReader struct {
	ctx   context.Context
	u     storage.UnitOfWork
	saved map[string][]string // by source ID and version: each saved line's origin
}

func (r originReader) of(l balance.PersonLine, amount money.Money) (string, error) {
	switch l.Source.Type {
	case ledger.SourceManualJournal, ledger.SourceOpeningBalance:
		key := fmt.Sprintf("%s:%d", l.Source.ID, l.Source.Version)
		entered, ok := r.saved[key]
		if !ok {
			id, err := uuid.Parse(l.Source.ID)
			if err != nil {
				return ledger.OriginOther, nil
			}
			doc, err := r.u.Documents().Version(r.ctx, id, l.Source.Version)
			if err != nil {
				return "", err
			}
			var p struct {
				Lines []struct {
					Origin string `json:"origin"`
				} `json:"lines"`
			}
			if err := json.Unmarshal(doc.Payload, &p); err != nil {
				return "", err
			}
			for _, x := range p.Lines {
				entered = append(entered, x.Origin)
			}
			r.saved[key] = entered
		}
		// The entry's lines are the version's lines in order (§10.2: posted as entered), its reversal's too.
		if i := l.LineNo - 1; i >= 0 && i < len(entered) && entered[i] != "" {
			return entered[i], nil
		}
		return ledger.OriginOther, nil
	case ledger.SourceExpenseClaim, ledger.SourcePreIncorporationCost: // a claim kept as a loan (FR-EQ-02); a cost before the books start (FR-EQ-03)
		return ledger.OriginPaidPersonally, nil
	case ledger.SourceBankTransaction:
		// Money in credits the person (lent), money out debits them (paid out); a reversal undoes the one it reverses.
		if credit := amount.Sign() < 0; credit != (l.ReversesEntryID != nil) {
			return ledger.OriginLent, nil
		}
		return ledger.OriginPaidOut, nil
	}
	return ledger.OriginOther, nil
}
