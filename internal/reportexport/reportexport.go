// Package reportexport prepares the exports that run in the background (FR-RPT-09, FR-RPT-10; p1-reports tasks 3.3
// and 3.4, design D5): the accountant exports, the general ledger, the trial balance, the journals, the fixed asset
// schedule, and the receivables and payables in detail, as CSV or XLSX. A person asks for one through the API; the
// worker reads the books, writes the file to the blob store under exports/<organisation>/<export>/ and marks the export
// ready, with the SHA-256 and the ledger revision it read at, until it expires 24 hours later; then tells the person.
// An export the books refuse (a range too long for the general ledger, or across a year's start) fails with why. A job
// deletes expired files every hour.
package reportexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/sheet"
)

// Kind is the outbox kind of preparing an export.
const Kind = "reports.export"

// Lifetime is how long a ready export can be downloaded (FR-RPT-09: 24 hours, a setting to come).
const Lifetime = 24 * time.Hour

// Actor is the principal the worker prepares and expires exports as.
var Actor = storage.Actor{Type: "system", ID: "reports.export"}

// The reports, as the API names them.
const (
	GeneralLedger = "general_ledger"
	TrialBalance  = "trial_balance"
	Journals      = "journals"
	FixedAssets   = "fixed_assets"
	Receivables   = "receivables"
	Payables      = "payables"
)

// Reports are the accountant exports and whether each runs over a range of days (else it is as at a day).
var Reports = map[string]bool{GeneralLedger: true, TrialBalance: false, Journals: true, FixedAssets: false, Receivables: false, Payables: false}

// Names are the reports' names, as a file and a sheet are titled.
var Names = map[string]string{GeneralLedger: "General ledger", TrialBalance: "Trial balance", Journals: "Journal entries",
	FixedAssets: "Fixed asset schedule", Receivables: "Receivables in detail", Payables: "Payables in detail"}

// Parameters are an export's: the first day of a range, and its last day or the day it is as at, YYYY-MM-DD.
type Parameters struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
}

// Refusal is an export the books refuse: it fails with why, and is not tried again.
type Refusal struct{ Why string }

func (r Refusal) Error() string { return r.Why }

// File is a prepared export.
type File struct {
	Name, MediaType string
	Data            []byte
	Rows            int
	Revision        int64
}

// Key is where an export's file is kept in the blob store.
func Key(org, id uuid.UUID, name string) string {
	return "exports/" + org.String() + "/" + id.String() + "/" + name
}

// Queue asks the worker to prepare x.
func Queue(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, x storage.ReportExport) error {
	payload, err := json.Marshal(map[string]string{"export_id": x.ID.String()})
	if err != nil {
		return err
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: payload, BusinessKey: "export:" + x.ID.String()})
	return err
}

// day is a parameter's day.
func day(s string) (time.Time, error) { return time.Parse(time.DateOnly, s) }

// Build reads the books for an export and writes its file.
func Build(ctx context.Context, u storage.UnitOfWork, x storage.ReportExport, created time.Time) (File, error) {
	var p Parameters
	if err := json.Unmarshal(x.Parameters, &p); err != nil {
		return File{}, Refusal{"its parameters are not readable"}
	}
	to, err := day(p.To)
	if err != nil {
		return File{}, Refusal{"its last day is not a date"}
	}
	from := to
	if Reports[x.Report] {
		if from, err = day(p.From); err != nil || from.After(to) {
			return File{}, Refusal{"its first day is not a date on or before its last"}
		}
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return File{}, err
	}
	base, err := money.Lookup(org.BaseCurrency)
	if err != nil {
		return File{}, err
	}
	revision, err := u.Ledger().Revision(ctx)
	if err != nil {
		return File{}, err
	}
	var sheets []sheet.Sheet
	switch x.Report {
	case GeneralLedger:
		sheets, err = generalLedger(ctx, u, base, from, to)
	case TrialBalance:
		sheets, err = trialBalance(ctx, u, base, to, revision)
	case Journals:
		sheets, err = journals(ctx, u, base, from, to, revision)
	case FixedAssets:
		sheets, err = fixedAssets(ctx, u, base, to)
	case Receivables, Payables:
		sheets, err = subLedger(ctx, u, base, x.Report, to)
	default:
		return File{}, Refusal{fmt.Sprintf("%q is not a report that exports", x.Report)}
	}
	if err != nil {
		var tooMany *balance.TooMany
		var crosses *balance.CrossesYear
		switch {
		case errors.As(err, &tooMany):
			return File{}, Refusal{"an account has more lines in the range than one export holds: export a shorter range"}
		case errors.As(err, &crosses):
			return File{}, Refusal{"the range crosses the start of a financial year: export each year apart"}
		}
		return File{}, err
	}
	rows := 0
	for _, s := range sheets {
		rows += len(s.Rows)
	}
	name := strings.ReplaceAll(strings.ToLower(Names[x.Report]), " ", "-")
	if Reports[x.Report] {
		name += "-" + p.From + "-to-" + p.To
	} else {
		name += "-" + p.To
	}
	if x.Format == "csv" {
		return File{Name: name + ".csv", MediaType: "text/csv; charset=utf-8", Data: sheet.CSV(sheets[0]), Rows: rows, Revision: revision}, nil
	}
	data, err := sheet.XLSX(sheets, created)
	if err != nil {
		return File{}, err
	}
	return File{Name: name + ".xlsx", MediaType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Data: data, Rows: rows,
		Revision: revision}, nil
}

