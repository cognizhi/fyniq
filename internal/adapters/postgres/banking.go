package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Banking() storage.Banking { return banking{u} }

type banking struct{ u *unitOfWork }

func decPtr(d *storage.Decimal) *string {
	if d == nil {
		return nil
	}
	s := string(*d)
	return &s
}

func toDecPtr(s *string) *storage.Decimal {
	if s == nil {
		return nil
	}
	d := storage.Decimal(*s)
	return &d
}

func toMoneyAccount(r sqlcdb.FyniqMoneyAccount) storage.MoneyAccount {
	return storage.MoneyAccount{ID: r.ID, AccountID: r.AccountID, Kind: r.Kind, Name: r.Name, Institution: str(r.Institution),
		Identifier: str(r.Identifier), Currency: r.Currency, OpenedOn: nullDate(r.OpenedOn), ClosedOn: nullDate(r.ClosedOn),
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, ChangedAt: r.ChangedAt, Version: int(r.Version)}
}

func toSource(r sqlcdb.FyniqMoneyAccountSource) storage.MoneyAccountSource {
	return storage.MoneyAccountSource{ID: r.ID, MoneyAccountID: r.MoneyAccountID, Kind: r.Kind, ConnectorID: r.ConnectorID,
		Senders: r.Senders, ReportFormat: str(r.ReportFormat), MappingID: r.MappingID, FromDate: r.FromDate, SetBy: r.SetBy, SetAt: r.SetAt}
}

func toMapping(r sqlcdb.FyniqStatementMapping) storage.StatementMapping {
	return storage.StatementMapping{ID: r.ID, Name: r.Name, Institution: str(r.Institution), Format: r.Format, Spec: r.Spec,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, ChangedAt: r.ChangedAt, Version: int(r.Version)}
}

func toStatement(r sqlcdb.FyniqBankStatement) storage.BankStatement {
	return storage.BankStatement{ID: r.ID, MoneyAccountID: r.MoneyAccountID, Source: r.Source, Format: r.Format, ParserVersion: r.ParserVersion,
		PeriodFrom: r.PeriodFrom, PeriodTo: r.PeriodTo, Currency: r.Currency, Opening: toDecPtr(r.OpeningBalance), Closing: toDecPtr(r.ClosingBalance),
		LineCount: int(r.LineCount), FileRef: str(r.FileRef), ConnectorID: r.ConnectorID, ImportedBy: r.ImportedBy, ImportedAt: r.ImportedAt}
}

func toLine(r sqlcdb.FyniqBankLine) storage.BankLine {
	return storage.BankLine{ID: r.ID, StatementID: r.StatementID, MoneyAccountID: r.MoneyAccountID, Position: int(r.Position),
		BookingDate: r.BookingDate, ValueDate: nullDate(r.ValueDate), Amount: storage.Decimal(r.Amount), Currency: r.Currency,
		Reference: str(r.Reference), Counterparty: str(r.Counterparty), Description: str(r.Description),
		RunningBalance: toDecPtr(r.RunningBalance), Fingerprint: r.Fingerprint, Status: r.Status, DuplicateOf: r.DuplicateOf,
		StatusBy: r.StatusBy, StatusAt: r.StatusAt, Version: int(r.Version)}
}

func toGap(r sqlcdb.FyniqBankGap) storage.BankGap {
	return storage.BankGap{ID: r.ID, MoneyAccountID: r.MoneyAccountID, StatementID: r.StatementID, AfterDate: nullDate(r.AfterDate),
		BeforeDate: r.BeforeDate, Currency: r.Currency, ExpectedOpening: toDecPtr(r.ExpectedOpening), FoundOpening: toDecPtr(r.FoundOpening),
		FoundAt: r.FoundAt, ResolvedBy: str(r.ResolvedBy), ResolvedAt: r.ResolvedAt, Resolution: str(r.Resolution)}
}

