package checks

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/deadlines"
	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The audit object types whose change reruns the evidence check (design D5).
var onEvidence = []string{"document", "journal_entry", "evidence", "organisation_setting", "period", "period_close", "period_reopen"}

// The checks of the evidence rules (FR-DOC-06, rev 0.182; moved from p1-checks by rev 0.166's rule).
var evidenceChecks = []Definition{
	{Key: "evidence.missing", Ref: "C.2 #30", Area: "Evidence", Worst: Warning, Title: "Posted records have their evidence",
		Explain: "A bill rests on the supplier's invoice or receipt, a manual journal above the evidence threshold on a supporting " +
			"calculation or document, and a filing recorded as done on its acknowledgement (FR-DOC-06). Each posted record of an open month whose evidence is required or recommended, " +
			"with no file attached, is listed: one posted while its evidence was only recommended, or before the rule. Attach the file " +
			"in the record's Evidence panel.",
		Rules: "FR-DOC-06", Owner: "accountant", View: viewJournals, Triggers: onEvidence, Run: missingEvidence},
}

// C.2 #30: the open months' posted bills, and manual journals whose debits in the base currency are above the
// threshold, with no file attached while their evidence is not optional.
func missingEvidence(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	from, to, ok := e.openSpan()
	if !ok {
		return Fine("Every month is closed: their evidence was looked at the close"), nil
	}
	changes, err := u.Settings().Changes(ctx, "")
	if err != nil {
		return Result{}, err
	}
	value := func(key string) string {
		d, _ := settings.Lookup(key)
		return settings.On(d, changes, e.Today).Value
	}
	missing := func(docs []storage.Document) ([]storage.Document, error) {
		ids := make([]uuid.UUID, len(docs))
		for i, d := range docs {
			ids[i] = d.ID
		}
		counts, err := u.Evidence().Counts(ctx, ids)
		if err != nil {
			return nil, err
		}
		return slices.DeleteFunc(slices.Clone(docs), func(d storage.Document) bool { return counts[d.ID] > 0 }), nil
	}
	var items []Item
	if value(evidence.Bill.Setting) != evidence.LevelOptional {
		bills, err := documents(ctx, u, []storage.DocumentType{storage.DocumentBill}, storage.DocumentPosted, from, to)
		if err != nil {
			return Result{}, err
		}
		bare, err := missing(bills)
		if err != nil {
			return Result{}, err
		}
		for _, d := range bare {
			items = append(items, docItem("bill", d, "", "No "+evidence.Bill.Expected+" attached"))
		}
	}
	if value(evidence.ManualJournal.Setting) != evidence.LevelOptional {
		threshold, err := money.Parse(value(evidence.JournalThreshold), e.Base)
		if err != nil {
			return Result{}, err
		}
		journals, err := documents(ctx, u, []storage.DocumentType{storage.DocumentJournal}, storage.DocumentPosted, from, to)
		if err != nil {
			return Result{}, err
		}
		bare, err := missing(journals)
		if err != nil {
			return Result{}, err
		}
		for _, d := range bare {
			debits, err := postedDebits(ctx, u, d.ID, e.Base)
			if err != nil {
				return Result{}, err
			}
			if n, err := debits.Cmp(threshold); err != nil || n <= 0 {
				if err != nil {
					return Result{}, err
				}
				continue
			}
			items = append(items, docItem("journal", d, "", fmt.Sprintf("Debits of %s %s and no %s attached", debits, e.Base.Code, evidence.ManualJournal.Expected)))
		}
	}
	if value(evidence.StatutoryFiling.Setting) != evidence.LevelOptional {
		filings, err := documents(ctx, u, []storage.DocumentType{storage.DocumentStatutoryFiling}, storage.DocumentPosted, from, to)
		if err != nil {
			return Result{}, err
		}
		bare, err := missing(filings)
		if err != nil {
			return Result{}, err
		}
		for _, d := range bare {
			f, err := deadlines.ReadFiling(d)
			if err != nil {
				return Result{}, err
			}
			if f.Outcome != deadlines.OutcomeFiled {
				continue // not required, or a withdrawal: its reason is its evidence
			}
			items = append(items, Item{Kind: "statutory_filing", ID: d.ID.String(), Label: f.Key,
				Detail: "Recorded as filed with no " + evidence.StatutoryFiling.Expected + " attached: withdraw it and record it again with the file"})
		}
	}
	if len(items) == 0 {
		return Fine("Every posted bill, every manual journal above the evidence threshold, and every filing recorded, of the open months, has its evidence"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d posted record(s) without their evidence", len(items)),
		"Attach each one's file in its Evidence panel"), nil
}

// postedDebits are a posted journal's debits in the base currency, as its latest entry posted them: the one no later
// entry reverses.
func postedDebits(ctx context.Context, u storage.UnitOfWork, doc uuid.UUID, base money.Currency) (money.Money, error) {
	entries, err := u.Ledger().OfSource(ctx, doc)
	if err != nil {
		return money.Money{}, err
	}
	reversed := map[uuid.UUID]bool{}
	for _, en := range entries {
		if en.ReversesEntryID != nil {
			reversed[*en.ReversesEntryID] = true
		}
	}
	out := money.Zero(base)
	for _, en := range entries {
		if en.ReversesEntryID != nil || reversed[en.ID] {
			continue
		}
		for _, l := range en.Lines {
			v, err := money.ParseStored(string(l.BaseAmount), base)
			if err != nil {
				return money.Money{}, err
			}
			if v.Sign() > 0 {
				if out, err = out.Add(v); err != nil {
					return money.Money{}, err
				}
			}
		}
	}
	return out, nil
}
