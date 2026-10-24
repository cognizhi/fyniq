package ledger

import (
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The bill payment's source type and rule version (the supplier half of the posting rule "Customer and supplier
// payments", PRD-001 §10.2; ENG-41; the owner's decision of 22 Oct 2026 to record payments by hand until bank import).
const (
	SourceBillPayment = "bill_payment"
	RuleBillPayment   = "bill_payment@1"
)

// The bill payment rule's validation codes (ENG-22, ENG-23).
const (
	CodeNotMoneyAccount = "LEDGER-25" // a payment from an account that is not an active money account
	CodePaymentAmount   = "LEDGER-26" // a payment's amounts: none, not positive, or in another currency
	CodePaidAmount      = "LEDGER-27" // a foreign bill paid from a base-currency account names what left the account, and only then
)

// PaidBill is one bill a payment settles: the amount settled in the bill's currency, and the bill's stored rate into
// the base currency, which its accounts payable line carries (ENG-14). Clears is the bill's carrying amount left in the
// base currency when the payment settles all that is open of it; nil otherwise. RND-FX of the parts of a bill paid in
// parts need not sum to RND-FX of the whole: what the last part leaves is the residual, cleared to 7030 in the month
// of the last payment (Appendix B, "Settlement difference"), so no cent of the bill stays on 2010.
//
// Short, when set, is what is left of the bill after Amount, cleared as a settlement difference within tolerance
// (FR-BNK-08; Appendix B, "Settlement difference on an invoice or bill"): the payment settles Amount + Short of the bill
// and pays Amount; Short's carrying amount goes to 7030 on a line of its own, apart from the realised FX. Clears is set
// with it: the payment settles the bill in full. Whether Short is within the organisation's tolerances is the
// document's to decide; the rule posts it.
type PaidBill struct {
	BillID     uuid.UUID
	Amount     money.Money
	StoredRate money.Rate
	Clears     *money.Money
	Short      *money.Money
}

// BillPayment is one version of a payment of bills as the rules read it: the money account it was paid from, the bills'
// currency, the payment's rate into the base currency, what it settles, and what is left as a credit with the supplier.
type BillPayment struct {
	ID        uuid.UUID
	Version   int
	Date      time.Time
	Memo      string    // the supplier and the payment's reference
	Account   uuid.UUID // the money account it was paid from
	Currency  money.Currency
	Rate      *money.Rate  // the payment's rate into the base currency; nil for a payment in the base currency
	Paid      *money.Money // what left a base-currency money account for bills in another currency; nil otherwise
	Bills     []PaidBill
	Unapplied money.Money // in the bills' currency: kept as a credit with the supplier (FR-AR-18 mirrored)
}

// MaxResidual is the most a residual of RND-FX may be, in units of the base currency: the base tolerance of open
// question 32 (S$1.00), far above the cent a part can leave. The database holds a bill payment's base-only line to it.
const MaxResidual = 1

// Settlement is what a payment or a receipt settled of one document (a bill, an invoice, or a credit): the amount in the
// document's currency, the carrying amount it cleared in the base currency, and the realised gain (+) or loss (−) in
// the base currency (ENG-41), the allocation it records (FR-AR-18).
// Tolerance is the carrying amount a settlement tolerance cleared, in the base currency; zero for none.
type Settlement struct {
	DocumentID uuid.UUID
	Amount     money.Money
	Base       money.Money
	RealisedFX money.Money
	Tolerance  money.Money
}

// BillPaymentIdentity is the posting identity of a bill payment version under a rule version (§11.4).
func BillPaymentIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceBillPayment, id, version, rule)
}

// PostBillPayment turns one version of a payment of bills into its entry (bill_payment@1) and the settlements it
// records:
//   - Dr 2010 with each bill's amount settled, at that bill's stored rate (its carrying amount: ENG-14); and for the
//     part that settles what is left of a bill in another currency, a base-only line on 2010 with the residual, what
//     the carrying amount left differs from it by (at most MaxResidual: a cent of RND-FX a part);
//   - Dr 2010 with any unapplied amount, at the payment's rate: a credit with the supplier;
//   - Cr the money account with what left it: the total at the payment's rate, or, for bills in another currency paid
//     from a base-currency account, what the account paid in the base currency;
//   - and what makes the entry balance in the base currency to 7030: the realised FX (ENG-41: RND-FX of each amount at
//     the payment's rate against its stored rate, with no tolerance), the residuals, and any cent RND-FX leaves
//     between the lines.
//
// Every rule is checked first and every failure returned together (ENG-22).
func PostBillPayment(p BillPayment, s Settings) (Entry, []Settlement, error) {
	return postPayment(p, s, RoleAccountsPayable)
}

