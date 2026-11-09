package gstmonitor

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/banktransaction"
	"github.com/cognizhi/fyniq/internal/domain/gst"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The GST F5 boxes of taxable supplies: IRAS's taxable supplies are the standard-rated (Box 1) and the zero-rated
// (Box 2) ones; exempt supplies (Box 3) and out-of-scope supplies (no box) are not taxable supplies.
const (
	BoxStandardRated = 1
	BoxZeroRated     = 2
)

// controls are the accounts beside a sales document's lines in its entry, which are not what it supplied.
var controls = []string{ledger.RoleAccountsReceivable, ledger.RoleGSTControl, ledger.RoleCustomerDeposits, ledger.RoleWithholdingTax, ledger.RoleFXRounding}

// Supply is what one entry of a document adds to the taxable supplies.
type Supply struct {
	DocumentID uuid.UUID
	Type       storage.DocumentType
	Number     string
	On         time.Time // the entry's posting date
	Amount     money.Money
	Reversal   bool // the entry reverses an earlier one, as a void or an edit does
}

// Supplies are the taxable supplies of a run of days, read at a ledger revision.
type Supplies struct {
	From, To time.Time
	Revision int64
	Total    money.Money
	Entries  []Supply
	// Unmatched are the documents whose entry's lines did not match their document's, line by line: their codes cannot
	// be read, so they are left out and named.
	Unmatched []uuid.UUID
}

// TaxableSupplies adds up the taxable supplies of the entries posted from from to to (design D3): the invoices, credit
// notes and bank transactions, line by line, each line coded for a supply the sg-gst version in force reports in Box 1
// or Box 2, on an account that is not an asset (a sale of a capital asset credits one, and is no taxable supply), at its
// base amount with a credit's sign turned. A reversal counts on its own day. Deposits count when the invoice that uses
// them posts. The posting date stands for the time of supply [accountant to confirm].
func TaxableSupplies(ctx context.Context, u storage.UnitOfWork, base money.Currency, from, to time.Time) (Supplies, error) {
	out := Supplies{From: from, To: to, Total: money.Zero(base)}
	rev, err := u.Ledger().Revision(ctx)
	if err != nil {
		return out, err
	}
	out.Revision = rev
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return out, err
	}
	asset, control := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, a := range accounts {
		asset[a.ID] = a.Type == storage.Asset
		control[a.ID] = slices.Contains(controls, string(a.Role))
	}
	versions := map[string]pack.Version{}
	codesOn := func(day time.Time) (pack.Version, error) {
		key := day.Format(time.DateOnly)
		if v, ok := versions[key]; ok {
			return v, nil
		}
		v, err := gst.Version(ctx, u, day)
		versions[key] = v
		return v, err
	}
	type docLine struct {
		account uuid.UUID
		code    string
	}
	unmatched := map[uuid.UUID]bool{}
	err = balance.EachEntry(ctx, u, rev, func(e storage.Entry) error {
		if e.PostingDate.Before(from) || e.PostingDate.After(to) {
			return nil
		}
		t := storage.DocumentType(e.Source.Type)
		if t != storage.DocumentInvoice && t != storage.DocumentCreditNote && t != storage.DocumentBankTransaction {
			return nil
		}
		docID, err := uuid.Parse(e.Source.ID)
		if err != nil {
			return err
		}
		doc, err := u.Documents().Version(ctx, docID, e.Source.Version)
		if err != nil {
			return err
		}
		var lines []docLine
		skip := uuid.Nil // a bank transaction's own money account
		if t == storage.DocumentBankTransaction {
			s, err := banktransaction.Read(doc, base)
			if err != nil {
				return err
			}
			skip = s.AccountID
			for _, p := range s.Parts {
				lines = append(lines, docLine{p.AccountID, p.GSTCode})
			}
		} else {
			d, err := invoice.Read(doc, base)
			if err != nil {
				return err
			}
			for _, l := range d.Lines {
				lines = append(lines, docLine{l.AccountID, l.GSTCode})
			}
		}
		var amounts []money.Money
		for _, l := range e.Lines {
			if control[l.AccountID] || l.AccountID == skip {
				continue
			}
			m, err := money.ParseStored(string(l.BaseAmount), base)
			if err != nil {
				return err
			}
			amounts = append(amounts, m.Neg()) // a supply is a credit
		}
		if len(amounts) != len(lines) {
			if !unmatched[docID] {
				unmatched[docID] = true
				out.Unmatched = append(out.Unmatched, docID)
			}
			return nil
		}
		codes, err := codesOn(e.PostingDate)
		if err != nil {
			return err
		}
		sum := money.Zero(base)
		for i, l := range lines {
			c, ok := pack.Code(codes, l.code)
			if !ok || c.Direction != pack.DirSupply || (c.ValueBox != BoxStandardRated && c.ValueBox != BoxZeroRated) || asset[l.account] {
				continue
			}
			if sum, err = sum.Add(amounts[i]); err != nil {
				return err
			}
		}
		if sum.IsZero() {
			return nil
		}
		if out.Total, err = out.Total.Add(sum); err != nil {
			return err
		}
		out.Entries = append(out.Entries, Supply{DocumentID: docID, Type: t, Number: doc.Number, On: e.PostingDate, Amount: sum, Reversal: e.ReversesEntryID != nil})
		return nil
	})
	return out, err
}
