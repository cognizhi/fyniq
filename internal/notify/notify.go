// Package notify produces notifications from one event stream (PRD-001 FR-NTF-01, FR-NTF-02, FR-NTF-05; the owner's
// decisions of 24 Oct 2026, rev 0.108). Something that happened is emitted once, in the unit of work of what happened,
// as a notification event; the worker fans it out to each person it concerns as a notification in their centre, and by
// email at once or in their daily or weekly digest, as their preference for its type says. Security types are always
// on and emailed at once; quiet hours hold other immediate email until they end.
package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/email"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The categories of notification types, as preferences group them.
const (
	Security   = "security"
	Approvals  = "approvals"
	Email      = "email"
	Operations = "operations"
)

// Type is a kind of notification: what preferences call it, how severe it is, whether it links to an Inbox item, and
// how it goes by email unless a person chooses otherwise. A locked type (security) is never turned off, and goes by
// email as it says, whatever the person chose: at once, or not at all where another channel already sends it.
type Type struct {
	Key        string
	Label      string
	Category   string
	Severity   string // info, warning or critical
	Actionable bool
	Email      string // the recommended email delivery: storage.Email…; a locked type's, fixed
	Locked     bool
	Note       string // why a locked type goes by email as it does, where that needs saying
}

// Catalogue is every notification type, on the owner's decisions of 24 Oct 2026: approvals and security emailed at
// once, the rest in the daily digest; in-app always. More types join as their events are emitted.
var Catalogue = []Type{
	{Key: "security.sign_in", Label: "You signed in", Category: Security, Severity: "info", Email: storage.EmailOff, Locked: true,
		Note: "Every sign-in is emailed at once by the sign-in alert, so it is not emailed again here"},
	{Key: "security.second_factor", Label: "A passkey was added or removed", Category: Security, Severity: "warning", Email: storage.EmailImmediate, Locked: true},
	{Key: "security.roles", Label: "Your roles changed", Category: Security, Severity: "warning", Email: storage.EmailImmediate, Locked: true},
	{Key: "approval.requested", Label: "An approval waits for you", Category: Approvals, Severity: "info", Actionable: true, Email: storage.EmailImmediate},
	{Key: "approval.decided", Label: "Your request was decided", Category: Approvals, Severity: "info", Email: storage.EmailImmediate},
	{Key: "email.quarantined", Label: "Mail waits in quarantine", Category: Email, Severity: "info", Actionable: true, Email: storage.EmailDaily},
	{Key: "email.suppressed", Label: "Email to an address stopped", Category: Email, Severity: "warning", Email: storage.EmailDaily},
	{Key: "email.domain_failed", Label: "A sending domain failed its check", Category: Email, Severity: "warning", Email: storage.EmailDaily},
	{Key: "ledger.posted", Label: "Someone else posted or voided a document", Category: Operations, Severity: "info", Email: storage.EmailDaily},
	{Key: "period.closed", Label: "A month was closed", Category: Operations, Severity: "info", Email: storage.EmailDaily},
	{Key: "period.reopened", Label: "A closed month was reopened", Category: Operations, Severity: "warning", Email: storage.EmailDaily},
	{Key: "recurring.made", Label: "A recurring journal or bill made a draft", Category: Operations, Severity: "info", Actionable: true, Email: storage.EmailDaily},
	{Key: "bills.due", Label: "Bills to pay soon", Category: Operations, Severity: "info", Actionable: true, Email: storage.EmailDaily},
	{Key: "jobs.failed", Label: "A background job failed", Category: Operations, Severity: "warning", Email: storage.EmailDaily},
}

// Lookup is the type with key.
func Lookup(key string) (Type, bool) {
	i := slices.IndexFunc(Catalogue, func(t Type) bool { return t.Key == key })
	if i < 0 {
		return Type{}, false
	}
	return Catalogue[i], true
}

// Audience is whom an event concerns: these users, and the members whose roles hold one of Need's rights on the
// Appendix D row Capability, except these.
type Audience struct {
	Users      []uuid.UUID `json:"users,omitempty"`
	Capability string      `json:"capability,omitempty"`
	Need       string      `json:"need,omitempty"` // the rights, any of which will do: "A", "CA", "P"…
	Except     []uuid.UUID `json:"except,omitempty"`
}

// Event is something that happened, to notify.
type Event struct {
	Type       string
	RecordType string
	RecordID   *uuid.UUID
	Title      string // plain words for the audience, who may see what it is about; email carries it only under DetailsSetting
	Link       string // a path in the web app
	Audience   Audience
	At         time.Time
}

// FanoutKind is the outbox kind of fanning an event out, run by the worker class.
const FanoutKind = "notify.fanout"

// DigestJob is the hourly platform job that sends held and digested email (outbox.PlatformSchedules).
const DigestJob = "notify.digest"

// DetailsSetting is the organisation's switch for notification email to carry each notification's title; off, as
// recommended (the owner's decision of 24 Oct 2026), the email names only its type and links to Fyniq, so amounts,
// names and reasons stay in the app.
const DetailsSetting = "notify.email_details"

