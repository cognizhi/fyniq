package connector_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/connector"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func code(err error) string {
	var e *connector.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Each step leaves a connector where the lifecycle says, or is refused in plain words with its code: a configuration
// the manifest refuses, enabling before a passing test or without every secret, pausing what is not enabled, and any
// step on an uninstalled connector. A changed configuration is tested again; a failed test changes no state.
// req: FR-INT-26
// req: ENG-23
func TestPlanFollowsTheLifecycle(t *testing.T) {
	m, err := connector.Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	id := uuid.New()
	c := func(state string) storage.InstalledConnector {
		return storage.InstalledConnector{ID: id, Connector: "example", State: state, Configuration: map[string]any{"address": "a.b:1"},
			Position: 1, ManifestVersion: "1.0.0", ManifestHash: m.Hash(), SecretNames: []string{"key"}, Version: 3}
	}
	key := []storage.ConnectorSecret{{ConnectorID: id, Name: "key", Ref: "local:connector-example-key/1"}}
	passed := &storage.ConnectorTest{Passed: true, Reason: "Accepted.", At: at}
	failed := &storage.ConnectorTest{Passed: false, Reason: "Refused.", At: at}
	tested := c(storage.ConnectorTested)
	tested.Test = passed
	disabledFailed := c(storage.ConnectorDisabled)
	disabledFailed.Test = failed
	enabled := c(storage.ConnectorEnabled)
	enabled.Test = passed
	cases := []struct {
		name      string
		c         storage.InstalledConnector
		step      string
		in        connector.Input
		secrets   []storage.ConnectorSecret
		wantState string
		wantCode  string
	}{
		{"configure", c(storage.ConnectorInstalled), storage.StepConfigure, connector.Input{Configuration: map[string]any{"address": "smtp.example.com:587"}}, nil, storage.ConnectorConfigured, ""},
		{"a configuration the manifest refuses", c(storage.ConnectorInstalled), storage.StepConfigure, connector.Input{Configuration: map[string]any{}}, nil, "", connector.CodeConfiguration},
		{"reconfiguring a tested connector tests it again", tested, storage.StepConfigure, connector.Input{Configuration: map[string]any{"address": "x.y:2"}}, nil, storage.ConnectorConfigured, ""},
		{"reconfiguring an enabled connector keeps it running", enabled, storage.StepConfigure, connector.Input{Configuration: map[string]any{"address": "x.y:2"}}, nil, storage.ConnectorEnabled, ""},
		{"a passing test", c(storage.ConnectorConfigured), storage.StepTest, connector.Input{Test: passed}, nil, storage.ConnectorTested, ""},
		{"a failing test changes no state", c(storage.ConnectorConfigured), storage.StepTest, connector.Input{Test: failed}, nil, storage.ConnectorConfigured, ""},
		{"a test of an installed connector", c(storage.ConnectorInstalled), storage.StepTest, connector.Input{Test: passed}, nil, "", connector.CodeState},
		{"enable", tested, storage.StepEnable, connector.Input{}, key, storage.ConnectorEnabled, ""},
		{"enable without its secret", tested, storage.StepEnable, connector.Input{}, nil, "", connector.CodeSecretMissing},
		{"enable before a test", c(storage.ConnectorConfigured), storage.StepEnable, connector.Input{}, key, "", connector.CodeState},
		{"re-enable after a failed test", disabledFailed, storage.StepEnable, connector.Input{}, key, "", connector.CodeState},
		{"pause", enabled, storage.StepPause, connector.Input{}, nil, storage.ConnectorPaused, ""},
		{"pause what is not enabled", tested, storage.StepPause, connector.Input{}, nil, "", connector.CodeState},
		{"resume", c(storage.ConnectorPaused), storage.StepResume, connector.Input{}, nil, storage.ConnectorEnabled, ""},
		{"disable", c(storage.ConnectorPaused), storage.StepDisable, connector.Input{}, nil, storage.ConnectorDisabled, ""},
		{"uninstall", enabled, storage.StepUninstall, connector.Input{}, nil, storage.ConnectorUninstalled, ""},
		{"a step on an uninstalled connector", c(storage.ConnectorUninstalled), storage.StepConfigure, connector.Input{Configuration: map[string]any{"address": "a.b:1"}}, nil, "", connector.CodeState},
		{"order", enabled, storage.StepOrder, connector.Input{Position: 2}, nil, storage.ConnectorEnabled, ""},
		{"an order out of range", enabled, storage.StepOrder, connector.Input{Position: 0}, nil, "", connector.CodeState},
		{"consent to the manifest it runs", enabled, storage.StepConsent, connector.Input{}, nil, "", connector.CodeState},
		{"an unknown step", enabled, "explode", connector.Input{}, nil, "", connector.CodeState},
	}
	for _, x := range cases {
		got, err := connector.Plan(m, x.c, x.step, x.in, x.secrets, at)
		if x.wantCode != "" {
			if code(err) != x.wantCode {
				t.Errorf("%s: %v; want %s", x.name, err, x.wantCode)
			}
			continue
		}
		if err != nil || got.State != x.wantState || got.Step != x.step || !got.At.Equal(at) {
			t.Errorf("%s: %+v, %v; want %s", x.name, got, err, x.wantState)
		}
	}
	reconfigured, _ := connector.Plan(m, tested, storage.StepConfigure, connector.Input{Configuration: map[string]any{"address": "x.y:2"}}, nil, at)
	if reconfigured.Test != nil {
		t.Errorf("a changed configuration keeps its old test: %+v", reconfigured.Test)
	}
	ordered, _ := connector.Plan(m, enabled, storage.StepOrder, connector.Input{Position: 2}, nil, at)
	if ordered.Position != 2 {
		t.Errorf("order: %+v", ordered)
	}
	old := enabled
	old.ManifestHash, old.ManifestVersion = "0000000000000000000000000000000000000000000000000000000000000000", "0.9.0"
	consented, err := connector.Plan(m, old, storage.StepConsent, connector.Input{}, nil, at)
	if err != nil || consented.ManifestHash != m.Hash() || consented.ManifestVersion != "1.0.0" || len(consented.SecretNames) != 1 ||
		len(consented.Consent.Destinations) != 2 || consented.Consent.DataClasses[0] != "personal-data" {
		t.Errorf("consent: %+v, %v", consented, err)
	}
}

