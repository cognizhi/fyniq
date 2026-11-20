// Package linksend is how Fyniq tells Nemura what became of what Nemura sent (PRD-001 FR-PPL-04, FR-PPL-11, FR-PPL-12, FR-PPL-15; PRD-002 NM-INT-03, NM-INT-10; p1-payroll-link task 5.3): the
// answer to a pay run (`payroll.run.posted`) and, when salary payments exist, the state of each payment (`payroll.payments.status`). An event is queued in the unit of work of the change it
// reports (the transactional outbox, ADR-T7), with its identity and sequence fixed then, and the worker signs it with the link's envelope key, gets the link's token from Nemura's own OAuth server
// with the link's client key (private_key_jwt) and posts it to Nemura's inbox. A retry after a lost answer is the same event, which Nemura's dedupe by event ID absorbs. Nothing here logs a payload or
// a token, and the worker's role alone can read the keys.
package linksend

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/peoplelink"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Kind is the outbox kind of an event for Nemura.
const Kind = "people_link.send"

// The event types Fyniq sends (PRD-002 Appendix E).
const (
	TypeRunPosted      = "payroll.run.posted"
	TypePaymentsStatus = "payroll.payments.status"
	// TypeDepartmentChanged tells Nemura the version of the payroll account mapping and the departments in it, which a pay run is calculated against (PRD-002 Appendix E, NM-INT-01: departments come from
	// Fyniq). Its object is the mapping and its object version is the mapping's version.
	TypeDepartmentChanged = "people.department.changed"
	// TypeSessionRevoked tells Nemura a person was signed out of, or removed from, Fyniq, by the identity the shared sign-in knows them by, so Nemura ends their sessions too (NM-INT-12, FR-PPL-18).
	TypeSessionRevoked = "identity.session.revoked"
	// TypeIdentityAccess tells Nemura what an identity may enter in Fyniq (one sign-in across the apps, design D7): whether it holds a role in the finance workspace and whether a passkey is enrolled, by issuer and
	// subject and never an email or a role name. A door and a hint, never a grant: Nemura shows or hides its switch with it and warns an approver whose passkey is missing. Its object version is the event's sequence.
	TypeIdentityAccess = "identity.access.changed"
	// TypeReplayRequested asks Nemura to send again what Fyniq's inbox missed: a range of Nemura's sequence numbers (PRD-002 NM-INT-10; contracts/peoplelink). Nemura answers by sending the current state of the objects
	// the range names, never an old payload.
	TypeReplayRequested = "link.replay.requested"
)

// Scope of Nemura's own OAuth server that the link's client holds: it reaches Nemura's event inbox and nothing else.
const Scope = "link.events.write"

// path is where each event type goes on Nemura's API.
var path = map[string]string{TypeRunPosted: "/v1/link/events", TypePaymentsStatus: "/v1/link/events", TypeDepartmentChanged: "/v1/link/events", TypeSessionRevoked: "/v1/link/events", TypeIdentityAccess: "/v1/link/events", TypeReplayRequested: "/v1/link/events"}

// Queued is what the outbox row keeps of an event. Only an event whose payload carries no pay or bank figure may keep it here (payroll.run.posted: a run's ID, its version, the posting's reference or
// the refusal's stable code and plain message): a type with figures is built from its source when it is sent, never kept in a row (design D5).
type Queued struct {
	EventID       string          `json:"event_id"`
	Sequence      int64           `json:"sequence"`
	Type          string          `json:"type"`
	ObjectType    string          `json:"object_type"`
	ObjectID      string          `json:"object_id"`
	ObjectVersion int             `json:"object_version"`
	CorrelationID string          `json:"correlation_id"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// RunPosted is the payload of payroll.run.posted: the posting's reference, or the stable code and the plain words of a refusal.
type RunPosted struct {
	RunID      string `json:"run_id"`
	Version    int    `json:"version"`
	Status     string `json:"status"` // posted or refused
	PostingRef string `json:"posting_ref"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

// Department is a department of Fyniq's payroll mapping as Nemura is told of it: Fyniq's own dimension value and its name. The mapping's accounts stay the accountant's and never travel.
type Department struct {
	DepartmentID string  `json:"department_id"`
	Name         string  `json:"name"`
	CostCentreID *string `json:"cost_centre_id"`
}

// DepartmentChanged is the payload of people.department.changed: the mapping's version and the departments it has.
type DepartmentChanged struct {
	MappingVersion int          `json:"mapping_version"`
	Departments    []Department `json:"departments"`
}

// SessionRevoked is the payload of identity.session.revoked.
type SessionRevoked struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
	Reason  string `json:"reason"` // signed_out or removed
}

