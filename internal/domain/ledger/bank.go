package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The bank transaction's source type and rule version (FR-BNK-06, Appendix B.1; p1-bank-import-reconcile task 4.1):
// a bank, card, wallet or cash line categorised by a person or a rule. Its spread releases from 1210 Prepayments when
// money went out, and from 2400 Deferred revenue when it came in, each month from the posting month (B.1: a bank line
// has no service period). RoleSuspense is the account a line that cannot be classified yet posts to (2999, FR-BNK-11).
const (
	SourceBankTransaction = "bank_transaction"
	RuleBankTransaction   = "bank_transaction@1"
	SourceBankOutRelease  = "bank_out_release"
	RuleBankOutRelease    = "bank_out_release@1"
	SourceBankInRelease   = "bank_in_release"
	RuleBankInRelease     = "bank_in_release@1"
	RoleSuspense          = "suspense"
)

// CodeBankPart is a bank transaction's part that does not fit the line: another direction, parts that do not add up
// to it, or an account the line posts to by another path (a money account, receivables or payables).
const CodeBankPart = "LEDGER-34"

// BankPart is one part of a categorised line: its account, its share of the line's signed gross in the line's
// currency, the GST rate it bears as a fraction ("0.09"; "0" for a zero-rated or exempt code), empty when it bears
// none (the organisation is not GST-registered at the line's date, or the code carries no GST: the domain decides), the
// months it spreads over (0 for none) and its description.
type BankPart struct {
	AccountID   uuid.UUID
	Amount      money.Money
	GSTRate     string
	Months      int
	Description string
}

// BankTransaction is one version of a categorised bank line, as the rule reads it: its date (the line's booking date,
// its tax point), the money account's chart account, the line's signed gross in that account's currency (money in
// positive, money out negative), the rate into the base currency for an account in another currency (nil for one in
// the base currency), and its parts, each of the line's sign, which add up to it.
type BankTransaction struct {
	ID      uuid.UUID
	Version int
	Date    time.Time
	Memo    string
	Account uuid.UUID
	Amount  money.Money
	Rate    *money.Rate
	Parts   []BankPart
}

// BankTransactionIdentity is the posting identity of a bank transaction version under a rule version (§11.4).
func BankTransactionIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceBankTransaction, id, version, rule)
}

