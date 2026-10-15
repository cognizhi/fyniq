package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// unitOfWork holds one transaction's queries. Repositories reach the database only through q, which
// can run statements but cannot begin, commit or roll back (NFR-PORT-07).
type unitOfWork struct {
	q           *sqlcdb.Queries
	store       *Store
	org         uuid.UUID
	pending     []audit.Event // written just before commit (flush)
	afterCommit []func(ctx context.Context)
	done        bool
}

var _ storage.UnitOfWork = (*unitOfWork)(nil)

func (u *unitOfWork) check() error {
	if u.done {
		return &storage.Error{Code: storage.CodeInvalid, Message: "the unit of work has ended; run the work inside Store.Do"}
	}
	return nil
}

func (u *unitOfWork) Organisation(ctx context.Context) (storage.Organisation, error) {
	if err := u.check(); err != nil {
		return storage.Organisation{}, err
	}
	row, err := u.q.GetOrganisation(ctx)
	if err != nil {
		return storage.Organisation{}, mapError(err)
	}
	return storage.Organisation{ID: row.OrganisationID, Name: row.Name, BaseCurrency: row.BaseCurrency,
		BaseMinorUnits: int(row.BaseMinorUnits), Version: int(row.Version)}, nil
}

func (u *unitOfWork) Accounts() storage.Accounts   { return accounts{u} }
func (u *unitOfWork) Periods() storage.Periods     { return periods{u} }
func (u *unitOfWork) Documents() storage.Documents { return documents{u} }
func (u *unitOfWork) Ledger() storage.Ledger       { return ledger{u} }
func (u *unitOfWork) Balances() storage.Balances   { return balances{u} }

// ------------------------------------------------------------------ accounts

type accounts struct{ u *unitOfWork }

func toAccount(id uuid.UUID, code, name, typ string, parent *uuid.UUID, group, active bool, version int32) storage.Account {
	return storage.Account{ID: id, Code: code, Name: name, Type: storage.AccountType(typ), ParentID: parent,
		Group: group, Active: active, Version: int(version)}
}

func (r accounts) Create(ctx context.Context, a storage.NewAccount) (storage.Account, error) {
	if err := r.u.check(); err != nil {
		return storage.Account{}, err
	}
	row, err := r.u.q.InsertAccount(ctx, sqlcdb.InsertAccountParams{Code: a.Code, Name: a.Name, Type: string(a.Type),
		ParentID: a.ParentID, IsGroup: a.Group})
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	v := int(row.Version)
	if err := r.u.event(ctx, "account.created", "account", row.ID, &v, map[string][2]any{
		"code": {nil, row.Code}, "name": {nil, row.Name}, "type": {nil, row.Type},
		"parent_id": {nil, row.ParentID}, "is_group": {nil, row.IsGroup}, "is_active": {nil, row.IsActive}}); err != nil {
		return storage.Account{}, err
	}
	return toAccount(row.ID, row.Code, row.Name, row.Type, row.ParentID, row.IsGroup, row.IsActive, row.Version), nil
}

func (r accounts) Get(ctx context.Context, id uuid.UUID) (storage.Account, error) {
	if err := r.u.check(); err != nil {
		return storage.Account{}, err
	}
	row, err := r.u.q.GetAccount(ctx, id)
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	return toAccount(row.ID, row.Code, row.Name, row.Type, row.ParentID, row.IsGroup, row.IsActive, row.Version), nil
}

func (r accounts) ByCode(ctx context.Context, code string) (storage.Account, error) {
	if err := r.u.check(); err != nil {
		return storage.Account{}, err
	}
	row, err := r.u.q.GetAccountByCode(ctx, code)
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	return toAccount(row.ID, row.Code, row.Name, row.Type, row.ParentID, row.IsGroup, row.IsActive, row.Version), nil
}

