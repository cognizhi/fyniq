package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/people"
)

func orNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// historyError turns the database's refusal of overlapping or rewritten history into the plain one.
func historyError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "NM121", "NM123":
			return &people.Refusal{Code: people.CodeHistory, Message: "That record is history: change it by starting a new one from a later date."}
		case "NM122", "NM124":
			return &people.Refusal{Code: people.CodeHistory, Message: "That date overlaps a record the person already has: start the new one on or after the day the last one ends."}
		}
	}
	return err
}

// AddPerson creates a person with their first job and, when given, classification, with their audit events (NM-PPL-01, NM-PPL-08). When the
// person has a manager with a login and they have one too, the reporting line of members is started on the same day.
func AddPerson(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, p people.Person, j people.Job, c *people.Classification) (uuid.UUID, error) {
	id := uuid.Must(uuid.NewV7())
	return id, addPersonWithID(ctx, tx, org, at, by, id, p, j, c)
}

// addPersonWithID is AddPerson for an ID the caller chose, so a repeat of the same request is refused, not done twice.
func addPersonWithID(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, id uuid.UUID, p people.Person, j people.Job, c *people.Classification) error {
	if err := p.Check(); err != nil {
		return err
	}
	if err := j.Check(); err != nil {
		return err
	}
	if c != nil {
		if err := c.Check(); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person (id, member_id, legal_name, preferred_name, work_email, phone, employment_start, probation_end, leaving_date, leaving_reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, id, p.MemberID, p.LegalName, orNil(p.PreferredName), orNil(p.WorkEmail), orNil(p.Phone), day(p.EmploymentStart), dayPtr(p.ProbationEnd), dayPtr(p.LeavingDate), orNil(p.LeavingReason)); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.person.created", audit.Object{Type: "person", ID: id},
		map[string]audit.Change{"employment_start": {nil, day(p.EmploymentStart)}}, ""); err != nil {
		return err
	}
	if err := insertJob(ctx, tx, org, at, by, id, p.EmploymentStart, j); err != nil {
		return err
	}
	if c != nil {
		if err := insertClassification(ctx, tx, org, at, by, id, p.EmploymentStart, *c); err != nil {
			return err
		}
	}
	if err := syncReportingLine(ctx, tx, org, at, by, id, j.Manager, p.EmploymentStart); err != nil {
		return err
	}
	return nil
}

func dayPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := day(*t)
	return &s
}

func insertJob(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, from time.Time, j people.Job) error {
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_job (id, person_id, effective_from, job_title, department, worksite, employment_type, manager_person_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, id, person, day(from), j.Title, orNil(j.Department), j.Worksite, j.EmploymentType, j.Manager); err != nil {
		return historyError(err)
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.person.job_changed", audit.Object{Type: "person_job", ID: id},
		map[string]audit.Change{"person": {nil, person.String()}, "effective_from": {nil, day(from)}, "job_title": {nil, j.Title}, "department": {nil, j.Department}, "employment_type": {nil, j.EmploymentType}, "worksite": {nil, j.Worksite}}, "")
}

func insertClassification(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, from time.Time, c people.Classification) error {
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.person_classification (id, person_id, effective_from, contract_type, ea_covered, part_iv_class, residency, pr_start_date, birth_date)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, id, person, day(from), c.ContractType, c.EACovered, c.PartIVClass, c.Residency, dayPtr(c.PRStart), dayPtr(c.BirthDate)); err != nil {
		return historyError(err)
	}
	// The date of birth is restricted: the event says a classification changed, never the date.
	return Audit(ctx, tx, org, at, user(by), "nemura.person.classification_changed", audit.Object{Type: "person_classification", ID: id},
		map[string]audit.Change{"person": {nil, person.String()}, "effective_from": {nil, day(from)}, "contract_type": {nil, c.ContractType}, "ea_covered": {nil, c.EACovered},
			"part_iv_class": {nil, c.PartIVClass}, "residency": {nil, c.Residency}}, "")
}

