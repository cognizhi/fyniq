package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
	revoke "github.com/cognizhi/fyniq/internal/tokens"
)

// How long the codes Super admins create last.
const (
	InvitationTTL   = 7 * 24 * time.Hour
	ConfirmationTTL = 24 * time.Hour
)

// OneTimeCode is a code shown once: only its SHA-256 is kept.
type OneTimeCode struct {
	Code      string    `json:"code" doc:"The one-time code. It is shown once and cannot be shown again; give it to the person it is for by a channel you trust. Case, spaces and dashes do not matter." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
	ExpiresAt time.Time `json:"expires_at" doc:"When the code stops working, if unused." example:"2026-10-22T09:00:00Z"`
}

// RecoveryCodes are a Super admin's ten printed recovery codes.
type RecoveryCodes struct {
	RecoveryCodes []RecoveryCode `json:"recovery_codes" minItems:"10" maxItems:"10" doc:"Ten recovery codes, each usable once. They are shown once: print them and keep them offline. With the other Super admin's confirmation, one gives your access back if you lose your sign-in (FR-ADM-09). Any earlier unused codes no longer work."`
}

// RecoveryCode is one printed recovery code.
type RecoveryCode string

// Schema implements huma.SchemaProvider: a string, with an example.
func (RecoveryCode) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A recovery code, usable once.", Examples: []any{"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"}}
}

func recoveryCodes(codes []string) RecoveryCodes {
	out := make([]RecoveryCode, len(codes))
	for i, c := range codes {
		out[i] = RecoveryCode(c)
	}
	return RecoveryCodes{out}
}

// SuperAdminInvitation redeems an invitation.
type SuperAdminInvitation struct {
	InvitationCode string `json:"invitation_code" minLength:"20" maxLength:"100" doc:"The invitation a Super admin created for you. Case, spaces and dashes do not matter." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
	DisplayName    string `json:"display_name" minLength:"1" maxLength:"200" doc:"Your name, as other users will see it; ignored when you are a user already." example:"Wei Jie Lim"`
}

// NewSuperAdmin is the Super admin an invitation made.
type NewSuperAdmin struct {
	UserID string `json:"user_id" format:"uuid" doc:"Your user: now a Super admin." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d04"`
	RecoveryCodes
}

// SuperAdminRecovery gives a Super admin their access back.
type SuperAdminRecovery struct {
	RecoveryCode     string `json:"recovery_code,omitempty" minLength:"20" maxLength:"100" doc:"One of your printed recovery codes. Required with the other Super admin's confirmation; optional with a deployment recovery." example:"ABCDE-FGHIJ-KLMNO-PQRST-UVWXY-Z"`
	ConfirmationCode string `json:"confirmation_code" minLength:"20" maxLength:"100" doc:"The other Super admin's confirmation of your recovery, or the deployment recovery code (docs/runbooks/super-admin-recovery.md)." example:"FGHIJ-KLMNO-PQRST-UVWXY-ZABCD-E"`
}

// RecoveredSuperAdmin is the Super admin a recovery gave access back to.
type RecoveredSuperAdmin struct {
	UserID string `json:"user_id" format:"uuid" doc:"Your user." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
	RecoveryCodes
}

type oneTimeCodeOutput struct{ Body OneTimeCode }

type acceptInvitationInput struct{ Body SuperAdminInvitation }

type newSuperAdminOutput struct{ Body NewSuperAdmin }

type recoveryCodesOutput struct{ Body RecoveryCodes }

type confirmRecoveryInput struct {
	UserID string `path:"user_id" format:"uuid" doc:"The Super admin whose recovery you confirm: not you." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4d01"`
}

type recoverInput struct{ Body SuperAdminRecovery }

type recoveredOutput struct{ Body RecoveredSuperAdmin }

// signedInPerson refuses anyone but a person signed in to Fyniq: not an OAuth client, not the AI.
func signedInPerson(ctx context.Context, what string) (authn.Principal, error) {
	p, _ := authn.From(ctx)
	if p.Via != authn.ViaSession || p.AI() || p.Issuer == "" || p.Subject == "" {
		return p, problem(CodeSessionRequired, what+" needs a person signed in to Fyniq, not an OAuth client or the AI.",
			"Sign in to Fyniq in the browser and do it there.")
	}
	return p, nil
}