// PostBankTransaction turns one version of a categorised bank line into its entry (bank_transaction@1; Appendix B.1):
//   - the money account takes + the signed gross, at the rate for an account in another currency (RND-FX);
//   - 2200 GST control takes − the GST of the parts that bear it, each GST = RND-GST-INCL(the part's signed gross in
//     the base currency): output tax when money came in, input tax when it went out;
//   - each part's account takes − (its signed gross in the base currency − its GST), in the base currency; but a part
//     spread over months puts it on 2400 Deferred revenue (money in) or 1210 Prepayments (money out), released monthly
//     from the posting month under ALLOC-EVEN (bank_in_release@1, bank_out_release@1). Only a P&L category spreads
//     (B.1): revenue for money in, expense for money out;
//   - and, for a line in another currency split into parts, the cent RND-FX leaves between the parts and the line to
//     7030.
//
// Every rule is checked first and every failure returned together (ENG-22).
func PostBankTransaction(t BankTransaction, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if t.Date.IsZero() {
		add(CodeNoDate, "date", "the bank line has no date", "Import the line with its booking date")
	}
	if len(t.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(t.Memo), MaxMemo), "Shorten the description")
	}
	acc, ok := s.Accounts[t.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "money_account", "the account does not exist in this organisation", "Import the line into one of your money accounts")
	case acc.Group || !acc.Active || !acc.Money:
		add(CodeNotMoneyAccount, "money_account", fmt.Sprintf("account %s is not an active money account", acc.Code), "Reactivate the money account in the chart")
	}
	cur := s.Base
	if ok && acc.Currency != "" {
		if c, err := money.Lookup(acc.Currency); err == nil {
			cur = c
		}
	}
	if ok && t.Amount.Currency() != cur {
		add(CodeLineCurrency, "amount", fmt.Sprintf("the line is in %s; account %s is in %s", t.Amount.Currency(), acc.Code, cur),
			"Import the line into the money account of its currency")
	}
	if t.Amount.IsZero() {
		add(CodeZeroLine, "amount", "the line's amount is zero", "Exclude the line: a line of nothing moves no money")
	}
	foreign := t.Amount.Currency() != s.Base
	rate := money.Identity(s.Base)
	rated := true
	switch {
	case !foreign && t.Rate != nil:
		add(CodeRateOnBase, "fx_rate", fmt.Sprintf("the line is in %s, the base currency, so it takes no rate", s.Base), "Leave the rate out")
	case foreign && t.Rate == nil:
		add(CodeMissingRate, "fx_rate", fmt.Sprintf("the line is in %s, so it needs a rate into %s", t.Amount.Currency(), s.Base),
			"Add a rate for the line's date in Accounting › Currencies")
		rated = false
	case foreign && (t.Rate.From != t.Amount.Currency() || t.Rate.To != s.Base):
		add(CodeWrongRate, "fx_rate", fmt.Sprintf("the rate converts %s into %s, but the line needs %s into %s", t.Rate.From, t.Rate.To, t.Amount.Currency(), s.Base),
			"Use a rate from the line's currency into the base currency")
		rated = false
	case foreign:
		rate = *t.Rate
	}
	in := t.Amount.Sign() > 0
	if len(t.Parts) == 0 {
		add(CodeTooFewLines, "parts", "the line has no category", "Choose the account the money is for")
	}
	gstControl, hasGST := s.byRole(RoleGSTControl)
	holdingRole, holdingName, spreadType := RolePrepayments, "prepayments", "expense"
	if in {
		holdingRole, holdingName, spreadType = RoleDeferredRevenue, "deferred revenue", "revenue"
	}
	holding, hasHolding := s.byRole(holdingRole)
	sum := money.Zero(t.Amount.Currency())
	taxed, spread := false, false
	for i, p := range t.Parts {
		f := func(name string) string { return fmt.Sprintf("parts[%d].%s", i+1, name) }
		a, known := s.Accounts[p.AccountID]
		switch {
		case !known:
			add(CodeUnknownAccount, f("account"), "the account does not exist in this organisation", "Choose an account from the chart")
		case a.Group:
			add(CodeGroupAccount, f("account"), fmt.Sprintf("account %s is a group heading", a.Code), "Choose an account under it")
		case !a.Active:
			add(CodeInactiveAccount, f("account"), fmt.Sprintf("account %s is inactive", a.Code), "Reactivate it or choose another account")
		case a.ShareCapital:
			add(CodeShareCapital, f("account"), fmt.Sprintf("account %s is share capital, which changes only through the register of share issues", a.Code),
				"Record the share issue, and match the line to it")
		case a.Money:
			add(CodeBankPart, f("account"), fmt.Sprintf("account %s is a money account", a.Code), "Record a transfer between the two money accounts instead")
		case a.Role == RoleAccountsReceivable || a.Role == RoleAccountsPayable:
			add(CodeBankPart, f("account"), fmt.Sprintf("account %s holds what customers or suppliers owe, which only their documents change", a.Code),
				"Match the line to the invoice or bill it settles instead")
		case a.Role == RoleGSTControl && p.GSTRate != "":
			add(CodeRuleAccount, f("account"), fmt.Sprintf("account %s is the one the line posts its GST to", a.Code),
				"Choose the GST code NA for a payment to or a refund from IRAS")
		case a.Role == holdingRole && hasHolding && spreads(t.Parts):
			add(CodeSpread, f("account"), fmt.Sprintf("account %s holds the line's spread parts", a.Code), "Choose the account the money is for, and spread that part")
		case a.Currency != "" && a.Currency != s.Base.Code:
			add(CodeLineCurrency, f("account"), fmt.Sprintf("account %s is held in %s", a.Code, a.Currency), fmt.Sprintf("Choose an account kept in %s", s.Base))
		}
		if len(p.Description) > MaxDescription {
			add(CodeTooLong, f("description"), fmt.Sprintf("the description has %d characters; the most is %d", len(p.Description), MaxDescription), "Shorten it")
		}
		if p.Amount.Currency() != t.Amount.Currency() {
			add(CodeLineCurrency, f("amount"), fmt.Sprintf("the part is in %s; the line is in %s", p.Amount.Currency(), t.Amount.Currency()),
				"Enter each part in the line's currency")
			continue
		}
		if p.Amount.Sign() != t.Amount.Sign() {
			add(CodeBankPart, f("amount"), fmt.Sprintf("the part is %s %s; the line is %s %s", p.Amount, p.Amount.Currency(), t.Amount, t.Amount.Currency()),
				"Give every part the line's direction: money in positive, money out negative")
		}
		sum, _ = sum.Add(p.Amount)
		if p.GSTRate != "" {
			if _, err := rounding.GSTIncl(p.Amount, p.GSTRate); err != nil {
				add(CodeGSTRate, f("gst_code"), err.Error(), "Choose the part's GST code again: its rate comes from the sg-gst pack")
			}
			taxed = true
		}
		if p.Months != 0 {
			spread = true
			switch {
			case p.Months < 1 || p.Months > MaxSpreadMonths:
				add(CodeSpread, f("spread_months"), fmt.Sprintf("the part is spread over %d months", p.Months), fmt.Sprintf("Spread it over 1 to %d months", MaxSpreadMonths))
			case known && a.Type != spreadType:
				add(CodeSpread, f("spread_months"), fmt.Sprintf("account %s is not a %s account, so the part is not spread", a.Code, spreadType),
					fmt.Sprintf("Spread only money %s on a %s account (Appendix B.1: P&L categories only)", map[bool]string{true: "in", false: "out"}[in], spreadType))
			}
		}
	}
	if len(t.Parts) > 0 && sum.Currency() == t.Amount.Currency() && !sum.Equal(t.Amount) {
		add(CodeBankPart, "parts", fmt.Sprintf("the parts add up to %s %s; the line is %s %s", sum, sum.Currency(), t.Amount, t.Amount.Currency()),
			"Split the line into parts that add up to it")
	}
	if taxed && !hasGST {
		add(CodeSystemRole, "parts", "the chart has no GST control account for the GST", "Load the standard chart's 2200 GST control, or give a liability account the role")
	}
	if spread && !hasHolding {
		add(CodeSystemRole, "parts", "the chart has no "+holdingName+" account for the spread", "Load the standard chart's 1210 Prepayments and 2400 Deferred revenue, or give accounts the roles")
	}
	if len(probs) > 0 || !rated {
		return Entry{}, probs
	}
	id := money.Identity(s.Base)
	gross, err := rounding.FX(t.Amount, rate)
	if err != nil {
		return Entry{}, Problems{{CodeBaseRange, "amount", err.Error(), "Check the amount and the rate"}}
	}
	lines := []Line{{AccountID: t.Account, Amount: t.Amount, Rate: rate, Base: gross, Description: t.Memo}}
	tax := money.Zero(s.Base)
	parts := money.Zero(s.Base)
	for i, p := range t.Parts {
		base, err := rounding.FX(p.Amount, rate)
		if err != nil {
			return Entry{}, Problems{{CodeBaseRange, fmt.Sprintf("parts[%d].amount", i+1), err.Error(), "Check the amount and the rate"}}
		}
		parts, _ = parts.Add(base)
		gst := money.Zero(s.Base)
		if p.GSTRate != "" {
			gst, _ = rounding.GSTIncl(base, p.GSTRate)
			tax, _ = tax.Add(gst)
		}
		net, _ := base.Sub(gst)
		to := p.AccountID
		if p.Months != 0 {
			to = holding.ID
		}
		if !net.IsZero() {
			lines = append(lines, Line{AccountID: to, Amount: net.Neg(), Rate: id, Base: net.Neg(), Description: p.Description})
		}
	}
	if !tax.IsZero() {
		what := "Input tax"
		if in {
			what = "Output tax"
		}
		lines = append(lines, Line{AccountID: gstControl.ID, Amount: tax.Neg(), Rate: id, Base: tax.Neg(), Description: what})
	}
	if diff, _ := gross.Sub(parts); !diff.IsZero() {
		fxr, ok := s.byRole(RoleFXRounding)
		if !ok {
			return Entry{}, Problems{{CodeSystemRole, "parts", "the chart has no FX and rounding account for the cent RND-FX leaves between the parts",
				"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
		}
		lines = append(lines, Line{AccountID: fxr.ID, Amount: diff.Neg(), Rate: id, Base: diff.Neg(), Description: "Rounding of the parts into " + s.Base.Code})
	}
	y, m, d := t.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceBankTransaction, ID: t.ID.String(), Version: t.Version},
		RuleVersion:     RuleBankTransaction,
		PostingIdentity: BankTransactionIdentity(t.ID, t.Version, RuleBankTransaction),
		Memo:            t.Memo,
		Lines:           lines,
	}, nil
}

