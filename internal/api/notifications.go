package api

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ notifications (FR-NTF-01, FR-NTF-02, FR-NTF-05; rev 0.108)

// The notification codes.
const (
	CodeNotifyLocked = "NTF-01" // a security type is always on and emailed at once
	CodeNotifyTime   = "NTF-02" // not a time of day as HH:MM
	CodeNotifyType   = "NTF-03" // no such notification type
)

// notify emits e in u for the caller's organisation, at now unless e says when (FR-NTF-05); nothing without an outbox.
func (a *API) notify(ctx context.Context, u storage.UnitOfWork, e notify.Event) error {
	if a.deps.Outbox == nil {
		return nil
	}
	p, _ := authn.From(ctx)
	if e.At.IsZero() {
		e.At = a.deps.Clock.Now()
	}
	_, err := notify.Emit(ctx, *a.deps.Outbox, u, p.Organisation, e)
	return err
}

// Notification is something that happened, as the caller sees it in their centre.
type Notification struct {
	ID         string    `json:"id" format:"uuid" doc:"The notification's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f40"`
	Type       string    `json:"type" doc:"Its type: preferences list them." example:"approval.requested"`
	Label      string    `json:"label" doc:"What preferences call its type." example:"An approval waits for you"`
	Category   string    `json:"category" enum:"security,approvals,email,operations" doc:"Its type's group." example:"approvals"`
	Severity   string    `json:"severity" enum:"info,warning,critical" doc:"How much it matters." example:"info"`
	Title      string    `json:"title" doc:"What happened, in plain words." example:"Post journal JV-0042 waits for your approval"`
	Link       string    `json:"link,omitempty" doc:"Where in the web app it is about." example:"/approvals/01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f41"`
	Actionable bool      `json:"actionable" doc:"true when it links to something waiting for the caller in their Inbox." example:"true"`
	Read       bool      `json:"read" doc:"true once the caller read it." example:"false"`
	Archived   bool      `json:"archived" doc:"true once the caller archived it." example:"false"`
	CreatedAt  time.Time `json:"created_at" doc:"When it was made." example:"2026-10-24T01:00:00Z"`
}

// NotificationPage is a page of the caller's notifications, newest first, with their unread count.
type NotificationPage struct {
	Items      []Notification `json:"items" nullable:"false" doc:"The notifications, newest first."`
	Unread     int            `json:"unread" doc:"How many of the caller's notifications are unread and not archived: the bell's count." example:"3"`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjAxOTI4ZjZlIn0"`
}

// NotificationTypePreference is how the caller gets a notification type.
type NotificationTypePreference struct {
	Type        string `json:"type" doc:"The type." example:"approval.requested"`
	Label       string `json:"label" doc:"What it is." example:"An approval waits for you"`
	Category    string `json:"category" enum:"security,approvals,email,operations" doc:"Its group." example:"approvals"`
	Email       string `json:"email" enum:"immediate,daily,weekly,off" doc:"How it goes by email: at once, in the daily or weekly digest (08:00 Singapore time, weekly on Mondays), or not at all. In the app it always shows." example:"immediate"`
	Recommended string `json:"recommended" enum:"immediate,daily,weekly,off" doc:"How it goes by email unless the caller chooses otherwise." example:"immediate"`
	Locked      bool   `json:"locked" doc:"true for security notices, which are always on and go by email as email says, whatever the caller chooses." example:"false"`
	Note        string `json:"note,omitempty" doc:"Why a locked type goes by email as it does, where that needs saying; absent otherwise." example:"Every sign-in is emailed at once by the sign-in alert, so it is not emailed again here"`
	ETag        string `json:"etag" doc:"The preference in force: send it in If-Match to change it." example:"\"none\""`
}

// QuietHours is when immediate email waits, Singapore time.
type QuietHours struct {
	From string `json:"from" doc:"When they start, as HH:MM." example:"22:00"`
	To   string `json:"to" doc:"When they end, as HH:MM; before From when they run past midnight." example:"07:00"`
}

