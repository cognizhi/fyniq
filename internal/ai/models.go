// Package ai is the platform's work around the model port (PRD-001 §9; p1-ai-providers): reading each AI connection's
// model list from its provider (AI-80, AI-81, AI-84), and, as later tasks add them, the router, budgets and
// qualification. It calls providers only through ports.ModelProvider, never a provider's SDK (AI-70).
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ReadKind is the outbox's work of reading one connection's model list, as after its key is approved.
const ReadKind = "ai.models.read"

// DailyJob is the platform job that reads every connection's list, daily at 06:00 Singapore time (AI-80).
const DailyJob = "ai.models.read"

// readFor is how long one read may take, every page of it.
const readFor = 2 * time.Minute

// Lister reads AI connections' model lists from their providers and records them (AI-80, AI-81, AI-84).
type Lister struct {
	Store storage.Store
	// Provider builds a connection's model provider in the work's unit of work: the connector runtime's Model.
	Provider func(ctx context.Context, u storage.UnitOfWork, c storage.AIConnection) (ports.ModelProvider, error)
	Clock    ports.Clock
	Cap      int // the page cap; 0: aimodel.DefaultPageCap
	// Outbox dispatches the work a read queues, such as pricing the models it added; nil: the tick sends it.
	Outbox *outbox.Dispatcher
}

// readable reports whether a connection's list can be read: it has its key, or needs none.
func readable(c storage.AIConnection) bool { return c.Auth != "api-key" || c.CredentialRef != "" }

func toDomain(ms []storage.AIModel) []aimodel.Model {
	out := make([]aimodel.Model, 0, len(ms))
	for _, m := range ms {
		out = append(out, aimodel.Model{ID: m.Model, Name: m.Name, AliasOf: m.AliasOf, Created: m.Created, Shutdown: m.Shutdown,
			ContextWindow: m.ContextWindow, MaxOutput: m.MaxOutput, Reports: m.Reports, FirstListed: m.FirstListed, LastListed: m.LastListed,
			Listed: m.Listed, NotCallableAt: m.NotCallableAt, Allowed: m.Allowed, ListedPrice: listedPriceOf(m.ListedPrice)})
	}
	return out
}

// listedPriceOf is a stored listed price as the domain holds it; nil when there is none or it cannot be read.
func listedPriceOf(raw json.RawMessage) *ports.ListedPrice {
	if len(raw) == 0 {
		return nil
	}
	var p ports.ListedPrice
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	return &p
}

func fromDomain(connection uuid.UUID, ms []aimodel.Model) []storage.AIModel {
	out := make([]storage.AIModel, 0, len(ms))
	for _, m := range ms {
		out = append(out, storage.AIModel{ConnectionID: connection, Model: m.ID, Name: m.Name, AliasOf: m.AliasOf, Created: m.Created,
			Shutdown: m.Shutdown, ContextWindow: m.ContextWindow, MaxOutput: m.MaxOutput, Reports: m.Reports, FirstListed: m.FirstListed,
			LastListed: m.LastListed, Listed: m.Listed, NotCallableAt: m.NotCallableAt, Allowed: m.Allowed, ListedPrice: rawListedPrice(m.ListedPrice)})
	}
	return out
}

