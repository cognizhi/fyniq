package migration

import (
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The sample's plan (design D3, D5; rev 0.258): its set-up in order, each record keyed by its row once; the profile
// and the year first, the chart before the accounts the mapping creates, contacts after them, and the bank rule that
// gives a money account kept in the archive. A clean start starts its books the day after Carry Forward's date. Two
// rows that would import as one record are an error of the validation report.
// req: FR-MIG-01
// req: data/migration Set-up import and mapping review (M2)
func TestTheSamplePlansItsSetUpOnce(t *testing.T) {
	w := read(t, sample(t))
	m := w.Propose(standardTarget(t), nil)
	plan, probs := w.Plan(m, storage.PathHistory)
	if len(probs) != 0 {
		t.Fatalf("the sample's plan: %+v", probs)
	}
	kinds, keys, first := map[string]int{}, map[string]bool{}, map[string]int{}
	for i, r := range plan {
		if keys[r.Key] || r.Sheet == "" || r.make == nil {
			t.Errorf("record %d: %+v", i, r)
		}
		keys[r.Key] = true
		if _, ok := first[r.Kind]; !ok {
			first[r.Kind] = i
		}
		kinds[r.Kind]++
	}
	if kinds[KindContact] != 16 || kinds[KindItem] != 5 || kinds[KindFXRate] != 9 || kinds[KindBankRule] != 28 || kinds[KindSettings] != 1 ||
		kinds[KindFinancialYear] != 1 || kinds[KindGSTRegistration] != 1 || kinds[KindChart] != 1 || kinds[KindDepartment] != 5 {
		t.Errorf("the plan by kind: %v", kinds)
	}
	if plan[0].Key != "settings" || plan[1].Kind != KindFinancialYear || first[KindChart] > first[KindAccount] && kinds[KindAccount] > 0 ||
		first[KindContact] < first[KindChart] || first[KindBankRule] < first[KindContact] {
		t.Errorf("the plan's order: %v", first)
	}
	// The history (design D5): the opening balances after the set-up, the opening items first, then every invoice, credit
	// note, bill and journal in date order; a clean start has none of it.
	if kinds[KindOpeningBalances] != 1 || kinds[KindInvoice]+kinds[KindCreditNote] != 25 || kinds[KindCreditNote] != 1 || kinds[KindBill] != 17 ||
		kinds[KindJournal] != 2 || first[KindOpeningBalances] < first[KindBankRule] || plan[first[KindInvoice]].Key != "invoice:INV-0098" ||
		plan[first[KindBill]].Key != "bill:tally & co accounting:TAL-2025-112" || first[KindStatement] < first[KindOpeningBalances] ||
		first[KindInvoice] < first[KindStatement] {
		t.Errorf("the history: %v, the first invoice %s and bill %s", kinds, plan[first[KindInvoice]].Key, plan[first[KindBill]].Key)
	}
	// Transactions (rev 0.259): a statement for each of the 6 money accounts they move, then each row as a receipt,
	// a payment of a bill, a transfer or a categorised transaction; the 2 rows to share capital or a SAFE wait for the
	// Funding sheet (task 4.6).
	if kinds[KindStatement] != 6 || kinds[KindReceipt] != 18 || kinds[KindBillPayment] != 14 || kinds[KindTransfer] != 23 ||
		kinds[KindReceipt]+kinds[KindBillPayment]+kinds[KindTransfer]+kinds[KindBankTransaction] != len(w.Transactions)-2 || kinds[KindMoneyAccount] != 7 {
		t.Errorf("the transactions: %v of %d rows", kinds, len(w.Transactions))
	}
	clean, _ := w.Plan(m, storage.PathCleanStart)
	for _, r := range clean {
		if r.Kind == KindInvoice || r.Kind == KindOpeningBalances || r.Kind == KindJournal {
			t.Errorf("a clean start imports the history's %s", r.Key)
		}
	}
	// Quotes and orders, the assets and their depreciation to the "as of", last.
	if kinds[KindQuote] != 3 || kinds[KindPurchaseOrder] != 1 || kinds[KindAsset] != 3 || kinds[KindDepreciation] != 1 ||
		plan[len(plan)-1].Kind != KindDepreciation {
		t.Errorf("the quotes, orders and assets: %v, last %s", kinds, plan[len(plan)-1].Key)
	}
	if got := openingAccumulated(w.Assets[0], w.Settings.BooksStart, money.MustLookup("SGD")); got.Text('f') != "3000.00" {
		t.Errorf("the MacBooks' depreciation before the books start: %s, not the 3000.00 the opening balances carry", got.Text('f'))
	}
	if keys["bank-rule:1"] || !keys["bank-rule:2"] || !w.transferRule(1) {
		t.Error("the bank rule to a money account is imported")
	}
	if !w.BooksStart(storage.PathCleanStart).Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) || !w.BooksStart(storage.PathHistory).Equal(w.Settings.BooksStart) {
		t.Errorf("the books start: %s and %s", w.BooksStart(storage.PathHistory), w.BooksStart(storage.PathCleanStart))
	}

	w.Contacts = append(w.Contacts, Contact{Origin: Origin{Sheet: "Contacts", Row: 40}, Name: strings.ToUpper(w.Contacts[0].Name), Role: "Customer"})
	if _, probs := w.Plan(m, storage.PathHistory); len(probs) != 1 || probs[0].Code != CodeImport || probs[0].Row != 40 || !strings.Contains(probs[0].Message, "Contacts row 6") {
		t.Errorf("two contacts as one: %+v", probs)
	}
}

// A workbook's countries read as ISO codes: "USA" is US; one the import does not know, such as "Various", is none.
// req: FR-MIG-01
func TestCountriesReadAsCodes(t *testing.T) {
	for in, want := range map[string]string{"Singapore": "SG", "USA": "US", " united kingdom ": "GB", "MY": "MY", "Various": "", "": ""} {
		if got := countryOf(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
