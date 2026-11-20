package nemura

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/checks"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// ------------------------------------------------------------------ the link's health, its repair and its restore (NM-INT-04, 10, 13; FR-PPL-08, 19)

// TypeReplayRequested is what Fyniq sends when its inbox finds a gap in the sequence numbers it received, or a version it never applied: the range of Nemura's sequence numbers it is missing (PRD-002 NM-INT-10:
// a gap is repaired by reading the object's current state again). Nemura answers by sending the current state of each object the range names, as new events, never the old payloads.
const TypeReplayRequested = "link.replay.requested"

// replayRequest is the payload of link.replay.requested.
type replayRequest struct {
	FromSequence int64 `json:"from_sequence"`
	ToSequence   int64 `json:"to_sequence"`
}

// RequeueCurrent queues the current state of a person for Fyniq again, at the version it has now (no change of version: a person Fyniq already has at that version ignores it, and one it missed or holds
// at an older version applies it): the person, their bank-detail version and their hold. It is repair, never a change, and it queues nothing for a person Nemura does not know.
func RequeueCurrent(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, person uuid.UUID) (int, error) {
	if !linkActive(ctx, tx) {
		return 0, nil
	}
	n := 0
	var known bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nemura.person WHERE id = $1)`, person).Scan(&known); err != nil || !known {
		return 0, err
	}
	var v int
	err := tx.QueryRow(ctx, `SELECT version FROM nemura.link_object_version WHERE object_type = 'person' AND object_id = $1`, person.String()).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO nemura.link_object_version (object_type, object_id, version) VALUES ('person', $1, 1) RETURNING version`, person.String()).Scan(&v)
	}
	if err != nil {
		return 0, err
	}
	if ok, err := QueueLinkEvent(ctx, tx, org, at, TypePersonUpserted, "person", person.String(), v); err != nil {
		return n, err
	} else if ok {
		n++
	}
	if bv, err := bankVersionOf(ctx, tx, person); err != nil {
		return n, err
	} else if bv > 0 {
		if ok, err := QueueLinkEvent(ctx, tx, org, at, TypeBankDetailsChanged, "bank_details", person.String(), bv); err != nil {
			return n, err
		} else if ok {
			n++
		}
	}
	h, ok, err := TaxHoldOf(ctx, tx, person)
	if err != nil {
		return n, err
	}
	if ok {
		if sent, err := QueueLinkEvent(ctx, tx, org, at, "people.tax_hold.changed", "tax_hold", person.String(), h.Version); err != nil {
			return n, err
		} else if sent {
			n++
		}
	}
	k, err := requeueLeave(ctx, tx, org, at, person, uuid.Nil) // the person's approved and cancelled leave, at the version each has
	return n + k, err
}

// RepairLink queues the current state again, for one person or for everyone (person uuid.Nil), and sends again what could not be delivered: an event that failed or was refused (other than a pay run, which
// has its own retry that keeps its proof) is queued as a new event of the same object at its version. It returns how many events it queued, and audits the repair by its scope and number, never a payload.
func RepairLink(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by audit.Actor, person uuid.UUID) (int, error) {
	if !linkActive(ctx, tx) {
		return 0, &payroll.Refusal{Code: CodeLinkNone, Message: "The link to Fyniq is not active: repair it once it is."}
	}
	var persons []uuid.UUID
	if person != uuid.Nil {
		persons = []uuid.UUID{person}
	} else {
		rows, err := tx.Query(ctx, `SELECT id FROM nemura.person ORDER BY employment_start, id`)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return 0, err
			}
			persons = append(persons, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
	}
	n := 0
	for _, p := range persons {
		k, err := RequeueCurrent(ctx, tx, org, at, p)
		if err != nil {
			return n, err
		}
		n += k
	}
	if person == uuid.Nil {
		k, err := retryUndelivered(ctx, tx, org, at)
		if err != nil {
			return n, err
		}
		n += k
	}
	var l LinkRow
	l, err := LiveLink(ctx, tx)
	if err != nil {
		return n, err
	}
	scope := "everyone"
	if person != uuid.Nil {
		scope = person.String()
	}
	return n, Audit(ctx, tx, org, at, by, "nemura.fyniq_link.repaired", audit.Object{Type: "fyniq_link", ID: l.ID}, map[string]audit.Change{"scope": {nil, scope}, "queued": {nil, n}}, "")
}

