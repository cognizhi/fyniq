package webhooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/contracts/events"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/egress"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The signature is Standard Webhooks' v1: the headers, the signed content msg_id.timestamp.payload, the "v1," base64 HMAC-SHA256, the "whsec_" base64 secret and the space-delimited
// list of signatures are the specification's (github.com/standard-webhooks/standard-webhooks spec/standard-webhooks.md, retrieved 21 Nov 2026); the vector is the reference
// libraries' own, which the specification does not carry [verify], and the properties a receiver relies on: a changed body, id, timestamp or secret does not verify, a stale timestamp is
// refused, and two signatures while a secret rolls both verify.
// req: FR-INT-12
// req: FR-AUT-04
// asvs: V11.2.1, V13.2.1
func TestTheSignatureIsStandardWebhooksV1(t *testing.T) {
	const secret = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	at := time.Unix(1614265330, 0)
	body := []byte(`{"test": 2432232314}`)
	sig, err := Sign(secret, "msg_p5jXN8AQM9LWM0D4loKWxJek", at, body)
	if err != nil || sig != "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=" {
		t.Fatalf("%q %v", sig, err)
	}
	now := at.Add(time.Minute)
	ts := "1614265330"
	if err := Verify(secret, "msg_p5jXN8AQM9LWM0D4loKWxJek", ts, sig, body, now); err != nil {
		t.Fatal(err)
	}
	for name, f := range map[string]func() error{
		"a changed body":      func() error { return Verify(secret, "msg_p5jXN8AQM9LWM0D4loKWxJek", ts, sig, append(body, ' '), now) },
		"a changed id":        func() error { return Verify(secret, "msg_other", ts, sig, body, now) },
		"a changed timestamp": func() error { return Verify(secret, "msg_p5jXN8AQM9LWM0D4loKWxJek", "1614265331", sig, body, now) },
		"another secret": func() error {
			return Verify("whsec_"+"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "msg_p5jXN8AQM9LWM0D4loKWxJek", ts, sig, body, now)
		},
		"a stale timestamp": func() error {
			return Verify(secret, "msg_p5jXN8AQM9LWM0D4loKWxJek", ts, sig, body, at.Add(ReplayWindow+time.Second))
		},
		"a future timestamp": func() error {
			return Verify(secret, "msg_p5jXN8AQM9LWM0D4loKWxJek", ts, sig, body, at.Add(-ReplayWindow-time.Second))
		},
		"no signature": func() error { return Verify(secret, "msg_p5jXN8AQM9LWM0D4loKWxJek", ts, "", body, now) },
	} {
		if f() == nil {
			t.Errorf("%s verified", name)
		}
	}
	other, _ := NewSecret()
	two, err := Header([]string{secret, other}, "msg_1", at, body)
	if err != nil || len(strings.Fields(two)) != 2 {
		t.Fatalf("%q %v", two, err)
	}
	for _, s := range []string{secret, other} {
		if err := Verify(s, "msg_1", ts, two, body, now); err != nil {
			t.Errorf("a secret in force: %v", err)
		}
	}
	if _, err := Sign("not-a-secret", "m", at, body); err == nil {
		t.Error("a secret without whsec_ signed")
	}
}

// A secret is 32 random bytes, never repeated; its hint is its last four characters.
// req: FR-INT-12
func TestSecretsAreRandomAndShowOnlyTheirLastFour(t *testing.T) {
	a, _ := NewSecret()
	b, _ := NewSecret()
	if a == b || !strings.HasPrefix(a, SecretPrefix) || len(a) != len(SecretPrefix)+44 || Hint(a) != a[len(a)-4:] {
		t.Errorf("%q %q", a, b)
	}
}

// Retries: 5 s, 5 min, 30 min, 2 h, 5 h, 10 h, then every 12 h, until three days from the first attempt, then no more.
// req: FR-INT-12
// req: FR-AUT-04
func TestTheRetriesBackOffForThreeDays(t *testing.T) {
	first := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	now, tried := first, 0
	var gaps []time.Duration
	for {
		tried++
		next, ok := Next(tried, first, now)
		if !ok {
			break
		}
		gaps = append(gaps, next.Sub(now))
		now = next
	}
	want := []time.Duration{5 * time.Second, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 5 * time.Hour, 10 * time.Hour}
	if !slices.Equal(gaps[:6], want) || gaps[6] != 12*time.Hour || now.Sub(first) > Lifetime || now.Sub(first) < Lifetime-12*time.Hour {
		t.Errorf("gaps %v, last attempt at %v", gaps, now.Sub(first))
	}
}

// What an endpoint may ask for is a short list of operational and approval events that exist in the catalogue: none of the security, personal-data, payee, investor or
// personal types, whose data would be about a person or an attack.
// req: FR-INT-12
// asvs: V13.1.1
func TestOnlyOperationalEventsMayBeAskedFor(t *testing.T) {
	cat, err := events.Types()
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range DeliverableTypes() {
		if !slices.Contains(cat, typ) {
			t.Errorf("%s is not in the event catalogue", typ)
		}
		raw, _ := events.Schemas.ReadFile("schemas/" + typ + ".schema.json")
		var s struct {
			X struct {
				Category string `json:"category"`
			} `json:"x-fyniq-event"`
		}
		_ = json.Unmarshal(raw, &s)
		if s.X.Category != "operations" && s.X.Category != "approvals" {
			t.Errorf("%s is in the %q category", typ, s.X.Category)
		}
	}
	for _, never := range []string{"fyniq.security.alert.v1", "fyniq.pdpa.breach.v1", "fyniq.payee.bank_details.v1", "fyniq.comment.mention.v1", "fyniq.investor.update.v1", "fyniq.time.missing.v1", VerifyType} {
		if Deliverable(never) {
			t.Errorf("%s can be asked for", never)
		}
	}
}

func deliverer(guard *egress.Guard) Deliverer {
	return Deliverer{Egress: guard, Clock: clock.NewFrozen(time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)), Source: "https://fyniq.example"}
}

