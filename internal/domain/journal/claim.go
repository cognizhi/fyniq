package journal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
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
	entry, err := ledger.PostExpenseClaim(p.Claim, s)
	if err == nil && p.Advance != nil { // what it settles of the advance: the least of what it owes and what is open (FR-AP-17)
		owed := money.Zero(s.Base)
		for _, l := range entry.Lines {
			if s.Accounts[l.AccountID].Role == ledger.RoleExpenseClaimsPayable {
				owed, _ = owed.Add(l.Base.Neg())
			}
		}
		if p.Claim.Settles = p.Settle(owed); p.Claim.Settles != nil {
			entry, err = ledger.PostExpenseClaim(p.Claim, s)
		}
	}
	out := CheckedClaim{Prepared: p, Problems: probs, Owed: money.Zero(s.Base)}
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
		if d, err := claim.Read(doc, settings.Base); err == nil && d.AdvanceID != nil {
			// The advance it settles, locked so two claims or a refund of it wait for each other (ENG-47).
			if _, err := u.Documents().Lock(ctx, *d.AdvanceID); err != nil && !errors.Is(err, ports.ErrNotFound) {
				return err
			}
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
		if x := c.Prepared.Claim.Settles; x != nil && !x.IsZero() { // what it settles of the advance
			if _, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: *c.Prepared.Draft.AdvanceID,
				On: c.Prepared.Draft.Date, Currency: x.Currency().Code, Amount: storage.Decimal(x.String()), BaseCurrency: settings.Base.Code,
				BaseAmount: storage.Decimal(x.String()), RealisedFX: "0"}); err != nil {
				return err
			}
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
// cascade since, does not change what the claim posted; what it settled of an advance as its allocation kept it, to
// the advances account the entry credited.
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
	c.Settles, c.Advances = nil, uuid.Nil
	if p.Draft.AdvanceID != nil {
		allocs, err := u.Allocations().OfSource(ctx, doc.ID)
		if err != nil {
			return ledger.Entry{}, err
		}
		for _, a := range allocs {
			if a.ReversesID != nil || a.DocumentID != *p.Draft.AdvanceID || a.SourceVersion != doc.Version {
				continue
			}
			x, err := money.ParseStored(string(a.Amount), s.Base)
			if err != nil {
				return ledger.Entry{}, err
			}
			c.Settles = &x
			for _, l := range stored.Lines {
				role := s.Accounts[l.AccountID].Role
				if l.Base.Equal(x.Neg()) && role != ledger.RoleExpenseClaimsPayable && role != ledger.RoleFXRounding {
					c.Advances = l.AccountID
				}
			}
		}
	}
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

