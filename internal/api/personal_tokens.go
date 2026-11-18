package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/apiclient"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ personal access tokens (FR-INT-02, AI-34)
//
// A personal access token is a developer's own credential for the API (FR-INT-02): a client of the OAuth server (Ory Hydra,
// ADR-T3) whose secret the server generates, returns once and keeps only as a hash, so Fyniq never holds it. The
// developer exchanges client ID and secret at the OAuth server's token endpoint with the client-credentials grant for
// access tokens of an hour (AI-38) for the API's audience. It acts for the member who made it, never beyond that member's
// roles, narrowed to its scopes and networks; it expires within 90 days; and because a token may be pasted into an AI
// tool, its class is `agent` until the Owner reclasses it as `integration` with a second approver: an agent only reads
// and proposes (AI-34), whatever its scopes. A member makes, lists and revokes their own; an Administrator lists and
// revokes any, and asks to reclass one. Only a person signed in to Fyniq and stepped up makes one: no token, integration
// or AI makes another credential. Revoking is never held back, and the worker deletes the client from the OAuth server.

// DefaultPersonalAccessTokenDays is how long a personal access token lives when its request names no expiry: shorter than
// the 90 days FR-INT-02 allows, since less is safer.
const DefaultPersonalAccessTokenDays = 30

// PersonalAccessToken is a personal access token as Fyniq holds it: never its secret.
type PersonalAccessToken struct {
	ID              string          `json:"id" format:"uuid" example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d50"`
	ClientID        string          `json:"client_id" doc:"The OAuth client ID: the client_id of its token requests (client_credentials grant)." example:"6f1c2b9e-3d4a-4c5b-8e7f-0a1b2c3d4e5f"`
	Name            string          `json:"name" doc:"Its name, for its owner." example:"Claude Code on my laptop"`
	Class           string          `json:"class" enum:"agent,integration" doc:"Its client class (AI-34): agent until the Owner reclasses it as integration with a second approver. An agent only reads and proposes." example:"agent"`
	OwnerID         string          `json:"owner_id" format:"uuid" doc:"The member it acts for: it never does more than this member may do now." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	Scopes          []ClientScope   `json:"scopes" nullable:"false" doc:"Its narrow scopes: fyniq.read.<area> views an area, fyniq.write.<area> also prepares and posts in it once it is an integration. No scope approves or releases."`
	Networks        []ClientNetwork `json:"networks" nullable:"false" doc:"The networks it may call from, as CIDR prefixes; none: anywhere."`
	ReadsPerMinute  int             `json:"reads_per_minute,omitempty" doc:"Its own limit of reads a minute; none: the platform's 600." example:"60"`
	WritesPerMinute int             `json:"writes_per_minute,omitempty" doc:"Its own limit of writes a minute; none: the platform's 120." example:"30"`
	ExpiresAt       time.Time       `json:"expires_at" doc:"When it stops: within 90 days of its creation." example:"2026-12-17T00:00:00Z"`
	Status          string          `json:"status" enum:"live,expired,revoked" doc:"live until it expires or is revoked." example:"live"`
	Version         int             `json:"version" example:"1"`
	CreatedAt       time.Time       `json:"created_at" example:"2026-11-17T09:00:00Z"`
	RevokedAt       *time.Time      `json:"revoked_at,omitempty" example:"2026-12-01T09:00:00Z"`
	RevokedBy       string          `json:"revoked_by,omitempty" example:"user:01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	RevokeReason    string          `json:"revoke_reason,omitempty" example:"The laptop was lost."`
}

// PersonalAccessTokenCreated is a new personal access token with its secret, shown this once.
type PersonalAccessTokenCreated struct {
	PersonalAccessToken
	ClientSecret string `json:"client_secret" doc:"The secret that, with the client_id, obtains access tokens. Shown once and never again: Fyniq does not keep it and the OAuth server keeps only its hash. Store it as you would a password." example:"7Uv3uG2fT0y1-q9ZQ6c5pE8mJxA4rDkW"`
	Audience     string `json:"audience" doc:"The audience to ask the OAuth server for: its access tokens are accepted by the API for this audience only." example:"https://api.fyniq.example"`
	TokenLife    int    `json:"access_token_seconds" doc:"How long each access token it obtains lives, in seconds (AI-38)." example:"3600"`
}