func (r accounts) SetActive(ctx context.Context, id uuid.UUID, active bool) (storage.Account, error) {
	if err := r.u.check(); err != nil {
		return storage.Account{}, err
	}
	before, err := r.u.q.GetAccount(ctx, id)
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	row, err := r.u.q.SetAccountActive(ctx, sqlcdb.SetAccountActiveParams{ID: id, IsActive: active})
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	action, v := "account.deactivated", int(row.Version)
	if active {
		action = "account.activated"
	}
	if err := r.u.event(ctx, action, "account", row.ID, &v, map[string][2]any{
		"is_active": {before.IsActive, row.IsActive}, "version": {before.Version, row.Version}}); err != nil {
		return storage.Account{}, err
	}
	return toAccount(row.ID, row.Code, row.Name, row.Type, row.ParentID, row.IsGroup, row.IsActive, row.Version), nil
}

func (r accounts) List(ctx context.Context) ([]storage.Account, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListAccounts(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Account, 0, len(rows))
	for _, a := range rows {
		out = append(out, toAccount(a.ID, a.Code, a.Name, a.Type, a.ParentID, a.IsGroup, a.IsActive, a.Version))
	}
	return out, nil
}

// ------------------------------------------------------------------ documents

type documents struct{ u *unitOfWork }

func (r documents) Create(ctx context.Context, d storage.NewDocument) (storage.Document, error) {
	if err := r.u.check(); err != nil {
		return storage.Document{}, err
	}
	row, err := r.u.q.InsertDocument(ctx, string(d.Type))
	if err != nil {
		return storage.Document{}, mapError(err)
	}
	if err := r.u.q.InsertDocumentVersion(ctx, sqlcdb.InsertDocumentVersionParams{DocumentID: row.ID, Version: row.CurrentVersion, Payload: d.Payload}); err != nil {
		return storage.Document{}, mapError(err)
	}
	payload, err := jsonValue(d.Payload)
	if err != nil {
		return storage.Document{}, err
	}
	v := int(row.CurrentVersion)
	if err := r.u.event(ctx, "document.created", "document", row.ID, &v, map[string][2]any{
		"type": {nil, row.Type}, "status": {nil, row.Status}, "payload": {nil, payload}}); err != nil {
		return storage.Document{}, err
	}
	return storage.Document{ID: row.ID, Type: storage.DocumentType(row.Type), Status: storage.DocumentStatus(row.Status),
		Version: int(row.CurrentVersion), Payload: d.Payload}, nil
}

func (r documents) Get(ctx context.Context, id uuid.UUID) (storage.Document, error) {
	if err := r.u.check(); err != nil {
		return storage.Document{}, err
	}
	row, err := r.u.q.GetDocument(ctx, id)
	if err != nil {
		return storage.Document{}, mapError(err)
	}
	return storage.Document{ID: row.ID, Type: storage.DocumentType(row.Type), Status: storage.DocumentStatus(row.Status),
		Version: int(row.CurrentVersion), Payload: row.Payload}, nil
}

func (r documents) Lock(ctx context.Context, id uuid.UUID) (storage.Document, error) {
	if err := r.u.check(); err != nil {
		return storage.Document{}, err
	}
	row, err := r.u.q.LockDocument(ctx, id)
	if err != nil {
		return storage.Document{}, mapError(err)
	}
	return storage.Document{ID: row.ID, Type: storage.DocumentType(row.Type), Status: storage.DocumentStatus(row.Status),
		Version: int(row.CurrentVersion), Payload: row.Payload}, nil
}

func (r documents) SetStatus(ctx context.Context, id uuid.UUID, to storage.DocumentStatus) error {
	if err := r.u.check(); err != nil {
		return err
	}
	before, err := r.u.q.GetDocumentStatusForUpdate(ctx, id)
	if err != nil {
		return mapError(err)
	}
	if err := r.u.q.SetDocumentStatus(ctx, sqlcdb.SetDocumentStatusParams{ID: id, Status: string(to)}); err != nil {
		return mapError(err)
	}
	return r.u.event(ctx, "document."+string(to), "document", id, nil, map[string][2]any{"status": {before, string(to)}})
}

