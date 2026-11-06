package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/asset"
	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The stable error codes of an account merge (FR-COA-04, rev 0.174; ENG-23).
const (
	CodeMergeAccount   = "MERGE-01" // an account a merge does not take, or two that do not go together
	CodeMergeDate      = "MERGE-02" // a date in no open period, or before the account's last posting
	CodeMergeScheduled = "MERGE-03" // something scheduled still posts to the account
)

// AccountMerges merge an account into another once someone other than the one who asked approves it (FR-COA-04,
// Appendix B.8; PRD-001 rev 0.174): in the approval's unit of work, an account_merge document records what the account
// held, its entry account_merge@1 moves it into the other account, and the account becomes inactive for good, merged into
// the other. Earlier postings stay where they are.
type AccountMerges struct {
	// AfterPosting runs inside the unit of work after the entry is written and the account merged, when there was an entry.
	AfterPosting func(ctx context.Context, u storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// Merge is a merge as asked for: the account merged, the account it is merged into, and the day the balance moves.
type Merge struct {
	From, Into uuid.UUID
	Date       time.Time
}

// MergePlan is what a merge does now: the two accounts as they are, what the account merged holds and the entry that
// moves it, with no lines for an account holding nothing.
type MergePlan struct {
	Merge
	FromAccount, IntoAccount storage.Account
	Holdings                 []ledger.MergeHolding
	Entry                    ledger.Entry
}

// Memo is the merge's memo, as its document and entry carry it.
func (p MergePlan) Memo() string {
	return fmt.Sprintf("Merge %s %s into %s %s", p.FromAccount.Code, p.FromAccount.Name, p.IntoAccount.Code, p.IntoAccount.Name)
}

func mergeRefused(code, msg, fix string) error { return &Error{Code: code, Message: msg, Fix: fix} }

// Plan checks a merge and works out what it moves, writing nothing: what asking for its approval shows, and what its
// envelope depends on (FR-APR-05). The accounts must suit (FR-COA-04), the date fall in an open period on or after the
// account's last posting, and nothing scheduled still post to the account.
func (s AccountMerges) Plan(ctx context.Context, u storage.UnitOfWork, m Merge) (MergePlan, error) {
	plan := MergePlan{Merge: m}
	var err error
	if plan.FromAccount, err = u.Accounts().Get(ctx, m.From); err != nil {
		return plan, err
	}
	if plan.IntoAccount, err = u.Accounts().Get(ctx, m.Into); err != nil {
		return plan, err
	}
	if err := mergeable(plan.FromAccount, plan.IntoAccount); err != nil {
		return plan, err
	}
	from := plan.FromAccount
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return plan, err
	}
	day := time.Date(m.Date.Year(), m.Date.Month(), m.Date.Day(), 0, 0, 0, 0, time.UTC)
	i := slices.IndexFunc(periods, func(p storage.Period) bool { return !day.Before(p.StartsOn) && !day.After(p.EndsOn) })
	switch {
	case m.Date.IsZero():
		return plan, mergeRefused(CodeMergeDate, "the merge has no date", "Enter the day the balance moves, in an open period")
	case i < 0:
		return plan, mergeRefused(CodeMergeDate, fmt.Sprintf("no period covers %s", day.Format(time.DateOnly)), "Open the financial year that holds it, or choose another day")
	case periods[i].Status != storage.PeriodOpen:
		return plan, mergeRefused(CodeMergeDate, fmt.Sprintf("the period of %s is %s", day.Format(time.DateOnly), periods[i].Status), "Choose a day in an open period")
	}
	after, err := balance.LinesAfter(ctx, u, from.ID, day)
	if err != nil {
		return plan, err
	}
	if after {
		return plan, mergeRefused(CodeMergeDate, fmt.Sprintf("account %s has postings after %s", from.Code, day.Format(time.DateOnly)),
			"Choose the day of its last posting or later")
	}
	if waiting, err := scheduledTo(ctx, u, from, periods); err != nil {
		return plan, err
	} else if len(waiting) > 0 {
		return plan, mergeRefused(CodeMergeScheduled, fmt.Sprintf("account %s still has postings to come: %s", from.Code, strings.Join(waiting, "; ")),
			"Let them post, or change them to another account, then merge it")
	}

	o, err := u.Organisation(ctx)
	if err != nil {
		return plan, err
	}
	base, err := money.Lookup(o.BaseCurrency)
	if err != nil {
		return plan, err
	}
	// A balance-sheet account moves its whole balance; an income or expense account its movement in the financial year
	// (Appendix B.8), whose start the calendar knows.
	start := time.Time{}
	if from.Type == storage.AccountType("revenue") || from.Type == storage.AccountType("expense") {
		cal, err := period.CalendarOf(o.Profile)
		if err != nil {
			return plan, err
		}
		start = cal.Year(cal.Number(day)).StartsOn
	}
	var span []storage.Period
	for _, p := range periods {
		if !p.StartsOn.After(day) && !p.EndsOn.Before(start) {
			span = append(span, p)
		}
	}
	held, err := balance.Holdings(ctx, u, from.ID, span, base)
	if err != nil {
		return plan, err
	}
	for _, h := range held {
		plan.Holdings = append(plan.Holdings, ledger.MergeHolding{Amount: h.Amount, Base: h.Base, Values: h.Values})
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return plan, err
	}
	plan.Entry, err = ledger.PostAccountMerge(ledger.AccountMerge{Version: 1, Date: day, Memo: plan.Memo(), From: from.ID, Into: plan.IntoAccount.ID,
		Holdings: plan.Holdings}, settings)
	return plan, err
}

// mergeable refuses two accounts a merge does not take (FR-COA-04): the same one, one merged already, a system, group,
// money or control account, an inactive one, two of different types or currencies, or a revalued account into one that
// is not.
func mergeable(from, into storage.Account) error {
	refuse := func(msg, fix string) error { return mergeRefused(CodeMergeAccount, msg, fix) }
	switch {
	case from.ID == into.ID:
		return refuse("an account cannot be merged into itself", "Choose another account to merge it into")
	case from.MergedInto != nil:
		return refuse(fmt.Sprintf("account %s is merged already", from.Code), "Post to the account it was merged into")
	case into.MergedInto != nil || !into.Active:
		return refuse(fmt.Sprintf("account %s is inactive", into.Code), "Merge it into an active account")
	case !from.Active:
		return refuse(fmt.Sprintf("account %s is inactive", from.Code), "Reactivate it, so its balance can move, then merge it")
	}
	for _, a := range []storage.Account{from, into} {
		switch {
		case a.Role != "":
			return refuse(fmt.Sprintf("account %s is a system account (%s)", a.Code, a.Role), "Merge an account that has no system role")
		case a.Group:
			return refuse(fmt.Sprintf("account %s is a group heading", a.Code), "Merge an account under it")
		case a.MoneyAccount:
			return refuse(fmt.Sprintf("account %s is a money account", a.Code), "Move a money account's balance with a transfer, then deactivate it")
		case a.Control:
			return refuse(fmt.Sprintf("account %s is a control account", a.Code), "Only its sub-ledger posts to it: deactivate it instead")
		}
	}
	switch {
	case from.Type != into.Type:
		return refuse(fmt.Sprintf("account %s is %s and account %s is %s", from.Code, from.Type, into.Code, into.Type), "Merge it into an account of the same type")
	case from.Currency != into.Currency:
		return refuse(fmt.Sprintf("account %s is held in %s and account %s in %s", from.Code, held(from.Currency), into.Code, held(into.Currency)),
			"Merge it into an account held in the same currency")
	case from.Revalue && !into.Revalue:
		return refuse(fmt.Sprintf("account %s is revalued at each month's end and account %s is not", from.Code, into.Code),
			"Mark the other account revalue first, or merge it into one that is")
	}
	return nil
}

func held(currency string) string {
	if currency == "" {
		return "any currency"
	}
	return currency
}

// scheduledTo names what still posts to an account by itself: a spread's months to release to it, an asset whose cost
// is on it, or a journal's automatic reversal still to post with a line on it. The asset register's depreciation
// accounts are system accounts, which no merge takes.
func scheduledTo(ctx context.Context, u storage.UnitOfWork, acc storage.Account, periods []storage.Period) ([]string, error) {
	var out []string
	last := time.Time{}
	for _, p := range periods {
		if p.EndsOn.After(last) {
			last = p.EndsOn
		}
	}
	if !last.IsZero() {
		d, err := DeferralsAt(ctx, u, last)
		if err != nil {
			return nil, err
		}
		for _, x := range d.Schedules {
			if x.AccountID == acc.ID && !x.Remaining.IsZero() {
				out = append(out, fmt.Sprintf("%s line %d releases %s to it", nameOf(x.Document), x.Line, x.Remaining))
			}
		}
	}
	f := storage.DocumentFilter{Type: storage.DocumentFixedAsset, Status: storage.DocumentPosted, Limit: 500}
	for {
		docs, err := u.Documents().Page(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			a, err := asset.Read(doc)
			if err != nil {
				return nil, err
			}
			if a.AccountID == acc.ID && !a.Disposed {
				out = append(out, fmt.Sprintf("asset %s (%s) is held on it", nameOf(doc), a.Name))
			}
		}
		if len(docs) < f.Limit {
			break
		}
		f.AfterDate, f.AfterID = docs[len(docs)-1].Date, &docs[len(docs)-1].ID
	}
	due, err := u.Documents().DueReversals(ctx, "9999-12-31", 1000)
	if err != nil {
		return nil, err
	}
	for _, id := range due {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return nil, err
		}
		saved, err := ReadSaved(doc)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(saved.Lines, func(l SavedLine) bool { return l.AccountID == acc.ID }) {
			out = append(out, fmt.Sprintf("journal %s reverses on %s", nameOf(doc), saved.ReverseOn))
		}
	}
	return out, nil
}