// NotificationPreferences are the caller's preferences.
type NotificationPreferences struct {
	Types          []NotificationTypePreference `json:"types" doc:"Every notification type, in groups."`
	QuietHours     *QuietHours                  `json:"quiet_hours,omitempty" doc:"When immediate email waits until they end; security notices never wait. Absent when there are none."`
	QuietHoursETag string                       `json:"quiet_hours_etag" doc:"Send it in If-Match to change the quiet hours." example:"\"none\""`
}

// EmailPreferenceChange chooses how a type goes by email.
type EmailPreferenceChange struct {
	Email string `json:"email" enum:"immediate,daily,weekly,off" doc:"At once, in the daily or weekly digest, or not by email." example:"daily"`
}

// QuietHoursChange sets quiet hours, or leaves both out to have none.
type QuietHoursChange struct {
	From string `json:"from,omitempty" maxLength:"5" doc:"When they start, as HH:MM." example:"22:00"`
	To   string `json:"to,omitempty" maxLength:"5" doc:"When they end, as HH:MM." example:"07:00"`
}

// NotificationPath names a notification.
type NotificationPath struct {
	NotificationID string `path:"notification_id" format:"uuid" doc:"The notification's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4f40"`
}

// NotificationTypePath names a notification type.
type NotificationTypePath struct {
	Type string `path:"type" maxLength:"60" doc:"The notification type." example:"approval.requested"`
}

// MarkedAll is how many notifications were marked read.
type MarkedAll struct {
	Marked int `json:"marked" doc:"How many were unread." example:"3"`
}

type notificationsInput struct {
	PageQuery
	Unread   bool `query:"unread" doc:"true for unread ones only." example:"false"`
	Archived bool `query:"archived" doc:"true for archived ones instead." example:"false"`
}

type notificationActionInput struct {
	NotificationPath
	KeyHeader
	PreferHeader
}

type markAllInput struct {
	KeyHeader
	PreferHeader
}

type emailPreferenceInput struct {
	NotificationTypePath
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body EmailPreferenceChange
}

type quietHoursInput struct {
	KeyHeader
	IfMatchHeader
	PreferHeader
	Body QuietHoursChange
}

type notificationsOutput struct{ Body NotificationPage }

type notificationOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     Notification
}

type markAllOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     MarkedAll
}

type preferencesOutput struct {
	Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
	Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
	Body     NotificationPreferences
}

type preferencesReadOutput struct{ Body NotificationPreferences }

func notificationOf(n storage.Notification) Notification {
	t, _ := notify.Lookup(n.Type)
	return Notification{ID: n.ID.String(), Type: n.Type, Label: t.Label, Category: t.Category, Severity: n.Severity, Title: n.Title, Link: n.Link,
		Actionable: n.Actionable, Read: n.ReadAt != nil, Archived: n.ArchivedAt != nil, CreatedAt: n.CreatedAt.UTC()}
}

func preferenceETag(p *storage.NotificationPreference) string {
	if p == nil {
		return etag("none")
	}
	return etag(p.SetAt.UnixMicro())
}

func preferenceOf(prefs []storage.NotificationPreference, t string) *storage.NotificationPreference {
	for i := range prefs {
		if prefs[i].Type == t {
			return &prefs[i]
		}
	}
	return nil
}

