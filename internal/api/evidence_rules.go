package api

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/evidence"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ evidence rules (FR-DOC-06, rev 0.182)
//
// Each activity's evidence is required, recommended or optional, the organisation's setting in force today. A
// required file missing refuses the post, its request for approval and the approved post itself (DOC-10); a
// recommended one missing shows on Checks (C.2 #30).

// settingToday is a setting's value in force on the organisation's today.
func settingToday(ctx context.Context, a *API, u storage.UnitOfWork, key string) (string, error) {
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return "", err
	}
	d, _ := settings.Lookup(key)
	changes, err := u.Settings().Changes(ctx, key)
	if err != nil {
		return "", err
	}
	return settings.On(d, changes, today).Value, nil
}

// requireEvidence refuses completing doc, a record of act, while its evidence is required and no file is attached.
func (a *API) requireEvidence(ctx context.Context, u storage.UnitOfWork, act evidence.Activity, doc uuid.UUID) error {
	level, err := settingToday(ctx, a, u, act.Setting)
	if err != nil {
		return err
	}
	files, err := u.Evidence().Counts(ctx, []uuid.UUID{doc})
	if err != nil {
		return err
	}
	if blocks, _ := evidence.Needs(level, files[doc]); blocks {
		return problem(evidence.CodeMissing, fmt.Sprintf("%s posts only with its evidence attached: %s (FR-DOC-06).", sentenceStart(act.Label), act.Expected),
			"Attach it in the Evidence panel, then post it again")
	}
	return nil
}

// requireJournalEvidence is requireEvidence for a manual journal whose debits in the base currency are above the
// evidence threshold; one at or below it needs none.
func (a *API) requireJournalEvidence(ctx context.Context, u storage.UnitOfWork, doc uuid.UUID, debits money.Money) error {
	raw, err := settingToday(ctx, a, u, evidence.JournalThreshold)
	if err != nil {
		return err
	}
	threshold, err := money.Parse(raw, debits.Currency())
	if err != nil {
		return err
	}
	if n, err := debits.Cmp(threshold); err != nil || n <= 0 {
		return err
	}
	return a.requireEvidence(ctx, u, evidence.ManualJournal, doc)
}

// sentenceStart capitalises the first letter of s.
func sentenceStart(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
}
