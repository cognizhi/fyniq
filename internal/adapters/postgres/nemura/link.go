package nemura

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
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
	FyniqKey              ed25519.PublicKey
	Public                ed25519.PublicKey // Nemura's own, which Fyniq holds
	Sequence              int64
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
	err := tx.QueryRow(ctx, `SELECT id, state, epoch, version, fyniq_organisation_id, fyniq_key, link_public, sequence FROM nemura.fyniq_link WHERE state <> 'unlinked'`).
		Scan(&l.ID, &l.State, &l.Epoch, &l.Version, &l.FyniqOrganisation, &fk, &pub, &l.Sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return LinkRow{}, &payroll.Refusal{Code: CodeLinkNone, Message: "Nemura is not linked to Fyniq."}
	}
	l.FyniqKey, l.Public = fk, pub
	return l, err
}

// SignOutbound signs an event for Fyniq with the link's key, at the next sequence number, for the worker to send through the outbox (NM-INT-01, NM-INT-10). Pay and bank payloads are
// built by the caller and never reach a log. It needs the worker's grant on the link's secret.
func SignOutbound(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, typ, objectType, objectID string, objectVersion int, payload any) (link.Envelope, error) {
	l, err := LiveLink(ctx, tx)
	if err != nil {
		return link.Envelope{}, err
	}
	if l.State != "active" {
		return link.Envelope{}, &payroll.Refusal{Code: CodeLinkNone, Message: "The link is not active."}
	}
	var priv []byte
	if err := tx.QueryRow(ctx, `SELECT link_private FROM nemura.fyniq_link_secret WHERE link_id = $1`, l.ID).Scan(&priv); err != nil {
		return link.Envelope{}, err
	}
	var seq int64
	if err := tx.QueryRow(ctx, `UPDATE nemura.fyniq_link SET sequence = sequence + 1, version = version + 1, changed_at = $2 WHERE id = $1 RETURNING sequence`, l.ID, at).Scan(&seq); err != nil {
		return link.Envelope{}, err
	}
	if err := Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "link"}, "nemura.fyniq_link.sent", audit.Object{Type: "fyniq_link", ID: l.ID},
		map[string]audit.Change{"type": {nil, typ}, "sequence": {nil, seq}}, ""); err != nil { // the event's type and number, never its payload
		return link.Envelope{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return link.Envelope{}, err
	}
	now := at.UTC().Format(time.RFC3339)
	return link.Sign(ed25519.PrivateKey(priv), link.Header{EventID: uuid.NewString(), Type: typ, SchemaVersion: 1, SourceSystem: "nemura", SourceOrganisation: org.String(),
		TargetOrganisation: l.FyniqOrganisation.String(), Epoch: l.Epoch, Sequence: seq, ObjectType: objectType, ObjectID: objectID, ObjectVersion: objectVersion, EffectiveAt: now,
		RecordedAt: now, SignedAt: now, CorrelationID: uuid.NewString()}, raw)
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
	var ev TaxHoldEvent
	var version int
	var evidence *uuid.UUID
	var from time.Time
	err := tx.QueryRow(ctx, `SELECT hold_id, version, status, effective_from, evidence_id FROM nemura.tax_hold WHERE person_id = $1 ORDER BY version DESC LIMIT 1`, person).
		Scan(&ev.HoldID, &version, &ev.Status, &from, &evidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return link.Envelope{}, false, nil
	}
	if err != nil {
		return link.Envelope{}, false, err
	}
	ev.PersonID, ev.EffectiveFrom = person, from.Format(time.DateOnly)
	if evidence != nil {
		ev.EvidenceRef = evidence.String()
	}
	env, err := SignOutbound(ctx, tx, org, at, "people.tax_hold.changed", "tax_hold", person.String(), version, ev)
	return env, err == nil, err
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