// PersonalAccessTokenList is a page of personal access tokens.
type PersonalAccessTokenList struct {
	Items      []PersonalAccessToken `json:"items" nullable:"false" doc:"The tokens, oldest first."`
	NextCursor string                `json:"next_cursor,omitempty" doc:"The cursor of the next page; none on the last." example:"eyJhZnRlciI6IjEwMDAifQ"`
}

// PersonalAccessTokenCreate asks for a new personal access token.
type PersonalAccessTokenCreate struct {
	Name            string          `json:"name" minLength:"1" maxLength:"80" doc:"Its name, to tell your tokens apart." example:"Claude Code on my laptop"`
	Scopes          []ClientScope   `json:"scopes" minItems:"1" maxItems:"16" doc:"Its narrow scopes, only what it needs: fyniq.read.<area> (reports, sales, purchases, banking, accounting, planning, compliance) or fyniq.write.<area> (sales, purchases, banking, accounting). It never does more than you may."`
	Networks        []ClientNetwork `json:"networks,omitempty" maxItems:"32" doc:"The networks it may call from, as CIDR prefixes; none: anywhere. Give them where you can (SEC-07)."`
	ReadsPerMinute  int             `json:"reads_per_minute,omitempty" minimum:"0" maximum:"600" doc:"Its own limit of reads a minute, at most the platform's 600; none: the platform's." example:"60"`
	WritesPerMinute int             `json:"writes_per_minute,omitempty" minimum:"0" maximum:"120" doc:"Its own limit of writes a minute, at most the platform's 120; none: the platform's." example:"30"`
	ExpiresAt       *time.Time      `json:"expires_at,omitempty" doc:"When it stops, within 90 days; 30 days from now when left out." example:"2026-12-17T00:00:00Z"`
}

// PersonalAccessTokenRevoke ends a personal access token.
type PersonalAccessTokenRevoke struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is revoked, for the access review." example:"The laptop was lost."`
}

// PersonalAccessTokenReclass asks to reclass a personal access token as an integration.
type PersonalAccessTokenReclass struct {
	Reason string `json:"reason" minLength:"1" maxLength:"500" doc:"Why it should be an integration client: the Owner and a second approver read it." example:"It feeds the nightly import."`
}

type (
	// PersonalAccessTokenPath names a personal access token.
	PersonalAccessTokenPath struct {
		PersonalAccessTokenID string `path:"personal_access_token_id" doc:"The personal access token's ID in Fyniq." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d50"`
	}
	personalAccessTokenCreateInput struct {
		Body PersonalAccessTokenCreate
	}
	personalAccessTokenCreateOutput struct {
		CacheControl string `header:"Cache-Control" doc:"no-store: the answer carries a secret."`
		Body         PersonalAccessTokenCreated
	}
	personalAccessTokenListOutput  struct{ Body PersonalAccessTokenList }
	personalAccessTokenRevokeInput struct {
		PersonalAccessTokenPath
		KeyHeader
		PreferHeader
		Body PersonalAccessTokenRevoke
	}
	personalAccessTokenRevokeOutput struct {
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     PersonalAccessToken
	}
	personalAccessTokenReclassInput struct {
		PersonalAccessTokenPath
		KeyHeader
		PreferHeader
		Body PersonalAccessTokenReclass
	}
)

// PersonalAccessTokenCodes are the refusals of a personal access token's definition and steps.
var PersonalAccessTokenCodes = []string{apiclient.CodeName, apiclient.CodeScope, apiclient.CodeNetwork, apiclient.CodeLimit, apiclient.CodeExpiry,
	apiclient.CodeOwner, storage.CodeAPIClient}

