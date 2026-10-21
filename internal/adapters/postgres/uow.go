package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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
	voided      map[uuid.UUID]bool // approval requests whose voiding is audited already
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
	return organisationOf(sqlcdb.LockOrganisationRow(row)), nil
}

func organisationOf(r sqlcdb.LockOrganisationRow) storage.Organisation {
	return storage.Organisation{ID: r.OrganisationID, Name: r.Name, BaseCurrency: r.BaseCurrency,
		BaseMinorUnits: int(r.BaseMinorUnits), Version: int(r.Version), Profile: storage.OrganisationProfile{
			LegalName: str(r.LegalName), UEN: str(r.Uen), RegisteredAddress: str(r.RegisteredAddress),
			MailingAddress: str(r.MailingAddress), Email: str(r.Email), Phone: str(r.Phone), Website: str(r.Website),
			BankName: str(r.BankName), BankAccountName: str(r.BankAccountName), BankAccountNumber: str(r.BankAccountNumber),
			PayNowUEN: str(r.PaynowUen), DocumentFooter: str(r.DocumentFooter), FYStartMonth: int(r.FyStartMonth),
			BooksStart: nullDate(r.BooksStart), FirstYearEnd: nullDate(r.FirstYearEnd), TimeZone: r.TimeZone}}
}

// UpdateOrganisation implements storage.UnitOfWork: under the row's lock, the version must still be the
// one the caller read.
func (u *unitOfWork) UpdateOrganisation(ctx context.Context, version int, p storage.OrganisationProfile) (storage.Organisation, error) {
	if err := u.check(); err != nil {
		return storage.Organisation{}, err
	}
	cur, err := u.q.LockOrganisation(ctx)
	if err != nil {
		return storage.Organisation{}, mapError(err)
	}
	before := organisationOf(cur)
	if p.FYStartMonth < 1 || p.FYStartMonth > 12 {
		return storage.Organisation{}, &storage.Error{Code: storage.CodeOrganisation,
			Message: fmt.Sprintf("%d is not a month a financial year can start in", p.FYStartMonth)}
	}
	month := int16(p.FYStartMonth) // 1 to 12, checked above
	if before.Version != version {
		return storage.Organisation{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("the organisation changed since it was read: it is at version %d, not %d", before.Version, version)}
	}
	row, err := u.q.UpdateOrganisationProfile(ctx, sqlcdb.UpdateOrganisationProfileParams{LegalName: opt(p.LegalName), Uen: opt(p.UEN),
		RegisteredAddress: opt(p.RegisteredAddress), MailingAddress: opt(p.MailingAddress), Email: opt(p.Email), Phone: opt(p.Phone),
		Website: opt(p.Website), BankName: opt(p.BankName), BankAccountName: opt(p.BankAccountName),
		BankAccountNumber: opt(p.BankAccountNumber), PaynowUen: opt(p.PayNowUEN), DocumentFooter: opt(p.DocumentFooter),
		FyStartMonth: month, BooksStart: pgDate(p.BooksStart), FirstYearEnd: pgDate(p.FirstYearEnd), TimeZone: p.TimeZone})
	if err != nil {
		return storage.Organisation{}, mapError(err)
	}
	after := organisationOf(sqlcdb.LockOrganisationRow(row))
	changes := map[string][2]any{"version": {before.Version, after.Version}}
	for k, v := range profileChanges(before.Profile, after.Profile) {
		changes[k] = v
	}
	if err := u.event(ctx, "organisation.updated", "organisation", after.ID, &after.Version, changes); err != nil {
		return storage.Organisation{}, err
	}
	return after, nil
}

// profileChanges are the fields that differ, by column name; the audit trail masks the bank and PayNow
// numbers by their names (SEC-15).
func profileChanges(a, b storage.OrganisationProfile) map[string][2]any {
	out := map[string][2]any{}
	text := func(k, x, y string) {
		if x != y {
			out[k] = [2]any{nilIfEmpty(x), nilIfEmpty(y)}
		}
	}
	text("legal_name", a.LegalName, b.LegalName)
	text("uen", a.UEN, b.UEN)
	text("registered_address", a.RegisteredAddress, b.RegisteredAddress)
	text("mailing_address", a.MailingAddress, b.MailingAddress)
	text("email", a.Email, b.Email)
	text("phone", a.Phone, b.Phone)
	text("website", a.Website, b.Website)
	text("bank_name", a.BankName, b.BankName)
	text("bank_account_name", a.BankAccountName, b.BankAccountName)
	text("bank_account_number", a.BankAccountNumber, b.BankAccountNumber)
	text("paynow_uen", a.PayNowUEN, b.PayNowUEN)
	text("document_footer", a.DocumentFooter, b.DocumentFooter)
	text("time_zone", a.TimeZone, b.TimeZone)
	text("books_start", dayOf(a.BooksStart), dayOf(b.BooksStart))
	text("first_year_end", dayOf(a.FirstYearEnd), dayOf(b.FirstYearEnd))
	if a.FYStartMonth != b.FYStartMonth {
		out["fy_start_month"] = [2]any{a.FYStartMonth, b.FYStartMonth}
	}
	return out
}

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullDate(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}

