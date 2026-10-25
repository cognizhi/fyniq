package banking

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ImportRequest is one file, report or pull to import.
type ImportRequest struct {
	Format      string
	Data        []byte
	Account     *uuid.UUID        // the money account chosen: required for CSV and XLSX; otherwise the file's account must be it
	Mapping     *bankfile.Mapping // CSV and XLSX
	Source      string            // storage.MoneySourceFile, MoneySourceEmail or MoneySourceConnector
	FileRef     string            // the kept upload or report
	ConnectorID *uuid.UUID
	At          time.Time
}

// What becomes of each incoming line (FR-BNK-04, FR-BNK-16).
const (
	OutcomeAdd       = "add"                // a new line
	OutcomeFlag      = "possible_duplicate" // kept, and marked for a person to resolve (C.2 #51)
	OutcomeDuplicate = "duplicate"          // dropped: the bank's reference or the running balance proves it is held already
)

// ImportedLine is an incoming line and its outcome.
type ImportedLine struct {
	bankfile.Line
	Outcome     string
	DuplicateOf *uuid.UUID // the held line it duplicates or may duplicate
	Transfer    *uuid.UUID // a transfer in transit this line may be the other side of
	ID          *uuid.UUID // the stored line, once added
}

// ImportedStatement is what an incoming statement became.
type ImportedStatement struct {
	Account          storage.MoneyAccount
	Statement        *storage.BankStatement // nil when nothing was added: every line was held already
	From, To         time.Time
	Opening, Closing *money.Money // as the file gave them or as derived from its lines
	Lines            []ImportedLine
	Gap              *storage.NewBankGap
	Added, Flagged   int
	Dropped          int
}

// Import reads r's file and adds each statement's new lines to its money account, in u (FR-BNK-02): statements for
// several accounts at once, each matched to its money account by the identifier the file names and its currency; a
// line held already is dropped only on proof, a look-alike kept as a possible duplicate (FR-BNK-04, FR-BNK-16); a break
// in the balance chain is recorded as a gap, never filled; a line that may be a transfer's other side is named. Nothing
// is written for a statement whose every line is held already.
func Import(ctx context.Context, u storage.UnitOfWork, r ImportRequest) ([]ImportedStatement, error) {
	accounts, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var chosen *storage.MoneyAccount
	if r.Account != nil {
		i := indexOf(accounts, *r.Account)
		if i < 0 {
			return nil, refuse(CodeAccount, "Choose one of the organisation's money accounts.", "there is no such money account")
		}
		chosen = &accounts[i]
	}
	var cur money.Currency
	if r.Format == bankfile.FormatCSV || r.Format == bankfile.FormatXLSX {
		if chosen == nil {
			return nil, refuse(CodeRequest, "Import a CSV or XLSX statement from its money account.", "a %s statement needs the money account it is for", r.Format)
		}
		if cur, err = money.Lookup(chosen.Currency); err != nil {
			return nil, err
		}
	}
	parsed, err := bankfile.Parse(r.Format, r.Data, r.Mapping, cur, bankfile.DefaultLimits)
	if err != nil {
		var fe *bankfile.Error
		if errors.As(err, &fe) {
			return nil, &Error{Code: CodeFile, Message: fe.Error(), Fix: fe.Fix, Err: fe}
		}
		return nil, err
	}
	transit, err := u.BankMatching().InTransit(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ImportedStatement, 0, len(parsed))
	for _, st := range parsed {
		acct, err := accountOf(st, chosen, accounts)
		if err != nil {
			return nil, err
		}
		got, err := importOne(ctx, u, r, acct, st, transit)
		if err != nil {
			return nil, err
		}
		out = append(out, got)
	}
	return out, nil
}

