package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Switch is the organisation's setting: while it is off nothing is queued or sent (golden rule 6).
const Switch = "features.webhooks"

// Job is the platform job that sends what is due.
const Job = "webhooks.deliver"

// Actor is the job's principal in the audit trail.
var Actor = storage.Actor{Type: "scheduler", ID: "webhooks"}

// AAD is what a secret is sealed bound to: its endpoint, so a sealed secret copied to another endpoint does not open there.
func AAD(endpoint uuid.UUID) []byte { return []byte("webhook_endpoint:" + endpoint.String()) }

// On says whether the organisation has webhooks switched on at now.
func On(ctx context.Context, u storage.UnitOfWork, now time.Time) (bool, error) {
	def, _ := settings.Lookup(Switch)
	changes, err := u.Settings().Changes(ctx, def.Key)
	if err != nil {
		return false, err
	}
	return settings.On(def, changes, now).Value == "true", nil
}

// Secrets are the signing secrets of an endpoint in force at now: the current one, and the one before a roll until its overlap ends.
func Secrets(ctx context.Context, c ports.Cipher, e storage.WebhookEndpoint, now time.Time) ([]string, error) {
	cur, err := c.Open(ctx, e.SecretSealed, AAD(e.ID))
	if err != nil {
		return nil, fmt.Errorf("webhooks: the secret of endpoint %s does not open: %w", e.ID, err)
	}
	out := []string{string(cur)}
	if len(e.PreviousSealed) > 0 && e.PreviousUntil != nil && now.Before(*e.PreviousUntil) {
		prev, err := c.Open(ctx, e.PreviousSealed, AAD(e.ID))
		if err != nil {
			return nil, fmt.Errorf("webhooks: the earlier secret of endpoint %s does not open: %w", e.ID, err)
		}
		out = append(out, string(prev))
	}
	return out, nil
}

// QueueFor queues an event for each active endpoint of the organisation that asked for eventType, in u, the unit of work of the fan-out that recorded the event
// (the transactional outbox: no delivery exists for a change that did not commit). It does nothing while the switch is off or the type is not deliverable.
func QueueFor(ctx context.Context, u storage.UnitOfWork, eventType string, event uuid.UUID, data []byte, now time.Time) (int, error) {
	if !Deliverable(eventType) {
		return 0, nil
	}
	if on, err := On(ctx, u, now); err != nil || !on {
		return 0, err
	}
	eps, err := u.Webhooks().Subscribed(ctx, eventType)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range eps {
		// The same message ID for the same event and endpoint, however often it is sent.
		_, made, err := u.Webhooks().Queue(ctx, storage.NewWebhookDelivery{EndpointID: e.ID, EventID: event, EventType: eventType, MessageID: "msg_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
			Payload: data, At: now})
		if err != nil {
			return n, err
		}
		if made {
			n++
		}
	}
	return n, nil
}

// Deliverer sends the deliveries that are due.
type Deliverer struct {
	Store  storage.Store
	Cipher ports.Cipher
	Egress *egress.Guard // the guard for third parties: public addresses only, no redirect
	Clock  ports.Clock
	Source string // the installation's origin, the CloudEvent's source
	Batch  int    // deliveries a run sends for one organisation; 0 is 100
	// SandboxHosts are the only hosts a sandbox organisation's deliveries may reach (FR-INT-18), exact names; none by default, so a sandbox sends no webhook.
	SandboxHosts []string
}