func pgDate(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

func dayOf(t *time.Time) string {
	if t == nil {
		return ""
	}
	return day(*t)
}

func (u *unitOfWork) Accounts() storage.Accounts   { return accounts{u} }
func (u *unitOfWork) Periods() storage.Periods     { return periods{u} }
func (u *unitOfWork) Closes() storage.Closes       { return closes{u} }
func (u *unitOfWork) Documents() storage.Documents { return documents{u} }
func (u *unitOfWork) Ledger() storage.Ledger       { return ledger{u} }
func (u *unitOfWork) Balances() storage.Balances   { return balances{u} }

// ------------------------------------------------------------------ accounts

type accounts struct{ u *unitOfWork }

// accountRow is every query's account row: they all select the same columns.
type accountRow = sqlcdb.GetAccountRow

func toAccount(r accountRow) storage.Account {
	a := storage.Account{ID: r.ID, Code: r.Code, Name: r.Name, Type: storage.AccountType(r.Type), ParentID: r.ParentID,
		Group: r.IsGroup, Active: r.IsActive, Version: int(r.Version), AccountAttributes: storage.AccountAttributes{
			ReportGroup: str(r.ReportGroup), CashFlowLine: str(r.CashFlowLine), Function: str(r.Function), MoneyAccount: r.IsMoneyAccount,
			DefaultGSTCode: str(r.DefaultGstCode), Currency: str(r.Currency), Control: r.IsControl, PayrollCost: r.IsPayrollCost,
			Guidance: str(r.Guidance)}}
	if r.Role != nil {
		a.Role = storage.AccountRole(*r.Role)
	}
	return a
}

func (r accounts) Create(ctx context.Context, a storage.NewAccount) (storage.Account, error) {
	if err := r.u.check(); err != nil {
		return storage.Account{}, err
	}
	var role *string
	if a.Role != "" {
		role = (*string)(&a.Role)
	}
	row, err := r.u.q.InsertAccount(ctx, sqlcdb.InsertAccountParams{Code: a.Code, Name: a.Name, Type: string(a.Type),
		ParentID: a.ParentID, IsGroup: a.Group, Role: role, ReportGroup: opt(a.ReportGroup), CashFlowLine: opt(a.CashFlowLine),
		Function: opt(a.Function), IsMoneyAccount: a.MoneyAccount, DefaultGstCode: opt(a.DefaultGSTCode), Currency: opt(a.Currency),
		IsControl: a.Control, IsPayrollCost: a.PayrollCost, Guidance: opt(a.Guidance)})
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	v := int(row.Version)
	if err := r.u.event(ctx, "account.created", "account", row.ID, &v, map[string][2]any{
		"code": {nil, row.Code}, "name": {nil, row.Name}, "type": {nil, row.Type},
		"parent_id": {nil, row.ParentID}, "is_group": {nil, row.IsGroup}, "is_active": {nil, row.IsActive}, "role": {nil, row.Role},
		"report_group": {nil, row.ReportGroup}, "cash_flow_line": {nil, row.CashFlowLine}, "function": {nil, row.Function},
		"is_money_account": {nil, row.IsMoneyAccount}, "default_gst_code": {nil, row.DefaultGstCode}, "currency": {nil, row.Currency},
		"is_control": {nil, row.IsControl}, "is_payroll_cost": {nil, row.IsPayrollCost}, "guidance": {nil, row.Guidance}}); err != nil {
		return storage.Account{}, err
	}
	return toAccount(accountRow(row)), nil
}

func (r accounts) Get(ctx context.Context, id uuid.UUID) (storage.Account, error) {
	if err := r.u.check(); err != nil {
		return storage.Account{}, err
	}
	row, err := r.u.q.GetAccount(ctx, id)
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	return toAccount(accountRow(row)), nil
}

func (r accounts) ByCode(ctx context.Context, code string) (storage.Account, error) {
	if err := r.u.check(); err != nil {
		return storage.Account{}, err
	}
	row, err := r.u.q.GetAccountByCode(ctx, code)
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	return toAccount(accountRow(row)), nil
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
	return toAccount(accountRow(row)), nil
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
		out = append(out, toAccount(accountRow(a)))
	}
	return out, nil
}

