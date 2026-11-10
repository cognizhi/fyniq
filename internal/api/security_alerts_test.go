//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/security"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// alertsOf counts the alerts of rule in e's trail.
func alertsOf(t *testing.T, e *env, rule string) int {
	t.Helper()
	n := 0
	for _, ev := range strings.Split(auditTrail(t, e), "\n") {
		if strings.Contains(ev, `"action":"`+security.ActionAlerted+`"`) && strings.Contains(ev, `"`+rule+`"`) {
			n++
		}
	}
	return n
}

// Repeated refusals of one person and a mass export each raise one alert past their threshold, and no more within the
// window (SEC-31): an audit event of the platform, and the log entry Cloud Monitoring alerts on.
// req: SEC-31
// asvs: V16.3.1, V16.3.2
func TestRefusalsAndMassExportsRaiseAlerts(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	e.api.deps.AlertThresholds = security.Thresholds{DeviceDays: 90, TravelNearHours: 2, TravelFarHours: 6, ExportsPerHour: 2, RefusalsPer10: 3,
		AlertWindowMins: 60}
	mei := c.as(c.mei, authz.Approver, authz.Employee)
	for i := range 6 {
		if r := e.call(http.MethodGet, "/v1/members/invitations", mei, nil); r.Status != http.StatusForbidden {
			t.Fatalf("refusal %d: %d %s", i+1, r.Status, r.Raw)
		}
		if want := map[bool]int{true: 1, false: 0}[i >= 3]; alertsOf(t, e, security.RuleRefusals) != want {
			t.Fatalf("after %d refusals: %d alerts, want %d", i+1, alertsOf(t, e, security.RuleRefusals), want)
		}
	}
	_, file := matrixBillEvidence(e)
	weiling := c.as(c.weiling, authz.Owner, authz.Controller)
	for i := range 4 {
		if r := e.call(http.MethodGet, "/v1/evidence/"+file+"/file", weiling, nil); r.Status != http.StatusOK {
			t.Fatalf("opening %d: %d %s", i+1, r.Status, r.Raw)
		}
	}
	if n := alertsOf(t, e, security.RuleMassExport); n != 1 {
		t.Fatalf("a mass export raised %d alerts, want 1", n)
	}
	if !strings.Contains(e.log.String(), security.LogEvent) {
		t.Error("no log entry for Cloud Monitoring")
	}
}

// An admin's sign-in from a device none of their sign-ins of the last 90 days came from, or from another country
// sooner than travel allows, is an alert, checked by the worker from the details beside the chain; a first sign-in, the
// same device and country, and a member who is no admin are not (SEC-31).
// req: SEC-31
// asvs: V6.3.5, V16.3.1
func TestAnAdminsSignInIsChecked(t *testing.T) {
	c := newClaimants(t)
	e := c.e
	r := security.Raiser{Dispatcher: outbox.Dispatcher{Store: e.store, Queue: local.NewMemory(), Clock: e.clock}, Clock: e.clock, Thresholds: security.Defaults}
	work := security.SignInWork(workerStore(t), r)
	// The sign-ins are recorded on the test's clock, as the API's store records them on the system's in a deployment.
	api, err := postgres.Open(context.Background(), db.URL("fyniq_api"), postgres.WithClock(e.clock))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	const mac, phone = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) Firefox/140.0", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_1) Safari/604.1"
	signIn := func(user string, after time.Duration, agent, country string) {
		t.Helper()
		e.clock.Advance(after)
		ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: user},
			Request: storage.Request{IP: "203.0.113.7", UserAgent: agent, Country: country}})
		if err := api.Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.Audit().Record(ctx, storage.AuditRecord{Action: "session.started", ObjectType: "user", ObjectID: uuid.MustParse(user)})
		}); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(map[string]any{"user": user, "at": e.clock.Now().UTC()})
		if err := work(context.Background(), e.books.Org, storage.OutboxRow{ID: uuid.New(), Kind: security.SignInKind, Payload: b}); err != nil {
			t.Fatal(err)
		}
	}
	signIn(c.weiling, 0, mac, "SG")         // her first: no alert
	signIn(c.weiling, time.Hour, mac, "SG") // the same device and country
	if alertsOf(t, e, security.RuleNewDevice)+alertsOf(t, e, security.RuleImpossibleTravel) != 0 {
		t.Fatal("an alert for a first sign-in, or the same device and country")
	}
	signIn(c.weiling, time.Hour, phone, "GB") // a phone, from London an hour after Singapore
	if alertsOf(t, e, security.RuleNewDevice) != 1 || alertsOf(t, e, security.RuleImpossibleTravel) != 1 {
		t.Fatalf("new device %d, impossible travel %d; want 1 each", alertsOf(t, e, security.RuleNewDevice), alertsOf(t, e, security.RuleImpossibleTravel))
	}
	signIn(c.mei, 0, mac, "SG") // Mei is no admin
	signIn(c.mei, time.Minute, phone, "GB")
	if alertsOf(t, e, security.RuleNewDevice) != 1 || alertsOf(t, e, security.RuleImpossibleTravel) != 1 {
		t.Fatal("a member who is no admin raised an alert")
	}
}
