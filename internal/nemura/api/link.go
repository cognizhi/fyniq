package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/link"
	"github.com/cognizhi/fyniq/internal/nemura/linkauth"
	"github.com/cognizhi/fyniq/internal/nemura/payroll"
)

// The link's event inbox (PRD-002 NM-INT-10, NM-INT-03, NM-PAY-10; p1-nemura-fyniq-link 2.3): Fyniq calls it with the link client's bearer token and a signed envelope. It is the one operation
// of this API that takes a bearer token and no cookie, and the one that takes no person: who may call it is Fyniq's client of the live link, at the link's epoch, holding the link scope.

// LinkAuthenticator reads the bearer token of a call from Fyniq: the client that sends the link's events (Events) or the one that asks a hold's status (Ask), or a refusal (linkauth.ErrUnauthenticated,
// linkauth.ErrNotLink). A token of one is no token of the other.
type LinkAuthenticator interface {
	Events(ctx context.Context, authorization string) (linkauth.Caller, error)
	Ask(ctx context.Context, authorization string) (linkauth.Caller, error)
}

// LinkHold is a person's tax-clearance hold as Nemura, the hold's authority, answers it (PRD-002 NM-INT-08).
type LinkHold struct {
	Status  string // held, released, or none when the person has never had one
	HoldID  uuid.UUID
	Version int
}

// LinkHolds answers the ask at release for the asking client: it checks the call against the live link (the same link, epoch and client) and reads the hold's latest version.
type LinkHolds interface {
	Hold(ctx context.Context, caller linkauth.Caller, person uuid.UUID) (LinkHold, error)
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
		caller, err := a.d.LinkAuth.Events(ctx, in.Authorization)
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

// LinkView is the organisation's link to Fyniq as the people team sees it. Nothing in it is secret: the two public keys Fyniq needs to link, and the IDs of the clients.
type LinkView struct {
	State              string // none, proposed, active, suspended, unlinking or unlinked
	Epoch              int
	FyniqOrganisation  uuid.UUID
	NemuraOrganisation uuid.UUID
	LinkKey            []byte          // Nemura's Ed25519 public key for what it sends
	ClientKey          json.RawMessage // the JWK of Nemura's client key, which Fyniq registers
	PeerClientID       string          // the ID Fyniq's client has at Nemura's OAuth server
	PeerAskClientID    string          // the ID Fyniq's read-only client for the ask at release has there
	FyniqClientID      string          // the ID Nemura's client has at Fyniq's OAuth server, once linked
	ProposedBy         uuid.UUID
}

// LinkAdmin is how the people team makes the link (PRD-002 NM-ARC-04): an Owner proposes, another Owner or Administrator approves with Fyniq's half.
type LinkAdmin interface {
	Current(ctx context.Context, s Session) (LinkView, error)
	Propose(ctx context.Context, s Session, fyniqOrganisation uuid.UUID) (LinkView, error)
	Approve(ctx context.Context, s Session, fyniqKey []byte, fyniqClientID string, clientKey, askKey json.RawMessage) (LinkView, error)
}

func init() {
	IdempotencyExempt["POST /v1/link/proposal"] = "a link is proposed once: a second proposal while one is live is refused (NM_LINK_STATE)"
	IdempotencyExempt["POST /v1/link/approval"] = "a proposal is approved once: a second approval finds it active (NM_LINK_STATE)"
}

// LinkState is the link as the API shows it.
type LinkState struct {
	State              string         `json:"state" enum:"none,proposed,active,suspended,unlinking,unlinked" doc:"none: never linked; proposed: Nemura made its keys and waits for Fyniq's half; active: linked; unlinked is final, and a later link starts at a higher epoch." example:"proposed"`
	Epoch              int            `json:"epoch,omitempty" doc:"The link's epoch: every event carries it and an earlier one is refused." example:"1"`
	NemuraOrganisation string         `json:"nemura_organisation_id,omitempty" format:"uuid" doc:"This organisation: what Fyniq is told when it links." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"`
	FyniqOrganisation  string         `json:"fyniq_organisation_id,omitempty" format:"uuid" doc:"The Fyniq organisation the link is to." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45"`
	LinkKey            string         `json:"link_key,omitempty" doc:"Nemura's Ed25519 public key for the link's envelopes, 32 bytes as base64url without padding: give it to Fyniq when it links." example:"EJ6DPOspy58Ak9Q0e0rNKaX-nWfsuBEvOeZl0Y9qPzE"`
	ClientKey          map[string]any `json:"client_public_key,omitempty" doc:"The public half of the key Nemura's client signs its token request to Fyniq's OAuth server with, as a JWK: give it to Fyniq when it links. Never the private key."`
	PeerAskClientID    string         `json:"nemura_ask_client_id,omitempty" doc:"The ID Fyniq's read-only client, which asks whether a person is held at the release of a payment, will have at Nemura's OAuth server: give it to Fyniq when it links." example:"fyniq-ask-0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"`
	PeerClientID       string         `json:"nemura_client_id,omitempty" doc:"The ID Fyniq's client will have at Nemura's OAuth server: give it to Fyniq when it links." example:"fyniq-link-0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e44"`
	FyniqClientID      string         `json:"fyniq_oauth_client_id,omitempty" doc:"The ID Nemura's client has at Fyniq's OAuth server, once the link is approved." example:"4f6c1b52-8d2a-4c93-9a43-0c2d7e9b1a11"`
}

func linkStateOf(v LinkView) LinkState {
	out := LinkState{State: v.State, Epoch: v.Epoch, PeerClientID: v.PeerClientID, PeerAskClientID: v.PeerAskClientID, FyniqClientID: v.FyniqClientID}
	if v.State == "" {
		out.State = "none"
	}
	if v.NemuraOrganisation != uuid.Nil {
		out.NemuraOrganisation = v.NemuraOrganisation.String()
	}
	if v.FyniqOrganisation != uuid.Nil {
		out.FyniqOrganisation = v.FyniqOrganisation.String()
	}
	if len(v.LinkKey) > 0 {
		out.LinkKey = base64.RawURLEncoding.EncodeToString(v.LinkKey)
	}
	_ = json.Unmarshal(v.ClientKey, &out.ClientKey)
	return out
}

type linkShowInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}

type linkProposeInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		FyniqOrganisation string `json:"fyniq_organisation_id" format:"uuid" doc:"The Fyniq organisation to link to." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e45"`
	}
}

type linkApproveInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		FyniqKey      string         `json:"fyniq_link_key" minLength:"43" maxLength:"43" doc:"Fyniq's Ed25519 public key for what it sends, 32 bytes as base64url without padding: shown on Fyniq's People and HR page." example:"EJ6DPOspy58Ak9Q0e0rNKaX-nWfsuBEvOeZl0Y9qPzE"`
		FyniqClientID string         `json:"fyniq_oauth_client_id" minLength:"1" maxLength:"200" doc:"The ID Nemura's client has at Fyniq's OAuth server: shown on Fyniq's People and HR page." example:"4f6c1b52-8d2a-4c93-9a43-0c2d7e9b1a11"`
		ClientKey     map[string]any `json:"fyniq_client_public_key" doc:"The public half of the key Fyniq's own client signs its token request to Nemura's OAuth server with, as a JWK: Nemura registers it for Fyniq's client. Never the private key."`
		AskKey        map[string]any `json:"fyniq_ask_client_public_key" doc:"The public half of the key Fyniq's read-only client signs its token request with, as a JWK: Nemura registers it for the client that asks whether a person is held. Never the private key."`
	}
}

type linkStateOutput struct{ Body LinkState }

// linkAdminError turns what the ceremony answered into a problem.
func linkAdminError(err error) error {
	var pr *payroll.Refusal
	switch {
	case errors.As(err, &pr) && pr.Code == "NM_LINK_SECOND_APPROVER":
		return Err(CodeLinkApprover, pr.Message)
	case errors.As(err, &pr) && pr.Code == "NM_LINK_STEP_UP":
		return Err(CodeStepUp, pr.Message)
	case errors.As(err, &pr) && (pr.Code == "NM_LINK_STATE" || pr.Code == payroll.CodeConflict):
		return Err(CodeLinkState, pr.Message)
	case errors.As(err, &pr):
		return Err(CodeInvalid, pr.Message)
	case errors.Is(err, ErrUnavailable):
		return Err(CodeUnavailable, "The link to Fyniq needs Nemura's own OAuth server, which this deployment has not set up.")
	}
	var ar *apiclient.Refusal
	if errors.As(err, &ar) {
		return Err(CodeClientInvalid, ar.Message)
	}
	return huma.Error500InternalServerError("The link could not be changed.")
}

