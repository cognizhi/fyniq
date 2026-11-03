package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The source type of the register of share issues, and the rule version of a founder's balance converted to shares
// (FR-EQ-06, FR-EQ-08; p1-founders-money design D11). Share capital takes postings only from this source or the opening
// balances (INV-13).
const (
	SourceShareIssue  = "share_issue"
	RuleConversion    = "share_issue@1"
	CodeConversion    = "LEDGER-46"
	conversionVersion = 1
)

// Conversion is one share issue that capitalises what the company owes a director or shareholder, as the rule reads it:
// the issue (its ID is the entry's source), the day ACRA's register shows it, the person, the director's account the
// amount comes off, the class's share-capital account, the amount converted (RND-SHARE's) and what share capital is
// credited with: the converted amount under the carrying-amount basis, or the shares' fair value with the difference
// in profit or loss on the account the accountant chose (IFRIC 19 ¶9; SFRS for Small Entities ¶22.15A–C).
type Conversion struct {
	ID         uuid.UUID
	PostOn     time.Time
	Memo       string
	Party      uuid.UUID
	Account    uuid.UUID
	Class      uuid.UUID
	Converted  money.Money
	Equity     money.Money
	Difference uuid.UUID
}

// ConversionIdentity is the posting identity of a share issue's conversion under a rule version (§11.4).
func ConversionIdentity(id uuid.UUID, rule string) string {
	return identity(SourceShareIssue, id, conversionVersion, rule)
}

// PostConversion turns a conversion into its entry (share_issue@1), on the day ACRA's register shows the allotment: Dr
// the director's account (2500 or 2120) with the converted amount, naming the person (B.7: converted) · Cr the class's
// share capital with the equity amount · and, when the two differ, the difference to the profit-or-loss account chosen
// (Cr a gain, Dr a loss). Every rule is checked first and every failure returned together (ENG-22).
func PostConversion(c Conversion, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if c.PostOn.IsZero() {
		add(CodeNoDate, "effective_on", "the conversion has no effective date", "Enter the day ACRA's register of members shows the allotment")
	}
	if len(c.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(c.Memo), MaxMemo), "Shorten it")
	}
	owed, ok := s.Accounts[c.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "account_id", "the account does not exist in this organisation", "Convert a balance on 2500 or 2120")
	case !owed.DirectorAccount():
		add(CodeConversion, "account_id", fmt.Sprintf("account %s is not a director's account", owed.Code), "Convert a balance on 2500 or 2120")
	default:
		if p := partyProblem(s, owed, c.Party, "contact_id"); p != nil {
			probs = append(probs, *p)
		}
	}
	if class, ok := s.Accounts[c.Class]; !ok || !class.ShareCapital || class.Group || !class.Active {
		add(CodeConversion, "conversion.class_account_id", "the class is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
	}
	for _, x := range []struct {
		field string
		m     money.Money
	}{{"amount", c.Converted}, {"conversion.equity", c.Equity}} {
		if x.m.Currency() != s.Base || x.m.Sign() <= 0 {
			add(CodeConversion, x.field, fmt.Sprintf("the amount is %s %s", x.m, x.m.Currency()), fmt.Sprintf("Convert an amount in %s, more than zero", s.Base))
		}
	}
	var gainOrLoss money.Money
	if len(probs) == 0 {
		var err error
		if gainOrLoss, err = c.Equity.Sub(c.Converted); err != nil {
			return Entry{}, err
		}
		if gainOrLoss.Sign() != 0 {
			d, ok := s.Accounts[c.Difference]
			if !ok || d.Group || !d.Active || (d.Type != "revenue" && d.Type != "expense") || d.Money || d.DirectorAccount() {
				add(CodeConversion, "conversion.difference_account_id", "the difference between the debt and the shares' fair value goes to an active income or expense account",
					"Choose the profit-or-loss account the accountant picked")
			}
		}
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	base := money.Identity(s.Base)
	lines := []Line{
		{AccountID: c.Account, Amount: c.Converted, Rate: base, Base: c.Converted, Description: c.Memo, Party: c.Party},
		{AccountID: c.Class, Amount: c.Equity.Neg(), Rate: base, Base: c.Equity.Neg(), Description: c.Memo},
	}
	if gainOrLoss.Sign() != 0 { // fair value above the debt is a loss (Dr), below it a gain (Cr)
		lines = append(lines, Line{AccountID: c.Difference, Amount: gainOrLoss, Rate: base, Base: gainOrLoss, Description: c.Memo})
	}
	y, m, d := c.PostOn.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceShareIssue, ID: c.ID.String(), Version: conversionVersion},
		RuleVersion:     RuleConversion,
		PostingIdentity: ConversionIdentity(c.ID, RuleConversion),
		Memo:            c.Memo,
		Lines:           lines,
	}, nil
}

// ConversionRules are every rule version the engine has posted conversions with, by name.
var ConversionRules = map[string]func(c Conversion, s Settings) (Entry, error){
	RuleConversion: PostConversion,
}

// ReplayConversion re-runs the rule version that posted a conversion's entry over the same issue (ENG-06).
func ReplayConversion(rule string, c Conversion, s Settings) (Entry, error) {
	f, ok := ConversionRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(c, s)
}
