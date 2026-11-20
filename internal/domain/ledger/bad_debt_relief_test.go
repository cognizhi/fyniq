package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var reliefID = uuid.MustParse("0192f1a0-0000-7000-8000-0000000b0dc1")

// relief is a claim for a write-off of written of the debt the invoice made the customer owe (owed), over the output tax the invoice posted
// (output, in SGD), after the earlier claims' amounts.
func relief(t testing.TB, cur money.Currency, output, owed, before, written, claimed string, account Account) BadDebtRelief {
	return BadDebtRelief{ID: reliefID, Version: 1, Date: day, Memo: "INV-0140 Acme Analytics: bad debt relief", WriteOffID: writeOffID, InvoiceID: invoiceID,
		Written: amt(t, written, cur), Owed: amt(t, owed, cur), WrittenBefore: amt(t, before, cur), Output: amt(t, output, sgd), ClaimedBefore: amt(t, claimed, sgd), Account: account.ID}
}

// FR-AR-11 and the posting rule "GST bad debt relief", worked by hand: the claim takes the output tax in the part written off out of the GST
// control account and puts it against the account the write-off took the debt to, in SGD.
//   - All of an SGD invoice of 1,090.00 (output tax 90.00): Dr 2200 90.00, Cr the account 90.00.
//   - A half of it (545.00): 90.00 x 545.00 / 1,090.00 = 45.00.
//   - USD 54.50 of a USD 109.00 invoice that posted S$12.15 of output tax: 6.075, to S$6.08 half away from zero.
//   - S$10.00 of S$10.90 with S$0.90: 0.8257, S$0.83 (the figure the bad-debt queue shows).
//
// req: FR-AR-11
// req: RND-BDR
// req: ledger/posting-rules Bad-debt write-off
func TestPostBadDebtRelief(t *testing.T) {
	s := selling()
	for _, c := range []struct {
		name string
		r    BadDebtRelief
		want string
	}{
		{"all of an SGD invoice", relief(t, sgd, "90.00", "1090.00", "0.00", "1090.00", "0.00", software), "2200 90.00 SGD = 90.00; 6120 -90.00 SGD = -90.00"},
		{"half of it", relief(t, sgd, "90.00", "1090.00", "0.00", "545.00", "0.00", software), "2200 45.00 SGD = 45.00; 6120 -45.00 SGD = -45.00"},
		{"half a foreign invoice", relief(t, usd, "12.15", "109.00", "0.00", "54.50", "0.00", software), "2200 6.08 SGD = 6.08; 6120 -6.08 SGD = -6.08"},
		{"ten of 10.90", relief(t, sgd, "0.90", "10.90", "0.00", "10.00", "0.00", software), "2200 0.83 SGD = 0.83; 6120 -0.83 SGD = -0.83"},
		{"the last third takes what is left", relief(t, sgd, "0.10", "0.30", "0.20", "0.10", "0.06", software), "2200 0.04 SGD = 0.04; 6120 -0.04 SGD = -0.04"},
	} {
		e, err := PostBadDebtRelief(c.r, s)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := legs(s, e); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
		if e.RuleVersion != RuleBadDebtRelief || e.Source != (Source{SourceBadDebtRelief, reliefID.String(), 1}) || e.PostingIdentity != BadDebtReliefIdentity(reliefID, 1, RuleBadDebtRelief) {
			t.Errorf("%s: the entry's identity: %+v", c.name, e)
		}
	}
}

// What the claim rule refuses comes back together (ENG-22): no date, an account the claim cannot be credited to, a group heading, no GST left
// to claim, more written off than was owed, and a chart with no GST control account.
// req: FR-AR-11
// req: QA-RPT-02
func TestTheBadDebtReliefRuleRefusesWhatCannotPost(t *testing.T) {
	s := selling()
	r := relief(t, sgd, "0.00", "10.00", "0.00", "5.00", "0.00", receivables)
	r.Date = time.Time{}
	if _, err := PostBadDebtRelief(r, s); !slices.Equal(codes(err), []string{CodeNoDate, CodeRuleAccount, CodeNoRelief}) {
		t.Errorf("every refusal: %v", err)
	}
	if _, err := PostBadDebtRelief(relief(t, sgd, "0.90", "10.90", "0.00", "10.00", "0.00", group), s); !slices.Equal(codes(err), []string{CodeGroupAccount}) {
		t.Errorf("a group heading: %v", err)
	}
	if _, err := PostBadDebtRelief(relief(t, sgd, "0.90", "10.90", "0.00", "11.00", "0.00", software), s); !slices.Equal(codes(err), []string{CodePaymentAmount}) {
		t.Errorf("more than was owed: %v", err)
	}
	if _, err := PostBadDebtRelief(relief(t, sgd, "0.01", "100.00", "0.00", "1.00", "0.00", software), s); !slices.Equal(codes(err), []string{CodeNoRelief}) {
		t.Errorf("a share that rounds to nothing: %v", err)
	}
	if _, err := PostBadDebtRelief(relief(t, sgd, "0.90", "10.90", "5.00", "5.00", "0.90", software), s); !slices.Equal(codes(err), []string{CodeNoRelief}) {
		t.Errorf("everything already claimed: %v", err)
	}
	bad := relief(t, sgd, "0.90", "10.90", "0.00", "5.00", "0.00", software)
	bad.Output = amt(t, "0.90", usd)
	if _, err := PostBadDebtRelief(bad, s); !slices.Equal(codes(err), []string{CodePaymentAmount}) || !strings.Contains(err.Error(), "accounted for in SGD") {
		t.Errorf("output tax in another currency: %v", err)
	}
	noGST := selling()
	delete(noGST.Accounts, gstControl.ID)
	if _, err := PostBadDebtRelief(relief(t, sgd, "0.90", "10.90", "0.00", "10.00", "0.00", software), noGST); !slices.Equal(codes(err), []string{CodeSystemRole}) {
		t.Errorf("no GST control account: %v", err)
	}
}

// Each claim rule version reproduces exactly what it produced over the same claims (ENG-06).
// req: ENG-06
// req: QA-ENG-06
func TestEveryBadDebtReliefRuleVersionReplaysItsGoldenEntries(t *testing.T) {
	cases := map[string][]struct {
		name string
		r    BadDebtRelief
	}{
		RuleBadDebtRelief: {
			{"all of an SGD invoice", relief(t, sgd, "90.00", "1090.00", "0.00", "1090.00", "0.00", software)},
			{"half a foreign invoice", relief(t, usd, "12.15", "109.00", "0.00", "54.50", "0.00", software)},
			{"the last third takes what is left", relief(t, sgd, "0.10", "0.30", "0.20", "0.10", "0.06", software)},
		},
	}
	for rule := range BadDebtReliefRules {
		if len(cases[rule]) == 0 {
			t.Errorf("rule version %s has no replay cases: add them, and its golden file", rule)
		}
	}
	for rule, cs := range cases {
		var b strings.Builder
		fmt.Fprintf(&b, "# What %s posts for each replay case. Generated by TestEveryBadDebtReliefRuleVersionReplaysItsGoldenEntries;\n"+
			"# a rule version's output never changes (ENG-06): a different output is a new rule version.\n", rule)
		for _, c := range cs {
			e, err := ReplayBadDebtRelief(rule, c.r, selling())
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
	if _, err := ReplayBadDebtRelief("bad_debt_relief@0", BadDebtRelief{}, selling()); !slices.Contains(codes(err), CodeUnknownRule) {
		t.Errorf("an unknown rule version: %v", err)
	}
}
