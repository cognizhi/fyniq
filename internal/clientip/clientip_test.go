package clientip_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/cognizhi/fyniq/internal/clientip"
)

// The edge's headers name the client, its country and region only behind a trusted peer, and only by their last value
// (the one the edge added); a value that is not one is dropped; anyone else's request is its peer's.
// req: SEC-23
// req: SEC-31
// asvs: V2.4.1
func TestWhereARequestCameFrom(t *testing.T) {
	res := clientip.Resolver{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	at := func(peer string, h map[string][]string) clientip.Info {
		r := httptest.NewRequest(http.MethodGet, "/auth/signin", nil)
		r.RemoteAddr = peer
		for k, vs := range h {
			for _, v := range vs {
				r.Header.Add(k, v)
			}
		}
		return res.Of(r)
	}
	edge := map[string][]string{clientip.HeaderIP: {"192.0.2.66", "203.0.113.9"}, clientip.HeaderCountry: {"US", "sg"},
		clientip.HeaderRegion: {"SG01"}}
	if i := at("10.1.2.3:443", edge); i.IP.String() != "203.0.113.9" || i.Country != "SG" || i.Region != "SG01" {
		t.Fatalf("behind the edge: %+v", i)
	}
	if i := at("198.51.100.1:443", edge); i.IP.String() != "198.51.100.1" || i.Country != "" || i.Region != "" {
		t.Fatalf("from someone else: %+v", i)
	}
	bad := map[string][]string{clientip.HeaderIP: {"nowhere"}, clientip.HeaderCountry: {"Singapore"}, clientip.HeaderRegion: {"<script>"}}
	if i := at("10.1.2.3:443", bad); i.IP.String() != "10.1.2.3" || i.Country != "" || i.Region != "" {
		t.Fatalf("values that are not one: %+v", i)
	}
	var got clientip.Info
	res.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got, _ = clientip.From(r.Context()) })).ServeHTTP(httptest.NewRecorder(),
		func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = "10.9.9.9:1"
			r.Header.Set(clientip.HeaderCountry, "MY")
			return r
		}())
	if got.Country != "MY" || got.IP.String() != "10.9.9.9" {
		t.Fatalf("in the request's context: %+v", got)
	}
}
