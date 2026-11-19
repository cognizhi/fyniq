package nemura

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/hydra"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
	"github.com/cognizhi/fyniq/internal/ports"
)

// ------------------------------------------------------------------ Nemura's events to Fyniq: queued with the change, sent by the worker (NM-INT-01, NM-INT-10; ADR-T7)

// KindLinkSend is the outbox kind of an event for Fyniq. The row holds the event's identity and the object it is about, never its payload: the payload is built from the source table when the
// worker sends it, so a pay figure or a bank detail is in no outbox row, task or log (NM-INT-05, design D5).
const KindLinkSend = "link.send"

// QueuedEvent is what the outbox row keeps of an event: the event ID and sequence are fixed when it is queued, so a retry after a lost answer is the same event and Fyniq's dedupe by event ID
// absorbs it (design D13); only the signing time is the attempt's.
type QueuedEvent struct {
	EventID       string `json:"event_id"`
	Sequence      int64  `json:"sequence"`
	Type          string `json:"type"`
	ObjectType    string `json:"object_type"`
	ObjectID      string `json:"object_id"`
	ObjectVersion int    `json:"object_version"`
	CorrelationID string `json:"correlation_id"`
}

// QueueLinkEvent queues an event about an object for Fyniq in the transaction that changed the object, when the link is active; with no live link it does nothing and says so (false), so a
// standalone Nemura never queues one. The sequence is the link's next number and is taken here, under the link row's lock, so events leave in the order they were committed.
func QueueLinkEvent(ctx context.Context, tx pgx.Tx, org uuid.UUID, at time.Time, typ, objectType, objectID string, version int) (bool, error) {
	l, err := LiveLink(ctx, tx)
	if err != nil {
		if noLink(err) {
			return false, nil
		}
		return false, err
	}
	if l.State != "active" {
		return false, nil
	}
	var seq int64
	if err := tx.QueryRow(ctx, `UPDATE nemura.fyniq_link SET sequence = sequence + 1, version = version + 1, changed_at = $2 WHERE id = $1 RETURNING sequence`, l.ID, at).Scan(&seq); err != nil {
		return false, err
	}
	q := QueuedEvent{EventID: uuid.NewString(), Sequence: seq, Type: typ, ObjectType: objectType, ObjectID: objectID, ObjectVersion: version, CorrelationID: uuid.NewString()}
	raw, err := json.Marshal(q)
	if err != nil {
		return false, err
	}
	if _, err := Enqueue(ctx, tx, outbox.NewItem{Class: outbox.ClassWorker, Kind: KindLinkSend, Payload: raw, BusinessKey: "link:" + q.EventID, MaxAttempts: 8}); err != nil {
		return false, err
	}
	return true, Audit(ctx, tx, org, at, audit.Actor{Type: "service", ID: "link"}, "nemura.fyniq_link.queued", audit.Object{Type: "fyniq_link", ID: l.ID},
		map[string]audit.Change{"type": {nil, typ}, "sequence": {nil, seq}}, "")
}

// noLink reports whether err says Nemura has no link.
func noLink(err error) bool {
	var r *payroll.Refusal
	return errors.As(err, &r) && r.Code == CodeLinkNone
}

// linkPayload builds the payload of a queued event from its source rows, and nothing else (Appendix E); a type Nemura does not send yet is an error, so a row of it is never silently dropped.
func linkPayload(ctx context.Context, tx pgx.Tx, q QueuedEvent) (any, error) {
	switch q.Type {
	case "people.tax_hold.changed":
		person, err := uuid.Parse(q.ObjectID)
		if err != nil {
			return nil, fmt.Errorf("the hold event %s names no person", q.EventID)
		}
		ev, _, ok, err := taxHoldEventAt(ctx, tx, person, q.ObjectVersion)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("the hold event %s names a version that does not exist", q.EventID)
		}
		return ev, nil
	}
	return nil, fmt.Errorf("Nemura does not send %s yet", q.Type)
}

// linkRoute is where an event type goes on Fyniq's API and the scope its token needs (contracts/peoplelink/README.md).
func linkRoute(typ string) (path, scope string, ok bool) {
	switch typ {
	case "people.person.upserted", "people.bank_details.changed":
		return "/v1/people-link/projection-events", "people.projection.write", true
	case "people.tax_hold.changed":
		return "/v1/people-link/hold-events", "people.holds.write", true
	}
	return "", "", false
}

