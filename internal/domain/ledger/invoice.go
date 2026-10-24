package ledger

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The invoice's source type and rule version (FR-AR-03; posting rules "Posting an invoice" and "Amounts of a posted
// invoice", PRD-001 §10.2 and Appendix B.2). A change to the rule is a new version; an old version keeps producing what
// it produced (ENG-06).
const (
	SourceInvoice = "invoice"
	RuleInvoice   = "invoice@1"
)

// The system roles of the accounts the sales rules post to (FR-COA-03, B.5).
const (
	RoleAccountsReceivable = "accounts-receivable"
	RoleDeferredRevenue    = "deferred-revenue"
)

// The invoice rule's validation codes (ENG-22, ENG-23).
const (
	CodeInvoiceTotal = "LEDGER-31" // an invoice's total is not more than zero: a credit note records what the company owes back
	CodeGSTRate      = "LEDGER-32" // a line's GST rate that is not a fraction such as 0.09
	CodeDeposit      = "LEDGER-33" // a deposit an invoice deducts that is not in its currency, or more than its total
)

// InvoiceLine is one line of an invoice as the rules read it: the account it earns, its net in the invoice's currency
// (RND-LINE's, after its discount), its GST code, and the code's rate in force at the invoice's date as a fraction
// ("0.09"), empty when the line bears no GST (a zero-rated, exempt or out-of-scope code, or an organisation not
// GST-registered on the invoice's date: the domain decides). Spread spreads its net over months (FR-GL-03).
type InvoiceLine struct {
	AccountID   uuid.UUID
	Description string
	Net         money.Money
	GSTCode     string
	GSTRate     string
	Spread      *Spread // nil: the revenue goes to the line's account on the invoice's date
}

// Invoice is one version of a customer's invoice as the rules read it.
type Invoice struct {
	ID       uuid.UUID
	Version  int
	Date     time.Time // the invoice's date: its tax point (ENG-45)
	Memo     string    // the customer and the invoice's number
	Currency money.Currency
	Rate     *money.Rate // into the base currency; nil for an invoice in the base currency
	Lines    []InvoiceLine
	// GSTMethod is how RND-GST-EXCL rounds it: per GST code's subtotal, the default, or per line (the setting
	// gst.round_per_line, applied consistently).
	GSTMethod rounding.GSTMethod
	// Deposits are the customer's deposits the invoice deducts (FR-AR-09; rev 0.127): none for an invoice before then.
	Deposits []InvoiceDeposit
}

// InvoiceDeposit is a customer deposit an invoice deducts, as the rule reads it: the amount it takes in the invoice's
// currency, and the part of the deposit's base amount (Base) and of the output tax it bore (Tax) that amount takes
// under RND-DEPOSIT, in the base currency.
type InvoiceDeposit struct {
	DepositID uuid.UUID
	Number    string
	Amount    money.Money
	Base      money.Money
	Tax       money.Money
}

// InvoiceIdentity is the posting identity of an invoice version under a rule version (§11.4).
func InvoiceIdentity(id uuid.UUID, version int, rule string) string {
	return identity(SourceInvoice, id, version, rule)
}

// InvoiceTotals are what an invoice charges in its own currency: its net and GST by GST code under RND-GST-EXCL, and
// its total, as the customer's copy shows them.
type InvoiceTotals struct {
	Subtotals []rounding.GSTSubtotal
	Net       money.Money
	GST       money.Money
	Total     money.Money
}

// TotalsOf are an invoice's totals in its own currency: each line's net, and RND-GST-EXCL over the lines that bear GST.
func TotalsOf(i Invoice) (InvoiceTotals, error) {
	out := InvoiceTotals{Net: money.Zero(i.Currency), GST: money.Zero(i.Currency)}
	var nets []rounding.GSTNet
	for _, l := range i.Lines {
		var err error
		if out.Net, err = out.Net.Add(l.Net); err != nil {
			return out, err
		}
		if l.GSTRate != "" {
			nets = append(nets, rounding.GSTNet{Code: l.GSTCode, Rate: l.GSTRate, Net: l.Net})
		}
	}
	if len(nets) > 0 {
		var err error
		if out.Subtotals, err = rounding.GSTExcl(nets, i.method()); err != nil {
			return out, err
		}
		for _, s := range out.Subtotals {
			if out.GST, err = out.GST.Add(s.GST); err != nil {
				return out, err
			}
		}
	}
	var err error
	out.Total, err = out.Net.Add(out.GST)
	return out, err
}

