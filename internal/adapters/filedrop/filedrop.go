// Package filedrop reads a bank's statement files from a drop folder as a bank-data source (PRD-001 FR-BNK-15, FR-BNK-17; p2-bank-feeds-payments task 2.1): each
// file is a statement in one of the formats Fyniq reads (MT940, camt.053, OFX), parsed by internal/bankfile, and its lines are the account's transactions after the
// last file taken in. It is read-only, like every bank-data connector (FR-INT-27): it lists and reads, and never writes to the folder.
package filedrop

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/bankfile"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
)

// MaxFileBytes is the most one statement file may be; a larger one stops the account's sync with a plain reason.
const MaxFileBytes = 10 << 20

// ErrEncrypted is a file the bank wrapped in PGP: decrypting needs the organisation's key and is not built (IDEAL Connect's wrapping is [verify with DBS]).
var ErrEncrypted = errors.New("the file is PGP-encrypted, which Fyniq cannot open yet: ask the bank for the files unencrypted over SFTP, or import them by hand")

// Statements is a bank-data source over a drop folder.
type Statements struct {
	Drop   ports.FileDrop
	Format string // bankfile.FormatMT940, FormatCAMT053 or FormatOFX; MT940 when empty
	// Intraday is the folder the bank leaves its MT942 intraday reports in, when it has one: its files are read as pending signals and never as statement lines.
	Intraday ports.FileDrop
}

var (
	_ ports.BankData    = Statements{}
	_ ports.BankReports = Statements{}
)

// MaxReports is the most intraday report files one call reads, newest last: the rest wait for the next call.
const MaxReports = 200

// IntradayReports are the intraday folder's files changed since the time, oldest first, whole; none when the bank has no such folder. A PGP-wrapped file stops the call with its reason.
func (s Statements) IntradayReports(ctx context.Context, since time.Time) ([]ports.BankReport, error) {
	if s.Intraday == nil {
		return nil, nil
	}
	all, err := s.Intraday.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].ModTime.Before(all[j].ModTime) || (all[i].ModTime.Equal(all[j].ModTime) && all[i].Name < all[j].Name)
	})
	var out []ports.BankReport
	for _, f := range all {
		if f.ModTime.Before(since) {
			continue
		}
		lower := strings.ToLower(f.Name)
		if strings.HasSuffix(lower, ".pgp") || strings.HasSuffix(lower, ".gpg") || strings.HasSuffix(lower, ".asc") {
			return nil, fmt.Errorf("%s: %w", f.Name, ErrEncrypted)
		}
		if len(out) == MaxReports {
			break
		}
		data, err := s.Intraday.Read(ctx, f.Name, MaxFileBytes)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		out = append(out, ports.BankReport{Name: f.Name, Data: data})
	}
	return out, nil
}

func (s Statements) format() string {
	if s.Format == "" {
		return bankfile.FormatMT940
	}
	return s.Format
}

// statementFiles are the folder's files in the order they are taken in: by name, which a bank dates, never by when they were written.
func (s Statements) statementFiles(ctx context.Context) ([]ports.DropFile, error) {
	all, err := s.Drop.List(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all, nil
}

func (s Statements) parse(ctx context.Context, f ports.DropFile) ([]bankfile.Statement, error) {
	lower := strings.ToLower(f.Name)
	if strings.HasSuffix(lower, ".pgp") || strings.HasSuffix(lower, ".gpg") || strings.HasSuffix(lower, ".asc") {
		return nil, fmt.Errorf("%s: %w", f.Name, ErrEncrypted)
	}
	data, err := s.Drop.Read(ctx, f.Name, MaxFileBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	st, err := bankfile.Parse(s.format(), data, nil, money.Currency{}, bankfile.DefaultLimits)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	return st, nil
}

// Accounts are the accounts the files name: the identifier each statement gives, in the currency it is in. A file that cannot be read stops the listing with its reason.
func (s Statements) Accounts(ctx context.Context) ([]ports.BankDataAccount, error) {
	files, err := s.statementFiles(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []ports.BankDataAccount
	for _, f := range files {
		sts, err := s.parse(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, st := range sts {
			if st.Account == "" || seen[st.Account] {
				continue
			}
			seen[st.Account] = true
			out = append(out, ports.BankDataAccount{ID: st.Account, Currency: st.Currency.Code, Name: st.Account})
		}
	}
	return out, nil
}

// Transactions are the account's lines in the files after the one the cursor names (the last file taken in), oldest first. A page is the account's lines of the first file
// that has any after the cursor, and More says later files wait; with no cursor, lines booked before from are left out. A line's ID is its bank reference, else the file
// position and line, so a file taken in twice is a duplicate of itself, not new lines.
func (s Statements) Transactions(ctx context.Context, account, cursor string, from time.Time) (ports.BankDataPage, error) {
	files, err := s.statementFiles(ctx)
	if err != nil {
		return ports.BankDataPage{}, err
	}
	var rest []ports.DropFile
	for _, f := range files {
		if cursor == "" || f.Name > cursor {
			rest = append(rest, f)
		}
	}
	for i, f := range rest {
		sts, err := s.parse(ctx, f)
		if err != nil {
			return ports.BankDataPage{}, err
		}
		var page []ports.BankDataTransaction
		for _, st := range sts {
			if st.Account != account {
				continue
			}
			for _, l := range st.Lines {
				if cursor == "" && !from.IsZero() && l.Booked.Before(from) {
					continue
				}
				id := l.Reference
				if id == "" {
					id = fmt.Sprintf("%s#%d", f.Name, l.Position)
				}
				t := ports.BankDataTransaction{ID: id, BookingDate: l.Booked, ValueDate: l.Value, Amount: l.Amount.String(), Currency: st.Currency.Code,
					Counterparty: l.Counterparty, Description: l.Description}
				if l.Balance != nil {
					t.RunningBalance = l.Balance.String()
				}
				page = append(page, t)
			}
		}
		if len(page) > 0 || i == len(rest)-1 {
			return ports.BankDataPage{Transactions: page, Cursor: f.Name, More: i < len(rest)-1}, nil
		}
	}
	return ports.BankDataPage{Cursor: cursor}, nil
}
