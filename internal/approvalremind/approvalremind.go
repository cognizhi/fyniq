// Package approvalremind reminds the people a pending approval waits for, and escalates it (PRD-001 FR-APR-08; p2-mobile-approvals-automation design D2). Hourly,
// for each organisation, a pending request that has waited the reminder time since it was asked for, or since its last reminder, reminds those who may sign its current step now
// (up to three reminders), and one that has waited the escalation time tells the Owners and the requester, after which it is reminded no more. The job only notifies: it signs nothing, decides nothing and
// never extends a request's expiry, and a log row per request, kind and number keeps a run that comes again from repeating one.
package approvalremind

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Job is the platform job's name (outbox.PlatformSchedules).
const Job = "approvals.remind"

// The settings that time reminders and the escalation, in hours.
const (
	ReminderSetting   = "approvals.reminder_hours"
	EscalationSetting = "approvals.escalate_hours"
)

// MaxReminders is how many reminders one request gets before it is escalated.
const MaxReminders = 3

// Actor is the principal the job records what it sends as.
var Actor = storage.Actor{Type: "system", ID: Job}

// Runner sends what is due.
type Runner struct {
	Store  storage.Store
	Outbox outbox.Dispatcher
	Clock  ports.Clock
}

// RunAll looks at every organisation and returns how many reminders and escalations it sent.
func (r Runner) RunAll(ctx context.Context) (int, error) {
	orgs, err := r.Store.Organisations(ctx)
	if err != nil {
		return 0, err
	}
	ctx = storage.WithAudit(ctx, storage.AuditContext{Actor: &Actor})
	total := 0
	for _, org := range orgs {
		n, err := r.RunOrganisation(ctx, org)
		total += n
		if err != nil {
			return total, fmt.Errorf("approvalremind: %s: %w", org, err)
		}
	}
	return total, nil
}

func hours(ctx context.Context, u storage.UnitOfWork, key string, now time.Time) (time.Duration, error) {
	d, ok := settings.Lookup(key)
	if !ok {
		return 0, fmt.Errorf("approvalremind: no setting %s", key)
	}
	changes, err := u.Settings().Changes(ctx, key)
	if err != nil {
		return 0, err
	}
	h, err := strconv.Atoi(settings.On(d, changes, now).Value)
	if err != nil {
		return 0, err
	}
	return time.Duration(h) * time.Hour, nil
}

// RunOrganisation sends one organisation's due reminders and escalations.
func (r Runner) RunOrganisation(ctx context.Context, org uuid.UUID) (int, error) {
	sent := 0
	err := r.Store.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
		now := r.Clock.Now()
		pending, err := u.Approvals().List(ctx, string(approval.Pending))
		if err != nil || len(pending) == 0 {
			return err
		}
		remindAfter, err := hours(ctx, u, ReminderSetting, now)
		if err != nil {
			return err
		}
		escalateAfter, err := hours(ctx, u, EscalationSetting, now)
		if err != nil {
			return err
		}
		members, err := u.Members().List(ctx, now)
		if err != nil {
			return err
		}
		delegations, err := u.ApprovalDelegations().List(ctx)
		if err != nil {
			return err
		}
		for _, req := range pending {
			if !now.Before(req.ExpiresAt) {
				continue // it expires; nobody is reminded of what can no longer be signed
			}
			log, err := u.ApprovalReminders().Sent(ctx, req.ID)
			if err != nil {
				return err
			}
			n, last, escalated := 0, req.RequestedAt, false
			for _, l := range log {
				if l.Kind == "reminder" {
					n++
				} else {
					escalated = true
				}
				if l.SentAt.After(last) {
					last = l.SentAt
				}
			}
			sigs, err := u.Approvals().Signatures(ctx, req.ID)
			if err != nil {
				return err
			}
			if !escalated && !now.Before(req.RequestedAt.Add(escalateAfter)) {
				if err := r.escalate(ctx, u, org, req, members, now); err != nil {
					return err
				}
				sent++
				continue
			}
			if escalated || n >= MaxReminders || now.Before(last.Add(remindAfter)) { // the escalation is the last word
				continue
			}
			who := Approvers(req, sigs, members, delegations, now)
			if len(who) == 0 {
				continue // nobody can sign it now: escalation reaches the Owners
			}
			if err := u.ApprovalReminders().Record(ctx, req.ID, "reminder", n+1, now); err != nil {
				return err
			}
			if _, err := notify.Emit(ctx, r.Outbox, u, org, notify.Event{Type: notify.ApprovalReminderType, RecordType: "approval_request", RecordID: &req.ID, At: now,
				Title: "Still waiting for your approval: " + req.Summary, Link: "/approvals/" + req.ID.String(), Audience: notify.Audience{Users: who}}); err != nil {
				return err
			}
			sent++
		}
		return nil
	})
	return sent, err
}

