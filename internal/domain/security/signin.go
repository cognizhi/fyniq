package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// SignInKind is the outbox kind of an admin's sign-in to check, run by the worker class: the worker reads the request
// details beside the chain (AUD-06), which the API's role does not.
const SignInKind = "security.sign_in"

// adminRoles are the roles whose sign-ins the rules watch: an Owner and an Administrator.
var adminRoles = []string{"owner", "administrator"}

type signInPayload struct {
	User uuid.UUID `json:"user"`
	At   time.Time `json:"at"`
}

// QueueSignInCheck is a sign-in alert (signin.Alert) that queues a check of the sign-in in its person's organisation,
// once the sign-in's event is in the trail: every sign-in is queued, and the worker leaves those of people who are not
// admins.
func QueueSignInCheck(sessions storage.Sessions, store storage.Store, d outbox.Dispatcher) func(ctx context.Context, who ports.SignedIn,
	session []byte, userAgent string, at time.Time) error {
	return func(ctx context.Context, _ ports.SignedIn, session []byte, _ string, at time.Time) error {
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
		b, err := json.Marshal(signInPayload{User: s.UserID, At: at.UTC()})
		if err != nil {
			return err
		}
		return store.Do(ctx, s.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := d.Enqueue(ctx, u, s.Organisation, storage.NewOutboxItem{Class: storage.ClassWorker, Kind: SignInKind, Payload: b})
			return err
		})
	}
}

// SignInWork is the worker's check of an admin's sign-in: from a new device, or impossible travel, each an alert.
func SignInWork(store storage.Store, r Raiser) outbox.Work {
	return func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		var p signInPayload
		if err := json.Unmarshal(row.Payload, &p); err != nil || p.User == uuid.Nil {
			return fmt.Errorf("security: outbox row %s is not a sign-in to check", row.ID)
		}
		return store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			name, admin, err := adminOf(ctx, u, p.User, r.Clock.Now())
			if err != nil || !admin {
				return err
			}
			current, history, err := signIns(ctx, u, p, r.Thresholds)
			if err != nil || current == nil {
				return err
			}
			facts := map[string]any{"sign_in_event": current.event.String()}
			if NewDevice(history, current.SignIn, r.Thresholds) {
				if _, err := r.Raise(ctx, u, org, Alert{Rule: RuleNewDevice, Subject: p.User, Link: "/settings/members", Facts: facts,
					Title: name + " signed in from a device not seen for them in " + fmt.Sprint(r.Thresholds.DeviceDays) + " days"}); err != nil {
					return err
				}
			}
			if ImpossibleTravel(history, current.SignIn, r.Thresholds) {
				if _, err := r.Raise(ctx, u, org, Alert{Rule: RuleImpossibleTravel, Subject: p.User, Link: "/settings/members", Facts: facts,
					Title: name + " signed in from another country sooner than travel allows after their last sign-in"}); err != nil {
					return err
				}
			}
			return nil
		})
	}
}

// adminOf is user's name, and whether they are an Owner or an Administrator of u's organisation now.
func adminOf(ctx context.Context, u storage.UnitOfWork, user uuid.UUID, now time.Time) (string, bool, error) {
	members, err := u.Members().List(ctx, now)
	if err != nil {
		return "", false, err
	}
	for _, m := range members {
		if m.UserID == user {
			return m.DisplayName, slices.ContainsFunc(m.Roles, func(ra storage.RoleAssignment) bool { return slices.Contains(adminRoles, ra.Role) }), nil
		}
	}
	return "", false, nil
}

type placed struct {
	SignIn
	event uuid.UUID
}

// signIns are the sign-in p checks, and the person's others of the DeviceDays before it, from the trail and the
// request details beside it; nil when the sign-in's event is not there.
func signIns(ctx context.Context, u storage.UnitOfWork, p signInPayload, t Thresholds) (*placed, []SignIn, error) {
	since := p.At.AddDate(0, 0, -t.DeviceDays-1)
	events, err := u.Audit().Find(ctx, storage.AuditQuery{Action: "session.started", ObjectType: "user", ObjectID: &p.User, From: &since,
		Limit: 1000})
	if err != nil || len(events) == 0 {
		return nil, nil, err
	}
	ids := make([]uuid.UUID, len(events))
	for i, e := range events {
		ids[i] = e.ID
	}
	details, err := u.Audit().Details(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	var current *placed
	var history []SignIn
	for _, e := range events {
		entry, err := audit.Read(e)
		if err != nil {
			return nil, nil, err
		}
		at, err := time.Parse(time.RFC3339Nano, entry.OccurredAt)
		if err != nil {
			return nil, nil, fmt.Errorf("security: event %d's time %q: %w", e.Seq, entry.OccurredAt, err)
		}
		d := details[e.ID]
		s := placed{SignIn: SignIn{At: at, Device: DeviceOf(d.UserAgent), Country: d.Country}, event: e.ID}
		if current == nil && !s.At.After(p.At.Add(time.Minute)) && !s.At.Before(p.At.Add(-time.Minute)) {
			current = &s
			continue
		}
		history = append(history, s.SignIn)
	}
	return current, history, nil
}

// MemberName is user's name in u's organisation now, or "A member" when they are none.
func MemberName(ctx context.Context, u storage.UnitOfWork, user uuid.UUID, now time.Time) (string, error) {
	name, _, err := adminOf(ctx, u, user, now)
	if name == "" {
		name = "A member"
	}
	return name, err
}

// ActionOf is a stored event's action.
func ActionOf(e storage.AuditEvent) (string, error) {
	entry, err := audit.Read(e)
	return entry.Action, err
}
