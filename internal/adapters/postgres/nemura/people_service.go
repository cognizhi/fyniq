package nemura

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/people"
	"github.com/cognizhi/fyniq/internal/ports"
)

// PeopleService is the people record behind the API (NM-PPL-01, NM-PPL-06, NM-PPL-08): each call is one transaction for the caller's
// organisation, with the audit events of its changes.
type PeopleService struct {
	Store *Store
	Clock ports.Clock
}

var _ api.People = PeopleService{}

func (p PeopleService) in(ctx context.Context, s api.Session, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return p.Store.InOrg(ctx, s.Organisation, fn)
}

func dateStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.DateOnly)
}

func idStr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func (p PeopleService) Directory(ctx context.Context, s api.Session, on time.Time) ([]api.DirectoryEntry, error) {
	var out []api.DirectoryEntry
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := People(ctx, tx, on)
		for _, r := range rows {
			out = append(out, api.DirectoryEntry{ID: r.ID.String(), Name: r.LegalName, PreferredName: r.PreferredName, JobTitle: r.Title, Department: r.Department,
				WorkEmail: r.WorkEmail, ManagerID: idStr(r.ManagerID), ManagerName: r.ManagerName})
		}
		return err
	})
	return out, err
}

func (p PeopleService) Relation(ctx context.Context, s api.Session, id uuid.UUID) (own, report bool, err error) {
	err = p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		var member *uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT member_id FROM nemura.person WHERE id = $1", id).Scan(&member); errors.Is(err, pgx.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		if member == nil {
			return nil
		}
		own = *member == s.Member
		if !own {
			return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nemura.reporting_line WHERE report_id = $1 AND manager_id = $2
AND effective_from <= current_date AND (effective_to IS NULL OR effective_to > current_date))`, *member, s.Member).Scan(&report)
		}
		return nil
	})
	return
}

func (p PeopleService) Get(ctx context.Context, s api.Session, id uuid.UUID, on time.Time, classification bool) (api.Record, error) {
	var rec api.Record
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		r, err := PersonOn(ctx, tx, id, on)
		if err != nil {
			return err
		}
		rec = api.Record{ID: r.ID.String(), LegalName: r.LegalName, PreferredName: r.PreferredName, WorkEmail: r.WorkEmail, Phone: r.Phone, EmploymentStart: r.Start.Format(time.DateOnly),
			ProbationEnd: dateStr(r.ProbationEnd), LeavingDate: dateStr(r.LeavingDate), LeavingReason: r.LeavingReason, HasLogin: r.MemberID != nil, Jobs: []api.Job{}}
		jobs, err := JobHistory(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			rec.Jobs = append(rec.Jobs, api.Job{From: j.From.Format(time.DateOnly), To: dateStr(j.To), JobTitle: j.Title, Department: j.Department, Worksite: j.Worksite, EmploymentType: j.Type, ManagerID: idStr(j.ManagerID)})
		}
		if classification {
			c, ok, err := ClassificationOn(ctx, tx, id, on)
			if err != nil {
				return err
			}
			if ok {
				rec.Classification = &api.Classification{ContractType: c.ContractType, EACovered: c.EACovered, PartIVClass: c.PartIVClass, Residency: c.Residency, PRStart: dateStr(c.PRStart), BirthDate: dateStr(c.BirthDate)}
			}
		}
		return nil
	})
	return rec, err
}

func (p PeopleService) Create(ctx context.Context, s api.Session, id uuid.UUID, per people.Person, j people.Job, c *people.Classification) error {
	err := p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if err := addPersonWithID(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, id, per, j, c); err != nil {
			return err
		}
		// A company that invites its people to Fyniq gets a task for each new one (NM-ONB-02).
		return RaiseJoinTask(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, id)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return api.ErrPersonExists
	}
	return err
}

func (p PeopleService) Update(ctx context.Context, s api.Session, id uuid.UUID, per people.Person) error {
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		return UpdatePerson(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, id, per)
	})
}

func (p PeopleService) ChangeJob(ctx context.Context, s api.Session, id uuid.UUID, from time.Time, j people.Job) error {
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, id, from); err != nil {
			return err
		}
		return ChangeJob(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, id, from, j)
	})
}

func (p PeopleService) SetClassification(ctx context.Context, s api.Session, id uuid.UUID, from time.Time, c people.Classification) error {
	return p.in(ctx, s, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := PersonOn(ctx, tx, id, from); err != nil {
			return err
		}
		return SetClassification(ctx, tx, s.Organisation, p.Clock.Now(), s.Member, id, from, c)
	})
}