// num is an amount as a number cell.
func num(m money.Money) sheet.Cell { return sheet.Number(m.String(), m.Currency().MinorUnits) }

// debitCredit are an amount's debit and credit cells: the debit when it is positive, the credit when negative.
func debitCredit(m money.Money) (sheet.Cell, sheet.Cell) {
	switch m.Sign() {
	case 1:
		return num(m), sheet.Text("")
	case -1:
		return sheet.Text(""), num(m.Neg())
	}
	return sheet.Text(""), sheet.Text("")
}

// postable are the accounts that take lines, by code.
func postable(ctx context.Context, u storage.UnitOfWork) ([]storage.Account, error) {
	all, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	out := slices.DeleteFunc(all, func(a storage.Account) bool { return a.Group })
	slices.SortFunc(out, func(a, b storage.Account) int { return strings.Compare(a.Code, b.Code) })
	return out, nil
}

// generalLedger is every account's lines over the range with the running balance (FR-GL-11), an account with nothing
// in or before the range left out.
func generalLedger(ctx context.Context, u storage.UnitOfWork, base money.Currency, from, to time.Time) ([]sheet.Sheet, error) {
	accounts, err := postable(ctx, u)
	if err != nil {
		return nil, err
	}
	s := sheet.Sheet{Name: Names[GeneralLedger], Header: []string{"Account", "Name", "Date", "Number", "Source", "Memo", "Description", "Debit",
		"Credit", "Balance " + base.Code}}
	for _, a := range accounts {
		gl, err := balance.GeneralLedger(ctx, u, base, a.ID, from, to)
		if err != nil {
			return nil, err
		}
		if len(gl.Lines) == 0 && gl.Opening.IsZero() {
			continue
		}
		head := []sheet.Cell{sheet.Text(a.Code), sheet.Text(a.Name)}
		s.Rows = append(s.Rows, sheet.Row{Cells: append(slices.Clone(head), sheet.Text(from.Format(time.DateOnly)), sheet.Text(""), sheet.Text(""),
			sheet.Text("Opening balance"), sheet.Text(""), sheet.Text(""), sheet.Text(""), num(gl.Opening))})
		for _, l := range gl.Lines {
			dr, cr := debitCredit(l.Base)
			s.Rows = append(s.Rows, sheet.Row{Cells: append(slices.Clone(head), sheet.Text(l.PostingDate.Format(time.DateOnly)), sheet.Text(l.Number),
				sheet.Text(l.Source.Type), sheet.Text(l.Memo), sheet.Text(l.Description), dr, cr, num(l.Balance))})
		}
		s.Rows = append(s.Rows, sheet.Row{Strong: true, Cells: append(slices.Clone(head), sheet.Text(to.Format(time.DateOnly)), sheet.Text(""),
			sheet.Text(""), sheet.Text("Closing balance"), sheet.Text(""), num(gl.Debits), num(gl.Credits), num(gl.Closing))})
	}
	return []sheet.Sheet{s}, nil
}

// trialBalance is every account's balance at the end of the month that holds to, debits and credits, read at revision.
func trialBalance(ctx context.Context, u storage.UnitOfWork, base money.Currency, to time.Time, revision int64) ([]sheet.Sheet, error) {
	all, err := u.Periods().List(ctx)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(all, func(p storage.Period) bool { return !p.StartsOn.After(to) && !p.EndsOn.Before(to) })
	if i < 0 {
		return nil, Refusal{"the books have no period for its day"}
	}
	st, err := financials.Lay(ctx, u, base, financials.Request{Kind: financials.TrialBalance, All: all, First: i, Last: i, Revision: revision})
	if err != nil {
		return nil, err
	}
	s := sheet.Sheet{Name: Names[TrialBalance], Header: []string{"Code", "Account", "Type", "Debit " + base.Code, "Credit " + base.Code}}
	debits, credits := money.Zero(base), money.Zero(base)
	for _, b := range st.Blocks {
		for _, g := range b.Groups {
			for _, l := range g.Lines {
				m := l.Amounts[0]
				dr, cr := debitCredit(m)
				var err error
				if m.Sign() > 0 {
					debits, err = debits.Add(m)
				} else {
					credits, err = credits.Add(m.Neg())
				}
				if err != nil {
					return nil, err
				}
				s.Rows = append(s.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text(l.Account.Code), sheet.Text(l.Account.Name), sheet.Text(string(l.Account.Type)), dr, cr}})
			}
		}
	}
	s.Rows = append(s.Rows, sheet.Row{Strong: true, Cells: []sheet.Cell{sheet.Text(""), sheet.Text("Total, at " + all[i].EndsOn.Format(time.DateOnly)),
		sheet.Text(""), num(debits), num(credits)}})
	return []sheet.Sheet{s}, nil
}