// moneyAccountChanges are an account's audited fields, before and after.
func moneyAccountChanges(a, b storage.MoneyAccount) map[string][2]any {
	out := map[string][2]any{}
	text := func(k, x, y string) {
		if x != y {
			out[k] = [2]any{nilIfEmpty(x), nilIfEmpty(y)}
		}
	}
	text("kind", a.Kind, b.Kind)
	text("name", a.Name, b.Name)
	text("institution", a.Institution, b.Institution)
	text("identifier", masked(a.Identifier), masked(b.Identifier))
	text("currency", a.Currency, b.Currency)
	text("opened_on", dayOf(a.OpenedOn), dayOf(b.OpenedOn))
	text("closed_on", dayOf(a.ClosedOn), dayOf(b.ClosedOn))
	return out
}

// masked shows an account identifier's last four characters only, as logs and the audit trail keep it (SEC-15).
func masked(id string) string {
	if len(id) <= 4 {
		return id
	}
	return "…" + id[len(id)-4:]
}

func (r banking) CreateMoneyAccount(ctx context.Context, n storage.NewMoneyAccount) (storage.MoneyAccount, error) {
	if err := r.u.check(); err != nil {
		return storage.MoneyAccount{}, err
	}
	id, err := r.u.q.InsertMoneyAccount(ctx, sqlcdb.InsertMoneyAccountParams{AccountID: n.AccountID, Kind: n.Kind, Name: n.Name,
		Institution: opt(n.Institution), Identifier: opt(n.Identifier), Currency: n.Currency, OpenedOn: pgDate(n.OpenedOn),
		By: r.u.actor(ctx), At: n.At})
	if err != nil {
		return storage.MoneyAccount{}, mapError(err)
	}
	out, err := r.MoneyAccount(ctx, id)
	if err != nil {
		return out, err
	}
	changes := moneyAccountChanges(storage.MoneyAccount{}, out)
	changes["account_id"] = [2]any{nil, out.AccountID.String()}
	return out, r.u.event(ctx, "money_account.opened", "money_account", id, &out.Version, changes)
}

func (r banking) ChangeMoneyAccount(ctx context.Context, id uuid.UUID, version int, c storage.MoneyAccountChange) (storage.MoneyAccount, error) {
	before, err := r.MoneyAccount(ctx, id)
	if err != nil {
		return before, err
	}
	n, err := r.u.q.UpdateMoneyAccount(ctx, sqlcdb.UpdateMoneyAccountParams{Kind: c.Kind, Name: c.Name, Institution: opt(c.Institution),
		Identifier: opt(c.Identifier), OpenedOn: pgDate(c.OpenedOn), ClosedOn: pgDate(c.ClosedOn), By: r.u.actor(ctx), At: c.At,
		ID: id, Version: small(version)})
	if err != nil {
		return storage.MoneyAccount{}, mapError(err)
	}
	if n == 0 {
		return storage.MoneyAccount{}, &storage.Error{Code: storage.CodeVersion, Message: "the money account changed since it was read"}
	}
	after, err := r.MoneyAccount(ctx, id)
	if err != nil {
		return after, err
	}
	action := "money_account.changed"
	if before.ClosedOn == nil && after.ClosedOn != nil {
		action = "money_account.closed"
	}
	return after, r.u.event(ctx, action, "money_account", id, &after.Version, moneyAccountChanges(before, after))
}

