package ledger

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	openingClearing = Account{ID: uuid.MustParse("0192f1a0-0000-7000-8000-000000003900"), Code: "3900", Active: true, Type: "equity", Role: RoleOpeningClearing}
	booksStart      = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
)

func openingInvoice(i Invoice) Invoice {
	i.Opening = &booksStart
	return i
}

func openingBill(b Bill) Bill {
	b.Opening = &booksStart
	return b
}

func codesOf(err error) string {
	var ps Problems
	if !errors.As(err, &ps) {
		return ""
	}
	var out []string
	for _, p := range ps {
		out = append(out, p.Code+" "+p.Field)
	}
	return strings.Join(out, "; ")
}

// Worked by hand (FR-GL-06, Appendix B.2 "Opening open items"; rev 0.257) [accountant to confirm]:
//   - a USD invoice of 1,000.00 at 9% issued 14 Oct 2026, unpaid when the books start on 1 Nov 2026, at 1.35: its total
//     is USD 1,090.00, S$1,471.50 at 1.35. It posts on 1 Nov: Dr 1100 USD 1,090.00 (S$1,471.50), Cr 3900 S$1,471.50; no
//     revenue and no GST;
//   - an SGD bill of 500.00 with GST 45.00 stated: Dr 3900 S$545.00, Cr 2010 S$545.00, on 1 Nov; no cost and no GST.
//
// req: FR-GL-06
func TestAnOpeningItemPostsItsTotalAgainstTheClearingAccount(t *testing.T) {
	inv := openingInvoice(Invoice{ID: invoiceID, Version: 1, Date: day, Memo: "Globex INV-0099", Currency: usd, Rate: usdRate(t, "1.35"),
		Lines: []InvoiceLine{invoiceLine(t, consulting, "1000.00", usd, "SR", "0.09")}})
	s := selling()
	e, err := PostOpeningInvoice(inv, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Lines) != 2 || e.Lines[0].AccountID != receivables.ID || e.Lines[0].Amount.String() != "1090.00" || e.Lines[0].Base.String() != "1471.50" ||
		e.Lines[1].AccountID != openingClearing.ID || e.Lines[1].Base.String() != "-1471.50" || e.Lines[1].Amount.Currency() != sgd {
		t.Errorf("the opening invoice's legs: %s", legs(s, e))
	}
	if !e.PostingDate.Equal(booksStart) || e.RuleVersion != RuleOpeningInvoice || e.PostingIdentity != InvoiceIdentity(invoiceID, 1, RuleOpeningInvoice) {
		t.Errorf("posted %s under %s as %s", e.PostingDate, e.RuleVersion, e.PostingIdentity)
	}

	b := openingBill(sgdBill(billLine(t, software, "500.00", "45.00", sgd, true)))
	p := purchasing()
	e, err = PostOpeningBill(b, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Lines) != 2 || e.Lines[0].AccountID != payables.ID || e.Lines[0].Base.String() != "-545.00" || e.Lines[1].AccountID != openingClearing.ID ||
		e.Lines[1].Base.String() != "545.00" || !e.PostingDate.Equal(booksStart) || e.RuleVersion != RuleOpeningBill {
		t.Errorf("the opening bill: %s", legs(p, e))
	}
}