// journals is every entry posted in the range, read at revision, a row per line: the day book an auditor reads.
func journals(ctx context.Context, u storage.UnitOfWork, base money.Currency, from, to time.Time, revision int64) ([]sheet.Sheet, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]storage.Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}
	numbers := map[string]string{}
	number := func(src storage.Source) (string, error) {
		if n, ok := numbers[src.ID]; ok {
			return n, nil
		}
		id, err := uuid.Parse(src.ID)
		if err != nil {
			return "", nil
		}
		doc, err := u.Documents().Get(ctx, id)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return "", err
		}
		numbers[src.ID] = doc.Number
		return doc.Number, nil
	}
	s := sheet.Sheet{Name: Names[Journals], Header: []string{"Date", "Revision", "Source", "Number", "Memo", "Code", "Account", "Description", "Debit " + base.Code,
		"Credit " + base.Code}}
	err = balance.EachEntry(ctx, u, revision, func(e storage.Entry) error {
		if e.PostingDate.Before(from) || e.PostingDate.After(to) {
			return nil
		}
		n, err := number(e.Source)
		if err != nil {
			return err
		}
		for _, l := range e.Lines {
			b, err := money.ParseStored(string(l.BaseAmount), base)
			if err != nil {
				return err
			}
			dr, cr := debitCredit(b)
			a := byID[l.AccountID]
			s.Rows = append(s.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text(e.PostingDate.Format(time.DateOnly)), sheet.Number(fmt.Sprint(e.LedgerRevision), 0),
				sheet.Text(e.Source.Type), sheet.Text(n), sheet.Text(e.Memo), sheet.Text(a.Code), sheet.Text(a.Name), sheet.Text(l.Description), dr, cr}})
		}
		return nil
	})
	return []sheet.Sheet{s}, err
}

// fixedAssets is the register as at a day (FR-FA-01): each asset's cost, accumulated depreciation and net book value.
func fixedAssets(ctx context.Context, u storage.UnitOfWork, base money.Currency, asAt time.Time) ([]sheet.Sheet, error) {
	figures, err := journal.AssetFiguresAt(ctx, u, base, asAt)
	if err != nil {
		return nil, err
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]storage.Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}
	s := sheet.Sheet{Name: Names[FixedAssets], Header: []string{"Number", "Account", "Cost " + base.Code, "Accumulated depreciation " + base.Code,
		"Net book value " + base.Code}}
	cost, acc, nbv := money.Zero(base), money.Zero(base), money.Zero(base)
	for _, f := range figures {
		net, err := f.Cost.Sub(f.Accumulated)
		if err != nil {
			return nil, err
		}
		a := byID[f.AccountID]
		s.Rows = append(s.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text(f.Asset.Number), sheet.Text(a.Code + " " + a.Name), num(f.Cost), num(f.Accumulated), num(net)}})
		if cost, err = cost.Add(f.Cost); err != nil {
			return nil, err
		}
		if acc, err = acc.Add(f.Accumulated); err != nil {
			return nil, err
		}
		if nbv, err = nbv.Add(net); err != nil {
			return nil, err
		}
	}
	s.Rows = append(s.Rows, sheet.Row{Strong: true, Cells: []sheet.Cell{sheet.Text(""), sheet.Text("Total, at " + asAt.Format(time.DateOnly)), num(cost),
		num(acc), num(nbv)}})
	return []sheet.Sheet{s}, nil
}

