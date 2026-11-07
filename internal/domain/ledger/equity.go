package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The role of share application monies, the source type of the equity register's own entries, and the rule versions
// of a cash issue and a conversion between classes (FR-EQ-17, B.7, INV-13; p1-equity-register design D3 and D5).
const (
	RoleShareApplicationMonies = "share-application-monies" // 2040 Share application monies, keyed by subscriber (B.7)
	SourceEquityTransaction    = "equity_transaction"
	RuleCashIssue              = "share_issue_cash@1"
	RuleClassConversion        = "equity_transaction.class_conversion@1"
	CodeEquity                 = "LEDGER-48"
	cashIssueVersion           = 1
)

// SubscriberAccount reports whether the account holds share application monies, whose lines name their subscriber (B.7).
func (a Account) SubscriberAccount() bool { return a.Role == RoleShareApplicationMonies }

// CashIssue is one share issue paid in cash, as the rule reads it (Appendix B.7, G4-043): the issue (its ID is the
// entry's source), the day ACRA's register shows it, the subscriber, the account holding their application monies, the
// class's share-capital account and the amount, shares × price under RND-LINE.
type CashIssue struct {
	ID     uuid.UUID
	PostOn time.Time
	Memo   string
	Party  uuid.UUID
	Monies uuid.UUID
	Class  uuid.UUID
	Amount money.Money
}

// CashIssueIdentity is the posting identity of a cash issue under a rule version (§11.4).
func CashIssueIdentity(id uuid.UUID, rule string) string {
	return identity(SourceShareIssue, id, cashIssueVersion, rule)
}

// PostCashIssue turns a cash issue into its entry (share_issue_cash@1), on the day ACRA's register shows the allotment:
// Dr share application monies naming the subscriber · Cr the class's share capital, both with the amount. The cash came
// in earlier, to share application monies (B.7); the share capital moves only now, from the register (INV-13). Every
// rule is checked first and every failure returned together (ENG-22).
func PostCashIssue(c CashIssue, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if c.PostOn.IsZero() {
		add(CodeNoDate, "effective_on", "the issue has no effective date", "Enter the day ACRA's register of members shows the allotment")
	}
	if len(c.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(c.Memo), MaxMemo), "Shorten it")
	}
	monies, ok := s.Accounts[c.Monies]
	switch {
	case !ok || !monies.SubscriberAccount() || monies.Group || !monies.Active:
		add(CodeEquity, "monies", "the organisation has no active share application monies account", "Add 2040 Share application monies to the chart of accounts")
	default:
		if p := partyProblem(s, monies, c.Party, "holder_id"); p != nil {
			probs = append(probs, *p)
		}
	}
	if class, ok := s.Accounts[c.Class]; !ok || !class.ShareCapital || class.Group || !class.Active {
		add(CodeEquity, "class_account_id", "the class is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
	}
	if c.Amount.Currency() != s.Base || c.Amount.Sign() <= 0 {
		add(CodeEquity, "amount", fmt.Sprintf("the amount is %s %s", c.Amount, c.Amount.Currency()), fmt.Sprintf("Issue shares for an amount in %s, more than zero", s.Base))
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	base := money.Identity(s.Base)
	y, m, d := c.PostOn.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceShareIssue, ID: c.ID.String(), Version: cashIssueVersion},
		RuleVersion:     RuleCashIssue,
		PostingIdentity: CashIssueIdentity(c.ID, RuleCashIssue),
		Memo:            c.Memo,
		Lines: []Line{
			{AccountID: c.Monies, Amount: c.Amount, Rate: base, Base: c.Amount, Description: c.Memo, Party: c.Party},
			{AccountID: c.Class, Amount: c.Amount.Neg(), Rate: base, Base: c.Amount.Neg(), Description: c.Memo},
		},
	}, nil
}

// CashIssueRules are every rule version the engine has posted cash issues with, by name.
var CashIssueRules = map[string]func(c CashIssue, s Settings) (Entry, error){
	RuleCashIssue: PostCashIssue,
}

// ReplayCashIssue re-runs the rule version that posted a cash issue's entry over the same issue (ENG-06).
func ReplayCashIssue(rule string, c CashIssue, s Settings) (Entry, error) {
	f, ok := CashIssueRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(c, s)
}

// ClassConversion is a conversion of shares between classes whose share-capital accounts differ, as the rule reads it
// (FR-EQ-17): the equity transaction and its version, the day ACRA's register shows it, the account the shares leave,
// the account they join, and the carrying amount the accountant moved between them [accountant to confirm the basis].
type ClassConversion struct {
	ID      uuid.UUID
	Version int
	PostOn  time.Time
	Memo    string
	From    uuid.UUID
	To      uuid.UUID
	Amount  money.Money
}

// ClassConversionIdentity is the posting identity of a conversion between classes under a rule version (§11.4).
func ClassConversionIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceEquityTransaction, id, version, rule)
}

// PostClassConversion turns a conversion between classes into its entry (equity_transaction.class_conversion@1): Dr the
// share capital the shares leave · Cr the share capital they join, with the carrying amount, on the effective date.
// Share capital moves only from the equity register (INV-13, rev 0.205).
func PostClassConversion(c ClassConversion, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if c.PostOn.IsZero() {
		add(CodeNoDate, "effective_on", "the conversion has no effective date", "Enter the day ACRA's register of members shows it")
	}
	if len(c.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(c.Memo), MaxMemo), "Shorten it")
	}
	for _, x := range []struct {
		field string
		id    uuid.UUID
	}{{"class_account_id", c.From}, {"to_class_account_id", c.To}} {
		if a, ok := s.Accounts[x.id]; !ok || !a.ShareCapital || a.Group || !a.Active {
			add(CodeEquity, x.field, "the class is not an active share-capital account", "Choose 3010 Ordinary or 3020 Preference share capital")
		}
	}
	if c.From == c.To {
		add(CodeEquity, "to_class_account_id", "the classes share one account, so the conversion moves no share capital", "Leave the amount out")
	}
	if c.Amount.Currency() != s.Base || c.Amount.Sign() <= 0 {
		add(CodeEquity, "amount", fmt.Sprintf("the amount is %s %s", c.Amount, c.Amount.Currency()), fmt.Sprintf("Enter the carrying amount in %s, more than zero", s.Base))
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	base := money.Identity(s.Base)
	y, m, d := c.PostOn.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceEquityTransaction, ID: c.ID.String(), Version: c.Version},
		RuleVersion:     RuleClassConversion,
		PostingIdentity: ClassConversionIdentity(c.ID, c.Version, RuleClassConversion),
		Memo:            c.Memo,
		Lines: []Line{
			{AccountID: c.From, Amount: c.Amount, Rate: base, Base: c.Amount, Description: c.Memo},
			{AccountID: c.To, Amount: c.Amount.Neg(), Rate: base, Base: c.Amount.Neg(), Description: c.Memo},
		},
	}, nil
}

// ClassConversionRules are every rule version the engine has posted conversions between classes with, by name.
var ClassConversionRules = map[string]func(c ClassConversion, s Settings) (Entry, error){
	RuleClassConversion: PostClassConversion,
}

// ReplayClassConversion re-runs the rule version that posted a conversion between classes over the same version (ENG-06).
func ReplayClassConversion(rule string, c ClassConversion, s Settings) (Entry, error) {
	f, ok := ClassConversionRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(c, s)
}
