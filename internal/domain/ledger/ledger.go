// Package ledger is the posting engine (PRD-001 §10.1): pure functions from a document version and
// the organisation's settings to journal entries. No clock, randomness or I/O happens here
// (ENG-01): the caller loads what the rules need, calls the engine, and persists the entries
// through the unit of work in the document's own transaction (ENG-02). Debits are positive and
// credits negative; every base amount is computed once, here, under RND-FX (ENG-14).
//
// The P0 posting source is the manual journal; the P1 sources add their own rule sets beside it.
package ledger

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The manual journal's source type and rule version (ENG-03). A change to the rule is a new
// version; an old version keeps producing what it produced (ENG-06).
const (
	SourceManualJournal = "manual_journal"
	RuleManualJournal   = "manual_journal@1"
)

// Account is what the engine needs to know about an account (INV-04).
type Account struct {
	ID     uuid.UUID
	Code   string
	Group  bool
	Active bool
}

// Settings are the organisation's settings and master data the rules read.
type Settings struct {
	Base     money.Currency
	Accounts map[uuid.UUID]Account
}

// Source identifies the document version an entry comes from (ENG-03).
type Source struct {
	Type    string
	ID      string
	Version int
}

// Line is one posted line: the amount in its own currency, the rate into the base currency with its
// source, and the base amount computed from them under RND-FX.
type Line struct {
	AccountID   uuid.UUID
	Amount      money.Money
	Rate        money.Rate
	Base        money.Money
	Description string
}

// Entry is one balanced journal entry, ready to persist.
type Entry struct {
	PostingDate     time.Time // a calendar date, in UTC
	Source          Source
	RuleVersion     string
	PostingIdentity string // never expires: the same identity cannot post twice (§11.4)
	Memo            string
	Lines           []Line
}

// JournalLine is one line of a manual journal as entered: debit positive, credit negative. Rate is
// required when the amount is not in the base currency.
type JournalLine struct {
	AccountID   uuid.UUID
	Amount      money.Money
	Rate        *money.Rate
	Description string
}

// Journal is one version of a manual journal document.
type Journal struct {
	ID      uuid.UUID
	Version int
	Date    time.Time
	Memo    string
	Lines   []JournalLine
}

// MaxMemo and MaxDescription are the database's limits (db/migrations/000004).
const (
	MaxMemo        = 1000
	MaxDescription = 500
)

// The stable validation codes of the engine (ENG-22, ENG-23).
const (
	CodeNoDate          = "LEDGER-01"
	CodeTooFewLines     = "LEDGER-02"
	CodeZeroLine        = "LEDGER-03"
	CodeUnknownAccount  = "LEDGER-04"
	CodeGroupAccount    = "LEDGER-05"
	CodeInactiveAccount = "LEDGER-06"
	CodeMissingRate     = "LEDGER-07"
	CodeWrongRate       = "LEDGER-08"
	CodeUnbalancedBase  = "LEDGER-09"
	CodeUnbalancedCur   = "LEDGER-10"
	CodeTooLong         = "LEDGER-11"
	CodeNotPosted       = "LEDGER-12"
)

// Problem is one validation failure: a stable code, the field, what is wrong and how to fix it.
type Problem struct {
	Code    string
	Field   string
	Message string
	Fix     string
}

// Problems are every validation failure of a document; the document cannot post (✖).
type Problems []Problem

func (p Problems) Error() string {
	parts := make([]string, len(p))
	for i, x := range p {
		parts[i] = fmt.Sprintf("%s %s: %s (%s)", x.Code, x.Field, x.Message, x.Fix)
	}
	return strings.Join(parts, "; ")
}

// Has reports whether one of the problems has code.
func (p Problems) Has(code string) bool {
	for _, x := range p {
		if x.Code == code {
			return true
		}
	}
	return false
}

// JournalIdentity is the posting identity of a journal version under a rule version (§11.4).
func JournalIdentity(id uuid.UUID, version int, rule string) string {
	return fmt.Sprintf("%s:%s:v%d:%s", SourceManualJournal, id, version, rule)
}

