package ledger

import (
	"slices"
	"strings"
	"testing"
)

// Opening balances post as entered at the books start and net to zero (FR-GL-06, posting rule "Opening balances"),
// worked by hand: SGD 10,000.00 in the bank against SGD 10,000.00 of share capital, which INV-13 lets opening
// balances post to, under their own source, rule and identity.
// req: FR-GL-06
// req: ledger/posting-rules Opening balances
// req: INV-13
func TestOpeningBalancesPostAsEnteredWithShareCapital(t *testing.T) {
	j := Journal{ID: docID, Version: 2, Date: day, Memo: "Opening balances", Lines: []JournalLine{
		{AccountID: bank.ID, Amount: amt(t, "10000.00", sgd), Description: "DBS"},
		{AccountID: shareCapital.ID, Amount: amt(t, "-10000.00", sgd)},
	}}
	e, err := PostOpening(j, withShareCapital())
	if err != nil {
		t.Fatal(err)
	}
	if e.Source != (Source{Type: SourceOpeningBalance, ID: docID.String(), Version: 2}) || e.RuleVersion != RuleOpeningBalance ||
		e.PostingIdentity != "opening_balance:"+docID.String()+":v2:opening_balance@1" || e.PostingIdentity != OpeningIdentity(docID, 2, RuleOpeningBalance) {
		t.Errorf("provenance: %+v %s %s", e.Source, e.RuleVersion, e.PostingIdentity)
	}
	if !e.PostingDate.Equal(day) || e.Memo != "Opening balances" || len(e.Lines) != 2 || e.Lines[0].Base.String() != "10000.00" ||
		e.Lines[1].Base.String() != "-10000.00" || e.Lines[0].Description != "DBS" || e.Lines[1].Rate.Source != "base" {
		t.Errorf("lines as entered: %+v", e)
	}
	// The same lines in a manual journal are refused: share capital is not a journal's to change.
	if _, err := PostJournal(j, withShareCapital()); !slices.Contains(codes(err), CodeShareCapital) {
		t.Errorf("a manual journal to share capital: %v", err)
	}
	if strings.HasPrefix(JournalIdentity(docID, 2, RuleManualJournal), SourceOpeningBalance) {
		t.Error("the two sources share an identity")
	}
}

// Opening balances refuse what a journal refuses, and a line in another currency; every failure at once.
// req: FR-GL-06
// req: ENG-24
func TestOpeningBalancesNetToZeroInTheBaseCurrency(t *testing.T) {
	rate := usdRate(t, "1.35")
	for name, c := range map[string]struct {
		j    Journal
		want []string
	}{
		"they do not net to zero": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: bank.ID, Amount: amt(t, "100.00", sgd)}, {AccountID: shareCapital.ID, Amount: amt(t, "-90.00", sgd)}}},
			[]string{CodeUnbalancedBase, CodeUnbalancedCur}},
		"a line in another currency, with its rate": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: usdBank.ID, Amount: amt(t, "100.00", usd), Rate: rate}, {AccountID: shareCapital.ID, Amount: amt(t, "-135.00", sgd)}}},
			[]string{CodeForeignOpening}},
		"no date and one line": {Journal{ID: docID, Version: 1, Lines: []JournalLine{{AccountID: bank.ID, Amount: amt(t, "1.00", sgd)}}},
			[]string{CodeNoDate, CodeTooFewLines, CodeUnbalancedBase, CodeUnbalancedCur}},
		"an inactive account and a zero line": {Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
			{AccountID: closed.ID, Amount: amt(t, "5.00", sgd)}, {AccountID: bank.ID, Amount: amt(t, "-5.00", sgd)},
			{AccountID: bank.ID, Amount: amt(t, "0.00", sgd)}}}, []string{CodeInactiveAccount, CodeZeroLine}},
	} {
		e, err := PostOpening(c.j, withShareCapital())
		if got := codes(err); !slices.Equal(got, sortedCopy(c.want)) || len(e.Lines) != 0 {
			t.Errorf("%s: codes %v (want %v), %d lines", name, got, sortedCopy(c.want), len(e.Lines))
		}
	}
	// The messages name the opening balances, not a journal.
	_, err := PostOpening(Journal{ID: docID, Version: 1}, withShareCapital())
	if !strings.Contains(err.Error(), "the opening balance entry has no date") || !strings.Contains(err.Error(), "the opening balance entry has 0 line(s)") {
		t.Errorf("messages: %v", err)
	}
	_, err = PostOpening(Journal{ID: docID, Version: 1, Date: day, Lines: []JournalLine{
		{AccountID: usdBank.ID, Amount: amt(t, "1.00", usd), Rate: rate}, {AccountID: bank.ID, Amount: amt(t, "-1.35", sgd)}}}, withShareCapital())
	if p := err.(Problems); len(p) != 1 || p[0].Field != "lines[1].amount" || !strings.Contains(p[0].Message, "is in USD; opening balances are in the base currency SGD") {
		t.Errorf("the foreign line's problem: %v", err)
	}
}
