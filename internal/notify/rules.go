package notify

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/domain/automation"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Workflow rules without code (FR-AUT-03; p2-mobile-approvals-automation design D5). A rule is evaluated where the event is emitted, in the unit of work of what happened, and tells
// people through the same notification event and outbox fan-out as everything else, so a fan-out that fails comes round again and a posting is never held for a rule. The one action
// is to tell members: a rule cannot post, approve, pay, file or send anything outside Fyniq. A rule's own notification is not a trigger, so rules cannot set each other off.

// RuleType is the notification a rule sends.
const RuleType = "automation.rule"

// RulesSetting is the organisation's switch for workflow rules: off until an Owner turns it on with a second approver.
const RulesSetting = "features.automation_rules"

func init() {
	Catalogue = append(Catalogue, Type{Key: RuleType, Label: "A workflow rule of your organisation fired", Category: Operations, Severity: "info", Actionable: true, Email: storage.EmailDaily})
}

// RulesOn reports whether workflow rules are on for the organisation on day now.
func RulesOn(ctx context.Context, u storage.UnitOfWork, now time.Time) (bool, error) {
	d, ok := settings.Lookup(RulesSetting)
	if !ok {
		return false, fmt.Errorf("notify: no setting %s", RulesSetting)
	}
	changes, err := u.Settings().Changes(ctx, RulesSetting)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(settings.On(d, changes, now).Value)
}

// DocumentKinds are the document types a rule may watch, in order: those whose postings tell people (kinds).
func DocumentKinds() []string {
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, string(k))
	}
	slices.Sort(out)
	return out
}

// debitsBase is an entry's debits in the base currency.
func debitsBase(entry storage.Entry) (*apd.Decimal, error) {
	sum := apd.New(0, 0)
	ctx := apd.BaseContext.WithPrecision(100)
	for _, l := range entry.Lines {
		d, _, err := apd.NewFromString(string(l.BaseAmount))
		if err != nil {
			return nil, err
		}
		if d.Sign() > 0 {
			if _, err := ctx.Add(sum, sum, d); err != nil {
				return nil, err
			}
		}
	}
	return sum, nil
}

// fireRules tells the members the active rules name when doc's posting meets their conditions: each one who is an active member holding the right to view the document's row (the
// recipient's right is checked here, never trusted from the rule), and never the person who posted it. It does nothing while the feature is off.
func fireRules(ctx context.Context, d outbox.Dispatcher, u storage.UnitOfWork, org, actor uuid.UUID, doc storage.Document, entry storage.Entry, what, link string) error {
	now := d.Clock.Now()
	on, err := RulesOn(ctx, u, now)
	if err != nil || !on {
		return err
	}
	rules, err := u.AutomationRules().Active(ctx, automation.TriggerDocumentPosted)
	if err != nil || len(rules) == 0 {
		return err
	}
	total, err := debitsBase(entry)
	if err != nil {
		return err
	}
	members, err := u.Members().List(ctx, now)
	if err != nil {
		return err
	}
	row := authz.Capability(approval.Rows[kinds[doc.Type].object])
	for _, r := range rules {
		cond := automation.Rule{DocumentType: r.DocumentType}
		if r.AmountAtLeast != "" {
			if cond.AmountAtLeast, _, err = apd.NewFromString(string(r.AmountAtLeast)); err != nil {
				return err
			}
		}
		if !automation.Matches(cond, automation.Posted{DocumentType: string(doc.Type), AmountBase: total}) {
			continue
		}
		var who []uuid.UUID
		for _, m := range members {
			if m.Status != "active" || m.UserID == actor || !slices.Contains(r.Recipients, m.UserID) {
				continue
			}
			roles := make([]authz.Role, len(m.Roles))
			for i, a := range m.Roles {
				roles[i] = authz.Role(a.Role)
			}
			if authz.Decide(authz.Policy{Operation: "rule", Capability: row, Need: authz.View}, roles, false).Allowed {
				who = append(who, m.UserID)
			}
		}
		if len(who) == 0 {
			continue
		}
		if _, err := Emit(ctx, d, u, org, Event{Type: RuleType, RecordType: "document", RecordID: &doc.ID, At: now, Link: link,
			Title: "Rule “" + r.Name + "”: " + what + " was posted", Audience: Audience{Users: who}}); err != nil {
			return err
		}
	}
	return nil
}
