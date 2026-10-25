package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// RelayKind is the outbox kind of telling people of something that happened in the installation organisation, which has
// no members (FR-ADM-07): the event is queued in the unit of work of what happened, and the worker emits it in each
// organisation the people it names belong to, as DomainChecks does. The worker, which alone may list the Super admins,
// resolves an audience of every Super admin.
const RelayKind = "notify.relay"

type relayPayload struct {
	Type       string      `json:"type"`
	RecordType string      `json:"record_type"`
	RecordID   *uuid.UUID  `json:"record_id,omitempty"`
	Title      string      `json:"title"`
	Link       string      `json:"link"`
	Users      []uuid.UUID `json:"users,omitempty"`
	Supers     bool        `json:"super_admins,omitempty"` // every active Super admin, but Except
	Except     []uuid.UUID `json:"except,omitempty"`
	At         time.Time   `json:"at"`
}

// SuperAdmins is an audience for Relay: every active Super admin but except, as the worker finds them when it runs.
func SuperAdmins(except ...uuid.UUID) Audience {
	return Audience{Capability: superAdmins, Except: except}
}

// superAdmins marks the audience SuperAdmins makes; it is no Appendix D row, so Recipients never matches it.
const superAdmins = "super admins"

// Relay queues e, in u of the organisation org, for the users e.Audience names or, with SuperAdmins, every Super admin
// but those it excepts; nobody named, nothing is queued.
func Relay(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, e Event) error {
	if _, ok := Lookup(e.Type); !ok {
		return fmt.Errorf("notify: no notification type %s", e.Type)
	}
	supers := e.Audience.Capability == superAdmins
	if len(e.Audience.Users) == 0 && !supers {
		return nil
	}
	b, err := json.Marshal(relayPayload{Type: e.Type, RecordType: e.RecordType, RecordID: e.RecordID, Title: e.Title, Link: e.Link,
		Users: e.Audience.Users, Supers: supers, Except: e.Audience.Except, At: e.At})
	if err != nil {
		return err
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: RelayKind, Payload: b})
	return err
}

// Relays emits relayed events in the organisations of the people they name.
type Relays struct {
	Store      storage.Store
	Operators  Operators
	Dispatcher outbox.Dispatcher
}

// Work is the worker's handler of RelayKind: in each organisation, the named people who are its active members get the
// event, fanned out as any other; an organisation none of them belongs to gets nothing.
func (r Relays) Work() outbox.Work {
	return func(ctx context.Context, _ uuid.UUID, row storage.OutboxRow) error {
		var p relayPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil || (len(p.Users) == 0 && !p.Supers) {
			return fmt.Errorf("notify: outbox row %s is not a relay", row.ID)
		}
		users := p.Users
		if p.Supers {
			supers, err := r.Operators.SuperAdminUsers(ctx)
			if err != nil {
				return err
			}
			users = append(users, supers...)
		}
		users = slices.DeleteFunc(users, func(id uuid.UUID) bool { return slices.Contains(p.Except, id) })
		if len(users) == 0 {
			return nil
		}
		orgs, err := r.Store.Organisations(ctx)
		if err != nil {
			return err
		}
		var errs []error
		for _, org := range orgs {
			err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				members, err := u.Members().List(ctx, p.At)
				if err != nil {
					return err
				}
				here := Recipients(Audience{Users: users}, members)
				if len(here) == 0 {
					return nil
				}
				_, err = Emit(ctx, r.Dispatcher, u, org, Event{Type: p.Type, RecordType: p.RecordType, RecordID: p.RecordID, Title: p.Title,
					Link: p.Link, Audience: Audience{Users: here}, At: p.At})
				return err
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
			}
		}
		return errors.Join(errs...)
	}
}

// ConnectorType is the notification that a connector was paused by one person, which takes effect without a second
// (the owner's decision of 25 Oct 2026, FR-INT-26): a security type, so it is always on and emailed at once.
const ConnectorType = "security.connector"

// ConnectorPaused tells those who manage the organisation's connectors (Create or Approve on Organisation integrations:
// the Administrator and the Owner), but the person who paused it, that connector name was paused.
func ConnectorPaused(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, by, id uuid.UUID, name, link string, at time.Time) error {
	_, err := Emit(ctx, d, u, org, Event{Type: ConnectorType, RecordType: "organisation_connector", RecordID: &id, Link: link, At: at,
		Title:    name + " was paused: its work is held until someone resumes it, which needs a second person",
		Audience: Audience{Capability: string(authz.Integrations), Need: "CA", Except: []uuid.UUID{by}}})
	return err
}