func hhmm(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

var timeOfDay = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

func minutesOf(field, v string) (int, error) {
	m := timeOfDay.FindStringSubmatch(v)
	if m == nil {
		return 0, problem(CodeNotifyTime, fmt.Sprintf("%q is not a time of day.", v), "Give it as HH:MM, such as 22:00",
			FieldProblem{CodeNotifyTime, "body." + field, "not HH:MM", "Give it as HH:MM, such as 22:00"})
	}
	h, _ := strconv.Atoi(m[1])
	mm, _ := strconv.Atoi(m[2])
	return h*60 + mm, nil
}

// preferencesOf are user's preferences as the API shows them.
func preferencesOf(prefs []storage.NotificationPreference) NotificationPreferences {
	out := NotificationPreferences{Types: []NotificationTypePreference{}, QuietHoursETag: preferenceETag(preferenceOf(prefs, storage.QuietHours))}
	for _, t := range notify.Catalogue {
		out.Types = append(out.Types, NotificationTypePreference{Type: t.Key, Label: t.Label, Category: t.Category, Email: notify.Cadence(t, prefs),
			Recommended: t.Email, Locked: t.Locked, Note: t.Note, ETag: preferenceETag(preferenceOf(prefs, t.Key))})
	}
	if q := preferenceOf(prefs, storage.QuietHours); q != nil && q.QuietFrom != nil && q.QuietTo != nil {
		out.QuietHours = &QuietHours{From: hhmm(*q.QuietFrom), To: hhmm(*q.QuietTo)}
	}
	return out
}

// person is the caller's user, or a problem when the caller is not a person.
func person(ctx context.Context) (uuid.UUID, error) {
	p, _ := authn.From(ctx)
	if p.UserID == uuid.Nil || p.Class != authn.ClassUser {
		return uuid.Nil, problem(CodeCaptureNoUser, "Only a person who is a member has notifications of their own.", "Sign in as yourself")
	}
	return p.UserID, nil
}

func registerNotifications(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	actions := append([]string{CodeNotFound, CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeCaptureNoUser}, locks...)

	register(a, authz.ListNotifications, huma.Operation{
		OperationID: "listNotifications", Method: http.MethodGet, Path: "/v1/notifications", Tags: []string{"Notifications"},
		Summary: "List your notifications",
		Description: "The caller's notifications, newest first, with the bell's unread count (FR-NTF-01): what happened, each with its type, " +
			"severity, the record it is about and a link; an actionable one links to its Inbox item, where what waits for the caller is. " +
			"Archived ones only with archived=true. Empty for a caller who is not a person. Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *notificationsInput) (*notificationsOutput, error) {
		p, _ := authn.From(ctx)
		out := NotificationPage{Items: []Notification{}}
		if p.UserID == uuid.Nil {
			return &notificationsOutput{Body: out}, nil
		}
		f := storage.NotificationFilter{UserID: p.UserID, Unread: in.Unread, Archived: in.Archived, Limit: in.PageSize + 1}
		if in.Cursor != "" {
			after, err := decodeCursor(in.Cursor)
			if err != nil {
				return nil, err
			}
			if f.Before, err = uuid.Parse(after); err != nil {
				return nil, badCursor()
			}
		}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			ns, err := u.Notifications().List(ctx, f)
			if err != nil {
				return err
			}
			if len(ns) > in.PageSize {
				ns = ns[:in.PageSize]
				out.NextCursor = encodeCursor(ns[len(ns)-1].ID.String())
			}
			for _, n := range ns {
				out.Items = append(out.Items, notificationOf(n))
			}
			out.Unread, err = u.Notifications().Unread(ctx, p.UserID)
			return err
		})
		return &notificationsOutput{Body: out}, err
	})

	act := func(op authz.Policy, id, path, summary, description string, do func(ctx context.Context, u storage.UnitOfWork, n, user uuid.UUID, at time.Time) (storage.Notification, error)) {
		register(a, op, huma.Operation{OperationID: id, Method: http.MethodPost, Path: path, Tags: []string{"Notifications"}, Summary: summary,
			Description: description + " Side effects: the notification changes for the caller alone. Idempotent with Idempotency-Key; " +
				"Prefer: return=preview checks it and saves nothing."}, actions,
			func(ctx context.Context, in *notificationActionInput) (*notificationOutput, error) {
				user, err := person(ctx)
				if err != nil {
					return nil, err
				}
				nid, err := parseID("notification_id", in.NotificationID)
				if err != nil {
					return nil, err
				}
				w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (Notification, error) {
					n, err := u.Notifications().Get(ctx, nid)
					if err == nil && n.UserID != user {
						return Notification{}, problem(CodeNotFound, "No notification of yours has that ID.", "List your notifications")
					}
					if err != nil {
						return Notification{}, err
					}
					n, err = do(ctx, u, nid, user, a.deps.Clock.Now())
					return notificationOf(n), err
				})
				if err != nil {
					return nil, err
				}
				return &notificationOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
			})
	}
	act(authz.ReadNotification, "readNotification", "/v1/notifications/{notification_id}/read", "Mark a notification read",
		"Marks one of the caller's notifications read (FR-NTF-01).",
		func(ctx context.Context, u storage.UnitOfWork, n, user uuid.UUID, at time.Time) (storage.Notification, error) {
			return u.Notifications().Mark(ctx, n, user, true, at)
		})
	act(authz.UnreadNotification, "unreadNotification", "/v1/notifications/{notification_id}/unread", "Mark a notification unread",
		"Marks one of the caller's notifications unread again (FR-NTF-01).",
		func(ctx context.Context, u storage.UnitOfWork, n, user uuid.UUID, at time.Time) (storage.Notification, error) {
			return u.Notifications().Mark(ctx, n, user, false, at)
		})
	act(authz.ArchiveNotification, "archiveNotification", "/v1/notifications/{notification_id}/archive", "Archive a notification",
		"Archives one of the caller's notifications, for good: it leaves the list and the count (FR-NTF-01).",
		func(ctx context.Context, u storage.UnitOfWork, n, user uuid.UUID, at time.Time) (storage.Notification, error) {
			return u.Notifications().Archive(ctx, n, user, at)
		})

	register(a, authz.ReadAllNotifications, huma.Operation{
		OperationID: "readAllNotifications", Method: http.MethodPost, Path: "/v1/notifications/read-all", Tags: []string{"Notifications"},
		Summary:     "Mark all your notifications read",
		Description: "Marks every unread notification of the caller read (FR-NTF-01). Side effects: their notifications change for them alone. Idempotent with Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeCaptureNoUser}, locks...),
		func(ctx context.Context, in *markAllInput) (*markAllOutput, error) {
			user, err := person(ctx)
			if err != nil {
				return nil, err
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (MarkedAll, error) {
				n, err := u.Notifications().MarkAllRead(ctx, user, a.deps.Clock.Now())
				return MarkedAll{Marked: n}, err
			})
			if err != nil {
				return nil, err
			}
			return &markAllOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
		})

	register(a, authz.GetNotificationPreferences, huma.Operation{
		OperationID: "getNotificationPreferences", Method: http.MethodGet, Path: "/v1/notification-preferences", Tags: []string{"Notifications"},
		Summary: "Get your notification preferences",
		Description: "How the caller gets each notification type by email (FR-NTF-02; the owner's decisions of 24 Oct 2026): at once for security " +
			"and approvals, in the daily digest at 08:00 Singapore time for the rest, unless they chose otherwise; in the app always. Security " +
			"notices are locked on. And their quiet hours. The recommended values for a caller who is not a person. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*preferencesReadOutput, error) {
		p, _ := authn.From(ctx)
		if p.UserID == uuid.Nil {
			return &preferencesReadOutput{Body: preferencesOf(nil)}, nil
		}
		var out NotificationPreferences
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			prefs, err := u.Notifications().Preferences(ctx, p.UserID)
			out = preferencesOf(prefs)
			return err
		})
		return &preferencesReadOutput{Body: out}, err
	})

	writes := append([]string{CodeStale, CodeIfMatchRequired, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid, CodeIdempotencyReused,
		CodeIdempotencyBusy, CodeIdempotencyOld, CodeCaptureNoUser}, locks...)
	register(a, authz.SetNotificationPreference, huma.Operation{
		OperationID: "setNotificationPreference", Method: http.MethodPut, Path: "/v1/notification-preferences/{type}", Tags: []string{"Notifications"},
		Summary: "Choose how a notification type is emailed",
		Description: "Chooses how a notification type goes to the caller by email: at once, in the daily or weekly digest, or not at all " +
			"(FR-NTF-02); in the app it always shows. Refused for a security notice, which is locked on (NTF-01), and for a type that does not " +
			"exist (NTF-03). Side effects: the preference and its audit event. Needs If-Match with the type's etag. Idempotent with " +
			"Idempotency-Key; Prefer: return=preview checks it and saves nothing.",
	}, append(writes, CodeNotifyLocked, CodeNotifyType), func(ctx context.Context, in *emailPreferenceInput) (*preferencesOutput, error) {
		user, err := person(ctx)
		if err != nil {
			return nil, err
		}
		t, ok := notify.Lookup(in.Type)
		if !ok {
			return nil, problem(CodeNotifyType, fmt.Sprintf("There is no notification type %q.", in.Type), "Choose one of the types your preferences list")
		}
		if t.Locked {
			return nil, problem(CodeNotifyLocked, t.Label+" is a security notice: it is always on and emailed at once.", "Nothing to change")
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (NotificationPreferences, error) {
			prefs, err := u.Notifications().Preferences(ctx, user)
			if err != nil {
				return NotificationPreferences{}, err
			}
			if err := checkIfMatch(in.IfMatch, preferenceETag(preferenceOf(prefs, t.Key))); err != nil {
				return NotificationPreferences{}, err
			}
			if _, err := u.Notifications().Prefer(ctx, storage.NotificationPreference{UserID: user, Type: t.Key, Email: in.Body.Email}); err != nil {
				return NotificationPreferences{}, err
			}
			prefs, err = u.Notifications().Preferences(ctx, user)
			return preferencesOf(prefs), err
		})
		if err != nil {
			return nil, err
		}
		return &preferencesOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})

	register(a, authz.SetQuietHours, huma.Operation{
		OperationID: "setQuietHours", Method: http.MethodPut, Path: "/v1/notification-quiet-hours", Tags: []string{"Notifications"},
		Summary: "Set your quiet hours",
		Description: "Sets when the caller's immediate email waits until the hours end, Singapore time, or clears them when both are left out " +
			"(FR-NTF-02); security notices never wait. Times are HH:MM (NTF-02); hours that end before they start run past midnight. Side " +
			"effects: the preference and its audit event. Needs If-Match with quiet_hours_etag. Idempotent with Idempotency-Key; Prefer: " +
			"return=preview checks it and saves nothing.",
	}, append(writes, CodeNotifyTime), func(ctx context.Context, in *quietHoursInput) (*preferencesOutput, error) {
		user, err := person(ctx)
		if err != nil {
			return nil, err
		}
		pref := storage.NotificationPreference{UserID: user, Type: storage.QuietHours}
		if in.Body.From != "" || in.Body.To != "" {
			from, err := minutesOf("from", in.Body.From)
			if err != nil {
				return nil, err
			}
			to, err := minutesOf("to", in.Body.To)
			if err != nil {
				return nil, err
			}
			pref.QuietFrom, pref.QuietTo = &from, &to
		}
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (NotificationPreferences, error) {
			prefs, err := u.Notifications().Preferences(ctx, user)
			if err != nil {
				return NotificationPreferences{}, err
			}
			if err := checkIfMatch(in.IfMatch, preferenceETag(preferenceOf(prefs, storage.QuietHours))); err != nil {
				return NotificationPreferences{}, err
			}
			if _, err := u.Notifications().Prefer(ctx, pref); err != nil {
				return NotificationPreferences{}, err
			}
			prefs, err = u.Notifications().Preferences(ctx, user)
			return preferencesOf(prefs), err
		})
		if err != nil {
			return nil, err
		}
		return &preferencesOutput{Replayed: replayedHeader(w.Replayed), Applied: applied(w), Body: w.Body}, nil
	})
}