// An opening item is dated before the books start, spreads nothing, deducts no deposit, withholds no tax and needs the
// clearing account; every rule of its kind holds too, all said at once (ENG-22).
// req: FR-GL-06
func TestAnOpeningItemKeepsItsRules(t *testing.T) {
	late := openingInvoice(sgdInvoice(invoiceLine(t, consulting, "100.00", sgd, "SR", "0.09")))
	late.Date = booksStart
	spread := openingInvoice(sgdInvoice(spreadInvoiceLine(invoiceLine(t, subscribe, "1200.00", sgd, "SR", "0.09"), october, 12)))
	none := sgdInvoice(invoiceLine(t, consulting, "100.00", sgd, "SR", "0.09"))
	empty := openingInvoice(Invoice{ID: invoiceID, Version: 1, Date: day, Currency: sgd})
	for name, c := range map[string]struct {
		i    Invoice
		s    Settings
		want string
	}{
		"dated at the books start": {late, selling(), "LEDGER-50 date"},
		"spread":                   {spread, selling(), "LEDGER-50 lines"},
		"deducting a deposit":      {withDeposits(openingInvoice(sgdInvoice(invoiceLine(t, consulting, "2000.00", sgd, "SR", "0.09"))), deducting(t, "1090.00", "1090.00", "90.00", sgd)), depositing(), "LEDGER-50 deposits"},
		"without the books start":  {none, selling(), "LEDGER-50 opening_item"},
		"without lines":            {empty, selling(), "lines"},
	} {
		_, err := PostOpeningInvoice(c.i, c.s)
		if got := codesOf(err); !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %s", name, got, c.want)
		}
	}
	noClearing := selling()
	delete(noClearing.Accounts, openingClearing.ID)
	if _, err := PostOpeningInvoice(openingInvoice(sgdInvoice(invoiceLine(t, consulting, "100.00", sgd, "SR", "0.09"))), noClearing); !strings.Contains(codesOf(err), CodeSystemRole) {
		t.Errorf("without the clearing account: %v", err)
	}
	withheld := openingBill(Bill{ID: billID, Version: 1, Date: day, Memo: "Consultant", Currency: sgd, Withholding: &Withholding{RatePercent: "15"},
		Lines: []BillLine{billLine(t, software, "5000.00", "0.00", sgd, false)}})
	if _, err := PostOpeningBill(withheld, purchasing()); !strings.Contains(codesOf(err), "LEDGER-50 withholding") {
		t.Errorf("a withholding opening bill: %v", err)
	}
}

// A credit open at the books start is an opening item too (FR-GL-06; rev 0.261): a credit note to a customer of S$1,090.00
// (1,000.00 at 9%) credits 1100 with its total and debits 3900 with it, at the books start, posting no revenue or GST;
// a supplier's credit of S$218.00 debits 2010 and credits 3900. Each names its own rule and identity, and replays.
// req: FR-GL-06
// req: ledger/posting-rules Credit documents and opening items
func TestACreditOpenAtTheBooksStartPostsAgainstTheClearingAccount(t *testing.T) {
	cn := openingInvoice(Invoice{ID: creditNoteID, Version: 1, Date: day, Memo: "Acme Analytics CN-0009", Currency: sgd,
		Lines: []InvoiceLine{invoiceLine(t, consulting, "1000.00", sgd, "SR", "0.09")}})
	e, err := PostOpeningCreditNote(cn, selling())
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Lines) != 2 || e.Lines[0].AccountID != receivables.ID || e.Lines[0].Base.String() != "-1090.00" || e.Lines[1].AccountID != openingClearing.ID ||
		e.Lines[1].Base.String() != "1090.00" || !e.PostingDate.Equal(booksStart) || e.RuleVersion != RuleOpeningCreditNote ||
		e.PostingIdentity != CreditNoteIdentity(creditNoteID, 1, RuleOpeningCreditNote) || e.Source.Type != SourceCreditNote {
		t.Errorf("the opening credit note: %+v", e)
	}
	vc := openingBill(Bill{ID: creditID, Version: 1, Date: day, Memo: "Atlassian CN-20", Currency: sgd, Lines: []BillLine{billLine(t, software, "200.00", "18.00", sgd, true)}})
	e, err = PostOpeningVendorCredit(vc, purchasing())
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Lines) != 2 || e.Lines[0].AccountID != payables.ID || e.Lines[0].Base.String() != "218.00" || e.Lines[1].AccountID != openingClearing.ID ||
		e.Lines[1].Base.String() != "-218.00" || e.RuleVersion != RuleOpeningVendorCredit || e.Source.Type != SourceVendorCredit {
		t.Errorf("the opening vendor credit: %+v", e)
	}
	// One dated on the books start is refused as an opening invoice's is, in a credit note's words.
	late := cn
	late.Date = booksStart
	if _, err := PostOpeningCreditNote(late, selling()); err == nil || !strings.Contains(err.Error(), "credit note") {
		t.Errorf("an opening credit note on the books start: %v", err)
	}
}
