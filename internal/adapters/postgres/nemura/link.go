package nemura

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// ------------------------------------------------------------------ Nemura's side of the link to Fyniq (NM-ARC-04, NM-INT-01, 03, 10, 11)

// The refusals of the link's own rules; the envelope's (PPL-01 to PPL-06) come from internal/nemura/link.
const (
	CodeLinkNone    = "NM_LINK_NONE"    // no active link
	CodeLinkMinimum = "NM_LINK_MINIMUM" // the payload carries a field the contract does not list, or a value it cannot read
	CodeLinkType    = "NM_LINK_TYPE"    // an event type Nemura does not receive
)

// LinkRow is the organisation's live link to a Fyniq organisation.
type LinkRow struct {
	ID, FyniqOrganisation uuid.UUID
	State                 string
	Epoch, Version        int
	FyniqKey              ed25519.PublicKey // nil while the link is only proposed
	Public                ed25519.PublicKey // Nemura's own, which Fyniq holds
	Sequence              int64
	// PeerClientID is the ID Fyniq's client has at Nemura's own OAuth server (chosen at the proposal); FyniqClientID is the ID Nemura's client has at Fyniq's.
	PeerClientID, FyniqClientID string
	PeerAskClientID             string // the ID Fyniq's read-only client for the ask at release has at Nemura's OAuth server
	ProposedBy                  uuid.UUID
}

// MakeLink makes the link to a Fyniq organisation: Nemura's own key pair for what it sends, Fyniq's public key for what it receives, active at the next epoch. The caller is an
// Owner of both organisations with a second approver (NM-ARC-04); the private key is held for the worker alone. It returns the link and Nemura's public key, which is given to Fyniq.
func MakeLink(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, actor audit.Actor, fyniqOrg uuid.UUID, fyniqKey ed25519.PublicKey) (LinkRow, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return LinkRow{}, err
	}
	var epoch int
	if err := tx.QueryRow(ctx, "SELECT coalesce(max(epoch), 0) + 1 FROM nemura.fyniq_link").Scan(&epoch); err != nil {
		return LinkRow{}, err
	}
	id := uuid.Must(uuid.NewV7())
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.fyniq_link (id, state, epoch, fyniq_organisation_id, fyniq_key, link_public, created_at, changed_at) VALUES ($1, 'active', $2, $3, $4, $5, $6, $6)`,
		id, epoch, fyniqOrg, []byte(fyniqKey), []byte(pub), at); err != nil {
		return LinkRow{}, linkError(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.fyniq_link_secret (link_id, link_private) VALUES ($1, $2)`, id, []byte(priv)); err != nil {
		return LinkRow{}, linkError(err)
	}
	row := LinkRow{ID: id, FyniqOrganisation: fyniqOrg, State: "active", Epoch: epoch, Version: 1, FyniqKey: fyniqKey, Public: pub}
	// The audit trail names the link and its epoch and the keys' existence, never a key.
	return row, Audit(ctx, tx, org, at, actor, "nemura.fyniq_link.linked", audit.Object{Type: "fyniq_link", ID: id},
		map[string]audit.Change{"fyniq_organisation": {nil, fyniqOrg.String()}, "epoch": {nil, epoch}, "state": {nil, "active"}}, "")
}

func linkError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "NM190" || pg.Code == "23505") {
		return &payroll.Refusal{Code: payroll.CodeConflict, Message: "The organisation is linked already, or the link's epoch is out of order: unlink first (a link is never changed in place)."}
	}
	return err
}

