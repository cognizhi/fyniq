// Package clientip says where a request came from (SEC-23, SEC-31; p1-security-hardening design D4, D7): the client's
// address, and the country and region the edge's load balancer adds (Cloud Armor's geolocation, [live-verify]); locally
// Caddy names the address and a developer may set the others as stand-ins. The edge's headers are believed only from a
// trusted peer; anyone else's are ignored, and the peer's own address is the client's. No location database: a request
// without the edge's headers has no country or region.
package clientip

import (
	"context"
	"net/http"
	"net/netip"
	"regexp"
	"strings"
)

// The headers the edge adds (infra/gcp/modules/edge, deploy/local/Caddyfile).
const (
	HeaderIP      = "X-Fyniq-Client-IP"
	HeaderCountry = "X-Fyniq-Client-Country"
	HeaderRegion  = "X-Fyniq-Client-Region"
)

// Info is where a request came from.
type Info struct {
	IP      netip.Addr
	Country string // ISO 3166-1 alpha-2, upper case, such as SG; "" when the edge did not say
	Region  string // the country's subdivision as the load balancer names it, such as SG01; "" when it did not say
}

var (
	country = regexp.MustCompile(`^[A-Z]{2}$`)
	region  = regexp.MustCompile(`^[A-Za-z0-9-]{1,16}$`)
)

// Resolver reads where requests came from.
type Resolver struct {
	Trusted []netip.Prefix // the peers whose headers are believed: the load balancer, or Caddy locally
}

// last is a header's last value, the one the edge added, and its last comma-separated element.
func last(h http.Header, name string) string {
	vs := h.Values(name)
	if len(vs) == 0 {
		return ""
	}
	parts := strings.Split(vs[len(vs)-1], ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

// Of is where r came from: the edge's headers when the peer is trusted, else the peer's own address alone.
func (res Resolver) Of(r *http.Request) Info {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return Info{}
	}
	info := Info{IP: peer.Addr().Unmap()}
	trusted := false
	for _, t := range res.Trusted {
		if t.Contains(info.IP) {
			trusted = true
			break
		}
	}
	if !trusted {
		return info
	}
	if a, err := netip.ParseAddr(last(r.Header, HeaderIP)); err == nil {
		info.IP = a.Unmap()
	}
	if c := strings.ToUpper(last(r.Header, HeaderCountry)); country.MatchString(c) {
		info.Country = c
	}
	if s := last(r.Header, HeaderRegion); region.MatchString(s) {
		info.Region = s
	}
	return info
}

type key struct{}

// Wrap puts where each request came from in its context, for From.
func (res Resolver) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), key{}, res.Of(r))))
	})
}

// From is where the request of ctx came from, as Wrap put it there.
func From(ctx context.Context) (Info, bool) {
	i, ok := ctx.Value(key{}).(Info)
	return i, ok
}

// With is ctx carrying i, for a test or a caller that resolved it itself.
func With(ctx context.Context, i Info) context.Context { return context.WithValue(ctx, key{}, i) }
