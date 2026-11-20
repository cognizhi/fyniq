package nemura

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/erasure"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
)

// Erasure of a former employee's personal data (PRD-002 NM-SEC-08). An Owner or an HR administrator asks once the person's retention periods have run, a second one approves, and
// the worker files a signed entry in the register outside the database before it scrubs; after a restore the entry is applied again. See db/nemura/migrations/000058.

// ErasureCapability is Appendix B's row for it.
const ErasureCapability = "Erasure of a former employee's data (NM-SEC-08)"

// KindErasure is the outbox kind of an approved erasure.
const KindErasure = "people.erasure"

func canErase(held []authz.Role, right authz.Right) bool {
	for _, r := range held {
		if authz.Can(r, ErasureCapability, right) {
			return true
		}
	}
	return false
}

// ErasureView is an order, by identity.
type ErasureView struct {
	ID, Person    uuid.UUID
	State, Reason string
	EligibleOn    time.Time
	RequestedBy   uuid.UUID
	ApprovedBy    *uuid.UUID
	RequestedAt   time.Time
	ExecutedAt    *time.Time
}

// RequestErasure asks for a former employee's personal data to be erased. It refuses a person who has not left, retention periods that have not run, a legal hold that keeps their
// records and an access request not yet answered, each with a plain code.
func RequestErasure(ctx context.Context, tx pgx.Tx, org uuid.UUID, at, day time.Time, by uuid.UUID, held []authz.Role, person uuid.UUID, reason string) (uuid.UUID, error) {
	if !canErase(held, authz.Create) {
		return uuid.Nil, &erasure.Refusal{Code: "NM_ROLE_REQUIRED", Message: "Only an Owner or an HR administrator asks for a former employee's data to be erased."}
	}
	var left *time.Time
	var erased *time.Time
	if err := tx.QueryRow(ctx, `SELECT leaving_date, erased_at FROM nemura.person WHERE id = $1`, person).Scan(&left, &erased); err != nil {
		return uuid.Nil, &erasure.Refusal{Code: "NM_PERSON_NOT_FOUND", Message: "There is no such person in this organisation."}
	}
	if erased != nil {
		return uuid.Nil, &erasure.Refusal{Code: erasure.CodeAlready, Message: "This person's data was erased already."}
	}
	var lastPay *time.Time
	if err := tx.QueryRow(ctx, `SELECT max(month) FROM nemura.payslip WHERE person_id = $1`, person).Scan(&lastPay); err != nil {
		return uuid.Nil, err
	}
	if left == nil {
		return uuid.Nil, erasure.Check(nil, day, day)
	}
	eligible := erasure.EligibleOn(*left, lastPay, 0)
	if err := erasure.Check(left, eligible, day); err != nil {
		return uuid.Nil, err
	}
	holds, err := Holds(ctx, tx, day)
	if err != nil {
		return uuid.Nil, err
	}
	for _, h := range holds {
		if (h.Subject != nil && *h.Subject == person) || (h.Subject == nil && (h.RecordType == "people-records" || h.RecordType == "pay-records")) {
			return uuid.Nil, &erasure.Refusal{Code: erasure.CodeHeld, Message: "A legal hold keeps this person's records. Release the hold first, or leave the records as they are."}
		}
	}
	var open bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nemura.access_request WHERE requester_id = (SELECT member_id FROM nemura.person WHERE id = $1) AND state <> 'answered')`, person).Scan(&open); err != nil {
		return uuid.Nil, err
	}
	if open {
		return uuid.Nil, &erasure.Refusal{Code: erasure.CodeOpenAccessReq, Message: "This person has an access request that is not answered yet: answer it first (NM-SEC-07)."}
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.erasure_order (id, person_id, reason, eligible_on, requested_by, requested_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		id, person, reason, day2(eligible), by, at); err != nil {
		return uuid.Nil, err
	}
	return id, Audit(ctx, tx, org, at, user(by), "nemura.erasure.requested", audit.Object{Type: "erasure_order", ID: id},
		map[string]audit.Change{"person_id": {nil, person.String()}, "eligible_on": {nil, day2(eligible)}}, reason)
}

func day2(t time.Time) string { return t.Format(time.DateOnly) }

// DecideErasure approves or refuses a requested erasure. The approver holds the right and is not the one who asked; an approval queues the worker, which does the erasing.
func DecideErasure(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, held []authz.Role, order uuid.UUID, approve bool) error {
	if !canErase(held, authz.Approve) {
		return &erasure.Refusal{Code: "NM_ROLE_REQUIRED", Message: "Only an Owner or an HR administrator approves an erasure."}
	}
	var requester uuid.UUID
	var state string
	if err := tx.QueryRow(ctx, `SELECT requested_by, state FROM nemura.erasure_order WHERE id = $1 FOR UPDATE`, order).Scan(&requester, &state); err != nil {
		return erasure.ErrNotFound
	}
	if requester == by {
		return &erasure.Refusal{Code: erasure.CodeOwnRequest, Message: "You asked for this erasure: a second Owner or HR administrator approves it."}
	}
	if state != "requested" {
		return &erasure.Refusal{Code: erasure.CodeAlready, Message: "This erasure was decided already."}
	}
	if !approve {
		if _, err := tx.Exec(ctx, `UPDATE nemura.erasure_order SET state = 'refused' WHERE id = $1`, order); err != nil {
			return err
		}
		return Audit(ctx, tx, org, at, user(by), "nemura.erasure.refused", audit.Object{Type: "erasure_order", ID: order}, nil, "")
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.erasure_order SET state = 'approved', approved_by = $2, approved_at = $3 WHERE id = $1`, order, by, at); err != nil {
		return err
	}
	if _, err := Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassWorker, Kind: KindErasure, BusinessKey: "erasure:" + order.String(), Payload: []byte(`{"order":"` + order.String() + `"}`)}); err != nil {
		return err
	}
	return Audit(ctx, tx, org, at, user(by), "nemura.erasure.approved", audit.Object{Type: "erasure_order", ID: order}, nil, "")
}

