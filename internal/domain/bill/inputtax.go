package bill

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// The refusals of a bill's input-tax facts (ENG-23; rev 0.89).
const (
	CodeInputTax      = "BILL-14" // an input-tax fact that is not one Fyniq reads
	CodeBusinessShare = "BILL-15" // a business share that is not a percentage from 0 to 100
)

// The warnings of a bill's input tax (⚠, ENG-21; rev 0.89).
const (
	WarnTaxAgent       = "BILL-TAX-AGENT"       // a line's input tax waits for the tax agent: it is not claimed until decided
	WarnRepaidEvidence = "BILL-REPAID-EVIDENCE" // an agent's invoice whose input tax is claimed, with no file attached
)

// The documents a bill's input tax can rest on (FR-AP-20; rev 0.89).
const (
	DocTaxInvoice   = "tax_invoice"   // a tax invoice addressed to the company ("Company c/o another" counts)
	DocSimplified   = "simplified"    // a simplified tax invoice: no customer named, a total including GST within the limit
	DocImportPermit = "import_permit" // an import permit naming the company
	DocNone         = "none"          // no valid tax invoice, such as a receipt without the supplier's GST number
)

// Documents are the documents a bill's input tax can rest on.
var Documents = []string{DocTaxInvoice, DocSimplified, DocImportPermit, DocNone}

// Whom a bill's document is addressed to.
const (
	ToCompany = "company"
	ToAgent   = "agent" // an employee or a director who bought it as the company's agent: the bill is what they were repaid
)

// AddressedTo are whom a bill's document can be addressed to.
var AddressedTo = []string{ToCompany, ToAgent}

// How the company repaid an agent.
const (
	RepaidPaid         = "paid"          // the company paid the person back
	RepaidDirectorLoan = "director_loan" // credited to the director's loan account (FR-EQ-02): the tax agent decides
)

// Repaid are the ways an agent can be repaid.
var Repaid = []string{RepaidPaid, RepaidDirectorLoan}

// The tax agent's decision on a line the rules leave to them.
const (
	AgentClaimable    = "claimable"
	AgentNotClaimable = "not_claimable"
)

// AgentDecisions are the decisions the tax agent can give.
var AgentDecisions = []string{AgentClaimable, AgentNotClaimable}

// InputTax are the facts a bill's input tax is decided from (FR-AP-20; rev 0.89), never from a GST amount alone. The
// zero value is the usual bill: a tax invoice from a GST-registered supplier, addressed to the company. An invoice in an
// agent's name is billed at what the company repaid them, its GST the GST in that amount (IRAS's tax fraction of the
// amount reimbursed; the owner's decision of 23 Oct 2026). Each line adds its business share.
type InputTax struct {
	SupplierNotRegistered bool   // the supplier is not GST-registered, so it charged no GST
	Document              string // one of Documents; empty is a tax invoice
	AddressedTo           string // one of AddressedTo; empty is the company
	Repaid                string // for an agent's invoice, one of Repaid; empty is paid
	TaxAgent              string // the tax agent's decision on lines the rules leave to them, one of AgentDecisions; empty while none
}

func (f InputTax) document() string    { return or(f.Document, DocTaxInvoice) }
func (f InputTax) addressedTo() string { return or(f.AddressedTo, ToCompany) }
func (f InputTax) repaid() string      { return or(f.Repaid, RepaidPaid) }

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// The conditions a line's input tax is decided to (FR-AP-20): the line records the one that decided it.
const (
	Claimable    = "claimable"
	NotClaimable = "not_claimable"
	ForTaxAgent  = "tax_agent" // ⚠: not claimed until the tax agent decides
)

// The reasons a line's input tax was decided as it was, as stable codes.
const (
	ReasonTaxInvoice     = "tax_invoice"    // a tax invoice addressed to the company
	ReasonSimplified     = "simplified"     // a simplified tax invoice within the limit
	ReasonImportPermit   = "import_permit"  // an import permit naming the company
	ReasonAgent          = "agent"          // an invoice in an agent's name, and the company repaid them
	ReasonBusinessShare  = "business_share" // claimed for its business share only
	ReasonNoGST          = "no_gst"         // the line has no GST to claim
	ReasonNotRegistered  = "organisation_not_registered"
	ReasonCode           = "gst_code" // the GST code's input tax is not claimed (BL, NR, EP, OP, ZP)
	ReasonSupplier       = "supplier_not_registered"
	ReasonNoInvoice      = "no_tax_invoice"
	ReasonOverLimit      = "simplified_over_limit"
	ReasonNoLimit        = "simplified_no_limit" // ⚠: no limit in force on the bill's date
	ReasonDirectorLoan   = "director_loan"       // ⚠: an agent's invoice credited to a director's loan
	ReasonPrivate        = "private"             // a business share of 0
	ReasonTaxAgentDecide = "tax_agent_decision"  // the tax agent decided a line the rules left to them
)