// IdentityAccess is the payload of identity.access.changed, and the whole of it.
type IdentityAccess struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
	Finance bool   `json:"finance"` // the identity holds a role in Fyniq's finance workspace in this organisation
	Passkey bool   `json:"passkey"` // the identity has a passkey enrolled in Fyniq (what a pay run's approval is signed with)
}

// ReplayRequested is the payload of link.replay.requested: the range of Nemura's sequence numbers Fyniq is missing, at most 10,000 wide.
type ReplayRequested struct {
	FromSequence int64 `json:"from_sequence"`
	ToSequence   int64 `json:"to_sequence"`
}

// MaxReplaySpan is the widest range one replay request may ask for (Nemura refuses a wider one).
const MaxReplaySpan = 10000

// Queue queues an event about an object for Nemura in the unit of work that changed the object, when the organisation's link is active and has keys, and returns its event ID; otherwise it does
// nothing and returns "". The sequence is the link's next number, taken under the row's lock, so events leave in the order they were committed. The row's business key is "link:" and the event ID.
func Queue(ctx context.Context, u storage.UnitOfWork, typ, objectType, objectID string, version int, payload any) (string, error) {
	if typ != TypeRunPosted && typ != TypeDepartmentChanged && typ != TypeSessionRevoked && typ != TypeIdentityAccess && typ != TypeReplayRequested {
		return "", fmt.Errorf("Fyniq does not queue %s with a payload: build it from its source when it is sent", typ)
	}
	return queue(ctx, u, typ, objectType, objectID, version, payload)
}

// StatusRef is all the outbox keeps of a payroll.payments.status event: the ID of the line's state, from which the payload (the instruction ID, the amount, the date, the evidence's ID) is built when the
// event is sent, so that no pay figure waits in an outbox row (design D5, FR-PPL-17).
type StatusRef struct {
	StateEventID string `json:"state_event_id"`
}

// QueueStatus queues the state of a salary payment line for Nemura in the unit of work that recorded it (FR-PPL-12): nothing is queued with no link that has keys. The row names the state's ID and the
// line, never an amount.
func QueueStatus(ctx context.Context, u storage.UnitOfWork, line uuid.UUID, version int, stateEvent uuid.UUID) (string, error) {
	return queue(ctx, u, TypePaymentsStatus, "salary_line", line.String(), version, StatusRef{StateEventID: stateEvent.String()})
}