// spreads says whether any part spreads.
func spreads(parts []BankPart) bool {
	for _, p := range parts {
		if p.Months != 0 {
			return true
		}
	}
	return false
}

// BankTransactionRule turns one version of a categorised bank line into its entry under one rule version.
type BankTransactionRule func(t BankTransaction, s Settings) (Entry, error)

// BankTransactionRules are every bank transaction rule version the engine has posted with, by name: like Rules, a
// version is never removed or changed while an entry names it.
var BankTransactionRules = map[string]BankTransactionRule{
	RuleBankTransaction: PostBankTransaction,
}

// ReplayBankTransaction re-runs the rule version that posted a bank transaction's entry over the same version (ENG-06).
func ReplayBankTransaction(rule string, t BankTransaction, s Settings) (Entry, error) {
	f, ok := BankTransactionRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(t, s)
}

// BankTransactionReleases is the release schedule of a bank transaction version's spread parts (FR-GL-03, Appendix
// B.1): for each, in part order, what its entry e put on 1210 (money out) or 2400 (money in), split over its months
// under ALLOC-EVEN from the posting month. e is the transaction's entry under bank_transaction@1, whose holding lines
// are its spread parts' in order. A transaction with no spread has no releases.
func BankTransactionReleases(t BankTransaction, e Entry, s Settings) ([]Release, error) {
	source, role, what := SourceBankOutRelease, RolePrepayments, "prepayments"
	if t.Amount.Sign() > 0 {
		source, role, what = SourceBankInRelease, RoleDeferredRevenue, "deferred revenue"
	}
	holding, ok := s.byRole(role)
	var amounts []money.Money
	for _, l := range e.Lines {
		if ok && l.AccountID == holding.ID {
			b := l.Base
			if source == SourceBankInRelease {
				b = b.Neg() // money in credits 2400
			}
			amounts = append(amounts, b)
		}
	}
	lines := make([]scheduledLine, 0, len(t.Parts))
	for _, p := range t.Parts {
		var x *Spread
		if p.Months != 0 {
			x = &Spread{From: t.Date, Months: p.Months}
		}
		lines = append(lines, scheduledLine{AccountID: p.AccountID, Description: p.Description, Spread: x})
	}
	return releases(source, t.ID, t.Version, t.Date, t.Memo, lines, amounts, what)
}