// Decision is how a line's input tax was decided: its condition, why, in plain words, and the business share claimed
// when it is claimed in part (empty when claimed whole or not at all).
type Decision struct {
	Condition string
	Reason    string
	Text      string
	Share     string
}

// facts is what deciding a line needs beyond its own facts: the organisation's registration on the bill's date, the
// sg-gst version in force then, the bill's total including GST in the base currency (nil when it cannot be converted),
// and the base currency.
type facts struct {
	registered bool
	version    pack.Version
	date       time.Time
	total      *money.Money
	base       money.Currency
}

// decide decides a line's input tax from the bill's facts and its own (FR-AP-20; rev 0.89), the first rule that
// applies deciding it:
//  1. an organisation not GST-registered on the bill's date, or a GST code with no input-tax box, claims nothing;
//  2. nor does a supplier that is not GST-registered, or a bill with no valid tax invoice;
//  3. a simplified tax invoice claims nothing when the bill's total including GST is above the pack's limit, and goes
//     to the tax agent when no limit is in force on its date;
//  4. an agent's invoice credited to a director's loan goes to the tax agent;
//  5. the tax agent's recorded decision decides a line the rules left to them;
//  6. otherwise the line claims its business share of its GST: none at 0, the whole at 100.
func decide(f InputTax, l Line, code pack.GSTCode, known bool, x facts) Decision {
	no := func(reason, text string) Decision {
		return Decision{Condition: NotClaimable, Reason: reason, Text: text}
	}
	share := or(l.BusinessShare, "100")
	switch {
	case l.GST.IsZero():
		return Decision{Condition: Claimable, Reason: ReasonNoGST, Text: "the line has no GST to claim"}
	case !x.registered:
		return no(ReasonNotRegistered, "the organisation is not GST-registered on the bill's date: the GST stays in the cost")
	case !known || code.Direction != pack.DirPurchase || code.GSTBox == 0:
		what := "its GST code is not one for purchases"
		if known {
			what = fmt.Sprintf("its GST code is %s: %s", code.Code, code.Description)
		}
		return no(ReasonCode, what)
	case f.SupplierNotRegistered:
		return no(ReasonSupplier, "the supplier is not GST-registered, so no GST was charged")
	case f.document() == DocNone:
		return no(ReasonNoInvoice, "there is no valid tax invoice, such as for a receipt without the supplier's GST number")
	}
	var leftToAgent *Decision
	if f.document() == DocSimplified {
		amount, cur, ok := pack.Limit(x.version, pack.LimitSimplifiedInvoice, x.date)
		switch {
		case !ok || x.total == nil || cur != x.base.Code:
			leftToAgent = &Decision{Condition: ForTaxAgent, Reason: ReasonNoLimit,
				Text: fmt.Sprintf("%s holds no simplified-invoice limit on the bill's date: the tax agent decides", x.version.ID)}
		case over(*x.total, amount):
			return no(ReasonOverLimit, fmt.Sprintf("a simplified tax invoice covers a total of at most %s %s including GST; this bill's is %s %s: ask the supplier for a tax invoice",
				amount, cur, x.total, x.base.Code))
		}
	}
	if leftToAgent == nil && f.addressedTo() == ToAgent && f.repaid() == RepaidDirectorLoan {
		leftToAgent = &Decision{Condition: ForTaxAgent, Reason: ReasonDirectorLoan,
			Text: "the invoice is in an agent's name and the company credited their director's loan instead of paying them back: the tax agent decides"}
	}
	if leftToAgent != nil {
		switch f.TaxAgent {
		case AgentNotClaimable:
			return no(ReasonTaxAgentDecide, "the tax agent decided the input tax is not claimable")
		case AgentClaimable:
		default:
			return *leftToAgent
		}
	}
	d := Decision{Condition: Claimable, Reason: ReasonTaxInvoice, Text: "a tax invoice addressed to the company"}
	switch {
	case leftToAgent != nil:
		d.Reason, d.Text = ReasonTaxAgentDecide, "the tax agent decided the input tax is claimable"
	case f.addressedTo() == ToAgent:
		d.Reason, d.Text = ReasonAgent, "an invoice in an agent's name, for what the company paid them back"
	case f.document() == DocSimplified:
		d.Reason, d.Text = ReasonSimplified, "a simplified tax invoice within the limit"
	case f.document() == DocImportPermit:
		d.Reason, d.Text = ReasonImportPermit, "an import permit naming the company"
	}
	switch {
	case isZero(share):
		return no(ReasonPrivate, "the purchase is private: its business share is 0%")
	case !isWhole(share):
		d.Reason, d.Share = ReasonBusinessShare, share
		d.Text = fmt.Sprintf("%s, claimed for its business share of %s%% (%s); the rest stays in the cost", d.Text, share, rounding.GSTShareName)
	}
	return d
}