// Erasures lists the orders, newest first, by identity.
func Erasures(ctx context.Context, tx pgx.Tx) ([]ErasureView, error) {
	rows, err := tx.Query(ctx, `SELECT id, person_id, state, reason, eligible_on, requested_by, approved_by, requested_at, executed_at FROM nemura.erasure_order ORDER BY requested_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ErasureView
	for rows.Next() {
		var v ErasureView
		if err := rows.Scan(&v.ID, &v.Person, &v.State, &v.Reason, &v.EligibleOn, &v.RequestedBy, &v.ApprovedBy, &v.RequestedAt, &v.ExecutedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ErasureRunner is the worker's side: it files the register entry, scrubs, deletes the person's files and tells Fyniq, and after a restore applies the register again.
type ErasureRunner struct {
	Store  *Store
	Blobs  ports.BlobStore // the register's place: a write-once store outside this database ([live-verify] in the cloud: its own bucket)
	Signer ports.Signer
	Clock  ports.Clock
}

// erasedAudits writes the audit events the scrubbed rows each need in the transaction, naming the person by ID and never by name.
func erasedAudits(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, person uuid.UUID, action string, reason string) error {
	var member *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT member_id FROM nemura.person WHERE id = $1`, person).Scan(&member); err != nil {
		return err
	}
	actor := audit.Actor{Type: "service", ID: "nemura-worker"}
	for _, o := range []audit.Object{{Type: "person", ID: person}, {Type: "person_secret", ID: person}, {Type: "person_contact", ID: person}} {
		if err := Audit(ctx, tx, org, at, actor, action, o, map[string]audit.Change{"person_id": {nil, person.String()}}, reason); err != nil {
			return err
		}
	}
	if member != nil {
		return Audit(ctx, tx, org, at, actor, action, audit.Object{Type: "member", ID: *member}, map[string]audit.Change{"person_id": {nil, person.String()}}, reason)
	}
	return nil
}

