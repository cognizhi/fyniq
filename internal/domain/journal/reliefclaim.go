package journal

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/reliefclaim"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ReliefClaims runs the commands of claims of GST bad debt relief (FR-AR-11; p2-tax-filings task 3.2) against a store: a claim is made
// and posted at once, in one unit of work (the document, its entry from the rule bad_debt_relief@1, ENG-02), the write-off's invoice locked
// first so no other claim of the invoice reads what it left; and it is voided by a reversal. A claim is never edited.
type ReliefClaims struct {
	Store storage.Store
}

// PostedRelief is a posted or voided claim: its document and its entry.
type PostedRelief struct {
	Claim storage.Document
	Entry storage.Entry
}

// Check reads a claim as claiming it now would post it, and writes nothing.
func (s ReliefClaims) Check(ctx context.Context, org uuid.UUID, r reliefclaim.Request, today time.Time) (reliefclaim.Prepared, ledger.Entry, error) {
	var prepared reliefclaim.Prepared
	var entry ledger.Entry
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		prepared, entry, err = checkRelief(ctx, u, r, today)
		return err
	})
	return prepared, entry, err
}

func checkRelief(ctx context.Context, u storage.UnitOfWork, r reliefclaim.Request, today time.Time) (reliefclaim.Prepared, ledger.Entry, error) {
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return reliefclaim.Prepared{}, ledger.Entry{}, err
	}
	p, err := reliefclaim.Prepare(ctx, u, r, settings, today)
	if err != nil {
		return p, ledger.Entry{}, err
	}
	entry, err := ledger.PostBadDebtRelief(p.Rule, settings)
	return p, entry, err
}

// Claim makes and posts a claim, in one transaction, the write-off's invoice locked first. A claim the rules refuse posts nothing.
func (s ReliefClaims) Claim(ctx context.Context, org uuid.UUID, r reliefclaim.Request, today time.Time) (PostedRelief, error) {
	var out PostedRelief
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		wo, err := u.Documents().Lock(ctx, r.WriteOffID) // ENG-47 step 1: the write-off, then its invoice
		if err != nil {
			return err
		}
		if wo.Type == storage.DocumentWriteOff {
			if err := lockBills(ctx, u, []uuid.UUID{writeOffInvoice(wo)}); err != nil {
				return err
			}
		}
		p, entry, err := checkRelief(ctx, u, r, today)
		if err != nil {
			return err
		}
		doc, err := u.Documents().Create(ctx, storage.NewDocument{Type: storage.DocumentBadDebtRelief, Payload: p.Payload})
		if err != nil {
			return err
		}
		p.Rule.ID, p.Rule.Version = doc.ID, doc.Version
		if entry, err = ledger.PostBadDebtRelief(p.Rule, mustSettings(ctx, u)); err != nil {
			return err
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(entry, nil)); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, doc.ID, storage.DocumentPosted); err != nil {
			return err
		}
		doc.Status = storage.DocumentPosted
		out.Claim = doc
		return nil
	})
	return out, err
}

func mustSettings(ctx context.Context, u storage.UnitOfWork) ledger.Settings {
	s, _ := loadSettings(ctx, u) // loaded and checked a moment ago in this transaction
	return s
}

// writeOffInvoice is the invoice a write-off document names, uuid.Nil when it cannot be read.
func writeOffInvoice(doc storage.Document) uuid.UUID {
	d, err := writeOffRead(doc)
	if err != nil {
		return uuid.Nil
	}
	return d
}

// Void undoes a posted claim with a reversing entry dated date, with a reason (FR-GL-12); the claim is marked reversed, which frees its
// write-off to be claimed again or voided.
func (s ReliefClaims) Void(ctx context.Context, org, id uuid.UUID, date time.Time, reason string) (PostedRelief, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out PostedRelief
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		doc, err := u.Documents().Lock(ctx, id)
		if err != nil {
			return err
		}
		if !reliefclaim.IsClaim(doc) {
			return &reliefclaim.Error{Code: reliefclaim.CodeNotClaim, Message: fmt.Sprintf("document %s is a %s, not a claim of relief", doc.ID, doc.Type), Fix: "Open it with its own module"}
		}
		if doc.Status != storage.DocumentPosted {
			return &reliefclaim.Error{Code: reliefclaim.CodeNotPosted, Message: fmt.Sprintf("the claim is %s, not posted", doc.Status), Fix: "Only a posted claim is voided"}
		}
		c, err := reliefclaim.Read(doc)
		if err != nil {
			return err
		}
		y, m, d := date.Date()
		date = time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
		if date.Before(c.Date) {
			return &reliefclaim.Error{Code: reliefclaim.CodeVoidDate, Message: fmt.Sprintf("the void is dated %s, before the claim's %s", date.Format(time.DateOnly), c.Date.Format(time.DateOnly)),
				Fix: "Date the void on or after the claim"}
		}
		if err := lockBills(ctx, u, []uuid.UUID{c.InvoiceID}); err != nil {
			return err
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		stored, err := u.Ledger().ByIdentity(ctx, ledger.BadDebtReliefIdentity(doc.ID, doc.Version, ledger.RuleBadDebtRelief))
		if err != nil {
			return err
		}
		original, err := fromStorage(stored, settings.Base)
		if err != nil {
			return err
		}
		reversal, err := ledger.Reverse(original, date, reason)
		if err != nil {
			return err
		}
		if out.Entry, err = u.Ledger().Append(ctx, toStorage(reversal, &stored.ID)); err != nil {
			return err
		}
		if err := u.Documents().SetStatus(ctx, id, storage.DocumentReversed); err != nil {
			return err
		}
		doc.Status = storage.DocumentReversed
		out.Claim = doc
		return nil
	})
	return out, err
}

// replayBadDebtRelief posts a stored claim again under the rule that posted e (ENG-06), from what the claim kept.
func replayBadDebtRelief(doc storage.Document, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	c, err := reliefclaim.Read(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayBadDebtRelief(e.RuleVersion, c.Rule, s)
}