// retryUndelivered queues again each object whose newest event failed or was refused and is not a pay run: the object's newest failed or refused event is replaced by one new event at the same version.
func retryUndelivered(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time) (int, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (type, object_type, object_id) type, object_type, object_id, object_version, state FROM nemura.link_event
WHERE type <> $1 ORDER BY type, object_type, object_id, sequence DESC`, link.TypePayrollRunApproved)
	if err != nil {
		return 0, err
	}
	type item struct {
		typ, objectType, objectID string
		version                   int
	}
	var todo []item
	for rows.Next() {
		var it item
		var state string
		if err := rows.Scan(&it.typ, &it.objectType, &it.objectID, &it.version, &state); err != nil {
			rows.Close()
			return 0, err
		}
		if state == "failed" || state == "refused" {
			todo = append(todo, it)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, it := range todo {
		if ok, err := QueueLinkEvent(ctx, tx, org, at, it.typ, it.objectType, it.objectID, it.version); err != nil {
			return n, err
		} else if ok {
			n++
		}
	}
	return n, nil
}

// applyReplay answers Fyniq's request for a range of sequence numbers: the objects the range's events were about are queued again at their current state (design D5; NM-INT-10). A request that names more
// than 10,000 numbers, or a range that is backwards, is refused; one that names nothing Nemura queued changes nothing.
func applyReplay(ctx context.Context, tx pgx.Tx, org uuid.UUID, now time.Time, ev replayRequest) error {
	if ev.FromSequence < 1 || ev.ToSequence < ev.FromSequence || ev.ToSequence-ev.FromSequence >= 10000 {
		return &payroll.Refusal{Code: payroll.CodeInvalid, Message: "A replay asks for a range of Nemura's sequence numbers, from 1, of at most 10,000."}
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT object_type, object_id, type FROM nemura.link_event WHERE sequence BETWEEN $1 AND $2 AND (object_type IN ('person', 'bank_details', 'tax_hold', 'leave_request') OR type IN ($3, 'payroll.payments.ready'))`, ev.FromSequence, ev.ToSequence, TypeIdentityAccess)
	if err != nil {
		return err
	}
	var persons, members, leaves, payRuns []uuid.UUID
	for rows.Next() {
		var t, id, typ string
		if err := rows.Scan(&t, &id, &typ); err != nil {
			rows.Close()
			return err
		}
		if p, err := uuid.Parse(id); err == nil {
			if typ == TypePaymentsReady {
				payRuns = append(payRuns, p) // the run's lines are made again at send time, at the version the run has now
			} else if t == "leave_request" {
				leaves = append(leaves, p) // the entry is told again at the version it has now
			} else if typ == TypeIdentityAccess {
				members = append(members, p) // what an identity may enter is the member's: told again as it stands now
			} else {
				persons = append(persons, p)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	seen := map[uuid.UUID]bool{}
	for _, m := range members {
		if seen[m] {
			continue
		}
		seen[m] = true
		if _, err := QueueAccessEvents(ctx, tx, org, now, m); err != nil {
			return err
		}
	}
	for _, p := range persons {
		if seen[p] {
			continue
		}
		seen[p] = true
		if _, err := RequeueCurrent(ctx, tx, org, now, p); err != nil {
			return err
		}
	}
	for _, r := range leaves {
		if _, err := requeueLeave(ctx, tx, org, now, uuid.Nil, r); err != nil {
			return err
		}
	}
	for _, r := range payRuns {
		if _, err := requeuePayments(ctx, tx, org, now, r); err != nil {
			return err
		}
	}
	return nil
}

// LinkHealthRow is the link's health as the people team reads it (NM-INT-04).
type LinkHealthRow struct {
	State                 string
	Epoch                 int
	Queued, Failed        int
	Sent, Refused         int
	OldestWaiting         *time.Time // the oldest event not yet sent or refused
	LastSent              *time.Time
	LastSequenceQueued    int64
	LastSequenceSent      int64
	LastFyniqSequence     int64
	FailingSince          *time.Time // the oldest waiting event, when it has waited more than an hour
	Undelivered           []UndeliveredEvent
	OpenRestore           *RestoreRow
	SyncFailingOver24Hour bool
	BankReconfirm         []BankReconfirmRow // people whose bank details wait to be re-confirmed after a restore (NM-INT-13)
}

// UndeliveredEvent is an event that failed or was refused, by identity: never its payload.
type UndeliveredEvent struct {
	EventID    uuid.UUID
	Sequence   int64
	Type       string
	ObjectType string
	ObjectID   string
	Version    int
	State      string
	Code       string
	Attempts   int
	QueuedAt   time.Time
}

// LinkHealth reads the link's health: events by state, the oldest waiting, the last sent, the checkpoint and what failed, with the ⚠ of N.1 #7 when nothing has synced for 24 hours while something waits.
func LinkHealth(ctx context.Context, tx pgx.Tx, now time.Time) (LinkHealthRow, bool, error) {
	var h LinkHealthRow
	l, err := LiveLink(ctx, tx)
	if noLink(err) {
		return h, false, nil
	}
	if err != nil {
		return h, false, err
	}
	h.State, h.Epoch, h.LastSequenceQueued = l.State, l.Epoch, l.Sequence
	// What is wrong is each object's newest event: a repair or a retry queues a new event for the object and leaves the old one as it was, and the old one is no longer what is waiting.
	if err := tx.QueryRow(ctx, `WITH newest AS (SELECT DISTINCT ON (type, object_type, object_id) state, queued_at FROM nemura.link_event WHERE link_id = $1 ORDER BY type, object_type, object_id, sequence DESC)
SELECT count(*) FILTER (WHERE state = 'queued'), count(*) FILTER (WHERE state = 'failed'), count(*) FILTER (WHERE state = 'refused'), min(queued_at) FILTER (WHERE state IN ('queued', 'failed')) FROM newest`, l.ID).
		Scan(&h.Queued, &h.Failed, &h.Refused, &h.OldestWaiting); err != nil {
		return h, true, err
	}
	if err := tx.QueryRow(ctx, `SELECT count(*), max(settled_at), coalesce(max(sequence), 0) FROM nemura.link_event WHERE link_id = $1 AND state = 'sent'`, l.ID).Scan(&h.Sent, &h.LastSent, &h.LastSequenceSent); err != nil {
		return h, true, err
	}
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(sequence), 0) FROM nemura.link_inbox WHERE link_id = $1`, l.ID).Scan(&h.LastFyniqSequence); err != nil {
		return h, true, err
	}
	// An event waiting for more than an hour is the sync failing since it was queued (it is sent within a minute when the sync works); 24 hours of it is the warning (N.1 #7).
	if h.OldestWaiting != nil && now.Sub(*h.OldestWaiting) >= time.Hour {
		h.FailingSince = h.OldestWaiting
		h.SyncFailingOver24Hour = now.Sub(*h.OldestWaiting) >= 24*time.Hour
	}
	rows, err := tx.Query(ctx, `SELECT event_id, sequence, type, object_type, object_id, object_version, state, coalesce(code, ''), attempts, queued_at FROM
 (SELECT DISTINCT ON (type, object_type, object_id) * FROM nemura.link_event WHERE link_id = $1 ORDER BY type, object_type, object_id, sequence DESC) newest
WHERE state IN ('failed', 'refused') ORDER BY sequence DESC LIMIT 50`, l.ID)
	if err != nil {
		return h, true, err
	}
	defer rows.Close()
	for rows.Next() {
		var e UndeliveredEvent
		if err := rows.Scan(&e.EventID, &e.Sequence, &e.Type, &e.ObjectType, &e.ObjectID, &e.Version, &e.State, &e.Code, &e.Attempts, &e.QueuedAt); err != nil {
			return h, true, err
		}
		h.Undelivered = append(h.Undelivered, e)
	}
	if err := rows.Err(); err != nil {
		return h, true, err
	}
	rows.Close()
	if h.OpenRestore, err = OpenRestore(ctx, tx); err != nil {
		return h, true, err
	}
	h.BankReconfirm, err = OpenBankReconfirms(ctx, tx)
	return h, true, err
}

// LinkFindings is N.1 #7 (NM-INT-04): a link that has not synced for 24 hours while events wait is ⚠, a restore not reconciled is ✖ (nothing is sent or paid until it is), and so is each person whose bank details wait to be re-confirmed after a restore (NM-INT-13).
func LinkFindings(ctx context.Context, tx pgx.Tx, now time.Time) ([]checks.Finding, error) {
	h, ok, err := LinkHealth(ctx, tx, now)
	if err != nil || !ok {
		return nil, err
	}
	var out []checks.Finding
	if h.SyncFailingOver24Hour {
		out = append(out, checks.Finding{Check: 7, Area: "Link", Status: checks.Warn, Subject: "link", Message: fmt.Sprintf("The link to Fyniq has not synced since %s and %d events are waiting: open the link, read why, and send them again.", h.FailingSince.Format("2 Jan 2006 15:04"), h.Queued+h.Failed)})
	}
	if h.OpenRestore != nil {
		out = append(out, checks.Finding{Check: 7, Area: "Link", Status: checks.Fail, Subject: "restore", Message: "Nemura was restored and the link to Fyniq is held: decide each run it lists against Fyniq's own record, then the link resumes."})
	}
	for _, r := range h.BankReconfirm {
		out = append(out, checks.Finding{Check: 7, Area: "Link", Status: checks.Fail, Subject: r.Person.String(), Message: "After a restore, Fyniq holds bank details for this person that Nemura does not have: no payment is made to them until their bank account is changed and confirmed again, then it is paid."})
	}
	return out, nil
}

// ListLinkEvents lists the events queued after a sequence number, oldest first, by identity and state; never a payload.
func ListLinkEvents(ctx context.Context, tx pgx.Tx, after int64, limit int) ([]UndeliveredEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := tx.Query(ctx, `SELECT event_id, sequence, type, object_type, object_id, object_version, state, coalesce(code, ''), attempts, queued_at FROM nemura.link_event WHERE sequence > $1 ORDER BY sequence LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UndeliveredEvent
	for rows.Next() {
		var e UndeliveredEvent
		if err := rows.Scan(&e.EventID, &e.Sequence, &e.Type, &e.ObjectType, &e.ObjectID, &e.Version, &e.State, &e.Code, &e.Attempts, &e.QueuedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ------------------------------------------------------------------ restore

// RestoreRow is a restore as recorded.
type RestoreRow struct {
	ID                                 uuid.UUID
	RecordedAt                         time.Time
	RecordedBy                         string
	LastQueued, LastSent, LastReceived int64
	State                              string
	Runs                               []RestoreRunRow
}

// RestoreRunRow is a run the restore holds as unknown.
type RestoreRunRow struct {
	RunID     uuid.UUID
	Version   int
	Outcome   string
	Reference string
}

// OpenRestore is the restore that is not reconciled yet, if any.
func OpenRestore(ctx context.Context, tx pgx.Tx) (*RestoreRow, error) {
	var r RestoreRow
	err := tx.QueryRow(ctx, `SELECT id, recorded_at, recorded_by, last_sequence_queued, last_sequence_sent, last_fyniq_sequence, state FROM nemura.link_restore WHERE state = 'open'`).
		Scan(&r.ID, &r.RecordedAt, &r.RecordedBy, &r.LastQueued, &r.LastSent, &r.LastReceived, &r.State)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT run_id, version, coalesce(outcome, ''), coalesce(reference, '') FROM nemura.link_restore_run WHERE restore_id = $1 ORDER BY run_id, version`, r.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var x RestoreRunRow
		if err := rows.Scan(&x.RunID, &x.Version, &x.Outcome, &x.Reference); err != nil {
			return nil, err
		}
		r.Runs = append(r.Runs, x)
	}
	return &r, rows.Err()
}

// RecordLinkRestore is the deployment's step after a restore of Nemura's database and before it opens (run with the migrator's credential, as the owner's recovery is, by two named operators): it records
// the link's checkpoint as the restored database knows it, lists each run approved with proof whose answer it does not hold (Fyniq may have posted it after the restore point) and suspends the link, so
// nothing is sent or paid until an Owner or Administrator has decided each run against Fyniq's own record. It does nothing and says so when the organisation has no link past its proposal.
func RecordLinkRestore(ctx context.Context, url string, org uuid.UUID, operators []string, at time.Time) (held int, linked bool, err error) {
	if len(operators) != 2 || strings.EqualFold(strings.TrimSpace(operators[0]), strings.TrimSpace(operators[1])) {
		return 0, false, errors.New("a restore is recorded by exactly two different operators, by name")
	}
	s, err := Open(ctx, url)
	if err != nil {
		return 0, false, err
	}
	defer s.Close()
	err = s.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
		l, err := LiveLink(ctx, tx)
		if noLink(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if l.State == "proposed" {
			return nil
		}
		linked = true
		h, _, err := LinkHealth(ctx, tx, at)
		if err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.link_restore (id, link_id, recorded_at, recorded_by, last_sequence_queued, last_sequence_sent, last_fyniq_sequence) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id, l.ID, at, strings.Join(operators, ", "), h.LastSequenceQueued, h.LastSequenceSent, h.LastFyniqSequence); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO nemura.link_restore_run (restore_id, run_id, version)
SELECT $1, a.run_id, a.version FROM nemura.link_run_approval a
WHERE NOT EXISTS (SELECT 1 FROM nemura.link_run_posting p WHERE p.run_id = a.run_id AND p.version = a.version AND p.status = 'posted')`, id)
		if err != nil {
			return err
		}
		held = int(tag.RowsAffected())
		if l.State == "active" {
			if _, err := tx.Exec(ctx, `UPDATE nemura.fyniq_link SET state = 'suspended', version = version + 1, changed_at = $2 WHERE id = $1`, l.ID, at); err != nil {
				return err
			}
			if err := Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "nemura-deployment-recovery"}, "nemura.fyniq_link.suspended", audit.Object{Type: "fyniq_link", ID: l.ID},
				map[string]audit.Change{"state": {"active", "suspended"}}, "after a restore of Nemura's database"); err != nil {
				return err
			}
		}
		return Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "nemura-deployment-recovery"}, "nemura.fyniq_link.restore_recorded", audit.Object{Type: "link_restore", ID: id},
			map[string]audit.Change{"operators": {nil, operators}, "runs_held": {nil, held}, "last_sequence_sent": {nil, h.LastSequenceSent}}, "after a restore of Nemura's database")
	})
	return held, linked, err
}