// LinkSender sends the queued events to Fyniq: the link's token (the client-credentials grant at Fyniq's OAuth server, signed by the link's client key), then the signed envelope to Fyniq's API.
// Both URLs and Fyniq's audience come from the deployment's settings, so the egress allow-list is fixed when the service starts and a link row cannot point the worker at another host (design D12).
type LinkSender struct {
	Store    *Store // the worker's
	Clock    ports.Clock
	HTTP     *http.Client // through internal/egress, to Fyniq's API and token hosts only
	APIURL   string       // Fyniq's API, without a trailing slash
	TokenURL string       // Fyniq's OAuth server's token endpoint
	Audience string       // Fyniq's API audience

	mu     sync.Mutex
	tokens map[uuid.UUID]*hydra.TokenSource
}

func (s *LinkSender) source(l LinkRow, key *ecdsa.PrivateKey) *hydra.TokenSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokens == nil {
		s.tokens = map[uuid.UUID]*hydra.TokenSource{}
	}
	t, ok := s.tokens[l.ID]
	if !ok {
		t = &hydra.TokenSource{TokenURL: s.TokenURL, ClientID: l.FyniqClientID, Key: key, Audience: s.Audience, HTTP: s.HTTP, Clock: s.Clock}
		s.tokens[l.ID] = t
	}
	return t
}

// Work is the worker's delivery of one queued event. It signs in a transaction of its own and sends after it commits, so no database lock is held over the network. An answer of 2xx finishes
// the row (a duplicate is the same); anything else fails it with Fyniq's status and stable code and never its payload, and the outbox tries again until its attempts are used up.
func (s *LinkSender) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row outbox.Row) error {
		var q QueuedEvent
		if err := json.Unmarshal(row.Payload, &q); err != nil || q.EventID == "" {
			return fmt.Errorf("the link row %s is not a queued event", row.ID)
		}
		path, scope, ok := linkRoute(q.Type)
		if !ok {
			return fmt.Errorf("Nemura does not send %s yet", q.Type)
		}
		if s.APIURL == "" || s.TokenURL == "" || s.Audience == "" || s.HTTP == nil {
			return errors.New("the link's endpoints are not configured: set NEMURA_FYNIQ_API_URL, NEMURA_FYNIQ_TOKEN_URL and NEMURA_FYNIQ_API_AUDIENCE")
		}
		var env link.Envelope
		var l LinkRow
		var key *ecdsa.PrivateKey
		gone := false
		if err := s.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			l, err = LiveLink(ctx, tx)
			if noLink(err) {
				gone = true
				return nil // the link ended with this event unsent: there is no one to tell
			}
			if err != nil {
				return err
			}
			if l.State != "active" && l.State != "unlinking" { // unlinking settles what is queued first
				return fmt.Errorf("the link is %s: the event waits", l.State)
			}
			if l.Epoch < 1 || l.FyniqClientID == "" {
				return errors.New("the link has no client at Fyniq yet")
			}
			sec, err := SecretsOf(ctx, tx, l.ID)
			if err != nil {
				return err
			}
			key = sec.Client
			payload, err := linkPayload(ctx, tx, q)
			if err != nil {
				return err
			}
			env, err = signQueued(ctx, tx, l, org, s.Clock.Now(), q, payload)
			return err
		}); err != nil {
			return err
		}
		if gone {
			return nil
		}
		token, err := s.source(l, key).Token(ctx, scope)
		if err != nil {
			return err
		}
		body, err := json.Marshal(env)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.APIURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := s.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("Fyniq did not answer: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		if resp.StatusCode/100 != 2 {
			var p struct {
				Code string `json:"code"`
			}
			_ = json.Unmarshal(answer, &p)
			return fmt.Errorf("Fyniq answered %d %s", resp.StatusCode, p.Code) // the status and Fyniq's code only, never the payload or the answer's text
		}
		return s.Store.InOrg(ctx, org, func(ctx context.Context, tx pgx.Tx) error {
			return Audit(ctx, tx, org, s.Clock.Now(), audit.Actor{Type: "service", ID: "link"}, "nemura.fyniq_link.sent", audit.Object{Type: "fyniq_link", ID: l.ID},
				map[string]audit.Change{"type": {nil, q.Type}, "sequence": {nil, q.Sequence}}, "")
		})
	}
}
