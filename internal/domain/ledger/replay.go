package ledger

import (
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// Versioned posting rules (ENG-06; QA-ENG-06): a change to how a document posts is a new rule version beside the old
// ones, never an edit of one, and every entry names the version that posted it. Re-running that version over the same
// document version reproduces the entry exactly; the replay tests check it for each version over fixed documents, and
// the nightly replay over every stored document.

// Rule turns one version of a document into its entry under one rule version.
type Rule func(j Journal, s Settings) (Entry, error)

// Rules are every rule version the engine has posted with, by name. A version is never removed or changed while an
// entry names it: a new version is added instead, with its own replay cases (testdata/replay/<version>.golden).
var Rules = map[string]Rule{
	RuleManualJournal:  PostJournal,
	RuleOpeningBalance: PostOpening,
}

// CodeUnknownRule is the replay's refusal of an entry posted under a rule version the engine no longer has.
const CodeUnknownRule = "LEDGER-18"

// Replay re-runs the rule version that posted an entry over the same document version and settings (ENG-06).
func Replay(rule string, j Journal, s Settings) (Entry, error) {
	r, ok := Rules[rule]
	if !ok {
		return Entry{}, Problems{{CodeUnknownRule, "rule_version", fmt.Sprintf("the engine has no rule version %s", rule),
			"Restore the rule version: an old version is never removed while an entry names it (ENG-06)"}}
	}
	return r(j, s)
}

// Differences lists where got differs from want, field by field and line by line; none when the two entries are the
// same in every stored field: date, source, rule version, posting identity, memo, and each line's account, amount and
// currency, rate with its source, base amount and description.
func Differences(want, got Entry) []string {
	var out []string
	diff := func(field string, w, g any) {
		if fmt.Sprint(w) != fmt.Sprint(g) {
			out = append(out, fmt.Sprintf("%s: %v, not %v", field, g, w))
		}
	}
	diff("posting_date", want.PostingDate.Format(time.DateOnly), got.PostingDate.Format(time.DateOnly))
	diff("source", want.Source, got.Source)
	diff("rule_version", want.RuleVersion, got.RuleVersion)
	diff("posting_identity", want.PostingIdentity, got.PostingIdentity)
	diff("memo", want.Memo, got.Memo)
	diff("lines", len(want.Lines), len(got.Lines))
	for i := 0; i < min(len(want.Lines), len(got.Lines)); i++ {
		w, g := want.Lines[i], got.Lines[i]
		at := fmt.Sprintf("line %d ", i+1)
		diff(at+"account", w.AccountID, g.AccountID)
		diff(at+"amount", w.Amount.String()+" "+w.Amount.Currency().Code, g.Amount.String()+" "+g.Amount.Currency().Code)
		if w.Rate.Decimal().Cmp(g.Rate.Decimal()) != 0 || w.Rate.From != g.Rate.From || w.Rate.To != g.Rate.To || w.Rate.Source != g.Rate.Source {
			out = append(out, fmt.Sprintf("%srate: %s, not %s", at, rateText(g.Rate), rateText(w.Rate)))
		}
		diff(at+"base", w.Base.String()+" "+w.Base.Currency().Code, g.Base.String()+" "+g.Base.Currency().Code)
		diff(at+"description", w.Description, g.Description)
	}
	return out
}

// rateText is a rate as the entry stores it: its value, the currencies it converts and its source. Rates compare by
// value, since a stored rate keeps the column's trailing zeros.
func rateText(r money.Rate) string {
	return fmt.Sprintf("%s %s→%s (%s)", r.Decimal().Text('f'), r.From.Code, r.To.Code, r.Source)
}
