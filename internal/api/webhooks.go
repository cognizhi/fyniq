package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/webhooks"
)

// ------------------------------------------------------------------ outbound webhooks (FR-INT-12; p2-api-third-parties design D1 to D4)
//
// An Administrator, signed in and stepped up, adds an endpoint for the event types it asks for (an allow-list: operational and approval events, as IDs and times).
// It is pending until it has echoed a challenge, then active; Fyniq signs every delivery as Standard Webhooks, retries for three days, and keeps a dead-letter list a
// person replays. All of it is off until an Owner turns on "Webhooks and events" with a second approver (golden rule 6). No endpoint, event or secret is on the AI or
// MCP surface.

// The refusals of a webhook endpoint (ENG-23).
const (
	CodeWebhooksOff     = "WEBHOOK-01" // webhooks are off for the organisation
	CodeWebhookURL      = "WEBHOOK-02" // the endpoint's address is not one Fyniq may send to
	CodeWebhookEvents   = "WEBHOOK-03" // an event type that cannot be asked for, or none
	CodeWebhookVerify   = "WEBHOOK-04" // the endpoint did not echo the challenge
	CodeWebhookState    = "WEBHOOK-05" // the step is not one of the endpoint's state
	CodeWebhookNotFound = "WEBHOOK-06" // no such endpoint or delivery
)

// WebhookCodes are the refusals of the webhook operations.
var WebhookCodes = []string{CodeSandboxRefused, CodeWebhooksOff, CodeWebhookURL, CodeWebhookEvents, CodeWebhookVerify, CodeWebhookState, CodeWebhookNotFound}

// secretOverlap is how long the secret before a roll still signs, so a receiver can change over.
const secretOverlap = 24 * time.Hour

// WebhookEventType is an event type an endpoint may ask for: fyniq.<object>.<event>.v<N>, one of webhooks.DeliverableTypes.
type WebhookEventType string

// Schema implements huma.SchemaProvider.
func (WebhookEventType) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Pattern: `^fyniq\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.v[1-9][0-9]*$`,
		Description: "An event type, fyniq.<object>.<event>.v<N>, from the deliverable list of the AsyncAPI catalogue.", Examples: []any{"fyniq.ledger.posted.v1"}}
}

// WebhookEndpoint is an endpoint as Fyniq holds it: never its secret.
type WebhookEndpoint struct {
	ID           string             `json:"id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5101"`
	URL          string             `json:"url" doc:"Where events are sent: an https address on a public host." example:"https://hooks.example.com/fyniq"`
	Label        string             `json:"label" doc:"Its name, for the people who run it." example:"Accounting sync"`
	EventTypes   []WebhookEventType `json:"event_types" nullable:"false" doc:"The event types it receives, each one asked for by name."`
	State        string             `json:"state" enum:"pending,active,paused,disabled" doc:"pending: not yet verified; active: events are sent; paused: by a person, or after ten deliveries failed in a row; disabled: by a person or because the endpoint answered 410 Gone." example:"active"`
	SecretHint   string             `json:"secret_hint" doc:"The last four characters of its signing secret, to tell two apart." example:"k3Xo"`
	VerifiedAt   *time.Time         `json:"verified_at,omitempty" doc:"When it echoed Fyniq's challenge." example:"2026-11-21T09:00:00Z"`
	FailureRun   int                `json:"failure_run" doc:"Deliveries failed in a row; ten pause the endpoint." example:"0"`
	PausedReason string             `json:"paused_reason,omitempty" doc:"Why it is paused or disabled." example:"Ten deliveries failed in a row"`
	Version      int                `json:"version" example:"1"`
	CreatedAt    time.Time          `json:"created_at" example:"2026-11-21T09:00:00Z"`
}

// WebhookEndpointCreated is a new endpoint with its secret, shown this once.
type WebhookEndpointCreated struct {
	WebhookEndpoint
	Secret string `json:"secret" doc:"The signing secret (whsec_ and base64): verify each delivery's webhook-signature with it. Shown once, here and when it is rolled; Fyniq keeps it sealed and cannot show it again." example:"whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"`
}