// ------------------------------------------------------------------ periods

type periods struct{ u *unitOfWork }

func toPeriod(id uuid.UUID, starts, ends time.Time, status string, closed *int64, version int32) storage.Period {
	return storage.Period{ID: id, StartsOn: starts, EndsOn: ends, Status: storage.PeriodStatus(status),
		ClosedRevision: closed, Version: int(version)}
}

func (r periods) Create(ctx context.Context, p storage.NewPeriod) (storage.Period, error) {
	if err := r.u.check(); err != nil {
		return storage.Period{}, err
	}
	row, err := r.u.q.InsertPeriod(ctx, sqlcdb.InsertPeriodParams{StartsOn: dateOf(p.StartsOn), EndsOn: dateOf(p.EndsOn)})
	if err != nil {
		return storage.Period{}, mapError(err)
	}
	v := int(row.Version)
	if err := r.u.event(ctx, "period.created", "period", row.ID, &v, map[string][2]any{
		"starts_on": {nil, day(row.StartsOn)}, "ends_on": {nil, day(row.EndsOn)}, "status": {nil, row.Status}}); err != nil {
		return storage.Period{}, err
	}
	return toPeriod(row.ID, row.StartsOn, row.EndsOn, row.Status, row.ClosedRevision, row.Version), nil
}

func (r periods) Get(ctx context.Context, id uuid.UUID) (storage.Period, error) {
	if err := r.u.check(); err != nil {
		return storage.Period{}, err
	}
	row, err := r.u.q.GetPeriod(ctx, id)
	if err != nil {
		return storage.Period{}, mapError(err)
	}
	return toPeriod(row.ID, row.StartsOn, row.EndsOn, row.Status, row.ClosedRevision, row.Version), nil
}

// Close and Reopen follow ENG-47: the ledger lock first, then the period FOR UPDATE, then the status.
// A posting holds the same ledger lock until it commits, so the two never interleave.
func (r periods) Close(ctx context.Context, id uuid.UUID) (storage.Period, error) {
	return r.setStatus(ctx, id, storage.PeriodClosed)
}

func (r periods) Reopen(ctx context.Context, id uuid.UUID) (storage.Period, error) {
	return r.setStatus(ctx, id, storage.PeriodOpen)
}

func (r periods) setStatus(ctx context.Context, id uuid.UUID, to storage.PeriodStatus) (storage.Period, error) {
	if err := r.u.check(); err != nil {
		return storage.Period{}, err
	}
	if _, err := r.u.q.LockLedger(ctx); err != nil {
		return storage.Period{}, mapError(err)
	}
	cur, err := r.u.q.LockPeriod(ctx, id)
	if err != nil {
		return storage.Period{}, mapError(err)
	}
	switch {
	case to == storage.PeriodClosed && cur.Status == string(storage.PeriodClosed):
		return storage.Period{}, &storage.Error{Code: storage.CodePeriodState,
			Message: fmt.Sprintf("the period %s to %s is already closed", day(cur.StartsOn), day(cur.EndsOn))}
	case to == storage.PeriodOpen && cur.Status != string(storage.PeriodClosed):
		return storage.Period{}, &storage.Error{Code: storage.CodePeriodState,
			Message: fmt.Sprintf("the period %s to %s is not closed, so it cannot be reopened", day(cur.StartsOn), day(cur.EndsOn))}
	}
	row, err := r.u.q.SetPeriodStatus(ctx, sqlcdb.SetPeriodStatusParams{ID: id, Status: string(to)})
	if err != nil {
		return storage.Period{}, mapError(err)
	}
	action, v := "period.closed", int(row.Version)
	if to == storage.PeriodOpen {
		action = "period.reopened"
	}
	if err := r.u.event(ctx, action, "period", row.ID, &v, map[string][2]any{
		"status": {cur.Status, row.Status}, "closed_revision": {cur.ClosedRevision, row.ClosedRevision}}); err != nil {
		return storage.Period{}, err
	}
	return toPeriod(row.ID, row.StartsOn, row.EndsOn, row.Status, row.ClosedRevision, row.Version), nil
}

