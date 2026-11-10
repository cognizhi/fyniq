package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/api"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

type ipWindows struct {
	used map[string]int
	fail bool
}

func (w *ipWindows) CountIP(_ context.Context, client, kind string, start time.Time) (int, error) {
	if w.fail {
		return 0, errors.New("the database is away")
	}
	w.used[client+" "+kind+" "+start.String()]++
	return w.used[client+" "+kind+" "+start.String()], nil
}

func (w *ipWindows) PurgeIP(context.Context, time.Time) (int64, error) { return 0, nil }

// The routes a person reaches before they belong to an organisation are counted per client address, a minute at a time:
// over the limit a request is answered 429 with Retry-After before it is served. The edge's header names the client only
// when the peer is trusted; an IPv6 client counts by its /64; other routes are left alone.
// req: SEC-23
// asvs: V2.4.1
func TestPerIPLimits(t *testing.T) {
	now := time.Date(2099, 1, 5, 9, 0, 40, 0, time.UTC)
	windows := &ipWindows{used: map[string]int{}}
	served := 0
	l := api.IPLimiter{Store: windows, Clock: clock.NewFrozen(now), Limits: map[string]int{storage.IPSignIn: 2, storage.IPSetup: 1},
		Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	h := l.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { served++ }))
	call := func(method, path, peer, header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = peer
		if header != "" {
			r.Header.Add(api.ClientIPHeader, header)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for i := range 2 {
		if w := call(http.MethodGet, "/auth/signin", "203.0.113.7:4000", ""); w.Code != 200 {
			t.Fatalf("sign-in %d: %d", i+1, w.Code)
		}
	}
	w := call(http.MethodGet, "/auth/signin", "203.0.113.7:4001", "")
	var p api.Problem
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "21" || p.Code != api.CodeRateLimited ||
		w.Header().Get("Content-Type") != "application/problem+json" || served != 2 {
		t.Fatalf("the third sign-in in a minute: %d %q %+v (served %d)", w.Code, w.Header().Get("Retry-After"), p, served)
	}
	// Another address, another route, a route the limits leave alone.
	if call(http.MethodGet, "/auth/signin", "198.51.100.1:4000", "").Code != 200 || call(http.MethodPost, "/v1/setup", "203.0.113.7:4000", "").Code != 200 ||
		call(http.MethodGet, "/v1/organisation", "203.0.113.7:4000", "").Code != 200 {
		t.Fatal("a limit counted for another address, kind or route")
	}

	// The edge's header names the client only behind a trusted peer, and only its last value (the one the edge added).
	behind := func(peer, header string) string {
		r := httptest.NewRequest(http.MethodGet, "/auth/signin", nil)
		r.RemoteAddr = peer
		for _, v := range []string{"192.0.2.66", header} {
			if v != "" {
				r.Header.Add(api.ClientIPHeader, v)
			}
		}
		return l.ClientIP(r).String()
	}
	if behind("10.1.2.3:443", "203.0.113.9") != "203.0.113.9" || behind("198.51.100.1:443", "203.0.113.9") != "198.51.100.1" ||
		behind("10.1.2.3:443", "not an address") != "10.1.2.3" {
		t.Fatalf("the client behind the edge: %s %s %s", behind("10.1.2.3:443", "203.0.113.9"), behind("198.51.100.1:443", "203.0.113.9"),
			behind("10.1.2.3:443", "not an address"))
	}
	if api.IPKey(netip.MustParseAddr("2001:db8:1:2::1")) != api.IPKey(netip.MustParseAddr("2001:db8:1:2:ffff::9")) ||
		api.IPKey(netip.MustParseAddr("2001:db8:1:3::1")) == api.IPKey(netip.MustParseAddr("2001:db8:1:2::1")) || len(api.IPKey(netip.MustParseAddr("203.0.113.7"))) != 64 {
		t.Fatal("an IPv6 client counts by its /64, by a hash of the address")
	}
	for path, want := range map[string]string{"/auth/callback": storage.IPSignIn, "/v1/sign-in-providers": storage.IPSignIn,
		"/v1/public/document": storage.IPPublic, "/v1/setup": storage.IPSetup, "/v1/session/join-offer": storage.IPJoin, "/v1/journals": ""} {
		if got := api.IPKindOf(httptest.NewRequest(http.MethodGet, path, nil)); got != want {
			t.Errorf("%s is %q, want %q", path, got, want)
		}
	}
	if api.IPKindOf(httptest.NewRequest(http.MethodPost, "/v1/session/recovery", nil)) != storage.IPJoin ||
		api.IPKindOf(httptest.NewRequest(http.MethodGet, "/v1/members", nil)) != "" {
		t.Error("recovery is counted; listing the members is not")
	}

	// A window that cannot be counted lets the request through: the edge's ban still holds.
	windows.fail = true
	if call(http.MethodGet, "/auth/signin", "203.0.113.7:4000", "").Code != 200 {
		t.Fatal("a request was refused because its window could not be counted")
	}
}

// req: SEC-23
// asvs: V2.4.1
func TestReadIPLimits(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	limits, trusted, err := api.ReadIPLimits(env(map[string]string{"FYNIQ_IP_LIMITS": `{"signin":50}`, "FYNIQ_TRUSTED_PROXIES": "10.0.0.0/8, ::/0"}))
	if err != nil || limits[storage.IPSignIn] != 50 || limits[storage.IPSetup] != api.IPLimitDefaults[storage.IPSetup] || len(trusted) != 2 {
		t.Fatalf("%v %v %v", limits, trusted, err)
	}
	for _, bad := range []map[string]string{{"FYNIQ_IP_LIMITS": "not json"}, {"FYNIQ_IP_LIMITS": `{"everything":5}`}, {"FYNIQ_IP_LIMITS": `{"join":0}`},
		{"FYNIQ_TRUSTED_PROXIES": "the load balancer"}} {
		if _, _, err := api.ReadIPLimits(env(bad)); err == nil {
			t.Errorf("%v was read", bad)
		}
	}
}
