package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/fieldseal"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Person keys (PRD-001 §11.8, rev 0.244; p1-pdpa-retention D8): an event about a person seals their personal values
// under a data key of their own for the event's financial year, wrapped by the deployment's key-encryption key. The
// key is made the first time the person's values of that year are sealed, and only the approved shredding run deletes
// it (FY317).

// personKey is a person's data key for a financial year, unwrapped for this unit of work.
type personKey struct {
	id  uuid.UUID
	dek []byte
}

// wrapAAD binds a wrapped person key to its organisation, person and financial year.
func wrapAAD(org uuid.UUID, person string, cohort time.Time) []byte {
	return []byte("fyniq-person-key@1:" + org.String() + ":" + person + ":" + cohort.Format(time.DateOnly))
}

// subject is the person an event is about, if any (audit.Subject): a contact only when it is an individual or a
// director.
func (u *unitOfWork) subject(ctx context.Context, objectType string, objectID uuid.UUID, changes map[string]audit.Change) (string, bool, error) {
	person, ok := audit.Subject(objectType, objectID, changes)
	if !ok || objectType != "contact" {
		return person, ok, nil
	}
	is, err := u.q.ContactIsPerson(ctx, objectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, mapError(err)
	}
	return person, is, nil
}

// cohort is the financial year a value sealed at is sealed for (audit.Cohort), in the organisation's time zone.
func (u *unitOfWork) cohort(ctx context.Context, at time.Time) (time.Time, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return time.Time{}, err
	}
	loc, err := time.LoadLocation(o.Profile.TimeZone)
	if err != nil || o.Profile.TimeZone == "" {
		loc = time.UTC
	}
	cal, err := period.CalendarOf(o.Profile)
	return audit.Cohort(cal, err == nil, at.In(loc)), nil
}

// personKey is person's key for the financial year that holds at, made the first time it is needed.
func (u *unitOfWork) personKey(ctx context.Context, person string, at time.Time) (personKey, error) {
	cohort, err := u.cohort(ctx, at)
	if err != nil {
		return personKey{}, err
	}
	cache := person + "@" + cohort.Format(time.DateOnly)
	if k, ok := u.personKeys[cache]; ok {
		return k, nil
	}
	aad := wrapAAD(u.org, person, cohort)
	row, err := u.q.GetPersonKey(ctx, sqlcdb.GetPersonKeyParams{Person: person, Cohort: cohort})
	if errors.Is(err, pgx.ErrNoRows) {
		dek, err := fieldseal.NewDataKey()
		if err != nil {
			return personKey{}, err
		}
		wrapped, err := u.store.keys.Seal(ctx, dek, aad)
		if err != nil {
			return personKey{}, err
		}
		id, err := u.q.InsertPersonKey(ctx, sqlcdb.InsertPersonKeyParams{Person: person, Cohort: cohort, Wrapped: wrapped})
		switch {
		case err == nil:
			k := personKey{id: id, dek: dek}
			u.keepPersonKey(cache, k)
			return k, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return personKey{}, mapError(err)
		}
		// Another transaction made it first: use theirs.
		row, err = u.q.GetPersonKey(ctx, sqlcdb.GetPersonKeyParams{Person: person, Cohort: cohort})
		if err != nil {
			return personKey{}, mapError(err)
		}
	} else if err != nil {
		return personKey{}, mapError(err)
	}
	dek, err := u.store.keys.Open(ctx, row.Wrapped, aad)
	if err != nil {
		return personKey{}, err
	}
	k := personKey{id: row.ID, dek: dek}
	u.keepPersonKey(cache, k)
	return k, nil
}

func (u *unitOfWork) keepPersonKey(cache string, k personKey) {
	if u.personKeys == nil {
		u.personKeys = map[string]personKey{}
	}
	u.personKeys[cache] = k
}

// sealPersonal seals the personal values of e, an event about person (audit.SealPersonal).
func (u *unitOfWork) sealPersonal(ctx context.Context, e *audit.Event, person string, at time.Time) error {
	var key *personKey
	sealed, err := audit.SealPersonal(e.Changes, func(field string, plaintext []byte) (map[string]any, error) {
		if key == nil {
			k, err := u.personKey(ctx, person, at)
			if err != nil {
				return nil, err
			}
			key = &k
		}
		ct, err := fieldseal.SealWith(key.dek, plaintext, audit.SealAAD(u.org, e.EventID, field))
		if err != nil {
			return nil, err
		}
		return audit.Sealed(key.id, ct), nil
	})
	if err != nil {
		return err
	}
	e.Changes = sealed
	return nil
}

func (r auditTrail) OpenSealed(ctx context.Context, values []storage.SealedValue) ([]storage.OpenedValue, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(values))
	for _, v := range values {
		ids = append(ids, v.Key)
	}
	rows, err := r.u.q.ListPersonKeys(ctx, ids)
	if err != nil {
		return nil, mapError(err)
	}
	keys := make(map[uuid.UUID][]byte, len(rows))
	for _, k := range rows {
		dek, err := r.u.store.keys.Open(ctx, k.Wrapped, wrapAAD(r.u.org, k.Person, k.Cohort))
		if err != nil {
			return nil, err
		}
		keys[k.ID] = dek
	}
	var gone []uuid.UUID
	for _, v := range values {
		if _, ok := keys[v.Key]; !ok {
			gone = append(gone, v.Key)
		}
	}
	shredded := map[uuid.UUID]sqlcdb.ShreddedKeysRow{}
	if len(gone) > 0 {
		rows, err := r.u.q.ShreddedKeys(ctx, gone)
		if err != nil {
			return nil, mapError(err)
		}
		for _, s := range rows {
			shredded[s.Key] = s
		}
	}
	out := make([]storage.OpenedValue, len(values))
	for i, v := range values {
		dek, ok := keys[v.Key]
		if !ok {
			out[i] = storage.OpenedValue{Shredded: true}
			if s, ok := shredded[v.Key]; ok {
				cohort := s.Cohort
				out[i].Cohort, out[i].ShreddedOn = &cohort, s.DoneAt
			}
			continue
		}
		plain, err := fieldseal.OpenWith(dek, v.Text, audit.SealAAD(r.u.org, v.EventID, v.Field))
		if err != nil {
			return nil, err
		}
		out[i] = storage.OpenedValue{Value: string(plain)}
	}
	return out, nil
}
