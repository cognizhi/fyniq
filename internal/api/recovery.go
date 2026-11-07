package api

import (
	"context"
	"crypto/rand"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
	revoke "github.com/cognizhi/fyniq/internal/tokens"
)

// ------------------------------------------------------------------ account recovery (SEC-35, rev 0.198)

// On the owner's decisions of 6 Nov 2026: each person keeps ten one-time recovery codes of 10 characters, kept only as
// keyed hashes; one who lost every authenticator recovers with one of them and a confirmation an Owner or Administrator
// gives after speaking to them; the recovery gives the session a second factor, ends the person's other sessions and
// tokens, replaces their codes and starts a 24-hour cooling-off (authz.HeldInCoolingOff). A person whose provider failed
// signs in with another one they linked (FR-ADM-08); one with an authenticator left confirms it's them with it.

// CoolingOff is how long an account cools off after its recovery (SEC-35's default).
const CoolingOff = 24 * time.Hour

// recoveryCodeScope is the keyed hash's scope for a person's recovery codes.
var recoveryCodeScope = []byte("fyniq:recovery-code")

// MyRecoveryCode is one of a person's recovery codes.
type MyRecoveryCode string

// Schema implements huma.SchemaProvider: a string, with an example.
func (MyRecoveryCode) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A recovery code, usable once: 10 characters.", Examples: []any{"K7MQ2-WXP4R"}}
}

// MyRecoveryCodes are a person's ten new recovery codes.
type MyRecoveryCodes struct {
	RecoveryCodes []MyRecoveryCode `json:"recovery_codes" minItems:"10" maxItems:"10" doc:"Ten recovery codes, each usable once. They are shown once: print them or keep them in a password manager. With an Owner's or Administrator's confirmation, one gives your account back if you lose every authenticator (SEC-35). Your earlier unused codes no longer work."`
}

// RecoveryCodeStatus says how many of a person's recovery codes are left.
type RecoveryCodeStatus struct {
	Unused    int        `json:"unused" doc:"How many of your recovery codes are unused: 0 until you make them." example:"10"`
	CreatedAt *time.Time `json:"created_at,omitempty" doc:"When you made them; absent until you do." example:"2026-11-07T09:00:00Z"`
}

// AccountRecoveryRequest gives back a person's account: one of their recovery codes and the confirmation an Owner or
// Administrator gave them.
type AccountRecoveryRequest struct {
	RecoveryCode     string `json:"recovery_code" minLength:"10" maxLength:"40" doc:"One of your recovery codes. Case, spaces and dashes do not matter." example:"K7MQ2-WXP4R"`
	ConfirmationCode string `json:"confirmation_code" minLength:"20" maxLength:"100" doc:"The confirmation an Owner or Administrator of your organisation gave you after speaking to you." example:"FGHIJ-KLMNO-PQRST-UVWXY-ZABCD-E"`
}

// RecoveredAccount is an account given back: until when it cools off, and its new recovery codes.
type RecoveredAccount struct {
	CoolingOffUntil time.Time `json:"cooling_off_until" doc:"Until when your account cools off: approving, releasing payments, changing bank details, roles or sign-ins, break-glass and confirming others' recoveries wait until then." example:"2026-11-08T09:00:00Z"`
	MyRecoveryCodes
}

type recoveryStatusOutput struct{ Body RecoveryCodeStatus }

type myRecoveryCodesOutput struct{ Body MyRecoveryCodes }

type recoverAccountInput struct{ Body AccountRecoveryRequest }

type recoveredAccountOutput struct{ Body RecoveredAccount }

// recoveryCodeHash is the keyed hash of a recovery code as typed: case, spaces and dashes do not matter.
func (a *API) recoveryCodeHash(ctx context.Context, typed string) ([]byte, error) {
	clean := strings.NewReplacer("-", "", " ", "", "\t", "").Replace(strings.ToUpper(strings.TrimSpace(typed)))
	return a.deps.Fields.Fingerprint(ctx, recoveryCodeScope, []byte(clean))
}

