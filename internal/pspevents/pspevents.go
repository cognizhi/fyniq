// Package pspevents receives and works the payment providers' webhooks (PRD-001 FR-PSP-01; p1-payment-providers design
// D3, D4): a webhook is verified on its raw bytes against its connector's own secret, stored once per provider event ID
// (the raw payload sealed, with the provider's API version and the parser's version), and queued; the worker then
// fetches the object the event names, because an event only tells Fyniq which object to fetch, and hands it to the
// posting rule of its kind. An event type the parser does not know is stored and parked for a person, and posts nothing
// (UC-PSP-01). Nothing here writes to a provider.
package pspevents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/bankfeed"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// MaxBody is the largest webhook read: Stripe's events are well under it [verify]; a larger body is refused unread.
const MaxBody = 512 << 10

// Kind is the outbox kind of working one stored event.
const Kind = "psp.process"

// Mux is where the receiver is mounted.
type Mux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// Hooks receives the payment-provider connectors' webhooks at /webhooks/psp/{connector}/{id} (the proxy's /api prefix
// stripped): the installed connector the ID names is found before any tenant is set; an unknown one, one of another
// connector, or one disabled or uninstalled gets 404; its binding checks the signature against its own secrets (401
// when it fails, and nothing is stored); a verified event is stored once, answered 200 whether new or repeated, and
// queued with an early bank sync.
type Hooks struct {
	Store      storage.Store
	Routes     storage.ConnectorRoutes
	Runtime    *connector.Runtime
	Dispatcher outbox.Dispatcher
	Clock      ports.Clock
	Fields     ports.Cipher
	Log        *slog.Logger
}

var errSignature = errors.New("the webhook's signature does not verify")

// Register mounts the receiver.
func (h *Hooks) Register(mux Mux) {
	mux.HandleFunc("POST /webhooks/psp/{connector}/{id}", h.receive)
}

// Aad is what a stored payload is sealed against: the event row it belongs to, so a payload copied to another row does
// not open there (SEC-11).
func Aad(org, id uuid.UUID) []byte { return []byte("psp_event:" + org.String() + ":" + id.String()) }

// payload is what an outbox row carries: the stored event and its connector.
type payload struct {
	EventID     uuid.UUID `json:"event_id"`
	ConnectorID uuid.UUID `json:"connector_id"`
}