func (r Runner) escalate(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, req storage.ApprovalRequest, members []storage.Member, now time.Time) error {
	if err := u.ApprovalReminders().Record(ctx, req.ID, "escalation", 1, now); err != nil {
		return err
	}
	who := []uuid.UUID{req.RequestedBy}
	for _, m := range members {
		if m.Status != "active" {
			continue
		}
		if slices.ContainsFunc(m.Roles, func(a storage.RoleAssignment) bool { return a.Role == string(authz.Owner) }) && !slices.Contains(who, m.UserID) {
			who = append(who, m.UserID)
		}
	}
	_, err := notify.Emit(ctx, r.Outbox, u, org, notify.Event{Type: notify.ApprovalEscalatedType, RecordType: "approval_request", RecordID: &req.ID, At: now,
		Title: "Waiting a long time for approval: " + req.Summary, Link: "/approvals/" + req.ID.String(), Audience: notify.Audience{Users: who}})
	return err
}

// Approvers are the members who may sign the request's current step now: an active member, not the requester, who has not signed or been signed for, who holds the approval right on
// the object's row and whom the step names, or who stands in for one it names under a live delegation. A request of an object type with no approval row (an AI proposal) has none here.
func Approvers(req storage.ApprovalRequest, sigs []storage.ApprovalSignature, members []storage.Member, delegations []storage.ApprovalDelegation, now time.Time) []uuid.UUID {
	row, ok := approval.Rows[req.ObjectType]
	if !ok {
		return nil
	}
	steps := make([]approval.Step, len(req.PolicySteps))
	for i, s := range req.PolicySteps {
		steps[i] = approval.Step{Quorum: s.Quorum, Roles: s.Roles, Users: s.Users}
	}
	var live []approval.Signature
	for _, g := range sigs {
		if g.VoidedAt == nil {
			live = append(live, approval.Signature{Step: g.Step, Signer: g.Signer, Approve: g.Decision == "approve"})
		}
	}
	step := approval.CurrentStep(steps, live)
	if step == 0 {
		return nil
	}
	taken := func(who uuid.UUID) bool {
		return slices.ContainsFunc(sigs, func(s storage.ApprovalSignature) bool {
			return s.VoidedAt == nil && (s.Signer == who || s.OnBehalfOf != nil && *s.OnBehalfOf == who)
		})
	}
	holds := func(roles []string) bool {
		rs := make([]authz.Role, len(roles))
		for i, x := range roles {
			rs[i] = authz.Role(x)
		}
		return authz.Decide(authz.Policy{Operation: "approve", Capability: authz.Capability(row), Need: authz.Approve}, rs, false).Allowed
	}
	rolesOf := func(m storage.Member) []string {
		out := make([]string, len(m.Roles))
		for i, a := range m.Roles {
			out[i] = a.Role
		}
		return out
	}
	byUser := func(who uuid.UUID) (storage.Member, bool) {
		i := slices.IndexFunc(members, func(m storage.Member) bool { return m.UserID == who && m.Status == "active" })
		if i < 0 {
			return storage.Member{}, false
		}
		return members[i], true
	}
	var dels []approval.Delegation
	for _, d := range delegations {
		if d.Delegator != req.RequestedBy {
			dels = append(dels, approval.Delegation{ID: d.ID, Delegator: d.Delegator, Delegate: d.Delegate, StartsOn: d.StartsOn, EndsOn: d.EndsOn, Ended: d.Status == "ended"})
		}
	}
	y, mo, d := now.Date()
	today := time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
	var out []uuid.UUID
	for _, m := range members {
		if m.Status != "active" || m.UserID == req.RequestedBy || taken(m.UserID) {
			continue
		}
		roles := rolesOf(m)
		if !holds(roles) {
			continue
		}
		if approval.Eligible(steps[step-1], m.UserID, roles) {
			out = append(out, m.UserID)
			continue
		}
		roleOf := func(who uuid.UUID) ([]string, bool) {
			dm, ok := byUser(who)
			return rolesOf(dm), ok
		}
		if _, ok := approval.StandIn(steps[step-1], m.UserID, dels, today, taken, roleOf, holds); ok {
			out = append(out, m.UserID)
		}
	}
	return out
}