// Work is the outbox work of an approved erasure.
func (r *ErasureRunner) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row outbox.Row) error {
		var p struct {
			Order uuid.UUID `json:"order"`
		}
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.Order == uuid.Nil {
			return fmt.Errorf("the erasure row %s names no order", row.ID)
		}
		return r.Execute(ctx, org, p.Order)
	}
}

// Execute runs an approved order: the register entry is filed first (write-once, signed), then the database scrubs in one transaction with its audit events and the tombstone for Fyniq, then the
// person's files are deleted. It can be run again after any step: the entry exists, the order is executed, a file is gone.
func (r *ErasureRunner) Execute(ctx context.Context, org, order uuid.UUID) error {
	now := r.Clock.Now()
	var person, requested uuid.UUID
	var approved *uuid.UUID
	var state, eligible string
	if err := r.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT person_id, requested_by, approved_by, state, to_char(eligible_on, 'YYYY-MM-DD') FROM nemura.erasure_order WHERE id = $1`, order).Scan(&person, &requested, &approved, &state, &eligible)
	}); err != nil {
		return erasure.ErrNotFound
	}
	if state == "executed" {
		return r.deleteFiles(ctx, org, person)
	}
	if state != "approved" || approved == nil {
		return &erasure.Refusal{Code: erasure.CodeNotApproved, Message: "An erasure runs only from an approved order."}
	}
	entry := erasure.Entry{Schema: erasure.Schema, Organisation: org, Person: person, Order: order, ErasedOn: day2(now), EligibleOn: eligible, RequestedBy: requested, ApprovedBy: *approved}
	msg, err := entry.Canonical()
	if err != nil {
		return err
	}
	sig, err := r.Signer.Sign(ctx, msg)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(erasure.Signed{Entry: entry, KeyID: r.Signer.KeyID(), Sig: sig})
	if err := r.Blobs.Create(ctx, erasure.Key(org, person), bytesReader(raw)); err != nil && !errors.Is(err, ports.ErrExists) {
		return fmt.Errorf("the register could not be written, so nothing was erased: %w", err)
	}
	digest, err := erasure.Digest(entry)
	if err != nil {
		return err
	}
	if err := r.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT nemura.erase_person($1, $2, $3)`, order, day2(now), digest[:]); err != nil {
			return err
		}
		if err := Audit(ctx, tx, org, now, audit.Actor{Type: "service", ID: "nemura-worker"}, "nemura.erasure.executed", audit.Object{Type: "erasure_order", ID: order},
			map[string]audit.Change{"person_id": {nil, person.String()}}, ""); err != nil {
			return err
		}
		if err := erasedAudits(ctx, tx, org, now, person, "nemura.erasure.scrubbed", "the person's personal data was erased after its retention periods"); err != nil {
			return err
		}
		_, err := QueuePersonEvent(ctx, tx, org, now, person) // Fyniq is told the person is gone: a tombstone, which clears the name and email it held
		return err
	}); err != nil {
		return err
	}
	return r.deleteFiles(ctx, org, person)
}

