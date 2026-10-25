package banking

import (
	"context"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Suggestions for a line in the Categorise queue (FR-BNK-06, FR-BNK-07; p1-bank-import-reconcile task 4.3, design
// D5): ranked, never applied by themselves (auto-reconcile is task 5.1). First a document recorded by hand for the
// line's money, which the line matches without posting again; then open invoices (money in) or bills (money out)
// whose number, or a bill's supplier number, the bank's reference or description names exactly, then once NFKC-
// normalised, trimmed and without regard to case; then those open for the line's amount from a contact the
// counterparty names; and last the first rule the line meets.

// How a suggestion was found, best first.
const (
	FoundRecorded   = "recorded"   // a receipt, payment, deposit or refund recorded by hand for this money
	FoundReference  = "reference"  // the document's number, exactly as written
	FoundNormalised = "normalised" // the document's number, NFKC-normalised, trimmed, without regard to case
	FoundAmount     = "amount"     // the amount open, from the contact the counterparty names
	FoundRule       = "rule"       // the first rule the line meets
)

var foundOrder = []string{FoundRecorded, FoundReference, FoundNormalised, FoundAmount, FoundRule}

// Suggestion is one match offered for a line: how it was found, the kind of match accepting it makes, and the
// document (with its contact and what is open of it) or the rule.
type Suggestion struct {
	Found      string
	Kind       string // storage.MatchRecorded, MatchReceipt (invoices), MatchBillPayment (bills) or MatchCategorised (a rule)
	DocumentID uuid.UUID
	Number     string
	ContactID  uuid.UUID
	Contact    string
	Open       *money.Money // what is open of an invoice or bill, in its currency
	Rule       *Rule
}

// SuggestWindow is how many days after a line's booking date an invoice or bill may be dated and still be what it
// settles: a payment made a few days before the document is entered.
const SuggestWindow = 7

// Suggest ranks what a line may match, from what is held in u: rules as Rules gives them.
func Suggest(ctx context.Context, u storage.UnitOfWork, line storage.BankLine, rules []Rule) ([]Suggestion, error) {
	var out []Suggestion
	recorded, err := journal.RecordedFor(ctx, u, line)
	if err != nil {
		return nil, err
	}
	for _, d := range recorded {
		out = append(out, Suggestion{Found: FoundRecorded, Kind: storage.MatchRecorded, DocumentID: d.ID, Number: d.Number})
	}
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return nil, err
	}
	amount, err := money.ParseStored(string(line.Amount), cur)
	if err != nil {
		return nil, err
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return nil, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return nil, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	role := ledger.RoleAccountsPayable
	if amount.Sign() > 0 {
		role = ledger.RoleAccountsReceivable
	}
	control := uuid.Nil
	for _, a := range accounts {
		if string(a.Role) == role {
			control = a.ID
		}
	}
	asAt := line.BookingDate.AddDate(0, 0, SuggestWindow)
	var groups []subledger.Group
	kind := storage.MatchBillPayment
	switch {
	case control == uuid.Nil:
	case amount.Sign() > 0:
		kind = storage.MatchReceipt
		l, err := receivables.Open(ctx, u, base, control, asAt)
		if err != nil {
			return nil, err
		}
		for _, c := range l.Customers {
			groups = append(groups, subledger.Group{ID: c.ID, Name: c.Name, Items: c.Items})
		}
	default:
		l, err := payables.Open(ctx, u, base, control, asAt)
		if err != nil {
			return nil, err
		}
		for _, sp := range l.Suppliers {
			groups = append(groups, subledger.Group{ID: sp.ID, Name: sp.Name, Items: sp.Items})
		}
	}
	size := amount
	if size.Sign() < 0 {
		size = size.Neg()
	}
	text := line.Reference + " " + line.Description
	party := foldRef(line.Counterparty + " " + line.Description)
	for _, g := range groups {
		for _, it := range g.Items {
			if it.Kind != receivables.KindInvoice && it.Kind != payables.KindBill || it.Open.Sign() <= 0 {
				continue
			}
			numbers := []string{it.Number}
			if kind == storage.MatchBillPayment {
				if ref, err := supplierRef(ctx, u, it.DocumentID, base); err != nil {
					return nil, err
				} else if ref != "" {
					numbers = append(numbers, ref)
				}
			}
			found := ""
			switch {
			case names(text, numbers, false):
				found = FoundReference
			case names(text, numbers, true):
				found = FoundNormalised
			case it.Open.Equal(size) && g.Name != "" && strings.Contains(party, foldRef(g.Name)):
				found = FoundAmount
			default:
				continue
			}
			open := it.Open
			out = append(out, Suggestion{Found: found, Kind: kind, DocumentID: it.DocumentID, Number: it.Number, ContactID: g.ID, Contact: g.Name, Open: &open})
		}
	}
	if r, ok := First(rules, line); ok {
		out = append(out, Suggestion{Found: FoundRule, Kind: storage.MatchCategorised, Rule: &r})
	}
	slices.SortStableFunc(out, func(a, b Suggestion) int {
		return slices.Index(foundOrder, a.Found) - slices.Index(foundOrder, b.Found)
	})
	return out, nil
}

// names says whether text names any of the numbers: exactly as written, or normalised.
func names(text string, numbers []string, normalised bool) bool {
	if normalised {
		text = foldRef(text)
	}
	for _, n := range numbers {
		if normalised {
			n = foldRef(n)
		}
		if n != "" && containsWord(text, n) {
			return true
		}
	}
	return false
}

// containsWord says whether text holds n with no letter or digit right before or after it, so INV-1 is not named by
// INV-10.
func containsWord(text, n string) bool {
	for i := 0; i+len(n) <= len(text); {
		j := strings.Index(text[i:], n)
		if j < 0 {
			return false
		}
		at := i + j
		before, _ := utf8.DecodeLastRuneInString(text[:at])
		after, _ := utf8.DecodeRuneInString(text[at+len(n):])
		if !wordRune(before) && !wordRune(after) {
			return true
		}
		_, k := utf8.DecodeRuneInString(text[at:])
		i = at + k
	}
	return false
}

func wordRune(r rune) bool { return r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r)) }

// foldRef is a reference as matching compares it (FR-BNK-07): NFKC, without regard to case, trimmed, with
// non-breaking and other spaces collapsed.
func foldRef(s string) string { return fold(strings.ReplaceAll(s, " ", " ")) }

// supplierRef is a bill's supplier number, "" for none.
func supplierRef(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, base money.Currency) (string, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return "", err
	}
	d, err := bill.Read(doc, base)
	if err != nil {
		return "", nil // a vendor credit or a bill that no longer reads: matched by our number only
	}
	return d.SupplierRef, nil
}