// linkRole checks the person holds one of the roles the link's page is for, and reads the link.
func (a *API) linkRole(ctx context.Context, cookie string, roles ...authz.Role) (Session, error) {
	s, err := a.signedIn(ctx, cookie)
	if err != nil {
		return Session{}, err
	}
	held, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, a.d.Clock.Now())
	if err != nil {
		return Session{}, huma.Error500InternalServerError("The roles could not be read.")
	}
	if !slices.ContainsFunc(roles, func(r authz.Role) bool { return slices.Contains(held, r) }) {
		return Session{}, Err(CodeRoleRequired, "Only an Owner or an Administrator sees the link to Fyniq, and only an Owner starts it.")
	}
	return s, nil
}

func (a *API) registerLinkAdmin() {
	huma.Register(a.API, huma.Operation{
		OperationID: "getLink", Method: http.MethodGet, Path: "/v1/link", Tags: []string{"Link"}, Summary: "Show the link to Fyniq",
		Description: "The state of the organisation's link to Fyniq, its epoch, and what Fyniq needs to link: Nemura's envelope key and the ID Fyniq's client will have here (the client's public key is shown by the proposal only). Only an Owner or an Administrator. Never a private key. It changes nothing.",
		Errors:      []int{401, 403, 503},
	}, func(ctx context.Context, in *linkShowInput) (*linkStateOutput, error) {
		s, err := a.linkRole(ctx, in.Cookie, authz.Owner, authz.Administrator)
		if err != nil {
			return nil, err
		}
		if a.d.LinkAdmin == nil {
			return nil, Err(CodeUnavailable, "The link to Fyniq is not set up in this deployment.")
		}
		v, err := a.d.LinkAdmin.Current(ctx, s)
		if err != nil {
			return nil, linkAdminError(err)
		}
		return &linkStateOutput{Body: linkStateOf(v)}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "proposeLink", Method: http.MethodPost, Path: "/v1/link/proposal", Tags: []string{"Link"}, Summary: "Start the link to Fyniq",
		Description: "The first of three steps (PRD-002 NM-ARC-04). Only an Owner, confirmed with a passkey in the last five minutes, names the Fyniq organisation. Nemura makes its two keys and the ID Fyniq's client will have here and keeps the link as proposed; the answer holds what to give Fyniq. The private keys are kept for the worker and are never shown. Audited. It is on the never-list for the AI and MCP surfaces.",
		Errors:      []int{401, 403, 409, 422, 503}, DefaultStatus: 201,
	}, func(ctx context.Context, in *linkProposeInput) (*linkStateOutput, error) {
		s, err := a.linkRole(ctx, in.Cookie, authz.Owner)
		if err != nil {
			return nil, err
		}
		if a.d.LinkAdmin == nil {
			return nil, Err(CodeUnavailable, "The link to Fyniq is not set up in this deployment.")
		}
		org, perr := uuid.Parse(in.Body.FyniqOrganisation)
		if perr != nil {
			return nil, Err(CodeInvalid, "fyniq_organisation_id is not an ID.", FieldProblem{Code: CodeInvalid, Field: "body.fyniq_organisation_id", Message: "is not an ID", Fix: "Copy the organisation's ID from Fyniq's People and HR page."})
		}
		v, err := a.d.LinkAdmin.Propose(ctx, s, org)
		if err != nil {
			return nil, linkAdminError(err)
		}
		return &linkStateOutput{Body: linkStateOf(v)}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "approveLink", Method: http.MethodPost, Path: "/v1/link/approval", Tags: []string{"Link"}, Summary: "Approve the link to Fyniq with Fyniq's half",
		Description: "The last of three steps: another Owner or an Administrator than the one who proposed, confirmed with a passkey in the last five minutes, gives Fyniq's envelope key, its client's ID and the public half of its client key. Nemura registers Fyniq's client at its own OAuth server with the one scope that reaches the event inbox, under the ID chosen at the proposal, and the link is active at its epoch. Audited. It is on the never-list for the AI and MCP surfaces.",
		Errors:      []int{401, 403, 409, 422, 503},
	}, func(ctx context.Context, in *linkApproveInput) (*linkStateOutput, error) {
		s, err := a.linkRole(ctx, in.Cookie, authz.Owner, authz.Administrator)
		if err != nil {
			return nil, err
		}
		if a.d.LinkAdmin == nil {
			return nil, Err(CodeUnavailable, "The link to Fyniq is not set up in this deployment.")
		}
		key, kerr := base64.RawURLEncoding.DecodeString(in.Body.FyniqKey)
		if kerr != nil || len(key) != 32 {
			return nil, Err(CodeInvalid, "fyniq_link_key is not an Ed25519 public key.", FieldProblem{Code: CodeInvalid, Field: "body.fyniq_link_key", Message: "is not 32 bytes as base64url without padding", Fix: "Copy the key from Fyniq's People and HR page."})
		}
		jwk, merr := json.Marshal(in.Body.ClientKey)
		if merr != nil {
			return nil, Err(CodeInvalid, "fyniq_client_public_key is not a JWK.")
		}
		askJWK, aerr := json.Marshal(in.Body.AskKey)
		if aerr != nil {
			return nil, Err(CodeInvalid, "fyniq_ask_client_public_key is not a JWK.")
		}
		v, err := a.d.LinkAdmin.Approve(ctx, s, key, in.Body.FyniqClientID, jwk, askJWK)
		if err != nil {
			return nil, linkAdminError(err)
		}
		return &linkStateOutput{Body: linkStateOf(v)}, nil
	})
}

