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

var creditNoteID = uuid.MustParse("0192f1a0-0000-7000-8000-0000000c2110")

// Worked by hand: a credit note for S$200.00 of consulting at 9% posts Dr 4020 200.00, Dr 2200 18.00, Cr 1100 218.00:
// an invoice's legs the other way. In US dollars at 1.3456, USD 1,000.00 at 9% credits USD 1,090.00 = S$1,466.70 to
// 1100, debits 4020 USD 1,000.00 = S$1,345.60 and 2200 S$121.10 (ENG-42's figures, credited back). A refusal names the
// credit note, and a spread line is refused.
// req: FR-AR-04
// req: ledger/posting-rules Credit documents and opening items
// req: ledger/posting-rules Posting an invoice
func TestPostCreditNoteReversesAnInvoicesLegs(t *testing.T) {
	s := selling()
	sgdNote := Invoice{ID: creditNoteID, Version: 1, Date: day, Memo: "Acme Analytics CN-0001", Currency: sgd,
		Lines: []InvoiceLine{invoiceLine(t, consulting, "200.00", sgd, "SR", "0.09")}}
	e, err := PostCreditNote(sgdNote, s)
	if err != nil || legs(s, e) != "1100 -218.00 SGD = -218.00; 4020 200.00 SGD = 200.00; 2200 18.00 SGD = 18.00" {
		t.Fatalf("an SGD credit note: %s %v", legs(s, e), err)
	}
	if e.RuleVersion != RuleCreditNote || e.Source != (Source{SourceCreditNote, creditNoteID.String(), 1}) ||
		e.PostingIdentity != CreditNoteIdentity(creditNoteID, 1, RuleCreditNote) {
		t.Errorf("the entry's identity: %+v", e)
	}
	usdNote := Invoice{ID: creditNoteID, Version: 1, Date: day, Memo: "Globex CN-0002", Currency: usd, Rate: usdRate(t, "1.3456"),
		Lines: []InvoiceLine{invoiceLine(t, consulting, "1000.00", usd, "SR", "0.09")}}
	if e, err := PostCreditNote(usdNote, s); err != nil || legs(s, e) != "1100 -1090.00 USD = -1466.70; 4020 1000.00 USD = 1345.60; 2200 121.10 SGD = 121.10" {
		t.Errorf("a USD credit note: %s %v", legs(s, e), err)
	}
	_, err = PostCreditNote(Invoice{ID: creditNoteID, Version: 1, Currency: sgd}, s)
	probs, _ := err.(Problems)
	if !slices.Equal(codes(err), []string{CodeNoDate, CodeTooFewLines}) || !slices.ContainsFunc(probs, func(p Problem) bool { return p.Message == "the credit note has no date" }) {
		t.Errorf("a refusal: %v", err)
	}
	negative := Invoice{ID: creditNoteID, Version: 1, Date: day, Currency: sgd, Lines: []InvoiceLine{invoiceLine(t, consulting, "-5.00", sgd, "SR", "")}}
	if _, err := PostCreditNote(negative, s); !strings.Contains(fmt.Sprint(err), "A credit note's total is more than zero") {
		t.Errorf("a credit note of a negative total: %v", err)
	}
	spread := sgdNote
	spread.Lines = []InvoiceLine{spreadInvoiceLine(invoiceLine(t, subscribe, "1200.00", sgd, "SR", "0.09"), october, 12)}
	_, err = PostCreditNote(spread, s)
	if probs, _ := err.(Problems); !slices.Equal(codes(err), []string{CodeSpread}) || probs[0].Field != "lines[1].spread_months" || probs[0].Message != "a credit note's line is not spread" {
		t.Errorf("a spread line: %v", err)
	}
}

