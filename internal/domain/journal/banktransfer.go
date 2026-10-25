package journal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Transfers between money accounts (FR-BNK-09; p1-bank-import-reconcile task 4.4, design D6): one record with a leg on
// each account, posted once by bank_transfer@1 and matched to a line on each account as the lines arrive; with one
// side's line only it is in transit until the other arrives. Across currencies each leg is booked at its own
// currency's rate for the day, a fee the provider reports goes to the bank charges account (the setting
// bank.charges_account, 6460 by default) and the rest of the difference to 7030 (the owner's decision of 25 Oct 2026).

// The stable codes of transfers (ENG-23).
const (
	CodeTransferLine   = "BANKTXN-14" // a line that cannot be a transfer's side: of the wrong direction or account, or of another transfer
	CodeTransferUndone = "BANKTXN-15" // the transfer was undone: its entry is reversed
)

// TransferRequest is a transfer as recorded: the lines known, one or both (the paying line's money out, the receiving
// line's money in); the money accounts and amounts of a side whose line has not arrived (a line gives its own); the fee
// the provider reports, in the paying currency; and the date, which a line gives too (the paying line's first).
type TransferRequest struct {
	FromLineID, ToLineID       *uuid.UUID
	FromAccountID, ToAccountID uuid.UUID
	Paid, Received             *money.Money
	Fee                        *money.Money
	Date                       time.Time
	Basis                      string
	At                         time.Time
}

// Transferred is a recorded transfer, its entry, and the matches of its lines.
type Transferred struct {
	Transfer storage.BankTransfer
	Entry    *storage.Entry
	Matches  []storage.BankMatch
}

// transferSide reads a side's line: its money account and its amount, money out for the paying side.
func (s BankTransactions) transferSide(ctx context.Context, u storage.UnitOfWork, id uuid.UUID, paying bool, at time.Time) (storage.BankLine, money.Money, error) {
	line, _, amount, err := s.matchable(ctx, u, id, at)
	if err != nil {
		return line, money.Money{}, err
	}
	if paying && amount.Sign() >= 0 || !paying && amount.Sign() <= 0 {
		what := map[bool]string{true: "paying side's line is money in", false: "receiving side's line is money out"}[paying]
		return line, money.Money{}, &banktransaction.Error{Code: CodeTransferLine, Message: "the " + what, Fix: "Choose the line that left the paying account and the one that reached the receiving account"}
	}
	if paying {
		amount = amount.Neg()
	}
	return line, amount, nil
}

