// Package alert is the pay-day path's failure alert (PRD-002 NM-NFR-05, NM-NFR-03): when work of the pay-day path dies (its attempts used up),
// the Payroll administrator and the Owner are told within an hour. A pay-day job's kind starts with "payday."; payroll registers its jobs
// under it. The alert is itself outbox work, one row per person written in the sweep's transaction, so it is not lost when the consumer is
// down: it waits, and goes out when the consumer comes back. How a person is told (email, in-app) is the Notifier's, which arrives with
// notifications; this package fixes who, when and that it is never lost or repeated.
package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/nemura/authz"
	"github.com/cognizhi/fyniq/internal/nemura/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
)

const (
	// PayDayPrefix starts the kind of every job of the pay-day path.
	PayDayPrefix = "payday."
	// KindNotify is the outbox kind of one person's notice.
	KindNotify = "alert.notify"
	// Window is how long after a pay-day job dies its alert may take to reach the person (NM-NFR-05).
	Window = time.Hour
	// Attempts is how often delivering a notice is tried before it is given up on and audited as dead.
	Attempts = 20
)

// Recipients are the roles that are told, as NM-NFR-05 names them.
var Recipients = []authz.Role{authz.PayrollAdmin, authz.Owner}

// The kinds of the link's alerts (PRD-001 FR-PPL-08, PRD-002 NM-INT-04): events for Fyniq that have waited or failed for an hour while a pay run waits for Fyniq's posting, and for 24 hours at any time.
const (
	KindLinkNearPayDay = PayDayPrefix + "link_events_stuck" // the 1-hour alert near pay day: a pay-day job in all but name
	KindLink24Hours    = "link.events_failing_24h"          // the 24-hour notice to the Owner and the Payroll administrator
)

// IsPayDay reports whether kind is a job of the pay-day path.
func IsPayDay(kind string) bool {
	return strings.HasPrefix(kind, PayDayPrefix) && len(kind) > len(PayDayPrefix)
}

// IsNotice reports whether kind is one a notice may carry: a pay-day job that died, or one of the link's alerts.
func IsNotice(kind string) bool { return IsPayDay(kind) || kind == KindLink24Hours }

// Payload is a notice: which person, which job died, and when. It holds no personal data, only identifiers and a kind.
type Payload struct {
	Member   uuid.UUID `json:"member"`
	Source   uuid.UUID `json:"source"`    // the dead outbox row
	Kind     string    `json:"kind"`      // the dead row's kind
	FailedAt time.Time `json:"failed_at"` // when it died
}

// DueBy is when the notice must have reached its person.
func (p Payload) DueBy() time.Time { return p.FailedAt.Add(Window) }

// Notifier tells one person (a Payroll administrator or the Owner) that a pay-day job died. A nil error means they were told; the
// outbox retries any other, and Late says the notice is past its window.
type Notifier interface {
	Notify(ctx context.Context, org uuid.UUID, p Payload, late bool) error
}

// LogNotifier is the notifier of the local build and of a deployment with no channel yet: an error line naming the organisation, the person's ID,
// the job's kind and whether it is late. It holds no personal data (SEC-15).
type LogNotifier struct{ Log *slog.Logger }

func (l LogNotifier) Notify(ctx context.Context, org uuid.UUID, p Payload, late bool) error {
	log := l.Log
	if log == nil {
		log = slog.Default()
	}
	log.ErrorContext(ctx, "payday.alert", "organisation", org.String(), "member", p.Member.String(), "kind", p.Kind, "source", p.Source.String(), "late", late)
	return nil
}

// Work is the outbox work of KindNotify: it reads the notice and tells the person.
func Work(n Notifier, clock ports.Clock) outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row outbox.Row) error {
		var p Payload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.Member == uuid.Nil || !IsNotice(p.Kind) {
			return fmt.Errorf("the alert row %s is not a notice: %v", row.ID, err)
		}
		return n.Notify(ctx, org, p, clock.Now().After(p.DueBy()))
	}
}
