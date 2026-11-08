// Package postgres is the PostgreSQL adapter of the storage port (ADR-D1, ADR-T8). It is the only
// package that holds SQL, pgx or sqlc (NFR-PORT-01), and the only place that opens, commits or rolls
// back a transaction (NFR-PORT-07). It runs on PostgreSQL 18 locally and on Cloud SQL, and on any
// PostgreSQL 17+ that passes the conformance and attack suites (NFR-PORT-02).
package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// DefaultLockTimeout is how long a unit of work waits for a row lock before it fails with
// storage.CodeLockTimeout (ENG-47: "a lock wait over 5 s (setting)").
const DefaultLockTimeout = 5 * time.Second

// Store implements storage.Store on a pgx pool. Each service connects as its own role (SEC-03).
type Store struct {
	pool        *pgxpool.Pool
	lockTimeout time.Duration
	clock       ports.Clock   // stamps audit events
	actor       storage.Actor // the service identity, when a request names no actor
	changed     storage.Changed
	// fingerprint proves a sensitive value in an audit event (AUD-02): ports.Cipher.Fingerprint.
	fingerprint func(ctx context.Context, scope, value []byte) ([]byte, error)
}

var _ storage.Store = (*Store)(nil)

// Option configures a Store.
type Option func(*Store)

// WithLockTimeout sets how long a unit of work waits for a row lock.
func WithLockTimeout(d time.Duration) Option { return func(s *Store) { s.lockTimeout = d } }

// WithClock sets the clock that stamps audit events (DEV-05; tests freeze it).
func WithClock(c ports.Clock) Option { return func(s *Store) { s.clock = c } }

// WithActor sets the service identity recorded when a request names no actor. The default is the
// database role the store connects as.
func WithActor(a storage.Actor) Option { return func(s *Store) { s.actor = a } }

// WithFingerprints sets the keyed hash that proves sensitive values in audit events (AUD-02, PRD-001 rev 0.230): the
// deployment's ports.Cipher, whose fingerprint key is held in Cloud KMS, or locally in a key file. Without it a store
// keys its fingerprints with a random key of its own: a value is still never kept, but proofs written by two stores, or
// by one store before and after a restart, do not compare. The wiring always sets it.
func WithFingerprints(c ports.Cipher) Option {
	return func(s *Store) {
		if c != nil {
			s.fingerprint = c.Fingerprint
		}
	}
}

// OnChange sets what a unit of work calls once its work is done, with the object types of the events it recorded
// (storage.Changed): the outbox item that reruns the checks a change concerns (FR-CTL-01). Set it before the store is
// used.
func (s *Store) OnChange(fn storage.Changed) { s.changed = fn }

// Open connects to url, for example FYNIQ_DATABASE_URL, and checks the connection.
func Open(ctx context.Context, url string, opts ...Option) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse the database URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to the database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to the database: %w", err)
	}
	return New(pool, opts...), nil
}

// Connect is Open without the first round trip: the pool connects when first used, so a service
// starts while the database is still coming up and reports it on /readyz.
func Connect(url string, opts ...Option) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse the database URL: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to the database: %w", err)
	}
	return New(pool, opts...), nil
}

// New wraps an existing pool.
func New(pool *pgxpool.Pool, opts ...Option) *Store {
	s := &Store{pool: pool, lockTimeout: DefaultLockTimeout, clock: clock.System{},
		actor: storage.Actor{Type: "service", ID: pool.Config().ConnConfig.User}, fingerprint: ownKey()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ownKey is a store's own fingerprint key when the wiring gives none: random, kept in memory only, laid out as
// fieldseal's (the scope's length, the scope, the value).
func ownKey() func(ctx context.Context, scope, value []byte) ([]byte, error) {
	key := make([]byte, 32)
	_, _ = rand.Read(key) // never fails (crypto/rand)
	return func(_ context.Context, scope, value []byte) ([]byte, error) {
		m := hmac.New(sha256.New, key)
		m.Write(binary.BigEndian.AppendUint32(nil, uint32(len(scope)))) // #nosec G115 -- a scope is an organisation's ID
		m.Write(scope)
		m.Write(value)
		return m.Sum(nil), nil
	}
}

// Close closes the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping checks the database answers, for /readyz.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// CreateOrganisation implements storage.Store.
func (s *Store) CreateOrganisation(ctx context.Context, o storage.NewOrganisation) (storage.Organisation, error) {
	var out storage.Organisation
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlcdb.Queries) error {
		id, err := q.NewOrganisationID(ctx)
		if err != nil {
			return err
		}
		if err := s.setTenant(ctx, q, id); err != nil {
			return err
		}
		row, err := q.InsertOrganisation(ctx, sqlcdb.InsertOrganisationParams{Name: o.Name, BaseCurrency: o.BaseCurrency})
		if errors.Is(err, pgx.ErrNoRows) {
			return &storage.Error{Code: storage.CodeAmount, Message: fmt.Sprintf("currency %q is not an ISO 4217 code Fyniq knows", o.BaseCurrency),
				Hint: "Choose the base currency from the list of currencies"}
		}
		if err != nil {
			return err
		}
		out = storage.Organisation{ID: row.OrganisationID, Name: row.Name, BaseCurrency: row.BaseCurrency,
			BaseMinorUnits: int(row.BaseMinorUnits), Version: int(row.Version)}
		u := &unitOfWork{q: q, store: s, org: id}
		version := 1
		if err := u.event(ctx, "organisation.created", "organisation", id, &version, map[string][2]any{
			"name": {nil, row.Name}, "base_currency": {nil, row.BaseCurrency}}); err != nil {
			return err
		}
		return u.flush(ctx)
	})
	return out, err
}