// RecordTransfer records a transfer and posts it, in one transaction with the matches of the lines it names and their
// status. A side without its line waits in transit until CompleteTransfer matches it.
func (s BankTransactions) RecordTransfer(ctx context.Context, org uuid.UUID, r TransferRequest) (Transferred, error) {
	var out Transferred
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if r.FromLineID == nil && r.ToLineID == nil {
			return &banktransaction.Error{Code: CodeTransferLine, Message: "a transfer names the line of one side at least", Fix: "Choose the line it was"}
		}
		n := storage.NewBankTransfer{FromAccountID: r.FromAccountID, ToAccountID: r.ToAccountID, OnDate: r.Date, At: r.At}
		var lines []storage.BankLine
		paid, received := r.Paid, r.Received
		if r.ToLineID != nil {
			line, amount, err := s.transferSide(ctx, u, *r.ToLineID, false, r.At)
			if err != nil {
				return err
			}
			n.ToLineID, n.ToAccountID, n.OnDate, received = &line.ID, line.MoneyAccountID, line.BookingDate, &amount
			lines = append(lines, line)
		}
		if r.FromLineID != nil {
			line, amount, err := s.transferSide(ctx, u, *r.FromLineID, true, r.At)
			if err != nil {
				return err
			}
			n.FromLineID, n.FromAccountID, n.OnDate, paid = &line.ID, line.MoneyAccountID, line.BookingDate, &amount
			lines = append(lines, line)
		}
		if paid == nil || received == nil {
			return &banktransaction.Error{Code: CodeTransferLine, Message: "the amount of the side whose line has not arrived is missing",
				Fix: "Enter what left the paying account or reached the receiving account, as the provider reports it"}
		}
		n.FromCurrency, n.FromAmount, n.ToCurrency, n.ToAmount = paid.Currency().Code, storage.Decimal(paid.String()), received.Currency().Code, storage.Decimal(received.String())
		if r.Fee != nil && !r.Fee.IsZero() {
			fee := storage.Decimal(r.Fee.String())
			n.Fee = &fee
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		t, probs, err := s.transferOf(ctx, u, n, *paid, *received, r.Fee, settings)
		if err != nil {
			return err
		}
		if _, err := ledger.PostBankTransfer(t, settings); err != nil || len(probs) > 0 { // checked before anything is written
			if ps, ok := err.(ledger.Problems); ok {
				for _, x := range ps {
					if x.Code != ledger.CodeMissingRate || !probs.Has(fx.CodeNoRate) {
						probs = append(probs, x)
					}
				}
			} else if err != nil {
				return err
			}
			return probs
		}
		if out.Transfer, err = u.BankMatching().RecordTransfer(ctx, n); err != nil {
			return err
		}
		t.ID = out.Transfer.ID
		e, err := ledger.PostBankTransfer(t, settings)
		if err != nil {
			return err
		}
		stored, err := u.Ledger().Append(ctx, toStorage(e, nil))
		if err != nil {
			return err
		}
		out.Entry = &stored
		if out.Transfer, err = u.BankMatching().CompleteTransfer(ctx, out.Transfer.ID, out.Transfer.Version, nil, nil, &stored.ID); err != nil {
			return err
		}
		for _, line := range lines {
			m, err := s.transferMatch(ctx, u, line, out.Transfer.ID, r.Basis, r.At)
			if err != nil {
				return err
			}
			out.Matches = append(out.Matches, m)
		}
		return nil
	})
	return out, err
}

// CompleteTransfer matches a transfer in transit to its other side's line, which must be the money out of its paying
// account or the money in of its receiving account, of the amount recorded (FY223). Nothing new is posted.
func (s BankTransactions) CompleteTransfer(ctx context.Context, org, transferID, lineID uuid.UUID, basis string, at time.Time) (Transferred, error) {
	var out Transferred
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		tr, err := u.BankMatching().Transfer(ctx, transferID)
		if err != nil {
			return err
		}
		if tr.UndoneAt != nil {
			return &banktransaction.Error{Code: CodeTransferUndone, Message: "the transfer was undone", Fix: "Record the transfer again"}
		}
		line, err := u.Banking().Line(ctx, lineID)
		if err != nil {
			return err
		}
		var from, to *uuid.UUID
		switch {
		case tr.FromLineID == nil && line.MoneyAccountID == tr.FromAccountID:
			from = &line.ID
		case tr.ToLineID == nil && line.MoneyAccountID == tr.ToAccountID:
			to = &line.ID
		default:
			return &banktransaction.Error{Code: CodeTransferLine, Message: "the line is not the missing side of the transfer",
				Fix: "Choose a line of the account whose side has not arrived yet"}
		}
		if _, _, err := s.transferSide(ctx, u, lineID, from != nil, at); err != nil {
			return err
		}
		if out.Transfer, err = u.BankMatching().CompleteTransfer(ctx, tr.ID, tr.Version, from, to, nil); err != nil {
			return err
		}
		line, err = u.Banking().Line(ctx, lineID) // as the suspense undo may have left it
		if err != nil {
			return err
		}
		m, err := s.transferMatch(ctx, u, line, tr.ID, basis, at)
		if err != nil {
			return err
		}
		out.Matches = append(out.Matches, m)
		if tr.EntryID != nil {
			e, err := u.Ledger().Entry(ctx, *tr.EntryID)
			if err != nil {
				return err
			}
			out.Entry = &e
		}
		return nil
	})
	return out, err
}


