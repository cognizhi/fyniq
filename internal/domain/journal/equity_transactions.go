package journal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/equity"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// EquityEffective is what makes an approved equity transaction effective (FR-EQ-17): the day ACRA's register of members
// shows it, never after today nor before its approval, ACRA's lodgement reference, and who records it.
type EquityEffective struct {
	EffectiveOn  time.Time
	LodgementRef string
	Today        time.Time
	RecordedBy   uuid.UUID
}

// EquityEffected is what an equity transaction wrote when it became effective: its movements, a cash issue's share issue,
// and its entries.
type EquityEffected struct {
	Doc       storage.Document
	Movements []storage.HoldingMovement
	Issue     *storage.ShareIssue
	Entries   []storage.Entry
}

// EquityTransactions make approved equity transactions effective (FR-EQ-17, B.7, INV-13; p1-equity-register design D3
// to D5). AfterPosting runs in the unit of work once a transaction is effective, with its first entry or none.
type EquityTransactions struct {
	AfterPosting func(ctx context.Context, u storage.UnitOfWork, doc storage.Document, entry storage.Entry) error
}

// effectiveError is a refusal to make a transaction effective (EQUITY-08).
func effectiveError(msg, fix string) error {
	return &equity.Error{Code: equity.CodeEffective, Message: msg, Fix: fix}
}