// LinkHoldAnswer is what Nemura says of a person's hold.
type LinkHoldAnswer struct {
	PersonID string `json:"person_id" format:"uuid" doc:"The person asked about." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
	Status   string `json:"status" enum:"held,released,none" doc:"held: the person is held for tax clearance and nothing is released to them; released: the hold was lifted; none: they never had one." example:"held"`
	HoldID   string `json:"hold_id,omitempty" format:"uuid" doc:"The hold's ID, stable across its versions." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e62"`
	Version  int    `json:"version" doc:"The hold's latest version, 0 when there is none: an answer older than the version Fyniq already saw is not an answer." example:"2"`
}

type linkHoldInput struct {
	Authorization string `header:"Authorization" doc:"Bearer, then the access token Fyniq's read-only link client got from Nemura's OAuth server for the audience nemura-api with the scope link.holds.read." example:"Bearer ory_at_…"`
	PersonID      string `path:"person_id" format:"uuid" doc:"The person." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e61"`
}

type linkHoldOutput struct{ Body LinkHoldAnswer }

func (a *API) registerLinkHold() {
	huma.Register(a.API, huma.Operation{
		OperationID: "getLinkHold", Method: http.MethodGet, Path: "/v1/link/people/{person_id}/hold", Tags: []string{"Link"}, Summary: "Ask whether a person is held for tax clearance",
		Description: "Nemura is the hold's authority (PRD-002 NM-INT-08). Fyniq asks here at the release of a payment to a person (PRD-001 FR-PPL-13), because the projection's copy may be older than Nemura's own answer. " +
			"Only Fyniq's read-only link client may call it, with a bearer token for the audience nemura-api and the scope link.holds.read, for the live link at its epoch: the client that sends events cannot ask, no session, no " +
			"person and no ordinary API client can. It answers the hold's status and version and nothing else of the person, and changes nothing. It is on the never-list for the AI and MCP surfaces.",
		Errors: []int{401, 403, 409, 503},
	}, func(ctx context.Context, in *linkHoldInput) (*linkHoldOutput, error) {
		if a.d.LinkAuth == nil || a.d.LinkHolds == nil {
			return nil, Err(CodeUnavailable, "The link to Fyniq is not set up in this deployment.")
		}
		caller, err := a.d.LinkAuth.Ask(ctx, in.Authorization)
		if err != nil {
			if errors.Is(err, linkauth.ErrUnauthenticated) || errors.Is(err, linkauth.ErrNotLink) {
				return nil, linkProblem(err)
			}
			return nil, Err(CodeUnavailable, "The OAuth server could not be asked to check the token: try again.")
		}
		person, perr := uuid.Parse(in.PersonID)
		if perr != nil {
			return nil, Err(CodeInvalid, "path.person_id is not an ID.")
		}
		h, err := a.d.LinkHolds.Hold(ctx, caller, person)
		if err != nil {
			return nil, linkProblem(err)
		}
		out := &linkHoldOutput{}
		out.Body.PersonID, out.Body.Status, out.Body.Version = person.String(), h.Status, h.Version
		if h.HoldID != uuid.Nil {
			out.Body.HoldID = h.HoldID.String()
		}
		return out, nil
	})
}