// over reports whether total is above the limit amount, an exact decimal from the pack.
func over(total money.Money, amount string) bool {
	limit, _, err := apd.NewFromString(amount)
	return err != nil || total.Decimal().Cmp(limit) > 0
}

func isZero(share string) bool {
	d, _, err := apd.NewFromString(share)
	return err == nil && d.IsZero()
}

func isWhole(share string) bool {
	d, _, err := apd.NewFromString(share)
	return err == nil && d.Cmp(apd.New(100, 0)) == 0
}

// usual is d with its input-tax facts that say the usual thing left empty, so the payload keeps only what differs: a
// tax invoice, addressed to the company, an agent paid back, a business share of 100.
func (d Draft) usual() Draft {
	f := &d.InputTax
	for _, x := range []struct {
		field *string
		def   string
	}{{&f.Document, DocTaxInvoice}, {&f.AddressedTo, ToCompany}, {&f.Repaid, RepaidPaid}} {
		if *x.field == x.def {
			*x.field = ""
		}
	}
	d.Lines = slices.Clone(d.Lines)
	for i := range d.Lines {
		if rounding.IsShare(d.Lines[i].BusinessShare) && isWhole(d.Lines[i].BusinessShare) {
			d.Lines[i].BusinessShare = ""
		}
	}
	return d
}

// checkFacts are a bill's input-tax facts that Fyniq cannot read, as problems with their fields.
func checkFacts(d Draft) ledger.Problems {
	var out ledger.Problems
	add := func(code, field, msg, fix string) {
		out = append(out, ledger.Problem{Code: code, Field: field, Message: msg, Fix: fix})
	}
	f := d.InputTax
	for _, x := range []struct {
		field, value string
		known        []string
	}{
		{"input_tax.document", f.Document, Documents},
		{"input_tax.addressed_to", f.AddressedTo, AddressedTo},
		{"input_tax.repaid", f.Repaid, Repaid},
		{"input_tax.tax_agent", f.TaxAgent, AgentDecisions},
	} {
		if x.value != "" && !slices.Contains(x.known, x.value) {
			add(CodeInputTax, x.field, fmt.Sprintf("%q is not one of %s", x.value, strings.Join(x.known, ", ")), "Choose one of them")
		}
	}
	if f.Repaid != "" && f.addressedTo() != ToAgent {
		add(CodeInputTax, "input_tax.repaid", "it says how an agent was paid back, but the invoice is addressed to the company",
			"Leave it out, or say the invoice is in an agent's name")
	}
	if f.document() == DocImportPermit && f.addressedTo() == ToAgent {
		add(CodeInputTax, "input_tax.addressed_to", "an import permit names the company, so it is not in an agent's name", "Say the document is addressed to the company")
	}
	for i, l := range d.Lines {
		if l.BusinessShare != "" && !rounding.IsShare(l.BusinessShare) {
			add(CodeBusinessShare, fmt.Sprintf("lines[%d].business_share", i+1), fmt.Sprintf("%q is not a percentage from 0 to 100 with at most two decimals", l.BusinessShare),
				"Enter the share of the purchase used for the business, such as 60 or 33.33")
		}
	}
	return out
}
