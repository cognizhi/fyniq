package signin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/clientip"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A session's start and end are recorded in its person's organisation's trail (PRD-001 §11.5 L2: logins) as security
// events: the IP address and device they came from are kept beside the chain and erased after the security-log
// retention (AUD-06, the owner's decision of rev 0.230). A person in no organisation yet is recorded in none.
const (
	ActionSignedIn  = "session.started"
	ActionSignedOut = "session.ended"
	// ActionEmailMatched is a sign-in by another identity with a member's verified email (FR-ADM-08, G4-061): it linked
	// and reached nothing, and is recorded against the member in each of their organisations.
	ActionEmailMatched = "session.email_matched"
)

// SignedOut is told that the session whose token hash is session ends, and why, before it does.
type SignedOut func(ctx context.Context, session []byte, reason string, at time.Time) error

// Why a session ends in its event.
const (
	EndedBySignOut   = "signed_out"
	EndedByNewSignIn = "replaced_by_a_new_sign_in"
)

// Hooks are what the flow tells others: the sign-in alerts, a linked sign-in, and a session's end. Each may be nil.
type Hooks struct {
	Alert     Alert
	Linked    LinkAlert
	SignedOut SignedOut
}

// client is where a sign-in request came from. It travels in its own context value, read only by the audit hooks
// here, so no other event of the sign-in carries an IP address into its body, where it could never be erased.
type client struct{ ip, userAgent, country, region string }

type clientKey struct{}

func withClient(r *http.Request) context.Context {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	c := client{ip: host, userAgent: r.UserAgent()}
	if from, ok := clientip.From(r.Context()); ok && from.IP.IsValid() { // the edge's view: the client, its country and region
		c.ip, c.country, c.region = from.IP.String(), from.Country, from.Region
	}
	return context.WithValue(r.Context(), clientKey{}, c)
}

// as is ctx for writing user's event, from where the request came.
func as(ctx context.Context, user uuid.UUID) context.Context {
	c, _ := ctx.Value(clientKey{}).(client)
	return storage.WithAudit(ctx, storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: user.String()},
		Request: storage.Request{IP: c.ip, UserAgent: c.userAgent, Country: c.country, Region: c.region}})
}

// AuditSignIn records a sign-in in the trail of the organisation of the person who signed in.
func AuditSignIn(sessions storage.Sessions, store storage.Store) Alert {
	return func(ctx context.Context, who ports.SignedIn, session []byte, _ string, at time.Time) error {
		return record(ctx, sessions, store, session, at, ActionSignedIn, map[string][2]any{
			"provider": {nil, who.Provider}, "second_factor": {nil, who.SecondFactor}})
	}
}

// AuditSignOut records a session's end in the trail of its person's organisation.
func AuditSignOut(sessions storage.Sessions, store storage.Store) SignedOut {
	return func(ctx context.Context, session []byte, reason string, at time.Time) error {
		return record(ctx, sessions, store, session, at, ActionSignedOut, map[string][2]any{"reason": {nil, reason}})
	}
}

func record(ctx context.Context, sessions storage.Sessions, store storage.Store, session []byte, at time.Time, action string,
	changes map[string][2]any) error {
	s, err := sessions.Session(ctx, session, at, authn.SessionIdle)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.UserID == uuid.Nil || s.Organisation == uuid.Nil {
		return nil
	}
	return store.Do(as(ctx, s.UserID), s.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		return u.Audit().Record(ctx, storage.AuditRecord{Action: action, ObjectType: "user", ObjectID: s.UserID, Changes: changes})
	})
}

// AuditEmailMatch records a sign-in by an identity with no Fyniq user whose verified email is a member's: in the trail
// of each organisation the member belongs to, against the member, it linked nothing and reached nothing, since only a
// link from the member's own session after step-up adds a sign-in (FR-ADM-08, G4-061). The actor is the identity,
// named by its provider and a hash of its issuer and subject, never the subject itself.
func AuditEmailMatch(sessions storage.Sessions, store storage.Store) Alert {
	return func(ctx context.Context, who ports.SignedIn, session []byte, _ string, at time.Time) error {
		if who.Email == "" || !who.EmailVerified {
			return nil
		}
		s, err := sessions.Session(ctx, session, at, authn.SessionIdle)
		if errors.Is(err, ports.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if s.UserID != uuid.Nil {
			return nil // the person's own sign-in, recorded as theirs (AuditSignIn)
		}
		matches, err := sessions.EmailMatches(ctx, who.Email, who.Issuer, who.Subject)
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(who.Issuer + "\x00" + who.Subject))
		actor := storage.Actor{Type: "identity", ID: who.Provider + ":" + hex.EncodeToString(sum[:8])}
		c, _ := ctx.Value(clientKey{}).(client)
		audited := storage.WithAudit(ctx, storage.AuditContext{Actor: &actor,
			Request: storage.Request{IP: c.ip, UserAgent: c.userAgent, Country: c.country, Region: c.region}})
		for _, m := range matches {
			if err := store.Do(audited, m.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				return u.Audit().Record(ctx, storage.AuditRecord{Action: ActionEmailMatched, ObjectType: "user", ObjectID: m.User,
					Changes: map[string][2]any{"provider": {nil, who.Provider}, "linked": {nil, false}}})
			}); err != nil {
				return err
			}
		}
		return nil
	}
}