func (h *Hooks) receive(w http.ResponseWriter, r *http.Request) {
	notFound := func() { http.Error(w, "no such connector, or it takes no webhook now", http.StatusNotFound) }
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		notFound()
		return
	}
	route, err := h.Routes.OrganisationConnectorRoute(r.Context(), id)
	if err != nil || route.Connector != r.PathValue("connector") || (route.State != storage.ConnectorEnabled && route.State != storage.ConnectorPaused) {
		notFound()
		return
	}
	if h.Fields == nil { // a payload carries personal data and is kept sealed or not at all (SEC-11)
		http.Error(w, "events cannot be kept: no field key is configured", http.StatusServiceUnavailable)
		return
	}
	b, ok := h.Runtime.Catalogue.Get(route.Connector)
	if !ok || b.ParseEvent == nil {
		notFound()
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBody+1))
	if err != nil || len(body) > MaxBody {
		http.Error(w, "the request is too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	ctx := storage.WithAudit(r.Context(), storage.AuditContext{Actor: &storage.Actor{Type: "integration", ID: "connector/" + id.String()},
		Reason: "A verified " + route.Connector + " webhook"})
	var badEvent bool
	err = h.Store.Do(ctx, route.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := h.Runtime.VerifyWebhook(ctx, u, id, r.Header, body)
		if err != nil {
			if errors.Is(err, connector.ErrNotEnabled) {
				return err
			}
			return errors.Join(errSignature, err)
		}
		ev, err := b.ParseEvent(r.Header, body)
		if err != nil {
			badEvent = true
			return nil // verified, but not an event of this provider's: answered 400 below, stored nowhere
		}
		_, err = h.store(ctx, u, route.Organisation, c, b, ev, body)
		return err
	})
	switch {
	case errors.Is(err, connector.ErrNotEnabled):
		notFound()
	case errors.Is(err, errSignature):
		http.Error(w, errSignature.Error(), http.StatusUnauthorized)
	case err != nil:
		if h.Log != nil {
			h.Log.ErrorContext(ctx, "psp.webhook", "connector", id.String(), "error", err.Error()) // never the payload (SEC-15)
		}
		http.Error(w, "the event could not be stored; send it again", http.StatusInternalServerError)
	case badEvent:
		http.Error(w, "the request is not an event Fyniq reads", http.StatusBadRequest)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

// store records a verified event and what follows from it, in u: a known, handled event is queued to be worked (and an
// early bank sync with it); a known informational one is settled processed; an unknown type is parked. A repeated event
// changes nothing.
func (h *Hooks) store(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, c storage.InstalledConnector, b connector.Binding, ev ports.ProviderEvent, body []byte) (storage.PSPEvent, error) {
	capability, _ := b.Manifest.Capability(connector.PaymentProvider)
	id := uuid.New()
	sealed, err := h.Fields.Seal(ctx, body, Aad(org, id))
	if err != nil {
		return storage.PSPEvent{}, err
	}
	sum := sha256.Sum256(body)
	now := h.Clock.Now()
	row, isNew, err := u.PSPEvents().Receive(ctx, storage.NewPSPEvent{ID: id, ConnectorID: c.ID, Provider: c.Connector, EventID: ev.ID, EventType: ev.Type,
		APIVersion: ev.APIVersion, ParserVersion: capability.ParserVersion, ObjectKind: string(ev.Kind), ObjectID: ev.ObjectID, Payload: sealed,
		PayloadSHA256: hex.EncodeToString(sum[:]), ReceivedAt: now})
	if err != nil || !isNew {
		return row, err
	}
	switch {
	case !ev.Known:
		return u.PSPEvents().Settle(ctx, row.ID, storage.PSPParked, "Fyniq does not know this event type ("+ev.Type+"): a person reviews it, and it posts nothing", now)
	case !ev.Handled || ev.ObjectID == "":
		return u.PSPEvents().Settle(ctx, row.ID, storage.PSPProcessed, "", now)
	}
	p, err := json.Marshal(payload{EventID: row.ID, ConnectorID: c.ID})
	if err != nil {
		return row, err
	}
	if _, err := h.Dispatcher.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: p, BusinessKey: "psp:" + row.ID.String()}); err != nil {
		return row, err
	}
	if b.BankData != nil {
		err = bankfeed.QueueOnce(ctx, h.Dispatcher, u, org, c.ID, now)
	}
	return row, err
}

// ------------------------------------------------------------------ the worker

// Handler posts what a fetched object means, in the unit of work u that also settles its event, as the connector's own
// principal. It returns a reason to park the event (an object Fyniq cannot post by its rules, a period that is locked),
// or an error to retry. It must be idempotent on the object: its postings carry a posting identity (design D4).
type Handler interface {
	Handle(ctx context.Context, u storage.UnitOfWork, c storage.InstalledConnector, ev storage.PSPEvent, obj ports.ProviderObject) (park string, err error)
}

// Processor works stored events.
type Processor struct {
	Store    storage.Store
	Runtime  *connector.Runtime
	Clock    ports.Clock
	Handlers map[ports.ProviderKind]Handler
}

// Work is the worker's handler of Kind.
func (p Processor) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var pl payload
		if err := json.Unmarshal(row.Payload, &pl); err != nil || pl.EventID == uuid.Nil || pl.ConnectorID == uuid.Nil {
			return fmt.Errorf("pspevents: outbox row %s is not an event", row.ID)
		}
		err := p.work(ctx, org, pl)
		if errors.Is(err, connector.ErrNotEnabled) {
			return p.notEnabled(ctx, org, pl)
		}
		return err
	}
}