// newMyRecoveryCodes makes ten recovery codes of 10 base-32 characters (50 random bits each), in two groups of five,
// and their keyed hashes.
func (a *API) newMyRecoveryCodes(ctx context.Context) (MyRecoveryCodes, [][]byte, error) {
	out := MyRecoveryCodes{RecoveryCodes: make([]MyRecoveryCode, 0, 10)}
	var hashes [][]byte
	for range 10 {
		code := rand.Text()[:10]
		h, err := a.recoveryCodeHash(ctx, code)
		if err != nil {
			return MyRecoveryCodes{}, nil, err
		}
		out.RecoveryCodes, hashes = append(out.RecoveryCodes, MyRecoveryCode(code[:5]+"-"+code[5:])), append(hashes, h)
	}
	return out, hashes, nil
}

// recoveryPerson returns the caller when they are a person with a Fyniq user in a browser session.
func (a *API) recoveryPerson(ctx context.Context, what string) (authn.Principal, error) {
	p, err := signedInPerson(ctx, what)
	if err != nil {
		return p, err
	}
	if p.UserID == uuid.Nil || len(p.SessionHash) == 0 {
		return p, problem(CodeSessionRequired, what+" needs a person with a Fyniq user, signed in to Fyniq in the browser.",
			"Sign in to Fyniq in the browser and do it there.")
	}
	if a.deps.Fields == nil || a.deps.Installation == nil {
		return p, problem(CodeUnavailable, "Recovery codes are not configured in this deployment.", "Configure the field keys, then try again.")
	}
	return p, nil
}