// superAdmin returns the caller's user when they are an active Super admin who passed step-up within
// authn.StepUpWindow (FR-ADM-07).
func (a *API) superAdmin(ctx context.Context, what string) (uuid.UUID, error) {
	p, err := signedInPerson(ctx, what)
	if err != nil {
		return uuid.Nil, err
	}
	id, err := a.deps.Installation.SuperAdminFor(ctx, p.Issuer, p.Subject)
	if errors.Is(err, ports.ErrNotFound) {
		return uuid.Nil, problem(CodeNotSuperAdmin, what+" is for a Super admin of this installation.",
			"Ask a Super admin to do it.")
	}
	if err != nil {
		return uuid.Nil, err
	}
	if !p.SteppedUp(a.deps.Clock.Now()) {
		return uuid.Nil, problem(CodeStepUpRequired, what+" needs step-up authentication in the last 5 minutes.",
			"Confirm it's you with your passkey or authenticator app, then try again.")
	}
	return id, nil
}

// inInstallation runs fn in a unit of work of the installation organisation, whose trail records every
// Super admin change, as the user when known (AUD-01). Before setup there is no Super admin and no code.
func (a *API) inInstallation(ctx context.Context, user uuid.UUID, fn func(ctx context.Context, org uuid.UUID, u storage.UnitOfWork) error) error {
	org, err := a.deps.Installation.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return codeRefused()
	}
	if err != nil {
		return err
	}
	if user != uuid.Nil {
		ctx = actingAs(ctx, user)
	}
	return a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error { return fn(ctx, org, u) })
}

// actingAs records the user, not their sign-in subject, as the actor of installation events.
func actingAs(ctx context.Context, user uuid.UUID) context.Context {
	audit := storage.AuditFrom(ctx)
	audit.Actor, audit.OnBehalfOf = &storage.Actor{Type: "user", ID: user.String()}, nil
	return storage.WithAudit(ctx, audit)
}

func codeRefused() error {
	return problem(CodeCodeRefused, "The code is not valid, used or expired.",
		"Check the code as it was given to you. If it expired or was replaced, ask for a new one.")
}

// refused maps a store refusal to the API's: every bad code is the same answer, and is logged without
// the code (SEC-15).
func (a *API) refused(ctx context.Context, err error) error {
	switch storage.CodeOf(err) {
	case storage.CodeGrant:
		op, _ := ctx.Value(operationKey{}).(string)
		a.deps.Log.Warn("api.super_admin.code_refused", "operation", op, "request_id", requestID(ctx))
		return codeRefused()
	case storage.CodeSuperAdmin:
		return problem(CodeSuperAdminRefused, "An installation has at most two Super admins until dual control arrives, and always has one.",
			"No action is needed: this installation has its two Super admins (FR-ADM-07).")
	}
	return err
}

