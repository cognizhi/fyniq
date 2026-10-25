package banking

import (
	"context"
	"slices"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RepeatedCodings is how many lines coded the same way make a pattern offered as a rule to promote (FR-BNK-14: "default
// 5"; Xero calls it memory). The offer is never applied by itself: a person saves it as a rule, and an approver may
// later make that rule apply by itself.
const RepeatedCodings = 5

// Offer is a rule offered from lines coded the same way: its conditions and action, and the lines it was seen on.
type Offer struct {
	When  Conditions
	Then  Action
	Lines []storage.BankLine
}

// pattern is what makes two codings the same: the counterparty, or the description's leading words, with the account
// and GST code it went to and its direction.
type pattern struct {
	party, words, gst string
	account           uuid.UUID
	in                bool
}

// leadingWords are a description's first two words, folded, before any word with a digit in it: what stays the same
// from one line to the next ("GRAB*TRIP 1234" is "grab*trip").
func leadingWords(desc string) string {
	var out []string
	for _, w := range strings.Fields(fold(desc)) {
		if strings.IndexFunc(w, unicode.IsDigit) >= 0 || len(out) == 2 {
			break
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

// Repeated are the patterns the organisation has coded the same way at least times (RepeatedCodings when zero) that no
// rule covers yet, most frequent first, read from the categorised lines and the bank transactions they posted: a line
// of one part, of either direction. Nothing is written.
func Repeated(ctx context.Context, u storage.UnitOfWork, rules []Rule, times int) ([]Offer, error) {
	if times <= 0 {
		times = RepeatedCodings
	}
	lines, err := u.Banking().Lines(ctx, storage.BankLineFilter{Statuses: []string{storage.LineCategorised}})
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
	seen := map[pattern][]storage.BankLine{}
	var order []pattern
	for _, l := range lines {
		if _, covered := First(rules, l); covered {
			continue
		}
		matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: &l.ID})
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(matches, func(m storage.BankMatch) bool { return m.Kind == storage.MatchCategorised && m.DocumentID != nil })
		if i < 0 {
			continue
		}
		doc, err := u.Documents().Get(ctx, *matches[i].DocumentID)
		if err != nil {
			return nil, err
		}
		saved, err := banktransaction.Read(doc, base)
		if err != nil || len(saved.Parts) != 1 {
			continue // a split line is not one coding
		}
		p := pattern{party: fold(l.Counterparty), gst: saved.Parts[0].GSTCode, account: saved.Parts[0].AccountID, in: saved.Amount.Sign() > 0}
		if p.party == "" {
			if p.words = leadingWords(l.Description); p.words == "" {
				continue // nothing that stays the same to recognise it by
			}
		}
		if _, ok := seen[p]; !ok {
			order = append(order, p)
		}
		seen[p] = append(seen[p], l)
	}
	var out []Offer
	for _, p := range order {
		ls := seen[p]
		if len(ls) < times {
			continue
		}
		o := Offer{Then: Action{AccountID: p.account, GSTCode: p.gst}, Lines: ls}
		o.When.Direction = DirectionOut
		if p.in {
			o.When.Direction = DirectionIn
		}
		if p.party != "" {
			o.When.CounterpartyContains = p.party
		} else {
			o.When.DescriptionContains = p.words
		}
		out = append(out, o)
	}
	slices.SortStableFunc(out, func(a, b Offer) int { return len(b.Lines) - len(a.Lines) })
	return out, nil
}