func (r accounts) Page(ctx context.Context, after string, limit int) ([]storage.Account, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 10000 {
		return nil, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("a page of %d accounts", limit), Hint: "Ask for 1 to 10,000"}
	}
	rows, err := r.u.q.PageAccounts(ctx, sqlcdb.PageAccountsParams{After: after, PageSize: int32(limit)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Account, 0, len(rows))
	for _, a := range rows {
		out = append(out, toAccount(accountRow(a)))
	}
	return out, nil
}

func (r accounts) Update(ctx context.Context, id uuid.UUID, version int, c storage.AccountChange) (storage.Account, error) {
	if err := r.u.check(); err != nil {
		return storage.Account{}, err
	}
	cur, err := r.u.q.LockAccount(ctx, id)
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	before := toAccount(accountRow(cur))
	if before.Version != version {
		return storage.Account{}, &storage.Error{Code: storage.CodeVersion,
			Message: fmt.Sprintf("account %s changed since it was read: it is at version %d, not %d", before.Code, before.Version, version)}
	}
	row, err := r.u.q.UpdateAccount(ctx, sqlcdb.UpdateAccountParams{ID: id, Name: c.Name, ParentID: c.ParentID, IsGroup: c.Group,
		IsActive: c.Active, ReportGroup: opt(c.ReportGroup), CashFlowLine: opt(c.CashFlowLine), Function: opt(c.Function),
		IsMoneyAccount: c.MoneyAccount, DefaultGstCode: opt(c.DefaultGSTCode), Currency: opt(c.Currency), IsControl: c.Control,
		IsPayrollCost: c.PayrollCost, Guidance: opt(c.Guidance)})
	if err != nil {
		return storage.Account{}, mapError(err)
	}
	after := toAccount(accountRow(row))
	changes := map[string][2]any{"version": {before.Version, after.Version}}
	diff := func(k string, x, y any) {
		if fmt.Sprint(x) != fmt.Sprint(y) {
			changes[k] = [2]any{x, y}
		}
	}
	parent := func(p *uuid.UUID) any {
		if p == nil {
			return nil
		}
		return p.String()
	}
	diff("name", before.Name, after.Name)
	diff("parent_id", parent(before.ParentID), parent(after.ParentID))
	diff("is_group", before.Group, after.Group)
	diff("is_active", before.Active, after.Active)
	diff("report_group", before.ReportGroup, after.ReportGroup)
	diff("cash_flow_line", before.CashFlowLine, after.CashFlowLine)
	diff("function", before.Function, after.Function)
	diff("is_money_account", before.MoneyAccount, after.MoneyAccount)
	diff("default_gst_code", before.DefaultGSTCode, after.DefaultGSTCode)
	diff("currency", before.Currency, after.Currency)
	diff("is_control", before.Control, after.Control)
	diff("is_payroll_cost", before.PayrollCost, after.PayrollCost)
	diff("guidance", before.Guidance, after.Guidance)
	if err := r.u.event(ctx, "account.updated", "account", after.ID, &after.Version, changes); err != nil {
		return storage.Account{}, err
	}
	return after, nil
}

func (r accounts) Count(ctx context.Context) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	n, err := r.u.q.CountAccounts(ctx)
	return n, mapError(err)
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
	by := r.u.actor(ctx)
	if err := r.u.q.InsertDocumentVersion(ctx, sqlcdb.InsertDocumentVersionParams{DocumentID: row.ID, Version: row.CurrentVersion, Payload: d.Payload,
		CreatedBy: &by}); err != nil {
		return storage.Document{}, mapError(err)
	}
	if d.Type == storage.DocumentOpeningBalance {
		if err := r.u.q.InsertOpeningBalance(ctx, row.ID); err != nil {
			return storage.Document{}, mapError(err)
		}
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
		Version: int(row.CurrentVersion), Payload: d.Payload, SavedBy: by}, nil
}

