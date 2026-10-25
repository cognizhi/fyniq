package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The releases' source types and rule versions (FR-GL-03, ENG-05; posting rule "Spreading an invoice or bill", Appendix
// B.2): a bill's spread cost (rev 0.83) and an invoice's spread revenue (rev 0.118). A release names the document
// version whose spread it releases.
const (
	SourceBillRelease    = "bill_release"
	RuleBillRelease      = "bill_release@1"
	SourceInvoiceRelease = "invoice_release"
	RuleInvoiceRelease   = "invoice_release@1"
)

// Release is one month's part of a spread line: of a bill line's cost, from 1210 Prepayments to the line's account
// (SourceBillRelease), or of an invoice line's revenue, from 2400 Deferred revenue to the line's account
// (SourceInvoiceRelease). It names the document version and line (from 1), the month (its first day) and which part of
// how many it is, the date it posts on, the line's account and description, and the part, in the base currency.
type Release struct {
	Source      string
	DocumentID  uuid.UUID
	Version     int
	Line        int
	Month       time.Time
	Part, Of    int
	Date        time.Time
	AccountID   uuid.UUID
	Description string
	Memo        string
	Amount      money.Money
}

// ReleaseIdentity is the posting identity of a document version's release of one line in one month under a rule version
// (§11.4): one release per line and month, ever.
func ReleaseIdentity(source string, id uuid.UUID, version, line int, month time.Time, rule string) string {
	return fmt.Sprintf("%s:%s:v%d:l%d:%s:%s", source, id, version, line, month.Format("2006-01"), rule)
}

// BillReleaseIdentity is the posting identity of a bill version's release of one line in one month under rule.
func BillReleaseIdentity(id uuid.UUID, version, line int, month time.Time, rule string) string {
	return ReleaseIdentity(SourceBillRelease, id, version, line, month, rule)
}

// InvoiceReleaseIdentity is the posting identity of an invoice version's release of one line in one month under rule.
func InvoiceReleaseIdentity(id uuid.UUID, version, line int, month time.Time, rule string) string {
	return ReleaseIdentity(SourceInvoiceRelease, id, version, line, month, rule)
}

// Identity is the release's posting identity under rule.
func (r Release) Identity(rule string) string {
	return ReleaseIdentity(r.Source, r.DocumentID, r.Version, r.Line, r.Month, rule)
}

// firstOfMonth is the first day of t's month.
func firstOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

// scheduledLine is what a release schedule reads of a document's line.
type scheduledLine struct {
	AccountID   uuid.UUID
	Description string
	Spread      *Spread
}

// releases is the schedule of a document version's spread lines: for each, in line order, its amount in the base
// currency (amounts, one a spread line in order: what the document's entry put on the holding account) split over its
// months under ALLOC-EVEN, one part a month from its first month. A part is dated on the first day of its month, or on
// the document's date when that is later: a service that started before the document releases what it has used up, or
// earned, when the document posts.
func releases(source string, id uuid.UUID, version int, date time.Time, memo string, lines []scheduledLine, amounts []money.Money, what string) ([]Release, error) {
	var out []Release
	y, m, d := date.Date()
	on0 := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	k := 0
	for i, l := range lines {
		if l.Spread == nil {
			continue
		}
		if k >= len(amounts) {
			return nil, fmt.Errorf("the entry has %d lines on %s, fewer than its spread lines", len(amounts), what)
		}
		parts, err := rounding.AllocEven(amounts[k], l.Spread.Months)
		if err != nil {
			return nil, err
		}
		k++
		from := firstOfMonth(l.Spread.From)
		for j, part := range parts {
			month := from.AddDate(0, j, 0)
			on := month
			if on0.After(on) {
				on = on0
			}
			out = append(out, Release{Source: source, DocumentID: id, Version: version, Line: i + 1, Month: month, Part: j + 1, Of: len(parts), Date: on,
				AccountID: l.AccountID, Description: l.Description, Memo: memo, Amount: part})
		}
	}
	if k != len(amounts) {
		return nil, fmt.Errorf("the entry has %d lines on %s, more than its %d spread lines", len(amounts), what, k)
	}
	return out, nil
}

