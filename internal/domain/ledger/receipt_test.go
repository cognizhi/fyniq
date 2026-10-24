package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var (
	receiptID    = uuid.MustParse("0192f1a0-0000-7000-8000-0000000ac110")
	otherInvoice = uuid.MustParse("0192f1a0-0000-7000-8000-0000000a1111")
)

// receiving is the chart a receipt posts against: selling's, with an SGD current account and a USD wallet.
func receiving() Settings {
	s := selling()
	s.Accounts[current.ID], s.Accounts[wallet.ID] = current, wallet
	return s
}

func received(t testing.TB, id uuid.UUID, amount string, cur money.Currency, stored *money.Rate) ReceivedInvoice {
	r := money.Identity(sgd)
	if stored != nil {
		r = *stored
	}
	return ReceivedInvoice{InvoiceID: id, Amount: amt(t, amount, cur), StoredRate: r}
}

func sgdReceipt(into Account, unapplied string, invoices ...ReceivedInvoice) CustomerReceipt {
	u, _ := money.Parse(unapplied, sgd)
	return CustomerReceipt{ID: receiptID, Version: 1, Date: day, Memo: "Acme Analytics, FAST 7731", Account: into.ID, Currency: sgd, Invoices: invoices, Unapplied: u}
}

func usdReceipt(t testing.TB, into Account, rate string, invoices ...ReceivedInvoice) CustomerReceipt {
	return CustomerReceipt{ID: receiptID, Version: 1, Date: day, Memo: "Globex, Wise 5512", Account: into.ID, Currency: usd, Rate: usdRate(t, rate),
		Invoices: invoices, Unapplied: money.Zero(usd)}
}