// details is the organisation's DetailsSetting on day now.
func details(ctx context.Context, u storage.UnitOfWork, now time.Time) (bool, error) {
	d, ok := settings.Lookup(DetailsSetting)
	if !ok {
		return false, fmt.Errorf("notify: no setting %s", DetailsSetting)
	}
	changes, err := u.Settings().Changes(ctx, DetailsSetting)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(settings.On(d, changes, now).Value)
}

// subject is what an email names n by: its title with details, else its type's label.
func subject(n storage.Notification, details bool) string {
	if details {
		return n.Title
	}
	if t, ok := Lookup(n.Type); ok {
		return t.Label
	}
	return "Something happened in Fyniq"
}

// maxTitle is the longest title an event keeps (migration 000105); a longer one, such as an approval's with a long
// reason, is cut short, never refused.
const maxTitle = 300

type fanoutPayload struct {
	EventID uuid.UUID `json:"event_id"`
}

// Emit records e in the unit of work u and queues its fan-out.
func Emit(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org uuid.UUID, e Event) (storage.NotificationEvent, error) {
	if _, ok := Lookup(e.Type); !ok {
		return storage.NotificationEvent{}, fmt.Errorf("notify: no notification type %s", e.Type)
	}
	audience, err := json.Marshal(e.Audience)
	if err != nil {
		return storage.NotificationEvent{}, err
	}
	if r := []rune(e.Title); len(r) > maxTitle {
		e.Title = string(r[:maxTitle-1]) + "…"
	}
	ev, err := u.Notifications().Emit(ctx, storage.NewNotificationEvent{Type: e.Type, RecordType: e.RecordType, RecordID: e.RecordID,
		Title: e.Title, Link: e.Link, Audience: audience, OccurredAt: e.At})
	if err != nil {
		return ev, err
	}
	b, err := json.Marshal(fanoutPayload{EventID: ev.ID})
	if err != nil {
		return ev, err
	}
	_, err = d.Enqueue(ctx, u, org, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: FanoutKind, Payload: b, BusinessKey: "notify:" + ev.ID.String()})
	return ev, err
}

// Recipients are the active members a's audience names, in the order they joined.
func Recipients(a Audience, members []storage.Member) []uuid.UUID {
	var out []uuid.UUID
	for _, m := range members {
		if m.Status != "active" || slices.Contains(a.Except, m.UserID) {
			continue
		}
		in := slices.Contains(a.Users, m.UserID)
		if !in && a.Capability != "" {
			var roles []authz.Role
			for _, r := range m.Roles {
				roles = append(roles, authz.Role(r.Role))
			}
			for i := range len(a.Need) {
				in = in || authz.Decide(authz.Policy{Operation: "notify", Capability: authz.Capability(a.Capability), Need: authz.Right(a.Need[i])}, roles, false).Allowed
			}
		}
		if in {
			out = append(out, m.UserID)
		}
	}
	return out
}

// Cadence is how a notification of t goes by email to a person with preferences prefs: their choice for t, else t's
// recommendation; a locked type as it says, whatever they chose.
func Cadence(t Type, prefs []storage.NotificationPreference) string {
	if t.Locked {
		return t.Email
	}
	for _, p := range prefs {
		if p.Type == t.Key && p.Email != "" {
			return p.Email
		}
	}
	return t.Email
}

// Quiet reports whether at is within a person's quiet hours, Singapore time; hours that end before they start run past
// midnight.
func Quiet(prefs []storage.NotificationPreference, at time.Time) bool {
	for _, p := range prefs {
		if p.Type != storage.QuietHours || p.QuietFrom == nil || p.QuietTo == nil {
			continue
		}
		local := at.In(appclock.Singapore)
		m := local.Hour()*60 + local.Minute()
		from, to := *p.QuietFrom, *p.QuietTo
		if from <= to {
			return m >= from && m < to
		}
		return m >= from || m < to
	}
	return false
}

// Fanout fans each event out to the people it concerns.
type Fanout struct {
	Store      storage.Store
	Dispatcher outbox.Dispatcher
	Clock      ports.Clock
	Origin     string // the web app's origin, for the links in emails
}