func (p Processor) work(ctx context.Context, org uuid.UUID, pl payload) error {
	var (
		ev  storage.PSPEvent
		pp  ports.PaymentProvider
		c   storage.InstalledConnector
		err error
	)
	if err = p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if ev, err = u.PSPEvents().Get(ctx, pl.EventID); err != nil {
			return err
		}
		pp, c, err = p.Runtime.PaymentProvider(ctx, u, pl.ConnectorID, connector.ProviderWork)
		return err
	}); err != nil {
		return err
	}
	if ev.Status != storage.PSPReceived && ev.Status != storage.PSPFailed {
		return nil // worked already: a second delivery of the row does nothing
	}
	ctx = connector.Principal(ctx, c, nil)
	ctx = storage.WithReason(ctx, "Provider event "+ev.EventID)
	kind := ports.ProviderKind(ev.ObjectKind)
	obj, err := pp.Object(ctx, kind, ev.ObjectID) // the provider's truth now, never the event's payload
	if err != nil {
		return p.fail(ctx, org, ev, err)
	}
	return p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		h, ok := p.Handlers[kind]
		if !ok {
			_, err := u.PSPEvents().Settle(ctx, ev.ID, storage.PSPParked, "no posting rule reads a "+string(kind)+" yet: a person reviews it", p.Clock.Now())
			return err
		}
		park, err := h.Handle(ctx, u, c, ev, obj)
		if err != nil {
			return err
		}
		if park != "" {
			_, err = u.PSPEvents().Settle(ctx, ev.ID, storage.PSPParked, park, p.Clock.Now())
			return err
		}
		_, err = u.PSPEvents().Settle(ctx, ev.ID, storage.PSPProcessed, "", p.Clock.Now())
		return err
	})
}

// HoldFor is how long work of a paused or disabled connector waits before it is delivered again.
const HoldFor = 15 * time.Minute

// notEnabled holds an event's work while its connector is paused or disabled, as the manifest says pause and disable do
// (on_pause and on_disable: hold, FR-INT-26): the work is delivered again, with no attempt used, and goes on once the
// connector is enabled. An uninstalled connector's event is parked for a person: nothing will ever work it.
func (p Processor) notEnabled(ctx context.Context, org uuid.UUID, pl payload) error {
	state := ""
	if err := p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Connectors().Organisation(ctx)
		for _, c := range all {
			if c.ID == pl.ConnectorID {
				state = string(c.State)
			}
		}
		return err
	}); err != nil {
		return err
	}
	if state == string(storage.ConnectorPaused) || state == string(storage.ConnectorDisabled) {
		return outbox.Held{For: HoldFor, Reason: "the connector is " + state}
	}
	return p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		ev, err := u.PSPEvents().Get(ctx, pl.EventID)
		if err != nil || ev.Status == storage.PSPProcessed || ev.Status == storage.PSPParked {
			return err
		}
		_, err = u.PSPEvents().Settle(ctx, ev.ID, storage.PSPParked, "its connector is no longer installed: a person reviews the event", p.Clock.Now())
		return err
	})
}

// fail records why an attempt failed and returns the error, for the outbox to retry; the provider's answer is a status,
// never a body (SEC-15).
func (p Processor) fail(ctx context.Context, org uuid.UUID, ev storage.PSPEvent, cause error) error {
	if serr := p.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.PSPEvents().Settle(ctx, ev.ID, storage.PSPFailed, cause.Error(), p.Clock.Now())
		return err
	}); serr != nil {
		return errors.Join(cause, serr)
	}
	return cause
}

// Dead parks an event whose attempts ran out, for a person (FR-PSP-01): it never stays failed unseen.
func (p Processor) Dead() outbox.Dead {
	return func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, row storage.OutboxRow, why string) error {
		if row.Kind != Kind {
			return nil
		}
		var pl payload
		if err := json.Unmarshal(row.Payload, &pl); err != nil {
			return nil
		}
		_, err := u.PSPEvents().Settle(ctx, pl.EventID, storage.PSPParked, "Fyniq could not work this event after its retries ("+why+"): a person reviews it", p.Clock.Now())
		return err
	}
}
