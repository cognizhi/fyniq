// Package egress is the guard on outbound connections to addresses that come from configuration (PRD-001 SEC-36;
// OWASP SSRF prevention): a connector's endpoint, an SMTP host, a test action's target. It resolves the name and
// refuses every address that is not a public one: loopback, private, link-local (the cloud metadata service among
// them), unique-local, unspecified, multicast and shared address space, and the platform's internal names. It dials
// the address it checked, so a name that changes between the check and the connection (DNS rebinding) changes
// nothing; it follows no redirect; and it bounds time and the size of an answer. The local flavour lets the local
// substitutes through (Mailpit, Prism), on loopback and the Docker network, never the metadata service.
//
// p1-connector-framework puts connector traffic through it; p1-security-hardening moves every other outbound request
// onto it.
package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// ErrRefused is a destination the guard refuses.
var ErrRefused = errors.New("egress refused")

// ErrRedirect is a redirect, which the guard never follows.
var ErrRedirect = errors.New("egress refused: the destination answered with a redirect, which is never followed")

// ErrTooLarge is an answer larger than the guard allows.
var ErrTooLarge = errors.New("egress refused: the answer is larger than allowed")

// Resolver resolves a host name; net.DefaultResolver is one.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Guard checks and makes outbound connections.
type Guard struct {
	// Local allows loopback and private addresses, for the local flavour's substitutes; never set it in a release build.
	Local bool
	// Resolver resolves names; nil uses net.DefaultResolver.
	Resolver Resolver
	// Internal are host names refused whatever they resolve to: the platform's own services, exactly or, starting
	// with ".", by suffix.
	Internal []string
	// Timeout bounds a connection and its answer; 0 is 30 seconds.
	Timeout time.Duration
	// MaxBody bounds an HTTP answer's body; 0 is 1 MiB.
	MaxBody int64
}

// alwaysInternal are names no configuration may reach: the cloud metadata service and the platform's internal domains.
var alwaysInternal = []string{"metadata.google.internal", "metadata", ".internal", ".localhost", "localhost"}

// shared is 100.64.0.0/10, the shared address space of carrier-grade NAT; benchmark is 198.18.0.0/15.
var (
	shared    = netip.MustParsePrefix("100.64.0.0/10")
	benchmark = netip.MustParsePrefix("198.18.0.0/15")
	thisNet   = netip.MustParsePrefix("0.0.0.0/8")
	metadata  = netip.MustParseAddr("169.254.169.254")
)

// Allowed reports whether a may be connected to, and why not.
func (g *Guard) Allowed(a netip.Addr) error {
	a = a.Unmap()
	switch {
	case a == metadata, a.IsLinkLocalUnicast(), a.IsLinkLocalMulticast():
		return fmt.Errorf("%w: %s is link-local, where the cloud metadata service is", ErrRefused, a)
	case a.IsUnspecified(), thisNet.Contains(a):
		return fmt.Errorf("%w: %s is no destination", ErrRefused, a)
	case a.IsMulticast(), a.IsInterfaceLocalMulticast():
		return fmt.Errorf("%w: %s is multicast", ErrRefused, a)
	case g.Local && (a.IsLoopback() || a.IsPrivate()):
		return nil // the local flavour's substitutes
	case a.IsLoopback():
		return fmt.Errorf("%w: %s is this machine", ErrRefused, a)
	case a.IsPrivate():
		return fmt.Errorf("%w: %s is a private address", ErrRefused, a)
	case shared.Contains(a), benchmark.Contains(a):
		return fmt.Errorf("%w: %s is not a public address", ErrRefused, a)
	}
	return nil
}

func (g *Guard) internal(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, n := range append(alwaysInternal, g.Internal...) {
		if host == n || (strings.HasPrefix(n, ".") && strings.HasSuffix(host, n)) {
			return !(g.Local && (host == "localhost" || strings.HasSuffix(host, ".localhost")))
		}
	}
	return false
}

func (g *Guard) timeout() time.Duration {
	if g.Timeout <= 0 {
		return 30 * time.Second
	}
	return g.Timeout
}

// Resolve checks host and returns the addresses to dial: every address it resolves to must be allowed, so an answer
// that mixes a public and a private address is refused rather than raced.
func (g *Guard) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if host == "" {
		return nil, fmt.Errorf("%w: no host", ErrRefused)
	}
	if g.internal(host) {
		return nil, fmt.Errorf("%w: %s is one of the platform's internal names", ErrRefused, host)
	}
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		if err := g.Allowed(a); err != nil {
			return nil, err
		}
		return []netip.Addr{a}, nil
	}
	r := g.Resolver
	if r == nil {
		r = net.DefaultResolver
	}
	addrs, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("%w: %s does not resolve: %v", ErrRefused, host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: %s resolves to nothing", ErrRefused, host)
	}
	for _, a := range addrs {
		if err := g.Allowed(a); err != nil {
			return nil, fmt.Errorf("%s: %w", host, err)
		}
	}
	return addrs, nil
}

// DialContext connects to addr (host:port) through the guard: to the first address the check allowed, never to a
// name resolved again.
func (g *Guard) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRefused, err)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return nil, fmt.Errorf("%w: port %q", ErrRefused, port)
	}
	addrs, err := g.Resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: g.timeout()}
	var last error
	for _, a := range addrs {
		c, err := d.DialContext(ctx, network, net.JoinHostPort(a.String(), port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

// Client is an HTTP client through the guard for the hosts allow accepts (a connector's consented destinations); nil
// accepts any host the guard allows. It follows no redirect and reads at most MaxBody of an answer.
func (g *Guard) Client(allow func(host string, port int) bool) *http.Client {
	max := g.MaxBody
	if max <= 0 {
		max = 1 << 20
	}
	base := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrRefused, err)
			}
			p, _ := strconv.Atoi(port)
			if allow != nil && !allow(strings.ToLower(host), p) {
				return nil, fmt.Errorf("%w: %s is not one of the connector's destinations", ErrRefused, host)
			}
			return g.DialContext(ctx, network, addr)
		},
		Proxy:                 nil, // never an environment proxy: the guard is the egress path
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: g.timeout(),
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
	}
	return &http.Client{
		Timeout:       g.timeout(),
		Transport:     limited{base: base, max: max},
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirect },
	}
}

// limited bounds every answer's body.
type limited struct {
	base http.RoundTripper
	max  int64
}

func (l limited) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := l.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if res.ContentLength > l.max {
		_ = res.Body.Close()
		return nil, ErrTooLarge
	}
	res.Body = &capped{r: res.Body, left: l.max}
	return res, nil
}

type capped struct {
	r    io.ReadCloser
	left int64
}

func (c *capped) Read(p []byte) (int, error) {
	if c.left <= 0 {
		// one byte more tells a body exactly at the limit from one beyond it
		var one [1]byte
		if n, _ := c.r.Read(one[:]); n > 0 {
			return 0, ErrTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	c.left -= int64(n)
	return n, err
}

func (c *capped) Close() error { return c.r.Close() }