// postPayment is bill_payment@1's legs against the control account with role: accounts payable for a payment of bills
// (and, negated, a supplier refund), accounts receivable for a receipt from customers, negated (customer_receipt@1).
func postPayment(p BillPayment, s Settings, role string) (Entry, []Settlement, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if p.Date.IsZero() {
		add(CodeNoDate, "date", "the payment has no date", "Enter the date the money left the account")
	}
	if len(p.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(p.Memo), MaxMemo), "Shorten the reference")
	}
	acc, ok := s.Accounts[p.Account]
	switch {
	case !ok:
		add(CodeUnknownAccount, "money_account", "the account does not exist in this organisation", "Choose the bank, card or wallet account it was paid from")
	case acc.Group || !acc.Active || !acc.Money:
		add(CodeNotMoneyAccount, "money_account", fmt.Sprintf("account %s is not an active money account", acc.Code), "Choose the bank, card or wallet account it was paid from")
	}
	paidFrom := s.Base
	if acc.Currency != "" {
		if c, err := money.Lookup(acc.Currency); err == nil {
			paidFrom = c
		}
	}
	foreign := p.Currency != s.Base
	fromBase := ok && foreign && paidFrom == s.Base
	rate := money.Identity(s.Base)
	switch {
	case fromBase && p.Rate != nil:
		add(CodeWrongRate, "fx_rate", fmt.Sprintf("account %s is in %s: the payment settles at the rate what it paid implies", acc.Code, s.Base),
			"Leave the rate out, and enter what the account paid")
	case fromBase:
	case !foreign && p.Rate != nil:
		add(CodeRateOnBase, "fx_rate", fmt.Sprintf("the bills are in %s, the base currency, so the payment takes no rate", s.Base), "Leave the rate out")
	case foreign && p.Rate == nil:
		add(CodeMissingRate, "fx_rate", fmt.Sprintf("the bills are in %s, so the payment needs a rate into %s", p.Currency, s.Base),
			"Enter the rate, or add a rate for the payment's date in Accounting › Currencies")
	case foreign && (p.Rate.From != p.Currency || p.Rate.To != s.Base):
		add(CodeWrongRate, "fx_rate", fmt.Sprintf("the rate converts %s into %s, but the payment needs %s into %s", p.Rate.From, p.Rate.To, p.Currency, s.Base),
			"Use a rate from the bills' currency into the base currency")
	case foreign:
		rate = *p.Rate
	}
	if ok && paidFrom != p.Currency && paidFrom != s.Base {
		add(CodeLineCurrency, "money_account", fmt.Sprintf("account %s is in %s; the bills are in %s", acc.Code, paidFrom, p.Currency),
			"Pay from an account in the bills' currency, or in the base currency")
	}
	switch {
	case fromBase && p.Paid == nil:
		add(CodePaidAmount, "paid", fmt.Sprintf("account %s is in %s: enter what it paid in %s", acc.Code, s.Base, s.Base), "Enter the amount the account statement shows")
	case p.Paid != nil && !fromBase:
		add(CodePaidAmount, "paid", "only bills in another currency paid from a base-currency account name what the account paid", "Leave it out")
	case p.Paid != nil && (p.Paid.Currency() != s.Base || p.Paid.Sign() <= 0):
		add(CodePaidAmount, "paid", fmt.Sprintf("what the account paid is %s %s", p.Paid, p.Paid.Currency()), fmt.Sprintf("Enter a positive amount in %s", s.Base))
	}
	ap, hasAP := s.byRole(role)
	switch {
	case !hasAP && role == RoleAccountsReceivable:
		add(CodeSystemRole, "bills", "the chart has no accounts receivable account", "Load the standard chart's 1100 Accounts receivable, or give an asset account the role")
	case !hasAP:
		add(CodeSystemRole, "bills", "the chart has no accounts payable account", "Load the standard chart's 2010 Accounts payable, or give a liability account the role")
	}
	total := money.Zero(p.Currency)
	if p.Unapplied.Currency() != p.Currency || p.Unapplied.Sign() < 0 {
		add(CodePaymentAmount, "unapplied", fmt.Sprintf("the unapplied amount is %s %s", p.Unapplied, p.Unapplied.Currency()),
			fmt.Sprintf("It is in %s and not below zero", p.Currency))
	} else {
		total, _ = total.Add(p.Unapplied)
	}
	for i, b := range p.Bills {
		f := func(name string) string { return fmt.Sprintf("bills[%d].%s", i+1, name) }
		if b.Amount.Currency() != p.Currency || b.Amount.Sign() <= 0 {
			add(CodePaymentAmount, f("amount"), fmt.Sprintf("the amount settled is %s %s", b.Amount, b.Amount.Currency()), fmt.Sprintf("Enter a positive amount in %s", p.Currency))
			continue
		}
		if b.StoredRate.From != p.Currency || b.StoredRate.To != s.Base {
			add(CodeWrongRate, f("bill"), "the bill's stored rate is not of the payment's currency", "Pay bills of one currency together")
			continue
		}
		settles := b.Amount
		if b.Short != nil {
			switch {
			case b.Short.Currency() != p.Currency || b.Short.Sign() <= 0:
				add(CodePaymentAmount, f("amount"), fmt.Sprintf("the settlement difference is %s %s", b.Short, b.Short.Currency()), fmt.Sprintf("It is a positive amount in %s", p.Currency))
				continue
			case b.Clears == nil:
				add(CodePaymentAmount, f("amount"), "a settlement difference clears only what is left of a bill paid in full", "Settle the bill in full, or leave the difference open")
				continue
			}
			settles, _ = settles.Add(*b.Short)
			if _, ok := s.byRole(RoleFXRounding); !ok {
				add(CodeSystemRole, f("amount"), "the chart has no FX and rounding account for the settlement difference",
					"Load the standard chart's 7030 FX & rounding differences, or give an account the role")
				continue
			}
		}
		if b.Clears != nil {
			if b.Clears.Currency() != s.Base || b.Clears.Sign() <= 0 {
				add(CodePaymentAmount, f("bill"), fmt.Sprintf("the carrying amount left is %s %s", b.Clears, b.Clears.Currency()), fmt.Sprintf("It is a positive amount in %s", s.Base))
				continue
			}
			base, err := rounding.FX(settles, b.StoredRate)
			if err != nil {
				add(CodeBaseRange, f("amount"), err.Error(), "Check the amount")
				continue
			}
			residual, _ := b.Clears.Sub(base)
			if r := residual.Decimal(); r.Abs(r).Cmp(apd.New(MaxResidual, 0)) > 0 || (!residual.IsZero() && p.Currency == s.Base) {
				add(CodePaymentAmount, f("amount"), fmt.Sprintf("the bill carries %s %s, but %s %s at its rate is %s", b.Clears, s.Base, settles, p.Currency, base),
					"Settle what is open of the bill; a larger difference is a write-off")
				continue
			}
		}
		total, _ = total.Add(b.Amount)
	}
	if total.IsZero() && len(probs) == 0 {
		add(CodePaymentAmount, "bills", "the payment settles nothing and leaves nothing unapplied", "Enter what it paid of each bill, or the amount paid")
	}
	if len(probs) > 0 {
		return Entry{}, nil, probs
	}
	var lines []Line
	var settled []Settlement
	baseTotal := money.Zero(s.Base)
	// The payment's own rate for a foreign bill paid from a base-currency account is what it paid for the total; the
	// realised FX of each bill is at it, and the cent left by RND-FX goes to 7030 with the rest.
	settleRate := rate
	if p.Paid != nil {
		r, err := rounding.Implied(*p.Paid, total, "paid "+p.Paid.String()+" "+s.Base.Code+" for "+total.String()+" "+p.Currency.Code)
		if err != nil {
			return Entry{}, nil, Problems{{CodeBaseRange, "paid", err.Error(), "Check what the account paid"}}
		}
		settleRate = r
	}
	for _, b := range p.Bills {
		paidBase, err := rounding.FX(b.Amount, b.StoredRate)
		if err != nil {
			return Entry{}, nil, Problems{{CodeBaseRange, "bills", err.Error(), "Check the amounts"}}
		}
		// What the part cost at the payment's rate, against what it carried: a gain when it cost less (ENG-41).
		cost, err := rounding.FX(b.Amount, settleRate)
		if err != nil {
			return Entry{}, nil, Problems{{CodeBaseRange, "bills", err.Error(), "Check the amounts and the rate"}}
		}
		gain, err := paidBase.Sub(cost)
		if err != nil {
			return Entry{}, nil, err
		}
		// A settlement difference within tolerance settles the rest of the bill on the same line, and its carrying amount
		// goes to 7030 on a line of its own: a gain when the company pays less than it owed.
		settles, base, tolerance := b.Amount, paidBase, money.Zero(s.Base)
		if b.Short != nil {
			settles, _ = settles.Add(*b.Short)
			if base, err = rounding.FX(settles, b.StoredRate); err != nil {
				return Entry{}, nil, Problems{{CodeBaseRange, "bills", err.Error(), "Check the amounts"}}
			}
			tolerance, _ = base.Sub(paidBase)
		}
		lines = append(lines, Line{AccountID: ap.ID, Amount: settles, Rate: b.StoredRate, Base: base, Description: p.Memo})
		baseTotal, _ = baseTotal.Add(base)
		if b.Short != nil {
			fxAcc, _ := s.byRole(RoleFXRounding)
			lines = append(lines, Line{AccountID: fxAcc.ID, Amount: tolerance.Neg(), Rate: money.Identity(s.Base), Base: tolerance.Neg(),
				Description: "Settlement difference within tolerance"})
			baseTotal, _ = baseTotal.Add(tolerance.Neg())
		}
		cleared := base
		if b.Clears != nil && !b.Clears.Equal(base) {
			residual, _ := b.Clears.Sub(base)
			lines = append(lines, Line{AccountID: ap.ID, Amount: money.Zero(p.Currency), Rate: b.StoredRate, Base: residual,
				Description: "Settlement residual: RND-FX of the parts"})
			baseTotal, _ = baseTotal.Add(residual)
			cleared = *b.Clears
		}
		settled = append(settled, Settlement{DocumentID: b.BillID, Amount: settles, Base: cleared, RealisedFX: gain, Tolerance: tolerance})
	}
	if !p.Unapplied.IsZero() {
		base, err := rounding.FX(p.Unapplied, settleRate)
		if err != nil {
			return Entry{}, nil, Problems{{CodeBaseRange, "unapplied", err.Error(), "Check the amount"}}
		}
		lines = append(lines, Line{AccountID: ap.ID, Amount: p.Unapplied, Rate: settleRate, Base: base, Description: "Unapplied: a credit with the supplier"})
		baseTotal, _ = baseTotal.Add(base)
	}
	if p.Paid != nil {
		lines = append(lines, Line{AccountID: p.Account, Amount: p.Paid.Neg(), Rate: money.Identity(s.Base), Base: p.Paid.Neg(), Description: p.Memo})
		baseTotal, _ = baseTotal.Add(p.Paid.Neg())
	} else {
		base, err := rounding.FX(total.Neg(), rate)
		if err != nil {
			return Entry{}, nil, Problems{{CodeBaseRange, "bills", err.Error(), "Check the amounts and the rate"}}
		}
		lines = append(lines, Line{AccountID: p.Account, Amount: total.Neg(), Rate: rate, Base: base, Description: p.Memo})
		baseTotal, _ = baseTotal.Add(base)
	}
	if !baseTotal.IsZero() {
		fxAcc, ok := s.byRole(RoleFXRounding)
		if !ok {
			return Entry{}, nil, Problems{{CodeSystemRole, "bills", "the chart has no FX and rounding account for the realised FX",
				"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
		}
		diff := baseTotal.Neg()
		lines = append(lines, Line{AccountID: fxAcc.ID, Amount: diff, Rate: money.Identity(s.Base), Base: diff, Description: "Realised FX and rounding on settlement"})
	}
	y, m, d := p.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceBillPayment, ID: p.ID.String(), Version: p.Version},
		RuleVersion:     RuleBillPayment,
		PostingIdentity: BillPaymentIdentity(p.ID, p.Version, RuleBillPayment),
		Memo:            p.Memo,
		Lines:           lines,
	}, settled, nil
}

// BillPaymentRule turns one version of a payment of bills into its entry and settlements under one rule version.
type BillPaymentRule func(p BillPayment, s Settings) (Entry, []Settlement, error)

// BillPaymentRules are every bill payment rule version the engine has posted with, by name: like Rules, a version is
// never removed or changed while an entry names it.
var BillPaymentRules = map[string]BillPaymentRule{
	RuleBillPayment: PostBillPayment,
}

// ReplayBillPayment re-runs the rule version that posted a bill payment's entry over the same version (ENG-06).
func ReplayBillPayment(rule string, p BillPayment, s Settings) (Entry, []Settlement, error) {
	f, ok := BillPaymentRules[rule]
	if !ok {
		return Entry{}, nil, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(p, s)
}
