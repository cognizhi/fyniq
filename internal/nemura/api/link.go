package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/linkauth"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// The link's event inbox (PRD-002 NM-INT-10, NM-INT-03, NM-PAY-10; p1-nemura-fyniq-link 2.3): Fyniq calls it with the link client's bearer token and a signed envelope. It is the one operation
// of this API that takes a bearer token and no cookie, and the one that takes no person: who may call it is Fyniq's client of the live link, at the link's epoch, holding the link scope.

// LinkAuthenticator reads the bearer token of an inbox call: the link's client, or a refusal (linkauth.ErrUnauthenticated, linkauth.ErrNotLink).
type LinkAuthenticator interface {
	Authenticate(ctx context.Context, authorization string) (linkauth.Caller, error)
}

// LinkInbox receives one event Fyniq sent, for the link's client: it verifies the envelope against the link and applies the payload once. The outcome is applied, ignored_older or duplicate; a
// refusal is an error (a *link.Refusal for the envelope, a *payroll.Refusal for the payload or the state it meets).
type LinkInbox interface {
	Receive(ctx context.Context, caller linkauth.Caller, raw []byte) (outcome string, err error)
}

func init() {
	IdempotencyExempt["POST /v1/link/events"] = "an event is received once by its own event ID: a repeat answers duplicate and changes nothing (NM-INT-10)"
}