// ImportStatement adds a statement already read, such as a bank-data connector's pull (FR-BNK-15), to money account
// r.Account, in u: the same account, currency, duplicate and continuity checks as a file's (FR-BNK-04, FR-BNK-16).
func ImportStatement(ctx context.Context, u storage.UnitOfWork, r ImportRequest, st bankfile.Statement) (ImportedStatement, error) {
	if r.Account == nil {
		return ImportedStatement{}, refuse(CodeRequest, "Import a pulled statement into its money account.", "the statement names no money account")
	}
	accounts, err := u.Banking().MoneyAccounts(ctx)
	if err != nil {
		return ImportedStatement{}, err
	}
	i := indexOf(accounts, *r.Account)
	if i < 0 {
		return ImportedStatement{}, refuse(CodeAccount, "Choose one of the organisation's money accounts.", "there is no such money account")
	}
	acct, err := accountOf(st, &accounts[i], accounts)
	if err != nil {
		return ImportedStatement{}, err
	}
	transit, err := u.BankMatching().InTransit(ctx)
	if err != nil {
		return ImportedStatement{}, err
	}
	return importOne(ctx, u, r, acct, st, transit)
}

func indexOf(accounts []storage.MoneyAccount, id uuid.UUID) int {
	for i, a := range accounts {
		if a.ID == id {
			return i
		}
	}
	return -1
}

// accountOf is the money account a statement is for: the one chosen, which the file's identifier must name when it
// names one, or the one whose identifier and currency the file names.
func accountOf(st bankfile.Statement, chosen *storage.MoneyAccount, accounts []storage.MoneyAccount) (storage.MoneyAccount, error) {
	var acct storage.MoneyAccount
	switch {
	case chosen != nil:
		acct = *chosen
		if st.Account != "" && acct.Identifier != "" && identifier(st.Account) != identifier(acct.Identifier) {
			return acct, refuse(CodeAccount, "Import the file into the money account it is for, or correct the account's identifier.",
				"the statement is for account %s, not %s (%s)", last4(st.Account), acct.Name, last4(acct.Identifier))
		}
	default:
		found := false
		for _, a := range accounts {
			if a.Identifier != "" && identifier(a.Identifier) == identifier(st.Account) && a.Currency == st.Currency.Code {
				acct, found = a, true
				break
			}
		}
		if !found {
			return acct, refuse(CodeAccount, "Add the money account with this identifier first, or import the file from the account it is for.",
				"the statement is for account %s in %s, which is no money account here", last4(st.Account), st.Currency.Code)
		}
	}
	if acct.Currency != st.Currency.Code {
		return acct, refuse(CodeCurrency, "Import the statement into the money account in its currency.",
			"the statement is in %s and %s is in %s", st.Currency.Code, acct.Name, acct.Currency)
	}
	if acct.ClosedOn != nil {
		return acct, refuse(CodeClosed, "Reopen the money account, or import into the account that replaced it.", "%s is closed", acct.Name)
	}
	return acct, nil
}

// balances fills in the running balance of lines that lack it, from the statement's opening balance forward or its
// closing balance backward (a running-balance column's last value being the closing balance), and returns the opening
// balance the lines imply.
func balances(st *bankfile.Statement) (*money.Money, error) {
	if st.Opening == nil && st.Closing == nil && len(st.Lines) > 0 && st.Lines[len(st.Lines)-1].Balance != nil {
		c := *st.Lines[len(st.Lines)-1].Balance // a running-balance column ends at the closing balance
		st.Closing = &c
	}
	opening := st.Opening
	if opening == nil && st.Closing != nil {
		o := *st.Closing
		for _, l := range st.Lines {
			var err error
			if o, err = o.Sub(l.Amount); err != nil {
				return nil, err
			}
		}
		opening = &o
	}
	if opening == nil {
		return nil, nil
	}
	run := *opening
	for i := range st.Lines {
		var err error
		if run, err = run.Add(st.Lines[i].Amount); err != nil {
			return nil, err
		}
		if st.Lines[i].Balance == nil {
			b := run
			st.Lines[i].Balance = &b
		}
	}
	return opening, nil
}

// Fingerprint is a line's fingerprint (FR-BNK-04): its account, booking date, amount, currency, the bank's reference,
// the counterparty, and its running balance or, without one, its position in its statement.
func Fingerprint(account uuid.UUID, l bankfile.Line) string {
	last := "#" + strconv.Itoa(l.Position)
	if l.Balance != nil {
		last = l.Balance.String()
	}
	h := sha256.Sum256([]byte(strings.Join([]string{account.String(), l.Booked.Format("2006-01-02"), l.Amount.String(),
		l.Amount.Currency().Code, l.Reference, l.Counterparty, last}, "|")))
	return hex.EncodeToString(h[:])
}