// EffectiveIn makes an approved equity transaction effective in u on e.EffectiveOn, writing everything it moves together:
//   - a cash issue once its subscriber's application monies cover it (B.7): the subscriber marked shareholder, the issue
//     posted from the register (share_issue_cash@1: Dr 2040 naming them · Cr the class) and recorded with its movement;
//   - a transfer, once the seller holds the shares on the day: the buyer marked shareholder, and its two movements, with
//     no entry;
//   - a conversion between classes, once the holder holds the shares: its two movements, and when the accounts differ its
//     entry (equity_transaction.class_conversion@1);
//   - a reversal: the negation of every movement of the transaction it reverses, and the reversal of every entry that
//     transaction posted, dated the reversal's day.
//
// The day is no later than today and no earlier than the approval (EQUITY-08); a closed period refuses it (INV-03), and
// reopening the period is the fix, never a later date. The database checks every movement again (FY288).
func (s EquityTransactions) EffectiveIn(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, e EquityEffective) (EquityEffected, error) {
	doc, err := u.Documents().Lock(ctx, id)
	if err != nil {
		return EquityEffected{}, err
	}
	t, err := equity.ReadTransaction(doc)
	if err != nil {
		return EquityEffected{}, err
	}
	out := EquityEffected{Doc: doc}
	if doc.Status != storage.DocumentPosted {
		return out, effectiveError("only an approved transaction becomes effective", "Ask for its approval first")
	}
	if moved, err := u.Holdings().OfTransaction(ctx, id); err != nil {
		return out, err
	} else if len(moved) > 0 {
		return out, effectiveError("the transaction is effective already", "Nothing to record: its shares are in the register")
	}
	day := func(t time.Time) string { return t.Format(time.DateOnly) }
	switch {
	case e.EffectiveOn.After(e.Today):
		return out, effectiveError(fmt.Sprintf("the effective date %s is after today", day(e.EffectiveOn)),
			"Enter the day ACRA's register of members shows it, once it does")
	case t.ApprovedOn != nil && e.EffectiveOn.Before(*t.ApprovedOn):
		return out, effectiveError(fmt.Sprintf("the effective date %s is before the approval on %s", day(e.EffectiveOn), day(*t.ApprovedOn)),
			"Enter the day ACRA's register shows it: never earlier than its approval")
	}
	settings, err := loadSettings(ctx, u)
	if err != nil {
		return out, err
	}
	move := func(m storage.HoldingMovement) error {
		if m.ID == uuid.Nil {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			m.ID = id
		}
		m.TransactionID, m.EffectiveOn, m.RecordedBy, m.RecordedAt = &doc.ID, e.EffectiveOn, e.RecordedBy, e.Today
		stored, err := u.Holdings().Move(ctx, m)
		out.Movements = append(out.Movements, stored)
		return err
	}
	memo := strings.TrimSpace(doc.Number + " " + strings.ReplaceAll(t.Kind, "_", " "))
	switch t.Kind {
	case equity.KindIssueCash:
		err = s.issueCash(ctx, u, doc, t, e, settings, memo, &out)
	case equity.KindTransfer:
		if err = holds(ctx, u, t.SellerID, t.ClassAccountID, t.ClassID, t.Shares, e.EffectiveOn); err != nil {
			return out, err
		}
		if err = markShareholder(ctx, u, t.BuyerID); err != nil {
			return out, err
		}
		if err = move(storage.HoldingMovement{Kind: storage.MovementTransfer, HolderID: t.SellerID, ClassAccountID: t.ClassAccountID, ClassID: t.ClassID,
			Shares: storage.Decimal("-" + t.Shares)}); err != nil {
			return out, err
		}
		err = move(storage.HoldingMovement{Kind: storage.MovementTransfer, HolderID: t.BuyerID, ClassAccountID: t.ClassAccountID, ClassID: t.ClassID,
			Shares: storage.Decimal(t.Shares)})
	case equity.KindClassConversion:
		if err = holds(ctx, u, t.HolderID, t.ClassAccountID, t.ClassID, t.Shares, e.EffectiveOn); err != nil {
			return out, err
		}
		if err = move(storage.HoldingMovement{Kind: storage.MovementClassConversion, HolderID: t.HolderID, ClassAccountID: t.ClassAccountID,
			ClassID: t.ClassID, Shares: storage.Decimal("-" + t.Shares)}); err != nil {
			return out, err
		}
		if err = move(storage.HoldingMovement{Kind: storage.MovementClassConversion, HolderID: t.HolderID, ClassAccountID: t.ToAccountID,
			ClassID: t.ToClassID, Shares: storage.Decimal(t.ToShares)}); err != nil {
			return out, err
		}
		if t.ClassAccountID != t.ToAccountID {
			amount, err := money.ParseStored(t.Amount, settings.Base)
			if err != nil {
				return out, err
			}
			entry, err := ledger.PostClassConversion(ledger.ClassConversion{ID: doc.ID, Version: doc.Version, PostOn: e.EffectiveOn, Memo: memo,
				From: t.ClassAccountID, To: t.ToAccountID, Amount: amount}, settings)
			if err != nil {
				return out, err
			}
			stored, err := u.Ledger().Append(ctx, toStorage(entry, nil))
			if err != nil {
				return out, err
			}
			out.Entries = append(out.Entries, stored)
		}
	case equity.KindReversal:
		err = s.reverse(ctx, u, t, e, settings, move, &out)
	default:
		return out, effectiveError(fmt.Sprintf("%q is not a kind of equity transaction Fyniq makes effective", t.Kind), "Draft it again")
	}
	if err != nil {
		return out, err
	}
	if s.AfterPosting != nil {
		var first storage.Entry
		if len(out.Entries) > 0 {
			first = out.Entries[0]
		}
		if err := s.AfterPosting(ctx, u, doc, first); err != nil {
			return out, err
		}
	}
	return out, nil
}