func queue(ctx context.Context, u storage.UnitOfWork, typ, objectType, objectID string, version int, payload any) (string, error) {
	l, err := u.PeopleLinks().Live(ctx)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && l.State != storage.LinkActive) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if _, err := u.PeopleLinks().Keys(ctx, l.ID); errors.Is(err, ports.ErrNotFound) {
		return "", nil // a link made before it had keys cannot send
	} else if err != nil {
		return "", err
	}
	seq, err := u.PeopleLinks().NextSequence(ctx, l.ID)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if version <= 0 {
		version = int(seq) // an identity's access is versioned by the event's own sequence, which rises with every event of the link
	}
	q := Queued{EventID: uuid.NewString(), Sequence: seq, Type: typ, ObjectType: objectType, ObjectID: objectID, ObjectVersion: version, CorrelationID: uuid.NewString(), Payload: raw}
	row, err := json.Marshal(q)
	if err != nil {
		return "", err
	}
	if _, err := u.Outbox().Enqueue(ctx, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: row, BusinessKey: "link:" + q.EventID, MaxAttempts: 8}); err != nil {
		return "", err
	}
	return q.EventID, u.Audit().Record(ctx, storage.AuditRecord{Action: "people_link.event_queued", ObjectType: "people_link", ObjectID: l.ID,
		Changes: map[string][2]any{"event": {nil, map[string]any{"type": typ, "sequence": seq}}}}) // the type and the number, never the payload
}

// Sender sends the queued events: the link's token (the client-credentials grant at Nemura's OAuth server, signed by the link's client key), then the signed envelope to Nemura's inbox. Nemura's API URL,
// token endpoint and audience come from the deployment's settings, so the egress allow-list is fixed when the service starts and no link row can point the worker at another host.
type Sender struct {
	Store    storage.Store // the worker's
	Clock    ports.Clock
	HTTP     *http.Client // through internal/egress, to Nemura's API and token hosts only
	APIURL   string       // Nemura's API, without a trailing slash and with its /api base
	TokenURL string       // Nemura's OAuth server's token endpoint
	Audience string       // Nemura's API audience

	mu     sync.Mutex
	tokens map[uuid.UUID]*hydra.TokenSource
}

func (s *Sender) source(link storage.PeopleLinkRow, clientID string, key *ecdsa.PrivateKey) *hydra.TokenSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = map[uuid.UUID]*hydra.TokenSource{}
	}
	t, ok := s.tokens[link.ID]
	if !ok {
		t = &hydra.TokenSource{TokenURL: s.TokenURL, ClientID: clientID, Key: key, Audience: s.Audience, HTTP: s.HTTP, Clock: s.Clock}
		s.tokens[link.ID] = t
	}
	return t
}