// ------------------------------------------------------------------ ledger

type ledger struct{ u *unitOfWork }

func (r ledger) Append(ctx context.Context, e storage.NewEntry) (storage.Entry, error) {
	if err := r.u.check(); err != nil {
		return storage.Entry{}, err
	}
	p := sqlcdb.InsertLinesParams{}
	for i, l := range e.Lines {
		for _, f := range []struct {
			name string
			d    storage.Decimal
		}{{"amount", l.Amount}, {"fx_rate", l.FXRate}, {"base_amount", l.BaseAmount}} {
			if !f.d.Valid() {
				return storage.Entry{}, &storage.Error{Code: storage.CodeInvalid,
					Message: fmt.Sprintf("line %d: %s %q is not a plain decimal", i+1, f.name, string(f.d)),
					Hint:    "Send digits with an optional minus and decimal point, such as -1234.50"}
			}
		}
		p.LineNos = append(p.LineNos, int32(i+1))
		p.AccountIds = append(p.AccountIds, l.AccountID)
		p.Currencies = append(p.Currencies, l.Currency)
		p.Amounts = append(p.Amounts, string(l.Amount))
		p.FxRates = append(p.FxRates, string(l.FXRate))
		p.FxRateSources = append(p.FxRateSources, l.FXRateSource)
		p.BaseAmounts = append(p.BaseAmounts, string(l.BaseAmount))
		p.Descriptions = append(p.Descriptions, l.Description)
	}
	var memo *string
	if e.Memo != "" {
		memo = &e.Memo
	}
	row, err := r.u.q.InsertEntry(ctx, sqlcdb.InsertEntryParams{
		PostingDate: dateOf(e.PostingDate), PostingIdentity: e.PostingIdentity,
		SourceType: e.Source.Type, SourceID: e.Source.ID, SourceVersion: int32(e.Source.Version),
		RuleVersion: e.RuleVersion, ReversesEntryID: e.ReversesEntryID, Memo: memo,
	})
	if err != nil {
		return storage.Entry{}, mapError(err)
	}
	p.EntryID = row.ID
	if len(e.Lines) > 0 {
		if err := r.u.q.InsertLines(ctx, p); err != nil {
			return storage.Entry{}, mapError(err)
		}
	}
	posted, err := r.Entry(ctx, row.ID)
	if err != nil {
		return storage.Entry{}, err
	}
	lines := make([]any, len(posted.Lines))
	for i, l := range posted.Lines {
		lines[i] = map[string]any{"line_no": l.LineNo, "account_id": l.AccountID.String(), "currency": l.Currency,
			"amount": string(l.Amount), "fx_rate": string(l.FXRate), "fx_rate_source": l.FXRateSource,
			"base_currency": l.BaseCurrency, "base_amount": string(l.BaseAmount), "description": l.Description}
	}
	var reverses any
	if posted.ReversesEntryID != nil {
		reverses = posted.ReversesEntryID.String()
	}
	if err := r.u.event(ctx, "journal_entry.posted", "journal_entry", posted.ID, nil, map[string][2]any{
		"posting_identity": {nil, posted.PostingIdentity}, "posting_date": {nil, day(posted.PostingDate)},
		"period_id": {nil, posted.PeriodID.String()}, "ledger_revision": {nil, posted.LedgerRevision},
		"source":       {nil, map[string]any{"type": posted.Source.Type, "id": posted.Source.ID, "version": posted.Source.Version}},
		"rule_version": {nil, posted.RuleVersion}, "reverses_entry_id": {nil, reverses}, "memo": {nil, posted.Memo},
		"lines": {nil, lines}}); err != nil {
		return storage.Entry{}, err
	}
	return posted, nil
}