// subLedger is the receivables or payables in detail as at a day (INV-07): each open document and credit by party, in
// its currency and at its carrying amount.
func subLedger(ctx context.Context, u storage.UnitOfWork, base money.Currency, report string, asAt time.Time) ([]sheet.Sheet, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	role, party := ledger.RoleAccountsReceivable, "Customer"
	if report == Payables {
		role, party = ledger.RoleAccountsPayable, "Supplier"
	}
	var control *storage.Account
	for i := range accounts {
		if string(accounts[i].Role) == role {
			control = &accounts[i]
		}
	}
	s := sheet.Sheet{Name: Names[report], Header: []string{party, "Kind", "Number", "Date", "Due", "Currency", "Total", "Open", "Carrying " + base.Code}}
	if control == nil {
		return []sheet.Sheet{s}, nil
	}
	var groups []subledger.Group
	if report == Payables {
		l, err := payables.Open(ctx, u, base, control.ID, asAt)
		if err != nil {
			return nil, err
		}
		for _, x := range l.Suppliers {
			groups = append(groups, subledger.Group{ID: x.ID, Name: x.Name, Items: x.Items})
		}
	} else {
		l, err := receivables.Open(ctx, u, base, control.ID, asAt)
		if err != nil {
			return nil, err
		}
		for _, x := range l.Customers {
			groups = append(groups, subledger.Group{ID: x.ID, Name: x.Name, Items: x.Items})
		}
	}
	for _, g := range groups {
		for _, it := range g.Items {
			due := ""
			if !it.DueDate.IsZero() {
				due = it.DueDate.Format(time.DateOnly)
			}
			s.Rows = append(s.Rows, sheet.Row{Cells: []sheet.Cell{sheet.Text(g.Name), sheet.Text(it.Kind), sheet.Text(it.Number), sheet.Text(it.Date.Format(time.DateOnly)),
				sheet.Text(due), sheet.Text(it.Open.Currency().Code), num(it.Total), num(it.Open), num(it.Carrying)}})
		}
	}
	return []sheet.Sheet{s}, nil
}

// Preparer prepares and expires exports, in the worker.
type Preparer struct {
	Store  storage.Store
	Files  ports.BlobStore
	Outbox outbox.Dispatcher
	Clock  ports.Clock
}

// Work prepares one export: a pending one is built, its file kept, and it is marked ready and its requester told; one
// the books refuse fails with why. One that is no longer pending needs nothing.
func (p Preparer) Work() func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var job struct {
			ID uuid.UUID `json:"export_id"`
		}
		if err := json.Unmarshal(row.Payload, &job); err != nil {
			return err
		}
		ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor})
		now := p.Clock.Now()
		var x storage.ReportExport
		var f File
		var refused error
		err := p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			if x, err = u.ReportExports().Get(ctx, job.ID); err != nil || x.Status != storage.ExportPending {
				return err
			}
			f, err = Build(ctx, u, x, now)
			if r := (Refusal{}); errors.As(err, &r) {
				refused, err = r, nil
			}
			return err
		})
		if err != nil || x.Status != storage.ExportPending {
			return err
		}
		if refused == nil {
			if p.Files == nil {
				return errors.New("reportexport: no blob store is configured")
			}
			if err := p.Files.Put(ctx, Key(org, x.ID, f.Name), bytes.NewReader(f.Data)); err != nil {
				return err
			}
		}
		return p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			title, link := "Your export is ready: "+f.Name, "/reports"
			if refused != nil {
				if _, err := u.ReportExports().Fail(ctx, x.ID, refused.Error()); err != nil {
					return err
				}
				title = "Your export of the " + strings.ToLower(Names[x.Report]) + " could not be made: " + refused.Error()
			} else {
				sum := sha256.Sum256(f.Data)
				if _, err := u.ReportExports().Ready(ctx, x.ID, storage.ReportFileDone{Revision: f.Revision, FileName: f.Name, MediaType: f.MediaType,
					Size: int64(len(f.Data)), Rows: int64(f.Rows), SHA256: sum[:], ReadyAt: now, ExpiresAt: now.Add(Lifetime)}); err != nil {
					return err
				}
			}
			user, err := uuid.Parse(strings.TrimPrefix(x.RequestedBy, "user:"))
			if err != nil {
				return nil // asked for by no person: nobody to tell
			}
			_, err = notify.Emit(ctx, p.Outbox, u, org, notify.Event{Type: notify.ExportReadyType, RecordType: "report_export", RecordID: &x.ID, At: now,
				Title: title, Link: link, Audience: notify.Audience{Users: []uuid.UUID{user}}})
			return err
		})
	}
}

// Expire deletes the files of every organisation's exports whose time is up by now and marks them expired, returning
// how many.
func (p Preparer) Expire(ctx context.Context, now time.Time) (int, error) {
	orgs, err := p.Store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor})
	n := 0
	for _, org := range orgs {
		err := p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			xs, err := u.ReportExports().Expiring(ctx, now, 500)
			if err != nil {
				return err
			}
			for _, x := range xs {
				if p.Files != nil && x.File != nil {
					if err := p.Files.Delete(ctx, Key(org, x.ID, x.File.FileName)); err != nil {
						return err
					}
				}
				if err := u.ReportExports().Expire(ctx, x.ID); err != nil {
					return err
				}
				n++
			}
			return nil
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