// BillReleases is the release schedule of a bill version's spread lines (FR-GL-03): for each, in the bill's line order,
// what its entry e debited to 1210 split over its months under ALLOC-EVEN. e is the bill's entry under bill@1, whose
// 1210 lines are its spread lines' in order. A bill with no spread has no releases.
func BillReleases(b Bill, e Entry, s Settings) ([]Release, error) {
	prepay, ok := s.byRole(RolePrepayments)
	var amounts []money.Money
	for _, l := range e.Lines {
		if ok && l.AccountID == prepay.ID {
			amounts = append(amounts, l.Base)
		}
	}
	lines := make([]scheduledLine, 0, len(b.Lines))
	for _, l := range b.Lines {
		lines = append(lines, scheduledLine{AccountID: l.AccountID, Description: l.Description, Spread: l.Spread})
	}
	return releases(SourceBillRelease, b.ID, b.Version, b.Date, b.Memo, lines, amounts, "prepayments")
}

// InvoiceReleases is the release schedule of an invoice version's spread lines (FR-GL-03, rev 0.118): for each, in the
// invoice's line order, what its entry e credited to 2400 split over its months under ALLOC-EVEN. e is the invoice's
// entry under invoice@1, whose 2400 lines are its spread lines' in order. An invoice with no spread has no releases.
func InvoiceReleases(i Invoice, e Entry, s Settings) ([]Release, error) {
	deferred, ok := s.byRole(RoleDeferredRevenue)
	var amounts []money.Money
	for _, l := range e.Lines {
		if ok && l.AccountID == deferred.ID {
			amounts = append(amounts, l.Base.Neg())
		}
	}
	lines := make([]scheduledLine, 0, len(i.Lines))
	for _, l := range i.Lines {
		lines = append(lines, scheduledLine{AccountID: l.AccountID, Description: l.Description, Spread: l.Spread})
	}
	return releases(SourceInvoiceRelease, i.ID, i.Version, i.Date, i.Memo, lines, amounts, "deferred revenue")
}

// releaseKind is what a release posts between, and the words its refusals use.
type releaseKind struct {
	rule    string
	role    string // the holding account's: what the document's entry put the spread on
	holding string // its name, in words
	missing string // how to give the chart one
	spread  string // what is spread: "cost" or "revenue"
	account string // the line's account, in words
	toLine  bool   // Dr the line's account, Cr the holding account (a cost); false the other way (revenue)
}

var (
	billRelease = releaseKind{rule: RuleBillRelease, role: RolePrepayments, holding: "prepayments", spread: "cost", account: "an expense account",
		missing: "Load the standard chart's 1210 Prepayments, or give an asset account the role", toLine: true}
	invoiceRelease = releaseKind{rule: RuleInvoiceRelease, role: RoleDeferredRevenue, holding: "deferred revenue", spread: "revenue", account: "a revenue account",
		missing: "Load the standard chart's 2400 Deferred revenue, or give a liability account the role"}
)

// postRelease turns one month's release into its entry under k, in the base currency on the release's date: the part
// moves between the line's account and the holding account. Every rule is checked first and every failure returned
// together (ENG-22).
func postRelease(r Release, s Settings, k releaseKind) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if r.Date.IsZero() || r.Month.IsZero() {
		add(CodeNoDate, "date", "the release has no date", "Release a month of a posted document's spread")
	}
	if r.Amount.Currency() != s.Base {
		add(CodeLineCurrency, "amount", fmt.Sprintf("the release is in %s, not the base currency %s", r.Amount.Currency(), s.Base),
			"Release the base amount the document put on "+k.holding)
	}
	memo := fmt.Sprintf("%s: %s spread for %s (%d of %d)", r.Memo, k.spread, r.Month.Format("January 2006"), r.Part, r.Of)
	if len(memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(memo), MaxMemo), "Shorten the document's memo")
	}
	holding, ok := s.byRole(k.role)
	if !ok {
		add(CodeSystemRole, "lines", "the chart has no "+k.holding+" account to release from", k.missing)
	}
	acc, known := s.Accounts[r.AccountID]
	switch {
	case !known:
		add(CodeUnknownAccount, "account", "the account does not exist in this organisation", "Choose "+k.account+" from the chart")
	case acc.Group:
		add(CodeGroupAccount, "account", fmt.Sprintf("account %s is a group heading", acc.Code), "Choose an account under it")
	case !acc.Active:
		add(CodeInactiveAccount, "account", fmt.Sprintf("account %s is inactive", acc.Code), "Reactivate it, so the spread can release to it")
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	if r.Amount.IsZero() { // a spread of a few cents over many months has months with nothing to release
		return Entry{}, Problems{{CodeZeroLine, "amount", "this month's part of the spread is zero", "Nothing is released this month"}}
	}
	id := money.Identity(s.Base)
	debit, credit := r.AccountID, holding.ID
	if !k.toLine {
		debit, credit = holding.ID, r.AccountID
	}
	y, m, d := r.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: r.Source, ID: r.DocumentID.String(), Version: r.Version},
		RuleVersion:     k.rule,
		PostingIdentity: r.Identity(k.rule),
		Memo:            memo,
		Lines: []Line{
			{AccountID: debit, Amount: r.Amount, Rate: id, Base: r.Amount, Description: r.Description},
			{AccountID: credit, Amount: r.Amount.Neg(), Rate: id, Base: r.Amount.Neg(), Description: r.Description},
		},
	}, nil
}