// Do implements storage.Store.
func (s *Store) Do(ctx context.Context, org uuid.UUID, fn func(ctx context.Context, uow storage.UnitOfWork) error) error {
	if org == uuid.Nil {
		return &storage.Error{Code: storage.CodeNoTenant, Message: "no organisation was given for the unit of work"}
	}
	if open, ok := ctx.Value(openUnit{}).(*unitOfWork); ok && open.store == s && !open.done {
		if open.org != org {
			return &storage.Error{Code: storage.CodeNoTenant, Message: "a unit of work for another organisation is already open",
				Hint: "Run the work for each organisation in its own unit of work"}
		}
		return fn(ctx, open) // joins it: commits or rolls back with it
	}
	var u *unitOfWork
	var attempts []*unitOfWork // a retried attempt rolled back: its AfterRollback runs too
	err := s.inTx(ctx, func(tx pgx.Tx, q *sqlcdb.Queries) error {
		if err := s.setTenant(ctx, q, org); err != nil {
			return err
		}
		u = &unitOfWork{q: q, store: s, org: org}
		attempts = append(attempts, u)
		defer func() { u.done = true }()
		inner := context.WithValue(ctx, openUnit{}, u)
		if err := fn(inner, u); err != nil {
			return err
		}
		if s.changed != nil && len(u.pending) > 0 {
			if err := s.changed(inner, u, org, u.changedTypes()); err != nil {
				return err
			}
		}
		// The audit events go last, just before commit: the chain head is the last lock (ENG-47).
		if err := u.flush(ctx); err != nil {
			return err
		}
		if storage.IsPreview(ctx) {
			// Every check the commit would run, then roll back: preview and commit cannot disagree.
			if err := q.CheckDeferred(ctx); err != nil {
				return err
			}
			return storage.ErrPreviewed
		}
		return nil
	})
	for _, a := range attempts {
		if err == nil && a == u {
			for _, f := range a.afterCommit {
				f(ctx)
			}
			continue
		}
		for _, f := range a.afterRollback {
			f(ctx)
		}
	}
	return err
}

// openUnit marks the ctx a unit of work gives its fn, so a Do inside it joins it.
type openUnit struct{}

func (s *Store) setTenant(ctx context.Context, q *sqlcdb.Queries, org uuid.UUID) error {
	return q.SetTenant(ctx, sqlcdb.SetTenantParams{OrgID: org.String(), LockTimeout: fmt.Sprintf("%dms", s.lockTimeout.Milliseconds())})
}

// Organisations implements storage.Store: every organisation in the tenant directory.
func (s *Store) Organisations(ctx context.Context) ([]uuid.UUID, error) {
	ids, err := sqlcdb.New(s.pool).ListTenants(ctx)
	return ids, mapError(err)
}

// inTx is the one place a transaction begins and ends (NFR-PORT-07).
func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx, q *sqlcdb.Queries) error) (err error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return mapError(err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()
	if err = fn(tx, sqlcdb.New(tx)); err != nil {
		return mapError(err)
	}
	// Deferred constraint triggers (INV-01) run here, so commit errors are mapped too.
	if err = tx.Commit(ctx); err != nil {
		return mapError(err)
	}
	return nil
}