// PostJournal turns one version of a manual journal into its entry (FR-GL-01 in P0 form). It
// validates everything first and returns every problem together (ENG-22).
func PostJournal(j Journal, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }

	if j.Date.IsZero() {
		add(CodeNoDate, "date", "the journal has no date", "Enter the date the journal belongs to")
	}
	if len(j.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(j.Memo), MaxMemo), "Shorten the memo")
	}
	if len(j.Lines) < 2 {
		add(CodeTooFewLines, "lines", fmt.Sprintf("the journal has %d line(s)", len(j.Lines)),
			"Add at least one debit and one credit line")
	}

	lines := make([]Line, 0, len(j.Lines))
	baseTotal := money.Zero(s.Base)
	perCurrency := map[string]money.Money{}
	var currencies []string
	for i, l := range j.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
		acc, ok := s.Accounts[l.AccountID]
		switch {
		case !ok:
			add(CodeUnknownAccount, f("account"), "the account does not exist in this organisation", "Choose an account from the chart of accounts")
		case acc.Group:
			add(CodeGroupAccount, f("account"), fmt.Sprintf("account %s is a group heading", acc.Code), "Post to an account under it")
		case !acc.Active:
			add(CodeInactiveAccount, f("account"), fmt.Sprintf("account %s is inactive", acc.Code), "Reactivate it or choose another account")
		}
		if len(l.Description) > MaxDescription {
			add(CodeTooLong, f("description"), fmt.Sprintf("the description has %d characters; the most is %d", len(l.Description), MaxDescription), "Shorten it")
		}
		if l.Amount.Currency().Code == "" {
			add(CodeZeroLine, f("amount"), "the line has no amount", "Enter a debit or a credit")
			continue
		}
		if l.Amount.IsZero() {
			add(CodeZeroLine, f("amount"), "the line's amount is zero", "Enter a debit or a credit, or remove the line")
			continue
		}
		rate := money.Identity(s.Base)
		cur := l.Amount.Currency()
		switch {
		case cur != s.Base && l.Rate == nil:
			add(CodeMissingRate, f("fx_rate"), fmt.Sprintf("the line is in %s, so it needs a rate into %s", cur, s.Base),
				"Enter the exchange rate and where it came from")
			continue
		case l.Rate != nil && (l.Rate.From != cur || l.Rate.To != s.Base):
			add(CodeWrongRate, f("fx_rate"), fmt.Sprintf("the rate converts %s into %s, but the line needs %s into %s", l.Rate.From, l.Rate.To, cur, s.Base),
				"Use a rate from the line's currency into the base currency")
			continue
		case l.Rate != nil:
			rate = *l.Rate
		}
		base, err := rounding.FX(l.Amount, rate)
		if err != nil {
			add(CodeWrongRate, f("fx_rate"), err.Error(), "Check the rate")
			continue
		}
		if baseTotal, err = baseTotal.Add(base); err != nil {
			add(CodeWrongRate, f("amount"), err.Error(), "Check the amount")
			continue
		}
		if _, seen := perCurrency[cur.Code]; !seen {
			perCurrency[cur.Code] = money.Zero(cur)
			currencies = append(currencies, cur.Code)
		}
		perCurrency[cur.Code], _ = perCurrency[cur.Code].Add(l.Amount)
		lines = append(lines, Line{AccountID: l.AccountID, Amount: l.Amount, Rate: rate, Base: base, Description: l.Description})
	}

	if len(probs) == 0 {
		if !baseTotal.IsZero() {
			add(CodeUnbalancedBase, "lines", fmt.Sprintf("debits and credits differ by %s %s in the base currency", baseTotal, s.Base),
				"Make the debits equal the credits")
		}
		// A single-currency journal also balances in its own currency (INV-01).
		if len(currencies) == 1 {
			if total := perCurrency[currencies[0]]; !total.IsZero() {
				add(CodeUnbalancedCur, "lines", fmt.Sprintf("debits and credits differ by %s %s", total, currencies[0]),
					"Make the debits equal the credits in the journal's currency")
			}
		}
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	y, m, d := j.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceManualJournal, ID: j.ID.String(), Version: j.Version},
		RuleVersion:     RuleManualJournal,
		PostingIdentity: JournalIdentity(j.ID, j.Version, RuleManualJournal),
		Memo:            j.Memo,
		Lines:           lines,
	}, nil
}

// ReversalIdentity is the posting identity of the reversal of the entry with identity id.
func ReversalIdentity(id string) string { return id + ":reversal" }

// Reverse is the entry that undoes e on date: every line with the opposite sign, at the same rate,
// so the two net to zero in every currency and in the base currency (FR-GL-12). Posted history is
// never changed; it is reversed.
func Reverse(e Entry, date time.Time, reason string) (Entry, error) {
	if len(e.Lines) == 0 {
		return Entry{}, Problems{{CodeNotPosted, "entry", "there is no posted entry to reverse", "Post the journal first"}}
	}
	if date.IsZero() {
		return Entry{}, Problems{{CodeNoDate, "date", "the reversal has no date", "Enter the date of the reversal"}}
	}
	memo := "Reversal"
	if reason != "" {
		memo += ": " + reason
	}
	if len(memo) > MaxMemo {
		return Entry{}, Problems{{CodeTooLong, "reason", fmt.Sprintf("the reason makes the memo %d characters; the most is %d", len(memo), MaxMemo), "Shorten the reason"}}
	}
	y, m, d := date.Date()
	out := Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          e.Source,
		RuleVersion:     e.RuleVersion,
		PostingIdentity: ReversalIdentity(e.PostingIdentity),
		Memo:            memo,
		Lines:           make([]Line, len(e.Lines)),
	}
	for i, l := range e.Lines {
		out.Lines[i] = Line{AccountID: l.AccountID, Amount: l.Amount.Neg(), Rate: l.Rate, Base: l.Base.Neg(), Description: l.Description}
	}
	return out, nil
}
