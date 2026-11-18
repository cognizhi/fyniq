package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/apiclient"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	revoke "github.com/cognizhi/fyniq/internal/tokens"
)

// ------------------------------------------------------------------ integration clients (FR-INT-02, SEC-07, AI-34)
//
// An integration client is a system that calls the API on its own: OAuth 2.1 client credentials, authenticated with
// private_key_jwt by the integration's own key, with tokens from the OAuth server (ADR-T3). It acts for a member of the
// organisation and never does more than that member may; its narrow scopes narrow that further, to whole areas, and never
// reach an approval or a release (authz.ScopeAllows). It calls only from its networks, within its own rate limits, until
// its expiry. An Administrator asks to register one; the Owner approves with a second approver (FR-INT-02's dual
// control), and only then is it registered with the OAuth server. Revoking one is never held back: the API refuses its
// tokens at once (within the introspection cache's 30 seconds, AI-38), and the worker deletes it and its tokens from the
// OAuth server. Every change it makes is audited as the integration, with its client ID (AUD-01).

// ClientScope is a narrow scope of an API client (FR-INT-02): fyniq.read.<area> or fyniq.write.<area>.
type ClientScope string

// Schema implements huma.SchemaProvider.
func (ClientScope) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Pattern: `^fyniq\.(read|write)\.[a-z]+$`, Description: "A narrow scope: fyniq.read.<area> or fyniq.write.<area>.",
		Examples: []any{"fyniq.read.accounting"}}
}

// ClientNetwork is a network an API client may call from, as a CIDR prefix (SEC-07).
type ClientNetwork string

// Schema implements huma.SchemaProvider.
func (ClientNetwork) Schema(huma.Registry) *huma.Schema {
	one, most := 1, 49
	return &huma.Schema{Type: "string", MinLength: &one, MaxLength: &most, Description: "A network, as a CIDR prefix.", Examples: []any{"203.0.113.0/24"}}
}

// The lists of scopes and networks, as the contract types them and as the domain holds them.
func clientScopes(xs []string) []ClientScope {
	out := make([]ClientScope, len(xs))
	for i, x := range xs {
		out[i] = ClientScope(x)
	}
	return out
}

func clientNetworks(xs []string) []ClientNetwork {
	out := make([]ClientNetwork, len(xs))
	for i, x := range xs {
		out[i] = ClientNetwork(x)
	}
	return out
}

func scopeStrings(xs []ClientScope) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = string(x)
	}
	return out
}

func networkStrings(xs []ClientNetwork) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = string(x)
	}
	return out
}

// IntegrationClientTokenLifespan is how long each access token of an integration client lives (AI-38: at most 60 minutes).
const IntegrationClientTokenLifespan = time.Hour

