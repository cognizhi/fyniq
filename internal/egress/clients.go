package egress

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// The clients every outbound request goes through (SEC-36; p1-security-hardening design D3). A configured connection
// (an email provider, Hydra, the render service, Google's APIs, a sign-in provider) reaches only the hosts its
// configuration names, through the guard; the cloud's metadata server and the process itself each have one narrow
// client of their own. internal/archtest refuses any other HTTP client outside this package.

// For is a client through the guard that reaches only hosts: each an exact host name, or ".example.com" for every name
// under it. A connection names the hosts of its configured URLs (HostOf); a list with none reaches nothing.
func (g *Guard) For(hosts ...string) *http.Client {
	allow := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), ".")); h != "" && !slices.Contains(allow, h) {
			allow = append(allow, h)
		}
	}
	return g.client(func(host string, _ int) bool { return matches(allow, host) }, "is not one of this connection's hosts")
}

// matches reports whether host is one of allow: equal to an exact name, or under a ".suffix" (not the suffix itself).
func matches(allow []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, a := range allow {
		if host == a || (strings.HasPrefix(a, ".") && strings.HasSuffix(host, a)) {
			return true
		}
	}
	return false
}

// HostOf is the host a configured URL names, for For; "" when it names none.
func HostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// Bounded is a copy of the guard whose answers may be up to maxBody bytes and take up to timeout: an object download,
// or the render service's documents. Zero keeps the guard's own.
func (g *Guard) Bounded(maxBody int64, timeout time.Duration) *Guard {
	c := *g
	if maxBody > 0 {
		c.MaxBody = maxBody
	}
	if timeout > 0 {
		c.Timeout = timeout
	}
	return &c
}

// public is the guard of last resort: the public internet only, any host.
var public = sync.OnceValue(func() *http.Client { return (&Guard{}).Client(nil) })

// Public is a client through a guard with no allow-list, which reaches public addresses only: what an adapter falls
// back to when its wiring gave it no client. Wiring gives every configured connection one from For.
func Public() *http.Client { return public() }

// metadataAddr is the cloud's metadata server: the one link-local address any client may reach, and only Metadata.
const metadataAddr = "169.254.169.254:80"

// Metadata is the one client that reaches the cloud's metadata server, which the guard refuses everywhere else: it
// dials 169.254.169.254:80 itself for metadata.google.internal or that address, refuses any other host, follows no
// redirect and reads at most 64 KiB. Only the gcp adapter's token sources use it (archtest).
func Metadata() *http.Client {
	return narrow(func(host, port string) bool {
		return (host == "metadata.google.internal" || host == "169.254.169.254") && port == "80"
	}, metadataAddr, 5*time.Second, 64<<10)
}

// Loopback is a client that reaches only this machine (127.0.0.1, ::1 or localhost): the image's healthcheck of its own
// /readyz. It refuses any other host, follows no redirect and reads at most 64 KiB. Only internal/app uses it.
func Loopback() *http.Client {
	return narrow(func(host, _ string) bool { return host == "127.0.0.1" || host == "::1" || host == "localhost" }, "", 3*time.Second, 64<<10)
}

// narrow is a client for one fixed destination: allow says which host and port a request may name, and dial, when
// set, is the address it connects to whatever the name resolves to.
func narrow(allow func(host, port string) bool, dial string, timeout time.Duration, maxBody int64) *http.Client {
	d := net.Dialer{Timeout: timeout}
	return &http.Client{
		Timeout: timeout,
		Transport: limited{max: maxBody, base: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil || !allow(strings.ToLower(host), port) {
					return nil, fmt.Errorf("%w: %s is not this client's destination", ErrRefused, addr)
				}
				if dial != "" {
					addr = dial
				} else if host == "localhost" {
					addr = net.JoinHostPort("127.0.0.1", port)
				}
				return d.DialContext(ctx, network, addr)
			},
			Proxy:                 nil,
			ResponseHeaderTimeout: timeout,
			MaxIdleConns:          2,
			IdleConnTimeout:       30 * time.Second,
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirect },
	}
}