func personalAccessTokenOut(c storage.APIClient, now time.Time) PersonalAccessToken {
	return PersonalAccessToken{ID: c.ID.String(), ClientID: c.ClientID, Name: c.Name, Class: c.Class, OwnerID: c.OwnerID.String(), Scopes: clientScopes(c.Scopes),
		Networks: clientNetworks(c.IPAllow), ReadsPerMinute: c.ReadsPerMinute, WritesPerMinute: c.WritesPerMinute, ExpiresAt: c.ExpiresAt,
		Status: clientStatus(c, now), Version: c.Version, CreatedAt: c.CreatedAt, RevokedAt: c.RevokedAt, RevokedBy: c.RevokedBy, RevokeReason: c.RevokeReason}
}

// personalToken reads a personal access token by its ID in the request; not found for any other kind of client, or, when
// owner is set, for another member's.
func (a *API) personalToken(ctx context.Context, u storage.UnitOfWork, raw string, owner uuid.UUID) (storage.APIClient, error) {
	id, err := parseID("personal_access_token_id", raw)
	if err != nil {
		return storage.APIClient{}, err
	}
	c, err := u.APIClients().Get(ctx, id)
	if err == nil && (c.Kind != storage.APIClientPersonal || (owner != uuid.Nil && c.OwnerID != owner)) {
		err = ports.ErrNotFound
	}
	return c, err
}

// personalTokens lists the personal access tokens of the organisation, or only owner's, a page at a time.
func (a *API) personalTokens(ctx context.Context, in PageQuery, owner uuid.UUID) (*personalAccessTokenListOutput, error) {
	out := PersonalAccessTokenList{Items: []PersonalAccessToken{}}
	p, _ := authn.From(ctx)
	now := a.deps.Clock.Now()
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.APIClients().List(ctx, storage.APIClientPersonal)
		if err != nil {
			return err
		}
		mine := all[:0:0]
		for _, c := range all {
			if owner == uuid.Nil || c.OwnerID == owner {
				mine = append(mine, c)
			}
		}
		page, next, err := pageOf(mine, func(c storage.APIClient) string { return c.ID.String() }, in)
		for _, c := range page {
			out.Items = append(out.Items, personalAccessTokenOut(c, now))
		}
		out.NextCursor = next
		return err
	})
	return &personalAccessTokenListOutput{Body: out}, err
}

// revokePersonalToken revokes a personal access token at once: owner is set when it must be that member's own.
func (a *API) revokePersonalToken(ctx context.Context, in *personalAccessTokenRevokeInput, owner uuid.UUID) (*personalAccessTokenRevokeOutput, error) {
	resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (storage.APIClient, error) {
		c, err := a.personalToken(ctx, u, in.PersonalAccessTokenID, owner)
		if err != nil {
			return c, err
		}
		if c.RevokedAt != nil {
			return c, problem(apiclient.CodeState, "The personal access token is revoked already.", "Nothing to do")
		}
		out, err := u.APIClients().Revoke(ctx, c.ID, c.Version, uploader(ctx), in.Body.Reason, a.deps.Clock.Now())
		if err != nil {
			return out, err
		}
		return out, a.removeOAuthClient(ctx, u, c.ClientID)
	})
	if err != nil {
		return nil, err
	}
	return &personalAccessTokenRevokeOutput{Replayed: replayedHeader(resp.Replayed), Applied: applied(resp),
		Body: personalAccessTokenOut(resp.Body, a.deps.Clock.Now())}, nil
}

