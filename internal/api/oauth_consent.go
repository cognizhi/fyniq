package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/oauthflow"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The consent screen's two steps as operations of the API's contract (PRD-001 AI-37; p1-mcp-first-party task 9.3): the web app shows
// what an app asks for and records the person's answer, with their own first-party session, and never a token. The OAuth server sends
// the browser to the web app's consent page, which calls these; the answer is where to send the browser next. Only the person's session
// reaches them: no token, no AI, no MCP client.

// The codes of the consent screen's refusals (ENG-23).
const (
	CodeConsentGone   = "MCP-CONSENT-01" // the consent request expired or is not known
	CodeConsentOther  = "MCP-CONSENT-02" // the consent belongs to another person
	CodeConsentNoPath = "MCP-CONSENT-03" // consent is not available on this deployment
)

// ConsentClient is the app asking, as the consent screen names it.
type ConsentClient struct {
	ID           string        `json:"id" doc:"The app's client ID." example:"claude-code"`
	Name         string        `json:"name" doc:"The name it is registered with." example:"Claude Code"`
	Class        string        `json:"class" enum:"user,integration,agent" doc:"Its class: an agent only reads and proposes (AI-34)." example:"agent"`
	Publisher    string        `json:"publisher,omitempty" doc:"The publisher Fyniq verified: only for an app the release pre-registered." example:"Anthropic"`
	Verified     bool          `json:"verified" doc:"Whether Fyniq verified who publishes it." example:"true"`
	ProposalOnly bool          `json:"proposal_only" doc:"Whether it can only read and propose." example:"true"`
	RedirectURIs []RedirectURI `json:"redirect_uris,omitempty" maxItems:"5" doc:"For an app Fyniq has not verified: where it sends you after you answer."`
}

// ConsentDecision is what consent may grant.
type ConsentDecision struct {
	Scopes        []MCPScopeName `json:"scopes" nullable:"false" maxItems:"30" doc:"What would be granted."`
	RefusedScopes []MCPScopeName `json:"refused_scopes" nullable:"false" maxItems:"30" doc:"What was asked for and cannot be granted."`
	Audiences     []RedirectURI  `json:"audiences" nullable:"false" maxItems:"3" doc:"Who the token is for."`
	Problem       string         `json:"problem,omitempty" doc:"When set, consent is refused whatever you say, in plain words." example:"AI apps are not turned on for this organisation."`
	Code          string         `json:"code,omitempty" doc:"The stable code of the problem." example:"MCP-GATE-01"`
}

// ConsentCategory says what a scope reaches.
type ConsentCategory struct {
	Scope   string `json:"scope" example:"fyniq.read.reports"`
	Reaches string `json:"reaches" doc:"In plain words." example:"Reports and key figures"`
}

// ConsentExpiry is how long a consent and its tokens last.
type ConsentExpiry struct {
	ConsentValidDays   int `json:"consent_valid_days" example:"90"`
	AccessTokenMinutes int `json:"access_token_minutes" example:"60"`
}

// ConsentScreen is what the consent screen shows (AI-37): the client, what it asks for, what would be granted, what each scope reaches
// and how long it lasts.
type ConsentScreen struct {
	Client          ConsentClient     `json:"client"`
	RequestedScopes []MCPScopeName    `json:"requested_scopes" nullable:"false" maxItems:"30" doc:"What the app asked for."`
	Decision        ConsentDecision   `json:"decision"`
	DataCategories  []ConsentCategory `json:"data_categories,omitempty" maxItems:"30" doc:"What each scope reaches, in plain words (an AI app's consent)."`
	Expiry          *ConsentExpiry    `json:"expiry,omitempty" doc:"How long the consent and its tokens last (an AI app's consent)."`
}

// ConsentAnswer is the person's answer.
type ConsentAnswer struct {
	Challenge string         `json:"consent_challenge" minLength:"1" maxLength:"2000" doc:"The consent request, from the address the OAuth server sent the browser to." example:"nRq8"`
	Decision  string         `json:"decision" enum:"accept,deny" doc:"accept to grant what the screen shows; deny to refuse." example:"accept"`
	Scopes    []MCPScopeName `json:"scopes,omitempty" maxItems:"30" doc:"Only these of the scopes the screen showed (and always openid). Omit to grant all that the screen showed; a scope it did not show is never added."`
}

// ConsentNext is where to send the browser.
type ConsentNext struct {
	RedirectTo string `json:"redirect_to" format:"uri" doc:"Where to send the browser next: the OAuth server, which carries the app's authorisation on." example:"http://localhost:8080/oauth/oauth2/auth?consent_verifier=abc"`
}