// ClaimReimbursements records money paid back to claimants for their claims, by hand until payment batches (FR-AP-07;
// the owner's decisions of 26 Oct 2026, rev 0.147 and 0.148): each in one unit of work, the document, its entry from
// claim_reimbursement@1 and an allocation for each claim it settles together (ENG-02). It is undone by a void, a
// reversing entry with reversing allocations, which reopens the claims (FR-GL-12).
type ClaimReimbursements struct {
	Store storage.Store
	// AfterPosting runs inside the unit of work after an entry (or its reversal) is written and the document updated.
	AfterPosting func(ctx context.Context, uow storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// CheckedReimbursement is a reimbursement as recording it would post it, written nowhere.
type CheckedReimbursement struct {
	Prepared    claim.PreparedReimbursement
	Entry       ledger.Entry
	Settlements []ledger.Settlement
	Problems    ledger.Problems
}

// RecordedReimbursement is a recorded reimbursement, or its void: its document, the entry it posted, and its allocations.
type RecordedReimbursement struct {
	Reimbursement storage.Document
	Entry         storage.Entry
	Allocations   []storage.Allocation
}

func checkReimbursement(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings) (CheckedReimbursement, error) {
	p, probs, err := claim.PrepareReimbursement(ctx, u, doc, s.Base)
	if err != nil {
		return CheckedReimbursement{}, err
	}
	out := CheckedReimbursement{Prepared: p, Problems: probs}
	entry, settled, err := ledger.PostClaimReimbursement(p.Rule, s)
	if ps, ok := err.(ledger.Problems); ok {
		out.Problems = append(out.Problems, ps...)
	} else if err != nil {
		return CheckedReimbursement{}, err
	}
	if len(out.Problems) == 0 {
		out.Entry, out.Settlements = entry, settled
	}
	return out, nil
}

// lockClaims locks the claims a reimbursement settles, in the order of their IDs, so two reimbursements of one claim
// wait for each other (ENG-47).
func lockClaims(ctx context.Context, u storage.UnitOfWork, r claim.Reimbursement) error {
	ids := make([]uuid.UUID, 0, len(r.Claims))
	for _, c := range r.Claims {
		ids = append(ids, c.ClaimID)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range slices.Compact(ids) {
		if _, err := u.Documents().Lock(ctx, id); err != nil && !errors.Is(err, ports.ErrNotFound) {
			return err
		}
	}
	return nil
}

// Check runs the rule over a reimbursement as it would be recorded, and writes nothing.
func (s ClaimReimbursements) Check(ctx context.Context, org uuid.UUID, r claim.Reimbursement) (CheckedReimbursement, error) {
	b, err := claim.EncodeReimbursement(r)
	if err != nil {
		return CheckedReimbursement{}, err
	}
	var out CheckedReimbursement
	err = s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		doc := storage.Document{ID: uuid.Nil, Type: storage.DocumentClaimReimbursement, Version: 1, Status: storage.DocumentDraft, Payload: b}
		out, err = checkReimbursement(ctx, u, doc, settings)
		return err
	})
	return out, err
}

// Record records a reimbursement and posts it in one transaction: the document, its number (RB-, gap-free), its entry, an
// allocation for each claim it settles, and the document's change to posted. A reimbursement the rule refuses writes
// nothing and answers every problem.
func (s ClaimReimbursements) Record(ctx context.Context, org uuid.UUID, r claim.Reimbursement) (RecordedReimbursement, error) {
	var out RecordedReimbursement
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = s.RecordIn(ctx, u, r)
		return err
	})
	return out, err
}

// RecordIn records a reimbursement in u, the caller's unit of work: what Record does, for a command that writes more with
// it, as matching a bank line does (ENG-02).
func (s ClaimReimbursements) RecordIn(ctx context.Context, u storage.UnitOfWork, r claim.Reimbursement) (RecordedReimbursement, error) {
	b, err := claim.EncodeReimbursement(r)
	if err != nil {
		return RecordedReimbursement{}, err
	}
	if err := lockClaims(ctx, u, r); err != nil {
		return RecordedReimbursement{}, err
	}
	doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentClaimReimbursement, Payload: b})
	if err != nil {
		return RecordedReimbursement{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return RecordedReimbursement{}, err
	}
	c, err := checkReimbursement(ctx, u, doc, settings)
	if err != nil {
		return RecordedReimbursement{}, err
	}
	if len(c.Problems) > 0 {
		return RecordedReimbursement{}, c.Problems
	}
	if doc.Number, err = u.Sequences().Number(ctx, doc.ID, storage.DocumentClaimReimbursement); err != nil {
		return RecordedReimbursement{}, err
	}
	out := RecordedReimbursement{}
	if out.Entry, err = u.Ledger().Append(ctx, toStorage(c.Entry, nil)); err != nil {
		return RecordedReimbursement{}, err
	}
	for _, x := range c.Settlements {
		a, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: doc.ID, SourceVersion: doc.Version, DocumentID: x.DocumentID, On: r.Date,
			Currency: x.Amount.Currency().Code, Amount: storage.Decimal(x.Amount.String()), BaseCurrency: settings.Base.Code,
			BaseAmount: storage.Decimal(x.Base.String()), RealisedFX: storage.Decimal(x.RealisedFX.String())})
		if err != nil {
			return RecordedReimbursement{}, err
		}
		out.Allocations = append(out.Allocations, a)
	}
	if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
		return RecordedReimbursement{}, err
	}
	doc.Status = storage.DocumentPosted
	out.Reimbursement = doc
	if s.AfterPosting == nil {
		return out, nil
	}
	return out, s.AfterPosting(ctx, u, doc, out.Entry)
}