// The clean-up destroys the new key of an organisation's connector request that ended without applying (rejected,
// withdrawn, expired, or pending past its expiry) within eight days of its end; never one approved, still pending or
// of another object.
// req: FR-INT-26
// asvs: V13.3.4
func TestRefusedRequestsLoseTheirNewKeys(t *testing.T) {
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	change := func(ref string) []byte { return []byte(`{"execute":{"step":"secret","ref":"` + ref + `"}}`) }
	at := func(d time.Duration) *time.Time { x := now.Add(d); return &x }
	requests := []storage.ApprovalRequest{
		{ObjectType: "connector", Status: "rejected", DecidedAt: at(-time.Hour), Change: change("rejected")},
		{ObjectType: "connector", Status: "void", DecidedAt: at(-time.Hour), Change: change("withdrawn")},
		{ObjectType: "connector", Status: "expired", ExpiresAt: now.Add(-time.Hour), Change: change("expired")},
		{ObjectType: "connector", Status: "pending", ExpiresAt: now.Add(-time.Minute), Change: change("lapsed")},
		{ObjectType: "connector", Status: "pending", ExpiresAt: now.Add(time.Hour), Change: change("waiting")},
		{ObjectType: "connector", Status: "approved", DecidedAt: at(-time.Hour), Change: change("approved")},
		{ObjectType: "connector", Status: "rejected", DecidedAt: at(-9 * 24 * time.Hour), Change: change("long-ago")},
		{ObjectType: "invoice", Status: "rejected", DecidedAt: at(-time.Hour), Change: change("other")},
	}
	got := connector.RefusedRefs(requests, now)
	want := []ports.SecretRef{"rejected", "withdrawn", "expired", "lapsed"}
	if !slices.Equal(got, want) {
		t.Fatalf("%v; want %v", got, want)
	}
}