// UndoTransfer undoes a transfer with a reason, while its period is open (FY220, FY224): its entry is reversed on its
// own date, the record is marked undone, the matches of its lines are undone and the lines go back to the queue
// unreconciled, free to join another transfer. The record stays as history, and is in transit no more.
func (s BankTransactions) UndoTransfer(ctx context.Context, org, transferID uuid.UUID, reason string, at time.Time) (Transferred, error) {
	if reason != "" {
		ctx = storage.WithReason(ctx, reason)
	}
	var out Transferred
	err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		tr, err := u.BankMatching().Transfer(ctx, transferID)
		if err != nil {
			return err
		}
		out.Transfer = tr
		if tr.UndoneAt != nil || tr.EntryID == nil {
			return &banktransaction.Error{Code: CodeTransferUndone, Message: "the transfer was undone already", Fix: "Nothing to undo"}
		}
		settings, err := loadSettings(ctx, u)
		if err != nil {
			return err
		}
		stored, err := u.Ledger().Entry(ctx, *tr.EntryID)
		if err != nil {
			return err
		}
		original, err := fromStorage(stored, settings.Base)
		if err != nil {
			return err
		}
		reversal, err := ledger.Reverse(original, original.PostingDate, reason)
		if err != nil {
			return err
		}
		e, err := u.Ledger().Append(ctx, toStorage(reversal, &stored.ID))
		if err != nil {
			return err
		}
		out.Entry = &e
		if out.Transfer, err = u.BankMatching().UndoTransfer(ctx, tr.ID, tr.Version, reason, at); err != nil {
			return err
		}
		for _, id := range []*uuid.UUID{tr.FromLineID, tr.ToLineID} {
			if id == nil {
				continue
			}
			matches, err := u.BankMatching().Matches(ctx, storage.BankMatchFilter{LineID: id})
			if err != nil {
				return err
			}
			for _, m := range matches {
				if m.TransferID == nil || *m.TransferID != tr.ID {
					continue
				}
				undone, err := u.BankMatching().UndoMatch(ctx, m.ID, reason, at)
				if err != nil {
					return err
				}
				out.Matches = append(out.Matches, undone)
			}
			line, err := u.Banking().Line(ctx, *id)
			if err != nil {
				return err
			}
			if line.Status == storage.LineTransferred {
				if _, err := u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineUnreconciled, at); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return out, err
}

func (s BankTransactions) transferMatch(ctx context.Context, u storage.UnitOfWork, line storage.BankLine, transfer uuid.UUID, basis string, at time.Time) (storage.BankMatch, error) {
	if basis == "" {
		basis = storage.BasisPerson
	}
	m, err := u.BankMatching().MakeMatch(ctx, storage.NewBankMatch{LineID: line.ID, Kind: storage.MatchTransfer, TransferID: &transfer, Basis: basis, At: at})
	if err != nil {
		return m, err
	}
	_, err = u.Banking().SetLineStatus(ctx, line.ID, line.Version, storage.LineTransferred, at)
	return m, err
}

// transferOf is a transfer as the rule reads it: the money accounts' chart accounts, each foreign leg's rate for the
// day from the cascade (FX-01 when there is none), and the bank charges account the setting names on the day.
func (s BankTransactions) transferOf(ctx context.Context, u storage.UnitOfWork, n storage.NewBankTransfer, paid, received money.Money, fee *money.Money,
	set ledger.Settings) (ledger.BankTransfer, ledger.Problems, error) {
	var probs ledger.Problems
	from, err := u.Banking().MoneyAccount(ctx, n.FromAccountID)
	if err != nil {
		return ledger.BankTransfer{}, nil, err
	}
	to, err := u.Banking().MoneyAccount(ctx, n.ToAccountID)
	if err != nil {
		return ledger.BankTransfer{}, nil, err
	}
	t := ledger.BankTransfer{Date: n.OnDate, Memo: fmt.Sprintf("Transfer from %s to %s", from.Name, to.Name), From: from.AccountID, Paid: paid,
		To: to.AccountID, Received: received, Fee: fee}
	if len(t.Memo) > ledger.MaxMemo {
		t.Memo = clip(t.Memo, ledger.MaxMemo)
	}
	for _, leg := range []struct {
		amount money.Money
		to     **money.Rate
		field  string
	}{{paid, &t.FromRate, "from_rate"}, {received, &t.ToRate, "to_rate"}} {
		if leg.amount.Currency() == set.Base {
			continue
		}
		f, err := fx.Lookup(ctx, u, set.Base, leg.amount.Currency(), n.OnDate)
		var fe *fx.Error
		switch {
		case errors.As(err, &fe):
			probs = append(probs, ledger.Problem{Code: fe.Code, Field: leg.field, Message: fe.Message, Fix: fe.Fix})
		case err != nil:
			return ledger.BankTransfer{}, nil, err
		default:
			rate := f.Rate
			*leg.to = &rate
		}
	}
	def, _ := settings.Lookup("bank.charges_account")
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return ledger.BankTransfer{}, nil, err
	}
	code := settings.On(def, changes, n.OnDate).Value
	acc, err := u.Accounts().ByCode(ctx, code)
	switch {
	case errors.Is(err, ports.ErrNotFound):
		if fee != nil && !fee.IsZero() {
			probs = append(probs, ledger.Problem{Code: ledger.CodeSystemRole, Field: "fee", Message: fmt.Sprintf("the chart has no account %s, the bank charges account", code),
				Fix: "Set Settings › Organisation › Bank charges account to an account of your chart"})
		}
	case err != nil:
		return ledger.BankTransfer{}, nil, err
	default:
		t.Charges = acc.ID
	}
	return t, probs, nil
}

