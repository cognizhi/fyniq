package banking

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CodeCardholder is a cardholder on a money account that is not a card, or one who is not an active member (FR-AP-18).
const CodeCardholder = "BANK-14"

// CheckCardholder checks a money account's cardholder as it is set (FR-AP-18; p1-expense-claims design D9): only a card
// has one, and they are an active member of the organisation, whose My claims shows the card's lines.
func CheckCardholder(ctx context.Context, u storage.UnitOfWork, kind string, holder *uuid.UUID) error {
	if holder == nil {
		return nil
	}
	if kind != storage.MoneyCard {
		return refuse(CodeCardholder, "Leave the cardholder out, or make it a card", "a %s has no cardholder: only a card does", kind)
	}
	m, err := u.Members().Membership(ctx, *holder)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return refuse(CodeCardholder, "Choose a member of the organisation", "the cardholder is not a member of this organisation")
	case err != nil:
		return err
	case m.Status != "active":
		return refuse(CodeCardholder, "Choose an active member", "the cardholder is no longer a member of this organisation")
	}
	return nil
}
