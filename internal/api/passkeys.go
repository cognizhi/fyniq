package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/passkey"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ passkeys (FR-ADM-02, SEC-01, FR-APR-04, rev 0.43)

// ChallengeTTL is how long a passkey challenge may be answered.
const ChallengeTTL = 5 * time.Minute

// WebAuthnJSON is a WebAuthn document as the browser takes or gives it: the options for navigator.credentials.create
// or .get (under publicKey), or the PublicKeyCredential they return, as its toJSON() writes it.
type WebAuthnJSON map[string]any

// Schema implements huma.SchemaProvider: an object, as the W3C WebAuthn Level 3 JSON encodings define it.
func (WebAuthnJSON) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "object", AdditionalProperties: true,
		Description: "A WebAuthn document in its JSON encoding (W3C WebAuthn Level 3): binary values are base64url strings.",
		Examples:    []any{map[string]any{"publicKey": map[string]any{"challenge": "c2FtcGxlLWNoYWxsZW5nZS0zMi1ieXRlcy1sb25nLXh4", "rpId": "fyniq.example.com"}}}}
}

// Transport is how a browser reaches an authenticator.
type Transport string

// Schema implements huma.SchemaProvider: a string, with an example.
func (Transport) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "How the browser reaches the authenticator: internal, hybrid, usb, nfc, ble or smart-card.",
		Examples: []any{"internal"}}
}

// Passkey is one of your passkeys.
type Passkey struct {
	ID             string      `json:"id" format:"uuid" doc:"The passkey's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f40"`
	Name           string      `json:"name" doc:"The name you gave it." example:"Work laptop"`
	Authenticator  string      `json:"authenticator" format:"uuid" doc:"The authenticator's model (its AAGUID); all zeros when it names none." example:"ea9b8d66-4d01-1d21-3ce4-b6b48cb575d4"`
	Transports     []Transport `json:"transports" doc:"How the browser reaches it, as it said."`
	BackupEligible bool        `json:"backup_eligible" doc:"true for a passkey that syncs between your devices." example:"true"`
	BackedUp       bool        `json:"backed_up" doc:"true when it was synced, as of its last use." example:"true"`
	CreatedAt      time.Time   `json:"created_at" doc:"When you registered it." example:"2026-10-20T09:00:00Z"`
	LastUsedAt     *time.Time  `json:"last_used_at,omitempty" doc:"When you last confirmed it's you with it." example:"2026-10-20T09:30:00Z"`
	RevokedAt      *time.Time  `json:"revoked_at,omitempty" doc:"When you revoked it; it works no more." example:"2026-10-21T09:00:00Z"`
}

// PasskeyPage is a page of your passkeys, oldest first.
type PasskeyPage struct {
	Items      []Passkey `json:"items" doc:"Your passkeys that work, on this page."`
	NextCursor string    `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// PasskeyChallenge is a challenge for your authenticator, with the options to pass to the browser.
type PasskeyChallenge struct {
	ChallengeID string       `json:"challenge_id" format:"uuid" doc:"Send it back with the answer." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f41"`
	ExpiresAt   time.Time    `json:"expires_at" doc:"Answer it before then: 5 minutes after it was made." example:"2026-10-20T09:05:00Z"`
	Options     WebAuthnJSON `json:"options" doc:"Pass it to navigator.credentials.create (registration) or navigator.credentials.get (step-up), after decoding its base64url values, or to PublicKeyCredential.parseCreationOptionsFromJSON / parseRequestOptionsFromJSON."`
}

// NewPasskey is your authenticator's answer to a registration challenge.
type NewPasskey struct {
	ChallengeID string       `json:"challenge_id" format:"uuid" doc:"The registration challenge it answers." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f41"`
	Name        string       `json:"name" minLength:"1" maxLength:"100" doc:"A name to know it by." example:"Work laptop"`
	Credential  WebAuthnJSON `json:"credential" doc:"The PublicKeyCredential navigator.credentials.create returned, as its toJSON() writes it."`
}