// rawListedPrice is a listed price as the store keeps it; nil for none.
func rawListedPrice(p *ports.ListedPrice) json.RawMessage {
	if p == nil {
		return nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return b
}

// failure is why a read was not complete, in Fyniq's words: the adapter's error, already scrubbed of the credential
// (AI-79), cut to what the record keeps.
func failure(outcome string, err error) string {
	if outcome == aimodel.Complete {
		return ""
	}
	msg := "the provider's list ended before its last page"
	if outcome == aimodel.Partial && err == nil {
		msg = "the page cap was reached before the provider's last page"
	}
	if err != nil {
		msg = err.Error()
	}
	if len(msg) > 500 {
		msg = msg[:497] + "…"
	}
	return msg
}

// Read reads connection id's list for trigger, by who, and records the read and the models it leaves: a complete read
// marks the models it does not list no longer listed; a partial or failed one changes nothing it did not see (AI-84).
// The provider is called outside any transaction.
func (l Lister) Read(ctx context.Context, org, id uuid.UUID, trigger, by string) (storage.AIModelListRead, []storage.AIModel, error) {
	var c storage.AIConnection
	var kept []storage.AIModel
	var p ports.ModelProvider
	var built error
	if err := l.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if c, err = u.AIConnections().Get(ctx, id); err != nil {
			return err
		}
		if kept, err = u.AIModels().Of(ctx, id); err != nil {
			return err
		}
		if !readable(c) {
			built = errors.New("the connection has no API key yet")
			return nil
		}
		p, built = l.Provider(ctx, u, c)
		return nil
	}); err != nil {
		return storage.AIModelListRead{}, nil, err
	}
	listed, outcome, pages := []ports.ListedModel(nil), aimodel.Failed, 0
	err := built
	if err == nil {
		cap := l.Cap
		if cap <= 0 {
			cap = aimodel.DefaultPageCap
		}
		readCtx, cancel := context.WithTimeout(ctx, readFor)
		listed, outcome, pages, err = aimodel.Read(func(after string) (ports.ModelPage, error) { return p.ListModels(readCtx, after) }, cap)
		cancel()
	}
	now := l.Clock.Now()
	priced := slices.ContainsFunc(listed, func(m ports.ListedModel) bool { return m.Price != nil })
	merged, added, removed := toDomain(kept), 0, 0
	if outcome != aimodel.Failed {
		merged, added, removed = aimodel.Merge(merged, listed, outcome, now)
	}
	read := storage.AIModelListRead{ConnectionID: id, Trigger: trigger, Outcome: outcome, Pages: pages, Added: added, Removed: removed,
		Failure: failure(outcome, err), ReadBy: by, ReadAt: now}
	models := fromDomain(id, merged)
	err = l.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if read, err = u.AIModels().Record(ctx, read, models); err != nil || (read.Added == 0 && !priced) {
			return err
		}
		// New models, and the prices the list gives, are priced at once, not on the next daily read (AI-82).
		item, err := PriceItem(id, read.ID)
		switch {
		case err != nil:
		case l.Outbox != nil:
			_, err = l.Outbox.Enqueue(ctx, u, org, item)
		default:
			_, err = u.Outbox().Enqueue(ctx, item)
		}
		return err
	})
	return read, models, err
}

// Work is the outbox's work of ReadKind: read the connection its payload names, for its trigger.
func (l Lister) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p struct {
			Connection uuid.UUID `json:"connection_id"`
			Trigger    string    `json:"trigger"`
		}
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return fmt.Errorf("ai.models.read: %w", err)
		}
		_, _, err := l.Read(ctx, org, p.Connection, p.Trigger, "system:"+ReadKind)
		return err
	}
}

// ReadItem is the outbox item that has the worker read a connection's list for trigger, once for the change that
// asked for it, such as the approval of a key.
func ReadItem(connection uuid.UUID, trigger, change string) (storage.NewOutboxItem, error) {
	payload, err := json.Marshal(map[string]string{"connection_id": connection.String(), "trigger": trigger})
	return storage.NewOutboxItem{Class: storage.ClassWorker, Kind: ReadKind, Payload: payload,
		BusinessKey: "ai-models:" + connection.String() + ":" + trigger + ":" + change}, err
}

// Daily reads the list of every connection that can be read, in every organisation (AI-80): a connection whose read
// fails keeps its last good list (AI-84), and the others are still read.
func (l Lister) Daily(ctx context.Context) error {
	orgs, err := l.Store.Organisations(ctx)
	if err != nil {
		return err
	}
	var failed []error
	for _, org := range orgs {
		var all []storage.AIConnection
		if err := l.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			all, err = u.AIConnections().List(ctx)
			return err
		}); err != nil {
			failed = append(failed, err)
			continue
		}
		for _, c := range all {
			if !readable(c) {
				continue
			}
			if _, _, err := l.Read(ctx, org, c.ID, aimodel.ByDaily, "system:"+DailyJob); err != nil {
				failed = append(failed, fmt.Errorf("%s: %w", c.ID, err))
			}
		}
	}
	return errors.Join(failed...)
}
