package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The source type and rule version of a cost a founder paid before the books start (FR-EQ-03, B.7 "Cost a founder paid
// personally, kept as a loan"; p1-founders-money design D7).
const (
	SourcePreIncorporationCost = "pre_incorporation_cost"
	RulePreIncorporationCost   = "pre_incorporation_cost@1"
)

// CodePreIncorporationCost is a cost before the books start that does not fit: an account the accountant may not post
// it to, an amount or GST that is not a positive amount in the base currency, or GST claimed while the organisation was
// not GST-registered.
const CodePreIncorporationCost = "LEDGER-42"

// PreIncorporationCost is one version of a cost before the books start, decided, as the rule reads it: the date it
// posts on (the books start, or the decision date when the books start's period is closed), the director or
// shareholder who paid it, the account the accountant chose, the gross they paid in the base currency, the GST the
// receipt shows, and whether the accountant claims that GST as input tax (Registered says the organisation was
// GST-registered on the posting date, the only time it may).
type PreIncorporationCost struct {
	ID         uuid.UUID
	Version    int
	PostOn     time.Time
	Memo       string
	Party      uuid.UUID
	Account    uuid.UUID
	Gross      money.Money
	GST        money.Money
	ClaimGST   bool
	Registered bool
}

// PreIncorporationCostIdentity is the posting identity of a cost's version under a rule version (§11.4).
func PreIncorporationCostIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourcePreIncorporationCost, id, version, rule)
}

// PostPreIncorporationCost turns one decided version of a cost before the books start into its entry
// (pre_incorporation_cost@1), on its posting date:
//   - Dr the account the accountant chose with the gross, or with the gross less the GST when the GST is claimed;
//   - Dr 2200 GST control with the GST, when claimed;
//   - Cr 2500 Loan from director / shareholder with the gross, naming the person who paid it (B.7: paid personally).
//
// Every rule is checked first and every failure returned together (ENG-22).
func PostPreIncorporationCost(c PreIncorporationCost, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if c.PostOn.IsZero() {
		add(CodeNoDate, "date", "the cost has no posting date", "Set the books start in Settings › Organisation")
	}
	if len(c.Memo) > MaxMemo {
		add(CodeTooLong, "description", fmt.Sprintf("the description has %d characters; the most is %d", len(c.Memo), MaxMemo), "Shorten it")
	}
	acc, ok := s.Accounts[c.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "account_id", "the account does not exist in this organisation", "Choose an account from the chart of accounts")
	case acc.Group || !acc.Active:
		add(CodePreIncorporationCost, "account_id", fmt.Sprintf("account %s is not an active, postable account", acc.Code), "Choose an active account that is not a group")
	case acc.ShareCapital || acc.Money || acc.DirectorAccount() || acc.Role == RoleGSTControl:
		add(CodePreIncorporationCost, "account_id", fmt.Sprintf("account %s is share capital, a money account, a director's account or GST control", acc.Code),
			"Choose the cost, asset or other account the cost belongs to")
	}
	loan, hasLoan := s.byRole(RoleDirectorLoan)
	if !hasLoan {
		add(CodeSystemRole, "contact_id", "the chart has no loan from director / shareholder account",
			"Load the standard chart's 2500 Loan from director / shareholder, or give a liability account the role")
	} else if p := partyProblem(s, loan, c.Party, "contact_id"); p != nil {
		probs = append(probs, *p)
	}
	if c.Gross.Currency() != s.Base || c.Gross.Sign() <= 0 {
		add(CodePreIncorporationCost, "gross", fmt.Sprintf("the amount is %s %s", c.Gross, c.Gross.Currency()), fmt.Sprintf("Enter what they paid in %s, more than zero", s.Base))
	}
	gst := c.GST
	if gst.Currency().Code == "" { // none shown
		gst = money.Zero(s.Base)
	}
	more := 0
	if gst.Currency() == s.Base && c.Gross.Currency() == s.Base {
		more, _ = gst.Cmp(c.Gross)
	}
	if gst.Currency() != s.Base || gst.Sign() < 0 || (c.Gross.Sign() > 0 && more >= 0) {
		add(CodePreIncorporationCost, "gst", fmt.Sprintf("the GST shown is %s %s", c.GST, c.GST.Currency()),
			fmt.Sprintf("Enter the GST the receipt shows in %s: zero or more, and less than the amount", s.Base))
	}
	gstAcc, hasGST := s.byRole(RoleGSTControl)
	switch {
	case !c.ClaimGST:
	case gst.IsZero():
		add(CodePreIncorporationCost, "claim_gst", "the receipt shows no GST to claim", "Leave the GST unclaimed, or enter the GST it shows")
	case !c.Registered:
		add(CodePreIncorporationCost, "claim_gst", "the organisation was not GST-registered on the posting date, so it claims no input tax",
			"Leave the GST in the cost; it is claimed only in the first GST return, with IRAS's approval [verify]")
	case !hasGST:
		add(CodeSystemRole, "claim_gst", "the chart has no GST control account", "Load the standard chart's 2200 GST control, or give a liability account the role")
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	y, m, d := c.PostOn.Date()
	base := money.Identity(s.Base)
	cost := c.Gross
	var lines []Line
	if c.ClaimGST {
		cost, _ = c.Gross.Sub(gst)
	}
	lines = append(lines, Line{AccountID: c.Account, Amount: cost, Rate: base, Base: cost, Description: c.Memo})
	if c.ClaimGST {
		lines = append(lines, Line{AccountID: gstAcc.ID, Amount: gst, Rate: base, Base: gst, Description: c.Memo})
	}
	lines = append(lines, Line{AccountID: loan.ID, Amount: c.Gross.Neg(), Rate: base, Base: c.Gross.Neg(), Description: c.Memo, Party: c.Party})
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourcePreIncorporationCost, ID: c.ID.String(), Version: c.Version},
		RuleVersion:     RulePreIncorporationCost,
		PostingIdentity: PreIncorporationCostIdentity(c.ID, c.Version, RulePreIncorporationCost),
		Memo:            c.Memo,
		Lines:           lines,
	}, nil
}

// PreIncorporationCostRule turns one version of a cost into its entry under one rule version.
type PreIncorporationCostRule func(c PreIncorporationCost, s Settings) (Entry, error)

// PreIncorporationCostRules are every rule version the engine has posted costs before the books start with, by name.
var PreIncorporationCostRules = map[string]PreIncorporationCostRule{
	RulePreIncorporationCost: PostPreIncorporationCost,
}

// ReplayPreIncorporationCost re-runs the rule version that posted a cost's entry over the same version (ENG-06).
func ReplayPreIncorporationCost(rule string, c PreIncorporationCost, s Settings) (Entry, error) {
	f, ok := PreIncorporationCostRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(c, s)
}