func (r documents) Revise(ctx context.Context, id uuid.UUID, payload []byte) (storage.Document, error) {
	if err := r.u.check(); err != nil {
		return storage.Document{}, err
	}
	cur, err := r.u.q.LockDocument(ctx, id)
	if err != nil {
		return storage.Document{}, mapError(err)
	}
	next, by := cur.CurrentVersion+1, r.u.actor(ctx)
	if err := r.u.q.InsertDocumentVersion(ctx, sqlcdb.InsertDocumentVersionParams{DocumentID: id, Version: next, Payload: payload, CreatedBy: &by}); err != nil {
		return storage.Document{}, mapError(err)
	}
	if err := r.u.q.SetDocumentVersion(ctx, sqlcdb.SetDocumentVersionParams{ID: id, Version: next}); err != nil {
		return storage.Document{}, mapError(err)
	}
	before, err := jsonValue(cur.Payload)
	if err != nil {
		return storage.Document{}, err
	}
	after, err := jsonValue(payload)
	if err != nil {
		return storage.Document{}, err
	}
	v := int(next)
	if err := r.u.event(ctx, "document.revised", "document", id, &v, map[string][2]any{
		"version": {cur.CurrentVersion, next}, "payload": {before, after}}); err != nil {
		return storage.Document{}, err
	}
	return storage.Document{ID: id, Type: storage.DocumentType(cur.Type), Status: storage.DocumentStatus(cur.Status), Version: v, Payload: payload,
		Number: str(cur.Number), SavedBy: by, PostedBy: str(cur.PostedBy), PostedAt: cur.PostedAt}, nil
}

func (r documents) Edit(ctx context.Context, id uuid.UUID, payload []byte, preparer string) (storage.Document, error) {
	if err := r.u.check(); err != nil {
		return storage.Document{}, err
	}
	cur, err := r.u.q.LockDocument(ctx, id)
	if err != nil {
		return storage.Document{}, mapError(err)
	}
	next, by, at := cur.CurrentVersion+1, r.u.actor(ctx), r.u.store.clock.Now()
	saver := preparer
	if saver == "" {
		saver = by
	}
	if err := r.u.q.InsertDocumentVersion(ctx, sqlcdb.InsertDocumentVersionParams{DocumentID: id, Version: next, Payload: payload, CreatedBy: &saver}); err != nil {
		return storage.Document{}, mapError(err)
	}
	if err := r.u.q.EditDocument(ctx, sqlcdb.EditDocumentParams{ID: id, Version: next, PostedBy: by, PostedAt: at}); err != nil {
		return storage.Document{}, mapError(err)
	}
	before, err := jsonValue(cur.Payload)
	if err != nil {
		return storage.Document{}, err
	}
	after, err := jsonValue(payload)
	if err != nil {
		return storage.Document{}, err
	}
	v := int(next)
	if err := r.u.event(ctx, "document.edited", "document", id, &v, map[string][2]any{
		"version": {cur.CurrentVersion, next}, "payload": {before, after}, "saved_by": {str(cur.CreatedBy), saver},
		"posted_by": {str(cur.PostedBy), by}}); err != nil {
		return storage.Document{}, err
	}
	return storage.Document{ID: id, Type: storage.DocumentType(cur.Type), Status: storage.DocumentStatus(cur.Status), Version: v, Payload: payload,
		Number: str(cur.Number), SavedBy: saver, PostedBy: by, PostedAt: &at}, nil
}

func (r documents) DueReversals(ctx context.Context, day string, limit int) ([]uuid.UUID, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	ids, err := r.u.q.DueReversals(ctx, sqlcdb.DueReversalsParams{OnDay: day, MaxRows: int32(min(limit, 10000))}) // #nosec G115 -- bounded above
	return ids, mapError(err)
}