type (
	consentChallengeInput struct {
		Challenge string `query:"consent_challenge" required:"true" minLength:"1" maxLength:"2000" doc:"The consent request." example:"nRq8"`
	}
	consentScreenOutput struct{ Body ConsentScreen }
	consentAnswerInput  struct{ Body ConsentAnswer }
	consentNextOutput   struct{ Body ConsentNext }
)

// ConsentFlow is the consent app's two steps (oauthflow.Flow); nil: consent is not available here.
type ConsentFlow interface {
	ConsentView(ctx context.Context, challenge string, p authn.Principal) (map[string]any, error)
	AnswerConsent(ctx context.Context, challenge string, accept bool, only []string, p authn.Principal) (string, error)
}

var _ ConsentFlow = oauthflow.Flow{}

func registerOAuthConsent(a *API) {
	own := func(ctx context.Context) (authn.Principal, error) {
		p, _ := authn.From(ctx)
		if a.deps.Consent == nil {
			return p, problem(CodeConsentNoPath, "Consent is not available here.", "Connect the app on a deployment that offers AI apps")
		}
		if p.Via != authn.ViaSession || p.UserID == uuid.Nil || p.AI() {
			return p, problem(CodeSessionRequired, "Consent is answered with your own signed-in session.", "Sign in to Fyniq in the browser, then return to the app that sent you here")
		}
		return p, nil
	}
	gone := func(err error) error {
		switch {
		case errors.Is(err, ports.ErrNoChallenge):
			return problem(CodeConsentGone, "The request to connect expired or is not known.", "Start again from the app")
		case errors.Is(err, oauthflow.ErrOtherUser):
			return problem(CodeConsentOther, "This request to connect is for another person.", "Sign in as the person who started it")
		}
		return err
	}
	register(a, authz.GetOAuthConsent, huma.Operation{
		OperationID: "getOAuthConsent", Method: http.MethodGet, Path: "/v1/oauth/consent", Tags: []string{"Connected apps"},
		Summary: "See what an app asks to do for you",
		Description: "The consent screen's first step (AI-37): the app that asks to connect, whether Fyniq has verified who publishes it (an unverified app " +
			"shows where it sends you), what it asks for and what would be granted, what each scope reaches, and how long it lasts; or why it " +
			"cannot be granted (an organisation that has not turned AI apps on, an app that is not allowed, your own opt-in, a second factor you " +
			"must pass first). Only your own signed-in session reaches it. Read-only; no side effects.",
	}, []string{CodeConsentGone, CodeConsentOther, CodeConsentNoPath, CodeSessionRequired}, func(ctx context.Context, in *consentChallengeInput) (*consentScreenOutput, error) {
		p, err := own(ctx)
		if err != nil {
			return nil, err
		}
		m, err := a.deps.Consent.ConsentView(ctx, in.Challenge, p)
		if err != nil {
			return nil, gone(err)
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return nil, err
		}
		var out ConsentScreen
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		return &consentScreenOutput{Body: out}, nil
	})
	register(a, authz.AnswerOAuthConsent, exempt(exempt(huma.Operation{
		OperationID: "answerOAuthConsent", Method: http.MethodPost, Path: "/v1/oauth/consent", Tags: []string{"Connected apps"},
		Summary: "Allow or refuse an app",
		Description: "The consent screen's second step (AI-37): records your answer to the app's request and says where to send the browser next. Allowing " +
			"grants only what the screen showed, checked again here against every gate; a gate that refuses is answered to the app as a refusal. " +
			"Only your own signed-in session reaches it. Side effects: the OAuth server's consent record. " +
			"Never on the AI's or MCP's tool list.",
	}, "Idempotency-Key", "A consent request belongs to one authorisation request of the app, which the OAuth server binds to the browser, so a stored response would only replay a grant (AI-37)."),
		"Prefer", "A preview would show a grant that is not recorded (AI-37)."), []string{CodeConsentGone, CodeConsentOther, CodeConsentNoPath, CodeSessionRequired},
		func(ctx context.Context, in *consentAnswerInput) (*consentNextOutput, error) {
			p, err := own(ctx)
			if err != nil {
				return nil, err
			}
			to, err := a.deps.Consent.AnswerConsent(ctx, in.Body.Challenge, in.Body.Decision == "accept", consentScopes(in.Body.Scopes), p)
			if err != nil {
				return nil, gone(err)
			}
			return &consentNextOutput{Body: ConsentNext{RedirectTo: to}}, nil
		})
}

func consentScopes(in []MCPScopeName) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = string(s)
	}
	return out
}
