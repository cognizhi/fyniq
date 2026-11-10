package egress

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
)

// Registered private endpoints (SEC-37; p1-security-hardening design D3). A destination on a private network (a
// self-hosted model, a connector inside an organisation's network) is reachable only once the platform operator registers
// it: its address or range, its port, the key its TLS certificate must present, and the organisation and credential it
// is bound to. Whoever builds a client for that credential passes the endpoints registered for it to With, and only that
// client reaches them, over TLS whose key matches the pin. An address an organisation user types reaches no private
// network: the guard refuses it before it dials. The metadata service, link-local addresses and the platform's internal
// names stay refused whatever is registered.

// Endpoint is a registered private endpoint: the address or range a client may reach, on one port, and the SHA-256 of
// the SubjectPublicKeyInfo its TLS certificate must present.
type Endpoint struct {
	Prefix netip.Prefix
	Port   int
	Pin    [32]byte
}

// ParsePin reads a pinned key as registered: the base64 of the SHA-256 of a certificate's SubjectPublicKeyInfo.
func ParsePin(s string) ([32]byte, error) {
	var pin [32]byte
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != len(pin) {
		return pin, errors.New("a pin is the base64 of a SHA-256: 44 characters ending in =")
	}
	copy(pin[:], b)
	return pin, nil
}

// PinOf is the pin of a certificate's SubjectPublicKeyInfo, as ParsePin reads it.
func PinOf(spki []byte) string {
	sum := sha256.Sum256(spki)
	return base64.StdEncoding.EncodeToString(sum[:])
}

// With is a copy of the guard that may also reach eps, each only on its port and only over TLS that presents its pinned
// key. Pass only the endpoints registered for the credential the client sends, so that credential goes nowhere else.
func (g *Guard) With(eps ...Endpoint) *Guard {
	c := *g
	c.registered = append([]Endpoint(nil), eps...)
	return &c
}

// endpoint is the registered endpoint a on port reaches, or nil. Never one for the metadata service, a link-local,
// unspecified or multicast address, whatever range was registered.
func (g *Guard) endpoint(a netip.Addr, port int) *Endpoint {
	a = a.Unmap()
	if a == metadata || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsUnspecified() || thisNet.Contains(a) || a.IsMulticast() {
		return nil
	}
	for _, own := range g.Own {
		if own.Contains(a) {
			return nil
		}
	}
	for i := range g.registered {
		if e := &g.registered[i]; e.Port == port && e.Prefix.Contains(a) {
			return e
		}
	}
	return nil
}

// IsOwn reports whether p overlaps one of the platform's own networks, which no registration reaches.
func (g *Guard) IsOwn(p netip.Prefix) bool {
	for _, own := range g.Own {
		if own.Overlaps(p) {
			return true
		}
	}
	return false
}

// dialTLS connects to addr through the guard and over TLS: to a public address with the usual verification of its
// certificate for the host name; to a registered endpoint with its pinned key, which replaces the certificate's chain.
func (g *Guard) dialTLS(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRefused, err)
	}
	conn, e, err := g.dial(ctx, network, addr, true)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if e != nil {
		pin := e.Pin
		// The registered key replaces the chain: a self-hosted endpoint's certificate is usually its own (SEC-37).
		cfg.InsecureSkipVerify = true // #nosec G402 -- VerifyConnection checks the pinned key instead
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 || sha256.Sum256(cs.PeerCertificates[0].RawSubjectPublicKeyInfo) != pin {
				return fmt.Errorf("%w: %s did not present its registered key", ErrRefused, addr)
			}
			return nil
		}
	}
	tc := tls.Client(conn, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tc, nil
}
