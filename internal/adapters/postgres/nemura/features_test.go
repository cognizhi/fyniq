//go:build local

package nemura_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/nemura"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/nemura/api"
	"github.com/cognizhi/fyniq/internal/nemura/features"
)

type openWork struct {
	items map[string][]features.Outstanding
}

func (o openWork) Outstanding(_ context.Context, feature string) ([]features.Outstanding, error) {
	return o.items[feature], nil
}

func featureCode(err error) string {
	var r *features.Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

// A feature switch is a row of the organisation: no row is off, switching keeps every record and audits the change, the dependencies and the
// open work are checked before it is changed, a refused switch changes nothing, one organisation's switches never reach another, and a switch is
// never removed or renamed (NM-FEAT-01 to NM-FEAT-05).
// req: NM-FEAT-01, NM-FEAT-04
// asvs: V8.2.1
func TestAFeatureIsSwitchedWithItsDependenciesAndOpenWorkChecked(t *testing.T) {
	ctx := context.Background()
	c := newCompany(t)
	other := newCompany(t)
	work := openWork{items: map[string][]features.Outstanding{"leave": {{Feature: "leave", What: "leave requests waiting for approval", Count: 2}}}}
	svc := nemura.FeatureService{Store: store(t, "nemura_api"), Clock: clock.NewFrozen(today.Add(9 * time.Hour)), Outstanders: []features.Outstander{work}}
	s := api.Session{Organisation: c.org, Member: c.owner}

	cur, err := svc.List(ctx, s)
	if err != nil || len(cur) != 0 {
		t.Fatalf("a new organisation has no switches: %v %v", cur, err)
	}
	if err := svc.Set(ctx, s, "leave", features.On, "", nil); err != nil {
		t.Fatal(err)
	}
	// Payroll held by another system needs leave; handing leave to another system while payroll relies on it is allowed, off is not.
	if err := svc.Set(ctx, s, "payroll", features.External, "Acme Payroll", nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.Set(ctx, s, "leave", features.Off, "", []features.Arrangement{{What: "leave requests waiting for approval", Arrangement: "x"}}); featureCode(err) != features.CodeRequired {
		t.Errorf("leave off under payroll: %v", err)
	}
	if err := svc.Set(ctx, s, "payroll", features.On, "", nil); featureCode(err) != features.CodeUnavailable {
		t.Errorf("payroll on: %v", err)
	}
	// Open work needs an arrangement for each item, recorded in the audit event; a refusal changes nothing.
	if err := svc.Set(ctx, s, "payroll", features.Off, "", nil); err != nil {
		t.Fatal(err) // payroll has no open work
	}
	if err := svc.Set(ctx, s, "leave", features.Off, "", nil); featureCode(err) != features.CodeObligations {
		t.Errorf("leave off with open work and no arrangement: %v", err)
	}
	if cur, _ := svc.List(ctx, s); cur["leave"].State != features.On {
		t.Errorf("a refused switch changed the state: %v", cur["leave"])
	}
	if err := svc.Set(ctx, s, "leave", features.Off, "", []features.Arrangement{{What: "leave requests waiting for approval", Arrangement: "Approved in the old system until 31 Jan"}}); err != nil {
		t.Fatal(err)
	}
	var body string
	if err := connect(t, db.SuperURL()).QueryRow(ctx, "SELECT body FROM nemura.audit_event WHERE organisation_id = $1 AND body LIKE '%Approved in the old system%'", c.org).Scan(&body); err != nil || !strings.Contains(body, "nemura.feature.changed") {
		t.Errorf("the arrangement is not in the audit event: %v", err)
	}
	if cur, _ := svc.List(ctx, s); cur["leave"].State != features.Off || cur["payroll"].State != features.Off {
		t.Errorf("the switches: %v", cur)
	}
	// Another organisation never sees these, the database refuses a rename and a removal, and an external feature names its system below the code.
	if cur, _ := svc.List(ctx, api.Session{Organisation: other.org, Member: other.owner}); len(cur) != 0 {
		t.Errorf("another organisation sees %v", cur)
	}
	super := connect(t, db.SuperURL())
	for _, sql := range []string{"UPDATE nemura.feature_state SET feature = 'letters'", "DELETE FROM nemura.feature_state"} {
		if _, err := super.Exec(ctx, sql); err == nil || !strings.Contains(err.Error(), "NM125") {
			t.Errorf("%s: %v", sql, err)
		}
	}
	if _, err := super.Exec(ctx, "UPDATE nemura.feature_state SET state = 'external', external_system = NULL WHERE feature = 'leave'"); err == nil || !strings.Contains(err.Error(), "feature_state_external_named") {
		t.Errorf("an external feature with no system: %v", err)
	}
	if _, err := store(t, "nemura_worker").VerifyAudit(ctx, c.org); err != nil {
		t.Errorf("the chain: %v", err)
	}
}