// StepUpAnswer is your authenticator's answer to a step-up challenge.
type StepUpAnswer struct {
	ChallengeID string       `json:"challenge_id" format:"uuid" doc:"The step-up challenge it answers." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f41"`
	Credential  WebAuthnJSON `json:"credential" doc:"The PublicKeyCredential navigator.credentials.get returned, as its toJSON() writes it."`
}

// SteppedUp is your session after a step-up.
type SteppedUp struct {
	SteppedUpUntil time.Time `json:"stepped_up_until" doc:"Until when the step-up counts for the actions that need one: 5 minutes." example:"2026-10-20T09:05:00Z"`
	StepUpMethod   string    `json:"step_up_method" enum:"passkey" doc:"How you passed it." example:"passkey"`
	PasskeyID      string    `json:"passkey_id" format:"uuid" doc:"The passkey you used." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f40"`
}

type passkeyListInput struct{ PageQuery }

type passkeyRegisterInput struct{ Body NewPasskey }

type passkeyRevokeInput struct {
	PasskeyID string `path:"passkey_id" format:"uuid" doc:"The passkey's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f40"`
}

type stepUpInput struct{ Body StepUpAnswer }

type passkeyListOutput struct{ Body PasskeyPage }

type passkeyChallengeOutput struct {
	CacheControl string `header:"Cache-Control" doc:"no-store: a challenge is answered once."`
	Body         PasskeyChallenge
}

type passkeyOutput struct{ Body Passkey }

type steppedUpOutput struct{ Body SteppedUp }

// PasskeyCodes are what a passkey ceremony can answer besides the common codes.
var PasskeyCodes = []string{CodeSessionRequired, CodeUnavailable, CodeChallenge, CodePasskeyRefused, storage.CodePasskey}

func passkeyOf(k storage.Passkey) Passkey {
	out := Passkey{ID: k.ID.String(), Name: k.Name, Authenticator: uuidOrZero(k.AAGUID), Transports: []Transport{}, BackupEligible: k.BackupEligible,
		BackedUp: k.BackupState, CreatedAt: k.CreatedAt.UTC(), LastUsedAt: utc(k.LastUsedAt), RevokedAt: utc(k.RevokedAt)}
	for _, t := range k.Transports {
		out.Transports = append(out.Transports, Transport(t))
	}
	return out
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func uuidOrZero(b []byte) string {
	id, err := uuid.FromBytes(b)
	if err != nil {
		return uuid.Nil.String()
	}
	return id.String()
}

// ceremony exempts a passkey ceremony from Idempotency-Key and preview: its challenge makes it one-time already.
func ceremony(op huma.Operation) huma.Operation {
	op = exempt(op, "Idempotency-Key", "A passkey ceremony answers a challenge that works once, for one session: the challenge makes it "+
		"one-time, and a stored answer replayed later would step a session up without the authenticator (rev 0.43).")
	return exempt(op, "Prefer", "A preview would spend the challenge, which works once, without the ceremony's effect (FR-INT-14, rev 0.43).")
}

// passkeyPerson returns the caller when they are a person with a Fyniq user in a browser session, the installation
// organisation, whose trail records passkeys, and the caller's name.
func (a *API) passkeyPerson(ctx context.Context, what string) (authn.Principal, uuid.UUID, string, error) {
	if a.passkeys == nil || a.deps.Installation == nil {
		return authn.Principal{}, uuid.Nil, "", problem(CodeUnavailable, "Passkeys need the web app's origin and the database configured.",
			"Set FYNIQ_PUBLIC_ORIGIN and the database, then try again.")
	}
	p, err := signedInPerson(ctx, what)
	if err != nil {
		return p, uuid.Nil, "", err
	}
	if p.UserID == uuid.Nil || len(p.SessionHash) == 0 {
		return p, uuid.Nil, "", problem(CodeSessionRequired, what+" needs a person with a Fyniq user, signed in to Fyniq in the browser.",
			"Sign in to Fyniq in the browser with your own sign-in and do it there.")
	}
	org, err := a.deps.Installation.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return p, uuid.Nil, "", problem(CodeUnavailable, "The installation is not set up yet.", "Complete first-run setup first.")
	}
	if err != nil {
		return p, uuid.Nil, "", err
	}
	name, err := a.personName(ctx, p)
	return p, org, name, err
}