// Any credit note the rule accepts posts exactly the legs the same content posts as an invoice, negated.
// req: QA-ENG-03
// req: FR-AR-04
func TestACreditNoteIsAnInvoiceNegated(t *testing.T) {
	s := selling()
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.SampledFrom([]money.Currency{sgd, usd}).Draw(t, "currency")
		i := Invoice{ID: creditNoteID, Version: 1, Date: day, Currency: cur}
		if cur == usd {
			r, _ := money.ParseRate(rapid.SampledFrom([]string{"1.3472", "1.33335", "0.7"}).Draw(t, "rate"), usd, sgd, "test")
			i.Rate = &r
		}
		for k := range rapid.IntRange(1, 4).Draw(t, "lines") {
			code := rapid.SampledFrom([]string{"OS", "ZR", "SR"}).Draw(t, fmt.Sprintf("code %d", k)) // each code with its one rate
			i.Lines = append(i.Lines, InvoiceLine{AccountID: rapid.SampledFrom([]uuid.UUID{consulting.ID, subscribe.ID}).Draw(t, fmt.Sprintf("account %d", k)),
				Net: money.FromMinor(rapid.Int64Range(1, 9_999_999).Draw(t, fmt.Sprintf("net %d", k)), cur), GSTCode: code,
				GSTRate: map[string]string{"OS": "", "ZR": "0", "SR": "0.09"}[code]})
		}
		asInvoice, err := PostInvoice(i, s)
		if err != nil {
			t.Fatal(err)
		}
		note, err := PostCreditNote(i, s)
		if err != nil || len(note.Lines) != len(asInvoice.Lines) {
			t.Fatalf("%v: %d lines against %d", err, len(note.Lines), len(asInvoice.Lines))
		}
		for k, l := range note.Lines {
			if !l.Amount.Equal(asInvoice.Lines[k].Amount.Neg()) || !l.Base.Equal(asInvoice.Lines[k].Base.Neg()) || l.AccountID != asInvoice.Lines[k].AccountID {
				t.Fatalf("line %d: %s = %s, the invoice's %s = %s", k, l.Amount, l.Base, asInvoice.Lines[k].Amount, asInvoice.Lines[k].Base)
			}
		}
	})
}

// Each credit note rule version reproduces exactly what it produced over the same credit notes (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryCreditNoteRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	cases := map[string][]struct {
		name string
		i    Invoice
	}{
		RuleCreditNote: {
			{"an SGD credit note, standard and zero-rated", Invoice{ID: creditNoteID, Version: 1, Date: day, Memo: "Acme Analytics CN-0001", Currency: sgd,
				Lines: []InvoiceLine{invoiceLine(t, subscribe, "500.00", sgd, "ZR", "0"), invoiceLine(t, consulting, "200.00", sgd, "SR", "0.09")}}},
			{"not registered", Invoice{ID: creditNoteID, Version: 1, Date: day, Memo: "Acme Analytics CN-0002", Currency: sgd,
				Lines: []InvoiceLine{invoiceLine(t, consulting, "1000.00", sgd, "SR", "")}}},
			{"USD with its GST on the SGD net", Invoice{ID: creditNoteID, Version: 2, Date: day, Memo: "Globex CN-0003", Currency: usd, Rate: usdRate(t, "1.3456"),
				Lines: []InvoiceLine{invoiceLine(t, consulting, "1000.00", usd, "SR", "0.09")}}},
			{"two cents of rounding the other way", Invoice{ID: creditNoteID, Version: 1, Date: day, Memo: "Globex CN-0004", Currency: usd, Rate: usdRate(t, "1.33335"),
				Lines: []InvoiceLine{invoiceLine(t, consulting, "0.05", usd, "OS", ""), invoiceLine(t, consulting, "0.05", usd, "OS", "")}}},
		},
	}
	for rule := range CreditNoteRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryCreditNoteRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayCreditNote(rule, c.i, selling())
			if err != nil {
				t.Fatalf("%s, %s: %v", rule, c.name, err)
			}
			b.WriteString(render(c.name, e))
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
	if _, err := ReplayCreditNote("credit_note@0", Invoice{ID: creditNoteID, Version: 1}, selling()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