// LinkHeader is the header of an envelope (PRD-002 NM-INT-10), as Fyniq signs it.
type LinkHeader struct {
	EventID            string `json:"event_id" format:"uuid" doc:"The event's ID: a repeat does nothing." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Type               string `json:"type" doc:"The event type: payroll.run.posted or payroll.payments.status." example:"payroll.payments.status"`
	SchemaVersion      int    `json:"schema_version" minimum:"1" doc:"The payload schema's version." example:"1"`
	SourceSystem       string `json:"source_system" enum:"fyniq" doc:"The sending system." example:"fyniq"`
	SourceOrganisation string `json:"source_organisation" format:"uuid" doc:"The Fyniq organisation." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	TargetOrganisation string `json:"target_organisation" format:"uuid" doc:"The Nemura organisation." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Epoch              int    `json:"epoch" minimum:"1" doc:"The link's epoch; an earlier or a later one is refused." example:"1"`
	Sequence           int64  `json:"sequence" minimum:"0" doc:"The sender's running number on this link." example:"41"`
	ObjectType         string `json:"object_type" minLength:"1" maxLength:"50" doc:"The kind of object the event is about." example:"payroll_payment"`
	ObjectID           string `json:"object_id" minLength:"1" maxLength:"100" doc:"The object's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	ObjectVersion      int    `json:"object_version" minimum:"1" doc:"The object's version: an older one than applied is ignored." example:"2"`
	EffectiveAt        string `json:"effective_at" format:"date-time" doc:"When the change took effect, RFC 3339 in UTC." example:"2026-11-20T09:00:00Z"`
	RecordedAt         string `json:"recorded_at" format:"date-time" doc:"When the sender recorded it." example:"2026-11-20T09:00:00Z"`
	SignedAt           string `json:"signed_at" format:"date-time" doc:"When it was signed: more than 5 minutes from now is refused." example:"2026-11-20T09:00:01Z"`
	CorrelationID      string `json:"correlation_id" format:"uuid" doc:"Ties the event to its cause." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	PayloadSHA256      string `json:"payload_sha256" pattern:"^[0-9a-f]{64}$" doc:"The SHA-256 of the payload's canonical JSON (contracts/peoplelink)." example:"54a65415ad370228851a1da4b31b6fd42dc58b19a50d35cae759325f7388ce64"`
}

// LinkEvent is an event Fyniq sends: the envelope of NM-INT-10.
type LinkEvent struct {
	Header    LinkHeader     `json:"header"`
	Signature string         `json:"signature" minLength:"1" maxLength:"200" doc:"Ed25519 over the canonical header, base64url without padding, with Fyniq's link key (contracts/peoplelink)." example:"EJ6DPOspy58Ak9Q0e0rNKaX-nWfsuBEvOeZl0Y9qPzE5S1xv2o8mZl7sQd0pTuHk3r9iV6bYw4cG1nXfA2JgQ"`
	Payload   map[string]any `json:"payload" doc:"The event's data: only the fields PRD-002 Appendix E lists for its type."`
}

// LinkReceipt says what became of an event.
type LinkReceipt struct {
	EventID string `json:"event_id" format:"uuid" doc:"The event's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d77"`
	Outcome string `json:"outcome" enum:"applied,ignored_older,duplicate" doc:"applied; ignored_older: the same answer was kept already; duplicate: the event ID was received before." example:"applied"`
}

type linkEventInput struct {
	Authorization string `header:"Authorization" doc:"Bearer, then the access token Fyniq's link client got from Nemura's OAuth server for the audience nemura-api with the scope link.events.write." example:"Bearer ory_at_…"`
	RawBody       []byte
	Body          LinkEvent
}

type linkEventOutput struct{ Body LinkReceipt }

// linkProblem turns what the inbox answered into a problem. A refusal of the signed envelope or its payload is NM_LINK_REFUSED with the refusal's own code (PPL-02, NM_LINK_MINIMUM) as the field's code, so
// Fyniq can tell why; a token that is not the link's is a different answer.
func linkProblem(err error) error {
	var lr *link.Refusal
	var pr *payroll.Refusal
	switch {
	case errors.Is(err, linkauth.ErrNotLink):
		return Err(CodeLinkClient, "The token is not the link's client's, or it lacks the link scope.")
	case errors.Is(err, linkauth.ErrUnauthenticated):
		return Err(CodeUnauthenticated, "The bearer token is not valid for this API: it must be issued to the link's client for the audience nemura-api.")
	case errors.As(err, &lr):
		return Err(CodeLinkRefused, "The event was refused: "+lr.Message+".", FieldProblem{Code: lr.Code, Field: "body", Message: lr.Message, Fix: "Send the event as Fyniq signed it, within five minutes, at the link's epoch."})
	case errors.As(err, &pr) && pr.Code == "NM_LINK_NONE":
		return Err(CodeLinkNone, pr.Message)
	case errors.As(err, &pr):
		return Err(CodeLinkRefused, "The event was refused: "+pr.Message, FieldProblem{Code: pr.Code, Field: "body.payload", Message: pr.Message, Fix: "Send what PRD-002 Appendix E lists for the event's type."})
	}
	return huma.Error500InternalServerError("The event could not be received.")
}

func (a *API) registerLink() {
	huma.Register(a.API, huma.Operation{
		OperationID: "receiveLinkEvent", Method: http.MethodPost, Path: "/v1/link/events", Tags: []string{"Link"}, DefaultStatus: http.StatusOK,
		Summary: "Receive an event from Fyniq",
		Description: "Fyniq's link calls this with its client's bearer token and a signed envelope (PRD-002 NM-INT-10) carrying payroll.run.posted or payroll.payments.status. Nemura checks the token (the " +
			"audience nemura-api, the scope link.events.write, and that the client is the live link's), then the envelope against the link: both organisations, the epoch, a signing time within five " +
			"minutes, Fyniq's signature and the payload's digest. A payment's state goes through the one function of the payment events and a run's posting answer is kept for the run to show. Each " +
			"event is applied once by its ID: a repeat answers duplicate. A refusal after the signature held is kept, by code and never with the payload. Only Fyniq's link client may call it: no " +
			"session, no person and no ordinary API client; it changes only what the event says. It is on the never-list for the AI and MCP surfaces.",
		Errors: []int{401, 403, 409, 422, 503},
	}, func(ctx context.Context, in *linkEventInput) (*linkEventOutput, error) {
		if a.d.LinkAuth == nil || a.d.Link == nil {
			return nil, Err(CodeUnavailable, "The link to Fyniq is not set up in this deployment.")
		}
		caller, err := a.d.LinkAuth.Authenticate(ctx, in.Authorization)
		if err != nil {
			if errors.Is(err, linkauth.ErrUnauthenticated) || errors.Is(err, linkauth.ErrNotLink) {
				return nil, linkProblem(err)
			}
			return nil, Err(CodeUnavailable, "The OAuth server could not be asked to check the token: try again.")
		}
		outcome, err := a.d.Link.Receive(ctx, caller, in.RawBody)
		if err != nil {
			return nil, linkProblem(err)
		}
		out := &linkEventOutput{}
		out.Body.EventID, out.Body.Outcome = in.Body.Header.EventID, outcome
		return out, nil
	})
}
