package api

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/domain/compliance"
	"github.com/cognizhi/fyniq/internal/domain/security"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ security alerts (SEC-31)

// The API raises three of SEC-31's alerts as they happen (p1-security-hardening design D7): a vendor's bank details
// changing, a mass export (more audited reads than the threshold by one person in an hour) and repeated authorisation
// refusals (more than the threshold of one person in ten minutes). The worker raises the sign-in and audit-chain ones.

// refusalWindow is the window refusals are counted in.
const refusalWindow = 10 * time.Minute

// raiser is the API's alert raiser; it raises nothing without an outbox.
func (a *API) raiser() (security.Raiser, bool) {
	if a.deps.Outbox == nil {
		return security.Raiser{}, false
	}
	t := a.deps.AlertThresholds
	if t == (security.Thresholds{}) {
		t = security.Defaults
	}
	return security.Raiser{Dispatcher: *a.deps.Outbox, Clock: a.deps.Clock, Thresholds: t, Log: a.deps.Log}, true
}

// alert raises al in u, the organisation's unit of work.
func (a *API) alert(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, al security.Alert) error {
	r, ok := a.raiser()
	if !ok {
		return nil
	}
	_, err := r.Raise(ctx, u, org, al)
	return err
}

// afterRefusal counts a refusal of p by the policy layer, in its own unit of work, and raises the alert once p's
// window holds more than the threshold. A failure is logged: the refusal stands whatever happens here.
func (a *API) afterRefusal(ctx context.Context, p authn.Principal) {
	r, ok := a.raiser()
	if !ok || a.deps.Store == nil || p.UserID == uuid.Nil || p.Organisation == uuid.Nil {
		return
	}
	start := a.deps.Clock.Now().Truncate(refusalWindow)
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		n, err := u.RateLimits().Refused(ctx, p.UserID, start)
		if err != nil || n <= r.Thresholds.RefusalsPer10 {
			return err
		}
		name, err := security.MemberName(ctx, u, p.UserID, a.deps.Clock.Now())
		if err != nil {
			return err
		}
		_, err = r.Raise(ctx, u, p.Organisation, security.Alert{Rule: security.RuleRefusals, Subject: p.UserID, Link: "/compliance/audit-trail",
			Title: fmt.Sprintf("%s was refused more than %d times in 10 minutes", name, r.Thresholds.RefusalsPer10)})
		return err
	})
	if err != nil {
		a.deps.Log.Warn("api.alert.refusals", "error", err.Error(), "organisation", p.Organisation.String())
	}
}

// afterAuditedRead counts p's audited reads and exports of the last hour, after one, and raises the mass-export alert
// past the threshold. A failure is logged: the read happened whatever happens here.
func (a *API) afterAuditedRead(ctx context.Context, p authn.Principal) {
	r, ok := a.raiser()
	if !ok || a.deps.Store == nil || p.UserID == uuid.Nil || p.Organisation == uuid.Nil {
		return
	}
	now := a.deps.Clock.Now()
	since := now.Add(-time.Hour)
	err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Find(ctx, storage.AuditQuery{Actor: p.UserID.String(), From: &since, Limit: 1000})
		if err != nil {
			return err
		}
		reads := map[string]bool{}
		for _, action := range compliance.AuditedReads {
			reads[action] = true
		}
		n := 0
		for _, e := range events {
			if action, err := security.ActionOf(e); err == nil && reads[action] {
				n++
			}
		}
		if n <= r.Thresholds.ExportsPerHour {
			return nil
		}
		name, err := security.MemberName(ctx, u, p.UserID, now)
		if err != nil {
			return err
		}
		_, err = r.Raise(ctx, u, p.Organisation, security.Alert{Rule: security.RuleMassExport, Subject: p.UserID, Link: "/compliance/audit-trail",
			Title: fmt.Sprintf("%s exported or revealed more than %d times in an hour", name, r.Thresholds.ExportsPerHour)})
		return err
	})
	if err != nil {
		a.deps.Log.Warn("api.alert.exports", "error", err.Error(), "organisation", p.Organisation.String())
	}
}