// Worked by hand: an SGD invoice of 1,090.00 received in full into the current account posts Dr 1011 1,090.00, Cr 1100
// 1,090.00, with no realised FX. Two invoices and S$25.00 more than they owe (a credit with the customer) post one 1100
// line an invoice and one for the credit, and Dr the account with the total, S$175.00.
// req: ledger/posting-rules Customer and supplier payments
// req: FR-AR-18
func TestPostCustomerReceiptInTheBaseCurrency(t *testing.T) {
	s := receiving()
	for _, c := range []struct {
		name         string
		r            CustomerReceipt
		want, settle string
	}{
		{"in full", sgdReceipt(current, "0", received(t, invoiceID, "1090.00", sgd, nil)),
			"1100 -1090.00 SGD = -1090.00; 1011 1090.00 SGD = 1090.00", "a1110 1090.00 = 1090.00 fx 0.00"},
		{"two invoices and a credit", sgdReceipt(current, "25.00", received(t, invoiceID, "100.00", sgd, nil), received(t, otherInvoice, "50.00", sgd, nil)),
			"1100 -100.00 SGD = -100.00; 1100 -50.00 SGD = -50.00; 1100 -25.00 SGD = -25.00; 1011 175.00 SGD = 175.00",
			"a1110 100.00 = 100.00 fx 0.00; a1111 50.00 = 50.00 fx 0.00"},
		{"all of it a credit", sgdReceipt(current, "40.00"), "1100 -40.00 SGD = -40.00; 1011 40.00 SGD = 40.00", ""},
	} {
		e, ss, err := PostCustomerReceipt(c.r, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
		if got := settlements(ss); got != c.settle {
			t.Errorf("%s: settled %s, want %s", c.name, got, c.settle)
		}
		if e.RuleVersion != RuleCustomerReceipt || e.Source != (Source{SourceCustomerReceipt, receiptID.String(), 1}) ||
			e.PostingIdentity != CustomerReceiptIdentity(receiptID, 1, RuleCustomerReceipt) || e.Memo != "Acme Analytics, FAST 7731" || !e.PostingDate.Equal(day) {
			t.Errorf("%s: the entry's identity: %+v", c.name, e)
		}
	}
	e, _, _ := PostCustomerReceipt(sgdReceipt(current, "25.00", received(t, invoiceID, "100.00", sgd, nil)), s)
	if e.Lines[1].Description != "Unapplied: a credit with the customer" {
		t.Errorf("the credit's line: %+v", e.Lines[1])
	}
}

// ENG-41 for receipts, worked by hand. A USD invoice of 1,000.00 issued at 1.34 carries S$1,340.00 on 1100.
//   - Received into the USD wallet at 1.36: Cr 1100 USD 1,000.00 = S$1,340.00 (its carrying amount), Dr 1021 USD 1,000.00
//     = S$1,360.00, and Cr 7030 S$20.00, a realised gain: more came in than it carried.
//   - Received into the SGD current account, which received S$1,350.00: Dr 1011 S$1,350.00 and Cr 7030 S$10.00.
//   - Received at 1.30: S$1,300.00 came in, S$40.00 less, a realised loss: Dr 7030 S$40.00.
//   - US$0.03 at 1.5 carries S$0.05 (RND-FX); received a cent at a time, each cent is S$0.02 at its rate, and the last
//     clears the S$0.01 left: a residual of −0.01 on 1100, base-only, and 7030 takes it.
//
// req: ENG-41
// req: ledger/posting-rules Customer and supplier payments
// req: ledger/posting-rules Settlement difference on an invoice or bill
func TestPostCustomerReceiptRealisesFX(t *testing.T) {
	s := receiving()
	stored := usdRate(t, "1.34")
	intoSGD := usdReceipt(t, current, "1.34", received(t, invoiceID, "1000.00", usd, stored))
	intoSGD.Rate = nil
	x := amt(t, "1350.00", sgd)
	intoSGD.Received = &x
	odd := received(t, invoiceID, "0.01", usd, usdRate(t, "1.5"))
	left := amt(t, "0.01", sgd)
	odd.Clears = &left
	for _, c := range []struct {
		name         string
		r            CustomerReceipt
		want, settle string
	}{
		{"a gain, into the wallet", usdReceipt(t, wallet, "1.36", received(t, invoiceID, "1000.00", usd, stored)),
			"1100 -1000.00 USD = -1340.00; 1021 1000.00 USD = 1360.00; 7030 -20.00 SGD = -20.00", "a1110 1000.00 = 1340.00 fx 20.00"},
		{"a gain, into the current account", intoSGD,
			"1100 -1000.00 USD = -1340.00; 1011 1350.00 SGD = 1350.00; 7030 -10.00 SGD = -10.00", "a1110 1000.00 = 1340.00 fx 10.00"},
		{"a loss", usdReceipt(t, wallet, "1.30", received(t, invoiceID, "1000.00", usd, stored)),
			"1100 -1000.00 USD = -1340.00; 1021 1000.00 USD = 1300.00; 7030 40.00 SGD = 40.00", "a1110 1000.00 = 1340.00 fx -40.00"},
		{"the last cent clears a residual", usdReceipt(t, wallet, "1.5", odd),
			"1100 -0.01 USD = -0.02; 1100 0.00 USD = 0.01; 1021 0.01 USD = 0.02; 7030 -0.01 SGD = -0.01", "a1110 0.01 = 0.01 fx 0.00"},
	} {
		e, ss, err := PostCustomerReceipt(c.r, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
		if got := settlements(ss); got != c.settle {
			t.Errorf("%s: settled %s, want %s", c.name, got, c.settle)
		}
	}
}

// What the receipt rule refuses comes back together, worded for a receipt and naming its fields (ENG-22, ENG-23): a
// chart with no accounts receivable account, a part in another currency, and a base-currency account with no amount
// received for USD invoices.
// req: ledger/posting-rules Customer and supplier payments
// req: QA-RPT-02
func TestTheReceiptRuleRefusesWhatCannotPost(t *testing.T) {
	noAR := receiving()
	delete(noAR.Accounts, receivables.ID)
	r := usdReceipt(t, current, "1.34", received(t, invoiceID, "10.00", sgd, nil))
	r.Rate = nil
	_, _, err := PostCustomerReceipt(r, noAR)
	probs, _ := err.(Problems)
	got := map[string]string{}
	for _, p := range probs {
		got[p.Field] = p.Code + " " + p.Message + " / " + p.Fix
	}
	if !strings.HasPrefix(got["received"], CodePaidAmount+" account 1011 is in SGD: enter what it received in SGD") ||
		!strings.HasPrefix(got["invoices"], CodeSystemRole+" the chart has no accounts receivable account / Load the standard chart's 1100 Accounts receivable") ||
		!strings.HasPrefix(got["invoices[1].amount"], CodePaymentAmount+" the amount settled is 10.00 SGD") {
		t.Errorf("the refusals: %v", got)
	}
	if _, _, err := PostCustomerReceipt(sgdReceipt(current, "0"), receiving()); !slices.Equal(codes(err), []string{CodePaymentAmount}) ||
		!strings.Contains(err.Error(), "the receipt settles nothing") {
		t.Errorf("a receipt of nothing: %v", err)
	}
}

// Any receipt balances in the base currency (INV-01), takes exactly the invoices' carrying amounts off 1100, and 7030
// takes its settlements' realised FX (ENG-41), give or take the cent RND-FX of the total leaves against each part.
// req: QA-ENG-03
// req: INV-01
// req: ENG-41
func TestCustomerReceiptsProperties(t *testing.T) {
	s := receiving()
	rapid.Check(t, func(t *rapid.T) {
		stored := rapid.SampledFrom([]string{"1.34", "1.3456", "1.5", "0.7"}).Draw(t, "stored")
		rate := rapid.SampledFrom([]string{"1.30", "1.36", "1.3456", "1.5"}).Draw(t, "rate")
		rr, _ := money.ParseRate(rate, usd, sgd, "test")
		r := CustomerReceipt{ID: receiptID, Version: 1, Date: day, Memo: "Globex", Account: wallet.ID, Currency: usd, Rate: &rr, Unapplied: money.Zero(usd)}
		sr, _ := money.ParseRate(stored, usd, sgd, "test")
		for i := range rapid.IntRange(1, 4).Draw(t, "invoices") {
			amount := money.FromMinor(rapid.Int64Range(1, 10_000_000).Draw(t, fmt.Sprintf("amount %d", i)), usd)
			r.Invoices = append(r.Invoices, ReceivedInvoice{InvoiceID: uuid.New(), Amount: amount, StoredRate: sr})
		}
		e, ss, err := PostCustomerReceipt(r, s)
		if err != nil {
			t.Fatal(err)
		}
		sum, ar, fx := money.Zero(sgd), money.Zero(sgd), money.Zero(sgd)
		for _, l := range e.Lines {
			sum, _ = sum.Add(l.Base)
			switch l.AccountID {
			case receivables.ID:
				ar, _ = ar.Add(l.Base)
			case fxRounding.ID:
				fx, _ = fx.Add(l.Base)
			}
		}
		if !sum.IsZero() {
			t.Fatalf("the entry does not balance: %s", sum)
		}
		carried, gains := money.Zero(sgd), money.Zero(sgd)
		for _, x := range ss {
			carried, _ = carried.Add(x.Base)
			gains, _ = gains.Add(x.RealisedFX)
		}
		// 7030 takes the realised FX, and the cents RND-FX of the total leaves against the parts': at most one a part.
		cents, _ := fx.Neg().Sub(gains)
		if c := cents.Decimal(); !ar.Neg().Equal(carried) || c.Abs(c).Cmp(money.FromMinor(int64(len(r.Invoices)), sgd).Decimal()) > 0 {
			t.Fatalf("1100 takes %s of %s carried; 7030 takes %s of %s realised", ar.Neg(), carried, fx.Neg(), gains)
		}
	})
}

// Each customer receipt rule version reproduces exactly what it produced over the same receipts (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryCustomerReceiptRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	stored := usdRate(t, "1.34")
	intoSGD := usdReceipt(t, current, "1.34", received(t, invoiceID, "1000.00", usd, stored))
	intoSGD.Rate = nil
	x := amt(t, "1350.00", sgd)
	intoSGD.Received = &x
	cases := map[string][]struct {
		name string
		r    CustomerReceipt
	}{
		RuleCustomerReceipt: {
			{"two SGD invoices and a credit", sgdReceipt(current, "25.00", received(t, invoiceID, "100.00", sgd, nil), received(t, otherInvoice, "50.00", sgd, nil))},
			{"USD into the wallet, a gain", usdReceipt(t, wallet, "1.36", received(t, invoiceID, "1000.00", usd, stored))},
			{"USD into the SGD account", intoSGD},
		},
	}
	for rule := range CustomerReceiptRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryCustomerReceiptRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, ss, err := ReplayCustomerReceipt(rule, c.r, receiving())
			if err != nil {
				t.Fatalf("%s, %s: %v", rule, c.name, err)
			}
			b.WriteString(render(c.name, e))
			for _, x := range ss {
				fmt.Fprintf(&b, "settles %s %s %s = %s %s, realised %s\n", x.DocumentID, x.Amount, x.Amount.Currency().Code, x.Base, x.Base.Currency().Code, x.RealisedFX)
			}
		}
		path := filepath.Join("testdata", "replay", rule+".golden")
		if *update {
			if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run `go test ./internal/domain/ledger -run %s -update` for a new rule version, and review it)", err, t.Name())
		}
		if string(want) != b.String() {
			t.Errorf("%s no longer reproduces its entries (ENG-06): make the change a new rule version.\n--- want\n%s\n--- got\n%s", rule, want, b.String())
		}
	}
	if _, _, err := ReplayCustomerReceipt("customer_receipt@0", CustomerReceipt{}, receiving()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