// personName is the caller's name as their organisation knows it, for their authenticator's account list; read before
// the installation's unit of work, since a unit of work holds one organisation.
func (a *API) personName(ctx context.Context, p authn.Principal) (string, error) {
	name := "Fyniq user"
	if p.Organisation == uuid.Nil {
		return name, nil
	}
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, o storage.UnitOfWork) error {
		names, err := memberNames(ctx, o, a.deps.Clock.Now())
		if n := names[p.UserID]; n != "" {
			name = n
		}
		return err
	})
	return name, err
}

// passkeyUser is the person to their authenticator, with their live passkeys.
func passkeyUser(ctx context.Context, u storage.UnitOfWork, p authn.Principal, name string) (passkey.User, error) {
	ks, err := u.Passkeys().List(ctx, p.UserID, false)
	return passkey.User{ID: p.UserID, Name: name, Passkeys: ks}, err
}

// issue makes a challenge for the session and returns it with the options document.
func (a *API) issue(ctx context.Context, u storage.UnitOfWork, p authn.Principal, purpose string, challenge []byte, options any) (PasskeyChallenge, error) {
	now := a.deps.Clock.Now()
	id, err := u.Passkeys().Challenge(ctx, storage.NewPasskeyChallenge{SessionHash: p.SessionHash, Purpose: purpose, Challenge: challenge,
		Now: now, Expires: now.Add(ChallengeTTL)})
	if err != nil {
		return PasskeyChallenge{}, err
	}
	b, err := json.Marshal(options)
	if err != nil {
		return PasskeyChallenge{}, err
	}
	var doc WebAuthnJSON
	if err := json.Unmarshal(b, &doc); err != nil {
		return PasskeyChallenge{}, err
	}
	return PasskeyChallenge{ChallengeID: id.String(), ExpiresAt: now.Add(ChallengeTTL).UTC(), Options: doc}, nil
}

// take takes the challenge a ceremony answers, in a unit of work of its own: a challenge taken stays taken, whether the
// answer passes or not.
func (a *API) take(ctx context.Context, org uuid.UUID, p authn.Principal, rawID, purpose string) (storage.PasskeyChallenge, uuid.UUID, error) {
	id, err := parseID("challenge_id", rawID)
	if err != nil {
		return storage.PasskeyChallenge{}, uuid.Nil, err
	}
	var c storage.PasskeyChallenge
	err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err = u.Passkeys().Take(ctx, id, p.SessionHash, purpose, a.deps.Clock.Now())
		return err
	})
	if errors.Is(err, ports.ErrNotFound) || (err == nil && c.UserID != p.UserID) {
		return c, id, problem(CodeChallenge, "That challenge is unknown, belongs to another session, was used already or expired.",
			"Ask for a new challenge and answer it within 5 minutes.")
	}
	return c, id, err
}

// passkeyRefused logs why the authenticator's answer failed, without key material (SEC-15), and returns the problem.
func (a *API) passkeyRefused(ctx context.Context, op string, err error) error {
	a.deps.Log.Warn("api.passkey.refused", "operation", op, "reason", err.Error(), "request_id", requestID(ctx))
	return problem(CodePasskeyRefused, "Your authenticator's answer did not pass Fyniq's checks, so nothing changed.",
		"Try again on this site, with a passkey you registered here, and confirm with your fingerprint, face or PIN.")
}