// Void undoes a posted reimbursement with a reversing entry dated date, with a reason (FR-GL-12), and a reversing
// allocation for each of its allocations on that date, which reopens the claims. The date is not before the
// reimbursement's, and its period must be open (INV-03).
func (s ClaimReimbursements) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (RecordedReimbursement, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out RecordedReimbursement
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		out, err = s.VoidIn(ctx, u, id, date, reason)
		return err
	})
	return out, err
}

// VoidIn voids a posted reimbursement in u, the caller's unit of work: what Void does, for undoing the match of the bank
// line it was matched to in the same transaction.
func (s ClaimReimbursements) VoidIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, date time.Time, reason string) (RecordedReimbursement, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return RecordedReimbursement{}, err
	}
	r, err := claim.ReadReimbursement(doc)
	if err != nil {
		return RecordedReimbursement{}, err
	}
	if doc.Status != storage.DocumentPosted {
		return RecordedReimbursement{}, &claim.Error{Code: claim.CodeNotPosted, Message: fmt.Sprintf("the reimbursement is %s, not posted", doc.Status),
			Fix: "Only a posted reimbursement is voided"}
	}
	y, m, dd := date.Date()
	date = time.Date(y, m, dd, 0, 0, 0, 0, time.UTC)
	if date.Before(r.Date) {
		return RecordedReimbursement{}, &claim.Error{Code: claim.CodeVoidDate, Message: fmt.Sprintf("the void is dated %s, before the reimbursement's %s",
			date.Format(time.DateOnly), r.Date.Format(time.DateOnly)), Fix: "Date the void on or after the reimbursement"}
	}
	if err := lockClaims(ctx, u, r); err != nil {
		return RecordedReimbursement{}, err
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return RecordedReimbursement{}, err
	}
	stored, err := u.Ledger().ByIdentity(ctx, ledger.ClaimReimbursementIdentity(doc.ID, doc.Version, ledger.RuleClaimReimbursement))
	if err != nil {
		return RecordedReimbursement{}, err
	}
	original, err := fromStorage(stored, settings.Base)
	if err != nil {
		return RecordedReimbursement{}, err
	}
	reversal, err := ledger.Reverse(original, date, reason)
	if err != nil {
		return RecordedReimbursement{}, err
	}
	out := RecordedReimbursement{}
	if out.Entry, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
		return RecordedReimbursement{}, err
	}
	allocs, err := u.Allocations().OfSource(ctx, doc.ID)
	if err != nil {
		return RecordedReimbursement{}, err
	}
	for _, a := range allocs {
		if a.ReversesID != nil {
			continue
		}
		id := a.ID
		x, err := u.Allocations().Add(ctx, storage.NewAllocation{SourceID: a.SourceID, SourceVersion: a.SourceVersion, DocumentID: a.DocumentID, On: date,
			Currency: a.Currency, Amount: negated(a.Amount), BaseCurrency: a.BaseCurrency, BaseAmount: negated(a.BaseAmount),
			RealisedFX: negated(a.RealisedFX), Tolerance: negated(a.Tolerance), ReversesID: &id})
		if err != nil {
			return RecordedReimbursement{}, err
		}
		out.Allocations = append(out.Allocations, x)
	}
	if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
		return RecordedReimbursement{}, err
	}
	doc.Status = storage.DocumentReversed
	out.Reimbursement = doc
	if s.AfterPosting == nil {
		return out, nil
	}
	return out, s.AfterPosting(ctx, u, doc, out.Entry)
}

// replayClaimReimbursement posts a reimbursement version again as its entry posted it (ENG-06).
func replayClaimReimbursement(ctx context.Context, u storage.UnitOfWork, doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	p, _, err := claim.PrepareReimbursement(ctx, u, doc, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayClaimReimbursement(e.RuleVersion, p.Rule, s)
}
