// Package chatroute sends notification events to chat channels by an organisation's routing rules (PRD-001 FR-NTF-04, FR-NTF-05; p2-api-third-parties design D9). A chat connector
// (the Slack connector today) holds the rules in its configuration: which event types go to which channel. The fan-out job asks Queue, in its own transaction, which enabled
// chat connectors have a rule for the event, and queues one outbox row for each; the worker's Maker posts it through the connector runtime. What is posted is the type's label
// and a deep link into the web app, never a title, a name or an amount (the minimal content level, the only one offered until the data inventory and the PDPA transfer assessment
// name the provider), only for the events a webhook may also carry (operational and approval events), and never an approval: a message only opens the web app, where step-up applies.
package chatroute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/contracts/events"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/webhooks"
)

// Kind is the outbox kind of one chat message.
const Kind = "chat.send"

// Actor is the job's principal in the audit trail.
var Actor = storage.Actor{Type: "scheduler", ID: "chat"}

// Route is one routing rule: an event type, in its versioned name, to a channel.
type Route struct {
	EventType string `json:"event_type"`
	Channel   string `json:"channel"`
}

// Routes reads the rules from a connector's configuration, leaving out any whose type chat may not carry (a security, personal-data or payee event).
func Routes(configuration map[string]any) []Route {
	raw, _ := json.Marshal(configuration["routes"])
	var all []Route
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil
	}
	var out []Route
	for _, r := range all {
		if r.Channel != "" && webhooks.Deliverable(r.EventType) {
			out = append(out, r)
		}
	}
	return out
}

// Text is the message for an event: the type's label and the deep link into the web app, nothing of the event's own words.
func Text(label, origin, link string) string {
	if origin == "" || !strings.HasPrefix(link, "/") {
		return label
	}
	return label + ": " + strings.TrimSuffix(origin, "/") + link
}

type payload struct {
	ConnectorID uuid.UUID `json:"connector_id"`
	Channel     string    `json:"channel"`
	Text        string    `json:"text"`
	EventID     uuid.UUID `json:"event_id"`
}

// Queuer queues the chat messages of events, in the fan-out's transaction.
type Queuer struct {
	Catalogue  *connector.Catalogue
	Dispatcher outbox.Dispatcher
	Origin     string
}

// Queue queues a message for each enabled chat connector with a rule for the event; it is the notify.Fanout's Chat hook. It queues nothing for a sandbox organisation (the
// outbox would end it, and the connector runtime would refuse it, as well).
func (q Queuer) Queue(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, ev notify.ChatEvent) error {
	if q.Catalogue == nil {
		return nil
	}
	if sandbox, err := u.Sandboxes().Is(ctx); err != nil || sandbox {
		return err
	}
	all, err := u.Connectors().Organisation(ctx)
	if err != nil {
		return err
	}
	name := events.Type(ev.Type, 1)
	for _, c := range all {
		b, ok := q.Catalogue.Get(c.Connector)
		if !ok || b.Messaging == nil || c.State != storage.ConnectorEnabled {
			continue
		}
		for _, r := range Routes(c.Configuration) {
			if r.EventType != name {
				continue
			}
			body, err := json.Marshal(payload{ConnectorID: c.ID, Channel: r.Channel, Text: Text(ev.Label, q.Origin, ev.Link), EventID: ev.ID})
			if err != nil {
				return err
			}
			if _, err := q.Dispatcher.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: Kind, Payload: body}); err != nil {
				return err
			}
		}
	}
	return nil
}

// Maker posts the queued messages.
type Maker struct {
	Store storage.Store
	// Messaging builds the connector's adapter in the work's own unit of work: connector.Runtime.Messaging under connector.ChatRule.
	Messaging func(ctx context.Context, u storage.UnitOfWork, connector uuid.UUID) (ports.Messaging, error)
	Clock     ports.Clock
}

// Runtime is the Messaging builder of a connector runtime, for the worker.
func Runtime(rt *connector.Runtime) func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (ports.Messaging, error) {
	return func(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (ports.Messaging, error) {
		p, _, err := rt.Messaging(ctx, u, id, connector.ChatRule)
		return p, err
	}
}

// Work is the outbox handler of Kind. A connector paused or disabled holds the message (FR-INT-26); the provider's request to wait holds it for that long; a refusal that will
// not change (a channel the bot is not in, a revoked token) is an error, so the row is retried and then dead, which tells the people who look after the organisation
// (the fallback to email of the content is `[left]`).
func (m Maker) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.ConnectorID == uuid.Nil || p.Channel == "" || p.Text == "" {
			return fmt.Errorf("chatroute: outbox row %s is not a chat message", row.ID)
		}
		ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor, Reason: "A chat message the routing rules asked for (FR-NTF-04)"})
		var post ports.Messaging
		if err := m.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			post, err = m.Messaging(ctx, u, p.ConnectorID)
			return err
		}); err != nil {
			if errors.Is(err, connector.ErrNotEnabled) {
				return outbox.Held{Reason: err.Error()}
			}
			if errors.Is(err, connector.ErrSandbox) {
				return nil // a sandbox organisation sends nothing outside Fyniq (FR-INT-18)
			}
			return err
		}
		_, err := post.Post(ctx, ports.ChatMessage{Channel: p.Channel, Text: p.Text}) // the provider, outside any unit of work
		var limited ports.ChatLimited
		if errors.As(err, &limited) {
			return outbox.Held{For: limited.After, Reason: "the chat provider asked to wait"}
		}
		return err
	}
}