// WebhookEndpointPage is a page of endpoints.
type WebhookEndpointPage struct {
	Items      []WebhookEndpoint `json:"items" nullable:"false" doc:"The endpoints, oldest first."`
	NextCursor string            `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

// WebhookEndpointCreate asks for an endpoint.
type WebhookEndpointCreate struct {
	URL        string             `json:"url" minLength:"8" maxLength:"2048" doc:"An https address on a public host; Fyniq never sends to a private, loopback or link-local address, never follows a redirect, and refuses an address with credentials in it." example:"https://hooks.example.com/fyniq"`
	Label      string             `json:"label" minLength:"1" maxLength:"100" doc:"Its name." example:"Accounting sync"`
	EventTypes []WebhookEventType `json:"event_types" minItems:"1" maxItems:"50" doc:"The event types to receive, by name from the AsyncAPI catalogue (contracts/events); no wildcard. Security, personal-data and payee events are never sent."`
}

// WebhookEndpointChange changes an endpoint; a field left out stays.
type WebhookEndpointChange struct {
	URL        *string            `json:"url,omitempty" maxLength:"2048" doc:"A new address; the endpoint is pending again until it echoes a challenge." example:"https://hooks.example.com/v2"`
	Label      *string            `json:"label,omitempty" maxLength:"100" doc:"A new name." example:"Accounting sync"`
	EventTypes []WebhookEventType `json:"event_types,omitempty" maxItems:"50" doc:"The whole new list of event types."`
	State      *string            `json:"state,omitempty" enum:"active,paused,disabled,pending" doc:"paused or disabled stop deliveries at once; active resumes a paused (verified) endpoint; pending re-verifies a disabled one." example:"paused"`
	Reason     *string            `json:"reason,omitempty" maxLength:"500" doc:"Why it was paused or disabled, for the people who run it." example:"The receiver is down for maintenance"`
}

// WebhookDelivery is one event's delivery to an endpoint.
type WebhookDelivery struct {
	ID            string     `json:"id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5201"`
	EndpointID    string     `json:"endpoint_id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5101"`
	EventType     string     `json:"event_type" example:"fyniq.ledger.posted.v1"`
	MessageID     string     `json:"message_id" doc:"The webhook-id of every attempt: the same each time, so a receiver deduplicates on it." example:"msg_2c4e6a8b0d1f4a3e9b7c5d3f1a2b4c6d"`
	State         string     `json:"state" enum:"pending,delivered,dead" doc:"pending: to send or retry; delivered: the endpoint answered 2xx; dead: its three days of retries are used up, and a person may replay it." example:"delivered"`
	Attempts      int        `json:"attempts" example:"1"`
	Replays       int        `json:"replays" example:"0"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty" doc:"When a pending delivery is next tried." example:"2026-11-21T09:05:00Z"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty" example:"2026-11-21T09:00:00Z"`
	LastStatus    *int       `json:"last_status,omitempty" doc:"The status of the last answer; none when the connection failed." example:"200"`
	LastError     string     `json:"last_error,omitempty" doc:"Why the last attempt failed, in a few words." example:"the endpoint answered 503"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty" example:"2026-11-21T09:00:01Z"`
	CreatedAt     time.Time  `json:"created_at" example:"2026-11-21T09:00:00Z"`
}

// WebhookDeliveryPage is a page of deliveries, newest first.
type WebhookDeliveryPage struct {
	Items      []WebhookDelivery `json:"items" nullable:"false" doc:"The deliveries, newest first."`
	NextCursor string            `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

type (
	// WebhookEndpointPath names an endpoint.
	WebhookEndpointPath struct {
		EndpointID string `path:"endpoint_id" format:"uuid" doc:"The endpoint's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5101"`
	}
	// WebhookDeliveryPath names a delivery.
	WebhookDeliveryPath struct {
		DeliveryID string `path:"delivery_id" format:"uuid" doc:"The delivery's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c5201"`
	}
	webhookEndpointListOutput struct{ Body WebhookEndpointPage }
	webhookEndpointGetInput   struct{ WebhookEndpointPath }
	webhookEndpointOutput     struct {
		ETag string `header:"ETag" doc:"The endpoint's version, for If-Match."`
		Body WebhookEndpoint
	}
	webhookEndpointCreateInput  struct{ Body WebhookEndpointCreate }
	webhookEndpointCreateOutput struct {
		CacheControl string `header:"Cache-Control" doc:"no-store: the answer carries a secret."`
		ETag         string `header:"ETag" doc:"The endpoint's version, for If-Match."`
		Body         WebhookEndpointCreated
	}
	webhookEndpointChangeInput struct {
		WebhookEndpointPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body WebhookEndpointChange
	}
	webhookEndpointChangeOutput struct {
		ETag     string `header:"ETag" doc:"The endpoint's version after the change, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     WebhookEndpoint
	}
	webhookEndpointVerifyInput struct {
		WebhookEndpointPath
		IfMatchHeader
	}
	webhookSecretRollInput struct {
		WebhookEndpointPath
		IfMatchHeader
	}
	webhookDeliveryListInput struct {
		WebhookEndpointPath
		PageQuery
		State string `query:"state" enum:"pending,delivered,dead" doc:"Only deliveries in this state; leave it out for all. The dead-letter list is state=dead." example:"dead"`
	}
	webhookDeliveryListOutput struct{ Body WebhookDeliveryPage }
	webhookReplayInput        struct {
		WebhookDeliveryPath
		KeyHeader
		PreferHeader
	}
	webhookReplayOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     WebhookDelivery
	}
)