// replayBankTransfer posts a stored transfer again under the rule that posted e (ENG-06), with what the entry kept:
// each leg's rate as it was, and the account its fee went to.
func replayBankTransfer(ctx context.Context, u storage.UnitOfWork, s ledger.Settings, e storage.Entry) (ledger.Entry, error) {
	id, err := uuid.Parse(e.Source.ID)
	if err != nil {
		return ledger.Entry{}, err
	}
	tr, err := u.BankMatching().Transfer(ctx, id)
	if err != nil {
		return ledger.Entry{}, err
	}
	from, err := u.Banking().MoneyAccount(ctx, tr.FromAccountID)
	if err != nil {
		return ledger.Entry{}, err
	}
	to, err := u.Banking().MoneyAccount(ctx, tr.ToAccountID)
	if err != nil {
		return ledger.Entry{}, err
	}
	stored, err := fromStorage(e, s.Base)
	if err != nil {
		return ledger.Entry{}, err
	}
	amount := func(d storage.Decimal, code string) (money.Money, error) {
		c, err := money.Lookup(code)
		if err != nil {
			return money.Money{}, err
		}
		return money.ParseStored(string(d), c)
	}
	t := ledger.BankTransfer{ID: tr.ID, Date: stored.PostingDate, Memo: stored.Memo, From: from.AccountID, To: to.AccountID}
	if t.Paid, err = amount(tr.FromAmount, tr.FromCurrency); err != nil {
		return ledger.Entry{}, err
	}
	if t.Received, err = amount(tr.ToAmount, tr.ToCurrency); err != nil {
		return ledger.Entry{}, err
	}
	if tr.Fee != nil {
		fee, err := amount(*tr.Fee, tr.FromCurrency)
		if err != nil {
			return ledger.Entry{}, err
		}
		t.Fee = &fee
	}
	for _, l := range stored.Lines {
		rate := l.Rate
		switch {
		case l.AccountID == t.From && l.Amount.Currency() != s.Base:
			t.FromRate = &rate
		case l.AccountID == t.To && l.Amount.Currency() != s.Base:
			t.ToRate = &rate
		case l.AccountID != t.From && l.AccountID != t.To && s.Accounts[l.AccountID].Role != ledger.RoleFXRounding:
			t.Charges = l.AccountID // the fee's account as it posted
		}
	}
	return ledger.ReplayBankTransfer(e.RuleVersion, t, s)
}