func nameOf(doc storage.Document) string {
	if doc.Number != "" {
		return doc.Number
	}
	return doc.ID.String()
}

// mergePayload is an account_merge document's content: the merge and what the account held at its date.
type mergePayload struct {
	FromAccountID uuid.UUID       `json:"from_account_id"`
	IntoAccountID uuid.UUID       `json:"into_account_id"`
	Date          string          `json:"date"`
	Memo          string          `json:"memo"`
	Holdings      []mergedHolding `json:"holdings"`
}

type mergedHolding struct {
	Currency          string      `json:"currency"`
	Amount            string      `json:"amount"`
	Base              string      `json:"base"`
	DimensionValueIDs []uuid.UUID `json:"dimension_value_ids,omitempty"`
}

// PostIn posts a merge in u, the approval's unit of work, under the ledger lock (ENG-47): it plans the merge again, records
// it as an account_merge document, posts its entry when the account holds anything, and merges the account, inactive
// for good. It returns the document and the entry, nil for an account that held nothing.
func (s AccountMerges) PostIn(ctx context.Context, u storage.UnitOfWork, m Merge) (storage.Document, *storage.Entry, error) {
	if _, err := u.Ledger().Lock(ctx); err != nil {
		return storage.Document{}, nil, err
	}
	plan, err := s.Plan(ctx, u, m)
	if err != nil {
		return storage.Document{}, nil, err
	}
	p := mergePayload{FromAccountID: plan.FromAccount.ID, IntoAccountID: plan.IntoAccount.ID, Date: plan.Entry.PostingDate.Format(time.DateOnly),
		Memo: plan.Memo(), Holdings: []mergedHolding{}}
	for _, h := range plan.Holdings {
		p.Holdings = append(p.Holdings, mergedHolding{Currency: h.Amount.Currency().Code, Amount: h.Amount.String(), Base: h.Base.String(),
			DimensionValueIDs: h.Values})
	}
	b, err := json.Marshal(p)
	if err != nil {
		return storage.Document{}, nil, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentAccountMerge, Payload: b})
	if err != nil {
		return doc, nil, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return doc, nil, err
	}
	merge, err := readMerge(doc, settings.Base)
	if err != nil {
		return doc, nil, err
	}
	entry, err := ledger.PostAccountMerge(merge, settings)
	if err != nil {
		return doc, nil, err
	}
	var stored *storage.Entry
	if len(entry.Lines) > 0 {
		e, err := u.Ledger().Append(ctx, toStorage(entry, nil))
		if err != nil {
			return doc, nil, err
		}
		stored = &e
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
		return doc, nil, err
	}
	doc.Status = storage.DocumentPosted
	from := plan.FromAccount
	if _, err := u.Accounts().Update(ctx, from.ID, from.Version, storage.AccountChange{Name: from.Name, ParentID: from.ParentID, Group: from.Group,
		Active: false, MergedInto: &plan.IntoAccount.ID, AccountAttributes: from.AccountAttributes}); err != nil {
		return doc, nil, err
	}
	if s.AfterPosting == nil || stored == nil {
		return doc, stored, nil
	}
	return doc, stored, s.AfterPosting(ctx, u, doc, *stored)
}