// ChangeJob ends the person's current job on the day before from and starts the new one on from: job, manager, department, worksite and
// employment type are history, never edited (NM-PPL-01). A change from a day on or before the current job's start is refused.
func ChangeJob(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, from time.Time, j people.Job) error {
	if err := j.Check(); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, "UPDATE nemura.person_job SET effective_to = $2 WHERE person_id = $1 AND effective_to IS NULL AND effective_from < $2", person, day(from))
	if err != nil {
		return historyError(err)
	}
	if tag.RowsAffected() != 1 {
		return &people.Refusal{Code: people.CodeHistory, Message: "The person has no current job that began before that day: choose a later day."}
	}
	var open uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT id FROM nemura.person_job WHERE person_id = $1 AND effective_to = $2", person, day(from)).Scan(&open); err != nil {
		return err
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.person.job_ended", audit.Object{Type: "person_job", ID: open}, map[string]audit.Change{"effective_to": {nil, day(from)}}, ""); err != nil {
		return err
	}
	if err := insertJob(ctx, tx, org, at, by, person, from, j); err != nil {
		return err
	}
	return syncReportingLine(ctx, tx, org, at, by, person, j.Manager, from)
}

// SetClassification ends the current classification on from and starts the new one.
func SetClassification(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, from time.Time, c people.Classification) error {
	if err := c.Check(); err != nil {
		return err
	}
	var open uuid.UUID
	err := tx.QueryRow(ctx, "UPDATE nemura.person_classification SET effective_to = $2 WHERE person_id = $1 AND effective_to IS NULL AND effective_from < $2 RETURNING id", person, day(from)).Scan(&open)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No current classification before that day: the first one, or a day that is not later than the current start.
		var n int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM nemura.person_classification WHERE person_id = $1", person).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return &people.Refusal{Code: people.CodeHistory, Message: "The person has a classification that began on or after that day: choose a later day."}
		}
	case err != nil:
		return historyError(err)
	default:
		if err := Audit(ctx, tx, org, at, user(by), "nemura.person.classification_ended", audit.Object{Type: "person_classification", ID: open}, map[string]audit.Change{"effective_to": {nil, day(from)}}, ""); err != nil {
			return err
		}
	}
	return insertClassification(ctx, tx, org, at, by, person, from, c)
}