// Work is the worker's handler of FanoutKind: an event fanned out before is left as it is; each person it concerns gets
// a notification once; one going at once by email, outside their quiet hours or of a locked type, is queued now; the
// rest wait for the digest job. A person with no verified email gets it in-app only.
func (f Fanout) Work() outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p fanoutPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.EventID == uuid.Nil {
			return fmt.Errorf("notify: outbox row %s is not a fan-out", row.ID)
		}
		now := f.Clock.Now()
		return f.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			ev, err := u.Notifications().Event(ctx, p.EventID)
			if err != nil || ev.FannedOutAt != nil {
				return err
			}
			t, ok := Lookup(ev.Type)
			if !ok {
				return fmt.Errorf("notify: event %s has no type %s", ev.ID, ev.Type)
			}
			var a Audience
			if err := json.Unmarshal(ev.Audience, &a); err != nil {
				return err
			}
			members, err := u.Members().List(ctx, now)
			if err != nil {
				return err
			}
			emails, err := u.InboundMessages().MemberEmails(ctx)
			if err != nil {
				return err
			}
			detailed, err := details(ctx, u, now)
			if err != nil {
				return err
			}
			for _, user := range Recipients(a, members) {
				prefs, err := u.Notifications().Preferences(ctx, user)
				if err != nil {
					return err
				}
				cadence := Cadence(t, prefs)
				if emails[user] == "" {
					cadence = storage.EmailOff
				}
				n, created, err := u.Notifications().Add(ctx, storage.NewNotification{EventID: ev.ID, UserID: user, Type: t.Key, Severity: t.Severity,
					Actionable: t.Actionable, Email: cadence})
				if err != nil {
					return err
				}
				if !created || cadence != storage.EmailImmediate || (!t.Locked && Quiet(prefs, now)) {
					continue
				}
				m, err := email.Queue(ctx, f.Dispatcher, u, org, "notification", emails[user], subject(n, detailed), f.text([]storage.Notification{n}, detailed))
				if err != nil {
					return err
				}
				if err := u.Notifications().Emailed(ctx, n.ID, m.ID); err != nil {
					return err
				}
			}
			return u.Notifications().FannedOut(ctx, ev.ID, now)
		})
	}
}

// text is the plain-text body of an email carrying ns, each by its title with details, else by its type's label.
func (f Fanout) text(ns []storage.Notification, details bool) string {
	var b strings.Builder
	for _, n := range ns {
		b.WriteString("- " + subject(n, details) + "\n")
		if n.Link != "" {
			b.WriteString("  " + strings.TrimSuffix(f.Origin, "/") + n.Link + "\n")
		}
	}
	b.WriteString("\nChange what you receive, and when, in your notification preferences in Fyniq.\n")
	return b.String()
}

// due reports whether a held notification of cadence, made at created, is due at now: immediate ones as soon as the
// person's quiet hours end; daily ones made before today's 08:00 Singapore time, once it has passed; weekly ones made
// before this Monday's 08:00.
func due(cadence string, created, now time.Time) bool {
	local := now.In(appclock.Singapore)
	eight := time.Date(local.Year(), local.Month(), local.Day(), 8, 0, 0, 0, appclock.Singapore)
	switch cadence {
	case storage.EmailDaily:
		return !now.Before(eight) && created.Before(eight)
	case storage.EmailWeekly:
		monday := eight.AddDate(0, 0, -((int(local.Weekday()) + 6) % 7))
		return !now.Before(monday) && created.Before(monday)
	}
	return true
}

// Digest sends every organisation's held email: an immediate one held in quiet hours once they end, and each person's
// daily or weekly digest when it is due, one email for all its notifications.
func (f Fanout) Digest(ctx context.Context) (int, error) {
	orgs, err := f.Store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	now := f.Clock.Now()
	var sent int
	var errs []error
	for _, org := range orgs {
		err := f.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			emails, err := u.InboundMessages().MemberEmails(ctx)
			if err != nil {
				return err
			}
			detailed, err := details(ctx, u, now)
			if err != nil {
				return err
			}
			for _, cadence := range []string{storage.EmailImmediate, storage.EmailDaily, storage.EmailWeekly} {
				held, err := u.Notifications().Held(ctx, cadence)
				if err != nil {
					return err
				}
				byUser := map[uuid.UUID][]storage.Notification{}
				var users []uuid.UUID
				for _, n := range held {
					if !due(cadence, n.CreatedAt, now) || emails[n.UserID] == "" {
						continue
					}
					if _, seen := byUser[n.UserID]; !seen {
						users = append(users, n.UserID)
					}
					byUser[n.UserID] = append(byUser[n.UserID], n)
				}
				for _, user := range users {
					ns := byUser[user]
					if cadence == storage.EmailImmediate {
						prefs, err := u.Notifications().Preferences(ctx, user)
						if err != nil {
							return err
						}
						if Quiet(prefs, now) {
							continue
						}
						for _, n := range ns {
							m, err := email.Queue(ctx, f.Dispatcher, u, org, "notification", emails[user], subject(n, detailed), f.text([]storage.Notification{n}, detailed))
							if err != nil {
								return err
							}
							if err := u.Notifications().Emailed(ctx, n.ID, m.ID); err != nil {
								return err
							}
							sent++
						}
						continue
					}
					heading := fmt.Sprintf("Your Fyniq %s digest: %d notifications", cadence, len(ns))
					if len(ns) == 1 {
						heading = fmt.Sprintf("Your Fyniq %s digest: 1 notification", cadence)
					}
					m, err := email.Queue(ctx, f.Dispatcher, u, org, "digest", emails[user], heading, f.text(ns, detailed))
					if err != nil {
						return err
					}
					ids := make([]uuid.UUID, 0, len(ns))
					for _, n := range ns {
						ids = append(ids, n.ID)
					}
					if err := u.Notifications().Digested(ctx, user, cadence, m.ID, ids, now); err != nil {
						return err
					}
					sent++
				}
			}
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("organisation %s: %w", org, err))
		}
	}
	return sent, errors.Join(errs...)
}
