package journal

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Claims runs the expense claim commands (FR-AP-07, FR-AP-17; p1-expense-claims) against a store: each in one unit of
// work, the document and its entry together (ENG-02), the entry from the rule expense_claim@1 (ENG-01). A claim is
// drafted and revised by its claimant, and posted when another person approves it; a returned claim stays a draft for
// its claimant to fix, and every version it had stays in its history (FR-GL-12).
type Claims struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// CheckedClaim is a claim version as posting it would post it, written nowhere: what the rule reads, the entry, and the
// problems that stop it posting (✖). Owed is what the company would owe the claimant in the base currency, as the entry
// credits it: what its approval is judged on; zero when it would not post.
type CheckedClaim struct {
	Prepared claim.Prepared
	Entry    ledger.Entry
	Problems ledger.Problems
	Owed     money.Money
}

func checkClaim(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (CheckedClaim, error) {
	p, probs, err := claim.Prepare(ctx, u, doc, s.Base)
	if err != nil {
		return CheckedClaim{}, err
	}
	out := CheckedClaim{Prepared: p, Problems: probs, Owed: money.Zero(s.Base)}
	entry, err := ledger.PostExpenseClaim(p.Claim, s)
	if ps, ok := err.(ledger.Problems); ok {
		for _, x := range ps {
			if x.Code == ledger.CodeMissingRate && probs.Has(fx.CodeNoRate) {
				continue // the cascade found no rate: FX-01 says so, in place of the engine's LEDGER-07
			}
			if x.Code == ledger.CodeUnknownAccount && probs.Has(claim.CodeCategory) {
				continue // the line's category could not be read: CLAIM-05 says so, in place of the engine's LEDGER-04
			}
			out.Problems = append(out.Problems, x)
		}
	} else if err != nil {
		return CheckedClaim{}, err
	}
	if len(out.Problems) == 0 {
		out.Entry = entry
		for _, l := range entry.Lines {
			if s.Accounts[l.AccountID].Role == ledger.RoleExpenseClaimsPayable || l.AccountID == p.Claim.Advances && p.Claim.Advances != uuid.Nil {
				out.Owed, _ = out.Owed.Add(l.Base.Neg())
			}
		}
	}
	return out, nil
}

func isClaim(doc storage.Document) error {
	if !claim.IsClaim(doc) {
		return &claim.Error{Code: claim.CodeNotClaim, Message: fmt.Sprintf("document %s is a %s, not an expense claim", doc.ID, doc.Type),
			Fix: "Open it with its own module"}
	}
	return nil
}

func claimDraft(doc storage.Document, what string) error {
	if doc.Status != storage.DocumentDraft {
		return &claim.Error{Code: claim.CodeNotDraft, Message: fmt.Sprintf("the claim is %s, not a draft", doc.Status), Fix: what}
	}
	return nil
}

// Create saves a draft claim (✎), numbered now unless its sequence is gap-free. It is checked when it posts; Check shows
// what posting it would do.
func (s Claims) Create(ctx context.Context, org uuid.UUID, d claim.Draft) (storage.Document, error) {
	b, err := claim.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var doc storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err = createNumbered(ctx, u, storage.DocumentExpenseClaim, b)
		return err
	})
	return doc, err
}

// Revise saves a draft claim's new content as its next version, recording who saved it (FR-GL-01's preparer); a claim
// returned to its claimant is revised this way, its earlier versions kept (UC-EXP-04).
func (s Claims) Revise(ctx context.Context, org, id uuid.UUID, d claim.Draft) (storage.Document, error) {
	b, err := claim.Encode(d)
	if err != nil {
		return storage.Document{}, err
	}
	var out storage.Document
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1
		if err != nil {
			return err
		}
		if err := isClaim(doc); err != nil {
			return err
		}
		if err := claimDraft(doc, "Only a draft changes"); err != nil {
			return err
		}
		out, err = u.Documents().Revise(ctx, id, b)
		return err
	})
	return out, err
}

// CheckClaim runs the rule over doc, a version of a claim, in u, and writes nothing: what Check answers, inside a unit of
// work already open.
func CheckClaim(ctx context.Context, u storage.UnitOfWork, doc storage.Document) (CheckedClaim, error) {
	if err := isClaim(doc); err != nil {
		return CheckedClaim{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return CheckedClaim{}, err
	}
	return checkClaim(ctx, u, doc, settings)
}

// Check runs the rule over a claim's current version and writes nothing: the ledger impact a person sees before
// submitting or approving it, the problems that would stop it, and what the company would owe.
func (s Claims) Check(ctx context.Context, org, id uuid.UUID) (CheckedClaim, error) {
	var out CheckedClaim
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Get(ctx, id)
		if err != nil {
			return err
		}
		out, err = CheckClaim(ctx, u, doc)
		return err
	})
	return out, err
}

// Post posts a draft claim: its number when its sequence is gap-free, then the entry of its current version, with the
// document's change of status, in one transaction. A claim the rule refuses posts nothing and answers every problem.
// The approval engine posts a claim when its last step is approved (p1-expense-claims design D3).
func (s Claims) Post(ctx context.Context, org, id uuid.UUID) (storage.Entry, error) {
	var out storage.Entry
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id) // ENG-47 step 1
		if err != nil {
			return err
		}
		if err := isClaim(doc); err != nil {
			return err
		}
		if err := claimDraft(doc, "Only a draft is posted"); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		c, err := checkClaim(ctx, u, doc, settings)
		if err != nil {
			return err
		}
		if len(c.Problems) > 0 {
			return c.Problems
		}
		if doc.Number == "" {
			if doc.Number, err = u.Sequences().Number(ctx, id, doc.Type); err != nil {
				return err
			}
		}
		if out, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		if s.AfterPosting == nil {
			return nil
		}
		return s.AfterPosting(ctx, u, doc, out)
	})
	return out, err
}

// replayExpenseClaim posts a claim version again as its entry posted it (ENG-06): each line's account, rate and whether
// its GST was claimed as the entry kept them, so a category moved to another account since, or a rate added to the
// cascade since, does not change what the claim posted.
func replayExpenseClaim(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	p, _, err := claim.Prepare(ctx, u, doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	c := p.Claim
	c.Memo = stored.Memo
	k := 0 // the next stored line that is a claim line's cost
	for i := range c.Lines {
		l := &c.Lines[i]
		net, _ := l.Amount.Sub(l.GST)
		if k >= len(stored.Lines) {
			break
		}
		x := stored.Lines[k]
		switch {
		case x.Amount.Equal(l.Amount) && !l.GST.IsZero():
			l.Claimable = false
		case x.Amount.Equal(net):
			l.Claimable = !l.GST.IsZero()
		case net.IsZero():
			l.Claimable = true // a claimable line of GST alone posts no cost
			continue
		default:
			continue
		}
		l.AccountID = x.AccountID
		if l.Amount.Currency() != s.Base && p.Draft.Lines[i].Rate == nil {
			r := x.Rate // the cascade's rate as the entry kept it
			l.Rate = &r
		}
		k++
	}
	return ledger.ReplayExpenseClaim(e.RuleVersion, c, s)
}
