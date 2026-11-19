package nemura

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

func payrollError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM171":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "A pay record, an election or a one-off item is history: it keeps its terms, and a change starts a new one from a later day."}
		case "NM172":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "That day overlaps the person's pay record: start the new one after the current one began."}
		case "23505":
			return &payroll.Refusal{Code: payroll.CodeConflict, Message: "That name or key is used already: choose another."}
		}
	}
	return err
}

// PayGroupRow is a pay group as stored.
type PayGroupRow struct {
	ID uuid.UUID
	payroll.PayGroup
}

// CreatePayGroup makes a pay group (NM-PAY-03). The audit event names the group, never a person's pay.
func CreatePayGroup(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, g payroll.PayGroup) (uuid.UUID, error) {
	if err := g.Check(); err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.pay_group (id, name, pay_day, cutoff_days, approval_days, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)",
		id, strings.TrimSpace(g.Name), g.PayDay, g.CutoffDays, g.ApprovalDays, by, at); err != nil {
		return uuid.Nil, payrollError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.pay_group.created", audit.Object{Type: "pay_group", ID: id}, map[string]audit.Change{"name": {nil, strings.TrimSpace(g.Name)}}, "")
}

// PayGroups lists the pay groups by name.
func PayGroups(ctx context.Context, tx pgx.Tx) ([]PayGroupRow, error) {
	rows, err := tx.Query(ctx, "SELECT id, name, pay_day, cutoff_days, approval_days FROM nemura.pay_group ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PayGroupRow
	for rows.Next() {
		var r PayGroupRow
		if err := rows.Scan(&r.ID, &r.Name, &r.PayDay, &r.CutoffDays, &r.ApprovalDays); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PayRow is a person's pay on a day.
type PayRow struct {
	ID, Group    uuid.UUID
	GroupName    string
	From         time.Time
	To           *time.Time
	Basic        string
	SHGFund      string
	SHGOptOut    bool
	Items        []payroll.RecurringItem
	Election     bool   // a joint election is in force
	ElectionKind string // fg (full employer, graduated employee rates) or ff (full rates for both); empty when none
	ElectionFrom *time.Time
}

// PayOn is a person's pay record on a day, or false.
func PayOn(ctx context.Context, tx pgx.Tx, person uuid.UUID, on time.Time) (PayRow, bool, error) {
	var r PayRow
	var fund *string
	err := tx.QueryRow(ctx, `SELECT p.id, p.pay_group_id, g.name, p.effective_from, p.effective_to, p.basic_monthly::numeric(20,2)::text, p.shg_fund, p.shg_opt_out
FROM nemura.person_pay p JOIN nemura.pay_group g ON g.organisation_id = p.organisation_id AND g.id = p.pay_group_id
WHERE p.person_id = $1 AND p.effective_from <= $2 AND (p.effective_to IS NULL OR p.effective_to > $2)`, person, day(on)).Scan(&r.ID, &r.Group, &r.GroupName, &r.From, &r.To, &r.Basic, &fund, &r.SHGOptOut)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if fund != nil {
		r.SHGFund = *fund
	}
	rows, err := tx.Query(ctx, "SELECT key, name, kind, nature, amount::numeric(20,2)::text, cpf_wages FROM nemura.person_pay_item WHERE pay_id = $1 ORDER BY key", r.ID)
	if err != nil {
		return r, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var it payroll.RecurringItem
		if err := rows.Scan(&it.Key, &it.Name, &it.Kind, &it.Nature, &it.Amount, &it.CPFWages); err != nil {
			return r, false, err
		}
		r.Items = append(r.Items, it)
	}
	if err := rows.Err(); err != nil {
		return r, false, err
	}
	rows.Close()
	var from *time.Time
	var kind string
	if err := tx.QueryRow(ctx, "SELECT effective_from, kind FROM nemura.pr_election WHERE person_id = $1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)", person, day(on)).Scan(&from, &kind); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return r, false, err
	}
	r.Election, r.ElectionFrom, r.ElectionKind = from != nil, from, kind
	return r, true, nil
}

// SetPay starts a person's pay record from a day, ending the current one the day before; the items go with it. The audit event says that pay was set and from when, never what it was
// (D8 of p1-nemura-payroll).
func SetPay(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, from time.Time, rec payroll.PayRecord) (uuid.UUID, error) {
	if err := rec.Check(); err != nil {
		return uuid.Nil, err
	}
	gid, err := uuid.Parse(rec.Group)
	if err != nil {
		return uuid.Nil, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The pay group is not one of this company's: choose one from the list."}
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.pay_group WHERE id = $1)", gid).Scan(&exists); err != nil || !exists {
		if err == nil {
			err = &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The pay group is not one of this company's: choose one from the list."}
		}
		return uuid.Nil, err
	}
	p, err := PersonOn(ctx, tx, person, from)
	if err != nil {
		return uuid.Nil, err
	}
	if from.Before(p.Start) {
		return uuid.Nil, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The pay starts on or after the day the person started."}
	}
	var cur uuid.UUID
	var curFrom time.Time
	err = tx.QueryRow(ctx, "SELECT id, effective_from FROM nemura.person_pay WHERE person_id = $1 AND effective_to IS NULL FOR UPDATE", person).Scan(&cur, &curFrom)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return uuid.Nil, err
	default:
		if !from.After(curFrom) {
			return uuid.Nil, &payroll.Refusal{Code: payroll.CodeConflict, Message: "The new pay starts after the current record began: choose a later day."}
		}
		if _, err := tx.Exec(ctx, "UPDATE nemura.person_pay SET effective_to = $2 WHERE id = $1", cur, day(from)); err != nil {
			return uuid.Nil, payrollError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.person_pay.ended", audit.Object{Type: "person_pay", ID: cur}, map[string]audit.Change{"effective_to": {nil, day(from)}}, ""); err != nil {
			return uuid.Nil, err
		}
	}
	id := uuid.Must(uuid.NewV7())
	var fund any
	if rec.SHGFund != "" {
		fund = rec.SHGFund
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_pay (id, person_id, effective_from, pay_group_id, basic_monthly, shg_fund, shg_opt_out, created_by, created_at) VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8, $9)`,
		id, person, day(from), gid, rec.Basic, fund, rec.SHGOptOut, by, at); err != nil {
		return uuid.Nil, payrollError(err)
	}
	for _, it := range rec.Items {
		if _, err := tx.Exec(ctx, "INSERT INTO nemura.person_pay_item (pay_id, key, name, kind, nature, amount, cpf_wages) VALUES ($1, $2, $3, $4, $5, $6::numeric, $7)",
			id, strings.TrimSpace(it.Key), strings.TrimSpace(it.Name), it.Kind, it.Nature, it.Amount, it.CPFWages); err != nil {
			return uuid.Nil, payrollError(err)
		}
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.person_pay.set", audit.Object{Type: "person_pay", ID: id}, map[string]audit.Change{"person": {nil, person.String()}, "effective_from": {nil, day(from)}, "items": {nil, len(rec.Items)}}, "")
}

// SetElection records a joint election to contribute at full employer rates in the first two years of permanent residence, with its evidence, from a day; it ends the one before. The kind is
// "fg" (full employer, graduated employee rates) or "ff" (full rates for both, the full table's): the CPF Board's approval says which. The audit event names the kind, never an amount.
func SetElection(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, from time.Time, evidence uuid.UUID, kind string) (uuid.UUID, error) {
	if kind == "" {
		kind = "fg"
	}
	if kind != "fg" && kind != "ff" {
		return uuid.Nil, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "kind: the CPF Board's approval is for fg (full employer rates, graduated employee rates) or ff (full rates for both)."}
	}
	var own bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.person_document WHERE id = $1 AND person_id = $2)", evidence, person).Scan(&own); err != nil {
		return uuid.Nil, err
	}
	if !own {
		return uuid.Nil, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The evidence is a document on the person's file (the CPF Board's approval of the application): add it first, then record the election."}
	}
	var cur uuid.UUID
	var curFrom time.Time
	err := tx.QueryRow(ctx, "SELECT id, effective_from FROM nemura.pr_election WHERE person_id = $1 AND effective_to IS NULL FOR UPDATE", person).Scan(&cur, &curFrom)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return uuid.Nil, err
	default:
		if !from.After(curFrom) {
			return uuid.Nil, &payroll.Refusal{Code: payroll.CodeConflict, Message: "The new election starts after the current one began: choose a later day."}
		}
		if _, err := tx.Exec(ctx, "UPDATE nemura.pr_election SET effective_to = $2 WHERE id = $1", cur, day(from)); err != nil {
			return uuid.Nil, payrollError(err)
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.pr_election.ended", audit.Object{Type: "pr_election", ID: cur}, map[string]audit.Change{"effective_to": {nil, day(from)}}, ""); err != nil {
			return uuid.Nil, err
		}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, "INSERT INTO nemura.pr_election (id, person_id, effective_from, evidence_id, kind, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7)", id, person, day(from), evidence, kind, by, at); err != nil {
		return uuid.Nil, payrollError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.pr_election.set", audit.Object{Type: "pr_election", ID: id}, map[string]audit.Change{"person": {nil, person.String()}, "effective_from": {nil, day(from)}, "kind": {nil, kind}}, "")
}

// YTDRow is a person's year to date of CPF wages.
type YTDRow struct {
	Year                  int
	OW, AWSubject, AWPaid string
	Through               time.Time
	Source                string
}

// ImportYTD records the year to date of CPF wages the employer has counted before Nemura's first run (FR-PAY-14), once for a year: a run adds to it afterwards. The audit event says that a
// year to date was imported for a person, never the figures.
func ImportYTD(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, y payroll.YTDImport) error {
	if err := y.Check(); err != nil {
		return err
	}
	if _, err := PersonOn(ctx, tx, person, y.Through); err != nil {
		return err
	}
	var has bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM nemura.cpf_ytd WHERE person_id = $1 AND year = $2)", person, y.Year).Scan(&has); err != nil {
		return err
	}
	if has {
		return &payroll.Refusal{Code: payroll.CodeConflict, Message: "That year's year to date is held already: a run adds to it, and a correction is a correction of the run."}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.cpf_ytd (person_id, year, ow, aw_subject, aw_paid, through_month, source, updated_by, updated_at) VALUES ($1, $2, $3::numeric, $4::numeric, $5::numeric, $6, 'import', $7, $8)`,
		person, y.Year, y.OW, y.AWSubject, y.AWPaid, day(y.Through), by, at); err != nil {
		return payrollError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.cpf_ytd.imported", audit.Object{Type: "cpf_ytd", ID: person}, map[string]audit.Change{"year": {nil, y.Year}, "through": {nil, day(y.Through)}}, "")
}