// issueCash makes a cash issue effective (B.7, G4-043): refused until the subscriber's share application monies on the
// day cover its amount; then the subscriber marked shareholder, the entry from the register, and the issue with its
// movement.
func (s EquityTransactions) issueCash(ctx context.Context, u storage.UnitOfWork, doc storage.Document, t equity.Transaction, e EquityEffective,
	settings ledger.Settings, memo string, out *EquityEffected) error {
	var monies uuid.UUID
	for id, a := range settings.Accounts {
		if a.SubscriberAccount() && a.Active && !a.Group {
			monies = id
			break
		}
	}
	if monies == uuid.Nil {
		return &equity.Error{Code: equity.CodeNoMonies, Message: "the chart of accounts has no share application monies account",
			Fix: "Add 2040 Share application monies (a liability with the role share-application-monies), then match the subscriber's payment to it"}
	}
	amount, err := money.ParseStored(t.Amount, settings.Base)
	if err != nil {
		return err
	}
	paid, err := balance.ApplicationMonies(ctx, u, settings.Base, monies, t.HolderID, e.EffectiveOn)
	if err != nil {
		return err
	}
	if c, err := paid.Cmp(amount); err != nil {
		return err
	} else if c < 0 {
		return effectiveError(fmt.Sprintf("%s %s of the %s %s due is paid on %s", paid, paid.Currency().Code, amount, amount.Currency().Code,
			e.EffectiveOn.Format(time.DateOnly)), "Match the subscriber's payment to share application monies first: a cash issue is never made effective unpaid")
	}
	if err := markShareholder(ctx, u, t.HolderID); err != nil {
		return err
	}
	issueID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	entry, err := ledger.PostCashIssue(ledger.CashIssue{ID: issueID, PostOn: e.EffectiveOn, Memo: memo, Party: t.HolderID, Monies: monies,
		Class: t.ClassAccountID, Amount: amount}, settings)
	if err != nil {
		return err
	}
	stored, err := u.Ledger().Append(ctx, toStorage(entry, nil))
	if err != nil {
		return err
	}
	out.Entries = append(out.Entries, stored)
	issue, err := equity.RecordIssue(ctx, u, storage.ShareIssue{ID: issueID, HolderID: t.HolderID, ClassAccountID: t.ClassAccountID, ClassID: t.ClassID,
		Shares: storage.Decimal(t.Shares), Price: storage.Decimal(t.Price), Currency: settings.Base.Code, Amount: storage.Decimal(amount.String()),
		Consideration: storage.ConsiderationCash, ResolutionRefs: t.ResolutionRefs, LodgementRef: e.LodgementRef, EffectiveOn: e.EffectiveOn,
		EntryID: &stored.ID, RecordedBy: e.RecordedBy, RecordedAt: e.Today}, &doc.ID)
	if err != nil {
		return err
	}
	out.Issue = &issue
	moved, err := u.Holdings().OfTransaction(ctx, doc.ID)
	out.Movements = moved
	return err
}

// reverse makes a reversal effective: every movement of the transaction it reverses negated, and every entry that
// transaction posted reversed on the reversal's day (FR-GL-12), so a cash issue's capital goes back to share application
// monies for its refund.
func (s EquityTransactions) reverse(ctx context.Context, u storage.UnitOfWork, t equity.Transaction, e EquityEffective, settings ledger.Settings,
	move func(storage.HoldingMovement) error, out *EquityEffected) error {
	moved, err := u.Holdings().OfTransaction(ctx, t.ReversesID)
	if err != nil {
		return err
	}
	if len(moved) == 0 {
		return effectiveError("the transaction it reverses is not effective", "Draft the reversal of an effective transaction")
	}
	var entries []uuid.UUID
	issues, err := u.ShareIssues().List(ctx)
	if err != nil {
		return err
	}
	for _, m := range moved {
		if m.Shares[0] != '-' { // shares going back out of a holding must be held on the day
			if err := holds(ctx, u, m.HolderID, m.ClassAccountID, m.ClassID, string(m.Shares), e.EffectiveOn); err != nil {
				return err
			}
		}
		if m.ShareIssueID != nil {
			for _, x := range issues {
				if x.ID == *m.ShareIssueID && x.EntryID != nil {
					entries = append(entries, *x.EntryID)
				}
			}
		}
	}
	reversed, err := u.Documents().Get(ctx, t.ReversesID)
	if err != nil {
		return err
	}
	if c, err := u.Ledger().ByIdentity(ctx, ledger.ClassConversionIdentity(reversed.ID, reversed.Version, ledger.RuleClassConversion)); err == nil {
		entries = append(entries, c.ID)
	}
	for _, m := range moved {
		id := m.ID
		shares := string(m.Shares)
		if strings.HasPrefix(shares, "-") {
			shares = shares[1:]
		} else {
			shares = "-" + shares
		}
		if err := move(storage.HoldingMovement{Kind: storage.MovementReversal, ReversesID: &id, HolderID: m.HolderID, ClassAccountID: m.ClassAccountID,
			ClassID: m.ClassID, Shares: storage.Decimal(shares)}); err != nil {
			return err
		}
	}
	for _, id := range entries {
		stored, err := u.Ledger().Entry(ctx, id)
		if err != nil {
			return err
		}
		original, err := fromStorage(stored, settings.Base)
		if err != nil {
			return err
		}
		reversal, err := ledger.Reverse(original, e.EffectiveOn, t.Reason)
		if err != nil {
			return err
		}
		r, err := u.Ledger().Append(ctx, toStorage(reversal, &stored.ID))
		if err != nil {
			return err
		}
		out.Entries = append(out.Entries, r)
	}
	return nil
}