func registerRecovery(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.GetMyRecoveryCodes, huma.Operation{
		OperationID: "getMyRecoveryCodes", Method: http.MethodGet, Path: "/v1/session/recovery-codes", Tags: []string{"Session"},
		Summary: "Say whether you keep recovery codes",
		Description: "How many of your recovery codes are unused, and when you made them (SEC-35); never the codes. " +
			authz.GetMyRecoveryCodes.Note + " Read-only; no side effects.",
	}, append([]string{CodeSessionRequired, CodeUnavailable}, locks...), func(ctx context.Context, _ *struct{}) (*recoveryStatusOutput, error) {
		p, err := a.recoveryPerson(ctx, "Reading your recovery codes")
		if err != nil {
			return nil, err
		}
		org, err := a.deps.Installation.InstallationOrganisation(ctx)
		if err != nil {
			return nil, err
		}
		var out RecoveryCodeStatus
		err = a.deps.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := u.Recoveries().Codes(ctx, p.UserID)
			out.Unused = c.Unused
			if !c.CreatedAt.IsZero() {
				t := c.CreatedAt
				out.CreatedAt = &t
			}
			return err
		})
		return &recoveryStatusOutput{Body: out}, err
	})

	register(a, authz.ReplaceMyRecoveryCodes, oneTimeSecret(huma.Operation{
		OperationID: "replaceMyRecoveryCodes", Method: http.MethodPost, Path: "/v1/session/recovery-codes", Tags: []string{"Session"},
		Summary: "Make your recovery codes",
		Description: "Gives you ten recovery codes, shown once; your earlier unused codes stop working (SEC-35). Keep them offline or in a " +
			"password manager: with an Owner's or Administrator's confirmation, one gives your account back if you lose every " +
			"authenticator. " + authz.ReplaceMyRecoveryCodes.Note + " Side effects: the codes, kept as keyed hashes only, and their event " +
			"in the installation's trail. It takes no Idempotency-Key: a response with codes is never stored.",
	}), append([]string{CodeSessionRequired, CodeStepUpRequired, CodeUnavailable, storage.CodeRecovery}, locks...),
		func(ctx context.Context, _ *struct{}) (*myRecoveryCodesOutput, error) {
			p, err := a.recoveryPerson(ctx, "Making your recovery codes")
			if err != nil {
				return nil, err
			}
			if !p.SteppedUp(a.deps.Clock.Now()) {
				return nil, problem(CodeStepUpRequired, "Making recovery codes needs a second factor in the last 5 minutes (SEC-35).",
					"Confirm it's you with your authenticator app or a passkey, then try again.")
			}
			codes, hashes, err := a.newMyRecoveryCodes(ctx)
			if err != nil {
				return nil, err
			}
			err = a.inInstallation(ctx, p.UserID, func(ctx context.Context, _ uuid.UUID, u storage.UnitOfWork) error {
				_, err := u.Recoveries().SetCodes(ctx, p.SessionHash, hashes, a.deps.Clock.Now())
				return err
			})
			if err != nil {
				return nil, err
			}
			return &myRecoveryCodesOutput{Body: codes}, nil
		})

	register(a, authz.ConfirmAccountRecovery, oneTimeSecret(huma.Operation{
		OperationID: "confirmAccountRecovery", Method: http.MethodPost, Path: "/v1/members/{member_id}/recovery-confirmations",
		Tags: []string{"Members"}, DefaultStatus: http.StatusCreated, Summary: "Confirm a member's account recovery",
		Description: "Creates the one-time confirmation that, with one of their recovery codes, gives a member who lost every " +
			"authenticator their account back: the second proof a recovery needs (SEC-35). Confirm only after speaking to them, in " +
			"person or on a call you started, and give them the code that way. " + authz.ConfirmAccountRecovery.Note + " Side effects: " +
			"the confirmation (it lasts 24 hours and replaces an unused one), its event in the organisation's trail, and a security notice " +
			"to the member and every Owner. It takes no Idempotency-Key: a response with a code is never stored.",
	}), append([]string{CodeSessionRequired, CodeStepUpRequired, CodeUnavailable, CodeNotFound, CodeInvalid, storage.CodeRecovery}, locks...),
		func(ctx context.Context, in *MemberPath) (*oneTimeCodeOutput, error) {
			p, err := signedInPerson(ctx, "Confirming a recovery")
			if err != nil {
				return nil, err
			}
			if p.UserID == uuid.Nil {
				return nil, problem(CodeSessionRequired, "Confirming a recovery needs a person with a Fyniq user.", "Sign in to Fyniq in the browser and do it there.")
			}
			now := a.deps.Clock.Now()
			if !p.SteppedUp(now) {
				return nil, problem(CodeStepUpRequired, "Confirming a recovery needs a second factor in the last 5 minutes (SEC-35).",
					"Confirm it's you with your authenticator app or a passkey, then try again.")
			}
			id, err := parseID("member_id", in.MemberID)
			if err != nil {
				return nil, err
			}
			code, hash := setupcode.New()
			out := OneTimeCode{Code: code, ExpiresAt: now.Add(ConfirmationTTL).UTC()}
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.Members().List(ctx, now)
				if err != nil {
					return err
				}
				var m *storage.Member
				for i := range all {
					if all[i].ID == id && all[i].Status == "active" {
						m = &all[i]
					}
				}
				if m == nil {
					return problem(CodeNotFound, "The organisation has no active member with that ID.", "List the members to find their IDs")
				}
				if m.UserID == p.UserID {
					return problem(storage.CodeRecovery, "Nobody confirms their own recovery.",
						"Ask another Owner or Administrator to confirm it, after speaking to you.")
				}
				if _, err := u.Recoveries().Confirm(ctx, p.UserID, m.UserID, p.Organisation, hash, now, out.ExpiresAt); err != nil {
					return err
				}
				if err := a.notify(ctx, u, notify.Event{Type: notify.RecoveryType, RecordType: "membership", RecordID: &m.ID, Link: "/settings/profile",
					Title:    "A recovery of your account was confirmed. If you did not ask for it, tell an Owner at once.",
					Audience: notify.Audience{Users: []uuid.UUID{m.UserID}}}); err != nil {
					return err
				}
				return a.notify(ctx, u, notify.Event{Type: notify.RecoveryType, RecordType: "membership", RecordID: &m.ID, Link: "/settings/members",
					Title:    "A recovery of " + m.DisplayName + "'s account was confirmed",
					Audience: notify.Audience{Capability: string(authz.UsersAndRoles), Need: "A", Except: []uuid.UUID{m.UserID}}})
			})
			if err != nil {
				return nil, err
			}
			return &oneTimeCodeOutput{Body: out}, nil
		})

	register(a, authz.RecoverAccount, oneTimeSecret(huma.Operation{
		OperationID: "recoverAccount", Method: http.MethodPost, Path: "/v1/session/recovery", Tags: []string{"Session"},
		Summary: "Recover your account",
		Description: "Gives your account back after you lost every authenticator, with two proofs (SEC-35): one of your recovery codes and " +
			"the confirmation an Owner or Administrator gave you. Your session passes its second factor by recovery, so you can add a new " +
			"passkey; your other sessions and tokens end; your recovery codes are replaced; and for 24 hours approving, releasing payments, " +
			"changing bank details, roles or sign-ins, break-glass and confirming others' recoveries wait. Email alone or a support request " +
			"never recovers an account. " + authz.RecoverAccount.Note + " Side effects: the recovery, ten new codes (shown once), the " +
			"revocation of your tokens (AI-38), events in the installation's trail, an entry in the weekly review and a security notice " +
			"to you and every Owner. Wrong, used or expired codes change nothing. It takes no Idempotency-Key: a response with codes is never stored.",
	}), append([]string{CodeSessionRequired, CodeUnavailable, CodeMalformed, CodeTooLarge, CodeMediaType, storage.CodeRecovery}, locks...),
		func(ctx context.Context, in *recoverAccountInput) (*recoveredAccountOutput, error) {
			p, err := a.recoveryPerson(ctx, "Recovering your account")
			if err != nil {
				return nil, err
			}
			if a.deps.Outbox == nil {
				return nil, problem(CodeUnavailable, "Recovery needs the outbox, which is not configured in this deployment.", "Configure the database, then try again.")
			}
			codeHash, err := a.recoveryCodeHash(ctx, in.Body.RecoveryCode)
			if err != nil {
				return nil, err
			}
			codes, hashes, err := a.newMyRecoveryCodes(ctx)
			if err != nil {
				return nil, err
			}
			now := a.deps.Clock.Now()
			var done storage.AccountRecovered
			err = a.inInstallation(ctx, p.UserID, func(ctx context.Context, org uuid.UUID, u storage.UnitOfWork) error {
				if done, err = u.Recoveries().Recover(ctx, storage.NewRecovery{SessionHash: p.SessionHash, CodeHash: codeHash,
					ConfirmationHash: setupcode.Hash(in.Body.ConfirmationCode), NewHashes: hashes, Now: now, CoolingOff: CoolingOff}); err != nil {
					return err
				}
				// AI-38: a recovery revokes every token of the person.
				if err := revoke.RevokeAll(ctx, *a.deps.Outbox, u, org, done.UserID.String(), "account.recovered"); err != nil {
					return err
				}
				return notify.Relay(ctx, *a.deps.Outbox, u, org, notify.Event{Type: notify.RecoveryType, RecordType: "user", RecordID: &done.UserID,
					Link: "/settings/profile", At: now, Title: "An account was recovered with a recovery code and a confirmation; it cools off until " +
						singapore(done.CoolingOffUntil) + " Singapore time. If it was not you, tell an Owner at once.",
					Audience: notify.UsersAndTheirOwners(done.UserID)})
			})
			if err != nil {
				return nil, err
			}
			// FM-3: the recovery enters the weekly review of the organisation the confirmation came from.
			err = a.deps.Store.Do(ctx, done.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				all, err := u.Members().List(ctx, now)
				if err != nil {
					return err
				}
				for _, m := range all {
					if m.UserID != done.UserID {
						continue
					}
					by := done.UserID
					_, err := u.ReviewLog().Log(ctx, storage.NewReviewLogEntry{Kind: storage.ReviewAccountRecovery, ObjectType: "membership",
						ObjectKey: m.ID.String(), Summary: m.DisplayName + " recovered their account on " + singapore(now) +
							" Singapore time, with a recovery code and a confirmation; it cools off for 24 hours", Actor: &by, LoggedAt: now})
					return err
				}
				return nil
			})
			if err != nil {
				a.deps.Log.Error("api.recovery.review_log", "error", err.Error(), "request_id", requestID(ctx))
			}
			return &recoveredAccountOutput{Body: RecoveredAccount{CoolingOffUntil: done.CoolingOffUntil, MyRecoveryCodes: codes}}, nil
		})
}
