package signin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// AlertPurpose is the purpose of a sign-in alert's email (migration 000049).
const AlertPurpose = "sign_in_alert"

// EmailAlert emails a person each time they sign in, to the address their provider verified (FR-ADM-03): when, with
// which provider and from which browser, and what to do if it was not them. It is queued through the outbox in the
// installation's organisation, whose trail records it; before first-run setup, or for an address the provider did not
// verify, it sends nothing.
func EmailAlert(store storage.Store, installation storage.Installation, d outbox.Dispatcher) Alert {
	return func(ctx context.Context, who ports.SignedIn, userAgent string, at time.Time) error {
		if who.Email == "" || !who.EmailVerified {
			return nil
		}
		org, err := installation.InstallationOrganisation(ctx)
		if errors.Is(err, ports.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		browser := userAgent
		if browser == "" {
			browser = "a browser that did not say which"
		}
		if len([]rune(browser)) > 200 {
			browser = string([]rune(browser)[:200]) + "…"
		}
		text := fmt.Sprintf("You signed in to Fyniq with %s at %s UTC, from %s.\n\n"+
			"If this was you, there is nothing to do.\n\n"+
			"If it was not you, sign in to Fyniq, open your devices and end the session you do not know, then tell your "+
			"Owner or Super admin.\n", who.Provider, at.UTC().Format("2 Jan 2006 15:04"), browser)
		return store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := email.Queue(ctx, d, u, org, AlertPurpose, who.Email, "New sign-in to Fyniq", text)
			return err
		})
	}
}
