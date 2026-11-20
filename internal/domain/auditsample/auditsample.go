// Package auditsample draws an auditor's sample from posted ledger lines by a rule, reproducibly (PRD-001 FR-UX-42; p2-reporting-planning design D13).
//
// The draw is a pure function of the lines, the rule and a stored seed: every qualifying line is ranked by the SHA-256 of the seed, its entry's ID and its line
// number, and the first Count are the sample. The same seed over the same ledger gives the same sample, so an auditor (or anyone) can re-draw it and see that
// nothing was picked by hand. The ledger is only read; a sample posts nothing.
package auditsample

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/cockroachdb/apd/v3"
)

// MaxCount is the most lines one sample holds.
const MaxCount = 200

// Rule is the auditor's rule: a period, an account (by code; empty for any), a minimum size of a line (its absolute base amount) and how many lines.
type Rule struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Account   string `json:"account,omitempty"`
	MinAmount string `json:"min_amount,omitempty"`
	Count     int    `json:"count"`
}

// Line is a posted line a rule may pick.
type Line struct {
	EntryID     string `json:"entry_id"`
	LineNo      int    `json:"line_no"`
	PostingDate string `json:"posting_date"`
	Account     string `json:"account"`
	AccountName string `json:"account_name"`
	BaseAmount  string `json:"base_amount"`
	Memo        string `json:"memo,omitempty"`
	SourceType  string `json:"source_type"`
	SourceID    string `json:"source_id,omitempty"`
}

var ctx = apd.BaseContext.WithPrecision(34)

// Validate is why a rule cannot be drawn, or nil: a count of 1 to MaxCount and a minimum that is a decimal of zero or more.
func (r Rule) Validate() error {
	if r.Count < 1 || r.Count > MaxCount {
		return fmt.Errorf("a sample holds 1 to %d lines", MaxCount)
	}
	if r.MinAmount != "" {
		if min, _, err := apd.NewFromString(r.MinAmount); err != nil || min.Form != apd.Finite || min.Sign() < 0 {
			return fmt.Errorf("the minimum amount %q is not a decimal of zero or more", r.MinAmount)
		}
	}
	return nil
}

// Qualifies reports whether a line falls under the rule: dated from..to (inclusive, ISO dates compare as text), on the account when the rule names one, and of at
// least the minimum size, taking the line's absolute amount.
func (r Rule) Qualifies(l Line) (bool, error) {
	if l.PostingDate < r.From || l.PostingDate > r.To || (r.Account != "" && l.Account != r.Account) {
		return false, nil
	}
	if r.MinAmount == "" {
		return true, nil
	}
	min, _, err := apd.NewFromString(r.MinAmount)
	if err != nil || min.Form != apd.Finite || min.Sign() < 0 {
		return false, fmt.Errorf("the minimum amount %q is not a decimal of zero or more", r.MinAmount)
	}
	amt, _, err := apd.NewFromString(l.BaseAmount)
	if err != nil || amt.Form != apd.Finite {
		return false, fmt.Errorf("the line's amount %q is not a decimal", l.BaseAmount)
	}
	abs := new(apd.Decimal)
	abs.Abs(amt)
	return abs.Cmp(min) >= 0, nil
}

func rank(seed string, l Line) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", seed, l.EntryID, l.LineNo)))
	return hex.EncodeToString(h[:])
}

// Draw is the population (how many lines qualify) and the sample: the Count qualifying lines with the lowest rank for the seed, ranked order.
func Draw(rule Rule, seed string, lines []Line) (int, []Line, error) {
	if err := rule.Validate(); err != nil {
		return 0, nil, err
	}
	if seed == "" {
		return 0, nil, fmt.Errorf("a sample needs a seed")
	}
	type ranked struct {
		key  string
		line Line
	}
	var all []ranked
	for _, l := range lines {
		ok, err := rule.Qualifies(l)
		if err != nil {
			return 0, nil, err
		}
		if ok {
			all = append(all, ranked{rank(seed, l), l})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].key < all[j].key })
	n := min(rule.Count, len(all))
	out := make([]Line, 0, n)
	for _, x := range all[:n] {
		out = append(out, x.line)
	}
	return len(all), out, nil
}
