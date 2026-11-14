package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The commit's problem codes (ENG-23).
const (
	CodeImport = "MIGRATION-13" // a record the commit could not import: its row, what is wrong and how to fix it
	CodeCommit = "MIGRATION-14" // the import cannot be committed or run: no valid workbook, no signed mapping, or committed already
)

// BatchSize is the most records one batch imports (rev 0.258).
const BatchSize = 50

// Made is the Fyniq record a workbook row became.
type Made struct {
	Type string
	ID   uuid.UUID
}

// Record is one record the commit imports, in the plan's order: its business key from its row, unique in the commit,
// what the row becomes, and where in the workbook it is (design D3). Its make writes it through the domain command a
// person's request uses, in the unit of work that keeps its migration record.
type Record struct {
	Key  string
	Kind string
	Origin
	make func(ctx context.Context, u storage.UnitOfWork, e *Env) (Made, error)
}

// Env is what a record's make reads besides the workbook and the mapping: the records imported before it by key, and
// the time of the batch.
type Env struct {
	made map[string]uuid.UUID
	Now  time.Time
	// The store, organisation and clock of the batch, for the services a record's command goes through: they join its
	// unit of work.
	Store storage.Store
	Org   uuid.UUID
	Clock ports.Clock
}

// clockOf is a clock that reads now.
type clockOf func() time.Time

func (c clockOf) Now() time.Time { return c() }

// Made is the record an earlier row became, by its key.
func (e *Env) Made(key string) (uuid.UUID, bool) {
	id, ok := e.made[key]
	return id, ok
}

// Refusal is what a domain rule said about a record: its field, what is wrong and how to fix it.
type Refusal struct {
	Code, Field, Message, Fix string
}

// Refused are a record's refusals, every rule that failed at once.
type Refused []Refusal

func (r Refused) Error() string {
	parts := make([]string, len(r))
	for i, x := range r {
		parts[i] = fmt.Sprintf("%s: %s %s", x.Code, x.Field, x.Message)
	}
	return strings.Join(parts, "; ")
}

// refused turns a domain package's problems, each a {Code, Field, Message, Fix}, into the record's refusal; nil for none.
func refused[P ~struct{ Code, Field, Message, Fix string }](ps []P) error {
	if len(ps) == 0 {
		return nil
	}
	out := make(Refused, len(ps))
	for i, p := range ps {
		out[i] = Refusal(p)
	}
	return out
}

// ImportError is a record the commit could not import, with its row: the batch stops at it, and the records before
// it stay imported.
type ImportError struct {
	Record Record
	Err    error
}

func (e *ImportError) Error() string {
	return fmt.Sprintf("%s row %d (%s): %v", e.Record.Sheet, e.Record.Row, e.Record.Key, e.Err)
}

func (e *ImportError) Unwrap() error { return e.Err }

// Plan is what the commit imports, in order, for the founders' path (§17.1 M3): the set-up, then the history of the
// current financial year or the clean start's balances, then what both paths import. A key two rows share is an error
// the validation reports, as the second row would never import.
func (w *Workbook) Plan(m Mapping, path string) ([]Record, []Problem) {
	var out []Record
	out = append(out, w.setUp(m, path)...)
	switch path {
	case storage.PathHistory:
		out = append(out, w.history(m)...)
	case storage.PathCleanStart:
		out = append(out, w.cleanStart(m)...)
	}
	seen := map[string]Origin{}
	var probs []Problem
	for _, r := range out {
		if first, ok := seen[r.Key]; ok {
			probs = append(probs, Problem{Sheet: r.Sheet, Row: r.Row, Severity: Error, Code: CodeImport,
				Message: fmt.Sprintf("this row imports as %s, as %s row %d does", r.Key, first.Sheet, first.Row),
				Fix:     "Give each row its own name or code in the workbook"})
			continue
		}
		seen[r.Key] = r.Origin
	}
	return out, probs
}

// errImported and errNotRunning end a record's unit of work without writing: another batch imported the record, or
// committed the commit, since this batch read it.
var (
	errImported   = errors.New("migration: the record is imported already")
	errNotRunning = errors.New("migration: the commit is not running")
)

// Progress is how far a commit is: its records imported and planned, and the next to import.
type Progress struct {
	Imported int // by this batch
	Done     int
	Total    int
	Next     *Record
}

// Batch imports up to n of the plan's records not yet imported, in order, each in its own unit of work with its
// migration record, as by (rev 0.258). It stops at the first record that cannot be imported and returns it as an
// ImportError with the progress so far: the records before it stay imported, and the next batch starts again at it.
// When every record is imported, the commit is committed.
func Batch(ctx context.Context, store storage.Store, org uuid.UUID, c storage.MigrationCommit, plan []Record, n int, by string,
	now func() time.Time) (Progress, error) {
	p := Progress{Total: len(plan)}
	e := &Env{made: map[string]uuid.UUID{}, Store: store, Org: org, Clock: clockOf(now)}
	err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		done, err := u.Migrations().Records(ctx, c.ID)
		for _, r := range done {
			e.made[r.Key] = r.RecordID
		}
		return err
	})
	if err != nil {
		return p, err
	}
	p.Done = len(e.made)
	for i := range plan {
		r := plan[i]
		if _, ok := e.made[r.Key]; ok {
			continue
		}
		if p.Imported == n {
			p.Next = &plan[i]
			return p, nil
		}
		e.Now = now()
		var made Made
		err := store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			cur, err := u.Migrations().LockCommit(ctx, c.ID)
			if err != nil {
				return err
			}
			if cur.Status != storage.CommitRunning {
				return errNotRunning
			}
			// Another batch may have imported it since this one read the commit's records.
			if x, err := u.Migrations().RecordByKey(ctx, c.ID, r.Key); err == nil {
				made = Made{Type: x.RecordType, ID: x.RecordID}
				return errImported
			} else if !errors.Is(err, ports.ErrNotFound) {
				return err
			}
			if made, err = r.make(ctx, u, e); err != nil {
				return err
			}
			_, err = u.Migrations().Import(ctx, storage.MigrationRecord{RunID: c.RunID, CommitID: c.ID, Key: r.Key, Kind: r.Kind, Sheet: r.Sheet,
				Row: r.Row, RecordType: made.Type, RecordID: made.ID, ImportedBy: by, ImportedAt: e.Now})
			return err
		})
		switch {
		case errors.Is(err, errImported):
			e.made[r.Key] = made.ID
			p.Done++
			continue
		case errors.Is(err, errNotRunning):
			return p, nil
		case err != nil:
			var se *storage.Error
			if errors.As(err, &se) && (se.Code == storage.CodeRetry || se.Code == storage.CodeLockTimeout) {
				return p, err // nothing wrong with the row: the batch is tried again
			}
			p.Next = &plan[i]
			return p, &ImportError{Record: r, Err: err}
		}
		e.made[r.Key] = made.ID
		p.Imported++
		p.Done++
	}
	if p.Done < len(plan) {
		return p, nil
	}
	err = store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		cur, err := u.Migrations().LockCommit(ctx, c.ID)
		if err != nil || cur.Status != storage.CommitRunning {
			return err
		}
		_, err = u.Migrations().Committed(ctx, c.ID, now())
		return err
	})
	return p, err
}