// IntegrationClient is an integration client of the organisation.
type IntegrationClient struct {
	ID              string          `json:"id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d50"`
	ClientID        string          `json:"client_id" doc:"The OAuth client ID: the iss and sub of the client's private_key_jwt assertions, and the client_id of its token requests (client_credentials grant)." example:"6f1c2b9e-3d4a-4c5b-8e7f-0a1b2c3d4e5f"`
	Name            string          `json:"name" doc:"Its name in the organisation." example:"Nightly canary"`
	Class           string          `json:"class" enum:"integration" doc:"Its client class (AI-34): integration. Every change it makes still goes through the approval policies." example:"integration"`
	OwnerID         string          `json:"owner_id" format:"uuid" doc:"The member it acts for: it never does more than this member may do now." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Scopes          []ClientScope   `json:"scopes" nullable:"false" doc:"Its narrow scopes: fyniq.read.<area> views an area, fyniq.write.<area> also prepares and posts in it. No scope approves or releases."`
	Networks        []ClientNetwork `json:"networks" nullable:"false" doc:"The networks it may call from, as CIDR prefixes; none: anywhere."`
	ReadsPerMinute  int             `json:"reads_per_minute,omitempty" doc:"Its own limit of reads a minute; none: the platform's 600." example:"60"`
	WritesPerMinute int             `json:"writes_per_minute,omitempty" doc:"Its own limit of writes a minute; none: the platform's 120." example:"30"`
	KeyThumbprint   string          `json:"key_thumbprint" doc:"The RFC 7638 SHA-256 thumbprint of the public key its assertions are signed with." example:"NzbLsXh8uDCcd-6MNwXF4W_7noWXFZAfHkxZsRGC9Xs"`
	ExpiresAt       time.Time       `json:"expires_at" doc:"When it stops: within a year of its registration (SEC-07)." example:"2027-11-17T00:00:00Z"`
	Status          string          `json:"status" enum:"live,expired,revoked" doc:"live until it expires or is revoked." example:"live"`
	Version         int             `json:"version" example:"1"`
	ApprovalID      string          `json:"approval_id" format:"uuid" doc:"The approval request that registered it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d60"`
	CreatedBy       string          `json:"created_by" example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	CreatedAt       time.Time       `json:"created_at" example:"2026-11-17T09:00:00Z"`
	RevokedAt       *time.Time      `json:"revoked_at,omitempty" example:"2026-12-01T09:00:00Z"`
	RevokedBy       string          `json:"revoked_by,omitempty" example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	RevokeReason    string          `json:"revoke_reason,omitempty" example:"The integration was retired."`
}

// IntegrationClientList is a page of the organisation's integration clients.
type IntegrationClientList struct {
	Items      []IntegrationClient `json:"items" nullable:"false" doc:"The clients, oldest first."`
	NextCursor string              `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

// IntegrationClientCreate asks for a new integration client.
type IntegrationClientCreate struct {
	Name            string          `json:"name" minLength:"1" maxLength:"80" doc:"Its name in the organisation." example:"Nightly canary"`
	OwnerID         string          `json:"owner_id,omitempty" format:"uuid" doc:"The member it acts for; you when left out. It never does more than this member may do now." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Scopes          []ClientScope   `json:"scopes" minItems:"1" maxItems:"16" doc:"Its narrow scopes, only what it needs: fyniq.read.<area> (reports, sales, purchases, banking, accounting, planning, compliance) or fyniq.write.<area> (sales, purchases, banking, accounting)."`
	Networks        []ClientNetwork `json:"networks,omitempty" maxItems:"32" doc:"The networks it may call from, as CIDR prefixes; none: anywhere. Give them where you can (SEC-07)."`
	ReadsPerMinute  int             `json:"reads_per_minute,omitempty" minimum:"0" maximum:"600" doc:"Its own limit of reads a minute, at most the platform's 600; none: the platform's." example:"60"`
	WritesPerMinute int             `json:"writes_per_minute,omitempty" minimum:"0" maximum:"120" doc:"Its own limit of writes a minute, at most the platform's 120; none: the platform's." example:"30"`
	ExpiresAt       time.Time       `json:"expires_at" doc:"When it stops, within a year (SEC-07)." example:"2027-11-17T00:00:00Z"`
	PublicKey       map[string]any  `json:"public_key" doc:"The public half of the key its private_key_jwt assertions are signed with, as a JWK (RFC 7517): an EC P-256 key (ES256) or an RSA key of 2048 bits or more (RS256). Never send the private key." example:"{\"kty\":\"EC\",\"crv\":\"P-256\",\"x\":\"f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU\",\"y\":\"x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0\"}"`
}

// IntegrationClientRevoke ends an integration client.
type IntegrationClientRevoke struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is revoked, for the access review." example:"The integration was retired."`
}

type (
	// IntegrationClientPath names an integration client.
	IntegrationClientPath struct {
		IntegrationClientID string `path:"integration_client_id" doc:"The integration client's ID in Fyniq." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d50"`
	}
	integrationClientListOutput struct{ Body IntegrationClientList }
	integrationClientOutput     struct {
		ETag string `header:"ETag" doc:"The client's version, for If-Match."`
		Body IntegrationClient
	}
	integrationClientAskOutput struct {
		Status   int
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     Approval
	}
	integrationClientCreateInput struct {
		KeyHeader
		PreferHeader
		Body IntegrationClientCreate
	}
	integrationClientRevokeInput struct {
		IntegrationClientPath
		KeyHeader
		PreferHeader
		IfMatchHeader
		Body IntegrationClientRevoke
	}
	integrationClientRevokeOutput struct {
		ETag     string `header:"ETag" doc:"The client's version, for If-Match."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     IntegrationClient
	}
)

// IntegrationClientCodes are the refusals of an integration client's definition and steps.
var IntegrationClientCodes = []string{apiclient.CodeName, apiclient.CodeScope, apiclient.CodeNetwork, apiclient.CodeLimit, apiclient.CodeExpiry,
	apiclient.CodeKey, apiclient.CodeOwner, storage.CodeAPIClient}

// clientStatus is a client's status at now.
func clientStatus(c storage.APIClient, now time.Time) string {
	switch {
	case c.RevokedAt != nil:
		return "revoked"
	case !now.Before(c.ExpiresAt):
		return "expired"
	}
	return "live"
}

func integrationClientOut(c storage.APIClient, now time.Time) IntegrationClient {
	out := IntegrationClient{ID: c.ID.String(), ClientID: c.ClientID, Name: c.Name, Class: c.Class, OwnerID: c.OwnerID.String(), Scopes: clientScopes(c.Scopes),
		Networks: clientNetworks(c.IPAllow), ReadsPerMinute: c.ReadsPerMinute, WritesPerMinute: c.WritesPerMinute, KeyThumbprint: c.KeyThumbprint,
		ExpiresAt: c.ExpiresAt, Status: clientStatus(c, now), Version: c.Version, CreatedBy: c.CreatedBy, CreatedAt: c.CreatedAt,
		RevokedAt: c.RevokedAt, RevokedBy: c.RevokedBy, RevokeReason: c.RevokeReason}
	if c.ApprovalID != nil {
		out.ApprovalID = c.ApprovalID.String()
	}
	return out
}

// clientProblem is the domain's refusals as one problem, every field at once (FR-INT-07).
func clientProblem(err error) error {
	var errs apiclient.Errors
	if !errors.As(err, &errs) || len(errs) == 0 {
		var one *apiclient.Error
		if errors.As(err, &one) {
			errs = apiclient.Errors{one}
		} else {
			return err
		}
	}
	fields := make([]FieldProblem, len(errs))
	for i, e := range errs {
		fields[i] = FieldProblem{Code: e.Code, Field: "body." + e.Field, Message: e.Message, Fix: e.Fix}
	}
	return problem(errs[0].Code, errs[0].Field+" "+errs[0].Message+".", errs[0].Fix, fields...)
}

// clientOwner checks that the member a client acts for is an active member with a role now.
func clientOwner(ctx context.Context, u storage.UnitOfWork, owner uuid.UUID, now time.Time) error {
	roles, err := u.Members().Roles(ctx, owner, now)
	if err != nil && !errors.Is(err, ports.ErrNotFound) {
		return err
	}
	if len(roles) == 0 {
		return &apiclient.Error{Code: apiclient.CodeOwner, Field: "owner_id", Message: "is not an active member with a role in this organisation",
			Fix: "Name a member who holds the roles the integration needs"}
	}
	return nil
}

// apiClientDependencies are a client's version and whether it is live: "new" before it exists (FR-INT-02).
func apiClientDependencies(ctx context.Context, u storage.UnitOfWork, key string) (int, map[string]any, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return 0, nil, err
	}
	c, err := u.APIClients().Get(ctx, id)
	if errors.Is(err, ports.ErrNotFound) {
		return 0, map[string]any{"status": "new"}, nil
	}
	if err != nil {
		return 0, nil, err
	}
	status := "live"
	if c.RevokedAt != nil {
		status = "revoked"
	}
	return c.Version, map[string]any{"status": status}, nil
}

func (a *API) integrationClient(ctx context.Context, u storage.UnitOfWork, raw string) (storage.APIClient, error) {
	id, err := parseID("integration_client_id", raw)
	if err != nil {
		return storage.APIClient{}, err
	}
	c, err := u.APIClients().Get(ctx, id)
	if err == nil && c.Kind != storage.APIClientIntegration {
		err = ports.ErrNotFound
	}
	return c, err
}

func registerIntegrationClients(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	versioned := []string{CodeIfMatchRequired, CodeStale, storage.CodeVersion}

	register(a, authz.ListIntegrationClients, huma.Operation{
		OperationID: "listIntegrationClients", Method: http.MethodGet, Path: "/v1/integration-clients", Tags: []string{"Integrations"},
		Summary: "List the organisation's integration clients",
		Description: "The organisation's integration clients (FR-INT-02, SEC-07), oldest first, a page at a time (FR-INT-10): each one's OAuth " +
			"client ID, the member it acts for, its scopes, networks, own rate limits, key thumbprint, expiry and status, revoked ones " +
			"included, for the quarterly access review (FR-CTL-06). Never a key or a token. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*integrationClientListOutput, error) {
		out := IntegrationClientList{Items: []IntegrationClient{}}
		p, _ := authn.From(ctx)
		now := a.deps.Clock.Now()
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			all, err := u.APIClients().List(ctx, storage.APIClientIntegration)
			if err != nil {
				return err
			}
			page, next, err := pageOf(all, func(c storage.APIClient) string { return c.ID.String() }, *in)
			for _, c := range page {
				out.Items = append(out.Items, integrationClientOut(c, now))
			}
			out.NextCursor = next
			return err
		})
		return &integrationClientListOutput{Body: out}, err
	})

	register(a, authz.GetIntegrationClient, huma.Operation{
		OperationID: "getIntegrationClient", Method: http.MethodGet, Path: "/v1/integration-clients/{integration_client_id}",
		Tags: []string{"Integrations"}, Summary: "Get an integration client",
		Description: "One integration client (FR-INT-02) with its ETag for revoking it. Never a key or a token. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *IntegrationClientPath) (*integrationClientOutput, error) {
		var out storage.APIClient
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.integrationClient(ctx, u, in.IntegrationClientID)
			return err
		})
		if err != nil {
			return nil, err
		}
		return &integrationClientOutput{ETag: etag(out.Version, clientStatus(out, a.deps.Clock.Now())), Body: integrationClientOut(out, a.deps.Clock.Now())}, nil
	})

	register(a, authz.CreateIntegrationClient, huma.Operation{
		OperationID: "createIntegrationClient", Method: http.MethodPost, Path: "/v1/integration-clients", Tags: []string{"Integrations"},
		DefaultStatus: http.StatusAccepted, Summary: "Ask to register an integration client",
		Description: "Asks to register an integration client (FR-INT-02, SEC-07, AI-34): a system that obtains its own access tokens from " +
			"Fyniq's OAuth server with the client-credentials grant, authenticating with private_key_jwt (RFC 7523) signed by the key " +
			"whose public half you give; no shared secret exists. It acts for the member you name (you, when you name none) and never " +
			"does more than they may now; its scopes narrow that to whole areas, and no scope approves or releases. Give the networks " +
			"it calls from where you can, and its expiry, within a year. Every field is checked at once. It is dual controlled (golden " +
			"rule 6, FR-INT-02): the answer is the approval request, which the Owner, who holds A on \"Connected apps\", approves; " +
			"only then is the client registered with the OAuth server, and its client ID appears on getIntegrationClient. Side effects: " +
			"the approval request and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks " +
			"nothing. Needs a person signed in with step-up. On the never-list for the AI and MCP (§9.6).",
	}, append(append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired,
		CodeUnavailable, approval.CodePending}, locks...), IntegrationClientCodes...), func(ctx context.Context, in *integrationClientCreateInput) (*integrationClientAskOutput, error) {
		if a.deps.OAuthClients == nil || a.deps.APIAudience == "" {
			return nil, problem(CodeUnavailable, "This installation has no OAuth server configured, so it cannot register an integration client.",
				"Ask the operator to configure the OAuth server (FYNIQ_OAUTH_ADMIN_URL and FYNIQ_API_AUDIENCE).")
		}
		key, err := json.Marshal(in.Body.PublicKey)
		if err != nil || len(in.Body.PublicKey) == 0 {
			key = nil
		}
		now := a.deps.Clock.Now()
		checked, err := apiclient.CheckIntegration(apiclient.Integration{Name: in.Body.Name, Scopes: scopeStrings(in.Body.Scopes), Networks: networkStrings(in.Body.Networks),
			ReadsPerMinute: in.Body.ReadsPerMinute, WritesPerMinute: in.Body.WritesPerMinute, ExpiresAt: in.Body.ExpiresAt, Key: key}, now)
		if err != nil {
			return nil, clientProblem(err)
		}
		public, err := apiclient.PublicJWK(key, checked.Alg)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		owner := p.UserID
		if in.Body.OwnerID != "" {
			if owner, err = parseID("body.owner_id", in.Body.OwnerID); err != nil {
				return nil, err
			}
		}
		id := uuid.Must(uuid.NewV7())
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
			if err := clientOwner(ctx, u, owner, now); err != nil {
				return Approval{}, clientProblem(err)
			}
			change := map[string]any{"id": id.String(), "name": checked.Name, "owner_id": owner.String(), "scopes": anyList(checked.Scopes),
				"networks": anyList(checked.Networks), "reads_per_minute": checked.ReadsPerMinute, "writes_per_minute": checked.WritesPerMinute,
				"expires_at": checked.ExpiresAt.UTC().Format(time.RFC3339), "public_key": string(public), "alg": checked.Alg,
				"key_thumbprint": checked.Thumbprint}
			diff := []ApprovalDiff{{Field: "name", After: checked.Name}, {Field: "owner_id", After: owner.String()},
				{Field: "scopes", After: fmt.Sprint(checked.Scopes)}, {Field: "networks", After: fmt.Sprint(checked.Networks)},
				{Field: "expires_at", After: checked.ExpiresAt.UTC().Format(time.RFC3339)}, {Field: "key_thumbprint", After: checked.Thumbprint}}
			return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAPIClient, ObjectKey: id.String(),
				Operation: approval.OpAPIClientCreate, Summary: "Register the integration client " + checked.Name, Execute: change, Diff: diff})
		})
		if err != nil {
			return nil, err
		}
		return &integrationClientAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
	})

	register(a, authz.RevokeIntegrationClient, huma.Operation{
		OperationID: "revokeIntegrationClient", Method: http.MethodPost, Path: "/v1/integration-clients/{integration_client_id}/revoke",
		Tags: []string{"Integrations"}, Summary: "Revoke an integration client",
		Description: "Revokes an integration client at once (FR-INT-02, SEC-07): never held back for a second person. The API refuses its " +
			"tokens from now, within the 30 seconds an introspection is trusted (AI-38), and the OAuth server then deletes the client and " +
			"every token it was issued. It is kept, revoked, for the access review; it is never live again. Side effects: its new " +
			"version, the audit event api_client.revoked with the reason, and the deletion queued for the worker. Needs If-Match with its " +
			"ETag; idempotent with Idempotency-Key; Prefer: return=preview shows the result and saves nothing. On the never-list for the " +
			"AI and MCP (§9.6).",
	}, append(append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, apiclient.CodeState,
		storage.CodeAPIClient}, versioned...), locks...), func(ctx context.Context, in *integrationClientRevokeInput) (*integrationClientRevokeOutput, error) {
		resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (storage.APIClient, error) {
			c, err := a.integrationClient(ctx, u, in.IntegrationClientID)
			if err != nil {
				return c, err
			}
			now := a.deps.Clock.Now()
			if err := checkIfMatch(in.IfMatch, etag(c.Version, clientStatus(c, now))); err != nil {
				return c, err
			}
			if c.RevokedAt != nil {
				return c, problem(apiclient.CodeState, "The integration client is revoked already.", "Nothing to do")
			}
			out, err := u.APIClients().Revoke(ctx, c.ID, c.Version, uploader(ctx), in.Body.Reason, now)
			if err != nil {
				return out, err
			}
			return out, a.removeOAuthClient(ctx, u, c.ClientID)
		})
		if err != nil {
			return nil, err
		}
		now := a.deps.Clock.Now()
		return &integrationClientRevokeOutput{ETag: etag(resp.Body.Version, clientStatus(resp.Body, now)), Replayed: replayedHeader(resp.Replayed),
			Applied: applied(resp), Body: integrationClientOut(resp.Body, now)}, nil
	})
}

// removeOAuthClient has the worker delete a client and its tokens from the OAuth server once this change commits.
func (a *API) removeOAuthClient(ctx context.Context, u storage.UnitOfWork, clientID string) error {
	item, err := revoke.RemoveClientItem(clientID)
	if err != nil {
		return err
	}
	if a.deps.Outbox != nil {
		p, _ := authn.From(ctx)
		_, err = a.deps.Outbox.Enqueue(ctx, u, p.Organisation, item)
		return err
	}
	_, err = u.Outbox().Enqueue(ctx, item) // no dispatcher here: the tick sends it
	return err
}

// executeAPIClient runs an approved request on an API client (FR-INT-02), as its requester asked, after checking it once
// more: the integration client is registered with the OAuth server, then recorded. A preview (the approval's impact)
// registers nothing; a unit of work that rolls back removes what it registered, and a client the OAuth server holds but
// Fyniq never recorded gets nothing from the API.
func (a *API) executeAPIClient(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	if r.Operation != approval.OpAPIClientCreate {
		return fmt.Errorf("api client: no operation %q yet", r.Operation)
	}
	str := func(k string) string { v, _ := s.Execute[k].(string); return v }
	num := func(k string) int {
		switch v := s.Execute[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case json.Number:
			n, _ := v.Int64()
			return int(n)
		}
		return 0
	}
	list := func(k string) []string {
		out := []string{}
		if vs, ok := s.Execute[k].([]any); ok {
			for _, v := range vs {
				out = append(out, fmt.Sprint(v))
			}
		}
		return out
	}
	id, err := uuid.Parse(str("id"))
	if err != nil {
		return err
	}
	owner, err := uuid.Parse(str("owner_id"))
	if err != nil {
		return err
	}
	expires, err := time.Parse(time.RFC3339, str("expires_at"))
	if err != nil {
		return err
	}
	now := a.deps.Clock.Now()
	checked, err := apiclient.CheckIntegration(apiclient.Integration{Name: str("name"), Scopes: list("scopes"), Networks: list("networks"),
		ReadsPerMinute: num("reads_per_minute"), WritesPerMinute: num("writes_per_minute"), ExpiresAt: expires, Key: json.RawMessage(str("public_key"))}, now)
	if err != nil {
		return clientProblem(err)
	}
	if checked.Thumbprint != str("key_thumbprint") {
		return errors.New("api client: the key is not the one asked for")
	}
	if err := clientOwner(ctx, u, owner, now); err != nil {
		return clientProblem(err)
	}
	if a.deps.OAuthClients == nil || a.deps.APIAudience == "" {
		return problem(CodeUnavailable, "This installation has no OAuth server configured, so it cannot register an integration client.",
			"Ask the operator to configure the OAuth server (FYNIQ_OAUTH_ADMIN_URL and FYNIQ_API_AUDIENCE).")
	}
	org, err := u.Organisation(ctx)
	if err != nil {
		return err
	}
	clientID := "preview-" + id.String()
	if !storage.IsPreview(ctx) {
		clientID, err = a.deps.OAuthClients.RegisterIntegration(ctx, ports.IntegrationRegistration{Name: checked.Name, Class: authn.ClassIntegration,
			Audience: a.deps.APIAudience, Scopes: checked.Scopes, Key: json.RawMessage(str("public_key")), Alg: checked.Alg,
			TokenLifespan: IntegrationClientTokenLifespan, Organisation: org.ID.String()})
		if err != nil {
			return fmt.Errorf("register the integration client with the OAuth server: %w", err)
		}
		registered := clientID
		u.AfterRollback(func(ctx context.Context) { _ = a.deps.OAuthClients.RemoveClient(ctx, registered) })
	}
	_, err = u.APIClients().Create(ctx, storage.APIClient{ID: id, Kind: storage.APIClientIntegration, ClientID: clientID, Name: checked.Name,
		Class: authn.ClassIntegration, OwnerID: owner, Scopes: checked.Scopes, IPAllow: checked.Networks, ReadsPerMinute: checked.ReadsPerMinute,
		WritesPerMinute: checked.WritesPerMinute, KeyThumbprint: checked.Thumbprint, ExpiresAt: checked.ExpiresAt, ApprovalID: &r.ID,
		CreatedBy: "user:" + r.RequestedBy.String(), CreatedAt: now})
	return err
}

// anyList is a list of strings as an envelope holds it.
func anyList(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range slices.Clone(ss) {
		out[i] = s
	}
	return out
}