func registerPersonalTokens(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	revokeCodes := append(append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, apiclient.CodeState,
		storage.CodeAPIClient}, locks...), CodeMalformed)

	register(a, authz.CreatePersonalAccessToken, oneTimeSecret(huma.Operation{
		OperationID: "createPersonalAccessToken", Method: http.MethodPost, Path: "/v1/personal-access-tokens", Tags: []string{"Integrations"},
		DefaultStatus: http.StatusCreated, Summary: "Create a personal access token",
		Description: "Creates your own personal access token (FR-INT-02, AI-34): a client of Fyniq's OAuth server whose secret is generated there, " +
			"shown to you once in this answer and kept only as a hash; Fyniq never holds it. With the client_id and the secret you ask the OAuth " +
			"server's token endpoint for access tokens (grant_type=client_credentials, the client_id and secret as HTTP Basic credentials, and " +
			"the audience returned here); each lives an hour. It acts for you and never does more than you may now; its scopes narrow that to " +
			"whole areas, and no scope approves or releases. It expires within 90 days (30 when you name no expiry), and give its networks where " +
			"you can. Its class is `agent` because a token may be pasted into an AI tool: an agent reads and proposes and cannot write, " +
			"whatever its scopes, until the Owner reclasses it as an integration with a second approver (requestPersonalAccessTokenReclass). " +
			"Every field is checked at once. Side effects: the OAuth client and its record, and the audit event api_client.registered (never " +
			"the secret). Needs a person signed in to Fyniq with step-up: an OAuth client, a token or the AI is refused, so no credential " +
			"makes another. It takes no Idempotency-Key and no preview: a stored answer would keep the secret at rest. On the never-list " +
			"for the AI and MCP (§9.6).",
	}), append(append([]string{CodeSessionRequired, CodeStepUpRequired, CodeUnavailable, CodeMalformed, CodeTooLarge, CodeMediaType}, locks...), PersonalAccessTokenCodes...),
		func(ctx context.Context, in *personalAccessTokenCreateInput) (*personalAccessTokenCreateOutput, error) {
			p, err := signedIn(ctx, "Creating a personal access token")
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			if !p.SteppedUp(now) {
				return nil, problem(CodeStepUpRequired, "Creating a personal access token needs step-up authentication in the last 5 minutes (FR-INT-02).",
					"Confirm it's you with your passkey or authenticator app, then try again.")
			}
			if a.deps.OAuthClients == nil || a.deps.APIAudience == "" {
				return nil, problem(CodeUnavailable, "This installation has no OAuth server configured, so it cannot issue a personal access token.",
					"Ask the operator to configure the OAuth server (FYNIQ_OAUTH_ADMIN_URL and FYNIQ_API_AUDIENCE).")
			}
			expires := now.AddDate(0, 0, DefaultPersonalAccessTokenDays)
			if in.Body.ExpiresAt != nil {
				expires = *in.Body.ExpiresAt
			}
			checked, err := apiclient.CheckPersonal(apiclient.Personal{Name: in.Body.Name, Scopes: scopeStrings(in.Body.Scopes), Networks: networkStrings(in.Body.Networks),
				ReadsPerMinute: in.Body.ReadsPerMinute, WritesPerMinute: in.Body.WritesPerMinute, ExpiresAt: expires}, now)
			if err != nil {
				return nil, clientProblem(err)
			}
			var out PersonalAccessTokenCreated
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				if err := clientOwner(ctx, u, p.UserID, now); err != nil { // the member's roles now: a token never exceeds them
					return clientProblem(err)
				}
				org, err := u.Organisation(ctx)
				if err != nil {
					return err
				}
				clientID, secret, err := a.deps.OAuthClients.RegisterPersonal(ctx, ports.PersonalRegistration{Name: checked.Name, Class: authn.ClassAgent,
					Audience: a.deps.APIAudience, Scopes: checked.Scopes, TokenLifespan: IntegrationClientTokenLifespan, Organisation: org.ID.String()})
				if err != nil {
					return fmt.Errorf("register the personal access token with the OAuth server: %w", err)
				}
				u.AfterRollback(func(ctx context.Context) { _ = a.deps.OAuthClients.RemoveClient(ctx, clientID) })
				c, err := u.APIClients().Create(ctx, storage.APIClient{ID: uuid.Must(uuid.NewV7()), Kind: storage.APIClientPersonal, ClientID: clientID,
					Name: checked.Name, Class: authn.ClassAgent, OwnerID: p.UserID, Scopes: checked.Scopes, IPAllow: checked.Networks,
					ReadsPerMinute: checked.ReadsPerMinute, WritesPerMinute: checked.WritesPerMinute, ExpiresAt: checked.ExpiresAt,
					CreatedBy: uploader(ctx), CreatedAt: now})
				if err != nil {
					return err
				}
				out = PersonalAccessTokenCreated{PersonalAccessToken: personalAccessTokenOut(c, now), ClientSecret: secret, Audience: a.deps.APIAudience,
					TokenLife: int(IntegrationClientTokenLifespan / time.Second)}
				return nil
			})
			if err != nil {
				return nil, err
			}
			return &personalAccessTokenCreateOutput{CacheControl: "no-store", Body: out}, nil
		})

	register(a, authz.ListMyPersonalAccessTokens, huma.Operation{
		OperationID: "listMyPersonalAccessTokens", Method: http.MethodGet, Path: "/v1/personal-access-tokens/mine", Tags: []string{"Integrations"},
		Summary: "List your personal access tokens",
		Description: "Your own personal access tokens (FR-INT-02), oldest first, a page at a time (FR-INT-10): each one's client ID, class, scopes, " +
			"networks, own rate limits, expiry and status, revoked ones included. Never a secret. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*personalAccessTokenListOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil {
			return &personalAccessTokenListOutput{Body: PersonalAccessTokenList{Items: []PersonalAccessToken{}}}, nil
		}
		return a.personalTokens(ctx, *in, p.UserID)
	})

	register(a, authz.ListPersonalAccessTokens, huma.Operation{
		OperationID: "listPersonalAccessTokens", Method: http.MethodGet, Path: "/v1/personal-access-tokens", Tags: []string{"Integrations"},
		Summary: "List the organisation's personal access tokens",
		Description: "Every member's personal access tokens (FR-INT-02, SEC-07), oldest first, a page at a time (FR-INT-10): each one's client ID, the " +
			"member it acts for, its class, scopes, networks, expiry and status, revoked ones included, for the quarterly access review (FR-CTL-06). " +
			"Never a secret. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *PageQuery) (*personalAccessTokenListOutput, error) {
		return a.personalTokens(ctx, *in, uuid.Nil)
	})

	register(a, authz.RevokeMyPersonalAccessToken, huma.Operation{
		OperationID: "revokeMyPersonalAccessToken", Method: http.MethodPost, Path: "/v1/personal-access-tokens/mine/{personal_access_token_id}/revoke",
		Tags: []string{"Integrations"}, Summary: "Revoke your personal access token",
		Description: "Revokes one of your own personal access tokens at once (FR-INT-02, SEC-07): never held back. The API refuses its tokens from now, " +
			"within the 30 seconds an introspection is trusted (AI-38), and the OAuth server then deletes the client and every token it obtained. " +
			"It is kept, revoked, for the access review. Side effects: its new version, the audit event api_client.revoked with the reason, and the " +
			"deletion queued for the worker. Another member's token is not found. Idempotent with Idempotency-Key; Prefer: return=preview shows " +
			"the result and saves nothing. On the never-list for the AI and MCP (§9.6).",
	}, revokeCodes, func(ctx context.Context, in *personalAccessTokenRevokeInput) (*personalAccessTokenRevokeOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil {
			return nil, ports.ErrNotFound
		}
		return a.revokePersonalToken(ctx, in, p.UserID)
	})

	register(a, authz.RevokePersonalAccessToken, huma.Operation{
		OperationID: "revokePersonalAccessToken", Method: http.MethodPost, Path: "/v1/personal-access-tokens/{personal_access_token_id}/revoke",
		Tags: []string{"Integrations"}, Summary: "Revoke a member's personal access token",
		Description: "Revokes any member's personal access token at once (FR-INT-02, SEC-07): never held back for a second person, for a lost laptop or " +
			"a member who left. The API refuses its tokens from now, within the 30 seconds an introspection is trusted (AI-38), and the OAuth server " +
			"then deletes the client and every token it obtained. It is kept, revoked, for the access review. Side effects: its new version, the " +
			"audit event api_client.revoked with the reason, and the deletion queued for the worker. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview shows the result and saves nothing. On the never-list for the AI and MCP (§9.6).",
	}, revokeCodes, func(ctx context.Context, in *personalAccessTokenRevokeInput) (*personalAccessTokenRevokeOutput, error) {
		return a.revokePersonalToken(ctx, in, uuid.Nil)
	})

	register(a, authz.RequestPersonalAccessTokenReclass, huma.Operation{
		OperationID: "requestPersonalAccessTokenReclass", Method: http.MethodPost, Path: "/v1/personal-access-tokens/{personal_access_token_id}/reclass",
		Tags: []string{"Integrations"}, DefaultStatus: http.StatusAccepted, Summary: "Ask to reclass a personal access token as an integration",
		Description: "Asks to reclass a live personal access token from `agent` to `integration` (FR-INT-02, AI-34): from then on it may prepare and post " +
			"in its write scopes' areas under the approval policies, as an integration client may, instead of only reading and proposing. It is dual " +
			"controlled (golden rule 6): the answer is the approval request, which the Owner, who holds A on \"Connected apps\", approves with a second " +
			"approver; the token is unchanged until then, and the approval lapses if the token is revoked first. Side effects: the approval request " +
			"and its audit event. Idempotent with Idempotency-Key; Prefer: return=preview checks it and asks nothing. Needs a person signed in " +
			"with step-up. On the never-list for the AI and MCP (§9.6).",
	}, append(append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeSessionRequired, CodeStepUpRequired,
		CodeUnavailable, approval.CodePending, apiclient.CodeState, storage.CodeAPIClient}, locks...), CodeMalformed),
		func(ctx context.Context, in *personalAccessTokenReclassInput) (*integrationClientAskOutput, error) {
			resp, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusAccepted, func(ctx context.Context, u storage.UnitOfWork) (Approval, error) {
				c, err := a.personalToken(ctx, u, in.PersonalAccessTokenID, uuid.Nil)
				if err != nil {
					return Approval{}, err
				}
				if err := personalReclassable(c, a.deps.Clock.Now()); err != nil {
					return Approval{}, err
				}
				change := map[string]any{"id": c.ID.String(), "from": authn.ClassAgent, "to": authn.ClassIntegration, "reason": in.Body.Reason}
				diff := []ApprovalDiff{{Field: "class", Before: authn.ClassAgent, After: authn.ClassIntegration}, {Field: "scopes", After: fmt.Sprint(c.Scopes)},
					{Field: "owner_id", After: c.OwnerID.String()}, {Field: "expires_at", After: c.ExpiresAt.UTC().Format(time.RFC3339)}}
				return a.requestApproval(ctx, u, pendingChange{ObjectType: approval.ObjectAPIClient, ObjectKey: c.ID.String(),
					Operation: approval.OpAPIClientReclass, Summary: "Reclass the personal access token " + c.Name + " as an integration client", Execute: change, Diff: diff})
			})
			if err != nil {
				return nil, err
			}
			return &integrationClientAskOutput{Status: resp.Status, Replayed: replayedHeader(resp.Replayed), Applied: applied(resp), Body: resp.Body}, nil
		})
}

