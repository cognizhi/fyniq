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

// The opening balances' source type and rule version (FR-GL-06, posting rule "Opening balances"; PRD-001 rev 0.49).
const (
	SourceOpeningBalance = "opening_balance"
	RuleOpeningBalance   = "opening_balance@1"
)

// Account is what the engine needs to know about an account (INV-04, INV-13).
type Account struct {
	ID     uuid.UUID
	Code   string
	Group  bool
	Active bool
	// ShareCapital marks an account only the register of share issues and opening balances post to:
	// a manual journal line to it is refused (INV-13, from P0).
	ShareCapital bool
	// Revalue marks a monetary item, whose foreign-currency balances are revalued at month end, and UnrealisedFX the
	// account the revaluation posts against (FR-GL-04; rev 0.72).
	Revalue      bool
	UnrealisedFX bool
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
	CodeRateOnBase      = "LEDGER-13" // a rate entered on a base-currency line (ENG-03, ENG-24 rule 5)
	CodeBaseRange       = "LEDGER-14" // a computed base amount its column cannot hold (ENG-44, ENG-24 rule 6)
	CodeShareCapital    = "LEDGER-15" // a manual journal line to a share-capital account (INV-13)
	CodeReversalDate    = "LEDGER-16" // a reversal dated before the entry it reverses (FR-GL-12)
	CodeForeignOpening  = "LEDGER-17" // an opening balance not in the base currency (FR-GL-06, rev 0.49)
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
	return identity(SourceManualJournal, id, version, rule)
}

// OpeningIdentity is the posting identity of a version of the opening balances under a rule version.
func OpeningIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceOpeningBalance, id, version, rule)
}

func identity(source string, id uuid.UUID, version int, rule string) string {
	return fmt.Sprintf("%s:%s:v%d:%s", source, id, version, rule)
}

// asEntered are the differences between the sources whose lines post as entered (§10.2).
type asEntered struct {
	source, rule string
	what         string // the document, as the messages name it
	shareCapital bool   // lines may post to share-capital accounts (INV-13)
	baseOnly     bool   // every line is in the base currency
}

// PostJournal turns one version of a manual journal into its entry (FR-GL-01 in P0 form) under the
// validity rules of ENG-24. It checks every rule first and returns every failure together (ENG-22,
// FR-INT-07): the journal is rejected whole, and the engine never adds, drops or changes a line to make
// it post (§10.2). The balance rules (7 and 8) are checked whenever every line has a base amount, so
// they are reported beside any other failure.
func PostJournal(j Journal, s Settings) (Entry, error) {
	return post(j, s, asEntered{source: SourceManualJournal, rule: RuleManualJournal, what: "journal"})
}

// PostOpening turns one version of the opening balances into its entry (FR-GL-06; posting rule "Opening balances"):
// posted as entered at the books start, netting to zero. It checks ENG-24's rules as a manual journal does, except
// that share capital may take a line (INV-13: share capital changes through the register of share issues or opening
// balances) and every line is in the base currency (rev 0.49): open items in other currencies come with sales and
// purchases. The caller dates it at the books start.
func PostOpening(j Journal, s Settings) (Entry, error) {
	return post(j, s, asEntered{source: SourceOpeningBalance, rule: RuleOpeningBalance, what: "opening balance entry", shareCapital: true, baseOnly: true})
}