func credentialBytes(c WebAuthnJSON) ([]byte, error) {
	if len(c) == 0 {
		return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.credential", Message: "is missing",
			Fix: "Send the PublicKeyCredential your browser returned, as its toJSON() writes it"}})
	}
	return json.Marshal(c)
}

func registerPasskeys(a *API) {
	register(a, authz.ListPasskeys, huma.Operation{
		OperationID: "listPasskeys", Method: http.MethodGet, Path: "/v1/session/passkeys", Tags: []string{"Session"},
		Summary:     "List your passkeys",
		Description: "Your passkeys that work, oldest first (FR-ADM-02). " + authz.ListPasskeys.Note + " Read-only; no side effects.",
	}, append([]string{CodeCursor}, PasskeyCodes...), func(ctx context.Context, in *passkeyListInput) (*passkeyListOutput, error) {
		p, org, _, err := a.passkeyPerson(ctx, "Listing your passkeys")
		if err != nil {
			return nil, err
		}
		all := []Passkey{}
		err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			ks, err := u.Passkeys().List(ctx, p.UserID, false)
			for _, k := range ks {
				all = append(all, passkeyOf(k))
			}
			return err
		})
		if err != nil {
			return nil, err
		}
		items, next, err := pageOf(all, func(k Passkey) string { return k.ID }, in.PageQuery)
		if err != nil {
			return nil, err
		}
		return &passkeyListOutput{Body: PasskeyPage{Items: items, NextCursor: next}}, nil
	})

	register(a, authz.StartPasskeyRegistration, ceremony(huma.Operation{
		OperationID: "startPasskeyRegistration", Method: http.MethodPost, Path: "/v1/session/passkeys/challenge", Tags: []string{"Session"},
		Summary: "Start registering a passkey",
		Description: "A registration challenge for your session, with the options to pass to navigator.credentials.create: a passkey for " +
			"this site, with your fingerprint, face or PIN required; your passkeys already registered are excluded. Answer it within 5 " +
			"minutes with POST /v1/session/passkeys. " + authz.StartPasskeyRegistration.Note + " Side effects: the challenge, kept for the " +
			"answer; nothing else.",
	}), append([]string{CodeStepUpRequired}, PasskeyCodes...), func(ctx context.Context, _ *struct{}) (*passkeyChallengeOutput, error) {
		p, org, name, err := a.passkeyPerson(ctx, "Registering a passkey")
		if err != nil {
			return nil, err
		}
		if !p.SteppedUp(a.deps.Clock.Now()) {
			return nil, problem(CodeStepUpRequired, "Registering a passkey needs a second factor in the last 5 minutes (FR-ADM-02).",
				"Confirm it's you with your authenticator app or another passkey, then try again.")
		}
		out := passkeyChallengeOutput{CacheControl: "no-store"}
		err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			user, err := passkeyUser(ctx, u, p, name)
			if err != nil {
				return err
			}
			options, challenge, err := a.passkeys.Creation(user)
			if err != nil {
				return err
			}
			out.Body, err = a.issue(ctx, u, p, storage.PurposeRegister, challenge, options)
			return err
		})
		return &out, err
	})

	register(a, authz.RegisterPasskey, ceremony(huma.Operation{
		OperationID: "registerPasskey", Method: http.MethodPost, Path: "/v1/session/passkeys", Tags: []string{"Session"},
		DefaultStatus: http.StatusCreated, Summary: "Register a passkey",
		Description: "Registers the passkey your authenticator made for the registration challenge (FR-ADM-02). Fyniq checks the challenge, " +
			"this site's origin and relying party ID, that you were present and verified, and the key; the challenge works once. " +
			authz.RegisterPasskey.Note + " Side effects: the passkey and its event in the installation's audit trail.",
	}), append([]string{CodeStepUpRequired, CodeMalformed, CodeTooLarge, CodeMediaType}, PasskeyCodes...),
		func(ctx context.Context, in *passkeyRegisterInput) (*passkeyOutput, error) {
			p, org, name, err := a.passkeyPerson(ctx, "Registering a passkey")
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			if !p.SteppedUp(now) {
				return nil, problem(CodeStepUpRequired, "Registering a passkey needs a second factor in the last 5 minutes (FR-ADM-02).",
					"Confirm it's you with your authenticator app or another passkey, then try again.")
			}
			label := strings.TrimSpace(in.Body.Name)
			if label == "" || len([]rune(label)) > 100 {
				return nil, invalid([]FieldProblem{{Code: CodeInvalid, Field: "body.name", Message: "is blank or longer than 100 characters",
					Fix: "Name the passkey, such as \"Work laptop\""}})
			}
			response, err := credentialBytes(in.Body.Credential)
			if err != nil {
				return nil, err
			}
			c, id, err := a.take(ctx, org, p, in.Body.ChallengeID, storage.PurposeRegister)
			if err != nil {
				return nil, err
			}
			var out passkeyOutput
			err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				user, err := passkeyUser(ctx, u, p, name)
				if err != nil {
					return err
				}
				r, err := a.passkeys.Register(user, c.Challenge, response)
				if err != nil {
					return a.passkeyRefused(ctx, "registerPasskey", err)
				}
				k, err := u.Passkeys().Register(ctx, storage.NewPasskey{ChallengeID: id, SessionHash: p.SessionHash, UserID: p.UserID,
					CredentialID: r.CredentialID, PublicKey: r.PublicKey, SignCount: r.SignCount, AAGUID: r.AAGUID, Transports: r.Transports,
					BackupEligible: r.BackupEligible, BackupState: r.BackupState, Name: label, Now: now})
				out.Body = passkeyOf(k)
				return err
			})
			if err != nil {
				return nil, err
			}
			return &out, nil
		})

	register(a, authz.RevokePasskey, exempt(huma.Operation{
		OperationID: "revokePasskey", Method: http.MethodDelete, Path: "/v1/session/passkeys/{passkey_id}", Tags: []string{"Session"},
		Summary: "Revoke a passkey",
		Description: "Revokes one of your passkeys: it works no more, for step-up or anything else (FR-ADM-02). " + authz.RevokePasskey.Note +
			" Side effects: the revocation and its event in the installation's audit trail.",
	}, "Prefer", "Revoking a passkey only takes away a way to confirm it's you; it is checked and done at once, and its audit event is the record (FR-INT-14, rev 0.43)."),
		append([]string{CodeStepUpRequired, CodeNotFound}, PasskeyCodes...), func(ctx context.Context, in *passkeyRevokeInput) (*passkeyOutput, error) {
			id, err := parseID("passkey_id", in.PasskeyID)
			if err != nil {
				return nil, err
			}
			p, org, _, err := a.passkeyPerson(ctx, "Revoking a passkey")
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			if !p.SteppedUp(now) {
				return nil, problem(CodeStepUpRequired, "Revoking a passkey needs a second factor in the last 5 minutes (FR-ADM-02).",
					"Confirm it's you with your authenticator app or another passkey, then try again.")
			}
			var out passkeyOutput
			err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				if err := u.Passkeys().Revoke(ctx, p.UserID, id, now); errors.Is(err, ports.ErrNotFound) {
					return problem(CodeNotFound, "You have no working passkey with that ID.", "List your passkeys to find their IDs")
				} else if err != nil {
					return err
				}
				all, err := u.Passkeys().List(ctx, p.UserID, true)
				for _, k := range all {
					if k.ID == id {
						out.Body = passkeyOf(k)
					}
				}
				return err
			})
			if err != nil {
				return nil, err
			}
			return &out, nil
		})

	register(a, authz.StartStepUp, ceremony(huma.Operation{
		OperationID: "startStepUp", Method: http.MethodPost, Path: "/v1/session/step-up/challenge", Tags: []string{"Session"},
		Summary: "Start confirming it's you with a passkey",
		Description: "A step-up challenge for your session, with the options to pass to navigator.credentials.get: any of your passkeys, " +
			"with your fingerprint, face or PIN required (FR-APR-04, SEC-01). Answer it within 5 minutes with POST /v1/session/step-up. " +
			authz.StartStepUp.Note + " Side effects: the challenge, kept for the answer; nothing else.",
	}), append([]string{CodeNoPasskey}, PasskeyCodes...), func(ctx context.Context, _ *struct{}) (*passkeyChallengeOutput, error) {
		p, org, name, err := a.passkeyPerson(ctx, "Confirming it's you with a passkey")
		if err != nil {
			return nil, err
		}
		out := passkeyChallengeOutput{CacheControl: "no-store"}
		err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			user, err := passkeyUser(ctx, u, p, name)
			if err != nil {
				return err
			}
			if len(user.Passkeys) == 0 {
				return problem(CodeNoPasskey, "You have no passkey to confirm it's you with.",
					"Sign in again with your authenticator app, then register a passkey")
			}
			challenge := make([]byte, 32)
			_, _ = rand.Read(challenge)
			options, err := a.passkeys.Assertion(user, challenge)
			if err != nil {
				return err
			}
			out.Body, err = a.issue(ctx, u, p, storage.PurposeStepUp, challenge, options)
			return err
		})
		return &out, err
	})

	register(a, authz.StepUp, ceremony(huma.Operation{
		OperationID: "stepUp", Method: http.MethodPost, Path: "/v1/session/step-up", Tags: []string{"Session"},
		Summary: "Confirm it's you with a passkey",
		Description: "Checks your authenticator's answer to the step-up challenge and, when it passes, steps your session up: for 5 minutes " +
			"it may do the actions that need a second factor, such as asking for or approving a change (FR-APR-04, SEC-01). Fyniq checks the " +
			"challenge, this site's origin and relying party ID, that you were present and verified, the signature with one of your " +
			"passkeys, and that its counter went up; the challenge works once. " + authz.StepUp.Note + " Side effects: the session's step-up " +
			"and the passkey's last use.",
	}), append([]string{CodeMalformed, CodeTooLarge, CodeMediaType}, PasskeyCodes...),
		func(ctx context.Context, in *stepUpInput) (*steppedUpOutput, error) {
			p, org, name, err := a.passkeyPerson(ctx, "Confirming it's you with a passkey")
			if err != nil {
				return nil, err
			}
			response, err := credentialBytes(in.Body.Credential)
			if err != nil {
				return nil, err
			}
			c, id, err := a.take(ctx, org, p, in.Body.ChallengeID, storage.PurposeStepUp)
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			var out steppedUpOutput
			err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
				user, err := passkeyUser(ctx, u, p, name)
				if err != nil {
					return err
				}
				got, err := a.passkeys.Verify(user, c.Challenge, response)
				if err != nil {
					return a.passkeyRefused(ctx, "stepUp", err)
				}
				if err := u.Passkeys().StepUp(ctx, storage.PasskeyStepUp{ChallengeID: id, SessionHash: p.SessionHash, PasskeyID: got.Passkey.ID,
					SignCount: got.SignCount, BackupState: got.BackupState, Now: now}); err != nil {
					return err
				}
				out.Body = SteppedUp{SteppedUpUntil: now.Add(authn.StepUpWindow).UTC().Truncate(time.Second), StepUpMethod: "passkey",
					PasskeyID: got.Passkey.ID.String()}
				return nil
			})
			if err != nil {
				return nil, err
			}
			a.deps.Log.Info("api.session.stepped_up", "method", "passkey", "passkey", out.Body.PasskeyID, "request_id", requestID(ctx))
			return &out, nil
		})
}