func registerSuperAdmins(a *API) {
	common := []string{CodeSessionRequired, CodeUnavailable}
	admin := append([]string{CodeNotSuperAdmin, CodeStepUpRequired}, common...)
	unavailable := func() error {
		return problem(CodeUnavailable, "The installation's database is not configured.", "Configure the database, then try again.")
	}
	noKey := " It takes no Idempotency-Key: a response with a code is never stored, and sending it again makes a new code that replaces the first."

	register(a, authz.CreateSuperAdminInvitation, oneTimeSecret(huma.Operation{
		OperationID: "createSuperAdminInvitation", Method: http.MethodPost, Path: "/v1/super-admins/invitations", Tags: []string{"Installation"},
		DefaultStatus: http.StatusCreated, Summary: "Invite the second Super admin",
		Description: "Creates the one-time invitation that makes its redeemer the installation's second Super admin (FR-ADM-06/07; one per " +
			"founder, open question 36). Side effects: the invitation (it lasts 7 days and replaces an unused one) and its event in the " +
			"installation's trail. Refused when the installation has two Super admins already." + noKey,
	}), append([]string{CodeSuperAdminRefused}, admin...), func(ctx context.Context, _ *struct{}) (*oneTimeCodeOutput, error) {
		if a.deps.Installation == nil {
			return nil, unavailable()
		}
		by, err := a.superAdmin(ctx, "Inviting a Super admin")
		if err != nil {
			return nil, err
		}
		code, hash := setupcode.New()
		now := a.deps.Clock.Now()
		out := OneTimeCode{Code: code, ExpiresAt: now.Add(InvitationTTL).UTC()}
		err = a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			return u.SuperAdmins().Invite(ctx, by, hash, now, out.ExpiresAt)
		})
		if err != nil {
			return nil, a.refused(ctx, err)
		}
		return &oneTimeCodeOutput{Body: out}, nil
	})

	register(a, authz.AcceptSuperAdminInvitation, oneTimeSecret(huma.Operation{
		OperationID: "acceptSuperAdminInvitation", Method: http.MethodPost, Path: "/v1/super-admins", Tags: []string{"Installation"},
		DefaultStatus: http.StatusCreated, Summary: "Become a Super admin with an invitation",
		Description: "Redeems a Super admin's invitation: you (a new user, or the one your sign-in belongs to) become a Super admin and " +
			"receive your ten recovery codes, shown once (FR-ADM-06/09). Side effects: your user and sign-in identity when new, the Super " +
			"admin, the recovery codes, events in the installation's trail. A wrong, used or expired invitation changes nothing." + noKey,
	}), append([]string{CodeCodeRefused, CodeSuperAdminRefused, CodeMalformed, CodeTooLarge, CodeMediaType}, common...),
		func(ctx context.Context, in *acceptInvitationInput) (*newSuperAdminOutput, error) {
			if a.deps.Installation == nil {
				return nil, unavailable()
			}
			p, err := signedInPerson(ctx, "Becoming a Super admin")
			if err != nil {
				return nil, err
			}
			codes, hashes := setupcode.NewSet(setupcode.RecoveryCodes)
			var user uuid.UUID
			err = a.inInstallation(ctx, uuid.Nil, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
				user, err = u.SuperAdmins().AcceptInvitation(ctx, storage.AcceptInvitation{CodeHash: setupcode.Hash(in.Body.InvitationCode),
					DisplayName: in.Body.DisplayName, Issuer: p.Issuer, Subject: p.Subject, RecoveryCodeHashes: hashes, Now: a.deps.Clock.Now()})
				return err
			})
			if err != nil {
				return nil, a.refused(ctx, err)
			}
			return &newSuperAdminOutput{Body: NewSuperAdmin{UserID: user.String(), RecoveryCodes: recoveryCodes(codes)}}, nil
		})

	register(a, authz.ReplaceRecoveryCodes, oneTimeSecret(huma.Operation{
		OperationID: "replaceSuperAdminRecoveryCodes", Method: http.MethodPost, Path: "/v1/super-admins/recovery-codes", Tags: []string{"Installation"},
		Summary: "Replace your recovery codes",
		Description: "Gives you ten new recovery codes, shown once; your unused codes stop working (FR-ADM-09). Side effects: the codes and " +
			"their event in the installation's trail." + noKey,
	}), admin, func(ctx context.Context, _ *struct{}) (*recoveryCodesOutput, error) {
		if a.deps.Installation == nil {
			return nil, unavailable()
		}
		user, err := a.superAdmin(ctx, "Replacing recovery codes")
		if err != nil {
			return nil, err
		}
		codes, hashes := setupcode.NewSet(setupcode.RecoveryCodes)
		err = a.inInstallation(ctx, user, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			return u.SuperAdmins().SetRecoveryCodes(ctx, user, hashes, a.deps.Clock.Now())
		})
		if err != nil {
			return nil, a.refused(ctx, err)
		}
		return &recoveryCodesOutput{Body: recoveryCodes(codes)}, nil
	})

	register(a, authz.ConfirmSuperAdminRecovery, oneTimeSecret(huma.Operation{
		OperationID: "confirmSuperAdminRecovery", Method: http.MethodPost, Path: "/v1/super-admins/{user_id}/recovery-confirmations",
		Tags: []string{"Installation"}, DefaultStatus: http.StatusCreated, Summary: "Confirm the other Super admin's recovery",
		Description: "Creates the one-time confirmation that, with one of their printed recovery codes, gives the other Super admin their " +
			"access back: the second proof a recovery needs (SEC-35, FR-ADM-09). Confirm only after you have spoken to them. Side effects: " +
			"the confirmation (it lasts 24 hours and replaces an unused one) and its event in the installation's trail. Nobody confirms " +
			"their own recovery." + noKey,
	}), append([]string{CodeSuperAdminRefused, CodeInvalid}, admin...), func(ctx context.Context, in *confirmRecoveryInput) (*oneTimeCodeOutput, error) {
		if a.deps.Installation == nil {
			return nil, unavailable()
		}
		by, err := a.superAdmin(ctx, "Confirming a recovery")
		if err != nil {
			return nil, err
		}
		of, err := uuid.Parse(in.UserID)
		if err != nil {
			return nil, problem(CodeInvalid, "user_id is not a UUID.", "Use the Super admin's user ID.")
		}
		if of == by {
			return nil, problem(CodeSuperAdminRefused, "You cannot confirm your own recovery.",
				"Ask the other Super admin to confirm it, or use the deployment recovery (docs/runbooks/super-admin-recovery.md).")
		}
		code, hash := setupcode.New()
		now := a.deps.Clock.Now()
		out := OneTimeCode{Code: code, ExpiresAt: now.Add(ConfirmationTTL).UTC()}
		err = a.inInstallation(ctx, by, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
			return u.SuperAdmins().ConfirmRecovery(ctx, by, of, hash, now, out.ExpiresAt)
		})
		if storage.CodeOf(err) == storage.CodeGrant { // the other user is not an active Super admin
			return nil, problem(CodeSuperAdminRefused, "That user is not an active Super admin.", "Check the user ID of the other Super admin.")
		}
		if err != nil {
			return nil, a.refused(ctx, err)
		}
		return &oneTimeCodeOutput{Body: out}, nil
	})

	register(a, authz.RecoverSuperAdmin, oneTimeSecret(huma.Operation{
		OperationID: "recoverSuperAdmin", Method: http.MethodPost, Path: "/v1/super-admins/recovery", Tags: []string{"Installation"},
		Summary: "Recover your Super admin access",
		Description: "Gives a Super admin who lost their sign-in their access back, with two proofs (SEC-35, FR-ADM-09): one of their " +
			"printed recovery codes with the other Super admin's confirmation, or the deployment's two-person recovery " +
			"(docs/runbooks/super-admin-recovery.md). The sign-in you use now replaces your others. Side effects: your sign-in " +
			"identities, ten new recovery codes (shown once; the old ones stop working), the revocation of the tokens of the replaced " +
			"sign-ins (AI-38) and events in the installation's trail. Wrong, used or expired codes change nothing." + noKey,
	}), append([]string{CodeCodeRefused, CodeMalformed, CodeTooLarge, CodeMediaType}, common...),
		func(ctx context.Context, in *recoverInput) (*recoveredOutput, error) {
			if a.deps.Installation == nil || a.deps.Outbox == nil {
				return nil, unavailable()
			}
			p, err := signedInPerson(ctx, "Recovering Super admin access")
			if err != nil {
				return nil, err
			}
			rec := storage.Recovery{ConfirmationHash: setupcode.Hash(in.Body.ConfirmationCode), Issuer: p.Issuer, Subject: p.Subject,
				Now: a.deps.Clock.Now()}
			if in.Body.RecoveryCode != "" {
				rec.RecoveryCodeHash = setupcode.Hash(in.Body.RecoveryCode)
			}
			codes, hashes := setupcode.NewSet(setupcode.RecoveryCodes)
			rec.RecoveryCodeHashes = hashes
			var out storage.Recovered
			err = a.inInstallation(ctx, uuid.Nil, func(ctx context.Context, org uuid.UUID, u storage.UnitOfWork) error {
				if out, err = u.SuperAdmins().Recover(ctx, rec); err != nil {
					return err
				}
				// SEC-35: a recovery revokes the tokens of every sign-in it replaced, and of the user (the
				// subject of tokens once the SignIn port signs people in as their user).
				ctx = actingAs(ctx, out.UserID)
				for _, subject := range append(out.RemovedSubjects, out.UserID.String()) {
					if err := revoke.RevokeAll(ctx, *a.deps.Outbox, u, org, subject, "super_admin.recovered"); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return nil, a.refused(ctx, err)
			}
			return &recoveredOutput{Body: RecoveredSuperAdmin{UserID: out.UserID.String(), RecoveryCodes: recoveryCodes(codes)}}, nil
		})
}