func post(j Journal, s Settings, r asEntered) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }

	if j.Date.IsZero() {
		add(CodeNoDate, "date", fmt.Sprintf("the %s has no date", r.what), "Enter the date the journal belongs to")
	}
	if len(j.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(j.Memo), MaxMemo), "Shorten the memo")
	}
	if len(j.Lines) < 2 {
		add(CodeTooFewLines, "lines", fmt.Sprintf("the %s has %d line(s)", r.what, len(j.Lines)),
			"Add at least one debit and one credit line")
	}

	lines := make([]Line, 0, len(j.Lines))
	baseTotal := money.Zero(s.Base)
	perCurrency := map[string]money.Money{}
	var currencies []string
	converted := true // every line so far has a base amount (rules 7 and 8 need them all)
	for i, l := range j.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", i+1, name) }
		// Rule 4 (INV-04) and INV-13: the account.
		acc, ok := s.Accounts[l.AccountID]
		switch {
		case !ok:
			add(CodeUnknownAccount, f("account"), "the account does not exist in this organisation", "Choose an account from the chart of accounts")
		case acc.Group:
			add(CodeGroupAccount, f("account"), fmt.Sprintf("account %s is a group heading", acc.Code), "Post to an account under it")
		case !acc.Active:
			add(CodeInactiveAccount, f("account"), fmt.Sprintf("account %s is inactive", acc.Code), "Reactivate it or choose another account")
		case acc.ShareCapital && !r.shareCapital:
			add(CodeShareCapital, f("account"), fmt.Sprintf("account %s is share capital, which changes only through the register of share issues or opening balances", acc.Code),
				"Record the share issue in the register of share issues instead of a manual journal")
		}
		if len(l.Description) > MaxDescription {
			add(CodeTooLong, f("description"), fmt.Sprintf("the description has %d characters; the most is %d", len(l.Description), MaxDescription), "Shorten it")
		}
		// Rule 3: no zero line.
		if l.Amount.Currency().Code == "" {
			add(CodeZeroLine, f("amount"), "the line has no amount", "Enter a debit or a credit")
			converted = false
			continue
		}
		if l.Amount.IsZero() {
			add(CodeZeroLine, f("amount"), "the line's amount is zero", "Enter a debit or a credit, or remove the line")
			converted = false
			continue
		}
		// Rule 5 (ENG-03): a line in the base currency has rate 1 from "base", and none entered; any
		// other line has a rate into the base currency, with its source.
		rate := money.Identity(s.Base)
		cur := l.Amount.Currency()
		if r.baseOnly && cur != s.Base {
			add(CodeForeignOpening, f("amount"), fmt.Sprintf("the line is in %s; opening balances are in the base currency %s", cur, s.Base),
				fmt.Sprintf("Enter the balance in %s; open invoices and bills in other currencies come with sales and purchases", s.Base))
			converted = false
			continue
		}
		switch {
		case cur == s.Base && l.Rate != nil:
			add(CodeRateOnBase, f("fx_rate"), fmt.Sprintf("the line is in the base currency %s, so it takes no exchange rate", s.Base),
				"Remove the rate: Fyniq records rate 1 for base-currency lines")
		case cur != s.Base && l.Rate == nil:
			add(CodeMissingRate, f("fx_rate"), fmt.Sprintf("the line is in %s, so it needs a rate into %s", cur, s.Base),
				"Enter the exchange rate and where it came from")
			converted = false
			continue
		case l.Rate != nil && (l.Rate.From != cur || l.Rate.To != s.Base):
			add(CodeWrongRate, f("fx_rate"), fmt.Sprintf("the rate converts %s into %s, but the line needs %s into %s", l.Rate.From, l.Rate.To, cur, s.Base),
				"Use a rate from the line's currency into the base currency")
			converted = false
			continue
		case l.Rate != nil:
			rate = *l.Rate
		}
		// Rule 6 (ENG-44): the base amount, under RND-FX, fits its column; it is never truncated. A
		// non-zero line whose base amount rounds to 0.00 posts with base 0.00 (INV-05, rev 0.24).
		base, err := rounding.FX(l.Amount, rate)
		if err != nil {
			add(CodeBaseRange, f("amount"), fmt.Sprintf("at this rate the base amount does not fit: %v", err),
				"Check the amount and the rate: no ledger amount is that large")
			converted = false
			continue
		}
		if baseTotal, err = baseTotal.Add(base); err != nil {
			add(CodeWrongRate, f("amount"), err.Error(), "Check the amount")
			converted = false
			continue
		}
		if _, seen := perCurrency[cur.Code]; !seen {
			perCurrency[cur.Code] = money.Zero(cur)
			currencies = append(currencies, cur.Code)
		}
		perCurrency[cur.Code], _ = perCurrency[cur.Code].Add(l.Amount)
		lines = append(lines, Line{AccountID: l.AccountID, Amount: l.Amount, Rate: rate, Base: base, Description: l.Description})
	}

	// Rule 7: the base amounts, each RND-FX of its line, sum to zero; rule 8: a journal in one currency
	// also sums to zero in it (INV-01, FR-GL-01). Checked whenever every line has a base amount.
	if converted && len(lines) > 0 {
		if !baseTotal.IsZero() {
			add(CodeUnbalancedBase, "lines", fmt.Sprintf("debits and credits differ by %s %s in the base currency", baseTotal, s.Base),
				"Make the debits equal the credits")
		}
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
		Source:          Source{Type: r.source, ID: j.ID.String(), Version: j.Version},
		RuleVersion:     r.rule,
		PostingIdentity: identity(r.source, j.ID, j.Version, r.rule),
		Memo:            j.Memo,
		Lines:           lines,
	}, nil
}

// ReversalIdentity is the posting identity of the reversal of the entry with identity id.
func ReversalIdentity(id string) string { return id + ":reversal" }

// Reverse is the entry that undoes e on date: every stored line negated, amount and base amount as
// stored and never re-converted, at the same rate, so the two net to zero in every currency and in
// the base currency (ENG-14, FR-GL-12). It is dated on or after e (FR-GL-12, from P0 for manual
// journals); the period must be open (INV-03, the database) and an entry is reversed once (the
// caller's document status and the reversal's posting identity). Posted history is never changed.
func Reverse(e Entry, date time.Time, reason string) (Entry, error) {
	if len(e.Lines) == 0 {
		return Entry{}, Problems{{CodeNotPosted, "entry", "there is no posted entry to reverse", "Post the journal first"}}
	}
	if date.IsZero() {
		return Entry{}, Problems{{CodeNoDate, "date", "the reversal has no date", "Enter the date of the reversal"}}
	}
	y, m, d := date.Date()
	if day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC); day.Before(e.PostingDate) {
		return Entry{}, Problems{{CodeReversalDate, "date", fmt.Sprintf("the reversal is dated %s, before the entry it reverses (%s)",
			day.Format(time.DateOnly), e.PostingDate.Format(time.DateOnly)), "Date the reversal on or after the entry's date"}}
	}
	memo := "Reversal"
	if reason != "" {
		memo += ": " + reason
	}
	if len(memo) > MaxMemo {
		return Entry{}, Problems{{CodeTooLong, "reason", fmt.Sprintf("the reason makes the memo %d characters; the most is %d", len(memo), MaxMemo), "Shorten the reason"}}
	}
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