func jobTo(url string) job {
	return job{delivery: storage.WebhookDelivery{MessageID: "msg_1", EventType: "fyniq.ledger.posted.v1", Payload: []byte(`{"organisation_id":"x"}`), CreatedAt: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)},
		endpoint: storage.WebhookEndpoint{URL: url}, secrets: []string{"whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"}}
}

// A delivery goes through the guard for third parties (SEC-36): a loopback or private address is refused, whatever the endpoint says, a redirect is never followed, the web
// address must be https, and a refusal is a failure with its next try, never a send. Run against a guard that is not the local flavour's.
// req: FR-INT-12
// req: SEC-36
// asvs: V1.2.6, V4.1.1
func TestADeliveryNeverReachesAPrivateAddressOrFollowsARedirect(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	d := deliverer(&egress.Guard{})
	now := d.Clock.Now()
	for name, url := range map[string]string{"loopback over http": target.URL, "loopback over https": strings.Replace(target.URL, "http://", "https://", 1), "a link-local metadata address": "https://169.254.169.254/latest/meta-data",
		"an internal name": "https://metadata.google.internal/computeMetadata/v1/", "localhost": "https://localhost:8443/x"} {
		r := d.attempt(context.Background(), jobTo(url), now)
		if r.Delivered || r.Error == "" || (r.NextAt.IsZero() && !r.Dead) || strings.Contains(r.Error, "127.0.0.1") {
			t.Errorf("%s: %+v", name, r)
		}
	}
	if hits != 0 {
		t.Errorf("the guard let %d requests through to a private address", hits)
	}
	// With the local flavour, loopback is reachable, but a redirect still is not followed.
	local := deliverer(&egress.Guard{Local: true})
	r := local.attempt(context.Background(), jobTo(redirect.URL), now)
	if r.Delivered || hits != 0 || !strings.Contains(r.Error, "redirect") {
		t.Errorf("a redirect: %+v (hits %d)", r, hits)
	}
	if r := local.attempt(context.Background(), jobTo(target.URL), now); !r.Delivered || hits != 1 {
		t.Errorf("a local endpoint: %+v (hits %d)", r, hits)
	}
}

// A sandbox organisation's delivery reaches only a host the deployment's operator allowed, and with none allowed, which is the default, nothing is
// sent: the attempt is dead at once and never touches the network, though the same endpoint receives for a real organisation (FR-INT-18).
// req: FR-INT-18
// asvs: V1.2.6, V4.1.1
func TestASandboxOrganisationsWebhookReachesOnlyAnAllowedHost(t *testing.T) {
	hits := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer target.Close()
	d := deliverer(&egress.Guard{Local: true})
	now := d.Clock.Now()
	sandbox := jobTo(target.URL)
	sandbox.sandbox = true
	if r := d.attempt(context.Background(), sandbox, now); r.Delivered || !r.Dead || hits != 0 || !strings.Contains(r.Error, "sandbox") {
		t.Fatalf("a sandbox's delivery with no host allowed: %+v (hits %d)", r, hits)
	}
	d.SandboxHosts = []string{"somewhere.example"}
	if r := d.attempt(context.Background(), sandbox, now); r.Delivered || !r.Dead || hits != 0 {
		t.Fatalf("a sandbox's delivery to a host not allowed: %+v (hits %d)", r, hits)
	}
	if r := d.attempt(context.Background(), jobTo(target.URL), now); !r.Delivered || hits != 1 {
		t.Fatalf("the same endpoint for a real organisation: %+v (hits %d)", r, hits)
	}
	d.SandboxHosts = []string{"127.0.0.1"}
	if r := d.attempt(context.Background(), sandbox, now); !r.Delivered || hits != 2 {
		t.Fatalf("a sandbox's delivery to an allowed host: %+v (hits %d)", r, hits)
	}
}