func (r documents) WaitingReversals(ctx context.Context, afterOn string, afterID *uuid.UUID, limit int) ([]storage.Document, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.WaitingReversals(ctx, sqlcdb.WaitingReversalsParams{AfterOn: afterOn, AfterID: afterID,
		MaxRows: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- 1 to 1,000
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Document, len(rows))
	for i, row := range rows {
		out[i] = storage.Document{ID: row.ID, Type: storage.DocumentType(row.Type), Status: storage.DocumentStatus(row.Status),
			Version: int(row.CurrentVersion), Payload: row.Payload, Number: str(row.Number), SavedBy: str(row.CreatedBy),
			PostedBy: str(row.PostedBy), PostedAt: row.PostedAt, Date: row.Date}
	}
	return out, nil
}

func (r documents) Page(ctx context.Context, f storage.DocumentFilter) ([]storage.Document, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	opt := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	rows, err := r.u.q.ListDocuments(ctx, sqlcdb.ListDocumentsParams{Type: string(f.Type), Status: opt(string(f.Status)), DateFrom: opt(f.From),
		DateTo: opt(f.To), Search: opt(f.Search), AfterID: f.AfterID, AfterDate: f.AfterDate, PageSize: int32(min(max(f.Limit, 1), 1000))}) // 1 to 1,000
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Document, len(rows))
	for i, row := range rows {
		out[i] = storage.Document{ID: row.ID, Type: storage.DocumentType(row.Type), Status: storage.DocumentStatus(row.Status),
			Version: int(row.CurrentVersion), Payload: row.Payload, Number: str(row.Number), SavedBy: str(row.CreatedBy),
			PostedBy: str(row.PostedBy), PostedAt: row.PostedAt, Date: row.Date}
	}
	return out, nil
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
		Version: int(row.CurrentVersion), Payload: row.Payload, Number: str(row.Number), SavedBy: str(row.CreatedBy),
		PostedBy: str(row.PostedBy), PostedAt: row.PostedAt}, nil
}

func (r documents) OpeningBalance(ctx context.Context) (storage.Document, error) {
	if err := r.u.check(); err != nil {
		return storage.Document{}, err
	}
	row, err := r.u.q.GetOpeningBalance(ctx)
	if err != nil {
		return storage.Document{}, mapError(err)
	}
	return storage.Document{ID: row.ID, Type: storage.DocumentType(row.Type), Status: storage.DocumentStatus(row.Status),
		Version: int(row.CurrentVersion), Payload: row.Payload, Number: str(row.Number), SavedBy: str(row.CreatedBy),
		PostedBy: str(row.PostedBy), PostedAt: row.PostedAt}, nil
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
		Version: int(row.CurrentVersion), Payload: row.Payload, Number: str(row.Number), SavedBy: str(row.CreatedBy),
		PostedBy: str(row.PostedBy), PostedAt: row.PostedAt}, nil
}

func (r documents) SetStatus(ctx context.Context, id uuid.UUID, to storage.DocumentStatus) error {
	if err := r.u.check(); err != nil {
		return err
	}
	before, err := r.u.q.GetDocumentStatusForUpdate(ctx, id)
	if err != nil {
		return mapError(err)
	}
	if to == storage.DocumentPosted {
		by, at := r.u.actor(ctx), r.u.store.clock.Now()
		if err := r.u.q.SetDocumentStatus(ctx, sqlcdb.SetDocumentStatusParams{ID: id, Status: string(to), PostedBy: &by, PostedAt: &at}); err != nil {
			return mapError(err)
		}
	} else if err := r.u.q.SetDocumentStatusOnly(ctx, sqlcdb.SetDocumentStatusOnlyParams{ID: id, Status: string(to)}); err != nil {
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

func (r periods) List(ctx context.Context) ([]storage.Period, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListPeriods(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Period, len(rows))
	for i, row := range rows {
		out[i] = toPeriod(row.ID, row.StartsOn, row.EndsOn, row.Status, row.ClosedRevision, row.Version)
	}
	return out, nil
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
	if e.Source.Version < 1 || e.Source.Version > math.MaxInt32 {
		return storage.Entry{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("source version %d is out of range", e.Source.Version)}
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

func (r ledger) AccountLines(ctx context.Context, account uuid.UUID, from, to time.Time, limit int) ([]storage.AccountLine, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AccountLines(ctx, sqlcdb.AccountLinesParams{AccountID: account, DateFrom: dateOf(from), DateTo: dateOf(to),
		RowLimit: int32(min(max(limit, 1), 100000))}) // 1 to 100,000
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AccountLine, len(rows))
	for i, x := range rows {
		out[i] = storage.AccountLine{EntryID: x.EntryID, LedgerRevision: x.LedgerRevision, PostingDate: x.PostingDate,
			Source: storage.Source{Type: x.SourceType, ID: x.SourceID, Version: int(x.SourceVersion)}, Memo: str(x.Memo), ReversesEntryID: x.ReversesEntryID,
			LineNo: int(x.LineNo), Currency: x.Currency, Amount: storage.Decimal(x.Amount), BaseAmount: storage.Decimal(x.BaseAmount),
			Description: str(x.Description), Number: str(x.Number)}
	}
	return out, nil
}

func (r ledger) Lock(ctx context.Context) (int64, error) {
	if err := r.u.check(); err != nil {
		return 0, err
	}
	rev, err := r.u.q.LockLedger(ctx)
	return rev, mapError(err)
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
