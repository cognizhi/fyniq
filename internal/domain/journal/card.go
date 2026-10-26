package journal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/claim"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeNotYourCard is a card line completed by someone other than the card's holder (FR-AP-18).
const CodeNotYourCard = "CLAIM-34"

// CardSpend is how a cardholder completes a line of their company card from My claims (FR-AP-18; p1-expense-claims
// design D9): the expense policy's category it was for, its GST code and what it was for; or, when Personal, personal
// spend the cardholder owes.
type CardSpend struct {
	LineID      uuid.UUID
	CategoryID  uuid.UUID
	GSTCode     string
	Description string
	Personal    bool
	At          time.Time
}

// CardCompleted is a completed card line: the document it made (the bank transaction that categorised it, or the
// personal spend recorded from it) and the line as it now is.
type CardCompleted struct {
	Document storage.Document
	Line     storage.BankLine
}

// CardLine is a line of a card the holder holds, with its money account; CodeNotYourCard when the line is not on a
// card holder holds.
func CardLine(ctx context.Context, u storage.UnitOfWork, holder, lineID uuid.UUID) (storage.BankLine, storage.MoneyAccount, error) {
	refuse := func() (storage.BankLine, storage.MoneyAccount, error) {
		return storage.BankLine{}, storage.MoneyAccount{}, &claim.Error{Code: CodeNotYourCard, Message: "the line is not on a company card you hold",
			Fix: "Complete the lines of your own card; finance categorises the others in Banking"}
	}
	line, err := u.Banking().Line(ctx, lineID)
	if errors.Is(err, ports.ErrNotFound) {
		return refuse()
	}
	if err != nil {
		return storage.BankLine{}, storage.MoneyAccount{}, err
	}
	acct, err := u.Banking().MoneyAccount(ctx, line.MoneyAccountID)
	if err != nil {
		return storage.BankLine{}, storage.MoneyAccount{}, err
	}
	if acct.Kind != storage.MoneyCard || acct.CardholderID == nil || *acct.CardholderID != holder {
		return refuse()
	}
	return line, acct, nil
}

// CompleteCard completes a line of holder's company card in one transaction (FR-AP-18): spent for the company, the
// line is categorised to the account of the expense policy's category, with its GST code, as Banking categorises it;
// personal spend is recorded as an advance to the cardholder from the card (Dr the advances account · Cr the card,
// cash_advance@1) and the line matched to it, so the cardholder owes it until they refund it. Only the card's holder
// completes its lines, and only to a category of the expense policy.
func (s BankTransactions) CompleteCard(ctx context.Context, org, holder uuid.UUID, c CardSpend) (CardCompleted, error) {
	var out CardCompleted
	var categorised *uuid.UUID
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		line, acct, err := CardLine(ctx, u, holder, c.LineID)
		if err != nil {
			return err
		}
		if c.Personal {
			return s.personalSpend(ctx, u, holder, line, acct, c, &out)
		}
		cat, err := u.ClaimCategories().Get(ctx, c.CategoryID)
		switch {
		case errors.Is(err, ports.ErrNotFound):
			return &claim.Error{Code: claim.CodeCategory, Message: "the category does not exist in this organisation",
				Fix: "Choose one of the expense policy's categories"}
		case err != nil:
			return err
		case !cat.Active || cat.Mileage:
			return &claim.Error{Code: claim.CodeCategory, Message: fmt.Sprintf("the category %s cannot be used for card spend", cat.Name),
				Fix: "Choose an active category for receipts"}
		}
		done, err := s.categorise(ctx, u, Categorisation{Basis: storage.BasisPerson, At: c.At, Draft: banktransaction.Draft{LineID: line.ID,
			Reference: strings.TrimSpace(c.Description), Parts: []banktransaction.Part{{AccountID: cat.AccountID, GSTCode: c.GSTCode,
				Description: strings.TrimSpace(c.Description)}}}})
		if err != nil {
			return err
		}
		out = CardCompleted{Document: done.Transaction, Line: done.Line}
		categorised = &done.Transaction.ID
		return nil
	})
	if err == nil && categorised != nil {
		s.releaseNow(ctx, org, *categorised)
	}
	return out, err
}

// personalSpend records a card line as personal spend: an advance to the cardholder of what left the card, matched to
// the line.
func (s BankTransactions) personalSpend(ctx context.Context, u storage.UnitOfWork, holder uuid.UUID, line storage.BankLine, acct storage.MoneyAccount,
	c CardSpend, out *CardCompleted) error {
	cur, err := money.Lookup(line.Currency)
	if err != nil {
		return err
	}
	amount, err := money.ParseStored(string(line.Amount), cur)
	if err != nil {
		return err
	}
	if amount.Sign() >= 0 {
		return &claim.Error{Code: claim.CodeAdvanceRefund, Message: "the line is money into the card, not spend on it",
			Fix: "Mark only spend as personal; finance categorises money in"}
	}
	if line.Status != storage.LineUnreconciled {
		return &banktransaction.Error{Code: CodeLineDone, Message: fmt.Sprintf("the line is %s, not waiting to be completed", line.Status),
			Fix: "Ask finance to undo what it is matched to first"}
	}
	rec, err := (CashAdvances{Store: s.Store, AfterPosting: s.AfterPosting}).RecordIn(ctx, u, claim.Advance{PersonID: holder, Personal: true,
		Date: line.BookingDate, AccountID: acct.AccountID, Amount: amount.Neg(), Reference: strings.TrimSpace(c.Description)})
	if err != nil {
		return err
	}
	m, err := s.matchRecorded(ctx, u, line.ID, rec.Advance.ID, storage.BasisPerson, c.At)
	if err != nil {
		return err
	}
	*out = CardCompleted{Document: rec.Advance, Line: m.Line}
	return nil
}