func (r ledger) Entry(ctx context.Context, id uuid.UUID) (storage.Entry, error) {
	if err := r.u.check(); err != nil {
		return storage.Entry{}, err
	}
	row, err := r.u.q.GetEntry(ctx, id)
	if err != nil {
		return storage.Entry{}, mapError(err)
	}
	e := storage.Entry{ID: row.ID, LedgerRevision: row.LedgerRevision, PostingDate: row.PostingDate, PeriodID: row.PeriodID,
		PostingIdentity: row.PostingIdentity, Source: storage.Source{Type: row.SourceType, ID: row.SourceID, Version: int(row.SourceVersion)},
		RuleVersion: row.RuleVersion, ReversesEntryID: row.ReversesEntryID, RecordedAt: row.RecordedAt}
	if row.Memo != nil {
		e.Memo = *row.Memo
	}
	lines, err := r.u.q.GetEntryLines(ctx, id)
	if err != nil {
		return storage.Entry{}, mapError(err)
	}
	for _, l := range lines {
		line := storage.Line{ID: l.ID, LineNo: int(l.LineNo), AccountID: l.AccountID, Currency: l.Currency,
			Amount: storage.Decimal(l.Amount), FXRate: storage.Decimal(l.FxRate), FXRateSource: l.FxRateSource,
			BaseCurrency: l.BaseCurrency, BaseAmount: storage.Decimal(l.BaseAmount)}
		if l.Description != nil {
			line.Description = *l.Description
		}
		e.Lines = append(e.Lines, line)
	}
	return e, nil
}

func (r ledger) ByIdentity(ctx context.Context, postingIdentity string) (storage.Entry, error) {
	if err := r.u.check(); err != nil {
		return storage.Entry{}, err
	}
	id, err := r.u.q.GetEntryIDByIdentity(ctx, postingIdentity)
	if err != nil {
		return storage.Entry{}, mapError(err)
	}
	return r.Entry(ctx, id)
}

func (r ledger) Revision(ctx context.Context) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	rev, err := r.u.q.GetLedgerRevision(ctx)
	return rev, mapError(err)
}

// ------------------------------------------------------------------ balances

type balances struct{ u *unitOfWork }

func (r balances) ForPeriod(ctx context.Context, periodID uuid.UUID) ([]storage.Balance, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BalancesForPeriod(ctx, periodID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Balance, 0, len(rows))
	for _, b := range rows {
		out = append(out, storage.Balance{AccountID: b.AccountID, PeriodID: b.PeriodID, Currency: b.Currency,
			Amount: storage.Decimal(b.Amount), BaseAmount: storage.Decimal(b.BaseAmount), LineCount: b.LineCount})
	}
	return out, nil
}

func (r balances) Differences(ctx context.Context) ([]storage.BalanceDifference, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BalanceDifferences(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	opt := func(s string) *storage.Decimal {
		if s == "" {
			return nil
		}
		d := storage.Decimal(s)
		return &d
	}
	out := make([]storage.BalanceDifference, 0, len(rows))
	for _, d := range rows {
		out = append(out, storage.BalanceDifference{AccountID: d.AccountID, PeriodID: d.PeriodID, Currency: d.Currency,
			CachedAmount: opt(d.CachedAmount), CachedBaseAmount: opt(d.CachedBaseAmount),
			LinesAmount: opt(d.LinesAmount), LinesBaseAmount: opt(d.LinesBaseAmount)})
	}
	return out, nil
}

func (r balances) Rebuild(ctx context.Context) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.RebuildBalances(ctx)
	if err != nil {
		return 0, mapError(err)
	}
	return n, r.u.event(ctx, "balance.rebuilt", "organisation", r.u.org, nil, map[string][2]any{"rows": {nil, n}})
}

// ------------------------------------------------------------------ helpers

// dateOf keeps the calendar date of t, whatever its location.
func dateOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

// jsonValue decodes a JSON document for an audit change, keeping numbers exact (json.Number).
func jsonValue(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("the document is not valid JSON: %v", err)}
	}
	return v, nil
}

// notFound reports pgx's "no rows" as the ports' ErrNotFound.
func notFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ports.ErrNotFound)
}