// BaseTotalsOf are an invoice's totals in the base currency as invoice@1 posts them, for the base amounts a foreign-
// currency tax invoice shows (FR-AR-05: net, GST and gross in SGD): each line's net under RND-FX at the invoice's rate,
// RND-GST-EXCL over those base nets (ENG-42), which is the output tax posted before deposits, and their sum. An invoice
// in the base currency has its own totals.
func BaseTotalsOf(i Invoice, base money.Currency) (InvoiceTotals, error) {
	if i.Currency == base {
		return TotalsOf(i)
	}
	if i.Rate == nil || i.Rate.From != i.Currency || i.Rate.To != base {
		return InvoiceTotals{}, fmt.Errorf("ledger: an invoice in %s needs a rate into %s", i.Currency, base)
	}
	out := InvoiceTotals{Net: money.Zero(base), GST: money.Zero(base)}
	var nets []rounding.GSTNet
	for _, l := range i.Lines {
		b, err := rounding.FX(l.Net, *i.Rate)
		if err != nil {
			return out, err
		}
		if out.Net, err = out.Net.Add(b); err != nil {
			return out, err
		}
		if l.GSTRate != "" {
			nets = append(nets, rounding.GSTNet{Code: l.GSTCode, Rate: l.GSTRate, Net: b})
		}
	}
	if len(nets) > 0 {
		var err error
		if out.Subtotals, err = rounding.GSTExcl(nets, i.method()); err != nil {
			return out, err
		}
		for _, s := range out.Subtotals {
			if out.GST, err = out.GST.Add(s.GST); err != nil {
				return out, err
			}
		}
	}
	var err error
	out.Total, err = out.Net.Add(out.GST)
	return out, err
}

func (i Invoice) method() rounding.GSTMethod {
	if i.GSTMethod == "" {
		return rounding.GSTPerSubtotal
	}
	return i.GSTMethod
}