// holds refuses a transaction that would leave holder with fewer than no shares of a class on day (EQUITY-08; FY288).
func holds(ctx context.Context, u storage.UnitOfWork, holder, account uuid.UUID, class *uuid.UUID, shares string, day time.Time) error {
	have, err := equity.Holding(ctx, u, holder, account, class, day)
	if err != nil {
		return err
	}
	var want apd.Decimal
	if _, _, err := want.SetString(strings.TrimPrefix(shares, "-")); err != nil {
		return err
	}
	if have.Cmp(&want) < 0 {
		return effectiveError(fmt.Sprintf("the holder holds %s of these shares on %s, fewer than the %s it moves", have.Text('f'),
			day.Format(time.DateOnly), want.Text('f')), "Check the register: a holder moves only shares they hold on the day")
	}
	return nil
}

// markShareholder marks a contact shareholder once shares are theirs, as the register of members does (FR-EQ-17).
func markShareholder(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) error {
	c, err := u.Contacts().Get(ctx, id)
	if err != nil || c.Shareholder || c.Director {
		return err
	}
	d := c.ContactDetails
	d.Shareholder = true
	_, err = u.Contacts().Update(ctx, c.ID, c.Version, d, c.Active)
	return err
}

// replayCashIssue posts a cash issue's entry again from its row in the register (ENG-06): the monies account is the one
// its entry debited, which the register does not keep.
func replayCashIssue(s ledger.Settings, e storage.Entry, x storage.ShareIssue) (ledger.Entry, error) {
	amount, err := money.ParseStored(string(x.Amount), s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	var monies uuid.UUID
	for _, l := range e.Lines {
		if s.Accounts[l.AccountID].SubscriberAccount() {
			monies = l.AccountID
		}
	}
	return ledger.ReplayCashIssue(e.RuleVersion, ledger.CashIssue{ID: x.ID, PostOn: e.PostingDate, Memo: e.Memo, Party: x.HolderID, Monies: monies,
		Class: x.ClassAccountID, Amount: amount}, s)
}

// replayClassConversion posts a conversion between classes again from the version its entry names (ENG-06).
func replayClassConversion(ctx context.Context, u storage.UnitOfWork, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	id, err := uuid.Parse(e.Source.ID)
	if err != nil {
		return ledger.Entry{}, err
	}
	doc, err := u.Documents().Version(ctx, id, e.Source.Version)
	if err != nil {
		return ledger.Entry{}, err
	}
	t, err := equity.ReadTransaction(doc)
	if err != nil {
		return ledger.Entry{}, err
	}
	amount, err := money.ParseStored(t.Amount, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	return ledger.ReplayClassConversion(e.RuleVersion, ledger.ClassConversion{ID: id, Version: e.Source.Version, PostOn: e.PostingDate, Memo: e.Memo,
		From: t.ClassAccountID, To: t.ToAccountID, Amount: amount}, s)
}
