//go:build local

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/connectors"
	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/setupcode"
)

// acceptingEmail is a provider that accepts every message.
type acceptingEmail struct{ name string }

func (a acceptingEmail) Name() string { return a.name }
func (a acceptingEmail) Send(context.Context, ports.OutboundEmail) (ports.EmailReceipt, error) {
	return ports.EmailReceipt{ProviderID: a.name + "-1"}, nil
}
func (a acceptingEmail) Status(context.Context, ports.OutboundEmail, time.Time) (ports.EmailReceipt, error) {
	return ports.EmailReceipt{}, ports.ErrEmailUnknown
}

func fakeConnector(t testing.TB, id, scope string, secrets string) connector.Binding {
	t.Helper()
	m, err := connector.Parse([]byte(`{"id":"` + id + `","name":"` + id + `","publisher":"Fyniq","version":"1.0.0","min_platform":"0.1.0",
	  "summary":"A fake.","scope":"` + scope + `","capabilities":[{"kind":"outbound-email","trigger":"system-rule",
	  "trigger_rule":"A message the outbox holds.","on_pause":"hold","on_disable":"hold","in_flight":"reconcile"}],
	  "configuration":{"type":"object","additionalProperties":false,"properties":{"address":{"type":"string","pattern":"^[a-z.]+:[0-9]+$"}}},
	  "secrets":[` + secrets + `],"provider_scopes":[],"fyniq_scopes":[],"data_classes":["personal-data"],"regions":["SG"],
	  "destinations":[{"host":"` + id + `.example.com"}],"webhooks":[],"health_check":"Counts.","test_action":"Sends.","local_substitute":"A fake.",
	  "limits":{"requests_per_minute":600,"breaker_failures":5,"breaker_cooldown_seconds":60}}`))
	if err != nil {
		t.Fatal(err)
	}
	return connector.Binding{Manifest: m, OutboundEmail: func(connector.Config) (ports.Email, error) { return acceptingEmail{name: id}, nil }}
}

// testConnectors is the catalogue the API tests run with: this release's connectors, an installation fake that accepts
// every message, and an organisation fake with a key; keys in files under a temporary directory.
func testConnectors(t testing.TB, c ports.Clock) ConnectorDeps {
	t.Helper()
	bs := []connector.Binding{fakeConnector(t, "example-email", connector.ScopeInstallation, ""),
		fakeConnector(t, "example-org", connector.ScopeOrganisation, `{"name":"key","title":"Key","description":"Its key.","used_by":["worker"],"rotation_overlap_hours":1}`),
		fakeBankConnector(t)}
	real, err := connectors.Catalogue()
	if err != nil {
		t.Fatal(err)
	}
	bs = append(bs, real.All()...)
	cat, err := connector.NewCatalogue(bs...)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := local.NewSecretFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return ConnectorDeps{Catalogue: cat, Secrets: secrets, Runtime: &connector.Runtime{Catalogue: cat, Secrets: connector.NewSecretCache(secrets, c),
		Egress: &egress.Guard{Local: true}, Clock: c}}
}

// matrixOrgConnector is a configured organisation connector of the matrix's organisation, made once.
func matrixOrgConnector(e *env) string {
	e.t.Helper()
	if e.orgConnector != "" {
		return e.orgConnector
	}
	b, _ := e.api.deps.Connectors.Catalogue.Get("example-org")
	m := b.Manifest
	if err := e.store.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Connectors().InstallOrganisation(ctx, storage.NewConnector{Connector: m.ID, ManifestVersion: m.Version, ManifestHash: m.Hash(),
			SecretNames: []string{"key"}, Consent: m.Consent(), Position: 1, At: e.clock.Now()})
		if err != nil {
			return err
		}
		_, err = u.Connectors().ChangeOrganisation(ctx, c.ID, c.Version, storage.ConnectorChange{Step: storage.StepConfigure, State: storage.ConnectorConfigured,
			Configuration: map[string]any{"address": "smtp.example.com:587"}, Position: 1, ManifestVersion: m.Version, ManifestHash: m.Hash(),
			SecretNames: []string{"key"}, Consent: m.Consent(), At: e.clock.Now()})
		e.orgConnector = c.ID.String()
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return e.orgConnector
}