// PostInvoice turns one version of an invoice into its entry (invoice@1):
//   - Dr 1100 Accounts receivable with the total, net plus GST, at the invoice's rate under RND-FX;
//   - Cr each line's account with its net at the invoice's rate under RND-FX; but Cr 2400 Deferred revenue, in the base
//     currency, with the base amount of a line spread over months (FR-GL-03, Appendix B.2): deferred revenue is not
//     monetary, so it keeps its base amount, and each month's release moves it to the line's account. Only a revenue
//     line is spread;
//   - Cr 2200 GST control with the output GST in the base currency: RND-GST-EXCL over the lines' nets converted under
//     RND-FX (ENG-42: the net in SGD, then the GST on it), per GST code's subtotal or per line;
//   - and, for an invoice in another currency, what makes the entry balance in the base currency to 7030: the cents
//     RND-FX and RND-GST-EXCL leave between the total and its parts.
//
// An invoice that deducts customer deposits (FR-AR-09; rev 0.127, the owner's decision of 24 Oct 2026) debits 1100
// with its total less what they take, at its rate (no line when nothing is left), and 2410 with each deposit's base
// part less its tax part, in the base currency; its output tax line takes what the deposits bore off the GST it
// charges, so GST is accounted for once (IRAS: GST on the balance); and what the deposits' parts came to at the rates
// they came in, against what they take at the invoice's rate, goes to 7030 as realised FX (IFRIC 22 would put it in
// revenue) [verify]. An invoice without deposits posts as it always did.
//
// Every rule is checked first and every failure returned together (ENG-22): the invoice is rejected whole.
func PostInvoice(i Invoice, s Settings) (Entry, error) {
	var probs Problems
	add := func(code, field, msg, fix string) { probs = append(probs, Problem{code, field, msg, fix}) }
	if i.Date.IsZero() {
		add(CodeNoDate, "date", "the invoice has no date", "Enter the invoice's date: its tax point")
	}
	if len(i.Memo) > MaxMemo {
		add(CodeTooLong, "memo", fmt.Sprintf("the memo has %d characters; the most is %d", len(i.Memo), MaxMemo), "Shorten the memo")
	}
	if len(i.Lines) == 0 {
		add(CodeTooFewLines, "lines", "the invoice has no lines", "Add a line for what the invoice is for")
	}
	rate := money.Identity(s.Base)
	foreign := i.Currency != s.Base
	rated := true
	switch {
	case !foreign && i.Rate != nil:
		add(CodeRateOnBase, "fx_rate", fmt.Sprintf("the invoice is in the base currency %s, so it takes no exchange rate", s.Base),
			"Remove the rate: Fyniq records rate 1 for base-currency amounts")
	case foreign && i.Rate == nil:
		add(CodeMissingRate, "fx_rate", fmt.Sprintf("the invoice is in %s, so it needs a rate into %s", i.Currency, s.Base),
			"Enter the exchange rate, or add a rate for the invoice's date in Accounting › Currencies")
		rated = false
	case foreign && (i.Rate.From != i.Currency || i.Rate.To != s.Base):
		add(CodeWrongRate, "fx_rate", fmt.Sprintf("the rate converts %s into %s, but the invoice needs %s into %s", i.Rate.From, i.Rate.To, i.Currency, s.Base),
			"Use a rate from the invoice's currency into the base currency")
		rated = false
	case foreign:
		rate = *i.Rate
	}
	ar, hasAR := s.byRole(RoleAccountsReceivable)
	if !hasAR {
		add(CodeSystemRole, "lines", "the chart has no accounts receivable account", "Load the standard chart's 1100 Accounts receivable, or give an asset account the role")
	}
	gstControl, hasGST := s.byRole(RoleGSTControl)
	deferred, hasDeferred := s.byRole(RoleDeferredRevenue)
	spread, taxed := false, false

	var lines []Line
	var baseNets []rounding.GSTNet
	baseTotal := money.Zero(s.Base)
	for n, l := range i.Lines {
		f := func(name string) string { return fmt.Sprintf("lines[%d].%s", n+1, name) }
		acc, ok := s.Accounts[l.AccountID]
		switch {
		case !ok:
			add(CodeUnknownAccount, f("account"), "the account does not exist in this organisation", "Choose a revenue account from the chart")
		case acc.Group:
			add(CodeGroupAccount, f("account"), fmt.Sprintf("account %s is a group heading", acc.Code), "Choose an account under it")
		case !acc.Active:
			add(CodeInactiveAccount, f("account"), fmt.Sprintf("account %s is inactive", acc.Code), "Reactivate it or choose another account")
		case acc.ShareCapital:
			add(CodeShareCapital, f("account"), fmt.Sprintf("account %s is share capital, which changes only through the register of share issues", acc.Code),
				"Choose the account the invoice earns")
		case acc.Role == RoleAccountsReceivable || acc.Role == RoleGSTControl || acc.Role == RoleDeferredRevenue:
			add(CodeRuleAccount, f("account"), fmt.Sprintf("account %s is one the invoice posts its total, its GST or its deferred revenue to", acc.Code),
				"Choose the account the invoice earns: the rule posts to it by itself")
		}
		if len(l.Description) > MaxDescription {
			add(CodeTooLong, f("description"), fmt.Sprintf("the description has %d characters; the most is %d", len(l.Description), MaxDescription), "Shorten it")
		}
		if x := l.Spread; x != nil {
			spread = true
			switch {
			case x.Months < 1 || x.Months > MaxSpreadMonths:
				add(CodeSpread, f("spread_months"), fmt.Sprintf("the line is spread over %d months", x.Months), fmt.Sprintf("Spread it over 1 to %d months", MaxSpreadMonths))
			case x.From.IsZero():
				add(CodeSpread, f("spread_from"), "the spread has no first month", "Enter the month the service starts")
			case ok && acc.Type != "revenue":
				add(CodeSpread, f("spread_months"), fmt.Sprintf("account %s is not a revenue account, so its amount is not spread", acc.Code),
					"Spread only revenue earned over months, on a revenue account")
			}
		}
		if l.Net.Currency() != i.Currency {
			add(CodeLineCurrency, f("net"), fmt.Sprintf("the line is in %s; the invoice is in %s", l.Net.Currency(), i.Currency), "Enter every amount of the invoice in its currency")
			continue
		}
		if l.Net.IsZero() {
			add(CodeZeroLine, f("net"), "the line's amount is zero", "Enter its amount, or remove the line")
			continue
		}
		if l.Spread != nil && l.Net.Sign() < 0 {
			add(CodeSpread, f("spread_months"), "the line's amount is below zero, so there is nothing to spread", "Spread a line that earns, or remove its spread")
		}
		if !rated {
			continue
		}
		base, err := rounding.FX(l.Net, rate)
		if err != nil {
			add(CodeBaseRange, f("net"), fmt.Sprintf("at this rate the base amount does not fit: %v", err), "Check the amount and the rate")
			continue
		}
		if l.GSTRate != "" {
			taxed = true
			baseNets = append(baseNets, rounding.GSTNet{Code: l.GSTCode, Rate: l.GSTRate, Net: base})
		}
		baseTotal, _ = baseTotal.Add(base.Neg())
		if l.Spread != nil {
			lines = append(lines, Line{AccountID: deferred.ID, Amount: base.Neg(), Rate: money.Identity(s.Base), Base: base.Neg(), Description: l.Description})
			continue
		}
		lines = append(lines, Line{AccountID: l.AccountID, Amount: l.Net.Neg(), Rate: rate, Base: base.Neg(), Description: l.Description})
	}
	if spread && !hasDeferred {
		add(CodeSystemRole, "lines", "the chart has no deferred revenue account for the spread revenue", "Load the standard chart's 2400 Deferred revenue, or give a liability account the role")
	}
	if taxed && !hasGST {
		add(CodeSystemRole, "lines", "the chart has no GST control account for the output tax", "Load the standard chart's 2200 GST control, or give a liability account the role")
	}
	totals, err := TotalsOf(i)
	if err != nil {
		add(CodeGSTRate, "lines", err.Error(), "Choose each line's GST code again: its rate comes from the sg-gst pack")
	} else if len(i.Lines) > 0 && totals.Total.Sign() <= 0 {
		add(CodeInvoiceTotal, "lines", fmt.Sprintf("the invoice's total is %s %s", totals.Total, i.Currency),
			"An invoice's total is more than zero; record what the company owes a customer as a credit note")
	}
	deducted, depositTax, depositBase := money.Zero(i.Currency), money.Zero(s.Base), money.Zero(s.Base)
	deposits, hasDeposits := s.byRole(RoleCustomerDeposits)
	for n, d := range i.Deposits {
		f := func(name string) string { return fmt.Sprintf("deposits[%d].%s", n+1, name) }
		switch {
		case d.Amount.Currency() != i.Currency || d.Amount.Sign() <= 0:
			add(CodeDeposit, f("amount"), fmt.Sprintf("the deposit deducted is %s %s", d.Amount, d.Amount.Currency()), fmt.Sprintf("Enter a positive amount in %s", i.Currency))
			continue
		case d.Base.Currency() != s.Base || d.Tax.Currency() != s.Base || d.Base.Sign() < 0 || d.Tax.Sign() < 0:
			add(CodeDeposit, f("amount"), fmt.Sprintf("the deposit's parts are %s %s and %s %s of tax", d.Base, d.Base.Currency(), d.Tax, d.Tax.Currency()),
				fmt.Sprintf("Its parts are amounts in %s, not below zero", s.Base))
			continue
		}
		if c, _ := d.Tax.Cmp(d.Base); c > 0 {
			add(CodeDeposit, f("amount"), fmt.Sprintf("the deposit's tax part %s is more than its base part %s", d.Tax, d.Base), "Its tax part is a part of its base amount")
			continue
		}
		deducted, _ = deducted.Add(d.Amount)
		depositTax, _ = depositTax.Add(d.Tax)
		depositBase, _ = depositBase.Add(d.Base)
	}
	if len(i.Deposits) > 0 {
		if !hasDeposits {
			add(CodeSystemRole, "deposits", "the chart has no customer deposits account", "Load the standard chart's 2410 Customer deposits & retainers, or give a liability account the role")
		}
		if !depositTax.IsZero() && !hasGST {
			add(CodeSystemRole, "deposits", "the chart has no GST control account for the output tax the deposits bore", "Load the standard chart's 2200 GST control, or give a liability account the role")
		}
		if c, err := deducted.Cmp(totals.Total); err == nil && c > 0 {
			add(CodeDeposit, "deposits", fmt.Sprintf("the deposits deduct %s %s from a total of %s", deducted, i.Currency, totals.Total), "Deduct at most the invoice's total")
		}
	}
	var gstBase money.Money
	if len(probs) == 0 && len(baseNets) > 0 {
		subtotals, err := rounding.GSTExcl(baseNets, i.method())
		if err != nil {
			add(CodeGSTRate, "lines", err.Error(), "Choose each line's GST code again: its rate comes from the sg-gst pack")
		}
		gstBase = money.Zero(s.Base)
		for _, x := range subtotals {
			gstBase, _ = gstBase.Add(x.GST)
		}
	}
	if len(probs) > 0 {
		return Entry{}, probs
	}
	if !depositTax.IsZero() { // the deposits' output tax, accounted for when they came in, comes off the invoice's
		if gstBase.Currency() != s.Base {
			gstBase = money.Zero(s.Base)
		}
		gstBase, _ = gstBase.Sub(depositTax)
		if !gstBase.IsZero() {
			lines = append(lines, Line{AccountID: gstControl.ID, Amount: gstBase.Neg(), Rate: money.Identity(s.Base), Base: gstBase.Neg(),
				Description: "Output tax, less what the deposits bore"})
			baseTotal, _ = baseTotal.Add(gstBase.Neg())
		}
	} else if !gstBase.IsZero() {
		lines = append(lines, Line{AccountID: gstControl.ID, Amount: gstBase.Neg(), Rate: money.Identity(s.Base), Base: gstBase.Neg(), Description: "Output tax"})
		baseTotal, _ = baseTotal.Add(gstBase.Neg())
	}
	receivable, _ := totals.Total.Sub(deducted)
	arBase, err := rounding.FX(receivable, rate)
	if err != nil {
		return Entry{}, Problems{{CodeBaseRange, "lines", fmt.Sprintf("at this rate the total does not fit: %v", err), "Check the amounts and the rate"}}
	}
	var head []Line
	if !receivable.IsZero() {
		head = append(head, Line{AccountID: ar.ID, Amount: receivable, Rate: rate, Base: arBase, Description: i.Memo})
	}
	baseTotal, _ = baseTotal.Add(arBase)
	for _, d := range i.Deposits {
		held, _ := d.Base.Sub(d.Tax)
		if held.IsZero() {
			continue
		}
		head = append(head, Line{AccountID: deposits.ID, Amount: held, Rate: money.Identity(s.Base), Base: held, Description: "Deposit " + d.Number + " deducted"})
		baseTotal, _ = baseTotal.Add(held)
	}
	lines = append(head, lines...)
	if len(i.Deposits) > 0 { // realised FX on the deposits: their base parts against what they take at the invoice's rate
		fullBase, err := rounding.FX(totals.Total, rate)
		if err != nil {
			return Entry{}, Problems{{CodeBaseRange, "lines", fmt.Sprintf("at this rate the total does not fit: %v", err), "Check the amounts and the rate"}}
		}
		atRate, _ := fullBase.Sub(arBase)
		gain, _ := depositBase.Sub(atRate)
		if !gain.IsZero() {
			fxAcc, ok := s.byRole(RoleFXRounding)
			if !ok {
				return Entry{}, Problems{{CodeSystemRole, "deposits", "the chart has no FX and rounding account for the realised FX on the deposits",
					"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
			}
			lines = append(lines, Line{AccountID: fxAcc.ID, Amount: gain.Neg(), Rate: money.Identity(s.Base), Base: gain.Neg(), Description: "Realised FX on the deposits"})
			baseTotal, _ = baseTotal.Add(gain.Neg())
		}
	}
	if !baseTotal.IsZero() { // only for an invoice in another currency: an SGD invoice's lines are their own base amounts
		fxAcc, ok := s.byRole(RoleFXRounding)
		if !ok {
			return Entry{}, Problems{{CodeSystemRole, "lines", "the chart has no FX and rounding account for the cents of the conversion",
				"Load the standard chart's 7030 FX & rounding differences, or give an account the role"}}
		}
		diff := baseTotal.Neg()
		lines = append(lines, Line{AccountID: fxAcc.ID, Amount: diff, Rate: money.Identity(s.Base), Base: diff, Description: "Rounding of the conversion"})
	}
	y, m, d := i.Date.Date()
	return Entry{
		PostingDate:     time.Date(y, m, d, 0, 0, 0, 0, time.UTC),
		Source:          Source{Type: SourceInvoice, ID: i.ID.String(), Version: i.Version},
		RuleVersion:     RuleInvoice,
		PostingIdentity: InvoiceIdentity(i.ID, i.Version, RuleInvoice),
		Memo:            i.Memo,
		Lines:           lines,
	}, nil
}

// InvoiceRule turns one version of an invoice into its entry under one rule version.
type InvoiceRule func(i Invoice, s Settings) (Entry, error)

// InvoiceRules are every invoice rule version the engine has posted with, by name: like Rules, a version is never
// removed or changed while an entry names it.
var InvoiceRules = map[string]InvoiceRule{
	RuleInvoice: PostInvoice,
}

// ReplayInvoice re-runs the rule version that posted an invoice's entry over the same version (ENG-06).
func ReplayInvoice(rule string, i Invoice, s Settings) (Entry, error) {
	f, ok := InvoiceRules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return f(i, s)
}