func (r banking) MoneyAccount(ctx context.Context, id uuid.UUID) (storage.MoneyAccount, error) {
	if err := r.u.check(); err != nil {
		return storage.MoneyAccount{}, err
	}
	row, err := r.u.q.MoneyAccount(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.MoneyAccount{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.MoneyAccount{}, mapError(err)
	}
	return toMoneyAccount(row), nil
}

func (r banking) MoneyAccounts(ctx context.Context) ([]storage.MoneyAccount, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.MoneyAccounts(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MoneyAccount, 0, len(rows))
	for _, x := range rows {
		out = append(out, toMoneyAccount(x))
	}
	return out, nil
}

func (r banking) AddSource(ctx context.Context, n storage.NewMoneyAccountSource) (storage.MoneyAccountSource, error) {
	if err := r.u.check(); err != nil {
		return storage.MoneyAccountSource{}, err
	}
	senders := n.Senders
	if senders == nil {
		senders = []string{}
	}
	row, err := r.u.q.InsertMoneyAccountSource(ctx, sqlcdb.InsertMoneyAccountSourceParams{MoneyAccountID: n.MoneyAccountID, Kind: n.Kind,
		ConnectorID: n.ConnectorID, Senders: senders, ReportFormat: opt(n.ReportFormat), MappingID: n.MappingID, FromDate: n.FromDate,
		By: r.u.actor(ctx), At: n.At})
	if err != nil {
		return storage.MoneyAccountSource{}, mapError(err)
	}
	out := toSource(row)
	changes := map[string][2]any{"source.kind": {nil, out.Kind}, "source.from": {nil, day(out.FromDate)}}
	if out.ConnectorID != nil {
		changes["source.connector_id"] = [2]any{nil, out.ConnectorID.String()}
	}
	if out.ReportFormat != "" {
		changes["source.format"] = [2]any{nil, out.ReportFormat}
	}
	return out, r.u.event(ctx, "money_account.source_set", "money_account", n.MoneyAccountID, nil, changes)
}

func (r banking) Sources(ctx context.Context, moneyAccountID uuid.UUID) ([]storage.MoneyAccountSource, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.MoneyAccountSources(ctx, moneyAccountID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MoneyAccountSource, 0, len(rows))
	for _, x := range rows {
		out = append(out, toSource(x))
	}
	return out, nil
}

func (r banking) SaveMapping(ctx context.Context, n storage.NewStatementMapping) (storage.StatementMapping, error) {
	if err := r.u.check(); err != nil {
		return storage.StatementMapping{}, err
	}
	row, err := r.u.q.InsertStatementMapping(ctx, sqlcdb.InsertStatementMappingParams{Name: n.Name, Institution: opt(n.Institution),
		Format: n.Format, Spec: n.Spec, By: r.u.actor(ctx), At: n.At})
	if err != nil {
		return storage.StatementMapping{}, mapError(err)
	}
	out := toMapping(row)
	return out, r.u.event(ctx, "statement_mapping.saved", "statement_mapping", out.ID, &out.Version,
		map[string][2]any{"name": {nil, out.Name}, "format": {nil, out.Format}, "spec": {nil, string(out.Spec)}})
}

func (r banking) ChangeMapping(ctx context.Context, id uuid.UUID, version int, n storage.NewStatementMapping) (storage.StatementMapping, error) {
	if err := r.u.check(); err != nil {
		return storage.StatementMapping{}, err
	}
	row, err := r.u.q.UpdateStatementMapping(ctx, sqlcdb.UpdateStatementMappingParams{Name: n.Name, Institution: opt(n.Institution),
		Spec: n.Spec, By: r.u.actor(ctx), At: n.At, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.StatementMapping{}, &storage.Error{Code: storage.CodeVersion, Message: "the saved mapping changed since it was read"}
	}
	if err != nil {
		return storage.StatementMapping{}, mapError(err)
	}
	out := toMapping(row)
	return out, r.u.event(ctx, "statement_mapping.changed", "statement_mapping", out.ID, &out.Version,
		map[string][2]any{"name": {nil, out.Name}, "spec": {nil, string(out.Spec)}})
}

func (r banking) Mappings(ctx context.Context) ([]storage.StatementMapping, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.StatementMappings(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.StatementMapping, 0, len(rows))
	for _, x := range rows {
		out = append(out, toMapping(x))
	}
	return out, nil
}

func (r banking) Import(ctx context.Context, n storage.NewBankStatement) (storage.BankStatement, []storage.BankLine, error) {
	if err := r.u.check(); err != nil {
		return storage.BankStatement{}, nil, err
	}
	by := r.u.actor(ctx)
	srow, err := r.u.q.InsertBankStatement(ctx, sqlcdb.InsertBankStatementParams{MoneyAccountID: n.MoneyAccountID, Source: n.Source,
		Format: n.Format, ParserVersion: n.ParserVersion, PeriodFrom: n.PeriodFrom, PeriodTo: n.PeriodTo, Currency: n.Currency,
		OpeningBalance: decPtr(n.Opening), ClosingBalance: decPtr(n.Closing), LineCount: small(len(n.Lines)), FileRef: opt(n.FileRef),
		ConnectorID: n.ConnectorID, By: by, At: n.At})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankStatement{}, nil, &storage.Error{Code: storage.CodeInvalid, Message: "the statement's currency " + n.Currency + " is not known"}
	}
	if err != nil {
		return storage.BankStatement{}, nil, mapError(err)
	}
	st := toStatement(srow)
	lines := make([]storage.BankLine, 0, len(n.Lines))
	duplicates := 0
	for _, l := range n.Lines {
		status := storage.LineUnreconciled
		if l.DuplicateOf != nil {
			status = storage.LinePossibleDuplicate
			duplicates++
		}
		row, err := r.u.q.InsertBankLine(ctx, sqlcdb.InsertBankLineParams{StatementID: st.ID, MoneyAccountID: n.MoneyAccountID,
			Position: small(l.Position), BookingDate: l.BookingDate, ValueDate: pgDate(l.ValueDate), Amount: string(l.Amount),
			Reference: opt(l.Reference), Counterparty: opt(l.Counterparty), Description: opt(l.Description),
			RunningBalance: decPtr(l.RunningBalance), Fingerprint: l.Fingerprint, Status: status, DuplicateOf: l.DuplicateOf,
			By: by, At: n.At, Currency: n.Currency})
		if err != nil {
			return storage.BankStatement{}, nil, mapError(err)
		}
		lines = append(lines, toLine(row))
	}
	changes := map[string][2]any{"money_account_id": {nil, n.MoneyAccountID.String()}, "source": {nil, st.Source},
		"format": {nil, st.Format}, "parser_version": {nil, st.ParserVersion}, "period": {nil, day(st.PeriodFrom) + "/" + day(st.PeriodTo)},
		"lines": {nil, len(lines)}, "possible_duplicates": {nil, duplicates}}
	if st.Opening != nil {
		changes["opening_balance"] = [2]any{nil, string(*st.Opening)}
	}
	if st.Closing != nil {
		changes["closing_balance"] = [2]any{nil, string(*st.Closing)}
	}
	if err := r.u.event(ctx, "bank_statement.imported", "bank_statement", st.ID, nil, changes); err != nil {
		return st, lines, err
	}
	if n.Gap != nil {
		g, err := r.u.q.InsertBankGap(ctx, sqlcdb.InsertBankGapParams{MoneyAccountID: n.MoneyAccountID, StatementID: st.ID,
			AfterDate: pgDate(n.Gap.AfterDate), BeforeDate: n.Gap.BeforeDate, ExpectedOpening: decPtr(n.Gap.ExpectedOpening),
			FoundOpening: decPtr(n.Gap.FoundOpening), At: n.At, Currency: n.Currency})
		if err != nil {
			return st, lines, mapError(err)
		}
		gap := toGap(g)
		gc := map[string][2]any{"statement_id": {nil, st.ID.String()}, "before": {nil, day(gap.BeforeDate)}}
		if gap.ExpectedOpening != nil {
			gc["expected_opening"] = [2]any{nil, string(*gap.ExpectedOpening)}
		}
		if gap.FoundOpening != nil {
			gc["found_opening"] = [2]any{nil, string(*gap.FoundOpening)}
		}
		if err := r.u.event(ctx, "bank_gap.found", "bank_gap", gap.ID, nil, gc); err != nil {
			return st, lines, err
		}
	}
	return st, lines, nil
}

func (r banking) Statements(ctx context.Context, moneyAccountID uuid.UUID) ([]storage.BankStatement, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BankStatements(ctx, moneyAccountID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BankStatement, 0, len(rows))
	for _, x := range rows {
		out = append(out, toStatement(x))
	}
	return out, nil
}

func (r banking) lines(rows []sqlcdb.FyniqBankLine) []storage.BankLine {
	out := make([]storage.BankLine, 0, len(rows))
	for _, x := range rows {
		out = append(out, toLine(x))
	}
	return out
}

func (r banking) LinesByFingerprint(ctx context.Context, fingerprints []string) ([]storage.BankLine, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	if len(fingerprints) == 0 {
		return nil, nil
	}
	rows, err := r.u.q.BankLinesByFingerprint(ctx, fingerprints)
	if err != nil {
		return nil, mapError(err)
	}
	return r.lines(rows), nil
}

func (r banking) Lines(ctx context.Context, f storage.BankLineFilter) ([]storage.BankLine, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	statuses := f.Statuses
	if statuses == nil {
		statuses = []string{}
	}
	rows, err := r.u.q.BankLines(ctx, sqlcdb.BankLinesParams{MoneyAccountID: f.MoneyAccountID, Statuses: statuses,
		FromDate: pgDate(f.From), ToDate: pgDate(f.To)})
	if err != nil {
		return nil, mapError(err)
	}
	return r.lines(rows), nil
}

func (r banking) Line(ctx context.Context, id uuid.UUID) (storage.BankLine, error) {
	if err := r.u.check(); err != nil {
		return storage.BankLine{}, err
	}
	row, err := r.u.q.BankLine(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankLine{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.BankLine{}, mapError(err)
	}
	return toLine(row), nil
}

func (r banking) SetLineStatus(ctx context.Context, id uuid.UUID, version int, status string, at time.Time) (storage.BankLine, error) {
	before, err := r.Line(ctx, id)
	if err != nil {
		return before, err
	}
	row, err := r.u.q.SetBankLineStatus(ctx, sqlcdb.SetBankLineStatusParams{Status: status, By: r.u.actor(ctx), At: at, ID: id,
		Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankLine{}, &storage.Error{Code: storage.CodeVersion, Message: "the bank line changed since it was read"}
	}
	if err != nil {
		return storage.BankLine{}, mapError(err)
	}
	after := toLine(row)
	return after, r.u.event(ctx, "bank_line."+status, "bank_line", id, &after.Version, map[string][2]any{"status": {before.Status, after.Status}})
}

func (r banking) Gaps(ctx context.Context, moneyAccountID uuid.UUID, open bool) ([]storage.BankGap, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.BankGaps(ctx, sqlcdb.BankGapsParams{MoneyAccountID: moneyAccountID, OpenOnly: open})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.BankGap, 0, len(rows))
	for _, x := range rows {
		out = append(out, toGap(x))
	}
	return out, nil
}

func (r banking) ResolveGap(ctx context.Context, id uuid.UUID, resolution string, at time.Time) (storage.BankGap, error) {
	if err := r.u.check(); err != nil {
		return storage.BankGap{}, err
	}
	by := r.u.actor(ctx)
	row, err := r.u.q.ResolveBankGap(ctx, sqlcdb.ResolveBankGapParams{By: &by, At: &at, Resolution: &resolution, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.BankGap{}, &storage.Error{Code: storage.CodeBanking, Message: "the gap is resolved already, or there is no such gap"}
	}
	if err != nil {
		return storage.BankGap{}, mapError(err)
	}
	g := toGap(row)
	return g, r.u.event(ctx, "bank_gap.resolved", "bank_gap", id, nil, map[string][2]any{"resolution": {nil, resolution}})
}