// The installation's connectors through the API (FR-INT-20, FR-INT-26, FR-ADM-07): a Super admin with step-up installs,
// configures, sets a key (write-only, never answered), tests (pass or fail in Fyniq's words) and enables; with one Super
// admin each step applies at once; with two, pause still does, and every other step answers 202 with the change, which
// only the other one approves, signing its envelope's hash; a rejected key change is rejected. No answer, log or audit
// event carries the key.
// req: FR-INT-20
// req: FR-INT-26
// req: FR-ADM-07
// req: FR-INT-01
// asvs: V2.3.5, V13.3.1
func TestConnectorsThroughTheAPI(t *testing.T) {
	ctx := context.Background()
	e, fresh := freshInstallation(t)
	person := func(subject string) string {
		return e.principal(authn.Principal{Subject: subject, Client: "web", StepUpAt: e.clock.Now()})
	}
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, fresh.URL("fyniq_migrator"), hash); err != nil {
		t.Fatal(err)
	}
	setup := e.call(http.MethodPost, "/v1/setup", e.principal(authn.Principal{Subject: "founder", Client: "web"}), map[string]any{"setup_code": code,
		"display_name": "Mei Ling Tan", "organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}})
	if setup.Status != 201 {
		t.Fatalf("setup: %d %s", setup.Status, setup.Raw)
	}
	founder := person("founder")
	stale := e.principal(authn.Principal{Subject: "founder", Client: "web", StepUpAt: e.clock.Now().Add(-6 * time.Minute)})

	e.problemIs(e.call(http.MethodPost, "/v1/installation/connectors", stale, map[string]any{"connector": "example-email"}), "installInstallationConnector", 403, CodeStepUpRequired)
	e.problemIs(e.call(http.MethodPost, "/v1/installation/connectors", person("someone"), map[string]any{"connector": "example-email"}), "installInstallationConnector", 403, CodeNotSuperAdmin)
	e.problemIs(e.call(http.MethodPost, "/v1/installation/connectors", founder, map[string]any{"connector": "example-org"}), "installInstallationConnector", 422, connector.CodeScope)
	e.problemIs(e.call(http.MethodPost, "/v1/installation/connectors", founder, map[string]any{"connector": "fax"}), "installInstallationConnector", 404, connector.CodeUnknown)

	r := e.call(http.MethodPost, "/v1/installation/connectors", founder, map[string]any{"connector": "example-email"})
	if r.Status != 201 || r.str("state") != "installed" || r.str("connector") != "example-email" || r.Header.Get("ETag") != `"1"` {
		t.Fatalf("install: %d %s", r.Status, r.Raw)
	}
	id := r.str("id")
	path := "/v1/installation/connectors/" + id
	e.problemIs(e.call(http.MethodPut, path+"/configuration", founder, map[string]any{"configuration": map[string]any{"address": "Not An Address"}},
		"If-Match", `"1"`), "configureInstallationConnector", 422, connector.CodeConfiguration)
	e.problemIs(e.call(http.MethodPut, path+"/configuration", founder, map[string]any{"configuration": map[string]any{"address": "a.example:25"}}),
		"configureInstallationConnector", 428, CodeIfMatchRequired)
	r = e.call(http.MethodPut, path+"/configuration", founder, map[string]any{"configuration": map[string]any{"address": "a.example:25"}}, "If-Match", `"1"`)
	if r.Status != 200 || r.str("state") != "configured" {
		t.Fatalf("configure: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, path+"/steps", founder, map[string]any{"step": "enable"}, "If-Match", "*"), "stepInstallationConnector", 409, connector.CodeState)
	e.problemIs(e.call(http.MethodPost, path+"/test", founder, nil, "If-Match", "*"), "testInstallationConnector", 409, CodeNoEmailAddress)

	// The test goes to the organisation's own address: the Super admin signed in to their organisation, with its email set.
	org, _ := uuid.Parse(setup.str("organisation_id"))
	owner := e.principal(authn.Principal{Subject: "founder", Client: "web", Class: authn.ClassUser, StepUpAt: e.clock.Now(), Organisation: org,
		Roles: []authz.Role{authz.Owner, authz.Controller}})
	if p := e.call(http.MethodPatch, "/v1/organisation/profile", owner, map[string]any{"email": "accounts@cognizhi.example"}, "If-Match", "*"); p.Status != 200 {
		t.Fatalf("the organisation's email: %d %s", p.Status, p.Raw)
	}
	inOrg := owner
	r = e.call(http.MethodPost, path+"/test", inOrg, nil, "If-Match", "*")
	if r.Status != 200 || r.str("state") != "tested" || r.Body["test"].(map[string]any)["passed"] != true {
		t.Fatalf("test: %d %s", r.Status, r.Raw)
	}
	r = e.call(http.MethodPost, path+"/steps", founder, map[string]any{"step": "enable"}, "If-Match", r.Header.Get("ETag"))
	if r.Status != 200 || r.str("state") != "enabled" || r.str("health") != "ok" {
		t.Fatalf("enable: %d %s", r.Status, r.Raw)
	}

	// A key is write-only: set, it is never answered, logged or audited.
	z := e.call(http.MethodPost, "/v1/installation/connectors", founder, map[string]any{"connector": "zeptomail"})
	const key = "zepto-send-token-never-shown"
	r = e.call(http.MethodPut, "/v1/installation/connectors/"+z.str("id")+"/secrets/send_token", founder, map[string]any{"value": key}, "If-Match", "*")
	if r.Status != 200 || !strings.Contains(r.Raw, `"name":"send_token","set":true`) || strings.Contains(r.Raw, key) {
		t.Fatalf("a key: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/installation/connectors/"+z.str("id")+"/secrets/password", founder, map[string]any{"value": "x"}, "If-Match", "*"),
		"setInstallationConnectorSecret", 422, connector.CodeSecretUnknown)
	list := e.call(http.MethodGet, "/v1/installation/connectors", founder, nil)
	if list.Status != 200 || len(list.Body["items"].([]any)) != 2 || strings.Contains(list.Raw, key) {
		t.Fatalf("list: %d %s", list.Status, list.Raw)
	}

	// A second Super admin: pause alone; resume asks the other, who signs the envelope's hash.
	invite := e.call(http.MethodPost, "/v1/super-admins/invitations", founder, nil)
	accept := e.call(http.MethodPost, "/v1/super-admins", e.principal(authn.Principal{Subject: "cofounder", Client: "web"}),
		map[string]any{"invitation_code": invite.str("code"), "display_name": "Wei Jie Lim"})
	if accept.Status != 201 {
		t.Fatalf("the second Super admin: %d %s", accept.Status, accept.Raw)
	}
	cofounder := person("cofounder")
	r = e.call(http.MethodPost, path+"/steps", founder, map[string]any{"step": "pause"}, "If-Match", "*")
	if r.Status != 200 || r.str("state") != "paused" {
		t.Fatalf("pause by one of two: %d %s", r.Status, r.Raw)
	}
	r = e.call(http.MethodPost, path+"/steps", founder, map[string]any{"step": "resume"}, "If-Match", "*")
	sum := sha256.Sum256([]byte(r.str("envelope")))
	if r.Status != 202 || r.str("status") != "pending" || r.str("step") != "resume" || hex.EncodeToString(sum[:]) != r.str("envelope_hash") {
		t.Fatalf("resume by one of two: %d %s", r.Status, r.Raw)
	}
	change := r.str("id")
	e.problemIs(e.call(http.MethodPost, "/v1/installation/changes/"+change+"/approve", founder, map[string]any{"envelope_hash": r.str("envelope_hash")}),
		"approveInstallationChange", 409, storage.CodeConnectorChange)
	e.problemIs(e.call(http.MethodPost, "/v1/installation/changes/"+change+"/approve", cofounder, map[string]any{"envelope_hash": strings.Repeat("0", 64)}),
		"approveInstallationChange", 409, storage.CodeConnectorChange)
	a := e.call(http.MethodPost, "/v1/installation/changes/"+change+"/approve", cofounder, map[string]any{"envelope_hash": r.str("envelope_hash")})
	if a.Status != 200 || a.str("state") != "enabled" {
		t.Fatalf("approved by the other: %d %s", a.Status, a.Raw)
	}

	// A key change with two Super admins is asked for, and the other rejects it.
	r = e.call(http.MethodPut, "/v1/installation/connectors/"+z.str("id")+"/secrets/send_token", founder, map[string]any{"value": key + "-2"}, "If-Match", "*")
	if r.Status != 202 || r.str("step") != "secret" || strings.Contains(r.Raw, key) {
		t.Fatalf("a key change asks the other: %d %s", r.Status, r.Raw)
	}
	rj := e.call(http.MethodPost, "/v1/installation/changes/"+r.str("id")+"/reject", cofounder, map[string]any{"envelope_hash": r.str("envelope_hash"), "reason": "Not this key."})
	if rj.Status != 200 || rj.str("status") != "rejected" || rj.str("reason") != "Not this key." {
		t.Fatalf("reject: %d %s", rj.Status, rj.Raw)
	}
	changes := e.call(http.MethodGet, "/v1/installation/changes", cofounder, nil)
	if changes.Status != 200 || len(changes.Body["items"].([]any)) != 2 {
		t.Fatalf("changes: %d %s", changes.Status, changes.Raw)
	}

	// The others hear at once of a pause, which waits for nobody, the one who asked hears how each change was decided, and
	// a lone Super admin hears of each change that applied at once: in the organisations they belong to (the founder's; the
	// cofounder belongs to none here).
	if r := e.call(http.MethodPost, path+"/steps", cofounder, map[string]any{"step": "pause"}, "If-Match", "*"); r.Status != 200 {
		t.Fatalf("pause by the other: %d %s", r.Status, r.Raw)
	}
	fanOutAllIn(t, e, fresh.URL("fyniq_worker"))
	me, _ := uuid.Parse(setup.str("user_id"))
	told := e.call(http.MethodGet, "/v1/notifications?unread=true", e.principal(authn.Principal{Subject: "founder", Client: "web",
		Class: authn.ClassUser, Organisation: org, UserID: me, Roles: []authz.Role{authz.Owner}}), nil)
	for _, want := range []string{`"type":"security.connector"`, "was paused by another Super admin", "approved by another Super admin",
		"rejected by another Super admin: Not this key.", "changed at once, since there is one Super admin: enabled"} {
		if !strings.Contains(told.Raw, want) {
			t.Errorf("the founder was not told %q: %s", want, told.Raw)
		}
	}
	if strings.Contains(told.Raw, "waits for another Super admin") {
		t.Errorf("the founder was told of their own requests: %s", told.Raw)
	}
	if strings.Contains(e.log.String(), key) {
		t.Fatal("a key reached the log")
	}
	inst, _ := uuid.Parse(setup.str("installation_organisation_id"))
	for _, body := range trail(t, fresh.URL("fyniq_readonly"), inst) {
		if strings.Contains(body, key) {
			t.Fatalf("a key reached the installation's trail: %s", body)
		}
	}
}

// An organisation's connectors through the API (FR-INT-26, FM-1): asking to install one, configure it, set its key
// or take a step answers 202 with the approval request, which another member who holds the approval right approves,
// and the step is taken then; pause and test need nobody else; the key is never answered.
// req: FR-INT-26
// req: FM-1
// req: FR-INT-01
// asvs: V2.3.5
func TestOrganisationConnectorsThroughTheAPI(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow, weiling := f.as(f.ahkow, authz.Administrator), f.as(f.weiling) // an Administrator asks (C); an Owner approves (A)
	r := e.call(http.MethodPost, "/v1/organisation/connectors", ahkow, map[string]any{"connector": "example-org"})
	if r.Status != 202 || r.str("operation") != "connector.change" || r.str("object_type") != "connector" {
		t.Fatalf("asking to install: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/organisation/connectors", ahkow, map[string]any{"connector": "zeptomail"}), "installOrganisationConnector", 422, connector.CodeScope)
	if d := f.decide(weiling, r.str("id"), "approve", hashOf(r)); d.Status != 200 {
		t.Fatalf("approving the install: %d %s", d.Status, d.Raw)
	}
	list := e.call(http.MethodGet, "/v1/organisation/connectors", ahkow, nil)
	items := list.Body["items"].([]any)
	if list.Status != 200 || len(items) != 1 || items[0].(map[string]any)["state"] != "installed" {
		t.Fatalf("installed: %d %s", list.Status, list.Raw)
	}
	id := items[0].(map[string]any)["id"].(string)
	path := "/v1/organisation/connectors/" + id
	r = e.call(http.MethodPut, path+"/configuration", ahkow, map[string]any{"configuration": map[string]any{"address": "smtp.example.com:587"}}, "If-Match", `"1"`)
	if r.Status != 202 {
		t.Fatalf("asking to configure: %d %s", r.Status, r.Raw)
	}
	if d := f.decide(weiling, r.str("id"), "approve", hashOf(r)); d.Status != 200 {
		t.Fatalf("approving the configuration: %d %s", d.Status, d.Raw)
	}
	const key = "org-key-never-shown"
	r = e.call(http.MethodPut, path+"/secrets/key", ahkow, map[string]any{"value": key}, "If-Match", "*")
	if r.Status != 202 || strings.Contains(r.Raw, key) {
		t.Fatalf("asking to set the key: %d %s", r.Status, r.Raw)
	}
	if d := f.decide(weiling, r.str("id"), "approve", hashOf(r)); d.Status != 200 || strings.Contains(d.Raw, key) {
		t.Fatalf("approving the key: %d %s", d.Status, d.Raw)
	}
	matrixProfileEmail(e)
	r = e.call(http.MethodPost, path+"/test", ahkow, nil, "If-Match", "*")
	if r.Status != 200 || r.str("state") != "tested" {
		t.Fatalf("test: %d %s", r.Status, r.Raw)
	}
	r = e.call(http.MethodPost, path+"/steps", ahkow, map[string]any{"step": "enable"}, "If-Match", "*")
	if r.Status != 202 {
		t.Fatalf("asking to enable: %d %s", r.Status, r.Raw)
	}
	if d := f.decide(weiling, r.str("id"), "approve", hashOf(r)); d.Status != 200 {
		t.Fatalf("approving the enable: %d %s", d.Status, d.Raw)
	}
	r = e.call(http.MethodPost, path+"/steps", ahkow, map[string]any{"step": "pause"}, "If-Match", "*")
	if r.Status != 200 || r.str("state") != "paused" || !strings.Contains(r.Raw, `"name":"key","set":true`) {
		t.Fatalf("pause at once: %d %s", r.Status, r.Raw)
	}
	fanOutAll(t, e) // the others hear of the pause at once; the one who paused does not
	if n := e.call(http.MethodGet, "/v1/notifications?unread=true", weiling, nil); !strings.Contains(n.Raw, `"type":"security.connector"`) {
		t.Fatalf("the Owner was not told of the pause: %s", n.Raw)
	}
	if n := e.call(http.MethodGet, "/v1/notifications?unread=true", ahkow, nil); strings.Contains(n.Raw, `"type":"security.connector"`) {
		t.Fatalf("the one who paused was told: %s", n.Raw)
	}
	if got := e.call(http.MethodGet, path, e.as(authz.Auditor), nil); got.Status != 200 || strings.Contains(got.Raw, key) {
		t.Fatalf("read: %d %s", got.Status, got.Raw)
	}
	if strings.Contains(e.log.String(), key) {
		t.Fatal("a key reached the log")
	}
	for _, body := range trail(t, db.URL("fyniq_readonly"), e.books.Org) {
		if strings.Contains(body, key) {
			t.Fatalf("a key reached the trail: %s", body)
		}
	}
}

// trail is every audit event of org, as its bodies, read as the read-only role from the database at url.
func trail(t *testing.T, url string, org uuid.UUID) []string {
	t.Helper()
	reader, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var out []string
	if err := reader.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 100_000)
		for _, ev := range events {
			out = append(out, ev.Body)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("no audit events to check")
	}
	return out
}

// askConnector asks for an organisation connector's step as tok's person, with step-up, and withdraws the request it
// made, so the next role's request is not refused as pending; refused, nothing was asked.
func askConnector(e *env, tok, method, path string, body map[string]any, headers ...string) (response, func() bool) {
	was := requestsOf(e, "connector")
	person := steppedUp(e, tok)
	r := e.call(method, path, person, body, headers...)
	if r.Status == http.StatusAccepted {
		e.call(http.MethodPost, "/v1/approvals/"+r.str("id")+"/withdraw", person, nil, "If-Match", `"1"`)
	}
	return r, func() bool { return requestsOf(e, "connector") == was }
}
