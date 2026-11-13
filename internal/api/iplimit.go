package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ per-IP limits (SEC-23)

// The routes a person reaches before they are known to belong to an organisation (sign-in, joining and recovery, the
// public documents, first-run setup) are counted per client address, a minute at a time, in the installation's per-IP
// windows (p1-security-hardening design D4). Over its kind's limit a request is answered 429 with Retry-After before it
// is served. The edge's Cloud Armor bans an address over a coarser limit in front of this.

// IPLimitDefaults are the requests a minute per client address when the deployment sets none (FYNIQ_IP_LIMITS).
var IPLimitDefaults = map[string]int{storage.IPSignIn: 30, storage.IPJoin: 10, storage.IPPublic: 120, storage.IPSetup: 10}

// ClientIPHeader is the header the edge puts the client's address in: the load balancer's custom request header, and
// Caddy's locally (deploy/local/Caddyfile).
const ClientIPHeader = "X-Fyniq-Client-IP"

// IPLimiter counts the requests that need no organisation per client address.
type IPLimiter struct {
	Store  storage.IPLimits
	Clock  ports.Clock
	Limits map[string]int // kind → requests a minute per client address; a kind without one is not limited
	// Trusted are the peers whose ClientIPHeader is believed: the load balancer, or Caddy locally. Anyone else's is
	// ignored, and their own address counted.
	Trusted []netip.Prefix
	Log     *slog.Logger
}

// IPKindOf is the kind of request r is for the per-IP limits, or "" for one they leave alone.
func IPKindOf(r *http.Request) string {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/auth/"), p == "/v1/sign-in-providers":
		return storage.IPSignIn
	case strings.HasPrefix(p, "/v1/public/"), strings.HasPrefix(p, "/v1/evidence-downloads/"):
		return storage.IPPublic
	case p == "/v1/setup":
		return storage.IPSetup
	case p == "/v1/session/join-offer", r.Method == http.MethodPost && (p == "/v1/members" || p == "/v1/super-admins" ||
		p == "/v1/session/recovery" || p == "/v1/session/join-requests"):
		return storage.IPJoin
	}
	return ""
}

// ClientIP is r's client: the address the edge names in ClientIPHeader when the peer is trusted (its last value, the one
// the edge added), else the peer's own.
func (l IPLimiter) ClientIP(r *http.Request) netip.Addr {
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	addr := peer.Addr().Unmap()
	for _, t := range l.Trusted {
		if !t.Contains(addr) {
			continue
		}
		if vs := r.Header.Values(ClientIPHeader); len(vs) > 0 {
			parts := strings.Split(vs[len(vs)-1], ",")
			if a, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
				return a.Unmap()
			}
		}
		break
	}
	return addr
}

// IPKey is what the windows keep of a client: a SHA-256 of its address, never the address; an IPv6 client by its /64,
// which one subscriber holds.
func IPKey(a netip.Addr) string {
	if a.Is6() {
		if p, err := a.Prefix(64); err == nil {
			a = p.Addr()
		}
	}
	sum := sha256.Sum256([]byte("fyniq-ip:" + a.String()))
	return hex.EncodeToString(sum[:])
}

// Wrap counts each request next serves that IPKindOf names, and answers 429 with Retry-After for one over its limit. A
// window that cannot be counted lets the request through: the edge's ban still holds.
func (l IPLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind := IPKindOf(r)
		quota := l.Limits[kind]
		if kind == "" || quota <= 0 || l.Store == nil {
			next.ServeHTTP(w, r)
			return
		}
		now := l.Clock.Now()
		start := now.Truncate(time.Minute)
		used, err := l.Store.CountIP(r.Context(), IPKey(l.ClientIP(r)), kind, start)
		if err != nil {
			if l.Log != nil {
				l.Log.Warn("api.iplimit.uncounted", "kind", kind, "error", err.Error())
			}
			next.ServeHTTP(w, r)
			return
		}
		if used > quota {
			retry := int(start.Add(time.Minute).Sub(now)/time.Second) + 1
			p := problem(CodeRateLimited, fmt.Sprintf("This address made more than %d %s requests in this minute.", quota, kind),
				"Wait "+strconv.Itoa(retry)+" seconds, then try again.")
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			w.Header().Set("Content-Type", "application/problem+json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(p)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ReadIPLimits reads the deployment's per-IP limits and trusted proxies: FYNIQ_IP_LIMITS, JSON of kind → requests a
// minute (any kind left out keeps its default), and FYNIQ_TRUSTED_PROXIES, the comma-separated ranges whose
// ClientIPHeader is believed.
func ReadIPLimits(env func(string) string) (map[string]int, []netip.Prefix, error) {
	limits := map[string]int{}
	for k, v := range IPLimitDefaults {
		limits[k] = v
	}
	if raw := env("FYNIQ_IP_LIMITS"); raw != "" {
		var set map[string]int
		if err := json.Unmarshal([]byte(raw), &set); err != nil {
			return nil, nil, fmt.Errorf("FYNIQ_IP_LIMITS is not JSON of kind to requests a minute, such as {\"signin\":30}: %w", err)
		}
		for k, v := range set {
			if _, ok := IPLimitDefaults[k]; !ok || v < 1 {
				return nil, nil, fmt.Errorf("FYNIQ_IP_LIMITS: %q=%d; the kinds are signin, join, public and setup, each at least 1 a minute", k, v)
			}
			limits[k] = v
		}
	}
	var trusted []netip.Prefix
	for _, s := range strings.Split(env("FYNIQ_TRUSTED_PROXIES"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, nil, fmt.Errorf("FYNIQ_TRUSTED_PROXIES: %q is not a range such as 10.0.0.0/8", s)
		}
		trusted = append(trusted, p.Masked())
	}
	return limits, trusted, nil
}