func webhookEndpointOf(e storage.WebhookEndpoint) WebhookEndpoint {
	out := WebhookEndpoint{ID: e.ID.String(), URL: e.URL, Label: e.Label, State: e.State, SecretHint: e.SecretHint, VerifiedAt: e.VerifiedAt,
		FailureRun: e.FailureRun, PausedReason: e.PausedReason, Version: e.Version, CreatedAt: e.CreatedAt.UTC()}
	out.EventTypes = make([]WebhookEventType, 0, len(e.EventTypes))
	for _, t := range e.EventTypes {
		out.EventTypes = append(out.EventTypes, WebhookEventType(t))
	}
	return out
}

func webhookDeliveryOf(d storage.WebhookDelivery) WebhookDelivery {
	out := WebhookDelivery{ID: d.ID.String(), EndpointID: d.EndpointID.String(), EventType: d.EventType, MessageID: d.MessageID, State: d.State, Attempts: d.Attempts, Replays: d.Replays,
		LastAttemptAt: d.LastAttemptAt, LastStatus: d.LastStatus, LastError: d.LastError, DeliveredAt: d.DeliveredAt, CreatedAt: d.CreatedAt.UTC()}
	if d.State == storage.DeliveryPending {
		at := d.NextAttemptAt.UTC()
		out.NextAttemptAt = &at
	}
	return out
}

func webhookNotFound() error {
	return problem(CodeWebhookNotFound, "There is no such webhook endpoint or delivery.", "Choose one GET /v1/webhook-endpoints lists")
}

// eventStrings are the event types of a request as plain strings.
func eventStrings(in []WebhookEventType) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		out = append(out, string(t))
	}
	return out
}