// personalReclassable refuses what cannot be reclassed: a client revoked or expired, or one that is an integration already.
func personalReclassable(c storage.APIClient, now time.Time) error {
	switch {
	case !c.Live(now):
		return problem(apiclient.CodeState, "The personal access token is revoked or expired.", "Make a new token instead")
	case c.Class != authn.ClassAgent:
		return problem(apiclient.CodeState, "The personal access token is an integration client already.", "Nothing to do")
	}
	return nil
}

// executePersonalReclass runs an approved reclass of a personal access token (FR-INT-02), after checking it once more:
// the token must still be live and of class agent, and its version the one that was asked about (the approval lapses when
// it moved on).
func (a *API) executePersonalReclass(ctx context.Context, u storage.UnitOfWork, r storage.ApprovalRequest, s stored) error {
	raw, _ := s.Execute["id"].(string)
	id, err := uuid.Parse(raw)
	if err != nil {
		return err
	}
	c, err := u.APIClients().Get(ctx, id)
	if err != nil {
		return err
	}
	if c.Kind != storage.APIClientPersonal {
		return errors.New("api client: only a personal access token is reclassed")
	}
	if err := personalReclassable(c, a.deps.Clock.Now()); err != nil {
		return err
	}
	_, err = u.APIClients().Reclass(ctx, c.ID, c.Version, authn.ClassIntegration)
	return err
}