// LiveLink is the link that is not unlinked, or CodeLinkNone.
func LiveLink(ctx context.Context, tx pgx.Tx) (LinkRow, error) {
	var l LinkRow
	var fk, pub []byte
	var peer, fyniqClient, askPeer *string
	var proposer *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id, state, epoch, version, fyniq_organisation_id, fyniq_key, link_public, sequence, peer_client_id, fyniq_client_id, proposed_by, peer_ask_client_id FROM nemura.fyniq_link WHERE state <> 'unlinked'`).
		Scan(&l.ID, &l.State, &l.Epoch, &l.Version, &l.FyniqOrganisation, &fk, &pub, &l.Sequence, &peer, &fyniqClient, &proposer, &askPeer)
	if errors.Is(err, pgx.ErrNoRows) {
		return LinkRow{}, &payroll.Refusal{Code: CodeLinkNone, Message: "Nemura is not linked to Fyniq."}
	}
	l.FyniqKey, l.Public = fk, pub
	if peer != nil {
		l.PeerClientID = *peer
	}
	if fyniqClient != nil {
		l.FyniqClientID = *fyniqClient
	}
	if askPeer != nil {
		l.PeerAskClientID = *askPeer
	}
	if proposer != nil {
		l.ProposedBy = *proposer
	}
	return l, err
}

// SignOutbound signs an event for Fyniq with the link's key, at the next sequence number, for the worker to send through the outbox (NM-INT-01, NM-INT-10). Pay and bank payloads are
// built by the caller and never reach a log. It needs the worker's grant on the link's secret. An event that is queued first and sent later is signed with SignQueued instead, so a retry is the
// same event.
func SignOutbound(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, typ, objectType, objectID string, objectVersion int, payload any) (link.Envelope, error) {
	l, err := LiveLink(ctx, tx)
	if err != nil {
		return link.Envelope{}, err
	}
	if l.State != "active" {
		return link.Envelope{}, &payroll.Refusal{Code: CodeLinkNone, Message: "The link is not active."}
	}
	var seq int64
	if err := tx.QueryRow(ctx, `UPDATE nemura.fyniq_link SET sequence = sequence + 1, version = version + 1, changed_at = $2 WHERE id = $1 RETURNING sequence`, l.ID, at).Scan(&seq); err != nil {
		return link.Envelope{}, err
	}
	if err := Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "link"}, "nemura.fyniq_link.sent", audit.Object{Type: "fyniq_link", ID: l.ID},
		map[string]audit.Change{"type": {nil, typ}, "sequence": {nil, seq}}, ""); err != nil { // the event's type and number, never its payload
		return link.Envelope{}, err
	}
	return signQueued(ctx, tx, l, org, at, QueuedEvent{EventID: uuid.NewString(), Sequence: seq, Type: typ, ObjectType: objectType, ObjectID: objectID, ObjectVersion: objectVersion, CorrelationID: uuid.NewString()}, payload)
}

// signQueued signs a queued event with the link's envelope key: the event ID, the sequence and the correlation ID are the queued ones, only the time is the sending attempt's.
func signQueued(ctx context.Context, tx pgx.Tx, l LinkRow, org uuid.UUID, at time.Time, q QueuedEvent, payload any) (link.Envelope, error) {
	var priv []byte
	if err := tx.QueryRow(ctx, `SELECT link_private FROM nemura.fyniq_link_secret WHERE link_id = $1`, l.ID).Scan(&priv); err != nil {
		return link.Envelope{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return link.Envelope{}, err
	}
	now := at.UTC().Format(time.RFC3339)
	return link.Sign(ed25519.PrivateKey(priv), link.Header{EventID: q.EventID, Type: q.Type, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: org.String(),
		TargetOrganisation: l.FyniqOrganisation.String(), Epoch: l.Epoch, Sequence: q.Sequence, ObjectType: q.ObjectType, ObjectID: q.ObjectID, ObjectVersion: q.ObjectVersion, EffectiveAt: now,
		RecordedAt: now, SignedAt: now, CorrelationID: q.CorrelationID}, raw)
}

// TaxHoldEvent is the payload of people.tax_hold.changed (NM-INT-08), built from a row of nemura.tax_hold and nothing else.
type TaxHoldEvent struct {
	PersonID      uuid.UUID `json:"person_id"`
	HoldID        uuid.UUID `json:"hold_id"`
	EffectiveFrom string    `json:"effective_from"`
	Status        string    `json:"status"`
	EvidenceRef   string    `json:"evidence_ref"`
}

// TaxHoldChanged builds and signs people.tax_hold.changed for a person's latest hold version, to go at once, ahead of ordinary sync (NM-INT-08): the version is the object version.
// None when the person has no hold.
func TaxHoldChanged(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, person uuid.UUID) (link.Envelope, bool, error) {
	ev, version, ok, err := taxHoldEventAt(ctx, tx, person, 0)
	if err != nil || !ok {
		return link.Envelope{}, false, err
	}
	env, err := SignOutbound(ctx, tx, org, at, "people.tax_hold.changed", "tax_hold", person.String(), version, ev)
	return env, err == nil, err
}

// taxHoldEventAt reads the payload from a row of nemura.tax_hold and nothing else: the person's version (the latest when version is 0).
func taxHoldEventAt(ctx context.Context, tx pgx.Tx, person uuid.UUID, version int) (TaxHoldEvent, int, bool, error) {
	var ev TaxHoldEvent
	var evidence *uuid.UUID
	var from time.Time
	var got int
	err := tx.QueryRow(ctx, `SELECT hold_id, version, status, effective_from, evidence_id FROM nemura.tax_hold WHERE person_id = $1 AND ($2 = 0 OR version = $2) ORDER BY version DESC LIMIT 1`, person, version).
		Scan(&ev.HoldID, &got, &ev.Status, &from, &evidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return ev, 0, false, nil
	}
	if err != nil {
		return ev, 0, false, err
	}
	ev.PersonID, ev.EffectiveFrom = person, from.Format(time.DateOnly)
	if evidence != nil {
		ev.EvidenceRef = evidence.String()
	}
	return ev, got, true, nil
}

type paymentStatusEvent struct {
	RunID       uuid.UUID `json:"run_id"`
	PersonID    uuid.UUID `json:"person_id"`
	State       string    `json:"state"`
	Instruction string    `json:"instruction_id"`
	Amount      string    `json:"amount"`
	PaidOn      string    `json:"paid_on"`
	EvidenceRef string    `json:"evidence_ref"`
}

// departmentChangedEvent is people.department.changed: the version of Fyniq's payroll account mapping and the departments in it (PRD-002 Appendix E). Departments come from Fyniq (NM-INT-01), so Nemura keeps
// each version as Fyniq said it and uses the newest.
type departmentChangedEvent struct {
	MappingVersion int `json:"mapping_version"`
	Departments    []struct {
		DepartmentID uuid.UUID  `json:"department_id"`
		Name         string     `json:"name"`
		CostCentreID *uuid.UUID `json:"cost_centre_id"`
	} `json:"departments"`
}

// applyDepartments keeps a mapping version and its departments once: a version no newer than the newest held is ignored (a late older event never replaces a newer mapping), so a run is always
// calculated against the newest version Fyniq has told Nemura of.
func applyDepartments(ctx context.Context, tx pgx.Tx, now time.Time, event uuid.UUID, ev departmentChangedEvent) (string, error) {
	if ev.MappingVersion < 1 || len(ev.Departments) > 500 {
		return "", &payroll.Refusal{Code: payroll.CodeInvalid, Message: "A mapping has a version of 1 or more and at most 500 departments."}
	}
	var newest int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(mapping_version), 0) FROM nemura.link_mapping`).Scan(&newest); err != nil {
		return "", err
	}
	if ev.MappingVersion <= newest {
		return "ignored_older", nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.link_mapping (mapping_version, event_id, received_at) VALUES ($1, $2, $3)`, ev.MappingVersion, event, now); err != nil {
		return "", err
	}
	seen := map[uuid.UUID]bool{}
	for _, d := range ev.Departments {
		name := strings.TrimSpace(d.Name)
		if d.DepartmentID == uuid.Nil || name == "" || len(name) > 200 || seen[d.DepartmentID] {
			return "", &payroll.Refusal{Code: payroll.CodeInvalid, Message: "A department has its own ID, once, and a name of up to 200 characters."}
		}
		seen[d.DepartmentID] = true
		if _, err := tx.Exec(ctx, `INSERT INTO nemura.link_department (mapping_version, department_id, name, cost_centre_id) VALUES ($1, $2, $3, $4)`, ev.MappingVersion, d.DepartmentID, name, d.CostCentreID); err != nil {
			return "", err
		}
	}
	return "applied", nil
}

type runPostedEvent struct {
	RunID      uuid.UUID `json:"run_id"`
	Version    int       `json:"version"`
	Status     string    `json:"status"`
	PostingRef string    `json:"posting_ref"`
	Code       string    `json:"code"`
	Message    string    `json:"message"`
}

// The event types Fyniq sends Nemura (PRD-002 Appendix E).
const (
	TypePayrollRunPosted     = "payroll.run.posted"
	TypePayrollPaymentStatus = "payroll.payments.status"
	TypeDepartmentChanged    = "people.department.changed"
)

// ReceiveLinkEvent verifies and applies one event Fyniq sent (NM-INT-10): the envelope against the link as Nemura holds it (organisations, epoch, a 5-minute window, Fyniq's signature,
// the payload's digest), then once by its event ID (a repeat answers "duplicate"), and the payload strictly. A payment's state goes through RecordPaymentState, the one function that table has;
// a run's posting answer is kept for the run to show. A refusal after the signature held is kept in the inbox with its code; nothing before it is. The payload is never logged.
func ReceiveLinkEvent(ctx context.Context, tx pgx.Tx, org uuid.UUID, now time.Time, env link.Envelope) (outcome string, err error) {
	l, err := LiveLink(ctx, tx)
	if err != nil {
		return "", err
	}
	if l.State != "active" {
		return "", &payroll.Refusal{Code: CodeLinkNone, Message: "The link is not active."}
	}
	if err := env.Verify(link.Link{Key: l.FyniqKey, Epoch: l.Epoch, SourceOrganisation: l.FyniqOrganisation.String(), TargetOrganisation: org.String(), SourceSystem: "fyniq"}, now); err != nil {
		return "", err
	}
	eventID, err := uuid.Parse(env.Header.EventID)
	if err != nil {
		return "", &link.Refusal{Code: link.CodeMalformed, Message: "the event ID is not an ID"}
	}
	var seen bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nemura.link_inbox WHERE event_id = $1)`, eventID).Scan(&seen); err != nil {
		return "", err
	}
	if seen {
		return "duplicate", nil
	}
	strict := func(v any) error {
		d := json.NewDecoder(bytes.NewReader(env.Payload))
		d.DisallowUnknownFields()
		if err := d.Decode(v); err != nil {
			return &payroll.Refusal{Code: CodeLinkMinimum, Message: "The payload has a field, or a value, that PRD-002 Appendix E does not list for " + env.Header.Type + "."}
		}
		return nil
	}
	var applyErr error
	outcome = "applied"
	if _, err := tx.Exec(ctx, "SAVEPOINT link_apply"); err != nil { // a refusal rolls the apply back and the inbox still keeps the refusal
		return "", err
	}
	sp := tx
	switch env.Header.Type {
	case TypePayrollPaymentStatus:
		var ev paymentStatusEvent
		if applyErr = strict(&ev); applyErr == nil {
			paid, perr := time.Parse(time.DateOnly, ev.PaidOn)
			if perr != nil {
				applyErr = &payroll.Refusal{Code: payroll.CodeInvalid, Message: "paid_on: give the date of the payment as YYYY-MM-DD."}
			} else {
				applyErr = RecordPaymentState(ctx, sp, org, now, audit.Actor{Type: "service", ID: "link"}, PaymentInput{Run: ev.RunID, Person: ev.PersonID, State: ev.State,
					Instruction: ev.Instruction, Amount: ev.Amount, PaidOn: paid, Source: "fyniq"})
			}
		}
	case TypePayrollRunPosted:
		var ev runPostedEvent
		if applyErr = strict(&ev); applyErr == nil {
			var ref, code, msg *string
			if ev.PostingRef != "" {
				ref = &ev.PostingRef
			}
			if ev.Code != "" {
				code = &ev.Code
			}
			if ev.Message != "" {
				msg = &ev.Message
			}
			if _, e := sp.Exec(ctx, `INSERT INTO nemura.link_run_posting (run_id, version, status, posting_ref, code, message, received_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				ev.RunID, ev.Version, ev.Status, ref, code, msg, now); e != nil {
				var pg *pgconn.PgError
				if errors.As(e, &pg) && pg.Code == "23505" {
					outcome = "ignored_older" // the same answer for the run's version again
				} else {
					applyErr = &payroll.Refusal{Code: payroll.CodeInvalid, Message: "The posting answer has a status other than posted or refused, or lacks its reference or its code."}
				}
			}
		}
	case TypeDepartmentChanged:
		var ev departmentChangedEvent
		if applyErr = strict(&ev); applyErr == nil {
			var o string
			if o, applyErr = applyDepartments(ctx, sp, now, eventID, ev); applyErr == nil {
				outcome = o
			}
		}
	default:
		applyErr = &payroll.Refusal{Code: CodeLinkType, Message: "Nemura does not receive " + env.Header.Type + " from Fyniq."}
	}
	if applyErr != nil || outcome == "ignored_older" {
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT link_apply"); err != nil {
			return "", err
		}
	} else if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT link_apply"); err != nil {
		return "", err
	}
	var code *string
	if applyErr != nil {
		var r *payroll.Refusal
		if !errors.As(applyErr, &r) {
			return "", applyErr
		}
		outcome, code = "refused", &r.Code
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nemura.link_inbox (event_id, link_id, epoch, sequence, type, object_type, object_id, object_version, payload_sha256, outcome, code, received_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		eventID, l.ID, env.Header.Epoch, env.Header.Sequence, env.Header.Type, env.Header.ObjectType, env.Header.ObjectID, env.Header.ObjectVersion, env.Header.PayloadSHA256, outcome, code, now); err != nil {
		return "", err
	}
	return outcome, applyErr
}

// EndLink ends the link: it moves through unlinking to unlinked (NM-INT-11), after the caller settled work in flight (runs approved and not posted, payments with an unknown outcome,
// events waiting), and both sides keep their data. A credential or event of its epoch is refused from then, and a later link is a new row at the next epoch.
func EndLink(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, actor audit.Actor) error {
	l, err := LiveLink(ctx, tx)
	if err != nil {
		return err
	}
	for i, state := range []string{"unlinking", "unlinked"} {
		if _, err := tx.Exec(ctx, `UPDATE nemura.fyniq_link SET state = $2, version = version + 1, changed_at = $3 WHERE id = $1`, l.ID, state, at); err != nil {
			return linkError(err)
		}
		if err := Audit(ctx, tx, org, at, actor, "nemura.fyniq_link."+state, audit.Object{Type: "fyniq_link", ID: l.ID}, map[string]audit.Change{"state": {map[int]string{0: "active", 1: "unlinking"}[i], state}, "epoch": {nil, l.Epoch}}, ""); err != nil {
			return err
		}
	}
	return nil
}