func dec(m *money.Money) *storage.Decimal {
	if m == nil {
		return nil
	}
	d := storage.Decimal(m.String())
	return &d
}

func sameAmount(a storage.Decimal, m money.Money) bool {
	held, err := money.ParseStored(string(a), m.Currency())
	return err == nil && held.Equal(m)
}

func importOne(ctx context.Context, u storage.UnitOfWork, r ImportRequest, acct storage.MoneyAccount, st bankfile.Statement,
	transit []storage.BankTransfer) (ImportedStatement, error) {
	opening, err := balances(&st)
	if err != nil {
		return ImportedStatement{}, err
	}
	out := ImportedStatement{Account: acct, From: st.From, To: st.To, Opening: opening, Closing: st.Closing}

	// Duplicates: each held line on the account in the statement's period can absorb one incoming line; identical lines
	// within this statement are never compared with each other (G4-042).
	from, to := st.From, st.To
	held, err := u.Banking().Lines(ctx, storage.BankLineFilter{MoneyAccountID: &acct.ID, From: &from, To: &to})
	if err != nil {
		return out, err
	}
	used := map[uuid.UUID]bool{}
	for _, l := range st.Lines {
		il := ImportedLine{Line: l, Outcome: OutcomeAdd}
		var lookalike *storage.BankLine
		for i := range held {
			h := &held[i]
			if used[h.ID] || !h.BookingDate.Equal(l.Booked) || !sameAmount(h.Amount, l.Amount) {
				continue
			}
			proof := (l.Reference != "" && l.Reference == h.Reference) ||
				(l.Balance != nil && h.RunningBalance != nil && sameAmount(*h.RunningBalance, *l.Balance))
			if proof {
				il.Outcome, il.DuplicateOf = OutcomeDuplicate, &h.ID
				used[h.ID] = true
				lookalike = nil
				break
			}
			if lookalike == nil {
				lookalike = h
			}
		}
		if il.Outcome == OutcomeAdd && lookalike != nil {
			il.Outcome, il.DuplicateOf = OutcomeFlag, &lookalike.ID
			used[lookalike.ID] = true
		}
		if il.Outcome != OutcomeDuplicate {
			il.Transfer = transferOf(acct, l, transit)
		}
		switch il.Outcome {
		case OutcomeAdd:
			out.Added++
		case OutcomeFlag:
			out.Flagged++
		default:
			out.Dropped++
		}
		out.Lines = append(out.Lines, il)
	}
	if out.Added+out.Flagged == 0 {
		return out, nil // a re-import, or an overlap held already, adds nothing
	}

	// Continuity (FR-BNK-16): after the account's latest statement that ends before this one starts, this one's opening
	// balance must be that one's closing balance; otherwise a gap is recorded. Overlapping statements are left to the
	// duplicate check.
	stmts, err := u.Banking().Statements(ctx, acct.ID)
	if err != nil {
		return out, err
	}
	var prev *storage.BankStatement
	overlap := false
	for i := range stmts {
		s := &stmts[i]
		if !s.PeriodTo.Before(st.From) {
			overlap = true
			continue
		}
		if s.Closing != nil && (prev == nil || s.PeriodTo.After(prev.PeriodTo)) {
			prev = s
		}
	}
	if !overlap && prev != nil && opening != nil && !sameAmount(*prev.Closing, *opening) {
		after := prev.PeriodTo
		out.Gap = &storage.NewBankGap{AfterDate: &after, BeforeDate: st.From, ExpectedOpening: prev.Closing, FoundOpening: dec(opening)}
	}

	n := storage.NewBankStatement{MoneyAccountID: acct.ID, Source: r.Source, Format: formatOf(r), ParserVersion: bankfile.Versions[r.Format],
		PeriodFrom: st.From, PeriodTo: st.To, Currency: acct.Currency, Opening: dec(opening), Closing: dec(st.Closing),
		FileRef: r.FileRef, ConnectorID: r.ConnectorID, Gap: out.Gap, At: r.At}
	if r.Source == storage.MoneySourceConnector {
		n.ParserVersion = "api@1"
	}
	var keep []int
	for i, il := range out.Lines {
		if il.Outcome == OutcomeDuplicate {
			continue
		}
		keep = append(keep, i)
		n.Lines = append(n.Lines, storage.NewBankLine{Position: il.Position, BookingDate: il.Booked, ValueDate: il.Value,
			Amount: storage.Decimal(il.Amount.String()), Reference: il.Reference, Counterparty: il.Counterparty, Description: il.Description,
			RunningBalance: dec(il.Balance), Fingerprint: Fingerprint(acct.ID, il.Line), DuplicateOf: duplicateOf(il)})
	}
	stmt, lines, err := u.Banking().Import(ctx, n)
	if err != nil {
		return out, err
	}
	out.Statement = &stmt
	for j, i := range keep {
		id := lines[j].ID
		out.Lines[i].ID = &id
	}
	return out, nil
}

