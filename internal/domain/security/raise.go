package security

import (
	"context"
	"log/slog"
	"slices"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/audit"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ActionAlerted is the audit event of an alert.
const ActionAlerted = "security.alerted"

// LogEvent is the structured log entry of an alert, which a log-based metric and an alert policy watch in Cloud
// Monitoring (infra, [live-verify]).
const LogEvent = "fyniq.security_alert"

// Alert is one alert of a rule.
type Alert struct {
	Rule    string
	Subject uuid.UUID      // whom or what it is about: a person, or a record
	Title   string         // plain words for the Owners and the DPO, no secret and no full identifier
	Link    string         // where in the web app to look
	Facts   map[string]any // what the audit event keeps beside the rule
}

// Raiser raises alerts in an organisation's unit of work.
type Raiser struct {
	Dispatcher outbox.Dispatcher
	Clock      ports.Clock
	Thresholds Thresholds
	Log        *slog.Logger
	// Installation names the alerts of the installation's own organisation: they go to the Super admins.
	Installation bool
}

// alertRoles are who an organisation's alert goes to: its Owners and its DPO.
var alertRoles = []string{"owner", "dpo"}

// AI raises the AI red team's pattern hit about subject (SEC-31): the hook p1-ai-assistant calls when an AI request
// matches a red-team pattern (AI-18…25), with what the hit was in title and facts; never the prompt's text.
func (r Raiser) AI(ctx context.Context, u storage.UnitOfWork, org, subject uuid.UUID, title string, facts map[string]any) (bool, error) {
	return r.Raise(ctx, u, org, Alert{Rule: RuleAI, Subject: subject, Title: title, Link: "/settings/ai", Facts: facts})
}

// Raise raises a in org's unit of work u, and reports whether it did: not when an alert of the same rule about the same
// subject stands within the window. It records the audit event, sends the locked critical notice to the Owners and the
// DPO (the Super admins for the installation) and logs the entry Cloud Monitoring alerts on.
func (r Raiser) Raise(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, a Alert) (bool, error) {
	// The platform raises it, whoever's request set it off: the event's actor is the store's own service identity.
	ctx = storage.WithAudit(ctx, storage.AuditContext{Reason: "SEC-31: " + a.Rule})
	now := r.Clock.Now()
	since := now.Add(-r.Thresholds.Window())
	prior, err := u.Audit().Find(ctx, storage.AuditQuery{Action: ActionAlerted, ObjectID: &a.Subject, From: &since, Limit: 100})
	if err != nil {
		return false, err
	}
	for _, e := range prior {
		entry, err := audit.Read(e)
		if err != nil {
			return false, err
		}
		for _, c := range entry.Changes {
			if c.Field == "rule" && c.After == a.Rule {
				return false, nil
			}
		}
	}
	changes := map[string][2]any{"rule": {nil, a.Rule}, "title": {nil, a.Title}}
	for k, v := range a.Facts {
		changes[k] = [2]any{nil, v}
	}
	if err := u.Audit().Record(ctx, storage.AuditRecord{Action: ActionAlerted, ObjectType: "security_alert", ObjectID: a.Subject,
		Changes: changes}); err != nil {
		return false, err
	}
	audience := notify.SuperAdmins()
	if !r.Installation {
		members, err := u.Members().List(ctx, now)
		if err != nil {
			return false, err
		}
		var to []uuid.UUID
		for _, m := range members {
			if slices.ContainsFunc(m.Roles, func(ra storage.RoleAssignment) bool { return slices.Contains(alertRoles, ra.Role) }) {
				to = append(to, m.UserID)
			}
		}
		audience = notify.Audience{Users: to}
	}
	if err := notify.Relay(ctx, r.Dispatcher, u, org, notify.Event{Type: notify.SecurityAlertType, RecordType: "security_alert",
		RecordID: &a.Subject, Title: a.Title, Link: a.Link, Audience: audience, At: now}); err != nil {
		return false, err
	}
	if r.Log != nil {
		r.Log.Warn(LogEvent, "event", LogEvent, "rule", a.Rule, "organisation", org.String(), "subject", a.Subject.String())
	}
	return true, nil
}