// webhookEvents checks the event types an endpoint asks for: each deliverable, none twice.
func webhookEvents(types []string, field string) ([]string, error) {
	if len(types) == 0 {
		return nil, problem(CodeWebhookEvents, "An endpoint asks for at least one event type.", "Name the types to receive",
			FieldProblem{Code: CodeWebhookEvents, Field: field, Message: "is empty", Fix: "Name at least one"})
	}
	seen := map[string]bool{}
	var out []string
	for i, t := range types {
		if !webhooks.Deliverable(t) {
			return nil, problem(CodeWebhookEvents, t+" is not an event type an endpoint can ask for.", "Choose from "+strings.Join(webhooks.DeliverableTypes(), ", "),
				FieldProblem{Code: CodeWebhookEvents, Field: field, Message: "names an event that is never sent outside Fyniq, or none at all", Fix: "Choose a listed type"})
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
		_ = i
	}
	return out, nil
}

// webhookAddress checks an endpoint's address: a web address with a host and no credentials or fragment, https unless this is the local flavour. The guard checks the host at every
// send; this is the plain-words refusal at the door.
func (a *API) webhookAddress(raw, field string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	local := a.deps.Connectors.Runtime != nil && a.deps.Connectors.Runtime.Egress != nil && a.deps.Connectors.Runtime.Egress.Local
	bad := func(msg string) error {
		return problem(CodeWebhookURL, "The endpoint's address "+msg+".", "Give an https address on a public host, such as https://hooks.example.com/fyniq",
			FieldProblem{Code: CodeWebhookURL, Field: field, Message: msg, Fix: "Give an https address on a public host"})
	}
	switch {
	case err != nil || u.Host == "" || u.Hostname() == "":
		return "", bad("is not a web address")
	case u.User != nil:
		return "", bad("carries a user name or password")
	case u.Fragment != "":
		return "", bad("has a fragment")
	case u.Scheme != "https" && !(u.Scheme == "http" && local):
		return "", bad("is not https")
	}
	return u.String(), nil
}

// webhooksOn refuses while the organisation's switch is off.
func (a *API) webhooksOn(ctx context.Context, u storage.UnitOfWork) error {
	on, err := webhooks.On(ctx, u, a.deps.Clock.Now())
	if err != nil {
		return err
	}
	if !on {
		return problem(CodeWebhooksOff, "Webhooks and events are off for this organisation.",
			"Ask an Owner to turn on \"Webhooks and events\" in Settings → Organisation; it needs a second approver.")
	}
	return nil
}

// sealSecret makes a signing secret for endpoint id and seals it bound to that endpoint.
func (a *API) sealSecret(ctx context.Context, id uuid.UUID) (secret string, sealed []byte, err error) {
	if a.deps.Fields == nil {
		return "", nil, problem(CodeUnavailable, "This installation has no field key to seal a webhook secret with.", "Ask the operator to configure it")
	}
	if secret, err = webhooks.NewSecret(); err != nil {
		return "", nil, err
	}
	sealed, err = a.deps.Fields.Seal(ctx, []byte(secret), webhooks.AAD(id))
	return secret, sealed, err
}

// stepUp is the person signed in and stepped up: a webhook endpoint is a way out of the organisation's data, so no token, integration or AI makes or changes one.
func (a *API) stepUp(ctx context.Context, what string) (authn.Principal, error) {
	p, err := signedIn(ctx, what)
	if err != nil {
		return p, err
	}
	if !p.SteppedUp(a.deps.Clock.Now()) {
		return p, problem(CodeStepUpRequired, what+" needs step-up authentication in the last 5 minutes (FR-INT-12).", "Confirm it's you with your passkey or authenticator app, then try again.")
	}
	return p, nil
}

func registerWebhooks(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	versioned := []string{CodeIfMatchRequired, CodeStale, storage.CodeVersion}
	people := []string{CodeSessionRequired, CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType}

	register(a, authz.ListWebhookEndpoints, huma.Operation{
		OperationID: "listWebhookEndpoints", Method: http.MethodGet, Path: "/v1/webhook-endpoints", Tags: []string{"Integrations"},
		Summary: "List the organisation's webhook endpoints",
		Description: "The endpoints events are sent to (FR-INT-12), oldest first, a page at a time (FR-INT-10): each one's address, the event types it asked for, its state and the " +
			"last four characters of its secret. Never a secret. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*webhookEndpointListOutput, error) {
		p, _ := authn.From(ctx)
		out := WebhookEndpointPage{Items: []WebhookEndpoint{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.Webhooks().List(ctx)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(e storage.WebhookEndpoint) string { return e.ID.String() }, *in)
			for _, e := range page {
				out.Items = append(out.Items, webhookEndpointOf(e))
			}
			out.NextCursor = next
			return err
		})
		return &webhookEndpointListOutput{Body: out}, err
	})

	register(a, authz.GetWebhookEndpoint, huma.Operation{
		OperationID: "getWebhookEndpoint", Method: http.MethodGet, Path: "/v1/webhook-endpoints/{endpoint_id}", Tags: []string{"Integrations"},
		Summary:     "Read a webhook endpoint",
		Description: "One endpoint (FR-INT-12) with its ETag, for If-Match on a change. Never its secret. Read-only; no side effects.",
	}, append([]string{CodeNotFound, CodeWebhookNotFound}, locks...), func(ctx context.Context, in *webhookEndpointGetInput) (*webhookEndpointOutput, error) {
		p, _ := authn.From(ctx)
		id, err := uuid.Parse(in.EndpointID)
		if err != nil {
			return nil, webhookNotFound()
		}
		var e storage.WebhookEndpoint
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			e, err = u.Webhooks().Get(ctx, id)
			return err
		})
		if errors.Is(err, ports.ErrNotFound) {
			return nil, webhookNotFound()
		}
		if err != nil {
			return nil, err
		}
		return &webhookEndpointOutput{ETag: etag(e.Version), Body: webhookEndpointOf(e)}, nil
	})

	register(a, authz.CreateWebhookEndpoint, oneTimeSecret(huma.Operation{
		OperationID: "createWebhookEndpoint", Method: http.MethodPost, Path: "/v1/webhook-endpoints", Tags: []string{"Integrations"},
		DefaultStatus: http.StatusCreated, Summary: "Add a webhook endpoint",
		Description: "Adds an endpoint events are sent to (FR-INT-12): an https address on a public host and the event types it asks for by name (no wildcard; security, personal-data and payee events " +
			"are never sent). It is pending, and its signing secret is in this answer once: Fyniq keeps it sealed and cannot show it again (roll it to get a new one). Verify it " +
			"(verifyWebhookEndpoint) and it becomes active. Every event is a CloudEvents 1.0 message whose data is IDs and times, signed as Standard Webhooks with a webhook-id that stays the same " +
			"on every attempt. Refused (WEBHOOK-01) while the organisation's \"Webhooks and events\" setting is off, as it is until an Owner turns it on with a second approver (golden rule 6). " +
			"Needs a person signed in to Fyniq with step-up: no token, integration or AI makes an endpoint. Side effects: the endpoint and its audit event (never the secret). It takes no " +
			"Idempotency-Key and no preview, because the answer carries a secret. On the never-list for the AI and MCP (§9.6).",
	}), append(append([]string{CodeUnavailable}, people...), append(WebhookCodes, locks...)...), func(ctx context.Context, in *webhookEndpointCreateInput) (*webhookEndpointCreateOutput, error) {
		p, err := a.stepUp(ctx, "Adding a webhook endpoint")
		if err != nil {
			return nil, err
		}
		address, err := a.webhookAddress(in.Body.URL, "body.url")
		if err != nil {
			return nil, err
		}
		events, err := webhookEvents(eventStrings(in.Body.EventTypes), "body.event_types")
		if err != nil {
			return nil, err
		}
		label := strings.TrimSpace(in.Body.Label)
		if label == "" {
			return nil, problem(CodeInvalid, "The endpoint needs a name.", "Give it a short name", FieldProblem{Code: CodeInvalid, Field: "body.label", Message: "is empty", Fix: "Give it a short name"})
		}
		var out WebhookEndpointCreated
		var tag string
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if err := a.webhooksOn(ctx, u); err != nil {
				return err
			}
			if sandbox, err := inSandbox(ctx, u); err != nil || sandbox { // FR-INT-18: only to the hosts the operator allows
				if err != nil {
					return err
				}
				if err := a.sandboxWebhookHost(address); err != nil {
					return err
				}
			}
			id := uuid.Must(uuid.NewV7())
			secret, sealed, err := a.sealSecret(ctx, id)
			if err != nil {
				return err
			}
			e, err := u.Webhooks().Create(ctx, storage.NewWebhookEndpoint{ID: id, URL: address, Label: label, EventTypes: events, SecretSealed: sealed, SecretHint: webhooks.Hint(secret),
				At: a.deps.Clock.Now()}, uploader(ctx))
			if err != nil {
				return err
			}
			out, tag = WebhookEndpointCreated{WebhookEndpoint: webhookEndpointOf(e), Secret: secret}, etag(e.Version)
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &webhookEndpointCreateOutput{CacheControl: "no-store", ETag: tag, Body: out}, nil
	})

	register(a, authz.ChangeWebhookEndpoint, huma.Operation{
		OperationID: "changeWebhookEndpoint", Method: http.MethodPatch, Path: "/v1/webhook-endpoints/{endpoint_id}", Tags: []string{"Integrations"},
		Summary: "Change a webhook endpoint",
		Description: "Changes an endpoint's name, address, event types or state over the version you read (If-Match; FR-INT-12). A new address makes it pending again until it echoes a " +
			"challenge. paused and disabled stop deliveries at once; active resumes a paused endpoint that was verified; pending re-verifies a disabled one. Needs a person signed in to Fyniq with " +
			"step-up. Side effects: the change and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append(append(append(people, versioned...), WebhookCodes...), CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld), locks...),
		func(ctx context.Context, in *webhookEndpointChangeInput) (*webhookEndpointChangeOutput, error) {
			if _, err := a.stepUp(ctx, "Changing a webhook endpoint"); err != nil {
				return nil, err
			}
			id, err := uuid.Parse(in.EndpointID)
			if err != nil {
				return nil, webhookNotFound()
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (WebhookEndpoint, error) {
				before, err := u.Webhooks().Lock(ctx, id)
				if err != nil {
					return WebhookEndpoint{}, err
				}
				if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
					return WebhookEndpoint{}, err
				}
				c, err := a.endpointChange(before, in.Body)
				if err != nil {
					return WebhookEndpoint{}, err
				}
				out, err := u.Webhooks().Change(ctx, id, before.Version, c)
				return webhookEndpointOf(out), err
			})
			if errors.Is(err, ports.ErrNotFound) {
				return nil, webhookNotFound()
			}
			if err != nil {
				return nil, err
			}
			if w.Previewed {
				return &webhookEndpointChangeOutput{Applied: applied(w), Body: w.Body}, nil
			}
			return &webhookEndpointChangeOutput{ETag: etag(w.Body.Version), Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
		})

	register(a, authz.RollWebhookSecret, oneTimeSecret(huma.Operation{
		OperationID: "rollWebhookSecret", Method: http.MethodPost, Path: "/v1/webhook-endpoints/{endpoint_id}/roll-secret", Tags: []string{"Integrations"},
		Summary: "Roll a webhook endpoint's signing secret",
		Description: "Makes a new signing secret for the endpoint (FR-INT-12) and returns it once; the old one keeps signing alongside it for 24 hours, so the receiver can change over " +
			"(each delivery then carries two signatures in webhook-signature). Over the version you read (If-Match). Needs a person signed in to Fyniq with step-up. Side effects: the new " +
			"sealed secret and an audit event (never the secret). It takes no Idempotency-Key and no preview, because the answer carries a secret.",
	}), append(append(append(people, versioned...), CodeUnavailable), append(WebhookCodes, locks...)...), func(ctx context.Context, in *webhookSecretRollInput) (*webhookEndpointCreateOutput, error) {
		p, err := a.stepUp(ctx, "Rolling a webhook secret")
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.EndpointID)
		if err != nil {
			return nil, webhookNotFound()
		}
		var out WebhookEndpointCreated
		var tag string
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			before, err := u.Webhooks().Lock(ctx, id)
			if err != nil {
				return err
			}
			if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
				return err
			}
			secret, sealed, err := a.sealSecret(ctx, id)
			if err != nil {
				return err
			}
			now := a.deps.Clock.Now()
			until := now.Add(secretOverlap)
			hint := webhooks.Hint(secret)
			e, err := u.Webhooks().Change(ctx, id, before.Version, storage.WebhookEndpointChange{SecretSealed: sealed, SecretHint: &hint, PreviousSealed: before.SecretSealed, PreviousUntil: &until, At: now})
			if err != nil {
				return err
			}
			out, tag = WebhookEndpointCreated{WebhookEndpoint: webhookEndpointOf(e), Secret: secret}, etag(e.Version)
			return nil
		})
		if errors.Is(err, ports.ErrNotFound) {
			return nil, webhookNotFound()
		}
		if err != nil {
			return nil, err
		}
		return &webhookEndpointCreateOutput{CacheControl: "no-store", ETag: tag, Body: out}, nil
	})

	register(a, authz.VerifyWebhookEndpoint, exempt(exempt(huma.Operation{
		OperationID: "verifyWebhookEndpoint", Method: http.MethodPost, Path: "/v1/webhook-endpoints/{endpoint_id}/verify", Tags: []string{"Integrations"},
		Summary: "Verify a webhook endpoint",
		Description: "Proves an endpoint is the organisation's before any event is sent (FR-INT-12): Fyniq sends it one signed CloudEvent of type fyniq.webhook.verify.v1 whose data holds a " +
			"challenge, and the endpoint answers 2xx with the challenge in its body (as text or as {\"challenge\": ...}). Only then is it active; a pending or disabled-and-pending " +
			"endpoint can be verified. Fyniq sends through its guard: a private or loopback address, a redirect and an answer that takes over 10 seconds are refused. Needs a person signed in to " +
			"Fyniq with step-up. Side effects: one request to the endpoint and, when it echoes, the endpoint's change to active with its audit event.",
	}, "Idempotency-Key", "Verifying an endpoint that is active already changes nothing, and a repeat sends a new challenge: it is idempotent by its state."), "Prefer",
		"A preview would have to send the challenge to the outside, which is the operation itself."), append(append(people, versioned...), append(WebhookCodes, locks...)...),
		func(ctx context.Context, in *webhookEndpointVerifyInput) (*webhookEndpointOutput, error) {
			p, err := a.stepUp(ctx, "Verifying a webhook endpoint")
			if err != nil {
				return nil, err
			}
			id, err := uuid.Parse(in.EndpointID)
			if err != nil {
				return nil, webhookNotFound()
			}
			rt := a.deps.Connectors.Runtime
			if rt == nil || rt.Egress == nil || a.deps.Fields == nil {
				return nil, problem(CodeUnavailable, "This installation cannot send to a webhook endpoint.", "Ask the operator to configure the outbound guard and the field key")
			}
			var ep storage.WebhookEndpoint
			var secrets []string
			challenge := make([]byte, 16)
			if _, err := rand.Read(challenge); err != nil {
				return nil, err
			}
			ask := hex.EncodeToString(challenge)
			sum := sha256.Sum256([]byte(ask))
			if err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				if err := a.webhooksOn(ctx, u); err != nil {
					return err
				}
				before, err := u.Webhooks().Lock(ctx, id)
				if err != nil {
					return err
				}
				if err := checkIfMatch(in.IfMatch, etag(before.Version)); err != nil {
					return err
				}
				if sandbox, err := inSandbox(ctx, u); err != nil || sandbox { // FR-INT-18: a sandbox organisation's challenge goes only to an allowed host
					if err != nil {
						return err
					}
					if err := a.sandboxWebhookHost(before.URL); err != nil {
						return err
					}
				}
				if before.State != storage.WebhookPending {
					return problem(CodeWebhookState, "Only a pending endpoint is verified; this one is "+before.State+".", "Change its address or set it to pending to verify it again",
						FieldProblem{Code: CodeWebhookState, Field: "path.endpoint_id", Message: "is " + before.State, Fix: "Only a pending endpoint is verified"})
				}
				hash := hex.EncodeToString(sum[:])
				if ep, err = u.Webhooks().Change(ctx, id, before.Version, storage.WebhookEndpointChange{ChallengeHash: &hash, At: a.deps.Clock.Now()}); err != nil {
					return err
				}
				secrets, err = webhooks.Secrets(ctx, a.deps.Fields, ep, a.deps.Clock.Now())
				return err
			}); err != nil {
				if errors.Is(err, ports.ErrNotFound) {
					return nil, webhookNotFound()
				}
				return nil, err
			}
			now := a.deps.Clock.Now()
			data, _ := json.Marshal(map[string]string{"challenge": ask, "organisation_id": p.Organisation.String()})
			msg := "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			body, err := webhooks.Event(msg, webhooks.VerifyType, a.deps.Origin, "", now, data)
			if err != nil {
				return nil, err
			}
			res, err := webhooks.Send(ctx, rt.Egress.Bounded(64<<10, 10*time.Second).For(egress.HostOf(ep.URL)), a.deps.Clock, ep.URL, msg, now, body, secrets)
			echoed := err == nil && res.Status >= 200 && res.Status < 300 && strings.Contains(string(res.Body), ask)
			if !echoed {
				why := "did not answer"
				switch {
				case errors.Is(err, egress.ErrRefused):
					why = "is not an address Fyniq may send to (it must be a public one)"
				case err == nil && (res.Status < 200 || res.Status >= 300):
					why = "answered with a status other than 2xx"
				case err == nil:
					why = "answered, but not with the challenge"
				}
				return nil, problem(CodeWebhookVerify, "The endpoint "+why+", so it stays pending.", "Answer 2xx with the challenge from the event's data in the body, then verify again",
					FieldProblem{Code: CodeWebhookVerify, Field: "path.endpoint_id", Message: why, Fix: "Echo the challenge"})
			}
			var out storage.WebhookEndpoint
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				cur, err := u.Webhooks().Lock(ctx, id)
				if err != nil {
					return err
				}
				active := storage.WebhookActive
				out, err = u.Webhooks().Change(ctx, id, cur.Version, storage.WebhookEndpointChange{State: &active, VerifiedAt: &now, ClearChallenge: true, ClearPaused: true, ResetFailures: true, At: now})
				return err
			})
			if err != nil {
				return nil, err
			}
			return &webhookEndpointOutput{ETag: etag(out.Version), Body: webhookEndpointOf(out)}, nil
		})

	register(a, authz.ListWebhookDeliveries, huma.Operation{
		OperationID: "listWebhookDeliveries", Method: http.MethodGet, Path: "/v1/webhook-endpoints/{endpoint_id}/deliveries", Tags: []string{"Integrations"},
		Summary: "List an endpoint's deliveries",
		Description: "The deliveries to one endpoint (FR-INT-12), newest first, a page at a time (FR-INT-10), each with its attempts so far, the last status and why the last attempt failed. " +
			"state=dead is the dead-letter list: deliveries whose three days of retries are used up, which replayWebhookDelivery tries again. Read-only; no side effects.",
	}, append([]string{CodeCursor, CodeNotFound, CodeWebhookNotFound}, locks...), func(ctx context.Context, in *webhookDeliveryListInput) (*webhookDeliveryListOutput, error) {
		p, _ := authn.From(ctx)
		id, err := uuid.Parse(in.EndpointID)
		if err != nil {
			return nil, webhookNotFound()
		}
		afterText, err := decodeCursor(in.Cursor)
		if err != nil {
			return nil, err
		}
		var after *uuid.UUID
		if afterText != "" {
			x, err := uuid.Parse(afterText)
			if err != nil {
				return nil, badCursor()
			}
			after = &x
		}
		size := in.PageSize
		if size <= 0 {
			size = 100
		}
		out := WebhookDeliveryPage{Items: []WebhookDelivery{}}
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			if _, err := u.Webhooks().Get(ctx, id); err != nil {
				return err
			}
			rows, err := u.Webhooks().Deliveries(ctx, storage.WebhookQuery{EndpointID: id, State: in.State, After: after, Limit: size + 1})
			if err != nil {
				return err
			}
			if len(rows) > size {
				out.NextCursor = encodeCursor(rows[size-1].ID.String())
				rows = rows[:size]
			}
			for _, d := range rows {
				out.Items = append(out.Items, webhookDeliveryOf(d))
			}
			return nil
		})
		if errors.Is(err, ports.ErrNotFound) {
			return nil, webhookNotFound()
		}
		return &webhookDeliveryListOutput{Body: out}, err
	})

	register(a, authz.ReplayWebhookDelivery, huma.Operation{
		OperationID: "replayWebhookDelivery", Method: http.MethodPost, Path: "/v1/webhook-deliveries/{delivery_id}/replay", Tags: []string{"Integrations"},
		Summary: "Replay a dead webhook delivery",
		Description: "Puts a dead delivery back for another try at once (FR-INT-12), with the same webhook-id, so a receiver that already has it deduplicates. Only a dead delivery is " +
			"replayed; a pending or delivered one needs nothing. Needs a person signed in to Fyniq. Side effects: the delivery goes back to pending and an audit event. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(append([]string{CodeSessionRequired, CodeNotFound, CodeWebhookNotFound, CodeInvalid, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge,
		CodeMediaType}, WebhookCodes...), locks...), func(ctx context.Context, in *webhookReplayInput) (*webhookReplayOutput, error) {
		if _, err := signedIn(ctx, "Replaying a webhook delivery"); err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.DeliveryID)
		if err != nil {
			return nil, webhookNotFound()
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (WebhookDelivery, error) {
			d, err := u.Webhooks().Replay(ctx, id, a.deps.Clock.Now())
			return webhookDeliveryOf(d), err
		})
		if errors.Is(err, ports.ErrNotFound) {
			return nil, webhookNotFound()
		}
		if err != nil {
			return nil, err
		}
		if w.Previewed {
			return &webhookReplayOutput{Applied: applied(w), Body: w.Body}, nil
		}
		return &webhookReplayOutput{Replayed: replayedHeader(w.Replayed), Body: w.Body}, nil
	})
}