// ReconcileRestore records an Owner's or Administrator's decision on one run the restore holds, against Fyniq's own record of it: posted (with Fyniq's posting reference) or not posted (the same approval is
// then sent again). When every run is decided the restore is reconciled and the link resumes: the people, bank details and holds are queued again at their current state, so Fyniq has what Nemura has.
func ReconcileRestore(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, by uuid.UUID, run uuid.UUID, version int, posted bool, reference string) (resumed bool, err error) {
	r, err := OpenRestore(ctx, tx)
	if err != nil {
		return false, err
	}
	if r == nil {
		return false, &payroll.Refusal{Code: payroll.CodeConflict, Message: "There is no restore waiting to be reconciled."}
	}
	reference = strings.TrimSpace(reference)
	if posted && (reference == "" || len(reference) > 200) {
		return false, &payroll.Refusal{Code: payroll.CodeInvalid, Message: "reference: give the reference Fyniq shows for the posting (up to 200 characters)."}
	}
	var ref *string
	outcome := "not_posted"
	if posted {
		outcome, ref = "posted", &reference
	}
	tag, err := tx.Exec(ctx, `UPDATE nemura.link_restore_run SET outcome = $4, reference = $5, decided_by = $6, decided_at = $7 WHERE restore_id = $1 AND run_id = $2 AND version = $3 AND outcome IS NULL`,
		r.ID, run, version, outcome, ref, by, at)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, &payroll.Refusal{Code: payroll.CodeConflict, Message: "That run is not held by this restore, or it is decided already."}
	}
	if posted {
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.link_run_posting (run_id, version, status, posting_ref, received_at) VALUES ($1, $2, 'posted', $3, $4) ON CONFLICT (organisation_id, run_id, version, status) DO NOTHING`, run, version, reference, at); err != nil {
			return false, err
		}
	}
	if err := Audit(ctx, tx, org, at, user(by), "nemura.fyniq_link.restore_run_decided", audit.Object{Type: "link_restore", ID: r.ID},
		map[string]audit.Change{"run": {nil, run.String()}, "version": {nil, version}, "outcome": {nil, outcome}}, ""); err != nil {
		return false, err
	}
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM nemura.link_restore_run WHERE restore_id = $1 AND outcome IS NULL`, r.ID).Scan(&open); err != nil {
		return false, err
	}
	if open > 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE nemura.link_restore SET state = 'reconciled', reconciled_at = $2, reconciled_by = $3 WHERE id = $1`, r.ID, at, by); err != nil {
		return false, err
	}
	l, err := LiveLink(ctx, tx)
	if err != nil {
		return false, err
	}
	if l.State == "suspended" {
		if _, err := tx.Exec(ctx, `UPDATE nemura.fyniq_link SET state = 'active', version = version + 1, changed_at = $2 WHERE id = $1`, l.ID, at); err != nil {
			return false, err
		}
		if err := Audit(ctx, tx, org, at, user(by), "nemura.fyniq_link.resumed", audit.Object{Type: "fyniq_link", ID: l.ID}, map[string]audit.Change{"state": {"suspended", "active"}}, "the restore is reconciled"); err != nil {
			return false, err
		}
	}
	if _, err := RepairLink(ctx, tx, org, at, user(by), uuid.Nil); err != nil {
		return false, err
	}
	// A run Fyniq did not post is sent again with the proof it was approved with.
	rows, err := tx.Query(ctx, `SELECT run_id, version FROM nemura.link_restore_run WHERE restore_id = $1 AND outcome = 'not_posted'`, r.ID)
	if err != nil {
		return false, err
	}
	type held struct {
		run     uuid.UUID
		version int
	}
	var again []held
	for rows.Next() {
		var h held
		if err := rows.Scan(&h.run, &h.version); err != nil {
			rows.Close()
			return false, err
		}
		again = append(again, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, h := range again {
		if _, err := QueueLinkEvent(ctx, tx, org, at, link.TypePayrollRunApproved, "payroll_run", h.run.String(), h.version); err != nil {
			return false, err
		}
	}
	return true, Audit(ctx, tx, org, at, user(by), "nemura.fyniq_link.restore_reconciled", audit.Object{Type: "link_restore", ID: r.ID}, nil, "")
}
