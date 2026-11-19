package api

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/apiclient"
	"github.com/cognizhi/fyniq/internal/nemura/authz"
)

// Client is an API client as the API shows it. The key it signs in with is never shown again.
type Client struct {
	ID       string   `json:"id" format:"uuid" doc:"The client's ID in Nemura." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e60"`
	Name     string   `json:"name" doc:"The system that calls." example:"Payroll bureau"`
	Scopes   []string `json:"scopes" doc:"What its tokens may do." example:"[\"directory.read\"]"`
	ClientID string   `json:"client_id" doc:"The OAuth client ID the system signs in with at the token endpoint." example:"4f6c1b52-8d2a-4c93-9a43-0c2d7e9b1a11"`
	Audience string   `json:"audience" doc:"The audience its tokens name: the API's." example:"nemura-api"`
}

// APIClients registers, lists and revokes the organisation's API clients with Nemura's own OAuth server.
type APIClients interface {
	Register(ctx context.Context, s Session, name string, scopes []string, alg string, key json.RawMessage) (Client, error)
	List(ctx context.Context, s Session) ([]Client, error)
	Revoke(ctx context.Context, s Session, id uuid.UUID) error
}

// ErrNoClient is a client the organisation does not have.
var ErrNoClient = errors.New("no such API client")

// ErrUnavailable is returned when the deployment has no OAuth server configured for clients.
var ErrUnavailable = errors.New("API clients are not set up in this deployment")

// IdempotencyExempt names the writes of this file too: a name is unique among live clients, so a repeated registration is refused, and revoking is naturally repeatable.
func init() {
}

type clientsInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}

type createClientInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Name      string         `json:"name" minLength:"1" maxLength:"100" doc:"The system that will call Nemura." example:"Payroll bureau"`
		Scopes    []string       `json:"scopes" minItems:"1" doc:"What its tokens may do, from GET /v1/api-clients/scopes." example:"[\"directory.read\"]"`
		Algorithm string         `json:"algorithm" doc:"The algorithm its assertions are signed with: ES256, RS256 or EdDSA." example:"ES256"`
		PublicKey map[string]any `json:"public_key" doc:"Its public key as a JWK (RFC 7517), never the private key."`
	}
}

type clientOutput struct{ Body Client }

type clientsOutput struct {
	Body struct {
		Clients []Client `json:"clients" doc:"The organisation's live API clients."`
	}
}

type revokeInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	ID     string `path:"id" format:"uuid" doc:"The client to revoke." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e60"`
}

// owner checks that the person is the Owner and, for a change, confirmed it is them with a passkey in the last five minutes.
func (a *API) owner(ctx context.Context, cookie string, change bool) (Session, error) {
	s, err := a.signedIn(ctx, cookie)
	if err != nil {
		return Session{}, err
	}
	now := a.d.Clock.Now()
	roles, err := a.d.Sessions.RolesAt(ctx, s.Organisation, s.Member, now)
	if err != nil {
		return Session{}, huma.Error500InternalServerError("The roles could not be read.")
	}
	if !slices.Contains(roles, authz.Owner) {
		return Session{}, Err(CodeRoleRequired, "Only the Owner manages API clients.")
	}
	if change && !s.Fresh(now) {
		return Session{}, Err(CodeStepUp, "Registering or revoking an API client needs a passkey confirmation in the last five minutes.")
	}
	return s, nil
}

func clientError(err error) error {
	var r *apiclient.Refusal
	switch {
	case errors.As(err, &r) && r.Code == apiclient.CodeExists:
		return Err(CodeClientExists, r.Message)
	case errors.As(err, &r):
		return Err(CodeClientInvalid, r.Message)
	case errors.Is(err, ErrUnavailable):
		return Err(CodeUnavailable, "API clients are not set up in this deployment: it has no OAuth server configured for them.")
	case errors.Is(err, ErrNoClient):
		return Err(CodeNotFound, "There is no such API client in this organisation.")
	}
	return huma.Error500InternalServerError("The API client could not be saved.")
}

func (a *API) registerClients() {
	huma.Register(a.API, huma.Operation{
		OperationID: "createApiClient", Method: "POST", Path: "/v1/api-clients", Tags: []string{"API clients"}, Summary: "Register an API client",
		Description: "Registers a system that calls Nemura's API on its own with the client-credentials grant, signing in with the key pair it holds (private_key_jwt), for the API's audience and the scopes given. Only the Owner, confirmed with a passkey in the last five minutes. It is audited; the private key never reaches Nemura.",
		Errors:      []int{401, 403, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *createClientInput) (*clientOutput, error) {
		s, err := a.owner(ctx, in.Cookie, true)
		if err != nil {
			return nil, err
		}
		key, err := json.Marshal(in.Body.PublicKey)
		if err != nil {
			return nil, clientError(&apiclient.Refusal{Code: apiclient.CodeInvalid, Message: "The public key is not JSON."})
		}
		c, err := a.d.Clients.Register(ctx, s, in.Body.Name, in.Body.Scopes, in.Body.Algorithm, key)
		if err != nil {
			return nil, clientError(err)
		}
		return &clientOutput{Body: c}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "listApiClients", Method: "GET", Path: "/v1/api-clients", Tags: []string{"API clients"}, Summary: "List the API clients",
		Description: "Lists the organisation's live API clients: name, scopes and OAuth client ID, never a key. Only the Owner. It changes nothing.",
		Errors:      []int{401, 403},
	}, func(ctx context.Context, in *clientsInput) (*clientsOutput, error) {
		s, err := a.owner(ctx, in.Cookie, false)
		if err != nil {
			return nil, err
		}
		cs, err := a.d.Clients.List(ctx, s)
		if err != nil {
			return nil, clientError(err)
		}
		out := &clientsOutput{}
		out.Body.Clients = cs
		if out.Body.Clients == nil {
			out.Body.Clients = []Client{}
		}
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "revokeApiClient", Method: "DELETE", Path: "/v1/api-clients/{id}", Tags: []string{"API clients"}, Summary: "Revoke an API client",
		Description: "Revokes a client: its tokens are deleted and it can sign in no more. Only the Owner, confirmed with a passkey in the last five minutes. It is audited. A client that is not registered is NM_NOT_FOUND.",
		Errors:      []int{401, 403, 404}, DefaultStatus: 204,
	}, func(ctx context.Context, in *revokeInput) (*struct{}, error) {
		s, err := a.owner(ctx, in.Cookie, true)
		if err != nil {
			return nil, err
		}
		id, err := uuid.Parse(in.ID)
		if err != nil {
			return nil, Err(CodeNotFound, "There is no such API client in this organisation.")
		}
		if err := a.d.Clients.Revoke(ctx, s, id); err != nil {
			return nil, clientError(err)
		}
		return nil, nil
	})
}