// endpointChange is what a person's change of an endpoint comes to, checked against the endpoint as it is: a new address makes it pending again, and a state is a step it can take.
func (a *API) endpointChange(before storage.WebhookEndpoint, in WebhookEndpointChange) (storage.WebhookEndpointChange, error) {
	c := storage.WebhookEndpointChange{At: a.deps.Clock.Now()}
	if in.Label != nil {
		l := strings.TrimSpace(*in.Label)
		if l == "" {
			return c, problem(CodeInvalid, "The endpoint needs a name.", "Give it a short name", FieldProblem{Code: CodeInvalid, Field: "body.label", Message: "is empty", Fix: "Give it a short name"})
		}
		c.Label = &l
	}
	if in.EventTypes != nil {
		events, err := webhookEvents(eventStrings(in.EventTypes), "body.event_types")
		if err != nil {
			return c, err
		}
		c.EventTypes = events
	}
	state := before.State
	if in.URL != nil && *in.URL != before.URL {
		address, err := a.webhookAddress(*in.URL, "body.url")
		if err != nil {
			return c, err
		}
		c.URL = &address
		c.ClearVerified, c.ClearChallenge = true, true
		pending := storage.WebhookPending
		c.State, state = &pending, pending
	}
	if s := in.State; s != nil && *s != state {
		ok := false
		switch {
		case *s == storage.WebhookPaused && state == storage.WebhookActive:
			ok = true
		case *s == storage.WebhookActive && state == storage.WebhookPaused && before.VerifiedAt != nil && c.URL == nil:
			ok = true
		case *s == storage.WebhookDisabled && state != storage.WebhookDisabled:
			ok = true
		case *s == storage.WebhookPending && state == storage.WebhookDisabled:
			ok = true
		}
		if !ok {
			return c, problem(CodeWebhookState, "An endpoint that is "+state+" cannot become "+*s+".", "Pause an active endpoint, resume a paused verified one, verify a pending one (verifyWebhookEndpoint), or disable one",
				FieldProblem{Code: CodeWebhookState, Field: "body.state", Message: "is not a step from " + state, Fix: "Choose a step the endpoint can take"})
		}
		c.State = s
		switch *s {
		case storage.WebhookPaused, storage.WebhookDisabled:
			if in.Reason != nil {
				r := strings.TrimSpace(*in.Reason)
				c.PausedReason = &r
			}
		case storage.WebhookActive:
			c.ClearPaused, c.ResetFailures = true, true
		case storage.WebhookPending:
			c.ClearPaused, c.ClearVerified, c.ClearChallenge = true, true, true
		}
	}
	return c, nil
}