var (
	bankOutRelease = releaseKind{rule: RuleBankOutRelease, role: RolePrepayments, holding: "prepayments", spread: "cost", account: "an expense account",
		missing: "Load the standard chart's 1210 Prepayments, or give an asset account the role", toLine: true}
	bankInRelease = releaseKind{rule: RuleBankInRelease, role: RoleDeferredRevenue, holding: "deferred revenue", spread: "revenue", account: "a revenue account",
		missing: "Load the standard chart's 2400 Deferred revenue, or give a liability account the role"}
)

// PostBankOutRelease turns one month's release of a spread part of money out into its entry (bank_out_release@1), in
// the base currency on the release's date: Dr the part's account and Cr 1210 Prepayments with the part (ENG-05).
func PostBankOutRelease(r Release, s Settings) (Entry, error) {
	if r.Source != SourceBankOutRelease {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("%s releases money out of a bank line, not a %s", RuleBankOutRelease, r.Source),
			"Release it under its own document's rule"}}
	}
	return postRelease(r, s, bankOutRelease)
}

// PostBankInRelease turns one month's release of a spread part of money in into its entry (bank_in_release@1), in the
// base currency on the release's date: Dr 2400 Deferred revenue and Cr the part's account with the part (ENG-05).
func PostBankInRelease(r Release, s Settings) (Entry, error) {
	if r.Source != SourceBankInRelease {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("%s releases money into a bank line, not a %s", RuleBankInRelease, r.Source),
			"Release it under its own document's rule"}}
	}
	return postRelease(r, s, bankInRelease)
}