// deleteFiles removes the person's files (documents, payslip and letter PDFs); a file under retention stays and is reported as an error so the work is tried again when it lapses.
func (r *ErasureRunner) deleteFiles(ctx context.Context, org, person uuid.UUID) error {
	var keys []string
	if err := r.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT blob_key FROM nemura.payslip WHERE person_id = $1 UNION SELECT blob_key FROM nemura.letter_issued WHERE person_id = $1
UNION SELECT blob_key FROM nemura.person_document WHERE person_id = $1`, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return err
			}
			keys = append(keys, k)
		}
		return rows.Err()
	}); err != nil {
		return err
	}
	var retained error
	for _, k := range keys {
		if err := r.Blobs.Delete(ctx, k); err != nil {
			if errors.Is(err, ports.ErrRetained) {
				retained = err
				continue
			}
			return err
		}
	}
	return retained
}

// Reapply is the step after a restore, before the database opens: every signed entry the register holds for the organisation is applied again, so a personal field erased since the backup does
// not come back. An entry that does not verify is not applied and is reported. It returns how many people it scrubbed again; running it again is safe.
func (r *ErasureRunner) Reapply(ctx context.Context, org uuid.UUID) (int, error) {
	keys, err := r.Blobs.List(ctx, erasure.Prefix(org))
	if err != nil {
		return 0, err
	}
	now := r.Clock.Now()
	n := 0
	var bad error
	for _, k := range keys {
		rc, err := r.Blobs.Get(ctx, k)
		if err != nil {
			return n, err
		}
		raw, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return n, err
		}
		entry, err := erasure.Open(raw, r.Signer, org, k)
		if err != nil {
			bad = errors.Join(bad, fmt.Errorf("%s: %w", k, err))
			continue
		}
		var did bool
		if err := r.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT nemura.reapply_erasure($1, $2)`, entry.Person, day2(now)).Scan(&did); err != nil || !did {
				return err
			}
			if err := erasedAudits(ctx, tx, org, now, entry.Person, "nemura.erasure.reapplied", "a restore brought the person's erased data back: the register's entry was applied again"); err != nil {
				return err
			}
			_, err := QueuePersonEvent(ctx, tx, org, now, entry.Person)
			return err
		}); err != nil {
			return n, err
		}
		if did {
			n++
			if err := r.deleteFiles(ctx, org, entry.Person); err != nil {
				bad = errors.Join(bad, err)
			}
		}
	}
	return n, bad
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// ErasureOpsService is the API's side of the erasure: asking, deciding and listing, in the caller's own transaction.
type ErasureOpsService struct {
	Store *Store
	Clock ports.Clock
}

func erasureView(v ErasureView) api.ErasureOrderView {
	out := api.ErasureOrderView{ID: v.ID.String(), PersonID: v.Person.String(), State: v.State, Reason: v.Reason, EligibleOn: v.EligibleOn.Format(time.DateOnly), RequestedBy: v.RequestedBy.String(), RequestedAt: v.RequestedAt.UTC().Format(time.RFC3339)}
	if v.ApprovedBy != nil {
		s := v.ApprovedBy.String()
		out.ApprovedBy = &s
	}
	if v.ExecutedAt != nil {
		s := v.ExecutedAt.UTC().Format(time.RFC3339)
		out.ExecutedAt = &s
	}
	return out
}

// Request asks for the erasure.
func (c ErasureOpsService) Request(ctx context.Context, s api.Session, held []authz.Role, person uuid.UUID, reason string) (api.ErasureOrderView, error) {
	var out api.ErasureOrderView
	now := c.Clock.Now()
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		id, err := RequestErasure(ctx, tx, s.Organisation, now, now, s.Member, held, person, reason)
		if err != nil {
			return err
		}
		all, err := Erasures(ctx, tx)
		for _, v := range all {
			if v.ID == id {
				out = erasureView(v)
			}
		}
		return err
	})
	return out, err
}

// Decide approves or refuses.
func (c ErasureOpsService) Decide(ctx context.Context, s api.Session, held []authz.Role, order uuid.UUID, approve bool) error {
	return c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		return DecideErasure(ctx, tx, s.Organisation, c.Clock.Now(), s.Member, held, order, approve)
	})
}

// List lists the orders.
func (c ErasureOpsService) List(ctx context.Context, s api.Session) ([]api.ErasureOrderView, error) {
	out := []api.ErasureOrderView{}
	err := c.Store.InOrg(ctx, s.Organisation, func(ctx context.Context, tx pgx.Tx) error {
		all, err := Erasures(ctx, tx)
		for _, v := range all {
			out = append(out, erasureView(v))
		}
		return err
	})
	return out, err
}
