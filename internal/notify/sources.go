package notify

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/maildns"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The events of the security and email types (PRD-001 rev 0.108), emitted where they happen: through a hook where
// that package cannot import this one (email), or by these functions.

// emailAdmins are those who act on the organisation's email: Create on Organisation integrations (the Administrator).
var emailAdmins = Audience{Capability: string(authz.Integrations), Need: "C"}

// SignedIn tells user in org that they signed in with provider at at; the sign-in alert has emailed it already.
func SignedIn(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, user uuid.UUID, provider string, at time.Time) error {
	_, err := Emit(ctx, d, u, org, Event{Type: "security.sign_in", RecordType: "user", RecordID: &user, At: at, Link: "/settings/profile",
		Title:    fmt.Sprintf("You signed in with %s on %s Singapore time", provider, at.In(appclock.Singapore).Format("2 Jan 2006 at 15:04")),
		Audience: Audience{Users: []uuid.UUID{user}}})
	return err
}

// SecondFactor tells user in org that the passkey named name was added (or removed).
func SecondFactor(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, user uuid.UUID, name string, added bool, at time.Time) error {
	title := "A passkey was added to your account: " + name
	if !added {
		title = "A passkey was removed from your account: " + name
	}
	_, err := Emit(ctx, d, u, org, Event{Type: "security.second_factor", RecordType: "user", RecordID: &user, Title: title + ". If it was not you, " +
		"sign out your other devices and tell your Owner.", Link: "/settings/profile", Audience: Audience{Users: []uuid.UUID{user}}, At: at})
	return err
}

// Quarantined is the email-in fetcher's hook (email.Fetcher.Quarantined): mail at an address waits for a decision about
// its sender, by the member at their own address, else by those who decide the organisation's.
func Quarantined(d outbox.Dispatcher) func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, m storage.InboundMessage, addr storage.CaptureAddress, at time.Time) error {
	return func(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, m storage.InboundMessage, addr storage.CaptureAddress, at time.Time) error {
		a := emailAdmins
		if addr.UserID != nil {
			a = Audience{Users: []uuid.UUID{*addr.UserID}}
		}
		_, err := Emit(ctx, d, u, org, Event{Type: "email.quarantined", RecordType: "inbound_message", RecordID: &m.ID, Link: "/inbox", At: at,
			Title: fmt.Sprintf("Mail from %s to %s@ waits until someone accepts or rejects its sender", m.Sender, addr.LocalPart), Audience: a})
		return err
	}
}

// reasons say why an address was suppressed, in plain words.
var reasons = map[string]string{storage.EventHardBounce: "it bounced", storage.EventComplaint: "its owner marked our email as spam",
	storage.EventSuppressed: "the email provider stopped sending to it"}

// Suppressed tells those who release suppressions that the organisation's email to s's address stopped.
func Suppressed(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, s storage.EmailSuppression) error {
	why := reasons[s.Reason]
	if why == "" {
		why = s.Reason
	}
	_, err := Emit(ctx, d, u, org, Event{Type: "email.suppressed", RecordType: "email_suppression", RecordID: &s.ID, Link: "/settings", At: s.SuppressedAt,
		Title: fmt.Sprintf("Email to %s stopped because %s; release it once the address works", s.Address, why), Audience: emailAdmins})
	return err
}

// Installation finds the installation's organisation (storage.Installation).
type Installation interface {
	InstallationOrganisation(ctx context.Context) (uuid.UUID, error)
}

// DomainChecks tells those who act on the installation organisation's email of each sending domain that failed the
// nightly check; before first-run setup it tells nobody.
func DomainChecks(ctx context.Context, store storage.Store, inst Installation, d outbox.Dispatcher, results []maildns.Result, at time.Time) error {
	org, err := inst.InstallationOrganisation(ctx)
	if errors.Is(err, ports.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, r := range results {
			if r.OK {
				continue
			}
			if _, err := Emit(ctx, d, u, org, Event{Type: "email.domain_failed", RecordType: "sending_domain", Link: "/settings", At: at,
				Title: "The sending domain " + r.Domain + " failed its nightly check: its records or DMARC policy need fixing", Audience: emailAdmins}); err != nil {
				return err
			}
		}
		return nil
	})
}