// UpdatePerson changes a person's name, contact, probation end or leaving; the start date and the login are set once.
func UpdatePerson(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, p people.Person) error {
	var start time.Time
	if err := tx.QueryRow(ctx, "SELECT employment_start FROM nemura.person WHERE id = $1", person).Scan(&start); errors.Is(err, pgx.ErrNoRows) {
		return &people.Refusal{Code: people.CodeNotFound, Message: "There is no such person in this organisation."}
	} else if err != nil {
		return err
	}
	p.EmploymentStart = start
	if err := p.Check(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.person SET legal_name = $2, preferred_name = $3, work_email = $4, phone = $5, probation_end = $6, leaving_date = $7, leaving_reason = $8 WHERE id = $1`,
		person, p.LegalName, orNil(p.PreferredName), orNil(p.WorkEmail), orNil(p.Phone), dayPtr(p.ProbationEnd), dayPtr(p.LeavingDate), orNil(p.LeavingReason)); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.person.updated", audit.Object{Type: "person", ID: person}, map[string]audit.Change{"leaving_date": {nil, dayPtr(p.LeavingDate)}}, "")
}

// syncReportingLine keeps the members' reporting line, which scopes what a manager may see, in step with the person's manager when both have
// a login: the old line ends on from and a new one starts (a manager needs a login to act, so a person without one has no line to keep).
func syncReportingLine(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by, person uuid.UUID, manager *uuid.UUID, from time.Time) error {
	var reportMember *uuid.UUID
	if err := tx.QueryRow(ctx, "SELECT member_id FROM nemura.person WHERE id = $1", person).Scan(&reportMember); err != nil {
		return err
	}
	if reportMember == nil {
		return nil
	}
	rows, err := tx.Query(ctx, "SELECT id FROM nemura.reporting_line WHERE report_id = $1 AND effective_to IS NULL", *reportMember)
	if err != nil {
		return err
	}
	var open []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		open = append(open, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var managerMember *uuid.UUID
	if manager != nil {
		if err := tx.QueryRow(ctx, "SELECT member_id FROM nemura.person WHERE id = $1", *manager).Scan(&managerMember); err != nil {
			return err
		}
	}
	for _, id := range open {
		var same bool
		if managerMember != nil {
			if err := tx.QueryRow(ctx, "SELECT manager_id = $2 FROM nemura.reporting_line WHERE id = $1", id, *managerMember).Scan(&same); err != nil {
				return err
			}
		}
		if same {
			return nil // already the line
		}
		if err := EndReportingLine(ctx, tx, org, at, by, id, from); err != nil {
			return err
		}
	}
	if managerMember == nil {
		return nil
	}
	_, err = AddReportingLine(ctx, tx, org, at, by, *reportMember, *managerMember, from)
	return err
}

// PersonRow is a person as the directory and the record show them on a day.
type PersonRow struct {
	ID                                  uuid.UUID
	MemberID                            *uuid.UUID
	LegalName, PreferredName, WorkEmail string
	Phone                               string
	Start                               time.Time
	ProbationEnd, LeavingDate           *time.Time
	LeavingReason                       string
	Title, Department, Worksite, Type   string
	ManagerID                           *uuid.UUID
	ManagerName                         string
}

const personColumns = `p.id, p.member_id, p.legal_name, coalesce(p.preferred_name, ''), coalesce(p.work_email, ''), coalesce(p.phone, ''), p.employment_start,
 p.probation_end, p.leaving_date, coalesce(p.leaving_reason, ''), coalesce(j.job_title, ''), coalesce(j.department, ''), coalesce(j.worksite, ''), coalesce(j.employment_type, ''),
 j.manager_person_id, coalesce(m.legal_name, '')`

const personFrom = ` FROM nemura.person p
LEFT JOIN nemura.person_job j ON j.organisation_id = p.organisation_id AND j.person_id = p.id AND j.effective_from <= $1 AND (j.effective_to IS NULL OR j.effective_to > $1)
LEFT JOIN nemura.person m ON m.organisation_id = p.organisation_id AND m.id = j.manager_person_id`

func scanPerson(row pgx.Row) (PersonRow, error) {
	var r PersonRow
	err := row.Scan(&r.ID, &r.MemberID, &r.LegalName, &r.PreferredName, &r.WorkEmail, &r.Phone, &r.Start, &r.ProbationEnd, &r.LeavingDate, &r.LeavingReason,
		&r.Title, &r.Department, &r.Worksite, &r.Type, &r.ManagerID, &r.ManagerName)
	return r, err
}

// People lists the people who work for the organisation on a day (started, and not left before it), by name (NM-PPL-06).
func People(ctx context.Context, tx pgx.Tx, on time.Time) ([]PersonRow, error) {
	rows, err := tx.Query(ctx, "SELECT "+personColumns+personFrom+" WHERE p.employment_start <= $1 AND (p.leaving_date IS NULL OR p.leaving_date >= $1) ORDER BY lower(p.legal_name), p.id", day(on))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonRow
	for rows.Next() {
		r, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PersonOn is one person as they are on a day, or a not-found refusal.
func PersonOn(ctx context.Context, tx pgx.Tx, id uuid.UUID, on time.Time) (PersonRow, error) {
	r, err := scanPerson(tx.QueryRow(ctx, "SELECT "+personColumns+personFrom+" WHERE p.id = $2", day(on), id))
	if errors.Is(err, pgx.ErrNoRows) {
		return PersonRow{}, &people.Refusal{Code: people.CodeNotFound, Message: "There is no such person in this organisation."}
	}
	return r, err
}

// JobRow is one row of a person's job history.
type JobRow struct {
	From                              time.Time
	To                                *time.Time
	Title, Department, Worksite, Type string
	ManagerID                         *uuid.UUID
}

// JobHistory is every job record of a person, oldest first.
func JobHistory(ctx context.Context, tx pgx.Tx, person uuid.UUID) ([]JobRow, error) {
	rows, err := tx.Query(ctx, "SELECT effective_from, effective_to, job_title, coalesce(department, ''), worksite, employment_type, manager_person_id FROM nemura.person_job WHERE person_id = $1 ORDER BY effective_from", person)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobRow
	for rows.Next() {
		var j JobRow
		if err := rows.Scan(&j.From, &j.To, &j.Title, &j.Department, &j.Worksite, &j.Type, &j.ManagerID); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// ClassificationOn is the person's classification on a day, or false when there is none.
func ClassificationOn(ctx context.Context, tx pgx.Tx, person uuid.UUID, on time.Time) (people.Classification, bool, error) {
	var c people.Classification
	err := tx.QueryRow(ctx, `SELECT contract_type, ea_covered, part_iv_class, residency, pr_start_date, birth_date FROM nemura.person_classification
WHERE person_id = $1 AND effective_from <= $2 AND (effective_to IS NULL OR effective_to > $2)`, person, day(on)).Scan(&c.ContractType, &c.EACovered, &c.PartIVClass, &c.Residency, &c.PRStart, &c.BirthDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}