func duplicateOf(il ImportedLine) *uuid.UUID {
	if il.Outcome == OutcomeFlag {
		return il.DuplicateOf
	}
	return nil
}

func formatOf(r ImportRequest) string {
	if r.Source == storage.MoneySourceConnector {
		return storage.FormatAPI
	}
	return r.Format
}

// transferOf is the transfer in transit this line may be the other side of: the same account and amount, the side
// still missing, within seven days of the transfer (FR-BNK-04: a transfer already recorded from the other account).
func transferOf(acct storage.MoneyAccount, l bankfile.Line, transit []storage.BankTransfer) *uuid.UUID {
	const window = 7 * 24 * time.Hour
	for _, t := range transit {
		d := l.Booked.Sub(t.OnDate)
		if d < -window || d > window {
			continue
		}
		in := t.ToAccountID == acct.ID && t.ToLineID == nil && l.Amount.Sign() > 0 && sameAmount(t.ToAmount, l.Amount)
		out := t.FromAccountID == acct.ID && t.FromLineID == nil && l.Amount.Sign() < 0 && sameAmount(t.FromAmount, l.Amount.Neg())
		if in || out {
			id := t.ID
			return &id
		}
	}
	return nil
}

// Freshness is how complete a money account's data is (FR-BNK-16): when its latest statement was imported, and the
// date its data is complete to, which an open gap holds back to the day before it.
type Freshness struct {
	LastImport *time.Time
	CompleteTo *time.Time
	OpenGaps   int
}

// FreshnessOf reads a money account's freshness in u.
func FreshnessOf(ctx context.Context, u storage.UnitOfWork, account uuid.UUID) (Freshness, error) {
	if _, err := u.Banking().MoneyAccount(ctx, account); errors.Is(err, ports.ErrNotFound) {
		return Freshness{}, refuse(CodeAccount, "Choose one of the organisation's money accounts.", "there is no such money account")
	} else if err != nil {
		return Freshness{}, err
	}
	stmts, err := u.Banking().Statements(ctx, account)
	if err != nil {
		return Freshness{}, err
	}
	gaps, err := u.Banking().Gaps(ctx, account, true)
	if err != nil {
		return Freshness{}, err
	}
	var f Freshness
	f.OpenGaps = len(gaps)
	for _, s := range stmts {
		if f.LastImport == nil || s.ImportedAt.After(*f.LastImport) {
			at := s.ImportedAt
			f.LastImport = &at
		}
		if f.CompleteTo == nil || s.PeriodTo.After(*f.CompleteTo) {
			to := s.PeriodTo
			f.CompleteTo = &to
		}
	}
	for _, g := range gaps {
		held := g.BeforeDate.AddDate(0, 0, -1)
		if g.AfterDate != nil {
			held = *g.AfterDate
		}
		if f.CompleteTo == nil || held.Before(*f.CompleteTo) {
			f.CompleteTo = &held
		}
	}
	return f, nil
}
