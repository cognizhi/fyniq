package api

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/danielgtaylor/huma/v2"
)

// The operations of the passkey ceremonies take no Idempotency-Key: each answer is bound to a one-time challenge kept for five minutes, so
// repeating one is refused (NM_PASSKEY_NO_CEREMONY) and cannot do the work twice, and a response with a challenge is never stored.
var IdempotencyExempt = map[string]string{ // #nosec G101 -- operation names and reasons, not credentials
	"POST /v1/passkeys/registration":   "starts a ceremony whose answer is bound to a one-time challenge",
	"POST /v1/passkeys":                "answers a one-time challenge; a repeat is refused",
	"POST /v1/session/step-up":         "starts a ceremony whose answer is bound to a one-time challenge",
	"POST /v1/session/step-up/confirm": "answers a one-time challenge; a repeat is refused",
}

type ceremonyInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
}

// optionsOutput is the options document the browser passes to navigator.credentials (WebAuthn's PublicKeyCredentialCreationOptions or
// PublicKeyCredentialRequestOptions, wrapped in publicKey).
type optionsOutput struct {
	Body map[string]any
}

type registerInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Name       string         `json:"name" minLength:"1" maxLength:"100" doc:"What the person calls this passkey." example:"My laptop"`
		Credential map[string]any `json:"credential" doc:"The PublicKeyCredential the browser returned, as JSON."`
	}
}

type registeredOutput struct {
	Body struct {
		ID string `json:"id" format:"uuid" doc:"The new passkey." example:"0194f5a0-7c3e-7d11-8b3e-3f5c2a9d1e50"`
	}
}

type confirmInput struct {
	Cookie string `header:"Cookie" doc:"The browser's cookies; the session cookie is read from them." example:"nemura_session=…"`
	Body   struct {
		Credential map[string]any `json:"credential" doc:"The PublicKeyCredential the browser returned for the step-up, as JSON."`
	}
}

type confirmedOutput struct {
	Body struct {
		Confirmed bool `json:"confirmed" doc:"True: the session is confirmed with a passkey for the next five minutes." example:"true"`
	}
}

func (a *API) signedIn(ctx context.Context, cookie string) (Session, error) {
	s, err := a.d.Sessions.Authenticate(ctx, a.cookie(cookie), a.d.Clock.Now())
	if err != nil {
		return Session{}, Err(CodeUnauthenticated, "There is no valid session in this request.")
	}
	return s, nil
}

// ceremonyError turns the passkey service's refusals into stable codes.
func ceremonyError(err error) error {
	switch {
	case errors.Is(err, ErrPasskeyRefused):
		return Err(CodePasskeyRefused, "The passkey's answer did not pass its checks: the site, the challenge, the signature or the person's verification.")
	case errors.Is(err, ErrNoCeremony):
		return Err(CodeNoCeremony, "There is no open passkey ceremony for this session: it was not started, was answered already, or has expired.")
	case errors.Is(err, ErrNoPasskey):
		return Err(CodeNoPasskey, "This person has registered no passkey to confirm with.")
	}
	return huma.Error500InternalServerError("The passkey ceremony could not be completed.")
}

func raw(m map[string]any) ([]byte, error) { return json.Marshal(m) }

func asMap(b []byte) (map[string]any, error) {
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

func (a *API) registerPasskeys() {
	huma.Register(a.API, huma.Operation{
		OperationID: "beginPasskeyRegistration", Method: "POST", Path: "/v1/passkeys/registration", Tags: []string{"Session"}, Summary: "Start registering a passkey",
		Description: "Returns the options the browser passes to navigator.credentials.create to register a passkey for the signed-in person, and keeps a one-time challenge for five minutes. It needs a session and saves no passkey until the answer is sent to POST /v1/passkeys.",
		Errors:      []int{401},
	}, func(ctx context.Context, in *ceremonyInput) (*optionsOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		b, err := a.d.Passkeys.BeginRegistration(ctx, s)
		if err != nil {
			return nil, ceremonyError(err)
		}
		m, err := asMap(b)
		if err != nil {
			return nil, ceremonyError(err)
		}
		return &optionsOutput{Body: m}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "registerPasskey", Method: "POST", Path: "/v1/passkeys", Tags: []string{"Session"}, Summary: "Register a passkey",
		Description: "Checks the browser's answer to the registration challenge and keeps the passkey for the signed-in person, with its audit event. The challenge is used once, whatever the answer; a wrong answer is NM_PASSKEY_REFUSED and an unknown or used challenge is NM_PASSKEY_NO_CEREMONY.",
		Errors:      []int{401, 409, 422}, DefaultStatus: 201,
	}, func(ctx context.Context, in *registerInput) (*registeredOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		cred, err := raw(in.Body.Credential)
		if err != nil {
			return nil, Err(CodeInvalid, "body.credential is not JSON.", FieldProblem{Code: CodeInvalid, Field: "body.credential", Message: "is not JSON", Fix: "Send the PublicKeyCredential the browser returned."})
		}
		id, err := a.d.Passkeys.CompleteRegistration(ctx, s, in.Body.Name, cred)
		if err != nil {
			return nil, ceremonyError(err)
		}
		out := &registeredOutput{}
		out.Body.ID = id.String()
		return out, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "beginStepUp", Method: "POST", Path: "/v1/session/step-up", Tags: []string{"Session"}, Summary: "Start confirming the session with a passkey",
		Description: "Returns the options the browser passes to navigator.credentials.get to confirm it is the person, and keeps a one-time challenge for five minutes. It needs a session; without a registered passkey it is NM_NO_PASSKEY. Nothing changes until the answer is sent to POST /v1/session/step-up/confirm.",
		Errors:      []int{401, 409},
	}, func(ctx context.Context, in *ceremonyInput) (*optionsOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		b, err := a.d.Passkeys.BeginStepUp(ctx, s)
		if err != nil {
			return nil, ceremonyError(err)
		}
		m, err := asMap(b)
		if err != nil {
			return nil, ceremonyError(err)
		}
		return &optionsOutput{Body: m}, nil
	})
	huma.Register(a.API, huma.Operation{
		OperationID: "confirmStepUp", Method: "POST", Path: "/v1/session/step-up/confirm", Tags: []string{"Session"}, Summary: "Confirm the session with a passkey",
		Description: "Checks the browser's answer to the step-up challenge: one of the person's live passkeys signed exactly that challenge, for this site, with the person verified, and its counter did not go back. The session is then confirmed for five minutes, which approvals need. The challenge is used once, whatever the answer.",
		Errors:      []int{401, 409, 422},
	}, func(ctx context.Context, in *confirmInput) (*confirmedOutput, error) {
		s, err := a.signedIn(ctx, in.Cookie)
		if err != nil {
			return nil, err
		}
		cred, err := raw(in.Body.Credential)
		if err != nil {
			return nil, Err(CodeInvalid, "body.credential is not JSON.", FieldProblem{Code: CodeInvalid, Field: "body.credential", Message: "is not JSON", Fix: "Send the PublicKeyCredential the browser returned."})
		}
		if err := a.d.Passkeys.CompleteStepUp(ctx, s, cred); err != nil {
			return nil, ceremonyError(err)
		}
		out := &confirmedOutput{}
		out.Body.Confirmed = true
		return out, nil
	})
}