// RunAll sends what is due for every organisation. One that fails does not stop the others.
func (d Deliverer) RunAll(ctx context.Context) error {
	orgs, err := d.Store.Organisations(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, org := range orgs {
		if _, err := d.Run(ctx, org); err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return errors.Join(errs...)
}

type job struct {
	delivery storage.WebhookDelivery
	endpoint storage.WebhookEndpoint
	secrets  []string
	sandbox  bool // the organisation is a sandbox one, read in the unit of work that read the delivery
}

// Run sends org's due deliveries, each signed afresh now, and records every try. It answers how many it sent.
func (d Deliverer) Run(ctx context.Context, org uuid.UUID) (int, error) {
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor, Reason: "Webhook deliveries (FR-INT-12)"})
	now := d.Clock.Now()
	batch := d.Batch
	if batch <= 0 {
		batch = 100
	}
	var jobs []job
	if err := d.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		if on, err := On(ctx, u, now); err != nil || !on {
			return err
		}
		sandbox, err := u.Sandboxes().Is(ctx)
		if err != nil {
			return err
		}
		due, err := u.Webhooks().Due(ctx, now, batch)
		if err != nil {
			return err
		}
		eps := map[uuid.UUID]storage.WebhookEndpoint{}
		for _, x := range due {
			e, ok := eps[x.EndpointID]
			if !ok {
				if e, err = u.Webhooks().Get(ctx, x.EndpointID); err != nil {
					return err
				}
				eps[x.EndpointID] = e
			}
			secrets, err := Secrets(ctx, d.Cipher, e, now)
			if err != nil {
				return err
			}
			jobs = append(jobs, job{delivery: x, endpoint: e, secrets: secrets, sandbox: sandbox})
		}
		return nil
	}); err != nil {
		return 0, err
	}
	sent := 0
	for _, j := range jobs {
		res := d.attempt(ctx, j, now)
		if err := d.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Webhooks().Record(ctx, j.delivery.ID, res)
			return err
		}); err != nil {
			return sent, fmt.Errorf("delivery %s: %w", j.delivery.ID, err)
		}
		sent++
	}
	return sent, nil
}

// attempt sends one delivery and says what came of it. The body is built here from the stored data, so nothing but IDs and times is ever sent.
func (d Deliverer) attempt(ctx context.Context, j job, now time.Time) storage.WebhookResult {
	r := storage.WebhookResult{At: now}
	first := now
	if j.delivery.FirstAttemptAt != nil {
		first = *j.delivery.FirstAttemptAt
	}
	fail := func(status *int, why string) storage.WebhookResult {
		r.Status, r.Error = status, why
		if next, ok := Next(j.delivery.Attempts+1, first, now); ok {
			r.NextAt = next
		} else {
			r.Dead = true
		}
		return r
	}
	u, err := url.Parse(j.endpoint.URL)
	if err != nil || (u.Scheme != "https" && !(u.Scheme == "http" && d.Egress.Local)) {
		return fail(nil, "the endpoint's address is not an https address")
	}
	if j.sandbox && !slices.Contains(d.SandboxHosts, strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))) { // FR-INT-18: never to a host the operator has not allowed
		r.Status, r.Error, r.Dead = nil, "a sandbox organisation's webhook reaches only the hosts its operator allows", true
		return r
	}
	body, err := Event(j.delivery.MessageID, j.delivery.EventType, d.Source, subjectOf(j.delivery.Payload), j.delivery.CreatedAt, j.delivery.Payload)
	if err != nil {
		return fail(nil, "the event could not be written")
	}
	client := d.Egress.Bounded(64<<10, 10*time.Second).For(egress.HostOf(j.endpoint.URL))
	res, err := Send(ctx, client, d.Clock, j.endpoint.URL, j.delivery.MessageID, now, body, j.secrets)
	r.DurationMS = int(res.Duration / time.Millisecond)
	switch {
	case err != nil:
		return fail(nil, reasonOf(err))
	case res.Status >= 200 && res.Status < 300:
		s := res.Status
		r.Status, r.Delivered = &s, true
		return r
	default:
		s := res.Status
		return fail(&s, fmt.Sprintf("the endpoint answered %d", res.Status))
	}
}

// subjectOf is the record the event is about, from its data.
func subjectOf(data []byte) string {
	var d struct {
		RecordID *string `json:"record_id"`
	}
	if json.Unmarshal(data, &d) != nil || d.RecordID == nil {
		return ""
	}
	return *d.RecordID
}

// reasonOf is a connection failure in a few words, never the address or the answer: a refused address says so, anything else says it did not connect.
func reasonOf(err error) string {
	switch {
	case errors.Is(err, egress.ErrRefused):
		return "Fyniq does not send to that address (it is not a public one)"
	case errors.Is(err, egress.ErrRedirect):
		return "the endpoint answered with a redirect, which is never followed"
	case errors.Is(err, context.DeadlineExceeded):
		return "the endpoint did not answer in time"
	}
	return "the endpoint could not be reached"
}
