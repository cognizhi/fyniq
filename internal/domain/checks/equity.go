package checks

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D row whose view opens the equity register's items.
const viewEquity = "Cap table, holders, investors, equity transactions (rev 0.3)"

// The checks of the equity register (p1-equity-register).
var equityChecks = []Definition{
	{Key: "equity.instruments-converted", Ref: "C.2 #29", Area: "Equity", Worst: Warning, Title: "Every SAFE and note converts when it should",
		Explain: "A SAFE or note converts into shares at a priced round, and a note falls due at its maturity. A recorded one " +
			"whose maturity has passed, or that shares allotted for cash after it was signed have not converted, is for review: " +
			"draft its conversion from the round, or settle the note under its terms.",
		Rules: "FR-EQ-18", Owner: "accountant", View: viewEquity, Triggers: []string{"document", "share_issue", "holding_movement"}, Run: instrumentsConverted},
}

// C.2 #29: each recorded instrument no conversion still stands for, whose maturity has passed, or which converts at a
// priced round while shares were allotted for cash on or after the day it was signed (the register of share issues).
func instrumentsConverted(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	docs, err := equity.Instruments(ctx, u)
	if err != nil {
		return Result{}, err
	}
	standing, err := equity.StandingConversions(ctx, u)
	if err != nil {
		return Result{}, err
	}
	issues, err := u.ShareIssues().List(ctx)
	if err != nil {
		return Result{}, err
	}
	var rounds []storage.ShareIssue // the allotments for cash to today, latest first
	for _, s := range issues {
		if s.Consideration == storage.ConsiderationCash && !s.EffectiveOn.After(e.Today) {
			rounds = append(rounds, s)
		}
	}
	sort.Slice(rounds, func(a, b int) bool { return rounds[a].EffectiveOn.After(rounds[b].EffectiveOn) })
	var items []Item
	for _, d := range docs {
		if d.Status != storage.DocumentPosted {
			continue
		}
		if _, ok := standing[d.ID]; ok {
			continue
		}
		i, err := equity.ReadInstrument(d)
		if err != nil {
			return Result{}, err
		}
		c, err := u.Contacts().Get(ctx, i.HolderID)
		if err != nil {
			return Result{}, err
		}
		label := c.LegalName + ", " + formName[i.Form]
		if i.MaturityOn != nil && i.MaturityOn.Before(e.Today) {
			items = append(items, Item{Kind: "document", ID: d.ID.String() + "/maturity", Label: label,
				Detail: fmt.Sprintf("Past its maturity on %s. Convert it under its terms, or settle it with its holder", day(*i.MaturityOn)),
				Version: i.MaturityOn.Format(time.DateOnly)})
		}
		if i.Trigger != "priced_round" {
			continue
		}
		for _, s := range rounds {
			if !s.EffectiveOn.Before(i.SignedOn) {
				items = append(items, Item{Kind: "document", ID: d.ID.String() + "/round", Label: label,
					Detail: fmt.Sprintf("Shares were allotted for cash on %s, after it was signed on %s, and it has not converted. Draft its "+
						"conversion from the round's page, or note why that allotment was not its priced round", day(s.EffectiveOn), day(i.SignedOn)),
					Version: s.ID.String()})
				break
			}
		}
	}
	if len(items) == 0 {
		return Fine("Every recorded SAFE and note is converting, or waits for its round or maturity"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d SAFE(s) or note(s) to convert or settle", len(items)),
		"Draft the conversions from the priced round, or settle a note that has matured"), nil
}

// formName is an instrument's form in a check's words.
var formName = map[string]string{
	equity.FormPostMoneySAFE: "post-money SAFE", equity.FormPreMoneySAFE: "pre-money SAFE", equity.FormDiscountSAFE: "discount-only SAFE",
	equity.FormMFNSAFE: "MFN SAFE", equity.FormConvertibleNote: "convertible note",
}
