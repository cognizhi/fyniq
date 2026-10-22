package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The release's source type and rule version (FR-GL-03, ENG-05; posting rule "Spreading an invoice or bill", Appendix
// B.2; rev 0.83). A release names the bill version whose spread it releases.
const (
	SourceBillRelease = "bill_release"
	RuleBillRelease   = "bill_release@1"
)

// BillRelease is one month's part of a bill line's spread cost: the bill version and line (from 1), the month (its
// first day) and which part of how many it is, the date it posts on, the line's account and description, and the part,
// in the base currency.
type BillRelease struct {
	BillID      uuid.UUID
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

// BillReleaseIdentity is the posting identity of a bill version's release of one line in one month under a rule
// version (§11.4): one release per line and month, ever.
func BillReleaseIdentity(id uuid.UUID, version, line int, month time.Time, rule string) string {
	return fmt.Sprintf("%s:%s:v%d:l%d:%s:%s", SourceBillRelease, id, version, line, month.Format("2006-01"), rule)
}

// Identity is the release's posting identity under rule.
func (r BillRelease) Identity(rule string) string {
	return BillReleaseIdentity(r.BillID, r.Version, r.Line, r.Month, rule)
}

// firstOfMonth is the first day of t's month.
func firstOfMonth(t time.Time) time.Time {
	y, m, _ := t.Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

// BillReleases is the release schedule of a bill version's spread lines (FR-GL-03): for each, in the bill's line order,
// what its entry e debited to 1210 split over its months under ALLOC-EVEN, one part a month from its first month. A
// part is dated on the first day of its month, or on the bill's date when that is later: a service that started before
// the bill releases what it has used up when the bill posts. e is the bill's entry under bill@1, whose 1210 lines are
// its spread lines' in order. A bill with no spread has no releases.
func BillReleases(b Bill, e Entry, s Settings) ([]BillRelease, error) {
	prepay, ok := s.byRole(RolePrepayments)
	var spread []Line
	for _, l := range e.Lines {
		if ok && l.AccountID == prepay.ID {
			spread = append(spread, l)
		}
	}
	var out []BillRelease
	y, m, d := b.Date.Date()
	date := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	k := 0
	for i, l := range b.Lines {
		if l.Spread == nil {
			continue
		}
		if k >= len(spread) {
			return nil, fmt.Errorf("the bill's entry has %d lines on prepayments, fewer than its spread lines", len(spread))
		}
		parts, err := rounding.AllocEven(spread[k].Base, l.Spread.Months)
		if err != nil {
			return nil, err
		}
		k++
		from := firstOfMonth(l.Spread.From)
		for j, part := range parts {
			month := from.AddDate(0, j, 0)
			on := month
			if date.After(on) {
				on = date
			}
			out = append(out, BillRelease{BillID: b.ID, Version: b.Version, Line: i + 1, Month: month, Part: j + 1, Of: len(parts), Date: on,
				AccountID: l.AccountID, Description: l.Description, Memo: b.Memo, Amount: part})
		}
	}
	if k != len(spread) {
		return nil, fmt.Errorf("the bill's entry has %d lines on prepayments, more than its %d spread lines", len(spread), k)
	}
	return out, nil
}

// PostBillRelease turns one month's release of a spread bill line into its entry (bill_release@1), in the base
// currency on the release's date: Dr the line's account and Cr 1210 Prepayments with the part (ENG-05: posted by the
// scheduler as a real entry, up to the current period only). Every rule is checked first and every failure returned
// together (ENG-22).
func PostBillRelease(r BillRelease, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if r.Date.IsZero() || r.Month.IsZero() {
		add(CodeNoDate, "date", "the release has no date", "Release a month of a posted bill's spread")
	}
	if r.Amount.Currency() != s.Base {
		add(CodeLineCurrency, "amount", fmt.Sprintf("the release is in %s, not the base currency %s", r.Amount.Currency(), s.Base),
			"Release the base amount the bill put on prepayments")
	}
	memo := fmt.Sprintf("%s: cost spread for %s (%d of %d)", r.Memo, r.Month.Format("January 2006"), r.Part, r.Of)
	if len(memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(memo), MaxMemo), "Shorten the bill's memo")
	}
	prepay, ok := s.byRole(RolePrepayments)
	if !ok {
		add(CodeSystemRole, "lines", "the chart has no prepayments account to release from", "Load the standard chart's 1210 Prepayments, or give an asset account the role")
	}
	acc, known := s.Accounts[r.AccountID]
	switch {
	case !known:
		add(CodeUnknownAccount, "account", "the account does not exist in this organisation", "Choose an expense account from the chart")
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
	y, m, d := r.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceBillRelease, ID: r.BillID.String(), Version: r.Version},
		RuleVersion:     RuleBillRelease,
		PostingIdentity: r.Identity(RuleBillRelease),
		Memo:            memo,
		Lines: []Line{
			{AccountID: r.AccountID, Amount: r.Amount, Rate: id, Base: r.Amount, Description: r.Description},
			{AccountID: prepay.ID, Amount: r.Amount.Neg(), Rate: id, Base: r.Amount.Neg(), Description: r.Description},
		},
	}, nil
}

// BillReleaseRule turns one month's release into its entry under one rule version.
type BillReleaseRule func(r BillRelease, s Settings) (Entry, error)

// BillReleaseRules are every release rule version the engine has posted with, by name: like Rules, a version is never
// removed or changed while an entry names it.
var BillReleaseRules = map[string]BillReleaseRule{
	RuleBillRelease: PostBillRelease,
}

// ReplayBillRelease re-runs the rule version that posted a release's entry over the same release (ENG-06).
func ReplayBillRelease(rule string, r BillRelease, s Settings) (Entry, error) {
	f, ok := BillReleaseRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(r, s)
}