// YTDOf is a person's year to date for a year, or false.
func YTDOf(ctx context.Context, tx pgx.Tx, person uuid.UUID, year int) (YTDRow, bool, error) {
	r := YTDRow{Year: year}
	err := tx.QueryRow(ctx, "SELECT ow::numeric(20,2)::text, aw_subject::numeric(20,2)::text, aw_paid::numeric(20,2)::text, through_month, source FROM nemura.cpf_ytd WHERE person_id = $1 AND year = $2", person, year).
		Scan(&r.OW, &r.AWSubject, &r.AWPaid, &r.Through, &r.Source)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

// OneOffRow is a one-off item as stored.
type OneOffRow struct {
	ID                              uuid.UUID
	Person                          uuid.UUID
	PersonName                      string
	Month                           time.Time
	Key, Name, Nature, Amount, Note string
	CPFWages                        bool
	ForMonth, PayableOn             *time.Time
	Used                            bool
}

// AddOneOff records a one-off item for a wage month, with the facts that class it; a key is used once for a person and month.
func AddOneOff(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, month time.Time, o payroll.OneOff) (uuid.UUID, error) {
	if err := o.Check(); err != nil {
		return uuid.Nil, err
	}
	if month.Day() != 1 {
		return uuid.Nil, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The wage month is the first day of a month: write it as 2026-06-01."}
	}
	if _, err := PersonOn(ctx, tx, person, month); err != nil {
		return uuid.Nil, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.pay_one_off (id, person_id, month, key, name, nature, amount, cpf_wages, for_month, payable_on, note, created_by, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7::numeric, $8, $9, $10, $11, $12, $13)`,
		id, person, day(month), strings.TrimSpace(o.Key), strings.TrimSpace(o.Name), o.Nature, o.Amount, o.CPFWages, dayPtr(o.ForMonth), dayPtr(o.PayableOn), orNil(strings.TrimSpace(o.Note)), by, at); err != nil {
		return uuid.Nil, payrollError(err)
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.pay_one_off.added", audit.Object{Type: "pay_one_off", ID: id}, map[string]audit.Change{"person": {nil, person.String()}, "month": {nil, day(month)}, "key": {nil, strings.TrimSpace(o.Key)}}, "")
}

// OneOffs lists a month's one-off items.
func OneOffs(ctx context.Context, tx pgx.Tx, month time.Time) ([]OneOffRow, error) {
	rows, err := tx.Query(ctx, `SELECT o.id, o.person_id, p.legal_name, o.month, o.key, o.name, o.nature, o.amount::numeric(20,2)::text, o.cpf_wages, o.for_month, o.payable_on, coalesce(o.note, ''), o.used_in_run IS NOT NULL
FROM nemura.pay_one_off o JOIN nemura.person p ON p.organisation_id = o.organisation_id AND p.id = o.person_id WHERE o.month = $1 ORDER BY p.legal_name, o.key`, day(month))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OneOffRow
	for rows.Next() {
		var r OneOffRow
		if err := rows.Scan(&r.ID, &r.Person, &r.PersonName, &r.Month, &r.Key, &r.Name, &r.Nature, &r.Amount, &r.CPFWages, &r.ForMonth, &r.PayableOn, &r.Note, &r.Used); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