// Work is the worker's delivery of one queued event. It signs in a unit of work of its own and sends after it ends, so no database lock is held over the network. A 2xx answer finishes the row (a
// duplicate is the same); anything else fails it with Nemura's status and stable code and never the payload, and the outbox tries again until its attempts are used up.
func (s *Sender) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var q Queued
		if err := json.Unmarshal(row.Payload, &q); err != nil || q.EventID == "" {
			return fmt.Errorf("the link row %s is not a queued event", row.ID)
		}
		p, ok := path[q.Type]
		if !ok {
			return fmt.Errorf("Fyniq does not send %s", q.Type)
		}
		if s.APIURL == "" || s.TokenURL == "" || s.Audience == "" || s.HTTP == nil {
			return errors.New("the link's endpoints are not configured: set FYNIQ_NEMURA_API_URL, FYNIQ_NEMURA_TOKEN_URL and FYNIQ_NEMURA_API_AUDIENCE")
		}
		var env peoplelink.Envelope
		var link storage.PeopleLinkRow
		var clientID string
		var key *ecdsa.PrivateKey
		gone := false
		if err := s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			link, err = u.PeopleLinks().Live(ctx)
			if errors.Is(err, ports.ErrNotFound) {
				gone = true
				return nil // the link ended with this event unsent: there is no one to tell
			}
			if err != nil {
				return err
			}
			if link.State != storage.LinkActive && link.State != storage.LinkUnlinking { // unlinking settles what is queued first
				return fmt.Errorf("the link is %s: the event waits", link.State)
			}
			keys, err := u.PeopleLinks().Keys(ctx, link.ID)
			if err != nil {
				return err
			}
			sec, err := u.PeopleLinks().Secrets(ctx, link.ID)
			if err != nil {
				return err
			}
			der, err := x509.ParsePKCS8PrivateKey(sec.ClientPrivate)
			if err != nil {
				return errors.New("the link's client key cannot be read")
			}
			var ok bool
			if key, ok = der.(*ecdsa.PrivateKey); !ok {
				return errors.New("the link's client key is not an EC key")
			}
			clientID = keys.NemuraClientID
			if q.Type == TypePaymentsStatus { // built from its source now: the row held no figure (design D5)
				if q.Payload, err = statusPayload(ctx, u, q.Payload); err != nil {
					return err
				}
			}
			now := s.Clock.Now().UTC().Format(time.RFC3339)
			env, err = peoplelink.Sign(ed25519.PrivateKey(sec.LinkPrivate), peoplelink.Header{EventID: q.EventID, Type: q.Type, SchemaVersion: 1, SourceSystem: "fyniq",
				SourceOrganisation: org.String(), TargetOrganisation: link.NemuraOrganisationID.String(), Epoch: link.Epoch, Sequence: q.Sequence, ObjectType: q.ObjectType, ObjectID: q.ObjectID,
				ObjectVersion: q.ObjectVersion, EffectiveAt: now, RecordedAt: now, SignedAt: now, CorrelationID: q.CorrelationID}, q.Payload)
			return err
		}); err != nil {
			return err
		}
		if gone {
			return nil
		}
		token, err := s.source(link, clientID, key).Token(ctx, Scope)
		if err != nil {
			return err
		}
		body, err := json.Marshal(env)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.APIURL+p, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := s.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("Nemura did not answer: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		if resp.StatusCode/100 != 2 {
			var pr struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(answer, &pr)
			return fmt.Errorf("Nemura answered %d %s", resp.StatusCode, pr.Code) // the status and Nemura's code only, never the payload or the answer's text
		}
		return s.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "people_link.event_sent", ObjectType: "people_link", ObjectID: link.ID,
				Changes: map[string][2]any{"event": {nil, map[string]any{"type": q.Type, "sequence": q.Sequence}}}})
		})
	}
}

// PaymentStatus is the payload of payroll.payments.status (PRD-002 Appendix E, NM-PAY-10): the run, the person, the state, the instruction ID, the amount as dollars and cents, the date and the
// evidence's ID when a person recorded it.
type PaymentStatus struct {
	RunID       string `json:"run_id"`
	PersonID    string `json:"person_id"`
	State       string `json:"state"`
	Instruction string `json:"instruction_id"`
	Amount      string `json:"amount"`
	PaidOn      string `json:"paid_on"`
	EvidenceRef string `json:"evidence_ref"`
}

// StatusPayload is what the worker sends for a queued status row: read from its sources in a unit of work.
func StatusPayload(ctx context.Context, u storage.UnitOfWork, row json.RawMessage) (json.RawMessage, error) {
	return statusPayload(ctx, u, row)
}

// statusPayload reads the state a row names from its sources, in the worker's unit of work.
func statusPayload(ctx context.Context, u storage.UnitOfWork, row json.RawMessage) (json.RawMessage, error) {
	var ref StatusRef
	if err := json.Unmarshal(row, &ref); err != nil {
		return nil, errors.New("the status row names no state")
	}
	id, err := uuid.Parse(ref.StateEventID)
	if err != nil {
		return nil, errors.New("the status row names no state")
	}
	st, err := u.SalaryBatches().StatusOfEvent(ctx, id)
	if err != nil {
		return nil, err
	}
	amount, err := money.ParseStored(string(st.Amount), money.MustLookup("SGD"))
	if err != nil {
		return nil, err
	}
	out := PaymentStatus{RunID: st.RunID.String(), PersonID: st.PersonID.String(), State: st.State, Instruction: st.InstructionID, Amount: amount.String(), PaidOn: st.On.Format(time.DateOnly)}
	if st.EvidenceID != uuid.Nil {
		out.EvidenceRef = st.EvidenceID.String()
	}
	return json.Marshal(out)
}