// PostBillRelease turns one month's release of a spread bill line into its entry (bill_release@1), in the base
// currency on the release's date: Dr the line's account and Cr 1210 Prepayments with the part (ENG-05: posted by the
// scheduler as a real entry, up to the current period only). Every rule is checked first and every failure returned
// together (ENG-22).
func PostBillRelease(r Release, s Settings) (Entry, error) {
	if r.Source != SourceBillRelease {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("%s releases a bill's spread, not a %s", RuleBillRelease, r.Source),
			"Release it under its own document's rule"}}
	}
	return postRelease(r, s, billRelease)
}

// PostInvoiceRelease turns one month's release of a spread invoice line into its entry (invoice_release@1; rev 0.118),
// in the base currency on the release's date: Dr 2400 Deferred revenue and Cr the line's revenue account with the part
// (ENG-05: posted by the scheduler as a real entry, up to the current period only). Every rule is checked first and
// every failure returned together (ENG-22).
func PostInvoiceRelease(r Release, s Settings) (Entry, error) {
	if r.Source != SourceInvoiceRelease {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("%s releases an invoice's spread, not a %s", RuleInvoiceRelease, r.Source),
			"Release it under its own document's rule"}}
	}
	return postRelease(r, s, invoiceRelease)
}

// ReleaseRule turns one month's release into its entry under one rule version.
type ReleaseRule func(r Release, s Settings) (Entry, error)

// BillReleaseRules, InvoiceReleaseRules, BankOutReleaseRules and BankInReleaseRules are every release rule version the
// engine has posted with, by name: like Rules, a version is never removed or changed while an entry names it.
var (
	BillReleaseRules    = map[string]ReleaseRule{RuleBillRelease: PostBillRelease}
	InvoiceReleaseRules = map[string]ReleaseRule{RuleInvoiceRelease: PostInvoiceRelease}
	BankOutReleaseRules = map[string]ReleaseRule{RuleBankOutRelease: PostBankOutRelease}
	BankInReleaseRules  = map[string]ReleaseRule{RuleBankInRelease: PostBankInRelease}
)

// ReleaseRules are the rule versions of a release source.
func ReleaseRules(source string) map[string]ReleaseRule {
	switch source {
	case SourceInvoiceRelease:
		return InvoiceReleaseRules
	case SourceBankOutRelease:
		return BankOutReleaseRules
	case SourceBankInRelease:
		return BankInReleaseRules
	}
	return BillReleaseRules
}

// CurrentRelease is the rule version a new release of source posts under.
func CurrentRelease(source string) string {
	switch source {
	case SourceInvoiceRelease:
		return RuleInvoiceRelease
	case SourceBankOutRelease:
		return RuleBankOutRelease
	case SourceBankInRelease:
		return RuleBankInRelease
	}
	return RuleBillRelease
}

// IsRelease says whether a source type is a month's release of a spread.
func IsRelease(source string) bool {
	switch source {
	case SourceBillRelease, SourceInvoiceRelease, SourceBankOutRelease, SourceBankInRelease:
		return true
	}
	return false
}

// ReplayRelease re-runs the rule version that posted a release's entry over the same release (ENG-06).
func ReplayRelease(rule string, r Release, s Settings) (Entry, error) {
	f, ok := ReleaseRules(r.Source)[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(r, s)
}