// readMerge reads an account_merge document as the rule takes it.
func readMerge(doc storage.Document, base money.Currency) (ledger.AccountMerge, error) {
	var p mergePayload
	if err := json.Unmarshal(doc.Payload, &p); err != nil {
		return ledger.AccountMerge{}, fmt.Errorf("journal: account merge %s: %w", doc.ID, err)
	}
	day, err := time.Parse(time.DateOnly, p.Date)
	if err != nil {
		return ledger.AccountMerge{}, fmt.Errorf("journal: account merge %s: %w", doc.ID, err)
	}
	m := ledger.AccountMerge{ID: doc.ID, Version: doc.Version, Date: day, Memo: p.Memo, From: p.FromAccountID, Into: p.IntoAccountID}
	for _, h := range p.Holdings {
		cur, err := money.Lookup(h.Currency)
		if err != nil {
			return m, err
		}
		amount, err := money.ParseStored(h.Amount, cur)
		if err != nil {
			return m, err
		}
		value, err := money.ParseStored(h.Base, base)
		if err != nil {
			return m, err
		}
		m.Holdings = append(m.Holdings, ledger.MergeHolding{Amount: amount, Base: value, Values: h.DimensionValueIDs})
	}
	return m, nil
}

// replayAccountMerge posts a merge again as its entry posted it, from what its document kept (ENG-06).
func replayAccountMerge(doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	m, err := readMerge(doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayAccountMerge(e.RuleVersion, m, s)
}
